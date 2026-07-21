package configapp

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	configdomain "github.com/vortexops/vortexops/internal/domain/config"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// maybeSnapshot 写入快照（调用方已确认需要快照）。
func (s *Service) maybeSnapshot(ctx context.Context, targetType configdomain.SnapshotTargetType, targetID int64, before map[string]any, reason string, actorID int64) {
	if before == nil || len(extractFiles(before)) == 0 {
		return
	}
	no, err := s.repo.NextSnapshotNo(ctx, targetType, targetID)
	if err != nil {
		return
	}
	_ = s.repo.CreateContentSnapshot(ctx, &configdomain.ContentSnapshot{
		TargetType: targetType, TargetID: targetID, SnapshotNo: no,
		Content: before, ChangeReason: reason, FilesHash: hashFiles(extractFiles(before)),
		CreatedBy: actorID,
	})
}

// snapshotIfFilesChanged 比较前后 content，files 变更则快照旧内容。
func (s *Service) snapshotIfFilesChanged(ctx context.Context, targetType configdomain.SnapshotTargetType, targetID int64, before, after map[string]any, reason string, actorID int64) {
	if before == nil || !filesChanged(before, after) {
		return
	}
	s.maybeSnapshot(ctx, targetType, targetID, before, reason, actorID)
}

// snapshotAlways 无条件写入快照（绑定/解绑生效配置）。
func (s *Service) snapshotAlways(ctx context.Context, targetType configdomain.SnapshotTargetType, targetID int64, content map[string]any, reason string, actorID int64) {
	if content == nil || len(extractFiles(content)) == 0 {
		return
	}
	no, err := s.repo.NextSnapshotNo(ctx, targetType, targetID)
	if err != nil {
		return
	}
	_ = s.repo.CreateContentSnapshot(ctx, &configdomain.ContentSnapshot{
		TargetType: targetType, TargetID: targetID, SnapshotNo: no,
		Content: cloneContentMap(content), ChangeReason: reason,
		FilesHash: hashFiles(extractFiles(content)), CreatedBy: actorID,
	})
}

// ListSnapshots 列出目标的历史快照。
func (s *Service) ListSnapshots(ctx context.Context, targetType configdomain.SnapshotTargetType, targetID int64) ([]*configdomain.ContentSnapshot, error) {
	items, err := s.repo.ListContentSnapshots(ctx, targetType, targetID)
	if err != nil {
		return nil, apperr.Internal("list config snapshots", err)
	}
	return items, nil
}

// DiffFileInput 单文件 diff 输入。
type DiffFileInput struct {
	TargetType   configdomain.SnapshotTargetType
	TargetID     int64
	SnapshotID   int64 // 0 表示与当前比对的左侧为「上一快照」逻辑由调用方指定
	FromSnapshot int64 // 历史快照 ID；0=不用
	FilePath     string
	Current      map[string]any // 当前 content（由调用方解析后传入）
}

// FileDiffResult 单文件 diff 结果。
type FileDiffResult struct {
	FilePath string `json:"file_path"`
	Original string `json:"original"`
	Modified string `json:"modified"`
	Language string `json:"language"`
}

// DiffConfigFile 对比历史快照与当前版本的单个文件。
func (s *Service) DiffConfigFile(ctx context.Context, snapshotID int64, current map[string]any, filePath string) (*FileDiffResult, error) {
	if filePath == "" {
		return nil, apperr.Validation("file_path is required", nil)
	}
	snap, err := s.repo.GetContentSnapshot(ctx, snapshotID)
	if err != nil {
		if errors.Is(err, configdomain.ErrSnapshotNotFound) {
			return nil, apperr.NotFound("config snapshot", strconv.FormatInt(snapshotID, 10))
		}
		return nil, apperr.Internal("get config snapshot", err)
	}
	return &FileDiffResult{
		FilePath: filePath,
		Original: getFileContent(snap.Content, filePath),
		Modified: getFileContent(current, filePath),
		Language: languageForPath(filePath),
	}, nil
}

// ResolveGroupEffectiveContent 解析分组当前生效配置（绑定配置集优先，否则本地配置）。
func (s *Service) ResolveGroupEffectiveContent(ctx context.Context, groupID int64) (map[string]any, string, error) {
	bindings, err := s.repo.ListBindingsByGroup(ctx, groupID)
	if err != nil {
		return nil, "", apperr.Internal("list bindings", err)
	}
	if len(bindings) > 0 && bindings[0].ConfigSetID > 0 {
		cs, err := s.repo.GetConfigSetByID(ctx, bindings[0].ConfigSetID)
		if err != nil {
			return nil, "", apperr.Internal("get config set", err)
		}
		return cs.Content, "binding", nil
	}
	lc, err := s.repo.GetLocalConfigByGroup(ctx, groupID)
	if err != nil {
		if errors.Is(err, configdomain.ErrLocalConfigNotFound) {
			return map[string]any{}, "none", nil
		}
		return nil, "", apperr.Internal("get local config", err)
	}
	return lc.Content, "local", nil
}

// ListGroupConfigFiles 列出分组生效配置中的文件路径（供克隆选择）。
func (s *Service) ListGroupConfigFiles(ctx context.Context, groupID int64) ([]string, error) {
	content, _, err := s.ResolveGroupEffectiveContent(ctx, groupID)
	if err != nil {
		return nil, err
	}
	return listFilePaths(content), nil
}

// CloneFromGroupInput 从其他分组克隆配置。
type CloneFromGroupInput struct {
	TargetGroupID int64
	SourceGroupID int64
	FilePaths     []string
	IncludeEnv    bool
	IncludeCommand bool
	IncludeArgs   bool
	UpdatedBy     int64
}

// CloneFromGroup 将源分组选定文件克隆到目标分组本地配置（目标须未绑定配置集）。
func (s *Service) CloneFromGroup(ctx context.Context, in CloneFromGroupInput) (*configdomain.GroupLocalConfig, error) {
	if in.SourceGroupID == 0 || in.TargetGroupID == 0 {
		return nil, apperr.Validation("source_group_id and target_group_id are required", nil)
	}
	if in.SourceGroupID == in.TargetGroupID {
		return nil, apperr.Validation("cannot clone from self", nil)
	}
	if len(in.FilePaths) == 0 && !in.IncludeEnv && !in.IncludeCommand && !in.IncludeArgs {
		return nil, apperr.Validation("select at least one file or env/command/args", nil)
	}
	count, err := s.repo.CountActiveBindingsByGroup(ctx, in.TargetGroupID)
	if err != nil {
		return nil, apperr.Internal("count bindings", err)
	}
	if count > 0 {
		return nil, apperr.BusinessRule("target group has config binding, cannot clone to local config", configdomain.ErrGroupBoundCannotEditLocal)
	}
	srcContent, _, err := s.ResolveGroupEffectiveContent(ctx, in.SourceGroupID)
	if err != nil {
		return nil, err
	}
	var targetContent map[string]any
	if lc, err := s.repo.GetLocalConfigByGroup(ctx, in.TargetGroupID); err == nil {
		targetContent = cloneContentMap(lc.Content)
	} else if errors.Is(err, configdomain.ErrLocalConfigNotFound) {
		targetContent = map[string]any{}
	} else {
		return nil, apperr.Internal("get target local config", err)
	}
	merged := mergeSelectedFiles(targetContent, srcContent, in.FilePaths)
	merged = mergeEnvCommandArgs(merged, srcContent, in.IncludeEnv, in.IncludeCommand, in.IncludeArgs)
	return s.UpsertLocalConfig(ctx, UpsertLocalConfigInput{
		GroupID: in.TargetGroupID, Content: merged, UpdatedBy: in.UpdatedBy,
	})
}

// SnapshotTargetForGroup 返回分组配置 Tab 应展示的快照目标（绑定→config_set+group_bind，未绑定→group_local）。
func (s *Service) SnapshotTargetForGroup(ctx context.Context, groupID int64, configSetID int64, bound bool) (configdomain.SnapshotTargetType, int64, error) {
	if bound && configSetID > 0 {
		return configdomain.SnapshotConfigSet, configSetID, nil
	}
	return configdomain.SnapshotGroupLocal, groupID, nil
}

// ListGroupBindSnapshots 列出分组绑定/解绑生效快照。
func (s *Service) ListGroupBindSnapshots(ctx context.Context, groupID int64) ([]*configdomain.ContentSnapshot, error) {
	return s.ListSnapshots(ctx, configdomain.SnapshotGroupBind, groupID)
}

// GetCurrentContentForDiff 按目标类型取当前 content。
func (s *Service) GetCurrentContentForDiff(ctx context.Context, targetType configdomain.SnapshotTargetType, targetID int64) (map[string]any, error) {
	switch targetType {
	case configdomain.SnapshotConfigSet:
		cs, err := s.repo.GetConfigSetByID(ctx, targetID)
		if err != nil {
			return nil, apperr.NotFound("config set", strconv.FormatInt(targetID, 10))
		}
		return cs.Content, nil
	case configdomain.SnapshotGroupLocal:
		lc, err := s.repo.GetLocalConfigByGroup(ctx, targetID)
		if err != nil {
			if errors.Is(err, configdomain.ErrLocalConfigNotFound) {
				return map[string]any{}, nil
			}
			return nil, apperr.Internal("get local config", err)
		}
		return lc.Content, nil
	default:
		return nil, apperr.Validation(fmt.Sprintf("unsupported target type %s", targetType), nil)
	}
}
