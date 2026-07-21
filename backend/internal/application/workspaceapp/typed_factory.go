package workspaceapp

import (
	"context"
	"errors"
	"fmt"

	"github.com/vortexops/vortexops/internal/domain/workspace"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// EnsureTypedWorkspace 按类型获取或创建专用工作空间。
// 命名约定：{type}-system（如 inference-system）。
// 专用空间放宽配额（应用/分组上限放大），用于承载统一为应用分组的资源。
func (s *Service) EnsureTypedWorkspace(ctx context.Context, wsType workspace.Type, ownerID int64) (*workspace.Workspace, error) {
	if wsType != workspace.TypeInference {
		return nil, apperr.Validation(fmt.Sprintf("unsupported workspace type: %s", wsType), nil)
	}
	if ownerID == 0 {
		return nil, apperr.Validation("owner is required", nil)
	}
	name := string(wsType) + "-system"

	// 先按类型 + 名称查找（避免与 GetByName 的全局名称冲突）。
	if existing, err := s.repo.GetByTypeAndName(ctx, wsType, name); err == nil {
		return existing, nil
	} else if !errors.Is(err, workspace.ErrWorkspaceNotFound) {
		return nil, apperr.Internal("lookup typed workspace", err)
	}

	// 不存在则创建，配额放宽。
	w := &workspace.Workspace{
		Name:        name,
		DisplayName: displayNameForType(wsType),
		Description: fmt.Sprintf("系统自动创建的 %s 专用空间", wsType),
		Status:      workspace.StatusActive,
		Type:        wsType,
		OwnerID:     ownerID,
		Labels:      map[string]string{"system": "true", "ws_type": string(wsType)},
		Metadata:    map[string]any{"auto_created": true},
	}
	w.CreatedBy = ownerID
	w.UpdatedBy = ownerID

	quota := &workspace.Quota{
		MaxApplications:     10000,
		MaxGroups:           50000,
		MaxConcurrentBuilds: 50,
		MaxImagesRetained:   1000,
		MaxMembers:          10,
	}
	if err := s.repo.Create(ctx, w, quota); err != nil {
		if errors.Is(err, workspace.ErrWorkspaceNameExists) {
			// 并发创建兜底：再次查找。
			if existing, err2 := s.repo.GetByTypeAndName(ctx, wsType, name); err2 == nil {
				return existing, nil
			}
		}
		return nil, apperr.Internal("create typed workspace", err)
	}
	return w, nil
}

func displayNameForType(t workspace.Type) string {
	switch t {
	case workspace.TypeInference:
		return "模型推理系统空间"
	default:
		return string(t) + "-system"
	}
}
