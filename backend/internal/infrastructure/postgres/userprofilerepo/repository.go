// Package userprofilerepo 是用户画像的 Postgres 仓储实现。
package userprofilerepo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vortexops/vortexops/internal/domain/userprofile"
)

// Repository 用户画像仓储。
type Repository struct {
	pool *pgxpool.Pool
}

// New 创建仓储。
func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const profileColumns = "id, uuid, user_id, expertise_level, roles, domains, communication_style, preferred_language, summary, interaction_count, last_updated_at, version, created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by"

func (r *Repository) GetByUserID(ctx context.Context, userID int64) (*userprofile.Profile, error) {
	query := fmt.Sprintf("SELECT %s FROM vo_user_profiles WHERE user_id = $1 AND deleted = false", profileColumns)
	row := r.pool.QueryRow(ctx, query, userID)
	p, err := scanProfile(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, userprofile.ErrProfileNotFound
		}
		return nil, err
	}
	return p, nil
}

func (r *Repository) Upsert(ctx context.Context, p *userprofile.Profile) (*userprofile.Profile, error) {
	roles, _ := json.Marshal(p.Roles)
	if string(roles) == "null" {
		roles = []byte("[]")
	}
	domains, _ := json.Marshal(p.Domains)
	if string(domains) == "null" {
		domains = []byte("[]")
	}
	var lastUpdated any
	if p.LastUpdatedAt != nil {
		lastUpdated = *p.LastUpdatedAt
	}
	query := `
INSERT INTO vo_user_profiles (user_id, expertise_level, roles, domains, communication_style, preferred_language, summary, interaction_count, last_updated_at, version, created_by, updated_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 1, $10, $10)
ON CONFLICT (user_id) DO UPDATE SET
  expertise_level = EXCLUDED.expertise_level,
  roles = EXCLUDED.roles,
  domains = EXCLUDED.domains,
  communication_style = EXCLUDED.communication_style,
  preferred_language = EXCLUDED.preferred_language,
  summary = EXCLUDED.summary,
  interaction_count = EXCLUDED.interaction_count,
  last_updated_at = EXCLUDED.last_updated_at,
  version = vo_user_profiles.version + 1,
  updated_by = EXCLUDED.updated_by,
  updated_at = now()
RETURNING ` + profileColumns
	row := r.pool.QueryRow(ctx, query,
		p.UserID, defaultStr(p.ExpertiseLevel, "unknown"), roles, domains,
		defaultStr(p.CommunicationStyle, "balanced"), defaultStr(p.PreferredLanguage, "zh-CN"),
		p.Summary, p.InteractionCount, lastUpdated, p.UpdatedBy)
	return scanProfile(row)
}

func scanProfile(row pgx.Row) (*userprofile.Profile, error) {
	p := &userprofile.Profile{Roles: []string{}, Domains: []string{}}
	var (
		rolesBytes        []byte
		domainsBytes      []byte
		summary           *string
		lastUpdated       *time.Time
		expertiseLevel    *string
		communicationStyle *string
		preferredLanguage *string
		createdBy         *int64
		updatedBy         *int64
		deletedBy         *int64
	)
	if err := row.Scan(
		&p.ID, &p.UUID, &p.UserID, &expertiseLevel, &rolesBytes, &domainsBytes,
		&communicationStyle, &preferredLanguage, &summary, &p.InteractionCount, &lastUpdated,
		&p.Version, &p.CreatedAt, &createdBy, &p.UpdatedAt, &updatedBy,
		&p.Deleted, &p.DeletedAt, &deletedBy,
	); err != nil {
		return nil, err
	}
	if expertiseLevel != nil {
		p.ExpertiseLevel = *expertiseLevel
	}
	if communicationStyle != nil {
		p.CommunicationStyle = *communicationStyle
	}
	if preferredLanguage != nil {
		p.PreferredLanguage = *preferredLanguage
	}
	if summary != nil {
		p.Summary = *summary
	}
	if lastUpdated != nil {
		p.LastUpdatedAt = lastUpdated
	}
	if createdBy != nil {
		p.CreatedBy = *createdBy
	}
	if updatedBy != nil {
		p.UpdatedBy = *updatedBy
	}
	if deletedBy != nil {
		p.DeletedBy = *deletedBy
	}
	if len(rolesBytes) > 0 {
		_ = json.Unmarshal(rolesBytes, &p.Roles)
	}
	if len(domainsBytes) > 0 {
		_ = json.Unmarshal(domainsBytes, &p.Domains)
	}
	if p.Roles == nil {
		p.Roles = []string{}
	}
	if p.Domains == nil {
		p.Domains = []string{}
	}
	return p, nil
}

func defaultStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
