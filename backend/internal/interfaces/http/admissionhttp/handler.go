// Package admissionhttp 实现 VortexOps Mutating Webhook 的 HTTP 入口。
//
// 路由：
//   - POST /mutate         接收 kube-apiserver 的 AdmissionReview，为 Pod 注入稳定 IP 注解。
//   - GET  /healthz        健康检查（k8s readiness/liveness probe 用）。
//
// 多集群处理：webhook 通过 Pod 标签 app.vortexops.io/group-id 反查 group，
// 进而拿到 cluster_id，复用 clusterapp 的 IPAM 接口分配 IP。
// 同一 webhook 服务可服务多个集群（每个集群独立注册 MutatingWebhookConfiguration）。
package admissionhttp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/vortexops/vortexops/internal/application/clusterapp"
	"github.com/vortexops/vortexops/internal/domain/application"
	"github.com/vortexops/vortexops/internal/domain/networkprofile"
	"github.com/vortexops/vortexops/internal/infrastructure/k8s/admission"
	"github.com/vortexops/vortexops/internal/infrastructure/k8s/podnet"
	"github.com/vortexops/vortexops/internal/platform/logger"
)

// 与 workload/renderer.go 一致的 Pod 标签 key。
const (
	labelGroupID    = "app.vortexops.io/group-id"
	labelManaged    = "app.vortexops.io/managed"
	annotationReplica = "app.vortexops.io/replica-index"
)

// GroupResolver 从 group_id 解析 group（含 cluster_id / replicas）。
// 由 applicationapp 提供，避免 admissionhttp 直接依赖 application.Repository。
type GroupResolver interface {
	GetGroupByID(ctx context.Context, id int64) (*application.Group, error)
}

// K8sClientProvider 按 clusterID 返回该集群的 kubernetes.Interface（列出 Pod 用）。
// 由 clusterapp.Service 实现（GetClient）。
type K8sClientProvider interface {
	GetClient(ctx context.Context, clusterID int64) (kubernetes.Interface, error)
}

// Handler 处理 webhook admission 请求。
type Handler struct {
	groupResolver GroupResolver
	ipAllocator   clusterapp.GroupIPAllocator
	clientProvider K8sClientProvider
	log           *logger.Logger
}

// NewHandler 创建 admission handler。
func NewHandler(gr GroupResolver, ipAlloc clusterapp.GroupIPAllocator, cp K8sClientProvider, log *logger.Logger) *Handler {
	return &Handler{
		groupResolver:  gr,
		ipAllocator:    ipAlloc,
		clientProvider: cp,
		log:            log,
	}
}

// Register 注册路由到 mux（webhook 不走 httpauth 中间件，由 kube-apiserver 直接调用）。
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/mutate", h.Mutate)
	mux.HandleFunc("/healthz", h.Healthz)
}

// Healthz 健康检查。始终返回 200（webhook 进程存活即健康）。
func (h *Handler) Healthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// Mutate 处理 AdmissionReview：
//  1. 解析 AdmissionReview，提取 Pod 对象。
//  2. 从 Pod 标签 app.vortexops.io/group-id 解析 group_id；缺失则放行（非 VortexOps 管理的 Pod）。
//  3. 解析 group 拿 cluster_id 与 replicas。
//  4. 查该 group 现存 Pod 的 replica-index 注解，构建 occupied 集合。
//  5. 调 AllocateForPod 分配稳定 IP（幂等复用已分配槽位）。
//  6. 构造 JSONPatch 注入注解，返回 AdmissionResponse。
func (h *Handler) Mutate(w http.ResponseWriter, r *http.Request) {
	if r.Body == nil {
		writeAdmissionError(w, http.StatusBadRequest, "empty request body")
		return
	}
	var review admissionv1.AdmissionReview
	if err := json.NewDecoder(r.Body).Decode(&review); err != nil {
		writeAdmissionError(w, http.StatusBadRequest, fmt.Sprintf("decode admission review: %v", err))
		return
	}

	uid := ""
	if review.Request != nil {
		uid = string(review.Request.UID)
	}
	resp := &admissionv1.AdmissionResponse{
		UID:     review.Request.UID,
		Allowed: true, // 默认放行：webhook 仅做注解注入，不阻断 Pod 创建
	}

	if review.Request == nil {
		writeJSON(w, http.StatusOK, admissionv1.AdmissionReview{
			TypeMeta: metav1.TypeMeta{Kind: "AdmissionReview", APIVersion: "admission.k8s.io/v1"},
			Response: resp,
		})
		return
	}

	// 仅处理 Pod CREATE。
	if review.Request.Kind.Kind != "Pod" || review.Request.Operation != admissionv1.Create {
		writeJSON(w, http.StatusOK, admissionv1.AdmissionReview{
			TypeMeta: metav1.TypeMeta{Kind: "AdmissionReview", APIVersion: "admission.k8s.io/v1"},
			Response: resp,
		})
		return
	}

	// 反序列化 Pod。
	var pod corev1.Pod
	if err := json.Unmarshal(review.Request.Object.Raw, &pod); err != nil {
		h.log.Warn("webhook: unmarshal pod failed", "uid", uid, "err", err)
		// 解析失败不阻断：返回 allowed=true（无 patch）。
		writeJSON(w, http.StatusOK, wrapResponse(resp))
		return
	}

	// 非 VortexOps 管理的 Pod：放行。
	groupIDStr := pod.Labels[labelGroupID]
	if groupIDStr == "" || pod.Labels[labelManaged] != "true" {
		writeJSON(w, http.StatusOK, wrapResponse(resp))
		return
	}
	groupID, err := strconv.ParseInt(groupIDStr, 10, 64)
	if err != nil || groupID <= 0 {
		h.log.Warn("webhook: invalid group-id label", "uid", uid, "label", groupIDStr)
		writeJSON(w, http.StatusOK, wrapResponse(resp))
		return
	}

	// 解析 group。
	g, err := h.groupResolver.GetGroupByID(r.Context(), groupID)
	if err != nil {
		h.log.Warn("webhook: resolve group failed, allowing without IP injection",
			"uid", uid, "group_id", groupID, "err", err)
		writeJSON(w, http.StatusOK, wrapResponse(resp))
		return
	}

	// 已由 webhook 注入稳定 IP 的 Pod（重复 admission）：放行不重复注入。
	// 判断依据：webhook 注入的 app.vortexops.io/ip-assigned-by=webhook 注解存在。
	// 注意：renderer 会注入 keep-pod-ip + stable-ip-0 作为降级，webhook 必须覆盖它们。
	if pod.Annotations[admission.AnnotationAssignedBy] == "webhook" {
		writeJSON(w, http.StatusOK, wrapResponse(resp))
		return
	}

	// 查集群网络方案（决定 CNI 注解 key）。
	profile, perr := h.ipAllocator.GetNetworkProfile(r.Context(), g.ClusterID)
	if perr != nil {
		h.log.Warn("webhook: get network profile failed, using default whereabouts",
			"uid", uid, "cluster_id", g.ClusterID, "err", perr)
		profile = &networkprofile.ProfileConfig{Profile: networkprofile.ProfileDevSingle, CNI: networkprofile.CNIWhereabouts}
	}

	// 查该 group 现存 Pod 的 PodIP（实际占用 IP），构建 in-use 集合。
	// 同时解析 replica-index 注解作为兼容信号。
	inUseIPs, occupiedIdx := h.collectInUseIPs(r.Context(), g, &pod)

	// 分配稳定 IP（发布侧 maxSurge=0 逐台替换，旧 Pod 终止后槽位释放可复用）。
	ip, replicaIdx, err := h.ipAllocator.AllocateForPod(r.Context(), clusterapp.AllocateForPodInput{
		GroupID:                g.ID,
		ClusterID:              g.ClusterID,
		Replicas:               g.Replicas,
		InUseIPs:               inUseIPs,
		OccupiedReplicaIndices: occupiedIdx,
	})
	if err != nil {
		// 分配失败：软降级，放行 Pod 创建但不注入稳定 IP（Pod 走 CNI 默认 IPAM）。
		h.log.Warn("webhook: allocate stable IP failed, soft-degrade (pod will use CNI default IP)",
			"uid", uid, "group_id", g.ID, "cluster_id", g.ClusterID, "err", err)
		writeJSON(w, http.StatusOK, wrapResponse(resp))
		return
	}

	// 构造注解 + JSONPatch。
	desired := admission.BuildStableIPAnnotations(ip, replicaIdx, profile)
	patchOps := admission.BuildPatch(pod.Annotations, desired)
	if len(patchOps) == 0 {
		writeJSON(w, http.StatusOK, wrapResponse(resp))
		return
	}
	patchBytes, err := json.Marshal(patchOps)
	if err != nil {
		h.log.Warn("webhook: marshal patch failed", "uid", uid, "err", err)
		writeJSON(w, http.StatusOK, wrapResponse(resp))
		return
	}
	resp.Patch = patchBytes
	resp.PatchType = func() *admissionv1.PatchType {
		pt := admissionv1.PatchTypeJSONPatch
		return &pt
	}()

	h.log.Info("webhook: injected stable IP",
		"uid", uid, "group_id", g.ID, "cluster_id", g.ClusterID,
		"pod", pod.Name, "ip", ip, "replica_index", replicaIdx)

	writeJSON(w, http.StatusOK, wrapResponse(resp))
}

// collectInUseIPs 查询该 group 下现存 Pod 的实际 PodIP，返回已被占用的 IP 集合与 replica-index 槽位。
// 排除当前正在 admission 的 Pod（pod.Name）本身。
// 查询失败时返回空集合（不阻断分配）。
//
// 返回：
//   - inUseIPs：Running Pod 的 status.podIP（最可靠的占用信号）。
//   - occupiedIdx：Pod 的 app.vortexops.io/replica-index 注解值（兼容旧版/webhook 注入的 Pod）。
func (h *Handler) collectInUseIPs(ctx context.Context, g *application.Group, currentPod *corev1.Pod) (inUseIPs []string, occupiedIdx []int) {
	client, err := h.clientProvider.GetClient(ctx, g.ClusterID)
	if err != nil {
		h.log.Warn("webhook: get k8s client failed, assuming no in-use IPs",
			"group_id", g.ID, "cluster_id", g.ClusterID, "err", err)
		return nil, nil
	}
	selector := fmt.Sprintf("%s=%s", labelGroupID, strconv.FormatInt(g.ID, 10))
	podList, err := client.CoreV1().Pods(g.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		h.log.Warn("webhook: list pods failed, assuming no in-use IPs",
			"group_id", g.ID, "namespace", g.Namespace, "err", err)
		return nil, nil
	}
	for i := range podList.Items {
		p := &podList.Items[i]
		if p.Name == currentPod.Name {
			continue // 排除当前 Pod
		}
		// 正在终止的 Pod 不计入占用（其 IP 即将被释放，新 Pod 可复用）。
		// deletionTimestamp 非空即表示 Pod 处于 Terminating。
		if p.DeletionTimestamp != nil {
			continue
		}
		// 实际占用 IP：Multus 双网卡时 status.podIP 常为 Overlay，稳定物理 IP 在注解/副网卡。
		inUseIPs = append(inUseIPs, podnet.InUseIPs(p)...)
		// replica-index 注解（webhook 注入的 Pod 或旧版注解）。
		idxStr := p.Annotations[annotationReplica]
		if idxStr == "" {
			continue
		}
		idx, err := strconv.Atoi(idxStr)
		if err != nil || idx < 0 {
			continue
		}
		occupiedIdx = append(occupiedIdx, idx)
	}
	return inUseIPs, occupiedIdx
}

// writeAdmissionError 写入 HTTP 错误（非 admission response，用于请求级错误）。
func writeAdmissionError(w http.ResponseWriter, code int, msg string) {
	http.Error(w, msg, code)
}

// wrapResponse 包装 AdmissionResponse 为 AdmissionReview 响应。
func wrapResponse(resp *admissionv1.AdmissionResponse) admissionv1.AdmissionReview {
	return admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{Kind: "AdmissionReview", APIVersion: "admission.k8s.io/v1"},
		Response: resp,
	}
}

// writeJSON 写入 JSON 响应。
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
