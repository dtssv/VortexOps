package buildrepo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/vortexops/vortexops/internal/domain"
	"github.com/vortexops/vortexops/internal/domain/build"
)

// --- 构建工具（BuildTool）CRUD ---

const buildToolColumns = `id, uuid, name, runtime, tool, default_build_command, default_artifact_path, builder_image,
	is_system, description, version, created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by`

func scanBuildTool(row pgx.Row) (*build.BuildTool, error) {
	bt := &build.BuildTool{}
	var (
		defaultBuildCmd    *string
		defaultArtifactPth *string
		desc               *string
		createdBy          *int64
		updatedBy          *int64
		deletedAt          *time.Time
		deletedBy          *int64
	)
	if err := row.Scan(
		&bt.ID, &bt.UUID, &bt.Name, &bt.Runtime, &bt.Tool, &defaultBuildCmd, &defaultArtifactPth, &bt.BuilderImage,
		&bt.IsSystem, &desc, &bt.Version, &bt.CreatedAt, &createdBy, &bt.UpdatedAt, &updatedBy, &bt.Deleted, &deletedAt, &deletedBy,
	); err != nil {
		return nil, err
	}
	if defaultBuildCmd != nil {
		bt.DefaultBuildCommand = *defaultBuildCmd
	}
	if defaultArtifactPth != nil {
		bt.DefaultArtifactPath = *defaultArtifactPth
	}
	if desc != nil {
		bt.Description = *desc
	}
	if createdBy != nil {
		bt.CreatedBy = *createdBy
	}
	if updatedBy != nil {
		bt.UpdatedBy = *updatedBy
	}
	if deletedAt != nil {
		bt.DeletedAt = deletedAt
	}
	if deletedBy != nil {
		bt.DeletedBy = *deletedBy
	}
	return bt, nil
}

// CreateBuildTool 创建构建工具。
func (r *Repository) CreateBuildTool(ctx context.Context, bt *build.BuildTool) error {
	if bt.UUID == uuid.Nil {
		bt.UUID = uuid.New()
	}
	now := r.now()
	if bt.CreatedAt.IsZero() {
		bt.CreatedAt = now
		bt.UpdatedAt = now
	}
	const q = `INSERT INTO vo_build_tools
		(uuid, name, runtime, tool, default_build_command, default_artifact_path, builder_image,
		 is_system, description, version, created_at, created_by, updated_at, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		RETURNING id, version, created_at, updated_at`
	err := r.pool.QueryRow(ctx, q,
		bt.UUID, bt.Name, bt.Runtime, bt.Tool, nullableStr(bt.DefaultBuildCommand),
		nullableStr(bt.DefaultArtifactPath), bt.BuilderImage,
		bt.IsSystem, nullableStr(bt.Description), bt.Version, bt.CreatedAt, nullableInt64(bt.CreatedBy),
		bt.UpdatedAt, nullableInt64(bt.CreatedBy),
	).Scan(&bt.ID, &bt.Version, &bt.CreatedAt, &bt.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert build tool: %w", err)
	}
	return nil
}

// GetBuildToolByID 按 ID 查询构建工具。
func (r *Repository) GetBuildToolByID(ctx context.Context, id int64) (*build.BuildTool, error) {
	q := `SELECT ` + buildToolColumns + ` FROM vo_build_tools WHERE id=$1 AND deleted=false`
	bt, err := scanBuildTool(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, build.ErrBuildToolNotFound
		}
		return nil, err
	}
	return bt, nil
}

// GetBuildToolByRuntimeTool 按语言+工具名查询构建工具（触发构建时用）。
func (r *Repository) GetBuildToolByRuntimeTool(ctx context.Context, runtime build.BaseImageRuntime, tool string) (*build.BuildTool, error) {
	q := `SELECT ` + buildToolColumns + ` FROM vo_build_tools WHERE runtime=$1 AND tool=$2 AND deleted=false`
	bt, err := scanBuildTool(r.pool.QueryRow(ctx, q, runtime, tool))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, build.ErrBuildToolNotFound
		}
		return nil, err
	}
	return bt, nil
}

// ListBuildTools 分页列出构建工具（可按 runtime 过滤）。
func (r *Repository) ListBuildTools(ctx context.Context, runtime build.BaseImageRuntime, offset, limit int) ([]*build.BuildTool, int64, error) {
	if limit <= 0 {
		limit = 50
	}
	var (
		conds []string
		args  []any
	)
	conds = append(conds, "deleted = false")
	if runtime != "" {
		conds = append(conds, "runtime = $1")
		args = append(args, runtime)
	}
	where := joinConds(conds)
	var total int64
	if err := r.pool.QueryRow(ctx, "SELECT count(*) FROM vo_build_tools WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count build tools: %w", err)
	}
	listQ := fmt.Sprintf("SELECT %s FROM vo_build_tools WHERE %s ORDER BY runtime ASC, tool ASC LIMIT $%d OFFSET $%d",
		buildToolColumns, where, len(args)+1, len(args)+2)
	args = append(args, limit, offset)
	rows, err := r.pool.Query(ctx, listQ, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query build tools: %w", err)
	}
	defer rows.Close()
	var items []*build.BuildTool
	for rows.Next() {
		bt, err := scanBuildTool(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, bt)
	}
	return items, total, rows.Err()
}

// UpdateBuildTool 更新构建工具，乐观锁。
func (r *Repository) UpdateBuildTool(ctx context.Context, bt *build.BuildTool) error {
	now := r.now()
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_build_tools SET name=$1, runtime=$2, tool=$3, default_build_command=$4, default_artifact_path=$5,
		 builder_image=$6, is_system=$7, description=$8, updated_at=$9, updated_by=$10, version=version+1
		 WHERE id=$11 AND version=$12 AND deleted=false`,
		bt.Name, bt.Runtime, bt.Tool, nullableStr(bt.DefaultBuildCommand), nullableStr(bt.DefaultArtifactPath),
		bt.BuilderImage, bt.IsSystem, nullableStr(bt.Description),
		now, nullableInt64(bt.UpdatedBy), bt.ID, bt.Version)
	if err != nil {
		return fmt.Errorf("update build tool: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrConflict
	}
	return nil
}

// DeleteBuildTool 软删除构建工具。
func (r *Repository) DeleteBuildTool(ctx context.Context, id, actorID int64) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_build_tools SET deleted=true, deleted_at=$1, deleted_by=$2, updated_at=$3, version=version+1
		 WHERE id=$4 AND deleted=false`,
		r.now(), nullableInt64(actorID), r.now(), id)
	if err != nil {
		return fmt.Errorf("delete build tool: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return build.ErrBuildToolNotFound
	}
	return nil
}
