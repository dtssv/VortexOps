// Package dnshttp 提供 DNS 映射 HTTP API。
package dnshttp

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/vortexops/vortexops/internal/application/dnsapp"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpauth"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpx"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// Handler DNS HTTP handler。
type Handler struct {
	svc *dnsapp.Service
}

// NewHandler 创建 handler。
func NewHandler(svc *dnsapp.Service) *Handler {
	return &Handler{svc: svc}
}

// GetByGroup GET /api/v1/groups/{groupId}/dns
func (h *Handler) GetByGroup(w http.ResponseWriter, r *http.Request) {
	if mustAuth(w, r) == 0 {
		return
	}
	groupID, err := strconv.ParseInt(chi.URLParam(r, "groupId"), 10, 64)
	if err != nil || groupID <= 0 {
		httpx.WriteError(w, apperr.Validation("invalid group id", err))
		return
	}
	rec, backends, err := h.svc.GetByGroupID(r.Context(), groupID)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	type backendDTO struct {
		PodIP   string `json:"pod_ip"`
		PodName string `json:"pod_name"`
		Healthy bool   `json:"healthy"`
		Weight  int    `json:"weight"`
	}
	resp := map[string]any{"record": nil, "backends": []backendDTO{}}
	if rec != nil {
		resp["record"] = map[string]any{
			"id": rec.ID, "group_id": rec.GroupID, "fqdn": rec.FQDN,
			"zone": rec.Zone, "ttl": rec.TTL, "status": rec.Status,
		}
		dtos := make([]backendDTO, 0, len(backends))
		for _, b := range backends {
			dtos = append(dtos, backendDTO{PodIP: b.PodIP, PodName: b.PodName, Healthy: b.Healthy, Weight: b.Weight})
		}
		resp["backends"] = dtos
	}
	httpx.OK(w, resp)
}

// Reconcile POST /api/v1/groups/{groupId}/dns/reconcile
func (h *Handler) Reconcile(w http.ResponseWriter, r *http.Request) {
	if mustAuth(w, r) == 0 {
		return
	}
	groupID, err := strconv.ParseInt(chi.URLParam(r, "groupId"), 10, 64)
	if err != nil || groupID <= 0 {
		httpx.WriteError(w, apperr.Validation("invalid group id", err))
		return
	}
	var req struct {
		HealthyIPs []string `json:"healthy_ips"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	if err := h.svc.ReconcileBackends(r.Context(), groupID, req.HealthyIPs); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, map[string]string{"status": "reconciled"})
}

func mustAuth(w http.ResponseWriter, r *http.Request) int64 {
	uid := httpauth.UserID(r.Context())
	if uid == 0 {
		httpx.WriteError(w, apperr.Unauthorized("not authenticated", nil))
		return 0
	}
	return uid
}
