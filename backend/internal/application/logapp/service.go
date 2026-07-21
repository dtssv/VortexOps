// Package logapp 是日志流与 ES 搜索应用服务。
package logapp

import (
	"context"
	"fmt"
	"io"

	"github.com/vortexops/vortexops/internal/infrastructure/elasticsearch"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// Service 日志应用服务。
type Service struct {
	es *elasticsearch.Client
}

// New 创建日志服务。
func New(es *elasticsearch.Client) *Service {
	return &Service{es: es}
}

// SearchInput 日志搜索输入。
type SearchInput struct {
	Query       string
	WorkspaceID int64
	From        int
	Size        int
	Index       string
}

// SearchResultItem 搜索结果项。
type SearchResultItem struct {
	ID     string         `json:"id"`
	Index  string         `json:"index"`
	Score  float64        `json:"score"`
	Source map[string]any `json:"source"`
}

// Search 跨 Pod/构建日志 ES 全文搜索；ES 未启用时返回空结果。
func (s *Service) Search(ctx context.Context, in SearchInput) ([]SearchResultItem, int64, error) {
	if s.es == nil {
		return []SearchResultItem{}, 0, nil
	}
	req := elasticsearch.SearchRequest{
		Query: in.Query,
		From:  in.From,
		Size:  in.Size,
		Index: in.Index,
	}
	if in.WorkspaceID != 0 {
		req.Filters = map[string]string{
			"workspace_id": fmt.Sprintf("%d", in.WorkspaceID),
		}
	}
	var (
		res *elasticsearch.SearchResult
		err error
	)
	if in.Index != "" {
		res, err = s.es.Search(ctx, req)
	} else {
		res, err = s.es.SearchLogs(ctx, in.Query, in.From, in.Size)
	}
	if err != nil {
		return nil, 0, apperr.Internal("search logs", err)
	}
	items := make([]SearchResultItem, 0, len(res.Hits))
	for _, h := range res.Hits {
		items = append(items, SearchResultItem{ID: h.ID, Index: h.Index, Score: h.Score, Source: h.Source})
	}
	return items, res.Total, nil
}

// StreamInput Pod 日志流输入（stub：返回占位说明，完整实现经 ws-gateway + log-proxy）。
type StreamInput struct {
	ClusterID int64
	Namespace string
	Pod       string
	Container string
	Follow    bool
	TailLines int64
}

// Stream 日志流 stub：写入占位行，真实流由 ws-gateway 代理。
func (s *Service) Stream(ctx context.Context, in StreamInput, w io.Writer) error {
	if in.ClusterID == 0 || in.Namespace == "" || in.Pod == "" {
		return apperr.Validation("cluster_id, namespace and pod are required", nil)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	line := fmt.Sprintf("[vortexops] log stream stub cluster=%d ns=%s pod=%s follow=%v tail=%d\n",
		in.ClusterID, in.Namespace, in.Pod, in.Follow, in.TailLines)
	_, err := io.WriteString(w, line)
	return err
}

// SearchAudit 审计日志 ES 搜索。
func (s *Service) SearchAudit(ctx context.Context, query string, from, size int) ([]SearchResultItem, int64, error) {
	if s.es == nil || !s.es.Enabled() {
		return []SearchResultItem{}, 0, nil
	}
	res, err := s.es.SearchAudit(ctx, query, from, size)
	if err != nil {
		return nil, 0, apperr.Internal("search audit logs", err)
	}
	items := make([]SearchResultItem, 0, len(res.Hits))
	for _, h := range res.Hits {
		items = append(items, SearchResultItem{ID: h.ID, Index: h.Index, Score: h.Score, Source: h.Source})
	}
	return items, res.Total, nil
}
