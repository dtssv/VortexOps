// Package configapp 是配置管理领域的应用服务层。
// 编排：配置版本创建（自动 checksum/diff）、ConfigSet CRUD、group-config 绑定、配置 diff。
package configapp

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	configdomain "github.com/vortexops/vortexops/internal/domain/config"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// Service 配置应用服务。
type Service struct {
	repo configdomain.Repository
}

// New 创建配置服务。
func New(repo configdomain.Repository) *Service {
	return &Service{repo: repo}
}

// CreateConfigInput 创建配置输入。
type CreateConfigInput struct {
	Scope           configdomain.Scope
	ScopeID         int64
	GroupID         int64
	Name            string
	ConfigType      configdomain.ConfigType
	Description     string
	RenderedContent string
	CreatedBy       int64
}

// CreateConfig 创建配置版本（自动递增版本号、计算 checksum 与 diff）。
func (s *Service) CreateConfig(ctx context.Context, in CreateConfigInput) (*configdomain.Config, error) {
	if in.Name == "" {
		return nil, apperr.Validation("config name is required", nil)
	}
	if in.ConfigType == "" {
		in.ConfigType = configdomain.ConfigEnv
	}
	if in.Scope == "" {
		in.Scope = configdomain.ScopeGroup
	}
	c := &configdomain.Config{
		Scope: in.Scope, ScopeID: in.ScopeID, GroupID: in.GroupID, Name: in.Name,
		ConfigType: in.ConfigType, Description: in.Description, RenderedContent: in.RenderedContent,
	}
	c.CreatedBy = in.CreatedBy
	c.UpdatedBy = in.CreatedBy
	if err := s.repo.CreateConfig(ctx, c); err != nil {
		return nil, apperr.Internal("create config", err)
	}
	return c, nil
}

// GetConfig 按 ID 查询配置。
func (s *Service) GetConfig(ctx context.Context, id int64) (*configdomain.Config, error) {
	c, err := s.repo.GetConfigByID(ctx, id)
	if err != nil {
		if errors.Is(err, configdomain.ErrConfigNotFound) {
			return nil, apperr.NotFound("config", strconv.FormatInt(id, 10))
		}
		return nil, apperr.Internal("get config", err)
	}
	return c, nil
}

// GetLatestConfig 取最新版本配置。
func (s *Service) GetLatestConfig(ctx context.Context, scope configdomain.Scope, scopeID, groupID int64, name string) (*configdomain.Config, error) {
	c, err := s.repo.GetLatestConfig(ctx, scope, scopeID, groupID, name)
	if err != nil {
		if errors.Is(err, configdomain.ErrConfigNotFound) {
			return nil, apperr.NotFound("config", name)
		}
		return nil, apperr.Internal("get latest config", err)
	}
	return c, nil
}

// ListConfigs 分页查询配置。
func (s *Service) ListConfigs(ctx context.Context, q configdomain.ConfigQuery) ([]*configdomain.Config, int64, error) {
	items, total, err := s.repo.ListConfigs(ctx, q)
	if err != nil {
		return nil, 0, apperr.Internal("list configs", err)
	}
	return items, total, nil
}

// DiffConfigs 计算两个配置版本的 diff。
// 若 versionA/versionB 均指定，对比两版本；若仅指定 name，对比最新版本与上一版本。
// 返回 unified diff 文本。
func (s *Service) DiffConfigs(ctx context.Context, scope configdomain.Scope, scopeID int64, name string, versionA, versionB int) (string, error) {
	var a, b *configdomain.Config
	var err error
	if versionA != 0 && versionB != 0 {
		a, err = s.repo.GetConfigByVersion(ctx, scope, scopeID, name, versionA)
		if err != nil {
			return "", apperr.NotFound("config version", fmt.Sprintf("%s@%d", name, versionA))
		}
		b, err = s.repo.GetConfigByVersion(ctx, scope, scopeID, name, versionB)
		if err != nil {
			return "", apperr.NotFound("config version", fmt.Sprintf("%s@%d", name, versionB))
		}
	} else {
		// 最新 vs 上一版本。
		a, err = s.repo.GetLatestConfig(ctx, scope, scopeID, 0, name)
		if err != nil {
			return "", apperr.NotFound("config", name)
		}
		if a.ConfigVersion <= 1 {
			return "", apperr.BusinessRule("no previous version to diff", configdomain.ErrNoPreviousVersion)
		}
		b, err = s.repo.GetConfigByVersion(ctx, scope, scopeID, name, a.ConfigVersion-1)
		if err != nil {
			return "", apperr.NotFound("config version", fmt.Sprintf("%s@%d", name, a.ConfigVersion-1))
		}
	}
	return unifiedDiff(b.RenderedContent, a.RenderedContent), nil
}

// DiffCrossGroup 计算跨 group 同名配置最新版本的 diff。
func (s *Service) DiffCrossGroup(ctx context.Context, scope configdomain.Scope, scopeID int64, name string, groupA, groupB int64) (string, error) {
	a, err := s.repo.GetLatestConfig(ctx, scope, scopeID, groupA, name)
	if err != nil {
		return "", apperr.NotFound("config in group A", name)
	}
	b, err := s.repo.GetLatestConfig(ctx, scope, scopeID, groupB, name)
	if err != nil {
		return "", apperr.NotFound("config in group B", name)
	}
	return unifiedDiff(b.RenderedContent, a.RenderedContent), nil
}

// ArchiveConfig 归档配置版本。
func (s *Service) ArchiveConfig(ctx context.Context, id int64) error {
	if err := s.repo.ArchiveConfig(ctx, id); err != nil {
		if errors.Is(err, configdomain.ErrConfigNotFound) {
			return apperr.NotFound("config", strconv.FormatInt(id, 10))
		}
		return apperr.Internal("archive config", err)
	}
	return nil
}

// --- ConfigSet ---

// CreateConfigSetInput 创建 ConfigSet 输入。
type CreateConfigSetInput struct {
	WorkspaceID   int64
	ApplicationID int64 // 应用维度（新模型主键）
	Name          string
	Description   string
	Content       map[string]any
	CreatedBy     int64
}

// CreateConfigSet 创建 ConfigSet。
func (s *Service) CreateConfigSet(ctx context.Context, in CreateConfigSetInput) (*configdomain.ConfigSet, error) {
	if in.Name == "" {
		return nil, apperr.Validation("config set name is required", nil)
	}
	if in.ApplicationID == 0 && in.WorkspaceID == 0 {
		return nil, apperr.Validation("application_id or workspace_id is required", nil)
	}
	cs := &configdomain.ConfigSet{
		WorkspaceID: in.WorkspaceID, ApplicationID: in.ApplicationID,
		Name: in.Name, Description: in.Description, Content: in.Content,
	}
	cs.CreatedBy = in.CreatedBy
	cs.UpdatedBy = in.CreatedBy
	if err := s.repo.CreateConfigSet(ctx, cs); err != nil {
		if errors.Is(err, configdomain.ErrConfigSetExists) {
			return nil, apperr.Conflict("config set name already exists", err)
		}
		return nil, apperr.Internal("create config set", err)
	}
	return cs, nil
}

// GetConfigSet 按 ID 查询 ConfigSet。
func (s *Service) GetConfigSet(ctx context.Context, id int64) (*configdomain.ConfigSet, error) {
	cs, err := s.repo.GetConfigSetByID(ctx, id)
	if err != nil {
		if errors.Is(err, configdomain.ErrConfigSetNotFound) {
			return nil, apperr.NotFound("config set", strconv.FormatInt(id, 10))
		}
		return nil, apperr.Internal("get config set", err)
	}
	return cs, nil
}

// ListConfigSets 分页列出 ConfigSet。
func (s *Service) ListConfigSets(ctx context.Context, workspaceID int64, page, size int) ([]*configdomain.ConfigSet, int64, error) {
	items, total, err := s.repo.ListConfigSets(ctx, workspaceID, (page-1)*size, size)
	if err != nil {
		return nil, 0, apperr.Internal("list config sets", err)
	}
	return items, total, nil
}

// ListConfigSetsByApplication 列出应用下的所有配置集（供分组绑定下拉用）。
func (s *Service) ListConfigSetsByApplication(ctx context.Context, applicationID int64) ([]*configdomain.ConfigSet, error) {
	items, err := s.repo.ListConfigSetsByApplication(ctx, applicationID)
	if err != nil {
		return nil, apperr.Internal("list config sets by application", err)
	}
	return items, nil
}

// UpdateConfigSetInput 更新 ConfigSet 输入。
type UpdateConfigSetInput struct {
	ID          int64
	Name        string
	Description string
	Content     map[string]any
	Version     int
	UpdatedBy   int64
}

// UpdateConfigSet 更新 ConfigSet。
func (s *Service) UpdateConfigSet(ctx context.Context, in UpdateConfigSetInput) (*configdomain.ConfigSet, error) {
	cs, err := s.repo.GetConfigSetByID(ctx, in.ID)
	if err != nil {
		if errors.Is(err, configdomain.ErrConfigSetNotFound) {
			return nil, apperr.NotFound("config set", strconv.FormatInt(in.ID, 10))
		}
		return nil, apperr.Internal("get config set", err)
	}
	cs.Name = in.Name
	cs.Description = in.Description
	oldContent := cloneContentMap(cs.Content)
	cs.Content = in.Content
	cs.Version = in.Version
	cs.UpdatedBy = in.UpdatedBy
	s.snapshotIfFilesChanged(ctx, configdomain.SnapshotConfigSet, cs.ID, oldContent, in.Content, "update", in.UpdatedBy)
	if err := s.repo.UpdateConfigSet(ctx, cs); err != nil {
		return nil, apperr.Internal("update config set", err)
	}
	return cs, nil
}

// DeleteConfigSet 软删除 ConfigSet。
// 仍有分组绑定该配置集时禁止删除，避免悬空绑定。
func (s *Service) DeleteConfigSet(ctx context.Context, id, actorID int64) error {
	bindings, err := s.repo.ListActiveBindingsByConfigSet(ctx, id)
	if err != nil {
		return apperr.Internal("list bindings by config set", err)
	}
	if len(bindings) > 0 {
		groupIDs := make([]string, 0, len(bindings))
		for _, b := range bindings {
			groupIDs = append(groupIDs, strconv.FormatInt(b.GroupID, 10))
		}
		return apperr.BusinessRule(
			fmt.Sprintf("config set is bound by %d group(s): %s", len(bindings), strings.Join(groupIDs, ",")),
			configdomain.ErrConfigSetInUse,
		)
	}
	if err := s.repo.DeleteConfigSet(ctx, id, actorID); err != nil {
		if errors.Is(err, configdomain.ErrConfigSetNotFound) {
			return apperr.NotFound("config set", strconv.FormatInt(id, 10))
		}
		return apperr.Internal("delete config set", err)
	}
	return nil
}

// --- 绑定 ---

// CreateBindingInput 创建绑定输入。
// 新绑定走 ConfigSetID（指向 vo_config_sets）；历史兼容走 ConfigID（指向 vo_configs）。
type CreateBindingInput struct {
	GroupID       int64
	ConfigID      int64 // 兼容历史
	ConfigSetID   int64 // 新模型
	Priority      int
	PinnedVersion *int
	MountPath     string
	SubPath       string
	CreatedBy     int64
}

// CreateBinding 创建 group-config 绑定。
// 一个分组至多绑定一个配置集（uk_group_single_binding）。已有绑定时返回 422。
func (s *Service) CreateBinding(ctx context.Context, in CreateBindingInput) (*configdomain.GroupConfigBinding, error) {
	if in.ConfigSetID == 0 && in.ConfigID == 0 {
		return nil, apperr.Validation("config_set_id or config_id is required", nil)
	}
	if in.Priority == 0 {
		in.Priority = 100
	}
	// 单绑定校验：分组已有未删除绑定则拒绝。
	count, err := s.repo.CountActiveBindingsByGroup(ctx, in.GroupID)
	if err != nil {
		return nil, apperr.Internal("count group bindings", err)
	}
	if count > 0 {
		return nil, apperr.BusinessRule("group already has a config set binding", configdomain.ErrGroupAlreadyBound)
	}
	b := &configdomain.GroupConfigBinding{
		GroupID: in.GroupID, ConfigID: in.ConfigID, ConfigSetID: in.ConfigSetID,
		Priority: in.Priority, PinnedVersion: in.PinnedVersion,
		MountPath: in.MountPath, SubPath: in.SubPath,
	}
	b.CreatedBy = in.CreatedBy
	b.UpdatedBy = in.CreatedBy
	if err := s.repo.CreateBinding(ctx, b); err != nil {
		if errors.Is(err, configdomain.ErrBindingExists) {
			return nil, apperr.Conflict("config binding already exists", err)
		}
		// 兜底：DB 唯一索引冲突也视为「已有绑定」。
		var pgErr interface{ SQLState() string }
		if errors.As(err, &pgErr) && pgErr.SQLState() == "23505" {
			return nil, apperr.BusinessRule("group already has a config set binding", configdomain.ErrGroupAlreadyBound)
		}
		return nil, apperr.Internal("create binding", err)
	}
	// 绑定快照：记录绑定时刻的生效配置。
	if b.ConfigSetID > 0 {
		if cs, err := s.repo.GetConfigSetByID(ctx, b.ConfigSetID); err == nil {
			s.snapshotAlways(ctx, configdomain.SnapshotGroupBind, b.GroupID, cs.Content, "bind", in.CreatedBy)
		}
	}
	return b, nil
}

// ListBindings 列出 group 的配置绑定。
func (s *Service) ListBindings(ctx context.Context, groupID int64) ([]*configdomain.GroupConfigBinding, error) {
	items, err := s.repo.ListBindingsByGroup(ctx, groupID)
	if err != nil {
		return nil, apperr.Internal("list bindings", err)
	}
	return items, nil
}

// DeleteBinding 软删除绑定。解绑时将绑定配置内容快照并写入分组本地配置（沿用解绑时内容）。
func (s *Service) DeleteBinding(ctx context.Context, id, actorID int64) error {
	b, err := s.repo.GetBindingByID(ctx, id)
	if err != nil {
		if errors.Is(err, configdomain.ErrBindingNotFound) {
			return apperr.NotFound("config binding", strconv.FormatInt(id, 10))
		}
		return apperr.Internal("get binding", err)
	}
	var effective map[string]any
	if b.ConfigSetID > 0 {
		if cs, csErr := s.repo.GetConfigSetByID(ctx, b.ConfigSetID); csErr == nil {
			effective = cs.Content
		}
	}
	if effective != nil {
		s.snapshotAlways(ctx, configdomain.SnapshotGroupBind, b.GroupID, effective, "unbind", actorID)
		// 解绑后沿用配置：写入本地配置（覆盖已有本地配置）。
		lc := &configdomain.GroupLocalConfig{
			GroupID: b.GroupID, Content: cloneContentMap(effective),
		}
		lc.UpdatedBy = actorID
		_ = s.repo.UpsertLocalConfig(ctx, lc)
	}
	if err := s.repo.DeleteBinding(ctx, id, actorID); err != nil {
		if errors.Is(err, configdomain.ErrBindingNotFound) {
			return apperr.NotFound("config binding", strconv.FormatInt(id, 10))
		}
		return apperr.Internal("delete binding", err)
	}
	return nil
}

// --- 分组本地配置 ---

// UpsertLocalConfigInput 创建/更新分组本地配置输入。
type UpsertLocalConfigInput struct {
	GroupID     int64
	Name        string
	Description string
	Content     map[string]any
	Version     int
	UpdatedBy   int64
}

// GetLocalConfig 取分组的本地配置。
func (s *Service) GetLocalConfig(ctx context.Context, groupID int64) (*configdomain.GroupLocalConfig, error) {
	c, err := s.repo.GetLocalConfigByGroup(ctx, groupID)
	if err != nil {
		if errors.Is(err, configdomain.ErrLocalConfigNotFound) {
			return nil, apperr.NotFound("group local config", strconv.FormatInt(groupID, 10))
		}
		return nil, apperr.Internal("get group local config", err)
	}
	return c, nil
}

// UpsertLocalConfig 创建或更新分组本地配置。
// 互斥：分组已绑定配置集时拒绝（绑定覆盖本地配置）。
func (s *Service) UpsertLocalConfig(ctx context.Context, in UpsertLocalConfigInput) (*configdomain.GroupLocalConfig, error) {
	count, err := s.repo.CountActiveBindingsByGroup(ctx, in.GroupID)
	if err != nil {
		return nil, apperr.Internal("count group bindings", err)
	}
	if count > 0 {
		return nil, apperr.BusinessRule("group has config set binding, cannot edit local config", configdomain.ErrGroupBoundCannotEditLocal)
	}
	if in.Content == nil {
		in.Content = map[string]any{}
	}
	var oldContent map[string]any
	if existing, err := s.repo.GetLocalConfigByGroup(ctx, in.GroupID); err == nil {
		oldContent = cloneContentMap(existing.Content)
	}
	c := &configdomain.GroupLocalConfig{
		GroupID: in.GroupID, Name: in.Name, Description: in.Description,
		Content: in.Content,
	}
	c.UpdatedBy = in.UpdatedBy
	s.snapshotIfFilesChanged(ctx, configdomain.SnapshotGroupLocal, in.GroupID, oldContent, in.Content, "update", in.UpdatedBy)
	if err := s.repo.UpsertLocalConfig(ctx, c); err != nil {
		return nil, apperr.Internal("upsert group local config", err)
	}
	return c, nil
}

// DeleteLocalConfig 删除分组本地配置。
func (s *Service) DeleteLocalConfig(ctx context.Context, groupID, actorID int64) error {
	if err := s.repo.DeleteLocalConfig(ctx, groupID, actorID); err != nil {
		if errors.Is(err, configdomain.ErrLocalConfigNotFound) {
			return apperr.NotFound("group local config", strconv.FormatInt(groupID, 10))
		}
		return apperr.Internal("delete group local config", err)
	}
	return nil
}

// --- diff 实现 ---

// unifiedDiff 生成简化 unified diff。
// 真实场景可用 github.com/sergi/go-diff；此处实现按行的最小 diff，避免引入新依赖。
func unifiedDiff(a, b string) string {
	if a == b {
		return ""
	}
	aLines := splitLines(a)
	bLines := splitLines(b)
	var sb strings.Builder
	sb.WriteString("--- previous\n+++ current\n")
	maxLen := len(aLines)
	if len(bLines) > maxLen {
		maxLen = len(bLines)
	}
	for i := 0; i < maxLen; i++ {
		var aLine, bLine string
		if i < len(aLines) {
			aLine = aLines[i]
		}
		if i < len(bLines) {
			bLine = bLines[i]
		}
		if aLine == bLine {
			sb.WriteString(" " + aLine + "\n")
		} else {
			if aLine != "" {
				sb.WriteString("-" + aLine + "\n")
			}
			if bLine != "" {
				sb.WriteString("+" + bLine + "\n")
			}
		}
	}
	return sb.String()
}

func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.Split(s, "\n")
}
