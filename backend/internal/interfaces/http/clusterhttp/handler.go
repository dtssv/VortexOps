// Package clusterhttp 是集群领域的 HTTP handlers。
package clusterhttp

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/vortexops/vortexops/internal/application/clusterapp"
	"github.com/vortexops/vortexops/internal/domain/cluster"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpauth"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpx"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// Handler 处理 /api/v1/clusters 与 /api/v1/credentials 路由。
type Handler struct {
	svc *clusterapp.Service
}

// NewHandler 创建集群 handler。
func NewHandler(svc *clusterapp.Service) *Handler {
	return &Handler{svc: svc}
}

// --- 集群 ---

// Create POST /api/v1/clusters
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	var req struct {
		Name                   string            `json:"name"`
		DisplayName            string            `json:"display_name"`
		Description            string            `json:"description"`
		APIServer              string            `json:"api_server"`
		Kubeconfig             string            `json:"kubeconfig"`
		CACert                 string            `json:"ca_cert"`
		DefaultNamespacePrefix string            `json:"default_namespace_prefix"`
		InsecureSkipTLS        bool              `json:"insecure_skip_tls"`
		Region                 string            `json:"region"`
		Environment            string            `json:"environment"`
		Labels                 map[string]string `json:"labels"`
		Metadata               map[string]any    `json:"metadata"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	c, err := h.svc.Create(r.Context(), clusterapp.CreateInput{
		Name: req.Name, DisplayName: req.DisplayName, Description: req.Description, APIServer: req.APIServer,
		Kubeconfig: []byte(req.Kubeconfig), CACert: []byte(req.CACert), DefaultNamespacePrefix: req.DefaultNamespacePrefix,
		InsecureSkipTLS: req.InsecureSkipTLS, Region: req.Region, Environment: req.Environment,
		Labels: req.Labels, Metadata: req.Metadata, CreatedBy: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Created(w, toClusterDTO(c))
}

// Get GET /api/v1/clusters/{id}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	c, err := h.svc.Get(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toClusterDTO(c))
}

// List GET /api/v1/clusters?page=&size=&status=&region=&search=
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	page, size, _ := httpx.Pagination(r)
	status := cluster.Status(r.URL.Query().Get("status"))
	region := r.URL.Query().Get("region")
	search := r.URL.Query().Get("search")
	items, total, err := h.svc.List(r.Context(), status, region, search, page, size)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, httpx.Paged[clusterDTO]{
		Items: toClusterDTOs(items), Total: total, Page: page, Size: size,
	})
}

// Update PUT /api/v1/clusters/{id}
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var req struct {
		DisplayName            *string            `json:"display_name"`
		Description            *string            `json:"description"`
		Kubeconfig             *string            `json:"kubeconfig"`
		CACert                 *string            `json:"ca_cert"`
		DefaultNamespacePrefix *string            `json:"default_namespace_prefix"`
		InsecureSkipTLS        *bool              `json:"insecure_skip_tls"`
		Region                 *string            `json:"region"`
		Environment            *string            `json:"environment"`
		Labels                 *map[string]string `json:"labels"`
		Metadata               *map[string]any    `json:"metadata"`
		Version                int                `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	var kubeconfig []byte
	if req.Kubeconfig != nil && *req.Kubeconfig != "" {
		kubeconfig = []byte(*req.Kubeconfig)
	}
	var caCert []byte
	if req.CACert != nil && *req.CACert != "" {
		caCert = []byte(*req.CACert)
	}
	c, err := h.svc.Update(r.Context(), clusterapp.UpdateInput{
		ID: id, DisplayName: req.DisplayName, Description: req.Description, Kubeconfig: kubeconfig,
		CACert: caCert, DefaultNamespacePrefix: req.DefaultNamespacePrefix, InsecureSkipTLS: req.InsecureSkipTLS,
		Region: req.Region, Environment: req.Environment, Labels: req.Labels, Metadata: req.Metadata,
		Version: req.Version, ActorID: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toClusterDTO(c))
}

// Delete DELETE /api/v1/clusters/{id}
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if err := h.svc.Delete(r.Context(), id, uid); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// Probe POST /api/v1/clusters/{id}/probe
func (h *Handler) Probe(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	result, err := h.svc.Probe(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, probeResultDTO{
		K8sVersion: result.K8sVersion, NodeCount: result.NodeCount, APIServer: result.APIServer,
		AllocatableCPUM: result.AllocatableCPUM, AllocatableMemoryBytes: result.AllocatableMemoryBytes,
		AllocatableGPU: result.AllocatableGPU,
	})
}

// GetCapacity GET /api/v1/clusters/{id}/capacity?cpu_m=&memory_bytes=&gpu=
// 按指定单副本资源需求预估集群可调度副本数。
func (h *Handler) GetCapacity(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	cpuM, _ := strconv.Atoi(r.URL.Query().Get("cpu_m"))
	memBytes, _ := strconv.ParseInt(r.URL.Query().Get("memory_bytes"), 10, 64)
	gpu, _ := strconv.Atoi(r.URL.Query().Get("gpu"))
	cap, err := h.svc.GetClusterCapacity(r.Context(), clusterapp.CapacityQuery{
		ClusterID: id, PerCPUM: cpuM, PerMemBytes: memBytes, PerGPU: gpu,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, cap)
}

// --- 凭证 ---

// CreateCredential POST /api/v1/credentials
func (h *Handler) CreateCredential(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	var req struct {
		Name      string    `json:"name"`
		Kind      string    `json:"kind"`
		Scope     string    `json:"scope"`
		ScopeID   int64     `json:"scope_id"`
		Payload   []byte    `json:"payload"`
		ExpiresAt *time.Time `json:"expires_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	c, err := h.svc.CreateCredential(r.Context(), clusterapp.CreateCredentialInput{
		Name: req.Name, Kind: cluster.CredentialKind(req.Kind), Scope: cluster.CredentialScope(req.Scope),
		ScopeID: req.ScopeID, Payload: req.Payload, ExpiresAt: req.ExpiresAt, CreatedBy: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Created(w, toCredentialDTO(c))
}

// ListCredentials GET /api/v1/credentials?scope=&scope_id=&page=&size=
func (h *Handler) ListCredentials(w http.ResponseWriter, r *http.Request) {
	page, size, _ := httpx.Pagination(r)
	scope := cluster.CredentialScope(r.URL.Query().Get("scope"))
	scopeID, _ := strconv.ParseInt(r.URL.Query().Get("scope_id"), 10, 64)
	kind := cluster.CredentialKind(r.URL.Query().Get("kind"))
	items, total, err := h.svc.ListCredentials(r.Context(), scope, scopeID, kind, page, size)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, httpx.Paged[credentialDTO]{
		Items: toCredentialDTOs(items), Total: total, Page: page, Size: size,
	})
}

// RotateCredential POST /api/v1/credentials/{id}/rotate
func (h *Handler) RotateCredential(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var req struct {
		Payload []byte `json:"payload"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	if err := h.svc.RotateCredential(r.Context(), id, req.Payload, uid); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// DeleteCredential DELETE /api/v1/credentials/{id}
func (h *Handler) DeleteCredential(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if err := h.svc.DeleteCredential(r.Context(), id, uid); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// --- IP 池 ---

// CreateIPPool POST /api/v1/clusters/{id}/ip-pools
func (h *Handler) CreateIPPool(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	clusterID, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var req struct {
		Name        string            `json:"name"`
		CIDR        string            `json:"cidr"`
		Gateway     string            `json:"gateway"`
		Provider    string            `json:"provider"`
		ReservedIPs []string          `json:"reserved_ips"`
		Metadata    map[string]any    `json:"metadata"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	p, err := h.svc.CreateIPPool(r.Context(), clusterapp.CreateIPPoolInput{
		ClusterID: clusterID, Name: req.Name, CIDR: req.CIDR, Gateway: req.Gateway,
		Provider: cluster.IPPoolProvider(req.Provider), ReservedIPs: req.ReservedIPs,
		Metadata: req.Metadata, CreatedBy: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Created(w, toIPPoolDTO(p))
}

// ListIPPools GET /api/v1/clusters/{id}/ip-pools
func (h *Handler) ListIPPools(w http.ResponseWriter, r *http.Request) {
	clusterID, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	items, err := h.svc.ListIPPools(r.Context(), clusterID)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toIPPoolDTOs(items))
}

// DeleteIPPool DELETE /api/v1/ip-pools/{id}
func (h *Handler) DeleteIPPool(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if err := h.svc.DeleteIPPool(r.Context(), id, uid); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
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
