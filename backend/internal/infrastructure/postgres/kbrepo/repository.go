// Package kbrepo 是知识库的 Postgres 仓储实现。
package kbrepo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vortexops/vortexops/internal/domain"
	"github.com/vortexops/vortexops/internal/domain/kb"
)

// Repository 知识库仓储。
type Repository struct {
	pool *pgxpool.Pool
}

// New 创建仓储。
func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const categoryColumns = "id, uuid, name, code, description, sort_order, version, created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by"

const documentColumns = "id, uuid, category_id, title, source_type, source_url, content, tags, chunk_count, status, version, created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by"

// ==================== 分类 ====================

func (r *Repository) ListCategories(ctx context.Context) ([]*kb.Category, error) {
	query := fmt.Sprintf("SELECT %s FROM vo_kb_categories WHERE deleted = false ORDER BY sort_order ASC, id ASC", categoryColumns)
	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list kb categories: %w", err)
	}
	defer rows.Close()
	out := make([]*kb.Category, 0)
	for rows.Next() {
		c, err := scanCategory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *Repository) GetCategoryByCode(ctx context.Context, code string) (*kb.Category, error) {
	query := fmt.Sprintf("SELECT %s FROM vo_kb_categories WHERE code = $1 AND deleted = false", categoryColumns)
	row := r.pool.QueryRow(ctx, query, code)
	c, err := scanCategory(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, kb.ErrCategoryNotFound
		}
		return nil, err
	}
	return c, nil
}

func scanCategory(row pgx.Row) (*kb.Category, error) {
	c := &kb.Category{}
	var description *string
	var createdBy, updatedBy, deletedBy *int64
	if err := row.Scan(
		&c.ID, &c.UUID, &c.Name, &c.Code, &description, &c.SortOrder,
		&c.Version, &c.CreatedAt, &createdBy, &c.UpdatedAt, &updatedBy,
		&c.Deleted, &c.DeletedAt, &deletedBy,
	); err != nil {
		return nil, err
	}
	if description != nil {
		c.Description = *description
	}
	if createdBy != nil {
		c.CreatedBy = *createdBy
	}
	if updatedBy != nil {
		c.UpdatedBy = *updatedBy
	}
	if deletedBy != nil {
		c.DeletedBy = *deletedBy
	}
	return c, nil
}

// ==================== 文档 ====================

func (r *Repository) CreateDocument(ctx context.Context, d *kb.Document) (*kb.Document, error) {
	tags, _ := json.Marshal(d.Tags)
	if string(tags) == "null" {
		tags = []byte("[]")
	}
	query := `
INSERT INTO vo_kb_documents (category_id, title, source_type, source_url, content, tags, status, version, created_by, updated_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, 1, $8, $8)
RETURNING ` + documentColumns
	row := r.pool.QueryRow(ctx, query,
		nullableInt64(d.CategoryID), d.Title, d.SourceType, d.SourceURL, d.Content, tags, defaultStr(d.Status, "active"), d.CreatedBy)
	return scanDocument(row)
}

func (r *Repository) UpdateDocument(ctx context.Context, d *kb.Document) (*kb.Document, error) {
	tags, _ := json.Marshal(d.Tags)
	if string(tags) == "null" {
		tags = []byte("[]")
	}
	query := `
UPDATE vo_kb_documents SET
  category_id = $1, title = $2, source_type = $3, source_url = $4,
  content = $5, tags = $6, status = $7, version = version + 1, updated_by = $8, updated_at = now()
WHERE id = $9 AND version = $10 AND deleted = false
RETURNING ` + documentColumns
	row := r.pool.QueryRow(ctx, query,
		nullableInt64(d.CategoryID), d.Title, d.SourceType, d.SourceURL, d.Content, tags, defaultStr(d.Status, "active"), d.UpdatedBy, d.ID, d.Version)
	d2, err := scanDocument(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrConflict
		}
		return nil, err
	}
	return d2, nil
}

func (r *Repository) GetDocument(ctx context.Context, id int64) (*kb.Document, error) {
	query := fmt.Sprintf("SELECT %s FROM vo_kb_documents WHERE id = $1 AND deleted = false", documentColumns)
	row := r.pool.QueryRow(ctx, query, id)
	d, err := scanDocument(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, kb.ErrDocumentNotFound
		}
		return nil, err
	}
	return d, nil
}

func (r *Repository) GetDocumentByUUID(ctx context.Context, uuid string) (*kb.Document, error) {
	query := fmt.Sprintf("SELECT %s FROM vo_kb_documents WHERE uuid = $1 AND deleted = false", documentColumns)
	row := r.pool.QueryRow(ctx, query, uuid)
	d, err := scanDocument(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, kb.ErrDocumentNotFound
		}
		return nil, err
	}
	return d, nil
}

func (r *Repository) ListDocuments(ctx context.Context, q kb.Query) ([]*kb.Document, int64, error) {
	conds := []string{"d.deleted = false"}
	args := []any{}
	argIdx := 1
	if q.CategoryCode != "" {
		conds = append(conds, fmt.Sprintf("c.code = $%d", argIdx))
		args = append(args, q.CategoryCode)
		argIdx++
	}
	if q.Search != "" {
		conds = append(conds, fmt.Sprintf("(d.title ILIKE $%d OR d.content ILIKE $%d)", argIdx, argIdx))
		args = append(args, "%"+q.Search+"%")
		argIdx++
	}
	if q.Status != "" {
		conds = append(conds, fmt.Sprintf("d.status = $%d", argIdx))
		args = append(args, q.Status)
		argIdx++
	}
	where := strings.Join(conds, " AND ")
	if q.Limit <= 0 {
		q.Limit = 20
	}
	if q.Offset < 0 {
		q.Offset = 0
	}

	countQuery := fmt.Sprintf("SELECT count(*) FROM vo_kb_documents d LEFT JOIN vo_kb_categories c ON c.id = d.category_id WHERE %s", where)
	var total int64
	if err := r.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count kb documents: %w", err)
	}

	listQuery := fmt.Sprintf(`
SELECT %s
FROM vo_kb_documents d
LEFT JOIN vo_kb_categories c ON c.id = d.category_id
WHERE %s
ORDER BY d.updated_at DESC
LIMIT $%d OFFSET $%d`,
		prefixColumns(documentColumns, "d"), where, argIdx, argIdx+1)
	args = append(args, q.Limit, q.Offset)
	rows, err := r.pool.Query(ctx, listQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list kb documents: %w", err)
	}
	defer rows.Close()
	out := make([]*kb.Document, 0)
	for rows.Next() {
		d, err := scanDocument(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, d)
	}
	return out, total, rows.Err()
}

func (r *Repository) DeleteDocument(ctx context.Context, id int64, deletedBy int64) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_kb_documents SET deleted = true, deleted_at = now(), deleted_by = $1, version = version + 1 WHERE id = $2 AND deleted = false`,
		deletedBy, id)
	if err != nil {
		return fmt.Errorf("delete kb document: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return kb.ErrDocumentNotFound
	}
	// 同步软删除分块（保留数据便于恢复，但检索时 WHERE deleted=false 过滤）。
	_, _ = r.pool.Exec(ctx,
		`UPDATE vo_kb_chunks SET deleted = true, deleted_at = now(), deleted_by = $1 WHERE document_id = $2 AND deleted = false`,
		deletedBy, id)
	return nil
}

func scanDocument(row pgx.Row) (*kb.Document, error) {
	d := &kb.Document{Tags: []string{}}
	var (
		categoryID  *int64
		sourceURL   *string
		tagsBytes   []byte
		createdBy   *int64
		updatedBy   *int64
		deletedBy   *int64
	)
	if err := row.Scan(
		&d.ID, &d.UUID, &categoryID, &d.Title, &d.SourceType, &sourceURL, &d.Content, &tagsBytes,
		&d.ChunkCount, &d.Status, &d.Version, &d.CreatedAt, &createdBy, &d.UpdatedAt, &updatedBy,
		&d.Deleted, &d.DeletedAt, &deletedBy,
	); err != nil {
		return nil, err
	}
	if categoryID != nil {
		d.CategoryID = *categoryID
	}
	if sourceURL != nil {
		d.SourceURL = *sourceURL
	}
	if len(tagsBytes) > 0 {
		_ = json.Unmarshal(tagsBytes, &d.Tags)
	}
	if d.Tags == nil {
		d.Tags = []string{}
	}
	if createdBy != nil {
		d.CreatedBy = *createdBy
	}
	if updatedBy != nil {
		d.UpdatedBy = *updatedBy
	}
	if deletedBy != nil {
		d.DeletedBy = *deletedBy
	}
	return d, nil
}

// ==================== 分块 ====================

func (r *Repository) CreateChunks(ctx context.Context, chunks []*kb.Chunk) error {
	if len(chunks) == 0 {
		return nil
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	for _, c := range chunks {
		_, err := tx.Exec(ctx, `
INSERT INTO vo_kb_chunks (document_id, chunk_index, content, embedding, token_count, version, created_by, updated_by)
VALUES ($1, $2, $3, $4, $5, 1, $6, $6)`,
			c.DocumentID, c.ChunkIndex, c.Content, vectorToPGString(c.Embedding), c.TokenCount, c.CreatedBy)
		if err != nil {
			return fmt.Errorf("insert kb chunk: %w", err)
		}
	}
	// 更新文档 chunk_count。
	if len(chunks) > 0 {
		_, err = tx.Exec(ctx, `
UPDATE vo_kb_documents SET chunk_count = $1, status = 'active', updated_at = now() WHERE id = $2`,
			len(chunks), chunks[0].DocumentID)
		if err != nil {
			return fmt.Errorf("update document chunk_count: %w", err)
		}
	}
	return tx.Commit(ctx)
}

func (r *Repository) DeleteChunksByDocument(ctx context.Context, documentID int64) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM vo_kb_chunks WHERE document_id = $1`, documentID)
	if err != nil {
		return fmt.Errorf("delete kb chunks: %w", err)
	}
	return nil
}

func (r *Repository) ListChunksByDocument(ctx context.Context, documentID int64) ([]*kb.Chunk, error) {
	rows, err := r.pool.Query(ctx, `
SELECT id, uuid, document_id, chunk_index, content, token_count, version, created_at
FROM vo_kb_chunks WHERE document_id = $1 AND deleted = false ORDER BY chunk_index ASC`, documentID)
	if err != nil {
		return nil, fmt.Errorf("list kb chunks: %w", err)
	}
	defer rows.Close()
	out := make([]*kb.Chunk, 0)
	for rows.Next() {
		c := &kb.Chunk{}
		if err := rows.Scan(&c.ID, &c.UUID, &c.DocumentID, &c.ChunkIndex, &c.Content, &c.TokenCount, &c.Version, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SearchByVector 按向量相似度检索分块（余弦相似度，pgvector <=> 操作符）。
func (r *Repository) SearchByVector(ctx context.Context, embedding []float32, topK int, categoryCode string) ([]*kb.ChunkResult, error) {
	if len(embedding) == 0 {
		return nil, kb.ErrInvalidEmbedding
	}
	if topK <= 0 {
		topK = 5
	}
	vec := vectorToPGString(embedding)
	args := []any{vec, topK}
	argIdx := 3
	categoryCond := ""
	if categoryCode != "" {
		categoryCond = fmt.Sprintf(" AND c.code = $%d", argIdx)
		args = append(args, categoryCode)
	}
	query := fmt.Sprintf(`
SELECT ch.id, ch.uuid, ch.document_id, ch.chunk_index, ch.content, ch.token_count,
       ch.version, ch.created_at,
       d.title AS document_title, c.code AS category_code,
       1 - (ch.embedding <=> $1::vector) AS score
FROM vo_kb_chunks ch
JOIN vo_kb_documents d ON d.id = ch.document_id
LEFT JOIN vo_kb_categories c ON c.id = d.category_id
WHERE ch.deleted = false AND d.deleted = false AND d.status = 'active'%s
ORDER BY ch.embedding <=> $1::vector
LIMIT $2`, categoryCond)
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("search kb chunks by vector: %w", err)
	}
	defer rows.Close()
	out := make([]*kb.ChunkResult, 0)
	for rows.Next() {
		cr := &kb.ChunkResult{}
		if err := rows.Scan(
			&cr.ID, &cr.UUID, &cr.DocumentID, &cr.ChunkIndex, &cr.Content, &cr.TokenCount,
			&cr.Version, &cr.CreatedAt,
			&cr.DocumentTitle, &cr.CategoryCode, &cr.Score,
		); err != nil {
			return nil, err
		}
		out = append(out, cr)
	}
	return out, rows.Err()
}

// vectorToPGString 将 float32 切片序列化为 pgvector 接受的字符串格式：[0.1,0.2,...]
func vectorToPGString(v []float32) string {
	if len(v) == 0 {
		return "[]"
	}
	var b strings.Builder
	b.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(f), 'f', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}

// nullableInt64 0 视为 NULL。
func nullableInt64(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

// defaultStr 空字符串返回默认值。
func defaultStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// prefixColumns 给逗号分隔的列名列表加表别名前缀，例如 "id, uuid" -> "d.id, d.uuid"。
// 用于 JOIN 查询时消解列名歧义。
func prefixColumns(columns, alias string) string {
	parts := strings.Split(columns, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, alias+"."+p)
	}
	return strings.Join(out, ", ")
}
