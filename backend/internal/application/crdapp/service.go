// Package crdapp 是 K8s 自定义资源（CRD）管理的应用服务。
// 提供：
//   - 列出/查看集群中已注册的 CustomResourceDefinition
//   - 列出某 CRD 下的自定义资源实例（按 namespace）
//   - 查看单个自定义资源实例（YAML/JSON）
//   - 删除自定义资源实例
//
// CRD 定义通过 apiextensionsv1 客户端操作；CRD 实例通过 dynamic client 操作。
// 本服务为只读 + 删除（不创建 CRD 定义，避免误操作破坏集群），创建 CRD 由部署侧 kubectl/Helm 完成。
package crdapp

import (
	"context"
	"encoding/json"
	"fmt"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/yaml"

	"github.com/vortexops/vortexops/internal/application/clusterapp"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// Service CRD 管理应用服务。
type Service struct {
	clusters *clusterapp.Service
}

// New 创建 CRD 管理服务。
func New(clusters *clusterapp.Service) *Service {
	return &Service{clusters: clusters}
}

// CRDSummary CRD 摘要（列表用）。
type CRDSummary struct {
	Name         string            `json:"name"`          // CRD 名（如 ciliumnetworkpolicies.cilium.io）
	Group        string            `json:"group"`         // API group
	Version      string            `json:"version"`       // 首个 served version
	Kind         string            `json:"kind"`          // kind
	Scope        string            `json:"scope"`         // Namespaced / Cluster
	Categories   []string          `json:"categories"`    // kubectl categories
	ShortNames   []string          `json:"short_names"`   // 短名
	CreatedAt    metav1.Time       `json:"created_at"`
	Established  bool              `json:"established"`   // 是否已 Established
}

// ListCRDs 列出集群中所有 CRD（按名字排序）。
func (s *Service) ListCRDs(ctx context.Context, clusterID int64) ([]CRDSummary, error) {
	client, err := s.apiextClient(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	list, err := client.ApiextensionsV1().CustomResourceDefinitions().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, apperr.Internal("list crds", err)
	}
	out := make([]CRDSummary, 0, len(list.Items))
	for _, crd := range list.Items {
		out = append(out, toCRDSummary(&crd))
	}
	return out, nil
}

// GetCRD 查询单个 CRD 定义详情。
func (s *Service) GetCRD(ctx context.Context, clusterID int64, name string) (*apiextensionsv1.CustomResourceDefinition, error) {
	client, err := s.apiextClient(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	crd, err := client.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, apperr.Internal("get crd", err)
	}
	return crd, nil
}

// CRResourceSummary 自定义资源实例摘要。
type CRResourceSummary struct {
	Name       string         `json:"name"`
	Namespace  string         `json:"namespace"`
	Kind       string         `json:"kind"`
	APIVersion string         `json:"api_version"`
	CreatedAt  metav1.Time    `json:"created_at"`
	UID        string         `json:"uid"`
	Spec       map[string]any `json:"spec,omitempty"`
	Status     map[string]any `json:"status,omitempty"`
}

// ListResources 列出某 CRD 下的自定义资源实例。
// crdName 为 CRD 全名（如 ciliumnetworkpolicies.cilium.io）；namespace 为空时查集群级或全命名空间。
func (s *Service) ListResources(ctx context.Context, clusterID int64, crdName, namespace string) ([]CRResourceSummary, error) {
	dyn, crd, err := s.resolveCRD(ctx, clusterID, crdName)
	if err != nil {
		return nil, err
	}
	gvr, err := primaryGVR(crd)
	if err != nil {
		return nil, apperr.BusinessRule("crd has no served version", err)
	}
	// Namespaced CRD：namespace 为空时列全命名空间；Cluster CRD：namespace 忽略。
	ns := namespace
	if crd.Spec.Scope == apiextensionsv1.ClusterScoped {
		ns = ""
	}
	list, err := dyn.Resource(gvr).Namespace(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, apperr.Internal("list custom resources", err)
	}
	out := make([]CRResourceSummary, 0, len(list.Items))
	for _, item := range list.Items {
		out = append(out, toResourceSummary(&item))
	}
	return out, nil
}

// GetResource 查看单个自定义资源实例（返回 unstructured，前端可渲染 YAML）。
func (s *Service) GetResource(ctx context.Context, clusterID int64, crdName, namespace, name string) (*unstructured.Unstructured, error) {
	dyn, crd, err := s.resolveCRD(ctx, clusterID, crdName)
	if err != nil {
		return nil, err
	}
	gvr, err := primaryGVR(crd)
	if err != nil {
		return nil, apperr.BusinessRule("crd has no served version", err)
	}
	ns := namespace
	if crd.Spec.Scope == apiextensionsv1.ClusterScoped {
		ns = ""
	}
	item, err := dyn.Resource(gvr).Namespace(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, apperr.Internal("get custom resource", err)
	}
	return item, nil
}

// DeleteResource 删除单个自定义资源实例。
func (s *Service) DeleteResource(ctx context.Context, clusterID int64, crdName, namespace, name string) error {
	dyn, crd, err := s.resolveCRD(ctx, clusterID, crdName)
	if err != nil {
		return err
	}
	gvr, err := primaryGVR(crd)
	if err != nil {
		return apperr.BusinessRule("crd has no served version", err)
	}
	ns := namespace
	if crd.Spec.Scope == apiextensionsv1.ClusterScoped {
		ns = ""
	}
	if err := dyn.Resource(gvr).Namespace(ns).Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
		return apperr.Internal("delete custom resource", err)
	}
	return nil
}

// --- helpers ---

// apiextClient 构造 apiextensions clientset。
func (s *Service) apiextClient(ctx context.Context, clusterID int64) (apiextensionsclient.Interface, error) {
	cfg, err := s.clusters.GetRestConfig(ctx, clusterID)
	if err != nil {
		return nil, apperr.Internal("get cluster rest config", err)
	}
	client, err := apiextensionsclient.NewForConfig(cfg)
	if err != nil {
		return nil, apperr.Internal("build apiextensions client", err)
	}
	return client, nil
}

// resolveCRD 取 dynamic client + CRD 定义（用于资源实例操作）。
func (s *Service) resolveCRD(ctx context.Context, clusterID int64, crdName string) (dynamic.Interface, *apiextensionsv1.CustomResourceDefinition, error) {
	dyn, err := s.clusters.GetDynamicClient(ctx, clusterID)
	if err != nil {
		return nil, nil, apperr.Internal("get dynamic client", err)
	}
	client, err := s.apiextClient(ctx, clusterID)
	if err != nil {
		return nil, nil, err
	}
	crd, err := client.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, crdName, metav1.GetOptions{})
	if err != nil {
		return nil, nil, apperr.NotFound("crd", crdName)
	}
	return dyn, crd, nil
}

// toCRDSummary 把 CRD 转为列表摘要。
func toCRDSummary(crd *apiextensionsv1.CustomResourceDefinition) CRDSummary {
	ver, _ := primaryVersion(crd)
	established := false
	for _, cond := range crd.Status.Conditions {
		if cond.Type == apiextensionsv1.Established && cond.Status == apiextensionsv1.ConditionTrue {
			established = true
			break
		}
	}
	return CRDSummary{
		Name:        crd.Name,
		Group:       crd.Spec.Group,
		Version:     ver,
		Kind:        crd.Spec.Names.Kind,
		Scope:       string(crd.Spec.Scope),
		Categories:  crd.Spec.Names.Categories,
		ShortNames:  crd.Spec.Names.ShortNames,
		CreatedAt:   crd.CreationTimestamp,
		Established: established,
	}
}

// primaryVersion 取首个 served+storage 的 version 名。
func primaryVersion(crd *apiextensionsv1.CustomResourceDefinition) (string, bool) {
	for _, v := range crd.Spec.Versions {
		if v.Served && v.Storage {
			return v.Name, true
		}
	}
	// 降级：取首个 served。
	for _, v := range crd.Spec.Versions {
		if v.Served {
			return v.Name, true
		}
	}
	return "", false
}

// primaryGVR 取 CRD 首个 served 版本的 GroupVersionResource。
func primaryGVR(crd *apiextensionsv1.CustomResourceDefinition) (schema.GroupVersionResource, error) {
	ver, ok := primaryVersion(crd)
	if !ok {
		return schema.GroupVersionResource{}, fmt.Errorf("crd %s has no served version", crd.Name)
	}
	return schema.GroupVersionResource{
		Group:    crd.Spec.Group,
		Version:  ver,
		Resource: crd.Spec.Names.Plural,
	}, nil
}

// toResourceSummary 把 unstructured 转为资源摘要。
func toResourceSummary(item *unstructured.Unstructured) CRResourceSummary {
	out := CRResourceSummary{
		Name:       item.GetName(),
		Namespace:  item.GetNamespace(),
		Kind:       item.GetKind(),
		APIVersion: item.GetAPIVersion(),
		CreatedAt:  item.GetCreationTimestamp(),
		UID:        string(item.GetUID()),
	}
	if spec, ok := item.Object["spec"].(map[string]any); ok {
		out.Spec = spec
	}
	if status, ok := item.Object["status"].(map[string]any); ok {
		out.Status = status
	}
	return out
}

// MarshalYAML 把 unstructured 序列化为 YAML（前端展示用）。
func MarshalYAML(item *unstructured.Unstructured) (string, error) {
	// 先转 JSON 再转 YAML，避免 unstructured 直接 YAML 序列化的类型问题。
	raw, err := json.Marshal(item.Object)
	if err != nil {
		return "", fmt.Errorf("marshal json: %w", err)
	}
	var obj any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return "", fmt.Errorf("unmarshal: %w", err)
	}
	// 用 sigs.k8s.io/yaml 保证字段顺序稳定。
	yamlBytes, err := yaml.Marshal(obj)
	if err != nil {
		return "", fmt.Errorf("marshal yaml: %w", err)
	}
	return string(yamlBytes), nil
}
