package cilium

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// Applier 将 Cilium CRD 应用到集群（create-or-update 语义）。
type Applier struct {
	dynamic dynamic.Interface
}

// NewApplier 创建 Cilium CRD 应用器。
func NewApplier(dyn dynamic.Interface) *Applier {
	return &Applier{dynamic: dyn}
}

// Apply 应用一组 unstructured Cilium 资源。
func (a *Applier) Apply(ctx context.Context, objs ...*unstructured.Unstructured) error {
	if a == nil || a.dynamic == nil {
		return nil
	}
	for _, obj := range objs {
		if obj == nil {
			continue
		}
		if err := a.applyOne(ctx, obj); err != nil {
			return err
		}
	}
	return nil
}

func (a *Applier) applyOne(ctx context.Context, obj *unstructured.Unstructured) error {
	gvk := obj.GroupVersionKind()
	if gvk.Group == "" {
		gv, err := schema.ParseGroupVersion(obj.GetAPIVersion())
		if err != nil {
			return nil
		}
		gvk = gv.WithKind(obj.GetKind())
	}
	gvr, err := a.gvrFor(gvk)
	if err != nil {
		// CRD 未安装时软降级（开发环境可能仍用 Calico）。
		return nil
	}
	ns := obj.GetNamespace()
	var ri dynamic.ResourceInterface
	if ns != "" {
		ri = a.dynamic.Resource(gvr).Namespace(ns)
	} else {
		ri = a.dynamic.Resource(gvr)
	}
	name := obj.GetName()
	existing, err := ri.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			_, err = ri.Create(ctx, obj, metav1.CreateOptions{})
			if apierrors.IsAlreadyExists(err) {
				return nil
			}
			return err
		}
		// CRD 不存在 → 软降级
		if apierrors.IsNotFound(err) || apierrors.IsForbidden(err) {
			return nil
		}
		return err
	}
	obj.SetResourceVersion(existing.GetResourceVersion())
	_, err = ri.Update(ctx, obj, metav1.UpdateOptions{})
	if err != nil && (apierrors.IsNotFound(err) || apierrors.IsForbidden(err)) {
		return nil
	}
	return err
}

// Delete 删除指定 Cilium 资源（幂等）。
func (a *Applier) Delete(ctx context.Context, apiVersion, kind, namespace, name string) error {
	if a == nil || a.dynamic == nil || name == "" {
		return nil
	}
	gvk := schema.FromAPIVersionAndKind(apiVersion, kind)
	gvr, err := a.gvrFor(gvk)
	if err != nil {
		return nil
	}
	var ri dynamic.ResourceInterface
	if namespace != "" {
		ri = a.dynamic.Resource(gvr).Namespace(namespace)
	} else {
		ri = a.dynamic.Resource(gvr)
	}
	err = ri.Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

func (a *Applier) gvrFor(gvk schema.GroupVersionKind) (schema.GroupVersionResource, error) {
	// 已知 Cilium CRD 映射（集群未装 Cilium 时 apply 软降级）。
	switch gvk.Group {
	case ciliumGroup:
		switch gvk.Kind {
		case lbPoolKind:
			return schema.GroupVersionResource{Group: ciliumGroup, Version: "v2alpha1", Resource: "ciliumloadbalancerippools"}, nil
		case "CiliumNetworkPolicy":
			return schema.GroupVersionResource{Group: ciliumGroup, Version: "v2", Resource: "ciliumnetworkpolicies"}, nil
		case "CiliumClusterwideNetworkPolicy":
			return schema.GroupVersionResource{Group: ciliumGroup, Version: "v2", Resource: "ciliumclusterwidenetworkpolicies"}, nil
		case "CiliumEnvoyConfig":
			return schema.GroupVersionResource{Group: ciliumGroup, Version: "v2", Resource: "ciliumenvoyconfigs"}, nil
		default:
			return schema.GroupVersionResource{}, fmt.Errorf("unknown cilium kind: %s", gvk.Kind)
		}
	default:
		return schema.GroupVersionResource{}, fmt.Errorf("unknown group: %s", gvk.Group)
	}
}
