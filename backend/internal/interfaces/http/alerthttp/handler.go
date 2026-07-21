// Package alerthttp 是告警规则与事件 HTTP handlers。
package alerthttp

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/vortexops/vortexops/internal/application/alertapp"
	"github.com/vortexops/vortexops/internal/domain/alert"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpauth"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpx"
)

// Handler 告警 HTTP handler。
type Handler struct {
	svc *alertapp.Service
}

// NewHandler 创建 handler。
func NewHandler(svc *alertapp.Service) *Handler {
	return &Handler{svc: svc}
}

// CreateRule POST /api/v1/alert-rules
func (h *Handler) CreateRule(w http.ResponseWriter, r *http.Request) {
	var req createRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, err)
		return
	}
	rule, err := h.svc.CreateRule(r.Context(), alertapp.CreateRuleInput{
		Scope: alert.Scope(req.Scope), ScopeID: req.ScopeID, Name: req.Name, Description: req.Description,
		Metric: req.Metric, Condition: alert.Condition(req.Condition), Threshold: req.Threshold,
		WindowMinutes: req.WindowMinutes, Severity: alert.Severity(req.Severity), Enabled: req.Enabled,
		NotifyChannels: req.NotifyChannels, CooldownMinutes: req.CooldownMinutes,
		CreatedBy: httpauth.UserID(r.Context()),
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Created(w, toRuleDTO(rule))
}

// GetRule GET /api/v1/alert-rules/{id}
func (h *Handler) GetRule(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.WriteError(w, err)
		return
	}
	rule, err := h.svc.GetRule(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toRuleDTO(rule))
}

// UpdateRule PUT /api/v1/alert-rules/{id}
func (h *Handler) UpdateRule(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.WriteError(w, err)
		return
	}
	var req updateRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, err)
		return
	}
	rule, err := h.svc.UpdateRule(r.Context(), alertapp.UpdateRuleInput{
		ID: id, Name: req.Name, Description: req.Description, Metric: req.Metric,
		Condition: alert.Condition(req.Condition), Threshold: req.Threshold,
		WindowMinutes: req.WindowMinutes, Severity: alert.Severity(req.Severity),
		Enabled: req.Enabled, NotifyChannels: req.NotifyChannels,
		CooldownMinutes: req.CooldownMinutes, UpdatedBy: httpauth.UserID(r.Context()),
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toRuleDTO(rule))
}

// DeleteRule DELETE /api/v1/alert-rules/{id}
func (h *Handler) DeleteRule(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.WriteError(w, err)
		return
	}
	if err := h.svc.DeleteRule(r.Context(), id, httpauth.UserID(r.Context())); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// ListRules GET /api/v1/alert-rules
func (h *Handler) ListRules(w http.ResponseWriter, r *http.Request) {
	page, size, _ := httpx.Pagination(r)
	scopeID, _ := strconv.ParseInt(r.URL.Query().Get("scope_id"), 10, 64)
	var enabled *bool
	if v := r.URL.Query().Get("enabled"); v != "" {
		b := v == "true" || v == "1"
		enabled = &b
	}
	items, total, err := h.svc.ListRules(r.Context(), alert.RuleQuery{
		Scope: alert.Scope(r.URL.Query().Get("scope")), ScopeID: scopeID, Enabled: enabled,
		Offset: (page - 1) * size, Limit: size,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, httpx.Paged[ruleDTO]{
		Items: toRuleDTOs(items), Total: total, Page: page, Size: size,
	})
}

// ListEvents GET /api/v1/alert-events
func (h *Handler) ListEvents(w http.ResponseWriter, r *http.Request) {
	page, size, _ := httpx.Pagination(r)
	ruleID, _ := strconv.ParseInt(r.URL.Query().Get("rule_id"), 10, 64)
	scopeID, _ := strconv.ParseInt(r.URL.Query().Get("scope_id"), 10, 64)
	start, _ := time.Parse(time.RFC3339, r.URL.Query().Get("start"))
	end, _ := time.Parse(time.RFC3339, r.URL.Query().Get("end"))
	items, total, err := h.svc.ListEvents(r.Context(), alert.EventQuery{
		RuleID: ruleID, Scope: alert.Scope(r.URL.Query().Get("scope")), ScopeID: scopeID,
		Status: alert.EventStatus(r.URL.Query().Get("status")), StartTime: start, EndTime: end,
		Offset: (page - 1) * size, Limit: size,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, httpx.Paged[eventDTO]{
		Items: toEventDTOs(items), Total: total, Page: page, Size: size,
	})
}

// GetEvent GET /api/v1/alert-events/{id}
func (h *Handler) GetEvent(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.WriteError(w, err)
		return
	}
	evt, err := h.svc.GetEvent(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toEventDTO(evt))
}

type createRuleRequest struct {
	Scope           string   `json:"scope"`
	ScopeID         int64    `json:"scope_id"`
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	Metric          string   `json:"metric"`
	Condition       string   `json:"condition"`
	Threshold       *float64 `json:"threshold"`
	WindowMinutes   int      `json:"window_minutes"`
	Severity        string   `json:"severity"`
	Enabled         bool     `json:"enabled"`
	NotifyChannels  []int64  `json:"notify_channels"`
	CooldownMinutes int      `json:"cooldown_minutes"`
}

type updateRuleRequest struct {
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	Metric          string   `json:"metric"`
	Condition       string   `json:"condition"`
	Threshold       *float64 `json:"threshold"`
	WindowMinutes   int      `json:"window_minutes"`
	Severity        string   `json:"severity"`
	Enabled         bool     `json:"enabled"`
	NotifyChannels  []int64  `json:"notify_channels"`
	CooldownMinutes int      `json:"cooldown_minutes"`
}

type ruleDTO struct {
	ID              int64    `json:"id"`
	UUID            string   `json:"uuid"`
	Scope           string   `json:"scope"`
	ScopeID         int64    `json:"scope_id,omitempty"`
	Name            string   `json:"name"`
	Description     string   `json:"description,omitempty"`
	Metric          string   `json:"metric"`
	Condition       string   `json:"condition"`
	Threshold       *float64 `json:"threshold,omitempty"`
	WindowMinutes   int      `json:"window_minutes"`
	Severity        string   `json:"severity"`
	Enabled         bool     `json:"enabled"`
	NotifyChannels  []int64  `json:"notify_channels"`
	CooldownMinutes int      `json:"cooldown_minutes"`
	Version         int      `json:"version"`
	CreatedAt       string   `json:"created_at"`
	UpdatedAt       string   `json:"updated_at"`
}

type eventDTO struct {
	ID           int64    `json:"id"`
	UUID         string   `json:"uuid"`
	RuleID       int64    `json:"rule_id"`
	Scope        string   `json:"scope"`
	ScopeID      int64    `json:"scope_id,omitempty"`
	ResourceType string   `json:"resource_type,omitempty"`
	ResourceID   int64    `json:"resource_id,omitempty"`
	Severity     string   `json:"severity"`
	Status       string   `json:"status"`
	Message      string   `json:"message,omitempty"`
	CurrentValue *float64 `json:"current_value,omitempty"`
	FiredAt      string   `json:"fired_at"`
	ResolvedAt   string   `json:"resolved_at,omitempty"`
}

func toRuleDTO(r *alert.Rule) ruleDTO {
	return ruleDTO{
		ID: r.ID, UUID: r.UUID.String(), Scope: string(r.Scope), ScopeID: r.ScopeID,
		Name: r.Name, Description: r.Description, Metric: r.Metric, Condition: string(r.Condition),
		Threshold: r.Threshold, WindowMinutes: r.WindowMinutes, Severity: string(r.Severity),
		Enabled: r.Enabled, NotifyChannels: r.NotifyChannels, CooldownMinutes: r.CooldownMinutes,
		Version: r.Version, CreatedAt: r.CreatedAt.Format(time.RFC3339), UpdatedAt: r.UpdatedAt.Format(time.RFC3339),
	}
}

func toRuleDTOs(items []*alert.Rule) []ruleDTO {
	out := make([]ruleDTO, 0, len(items))
	for _, r := range items {
		out = append(out, toRuleDTO(r))
	}
	return out
}

func toEventDTO(e *alert.Event) eventDTO {
	d := eventDTO{
		ID: e.ID, UUID: e.UUID.String(), RuleID: e.RuleID, Scope: string(e.Scope), ScopeID: e.ScopeID,
		ResourceType: e.ResourceType, ResourceID: e.ResourceID, Severity: string(e.Severity),
		Status: string(e.Status), Message: e.Message, CurrentValue: e.CurrentValue,
		FiredAt: e.FiredAt.Format(time.RFC3339),
	}
	if e.ResolvedAt != nil {
		d.ResolvedAt = e.ResolvedAt.Format(time.RFC3339)
	}
	return d
}

func toEventDTOs(items []*alert.Event) []eventDTO {
	out := make([]eventDTO, 0, len(items))
	for _, e := range items {
		out = append(out, toEventDTO(e))
	}
	return out
}
