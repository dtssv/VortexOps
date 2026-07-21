// Package kbhttp 是 AI 助手知识库的 HTTP handlers。
package kbhttp

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/vortexops/vortexops/internal/application/kbapp"
	"github.com/vortexops/vortexops/internal/domain/kb"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpauth"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpx"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// Handler 知识库 handler。
type Handler struct {
	svc *kbapp.Service
}

// NewHandler 创建知识库 handler。
func NewHandler(svc *kbapp.Service) *Handler {
	return &Handler{svc: svc}
}

// ListCategories GET /api/v1/kb/categories
func (h *Handler) ListCategories(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListCategories(r.Context())
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toCategoryDTOs(items))
}

// ListDocuments GET /api/v1/kb/documents?category=&search=&status=&page=&size=
func (h *Handler) ListDocuments(w http.ResponseWriter, r *http.Request) {
	page, size, offset := httpx.Pagination(r)
	q := kb.Query{
		CategoryCode: r.URL.Query().Get("category"),
		Search:       r.URL.Query().Get("search"),
		Status:       r.URL.Query().Get("status"),
		Offset:       offset, Limit: size,
	}
	items, total, err := h.svc.ListDocuments(r.Context(), q)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, httpx.Paged[documentDTO]{
		Items: toDocumentDTOs(items), Total: total, Page: page, Size: size,
	})
}

// GetDocument GET /api/v1/kb/documents/{id}
func (h *Handler) GetDocument(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id == 0 {
		httpx.WriteError(w, apperr.Validation("invalid id", err))
		return
	}
	d, err := h.svc.GetDocument(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toDocumentDTO(d))
}

// CreateDocument POST /api/v1/kb/documents
func (h *Handler) CreateDocument(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CategoryID int64    `json:"category_id"`
		Title      string   `json:"title"`
		SourceType string   `json:"source_type"`
		SourceURL  string   `json:"source_url"`
		Content    string   `json:"content"`
		Tags       []string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid body", err))
		return
	}
	uid := httpauth.UserID(r.Context())
	d, err := h.svc.CreateDocument(r.Context(), kb.CreateDocumentInput{
		CategoryID: req.CategoryID, Title: req.Title, SourceType: req.SourceType,
		SourceURL: req.SourceURL, Content: req.Content, Tags: req.Tags, ActorID: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Created(w, toDocumentDTO(d))
}

// UpdateDocument PUT /api/v1/kb/documents/{id}
func (h *Handler) UpdateDocument(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id == 0 {
		httpx.WriteError(w, apperr.Validation("invalid id", err))
		return
	}
	var req struct {
		CategoryID *int64    `json:"category_id"`
		Title      *string   `json:"title"`
		SourceType *string   `json:"source_type"`
		SourceURL  *string   `json:"source_url"`
		Content    *string   `json:"content"`
		Tags       *[]string `json:"tags"`
		Status     *string   `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid body", err))
		return
	}
	uid := httpauth.UserID(r.Context())
	d, err := h.svc.UpdateDocument(r.Context(), kb.UpdateDocumentInput{
		ID: id, CategoryID: req.CategoryID, Title: req.Title, SourceType: req.SourceType,
		SourceURL: req.SourceURL, Content: req.Content, Tags: req.Tags, Status: req.Status,
		ActorID: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toDocumentDTO(d))
}

// DeleteDocument DELETE /api/v1/kb/documents/{id}
func (h *Handler) DeleteDocument(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id == 0 {
		httpx.WriteError(w, apperr.Validation("invalid id", err))
		return
	}
	uid := httpauth.UserID(r.Context())
	if err := h.svc.DeleteDocument(r.Context(), id, uid); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// ReindexDocument POST /api/v1/kb/documents/{id}/reindex
// 手动触发重新分块向量化。
func (h *Handler) ReindexDocument(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id == 0 {
		httpx.WriteError(w, apperr.Validation("invalid id", err))
		return
	}
	d, err := h.svc.GetDocument(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	uid := httpauth.UserID(r.Context())
	if err := h.svc.ReindexDocument(r.Context(), id, d.Content, uid); err != nil {
		httpx.WriteError(w, apperr.Internal("reindex document", err))
		return
	}
	httpx.OK(w, map[string]any{"reindexed": true})
}

// Search POST /api/v1/kb/search
// RAG 检索：返回 topK 相似分块（管理/调试用，对话时由后端自动调用）。
func (h *Handler) Search(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query       string `json:"query"`
		TopK        int    `json:"top_k"`
		CategoryCode string `json:"category_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid body", err))
		return
	}
	if req.Query == "" {
		httpx.WriteError(w, apperr.Validation("query is required", nil))
		return
	}
	if req.TopK <= 0 {
		req.TopK = 5
	}
	hits, err := h.svc.Search(r.Context(), req.Query, req.TopK, req.CategoryCode)
	if err != nil {
		httpx.WriteError(w, apperr.Internal("kb search", err))
		return
	}
	httpx.OK(w, toChunkResultDTOs(hits))
}
