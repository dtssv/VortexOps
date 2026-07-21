// Package bastionrepo 是堡垒机领域的 PostgreSQL 仓储实现。
package bastionrepo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vortexops/vortexops/internal/domain"
	"github.com/vortexops/vortexops/internal/domain/bastion"
)

// Repository 堡垒机 PostgreSQL 仓储。
type Repository struct {
	pool *pgxpool.Pool
}

// New 创建仓储。
func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// --- 资产 ---

const assetColumns = `id, uuid, workspace_id, name, host, port, protocol, platform, username, credential_id,
	jms_asset_id, jms_org_id, tags, comment, is_active, version, created_at, created_by, updated_at, updated_by`

func scanAsset(row pgx.Row) (*bastion.Asset, error) {
	a := &bastion.Asset{}
	var (
		username     *string
		credentialID *int64
		jmsAssetID   *string
		jmsOrgID     *string
		tags         []string
		comment      *string
		createdBy    *int64
		updatedBy    *int64
	)
	if err := row.Scan(
		&a.ID, &a.UUID, &a.WorkspaceID, &a.Name, &a.Host, &a.Port, &a.Protocol, &a.Platform,
		&username, &credentialID, &jmsAssetID, &jmsOrgID, &tags, &comment, &a.IsActive,
		&a.Version, &a.CreatedAt, &createdBy, &a.UpdatedAt, &updatedBy,
	); err != nil {
		return nil, err
	}
	if username != nil {
		a.Username = *username
	}
	if credentialID != nil {
		a.CredentialID = *credentialID
	}
	if jmsAssetID != nil {
		a.JMSAssetID = *jmsAssetID
	}
	if jmsOrgID != nil {
		a.JMSOrgID = *jmsOrgID
	}
	a.Tags = tags
	if comment != nil {
		a.Comment = *comment
	}
	if createdBy != nil {
		a.CreatedBy = *createdBy
	}
	if updatedBy != nil {
		a.UpdatedBy = *updatedBy
	}
	return a, nil
}

// CreateAsset 创建资产。
func (r *Repository) CreateAsset(ctx context.Context, a *bastion.Asset) error {
	now := time.Now()
	a.UUID = uuid.New()
	a.IsActive = true
	a.Version = 1
	a.CreatedAt = now
	a.UpdatedAt = now
	if a.Port == 0 {
		a.Port = 22
	}
	if a.Protocol == "" {
		a.Protocol = bastion.ProtocolSSH
	}
	const q = `INSERT INTO vo_bastion_assets (uuid, workspace_id, name, host, port, protocol, platform, username,
		credential_id, jms_asset_id, jms_org_id, tags, comment, is_active, version, created_at, created_by, updated_at, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19) RETURNING id`
	if err := r.pool.QueryRow(ctx, q,
		a.UUID, a.WorkspaceID, a.Name, a.Host, a.Port, a.Protocol, a.Platform, nullableStr(a.Username),
		nullableInt64(a.CredentialID), nullableStr(a.JMSAssetID), nullableStr(a.JMSOrgID), a.Tags, nullableStr(a.Comment),
		a.IsActive, a.Version, now, nullableInt64(a.CreatedBy), now, nullableInt64(a.UpdatedBy),
	).Scan(&a.ID); err != nil {
		return fmt.Errorf("insert bastion asset: %w", err)
	}
	return nil
}

// GetAssetByID 按 ID 查询资产。
func (r *Repository) GetAssetByID(ctx context.Context, id int64) (*bastion.Asset, error) {
	const q = `SELECT ` + assetColumns + ` FROM vo_bastion_assets WHERE id=$1 AND deleted=false`
	a, err := scanAsset(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, bastion.ErrAssetNotFound
		}
		return nil, fmt.Errorf("get bastion asset: %w", err)
	}
	return a, nil
}

// ListAssets 分页查询资产。
func (r *Repository) ListAssets(ctx context.Context, q bastion.AssetQuery) ([]*bastion.Asset, int64, error) {
	conds := []string{"deleted=false"}
	args := []any{}
	if q.WorkspaceID > 0 {
		args = append(args, q.WorkspaceID)
		conds = append(conds, fmt.Sprintf("workspace_id=$%d", len(args)))
	}
	if q.Protocol != "" {
		args = append(args, q.Protocol)
		conds = append(conds, fmt.Sprintf("protocol=$%d", len(args)))
	}
	if q.Search != "" {
		args = append(args, "%"+q.Search+"%")
		conds = append(conds, fmt.Sprintf("(name ILIKE $%d OR host ILIKE $%d)", len(args), len(args)))
	}
	where := joinConds(conds)
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM vo_bastion_assets WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count bastion assets: %w", err)
	}
	args = append(args, q.Limit, q.Offset)
	rows, err := r.pool.Query(ctx,
		`SELECT `+assetColumns+` FROM vo_bastion_assets WHERE `+where+` ORDER BY created_at DESC LIMIT $`+itoa(len(args)-1)+` OFFSET $`+itoa(len(args)),
		args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list bastion assets: %w", err)
	}
	defer rows.Close()
	var out []*bastion.Asset
	for rows.Next() {
		a, err := scanAsset(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan bastion asset: %w", err)
		}
		out = append(out, a)
	}
	return out, total, nil
}

// UpdateAsset 更新资产。
func (r *Repository) UpdateAsset(ctx context.Context, a *bastion.Asset) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_bastion_assets SET name=$1, host=$2, port=$3, protocol=$4, platform=$5, username=$6,
		 credential_id=$7, tags=$8, comment=$9, is_active=$10, version=version+1, updated_at=now(), updated_by=$11
		 WHERE id=$12 AND version=$13 AND deleted=false`,
		a.Name, a.Host, a.Port, a.Protocol, a.Platform, nullableStr(a.Username),
		nullableInt64(a.CredentialID), a.Tags, nullableStr(a.Comment), a.IsActive, nullableInt64(a.UpdatedBy), a.ID, a.Version)
	if err != nil {
		return fmt.Errorf("update bastion asset: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrConflict
	}
	a.Version++
	return nil
}

// DeleteAsset 软删除资产。
func (r *Repository) DeleteAsset(ctx context.Context, id, actorID int64) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_bastion_assets SET deleted=true, deleted_at=now(), updated_at=now(), updated_by=$1, version=version+1
		 WHERE id=$2 AND deleted=false`, nullableInt64(actorID), id)
	if err != nil {
		return fmt.Errorf("delete bastion asset: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return bastion.ErrAssetNotFound
	}
	return nil
}

// --- 会话 ---

const sessionColumns = `id, uuid, workspace_id, asset_id, jms_session_id, user_id, username, asset_name,
	protocol, remote_addr, login_from, status, started_at, ended_at, duration_ms, replay_url, command_count,
	version, created_at, updated_at`

func scanSession(row pgx.Row) (*bastion.Session, error) {
	s := &bastion.Session{}
	var (
		assetID       *int64
		jmsSessionID  *string
		userID        *int64
		username      *string
		assetName     *string
		protocol      *string
		remoteAddr    *string
		loginFrom     *string
		replayURL     *string
		startedAt     *time.Time
		endedAt       *time.Time
	)
	if err := row.Scan(
		&s.ID, &s.UUID, &s.WorkspaceID, &assetID, &jmsSessionID, &userID, &username, &assetName,
		&protocol, &remoteAddr, &loginFrom, &s.Status, &startedAt, &endedAt, &s.DurationMs,
		&replayURL, &s.CommandCount, &s.Version, &s.CreatedAt, &s.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if assetID != nil {
		s.AssetID = *assetID
	}
	if jmsSessionID != nil {
		s.JMSSessionID = *jmsSessionID
	}
	if userID != nil {
		s.UserID = *userID
	}
	if username != nil {
		s.Username = *username
	}
	if assetName != nil {
		s.AssetName = *assetName
	}
	if protocol != nil {
		s.Protocol = bastion.Protocol(*protocol)
	}
	if remoteAddr != nil {
		s.RemoteAddr = *remoteAddr
	}
	if loginFrom != nil {
		s.LoginFrom = *loginFrom
	}
	if replayURL != nil {
		s.ReplayURL = *replayURL
	}
	s.StartedAt = startedAt
	s.EndedAt = endedAt
	return s, nil
}

// CreateSession 创建会话。
func (r *Repository) CreateSession(ctx context.Context, s *bastion.Session) error {
	now := time.Now()
	s.UUID = uuid.New()
	if s.Status == "" {
		s.Status = bastion.SessionActive
	}
	s.Version = 1
	s.CreatedAt = now
	s.UpdatedAt = now
	const q = `INSERT INTO vo_bastion_sessions (uuid, workspace_id, asset_id, jms_session_id, user_id, username,
		asset_name, protocol, remote_addr, login_from, status, started_at, ended_at, duration_ms, replay_url,
		command_count, version, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19) RETURNING id`
	if err := r.pool.QueryRow(ctx, q,
		s.UUID, s.WorkspaceID, nullableInt64(s.AssetID), nullableStr(s.JMSSessionID), nullableInt64(s.UserID),
		nullableStr(s.Username), nullableStr(s.AssetName), nullableStr(string(s.Protocol)), nullableStr(s.RemoteAddr),
		nullableStr(s.LoginFrom), s.Status, s.StartedAt, s.EndedAt, s.DurationMs, nullableStr(s.ReplayURL),
		s.CommandCount, s.Version, now, now,
	).Scan(&s.ID); err != nil {
		return fmt.Errorf("insert bastion session: %w", err)
	}
	return nil
}

// GetSessionByID 按 ID 查询会话。
func (r *Repository) GetSessionByID(ctx context.Context, id int64) (*bastion.Session, error) {
	const q = `SELECT ` + sessionColumns + ` FROM vo_bastion_sessions WHERE id=$1`
	s, err := scanSession(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, bastion.ErrSessionNotFound
		}
		return nil, fmt.Errorf("get bastion session: %w", err)
	}
	return s, nil
}

// ListSessions 分页查询会话。
func (r *Repository) ListSessions(ctx context.Context, q bastion.SessionQuery) ([]*bastion.Session, int64, error) {
	conds := []string{"true"}
	args := []any{}
	if q.WorkspaceID > 0 {
		args = append(args, q.WorkspaceID)
		conds = append(conds, fmt.Sprintf("workspace_id=$%d", len(args)))
	}
	if q.AssetID > 0 {
		args = append(args, q.AssetID)
		conds = append(conds, fmt.Sprintf("asset_id=$%d", len(args)))
	}
	if q.UserID > 0 {
		args = append(args, q.UserID)
		conds = append(conds, fmt.Sprintf("user_id=$%d", len(args)))
	}
	if q.Status != "" {
		args = append(args, q.Status)
		conds = append(conds, fmt.Sprintf("status=$%d", len(args)))
	}
	where := joinConds(conds)
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM vo_bastion_sessions WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count bastion sessions: %w", err)
	}
	args = append(args, q.Limit, q.Offset)
	rows, err := r.pool.Query(ctx,
		`SELECT `+sessionColumns+` FROM vo_bastion_sessions WHERE `+where+` ORDER BY COALESCE(started_at, created_at) DESC LIMIT $`+itoa(len(args)-1)+` OFFSET $`+itoa(len(args)),
		args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list bastion sessions: %w", err)
	}
	defer rows.Close()
	var out []*bastion.Session
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan bastion session: %w", err)
		}
		out = append(out, s)
	}
	return out, total, nil
}

// UpdateSession 更新会话。
func (r *Repository) UpdateSession(ctx context.Context, s *bastion.Session) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_bastion_sessions SET status=$1, ended_at=$2, duration_ms=$3, replay_url=$4, command_count=$5,
		 version=version+1, updated_at=now() WHERE id=$6 AND version=$7`,
		s.Status, s.EndedAt, s.DurationMs, nullableStr(s.ReplayURL), s.CommandCount, s.ID, s.Version)
	if err != nil {
		return fmt.Errorf("update bastion session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrConflict
	}
	s.Version++
	return nil
}

// --- helpers ---

func nullableStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullableInt64(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

func joinConds(conds []string) string {
	if len(conds) == 0 {
		return "true"
	}
	out := ""
	for i, c := range conds {
		if i > 0 {
			out += " AND "
		}
		out += c
	}
	return out
}

func itoa(i int) string {
	return fmt.Sprintf("%d", i)
}
