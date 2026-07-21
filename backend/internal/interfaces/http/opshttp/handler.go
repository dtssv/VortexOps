// Package opshttp 是运维操作（exec / port-forward）HTTP handlers。
package opshttp

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/vortexops/vortexops/internal/application/opsapp"
	"github.com/vortexops/vortexops/internal/domain/behavioraudit"
	"github.com/vortexops/vortexops/internal/domain/opssession"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpauth"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpx"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// Handler 运维 HTTP handler。
type Handler struct {
	svc *opsapp.Service
}

// NewHandler 创建 handler。
func NewHandler(svc *opsapp.Service) *Handler {
	return &Handler{svc: svc}
}

type execRequest struct {
	ClusterID int64    `json:"cluster_id"`
	Namespace string   `json:"namespace"`
	Pod       string   `json:"pod"`
	Container string   `json:"container,omitempty"`
	Command   []string `json:"command"`
	TTY       bool     `json:"tty,omitempty"`
}

// Exec POST /api/v1/ops/exec
func (h *Handler) Exec(w http.ResponseWriter, r *http.Request) {
	var req execRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	err := h.svc.Exec(r.Context(), opsapp.ExecInput{
		UserID: httpauth.UserID(r.Context()), UserName: httpauth.Username(r.Context()),
		ClusterID: req.ClusterID, Namespace: req.Namespace, Pod: req.Pod,
		Container: req.Container, Command: req.Command, TTY: req.TTY,
		Stdout: w, Stderr: w,
	})
	if err != nil {
		httpx.WriteError(w, err)
	}
}

type portForwardRequest struct {
	ClusterID int64 `json:"cluster_id"`
	Namespace string `json:"namespace"`
	Pod       string `json:"pod"`
	Port      int   `json:"port"`
	LocalPort int   `json:"local_port,omitempty"`
}

// PortForward POST /api/v1/ops/port-forward
// 非阻塞启动端口转发，返回分配的本地端口与会话 ID。
func (h *Handler) PortForward(w http.ResponseWriter, r *http.Request) {
	var req portForwardRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if req.Port == 0 {
		httpx.WriteError(w, apperr.Validation("port is required", nil))
		return
	}
	res, err := h.svc.StartPortForward(r.Context(), opsapp.PortForwardStartInput{
		UserID: httpauth.UserID(r.Context()), UserName: httpauth.Username(r.Context()),
		ClusterID: req.ClusterID, Namespace: req.Namespace, Pod: req.Pod,
		Port: req.Port, LocalPort: req.LocalPort,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, res)
}

// ListSessions GET /api/v1/ops/sessions
func (h *Handler) ListSessions(w http.ResponseWriter, r *http.Request) {
	sessions := h.svc.ListSessions()
	out := make([]sessionDTO, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, sessionDTO{
			ID: s.ID, Type: s.Type, ClusterID: s.ClusterID,
			Namespace: s.Namespace, Pod: s.Pod, StartedAt: s.StartedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}
	httpx.OK(w, out)
}

// CloseSession DELETE /api/v1/ops/sessions/{id}
func (h *Handler) CloseSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		httpx.WriteError(w, nil)
		return
	}
	if !h.svc.CloseSession(id) {
		httpx.WriteError(w, nil)
		return
	}
	httpx.NoContent(w)
}

// --- WebSSH 交互式 exec（WebSocket） ---

// ExecWS GET /api/v1/ops/exec/ws?cluster_id=&namespace=&pod=&container=&command=
// 通过查询参数 ?token=<jwt> 鉴权（浏览器 WebSocket 无法自定义 header）。
func (h *Handler) ExecWS(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	clusterID, _ := strconv.ParseInt(q.Get("cluster_id"), 10, 64)
	namespace := q.Get("namespace")
	pod := q.Get("pod")
	container := q.Get("container")
	command := []string{}
	if c := q.Get("command"); c != "" {
		command = strings.Split(c, " ")
	}
	if clusterID == 0 || namespace == "" || pod == "" {
		httpx.WriteError(w, apperr.Validation("cluster_id, namespace and pod are required", nil))
		return
	}
	err := h.svc.HandleWSExec(r.Context(), w, r, opsapp.WSExecInput{
		UserID:      httpauth.UserID(r.Context()),
		UserName:    httpauth.Username(r.Context()),
		WorkspaceID: 0,
		ClusterID:   clusterID,
		Namespace:   namespace,
		Pod:         pod,
		Container:   container,
		Command:     command,
		ClientIP:    r.RemoteAddr,
	})
	if err != nil {
		// WS 升级失败前可返回 JSON；升级后已由 WS 帧传递错误。
		httpx.WriteError(w, err)
	}
}

// --- 运维会话历史与录像 ---

// ListOpsSessions GET /api/v1/ops/sessions/history?workspace_id=&cluster_id=&user_id=&status=
func (h *Handler) ListOpsSessions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, size, _ := httpx.Pagination(r)
	wsID, _ := strconv.ParseInt(q.Get("workspace_id"), 10, 64)
	clusterID, _ := strconv.ParseInt(q.Get("cluster_id"), 10, 64)
	userID, _ := strconv.ParseInt(q.Get("user_id"), 10, 64)
	items, total, err := h.svc.ListOpsSessions(r.Context(), opssession.Query{
		WorkspaceID: wsID, ClusterID: clusterID, UserID: userID,
		Status: opssession.Status(q.Get("status")),
		Offset: (page - 1) * size, Limit: size,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, httpx.Paged[opsSessionDTO]{
		Items: toOpsSessionDTOs(items), Total: total, Page: page, Size: size,
	})
}

// GetOpsSession GET /api/v1/ops/sessions/history/{id}
func (h *Handler) GetOpsSession(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	s, err := h.svc.GetOpsSession(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toOpsSessionDTO(s))
}

// ReplaySession GET /api/v1/ops/sessions/history/{id}/replay
// 返回 asciinema cast 预签名 URL，前端用 asciinema-player 回放。
func (h *Handler) ReplaySession(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	s, err := h.svc.GetOpsSession(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	if s.RecordingKey == "" {
		httpx.WriteError(w, apperr.NotFound("recording", fmt.Sprintf("session %d has no recording", id)))
		return
	}
	url, err := h.svc.PresignReplay(r.Context(), s.RecordingKey)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, map[string]string{"replay_url": url})
}

// CastDownload GET /api/v1/ops/sessions/history/{id}/cast
// 流式返回会话录像 cast 文件内容（后端代理 MinIO，避免暴露内部地址）。
// 前端 asciinema-player 直接以该 URL 作为 cast 源在线播放。
func (h *Handler) CastDownload(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	s, err := h.svc.GetOpsSession(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	if s.RecordingKey == "" {
		httpx.WriteError(w, apperr.NotFound("recording", fmt.Sprintf("session %d has no recording", id)))
		return
	}
	rc, err := h.svc.StreamRecording(r.Context(), s.RecordingKey)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", "application/x-asciicast; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, rc)
}

// ListBehaviorAudit GET /api/v1/audit/behavior?workspace_id=&session_id=&user_id=
func (h *Handler) ListBehaviorAudit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, size, _ := httpx.Pagination(r)
	wsID, _ := strconv.ParseInt(q.Get("workspace_id"), 10, 64)
	sessionID, _ := strconv.ParseInt(q.Get("session_id"), 10, 64)
	userID, _ := strconv.ParseInt(q.Get("user_id"), 10, 64)
	items, total, err := h.svc.ListBehavior(r.Context(), behavioraudit.Query{
		WorkspaceID: wsID, SessionID: sessionID, UserID: userID,
		Offset: (page - 1) * size, Limit: size,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, httpx.Paged[behaviorDTO]{
		Items: toBehaviorDTOs(items), Total: total, Page: page, Size: size,
	})
}

type opsSessionDTO struct {
	ID           int64  `json:"id"`
	UUID         string `json:"uuid"`
	WorkspaceID  int64  `json:"workspace_id"`
	ClusterID    int64  `json:"cluster_id"`
	Namespace    string `json:"namespace"`
	Pod          string `json:"pod"`
	Container    string `json:"container"`
	Type         string `json:"type"`
	Status       string `json:"status"`
	UserID       int64  `json:"user_id"`
	UserName     string `json:"user_name"`
	ClientIP     string `json:"client_ip"`
	RecordingKey string `json:"recording_key"`
	StartedAt    string `json:"started_at"`
	EndedAt      *string `json:"ended_at"`
	DurationMs   int64  `json:"duration_ms"`
}

type behaviorDTO struct {
	ID          int64  `json:"id"`
	UUID        string `json:"uuid"`
	WorkspaceID int64  `json:"workspace_id"`
	SessionID   int64  `json:"session_id"`
	ClusterID   int64  `json:"cluster_id"`
	Namespace   string `json:"namespace"`
	Pod         string `json:"pod"`
	UserID      int64  `json:"user_id"`
	UserName    string `json:"user_name"`
	Command     string `json:"command"`
	RiskLevel   string `json:"risk_level"`
	CreatedAt   string `json:"created_at"`
}

func toOpsSessionDTO(s *opssession.Session) opsSessionDTO {
	dto := opsSessionDTO{
		ID: s.ID, UUID: s.UUID.String(), WorkspaceID: s.WorkspaceID, ClusterID: s.ClusterID,
		Namespace: s.Namespace, Pod: s.Pod, Container: s.Container,
		Type: string(s.Type), Status: string(s.Status), UserID: s.UserID, UserName: s.UserName,
		ClientIP: s.ClientIP, RecordingKey: s.RecordingKey, DurationMs: s.DurationMs,
		StartedAt: s.StartedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
	if s.EndedAt != nil {
		t := s.EndedAt.Format("2006-01-02T15:04:05Z07:00")
		dto.EndedAt = &t
	}
	return dto
}

func toOpsSessionDTOs(items []*opssession.Session) []opsSessionDTO {
	out := make([]opsSessionDTO, 0, len(items))
	for _, s := range items {
		out = append(out, toOpsSessionDTO(s))
	}
	return out
}

func toBehaviorDTOs(items []*behavioraudit.Log) []behaviorDTO {
	out := make([]behaviorDTO, 0, len(items))
	for _, l := range items {
		out = append(out, behaviorDTO{
			ID: l.ID, UUID: l.UUID.String(), WorkspaceID: l.WorkspaceID, SessionID: l.SessionID,
			ClusterID: l.ClusterID, Namespace: l.Namespace, Pod: l.Pod,
			UserID: l.UserID, UserName: l.UserName, Command: l.Command,
			RiskLevel: string(l.RiskLevel), CreatedAt: l.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}
	return out
}

func parseID(w http.ResponseWriter, raw string) (int64, bool) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		httpx.WriteError(w, apperr.Validation("invalid id", err))
		return 0, false
	}
	return id, true
}

type sessionDTO struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	ClusterID int64  `json:"cluster_id"`
	Namespace string `json:"namespace"`
	Pod       string `json:"pod"`
	StartedAt string `json:"started_at"`
}

// ParsePorts 解析逗号分隔端口列表。
func ParsePorts(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
