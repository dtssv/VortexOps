package kbhttp

import (
	"time"

	"github.com/vortexops/vortexops/internal/domain/kb"
)

type categoryDTO struct {
	ID          int64    `json:"id"`
	UUID        string   `json:"uuid"`
	Name        string   `json:"name"`
	Code        string   `json:"code"`
	Description string   `json:"description"`
	SortOrder   int      `json:"sort_order"`
	Tags        []string `json:"tags,omitempty"`
}

func toCategoryDTO(c *kb.Category) categoryDTO {
	return categoryDTO{
		ID: c.ID, UUID: c.UUID, Name: c.Name, Code: c.Code,
		Description: c.Description, SortOrder: c.SortOrder,
	}
}

func toCategoryDTOs(items []*kb.Category) []categoryDTO {
	out := make([]categoryDTO, 0, len(items))
	for _, c := range items {
		out = append(out, toCategoryDTO(c))
	}
	return out
}

type documentDTO struct {
	ID           int64      `json:"id"`
	UUID         string     `json:"uuid"`
	CategoryID   int64      `json:"category_id"`
	Title        string     `json:"title"`
	SourceType   string     `json:"source_type"`
	SourceURL    string     `json:"source_url"`
	Content      string     `json:"content"`
	Tags         []string   `json:"tags"`
	ChunkCount   int        `json:"chunk_count"`
	Status       string     `json:"status"`
	Version      int        `json:"version"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

func toDocumentDTO(d *kb.Document) documentDTO {
	return documentDTO{
		ID: d.ID, UUID: d.UUID, CategoryID: d.CategoryID, Title: d.Title,
		SourceType: d.SourceType, SourceURL: d.SourceURL, Content: d.Content,
		Tags: d.Tags, ChunkCount: d.ChunkCount, Status: d.Status,
		Version: d.Version, CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt,
	}
}

func toDocumentDTOs(items []*kb.Document) []documentDTO {
	out := make([]documentDTO, 0, len(items))
	for _, d := range items {
		out = append(out, toDocumentDTO(d))
	}
	return out
}

type chunkResultDTO struct {
	ID            int64   `json:"id"`
	DocumentID    int64   `json:"document_id"`
	ChunkIndex    int     `json:"chunk_index"`
	Content       string  `json:"content"`
	DocumentTitle string  `json:"document_title"`
	CategoryCode  string  `json:"category_code"`
	Score         float64 `json:"score"`
}

func toChunkResultDTO(c *kb.ChunkResult) chunkResultDTO {
	return chunkResultDTO{
		ID: c.ID, DocumentID: c.DocumentID, ChunkIndex: c.ChunkIndex,
		Content: c.Content, DocumentTitle: c.DocumentTitle,
		CategoryCode: c.CategoryCode, Score: c.Score,
	}
}

func toChunkResultDTOs(items []*kb.ChunkResult) []chunkResultDTO {
	out := make([]chunkResultDTO, 0, len(items))
	for _, c := range items {
		out = append(out, toChunkResultDTO(c))
	}
	return out
}
