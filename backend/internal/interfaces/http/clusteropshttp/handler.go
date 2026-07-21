// Package clusteropshttp 是集群运维（监控/运维/通知）的 HTTP handlers。
// 暴露节点状态查询/同步、异常 Pod/节点查询、受影响应用预览与通知分发、计划运维任务 CRUD。
package clusteropshttp

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/vortexops/vortexops/internal/application/clusteropsapp"
	"github.com/vortexops/vortexops/internal/domain/clusterops"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpauth"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpx"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// Handler 处理 /api/v1/clusters/{id}/... 运维相关路由。
type Handler struct {
	svc *clusteropsapp.Service
}

// NewHandler 创建 handler。
func NewHandler(svc *clusteropsapp.Service) *Handler {
	return &Handler{svc: svc}
}

func clusterIDFromURL(r *http.Request) (int64, error) {
	v := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(v, 10, 64)
	if err != nil || id <= 0 {
		return 0, apperr.Validation("invalid cluster id", err)
	}
	return id, nil
}

// --- 节点状态 ---

// ListNodeStatuses GET /api/v1/clusters/{id}/node-statuses
func (h *Handler) ListNodeStatuses(w http.ResponseWriter, r *http.Request) {
	cid, err := clusterIDFromURL(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	items, err := h.svc.ListNodeStatuses(r.Context(), cid)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toNodeStatusDTOs(items))
}

// SyncNodeStatuses POST /api/v1/clusters/{id}/node-statuses/sync
func (h *Handler) SyncNodeStatuses(w http.ResponseWriter, r *http.Request) {
	cid, err := clusterIDFromURL(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	items, err := h.svc.SyncNodeStatuses(r.Context(), cid)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toNodeStatusDTOs(items))
}

// --- 异常资源 ---

// ListAbnormalPods GET /api/v1/clusters/{id}/abnormal-pods
func (h *Handler) ListAbnormalPods(w http.ResponseWriter, r *http.Request) {
	cid, err := clusterIDFromURL(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	items, err := h.svc.ListAbnormalPods(r.Context(), cid)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, items)
}

// ListAbnormalNodes GET /api/v1/clusters/{id}/abnormal-nodes
func (h *Handler) ListAbnormalNodes(w http.ResponseWriter, r *http.Request) {
	cid, err := clusterIDFromURL(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	items, err := h.svc.ListAbnormalNodes(r.Context(), cid)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, items)
}

// --- 受影响应用预览与通知分发 ---

// PreviewAffected POST /api/v1/clusters/{id}/affected-preview
func (h *Handler) PreviewAffected(w http.ResponseWriter, r *http.Request) {
	cid, err := clusterIDFromURL(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	var req notifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	apps, err := h.svc.PreviewAffected(r.Context(), cid, req.Scope, req.NodeName, req.PodNamespace, req.PodName)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, apps)
}

// NotifyAffected POST /api/v1/clusters/{id}/notify-affected
func (h *Handler) NotifyAffected(w http.ResponseWriter, r *http.Request) {
	cid, err := clusterIDFromURL(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	var req notifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	if req.Subject == "" {
		req.Subject = "【集群异常】受影响应用通知"
	}
	if req.Body == "" {
		req.Body = "您负责的应用受集群异常影响，请及时关注。"
	}
	res, err := h.svc.NotifyAffected(r.Context(), cid, clusteropsapp.NotifyInput{
		Scope:        req.Scope,
		NodeName:     req.NodeName,
		PodNamespace: req.PodNamespace,
		PodName:      req.PodName,
		Subject:      req.Subject,
		Body:         req.Body,
		ActorID:      httpauth.UserID(r.Context()),
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, res)
}

// --- 计划运维任务 ---

// ListOperations GET /api/v1/clusters/{id}/operations
func (h *Handler) ListOperations(w http.ResponseWriter, r *http.Request) {
	cid, err := clusterIDFromURL(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	page, size, _ := httpx.Pagination(r)
	items, total, err := h.svc.ListOperations(r.Context(), cid, clusterops.OperationStatus(r.URL.Query().Get("status")), page, size)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, httpx.Paged[operationDTO]{
		Items: toOperationDTOs(items), Total: total, Page: page, Size: size,
	})
}

// CreateOperation POST /api/v1/clusters/{id}/operations
func (h *Handler) CreateOperation(w http.ResponseWriter, r *http.Request) {
	cid, err := clusterIDFromURL(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	var req createOperationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	var scheduledAt time.Time
	if req.ScheduledAt != "" {
		t, perr := time.Parse(time.RFC3339, req.ScheduledAt)
		if perr != nil {
			httpx.WriteError(w, apperr.Validation("invalid scheduled_at (expect RFC3339)", perr))
			return
		}
		scheduledAt = t
	}
	op, err := h.svc.CreateOperation(r.Context(), clusteropsapp.CreateOperationInput{
		ClusterID:      cid,
		NodeName:       req.NodeName,
		OperationType:  clusterops.OperationType(req.OperationType),
		ScheduledAt:    scheduledAt,
		NotifyAffected: req.NotifyAffected,
		ActorID:        httpauth.UserID(r.Context()),
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Created(w, toOperationDTO(op))
}

// CancelOperation DELETE /api/v1/clusters/{id}/operations/{opId}
func (h *Handler) CancelOperation(w http.ResponseWriter, r *http.Request) {
	opID, err := strconv.ParseInt(chi.URLParam(r, "opId"), 10, 64)
	if err != nil || opID <= 0 {
		httpx.WriteError(w, apperr.Validation("invalid operation id", err))
		return
	}
	if err := h.svc.CancelOperation(r.Context(), opID, httpauth.UserID(r.Context())); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// ============================================================================
// DTO
// ============================================================================

type notifyRequest struct {
	Scope        string `json:"scope"`         // pod|node|cluster
	NodeName     string `json:"node_name"`
	PodNamespace string `json:"pod_namespace"`
	PodName      string `json:"pod_name"`
	Subject      string `json:"subject"`
	Body         string `json:"body"`
}

type createOperationRequest struct {
	NodeName       string `json:"node_name"`
	OperationType  string `json:"operation_type"`
	ScheduledAt    string `json:"scheduled_at"` // RFC3339；空表示立即执行
	NotifyAffected bool   `json:"notify_affected"`
}

type nodeStatusDTO struct {
	ID                     int64             `json:"id"`
	UUID                   string            `json:"uuid"`
	ClusterID              int64             `json:"cluster_id"`
	NodeName               string            `json:"node_name"`
	Status                 string            `json:"status"`
	Unschedulable          bool              `json:"unschedulable"`
	KubeletVersion         string            `json:"kubelet_version,omitempty"`
	AllocatableCPUM        int               `json:"allocatable_cpu_m"`
	AllocatableMemoryBytes int64             `json:"allocatable_memory_bytes"`
	AllocatableGPU         int               `json:"allocatable_gpu"`
	UsedCPUM               int               `json:"used_cpu_m"`
	UsedMemoryBytes        int64             `json:"used_memory_bytes"`
	UsedGPU                int               `json:"used_gpu"`
	PodCount               int               `json:"pod_count"`
	AbnormalPodCount       int               `json:"abnormal_pod_count"`
	Roles                  []string          `json:"roles"`
	Taints                 []map[string]any  `json:"taints"`
	Addresses              []map[string]any  `json:"addresses"`
	LastSyncedAt           *time.Time        `json:"last_synced_at,omitempty"`
}

func toNodeStatusDTOs(items []*clusterops.NodeStatus) []nodeStatusDTO {
	out := make([]nodeStatusDTO, 0, len(items))
	for _, n := range items {
		out = append(out, nodeStatusDTO{
			ID: n.ID, UUID: n.UUID.String(), ClusterID: n.ClusterID, NodeName: n.NodeName,
			Status: string(n.Status), Unschedulable: n.Unschedulable, KubeletVersion: n.KubeletVersion,
			AllocatableCPUM: n.AllocatableCPUM, AllocatableMemoryBytes: n.AllocatableMemoryBytes,
			AllocatableGPU: n.AllocatableGPU,
			UsedCPUM: n.UsedCPUM, UsedMemoryBytes: n.UsedMemoryBytes, UsedGPU: n.UsedGPU,
			PodCount: n.PodCount, AbnormalPodCount: n.AbnormalPodCount,
			Roles: n.Roles, Taints: n.Taints, Addresses: n.Addresses,
			LastSyncedAt: n.LastSyncedAt,
		})
	}
	return out
}

type operationDTO struct {
	ID              int64     `json:"id"`
	UUID            string    `json:"uuid"`
	ClusterID       int64     `json:"cluster_id"`
	NodeName        string    `json:"node_name,omitempty"`
	OperationType   string    `json:"operation_type"`
	ScheduledAt     time.Time `json:"scheduled_at"`
	Status          string    `json:"status"`
	ExecutedAt      *time.Time `json:"executed_at,omitempty"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
	ErrorMessage    string    `json:"error_message,omitempty"`
	NotifyAffected  bool      `json:"notify_affected"`
	NotifiedUserIDs []int64   `json:"notified_user_ids"`
	CreatedAt       time.Time `json:"created_at"`
}

func toOperationDTOs(items []*clusterops.Operation) []operationDTO {
	out := make([]operationDTO, 0, len(items))
	for _, o := range items {
		out = append(out, toOperationDTO(o))
	}
	return out
}

func toOperationDTO(o *clusterops.Operation) operationDTO {
	ids := o.NotifiedUserIDs
	if ids == nil {
		ids = []int64{}
	}
	return operationDTO{
		ID: o.ID, UUID: o.UUID.String(), ClusterID: o.ClusterID, NodeName: o.NodeName,
		OperationType: string(o.OperationType), ScheduledAt: o.ScheduledAt, Status: string(o.Status),
		ExecutedAt: o.ExecutedAt, CompletedAt: o.CompletedAt, ErrorMessage: o.ErrorMessage,
		NotifyAffected: o.NotifyAffected, NotifiedUserIDs: ids, CreatedAt: o.CreatedAt,
	}
}
