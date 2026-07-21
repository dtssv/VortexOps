// Package confighttp 是配置管理领域的 HTTP handlers。
package confighttp

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	configdomain "github.com/vortexops/vortexops/internal/domain/config"
	"github.com/vortexops/vortexops/internal/application/configapp"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpauth"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpx"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// Handler 处理配置相关路由。
type Handler struct {
	svc *configapp.Service
}

// NewHandler 创建配置 handler。
func NewHandler(svc *configapp.Service) *Handler {
	return &Handler{svc: svc}
}

// CreateConfig POST /api/v1/configs
func (h *Handler) CreateConfig(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	var req struct {
		Scope           string `json:"scope"`
		ScopeID         int64  `json:"scope_id"`
		GroupID         int64  `json:"group_id"`
		Name            string `json:"name"`
		ConfigType      string `json:"config_type"`
		Description     string `json:"description"`
		RenderedContent string `json:"rendered_content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	c, err := h.svc.CreateConfig(r.Context(), configapp.CreateConfigInput{
		Scope: configdomain.Scope(req.Scope), ScopeID: req.ScopeID, GroupID: req.GroupID,
		Name: req.Name, ConfigType: configdomain.ConfigType(req.ConfigType),
		Description: req.Description, RenderedContent: req.RenderedContent, CreatedBy: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Created(w, toConfigDTO(c))
}

// GetConfig GET /api/v1/configs/{id}
func (h *Handler) GetConfig(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	c, err := h.svc.GetConfig(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toConfigDTO(c))
}

// ListConfigs GET /api/v1/configs?scope=&scope_id=&group_id=&name=&page=&size=
func (h *Handler) ListConfigs(w http.ResponseWriter, r *http.Request) {
	page, size, _ := httpx.Pagination(r)
	scopeID, _ := strconv.ParseInt(r.URL.Query().Get("scope_id"), 10, 64)
	groupID, _ := strconv.ParseInt(r.URL.Query().Get("group_id"), 10, 64)
	q := configdomain.ConfigQuery{
		Scope:   configdomain.Scope(r.URL.Query().Get("scope")),
		ScopeID: scopeID, GroupID: groupID, Name: r.URL.Query().Get("name"),
		Offset: (page - 1) * size, Limit: size,
	}
	items, total, err := h.svc.ListConfigs(r.Context(), q)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, httpx.Paged[configDTO]{
		Items: toConfigDTOs(items), Total: total, Page: page, Size: size,
	})
}

// DiffConfigs GET /api/v1/configs/diff?scope=&scope_id=&name=&version_a=&version_b=
func (h *Handler) DiffConfigs(w http.ResponseWriter, r *http.Request) {
	scopeID, _ := strconv.ParseInt(r.URL.Query().Get("scope_id"), 10, 64)
	versionA, _ := strconv.Atoi(r.URL.Query().Get("version_a"))
	versionB, _ := strconv.Atoi(r.URL.Query().Get("version_b"))
	diff, err := h.svc.DiffConfigs(r.Context(),
		configdomain.Scope(r.URL.Query().Get("scope")), scopeID,
		r.URL.Query().Get("name"), versionA, versionB)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, map[string]string{"diff": diff})
}

// DiffCrossGroup GET /api/v1/configs/diff-cross-group?scope=&scope_id=&name=&group_a=&group_b=
func (h *Handler) DiffCrossGroup(w http.ResponseWriter, r *http.Request) {
	scopeID, _ := strconv.ParseInt(r.URL.Query().Get("scope_id"), 10, 64)
	groupA, _ := strconv.ParseInt(r.URL.Query().Get("group_a"), 10, 64)
	groupB, _ := strconv.ParseInt(r.URL.Query().Get("group_b"), 10, 64)
	diff, err := h.svc.DiffCrossGroup(r.Context(),
		configdomain.Scope(r.URL.Query().Get("scope")), scopeID,
		r.URL.Query().Get("name"), groupA, groupB)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, map[string]string{"diff": diff})
}

// ArchiveConfig POST /api/v1/configs/{id}/archive
func (h *Handler) ArchiveConfig(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if err := h.svc.ArchiveConfig(r.Context(), id); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// --- ConfigSet ---

// CreateConfigSet POST /api/v1/workspaces/{wsId}/config-sets  (兼容)
// 或 POST /api/v1/applications/{appId}/config-sets  (新模型)
func (h *Handler) CreateConfigSet(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	wsID, _ := parseID(w, chi.URLParam(r, "wsId"))
	appID, _ := parseID(w, chi.URLParam(r, "appId"))
	var req struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Content     map[string]any `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	cs, err := h.svc.CreateConfigSet(r.Context(), configapp.CreateConfigSetInput{
		WorkspaceID: wsID, ApplicationID: appID, Name: req.Name, Description: req.Description, Content: req.Content, CreatedBy: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Created(w, toConfigSetDTO(cs))
}

// ListConfigSets GET /api/v1/workspaces/{wsId}/config-sets?page=&size=  (兼容)
func (h *Handler) ListConfigSets(w http.ResponseWriter, r *http.Request) {
	wsID, ok := parseID(w, chi.URLParam(r, "wsId"))
	if !ok {
		return
	}
	page, size, _ := httpx.Pagination(r)
	items, total, err := h.svc.ListConfigSets(r.Context(), wsID, page, size)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, httpx.Paged[configSetDTO]{
		Items: toConfigSetDTOs(items), Total: total, Page: page, Size: size,
	})
}

// ListConfigSetsByApplication GET /api/v1/applications/{appId}/config-sets
func (h *Handler) ListConfigSetsByApplication(w http.ResponseWriter, r *http.Request) {
	appID, ok := parseID(w, chi.URLParam(r, "appId"))
	if !ok {
		return
	}
	items, err := h.svc.ListConfigSetsByApplication(r.Context(), appID)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toConfigSetDTOs(items))
}

// CreateConfigSetByApplication POST /api/v1/applications/{appId}/config-sets
func (h *Handler) CreateConfigSetByApplication(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	appID, ok := parseID(w, chi.URLParam(r, "appId"))
	if !ok {
		return
	}
	var req struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Content     map[string]any `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	cs, err := h.svc.CreateConfigSet(r.Context(), configapp.CreateConfigSetInput{
		ApplicationID: appID, Name: req.Name, Description: req.Description, Content: req.Content, CreatedBy: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Created(w, toConfigSetDTO(cs))
}

// GetConfigSet GET /api/v1/config-sets/{id}
func (h *Handler) GetConfigSet(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	cs, err := h.svc.GetConfigSet(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toConfigSetDTO(cs))
}

// UpdateConfigSet PUT /api/v1/config-sets/{id}
func (h *Handler) UpdateConfigSet(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var req struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Content     map[string]any `json:"content"`
		Version     int            `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	cs, err := h.svc.UpdateConfigSet(r.Context(), configapp.UpdateConfigSetInput{
		ID: id, Name: req.Name, Description: req.Description, Content: req.Content,
		Version: req.Version, UpdatedBy: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toConfigSetDTO(cs))
}

// DeleteConfigSet DELETE /api/v1/config-sets/{id}
func (h *Handler) DeleteConfigSet(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if err := h.svc.DeleteConfigSet(r.Context(), id, uid); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// --- 绑定 ---

// CreateBinding POST /api/v1/groups/{groupId}/config-bindings
func (h *Handler) CreateBinding(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	groupID, ok := parseID(w, chi.URLParam(r, "groupId"))
	if !ok {
		return
	}
	var req struct {
		ConfigID      int64  `json:"config_id"`
		ConfigSetID   int64  `json:"config_set_id"`
		Priority      int    `json:"priority"`
		PinnedVersion *int   `json:"pinned_version"`
		MountPath     string `json:"mount_path"`
		SubPath       string `json:"sub_path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	b, err := h.svc.CreateBinding(r.Context(), configapp.CreateBindingInput{
		GroupID: groupID, ConfigID: req.ConfigID, ConfigSetID: req.ConfigSetID,
		Priority: req.Priority, PinnedVersion: req.PinnedVersion,
		MountPath: req.MountPath, SubPath: req.SubPath, CreatedBy: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Created(w, toBindingDTO(b))
}

// ListBindings GET /api/v1/groups/{groupId}/config-bindings
func (h *Handler) ListBindings(w http.ResponseWriter, r *http.Request) {
	groupID, ok := parseID(w, chi.URLParam(r, "groupId"))
	if !ok {
		return
	}
	items, err := h.svc.ListBindings(r.Context(), groupID)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toBindingDTOs(items))
}

// DeleteBinding DELETE /api/v1/config-bindings/{id}
func (h *Handler) DeleteBinding(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if err := h.svc.DeleteBinding(r.Context(), id, uid); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// --- 分组本地配置 ---

// GetLocalConfig GET /api/v1/groups/{groupId}/local-config
func (h *Handler) GetLocalConfig(w http.ResponseWriter, r *http.Request) {
	groupID, ok := parseID(w, chi.URLParam(r, "groupId"))
	if !ok {
		return
	}
	c, err := h.svc.GetLocalConfig(r.Context(), groupID)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toGroupLocalConfigDTO(c))
}

// UpsertLocalConfig PUT /api/v1/groups/{groupId}/local-config
// 创建或更新分组本地配置。分组已绑定配置集时返回 422（互斥）。
func (h *Handler) UpsertLocalConfig(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	groupID, ok := parseID(w, chi.URLParam(r, "groupId"))
	if !ok {
		return
	}
	var req struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Content     map[string]any `json:"content"`
		Version     int            `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	c, err := h.svc.UpsertLocalConfig(r.Context(), configapp.UpsertLocalConfigInput{
		GroupID: groupID, Name: req.Name, Description: req.Description,
		Content: req.Content, Version: req.Version, UpdatedBy: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toGroupLocalConfigDTO(c))
}

// DeleteLocalConfig DELETE /api/v1/groups/{groupId}/local-config
func (h *Handler) DeleteLocalConfig(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	groupID, ok := parseID(w, chi.URLParam(r, "groupId"))
	if !ok {
		return
	}
	if err := h.svc.DeleteLocalConfig(r.Context(), groupID, uid); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// ListConfigSetSnapshots GET /api/v1/config-sets/{id}/snapshots
func (h *Handler) ListConfigSetSnapshots(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	items, err := h.svc.ListSnapshots(r.Context(), configdomain.SnapshotConfigSet, id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toSnapshotDTOs(items))
}

// ListLocalConfigSnapshots GET /api/v1/groups/{groupId}/local-config/snapshots
func (h *Handler) ListLocalConfigSnapshots(w http.ResponseWriter, r *http.Request) {
	groupID, ok := parseID(w, chi.URLParam(r, "groupId"))
	if !ok {
		return
	}
	items, err := h.svc.ListSnapshots(r.Context(), configdomain.SnapshotGroupLocal, groupID)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toSnapshotDTOs(items))
}

// ListGroupBindSnapshots GET /api/v1/groups/{groupId}/config-bind-snapshots
func (h *Handler) ListGroupBindSnapshots(w http.ResponseWriter, r *http.Request) {
	groupID, ok := parseID(w, chi.URLParam(r, "groupId"))
	if !ok {
		return
	}
	items, err := h.svc.ListGroupBindSnapshots(r.Context(), groupID)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toSnapshotDTOs(items))
}

// DiffConfigFile GET /api/v1/config-snapshots/{id}/diff?file_path=&target_type=&target_id=
func (h *Handler) DiffConfigFile(w http.ResponseWriter, r *http.Request) {
	snapshotID, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	filePath := r.URL.Query().Get("file_path")
	targetType := configdomain.SnapshotTargetType(r.URL.Query().Get("target_type"))
	targetID, _ := strconv.ParseInt(r.URL.Query().Get("target_id"), 10, 64)
	if targetType == "" || targetID == 0 {
		httpx.WriteError(w, apperr.Validation("target_type and target_id are required", nil))
		return
	}
	current, err := h.svc.GetCurrentContentForDiff(r.Context(), targetType, targetID)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	result, err := h.svc.DiffConfigFile(r.Context(), snapshotID, current, filePath)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, result)
}

// ListGroupConfigFiles GET /api/v1/groups/{groupId}/config/files
func (h *Handler) ListGroupConfigFiles(w http.ResponseWriter, r *http.Request) {
	groupID, ok := parseID(w, chi.URLParam(r, "groupId"))
	if !ok {
		return
	}
	paths, err := h.svc.ListGroupConfigFiles(r.Context(), groupID)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, map[string]any{"files": paths})
}

// CloneLocalConfigFromGroup POST /api/v1/groups/{groupId}/local-config/clone-from
func (h *Handler) CloneLocalConfigFromGroup(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	groupID, ok := parseID(w, chi.URLParam(r, "groupId"))
	if !ok {
		return
	}
	var req struct {
		SourceGroupID  int64    `json:"source_group_id"`
		FilePaths      []string `json:"file_paths"`
		IncludeEnv     bool     `json:"include_env"`
		IncludeCommand bool     `json:"include_command"`
		IncludeArgs    bool     `json:"include_args"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	c, err := h.svc.CloneFromGroup(r.Context(), configapp.CloneFromGroupInput{
		TargetGroupID: groupID, SourceGroupID: req.SourceGroupID, FilePaths: req.FilePaths,
		IncludeEnv: req.IncludeEnv, IncludeCommand: req.IncludeCommand, IncludeArgs: req.IncludeArgs,
		UpdatedBy: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toGroupLocalConfigDTO(c))
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
