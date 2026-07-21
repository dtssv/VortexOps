// Package loghttp 是日志搜索与流 HTTP handlers。
package loghttp

import (
	"net/http"
	"strconv"
	"time"

	"github.com/vortexops/vortexops/internal/application/logapp"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpx"
)

// Handler 日志 HTTP handler。
type Handler struct {
	svc *logapp.Service
}

// NewHandler 创建 handler。
func NewHandler(svc *logapp.Service) *Handler {
	return &Handler{svc: svc}
}

// Search GET /api/v1/logs/search?q=&workspace_id=&page=&size=
func (h *Handler) Search(w http.ResponseWriter, r *http.Request) {
	page, size, offset := httpx.Pagination(r)
	wsID, _ := strconv.ParseInt(r.URL.Query().Get("workspace_id"), 10, 64)
	items, total, err := h.svc.Search(r.Context(), logapp.SearchInput{
		Query: r.URL.Query().Get("q"), WorkspaceID: wsID, From: offset, Size: size,
		Index: r.URL.Query().Get("index"),
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, httpx.Paged[logapp.SearchResultItem]{
		Items: items, Total: total, Page: page, Size: size,
	})
}

// SearchAudit GET /api/v1/logs/audit-search?q=&page=&size=
func (h *Handler) SearchAudit(w http.ResponseWriter, r *http.Request) {
	page, size, offset := httpx.Pagination(r)
	items, total, err := h.svc.SearchAudit(r.Context(), r.URL.Query().Get("q"), offset, size)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, httpx.Paged[logapp.SearchResultItem]{
		Items: items, Total: total, Page: page, Size: size,
	})
}

// Stream GET /api/v1/logs/stream?cluster_id=&namespace=&pod=&follow=&tail=
func (h *Handler) Stream(w http.ResponseWriter, r *http.Request) {
	clusterID, _ := strconv.ParseInt(r.URL.Query().Get("cluster_id"), 10, 64)
	tail, _ := strconv.ParseInt(r.URL.Query().Get("tail"), 10, 64)
	follow := r.URL.Query().Get("follow") == "true" || r.URL.Query().Get("follow") == "1"
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, ok := w.(http.Flusher)
	if err := h.svc.Stream(r.Context(), logapp.StreamInput{
		ClusterID: clusterID, Namespace: r.URL.Query().Get("namespace"),
		Pod: r.URL.Query().Get("pod"), Container: r.URL.Query().Get("container"),
		Follow: follow, TailLines: tail,
	}, w); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if ok {
		flusher.Flush()
	}
	_ = time.Now()
}
