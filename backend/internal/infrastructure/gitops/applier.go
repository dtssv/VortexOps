// Package gitops 是自研轻量 GitOps 模块：把 releaseapp 渲染出的 K8s manifest
// 序列化为 YAML 推送到 Git 仓库（声明式状态源），同时保留对 K8s 的直接 apply。
//
// 设计：GitOpsApplier 实现 workload.Applier 同款 Apply 接口（装饰器模式），
// 在调用底层 applier 写 K8s 之前/之后，把 RenderResult 序列化为多文档 YAML
// 推送到配置的 GitOps manifest 仓库。这样：
//   - 直接 apply 路径不变（兼容非 GitOps 集群）
//   - GitOps 仓库自动获得期望态（可被 ArgoCD/Flux 等消费，或自研 sync loop 消费）
//
// 文件组织：每个 group 一个 YAML 文件，路径 <namespace>/<group-name>.yaml。
// 候选 Deployment 单独写 <namespace>/<group-name>-candidate.yaml。
package gitops

import (
	"context"
	"fmt"
	"path/filepath"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/vortexops/vortexops/internal/infrastructure/k8s/workload"
	"sigs.k8s.io/yaml"
)

// RepoManager GitOps manifest 仓库管理器接口（由 gitrepo.RepositoryManager 实现）。
type RepoManager interface {
	CommitAndPush(ctx context.Context, files map[string][]byte, message string) error
}

// GitOpsApplier 装饰 workload.Applier：apply K8s 的同时把 manifest 推到 Git。
type GitOpsApplier struct {
	inner    *workload.Applier // 底层 K8s applier（可为 nil：纯 GitOps 模式）
	repo      RepoManager
	enabled  bool // 是否启用 GitOps 推送（仓库未配置时 false）
}

// NewGitOpsApplier 创建装饰器。repo 为 nil 时降级为纯 K8s apply（透传 inner）。
func NewGitOpsApplier(inner *workload.Applier, repo RepoManager) *GitOpsApplier {
	return &GitOpsApplier{
		inner:   inner,
		repo:    repo,
		enabled: repo != nil,
	}
}

// Apply 实现 workload.Applier.Apply 同款签名（装饰器）。
// 先推 Git（声明期望态），再 apply K8s（实际部署）。
// Git 推送失败不阻断 K8s apply（软降级：GitOps 是辅助通道）。
func (a *GitOpsApplier) Apply(ctx context.Context, result *workload.RenderResult) error {
	// 1. 推 Git（软降级）。
	if a.enabled && result != nil {
		if err := a.pushToGit(ctx, result); err != nil {
			// 记录但不阻断部署；实际生产可接入日志/告警。
			fmt.Printf("gitops push warning: %v\n", err)
		}
	}
	// 2. apply K8s（透传底层 applier）。
	if a.inner == nil {
		return nil // 纯 GitOps 模式：只推 Git，不直接 apply。
	}
	return a.inner.Apply(ctx, result)
}

// pushToGit 把 RenderResult 序列化为多文档 YAML 并推送。
func (a *GitOpsApplier) pushToGit(ctx context.Context, result *workload.RenderResult) error {
	files := map[string][]byte{}

	// 主工作负载。
	if result.Workload != nil {
		name, ns := objNameNS(result.Workload)
		if name != "" {
			yamlBytes, err := toYAML(result.Workload)
			if err != nil {
				return fmt.Errorf("marshal workload: %w", err)
			}
			files[filePath(ns, name)] = yamlBytes
		}
	}

	// 候选工作负载。
	if result.CandidateWorkload != nil {
		name, ns := objNameNS(result.CandidateWorkload)
		if name != "" {
			yamlBytes, err := toYAML(result.CandidateWorkload)
			if err != nil {
				return fmt.Errorf("marshal candidate: %w", err)
			}
			files[filePath(ns, name+"-candidate")] = yamlBytes
		}
	}

	// ConfigMap。
	if result.ConfigMap != nil {
		yamlBytes, err := toYAML(result.ConfigMap)
		if err != nil {
			return fmt.Errorf("marshal configmap: %w", err)
		}
		files[filePath(result.ConfigMap.Namespace, result.ConfigMap.Name)] = yamlBytes
	}

	// HPA。
	if result.HPA != nil {
		yamlBytes, err := toYAML(result.HPA)
		if err != nil {
			return fmt.Errorf("marshal hpa: %w", err)
		}
		files[filePath(result.HPA.Namespace, result.HPA.Name)] = yamlBytes
	}

	// Cilium / Mesh CRD（unstructured）。
	for _, u := range result.CiliumResources {
		yamlBytes, err := toYAML(u)
		if err != nil {
			continue
		}
		files[filePath(u.GetNamespace(), u.GetName())] = yamlBytes
	}
	for _, u := range result.MeshResources {
		yamlBytes, err := toYAML(u)
		if err != nil {
			continue
		}
		files[filePath(u.GetNamespace(), u.GetName())] = yamlBytes
	}

	if len(files) == 0 {
		return nil
	}

	msg := fmt.Sprintf("vortexops: sync %d resources", len(files))
	return a.repo.CommitAndPush(ctx, files, msg)
}

// toYAML 把 K8s 对象序列化为 YAML。
// sigs.k8s.io/yaml.Marshal 对 typed runtime.Object 与 unstructured.Unstructured 均可工作
// （内部走 JSON 再转 YAML，保证字段顺序稳定）。
func toYAML(obj any) ([]byte, error) {
	if u, ok := obj.(*unstructured.Unstructured); ok {
		return yaml.Marshal(u.Object)
	}
	return yaml.Marshal(obj)
}

// objNameNS 从 runtime.Object 取 name/namespace（反射 unstructured 或 typed）。
func objNameNS(obj any) (string, string) {
	if u, ok := obj.(*unstructured.Unstructured); ok {
		return u.GetName(), u.GetNamespace()
	}
	if ro, ok := obj.(metav1Object); ok {
		return ro.GetName(), ro.GetNamespace()
	}
	return "", ""
}

// metav1Object 最小接口（typed K8s 对象都实现）。
type metav1Object interface {
	GetName() string
	GetNamespace() string
}

// filePath 构造仓库内文件路径：<namespace>/<name>.yaml。
// namespace 为空（集群级资源）时放 cluster/ 目录。
func filePath(namespace, name string) string {
	if namespace == "" {
		return filepath.Join("cluster", name+".yaml")
	}
	return filepath.Join(namespace, name+".yaml")
}

// DeleteDeployment 透传底层 applier 的删除（装饰器）。
// GitOps 模式下删除不推 Git（删除由 sync loop 检测 drift 处理，避免误删）。
func (a *GitOpsApplier) DeleteDeployment(ctx context.Context, namespace, name string) error {
	if a.inner == nil {
		return nil
	}
	return a.inner.DeleteDeployment(ctx, namespace, name)
}

// Delete 透传底层 applier 的删除。
func (a *GitOpsApplier) Delete(ctx context.Context, namespace, name, workloadType string) error {
	if a.inner == nil {
		return nil
	}
	return a.inner.Delete(ctx, namespace, name, workloadType)
}
