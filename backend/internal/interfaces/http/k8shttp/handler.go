// Package k8shttp 是 K8s 通用资源运维的 HTTP handlers。
// 暴露节点/工作负载/存储/网络/配置/事件的查询与运维操作，通过平台级权限控制。
package k8shttp

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/vortexops/vortexops/internal/application/clusterapp"
	"github.com/vortexops/vortexops/internal/application/k8sapp"
	"github.com/vortexops/vortexops/internal/application/nodepoolapp"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpauth"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpx"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// Handler 处理 /api/v1/k8s 路由。
type Handler struct {
	svc          *k8sapp.Service
	nodePoolSvc  *nodepoolapp.Service
	clusterSvc   *clusterapp.Service
}

// NewHandler 创建 K8s 运维 handler。nodePoolSvc 用于云厂商节点池扩缩容。
func NewHandler(svc *k8sapp.Service, nodePoolSvc *nodepoolapp.Service, clusterSvc *clusterapp.Service) *Handler {
	return &Handler{svc: svc, nodePoolSvc: nodePoolSvc, clusterSvc: clusterSvc}
}

// clusterID 从 URL 参数 {clusterId} 解析集群 ID。
func clusterID(r *http.Request) (int64, error) {
	v := chi.URLParam(r, "clusterId")
	id, err := strconv.ParseInt(v, 10, 64)
	if err != nil || id <= 0 {
		return 0, apperr.Validation("invalid cluster_id", err)
	}
	return id, nil
}

func namespace(r *http.Request) string {
	ns := r.URL.Query().Get("namespace")
	return ns
}

// --- 节点 ---

// ListNodes GET /api/v1/k8s/clusters/{clusterId}/nodes
func (h *Handler) ListNodes(w http.ResponseWriter, r *http.Request) {
	cid, err := clusterID(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	nodes, err := h.svc.ListNodes(r.Context(), cid)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, nodes)
}

// CordonNode POST /api/v1/k8s/clusters/{clusterId}/nodes/{nodeName}/cordon
func (h *Handler) CordonNode(w http.ResponseWriter, r *http.Request) {
	cid, err := clusterID(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	nodeName := chi.URLParam(r, "nodeName")
	if nodeName == "" {
		httpx.WriteError(w, apperr.Validation("node name is required", nil))
		return
	}
	if err := h.svc.CordonNode(r.Context(), cid, nodeName); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// UncordonNode POST /api/v1/k8s/clusters/{clusterId}/nodes/{nodeName}/uncordon
func (h *Handler) UncordonNode(w http.ResponseWriter, r *http.Request) {
	cid, err := clusterID(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	nodeName := chi.URLParam(r, "nodeName")
	if err := h.svc.UncordonNode(r.Context(), cid, nodeName); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// DrainNode POST /api/v1/k8s/clusters/{clusterId}/nodes/{nodeName}/drain
func (h *Handler) DrainNode(w http.ResponseWriter, r *http.Request) {
	cid, err := clusterID(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	nodeName := chi.URLParam(r, "nodeName")
	if err := h.svc.DrainNode(r.Context(), cid, nodeName); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// --- 工作负载 ---

// ListDeployments GET /api/v1/k8s/clusters/{clusterId}/deployments?namespace=
func (h *Handler) ListDeployments(w http.ResponseWriter, r *http.Request) {
	cid, err := clusterID(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	items, err := h.svc.ListDeployments(r.Context(), cid, namespace(r))
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, items)
}

// ScaleDeployment POST /api/v1/k8s/clusters/{clusterId}/namespaces/{namespace}/deployments/{name}/scale
func (h *Handler) ScaleDeployment(w http.ResponseWriter, r *http.Request) {
	cid, err := clusterID(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	ns := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")
	var req struct {
		Replicas int32 `json:"replicas"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	if err := h.svc.ScaleDeployment(r.Context(), cid, ns, name, req.Replicas); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// ListStatefulSets GET /api/v1/k8s/clusters/{clusterId}/statefulsets
func (h *Handler) ListStatefulSets(w http.ResponseWriter, r *http.Request) {
	cid, err := clusterID(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	items, err := h.svc.ListStatefulSets(r.Context(), cid, namespace(r))
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, items)
}

// ScaleStatefulSet POST /api/v1/k8s/clusters/{clusterId}/namespaces/{namespace}/statefulsets/{name}/scale
func (h *Handler) ScaleStatefulSet(w http.ResponseWriter, r *http.Request) {
	cid, err := clusterID(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	ns := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")
	var req struct {
		Replicas int32 `json:"replicas"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	if err := h.svc.ScaleStatefulSet(r.Context(), cid, ns, name, req.Replicas); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// ListDaemonSets GET /api/v1/k8s/clusters/{clusterId}/daemonsets
func (h *Handler) ListDaemonSets(w http.ResponseWriter, r *http.Request) {
	cid, err := clusterID(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	items, err := h.svc.ListDaemonSets(r.Context(), cid, namespace(r))
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, items)
}

// ListPods GET /api/v1/k8s/clusters/{clusterId}/pods?namespace=&fieldSelector=
func (h *Handler) ListPods(w http.ResponseWriter, r *http.Request) {
	cid, err := clusterID(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	fs := r.URL.Query().Get("fieldSelector")
	items, err := h.svc.ListPods(r.Context(), cid, namespace(r), fs)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, items)
}

// DeletePod DELETE /api/v1/k8s/clusters/{clusterId}/namespaces/{namespace}/pods/{name}
func (h *Handler) DeletePod(w http.ResponseWriter, r *http.Request) {
	cid, err := clusterID(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	ns := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")
	if err := h.svc.DeletePod(r.Context(), cid, ns, name); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// --- 存储 ---

// ListPersistentVolumes GET /api/v1/k8s/clusters/{clusterId}/persistentvolumes
func (h *Handler) ListPersistentVolumes(w http.ResponseWriter, r *http.Request) {
	cid, err := clusterID(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	items, err := h.svc.ListPersistentVolumes(r.Context(), cid)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, items)
}

// ListPersistentVolumeClaims GET /api/v1/k8s/clusters/{clusterId}/persistentvolumeclaims
func (h *Handler) ListPersistentVolumeClaims(w http.ResponseWriter, r *http.Request) {
	cid, err := clusterID(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	items, err := h.svc.ListPersistentVolumeClaims(r.Context(), cid, namespace(r))
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, items)
}

// ListStorageClasses GET /api/v1/k8s/clusters/{clusterId}/storageclasses
func (h *Handler) ListStorageClasses(w http.ResponseWriter, r *http.Request) {
	cid, err := clusterID(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	items, err := h.svc.ListStorageClasses(r.Context(), cid)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, items)
}

// --- 网络 ---

// ListServices GET /api/v1/k8s/clusters/{clusterId}/services
func (h *Handler) ListServices(w http.ResponseWriter, r *http.Request) {
	cid, err := clusterID(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	items, err := h.svc.ListServices(r.Context(), cid, namespace(r))
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, items)
}

// ListIngresses GET /api/v1/k8s/clusters/{clusterId}/ingresses
func (h *Handler) ListIngresses(w http.ResponseWriter, r *http.Request) {
	cid, err := clusterID(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	items, err := h.svc.ListIngresses(r.Context(), cid, namespace(r))
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, items)
}

// ListNetworkPolicies GET /api/v1/k8s/clusters/{clusterId}/networkpolicies
func (h *Handler) ListNetworkPolicies(w http.ResponseWriter, r *http.Request) {
	cid, err := clusterID(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	items, err := h.svc.ListNetworkPolicies(r.Context(), cid, namespace(r))
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, items)
}

// --- 配置 ---

// ListConfigMaps GET /api/v1/k8s/clusters/{clusterId}/configmaps
func (h *Handler) ListConfigMaps(w http.ResponseWriter, r *http.Request) {
	cid, err := clusterID(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	items, err := h.svc.ListConfigMaps(r.Context(), cid, namespace(r))
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, items)
}

// ListSecrets GET /api/v1/k8s/clusters/{clusterId}/secrets
func (h *Handler) ListSecrets(w http.ResponseWriter, r *http.Request) {
	cid, err := clusterID(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	items, err := h.svc.ListSecrets(r.Context(), cid, namespace(r))
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, items)
}

// --- 事件 ---

// ListEvents GET /api/v1/k8s/clusters/{clusterId}/events?namespace=&fieldSelector=
func (h *Handler) ListEvents(w http.ResponseWriter, r *http.Request) {
	cid, err := clusterID(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	fs := r.URL.Query().Get("fieldSelector")
	items, err := h.svc.ListEvents(r.Context(), cid, namespace(r), fs)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, items)
}

// --- 云节点池扩缩容 ---

// ScaleNodePool POST /api/v1/k8s/clusters/{clusterId}/node-pools/{nodePoolId}/scale
// 从集群 metadata 读取 provider 与云凭证，调用对应云厂商 scaler 调整节点池期望数。
func (h *Handler) ScaleNodePool(w http.ResponseWriter, r *http.Request) {
	cid, err := clusterID(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	nodePoolID := chi.URLParam(r, "nodePoolId")
	if nodePoolID == "" {
		httpx.WriteError(w, apperr.Validation("node_pool_id is required", nil))
		return
	}
	var req struct {
		DesiredSize int32 `json:"desired_size"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}

	uid := httpauth.UserID(r.Context())

	// 获取集群元数据以解析 provider 与凭证。
	c, err := h.clusterSvc.Get(r.Context(), cid)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	provider, _ := c.Metadata["provider"].(string)
	if provider == "" {
		provider, _ = c.Labels["provider"]
	}
	if provider == "" {
		httpx.WriteError(w, apperr.Validation("cluster has no provider in metadata; cannot scale node pool", nil))
		return
	}

	// 从 metadata 提取云凭证（生产环境应通过独立凭证管理，此处简化）。
	creds := make(map[string]string)
	if v, ok := c.Metadata["access_key"].(string); ok {
		creds["access_key"] = v
	}
	if v, ok := c.Metadata["secret_key"].(string); ok {
		creds["secret_key"] = v
	}

	res, err := h.nodePoolSvc.Scale(r.Context(), nodepoolapp.ScaleInput{
		ClusterID:   cid,
		NodePoolID:  nodePoolID,
		DesiredSize: req.DesiredSize,
		Credentials: creds,
		Region:      c.Region,
		Provider:    provider,
		ActorID:     uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, res)
}

// GetNodePool GET /api/v1/k8s/clusters/{clusterId}/node-pools/{nodePoolId}
func (h *Handler) GetNodePool(w http.ResponseWriter, r *http.Request) {
	cid, err := clusterID(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	nodePoolID := chi.URLParam(r, "nodePoolId")
	c, err := h.clusterSvc.Get(r.Context(), cid)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	provider, _ := c.Metadata["provider"].(string)
	if provider == "" {
		provider, _ = c.Labels["provider"]
	}
	creds := make(map[string]string)
	if v, ok := c.Metadata["access_key"].(string); ok {
		creds["access_key"] = v
	}
	if v, ok := c.Metadata["secret_key"].(string); ok {
		creds["secret_key"] = v
	}
	status, err := h.nodePoolSvc.Get(r.Context(), nodepoolapp.GetInput{
		ClusterID:   cid,
		NodePoolID:  nodePoolID,
		Credentials: creds,
		Region:      c.Region,
		Provider:    provider,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, status)
}

// （未使用：保留 uid 上下文以备后续细粒度审计）
var _ = httpauth.UserID
