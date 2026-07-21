// Package elasticsearch 提供 Elasticsearch HTTP 客户端（审计/日志全文检索）。
package elasticsearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/vortexops/vortexops/internal/config"
)

// Client ES HTTP 客户端。
type Client struct {
	baseURL    string
	indexAudit string
	indexLogs  string
	username   string
	password   string
	enabled    bool
	http       *http.Client
}

// New 从配置创建 ES 客户端。未启用时 Search 返回空结果。
func New(cfg config.ElasticsearchConfig) *Client {
	if !cfg.Enabled {
		return &Client{enabled: false}
	}
	base := strings.TrimRight(cfg.URL, "/")
	return &Client{
		baseURL:    base,
		indexAudit: cfg.IndexAudit,
		indexLogs:  cfg.IndexLogs,
		username:   cfg.Username,
		password:   cfg.Password,
		enabled:    true,
		http:       &http.Client{Timeout: cfg.Timeout},
	}
}

// Enabled 是否启用 ES。
func (c *Client) Enabled() bool { return c.enabled }

// SearchRequest 搜索请求。
type SearchRequest struct {
	Index   string
	Query   string
	From    int
	Size    int
	Filters map[string]string
}

// Hit 搜索命中。
type Hit struct {
	ID     string         `json:"_id"`
	Index  string         `json:"_index"`
	Score  float64        `json:"_score"`
	Source map[string]any `json:"_source"`
}

// SearchResult 搜索结果。
type SearchResult struct {
	Total int64 `json:"total"`
	Hits  []Hit `json:"hits"`
}

// SearchAudit 搜索审计日志索引。
func (c *Client) SearchAudit(ctx context.Context, query string, from, size int) (*SearchResult, error) {
	return c.Search(ctx, SearchRequest{Index: c.indexAudit, Query: query, From: from, Size: size})
}

// SearchLogs 搜索构建/应用日志索引。
func (c *Client) SearchLogs(ctx context.Context, query string, from, size int) (*SearchResult, error) {
	return c.Search(ctx, SearchRequest{Index: c.indexLogs, Query: query, From: from, Size: size})
}

// Search 执行 ES _search。
func (c *Client) Search(ctx context.Context, req SearchRequest) (*SearchResult, error) {
	if !c.enabled {
		return &SearchResult{Total: 0, Hits: []Hit{}}, nil
	}
	if req.Size <= 0 {
		req.Size = 20
	}
	if req.Size > 100 {
		req.Size = 100
	}
	must := []map[string]any{}
	if req.Query != "" {
		must = append(must, map[string]any{
			"multi_match": map[string]any{
				"query":  req.Query,
				"fields": []string{"message", "body", "operation", "resource_name", "user_name"},
			},
		})
	}
	for k, v := range req.Filters {
		must = append(must, map[string]any{
			"term": map[string]any{k: v},
		})
	}
	body := map[string]any{
		"from": req.From,
		"size": req.Size,
		"query": map[string]any{
			"bool": map[string]any{"must": must},
		},
	}
	if len(must) == 0 {
		body["query"] = map[string]any{"match_all": map[string]any{}}
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	index := req.Index
	if index == "" {
		index = "_all"
	}
	url := fmt.Sprintf("%s/%s/_search", c.baseURL, index)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.username != "" {
		httpReq.SetBasicAuth(c.username, c.password)
	}
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("es search: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("es search status %d: %s", resp.StatusCode, string(raw))
	}
	return parseSearchResponse(raw)
}

func parseSearchResponse(raw []byte) (*SearchResult, error) {
	var parsed struct {
		Hits struct {
			Total struct {
				Value int64 `json:"value"`
			} `json:"total"`
			Hits []Hit `json:"hits"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	return &SearchResult{Total: parsed.Hits.Total.Value, Hits: parsed.Hits.Hits}, nil
}

// Ping 健康检查。
func (c *Client) Ping(ctx context.Context) error {
	if !c.enabled {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL, nil)
	if err != nil {
		return err
	}
	if c.username != "" {
		req.SetBasicAuth(c.username, c.password)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("es ping status %d", resp.StatusCode)
	}
	return nil
}

// DefaultTimeout 默认 ES 请求超时。
const DefaultTimeout = 10 * time.Second
