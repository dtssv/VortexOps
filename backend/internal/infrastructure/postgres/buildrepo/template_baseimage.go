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

// --- 基础镜像 ---

const baseImageColumns = `id, uuid, name, runtime, registry, image_ref, digest, is_system, is_recommended, description,
	dockerfile_template, build_tool, default_build_command, default_artifact_path, default_build_args, entrypoint, is_web,
	version, created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by`

func scanBaseImage(row pgx.Row) (*build.BaseImage, error) {
	b := &build.BaseImage{DefaultBuildArgs: map[string]string{}}
	var (
		registry           *string
		digest             *string
		desc               *string
		dockerTmpl         *string
		buildTool          *string
		defaultBuildCmd    *string
		defaultArtifactPth *string
		defaultBuildArgs   []byte
		entrypoint         []byte
		createdBy          *int64
		updatedBy          *int64
		deletedAt          *time.Time
		deletedBy          *int64
	)
	if err := row.Scan(
		&b.ID, &b.UUID, &b.Name, &b.Runtime, &registry, &b.ImageRef, &digest, &b.IsSystem, &b.IsRecommended, &desc,
		&dockerTmpl, &buildTool, &defaultBuildCmd, &defaultArtifactPth, &defaultBuildArgs, &entrypoint, &b.IsWeb,
		&b.Version, &b.CreatedAt, &createdBy, &b.UpdatedAt, &updatedBy, &b.Deleted, &deletedAt, &deletedBy,
	); err != nil {
		return nil, err
	}
	if registry != nil {
		b.Registry = *registry
	}
	if digest != nil {
		b.Digest = *digest
	}
	if desc != nil {
		b.Description = *desc
	}
	if dockerTmpl != nil {
		b.DockerfileTemplate = *dockerTmpl
	}
	if buildTool != nil {
		b.BuildTool = *buildTool
	}
	if defaultBuildCmd != nil {
		b.DefaultBuildCommand = *defaultBuildCmd
	}
	if defaultArtifactPth != nil {
		b.DefaultArtifactPath = *defaultArtifactPth
	}
	if defaultBuildArgs != nil {
		_ = jsonUnmarshal(defaultBuildArgs, &b.DefaultBuildArgs)
	}
	if entrypoint != nil {
		_ = jsonUnmarshal(entrypoint, &b.Entrypoint)
	}
	if createdBy != nil {
		b.CreatedBy = *createdBy
	}
	if updatedBy != nil {
		b.UpdatedBy = *updatedBy
	}
	if deletedAt != nil {
		b.DeletedAt = deletedAt
	}
	if deletedBy != nil {
		b.DeletedBy = *deletedBy
	}
	return b, nil
}

// CreateBaseImage 创建基础镜像。
func (r *Repository) CreateBaseImage(ctx context.Context, b *build.BaseImage) error {
	if b.UUID == uuid.Nil {
		b.UUID = uuid.New()
	}
	now := r.now()
	if b.CreatedAt.IsZero() {
		b.CreatedAt = now
		b.UpdatedAt = now
	}
	const q = `INSERT INTO vo_base_images
		(uuid, name, runtime, registry, image_ref, digest, is_system, is_recommended, description, dockerfile_template,
		 build_tool, default_build_command, default_artifact_path, default_build_args, entrypoint, is_web,
		 version, created_at, created_by, updated_at, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)
		RETURNING id, version, created_at, updated_at`
	err := r.pool.QueryRow(ctx, q,
		b.UUID, b.Name, b.Runtime, nullableStr(b.Registry), b.ImageRef, nullableStr(b.Digest),
		b.IsSystem, b.IsRecommended, nullableStr(b.Description), nullableStr(b.DockerfileTemplate),
		nullableStr(b.BuildTool), nullableStr(b.DefaultBuildCommand), nullableStr(b.DefaultArtifactPath),
		jsonbPtrMarshal(b.DefaultBuildArgs), jsonbPtrMarshal(b.Entrypoint), b.IsWeb,
		b.Version, b.CreatedAt, nullableInt64(b.CreatedBy), b.UpdatedAt, nullableInt64(b.CreatedBy),
	).Scan(&b.ID, &b.Version, &b.CreatedAt, &b.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert base image: %w", err)
	}
	return nil
}

// GetBaseImageByID 按 ID 查询。
func (r *Repository) GetBaseImageByID(ctx context.Context, id int64) (*build.BaseImage, error) {
	q := `SELECT ` + baseImageColumns + ` FROM vo_base_images WHERE id=$1 AND deleted=false`
	b, err := scanBaseImage(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, build.ErrBaseImageNotFound
		}
		return nil, err
	}
	return b, nil
}

// ListBaseImages 分页列出基础镜像（可按 runtime 过滤）。
func (r *Repository) ListBaseImages(ctx context.Context, runtime build.BaseImageRuntime, offset, limit int) ([]*build.BaseImage, int64, error) {
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
	if err := r.pool.QueryRow(ctx, "SELECT count(*) FROM vo_base_images WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count base images: %w", err)
	}
	listQ := fmt.Sprintf("SELECT %s FROM vo_base_images WHERE %s ORDER BY is_recommended DESC, name ASC LIMIT $%d OFFSET $%d",
		baseImageColumns, where, len(args)+1, len(args)+2)
	args = append(args, limit, offset)
	rows, err := r.pool.Query(ctx, listQ, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query base images: %w", err)
	}
	defer rows.Close()
	var items []*build.BaseImage
	for rows.Next() {
		b, err := scanBaseImage(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, b)
	}
	return items, total, rows.Err()
}

// UpdateBaseImage 更新基础镜像。
func (r *Repository) UpdateBaseImage(ctx context.Context, b *build.BaseImage) error {
	now := r.now()
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_base_images SET name=$1, runtime=$2, registry=$3, image_ref=$4, digest=$5, is_system=$6, is_recommended=$7,
		 description=$8, dockerfile_template=$9, build_tool=$10, default_build_command=$11, default_artifact_path=$12,
		 default_build_args=$13, entrypoint=$14, is_web=$15, updated_at=$16, updated_by=$17, version=version+1
		 WHERE id=$18 AND version=$19 AND deleted=false`,
		b.Name, b.Runtime, nullableStr(b.Registry), b.ImageRef, nullableStr(b.Digest), b.IsSystem, b.IsRecommended,
		nullableStr(b.Description), nullableStr(b.DockerfileTemplate),
		nullableStr(b.BuildTool), nullableStr(b.DefaultBuildCommand), nullableStr(b.DefaultArtifactPath),
		jsonbPtrMarshal(b.DefaultBuildArgs), jsonbPtrMarshal(b.Entrypoint), b.IsWeb,
		now, nullableInt64(b.UpdatedBy), b.ID, b.Version)
	if err != nil {
		return fmt.Errorf("update base image: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrConflict
	}
	return nil
}

// DeleteBaseImage 软删除基础镜像。
func (r *Repository) DeleteBaseImage(ctx context.Context, id, actorID int64) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_base_images SET deleted=true, deleted_at=$1, deleted_by=$2, updated_at=$3, version=version+1
		 WHERE id=$4 AND deleted=false`,
		r.now(), nullableInt64(actorID), r.now(), id)
	if err != nil {
		return fmt.Errorf("delete base image: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return build.ErrBaseImageNotFound
	}
	return nil
}

// --- 构建模板 ---

const tmplColumns = `id, uuid, scope, scope_id, name, description, build_strategy, build_command, base_image_id,
	dockerfile_source, dockerfile_content, context_path, build_args, env_vars, is_default, usage_count, version,
	created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by`

func scanTemplate(row pgx.Row) (*build.BuildTemplate, error) {
	t := &build.BuildTemplate{
		BuildArgs: map[string]string{}, EnvVars: map[string]string{},
	}
	var (
		desc            *string
		buildCommand    *string
		dockerContent   *string
		buildArgs       []byte
		envVars         []byte
		createdBy       *int64
		updatedBy       *int64
		deletedAt       *time.Time
		deletedBy       *int64
	)
	if err := row.Scan(
		&t.ID, &t.UUID, &t.Scope, &t.ScopeID, &t.Name, &desc, &t.BuildStrategy, &buildCommand, &t.BaseImageID,
		&t.DockerfileSource, &dockerContent, &t.ContextPath, &buildArgs, &envVars, &t.IsDefault, &t.UsageCount, &t.Version,
		&t.CreatedAt, &createdBy, &t.UpdatedAt, &updatedBy, &t.Deleted, &deletedAt, &deletedBy,
	); err != nil {
		return nil, err
	}
	if desc != nil {
		t.Description = *desc
	}
	if buildCommand != nil {
		t.BuildCommand = *buildCommand
	}
	if dockerContent != nil {
		t.DockerfileContent = *dockerContent
	}
	if buildArgs != nil {
		_ = jsonUnmarshal(buildArgs, &t.BuildArgs)
	}
	if envVars != nil {
		_ = jsonUnmarshal(envVars, &t.EnvVars)
	}
	if createdBy != nil {
		t.CreatedBy = *createdBy
	}
	if updatedBy != nil {
		t.UpdatedBy = *updatedBy
	}
	if deletedAt != nil {
		t.DeletedAt = deletedAt
	}
	if deletedBy != nil {
		t.DeletedBy = *deletedBy
	}
	return t, nil
}

// CreateTemplate 创建构建模板。
func (r *Repository) CreateTemplate(ctx context.Context, t *build.BuildTemplate) error {
	if t.UUID == uuid.Nil {
		t.UUID = uuid.New()
	}
	now := r.now()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
		t.UpdatedAt = now
	}
	if t.Scope == "" {
		t.Scope = build.TmplScopePlatform
	}
	if t.ContextPath == "" {
		t.ContextPath = "."
	}
	if t.BuildArgs == nil {
		t.BuildArgs = map[string]string{}
	}
	if t.EnvVars == nil {
		t.EnvVars = map[string]string{}
	}
	const q = `INSERT INTO vo_build_templates
		(uuid, scope, scope_id, name, description, build_strategy, build_command, base_image_id, dockerfile_source,
		 dockerfile_content, context_path, build_args, env_vars, is_default, usage_count, version, created_at, created_by,
		 updated_at, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)
		RETURNING id, version, created_at, updated_at`
	err := r.pool.QueryRow(ctx, q,
		t.UUID, t.Scope, nullableInt64(t.ScopeID), t.Name, nullableStr(t.Description), t.BuildStrategy,
		nullableStr(t.BuildCommand), t.BaseImageID, nullableDockerfileSource(t.DockerfileSource), nullableStr(t.DockerfileContent),
		t.ContextPath, jsonbPtrMarshal(t.BuildArgs), jsonbPtrMarshal(t.EnvVars), t.IsDefault, t.UsageCount,
		t.Version, t.CreatedAt, nullableInt64(t.CreatedBy), t.UpdatedAt, nullableInt64(t.CreatedBy),
	).Scan(&t.ID, &t.Version, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert template: %w", err)
	}
	return nil
}

// GetTemplateByID 按 ID 查询模板。
func (r *Repository) GetTemplateByID(ctx context.Context, id int64) (*build.BuildTemplate, error) {
	q := `SELECT ` + tmplColumns + ` FROM vo_build_templates WHERE id=$1 AND deleted=false`
	t, err := scanTemplate(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, build.ErrTemplateNotFound
		}
		return nil, err
	}
	return t, nil
}

// ListTemplates 分页列出模板（可按 scope/scope_id 过滤）。
func (r *Repository) ListTemplates(ctx context.Context, scope build.TemplateScope, scopeID int64, offset, limit int) ([]*build.BuildTemplate, int64, error) {
	if limit <= 0 {
		limit = 50
	}
	var (
		conds []string
		args  []any
	)
	conds = append(conds, "deleted = false")
	if scope != "" {
		conds = append(conds, fmt.Sprintf("scope = $%d", len(args)+1))
		args = append(args, scope)
	}
	if scopeID != 0 {
		conds = append(conds, fmt.Sprintf("scope_id = $%d", len(args)+1))
		args = append(args, scopeID)
	}
	where := joinConds(conds)
	var total int64
	if err := r.pool.QueryRow(ctx, "SELECT count(*) FROM vo_build_templates WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count templates: %w", err)
	}
	listQ := fmt.Sprintf("SELECT %s FROM vo_build_templates WHERE %s ORDER BY is_default DESC, name ASC LIMIT $%d OFFSET $%d",
		tmplColumns, where, len(args)+1, len(args)+2)
	args = append(args, limit, offset)
	rows, err := r.pool.Query(ctx, listQ, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query templates: %w", err)
	}
	defer rows.Close()
	var items []*build.BuildTemplate
	for rows.Next() {
		t, err := scanTemplate(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, t)
	}
	return items, total, rows.Err()
}

// UpdateTemplate 更新模板。
func (r *Repository) UpdateTemplate(ctx context.Context, t *build.BuildTemplate) error {
	now := r.now()
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_build_templates SET name=$1, description=$2, build_strategy=$3, build_command=$4, base_image_id=$5,
		 dockerfile_source=$6, dockerfile_content=$7, context_path=$8, build_args=$9, env_vars=$10, is_default=$11,
		 updated_at=$12, updated_by=$13, version=version+1
		 WHERE id=$14 AND version=$15 AND deleted=false`,
		t.Name, nullableStr(t.Description), t.BuildStrategy, nullableStr(t.BuildCommand), t.BaseImageID,
		nullableDockerfileSource(t.DockerfileSource), nullableStr(t.DockerfileContent), t.ContextPath,
		jsonbPtrMarshal(t.BuildArgs), jsonbPtrMarshal(t.EnvVars), t.IsDefault,
		now, nullableInt64(t.UpdatedBy), t.ID, t.Version)
	if err != nil {
		return fmt.Errorf("update template: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrConflict
	}
	return nil
}

// DeleteTemplate 软删除模板。
func (r *Repository) DeleteTemplate(ctx context.Context, id, actorID int64) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_build_templates SET deleted=true, deleted_at=$1, deleted_by=$2, updated_at=$3, version=version+1
		 WHERE id=$4 AND deleted=false`,
		r.now(), nullableInt64(actorID), r.now(), id)
	if err != nil {
		return fmt.Errorf("delete template: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return build.ErrTemplateNotFound
	}
	return nil
}

// IncrementUsage 增加模板使用计数。
func (r *Repository) IncrementUsage(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx, `UPDATE vo_build_templates SET usage_count = usage_count + 1 WHERE id=$1`, id)
	return err
}

// --- helpers ---

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

func nullableDockerfileSource(s build.DockerfileSource) any {
	if s == "" {
		return "template"
	}
	return s
}

func jsonUnmarshal(data []byte, v any) error {
	if len(data) == 0 {
		return nil
	}
	return jsonDecode(data, v)
}
