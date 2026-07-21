// Package diagnosishttp 是 AI 诊断的 HTTP handlers。
package diagnosishttp

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/vortexops/vortexops/internal/application/diagnosisapp"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpauth"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpx"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// Handler AI 诊断 handler。
type Handler struct {
	svc *diagnosisapp.Service
}

// NewHandler 创建诊断 handler。
func NewHandler(svc *diagnosisapp.Service) *Handler {
	return &Handler{svc: svc}
}

// Analyze POST /api/v1/diagnosis/analyze
// Body: { resource_type, cluster_id, namespace, name, container }
func (h *Handler) Analyze(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ResourceType string `json:"resource_type"`
		ClusterID    int64  `json:"cluster_id"`
		Namespace    string `json:"namespace"`
		Name         string `json:"name"`
		Container    string `json:"container"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid body", err))
		return
	}
	if req.ClusterID == 0 || req.Name == "" {
		httpx.WriteError(w, apperr.Validation("cluster_id and name are required", nil))
		return
	}
	uid := httpauth.UserID(r.Context())
	// 简单限流：同一用户 60s 内最多一次诊断（按 request_id 去重可后续扩展）。
	_ = uid
	result, err := h.svc.Analyze(r.Context(), diagnosisapp.AnalyzeInput{
		ResourceType: diagnosisapp.ResourceType(req.ResourceType),
		ClusterID:    req.ClusterID, Namespace: req.Namespace,
		Name: req.Name, Container: req.Container,
		UserID: uid, UserName: httpauth.Username(r.Context()),
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, result)
}

// AnalyzeLogs POST /api/v1/diagnosis/analyze-logs
// 基于日志的诊断：前端在构建失败/Pod 启动失败时收集日志后调用。
// Body: { source, title, cluster_id, namespace, name, container, build_id, error_reason, logs }
func (h *Handler) AnalyzeLogs(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Source      string `json:"source"`
		Title       string `json:"title"`
		ClusterID   int64  `json:"cluster_id"`
		Namespace   string `json:"namespace"`
		Name        string `json:"name"`
		Container   string `json:"container"`
		BuildID     int64  `json:"build_id"`
		ErrorReason string `json:"error_reason"`
		Logs        string `json:"logs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid body", err))
		return
	}
	if req.Logs == "" && req.ErrorReason == "" {
		httpx.WriteError(w, apperr.Validation("logs or error_reason is required", nil))
		return
	}
	uid := httpauth.UserID(r.Context())
	result, err := h.svc.AnalyzeLogs(r.Context(), diagnosisapp.LogAnalyzeInput{
		Source:      diagnosisapp.LogSource(req.Source),
		Title:       req.Title,
		ClusterID:   req.ClusterID, Namespace: req.Namespace, Name: req.Name, Container: req.Container,
		BuildID: req.BuildID, ErrorReason: req.ErrorReason, Logs: req.Logs,
		UserID: uid, UserName: httpauth.Username(r.Context()),
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, result)
}

// AnalyzeLogsStream POST /api/v1/diagnosis/analyze-logs/stream
// 流式版本的日志诊断。响应为 text/event-stream（SSE）。
// 事件类型：
//   delta — 增量文本（多次）
//   done  — 流结束，携带完整结果（含 latency）
//   error — 出错时推送错误信息
func (h *Handler) AnalyzeLogsStream(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Source      string `json:"source"`
		Title       string `json:"title"`
		ClusterID   int64  `json:"cluster_id"`
		Namespace   string `json:"namespace"`
		Name        string `json:"name"`
		Container   string `json:"container"`
		BuildID     int64  `json:"build_id"`
		ErrorReason string `json:"error_reason"`
		Logs        string `json:"logs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid body", err))
		return
	}
	if req.Logs == "" && req.ErrorReason == "" {
		httpx.WriteError(w, apperr.Validation("logs or error_reason is required", nil))
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		httpx.WriteError(w, apperr.Internal("streaming unsupported", nil))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	writeSSE := func(event string, data any) {
		payload, _ := json.Marshal(data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, payload)
		flusher.Flush()
	}

	onDelta := func(delta string) {
		writeSSE("delta", map[string]string{"delta": delta})
	}
	uid := httpauth.UserID(r.Context())
	result, err := h.svc.AnalyzeLogsStream(r.Context(), diagnosisapp.LogAnalyzeInput{
		Source:      diagnosisapp.LogSource(req.Source),
		Title:       req.Title,
		ClusterID:   req.ClusterID, Namespace: req.Namespace, Name: req.Name, Container: req.Container,
		BuildID: req.BuildID, ErrorReason: req.ErrorReason, Logs: req.Logs,
		UserID: uid, UserName: httpauth.Username(r.Context()),
	}, onDelta)
	if err != nil {
		writeSSE("error", map[string]string{"message": err.Error()})
		return
	}
	writeSSE("done", result)
}

// Chat POST /api/v1/diagnosis/chat
// 多轮对话式 AI 助手。
// Body: { messages: [{role, content}], scene, session_id }
func (h *Handler) Chat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		Scene     string `json:"scene"`
		SessionID int64  `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid body", err))
		return
	}
	if len(req.Messages) == 0 {
		httpx.WriteError(w, apperr.Validation("messages is required", nil))
		return
	}
	msgs := make([]diagnosisapp.ChatMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		msgs = append(msgs, diagnosisapp.ChatMessage{Role: diagnosisapp.ChatRole(m.Role), Content: m.Content})
	}
	uid := httpauth.UserID(r.Context())
	result, err := h.svc.Chat(r.Context(), diagnosisapp.ChatInput{
		Messages: msgs, Scene: req.Scene, SessionID: req.SessionID,
		UserID: uid, UserName: httpauth.Username(r.Context()),
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, result)
}

// ListFAQ GET /api/v1/diagnosis/faq?scene=
// 返回指定场景的常见问题列表，用于前端展示快捷问题。
func (h *Handler) ListFAQ(w http.ResponseWriter, r *http.Request) {
	scene := r.URL.Query().Get("scene")
	items := h.svc.ListFAQ(scene)
	httpx.OK(w, items)
}

// ChatStream POST /api/v1/diagnosis/chat/stream
// 多轮对话流式接口。响应为 text/event-stream（SSE）。
// 事件类型：
//   meta    — 推送意图/工具/引用/会话/画像等元信息（开始流式回答前发送一次）
//   delta   — 增量文本（多次）
//   done    — 流结束，携带完整结果（含 latency）
//   error   — 出错时推送错误信息
func (h *Handler) ChatStream(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		Scene     string `json:"scene"`
		SessionID int64  `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid body", err))
		return
	}
	if len(req.Messages) == 0 {
		httpx.WriteError(w, apperr.Validation("messages is required", nil))
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		httpx.WriteError(w, apperr.Internal("streaming unsupported", nil))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	writeSSE := func(event string, data any) {
		payload, _ := json.Marshal(data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, payload)
		flusher.Flush()
	}

	msgs := make([]diagnosisapp.ChatMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		msgs = append(msgs, diagnosisapp.ChatMessage{Role: diagnosisapp.ChatRole(m.Role), Content: m.Content})
	}
	uid := httpauth.UserID(r.Context())

	onMeta := func(meta diagnosisapp.ChatResult) {
		writeSSE("meta", meta)
	}
	onDelta := func(delta string) {
		writeSSE("delta", map[string]string{"delta": delta})
	}

	result, err := h.svc.ChatStream(r.Context(), diagnosisapp.ChatInput{
		Messages: msgs, Scene: req.Scene, SessionID: req.SessionID,
		UserID: uid, UserName: httpauth.Username(r.Context()),
	}, onMeta, onDelta)
	if err != nil {
		writeSSE("error", map[string]string{"message": err.Error()})
		return
	}
	writeSSE("done", result)
}
