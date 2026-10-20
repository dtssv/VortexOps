// Package crdhttp 是 K8s CRD（自定义资源）管理的 HTTP handlers。
// 暴露 CRD 定义列表/详情、自定义资源实例列表/详情/删除，通过平台级权限控制。
package crdhttp

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/vortexops/vortexops/internal/application/crdapp"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpx"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// Handler 处理 /api/v1/k8s/clusters/{clusterId}/crds 路由。
type Handler struct {
	svc *crdapp.Service
}

// NewHandler 创建 CRD handler。
func NewHandler(svc *crdapp.Service) *Handler {
	return &Handler{svc: svc}
}

// --- helpers ---

func clusterID(r *http.Request) (int64, error) {
	v := chi.URLParam(r, "clusterId")
	id, err := strconv.ParseInt(v, 10, 64)
	if err != nil || id <= 0 {
		return 0, apperr.Validation("invalid cluster_id", err)
	}
	return id, nil
}

func crdName(r *http.Request) string {
	return chi.URLParam(r, "crdName")
}

func resourceName(r *http.Request) string {
	return chi.URLParam(r, "resourceName")
}

func namespace(r *http.Request) string {
	return r.URL.Query().Get("namespace")
}

// --- CRD 定义 ---

// ListCRDs GET /api/v1/k8s/clusters/{clusterId}/crds
func (h *Handler) ListCRDs(w http.ResponseWriter, r *http.Request) {
	cid, err := clusterID(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	items, err := h.svc.ListCRDs(r.Context(), cid)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, items)
}

// GetCRD GET /api/v1/k8s/clusters/{clusterId}/crds/{crdName}
func (h *Handler) GetCRD(w http.ResponseWriter, r *http.Request) {
	cid, err := clusterID(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	crd, err := h.svc.GetCRD(r.Context(), cid, crdName(r))
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, crd)
}

// --- 自定义资源实例 ---

// ListResources GET /api/v1/k8s/clusters/{clusterId}/crds/{crdName}/resources?namespace=xxx
func (h *Handler) ListResources(w http.ResponseWriter, r *http.Request) {
	cid, err := clusterID(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	items, err := h.svc.ListResources(r.Context(), cid, crdName(r), namespace(r))
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, items)
}

// GetResource GET /api/v1/k8s/clusters/{clusterId}/crds/{crdName}/resources/{resourceName}?namespace=xxx
// 返回 unstructured 对象，前端可渲染为 YAML。
func (h *Handler) GetResource(w http.ResponseWriter, r *http.Request) {
	cid, err := clusterID(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	item, err := h.svc.GetResource(r.Context(), cid, crdName(r), namespace(r), resourceName(r))
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	// 默认返回 unstructured JSON；若 query param format=yaml 返回 YAML。
	if r.URL.Query().Get("format") == "yaml" {
		yamlStr, err := crdapp.MarshalYAML(item)
		if err != nil {
			httpx.WriteError(w, apperr.Internal("marshal yaml", err))
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(yamlStr))
		return
	}
	httpx.OK(w, item)
}

// DeleteResource DELETE /api/v1/k8s/clusters/{clusterId}/crds/{crdName}/resources/{resourceName}?namespace=xxx
func (h *Handler) DeleteResource(w http.ResponseWriter, r *http.Request) {
	cid, err := clusterID(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	if err := h.svc.DeleteResource(r.Context(), cid, crdName(r), namespace(r), resourceName(r)); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}
