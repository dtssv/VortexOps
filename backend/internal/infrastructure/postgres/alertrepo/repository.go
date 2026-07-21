// Package alertrepo 是告警领域的 PostgreSQL 仓储实现。
package alertrepo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vortexops/vortexops/internal/domain/alert"
)

// Repository 告警 PostgreSQL 仓储。
type Repository struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

// New 创建仓储。
func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool, now: time.Now}
}

const ruleColumns = `id, uuid, scope, scope_id, name, description, metric, condition, threshold,
	window_minutes, severity, enabled, notify_channels, cooldown_minutes, version,
	created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by`

const eventColumns = `id, uuid, rule_id, scope, scope_id, resource_type, resource_id, severity, status,
	message, current_value, fired_at, resolved_at, notified_count, version,
	created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by`

func scanRule(row pgx.Row) (*alert.Rule, error) {
	r := &alert.Rule{}
	var (
		scopeID     *int64
		description *string
		threshold   *float64
		createdBy   *int64
		updatedBy   *int64
		deletedAt   *time.Time
		deletedBy   *int64
	)
	if err := row.Scan(
		&r.ID, &r.UUID, &r.Scope, &scopeID, &r.Name, &description, &r.Metric, &r.Condition, &threshold,
		&r.WindowMinutes, &r.Severity, &r.Enabled, &r.NotifyChannels, &r.CooldownMinutes, &r.Version,
		&r.CreatedAt, &createdBy, &r.UpdatedAt, &updatedBy, &r.Deleted, &deletedAt, &deletedBy,
	); err != nil {
		return nil, err
	}
	if scopeID != nil {
		r.ScopeID = *scopeID
	}
	if description != nil {
		r.Description = *description
	}
	r.Threshold = threshold
	if createdBy != nil {
		r.CreatedBy = *createdBy
	}
	if updatedBy != nil {
		r.UpdatedBy = *updatedBy
	}
	r.DeletedAt = deletedAt
	if deletedBy != nil {
		r.DeletedBy = *deletedBy
	}
	return r, nil
}

func scanEvent(row pgx.Row) (*alert.Event, error) {
	e := &alert.Event{}
	var (
		scopeID      *int64
		resourceType *string
		resourceID   *int64
		message      *string
		currentValue *float64
		resolvedAt   *time.Time
		createdBy    *int64
		updatedBy    *int64
		deletedAt    *time.Time
		deletedBy    *int64
	)
	if err := row.Scan(
		&e.ID, &e.UUID, &e.RuleID, &e.Scope, &scopeID, &resourceType, &resourceID, &e.Severity, &e.Status,
		&message, &currentValue, &e.FiredAt, &resolvedAt, &e.NotifiedCount, &e.Version,
		&e.CreatedAt, &createdBy, &e.UpdatedAt, &updatedBy, &e.Deleted, &deletedAt, &deletedBy,
	); err != nil {
		return nil, err
	}
	if scopeID != nil {
		e.ScopeID = *scopeID
	}
	if resourceType != nil {
		e.ResourceType = *resourceType
	}
	if resourceID != nil {
		e.ResourceID = *resourceID
	}
	if message != nil {
		e.Message = *message
	}
	e.CurrentValue = currentValue
	e.ResolvedAt = resolvedAt
	if createdBy != nil {
		e.CreatedBy = *createdBy
	}
	if updatedBy != nil {
		e.UpdatedBy = *updatedBy
	}
	e.DeletedAt = deletedAt
	if deletedBy != nil {
		e.DeletedBy = *deletedBy
	}
	return e, nil
}

// CreateRule 创建告警规则。
func (r *Repository) CreateRule(ctx context.Context, rule *alert.Rule) error {
	if rule.UUID == uuid.Nil {
		rule.UUID = uuid.New()
	}
	now := r.now()
	if rule.CreatedAt.IsZero() {
		rule.CreatedAt = now
	}
	rule.UpdatedAt = now
	if rule.NotifyChannels == nil {
		rule.NotifyChannels = []int64{}
	}
	const q = `INSERT INTO vo_alert_rules
		(uuid, scope, scope_id, name, description, metric, condition, threshold, window_minutes,
		 severity, enabled, notify_channels, cooldown_minutes, version, created_at, created_by, updated_at, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18) RETURNING id`
	err := r.pool.QueryRow(ctx, q,
		rule.UUID, rule.Scope, nullableInt64(rule.ScopeID), rule.Name, nullableStr(rule.Description),
		rule.Metric, rule.Condition, rule.Threshold, rule.WindowMinutes, rule.Severity, rule.Enabled,
		rule.NotifyChannels, rule.CooldownMinutes, rule.Version, rule.CreatedAt, nullableInt64(rule.CreatedBy),
		rule.UpdatedAt, nullableInt64(rule.UpdatedBy),
	).Scan(&rule.ID)
	if err != nil {
		return fmt.Errorf("insert alert rule: %w", err)
	}
	return nil
}

// GetRuleByID 按 ID 查询规则。
func (r *Repository) GetRuleByID(ctx context.Context, id int64) (*alert.Rule, error) {
	q := `SELECT ` + ruleColumns + ` FROM vo_alert_rules WHERE id=$1 AND deleted=false`
	rule, err := scanRule(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, alert.ErrRuleNotFound
		}
		return nil, err
	}
	return rule, nil
}

// UpdateRule 更新告警规则。
func (r *Repository) UpdateRule(ctx context.Context, rule *alert.Rule) error {
	rule.UpdatedAt = r.now()
	rule.Version++
	const q = `UPDATE vo_alert_rules SET
		name=$2, description=$3, metric=$4, condition=$5, threshold=$6, window_minutes=$7,
		severity=$8, enabled=$9, notify_channels=$10, cooldown_minutes=$11, version=$12,
		updated_at=$13, updated_by=$14
		WHERE id=$1 AND deleted=false`
	ct, err := r.pool.Exec(ctx, q,
		rule.ID, rule.Name, nullableStr(rule.Description), rule.Metric, rule.Condition, rule.Threshold,
		rule.WindowMinutes, rule.Severity, rule.Enabled, rule.NotifyChannels, rule.CooldownMinutes,
		rule.Version, rule.UpdatedAt, nullableInt64(rule.UpdatedBy),
	)
	if err != nil {
		return fmt.Errorf("update alert rule: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return alert.ErrRuleNotFound
	}
	return nil
}

// DeleteRule 软删除告警规则。
func (r *Repository) DeleteRule(ctx context.Context, id int64, deletedBy int64) error {
	now := r.now()
	const q = `UPDATE vo_alert_rules SET deleted=true, deleted_at=$2, deleted_by=$3, updated_at=$2
		WHERE id=$1 AND deleted=false`
	ct, err := r.pool.Exec(ctx, q, id, now, nullableInt64(deletedBy))
	if err != nil {
		return fmt.Errorf("delete alert rule: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return alert.ErrRuleNotFound
	}
	return nil
}

// ListRules 分页查询告警规则。
func (r *Repository) ListRules(ctx context.Context, q alert.RuleQuery) ([]*alert.Rule, int64, error) {
	if q.Limit <= 0 {
		q.Limit = 20
	}
	var conds []string
	args := []any{}
	conds = append(conds, "deleted=false")
	if q.Scope != "" {
		conds = append(conds, fmt.Sprintf("scope = $%d", len(args)+1))
		args = append(args, q.Scope)
	}
	if q.ScopeID != 0 {
		conds = append(conds, fmt.Sprintf("scope_id = $%d", len(args)+1))
		args = append(args, q.ScopeID)
	}
	if q.Enabled != nil {
		conds = append(conds, fmt.Sprintf("enabled = $%d", len(args)+1))
		args = append(args, *q.Enabled)
	}
	where := joinConds(conds)
	var total int64
	if err := r.pool.QueryRow(ctx, "SELECT count(*) FROM vo_alert_rules WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, q.Limit, q.Offset)
	rows, err := r.pool.Query(ctx,
		`SELECT `+ruleColumns+` FROM vo_alert_rules WHERE `+where+` ORDER BY id DESC LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)),
		args...,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var items []*alert.Rule
	for rows.Next() {
		rule, err := scanRule(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, rule)
	}
	return items, total, rows.Err()
}

// CreateEvent 创建告警事件。
func (r *Repository) CreateEvent(ctx context.Context, evt *alert.Event) error {
	if evt.UUID == uuid.Nil {
		evt.UUID = uuid.New()
	}
	now := r.now()
	if evt.FiredAt.IsZero() {
		evt.FiredAt = now
	}
	if evt.CreatedAt.IsZero() {
		evt.CreatedAt = now
	}
	evt.UpdatedAt = now
	if evt.Status == "" {
		evt.Status = alert.EventFiring
	}
	const q = `INSERT INTO vo_alert_events
		(uuid, rule_id, scope, scope_id, resource_type, resource_id, severity, status, message,
		 current_value, fired_at, resolved_at, notified_count, version, created_at, created_by, updated_at, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18) RETURNING id`
	err := r.pool.QueryRow(ctx, q,
		evt.UUID, evt.RuleID, evt.Scope, nullableInt64(evt.ScopeID), nullableStr(evt.ResourceType),
		nullableInt64(evt.ResourceID), evt.Severity, evt.Status, nullableStr(evt.Message),
		evt.CurrentValue, evt.FiredAt, evt.ResolvedAt, evt.NotifiedCount, evt.Version,
		evt.CreatedAt, nullableInt64(evt.CreatedBy), evt.UpdatedAt, nullableInt64(evt.UpdatedBy),
	).Scan(&evt.ID)
	if err != nil {
		return fmt.Errorf("insert alert event: %w", err)
	}
	return nil
}

// GetEventByID 按 ID 查询事件。
func (r *Repository) GetEventByID(ctx context.Context, id int64) (*alert.Event, error) {
	q := `SELECT ` + eventColumns + ` FROM vo_alert_events WHERE id=$1 AND deleted=false`
	evt, err := scanEvent(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, alert.ErrEventNotFound
		}
		return nil, err
	}
	return evt, nil
}

// ListEvents 分页查询告警事件。
func (r *Repository) ListEvents(ctx context.Context, q alert.EventQuery) ([]*alert.Event, int64, error) {
	if q.Limit <= 0 {
		q.Limit = 20
	}
	var conds []string
	args := []any{}
	conds = append(conds, "deleted=false")
	if q.RuleID != 0 {
		conds = append(conds, fmt.Sprintf("rule_id = $%d", len(args)+1))
		args = append(args, q.RuleID)
	}
	if q.Scope != "" {
		conds = append(conds, fmt.Sprintf("scope = $%d", len(args)+1))
		args = append(args, q.Scope)
	}
	if q.ScopeID != 0 {
		conds = append(conds, fmt.Sprintf("scope_id = $%d", len(args)+1))
		args = append(args, q.ScopeID)
	}
	if q.Status != "" {
		conds = append(conds, fmt.Sprintf("status = $%d", len(args)+1))
		args = append(args, q.Status)
	}
	if !q.StartTime.IsZero() {
		conds = append(conds, fmt.Sprintf("fired_at >= $%d", len(args)+1))
		args = append(args, q.StartTime)
	}
	if !q.EndTime.IsZero() {
		conds = append(conds, fmt.Sprintf("fired_at <= $%d", len(args)+1))
		args = append(args, q.EndTime)
	}
	where := joinConds(conds)
	var total int64
	if err := r.pool.QueryRow(ctx, "SELECT count(*) FROM vo_alert_events WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, q.Limit, q.Offset)
	rows, err := r.pool.Query(ctx,
		`SELECT `+eventColumns+` FROM vo_alert_events WHERE `+where+` ORDER BY fired_at DESC LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)),
		args...,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var items []*alert.Event
	for rows.Next() {
		evt, err := scanEvent(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, evt)
	}
	return items, total, rows.Err()
}

func joinConds(conds []string) string {
	out := ""
	for i, c := range conds {
		if i > 0 {
			out += " AND "
		}
		out += c
	}
	return out
}

func nullableInt64(v int64) *int64 {
	if v == 0 {
		return nil
	}
	return &v
}

func nullableStr(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}
