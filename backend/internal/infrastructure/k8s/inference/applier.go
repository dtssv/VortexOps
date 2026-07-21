package inference

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Applier 把 RenderResult 应用到 K8s 集群（create-or-update）。
type Applier struct {
	client kubernetes.Interface
}

// NewApplier 创建应用器。
func NewApplier(client kubernetes.Interface) *Applier {
	return &Applier{client: client}
}

// Apply 应用 Deployment/Service/HPA/Ingress/ExternalService。
func (a *Applier) Apply(ctx context.Context, result *RenderResult) error {
	if result == nil {
		return fmt.Errorf("render result is nil")
	}
	if result.Deployment != nil {
		if err := a.applyDeployment(ctx, result.Deployment); err != nil {
			return err
		}
	}
	if result.Service != nil {
		if err := a.applyService(ctx, result.Service); err != nil {
			return err
		}
	}
	if result.HPA != nil {
		if err := a.applyHPA(ctx, result.HPA); err != nil {
			return err
		}
	}
	if result.ExternalService != nil {
		if err := a.ApplyLoadBalancerService(ctx, result.ExternalService); err != nil {
			return err
		}
	}
	if result.Ingress != nil {
		if err := a.ApplyIngress(ctx, result.Ingress); err != nil {
			return err
		}
	}
	return nil
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

func (a *Applier) applyService(ctx context.Context, svc *corev1.Service) error {
	existing, err := a.client.CoreV1().Services(svc.Namespace).Get(ctx, svc.Name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			_, err := a.client.CoreV1().Services(svc.Namespace).Create(ctx, svc, metav1.CreateOptions{})
			return err
		}
		return err
	}
	svc.ResourceVersion = existing.ResourceVersion
	svc.Spec.ClusterIP = existing.Spec.ClusterIP
	svc.Spec.ClusterIPs = existing.Spec.ClusterIPs
	_, err = a.client.CoreV1().Services(svc.Namespace).Update(ctx, svc, metav1.UpdateOptions{})
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

// ScaleDeployment 直接调整 Deployment 副本数。
func (a *Applier) ScaleDeployment(ctx context.Context, namespace, name string, replicas int32) error {
	d, err := a.client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	d.Spec.Replicas = &replicas
	_, err = a.client.AppsV1().Deployments(namespace).Update(ctx, d, metav1.UpdateOptions{})
	return err
}

// ApplyJob 应用一个 Job（create-or-replace：若已存在则先删除再创建）。
func (a *Applier) ApplyJob(ctx context.Context, job *batchv1.Job) error {
	if job == nil {
		return fmt.Errorf("job is nil")
	}
	existing, err := a.client.BatchV1().Jobs(job.Namespace).Get(ctx, job.Name, metav1.GetOptions{})
	if err == nil {
		// 已存在：若已完成则删除后重建，否则跳过。
		if existing.Status.Succeeded > 0 || existing.Status.Failed >= *existing.Spec.BackoffLimit {
			prop := metav1.DeletePropagationBackground
			_ = a.client.BatchV1().Jobs(job.Namespace).Delete(ctx, job.Name, metav1.DeleteOptions{PropagationPolicy: &prop})
		} else {
			return nil
		}
	}
	_, err = a.client.BatchV1().Jobs(job.Namespace).Create(ctx, job, metav1.CreateOptions{})
	return err
}

// GetJob 查询 Job 状态（用于下载进度追踪）。
func (a *Applier) GetJob(ctx context.Context, namespace, name string) (*batchv1.Job, error) {
	return a.client.BatchV1().Jobs(namespace).Get(ctx, name, metav1.GetOptions{})
}

// GetDeployment 查询 Deployment（用于状态同步）。
func (a *Applier) GetDeployment(ctx context.Context, namespace, name string) (*appsv1.Deployment, error) {
	return a.client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
}

// CreateIngress 应用一个 Ingress（create-or-update）。
func (a *Applier) ApplyIngress(ctx context.Context, ing *networkingv1.Ingress) error {
	if ing == nil {
		return nil
	}
	_, err := a.client.NetworkingV1().Ingresses(ing.Namespace).Get(ctx, ing.Name, metav1.GetOptions{})
	if err != nil {
		_, err = a.client.NetworkingV1().Ingresses(ing.Namespace).Create(ctx, ing, metav1.CreateOptions{})
		return err
	}
	_, err = a.client.NetworkingV1().Ingresses(ing.Namespace).Update(ctx, ing, metav1.UpdateOptions{})
	return err
}

// CreateLoadBalancerService 创建一个 LoadBalancer Service（create-or-update）。
func (a *Applier) ApplyLoadBalancerService(ctx context.Context, svc *corev1.Service) error {
	existing, err := a.client.CoreV1().Services(svc.Namespace).Get(ctx, svc.Name, metav1.GetOptions{})
	if err != nil {
		_, err = a.client.CoreV1().Services(svc.Namespace).Create(ctx, svc, metav1.CreateOptions{})
		return err
	}
	svc.ResourceVersion = existing.ResourceVersion
	svc.Spec.ClusterIP = existing.Spec.ClusterIP
	svc.Spec.ClusterIPs = existing.Spec.ClusterIPs
	_, err = a.client.CoreV1().Services(svc.Namespace).Update(ctx, svc, metav1.UpdateOptions{})
	return err
}

// GetLoadBalancerIngress 获取 LoadBalancer Service 的外部地址。
func (a *Applier) GetLoadBalancerIngress(ctx context.Context, namespace, name string) (string, error) {
	svc, err := a.client.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	for _, ing := range svc.Status.LoadBalancer.Ingress {
		if ing.Hostname != "" {
			return ing.Hostname, nil
		}
		if ing.IP != "" {
			return ing.IP, nil
		}
	}
	return "", nil
}
