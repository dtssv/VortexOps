package applicationapp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/vortexops/vortexops/internal/domain/application"
	"github.com/vortexops/vortexops/internal/domain/workspace"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// TypedWorkspaceFactory 按 workspace 类型获取或创建专用工作空间。
// 由 workspaceapp.Service 实现（EnsureTypedWorkspace），通过接口注入避免循环依赖。
type TypedWorkspaceFactory interface {
	EnsureTypedWorkspace(ctx context.Context, wsType workspace.Type, ownerID int64) (*workspace.Workspace, error)
}

// CreateTypedApplicationInput 创建「带类型」的应用请求。
// 用于推理/中间件统一为应用分组：自动选择/创建对应类型的专用工作空间，
// 并在创建应用时写入 app_type 元数据 + code（默认 = name）。
type CreateTypedApplicationInput struct {
	AppType string // middleware / inference / web/worker/job
	Name    string
	Code    string // 空则回填为 Name
	// WorkspaceID 可选：若指定则在该空间创建；否则按 AppType 自动选择专用空间。
	WorkspaceID int64
	OwnerID     int64
}

// CreateTypedApplication 封装「按类型选空间 → 创建应用（带 app_type）」流程。
// 返回创建的应用。调用方后续可基于 application 创建 group（写 app_type 列）。
func (s *Service) CreateTypedApplication(ctx context.Context, in CreateTypedApplicationInput) (*application.Application, error) {
	if in.AppType == "" {
		return nil, apperr.Validation("app_type is required", nil)
	}
	if in.OwnerID == 0 {
		return nil, apperr.Validation("owner is required", nil)
	}
	if err := validateAppName(in.Name); err != nil {
		return nil, err
	}

	wsID := in.WorkspaceID
	if wsID == 0 {
		// 按类型自动建专用工作空间。
		if s.typedWSFactory == nil {
			return nil, apperr.BusinessRule("typed workspace factory not configured", nil)
		}
		ws, err := s.typedWSFactory.EnsureTypedWorkspace(ctx, appTypeToWorkspaceType(in.AppType), in.OwnerID)
		if err != nil {
			return nil, err
		}
		wsID = ws.ID
	}

	code := in.Code
	if code == "" {
		code = in.Name
	}
	// code 冲突兜底：追加 app_type 前缀避免在共用空间下撞码。
	if _, err := s.apps.GetApplicationByCode(ctx, wsID, code); err == nil {
		code = fmt.Sprintf("%s-%s", in.AppType, in.Name)
	} else if !errors.Is(err, application.ErrApplicationNotFound) {
		return nil, apperr.Internal("check application code", err)
	}

	a, err := s.Create(ctx, CreateInput{
		WorkspaceID: wsID,
		Name:        in.Name,
		Code:        code,
		OwnerID:     in.OwnerID,
		AppType:     in.AppType,
	})
	if err != nil {
		return nil, err
	}
	return a, nil
}

// WithTypedWorkspaceFactory 注入按类型创建专用工作空间的能力。
func (s *Service) WithTypedWorkspaceFactory(f TypedWorkspaceFactory) *Service {
	s.typedWSFactory = f
	return s
}

// CreateTypedApplicationForInfra 为推理/中间件创建带类型应用，返回 application id。
// 适配 inferenceapp 的 ApplicationCreator 接口（返回 id 而非实体）。
func (s *Service) CreateTypedApplicationForInfra(ctx context.Context, appType string, name, code string, workspaceID, ownerID int64) (int64, error) {
	a, err := s.CreateTypedApplication(ctx, CreateTypedApplicationInput{
		AppType: appType, Name: name, Code: code, WorkspaceID: workspaceID, OwnerID: ownerID,
	})
	if err != nil {
		return 0, err
	}
	return a.ID, nil
}

// CreateGroupForTypedAppForInfra 为带类型应用创建分组，返回 group id。
// 推理/中间件统一为应用分组时调用：1 集群 = 1 group。
func (s *Service) CreateGroupForTypedAppForInfra(ctx context.Context, applicationID int64, name string, clusterID int64, namespace string, appType string, actorID int64) (int64, error) {
	g, err := s.CreateGroup(ctx, CreateGroupInput{
		ApplicationID: applicationID,
		Name:          name,
		ClusterID:     clusterID,
		Namespace:     namespace,
		ActorID:       actorID,
	})
	if err != nil {
		return 0, err
	}
	return g.ID, nil
}

// sanitizeCode 生成合法 code（与 k8sName 类似但允许大写，用于业务编号展示）。
// 用于 code 自动生成场景兜底。
func sanitizeCode(parts ...string) string {
	var b strings.Builder
	for i, p := range parts {
		p = strings.TrimSpace(p)
		for _, r := range p {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
				b.WriteRune(r)
			case r == '-' || r == '_':
				b.WriteRune('-')
			default:
				b.WriteRune('-')
			}
		}
		if i < len(parts)-1 {
			b.WriteRune('-')
		}
	}
	out := b.String()
	for strings.Contains(out, "--") {
		out = strings.ReplaceAll(out, "--", "-")
	}
	return strings.Trim(out, "-")
}
