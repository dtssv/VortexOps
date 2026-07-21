// Package monitoringhttp 是监控查询的 HTTP handlers。
// 暴露 Prometheus 即时查询、范围查询与告警规则评估触发端点。
package monitoringhttp

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/vortexops/vortexops/internal/application/monitoringapp"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpx"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// Handler 处理 /api/v1/monitoring 路由。
type Handler struct {
	svc *monitoringapp.Service
}

// NewHandler 创建监控 handler。
func NewHandler(svc *monitoringapp.Service) *Handler {
	return &Handler{svc: svc}
}

// Query POST /api/v1/monitoring/query
// Body: { "query": "up", "time": "2026-06-23T00:00:00Z" }
func (h *Handler) Query(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query string `json:"query"`
		Time  string `json:"time"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	if req.Query == "" {
		httpx.WriteError(w, apperr.Validation("query is required", nil))
		return
	}
	results, err := h.svc.Query(r.Context(), req.Query)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, results)
}

// QueryRange POST /api/v1/monitoring/query-range
// Body: { "query": "rate(cpu_usage[5m])", "start": "...", "end": "...", "step": "1m" }
func (h *Handler) QueryRange(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query string `json:"query"`
		Start string `json:"start"`
		End   string `json:"end"`
		Step  string `json:"step"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	if req.Query == "" {
		httpx.WriteError(w, apperr.Validation("query is required", nil))
		return
	}
	start, err := parseTime(req.Start)
	if err != nil {
		httpx.WriteError(w, apperr.Validation("invalid start time", err))
		return
	}
	end, err := parseTime(req.End)
	if err != nil {
		httpx.WriteError(w, apperr.Validation("invalid end time", err))
		return
	}
	if end.IsZero() {
		end = time.Now()
	}
	if start.IsZero() {
		start = end.Add(-1 * time.Hour)
	}
	results, err := h.svc.QueryRange(r.Context(), monitoringapp.QueryRangeInput{
		PromQL: req.Query, Start: start, End: end, Step: req.Step,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, results)
}

// EvaluateRules POST /api/v1/monitoring/evaluate-rules
// 手动触发告警规则评估（通常由 worker 周期调用）。
func (h *Handler) EvaluateRules(w http.ResponseWriter, r *http.Request) {
	count, err := h.svc.EvaluateRules(r.Context())
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, map[string]any{"triggered": count})
}

func parseTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	// 先尝试 RFC3339
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	// 再尝试 Unix 时间戳
	if ts, err := strconv.ParseFloat(s, 64); err == nil {
		sec := int64(ts)
		nsec := int64((ts - float64(sec)) * 1e9)
		return time.Unix(sec, nsec), nil
	}
	return time.Time{}, apperr.Validation("invalid time format: use RFC3339 or unix timestamp", nil)
}
