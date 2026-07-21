// Package k8sdns 将 DNS 记录同步到集群 CoreDNS ConfigMap。
package k8sdns

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	dnsinfra "github.com/vortexops/vortexops/internal/infrastructure/dns"
	dnsdomain "github.com/vortexops/vortexops/internal/domain/dns"
)

const (
	configMapName      = "vortexops-dns-hosts"
	configMapNamespace = "kube-system"
	configMapKey       = "vortexops.hosts"
	corefileKey        = "Corefile"
)

// ConfigMapSyncer 周期把 DNS 记录渲染进 CoreDNS ConfigMap。
type ConfigMapSyncer struct {
	provider *dnsinfra.CoreDNSProvider
}

// NewConfigMapSyncer 创建同步器。
func NewConfigMapSyncer() *ConfigMapSyncer {
	return &ConfigMapSyncer{provider: dnsinfra.NewCoreDNSProvider()}
}

// Sync 把活跃记录写入集群 ConfigMap（create-or-update）。
func (s *ConfigMapSyncer) Sync(ctx context.Context, client kubernetes.Interface, records []*dnsdomain.Record, backends map[int64][]dnsdomain.Backend) error {
	if client == nil {
		return nil
	}
	hosts := s.provider.RenderHostsBlock(records, backends)
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: configMapNamespace,
			Labels: map[string]string{
				"app.vortexops.io/managed": "true",
				"app.vortexops.io/component": "dns",
			},
		},
		Data: map[string]string{
			configMapKey: hosts,
		},
	}
	existing, err := client.CoreV1().ConfigMaps(configMapNamespace).Get(ctx, configMapName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			_, err = client.CoreV1().ConfigMaps(configMapNamespace).Create(ctx, cm, metav1.CreateOptions{})
			return err
		}
		return err
	}
	cm.ResourceVersion = existing.ResourceVersion
	if existing.Data != nil {
		cm.Data[corefileKey] = existing.Data[corefileKey]
	}
	_, err = client.CoreV1().ConfigMaps(configMapNamespace).Update(ctx, cm, metav1.UpdateOptions{})
	return err
}

// Delete 删除 VortexOps DNS ConfigMap（分组删除时可选调用）。
func (s *ConfigMapSyncer) Delete(ctx context.Context, client kubernetes.Interface) error {
	if client == nil {
		return nil
	}
	err := client.CoreV1().ConfigMaps(configMapNamespace).Delete(ctx, configMapName, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete dns configmap: %w", err)
	}
	return nil
}
