// Package alertapp 是告警规则 CRUD 与事件查询应用服务。
package alertapp

import (
	"context"
	"errors"
	"strconv"

	"github.com/vortexops/vortexops/internal/domain/alert"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// Service 告警应用服务。
type Service struct {
	repo alert.Repository
}

// New 创建告警服务。
func New(repo alert.Repository) *Service {
	return &Service{repo: repo}
}

// CreateRuleInput 创建规则输入。
type CreateRuleInput struct {
	Scope           alert.Scope
	ScopeID         int64
	Name            string
	Description     string
	Metric          string
	Condition       alert.Condition
	Threshold       *float64
	WindowMinutes   int
	Severity        alert.Severity
	Enabled         bool
	NotifyChannels  []int64
	CooldownMinutes int
	CreatedBy       int64
}

// CreateRule 创建告警规则。
func (s *Service) CreateRule(ctx context.Context, in CreateRuleInput) (*alert.Rule, error) {
	if in.Name == "" || in.Metric == "" || in.Condition == "" {
		return nil, apperr.Validation("name, metric and condition are required", nil)
	}
	if in.Scope == "" {
		in.Scope = alert.ScopeWorkspace
	}
	if in.Severity == "" {
		in.Severity = alert.SeverityWarning
	}
	if in.WindowMinutes <= 0 {
		in.WindowMinutes = 5
	}
	if in.CooldownMinutes <= 0 {
		in.CooldownMinutes = 30
	}
	rule := &alert.Rule{
		Scope: in.Scope, ScopeID: in.ScopeID, Name: in.Name, Description: in.Description,
		Metric: in.Metric, Condition: in.Condition, Threshold: in.Threshold,
		WindowMinutes: in.WindowMinutes, Severity: in.Severity, Enabled: in.Enabled,
		NotifyChannels: in.NotifyChannels, CooldownMinutes: in.CooldownMinutes, CreatedBy: in.CreatedBy,
	}
	if err := s.repo.CreateRule(ctx, rule); err != nil {
		return nil, apperr.Internal("create alert rule", err)
	}
	return rule, nil
}

// GetRule 查询规则。
func (s *Service) GetRule(ctx context.Context, id int64) (*alert.Rule, error) {
	rule, err := s.repo.GetRuleByID(ctx, id)
	if err != nil {
		if errors.Is(err, alert.ErrRuleNotFound) {
			return nil, apperr.NotFound("alert_rule", strconv.FormatInt(id, 10))
		}
		return nil, apperr.Internal("get alert rule", err)
	}
	return rule, nil
}

// UpdateRuleInput 更新规则输入。
type UpdateRuleInput struct {
	ID              int64
	Name            string
	Description     string
	Metric          string
	Condition       alert.Condition
	Threshold       *float64
	WindowMinutes   int
	Severity        alert.Severity
	Enabled         bool
	NotifyChannels  []int64
	CooldownMinutes int
	UpdatedBy       int64
}

// UpdateRule 更新告警规则。
func (s *Service) UpdateRule(ctx context.Context, in UpdateRuleInput) (*alert.Rule, error) {
	rule, err := s.repo.GetRuleByID(ctx, in.ID)
	if err != nil {
		if errors.Is(err, alert.ErrRuleNotFound) {
			return nil, apperr.NotFound("alert_rule", strconv.FormatInt(in.ID, 10))
		}
		return nil, apperr.Internal("get alert rule", err)
	}
	rule.Name = in.Name
	rule.Description = in.Description
	rule.Metric = in.Metric
	rule.Condition = in.Condition
	rule.Threshold = in.Threshold
	rule.WindowMinutes = in.WindowMinutes
	rule.Severity = in.Severity
	rule.Enabled = in.Enabled
	rule.NotifyChannels = in.NotifyChannels
	rule.CooldownMinutes = in.CooldownMinutes
	rule.UpdatedBy = in.UpdatedBy
	if err := s.repo.UpdateRule(ctx, rule); err != nil {
		return nil, apperr.Internal("update alert rule", err)
	}
	return rule, nil
}

// DeleteRule 删除告警规则。
func (s *Service) DeleteRule(ctx context.Context, id, deletedBy int64) error {
	if err := s.repo.DeleteRule(ctx, id, deletedBy); err != nil {
		if errors.Is(err, alert.ErrRuleNotFound) {
			return apperr.NotFound("alert_rule", strconv.FormatInt(id, 10))
		}
		return apperr.Internal("delete alert rule", err)
	}
	return nil
}

// ListRules 分页查询规则。
func (s *Service) ListRules(ctx context.Context, q alert.RuleQuery) ([]*alert.Rule, int64, error) {
	items, total, err := s.repo.ListRules(ctx, q)
	if err != nil {
		return nil, 0, apperr.Internal("list alert rules", err)
	}
	return items, total, nil
}

// ListEvents 分页查询告警事件。
func (s *Service) ListEvents(ctx context.Context, q alert.EventQuery) ([]*alert.Event, int64, error) {
	items, total, err := s.repo.ListEvents(ctx, q)
	if err != nil {
		return nil, 0, apperr.Internal("list alert events", err)
	}
	return items, total, nil
}

// GetEvent 查询单条事件。
func (s *Service) GetEvent(ctx context.Context, id int64) (*alert.Event, error) {
	evt, err := s.repo.GetEventByID(ctx, id)
	if err != nil {
		if errors.Is(err, alert.ErrEventNotFound) {
			return nil, apperr.NotFound("alert_event", strconv.FormatInt(id, 10))
		}
		return nil, apperr.Internal("get alert event", err)
	}
	return evt, nil
}

// CreateEvent 由告警评估器在规则触发时调用，落库一条 firing 事件。
func (s *Service) CreateEvent(ctx context.Context, evt *alert.Event) error {
	if err := s.repo.CreateEvent(ctx, evt); err != nil {
		return apperr.Internal("create alert event", err)
	}
	return nil
}
