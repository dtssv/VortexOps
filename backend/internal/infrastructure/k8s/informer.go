package k8s

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

// RuntimeCache 是运行态缓存的抽象（由 infrastructure/redis/runtime 实现）。
// syncer 把 Informer 事件转换为 PodSummary 写入缓存，apiserver 读缓存返回运行态。
type RuntimeCache interface {
	UpsertPod(ctx context.Context, clusterID int64, ns string, pod *PodSummary) error
	DeletePod(ctx context.Context, clusterID int64, ns, name string) error
	UpsertGroupRuntime(ctx context.Context, clusterID, groupID int64, summary *GroupRuntime) error
	DeleteGroupRuntime(ctx context.Context, clusterID, groupID int64) error
}

// PodSummary Pod 运行态摘要（不入库，仅缓存）。
type PodSummary struct {
	Name            string    `json:"name"`
	Namespace       string    `json:"namespace"`
	UID             string    `json:"uid"`
	Phase           string    `json:"phase"`
	PodIP           string    `json:"pod_ip"`
	HostIP          string    `json:"host_ip"`
	NodeName        string    `json:"node_name"`
	Ready           bool      `json:"ready"`
	RestartCount    int32     `json:"restart_count"`
	StartTime       *time.Time `json:"start_time,omitempty"`
	Labels          map[string]string `json:"labels,omitempty"`
	Containers      []ContainerStatus `json:"containers,omitempty"`
	ClusterID       int64     `json:"cluster_id"`
}

// ContainerStatus 容器状态摘要。
type ContainerStatus struct {
	Name         string `json:"name"`
	Ready        bool   `json:"ready"`
	RestartCount int32  `json:"restart_count"`
	State        string `json:"state"`
	// WaitingReason 容器处于 waiting 态时的原因（如 CrashLoopBackOff / ImagePullBackOff / CreateContainerError）。
	// 探针持续失败会触发 K8s 重启容器，最终进入 CrashLoopBackOff，是探针失败的核心信号。
	WaitingReason string `json:"waiting_reason,omitempty"`
	// LastTerminationReason 上一次终止原因（如 OOMKilled / Error / Completed）。
	LastTerminationReason string `json:"last_termination_reason,omitempty"`
}

// GroupRuntime 分组（工作负载）运行态摘要。
type GroupRuntime struct {
	GroupID         int64 `json:"group_id"`
	ClusterID       int64 `json:"cluster_id"`
	DesiredReplicas int32 `json:"desired_replicas"`
	ReadyReplicas   int32 `json:"ready_replicas"`
	UpdatedReplicas int32 `json:"updated_replicas"`
	AvailableReplicas int32 `json:"available_replicas"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// GroupResolver 从 K8s 资源反查 group_id（通过 namespace + deployment_name 等标签映射）。
// 实现由 application 层提供，避免 infrastructure 反向依赖 application。
type GroupResolver interface {
	// ResolveByWorkload 按集群、namespace、工作负载名解析 group_id 与 workload 类型。
	ResolveByWorkload(ctx context.Context, clusterID int64, namespace, name string) (groupID int64, ok bool)
}

// PodAnomalyEvent Pod 异常事件（由 InformerManager 检测后通过 PodAnomalyHook 回调）。
// 字段从 PodSummary + 分类结果派生，供通知器消费。
type PodAnomalyEvent struct {
	ClusterID   int64
	Namespace   string
	PodName     string
	NodeName    string
	Phase       string
	Reason      string // HighRestartCount / ContainerNotReady / Pending / Failed / ...
	Message     string
	// RestartCount 触发 HighRestartCount 时的总重启次数（供通知正文展示）。
	RestartCount int32
	// PodLabelInstance pod label "app.kubernetes.io/instance" 的值（用于反查 group）。
	PodLabelInstance string
	// Recovered true 表示 pod 从异常恢复为正常（用于清除冷却）。
	Recovered bool
}

// PodAnomalyHook Pod 异常事件回调。
// 由 clusteropsapp.PodAnomalyNotifier 实现（复用集群重启通知的应用成员解析 + 站内通知逻辑）。
// InformerManager 在 onPod 中检测到异常或恢复正常时调用此 hook，避免反向依赖 application 层。
type PodAnomalyHook interface {
	OnPodAnomaly(ctx context.Context, e PodAnomalyEvent)
}

// noopPodAnomalyHook 空实现（未注入 hook 时使用）。
type noopPodAnomalyHook struct{}

func (noopPodAnomalyHook) OnPodAnomaly(_ context.Context, _ PodAnomalyEvent) {}

// 异常 Pod 判定阈值：重启次数超过此值视为异常（与 clusteropsapp.abnormalRestartThreshold 一致）。
const podAbnormalRestartThreshold = 5

// classifyPodSummary 判定 PodSummary 是否异常，返回异常原因与消息。
// 镜像 clusteropsapp.classifyPod 的判定逻辑（不直接依赖 corev1.Pod，便于在 informer 内复用）。
func classifyPodSummary(s *PodSummary) (abnormal bool, reason, msg string) {
	if s == nil {
		return false, "", ""
	}
	// Pending/Failed/Unknown 视为异常；Running/Succeeded 视为正常 phase。
	if s.Phase != "Running" && s.Phase != "Succeeded" {
		return true, s.Phase, ""
	}
	// 容器级判定：CrashLoopBackOff（探针/容器持续失败）优先，其次高重启，最后未就绪。
	for _, c := range s.Containers {
		if c.WaitingReason == "CrashLoopBackOff" {
			return true, "ProbeFailed", fmt.Sprintf("container %s in CrashLoopBackOff (last termination: %s)", c.Name, orReason(c.LastTerminationReason, "unknown"))
		}
		if c.RestartCount >= int32(podAbnormalRestartThreshold) {
			reason = "HighRestartCount"
			msg = fmt.Sprintf("container %s restart count %d >= %d", c.Name, c.RestartCount, podAbnormalRestartThreshold)
			return true, reason, msg
		}
		if !c.Ready {
			return true, "ContainerNotReady", fmt.Sprintf("container %s not ready", c.Name)
		}
	}
	return false, "", ""
}

// orReason 返回非空原因，否则返回 def（用于消息展示）。
func orReason(r, def string) string {
	if r != "" {
		return r
	}
	return def
}

// InformerManager 管理单个集群的共享 Informer 工厂与事件处理。
// 按 namespace 分片（每个 syncer 实例只 Watch 自己负责的 namespace 子集）。
type InformerManager struct {
	clusterID   int64
	client      kubernetes.Interface
	cache       RuntimeCache
	resolver    GroupResolver
	namespaces  []string
	// podAnomalyHook Pod 异常事件回调（可选）。注入后在 onPod 中检测异常并回调。
	podAnomalyHook PodAnomalyHook
	// lastPodAbnormal 记录上次检测到的异常状态（pod name → 是否异常），用于检测"恢复"事件。
	lastPodAbnormal map[string]bool

	mu       sync.Mutex
	stopCh   chan struct{}
	factory  informers.SharedInformerFactory
	started  bool
}

// NewInformerManager 创建 Informer 管理器。
func NewInformerManager(clusterID int64, client kubernetes.Interface, cache RuntimeCache, resolver GroupResolver, namespaces []string) *InformerManager {
	return &InformerManager{
		clusterID:       clusterID,
		client:          client,
		cache:           cache,
		resolver:        resolver,
		namespaces:      namespaces,
		stopCh:          make(chan struct{}),
		podAnomalyHook:  noopPodAnomalyHook{},
		lastPodAbnormal: make(map[string]bool),
	}
}

// WithPodAnomalyHook 注入 Pod 异常事件回调（链式）。返回 receiver 便于 syncer 装配。
func (m *InformerManager) WithPodAnomalyHook(hook PodAnomalyHook) *InformerManager {
	if hook == nil {
		m.podAnomalyHook = noopPodAnomalyHook{}
	} else {
		m.podAnomalyHook = hook
	}
	return m
}

// Start 启动所有 Informer（阻塞直到 ctx 取消）。
// 使用 namespace-scoped SharedInformerFactory；若 namespaces 为空则 Watch 全集群。
func (m *InformerManager) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return fmt.Errorf("informer manager already started")
	}
	m.started = true
	m.mu.Unlock()

	// 默认 30s resync。
	opts := []informers.SharedInformerOption{}
	if len(m.namespaces) > 0 {
		// 多 namespace：为每个 namespace 各建一个 factory。
		for _, ns := range m.namespaces {
			if err := m.startNamespaceInformer(ctx, ns); err != nil {
				return err
			}
		}
	} else {
		m.factory = informers.NewSharedInformerFactoryWithOptions(m.client, 30*time.Second, opts...)
		m.registerHandlers(ctx, m.factory)
		m.factory.Start(m.stopCh)
	}

	<-ctx.Done()
	m.Stop()
	return nil
}

func (m *InformerManager) startNamespaceInformer(ctx context.Context, ns string) error {
	f := informers.NewSharedInformerFactoryWithOptions(m.client, 30*time.Second, informers.WithNamespace(ns))
	m.registerHandlers(ctx, f)
	f.Start(m.stopCh)
	return nil
}

func (m *InformerManager) registerHandlers(ctx context.Context, f informers.SharedInformerFactory) {
	// Pod
	podInformer := f.Core().V1().Pods().Informer()
	podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) { m.onPod(ctx, obj, false) },
		UpdateFunc: func(_, obj any) { m.onPod(ctx, obj, false) },
		DeleteFunc: func(obj any) { m.onPod(ctx, obj, true) },
	})
	go podInformer.Run(m.stopCh)

	// Deployment
	depInformer := f.Apps().V1().Deployments().Informer()
	depInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) { m.onDeployment(ctx, obj) },
		UpdateFunc: func(_, obj any) { m.onDeployment(ctx, obj) },
		DeleteFunc: func(obj any) { m.onDeploymentDelete(ctx, obj) },
	})
	go depInformer.Run(m.stopCh)

	// StatefulSet
	stsInformer := f.Apps().V1().StatefulSets().Informer()
	stsInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) { m.onStatefulSet(ctx, obj) },
		UpdateFunc: func(_, obj any) { m.onStatefulSet(ctx, obj) },
		DeleteFunc: func(obj any) { m.onStatefulSetDelete(ctx, obj) },
	})
	go stsInformer.Run(m.stopCh)

	// Event：订阅 K8s 事件，捕获探针 Unhealthy / BackOff / Failed 等早期信号，
	// 转成 PodAnomalyEvent 经 hook 推送（通知器侧 Redis 冷却去重，避免风暴）。
	// 仅处理 involvedObject.Kind==Pod 且 reason 命中关键字的事件，不长期缓存事件全量。
	eventInformer := f.Core().V1().Events().Informer()
	eventInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) { m.onEvent(ctx, obj) },
		UpdateFunc: func(_, obj any) { m.onEvent(ctx, obj) },
	})
	go eventInformer.Run(m.stopCh)
}

func (m *InformerManager) onPod(ctx context.Context, obj any, deleted bool) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return
	}
	if deleted {
		_ = m.cache.DeletePod(ctx, m.clusterID, pod.Namespace, pod.Name)
		// Pod 删除：若之前记录为异常，触发恢复事件清除冷却。
		if m.podAnomalyHook != nil {
			if wasAbnormal := m.lastPodAbnormal[pod.Name]; wasAbnormal {
				delete(m.lastPodAbnormal, pod.Name)
				m.podAnomalyHook.OnPodAnomaly(ctx, PodAnomalyEvent{
					ClusterID:        m.clusterID,
					Namespace:        pod.Namespace,
					PodName:          pod.Name,
					NodeName:         pod.Spec.NodeName,
					Phase:            string(pod.Status.Phase),
					Reason:           "PodDeleted",
					PodLabelInstance: pod.Labels["app.kubernetes.io/instance"],
					Recovered:        true,
				})
			}
		}
		return
	}
	summary := podToSummary(m.clusterID, pod)
	_ = m.cache.UpsertPod(ctx, m.clusterID, pod.Namespace, summary)
	// 异常检测 + hook 回调。
	if m.podAnomalyHook != nil {
		m.detectAndNotifyAnomaly(ctx, summary)
	}
}

// detectAndNotifyAnomaly 检测 Pod 是否异常，状态变化时触发 hook。
// 异常 → 触发异常事件；从异常恢复为正常 → 触发恢复事件（清除冷却）。
func (m *InformerManager) detectAndNotifyAnomaly(ctx context.Context, s *PodSummary) {
	abnormal, reason, msg := classifyPodSummary(s)
	wasAbnormal := m.lastPodAbnormal[s.Name]
	if abnormal {
		if !wasAbnormal {
			// 新增异常：触发通知。
			m.lastPodAbnormal[s.Name] = true
		}
		// 即便持续异常也触发（通知器侧冷却去重，不会风暴）。
		m.podAnomalyHook.OnPodAnomaly(ctx, PodAnomalyEvent{
			ClusterID:        m.clusterID,
			Namespace:        s.Namespace,
			PodName:          s.Name,
			NodeName:         s.NodeName,
			Phase:            s.Phase,
			Reason:           reason,
			Message:          msg,
			RestartCount:     s.RestartCount,
			PodLabelInstance: s.Labels["app.kubernetes.io/instance"],
			Recovered:        false,
		})
	} else if wasAbnormal {
		// 从异常恢复正常：清除冷却。
		delete(m.lastPodAbnormal, s.Name)
		m.podAnomalyHook.OnPodAnomaly(ctx, PodAnomalyEvent{
			ClusterID:        m.clusterID,
			Namespace:        s.Namespace,
			PodName:          s.Name,
			NodeName:         s.NodeName,
			Phase:            s.Phase,
			PodLabelInstance: s.Labels["app.kubernetes.io/instance"],
			Recovered:        true,
		})
	}
}

func (m *InformerManager) onDeployment(ctx context.Context, obj any) {
	dep, ok := obj.(*appsv1.Deployment)
	if !ok {
		return
	}
	m.upsertGroupRuntime(ctx, dep.Namespace, dep.Name, dep.Status.Replicas, dep.Status.ReadyReplicas,
		dep.Status.UpdatedReplicas, dep.Status.AvailableReplicas)
}

func (m *InformerManager) onDeploymentDelete(ctx context.Context, obj any) {
	dep, ok := obj.(*appsv1.Deployment)
	if !ok {
		return
	}
	if m.resolver == nil {
		return
	}
	if gid, ok := m.resolver.ResolveByWorkload(ctx, m.clusterID, dep.Namespace, dep.Name); ok {
		_ = m.cache.DeleteGroupRuntime(ctx, m.clusterID, gid)
	}
}

func (m *InformerManager) onStatefulSet(ctx context.Context, obj any) {
	sts, ok := obj.(*appsv1.StatefulSet)
	if !ok {
		return
	}
	m.upsertGroupRuntime(ctx, sts.Namespace, sts.Name, sts.Status.Replicas, sts.Status.ReadyReplicas,
		sts.Status.UpdatedReplicas, sts.Status.AvailableReplicas)
}

func (m *InformerManager) onStatefulSetDelete(ctx context.Context, obj any) {
	sts, ok := obj.(*appsv1.StatefulSet)
	if !ok {
		return
	}
	if m.resolver == nil {
		return
	}
	if gid, ok := m.resolver.ResolveByWorkload(ctx, m.clusterID, sts.Namespace, sts.Name); ok {
		_ = m.cache.DeleteGroupRuntime(ctx, m.clusterID, gid)
	}
}

// abnormalEventReasons 命中即视为异常事件的 K8s Event reason 关键字（小写匹配）。
// 涵盖探针失败（Unhealthy/ProbeWarning）、容器反复重启（BackOff/Failed）、调度失败（FailedScheduling）等。
var abnormalEventReasons = map[string]bool{
	"unhealthy":       true, // Liveness/Readiness 探针失败
	"probewarning":    true,
	"backoff":         true, // CrashLoopBackOff / ImagePullBackOff
	"failed":          true,
	"failedscheduling": true,
	"evicted":         true,
	"oomkilled":       true,
}

// onEvent 处理 K8s Event：仅处理涉及 Pod 的异常事件，转成 PodAnomalyEvent 经 hook 推送。
// 通知器侧 Redis 冷却去重保证同一 pod 同一 reason 在窗口内只通知一次。
func (m *InformerManager) onEvent(ctx context.Context, obj any) {
	if m.podAnomalyHook == nil {
		return
	}
	ev, ok := obj.(*corev1.Event)
	if !ok {
		return
	}
	// 仅关注 Pod 相关事件。
	if ev.InvolvedObject.Kind != "Pod" || ev.InvolvedObject.Name == "" {
		return
	}
	reason := strings.ToLower(ev.Reason)
	if !abnormalEventReasons[reason] {
		return
	}
	m.podAnomalyHook.OnPodAnomaly(ctx, PodAnomalyEvent{
		ClusterID:        m.clusterID,
		Namespace:        ev.InvolvedObject.Namespace,
		PodName:          ev.InvolvedObject.Name,
		Phase:            "", // 事件不携带 phase，由 Pod informer 另行维护
		Reason:           "KubernetesEvent",
		Message:          fmt.Sprintf("%s: %s", ev.Reason, ev.Message),
		PodLabelInstance: "", // 事件不携带 pod label，由通知器侧按 pod 名兜底解析
		Recovered:        false,
	})
}

func (m *InformerManager) upsertGroupRuntime(ctx context.Context, ns, name string, desired, ready, updated, available int32) {
	if m.resolver == nil {
		return
	}
	gid, ok := m.resolver.ResolveByWorkload(ctx, m.clusterID, ns, name)
	if !ok {
		return
	}
	_ = m.cache.UpsertGroupRuntime(ctx, m.clusterID, gid, &GroupRuntime{
		GroupID:           gid,
		ClusterID:         m.clusterID,
		DesiredReplicas:   desired,
		ReadyReplicas:     ready,
		UpdatedReplicas:   updated,
		AvailableReplicas: available,
		UpdatedAt:         time.Now(),
	})
}

// Stop 停止所有 Informer。
func (m *InformerManager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.started {
		return
	}
	close(m.stopCh)
	m.started = false
}

func podToSummary(clusterID int64, pod *corev1.Pod) *PodSummary {
	s := &PodSummary{
		Name:      pod.Name,
		Namespace: pod.Namespace,
		UID:       string(pod.UID),
		Phase:     string(pod.Status.Phase),
		PodIP:     pod.Status.PodIP,
		HostIP:    pod.Status.HostIP,
		NodeName:  pod.Spec.NodeName,
		Labels:    pod.Labels,
		ClusterID: clusterID,
	}
	if pod.Status.StartTime != nil {
		t := pod.Status.StartTime.Time
		s.StartTime = &t
	}
	ready := true
	var restarts int32
	s.Containers = make([]ContainerStatus, 0, len(pod.Status.ContainerStatuses))
	for _, cs := range pod.Status.ContainerStatuses {
		state := "waiting"
		var waitingReason, lastTermReason string
		if cs.State.Running != nil {
			state = "running"
		} else if cs.State.Terminated != nil {
			state = "terminated"
		} else if cs.State.Waiting != nil {
			waitingReason = cs.State.Waiting.Reason
		}
		if cs.LastTerminationState.Terminated != nil {
			lastTermReason = cs.LastTerminationState.Terminated.Reason
		}
		s.Containers = append(s.Containers, ContainerStatus{
			Name:                  cs.Name,
			Ready:                 cs.Ready,
			RestartCount:          cs.RestartCount,
			State:                 state,
			WaitingReason:         waitingReason,
			LastTerminationReason: lastTermReason,
		})
		if !cs.Ready {
			ready = false
		}
		restarts += cs.RestartCount
	}
	s.Ready = ready
	s.RestartCount = restarts
	return s
}

// ListPodsFromInformer 从 informer 缓存列出 Pod（供 apiserver 读运行态兜底）。
// 注意：仅在该 syncer 实例持有 informer 时可用；跨实例读应走 Redis 缓存。
func ListPodsFromInformer(f informers.SharedInformerFactory, namespace string, sel labels.Selector) ([]*corev1.Pod, error) {
	lister := f.Core().V1().Pods().Lister()
	var pods []*corev1.Pod
	var err error
	if namespace == "" {
		pods, err = lister.List(sel)
	} else {
		pods, err = lister.Pods(namespace).List(sel)
	}
	if err != nil {
		return nil, err
	}
	return pods, nil
}

// HPAInformer 可选：监听 HPA 当前副本数（Phase 9 推理 HPA 用）。
type HPAInformer struct {
	client    kubernetes.Interface
	namespace string
}

// NewHPAInformer 创建 HPA 监听器。
func NewHPAInformer(client kubernetes.Interface, namespace string) *HPAInformer {
	return &HPAInformer{client: client, namespace: namespace}
}

// List 列出 HPA（一次性查询，非 informer；用于 reconcile 周期校正）。
func (h *HPAInformer) List(ctx context.Context) ([]autoscalingv2.HorizontalPodAutoscaler, error) {
	list, err := h.client.AutoscalingV2().HorizontalPodAutoscalers(h.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}
