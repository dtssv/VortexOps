package diagnosisapp

import (
	"context"
	"fmt"
	"strings"
)

// ToolProvider 提供诊断工具可调用的平台能力。
// 实现由 server 层注入（适配 buildapp / applicationapp / clusterapp），
// 避免 diagnosisapp 直接依赖这些应用服务（保持依赖单向）。
type ToolProvider interface {
	// GetBuild 获取构建详情（状态、错误信息、镜像等）。
	GetBuild(ctx context.Context, buildID int64) (*BuildInfo, error)
	// GetBuildLogs 获取构建日志（已截断）。
	GetBuildLogs(ctx context.Context, buildID int64) (string, error)
	// ListBuildSteps 获取构建步骤。
	ListBuildSteps(ctx context.Context, buildID int64) ([]BuildStepInfo, error)
	// FindFailedBuildsByApp 按应用 ID 查找最近的失败构建。
	FindFailedBuildsByApp(ctx context.Context, appID int64, limit int) ([]BuildInfo, error)

	// GetGroup 获取分组详情（含 cluster_id / namespace）。
	GetGroup(ctx context.Context, groupID int64) (*GroupInfo, error)
	// FindGroupByName 按名称模糊查找分组。
	FindGroupByName(ctx context.Context, name string) ([]GroupInfo, error)
	// ListGroupPods 列出分组下的 Pod。
	ListGroupPods(ctx context.Context, groupID int64) ([]PodInfo, error)
	// GetPodLogs 获取 Pod 日志（已截断）。
	GetPodLogs(ctx context.Context, clusterID int64, namespace, pod, container string, tail int64) (string, error)
	// ListPodEvents 列出 Pod 相关事件。
	ListPodEvents(ctx context.Context, clusterID int64, namespace, pod string) ([]PodEventInfo, error)
	// DescribePod 获取 Pod describe 摘要（phase、容器状态、事件）。
	DescribePod(ctx context.Context, clusterID int64, namespace, pod string) (string, error)
}

// BuildInfo 构建摘要（工具调用返回）。
type BuildInfo struct {
	ID            int64  `json:"id"`
	ApplicationID int64  `json:"application_id"`
	BuildNumber   int    `json:"build_number"`
	Branch        string `json:"branch"`
	CommitSHA     string `json:"commit_sha"`
	Status        string `json:"status"`
	ErrorMessage  string `json:"error_message"`
	ImageRepo     string `json:"image_repository"`
	ImageTag      string `json:"image_tag"`
	StartedAt     string `json:"started_at"`
	FinishedAt    string `json:"finished_at"`
	DurationMs    int64  `json:"duration_ms"`
}

// BuildStepInfo 构建步骤摘要。
type BuildStepInfo struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Message    string `json:"message"`
	DurationMs int64  `json:"duration_ms"`
}

// GroupInfo 分组摘要。
type GroupInfo struct {
	ID            int64  `json:"id"`
	ApplicationID int64  `json:"application_id"`
	Name          string `json:"name"`
	ClusterID     int64  `json:"cluster_id"`
	Namespace     string `json:"namespace"`
	Replicas      int    `json:"replicas"`
}

// PodInfo Pod 摘要。
type PodInfo struct {
	Name         string `json:"name"`
	Namespace    string `json:"namespace"`
	Phase        string `json:"phase"`
	NodeName     string `json:"node_name"`
	RestartCount int    `json:"restart_count"`
	Ready        bool   `json:"ready"`
	Containers   []PodContainerInfo `json:"containers"`
}

// PodContainerInfo 容器摘要。
type PodContainerInfo struct {
	Name          string `json:"name"`
	Image         string `json:"image"`
	Ready         bool   `json:"ready"`
	Started       bool   `json:"started"`
	RestartCount  int    `json:"restart_count"`
}

// PodEventInfo Pod 事件摘要。
type PodEventInfo struct {
	Type      string `json:"type"`
	Reason    string `json:"reason"`
	Message   string `json:"message"`
	Count     int32  `json:"count"`
	LastTime  string `json:"last_time"`
}

// noOpToolProvider 未注入时的占位实现（所有方法返回 ErrToolNotConfigured）。
type noOpToolProvider struct{}

func (noOpToolProvider) GetBuild(ctx context.Context, buildID int64) (*BuildInfo, error) {
	return nil, ErrToolNotConfigured
}
func (noOpToolProvider) GetBuildLogs(ctx context.Context, buildID int64) (string, error) {
	return "", ErrToolNotConfigured
}
func (noOpToolProvider) ListBuildSteps(ctx context.Context, buildID int64) ([]BuildStepInfo, error) {
	return nil, ErrToolNotConfigured
}
func (noOpToolProvider) FindFailedBuildsByApp(ctx context.Context, appID int64, limit int) ([]BuildInfo, error) {
	return nil, ErrToolNotConfigured
}
func (noOpToolProvider) GetGroup(ctx context.Context, groupID int64) (*GroupInfo, error) {
	return nil, ErrToolNotConfigured
}
func (noOpToolProvider) FindGroupByName(ctx context.Context, name string) ([]GroupInfo, error) {
	return nil, ErrToolNotConfigured
}
func (noOpToolProvider) ListGroupPods(ctx context.Context, groupID int64) ([]PodInfo, error) {
	return nil, ErrToolNotConfigured
}
func (noOpToolProvider) GetPodLogs(ctx context.Context, clusterID int64, namespace, pod, container string, tail int64) (string, error) {
	return "", ErrToolNotConfigured
}
func (noOpToolProvider) ListPodEvents(ctx context.Context, clusterID int64, namespace, pod string) ([]PodEventInfo, error) {
	return nil, ErrToolNotConfigured
}
func (noOpToolProvider) DescribePod(ctx context.Context, clusterID int64, namespace, pod string) (string, error) {
	return "", ErrToolNotConfigured
}

// ErrToolNotConfigured 工具未配置（diagnosisapp 未注入 ToolProvider）。
var ErrToolNotConfigured = fmt.Errorf("tool provider not configured")

// toolName 工具调用名称（前端展示）。
const (
	ToolGetBuild          = "get_build"
	ToolGetBuildLogs      = "get_build_logs"
	ToolListBuildSteps    = "list_build_steps"
	ToolFindFailedBuilds  = "find_failed_builds"
	ToolGetGroup          = "get_group"
	ToolFindGroupByName   = "find_group_by_name"
	ToolListGroupPods     = "list_group_pods"
	ToolGetPodLogs        = "get_pod_logs"
	ToolListPodEvents     = "list_pod_events"
	ToolDescribePod       = "describe_pod"
	ToolSearchFAQ         = "search_faq"
)

// ToolDefinition 工具定义（用于 LLM 工具调用提示）。
type ToolDefinition struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// AvailableTools 平台可用的工具列表（用于意图识别提示）。
func AvailableTools() []ToolDefinition {
	return []ToolDefinition{
		{Name: ToolGetBuild, Description: "按构建 ID 获取构建详情（状态、错误信息、镜像）"},
		{Name: ToolGetBuildLogs, Description: "按构建 ID 获取构建日志尾部（用于分析构建失败原因）"},
		{Name: ToolListBuildSteps, Description: "按构建 ID 获取分步骤状态（Tekton TaskRun / Jenkins stage）"},
		{Name: ToolFindFailedBuilds, Description: "按应用 ID 查找最近的失败构建"},
		{Name: ToolGetGroup, Description: "按分组 ID 获取分组详情（集群、命名空间、副本数）"},
		{Name: ToolFindGroupByName, Description: "按名称模糊查找分组"},
		{Name: ToolListGroupPods, Description: "按分组 ID 列出 Pod（phase、就绪、重启次数）"},
		{Name: ToolGetPodLogs, Description: "获取 Pod 日志尾部（用于分析启动失败/崩溃原因）"},
		{Name: ToolListPodEvents, Description: "列出 Pod 相关 K8s 事件（FailedScheduling/Unhealthy 等）"},
		{Name: ToolDescribePod, Description: "获取 Pod describe 摘要（phase、容器状态、事件）"},
		{Name: ToolSearchFAQ, Description: "检索内置知识库 FAQ（构建/发布/K8s/系统常见问题）"},
	}
}

// toolCall 执行一次工具调用，返回结果字符串与展示用的 ToolCall 记录。
func (s *Service) toolCall(ctx context.Context, name string, args map[string]string) (result string, display ToolCall, err error) {
	display.Name = name
	display.Arguments = formatArgs(args)
	defer func() { display.Result = truncate(result, 1000) }()

	getInt := func(k string) int64 {
		var v int64
		fmt.Sscanf(args[k], "%d", &v)
		return v
	}

	switch name {
	case ToolGetBuild:
		b, e := s.tools.GetBuild(ctx, getInt("build_id"))
		if e != nil {
			return "", display, e
		}
		result = fmt.Sprintf("构建 #%d: status=%s branch=%s image=%s:%s\n错误信息: %s\n开始: %s 完成: %s 耗时: %dms",
			b.ID, b.Status, b.Branch, b.ImageRepo, b.ImageTag, b.ErrorMessage, b.StartedAt, b.FinishedAt, b.DurationMs)
		return result, display, nil

	case ToolGetBuildLogs:
		logs, e := s.tools.GetBuildLogs(ctx, getInt("build_id"))
		if e != nil {
			return "", display, e
		}
		return truncate(logs, 8000), display, nil

	case ToolListBuildSteps:
		steps, e := s.tools.ListBuildSteps(ctx, getInt("build_id"))
		if e != nil {
			return "", display, e
		}
		var b strings.Builder
		for _, st := range steps {
			fmt.Fprintf(&b, "- %s: %s (%dms) %s\n", st.Name, st.Status, st.DurationMs, st.Message)
		}
		return b.String(), display, nil

	case ToolFindFailedBuilds:
		builds, e := s.tools.FindFailedBuildsByApp(ctx, getInt("app_id"), 5)
		if e != nil {
			return "", display, e
		}
		var b strings.Builder
		for _, bd := range builds {
			fmt.Fprintf(&b, "- 构建 #%d: %s branch=%s err=%s\n", bd.ID, bd.Status, bd.Branch, bd.ErrorMessage)
		}
		return b.String(), display, nil

	case ToolGetGroup:
		g, e := s.tools.GetGroup(ctx, getInt("group_id"))
		if e != nil {
			return "", display, e
		}
		return fmt.Sprintf("分组 %s (id=%d): cluster=%d namespace=%s replicas=%d",
			g.Name, g.ID, g.ClusterID, g.Namespace, g.Replicas), display, nil

	case ToolFindGroupByName:
		groups, e := s.tools.FindGroupByName(ctx, args["name"])
		if e != nil {
			return "", display, e
		}
		var b strings.Builder
		for _, g := range groups {
			fmt.Fprintf(&b, "- %s (id=%d cluster=%d ns=%s)\n", g.Name, g.ID, g.ClusterID, g.Namespace)
		}
		return b.String(), display, nil

	case ToolListGroupPods:
		pods, e := s.tools.ListGroupPods(ctx, getInt("group_id"))
		if e != nil {
			return "", display, e
		}
		var b strings.Builder
		for _, p := range pods {
			fmt.Fprintf(&b, "- %s: phase=%s ready=%v restarts=%d\n", p.Name, p.Phase, p.Ready, p.RestartCount)
			for _, c := range p.Containers {
				fmt.Fprintf(&b, "  容器 %s: ready=%v started=%v restarts=%d image=%s\n",
					c.Name, c.Ready, c.Started, c.RestartCount, c.Image)
			}
		}
		return b.String(), display, nil

	case ToolGetPodLogs:
		tail := int64(500)
		if v := getInt("tail"); v > 0 {
			tail = v
		}
		logs, e := s.tools.GetPodLogs(ctx, getInt("cluster_id"), args["namespace"], args["pod"], args["container"], tail)
		if e != nil {
			return "", display, e
		}
		return truncate(logs, 8000), display, nil

	case ToolListPodEvents:
		events, e := s.tools.ListPodEvents(ctx, getInt("cluster_id"), args["namespace"], args["pod"])
		if e != nil {
			return "", display, e
		}
		var b strings.Builder
		for _, ev := range events {
			fmt.Fprintf(&b, "- [%s] %s (%dx) %s\n", ev.Type, ev.Reason, ev.Count, ev.Message)
		}
		return b.String(), display, nil

	case ToolDescribePod:
		desc, e := s.tools.DescribePod(ctx, getInt("cluster_id"), args["namespace"], args["pod"])
		if e != nil {
			return "", display, e
		}
		return truncate(desc, 8000), display, nil

	case ToolSearchFAQ:
		hit, refs := searchFAQ("general", args["query"])
		if hit == "" {
			return "未命中知识库", display, nil
		}
		var b strings.Builder
		b.WriteString(hit)
		for _, r := range refs {
			fmt.Fprintf(&b, "\n引用: %s\n摘录: %s", r.Title, r.Snippet)
		}
		return b.String(), display, nil
	}

	return "", display, fmt.Errorf("unknown tool: %s", name)
}

func formatArgs(args map[string]string) string {
	if len(args) == 0 {
		return ""
	}
	parts := make([]string, 0, len(args))
	for k, v := range args {
		parts = append(parts, fmt.Sprintf("%s=%s", k, v))
	}
	return strings.Join(parts, ", ")
}
