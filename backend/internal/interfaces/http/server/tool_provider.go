package server

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/vortexops/vortexops/internal/application/applicationapp"
	"github.com/vortexops/vortexops/internal/application/buildapp"
	"github.com/vortexops/vortexops/internal/application/clusterapp"
	"github.com/vortexops/vortexops/internal/application/diagnosisapp"
	"github.com/vortexops/vortexops/internal/domain/build"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// toolProvider 实现 diagnosisapp.ToolProvider 接口。
// 适配 buildapp / applicationapp / clusterapp 三个应用服务，
// 让 AI 助手能够按意图调用平台能力（获取构建日志、Pod 日志、事件等）。
type toolProvider struct {
	builds    *buildapp.Service
	apps      *applicationapp.Service
	clusters  *clusterapp.Service
	jenkins  buildapp.JenkinsClientFactory
}

// newToolProvider 创建工具提供者。
// jenkins 用于构建日志拉取（与 buildhttp.Handler 共用同一个 factory）。
func newToolProvider(builds *buildapp.Service, apps *applicationapp.Service, clusters *clusterapp.Service, jenkins buildapp.JenkinsClientFactory) *toolProvider {
	return &toolProvider{builds: builds, apps: apps, clusters: clusters, jenkins: jenkins}
}

func (t *toolProvider) GetBuild(ctx context.Context, buildID int64) (*diagnosisapp.BuildInfo, error) {
	b, err := t.builds.GetBuild(ctx, buildID)
	if err != nil {
		return nil, err
	}
	return &diagnosisapp.BuildInfo{
		ID:            b.ID,
		ApplicationID: b.ApplicationID,
		BuildNumber:   b.BuildNumber,
		Branch:        b.RefValue,
		CommitSHA:     b.CommitSHA,
		Status:        string(b.Status),
		ErrorMessage:  coalesceStr(b.FailureReason, b.LogExcerpt),
		ImageRepo:     b.TargetRepository,
		ImageTag:      b.TargetTag,
		StartedAt:     timeStr(b.StartedAt),
		FinishedAt:    timeStr(b.FinishedAt),
		DurationMs:    b.DurationMs,
	}, nil
}

func (t *toolProvider) GetBuildLogs(ctx context.Context, buildID int64) (string, error) {
	logs, _, _, err := t.builds.GetBuildLogs(ctx, buildID, t.jenkins, 0)
	if err != nil {
		return "", err
	}
	return string(logs), nil
}

func (t *toolProvider) ListBuildSteps(ctx context.Context, buildID int64) ([]diagnosisapp.BuildStepInfo, error) {
	steps, err := t.builds.ListSteps(ctx, buildID)
	if err != nil {
		return nil, err
	}
	out := make([]diagnosisapp.BuildStepInfo, 0, len(steps))
	for _, s := range steps {
		out = append(out, diagnosisapp.BuildStepInfo{
			Name:       s.Name,
			Status:     string(s.Status),
			Message:    s.Message,
			DurationMs: s.DurationMs,
		})
	}
	return out, nil
}

func (t *toolProvider) FindFailedBuildsByApp(ctx context.Context, appID int64, limit int) ([]diagnosisapp.BuildInfo, error) {
	if limit <= 0 {
		limit = 5
	}
	items, _, err := t.builds.ListBuilds(ctx, appID, build.BuildFailed, 0, 1, limit)
	if err != nil {
		return nil, err
	}
	out := make([]diagnosisapp.BuildInfo, 0, len(items))
	for _, b := range items {
		out = append(out, diagnosisapp.BuildInfo{
			ID:            b.ID,
			ApplicationID: b.ApplicationID,
			BuildNumber:   b.BuildNumber,
			Branch:        b.RefValue,
			CommitSHA:     b.CommitSHA,
			Status:        string(b.Status),
			ErrorMessage:  coalesceStr(b.FailureReason, b.LogExcerpt),
			ImageRepo:     b.TargetRepository,
			ImageTag:      b.TargetTag,
			StartedAt:     timeStr(b.StartedAt),
			FinishedAt:    timeStr(b.FinishedAt),
			DurationMs:    b.DurationMs,
		})
	}
	return out, nil
}

func (t *toolProvider) GetGroup(ctx context.Context, groupID int64) (*diagnosisapp.GroupInfo, error) {
	g, err := t.apps.GetGroup(ctx, groupID)
	if err != nil {
		return nil, err
	}
	return &diagnosisapp.GroupInfo{
		ID:            g.ID,
		ApplicationID: g.ApplicationID,
		Name:          g.Name,
		ClusterID:     g.ClusterID,
		Namespace:     g.Namespace,
		Replicas:      g.Replicas,
	}, nil
}

func (t *toolProvider) FindGroupByName(ctx context.Context, name string) ([]diagnosisapp.GroupInfo, error) {
	if name == "" {
		return nil, apperr.Validation("name is required", nil)
	}
	// ListGroups 支持 search 模糊匹配；这里取前 10 条。
	items, _, err := t.apps.ListGroups(ctx, 0, "", 0, "", name, 1, 10)
	if err != nil {
		return nil, err
	}
	out := make([]diagnosisapp.GroupInfo, 0, len(items))
	for _, g := range items {
		out = append(out, diagnosisapp.GroupInfo{
			ID:            g.ID,
			ApplicationID: g.ApplicationID,
			Name:          g.Name,
			ClusterID:     g.ClusterID,
			Namespace:     g.Namespace,
			Replicas:      g.Replicas,
		})
	}
	return out, nil
}

func (t *toolProvider) ListGroupPods(ctx context.Context, groupID int64) ([]diagnosisapp.PodInfo, error) {
	g, err := t.apps.GetGroup(ctx, groupID)
	if err != nil {
		return nil, err
	}
	selector := fmt.Sprintf("app.kubernetes.io/instance=%s", g.Name)
	pods, err := t.clusters.ListGroupPods(ctx, g.ClusterID, g.Namespace, selector)
	if err != nil {
		return nil, err
	}
	out := make([]diagnosisapp.PodInfo, 0, len(pods))
	for _, p := range pods {
		pi := diagnosisapp.PodInfo{
			Name:         p.Name,
			Namespace:    p.Namespace,
			Phase:        p.Phase,
			NodeName:     p.NodeName,
			RestartCount: p.RestartCount,
			Ready:        p.Ready,
		}
		for _, c := range p.Containers {
			pi.Containers = append(pi.Containers, diagnosisapp.PodContainerInfo{
				Name:         c.Name,
				Image:        c.Image,
				Ready:        c.Ready,
				Started:      c.Started,
				RestartCount: c.RestartCount,
			})
		}
		out = append(out, pi)
	}
	return out, nil
}

func (t *toolProvider) GetPodLogs(ctx context.Context, clusterID int64, namespace, pod, container string, tail int64) (string, error) {
	if clusterID == 0 || namespace == "" || pod == "" {
		return "", apperr.Validation("cluster_id, namespace, pod are required", nil)
	}
	var buf bytes.Buffer
	err := t.clusters.StreamPodLogs(ctx, clusterapp.PodLogsInput{
		ClusterID: clusterID, Namespace: namespace, Pod: pod,
		Container: container, TailLines: tail, Follow: false,
	}, &buf)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (t *toolProvider) ListPodEvents(ctx context.Context, clusterID int64, namespace, pod string) ([]diagnosisapp.PodEventInfo, error) {
	if clusterID == 0 || namespace == "" || pod == "" {
		return nil, apperr.Validation("cluster_id, namespace, pod are required", nil)
	}
	// ListGroupEvents 按 selector 过滤；这里按 pod 名过滤需直接调用 K8s。
	// 复用 clusterSvc 的 ListGroupEvents，selector 留空取命名空间全部事件再过滤。
	events, err := t.clusters.ListGroupEvents(ctx, clusterID, namespace, "")
	if err != nil {
		return nil, err
	}
	out := make([]diagnosisapp.PodEventInfo, 0, len(events))
	for _, e := range events {
		// 简单按 message 包含 pod 名过滤（ListGroupEvents 未按 involvedObject.name 过滤）。
		if pod != "" && !strings.Contains(e.Message, pod) {
			continue
		}
		out = append(out, diagnosisapp.PodEventInfo{
			Type:     e.Type,
			Reason:   e.Reason,
			Message:  e.Message,
			Count:    e.Count,
			LastTime: e.LastTime,
		})
	}
	return out, nil
}

func (t *toolProvider) DescribePod(ctx context.Context, clusterID int64, namespace, pod string) (string, error) {
	if clusterID == 0 || namespace == "" || pod == "" {
		return "", apperr.Validation("cluster_id, namespace, pod are required", nil)
	}
	// 列出命名空间下所有 Pod 再过滤目标 Pod（ListGroupPods selector 留空取全部）。
	pods, err := t.clusters.ListGroupPods(ctx, clusterID, namespace, "")
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, p := range pods {
		if p.Name != pod {
			continue
		}
		fmt.Fprintf(&b, "Pod: %s\nPhase: %s\nNode: %s\nReady: %v\nRestarts: %d\n",
			p.Name, p.Phase, p.NodeName, p.Ready, p.RestartCount)
		for _, c := range p.Containers {
			fmt.Fprintf(&b, "- 容器 %s: ready=%v started=%v restarts=%d image=%s\n",
				c.Name, c.Ready, c.Started, c.RestartCount, c.Image)
		}
	}
	events, _ := t.ListPodEvents(ctx, clusterID, namespace, pod)
	if len(events) > 0 {
		fmt.Fprintln(&b, "\n事件:")
		for _, e := range events {
			fmt.Fprintf(&b, "- [%s] %s (%dx) %s\n", e.Type, e.Reason, e.Count, e.Message)
		}
	}
	if b.Len() == 0 {
		return fmt.Sprintf("Pod %s/%s 未找到", namespace, pod), nil
	}
	return b.String(), nil
}

// coalesceStr 返回第一个非空字符串。
func coalesceStr(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

// timeStr 安全格式化时间指针（nil 返回空字符串）。
func timeStr(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02 15:04:05")
}
