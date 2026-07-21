// Package alert 是告警规则与告警事件领域的核心实体与仓储接口。
package alert

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Scope 告警规则作用域。
type Scope string

const (
	ScopePlatform   Scope = "platform"
	ScopeWorkspace  Scope = "workspace"
	ScopeApplication Scope = "application"
	ScopeCluster    Scope = "cluster"
)

// Condition 告警条件运算符。
type Condition string

const (
	CondGT  Condition = "gt"
	CondGTE Condition = "gte"
	CondLT  Condition = "lt"
	CondLTE Condition = "lte"
	CondEQ  Condition = "eq"
	CondNEQ Condition = "neq"
)

// Severity 告警严重级别。
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

// EventStatus 告警事件状态。
type EventStatus string

const (
	EventFiring     EventStatus = "firing"
	EventResolved   EventStatus = "resolved"
	EventSuppressed EventStatus = "suppressed"
)

// Rule 告警规则。
type Rule struct {
	ID               int64
	UUID             uuid.UUID
	Scope            Scope
	ScopeID          int64
	Name             string
	Description      string
	Metric           string
	Condition        Condition
	Threshold        *float64
	WindowMinutes    int
	Severity         Severity
	Enabled          bool
	NotifyChannels   []int64
	CooldownMinutes  int
	Version          int
	CreatedAt        time.Time
	CreatedBy        int64
	UpdatedAt        time.Time
	UpdatedBy        int64
	Deleted          bool
	DeletedAt        *time.Time
	DeletedBy        int64
}

// Event 告警事件。
type Event struct {
	ID            int64
	UUID          uuid.UUID
	RuleID        int64
	Scope         Scope
	ScopeID       int64
	ResourceType  string
	ResourceID    int64
	Severity      Severity
	Status        EventStatus
	Message       string
	CurrentValue  *float64
	FiredAt       time.Time
	ResolvedAt    *time.Time
	NotifiedCount int
	Version       int
	CreatedAt     time.Time
	CreatedBy     int64
	UpdatedAt     time.Time
	UpdatedBy     int64
	Deleted       bool
	DeletedAt     *time.Time
	DeletedBy     int64
}

// RuleQuery 告警规则查询。
type RuleQuery struct {
	Scope    Scope
	ScopeID  int64
	Enabled  *bool
	Offset   int
	Limit    int
}

// EventQuery 告警事件查询。
type EventQuery struct {
	RuleID    int64
	Scope     Scope
	ScopeID   int64
	Status    EventStatus
	StartTime time.Time
	EndTime   time.Time
	Offset    int
	Limit     int
}

var (
	ErrRuleNotFound  = errors.New("alert rule not found")
	ErrEventNotFound = errors.New("alert event not found")
)

// Repository 告警规则与事件仓储接口。
type Repository interface {
	CreateRule(ctx context.Context, rule *Rule) error
	GetRuleByID(ctx context.Context, id int64) (*Rule, error)
	UpdateRule(ctx context.Context, rule *Rule) error
	DeleteRule(ctx context.Context, id int64, deletedBy int64) error
	ListRules(ctx context.Context, q RuleQuery) ([]*Rule, int64, error)

	CreateEvent(ctx context.Context, evt *Event) error
	GetEventByID(ctx context.Context, id int64) (*Event, error)
	ListEvents(ctx context.Context, q EventQuery) ([]*Event, int64, error)
}
