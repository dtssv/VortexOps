// Package k8sapp 提供 K8s 通用资源运维应用服务。
// 通过 clusterapp 获取目标集群的 clientset，执行节点/工作负载/存储/网络/配置/事件的只读查询与运维操作。
package k8sapp

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	"github.com/vortexops/vortexops/internal/application/clusterapp"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// Service K8s 运维应用服务。
type Service struct {
	clusters *clusterapp.Service
}

// New 创建 K8s 运维服务。
func New(clusters *clusterapp.Service) *Service {
	return &Service{clusters: clusters}
}

// client 获取目标集群的 clientset。
func (s *Service) client(ctx context.Context, clusterID int64) (kubernetes.Interface, error) {
	c, err := s.clusters.GetClient(ctx, clusterID)
	if err != nil {
		return nil, apperr.Internal("get cluster client", err)
	}
	return c, nil
}

// ============================================================================
// 节点 (Node)
// ============================================================================

// ListNodes 列出集群节点。
func (s *Service) ListNodes(ctx context.Context, clusterID int64) ([]corev1.Node, error) {
	c, err := s.client(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	list, err := c.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, apperr.Internal("list nodes", err)
	}
	return list.Items, nil
}

// CordonNode 设置节点为不可调度。
func (s *Service) CordonNode(ctx context.Context, clusterID int64, nodeName string) error {
	return s.patchNodeUnschedulable(ctx, clusterID, nodeName, true)
}

// UncordonNode 设置节点为可调度。
func (s *Service) UncordonNode(ctx context.Context, clusterID int64, nodeName string) error {
	return s.patchNodeUnschedulable(ctx, clusterID, nodeName, false)
}

// DrainNode 驱逐节点上的所有 Pod（忽略 DaemonSet）。简化实现：逐个 evict。
func (s *Service) DrainNode(ctx context.Context, clusterID int64, nodeName string) error {
	c, err := s.client(ctx, clusterID)
	if err != nil {
		return err
	}
	// 先 cordon
	if err := s.patchNodeUnschedulable(ctx, clusterID, nodeName, true); err != nil {
		return err
	}
	// 列出节点上所有 Pod（排除 DaemonSet 管理的）
	pods, err := c.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("spec.nodeName=%s,status.phase=Running", nodeName),
	})
	if err != nil {
		return apperr.Internal("list pods on node", err)
	}
	for _, pod := range pods.Items {
		if isDaemonSetPod(&pod) || isStaticPod(&pod) {
			continue
		}
		_ = c.CoreV1().Pods(pod.Namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{
			GracePeriodSeconds: ptr(int64(30)),
		})
	}
	return nil
}

func (s *Service) patchNodeUnschedulable(ctx context.Context, clusterID int64, nodeName string, unschedulable bool) error {
	c, err := s.client(ctx, clusterID)
	if err != nil {
		return err
	}
	patch := fmt.Sprintf(`{"spec":{"unschedulable":%t}}`, unschedulable)
	_, err = c.CoreV1().Nodes().Patch(ctx, nodeName, types.MergePatchType, []byte(patch), metav1.PatchOptions{})
	if err != nil {
		return apperr.Internal("patch node unschedulable", err)
	}
	return nil
}

// ============================================================================
// 工作负载 (Deployments / StatefulSets / DaemonSets / Pods)
// ============================================================================

// ListDeployments 列出指定命名空间的 Deployment（namespace 为空时查全集群）。
func (s *Service) ListDeployments(ctx context.Context, clusterID int64, namespace string) ([]any, error) {
	c, err := s.client(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	list, err := c.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, apperr.Internal("list deployments", err)
	}
	return toAnySlice(list.Items), nil
}

// ScaleDeployment 调整 Deployment 副本数。
func (s *Service) ScaleDeployment(ctx context.Context, clusterID int64, namespace, name string, replicas int32) error {
	c, err := s.client(ctx, clusterID)
	if err != nil {
		return err
	}
	scale, err := c.AppsV1().Deployments(namespace).GetScale(ctx, name, metav1.GetOptions{})
	if err != nil {
		return apperr.Internal("get deployment scale", err)
	}
	scale.Spec.Replicas = replicas
	_, err = c.AppsV1().Deployments(namespace).UpdateScale(ctx, name, scale, metav1.UpdateOptions{})
	if err != nil {
		return apperr.Internal("update deployment scale", err)
	}
	return nil
}

// ListStatefulSets 列出 StatefulSet。
func (s *Service) ListStatefulSets(ctx context.Context, clusterID int64, namespace string) ([]any, error) {
	c, err := s.client(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	list, err := c.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, apperr.Internal("list statefulsets", err)
	}
	return toAnySlice(list.Items), nil
}

// ScaleStatefulSet 调整 StatefulSet 副本数。
func (s *Service) ScaleStatefulSet(ctx context.Context, clusterID int64, namespace, name string, replicas int32) error {
	c, err := s.client(ctx, clusterID)
	if err != nil {
		return err
	}
	scale, err := c.AppsV1().StatefulSets(namespace).GetScale(ctx, name, metav1.GetOptions{})
	if err != nil {
		return apperr.Internal("get statefulset scale", err)
	}
	scale.Spec.Replicas = replicas
	_, err = c.AppsV1().StatefulSets(namespace).UpdateScale(ctx, name, scale, metav1.UpdateOptions{})
	if err != nil {
		return apperr.Internal("update statefulset scale", err)
	}
	return nil
}

// ListDaemonSets 列出 DaemonSet。
func (s *Service) ListDaemonSets(ctx context.Context, clusterID int64, namespace string) ([]any, error) {
	c, err := s.client(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	list, err := c.AppsV1().DaemonSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, apperr.Internal("list daemonsets", err)
	}
	return toAnySlice(list.Items), nil
}

// ListPods 列出 Pod。
func (s *Service) ListPods(ctx context.Context, clusterID int64, namespace string, fieldSelector string) ([]corev1.Pod, error) {
	c, err := s.client(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	list, err := c.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{FieldSelector: fieldSelector})
	if err != nil {
		return nil, apperr.Internal("list pods", err)
	}
	return list.Items, nil
}

// DeletePod 删除单个 Pod（由控制器重建）。
func (s *Service) DeletePod(ctx context.Context, clusterID int64, namespace, name string) error {
	c, err := s.client(ctx, clusterID)
	if err != nil {
		return err
	}
	if err := c.CoreV1().Pods(namespace).Delete(ctx, name, metav1.DeleteOptions{GracePeriodSeconds: ptr(int64(10))}); err != nil {
		return apperr.Internal("delete pod", err)
	}
	return nil
}

// ListGroupPodNames 列出分组（按 labelSelector）下所有 Pod 名（机器运维用）。
func (s *Service) ListGroupPodNames(ctx context.Context, clusterID int64, namespace, labelSelector string) ([]string, error) {
	c, err := s.client(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	list, err := c.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		return nil, apperr.Internal("list pods", err)
	}
	names := make([]string, 0, len(list.Items))
	for _, p := range list.Items {
		names = append(names, p.Name)
	}
	return names, nil
}

// ============================================================================
// 存储 (PersistentVolume / PersistentVolumeClaim / StorageClass)
// ============================================================================

// ListPersistentVolumes 列出 PV（集群级资源）。
func (s *Service) ListPersistentVolumes(ctx context.Context, clusterID int64) ([]any, error) {
	c, err := s.client(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	list, err := c.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, apperr.Internal("list persistent volumes", err)
	}
	return toAnySlice(list.Items), nil
}

// ListPersistentVolumeClaims 列出 PVC。
func (s *Service) ListPersistentVolumeClaims(ctx context.Context, clusterID int64, namespace string) ([]any, error) {
	c, err := s.client(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	list, err := c.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, apperr.Internal("list persistent volume claims", err)
	}
	return toAnySlice(list.Items), nil
}

// ListStorageClasses 列出 StorageClass（集群级资源）。
func (s *Service) ListStorageClasses(ctx context.Context, clusterID int64) ([]any, error) {
	c, err := s.client(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	list, err := c.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, apperr.Internal("list storage classes", err)
	}
	return toAnySlice(list.Items), nil
}

// ============================================================================
// 网络 (Service / Ingress / NetworkPolicy)
// ============================================================================

// ListServices 列出 Service。
func (s *Service) ListServices(ctx context.Context, clusterID int64, namespace string) ([]any, error) {
	c, err := s.client(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	list, err := c.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, apperr.Internal("list services", err)
	}
	return toAnySlice(list.Items), nil
}

// ListIngresses 列出 Ingress。
func (s *Service) ListIngresses(ctx context.Context, clusterID int64, namespace string) ([]networkingv1.Ingress, error) {
	c, err := s.client(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	list, err := c.NetworkingV1().Ingresses(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, apperr.Internal("list ingresses", err)
	}
	return list.Items, nil
}

// ListNetworkPolicies 列出 NetworkPolicy。
func (s *Service) ListNetworkPolicies(ctx context.Context, clusterID int64, namespace string) ([]any, error) {
	c, err := s.client(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	list, err := c.NetworkingV1().NetworkPolicies(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, apperr.Internal("list network policies", err)
	}
	return toAnySlice(list.Items), nil
}

// ============================================================================
// 配置 (ConfigMap / Secret)
// ============================================================================

// ListConfigMaps 列出 ConfigMap（不返回 data 内容以减小响应体）。
func (s *Service) ListConfigMaps(ctx context.Context, clusterID int64, namespace string) ([]any, error) {
	c, err := s.client(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	list, err := c.CoreV1().ConfigMaps(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, apperr.Internal("list configmaps", err)
	}
	return toAnySlice(list.Items), nil
}

// ListSecrets 列出 Secret（仅返回元数据与类型，不返回 data）。
func (s *Service) ListSecrets(ctx context.Context, clusterID int64, namespace string) ([]any, error) {
	c, err := s.client(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	list, err := c.CoreV1().Secrets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, apperr.Internal("list secrets", err)
	}
	// 脱敏：清空 data，仅保留元数据与类型。
	for i := range list.Items {
		list.Items[i].Data = nil
		list.Items[i].StringData = nil
	}
	return toAnySlice(list.Items), nil
}

// ============================================================================
// 事件 (Event)
// ============================================================================

// ListEvents 列出 Event（按时间倒序）。
func (s *Service) ListEvents(ctx context.Context, clusterID int64, namespace string, fieldSelector string) ([]corev1.Event, error) {
	c, err := s.client(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	list, err := c.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{
		FieldSelector: fieldSelector,
	})
	if err != nil {
		return nil, apperr.Internal("list events", err)
	}
	return list.Items, nil
}

// ============================================================================
// 辅助函数
// ============================================================================

func toAnySlice[T any](items []T) []any {
	out := make([]any, 0, len(items))
	for _, item := range items {
		out = append(out, item)
	}
	return out
}

func ptr[T any](v T) *T { return &v }

func isDaemonSetPod(pod *corev1.Pod) bool {
	for _, owner := range pod.OwnerReferences {
		if owner.Kind == "DaemonSet" {
			return true
		}
	}
	return false
}

func isStaticPod(pod *corev1.Pod) bool {
	for _, owner := range pod.OwnerReferences {
		if owner.Kind == "Node" && owner.Controller != nil && *owner.Controller {
			return true
		}
	}
	return false
}
