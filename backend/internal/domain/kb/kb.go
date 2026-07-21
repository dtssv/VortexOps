// Package kb 定义 AI 助手知识库领域模型。
package kb

import (
	"context"
	"errors"

	"github.com/vortexops/vortexops/internal/domain"
)

// Category 知识库分类。
type Category struct {
	ID          int64
	UUID        string
	Name        string
	Code        string
	Description string
	SortOrder   int
	domain.Audit
}

// Document 知识库文档。
type Document struct {
	ID          int64
	UUID        string
	CategoryID  int64
	CategoryCode string // 仅查询时填充，便于前端展示
	Title       string
	SourceType  string // manual / url / markdown / faq
	SourceURL   string
	Content     string
	Tags        []string
	ChunkCount  int
	Status      string // active / indexing / archived
	domain.Audit
}

// Chunk 文档分块（含向量）。
type Chunk struct {
	ID         int64
	UUID       string
	DocumentID int64
	ChunkIndex int
	Content    string
	Embedding  []float32 // 向量；写入时由 repo 序列化为 pgvector 字符串
	TokenCount int
	domain.Audit
}

// ChunkResult RAG 检索结果。
type ChunkResult struct {
	Chunk
	DocumentTitle string
	CategoryCode  string
	Score         float64 // 相似度分数（0~1，越大越相似）
}

// Query 文档查询条件。
type Query struct {
	CategoryCode string
	Search       string
	Status       string
	Offset       int
	Limit        int
}

// CreateDocumentInput 创建文档输入。
type CreateDocumentInput struct {
	CategoryID  int64
	Title       string
	SourceType  string
	SourceURL   string
	Content     string
	Tags        []string
	ActorID     int64
}

// UpdateDocumentInput 更新文档输入。
type UpdateDocumentInput struct {
	ID         int64
	CategoryID *int64
	Title      *string
	SourceType *string
	SourceURL  *string
	Content    *string
	Tags       *[]string
	Status     *string
	ActorID    int64
}

// Repository 知识库仓储接口。
type Repository interface {
	// 分类
	ListCategories(ctx context.Context) ([]*Category, error)
	GetCategoryByCode(ctx context.Context, code string) (*Category, error)

	// 文档
	CreateDocument(ctx context.Context, d *Document) (*Document, error)
	UpdateDocument(ctx context.Context, d *Document) (*Document, error)
	GetDocument(ctx context.Context, id int64) (*Document, error)
	GetDocumentByUUID(ctx context.Context, uuid string) (*Document, error)
	ListDocuments(ctx context.Context, q Query) ([]*Document, int64, error)
	DeleteDocument(ctx context.Context, id int64, deletedBy int64) error

	// 分块
	CreateChunks(ctx context.Context, chunks []*Chunk) error
	DeleteChunksByDocument(ctx context.Context, documentID int64) error
	ListChunksByDocument(ctx context.Context, documentID int64) ([]*Chunk, error)
	// SearchByVector 按向量相似度检索分块（余弦相似度）。
	// 返回 topK 个最相似的分块，按 score 降序。
	SearchByVector(ctx context.Context, embedding []float32, topK int, categoryCode string) ([]*ChunkResult, error)
}

// 领域错误。
var (
	ErrCategoryNotFound = errors.New("kb category not found")
	ErrDocumentNotFound = errors.New("kb document not found")
	ErrInvalidEmbedding = errors.New("invalid embedding vector")
)
