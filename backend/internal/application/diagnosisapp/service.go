// Package diagnosisapp 提供 AI 诊断：收集 K8s 资源上下文 → 调用可配置 LLM → 返回根因分析与修复建议。
package diagnosisapp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/vortexops/vortexops/internal/application/clusterapp"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// ResourceType 诊断目标资源类型。
type ResourceType string

const (
	ResourceTypePod        ResourceType = "pod"
	ResourceTypeDeployment ResourceType = "deployment"
	ResourceTypeNode       ResourceType = "node"
)

// AnalyzeInput 诊断请求。
type AnalyzeInput struct {
	ResourceType ResourceType
	ClusterID    int64
	Namespace    string
	Name         string
	Container    string
	UserID       int64
	UserName     string
}

// AnalysisResult 诊断结果。
type AnalysisResult struct {
	ResourceType string `json:"resource_type"`
	ClusterID    int64  `json:"cluster_id"`
	Namespace    string `json:"namespace"`
	Name         string `json:"name"`
	Summary      string `json:"summary"`      // 根因摘要
	Suggestions  string `json:"suggestions"`  // 修复建议（markdown）
	RawContext   string `json:"raw_context"`  // 收集到的上下文（供前端展示）
	Model        string `json:"model"`
	Provider     string `json:"provider"`
	LatencyMs    int64  `json:"latency_ms"`
}

// SettingProvider 系统设置读取器（由 systemapp.Service 实现）。
type SettingProvider interface {
	GetStringSetting(ctx context.Context, key, fallback string) (string, error)
}

// LogSource 上下文日志来源类型。
type LogSource string

const (
	LogSourceBuild     LogSource = "build"     // 镜像构建失败
	LogSourcePodStartup LogSource = "pod_startup" // 应用启动失败（Pod 未就绪/崩溃）
	LogSourcePodCrash  LogSource = "pod_crash"  // Pod 崩溃/重启
)

// LogAnalyzeInput 基于日志的诊断输入。
// 前端在「构建详情失败」或「Pod 启动失败」时收集日志 + 元信息调用此接口。
type LogAnalyzeInput struct {
	Source      LogSource `json:"source"`
	Title       string    `json:"title"`        // 人类可读的标题，例如「构建 #123 失败」「Pod api-xxx 启动失败」
	ClusterID   int64     `json:"cluster_id"`
	Namespace   string    `json:"namespace"`
	Name        string    `json:"name"`         // 构建号/Pod 名/Deployment 名
	Container   string    `json:"container"`
	BuildID     int64     `json:"build_id"`
	ErrorReason string    `json:"error_reason"` // 已知错误信息（build.error_message / event.reason）
	Logs        string    `json:"logs"`         // 收集到的日志（已截断）
	UserID      int64     `json:"-"`
	UserName    string    `json:"-"`
}

// ChatRole 对话角色。
type ChatRole string

const (
	ChatRoleSystem    ChatRole = "system"
	ChatRoleUser      ChatRole = "user"
	ChatRoleAssistant ChatRole = "assistant"
)

// ChatMessage 一条对话消息。
type ChatMessage struct {
	Role    ChatRole `json:"role"`
	Content string   `json:"content"`
}

// ChatInput 对话输入。
type ChatInput struct {
	Messages []ChatMessage `json:"messages"`
	// Scene 已废弃：改为后端 LLM 意图识别自动判断。
	// 保留字段以兼容旧客户端，后端忽略。
	Scene    string `json:"scene,omitempty"`
	// SessionID 可选：若提供则持久化消息并使用会话上下文。
	SessionID int64 `json:"session_id,omitempty"`
	UserID    int64 `json:"-"`
	UserName  string `json:"-"`
}

// ChatResult 对话结果。
type ChatResult struct {
	Answer     string `json:"answer"`
	Model      string `json:"model"`
	Provider   string `json:"provider"`
	LatencyMs  int64  `json:"latency_ms"`
	Tools      []ToolCall `json:"tools,omitempty"`     // 本轮触发的工具调用（展示用）
	References []Reference `json:"references,omitempty"` // RAG 命中的知识库片段
	Intent     *Intent `json:"intent,omitempty"`       // 意图识别结果（展示用，让用户看到 AI 如何理解问题）
	// SessionID 持久化会话 ID（前端可保存用于历史回看）。
	SessionID int64 `json:"session_id,omitempty"`
	// ProfileSummary 本轮使用的用户画像摘要（前端可展示个性化标签）。
	ProfileSummary string `json:"profile_summary,omitempty"`
}

// ToolCall 工具调用展示（前端渲染为可点击的卡片）。
type ToolCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Result    string `json:"result"`
}

// Reference RAG 命中的知识库引用。
type Reference struct {
	Title   string `json:"title"`
	URL     string `json:"url,omitempty"`
	Snippet string `json:"snippet"`
}

// Intent 意图识别结果。
type Intent struct {
	Category  string         `json:"category"`   // build_failure / pod_failure / release_issue / k8s_ops / general_question
	Reasoning string         `json:"reasoning"`  // AI 的推理过程（展示给用户）
	Tools     []IntentTool   `json:"tools"`      // 推荐调用的工具列表
}

// IntentTool 意图识别推荐的工具调用。
type IntentTool struct {
	Name string            `json:"name"`
	Args map[string]string `json:"args"`
}

// Service AI 诊断应用服务。
type Service struct {
	clusters *clusterapp.Service
	settings SettingProvider
	tools    ToolProvider
	// 可选依赖：知识库 RAG / 用户画像 / 对话会话。
	// 未注入时退化为原有行为（仅意图识别 + 工具调用）。
	kb     KBSearcher
	prof   Profiler
	sess   SessionManager
}

// KBSearcher 知识库检索接口（由 kbapp.Service 实现）。
type KBSearcher interface {
	Search(ctx context.Context, query string, topK int, categoryCode string) ([]KBHit, error)
}

// KBHit 知识库命中条目。
type KBHit struct {
	DocumentTitle string
	CategoryCode  string
	Content       string
	Score         float64
}

// Profiler 用户画像接口（由 userprofileapp.Service 实现）。
type Profiler interface {
	GetByUserID(ctx context.Context, userID int64) (*UserProfile, error)
	LearnFromConversation(ctx context.Context, userID int64, dialog string) (*UserProfile, error)
}

// UserProfile 用户画像（与 userprofileapp.Profile 字段对齐，避免循环依赖）。
type UserProfile struct {
	ExpertiseLevel     string
	Roles              []string
	Domains            []string
	CommunicationStyle string
	Summary            string
}

// SessionManager 对话会话接口（由 chatapp.Service 实现）。
type SessionManager interface {
	AppendMessage(ctx context.Context, in SessionAppendInput) error
	UpdateSessionMeta(ctx context.Context, sessionID int64, scene string, entities map[string]any, lastIntent string, actorID int64) error
	SummarizeIfNeeded(ctx context.Context, sessionID int64, threshold int) error
	BuildContext(ctx context.Context, sessionID int64, recentN int) (string, error)
}

// SessionAppendInput 追加消息输入（与 chatapp.AppendMessageInput 对齐）。
type SessionAppendInput struct {
	SessionID  int64
	UserID     int64
	Role       string
	Content    string
	Intent     any
	Tools      any
	References any
	LatencyMs  int
	ActorID    int64
}

// New 创建诊断服务。
func New(clusters *clusterapp.Service, settings SettingProvider) *Service {
	return &Service{clusters: clusters, settings: settings, tools: noOpToolProvider{}}
}

// WithTools 注入工具提供者（启用意图识别 + 工具调用）。
func (s *Service) WithTools(tp ToolProvider) *Service {
	if tp != nil {
		s.tools = tp
	}
	return s
}

// WithKB 注入知识库检索（启用 RAG）。
func (s *Service) WithKB(kb KBSearcher) *Service {
	s.kb = kb
	return s
}

// WithProfiler 注入用户画像（启用个性化）。
func (s *Service) WithProfiler(p Profiler) *Service {
	s.prof = p
	return s
}

// WithSessionManager 注入对话会话（启用持久化 + 上下文摘要）。
func (s *Service) WithSessionManager(sm SessionManager) *Service {
	s.sess = sm
	return s
}

// Analyze 收集上下文并调用 LLM 诊断。
func (s *Service) Analyze(ctx context.Context, in AnalyzeInput) (*AnalysisResult, error) {
	if in.ClusterID == 0 || in.Name == "" {
		return nil, apperr.Validation("cluster_id and name are required", nil)
	}
	if in.ResourceType == "" {
		in.ResourceType = ResourceTypePod
	}

	clientset, err := s.clusters.GetClient(ctx, in.ClusterID)
	if err != nil {
		return nil, apperr.Internal("get cluster client", err)
	}

	// 1) 收集上下文。
	ctxt, err := s.collectContext(ctx, clientset, in)
	if err != nil {
		return nil, apperr.Internal("collect context", err)
	}

	// 2) 读取 provider 配置。
	provider, _ := s.settings.GetStringSetting(ctx, "ai.diagnosis.provider", "openai")
	baseURL, _ := s.settings.GetStringSetting(ctx, "ai.diagnosis.url", "")
	apiKey, _ := s.settings.GetStringSetting(ctx, "ai.diagnosis.api_key", "")
	model, _ := s.settings.GetStringSetting(ctx, "ai.diagnosis.model", "gpt-4o-mini")
	if baseURL == "" || apiKey == "" {
		// 未配置 LLM：返回上下文 + 提示，不阻断。
		return &AnalysisResult{
			ResourceType: string(in.ResourceType), ClusterID: in.ClusterID,
			Namespace: in.Namespace, Name: in.Name,
			Summary: "AI 诊断未配置。请在「系统设置 → AI 诊断」中配置 provider、URL、API Key 与模型后重试。",
			Suggestions: "",
			RawContext: ctxt, Model: model, Provider: provider,
		}, nil
	}

	// 3) 构造 prompt 并调用 LLM。
	prompt := buildPrompt(in, ctxt)
	start := time.Now()
	llm, err := newLLMClient(provider, baseURL, apiKey, model)
	if err != nil {
		return nil, apperr.Internal("init llm client", err)
	}
	answer, err := llm.Chat(ctx, prompt)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return nil, apperr.Internal("llm chat", err)
	}

	summary, suggestions := splitAnswer(answer)
	return &AnalysisResult{
		ResourceType: string(in.ResourceType), ClusterID: in.ClusterID,
		Namespace: in.Namespace, Name: in.Name,
		Summary: summary, Suggestions: suggestions, RawContext: ctxt,
		Model: model, Provider: provider, LatencyMs: latency,
	}, nil
}

// collectContext 收集 K8s 资源上下文（events + logs + describe 摘要）。
func (s *Service) collectContext(ctx context.Context, clientset kubernetes.Interface, in AnalyzeInput) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "## 资源: %s %s/%s (cluster=%d)\n\n", in.ResourceType, in.Namespace, in.Name, in.ClusterID)

	switch in.ResourceType {
	case ResourceTypePod:
		pod, err := clientset.CoreV1().Pods(in.Namespace).Get(ctx, in.Name, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("get pod: %w", err)
		}
		describePod(&b, pod)
		// 事件（按 involvedObject.name 过滤）。
		events, _ := clientset.CoreV1().Events(in.Namespace).List(ctx, metav1.ListOptions{
			FieldSelector: fmt.Sprintf("involvedObject.name=%s,involvedObject.kind=Pod", in.Name),
			Limit:         50,
		})
		writeEvents(&b, events.Items)
		// 容器日志尾部。
		container := in.Container
		if container == "" && len(pod.Spec.Containers) > 0 {
			container = pod.Spec.Containers[0].Name
		}
		if container != "" {
			logs := fetchPodLogs(ctx, clientset, in.Namespace, in.Name, container, 200)
			fmt.Fprintf(&b, "\n### 容器 %s 日志尾部\n```\n%s\n```\n", container, logs)
		}

	case ResourceTypeDeployment:
		dep, err := clientset.AppsV1().Deployments(in.Namespace).Get(ctx, in.Name, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("get deployment: %w", err)
		}
		fmt.Fprintf(&b, "副本: desired=%d ready=%d updated=%d available=%d\n",
			dep.Status.Replicas, dep.Status.ReadyReplicas, dep.Status.UpdatedReplicas, dep.Status.AvailableReplicas)
		if dep.Status.Conditions != nil {
			for _, c := range dep.Status.Conditions {
				fmt.Fprintf(&b, "- 条件 %s: %s (%s)\n", c.Type, c.Status, c.Message)
			}
		}
		events, _ := clientset.CoreV1().Events(in.Namespace).List(ctx, metav1.ListOptions{
			FieldSelector: fmt.Sprintf("involvedObject.name=%s,involvedObject.kind=Deployment", in.Name),
			Limit:         50,
		})
		writeEvents(&b, events.Items)

	case ResourceTypeNode:
		node, err := clientset.CoreV1().Nodes().Get(ctx, in.Name, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("get node: %w", err)
		}
		fmt.Fprintf(&b, "节点 %s 状态:\n", node.Name)
		for _, cond := range node.Status.Conditions {
			if cond.Status != corev1.ConditionTrue {
				fmt.Fprintf(&b, "- %s: %s (%s)\n", cond.Type, cond.Status, cond.Message)
			}
		}
		events, _ := clientset.CoreV1().Events("").List(ctx, metav1.ListOptions{
			FieldSelector: fmt.Sprintf("involvedObject.name=%s,involvedObject.kind=Node", in.Name),
			Limit:         50,
		})
		writeEvents(&b, events.Items)
	}

	return b.String(), nil
}

func describePod(b *strings.Builder, pod *corev1.Pod) {
	fmt.Fprintf(b, "阶段: %s\n", pod.Status.Phase)
	fmt.Fprintf(b, "节点: %s  IP: %s\n", pod.Spec.NodeName, pod.Status.PodIP)
	if pod.DeletionTimestamp != nil {
		fmt.Fprintf(b, "正在终止 (deletionTimestamp=%s)\n", pod.DeletionTimestamp)
	}
	for _, cs := range pod.Status.ContainerStatuses {
		fmt.Fprintf(b, "- 容器 %s: ready=%v restarts=%d state=%s\n",
			cs.Name, cs.Ready, cs.RestartCount, containerStateString(cs.State))
	}
}

func containerStateString(s corev1.ContainerState) string {
	switch {
	case s.Waiting != nil:
		return fmt.Sprintf("waiting(%s: %s)", s.Waiting.Reason, s.Waiting.Message)
	case s.Terminated != nil:
		return fmt.Sprintf("terminated(%s exit=%d reason=%s)", s.Terminated.Reason, s.Terminated.ExitCode, s.Terminated.Message)
	case s.Running != nil:
		return "running"
	}
	return "unknown"
}

func writeEvents(b *strings.Builder, events []corev1.Event) {
	if len(events) == 0 {
		fmt.Fprintln(b, "\n### 事件: 无")
		return
	}
	fmt.Fprintln(b, "\n### 最近事件")
	for _, e := range events {
		fmt.Fprintf(b, "- [%s] %s (%dx) %s\n", e.LastTimestamp.Format("01-02 15:04:05"), e.Reason, e.Count, e.Message)
	}
}

func fetchPodLogs(ctx context.Context, clientset kubernetes.Interface, ns, pod, container string, tail int64) string {
	req := clientset.CoreV1().Pods(ns).GetLogs(pod, &corev1.PodLogOptions{
		Container: container,
		TailLines: &tail,
	})
	stream, err := req.Stream(ctx)
	if err != nil {
		return fmt.Sprintf("(读取日志失败: %v)", err)
	}
	defer stream.Close()
	buf := make([]byte, 0, 8192)
	tmp := make([]byte, 4096)
	for {
		n, err := stream.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			break
		}
		if len(buf) > 32*1024 {
			break
		}
	}
	return string(buf)
}

func buildPrompt(in AnalyzeInput, context string) string {
	return fmt.Sprintf(`你是 Kubernetes 运维专家。请根据以下资源上下文进行根因分析并给出修复建议。

资源类型: %s
命名空间: %s
名称: %s

%s

请用中文回答，格式如下：
## 根因分析
<简明根因摘要，2-4 句>

## 修复建议
<可执行的修复步骤，用 markdown 列表>
`, in.ResourceType, in.Namespace, in.Name, context)
}

// splitAnswer 将 LLM 回答拆为摘要与建议两段。
func splitAnswer(answer string) (summary, suggestions string) {
	lower := strings.ToLower(answer)
	if idx := strings.Index(lower, "## 修复建议"); idx > 0 {
		return strings.TrimSpace(answer[:idx]), strings.TrimSpace(answer[idx:])
	}
	if idx := strings.Index(lower, "## 根因分析"); idx >= 0 {
		return strings.TrimSpace(answer[idx:]), ""
	}
	return strings.TrimSpace(answer), ""
}

var errUnsupportedProvider = errors.New("unsupported ai diagnosis provider")

// AnalyzeLogs 基于日志的诊断：前端收集构建日志或 Pod 启动日志后调用。
// 与 Analyze 不同：不需要 cluster client（前端已收集好日志），专注根因 + 修复建议。
func (s *Service) AnalyzeLogs(ctx context.Context, in LogAnalyzeInput) (*AnalysisResult, error) {
	if in.Logs == "" && in.ErrorReason == "" {
		return nil, apperr.Validation("logs or error_reason is required", nil)
	}

	provider, baseURL, apiKey, model, err := s.llmConfig(ctx)
	if err != nil {
		return nil, err
	}
	if baseURL == "" || apiKey == "" {
		return &AnalysisResult{
			ResourceType: string(in.Source), ClusterID: in.ClusterID,
			Namespace: in.Namespace, Name: in.Name,
			Summary: "AI 诊断未配置。请在「系统设置 → AI 诊断」中配置 provider、URL、API Key 与模型后重试。",
			Suggestions: "", RawContext: truncateLogs(in.Logs, 4096),
			Model: model, Provider: provider,
		}, nil
	}

	prompt := buildLogPrompt(in)
	start := time.Now()
	llm, err := newLLMClient(provider, baseURL, apiKey, model)
	if err != nil {
		return nil, apperr.Internal("init llm client", err)
	}
	answer, err := llm.Chat(ctx, prompt)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return nil, apperr.Internal("llm chat", err)
	}

	summary, suggestions := splitAnswer(answer)
	rawCtx := buildLogContext(in)
	return &AnalysisResult{
		ResourceType: string(in.Source), ClusterID: in.ClusterID,
		Namespace: in.Namespace, Name: in.Name,
		Summary: summary, Suggestions: suggestions, RawContext: rawCtx,
		Model: model, Provider: provider, LatencyMs: latency,
	}, nil
}

// AnalyzeLogsStream 流式版本的日志诊断。
// onDelta 收到增量文本时回调（前端逐字渲染），返回完整结果（含 latency 与分段后的 summary/suggestions）。
// 实现复用 ChatMultiTurnStream：单轮 prompt 包成一条 user 消息。
func (s *Service) AnalyzeLogsStream(ctx context.Context, in LogAnalyzeInput, onDelta func(delta string)) (*AnalysisResult, error) {
	if in.Logs == "" && in.ErrorReason == "" {
		return nil, apperr.Validation("logs or error_reason is required", nil)
	}

	provider, baseURL, apiKey, model, err := s.llmConfig(ctx)
	if err != nil {
		return nil, err
	}
	if baseURL == "" || apiKey == "" {
		return &AnalysisResult{
			ResourceType: string(in.Source), ClusterID: in.ClusterID,
			Namespace: in.Namespace, Name: in.Name,
			Summary: "AI 诊断未配置。请在「系统设置 → AI 诊断」中配置 provider、URL、API Key 与模型后重试。",
			Suggestions: "", RawContext: truncateLogs(in.Logs, 4096),
			Model: model, Provider: provider,
		}, nil
	}

	prompt := buildLogPrompt(in)
	start := time.Now()
	llm, err := newLLMClient(provider, baseURL, apiKey, model)
	if err != nil {
		return nil, apperr.Internal("init llm client", err)
	}
	// 单轮对话包成一条 user 消息复用 ChatMultiTurnStream。
	answer, err := llm.ChatMultiTurnStream(ctx, []ChatMessage{{Role: ChatRoleUser, Content: prompt}}, onDelta)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		// 流式过程中出错：若已收到部分文本，仍返回（便于前端展示已生成内容），附加错误提示。
		if answer != "" {
			summary, suggestions := splitAnswer(answer + "\n\n> ⚠️ 流式过程出错：" + err.Error())
			rawCtx := buildLogContext(in)
			return &AnalysisResult{
				ResourceType: string(in.Source), ClusterID: in.ClusterID,
				Namespace: in.Namespace, Name: in.Name,
				Summary: summary, Suggestions: suggestions, RawContext: rawCtx,
				Model: model, Provider: provider, LatencyMs: latency,
			}, nil
		}
		return nil, apperr.Internal("llm stream chat", err)
	}

	summary, suggestions := splitAnswer(answer)
	rawCtx := buildLogContext(in)
	return &AnalysisResult{
		ResourceType: string(in.Source), ClusterID: in.ClusterID,
		Namespace: in.Namespace, Name: in.Name,
		Summary: summary, Suggestions: suggestions, RawContext: rawCtx,
		Model: model, Provider: provider, LatencyMs: latency,
	}, nil
}

// Chat 多轮对话式 AI 助手。
//
// 完整流程（意图识别 → RAG 检索 → 工具调用 → 个性化综合回答 → 画像学习）：
//  1. 加载用户画像（若已注入 Profiler），用于个性化 system prompt。
//  2. 加载会话上下文（若已注入 SessionManager 且提供 session_id）：摘要 + 实体记忆 + 近期消息。
//  3. 用一次轻量 LLM 调用做意图识别，从用户最近一条消息中提取：
//     - intent: build_failure / pod_failure / release_issue / k8s_ops / general_question
//     - entities: build_id / app_id / group_id / pod_name / namespace / cluster_id / 等
//     - tools: 推荐调用的工具列表
//  4. RAG 检索：若已注入 KB，按查询向量召回知识库片段（用于增强回答 + 引用展示）。
//  5. 按推荐工具列表依次调用工具（ToolProvider），收集实时平台上下文。
//  6. 命中 FAQ 时直接返回（跳过 LLM）。
//  7. 把画像 + 会话上下文 + 工具结果 + RAG 片段 + 对话历史作为上下文，调用 LLM 生成最终回答。
//  8. 持久化 user/assistant 消息到会话；异步触发画像学习与会话摘要。
//
// 工具调用、RAG 命中、意图识别过程对用户可见（返回 ToolCall/Reference/Intent），便于审计与调试。
func (s *Service) Chat(ctx context.Context, in ChatInput) (*ChatResult, error) {
	if len(in.Messages) == 0 {
		return nil, apperr.Validation("messages is required", nil)
	}

	provider, baseURL, apiKey, model, err := s.llmConfig(ctx)
	if err != nil {
		return nil, err
	}
	if baseURL == "" || apiKey == "" {
		return &ChatResult{
			Answer:   "AI 助手未配置。请联系管理员在「系统设置 → AI 诊断」中配置 LLM provider、URL、API Key 与模型。",
			Model:    model, Provider: provider,
		}, nil
	}

	// 取最后一条用户消息作为当前查询。
	var query string
	for i := len(in.Messages) - 1; i >= 0; i-- {
		if in.Messages[i].Role == ChatRoleUser {
			query = in.Messages[i].Content
			break
		}
	}

	llm, err := newLLMClient(provider, baseURL, apiKey, model)
	if err != nil {
		return nil, apperr.Internal("init llm client", err)
	}

	// 0) 加载用户画像与会话上下文（可选）。
	var profileSummary string
	if s.prof != nil && in.UserID != 0 {
		if p, err := s.prof.GetByUserID(ctx, in.UserID); err == nil && p != nil {
			profileSummary = profileToPrompt(p)
		}
	}
	var sessionCtx string
	if s.sess != nil && in.SessionID != 0 {
		if ctxStr, err := s.sess.BuildContext(ctx, in.SessionID, 8); err == nil {
			sessionCtx = ctxStr
		}
	}

	// 持久化用户消息（若启用会话）。
	if s.sess != nil && in.SessionID != 0 {
		_ = s.sess.AppendMessage(ctx, SessionAppendInput{
			SessionID: in.SessionID, UserID: in.UserID, Role: "user",
			Content: query, ActorID: in.UserID,
		})
	}

	start := time.Now()

	// 1) 意图识别（轻量 LLM 调用）。
	intent, err := s.recognizeIntent(ctx, llm, query)
	if err != nil {
		// 意图识别失败不阻断，降级为通用问答。
		intent = &Intent{Category: "general_question", Tools: nil, Reasoning: fmt.Sprintf("(识别失败: %v)", err)}
	}

	// 2) FAQ 优先：命中知识库直接返回（不调用 LLM）。
	if hit, refs := searchFAQ(intent.Category, query); hit != "" {
		result := &ChatResult{
			Answer:        hit,
			Model:         model, Provider: provider, LatencyMs: time.Since(start).Milliseconds(),
			References:     refs,
			Intent:         intent,
			SessionID:      in.SessionID,
			ProfileSummary: profileSummary,
		}
		// 持久化 assistant 消息。
		if s.sess != nil && in.SessionID != 0 {
			_ = s.sess.AppendMessage(ctx, SessionAppendInput{
				SessionID: in.SessionID, UserID: in.UserID, Role: "assistant",
				Content: hit, Intent: intent, References: refs, LatencyMs: int(result.LatencyMs),
				ActorID: in.UserID,
			})
		}
		// 更新会话元信息。
		if s.sess != nil && in.SessionID != 0 {
			_ = s.sess.UpdateSessionMeta(ctx, in.SessionID, intentToScene(intent.Category), entitiesFromIntent(intent), intent.Category, in.UserID)
		}
		return result, nil
	}

	// 3) RAG 检索：从知识库召回相关片段。
	var ragContext strings.Builder
	var references []Reference
	if s.kb != nil && query != "" {
		categoryCode := intentToKBCategory(intent.Category)
		hits, err := s.kb.Search(ctx, query, 3, categoryCode)
		if err == nil {
			for _, h := range hits {
				ragContext.WriteString("\n### 知识库参考：" + h.DocumentTitle + "\n")
				ragContext.WriteString(h.Content)
				ragContext.WriteString("\n")
				references = append(references, Reference{
					Title:   h.DocumentTitle,
					Snippet: truncate(h.Content, 200),
				})
			}
		}
	}

	// 4) 工具调用：按意图推荐的工具列表依次执行，收集上下文。
	toolCalls := make([]ToolCall, 0, len(intent.Tools))
	var toolCtx strings.Builder
	for _, call := range intent.Tools {
		// 安全护栏：工具白名单。
		if !isAllowedTool(call.Name) {
			continue
		}
		result, display, e := s.toolCall(ctx, call.Name, call.Args)
		if e != nil {
			display.Result = fmt.Sprintf("(工具调用失败: %v)", e)
			toolCalls = append(toolCalls, display)
			continue
		}
		toolCalls = append(toolCalls, display)
		fmt.Fprintf(&toolCtx, "\n### 工具 %s(%s)\n%s\n", call.Name, formatArgs(call.Args), result)
	}

	// 5) 综合回答：画像 + 会话上下文 + RAG + 工具结果 + 对话历史 → LLM。
	chatMsgs := s.buildChatMessages(in.Messages, intent, toolCtx.String(), ragContext.String(), sessionCtx, profileSummary)
	answer, err := llm.ChatMultiTurn(ctx, chatMsgs)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return nil, apperr.Internal("llm chat", err)
	}

	result := &ChatResult{
		Answer:    answer,
		Model:     model, Provider: provider, LatencyMs: latency,
		Tools:          toolCalls,
		References:     references,
		Intent:         intent,
		SessionID:      in.SessionID,
		ProfileSummary: profileSummary,
	}

	// 6) 持久化 assistant 消息 + 更新会话元信息。
	if s.sess != nil && in.SessionID != 0 {
		_ = s.sess.AppendMessage(ctx, SessionAppendInput{
			SessionID: in.SessionID, UserID: in.UserID, Role: "assistant",
			Content: answer, Intent: intent, Tools: toolCalls, References: references,
			LatencyMs: int(latency), ActorID: in.UserID,
		})
		_ = s.sess.UpdateSessionMeta(ctx, in.SessionID, intentToScene(intent.Category), entitiesFromIntent(intent), intent.Category, in.UserID)
		// 长会话摘要（异步触发，阈值=20 条）。
		go func(sessionID int64) {
			_ = s.sess.SummarizeIfNeeded(context.Background(), sessionID, 20)
		}(in.SessionID)
	}

	// 7) 异步画像学习：用本轮对话推断用户特征。
	if s.prof != nil && in.UserID != 0 {
		go func(uid int64, q, a string) {
			dialog := fmt.Sprintf("user: %s\nassistant: %s", q, a)
			_, _ = s.prof.LearnFromConversation(context.Background(), uid, dialog)
		}(in.UserID, query, answer)
	}

	return result, nil
}

// ChatStream 流式版本的多轮对话。
// onMeta 在最终答案开始流式输出前被调用一次，传入意图/工具/引用/会话/画像等元信息。
// onDelta 在收到增量文本时被调用（可能多次）。
// 返回完整答案与元信息（用于 handler 写入结束事件）。
func (s *Service) ChatStream(ctx context.Context, in ChatInput, onMeta func(meta ChatResult), onDelta func(delta string)) (*ChatResult, error) {
	if len(in.Messages) == 0 {
		return nil, apperr.Validation("messages is required", nil)
	}

	provider, baseURL, apiKey, model, err := s.llmConfig(ctx)
	if err != nil {
		return nil, err
	}
	if baseURL == "" || apiKey == "" {
		meta := &ChatResult{
			Answer:   "AI 助手未配置。请联系管理员在「系统设置 → AI 诊断」中配置 LLM provider、URL、API Key 与模型。",
			Model:    model, Provider: provider,
		}
		if onMeta != nil {
			onMeta(*meta)
		}
		if onDelta != nil {
			onDelta(meta.Answer)
		}
		return meta, nil
	}

	var query string
	for i := len(in.Messages) - 1; i >= 0; i-- {
		if in.Messages[i].Role == ChatRoleUser {
			query = in.Messages[i].Content
			break
		}
	}

	llm, err := newLLMClient(provider, baseURL, apiKey, model)
	if err != nil {
		return nil, apperr.Internal("init llm client", err)
	}

	var profileSummary string
	if s.prof != nil && in.UserID != 0 {
		if p, err := s.prof.GetByUserID(ctx, in.UserID); err == nil && p != nil {
			profileSummary = profileToPrompt(p)
		}
	}
	var sessionCtx string
	if s.sess != nil && in.SessionID != 0 {
		if ctxStr, err := s.sess.BuildContext(ctx, in.SessionID, 8); err == nil {
			sessionCtx = ctxStr
		}
	}

	if s.sess != nil && in.SessionID != 0 {
		_ = s.sess.AppendMessage(ctx, SessionAppendInput{
			SessionID: in.SessionID, UserID: in.UserID, Role: "user",
			Content: query, ActorID: in.UserID,
		})
	}

	start := time.Now()

	intent, err := s.recognizeIntent(ctx, llm, query)
	if err != nil {
		intent = &Intent{Category: "general_question", Tools: nil, Reasoning: fmt.Sprintf("(识别失败: %v)", err)}
	}

	// FAQ 命中：直接作为单个 delta 推送。
	if hit, refs := searchFAQ(intent.Category, query); hit != "" {
		latency := time.Since(start).Milliseconds()
		result := &ChatResult{
			Answer:        hit,
			Model:         model, Provider: provider, LatencyMs: latency,
			References:     refs,
			Intent:         intent,
			SessionID:      in.SessionID,
			ProfileSummary: profileSummary,
		}
		if onMeta != nil {
			onMeta(*result)
		}
		if onDelta != nil {
			onDelta(hit)
		}
		if s.sess != nil && in.SessionID != 0 {
			_ = s.sess.AppendMessage(ctx, SessionAppendInput{
				SessionID: in.SessionID, UserID: in.UserID, Role: "assistant",
				Content: hit, Intent: intent, References: refs, LatencyMs: int(latency),
				ActorID: in.UserID,
			})
			_ = s.sess.UpdateSessionMeta(ctx, in.SessionID, intentToScene(intent.Category), entitiesFromIntent(intent), intent.Category, in.UserID)
		}
		return result, nil
	}

	var ragContext strings.Builder
	var references []Reference
	if s.kb != nil && query != "" {
		categoryCode := intentToKBCategory(intent.Category)
		hits, err := s.kb.Search(ctx, query, 3, categoryCode)
		if err == nil {
			for _, h := range hits {
				ragContext.WriteString("\n### 知识库参考：" + h.DocumentTitle + "\n")
				ragContext.WriteString(h.Content)
				ragContext.WriteString("\n")
				references = append(references, Reference{
					Title:   h.DocumentTitle,
					Snippet: truncate(h.Content, 200),
				})
			}
		}
	}

	toolCalls := make([]ToolCall, 0, len(intent.Tools))
	var toolCtx strings.Builder
	for _, call := range intent.Tools {
		if !isAllowedTool(call.Name) {
			continue
		}
		result, display, e := s.toolCall(ctx, call.Name, call.Args)
		if e != nil {
			display.Result = fmt.Sprintf("(工具调用失败: %v)", e)
			toolCalls = append(toolCalls, display)
			continue
		}
		toolCalls = append(toolCalls, display)
		fmt.Fprintf(&toolCtx, "\n### 工具 %s(%s)\n%s\n", call.Name, formatArgs(call.Args), result)
	}

	chatMsgs := s.buildChatMessages(in.Messages, intent, toolCtx.String(), ragContext.String(), sessionCtx, profileSummary)

	// 推送元信息（让前端先渲染意图/工具/引用）。
	meta := &ChatResult{
		Model:          model,
		Provider:       provider,
		Tools:          toolCalls,
		References:     references,
		Intent:         intent,
		SessionID:      in.SessionID,
		ProfileSummary: profileSummary,
	}
	if onMeta != nil {
		onMeta(*meta)
	}

	// 流式调用 LLM。
	answer, err := llm.ChatMultiTurnStream(ctx, chatMsgs, onDelta)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		// 流式过程中出错：把已收到的部分作为答案返回，附加错误提示。
		if answer == "" {
			return nil, apperr.Internal("llm chat stream", err)
		}
		answer += fmt.Sprintf("\n\n（流式中断: %v）", err)
	}

	meta.Answer = answer
	meta.LatencyMs = latency

	if s.sess != nil && in.SessionID != 0 {
		_ = s.sess.AppendMessage(ctx, SessionAppendInput{
			SessionID: in.SessionID, UserID: in.UserID, Role: "assistant",
			Content: answer, Intent: intent, Tools: toolCalls, References: references,
			LatencyMs: int(latency), ActorID: in.UserID,
		})
		_ = s.sess.UpdateSessionMeta(ctx, in.SessionID, intentToScene(intent.Category), entitiesFromIntent(intent), intent.Category, in.UserID)
		go func(sessionID int64) {
			_ = s.sess.SummarizeIfNeeded(context.Background(), sessionID, 20)
		}(in.SessionID)
	}
	if s.prof != nil && in.UserID != 0 {
		go func(uid int64, q, a string) {
			dialog := fmt.Sprintf("user: %s\nassistant: %s", q, a)
			_, _ = s.prof.LearnFromConversation(context.Background(), uid, dialog)
		}(in.UserID, query, answer)
	}

	return meta, nil
}

// recognizeIntent 用 LLM 做意图识别。
// 返回结构化 Intent（类别、提取的实体、推荐工具）。
// 失败时返回 error，调用方降级为通用问答。
func (s *Service) recognizeIntent(ctx context.Context, llm llmClient, query string) (*Intent, error) {
	prompt := buildIntentPrompt(query)
	raw, err := llm.Chat(ctx, prompt)
	if err != nil {
		return nil, err
	}
	return parseIntent(raw)
}

// buildChatMessages 构造综合回答阶段的多轮消息。
func (s *Service) buildChatMessages(history []ChatMessage, intent *Intent, toolCtx, ragCtx, sessionCtx, profileCtx string) []ChatMessage {
	system := systemPromptForIntent(intent)
	// 注入用户画像。
	if profileCtx != "" {
		system += "\n\n" + profileCtx
	}
	// 注入会话上下文（摘要 + 实体记忆 + 近期对话）。
	if sessionCtx != "" {
		system += "\n\n## 会话上下文\n" + sessionCtx
	}
	// 注入 RAG 检索到的知识库片段。
	if ragCtx != "" {
		system += "\n\n## 知识库参考（可在回答中引用）\n" + ragCtx
	}
	// 仅保留最近 8 轮避免 token 超限。
	msgs := history
	if len(msgs) > 16 {
		msgs = msgs[len(msgs)-16:]
	}
	out := make([]ChatMessage, 0, len(msgs)+2)
	out = append(out, ChatMessage{Role: ChatRoleSystem, Content: system})
	out = append(out, msgs...)
	if toolCtx != "" {
		out = append(out, ChatMessage{Role: ChatRoleUser, Content: "以下是已收集到的诊断上下文（由工具调用获取）：\n" + toolCtx})
	}
	return out
}

func systemPromptForIntent(intent *Intent) string {
	base := "你是 VortexOps 平台的 AI 运维助手，擅长 Kubernetes、CI/CD、镜像构建、发布、监控等领域。" +
		"回答需简明、准确、可执行；不确定时明确说明；引用平台操作路径时使用「」标注。" +
		"对于复杂问题，分步骤回答；涉及危险操作（删除、强制重启）时给出风险提示。"
	switch intent.Category {
	case "build_failure":
		return base + "\n当前问题：镜像构建失败。请基于工具收集到的构建日志、错误信息、步骤状态进行根因分析，给出可执行的修复步骤。"
	case "pod_failure":
		return base + "\n当前问题：Pod 启动失败或崩溃。请基于 Pod describe、日志、事件进行根因分析（CrashLoopBackOff / ImagePullBackOff / OOM / 配置错误等），给出修复建议。"
	case "release_issue":
		return base + "\n当前问题：发布相关问题。请基于上下文分析发布失败原因（健康检查、滚动策略、回滚、PDB 等），给出修复建议。"
	case "k8s_ops":
		return base + "\n当前问题：K8s 运维问题。请基于 Pod/事件/资源状态分析（Pending / 资源不足 / 污点 / 网络 / 配置），给出修复建议。"
	default:
		return base + "\n当前问题：通用问答。请基于已知上下文回答，若需要更详细信息请明确告知用户应提供哪些参数。"
	}
}

// isAllowedTool 工具白名单校验。
func isAllowedTool(name string) bool {
	for _, t := range AvailableTools() {
		if t.Name == name {
			return true
		}
	}
	return false
}

// ListFAQ 返回常见问题列表（用于前端展示快捷问题）。
// 场景参数已废弃（意图识别由后端完成），保留参数以兼容旧客户端。
func (s *Service) ListFAQ(scene string) []FAQItem {
	// 始终返回全部 FAQ；前端按相关度展示。
	return faqForScene("general")
}

// llmConfig 读取 LLM 配置。
func (s *Service) llmConfig(ctx context.Context) (provider, baseURL, apiKey, model string, err error) {
	provider, _ = s.settings.GetStringSetting(ctx, "ai.diagnosis.provider", "openai")
	baseURL, _ = s.settings.GetStringSetting(ctx, "ai.diagnosis.url", "")
	apiKey, _ = s.settings.GetStringSetting(ctx, "ai.diagnosis.api_key", "")
	model, _ = s.settings.GetStringSetting(ctx, "ai.diagnosis.model", "gpt-4o-mini")
	return provider, baseURL, apiKey, model, nil
}

func buildLogPrompt(in LogAnalyzeInput) string {
	sceneDesc := map[LogSource]string{
		LogSourceBuild:      "镜像构建失败",
		LogSourcePodStartup: "应用启动失败（Pod 未就绪）",
		LogSourcePodCrash:   "Pod 崩溃或频繁重启",
	}[in.Source]
	if sceneDesc == "" {
		sceneDesc = string(in.Source)
	}
	logs := truncateLogs(in.Logs, 12000)
	return fmt.Sprintf(`你是 VortexOps 平台的资深 SRE 与构建工程师。请根据以下失败上下文进行根因分析并给出可执行的修复建议。

## 场景
%s
标题: %s
集群ID: %d
命名空间: %s
资源名: %s
容器: %s
构建ID: %d
已知错误: %s

## 日志
%s

请用中文回答，格式如下：
## 根因分析
<2-4 句简明根因摘要，指出具体错误位置>

## 修复建议
<可执行的修复步骤，用 markdown 列表，每条标注影响范围>

## 常见原因
<列举 2-3 个此类失败的常见原因，便于排查>
`, sceneDesc, in.Title, in.ClusterID, in.Namespace, in.Name, in.Container, in.BuildID, in.ErrorReason, logs)
}

func buildLogContext(in LogAnalyzeInput) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## 场景: %s\n", in.Source)
	fmt.Fprintf(&b, "标题: %s\n", in.Title)
	if in.ClusterID != 0 {
		fmt.Fprintf(&b, "集群: %d  命名空间: %s  资源: %s\n", in.ClusterID, in.Namespace, in.Name)
	}
	if in.BuildID != 0 {
		fmt.Fprintf(&b, "构建ID: %d\n", in.BuildID)
	}
	if in.ErrorReason != "" {
		fmt.Fprintf(&b, "\n### 已知错误\n%s\n", in.ErrorReason)
	}
	if in.Logs != "" {
		fmt.Fprintf(&b, "\n### 日志（截断）\n```\n%s\n```\n", truncateLogs(in.Logs, 4096))
	}
	return b.String()
}

func truncateLogs(s string, max int) string {
	if len(s) <= max {
		return s
	}
	// 保留尾部（错误通常在末尾）。
	return "...(已截断前部)...\n" + s[len(s)-max:]
}

// FAQItem 常见问题条目。
type FAQItem struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
	Category string `json:"category"`
}

// faqForScene 返回场景下的 FAQ 列表。
func faqForScene(scene string) []FAQItem {
	all := []FAQItem{
		{Question: "构建失败如何排查？", Answer: "1) 查看构建日志尾部错误信息；2) 检查 Dockerfile/构建命令；3) 确认基础镜像与依赖可访问；4) 检查 Jenkins/Tekton 执行器资源。", Category: "build"},
		{Question: "镜像推送失败怎么办？", Answer: "检查 Harbor/Registry 凭证是否有效、镜像仓库配额、网络连通性，以及 repository 命名是否符合规范。", Category: "build"},
		{Question: "Pod 一直 Pending 怎么排查？", Answer: "kubectl describe pod 查看 Events；常见原因：资源不足、节点污点未容忍、PVC 未绑定、镜像拉取失败。", Category: "k8s"},
		{Question: "Pod CrashLoopBackOff 如何处理？", Answer: "查看 kubectl logs --previous 容器上次退出的日志；常见原因：配置错误、依赖服务不可达、启动命令错误、资源限制过低。", Category: "k8s"},
		{Question: "应用启动失败但 Pod 在运行？", Answer: "检查 readiness/liveness probe 配置、应用启动日志、依赖中间件连通性、配置集是否正确挂载。", Category: "k8s"},
		{Question: "发布回滚如何操作？", Answer: "在「发布中心 → 发布详情」点击「回滚」，选择上一个稳定版本；Helm 发布支持 helm rollback。", Category: "release"},
		{Question: "滚动发布卡住怎么办？", Answer: "检查新 Pod 是否就绪（readiness probe）、maxSurge/maxUnavailable 配置、是否有 PDB 阻止、旧 Pod 终止 grace period。", Category: "release"},
		{Question: "如何查看应用资源使用？", Answer: "「集群运维 → 容器监控」选择集群/命名空间；或 kubectl top pod --namespace=<ns>。", Category: "system"},
		{Question: "如何配置发布审批？", Answer: "「系统管理 → 权限管理」配置发布审批流；环境级别可在应用详情 → 发布策略中开启审批。", Category: "system"},
		{Question: "Git 源拉取失败？", Answer: "检查凭证（SSH key/Token）、仓库地址、分支是否存在；私有仓库需在应用 Git 源配置凭证。", Category: "build"},
		{Question: "如何重启 Pod？", Answer: "分组详情 → Pods 标签 → 选择 Pod → 重启；或 kubectl delete pod <name> -n <ns>，控制器会自动重建。", Category: "k8s"},
		{Question: "配置中心如何使用？", Answer: "「配置管理」创建配置集 → 绑定到分组 → 配置集内容（env/files/command）会注入到 Pod。支持版本回滚。", Category: "system"},
	}
	if scene == "general" {
		return all
	}
	filtered := make([]FAQItem, 0, len(all))
	for _, f := range all {
		if f.Category == scene {
			filtered = append(filtered, f)
		}
	}
	if len(filtered) == 0 {
		return all
	}
	return filtered
}

// searchFAQ 在 FAQ 中检索匹配的问题。
// 命中条件：用户消息包含 FAQ 问题的关键词（简单子串匹配，避免引入额外依赖）。
func searchFAQ(scene, query string) (string, []Reference) {
	if query == "" {
		return "", nil
	}
	lower := strings.ToLower(query)
	items := faqForScene(scene)
	// 优先精确匹配问题。
	for _, item := range items {
		if strings.Contains(lower, strings.ToLower(item.Question)) ||
			strings.Contains(strings.ToLower(item.Question), lower) {
			refs := []Reference{{
				Title: item.Question, Snippet: truncate(item.Answer, 200),
			}}
			return item.Answer, refs
		}
	}
	// 关键词匹配。
	for _, item := range items {
		// 取问题前 6 个字作为关键词集合。
		keywords := strings.Fields(strings.ReplaceAll(item.Question, "？", " "))
		matched := 0
		for _, kw := range keywords {
			if len(kw) >= 2 && strings.Contains(lower, strings.ToLower(kw)) {
				matched++
			}
		}
		if matched >= 2 {
			refs := []Reference{{
				Title: item.Question, Snippet: truncate(item.Answer, 200),
			}}
			return item.Answer, refs
		}
	}
	return "", nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// profileToPrompt 将用户画像转换为 system prompt 片段。
func profileToPrompt(p *UserProfile) string {
	if p == nil {
		return ""
	}
	var parts []string
	levelMap := map[string]string{
		"beginner": "初学者", "intermediate": "中级", "advanced": "高级", "expert": "资深", "unknown": "",
	}
	if zh, ok := levelMap[p.ExpertiseLevel]; ok && zh != "" {
		parts = append(parts, "专业水平："+zh)
	}
	if len(p.Roles) > 0 {
		parts = append(parts, "角色："+strings.Join(p.Roles, "/"))
	}
	if len(p.Domains) > 0 {
		parts = append(parts, "擅长领域："+strings.Join(p.Domains, "/"))
	}
	if p.CommunicationStyle == "concise" {
		parts = append(parts, "回答简洁")
	} else if p.CommunicationStyle == "detailed" {
		parts = append(parts, "回答详细")
	}
	if p.Summary != "" {
		parts = append(parts, "画像摘要："+p.Summary)
	}
	if len(parts) == 0 {
		return ""
	}
	return "用户画像：" + strings.Join(parts, "；") + "。请根据用户画像调整回答深度与术语。"
}

// intentToScene 将意图类别映射为会话场景标签。
func intentToScene(category string) string {
	switch category {
	case "build_failure":
		return "build"
	case "pod_failure":
		return "k8s"
	case "release_issue":
		return "release"
	case "k8s_ops":
		return "k8s"
	default:
		return "general"
	}
}

// intentToKBCategory 将意图类别映射为知识库分类 code（用于 RAG 检索时按分类过滤）。
func intentToKBCategory(category string) string {
	switch category {
	case "build_failure":
		return "build"
	case "pod_failure", "k8s_ops":
		return "k8s"
	case "release_issue":
		return "release"
	default:
		return ""
	}
}

// entitiesFromIntent 从意图识别结果中提取实体记忆。
// 将工具调用参数中的 build_id/app_id/group_id/pod/namespace/cluster_id 等收集到 map。
func entitiesFromIntent(intent *Intent) map[string]any {
	if intent == nil {
		return nil
	}
	entities := map[string]any{}
	entityKeys := []string{"build_id", "app_id", "group_id", "pod", "pod_name", "namespace", "cluster_id", "name"}
	for _, t := range intent.Tools {
		for k, v := range t.Args {
			for _, ek := range entityKeys {
				if k == ek && v != "" {
					entities[k] = v
					break
				}
			}
		}
	}
	if len(entities) == 0 {
		return nil
	}
	return entities
}
