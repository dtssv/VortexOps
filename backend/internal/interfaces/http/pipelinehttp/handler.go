// Package pipelinehttp 是流水线领域的 HTTP handlers。
package pipelinehttp

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/vortexops/vortexops/internal/application/pipelineapp"
	"github.com/vortexops/vortexops/internal/domain/pipeline"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpauth"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpx"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// Handler 处理流水线路由。
type Handler struct {
	svc *pipelineapp.Service
}

// NewHandler 创建 handler。
func NewHandler(svc *pipelineapp.Service) *Handler { return &Handler{svc: svc} }

// CreatePipeline POST /api/v1/pipelines
func (h *Handler) CreatePipeline(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	var req struct {
		WorkspaceID       int64                  `json:"workspace_id"`
		Scope             string                 `json:"scope"`
		ScopeID           int64                  `json:"scope_id"`
		Name              string                 `json:"name"`
		Description       string                 `json:"description"`
		Trigger           string                 `json:"trigger"`
		TriggerConfig     map[string]any         `json:"trigger_config"`
		TriggerOnPipeline int64                  `json:"trigger_on_pipeline"`
		Stages            []pipelineapp.StageInput `json:"stages"`
		Enabled           bool                   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid body", err))
		return
	}
	p, err := h.svc.CreatePipeline(r.Context(), pipelineapp.CreatePipelineInput{
		WorkspaceID: req.WorkspaceID, Scope: pipeline.Scope(req.Scope), ScopeID: req.ScopeID,
		Name: req.Name, Description: req.Description, Trigger: pipeline.Trigger(req.Trigger),
		TriggerConfig: req.TriggerConfig, TriggerOnPipeline: req.TriggerOnPipeline, Stages: req.Stages,
		Enabled: req.Enabled, CreatedBy: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Created(w, toPipelineDTO(p))
}

// GetPipeline GET /api/v1/pipelines/{id}
func (h *Handler) GetPipeline(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	p, err := h.svc.GetPipeline(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	stages, _ := h.svc.ListStages(r.Context(), id)
	httpx.OK(w, map[string]any{"pipeline": toPipelineDTO(p), "stages": toStageDTOs(stages)})
}

// ListPipelines GET /api/v1/pipelines?workspace_id=&scope=&scope_id=&enabled=&page=&size=
func (h *Handler) ListPipelines(w http.ResponseWriter, r *http.Request) {
	page, size, _ := httpx.Pagination(r)
	wsID, _ := strconv.ParseInt(r.URL.Query().Get("workspace_id"), 10, 64)
	scopeID, _ := strconv.ParseInt(r.URL.Query().Get("scope_id"), 10, 64)
	q := pipeline.PipelineQuery{
		WorkspaceID: wsID, Scope: pipeline.Scope(r.URL.Query().Get("scope")), ScopeID: scopeID,
	}
	if e := r.URL.Query().Get("enabled"); e != "" {
		b := e == "true"
		q.Enabled = &b
	}
	items, total, err := h.svc.ListPipelines(r.Context(), q, page, size)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, httpx.Paged[pipelineDTO]{Items: toPipelineDTOs(items), Total: total, Page: page, Size: size})
}

// DeletePipeline DELETE /api/v1/pipelines/{id}
func (h *Handler) DeletePipeline(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if err := h.svc.DeletePipeline(r.Context(), id, uid); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// TriggerRun POST /api/v1/pipelines/{id}/runs
func (h *Handler) TriggerRun(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var req struct {
		TriggerRef       string `json:"trigger_ref"`
		TriggerCommitSHA string `json:"trigger_commit_sha"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	run, err := h.svc.TriggerRun(r.Context(), pipelineapp.TriggerRunInput{
		PipelineID: id, TriggerRef: req.TriggerRef, TriggerCommitSHA: req.TriggerCommitSHA, TriggerBy: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Created(w, toRunDTO(run))
}

// GetRun GET /api/v1/pipeline-runs/{id}
func (h *Handler) GetRun(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	run, err := h.svc.GetRun(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	stageRuns, _, _ := h.svc.ListStageRuns(r.Context(), id)
	httpx.OK(w, map[string]any{"run": toRunDTO(run), "stage_runs": toStageRunDTOs(stageRuns)})
}

// ListRuns GET /api/v1/pipeline-runs?pipeline_id=&workspace_id=&status=&page=&size=
func (h *Handler) ListRuns(w http.ResponseWriter, r *http.Request) {
	page, size, _ := httpx.Pagination(r)
	q := pipeline.RunQuery{
		PipelineID:  parseQueryID(r, "pipeline_id"),
		WorkspaceID: parseQueryID(r, "workspace_id"),
		Status:      pipeline.RunStatus(r.URL.Query().Get("status")),
	}
	items, total, err := h.svc.ListRuns(r.Context(), q, page, size)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, httpx.Paged[runDTO]{Items: toRunDTOs(items), Total: total, Page: page, Size: size})
}

// CancelRun POST /api/v1/pipeline-runs/{id}/cancel
func (h *Handler) CancelRun(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	run, err := h.svc.CancelRun(r.Context(), id, uid)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toRunDTO(run))
}

// --- promotions ---

// CreatePromotion POST /api/v1/promotions
func (h *Handler) CreatePromotion(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	var req struct {
		WorkspaceID           int64    `json:"workspace_id"`
		ApplicationID         int64    `json:"application_id"`
		SourceEnv             string   `json:"source_env"`
		TargetEnv             string   `json:"target_env"`
		ArtifactImageID       int64    `json:"artifact_image_id"`
		ArtifactConfigVersion int      `json:"artifact_config_version"`
		TargetGroupIDs        []int64  `json:"target_group_ids"`
		Strategy              string   `json:"strategy"`
		AutoPromoteOnVerify   bool     `json:"auto_promote_on_verify"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid body", err))
		return
	}
	p, err := h.svc.CreatePromotion(r.Context(), pipelineapp.CreatePromotionInput{
		WorkspaceID: req.WorkspaceID, ApplicationID: req.ApplicationID, SourceEnv: req.SourceEnv,
		TargetEnv: req.TargetEnv, ArtifactImageID: req.ArtifactImageID,
		ArtifactConfigVersion: req.ArtifactConfigVersion, TargetGroupIDs: req.TargetGroupIDs,
		Strategy: pipeline.PromotionStrategy(req.Strategy), AutoPromoteOnVerify: req.AutoPromoteOnVerify,
		StartedBy: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Created(w, toPromotionDTO(p))
}

// ListPromotions GET /api/v1/promotions?workspace_id=&application_id=&status=&page=&size=
func (h *Handler) ListPromotions(w http.ResponseWriter, r *http.Request) {
	page, size, _ := httpx.Pagination(r)
	q := pipeline.PromotionQuery{
		WorkspaceID:   parseQueryID(r, "workspace_id"),
		ApplicationID: parseQueryID(r, "application_id"),
		Status:        pipeline.PromotionStatus(r.URL.Query().Get("status")),
	}
	items, total, err := h.svc.ListPromotions(r.Context(), q, page, size)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, httpx.Paged[promotionDTO]{Items: toPromotionDTOs(items), Total: total, Page: page, Size: size})
}

// --- signatures ---

// RecordSignature POST /api/v1/artifacts/signatures
func (h *Handler) RecordSignature(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	var req struct {
		ImageID            int64  `json:"image_id"`
		SignatureType      string `json:"signature_type"`
		SignaturePayload   string `json:"signature_payload"`
		PublicKeyRef       string `json:"public_key_ref"`
		SignedBy           string `json:"signed_by"`
		SBOMStorageKey     string `json:"sbom_storage_key"`
		SBOMFormat         string `json:"sbom_format"`
		Provenance         map[string]any `json:"provenance"`
		VerificationStatus string `json:"verification_status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid body", err))
		return
	}
	sig, err := h.svc.RecordSignature(r.Context(), pipelineapp.RecordSignatureInput{
		ImageID: req.ImageID, SignatureType: pipeline.SignatureType(req.SignatureType),
		SignaturePayload: req.SignaturePayload, PublicKeyRef: req.PublicKeyRef, SignedBy: req.SignedBy,
		SBOMStorageKey: req.SBOMStorageKey, SBOMFormat: pipeline.SBOMFormat(req.SBOMFormat),
		Provenance: req.Provenance, VerificationStatus: pipeline.VerificationStatus(req.VerificationStatus),
		CreatedBy: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Created(w, toSignatureDTO(sig))
}

// GetSignature GET /api/v1/images/{id}/signature
func (h *Handler) GetSignature(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	sig, err := h.svc.GetSignature(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toSignatureDTO(sig))
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

func parseQueryID(r *http.Request, key string) int64 {
	v, _ := strconv.ParseInt(r.URL.Query().Get(key), 10, 64)
	return v
}
