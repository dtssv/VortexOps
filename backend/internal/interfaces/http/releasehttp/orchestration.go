package releasehttp

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/vortexops/vortexops/internal/application/releaseapp"
	"github.com/vortexops/vortexops/internal/domain/release"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpx"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// TriggerOrchestration POST /api/v1/applications/{appId}/multi-release
func (h *Handler) TriggerOrchestration(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	appID, ok := parseID(w, chi.URLParam(r, "appId"))
	if !ok {
		return
	}
	var req struct {
		WorkspaceID      int64                    `json:"workspace_id"`
		Name             string                   `json:"name"`
		Strategy         string                   `json:"strategy"`
		ImageID          int64                    `json:"image_id"`
		ConfigVersion    int                      `json:"config_version"`
		Replicas         int                      `json:"replicas"`
		MaxSurge         string                   `json:"max_surge"`
		MaxUnavailable   string                   `json:"max_unavailable"`
		BatchSize        int                      `json:"batch_size"`
		BatchIntervalSec int                      `json:"batch_interval_sec"`
		Targets          []orchestrationTargetReq `json:"targets"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	targets := make([]release.OrchestrationTargetInput, 0, len(req.Targets))
	for _, t := range req.Targets {
		targets = append(targets, release.OrchestrationTargetInput{
			GroupID:          t.GroupID,
			ClusterID:        t.ClusterID,
			ImageID:          t.ImageID,
			ConfigVersion:    t.ConfigVersion,
			Replicas:         t.Replicas,
			Seq:              t.Seq,
			BatchSize:        t.BatchSize,
			BatchIntervalSec: t.BatchIntervalSec,
		})
	}
	o, err := h.svc.TriggerOrchestration(r.Context(), releaseapp.TriggerOrchestrationInput{
		WorkspaceID:      req.WorkspaceID,
		ApplicationID:    appID,
		Name:             req.Name,
		Strategy:         release.OrchestrationStrategy(req.Strategy),
		ImageID:          req.ImageID,
		ConfigVersion:    req.ConfigVersion,
		Replicas:         req.Replicas,
		MaxSurge:         req.MaxSurge,
		MaxUnavailable:   req.MaxUnavailable,
		BatchSize:        req.BatchSize,
		BatchIntervalSec: req.BatchIntervalSec,
		TriggeredBy:      uid,
		TriggerSource:    release.TriggerManual,
		Targets:          targets,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Created(w, toOrchestrationDTO(o))
}

type orchestrationTargetReq struct {
	GroupID          int64 `json:"group_id"`
	ClusterID        int64 `json:"cluster_id"`
	ImageID          int64 `json:"image_id"`
	ConfigVersion    int   `json:"config_version"`
	Replicas         int   `json:"replicas"`
	Seq              int   `json:"seq"`
	BatchSize        int   `json:"batch_size"`
	BatchIntervalSec int   `json:"batch_interval_sec"`
}

// GetOrchestration GET /api/v1/orchestrations/{id}
func (h *Handler) GetOrchestration(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	o, err := h.svc.GetOrchestration(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	targets, _ := h.svc.ListOrchestrationTargets(r.Context(), id)
	httpx.OK(w, orchestrationDetailDTO{Orchestration: toOrchestrationDTO(o), Targets: toOrchestrationTargetDTOs(targets)})
}

// ListOrchestrations GET /api/v1/applications/{appId}/orchestrations?page=&size=
func (h *Handler) ListOrchestrations(w http.ResponseWriter, r *http.Request) {
	appID, ok := parseID(w, chi.URLParam(r, "appId"))
	if !ok {
		return
	}
	page, size, _ := httpx.Pagination(r)
	items, total, err := h.svc.ListOrchestrations(r.Context(), appID, page, size)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, httpx.Paged[orchestrationDTO]{
		Items: toOrchestrationDTOs(items), Total: total, Page: page, Size: size,
	})
}

// AbortOrchestration POST /api/v1/orchestrations/{id}/abort
func (h *Handler) AbortOrchestration(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	o, err := h.svc.AbortOrchestration(r.Context(), id, uid)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toOrchestrationDTO(o))
}

// --- DTO ---

type orchestrationDTO struct {
	ID               int64  `json:"id"`
	UUID             string `json:"uuid"`
	WorkspaceID      int64  `json:"workspace_id"`
	ApplicationID    int64  `json:"application_id"`
	Name             string `json:"name"`
	Strategy         string `json:"strategy"`
	Status           string `json:"status"`
	ProgressPercent  int    `json:"progress_percent"`
	ImageID          int64  `json:"image_id"`
	Replicas         int    `json:"replicas"`
	BatchSize        int    `json:"batch_size"`
	BatchIntervalSec int    `json:"batch_interval_sec"`
	FailureReason    string `json:"failure_reason"`
	StartedAt        *string `json:"started_at"`
	FinishedAt       *string `json:"finished_at"`
	DurationMs       int64  `json:"duration_ms"`
	TriggeredBy      int64  `json:"triggered_by"`
	CreatedAt        string `json:"created_at"`
}

type orchestrationTargetDTO struct {
	ID               int64  `json:"id"`
	GroupID          int64  `json:"group_id"`
	ClusterID        int64  `json:"cluster_id"`
	ImageID          int64  `json:"image_id"`
	Replicas         int    `json:"replicas"`
	Seq              int    `json:"seq"`
	BatchSize        int    `json:"batch_size"`
	ReleaseID        int64  `json:"release_id"`
	Status           string `json:"status"`
	FailureReason    string `json:"failure_reason"`
	StartedAt        *string `json:"started_at"`
	FinishedAt       *string `json:"finished_at"`
}

type orchestrationDetailDTO struct {
	Orchestration orchestrationDTO      `json:"orchestration"`
	Targets       []orchestrationTargetDTO `json:"targets"`
}

func toOrchestrationDTO(o *release.Orchestration) orchestrationDTO {
	dto := orchestrationDTO{
		ID: o.ID, UUID: o.UUID.String(), WorkspaceID: o.WorkspaceID, ApplicationID: o.ApplicationID,
		Name: o.Name, Strategy: string(o.Strategy), Status: string(o.Status),
		ProgressPercent: o.ProgressPercent, ImageID: o.ImageID, Replicas: o.Replicas,
		BatchSize: o.BatchSize, BatchIntervalSec: o.BatchIntervalSec, FailureReason: o.FailureReason,
		DurationMs: o.DurationMs, TriggeredBy: o.TriggeredBy,
		CreatedAt: o.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}
	if o.StartedAt != nil {
		s := o.StartedAt.Format("2006-01-02T15:04:05Z")
		dto.StartedAt = &s
	}
	if o.FinishedAt != nil {
		f := o.FinishedAt.Format("2006-01-02T15:04:05Z")
		dto.FinishedAt = &f
	}
	return dto
}

func toOrchestrationDTOs(items []*release.Orchestration) []orchestrationDTO {
	out := make([]orchestrationDTO, 0, len(items))
	for _, o := range items {
		out = append(out, toOrchestrationDTO(o))
	}
	return out
}

func toOrchestrationTargetDTOs(items []*release.OrchestrationTarget) []orchestrationTargetDTO {
	out := make([]orchestrationTargetDTO, 0, len(items))
	for _, t := range items {
		dto := orchestrationTargetDTO{
			ID: t.ID, GroupID: t.GroupID, ClusterID: t.ClusterID, ImageID: t.ImageID,
			Replicas: t.Replicas, Seq: t.Seq, BatchSize: t.BatchSize, ReleaseID: t.ReleaseID,
			Status: string(t.Status), FailureReason: t.FailureReason,
		}
		if t.StartedAt != nil {
			s := t.StartedAt.Format("2006-01-02T15:04:05Z")
			dto.StartedAt = &s
		}
		if t.FinishedAt != nil {
			f := t.FinishedAt.Format("2006-01-02T15:04:05Z")
			dto.FinishedAt = &f
		}
		out = append(out, dto)
	}
	return out
}

// ensure strconv used (avoid unused import in case of edits)
var _ = strconv.Itoa
