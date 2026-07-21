// Package extapi 提供对外 API HTTP handlers（/api/v1/ext）。
package extapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/vortexops/vortexops/internal/application/extapiapp"
	"github.com/vortexops/vortexops/internal/domain/application"
	"github.com/vortexops/vortexops/internal/domain/extapi"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// Handler 对外 API handler。
type Handler struct {
	svc *extapiapp.Service
}

// NewHandler 创建 handler。
func NewHandler(svc *extapiapp.Service) *Handler {
	return &Handler{svc: svc}
}

// Deploy POST /workspaces/{wsUuid}/groups/{groupUuid}:deploy
func (h *Handler) Deploy(w http.ResponseWriter, r *http.Request) {
	token := TokenFromContext(r.Context())
	var req struct {
		ImageUUID     string `json:"imageUuid"`
		ConfigVersion int    `json:"configVersion"`
		Strategy      string `json:"strategy"`
		CanaryPercent int    `json:"canaryPercent"`
		ChangeSummary string `json:"changeSummary"`
		CallbackURL   string `json:"callbackUrl"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, r, apperr.Validation("invalid request body", err))
		return
	}
	wsUUID, groupUUID, err := parseWSGroup(r)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	imgUUID, err := uuid.Parse(req.ImageUUID)
	if err != nil {
		WriteError(w, r, apperr.Validation("invalid imageUuid", err))
		return
	}
	rel, err := h.svc.Deploy(r.Context(), extapiapp.DeployInput{
		WorkspaceUUID: wsUUID, GroupUUID: groupUUID, ImageUUID: imgUUID,
		ConfigVersion: req.ConfigVersion, Strategy: req.Strategy, CanaryPercent: req.CanaryPercent,
		ChangeSummary: req.ChangeSummary, CallbackURL: req.CallbackURL,
		ActorID: token.UserID, Token: token,
	})
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteOK(w, r, map[string]any{
		"releaseUuid": rel.UUID.String(), "status": rel.Status, "releaseNumber": rel.ReleaseNumber,
	})
}

// ScaleGroup POST /workspaces/{wsUuid}/groups/{groupUuid}:scale
func (h *Handler) ScaleGroup(w http.ResponseWriter, r *http.Request) {
	token := TokenFromContext(r.Context())
	var req struct {
		Replicas      int    `json:"replicas"`
		ChangeSummary string `json:"changeSummary"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, r, apperr.Validation("invalid request body", err))
		return
	}
	wsUUID, groupUUID, err := parseWSGroup(r)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	g, err := h.svc.ScaleGroup(r.Context(), extapiapp.ScaleGroupInput{
		WorkspaceUUID: wsUUID, GroupUUID: groupUUID, Replicas: req.Replicas,
		ActorID: token.UserID, Token: token,
	})
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteOK(w, r, map[string]any{"groupUuid": g.UUID.String(), "replicas": g.Replicas})
}

// Rollback POST /workspaces/{wsUuid}/groups/{groupUuid}:rollback
func (h *Handler) Rollback(w http.ResponseWriter, r *http.Request) {
	token := TokenFromContext(r.Context())
	wsUUID, groupUUID, err := parseWSGroup(r)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	rel, err := h.svc.Rollback(r.Context(), extapiapp.RollbackInput{
		WorkspaceUUID: wsUUID, GroupUUID: groupUUID, ActorID: token.UserID, Token: token,
	})
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteOK(w, r, map[string]any{
		"releaseUuid": rel.UUID.String(), "status": rel.Status, "releaseNumber": rel.ReleaseNumber,
	})
}

// GetCurrentRelease GET /workspaces/{wsUuid}/groups/{groupUuid}/releases/current
func (h *Handler) GetCurrentRelease(w http.ResponseWriter, r *http.Request) {
	token := TokenFromContext(r.Context())
	wsUUID, groupUUID, err := parseWSGroup(r)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	rel, err := h.svc.GetCurrentRelease(r.Context(), wsUUID, groupUUID, token)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteOK(w, r, map[string]any{
		"releaseUuid": rel.UUID.String(), "status": rel.Status, "releaseNumber": rel.ReleaseNumber,
		"imageId": rel.ImageID, "configVersion": rel.ConfigVersion,
	})
}

// TriggerBuild POST /workspaces/{wsUuid}/applications/{appUuid}:build
func (h *Handler) TriggerBuild(w http.ResponseWriter, r *http.Request) {
	token := TokenFromContext(r.Context())
	wsUUID, err := parseUUIDParam(r, "wsUuid")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	appUUID, err := parseUUIDParam(r, "appUuid")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	var req struct {
		GitSourceID int64  `json:"gitSourceId"`
		RefType     string `json:"refType"`
		RefValue    string `json:"refValue"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, r, apperr.Validation("invalid request body", err))
		return
	}
	b, err := h.svc.TriggerBuild(r.Context(), extapiapp.TriggerBuildInput{
		WorkspaceUUID: wsUUID, AppUUID: appUUID, GitSourceID: req.GitSourceID,
		RefType: req.RefType, RefValue: req.RefValue,
		IdempotencyKey: r.Header.Get("Idempotency-Key"), ActorID: token.UserID, Token: token,
	})
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteOK(w, r, map[string]any{"buildUuid": b.UUID.String(), "status": b.Status, "buildNumber": b.BuildNumber})
}

// GetBuild GET /workspaces/{wsUuid}/builds/{buildUuid}
func (h *Handler) GetBuild(w http.ResponseWriter, r *http.Request) {
	token := TokenFromContext(r.Context())
	wsUUID, err := parseUUIDParam(r, "wsUuid")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	buildUUID, err := parseUUIDParam(r, "buildUuid")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	b, err := h.svc.GetBuild(r.Context(), wsUUID, buildUUID, token)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteOK(w, r, map[string]any{
		"buildUuid": b.UUID.String(), "status": b.Status, "buildNumber": b.BuildNumber,
		"progress": b.ProgressPercent,
	})
}

// TriggerPipeline POST /workspaces/{wsUuid}/pipelines/{pipelineUuid}:trigger
func (h *Handler) TriggerPipeline(w http.ResponseWriter, r *http.Request) {
	token := TokenFromContext(r.Context())
	wsUUID, err := parseUUIDParam(r, "wsUuid")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	pipelineUUID, err := parseUUIDParam(r, "pipelineUuid")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	var req struct {
		TriggerRef string `json:"triggerRef"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	run, err := h.svc.TriggerPipeline(r.Context(), extapiapp.TriggerPipelineInput{
		WorkspaceUUID: wsUUID, PipelineUUID: pipelineUUID, TriggerRef: req.TriggerRef,
		ActorID: token.UserID, Token: token,
	})
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteOK(w, r, map[string]any{"runUuid": run.UUID.String(), "status": run.Status, "runNumber": run.RunNumber})
}

// GetPipelineRun GET /workspaces/{wsUuid}/pipeline-runs/{runUuid}
func (h *Handler) GetPipelineRun(w http.ResponseWriter, r *http.Request) {
	token := TokenFromContext(r.Context())
	wsUUID, err := parseUUIDParam(r, "wsUuid")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	runUUID, err := parseUUIDParam(r, "runUuid")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	run, err := h.svc.GetPipelineRun(r.Context(), wsUUID, runUUID, token)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteOK(w, r, map[string]any{
		"runUuid": run.UUID.String(), "status": run.Status, "runNumber": run.RunNumber,
	})
}

// DeployInference POST /workspaces/{wsUuid}/inference-services
func (h *Handler) DeployInference(w http.ResponseWriter, r *http.Request) {
	token := TokenFromContext(r.Context())
	wsUUID, err := parseUUIDParam(r, "wsUuid")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	var req struct {
		Name             string `json:"name"`
		ClusterID        int64  `json:"clusterId"`
		Namespace        string `json:"namespace"`
		ModelVersionUUID string `json:"modelVersionUuid"`
		Replicas         int    `json:"replicas"`
		GPUCount         int    `json:"gpuCount"`
		Framework        string `json:"framework"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, r, apperr.Validation("invalid request body", err))
		return
	}
	modelUUID, err := uuid.Parse(req.ModelVersionUUID)
	if err != nil {
		WriteError(w, r, apperr.Validation("invalid modelVersionUuid", err))
		return
	}
	svc, err := h.svc.DeployInference(r.Context(), extapiapp.DeployInferenceInput{
		WorkspaceUUID: wsUUID, Name: req.Name, ClusterID: req.ClusterID, Namespace: req.Namespace,
		ModelVersionUUID: modelUUID, Replicas: req.Replicas, GPUCount: req.GPUCount,
		Framework: req.Framework, ActorID: token.UserID, Token: token,
	})
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteCreated(w, r, map[string]any{
		"serviceUuid": svc.UUID.String(), "status": svc.CurrentStatus, "replicas": svc.Replicas,
	})
}

// ScaleInference POST /workspaces/{wsUuid}/inference-services/{svcUuid}:scale
func (h *Handler) ScaleInference(w http.ResponseWriter, r *http.Request) {
	token := TokenFromContext(r.Context())
	wsUUID, err := parseUUIDParam(r, "wsUuid")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	svcUUID, err := parseUUIDParam(r, "svcUuid")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	var req struct {
		Replicas int `json:"replicas"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, r, apperr.Validation("invalid request body", err))
		return
	}
	svc, err := h.svc.ScaleInference(r.Context(), extapiapp.ScaleInferenceInput{
		WorkspaceUUID: wsUUID, ServiceUUID: svcUUID, Replicas: req.Replicas,
		ActorID: token.UserID, Token: token,
	})
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteOK(w, r, map[string]any{"serviceUuid": svc.UUID.String(), "replicas": svc.Replicas, "status": svc.CurrentStatus})
}

// GetInferenceService GET /workspaces/{wsUuid}/inference-services/{svcUuid}
func (h *Handler) GetInferenceService(w http.ResponseWriter, r *http.Request) {
	token := TokenFromContext(r.Context())
	wsUUID, err := parseUUIDParam(r, "wsUuid")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	svcUUID, err := parseUUIDParam(r, "svcUuid")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	svc, err := h.svc.GetInferenceService(r.Context(), wsUUID, svcUUID, token)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteOK(w, r, map[string]any{
		"serviceUuid": svc.UUID.String(), "status": svc.CurrentStatus, "readiness": svc.Readiness,
		"replicas": svc.Replicas, "endpoint": svc.ExternalEndpoint,
	})
}

// SelfCreateWorkspace POST /workspaces
func (h *Handler) SelfCreateWorkspace(w http.ResponseWriter, r *http.Request) {
	token := TokenFromContext(r.Context())
	var req struct {
		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, r, apperr.Validation("invalid request body", err))
		return
	}
	ws, err := h.svc.SelfCreateWorkspace(r.Context(), extapiapp.SelfCreateWorkspaceInput{
		Name: req.Name, DisplayName: req.DisplayName, Description: req.Description, ActorID: token.UserID,
	})
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteCreated(w, r, map[string]any{"workspaceUuid": ws.UUID.String(), "name": ws.Name, "status": ws.Status})
}

// GetGroupStatus GET /workspaces/{wsUuid}/groups/{groupUuid}
func (h *Handler) GetGroupStatus(w http.ResponseWriter, r *http.Request) {
	token := TokenFromContext(r.Context())
	wsUUID, groupUUID, err := parseWSGroup(r)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	data, err := h.svc.GetGroupStatus(r.Context(), wsUUID, groupUUID, token)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteOK(w, r, data)
}

// ListGroupPods GET /workspaces/{wsUuid}/groups/{groupUuid}/pods
func (h *Handler) ListGroupPods(w http.ResponseWriter, r *http.Request) {
	token := TokenFromContext(r.Context())
	wsUUID, groupUUID, err := parseWSGroup(r)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	pods, err := h.svc.ListGroupPods(r.Context(), wsUUID, groupUUID, token)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteOK(w, r, map[string]any{"items": pods})
}

// DeployMiddleware POST /workspaces/{wsUuid}/middleware-deployments
func (h *Handler) DeployMiddleware(w http.ResponseWriter, r *http.Request) {
	token := TokenFromContext(r.Context())
	wsUUID, err := parseUUIDParam(r, "wsUuid")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	var req struct {
		ApplicationUUID string                    `json:"applicationUuid"`
		Name            string                    `json:"name"`
		DisplayName     string                    `json:"displayName"`
		Description     string                    `json:"description"`
		GroupName       string                    `json:"groupName"`
		ImageRef        string                    `json:"imageRef"`
		RegistryUUID    string                    `json:"registryUuid"`
		ClusterID       int64                     `json:"clusterId"`
		ClusterUUID     string                    `json:"clusterUuid"`
		Namespace       string                    `json:"namespace"`
		Environment     string                    `json:"environment"`
		Replicas        int                       `json:"replicas"`
	Resources       application.Resources     `json:"resources"`
	MeshEnabled     bool                      `json:"meshEnabled"`
	WorkloadType    string                    `json:"workloadType"`
		Env             []map[string]any          `json:"env"`
		Files           []map[string]any          `json:"files"`
		Command         []string                  `json:"command"`
		Args            []string                  `json:"args"`
		ManagingTeam    string                    `json:"managingTeam"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, r, apperr.Validation("invalid request body", err))
		return
	}
	regUUID, err := uuid.Parse(req.RegistryUUID)
	if err != nil {
		WriteError(w, r, apperr.Validation("invalid registryUuid", err))
		return
	}
	in := extapiapp.DeployMiddlewareInput{
		WorkspaceUUID: wsUUID, Name: req.Name, DisplayName: req.DisplayName,
		Description: req.Description, GroupName: req.GroupName, ImageRef: req.ImageRef,
		RegistryUUID: regUUID, ClusterID: req.ClusterID, Namespace: req.Namespace,
		Environment: req.Environment, Replicas: req.Replicas, Resources: req.Resources,
		MeshEnabled: req.MeshEnabled, WorkloadType: req.WorkloadType, Env: req.Env, Files: req.Files,
		Command: req.Command, Args: req.Args, ManagingTeam: req.ManagingTeam,
		ActorID: token.UserID, Token: token,
	}
	if req.ApplicationUUID != "" {
		appUUID, err := uuid.Parse(req.ApplicationUUID)
		if err != nil {
			WriteError(w, r, apperr.Validation("invalid applicationUuid", err))
			return
		}
		in.ApplicationUUID = &appUUID
	}
	if req.ClusterUUID != "" {
		clusterUUID, err := uuid.Parse(req.ClusterUUID)
		if err != nil {
			WriteError(w, r, apperr.Validation("invalid clusterUuid", err))
			return
		}
		in.ClusterUUID = clusterUUID
	}
	result, err := h.svc.DeployMiddlewareAsApplication(r.Context(), in)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteCreated(w, r, map[string]any{
		"applicationUuid": result.ApplicationUUID.String(),
		"groupUuid":       result.GroupUUID.String(),
		"imageUuid":       result.ImageUUID.String(),
		"releaseId":       result.ReleaseID,
	})
}

// UpdateMiddleware PATCH /workspaces/{wsUuid}/middleware-deployments/{appUuid}
func (h *Handler) UpdateMiddleware(w http.ResponseWriter, r *http.Request) {
	token := TokenFromContext(r.Context())
	wsUUID, err := parseUUIDParam(r, "wsUuid")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	appUUID, err := parseUUIDParam(r, "appUuid")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	var req struct {
	Replicas     *int                  `json:"replicas"`
	Resources    *application.Resources `json:"resources"`
	MeshEnabled  *bool                  `json:"meshEnabled"`
	Env          []map[string]any       `json:"env"`
		Files        []map[string]any       `json:"files"`
		Command      []string               `json:"command"`
		Args         []string               `json:"args"`
		ImageRef     string                 `json:"imageRef"`
		RegistryUUID string                 `json:"registryUuid"`
		Version      int                    `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, r, apperr.Validation("invalid request body", err))
		return
	}
	in := extapiapp.UpdateMiddlewareInput{
		WorkspaceUUID: wsUUID, AppUUID: appUUID, Replicas: req.Replicas,
		Resources: req.Resources, MeshEnabled: req.MeshEnabled, Env: req.Env, Files: req.Files,
		Command: req.Command, Args: req.Args, ImageRef: req.ImageRef, Version: req.Version,
		ActorID: token.UserID, Token: token,
	}
	if req.RegistryUUID != "" {
		regUUID, err := uuid.Parse(req.RegistryUUID)
		if err != nil {
			WriteError(w, r, apperr.Validation("invalid registryUuid", err))
			return
		}
		in.RegistryUUID = regUUID
	}
	g, err := h.svc.UpdateMiddlewareApplication(r.Context(), in)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteOK(w, r, map[string]any{"groupUuid": g.UUID.String(), "replicas": g.Replicas, "version": g.Version})
}

// ScaleMiddleware POST /workspaces/{wsUuid}/middleware-deployments/{appUuid}:scale
func (h *Handler) ScaleMiddleware(w http.ResponseWriter, r *http.Request) {
	token := TokenFromContext(r.Context())
	wsUUID, err := parseUUIDParam(r, "wsUuid")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	appUUID, err := parseUUIDParam(r, "appUuid")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	var req struct {
		Replicas int `json:"replicas"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, r, apperr.Validation("invalid request body", err))
		return
	}
	g, err := h.svc.ScaleMiddleware(r.Context(), extapiapp.ScaleMiddlewareInput{
		WorkspaceUUID: wsUUID, AppUUID: appUUID, Replicas: req.Replicas,
		ActorID: token.UserID, Token: token,
	})
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteOK(w, r, map[string]any{"groupUuid": g.UUID.String(), "replicas": g.Replicas})
}

// DeleteMiddleware DELETE /workspaces/{wsUuid}/middleware-deployments/{appUuid}
func (h *Handler) DeleteMiddleware(w http.ResponseWriter, r *http.Request) {
	token := TokenFromContext(r.Context())
	wsUUID, err := parseUUIDParam(r, "wsUuid")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	appUUID, err := parseUUIDParam(r, "appUuid")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	if err := h.svc.DeleteMiddleware(r.Context(), wsUUID, appUUID, token, token.UserID); err != nil {
		WriteError(w, r, err)
		return
	}
	WriteOK(w, r, map[string]any{"deleted": true})
}

// StopMiddleware POST /workspaces/{wsUuid}/middleware-deployments/{appUuid}:stop
func (h *Handler) StopMiddleware(w http.ResponseWriter, r *http.Request) {
	token := TokenFromContext(r.Context())
	wsUUID, err := parseUUIDParam(r, "wsUuid")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	appUUID, err := parseUUIDParam(r, "appUuid")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	g, err := h.svc.StopMiddleware(r.Context(), wsUUID, appUUID, token, token.UserID)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteOK(w, r, map[string]any{"groupUuid": g.UUID.String(), "replicas": g.Replicas})
}

// StartMiddleware POST /workspaces/{wsUuid}/middleware-deployments/{appUuid}:start
func (h *Handler) StartMiddleware(w http.ResponseWriter, r *http.Request) {
	token := TokenFromContext(r.Context())
	wsUUID, err := parseUUIDParam(r, "wsUuid")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	appUUID, err := parseUUIDParam(r, "appUuid")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	g, err := h.svc.StartMiddleware(r.Context(), wsUUID, appUUID, token, token.UserID)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteOK(w, r, map[string]any{"groupUuid": g.UUID.String(), "replicas": g.Replicas})
}

// ListMiddlewareMembers GET /workspaces/{wsUuid}/middleware-deployments/{appUuid}/members
func (h *Handler) ListMiddlewareMembers(w http.ResponseWriter, r *http.Request) {
	token := TokenFromContext(r.Context())
	wsUUID, err := parseUUIDParam(r, "wsUuid")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	appUUID, err := parseUUIDParam(r, "appUuid")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	size, _ := strconv.Atoi(r.URL.Query().Get("size"))
	items, total, err := h.svc.ListMiddlewareMembers(r.Context(), wsUUID, appUUID, token, page, size)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteOK(w, r, map[string]any{"items": items, "total": total})
}

// AddMiddlewareMember POST /workspaces/{wsUuid}/middleware-deployments/{appUuid}/members
func (h *Handler) AddMiddlewareMember(w http.ResponseWriter, r *http.Request) {
	token := TokenFromContext(r.Context())
	wsUUID, err := parseUUIDParam(r, "wsUuid")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	appUUID, err := parseUUIDParam(r, "appUuid")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	var req struct {
		UserID int64 `json:"userId"`
		RoleID int64 `json:"roleId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, r, apperr.Validation("invalid request body", err))
		return
	}
	m, err := h.svc.AddMiddlewareMember(r.Context(), extapiapp.AddMiddlewareMemberInput{
		WorkspaceUUID: wsUUID, AppUUID: appUUID, UserID: req.UserID, RoleID: req.RoleID,
		ActorID: token.UserID, Token: token,
	})
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteCreated(w, r, map[string]any{"id": m.ID, "userId": m.UserID, "roleId": m.RoleID})
}

// UpdateMiddlewareMemberRole PUT /workspaces/{wsUuid}/middleware-deployments/{appUuid}/members/{userId}
func (h *Handler) UpdateMiddlewareMemberRole(w http.ResponseWriter, r *http.Request) {
	token := TokenFromContext(r.Context())
	wsUUID, err := parseUUIDParam(r, "wsUuid")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	appUUID, err := parseUUIDParam(r, "appUuid")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	userID, err := strconv.ParseInt(chi.URLParam(r, "userId"), 10, 64)
	if err != nil {
		WriteError(w, r, apperr.Validation("invalid userId", err))
		return
	}
	var req struct {
		RoleID int64 `json:"roleId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, r, apperr.Validation("invalid request body", err))
		return
	}
	if err := h.svc.UpdateMiddlewareMemberRole(r.Context(), wsUUID, appUUID, userID, req.RoleID, token, token.UserID); err != nil {
		WriteError(w, r, err)
		return
	}
	WriteOK(w, r, map[string]any{"updated": true})
}

// RemoveMiddlewareMember DELETE /workspaces/{wsUuid}/middleware-deployments/{appUuid}/members/{userId}
func (h *Handler) RemoveMiddlewareMember(w http.ResponseWriter, r *http.Request) {
	token := TokenFromContext(r.Context())
	wsUUID, err := parseUUIDParam(r, "wsUuid")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	appUUID, err := parseUUIDParam(r, "appUuid")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	userID, err := strconv.ParseInt(chi.URLParam(r, "userId"), 10, 64)
	if err != nil {
		WriteError(w, r, apperr.Validation("invalid userId", err))
		return
	}
	if err := h.svc.RemoveMiddlewareMember(r.Context(), wsUUID, appUUID, userID, token, token.UserID); err != nil {
		WriteError(w, r, err)
		return
	}
	WriteOK(w, r, map[string]any{"removed": true})
}

// GetMiddlewareStatus GET /workspaces/{wsUuid}/middleware-deployments/{appUuid}/status
func (h *Handler) GetMiddlewareStatus(w http.ResponseWriter, r *http.Request) {
	token := TokenFromContext(r.Context())
	wsUUID, err := parseUUIDParam(r, "wsUuid")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	appUUID, err := parseUUIDParam(r, "appUuid")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	data, err := h.svc.GetMiddlewareStatus(r.Context(), wsUUID, appUUID, token)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteOK(w, r, data)
}

// ListMiddlewarePods GET /workspaces/{wsUuid}/middleware-deployments/{appUuid}/pods
func (h *Handler) ListMiddlewarePods(w http.ResponseWriter, r *http.Request) {
	token := TokenFromContext(r.Context())
	wsUUID, err := parseUUIDParam(r, "wsUuid")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	appUUID, err := parseUUIDParam(r, "appUuid")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	pods, err := h.svc.ListMiddlewarePods(r.Context(), wsUUID, appUUID, token)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteOK(w, r, map[string]any{"items": pods})
}

// RollbackMiddleware POST /workspaces/{wsUuid}/middleware-deployments/{appUuid}:rollback
func (h *Handler) RollbackMiddleware(w http.ResponseWriter, r *http.Request) {
	token := TokenFromContext(r.Context())
	wsUUID, err := parseUUIDParam(r, "wsUuid")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	appUUID, err := parseUUIDParam(r, "appUuid")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	rel, err := h.svc.RollbackMiddleware(r.Context(), wsUUID, appUUID, token, token.UserID)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteOK(w, r, map[string]any{
		"releaseUuid": rel.UUID.String(), "status": rel.Status, "releaseNumber": rel.ReleaseNumber,
	})
}

// GetCurrentMiddlewareRelease GET /workspaces/{wsUuid}/middleware-deployments/{appUuid}/releases/current
func (h *Handler) GetCurrentMiddlewareRelease(w http.ResponseWriter, r *http.Request) {
	token := TokenFromContext(r.Context())
	wsUUID, err := parseUUIDParam(r, "wsUuid")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	appUUID, err := parseUUIDParam(r, "appUuid")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	rel, err := h.svc.GetCurrentMiddlewareRelease(r.Context(), wsUUID, appUUID, token)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteOK(w, r, map[string]any{
		"releaseUuid": rel.UUID.String(), "status": rel.Status, "releaseNumber": rel.ReleaseNumber,
	})
}

// ListMiddlewareReleases GET /workspaces/{wsUuid}/middleware-deployments/{appUuid}/releases
func (h *Handler) ListMiddlewareReleases(w http.ResponseWriter, r *http.Request) {
	token := TokenFromContext(r.Context())
	wsUUID, err := parseUUIDParam(r, "wsUuid")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	appUUID, err := parseUUIDParam(r, "appUuid")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	size, _ := strconv.Atoi(r.URL.Query().Get("size"))
	status := r.URL.Query().Get("status")
	items, total, err := h.svc.ListMiddlewareReleases(r.Context(), wsUUID, appUUID, status, page, size, token)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteOK(w, r, map[string]any{"items": items, "total": total})
}

// ListMiddlewareImages GET /workspaces/{wsUuid}/middleware-deployments/{appUuid}/images
func (h *Handler) ListMiddlewareImages(w http.ResponseWriter, r *http.Request) {
	token := TokenFromContext(r.Context())
	wsUUID, err := parseUUIDParam(r, "wsUuid")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	appUUID, err := parseUUIDParam(r, "appUuid")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	size, _ := strconv.Atoi(r.URL.Query().Get("size"))
	items, total, err := h.svc.ListMiddlewareImages(r.Context(), wsUUID, appUUID, page, size, token)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteOK(w, r, map[string]any{"items": items, "total": total})
}

// RetireMiddlewareImage DELETE /workspaces/{wsUuid}/middleware-deployments/{appUuid}/images/{imageId}
func (h *Handler) RetireMiddlewareImage(w http.ResponseWriter, r *http.Request) {
	token := TokenFromContext(r.Context())
	wsUUID, err := parseUUIDParam(r, "wsUuid")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	appUUID, err := parseUUIDParam(r, "appUuid")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	imageID, err := strconv.ParseInt(chi.URLParam(r, "imageId"), 10, 64)
	if err != nil {
		WriteError(w, r, apperr.Validation("invalid imageId", err))
		return
	}
	if err := h.svc.RetireMiddlewareImage(r.Context(), wsUUID, appUUID, imageID, token); err != nil {
		WriteError(w, r, err)
		return
	}
	WriteOK(w, r, map[string]any{"retired": true})
}

// GetMiddlewarePodLogs GET /workspaces/{wsUuid}/middleware-deployments/{appUuid}/pods/{pod}/logs?container=&tail=
func (h *Handler) GetMiddlewarePodLogs(w http.ResponseWriter, r *http.Request) {
	token := TokenFromContext(r.Context())
	wsUUID, err := parseUUIDParam(r, "wsUuid")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	appUUID, err := parseUUIDParam(r, "appUuid")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	pod := chi.URLParam(r, "pod")
	if pod == "" {
		WriteError(w, r, apperr.Validation("pod is required", nil))
		return
	}
	container := r.URL.Query().Get("container")
	tail, _ := strconv.ParseInt(r.URL.Query().Get("tail"), 10, 64)
	if tail <= 0 {
		tail = 1000
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if err := h.svc.StreamMiddlewarePodLogs(r.Context(), wsUUID, appUUID, pod, container, tail, token, w); err != nil {
		WriteError(w, r, err)
		return
	}
}

func parseWSGroup(r *http.Request) (uuid.UUID, uuid.UUID, error) {
	wsUUID, err := parseUUIDParam(r, "wsUuid")
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	groupUUID, err := parseUUIDParam(r, "groupUuid")
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	return wsUUID, groupUUID, nil
}

func parseUUIDParam(r *http.Request, key string) (uuid.UUID, error) {
	raw := chi.URLParam(r, key)
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, apperr.Validation("invalid "+key, err)
	}
	return id, nil
}

// Scope constants re-export for route wiring.
const (
	ScopeDeploy   = extapi.ScopeDeploy
	ScopeScale    = extapi.ScopeScale
	ScopeRollback = extapi.ScopeRollback
	ScopeBuild    = extapi.ScopeBuild
	ScopePipeline = extapi.ScopePipeline
	ScopeInference = extapi.ScopeInference
	ScopeMiddleware = extapi.ScopeMiddleware
	ScopeStatus   = extapi.ScopeStatus
)
