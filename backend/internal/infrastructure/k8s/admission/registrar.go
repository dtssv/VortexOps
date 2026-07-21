// Package admission 的 registrar 负责向每个 active 集群注册 MutatingWebhookConfiguration。
//
// 注册逻辑：
//   - 启动时遍历所有 active 集群，用 ClientPool 获取 clientset。
//   - CreateOrUpdate MutatingWebhookConfiguration（名称 vortexops-pod-ip-webhook）：
//     - clientConfig.url = webhook 服务地址（如 https://webhook:8443/mutate）。
//     - clientConfig.caBundle = TLS Bundle 的 CA bundle PEM。
//     - namespaceSelector 排除 kube-system（避免系统 Pod 被注入）。
//     - rules: apiGroups=[""], resources=["pods"], verbs=["create"]。
//   - 周期 reconcile：CA bundle 轮换或配置丢失时自动修复。
//
// 幂等：用 server-side apply 风格的 CreateOrUpdate（先 Get，存在则 Update，不存在则 Create）。
package admission

import (
	"context"
	"fmt"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/vortexops/vortexops/internal/platform/logger"
)

// WebhookName 注册到集群的 MutatingWebhookConfiguration 名称。
const WebhookName = "vortexops-pod-ip-webhook"

// Registrar 负责 MutatingWebhookConfiguration 的注册与维护。
type Registrar struct {
	// webhookURL kube-apiserver 访问 webhook 的完整 URL（如 https://webhook:8443/mutate）。
	webhookURL string
	// caBundlePEM 推送到 clientConfig.caBundle 的 CA bundle（PEM）。
	caBundlePEM []byte
	// log 日志。
	log *logger.Logger
}

// NewRegistrar 创建 registrar。
// webhookURL 必须是 kube-apiserver 可访问的地址（含 https:// + 路径）。
// caBundlePEM 为信任 webhook serving cert 的 CA bundle。
func NewRegistrar(webhookURL string, caBundlePEM []byte, log *logger.Logger) *Registrar {
	return &Registrar{webhookURL: webhookURL, caBundlePEM: caBundlePEM, log: log}
}

// EnsureWebhookConfig 在指定集群上 CreateOrUpdate MutatingWebhookConfiguration。
// 幂等：存在则更新（caBundle / url 变更），不存在则创建。
func (r *Registrar) EnsureWebhookConfig(ctx context.Context, client kubernetes.Interface) error {
	if client == nil {
		return fmt.Errorf("nil k8s client")
	}
	if r.webhookURL == "" {
		return fmt.Errorf("webhook URL is empty")
	}
	if len(r.caBundlePEM) == 0 {
		return fmt.Errorf("ca bundle is empty")
	}

	desired := r.buildDesired()

	existing, err := client.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(ctx, WebhookName, metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("get webhook config: %w", err)
		}
		// 不存在 → Create。
		_, err := client.AdmissionregistrationV1().MutatingWebhookConfigurations().Create(ctx, desired, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("create webhook config: %w", err)
		}
		r.log.Info("registrar: created MutatingWebhookConfiguration", "name", WebhookName, "url", r.webhookURL)
		return nil
	}

	// 存在 → Update（保留 ResourceVersion）。
	desired.ResourceVersion = existing.ResourceVersion
	_, err = client.AdmissionregistrationV1().MutatingWebhookConfigurations().Update(ctx, desired, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("update webhook config: %w", err)
	}
	r.log.Info("registrar: updated MutatingWebhookConfiguration", "name", WebhookName, "url", r.webhookURL)
	return nil
}

// RemoveWebhookConfig 删除指定集群上的 MutatingWebhookConfiguration（卸载/webhook 下线时用）。
// 幂等：不存在则忽略。
func (r *Registrar) RemoveWebhookConfig(ctx context.Context, client kubernetes.Interface) error {
	if client == nil {
		return nil
	}
	err := client.AdmissionregistrationV1().MutatingWebhookConfigurations().Delete(ctx, WebhookName, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete webhook config: %w", err)
	}
	return nil
}

// UpdateCABundle 更新 CA bundle（证书轮换时调用），并标记下次 reconcile 需推送。
func (r *Registrar) UpdateCABundle(caBundlePEM []byte) {
	r.caBundlePEM = caBundlePEM
}

// buildDesired 构造期望的 MutatingWebhookConfiguration 对象。
func (r *Registrar) buildDesired() *admissionregistrationv1.MutatingWebhookConfiguration {
	// namespaceSelector：排除 kube-system（系统 Pod 不注入稳定 IP）。
	// 也排除 kube-public / kube-node-lease。
	// "kubernetes.io/metadata.name" 是 K8s 1.21+ 自动给 namespace 打的标签，值为 namespace 名。
	namespaceSelector := &metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{
				Key:      "kubernetes.io/metadata.name",
				Operator: metav1.LabelSelectorOpNotIn,
				Values:   []string{"kube-system", "kube-public", "kube-node-lease"},
			},
		},
	}

	// failurePolicy=Ignore：webhook 不可用时放行 Pod 创建（避免集群级故障）。
	// 高可用场景可改 Fail（需 webhook 集群高可用 + 超时调短）。
	failurePolicy := admissionregistrationv1.Ignore
	matchPolicy := admissionregistrationv1.Equivalent
	sideEffects := admissionregistrationv1.SideEffectClassNone
	timeoutSecs := int32(5)

	return &admissionregistrationv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: WebhookName,
			Labels: map[string]string{
				"app.vortexops.io/managed": "true",
			},
		},
		Webhooks: []admissionregistrationv1.MutatingWebhook{
			{
				Name: "pod-ip.vortexops.io",
				ClientConfig: admissionregistrationv1.WebhookClientConfig{
					URL:      strPtr(r.webhookURL),
					CABundle: r.caBundlePEM,
				},
				Rules: []admissionregistrationv1.RuleWithOperations{
					{
						Operations: []admissionregistrationv1.OperationType{
							admissionregistrationv1.Create,
						},
						Rule: admissionregistrationv1.Rule{
							APIGroups:   []string{""},
							APIVersions: []string{"v1"},
							Resources:   []string{"pods"},
							Scope:       ptrScope(admissionregistrationv1.NamespacedScope),
						},
					},
				},
				FailurePolicy:           &failurePolicy,
				MatchPolicy:             &matchPolicy,
				NamespaceSelector:       namespaceSelector,
				SideEffects:             &sideEffects,
				TimeoutSeconds:          &timeoutSecs,
				AdmissionReviewVersions: []string{"v1"},
			},
		},
	}
}

// ReconcileAll 对多个集群并发 ensure webhook config，返回每个集群的注册结果。
// 单集群失败不影响其他集群。适合 syncer 周期调用。
func (r *Registrar) ReconcileAll(ctx context.Context, clients map[int64]kubernetes.Interface) map[int64]error {
	results := make(map[int64]error, len(clients))
	for clusterID, client := range clients {
		cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		results[clusterID] = r.EnsureWebhookConfig(cctx, client)
		cancel()
	}
	return results
}

func strPtr(s string) *string { return &s }

func ptrScope(s admissionregistrationv1.ScopeType) *admissionregistrationv1.ScopeType { return &s }

// IsAdmissionEnabled 检查集群是否已注册 VortexOps MutatingWebhookConfiguration。
// 由 healthz 或运维诊断调用。
func IsAdmissionEnabled(ctx context.Context, client kubernetes.Interface) (bool, error) {
	if client == nil {
		return false, fmt.Errorf("nil k8s client")
	}
	_, err := client.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(ctx, WebhookName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// DefaultNamespaceSelector 返回默认的 namespace selector（排除系统命名空间）。
// 导出供测试或自定义部署复用。
func DefaultNamespaceSelector() *metav1.LabelSelector {
	return &metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{
				Key:      "kubernetes.io/metadata.name",
				Operator: metav1.LabelSelectorOpNotIn,
				Values:   []string{"kube-system", "kube-public", "kube-node-lease"},
			},
		},
	}
}
