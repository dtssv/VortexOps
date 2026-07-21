// Package audithttp 是审计日志领域的 HTTP handlers。
package audithttp

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/vortexops/vortexops/internal/application/auditapp"
	"github.com/vortexops/vortexops/internal/domain/audit"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpx"
)

// Handler 处理审计日志路由。
type Handler struct {
	svc *auditapp.Service
}

// NewHandler 创建审计 handler。
func NewHandler(svc *auditapp.Service) *Handler {
	return &Handler{svc: svc}
}

// GetAuditLog GET /api/v1/audit-logs/{id}
func (h *Handler) GetAuditLog(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.WriteError(w, nil)
		return
	}
	log, err := h.svc.GetByID(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toLogDTO(log))
}

// ListAuditLogs GET /api/v1/audit-logs?user_id=&workspace_id=&resource_type=&action=&start=&end=&page=&size=
func (h *Handler) ListAuditLogs(w http.ResponseWriter, r *http.Request) {
	page, size, _ := httpx.Pagination(r)
	userID, _ := strconv.ParseInt(r.URL.Query().Get("user_id"), 10, 64)
	wsID, _ := strconv.ParseInt(r.URL.Query().Get("workspace_id"), 10, 64)
	startTime, _ := time.Parse(time.RFC3339, r.URL.Query().Get("start"))
	endTime, _ := time.Parse(time.RFC3339, r.URL.Query().Get("end"))
	items, total, err := h.svc.List(r.Context(), audit.Query{
		UserID: userID, WorkspaceID: wsID, ResourceType: r.URL.Query().Get("resource_type"),
		Action: audit.Action(r.URL.Query().Get("action")), StartTime: startTime, EndTime: endTime,
		Offset: (page - 1) * size, Limit: size,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, httpx.Paged[logDTO]{
		Items: toLogDTOs(items), Total: total, Page: page, Size: size,
	})
}

type logDTO struct {
	ID              int64          `json:"id"`
	UUID            string         `json:"uuid"`
	UserID          int64          `json:"user_id"`
	UserName        string         `json:"user_name,omitempty"`
	WorkspaceID     int64          `json:"workspace_id,omitempty"`
	ResourceType    string         `json:"resource_type"`
	ResourceID      int64          `json:"resource_id,omitempty"`
	ResourceName    string         `json:"resource_name,omitempty"`
	Action          string         `json:"action"`
	Operation       string         `json:"operation,omitempty"`
	RequestID       string         `json:"request_id,omitempty"`
	Method          string         `json:"method,omitempty"`
	Path            string         `json:"path,omitempty"`
	StatusCode      int            `json:"status_code,omitempty"`
	ClientIP        string         `json:"client_ip,omitempty"`
	UserAgent       string         `json:"user_agent,omitempty"`
	RequestBody     map[string]any `json:"request_body,omitempty"`
	ResponseSummary map[string]any `json:"response_summary,omitempty"`
	DurationMs      int            `json:"duration_ms,omitempty"`
	ErrorMessage    string         `json:"error_message,omitempty"`
	CreatedAt       string         `json:"created_at"`
}

func toLogDTO(l *audit.Log) *logDTO {
	if l == nil {
		return nil
	}
	return &logDTO{
		ID: l.ID, UUID: l.UUID.String(), UserID: l.UserID, UserName: l.UserName, WorkspaceID: l.WorkspaceID,
		ResourceType: l.ResourceType, ResourceID: l.ResourceID, ResourceName: l.ResourceName, Action: string(l.Action),
		Operation: l.Operation, RequestID: l.RequestID, Method: l.Method, Path: l.Path, StatusCode: l.StatusCode,
		ClientIP: l.ClientIP, UserAgent: l.UserAgent, RequestBody: l.RequestBody,
		ResponseSummary: l.ResponseSummary, DurationMs: l.DurationMs, ErrorMessage: l.ErrorMessage,
		CreatedAt: l.CreatedAt.Format(time.RFC3339),
	}
}

func toLogDTOs(items []*audit.Log) []logDTO {
	out := make([]logDTO, 0, len(items))
	for _, l := range items {
		out = append(out, *toLogDTO(l))
	}
	return out
}
