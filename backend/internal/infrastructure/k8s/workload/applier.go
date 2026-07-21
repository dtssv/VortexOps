package workload

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	ciliuminfra "github.com/vortexops/vortexops/internal/infrastructure/k8s/cilium"
)

// Applier 把 RenderResult 应用到 K8s 集群（Server-Side Apply 语义：create-or-update）。
type Applier struct {
	client  kubernetes.Interface
	dynamic dynamic.Interface // 可选：应用 Cilium/Mesh CRD
}

// NewApplier 创建应用器。
func NewApplier(client kubernetes.Interface) *Applier {
	return &Applier{client: client}
}

// WithDynamic 注入 dynamic client（Cilium/Mesh CRD apply 需要）。
func (a *Applier) WithDynamic(dyn dynamic.Interface) *Applier {
	a.dynamic = dyn
	return a
}

// Apply 应用渲染结果到指定 namespace。
// 采用「create or update」语义：不存在则创建，存在则覆盖 Spec（保留运行态 Status）。
// 应用顺序：ConfigMap → Workload → 附属资源。ConfigMap 必须先于 Deployment，
// 否则 Pod 引用不存在的 ConfigMap 会启动失败。
func (a *Applier) Apply(ctx context.Context, result *RenderResult) error {
	if result == nil {
		return fmt.Errorf("render result is nil")
	}
	// ConfigMap（分组配置文件）必须在 Workload 之前 apply。
	if result.ConfigMap != nil {
		if err := a.applyConfigMap(ctx, result.ConfigMap); err != nil {
			return err
		}
	}
	// 主工作负载
	switch w := result.Workload.(type) {
	case *appsv1.Deployment:
		if err := a.applyDeployment(ctx, w); err != nil {
			return err
		}
	case *appsv1.StatefulSet:
		if err := a.applyStatefulSet(ctx, w); err != nil {
			return err
		}
	case *batchv1.CronJob:
		if err := a.applyCronJob(ctx, w); err != nil {
			return err
		}
	case *batchv1.Job:
		if err := a.applyJob(ctx, w); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported workload type: %T", w)
	}
	// 候选工作负载（多版本共存：candidate Deployment）
	if result.CandidateWorkload != nil {
		if cand, ok := result.CandidateWorkload.(*appsv1.Deployment); ok {
			if err := a.applyDeployment(ctx, cand); err != nil {
				return err
			}
		}
	}
	// HPA
	if result.HPA != nil {
		if err := a.applyHPA(ctx, result.HPA); err != nil {
			return err
		}
	}
	// Cilium eBPF L4 LB（软降级：CRD 未安装时不阻断）。
	if len(result.CiliumResources) > 0 && a.dynamic != nil {
		ciliumApplier := ciliuminfra.NewApplier(a.dynamic)
		if err := ciliumApplier.Apply(ctx, result.CiliumResources...); err != nil {
			return fmt.Errorf("apply cilium resources: %w", err)
		}
	}
	// Mesh CRD（软降级）。
	if len(result.MeshResources) > 0 && a.dynamic != nil {
		ciliumApplier := ciliuminfra.NewApplier(a.dynamic)
		if err := ciliumApplier.Apply(ctx, toUnstructuredSlice(result.MeshResources)...); err != nil {
			return fmt.Errorf("apply mesh resources: %w", err)
		}
	}
	return nil
}

func toUnstructuredSlice(objs []*unstructured.Unstructured) []*unstructured.Unstructured {
	return objs
}

func (a *Applier) applyDeployment(ctx context.Context, d *appsv1.Deployment) error {
	existing, err := a.client.AppsV1().Deployments(d.Namespace).Get(ctx, d.Name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			_, err := a.client.AppsV1().Deployments(d.Namespace).Create(ctx, d, metav1.CreateOptions{})
			return err
		}
		return err
	}
	d.ResourceVersion = existing.ResourceVersion
	_, err = a.client.AppsV1().Deployments(d.Namespace).Update(ctx, d, metav1.UpdateOptions{})
	return err
}

func (a *Applier) applyStatefulSet(ctx context.Context, s *appsv1.StatefulSet) error {
	existing, err := a.client.AppsV1().StatefulSets(s.Namespace).Get(ctx, s.Name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			_, err := a.client.AppsV1().StatefulSets(s.Namespace).Create(ctx, s, metav1.CreateOptions{})
			return err
		}
		return err
	}
	s.ResourceVersion = existing.ResourceVersion
	_, err = a.client.AppsV1().StatefulSets(s.Namespace).Update(ctx, s, metav1.UpdateOptions{})
	return err
}

func (a *Applier) applyCronJob(ctx context.Context, c *batchv1.CronJob) error {
	existing, err := a.client.BatchV1().CronJobs(c.Namespace).Get(ctx, c.Name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			_, err := a.client.BatchV1().CronJobs(c.Namespace).Create(ctx, c, metav1.CreateOptions{})
			return err
		}
		return err
	}
	c.ResourceVersion = existing.ResourceVersion
	_, err = a.client.BatchV1().CronJobs(c.Namespace).Update(ctx, c, metav1.UpdateOptions{})
	return err
}

func (a *Applier) applyJob(ctx context.Context, j *batchv1.Job) error {
	_, err := a.client.BatchV1().Jobs(j.Namespace).Create(ctx, j, metav1.CreateOptions{})
	if err != nil && apierrors.IsAlreadyExists(err) {
		// Job 不可更新（Spec 不可变）；已存在则视为成功。
		return nil
	}
	return err
}

// applyConfigMap 创建或更新 ConfigMap（分组配置文件）。
// create-or-update：不存在则创建，存在则整体覆盖 Data（保留 ResourceVersion）。
func (a *Applier) applyConfigMap(ctx context.Context, cm *corev1.ConfigMap) error {
	existing, err := a.client.CoreV1().ConfigMaps(cm.Namespace).Get(ctx, cm.Name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			_, err := a.client.CoreV1().ConfigMaps(cm.Namespace).Create(ctx, cm, metav1.CreateOptions{})
			return err
		}
		return err
	}
	cm.ResourceVersion = existing.ResourceVersion
	_, err = a.client.CoreV1().ConfigMaps(cm.Namespace).Update(ctx, cm, metav1.UpdateOptions{})
	return err
}

func (a *Applier) applyHPA(ctx context.Context, hpa *autoscalingv2.HorizontalPodAutoscaler) error {
	existing, err := a.client.AutoscalingV2().HorizontalPodAutoscalers(hpa.Namespace).Get(ctx, hpa.Name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			_, err := a.client.AutoscalingV2().HorizontalPodAutoscalers(hpa.Namespace).Create(ctx, hpa, metav1.CreateOptions{})
			return err
		}
		return err
	}
	hpa.ResourceVersion = existing.ResourceVersion
	_, err = a.client.AutoscalingV2().HorizontalPodAutoscalers(hpa.Namespace).Update(ctx, hpa, metav1.UpdateOptions{})
	return err
}

// DeleteDeployment 删除指定 Deployment（用于候选晋升后删旧主，或回滚删候选）。
// 幂等：不存在视为成功。
func (a *Applier) DeleteDeployment(ctx context.Context, namespace, name string) error {
	propBackground := metav1.DeletePropagationBackground
	err := a.client.AppsV1().Deployments(namespace).Delete(ctx, name, metav1.DeleteOptions{PropagationPolicy: &propBackground})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

// Delete 删除工作负载及附属资源（用于 group 删除或彻底回滚）。
func (a *Applier) Delete(ctx context.Context, namespace, name string, workloadType string) error {
	propBackground := metav1.DeletePropagationBackground
	delOpts := metav1.DeleteOptions{PropagationPolicy: &propBackground}

	switch workloadType {
	case "deployment":
		if err := a.client.AppsV1().Deployments(namespace).Delete(ctx, name, delOpts); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	case "statefulset":
		if err := a.client.AppsV1().StatefulSets(namespace).Delete(ctx, name, delOpts); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	case "cronjob":
		if err := a.client.BatchV1().CronJobs(namespace).Delete(ctx, name, delOpts); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	case "job":
		if err := a.client.BatchV1().Jobs(namespace).Delete(ctx, name, delOpts); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	// 清理附属资源（忽略 not found）。
	_ = a.client.CoreV1().Services(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	_ = a.client.AutoscalingV2().HorizontalPodAutoscalers(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	_ = a.client.NetworkingV1().NetworkPolicies(namespace).Delete(ctx, name+"-netpol", metav1.DeleteOptions{})
	_ = a.client.NetworkingV1().Ingresses(namespace).Delete(ctx, name+"-ingress", metav1.DeleteOptions{})
	return nil
}
