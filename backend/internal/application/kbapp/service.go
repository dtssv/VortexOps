// Package kbapp 是 AI 助手知识库的应用服务层。
// 提供知识库文档 CRUD、自动分块、向量化与 RAG 检索。
package kbapp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/vortexops/vortexops/internal/domain"
	"github.com/vortexops/vortexops/internal/domain/kb"
	"github.com/vortexops/vortexops/internal/platform/llm"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// Service 知识库应用服务。
type Service struct {
	repo     kb.Repository
	settings SettingProvider
	embed    llm.EmbedClient
}

// SettingProvider 系统设置提供者（由 systemapp.Service 实现）。
type SettingProvider interface {
	GetStringSetting(ctx context.Context, key, fallback string) (string, error)
}

// 默认设置项 key 常量。
const (
	KeyEmbedProvider   = "ai.embedding.provider"
	KeyEmbedURL        = "ai.embedding.url"
	KeyEmbedAPIKey     = "ai.embedding.api_key"
	KeyEmbedModel      = "ai.embedding.model"
	KeyEmbedDimensions = "ai.embedding.dimensions"
)

// New 创建服务。embed 可为 nil，调用 EnsureEmbedClient 时按需创建。
func New(repo kb.Repository, settings SettingProvider) *Service {
	return &Service{repo: repo, settings: settings}
}

// WithEmbedClient 注入预构建的嵌入客户端（便于测试或单例复用）。
func (s *Service) WithEmbedClient(c llm.EmbedClient) *Service {
	s.embed = c
	return s
}

// EnsureEmbedClient 按系统设置懒加载嵌入客户端。
func (s *Service) EnsureEmbedClient(ctx context.Context) (llm.EmbedClient, error) {
	if s.embed != nil {
		return s.embed, nil
	}
	provider, _ := s.settings.GetStringSetting(ctx, KeyEmbedProvider, "openai")
	baseURL, _ := s.settings.GetStringSetting(ctx, KeyEmbedURL, "")
	apiKey, _ := s.settings.GetStringSetting(ctx, KeyEmbedAPIKey, "")
	model, _ := s.settings.GetStringSetting(ctx, KeyEmbedModel, "text-embedding-3-small")
	dimStr, _ := s.settings.GetStringSetting(ctx, KeyEmbedDimensions, "1536")
	dim := parseInt(dimStr, 1536)
	c, err := llm.NewEmbedClient(llm.Config{Provider: provider, BaseURL: baseURL, APIKey: apiKey, Model: model}, dim)
	if err != nil {
		return nil, apperr.Internal("create embed client", err)
	}
	s.embed = c
	return c, nil
}

// --- 文档 CRUD ---

// ListCategories 列出所有分类。
func (s *Service) ListCategories(ctx context.Context) ([]*kb.Category, error) {
	items, err := s.repo.ListCategories(ctx)
	if err != nil {
		return nil, apperr.Internal("list kb categories", err)
	}
	return items, nil
}

// CreateDocument 创建文档并自动分块向量化。
func (s *Service) CreateDocument(ctx context.Context, in kb.CreateDocumentInput) (*kb.Document, error) {
	if in.Title == "" {
		return nil, apperr.Validation("document title is required", nil)
	}
	if in.Content == "" {
		return nil, apperr.Validation("document content is required", nil)
	}
	if in.SourceType == "" {
		in.SourceType = "manual"
	}
	d := &kb.Document{
		CategoryID: in.CategoryID, Title: in.Title, SourceType: in.SourceType,
		SourceURL: in.SourceURL, Content: in.Content, Tags: in.Tags,
		Status: "indexing", Audit: domain.Audit{CreatedBy: in.ActorID, UpdatedBy: in.ActorID},
	}
	if d.Tags == nil {
		d.Tags = []string{}
	}
	out, err := s.repo.CreateDocument(ctx, d)
	if err != nil {
		return nil, apperr.Internal("create kb document", err)
	}
	// 异步分块向量化（不阻塞响应）；失败时文档保持 indexing 状态，管理员可手动重建。
	go func(docID int64, content string, actorID int64) {
		bgCtx := context.Background()
		if err := s.ReindexDocument(bgCtx, docID, content, actorID); err != nil {
			// 仅记录，不返回；可后续接入日志。
			_ = err
		}
	}(out.ID, in.Content, in.ActorID)
	return out, nil
}

// UpdateDocument 更新文档。若 content 变更则重新分块向量化。
func (s *Service) UpdateDocument(ctx context.Context, in kb.UpdateDocumentInput) (*kb.Document, error) {
	existing, err := s.repo.GetDocument(ctx, in.ID)
	if err != nil {
		if errors.Is(err, kb.ErrDocumentNotFound) {
			return nil, apperr.NotFound("kb document", fmt.Sprintf("%d", in.ID))
		}
		return nil, apperr.Internal("get kb document", err)
	}
	if in.CategoryID != nil {
		existing.CategoryID = *in.CategoryID
	}
	if in.Title != nil {
		existing.Title = *in.Title
	}
	if in.SourceType != nil {
		existing.SourceType = *in.SourceType
	}
	if in.SourceURL != nil {
		existing.SourceURL = *in.SourceURL
	}
	contentChanged := false
	if in.Content != nil {
		if *in.Content != existing.Content {
			contentChanged = true
		}
		existing.Content = *in.Content
	}
	if in.Tags != nil {
		existing.Tags = *in.Tags
	}
	if in.Status != nil {
		existing.Status = *in.Status
	} else if contentChanged {
		existing.Status = "indexing"
	}
	existing.UpdatedBy = in.ActorID
	out, err := s.repo.UpdateDocument(ctx, existing)
	if err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return nil, apperr.Conflict("kb document was modified by another request", err)
		}
		return nil, apperr.Internal("update kb document", err)
	}
	if contentChanged {
		go func(docID int64, content string, actorID int64) {
			bgCtx := context.Background()
			_ = s.ReindexDocument(bgCtx, docID, content, actorID)
		}(out.ID, existing.Content, in.ActorID)
	}
	return out, nil
}

// GetDocument 获取文档。
func (s *Service) GetDocument(ctx context.Context, id int64) (*kb.Document, error) {
	d, err := s.repo.GetDocument(ctx, id)
	if err != nil {
		if errors.Is(err, kb.ErrDocumentNotFound) {
			return nil, apperr.NotFound("kb document", fmt.Sprintf("%d", id))
		}
		return nil, apperr.Internal("get kb document", err)
	}
	return d, nil
}

// ListDocuments 列出文档。
func (s *Service) ListDocuments(ctx context.Context, q kb.Query) ([]*kb.Document, int64, error) {
	items, total, err := s.repo.ListDocuments(ctx, q)
	if err != nil {
		return nil, 0, apperr.Internal("list kb documents", err)
	}
	return items, total, nil
}

// DeleteDocument 删除文档。
func (s *Service) DeleteDocument(ctx context.Context, id int64, actorID int64) error {
	if err := s.repo.DeleteDocument(ctx, id, actorID); err != nil {
		if errors.Is(err, kb.ErrDocumentNotFound) {
			return apperr.NotFound("kb document", fmt.Sprintf("%d", id))
		}
		return apperr.Internal("delete kb document", err)
	}
	return nil
}

// ReindexDocument 重新分块并向量化文档。
func (s *Service) ReindexDocument(ctx context.Context, docID int64, content string, actorID int64) error {
	embed, err := s.EnsureEmbedClient(ctx)
	if err != nil {
		return err
	}
	// 1. 分块。
	chunksText := chunkText(content, 800, 100)
	if len(chunksText) == 0 {
		return nil
	}
	// 2. 批量向量化。OpenAI 一次最多 100 条；这里分批。
	const batchSize = 32
	var allChunks []*kb.Chunk
	for i := 0; i < len(chunksText); i += batchSize {
		end := i + batchSize
		if end > len(chunksText) {
			end = len(chunksText)
		}
		batch := chunksText[i:end]
		vecs, err := embed.Embed(ctx, batch)
		if err != nil {
			return fmt.Errorf("embed batch %d: %w", i, err)
		}
		for j, text := range batch {
			vec := vecs[j]
			if len(vec) != embed.Dim() {
				return fmt.Errorf("embedding dim mismatch: got %d, want %d", len(vec), embed.Dim())
			}
			allChunks = append(allChunks, &kb.Chunk{
				DocumentID: docID, ChunkIndex: i + j, Content: text,
				Embedding: vec, TokenCount: approxTokens(text),
				Audit: domain.Audit{CreatedBy: actorID, UpdatedBy: actorID},
			})
		}
	}
	// 3. 替换旧分块。
	if err := s.repo.DeleteChunksByDocument(ctx, docID); err != nil {
		return err
	}
	if err := s.repo.CreateChunks(ctx, allChunks); err != nil {
		return err
	}
	return nil
}

// Search RAG 检索：将查询向量化后召回 topK 相似分块。
func (s *Service) Search(ctx context.Context, query string, topK int, categoryCode string) ([]*kb.ChunkResult, error) {
	if query == "" {
		return nil, nil
	}
	embed, err := s.EnsureEmbedClient(ctx)
	if err != nil {
		return nil, err
	}
	vecs, err := embed.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	if len(vecs) == 0 {
		return nil, nil
	}
	return s.repo.SearchByVector(ctx, vecs[0], topK, categoryCode)
}

// chunkText 将长文本按 maxChars 分块，overlapChars 重叠。
// 优先按段落（双换行）切分，避免截断句子。
func chunkText(text string, maxChars, overlapChars int) []string {
	if maxChars <= 0 {
		maxChars = 800
	}
	if overlapChars < 0 {
		overlapChars = 0
	}
	if utf8.RuneCountInString(text) <= maxChars {
		return []string{text}
	}
	// 按段落切分。
	paragraphs := strings.Split(text, "\n\n")
	var chunks []string
	var current strings.Builder
	currentLen := 0
	for _, p := range paragraphs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		pLen := utf8.RuneCountInString(p)
		// 段落本身超长，按 maxChars 硬切。
		if pLen > maxChars {
			if current.Len() > 0 {
				chunks = append(chunks, current.String())
				current.Reset()
				currentLen = 0
			}
			for i := 0; i < pLen; i += maxChars {
				end := i + maxChars
				if end > pLen {
					end = pLen
				}
				chunks = append(chunks, string([]rune(p)[i:end]))
			}
			continue
		}
		if currentLen+pLen+2 > maxChars && current.Len() > 0 {
			chunks = append(chunks, current.String())
			// 重叠：保留尾部 overlapChars 字符。
			tail := current.String()
			if overlapChars > 0 && utf8.RuneCountInString(tail) > overlapChars {
				tail = string([]rune(tail)[utf8.RuneCountInString(tail)-overlapChars:])
			} else {
				tail = ""
			}
			current.Reset()
			current.WriteString(tail)
			currentLen = utf8.RuneCountInString(tail)
		}
		if current.Len() > 0 {
			current.WriteString("\n\n")
			currentLen += 2
		}
		current.WriteString(p)
		currentLen += pLen
	}
	if current.Len() > 0 {
		chunks = append(chunks, current.String())
	}
	return chunks
}

// approxTokens 粗略估算 token 数（中文按字符数，英文按 4 字符/token）。
func approxTokens(text string) int {
	runeCount := utf8.RuneCountInString(text)
	// 简单估算：中文字符占多数时 token≈字符数；否则 token≈字符数/4。
	return (runeCount + 3) / 4
}

func parseInt(s string, def int) int {
	if s == "" {
		return def
	}
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return def
	}
	return n
}
