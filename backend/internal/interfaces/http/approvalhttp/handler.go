// Package approvalhttp 是审批领域的 HTTP handlers。
package approvalhttp

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/vortexops/vortexops/internal/application/approvalapp"
	"github.com/vortexops/vortexops/internal/domain/approval"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpauth"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpx"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// Handler 处理审批相关路由。
type Handler struct {
	svc *approvalapp.Service
}

// NewHandler 创建审批 handler。
func NewHandler(svc *approvalapp.Service) *Handler {
	return &Handler{svc: svc}
}

// ListApprovals GET /api/v1/approvals?workspace_id=&resource_type=&status=&page=&size=
func (h *Handler) ListApprovals(w http.ResponseWriter, r *http.Request) {
	page, size, _ := httpx.Pagination(r)
	wsID, _ := strconv.ParseInt(r.URL.Query().Get("workspace_id"), 10, 64)
	rt := approval.ResourceType(r.URL.Query().Get("resource_type"))
	status := approval.Status(r.URL.Query().Get("status"))
	items, total, err := h.svc.List(r.Context(), approval.Query{
		WorkspaceID: wsID, ResourceType: rt, Status: status,
	}, page, size)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, httpx.Paged[approvalDTO]{
		Items: toApprovalDTOs(items), Total: total, Page: page, Size: size,
	})
}

// GetApproval GET /api/v1/approvals/{id}
func (h *Handler) GetApproval(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	a, err := h.svc.GetByID(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toApprovalDTO(a))
}

// Approve POST /api/v1/approvals/{id}/approve
func (h *Handler) Approve(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var req struct {
		Comment string `json:"comment"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	a, err := h.svc.Approve(r.Context(), id, uid, req.Comment)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toApprovalDTO(a))
}

// Reject POST /api/v1/approvals/{id}/reject
func (h *Handler) Reject(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var req struct {
		Comment string `json:"comment"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	a, err := h.svc.Reject(r.Context(), id, uid, req.Comment)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toApprovalDTO(a))
}

// --- DTO ---

type approvalDTO struct {
	ID           int64  `json:"id"`
	UUID         string `json:"uuid"`
	WorkspaceID  int64  `json:"workspace_id"`
	ResourceType string `json:"resource_type"`
	ResourceID   int64  `json:"resource_id"`
	Operation    string `json:"operation"`
	RequestedBy  int64  `json:"requested_by"`
	RequestedAt  string `json:"requested_at"`
	ApproverRole string `json:"approver_role"`
	Status       string `json:"status"`
	ApproverID   int64  `json:"approver_id"`
	ApprovedAt   *string `json:"approved_at"`
	Comment      string `json:"comment"`
	ExpiresAt    *string `json:"expires_at"`
	CreatedAt    string `json:"created_at"`
}

func toApprovalDTO(a *approval.Approval) approvalDTO {
	dto := approvalDTO{
		ID: a.ID, UUID: a.UUID.String(), WorkspaceID: a.WorkspaceID,
		ResourceType: string(a.ResourceType), ResourceID: a.ResourceID, Operation: a.Operation,
		RequestedBy: a.RequestedBy, RequestedAt: a.RequestedAt.Format("2006-01-02T15:04:05Z"),
		ApproverRole: a.ApproverRole, Status: string(a.Status), ApproverID: a.ApproverID,
		Comment: a.Comment, CreatedAt: a.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}
	if a.ApprovedAt != nil {
		s := a.ApprovedAt.Format("2006-01-02T15:04:05Z")
		dto.ApprovedAt = &s
	}
	if a.ExpiresAt != nil {
		s := a.ExpiresAt.Format("2006-01-02T15:04:05Z")
		dto.ExpiresAt = &s
	}
	return dto
}

func toApprovalDTOs(items []*approval.Approval) []approvalDTO {
	out := make([]approvalDTO, 0, len(items))
	for _, a := range items {
		out = append(out, toApprovalDTO(a))
	}
	return out
}

// --- helpers ---

func mustAuth(w http.ResponseWriter, r *http.Request) int64 {
	uid := httpauth.UserID(r.Context())
	if uid == 0 {
		httpx.WriteError(w, apperr.Unauthorized("not authenticated", nil))
		return 0
	}
	return uid
}

func parseID(w http.ResponseWriter, raw string) (int64, bool) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		httpx.WriteError(w, apperr.Validation("invalid id", err))
		return 0, false
	}
	return id, true
}
