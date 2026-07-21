// Package tekton 提供基于 Tekton PipelineRun/TaskRun CRD 的构建引擎客户端。
// 不依赖 tektoncd/client-go，改用 k8s dynamic client + unstructured 访问 CRD，
// 避免 引入庞大的 Tekton clientset 依赖。构建集群为平台自有集群（Option B），
// kubeconfig 来自系统设置 tekton.kubeconfig（为空时使用 in-cluster 配置）。
package tekton

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/vortexops/vortexops/internal/domain/build"
	"github.com/vortexops/vortexops/internal/infrastructure/k8s"
	"github.com/vortexops/vortexops/pkg/apperr"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

var (
	pipelineRunGVR = schema.GroupVersionResource{
		Group: "tekton.dev", Version: "v1", Resource: "pipelineruns",
	}
	taskRunGVR = schema.GroupVersionResource{
		Group: "tekton.dev", Version: "v1", Resource: "taskruns",
	}
)

// Client 实现 build.BuildEngineClient，通过 dynamic client 操作 Tekton CRD。
type Client struct {
	dynamic  dynamic.Interface
	core     kubernetes.Interface
	namespace string
}

// NewFromConfig 按系统设置中的 kubeconfig 与 namespace 构建 Tekton 客户端。
// kubeconfigBase64 为空时使用 in-cluster 配置（apiserver 运行在构建集群内）。
func NewFromConfig(kubeconfigRaw, namespace string) (*Client, error) {
	if namespace == "" {
		namespace = "vo-builds"
	}
	cfg, err := buildRESTConfig(kubeconfigRaw)
	if err != nil {
		return nil, err
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("build dynamic client: %w", err)
	}
	core, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("build core client: %w", err)
	}
	return &Client{dynamic: dyn, core: core, namespace: namespace}, nil
}

func buildRESTConfig(kubeconfigRaw string) (*rest.Config, error) {
	raw := strings.TrimSpace(kubeconfigRaw)
	if raw == "" || raw == "\"\"" {
		// 尝试 in-cluster 配置。
		cfg, err := rest.InClusterConfig()
		if err == nil {
			cfg.QPS = 50
			cfg.Burst = 100
			return cfg, nil
		}
		return nil, apperr.Internal("tekton kubeconfig 未配置且不在集群内运行", err)
	}
	// 兼容 base64 编码或裸 PEM 文本。
	var kubeconfigBytes []byte
	if decoded, err := base64.StdEncoding.DecodeString(raw); err == nil && len(decoded) > 0 {
		kubeconfigBytes = decoded
	} else {
		kubeconfigBytes = []byte(raw)
	}
	cfg, err := k8s.BuildFromKubeconfig(kubeconfigBytes, false)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

// Trigger 创建一个 Tekton PipelineRun 并返回其名称作为 runID。
// PipelineRun 模板内联 git-clone + buildkit 两个 Task，参数由 params 传入。
func (c *Client) Trigger(ctx context.Context, buildID int64, params map[string]string) (string, error) {
	runName := fmt.Sprintf("vo-build-%d-%d", buildID, time.Now().Unix())
	pr := buildPipelineRun(runName, c.namespace, params)
	_, err := c.dynamic.Resource(pipelineRunGVR).Namespace(c.namespace).Create(ctx, pr, metav1.CreateOptions{})
	if err != nil {
		return "", fmt.Errorf("create PipelineRun: %w", err)
	}
	return runName, nil
}

// GetStatus 查询 PipelineRun 状态，归一化为 build.BuildStatus。
func (c *Client) GetStatus(ctx context.Context, runID string) (build.BuildStatus, bool, error) {
	obj, err := c.dynamic.Resource(pipelineRunGVR).Namespace(c.namespace).Get(ctx, runID, metav1.GetOptions{})
	if err != nil {
		return "", false, fmt.Errorf("get PipelineRun: %w", err)
	}
	conditions, _, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	for _, condAny := range conditions {
		cond, ok := condAny.(map[string]any)
		if !ok {
			continue
		}
		if t, _ := cond["type"].(string); t != "Succeeded" {
			continue
		}
		status, _ := cond["status"].(string)
		reason, _ := cond["reason"].(string)
		switch status {
		case "True":
			return build.BuildSuccess, false, nil
		case "False":
			if strings.Contains(strings.ToLower(reason), "cancel") {
				return build.BuildCanceled, false, nil
			}
			if strings.Contains(strings.ToLower(reason), "timeout") {
				return build.BuildTimeout, false, nil
			}
			return build.BuildFailed, false, nil
		case "Unknown":
			return build.BuildRunning, true, nil
		}
	}
	// 无 status.conditions：尚未调度，视为 pending/running。
	return build.BuildRunning, true, nil
}

// GetLog 聚合 PipelineRun 下所有 TaskRun Pod 的容器日志，从 start 偏移开始。
// Tekton 没有原生增量日志 API，这里每次拉取全量并按 start 截断；hasMore 固定 false。
func (c *Client) GetLog(ctx context.Context, runID string, start int64) (string, bool, error) {
	trs, err := c.dynamic.Resource(taskRunGVR).Namespace(c.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("tekton.dev/pipelineRun=%s", runID),
	})
	if err != nil {
		return "", false, fmt.Errorf("list TaskRuns: %w", err)
	}
	var buf strings.Builder
	for _, tr := range trs.Items {
		podName, _, _ := unstructured.NestedString(tr.Object, "status", "podName")
		if podName == "" {
			continue
		}
		// 取 TaskRun 对应的 step 容器名（前缀 step-）。
		containers, _, _ := unstructured.NestedSlice(tr.Object, "status", "steps")
		for _, csAny := range containers {
			cs, ok := csAny.(map[string]any)
			if !ok {
				continue
			}
			containerName, _ := cs["container"].(string)
			if containerName == "" {
				containerName = "step"
			}
			logs, lerr := c.core.CoreV1().Pods(c.namespace).GetLogs(podName, &corev1.PodLogOptions{
				Container: containerName,
			}).DoRaw(ctx)
			if lerr == nil {
				buf.Write(logs)
				buf.WriteString("\n")
			}
		}
	}
	full := buf.String()
	if start >= int64(len(full)) {
		return "", false, nil
	}
	return full[start:], false, nil
}

// ListSteps 返回 PipelineRun 下所有 TaskRun 作为分步信息。
func (c *Client) ListSteps(ctx context.Context, runID string) ([]build.EngineStep, error) {
	trs, err := c.dynamic.Resource(taskRunGVR).Namespace(c.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("tekton.dev/pipelineRun=%s", runID),
	})
	if err != nil {
		return nil, fmt.Errorf("list TaskRuns: %w", err)
	}
	steps := make([]build.EngineStep, 0, len(trs.Items))
	for _, tr := range trs.Items {
		name, _, _ := unstructured.NestedString(tr.Object, "metadata", "name")
		taskName, _, _ := unstructured.NestedString(tr.Object, "spec", "taskRef", "name")
		if taskName == "" {
			taskName = name
		}
		step := build.EngineStep{Name: taskName, Status: build.StepRunning}
		conditions, _, _ := unstructured.NestedSlice(tr.Object, "status", "conditions")
		for _, condAny := range conditions {
			cond, ok := condAny.(map[string]any)
			if !ok {
				continue
			}
			if t, _ := cond["type"].(string); t != "Succeeded" {
				continue
			}
			switch s, _ := cond["status"].(string); s {
			case "True":
				step.Status = build.StepSuccess
			case "False":
				step.Status = build.StepFailed
				if msg, _ := cond["message"].(string); msg != "" {
					step.Message = msg
				}
			case "Unknown":
				step.Status = build.StepRunning
			}
			break
		}
		if startTime, found, _ := unstructured.NestedString(tr.Object, "status", "startTime"); found {
			if t, perr := time.Parse(time.RFC3339, startTime); perr == nil {
				step.StartedAt = &t
			}
		}
		if completionTime, found, _ := unstructured.NestedString(tr.Object, "status", "completionTime"); found {
			if t, perr := time.Parse(time.RFC3339, completionTime); perr == nil {
				step.FinishedAt = &t
			}
		}
		steps = append(steps, step)
	}
	return steps, nil
}

// Stop 取消运行中的 PipelineRun（设置 spec.status=Cancelled）。
func (c *Client) Stop(ctx context.Context, runID string) error {
	obj, err := c.dynamic.Resource(pipelineRunGVR).Namespace(c.namespace).Get(ctx, runID, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get PipelineRun for cancel: %w", err)
	}
	if err := unstructured.SetNestedField(obj.Object, "Cancelled", "spec", "status"); err != nil {
		return fmt.Errorf("set cancel status: %w", err)
	}
	_, err = c.dynamic.Resource(pipelineRunGVR).Namespace(c.namespace).Update(ctx, obj, metav1.UpdateOptions{})
	return err
}

// buildPipelineRun 构造内联 PipelineRun 对象（传统 CI 模式）：
//   - git-clone Task：克隆源码到 workspace
//   - build Task：用 builder_image 跑 BUILD_COMMAND 产出制品（在 workspace 上）
//   - build-and-push Task：用单阶段运行时 Dockerfile 构建（只 COPY 制品）并 push
//
// 参数通过 params 传入（REPO_URL/REF_VALUE/COMMIT_SHA/IMAGE_REGISTRY/IMAGE_REPO/IMAGE_TAG/
// BUILD_COMMAND/BUILDER_IMAGE/ARTIFACT_PATH/DOCKERFILE_PATH/DOCKERFILE/BUILD_ARGS_JSON 等）。
func buildPipelineRun(name, namespace string, params map[string]string) *unstructured.Unstructured {
	repoURL := params["REPO_URL"]
	refValue := params["REF_VALUE"]
	commitSHA := params["COMMIT_SHA"]
	imageRegistry := params["IMAGE_REGISTRY"]
	imageRepo := params["IMAGE_REPO"]
	imageTag := params["IMAGE_TAG"]
	buildCommand := params["BUILD_COMMAND"]
	builderImage := params["BUILDER_IMAGE"]
	dockerfileContent := params["DOCKERFILE"]
	dockerfilePath := params["DOCKERFILE_PATH"]
	if dockerfilePath == "" {
		dockerfilePath = "Dockerfile"
	}
	buildArgsJSON := params["BUILD_ARGS_JSON"]
	fullImage := fmt.Sprintf("%s/%s:%s", imageRegistry, imageRepo, imageTag)

	// build-and-push step 的脚本：template 模式写入渲染后的 Dockerfile；repo 模式用仓库自带。
	// BUILD_ARGS_JSON 解析为 buildctl --opt build-arg:<k>=<v>。
	buildPushScript := `#!/usr/bin/env sh
set -e
cd "$(workspaces.source.path)"
` + renderDockerfileWriteStep(dockerfileContent) + `
ARGS=""
` + buildArgsShell(buildArgsJSON) + `
DF="Dockerfile.generated"
if [ ! -f "$DF" ]; then DF="` + dockerfilePath + `"; fi
buildctl build \
  --frontend dockerfile.v0 \
  --opt context=. \
  --opt filename=$DF \
  $ARGS \
  --output type=image,name=$(params.image),push=true`

	// build Task 仅在 builder_image 与 build_command 均非空时插入。
	tasks := []map[string]any{
		{
			"name": "git-clone",
			"taskSpec": map[string]any{
				"workspaces": []map[string]any{{"name": "output"}},
				"params": []map[string]any{
					{"name": "url", "type": "string"},
					{"name": "revision", "type": "string"},
				},
				"steps": []map[string]any{
					{
						"name":  "clone",
						"image": "alpine/git:latest",
						"script": `#!/usr/bin/env sh
set -e
git clone "$(params.url)" "$(workspaces.output.path)"
cd "$(workspaces.output.path)"
git checkout "$(params.revision)" || true`,
					},
				},
			},
			"params": []map[string]any{
				{"name": "url", "value": "$(params.repo-url)"},
				{"name": "revision", "value": "$(params.revision)"},
			},
			"workspaces": []map[string]any{
				{"name": "output", "workspace": "source"},
			},
		},
	}
	afterClone := "git-clone"
	if builderImage != "" && buildCommand != "" {
		tasks = append(tasks, map[string]any{
			"name":     "build",
			"runAfter": []string{"git-clone"},
			"taskSpec": map[string]any{
				"workspaces": []map[string]any{{"name": "source"}},
				"params": []map[string]any{
					{"name": "build-command", "type": "string"},
				},
				"steps": []map[string]any{
					{
						"name":   "build",
						"image":  builderImage,
						"workingDir": "$(workspaces.source.path)",
						"script": "#!/usr/bin/env sh\nset -e\n" + buildCommand + "\n",
					},
				},
			},
			"params": []map[string]any{
				{"name": "build-command", "value": "$(params.build-command)"},
			},
			"workspaces": []map[string]any{
				{"name": "source", "workspace": "source"},
			},
		})
		afterClone = "build"
	}
	tasks = append(tasks, map[string]any{
		"name":     "build-and-push",
		"runAfter": []string{afterClone},
		"taskSpec": map[string]any{
			"workspaces": []map[string]any{{"name": "source"}},
			"params": []map[string]any{
				{"name": "image", "type": "string"},
			},
			"steps": []map[string]any{
				{
					"name":  "build",
					"image": "moby/buildkit:rootless",
					"securityContext": map[string]any{
						"privileged": true,
					},
					"env": []map[string]any{
						{"name": "BUILDKITD_FLAGS", "value": "--oci-worker-no-process-sandbox"},
					},
					"script": buildPushScript,
				},
			},
		},
		"params": []map[string]any{
			{"name": "image", "value": "$(params.image)"},
		},
		"workspaces": []map[string]any{
			{"name": "source", "workspace": "source"},
		},
	})

	pipelineParams := []map[string]any{
		{"name": "repo-url", "value": repoURL},
		{"name": "revision", "value": pickRevision(commitSHA, refValue)},
		{"name": "image", "value": fullImage},
	}
	paramDecls := []map[string]any{
		{"name": "repo-url", "type": "string"},
		{"name": "revision", "type": "string"},
		{"name": "image", "type": "string"},
	}
	if builderImage != "" && buildCommand != "" {
		paramDecls = append(paramDecls, map[string]any{"name": "build-command", "type": "string"})
		pipelineParams = append(pipelineParams, map[string]any{"name": "build-command", "value": buildCommand})
	}

	pr := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "tekton.dev/v1",
			"kind":       "PipelineRun",
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
				"labels": map[string]any{
					"app.kubernetes.io/managed-by": "vortexops",
					"vortexops.io/build-id":        params["BUILD_ID"],
				},
			},
			"spec": map[string]any{
				"pipelineSpec": map[string]any{
					"workspaces": []map[string]any{
						{"name": "source"},
					},
					"params": paramDecls,
					"tasks":  tasks,
				},
				"params": pipelineParams,
				"workspaces": []map[string]any{
					{
						"name": "source",
						"volumeClaimTemplate": map[string]any{
							"spec": map[string]any{
								"accessModes": []string{"ReadWriteOnce"},
								"resources": map[string]any{
									"requests": map[string]any{
										"storage": "1Gi",
									},
								},
							},
						},
					},
				},
				"timeout": "1h0m0s",
			},
		},
	}
	return pr
}

// renderDockerfileWriteStep 返回 shell 片段：若渲染后的 Dockerfile 内容非空，写入 Dockerfile.generated。
func renderDockerfileWriteStep(dockerfileContent string) string {
	if strings.TrimSpace(dockerfileContent) == "" {
		return ""
	}
	// 用 heredoc 写入避免转义问题。
	return "cat > Dockerfile.generated <<'VORTEXOPS_DOCKERFILE_EOF'\n" + dockerfileContent + "\nVORTEXOPS_DOCKERFILE_EOF"
}

// buildArgsShell 解析 BUILD_ARGS_JSON 为 buildctl --opt build-arg:<k>=<v> 的 shell 片段。
// 解析失败时返回空串（不阻断构建）。
func buildArgsShell(buildArgsJSON string) string {
	if strings.TrimSpace(buildArgsJSON) == "" {
		return ""
	}
	var args map[string]string
	if err := json.Unmarshal([]byte(buildArgsJSON), &args); err != nil {
		return ""
	}
	var b strings.Builder
	for k, v := range args {
		b.WriteString("ARGS=\"$ARGS --opt build-arg:")
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(v)
		b.WriteString("\"\n")
	}
	return b.String()
}

func pickRevision(commitSHA, refValue string) string {
	if commitSHA != "" {
		return commitSHA
	}
	return refValue
}

// 编译期断言：Client 实现 build.BuildEngineClient。
var _ build.BuildEngineClient = (*Client)(nil)

// 避免未使用 import（meta 在条件判断的扩展场景预留）。
var _ = meta.IsStatusConditionFalse
