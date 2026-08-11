// Package applicationhttp 是应用与分组领域的 HTTP handlers。
package applicationhttp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/vortexops/vortexops/internal/application/applicationapp"
	"github.com/vortexops/vortexops/internal/application/clusterapp"
	"github.com/vortexops/vortexops/internal/application/opsapp"
	"github.com/vortexops/vortexops/internal/domain/application"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpauth"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpx"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// Handler 处理 /api/v1/workspaces/{wsId}/applications 与 /api/v1/applications/{appId}/groups 路由。
type Handler struct {
	svc        *applicationapp.Service
	clusterSvc *clusterapp.Service
	opsSvc     *opsapp.Service
}

// NewHandler 创建应用 handler。clusterSvc 用于 group 的 K8s 运维操作（pods/events/yaml/logs）。
// opsSvc 用于 Pod 文件浏览器/网络命令（可为 nil：相关端点返回 503）。
func NewHandler(svc *applicationapp.Service, clusterSvc *clusterapp.Service, opsSvc *opsapp.Service) *Handler {
	return &Handler{svc: svc, clusterSvc: clusterSvc, opsSvc: opsSvc}
}

// --- 应用 ---

// CreateApplication POST /api/v1/workspaces/{wsId}/applications
func (h *Handler) CreateApplication(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	wsID, ok := parseID(w, chi.URLParam(r, "wsId"))
	if !ok {
		return
	}
	var req struct {
		Name              string            `json:"name"`
		Code              string            `json:"code"`
		DisplayName       string            `json:"display_name"`
		Description       string            `json:"description"`
		Icon              string            `json:"icon"`
		DefaultRegistryID int64             `json:"default_registry_id"`
		Labels            map[string]string `json:"labels"`
		Metadata          map[string]any    `json:"metadata"`
		AppType           string            `json:"app_type"`
		WorkloadType      string            `json:"workload_type"`
		GitURL            string            `json:"git_url"`
		DefaultBranch     string            `json:"default_branch"`
		Language          string            `json:"language"`
		Probe             *application.ProbeConfig `json:"probe"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	if req.Probe != nil {
		if err := req.Probe.Validate(); err != nil {
			httpx.WriteError(w, apperr.Validation(err.Error(), nil))
			return
		}
	}
	a, err := h.svc.Create(r.Context(), applicationapp.CreateInput{
		WorkspaceID: wsID, Name: req.Name, Code: req.Code, DisplayName: req.DisplayName, Description: req.Description,
		Icon: req.Icon, DefaultRegistryID: req.DefaultRegistryID, Labels: req.Labels, Metadata: req.Metadata,
		AppType: req.AppType, WorkloadType: req.WorkloadType, GitURL: req.GitURL, DefaultBranch: req.DefaultBranch,
		Language: req.Language, Probe: req.Probe,
		OwnerID: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Created(w, toApplicationDTO(a))
}

// GetApplication GET /api/v1/applications/{appId}
func (h *Handler) GetApplication(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "appId"))
	if !ok {
		return
	}
	a, err := h.svc.Get(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toApplicationDTO(a))
}

// ListApplications GET /api/v1/workspaces/{wsId}/applications
func (h *Handler) ListApplications(w http.ResponseWriter, r *http.Request) {
	wsID, ok := parseID(w, chi.URLParam(r, "wsId"))
	if !ok {
		return
	}
	page, size, _ := httpx.Pagination(r)
	ownerID := int64(httpx.QueryInt(r, "owner_id", 0))
	lifecycle := application.Lifecycle(r.URL.Query().Get("lifecycle"))
	appType := r.URL.Query().Get("app_type")
	search := r.URL.Query().Get("search")
	items, total, err := h.svc.List(r.Context(), wsID, ownerID, lifecycle, appType, search, page, size)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, httpx.Paged[applicationDTO]{
		Items: toApplicationDTOs(items), Total: total, Page: page, Size: size,
	})
}

// ListAllApplications GET /api/v1/applications
// 跨工作空间的应用列表：供「统一应用列表页」按 app_type 分 Tab 浏览。
func (h *Handler) ListAllApplications(w http.ResponseWriter, r *http.Request) {
	page, size, _ := httpx.Pagination(r)
	ownerID := int64(httpx.QueryInt(r, "owner_id", 0))
	lifecycle := application.Lifecycle(r.URL.Query().Get("lifecycle"))
	appType := r.URL.Query().Get("app_type")
	search := r.URL.Query().Get("search")
	items, total, err := h.svc.List(r.Context(), 0, ownerID, lifecycle, appType, search, page, size)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, httpx.Paged[applicationDTO]{
		Items: toApplicationDTOs(items), Total: total, Page: page, Size: size,
	})
}

// UpdateApplication PUT /api/v1/applications/{appId}
func (h *Handler) UpdateApplication(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "appId"))
	if !ok {
		return
	}
	var req struct {
		DisplayName        *string            `json:"display_name"`
		Description        *string            `json:"description"`
		Icon               *string            `json:"icon"`
		Lifecycle          *string            `json:"lifecycle"`
		DefaultGitSourceID *int64             `json:"default_git_source_id"`
		DefaultRegistryID  *int64             `json:"default_registry_id"`
		Labels             *map[string]string `json:"labels"`
		Metadata           *map[string]any    `json:"metadata"`
		AppType            *string            `json:"app_type"`
		WorkloadType       *string            `json:"workload_type"`
		GitURL             *string            `json:"git_url"`
		DefaultBranch      *string            `json:"default_branch"`
		Language           *string            `json:"language"`
		Probe              *application.ProbeConfig `json:"probe"`
		Version            int                `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	if req.Probe != nil {
		if err := req.Probe.Validate(); err != nil {
			httpx.WriteError(w, apperr.Validation(err.Error(), nil))
			return
		}
	}
	var lifecycle *application.Lifecycle
	if req.Lifecycle != nil {
		l := application.Lifecycle(*req.Lifecycle)
		lifecycle = &l
	}
	a, err := h.svc.Update(r.Context(), applicationapp.UpdateInput{
		ID: id, DisplayName: req.DisplayName, Description: req.Description, Icon: req.Icon,
		Lifecycle: lifecycle, DefaultGitSourceID: req.DefaultGitSourceID, DefaultRegistryID: req.DefaultRegistryID,
		Labels: req.Labels, Metadata: req.Metadata,
		AppType: req.AppType, WorkloadType: req.WorkloadType, GitURL: req.GitURL, DefaultBranch: req.DefaultBranch,
		Language: req.Language, Probe: req.Probe,
		Version: req.Version, ActorID: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toApplicationDTO(a))
}

// DeleteApplication DELETE /api/v1/applications/{appId}
func (h *Handler) DeleteApplication(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "appId"))
	if !ok {
		return
	}
	if err := h.svc.Delete(r.Context(), id, uid); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// --- 应用成员 ---

// AddAppMember POST /api/v1/applications/{appId}/members
func (h *Handler) AddAppMember(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "appId"))
	if !ok {
		return
	}
	var req struct {
		UserID int64 `json:"user_id"`
		RoleID int64 `json:"role_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	m, err := h.svc.AddMember(r.Context(), applicationapp.AddMemberInput{
		ApplicationID: id, UserID: req.UserID, RoleID: req.RoleID, ActorID: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Created(w, toAppMemberDTO(m))
}

// ListAppMembers GET /api/v1/applications/{appId}/members
func (h *Handler) ListAppMembers(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "appId"))
	if !ok {
		return
	}
	page, size, _ := httpx.Pagination(r)
	items, total, err := h.svc.ListMembers(r.Context(), id, page, size)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, httpx.Paged[appMemberDTO]{
		Items: toAppMemberDTOs(items), Total: total, Page: page, Size: size,
	})
}

// UpdateAppMemberRole PUT /api/v1/applications/{appId}/members/{userId}
func (h *Handler) UpdateAppMemberRole(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "appId"))
	if !ok {
		return
	}
	userID, ok := parseID(w, chi.URLParam(r, "userId"))
	if !ok {
		return
	}
	var req struct {
		RoleID int64 `json:"role_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	if err := h.svc.UpdateMemberRole(r.Context(), id, userID, req.RoleID, uid); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// RemoveAppMember DELETE /api/v1/applications/{appId}/members/{userId}
func (h *Handler) RemoveAppMember(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "appId"))
	if !ok {
		return
	}
	userID, ok := parseID(w, chi.URLParam(r, "userId"))
	if !ok {
		return
	}
	if err := h.svc.RemoveMember(r.Context(), id, userID, uid); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// --- 分组 ---

// CreateGroup POST /api/v1/applications/{appId}/groups
func (h *Handler) CreateGroup(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	appID, ok := parseID(w, chi.URLParam(r, "appId"))
	if !ok {
		return
	}
	var req createGroupReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	g, err := h.svc.CreateGroup(r.Context(), applicationapp.CreateGroupInput{
		ApplicationID: appID,
		Name:          req.Name, DisplayName: req.DisplayName, Description: req.Description,
		Environment: application.Environment(req.Environment), ClusterID: req.ClusterID, Namespace: req.Namespace,
		Replicas: req.Replicas, Resources: req.Resources.toDomain(), Storage: req.Storage.toDomain(),
		MeshEnabled: req.MeshEnabled, Scheduling: req.Scheduling.toDomain(), Workload: req.Workload.toDomain(),
		HealthCheck: req.HealthCheck.toDomain(), Autoscaling: req.Autoscaling.toDomain(),
		ReleaseRequiresApproval: req.ReleaseRequiresApproval, Labels: req.Labels, Metadata: req.Metadata,
		ActorID: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Created(w, toGroupDTO(g))
}

// GetGroup GET /api/v1/groups/{groupId}
func (h *Handler) GetGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "groupId"))
	if !ok {
		return
	}
	g, err := h.svc.GetGroup(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toGroupDTO(g))
}

// ListGroups GET /api/v1/applications/{appId}/groups
func (h *Handler) ListGroups(w http.ResponseWriter, r *http.Request) {
	appID, ok := parseID(w, chi.URLParam(r, "appId"))
	if !ok {
		return
	}
	page, size, _ := httpx.Pagination(r)
	env := application.Environment(r.URL.Query().Get("environment"))
	clusterID := int64(httpx.QueryInt(r, "cluster_id", 0))
	appType := r.URL.Query().Get("app_type")
	search := r.URL.Query().Get("search")
	items, total, err := h.svc.ListGroups(r.Context(), appID, env, clusterID, appType, search, page, size)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, httpx.Paged[groupDTO]{
		Items: toGroupDTOs(items), Total: total, Page: page, Size: size,
	})
}

// UpdateGroup PUT /api/v1/groups/{groupId}
func (h *Handler) UpdateGroup(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "groupId"))
	if !ok {
		return
	}
	var req updateGroupReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	g, err := h.svc.UpdateGroup(r.Context(), applicationapp.UpdateGroupInput{
		ID: id, DisplayName: req.DisplayName, Description: req.Description, Replicas: req.Replicas,
		Resources: req.Resources.toDomainPtr(), Storage: req.Storage.toDomainPtr(),
		MeshEnabled: req.MeshEnabled, Scheduling: req.Scheduling.toDomainPtr(),
		Workload: req.Workload.toDomainPtr(), HealthCheck: req.HealthCheck.toDomainPtr(),
		Autoscaling: req.Autoscaling.toDomainPtr(), ReleaseRequiresApproval: req.ReleaseRequiresApproval,
		Labels: req.Labels, Metadata: req.Metadata, ClusterID: req.ClusterID,
		Version: req.Version, ActorID: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toGroupDTO(g))
}

// ScaleGroup POST /api/v1/groups/{groupId}/scale
// 专用于扩缩容：仅改副本数并强制同步 K8s。
func (h *Handler) ScaleGroup(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "groupId"))
	if !ok {
		return
	}
	var req struct {
		Replicas       int      `json:"replicas"`
		Version        int      `json:"version"`
		RemovePodNames []string `json:"remove_pod_names"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	g, err := h.svc.ScaleGroup(r.Context(), applicationapp.ScaleGroupInput{
		ID: id, Replicas: req.Replicas, Version: req.Version, ActorID: uid,
		RemovePodNames: req.RemovePodNames,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toGroupDTO(g))
}

// RestartGroup POST /api/v1/groups/{groupId}:restart
func (h *Handler) RestartGroup(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "groupId"))
	if !ok {
		return
	}
	if err := h.svc.RestartGroup(r.Context(), id, uid); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, map[string]any{"restarted": true})
}

// ShutdownGroup POST /api/v1/groups/{groupId}:shutdown
func (h *Handler) ShutdownGroup(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "groupId"))
	if !ok {
		return
	}
	g, err := h.svc.ShutdownGroup(r.Context(), id, uid)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toGroupDTO(g))
}

// StartupGroup POST /api/v1/groups/{groupId}:startup
func (h *Handler) StartupGroup(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "groupId"))
	if !ok {
		return
	}
	g, err := h.svc.StartupGroup(r.Context(), id, uid)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toGroupDTO(g))
}

// RestartPod POST /api/v1/groups/{groupId}/pods/{pod}:restart
func (h *Handler) RestartPod(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "groupId"))
	if !ok {
		return
	}
	pod := chi.URLParam(r, "pod")
	if pod == "" {
		httpx.WriteError(w, apperr.Validation("pod is required", nil))
		return
	}
	if err := h.svc.RestartPod(r.Context(), id, pod, uid); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, map[string]any{"restarted": true})
}

// --- 文件浏览器 / 网络命令 ---

// resolvePodInput 从 group + pod 名解析 opsapp.PodFileInput（含容器名，默认取第一个容器）。
func (h *Handler) resolvePodInput(r *http.Request, uid int64, containerOverride string) (int64, string, *application.Group, error) {
	id, err := parseIDErr(chi.URLParam(r, "groupId"))
	if err != nil {
		return 0, "", nil, err
	}
	pod := chi.URLParam(r, "pod")
	if pod == "" {
		return 0, "", nil, apperr.Validation("pod is required", nil)
	}
	g, gerr := h.svc.GetGroup(r.Context(), id)
	if gerr != nil {
		return 0, "", nil, gerr
	}
	container := containerOverride
	if container == "" {
		// 取 Pod 第一个容器名。
		pods, perr := h.clusterSvc.ListGroupPods(r.Context(), g.ClusterID, g.Namespace, groupSelector(id))
		if perr != nil {
			return 0, "", nil, apperr.Internal("list group pods", perr)
		}
		for _, p := range pods {
			if p.Name == pod {
				if len(p.Containers) > 0 {
					container = p.Containers[0].Name
				}
				break
			}
		}
	}
	if container == "" {
		return 0, "", nil, apperr.Validation("could not resolve container", nil)
	}
	return id, pod, g, nil
}

// ListPodFiles GET /api/v1/groups/{groupId}/pods/{pod}/files?path=&container=
func (h *Handler) ListPodFiles(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	if h.opsSvc == nil {
		httpx.WriteError(w, apperr.Internal("ops service not configured", nil))
		return
	}
	_, pod, g, err := h.resolvePodInput(r, uid, r.URL.Query().Get("container"))
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	entries, err := h.opsSvc.ListFiles(r.Context(), opsapp.PodFileInput{
		UserID: uid, UserName: httpauth.Username(r.Context()), ClusterID: g.ClusterID,
		Namespace: g.Namespace, Pod: pod, Container: r.URL.Query().Get("container"),
		Path: r.URL.Query().Get("path"),
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, map[string]any{"items": entries})
}

// ReadPodFile GET /api/v1/groups/{groupId}/pods/{pod}/files/read?path=&container=&max_lines=
func (h *Handler) ReadPodFile(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	if h.opsSvc == nil {
		httpx.WriteError(w, apperr.Internal("ops service not configured", nil))
		return
	}
	_, pod, g, err := h.resolvePodInput(r, uid, r.URL.Query().Get("container"))
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		httpx.WriteError(w, apperr.Validation("path is required", nil))
		return
	}
	maxLines := 500
	if ml := r.URL.Query().Get("max_lines"); ml != "" {
		if v, e := strconv.Atoi(ml); e == nil && v > 0 {
			maxLines = v
		}
	}
	content, err := h.opsSvc.ReadFileContent(r.Context(), opsapp.PodFileInput{
		UserID: uid, UserName: httpauth.Username(r.Context()), ClusterID: g.ClusterID,
		Namespace: g.Namespace, Pod: pod, Container: r.URL.Query().Get("container"), Path: path,
	}, maxLines)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, map[string]any{"content": content, "path": path})
}

// SearchPodLogPaths GET /api/v1/groups/{groupId}/pods/{pod}/files/search-logs?pattern=&container=
func (h *Handler) SearchPodLogPaths(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	if h.opsSvc == nil {
		httpx.WriteError(w, apperr.Internal("ops service not configured", nil))
		return
	}
	_, pod, g, err := h.resolvePodInput(r, uid, r.URL.Query().Get("container"))
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	paths, err := h.opsSvc.SearchLogPaths(r.Context(), opsapp.PodFileInput{
		UserID: uid, UserName: httpauth.Username(r.Context()), ClusterID: g.ClusterID,
		Namespace: g.Namespace, Pod: pod, Container: r.URL.Query().Get("container"),
	}, r.URL.Query().Get("pattern"))
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, map[string]any{"items": paths})
}

// DownloadPodFile GET /api/v1/groups/{groupId}/pods/{pod}/files/download?path=&container=
func (h *Handler) DownloadPodFile(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	if h.opsSvc == nil {
		httpx.WriteError(w, apperr.Internal("ops service not configured", nil))
		return
	}
	_, pod, g, err := h.resolvePodInput(r, uid, r.URL.Query().Get("container"))
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		httpx.WriteError(w, apperr.Validation("path is required", nil))
		return
	}
	w.Header().Set("Content-Type", "application/x-tar")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.tar"`, baseOf(path)))
	if err := h.opsSvc.DownloadFile(r.Context(), opsapp.PodFileInput{
		UserID: uid, UserName: httpauth.Username(r.Context()), ClusterID: g.ClusterID,
		Namespace: g.Namespace, Pod: pod, Container: r.URL.Query().Get("container"), Path: path,
	}, w); err != nil {
		httpx.WriteError(w, err)
	}
}

// UploadPodFile POST /api/v1/groups/{groupId}/pods/{pod}/files/upload?path=&container=
// body 为 tar 流，上传到 Pod 内 path 目录。
func (h *Handler) UploadPodFile(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	if h.opsSvc == nil {
		httpx.WriteError(w, apperr.Internal("ops service not configured", nil))
		return
	}
	_, pod, g, err := h.resolvePodInput(r, uid, r.URL.Query().Get("container"))
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	if err := h.opsSvc.UploadFile(r.Context(), opsapp.PodFileInput{
		UserID: uid, UserName: httpauth.Username(r.Context()), ClusterID: g.ClusterID,
		Namespace: g.Namespace, Pod: pod, Container: r.URL.Query().Get("container"),
		Path: r.URL.Query().Get("path"),
	}, r.Body); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, map[string]any{"uploaded": true})
}

// DeletePodFile DELETE /api/v1/groups/{groupId}/pods/{pod}/files?path=&container=
func (h *Handler) DeletePodFile(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	if h.opsSvc == nil {
		httpx.WriteError(w, apperr.Internal("ops service not configured", nil))
		return
	}
	_, pod, g, err := h.resolvePodInput(r, uid, r.URL.Query().Get("container"))
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	if err := h.opsSvc.DeleteFile(r.Context(), opsapp.PodFileInput{
		UserID: uid, UserName: httpauth.Username(r.Context()), ClusterID: g.ClusterID,
		Namespace: g.Namespace, Pod: pod, Container: r.URL.Query().Get("container"),
		Path: r.URL.Query().Get("path"),
	}); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, map[string]any{"deleted": true})
}

// CleanupPodFiles POST /api/v1/groups/{groupId}/pods/{pod}/files/cleanup?preset=&container=
func (h *Handler) CleanupPodFiles(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	if h.opsSvc == nil {
		httpx.WriteError(w, apperr.Internal("ops service not configured", nil))
		return
	}
	_, pod, g, err := h.resolvePodInput(r, uid, r.URL.Query().Get("container"))
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	result, err := h.opsSvc.CleanupFiles(r.Context(), opsapp.PodFileInput{
		UserID: uid, UserName: httpauth.Username(r.Context()), ClusterID: g.ClusterID,
		Namespace: g.Namespace, Pod: pod, Container: r.URL.Query().Get("container"),
	}, r.URL.Query().Get("preset"))
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, result)
}

// PodNetCmd POST /api/v1/groups/{groupId}/pods/{pod}/netcmd
// body: {"cmd":"ping","args":["-c","3","8.8.8.8"],"container":""}
// 流式响应：stdout/stderr 实时以 text/plain 分块回写到客户端，避免长命令整体等待。
// 响应头 Content-Type: text/plain; charset=utf-8 + X-Accel-Buffering: no（禁用代理缓冲）。
func (h *Handler) PodNetCmd(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	if h.opsSvc == nil {
		httpx.WriteError(w, apperr.Internal("ops service not configured", nil))
		return
	}
	var req struct {
		Cmd       string   `json:"cmd"`
		Args      []string `json:"args"`
		Container string   `json:"container"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	_, pod, g, err := h.resolvePodInput(r, uid, req.Container)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	// 流式响应：直接把 exec stdout 写入 ResponseWriter。
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	// 禁用 nginx/vite 代理缓冲，确保分块实时透传。
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Cache-Control", "no-cache")
	// 200 状态必须在写 body 前发送；exec 错误会以文本形式追加到流尾。
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	out := &streamFlushWriter{w: w, flusher: flusher}
	if err := h.opsSvc.NetCmdStream(r.Context(), opsapp.PodFileInput{
		UserID: uid, UserName: httpauth.Username(r.Context()), ClusterID: g.ClusterID,
		Namespace: g.Namespace, Pod: pod, Container: req.Container,
	}, req.Cmd, req.Args, out); err != nil {
		// 流已开始，只能以文本追加错误信息。
		fmt.Fprintf(w, "\n[error] %s\n", err.Error())
		if flusher != nil {
			flusher.Flush()
		}
		return
	}
	if flusher != nil {
		flusher.Flush()
	}
}

// streamFlushWriter 包装 http.ResponseWriter，每次 Write 后调用 Flush，
// 让 chunked 输出实时到达前端（适合 ping/curl 等流式命令）。
type streamFlushWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

func (s *streamFlushWriter) Write(p []byte) (int, error) {
	n, err := s.w.Write(p)
	if s.flusher != nil {
		s.flusher.Flush()
	}
	return n, err
}

// parseIDErr 与 parseID 类似但返回 error。
func parseIDErr(s string) (int64, error) {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		return 0, apperr.Validation("invalid id", err)
	}
	return id, nil
}

// baseOf 取路径最后一段（文件名）。
func baseOf(path string) string {
	idx := -1
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			idx = i
			break
		}
	}
	if idx < 0 {
		return path
	}
	return path[idx+1:]
}

// DeleteGroup DELETE /api/v1/groups/{groupId}
func (h *Handler) DeleteGroup(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "groupId"))
	if !ok {
		return
	}
	if err := h.svc.DeleteGroup(r.Context(), id, uid); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// --- Group 运维（K8s） ---

// groupSelector 为分组构造 K8s label selector。
// 约定：分组下的资源打 label "app.vortexops.io/group-id"="<groupID>"。
func groupSelector(groupID int64) string {
	return fmt.Sprintf("app.vortexops.io/group-id=%d", groupID)
}

// ListGroupPods GET /api/v1/groups/{groupId}/pods
func (h *Handler) ListGroupPods(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "groupId"))
	if !ok {
		return
	}
	g, err := h.svc.GetGroup(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	pods, err := h.clusterSvc.ListGroupPods(r.Context(), g.ClusterID, g.Namespace, groupSelector(id))
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	// 按应用探活配置主动拨测，填充 app_ready（未配置则跳过）。
	if app, aerr := h.svc.GetApplicationByID(r.Context(), g.ApplicationID); aerr == nil {
		pods = h.clusterSvc.ProbePodsAppReady(r.Context(), g.ClusterID, pods, application.ProbeFromApplication(app))
	}
	httpx.OK(w, map[string]any{"items": pods})
}

// ListGroupStableIPs GET /api/v1/groups/{groupId}/stable-ips
// 返回分组已分配的稳定 IP，以及集群是否具备固定 IP 直连能力。
func (h *Handler) ListGroupStableIPs(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "groupId"))
	if !ok {
		return
	}
	g, err := h.svc.GetGroup(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	allocs, err := h.clusterSvc.ListGroupStableIPs(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	items := make([]map[string]any, 0, len(allocs))
	for _, a := range allocs {
		if a == nil {
			continue
		}
		items = append(items, map[string]any{
			"ip":            a.IPAddress,
			"replica_index": a.ReplicaIndex,
			"status":        string(a.Status),
		})
	}
	capOK := true
	capMsg := ""
	if err := h.clusterSvc.CheckDirectAccessCapability(r.Context(), g.ClusterID); err != nil {
		capOK = false
		if ae, ok := apperr.As(err); ok && ae.Message != "" {
			capMsg = ae.Message
		} else {
			capMsg = err.Error()
		}
	}
	httpx.OK(w, map[string]any{
		"items": items,
		"capability": map[string]any{
			"ok":      capOK,
			"message": capMsg,
		},
	})
}

// GetGroupPodLogs GET /api/v1/groups/{groupId}/pods/{pod}/logs?container=&tail=
func (h *Handler) GetGroupPodLogs(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "groupId"))
	if !ok {
		return
	}
	pod := chi.URLParam(r, "pod")
	if pod == "" {
		httpx.WriteError(w, apperr.Validation("pod is required", nil))
		return
	}
	g, err := h.svc.GetGroup(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	container := r.URL.Query().Get("container")
	tail, _ := strconv.ParseInt(r.URL.Query().Get("tail"), 10, 64)
	if tail <= 0 {
		tail = 1000
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if err := h.clusterSvc.StreamPodLogs(r.Context(), clusterapp.PodLogsInput{
		ClusterID: g.ClusterID, Namespace: g.Namespace, Pod: pod,
		Container: container, TailLines: tail, Follow: false,
	}, w); err != nil {
		httpx.WriteError(w, err)
		return
	}
}

// ListGroupEvents GET /api/v1/groups/{groupId}/events
func (h *Handler) ListGroupEvents(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "groupId"))
	if !ok {
		return
	}
	g, err := h.svc.GetGroup(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	events, err := h.clusterSvc.ListGroupEvents(r.Context(), g.ClusterID, g.Namespace, groupSelector(id))
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, map[string]any{"items": events})
}

// GetGroupYAML GET /api/v1/groups/{groupId}/yaml
func (h *Handler) GetGroupYAML(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "groupId"))
	if !ok {
		return
	}
	g, err := h.svc.GetGroup(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	name := g.DeploymentName
	if name == "" {
		name = g.Name
	}
	resources, err := h.clusterSvc.RenderGroupYAML(r.Context(), g.ClusterID, g.Namespace, string(g.Workload.Type), name)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, map[string]any{"items": resources})
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
