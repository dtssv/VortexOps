package extapiapp

import (
	"context"
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/vortexops/vortexops/internal/application/applicationapp"
	"github.com/vortexops/vortexops/internal/application/buildapp"
	"github.com/vortexops/vortexops/internal/application/configapp"
	"github.com/vortexops/vortexops/internal/application/releaseapp"
	"github.com/vortexops/vortexops/internal/domain/application"
	"github.com/vortexops/vortexops/internal/domain/build"
	"github.com/vortexops/vortexops/internal/domain/extapi"
	"github.com/vortexops/vortexops/internal/domain/release"
	"github.com/vortexops/vortexops/internal/domain/workspace"
	"github.com/vortexops/vortexops/internal/infrastructure/k8s"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// DeployMiddlewareInput 通过开放 API 部署中间件（作为普通应用）请求。
type DeployMiddlewareInput struct {
	WorkspaceUUID   uuid.UUID
	ApplicationUUID *uuid.UUID
	Name            string
	DisplayName     string
	Description     string
	GroupName       string
	ImageRef        string
	RegistryUUID    uuid.UUID
	ClusterID       int64
	ClusterUUID     uuid.UUID
	Namespace       string
	Environment     string
	Replicas        int
	Resources       application.Resources
	MeshEnabled     bool
	WorkloadType    string
	Env             []map[string]any
	Files           []map[string]any
	Command         []string
	Args            []string
	ManagingTeam    string
	ActorID         int64
	Token           *extapi.ExternalToken
}

// DeployMiddlewareResult 部署结果。
type DeployMiddlewareResult struct {
	ApplicationUUID uuid.UUID
	GroupUUID       uuid.UUID
	ImageUUID       uuid.UUID
	ReleaseID       int64
}

// DeployMiddlewareAsApplication 创建/复用外部托管应用并触发标准发布流程。
func (s *Service) DeployMiddlewareAsApplication(ctx context.Context, in DeployMiddlewareInput) (*DeployMiddlewareResult, error) {
	if in.Token != nil && !in.Token.HasScope(extapi.ScopeMiddleware) {
		return nil, apperr.Forbidden("scope ext:middleware required", nil)
	}
	ws, err := s.workspaces.GetByUUID(ctx, in.WorkspaceUUID)
	if err != nil {
		return nil, err
	}
	if err := s.CheckWorkspaceAccess(in.Token, ws.ID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Name) == "" {
		return nil, apperr.Validation("name is required", nil)
	}
	if strings.TrimSpace(in.ImageRef) == "" {
		return nil, apperr.Validation("imageRef is required", nil)
	}
	if in.RegistryUUID == uuid.Nil {
		return nil, apperr.Validation("registryUuid is required", nil)
	}
	clusterID, err := s.resolveClusterID(ctx, in.ClusterID, in.ClusterUUID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Namespace) == "" {
		return nil, apperr.Validation("namespace is required", nil)
	}
	registryID, err := s.pg.GetRegistryByUUID(ctx, in.RegistryUUID)
	if err != nil {
		return nil, apperr.NotFound("registry", in.RegistryUUID.String())
	}

	var app *application.Application
	if in.ApplicationUUID != nil && *in.ApplicationUUID != uuid.Nil {
		app, err = s.apps.GetByUUID(ctx, *in.ApplicationUUID)
		if err != nil {
			return nil, err
		}
		if app.WorkspaceID != ws.ID {
			return nil, apperr.NotFound("application", in.ApplicationUUID.String())
		}
		if !isExternalManagedApp(app) {
			return nil, apperr.Forbidden("application is not externally managed", nil)
		}
	} else {
		meta := map[string]any{"managed_by": "ext_api"}
		if in.ManagingTeam != "" {
			meta["managed_by_team"] = in.ManagingTeam
		}
		workloadType := in.WorkloadType
		if workloadType == "" {
			workloadType = "deployment"
		}
		app, err = s.apps.Create(ctx, applicationapp.CreateInput{
			WorkspaceID: ws.ID,
			Name:        in.Name,
			Code:        in.Name,
			DisplayName: in.DisplayName,
			Description: in.Description,
			OwnerID:     in.ActorID,
			AppType:     application.AppTypeWeb,
			WorkloadType: workloadType,
			Metadata:    meta,
		})
		if err != nil {
			return nil, err
		}
	}

	groupName := in.GroupName
	if groupName == "" {
		groupName = "default"
	}
	g, err := s.findOrCreateGroup(ctx, app, groupName, clusterID, in, in.ActorID)
	if err != nil {
		return nil, err
	}

	if s.configs != nil && hasMiddlewareConfig(in.Env, in.Files, in.Command, in.Args) {
		content := buildMiddlewareConfigContent(in.Env, in.Files, in.Command, in.Args)
		if _, err := s.configs.UpsertLocalConfig(ctx, configapp.UpsertLocalConfigInput{
			GroupID: g.ID, Name: "default", Content: content, UpdatedBy: in.ActorID,
		}); err != nil {
			return nil, err
		}
	}

	img, err := s.builds.RegisterExternalImage(ctx, buildapp.RegisterExternalImageInput{
		ApplicationID: app.ID,
		RegistryID:    registryID,
		FullReference: strings.TrimSpace(in.ImageRef),
		ActorID:       in.ActorID,
	})
	if err != nil {
		return nil, err
	}

	rel, err := s.releases.TriggerRelease(ctx, releaseapp.TriggerReleaseInput{
		GroupID: g.ID, ImageID: img.ID,
		ReleaseType: release.ReleaseRolling, Strategy: release.StrategyRolling,
		TriggeredBy: in.ActorID, TriggerSource: release.TriggerAPI,
	})
	if err != nil {
		return nil, err
	}

	return &DeployMiddlewareResult{
		ApplicationUUID: app.UUID,
		GroupUUID:       g.UUID,
		ImageUUID:       img.UUID,
		ReleaseID:       rel.ID,
	}, nil
}

// UpdateMiddlewareInput 更新外部托管中间件应用。
type UpdateMiddlewareInput struct {
	WorkspaceUUID uuid.UUID
	AppUUID       uuid.UUID
	Replicas      *int
	Resources     *application.Resources
	MeshEnabled   *bool
	Env           []map[string]any
	Files         []map[string]any
	Command       []string
	Args          []string
	ImageRef      string
	RegistryUUID  uuid.UUID
	Version       int
	ActorID       int64
	Token         *extapi.ExternalToken
}

// UpdateMiddlewareApplication 更新分组配置/资源，可选更换镜像并重新发布。
func (s *Service) UpdateMiddlewareApplication(ctx context.Context, in UpdateMiddlewareInput) (*application.Group, error) {
	if in.Token != nil && !in.Token.HasScope(extapi.ScopeMiddleware) {
		return nil, apperr.Forbidden("scope ext:middleware required", nil)
	}
	_, app, err := s.resolveMiddlewareApp(ctx, in.WorkspaceUUID, in.AppUUID, in.Token)
	if err != nil {
		return nil, err
	}
	g, err := s.primaryGroup(ctx, app.ID)
	if err != nil {
		return nil, err
	}

	if s.configs != nil && hasMiddlewareConfig(in.Env, in.Files, in.Command, in.Args) {
		content := buildMiddlewareConfigContent(in.Env, in.Files, in.Command, in.Args)
		if _, err := s.configs.UpsertLocalConfig(ctx, configapp.UpsertLocalConfigInput{
			GroupID: g.ID, Name: "default", Content: content, UpdatedBy: in.ActorID,
		}); err != nil {
			return nil, err
		}
	}

	version := in.Version
	if version == 0 {
		version = g.Version
	}
	updated, err := s.apps.UpdateGroup(ctx, applicationapp.UpdateGroupInput{
		ID: g.ID, Replicas: in.Replicas, Resources: in.Resources, MeshEnabled: in.MeshEnabled,
		Version: version, ActorID: in.ActorID,
	})
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(in.ImageRef) != "" {
		if in.RegistryUUID == uuid.Nil {
			return nil, apperr.Validation("registryUuid is required when imageRef is set", nil)
		}
		registryID, err := s.pg.GetRegistryByUUID(ctx, in.RegistryUUID)
		if err != nil {
			return nil, apperr.NotFound("registry", in.RegistryUUID.String())
		}
		img, err := s.builds.RegisterExternalImage(ctx, buildapp.RegisterExternalImageInput{
			ApplicationID: app.ID,
			RegistryID:    registryID,
			FullReference: strings.TrimSpace(in.ImageRef),
			ActorID:       in.ActorID,
		})
		if err != nil {
			return nil, err
		}
		if _, err := s.releases.TriggerRelease(ctx, releaseapp.TriggerReleaseInput{
			GroupID: updated.ID, ImageID: img.ID,
			ReleaseType: release.ReleaseRolling, Strategy: release.StrategyRolling,
			TriggeredBy: in.ActorID, TriggerSource: release.TriggerAPI,
		}); err != nil {
			return nil, err
		}
	}

	return updated, nil
}

// ScaleMiddlewareInput 中间件应用扩缩容。
type ScaleMiddlewareInput struct {
	WorkspaceUUID uuid.UUID
	AppUUID       uuid.UUID
	Replicas      int
	ActorID       int64
	Token         *extapi.ExternalToken
}

// ScaleMiddleware 调整外部托管应用主分组副本数。
func (s *Service) ScaleMiddleware(ctx context.Context, in ScaleMiddlewareInput) (*application.Group, error) {
	if in.Token != nil && !in.Token.HasScope(extapi.ScopeMiddleware) {
		return nil, apperr.Forbidden("scope ext:middleware required", nil)
	}
	_, app, err := s.resolveMiddlewareApp(ctx, in.WorkspaceUUID, in.AppUUID, in.Token)
	if err != nil {
		return nil, err
	}
	g, err := s.primaryGroup(ctx, app.ID)
	if err != nil {
		return nil, err
	}
	if in.Replicas < 0 {
		return nil, apperr.Validation("replicas must be non-negative", nil)
	}
	return s.apps.ScaleGroup(ctx, applicationapp.ScaleGroupInput{
		ID: g.ID, Replicas: in.Replicas, Version: g.Version, ActorID: in.ActorID,
	})
}

func (s *Service) resolveClusterID(ctx context.Context, clusterID int64, clusterUUID uuid.UUID) (int64, error) {
	if clusterID > 0 {
		return clusterID, nil
	}
	if clusterUUID != uuid.Nil {
		id, err := s.pg.GetClusterByUUID(ctx, clusterUUID)
		if err != nil {
			return 0, apperr.NotFound("cluster", clusterUUID.String())
		}
		return id, nil
	}
	return 0, apperr.Validation("clusterId or clusterUuid is required", nil)
}

func (s *Service) resolveMiddlewareApp(ctx context.Context, wsUUID, appUUID uuid.UUID, token *extapi.ExternalToken) (*workspace.Workspace, *application.Application, error) {
	ws, err := s.workspaces.GetByUUID(ctx, wsUUID)
	if err != nil {
		return nil, nil, err
	}
	if err := s.CheckWorkspaceAccess(token, ws.ID); err != nil {
		return nil, nil, err
	}
	app, err := s.apps.GetByUUID(ctx, appUUID)
	if err != nil {
		return nil, nil, err
	}
	if app.WorkspaceID != ws.ID {
		return nil, nil, apperr.NotFound("application", appUUID.String())
	}
	if !isExternalManagedApp(app) {
		return nil, nil, apperr.Forbidden("application is not externally managed", nil)
	}
	if token != nil && len(token.AllowedApps) > 0 {
		allowed := false
		for _, id := range token.AllowedApps {
			if id == app.ID {
				allowed = true
				break
			}
		}
		if !allowed {
			return nil, nil, apperr.Forbidden("application not allowed for this token", nil)
		}
	}
	return ws, app, nil
}

func (s *Service) primaryGroup(ctx context.Context, appID int64) (*application.Group, error) {
	groups, _, err := s.apps.ListGroups(ctx, appID, "", 0, "", "", 1, 1)
	if err != nil {
		return nil, apperr.Internal("list groups", err)
	}
	if len(groups) == 0 {
		return nil, apperr.NotFound("group", "no group for application")
	}
	return groups[0], nil
}

func (s *Service) findOrCreateGroup(ctx context.Context, app *application.Application, groupName string, clusterID int64, in DeployMiddlewareInput, actorID int64) (*application.Group, error) {
	groups, _, err := s.apps.ListGroups(ctx, app.ID, "", 0, "", "", 1, 200)
	if err != nil {
		return nil, apperr.Internal("list groups", err)
	}
	for _, g := range groups {
		if g.Name == groupName {
			return g, nil
		}
	}
	replicas := in.Replicas
	if replicas <= 0 {
		replicas = 1
	}
	env := application.EnvDev
	if in.Environment != "" {
		env = application.Environment(in.Environment)
	}
	return s.apps.CreateGroup(ctx, applicationapp.CreateGroupInput{
		ApplicationID: app.ID,
		Name:          groupName,
		DisplayName:   in.DisplayName,
		Description:   in.Description,
		Environment:   env,
		ClusterID:     clusterID,
		Namespace:     in.Namespace,
		Replicas:      replicas,
		Resources:     in.Resources,
		MeshEnabled:   in.MeshEnabled,
		ActorID:       actorID,
	})
}

func isExternalManagedApp(a *application.Application) bool {
	if a == nil || a.Metadata == nil {
		return false
	}
	v, _ := a.Metadata["managed_by"].(string)
	return v == "ext_api"
}

func hasMiddlewareConfig(env, files []map[string]any, command, args []string) bool {
	return len(env) > 0 || len(files) > 0 || len(command) > 0 || len(args) > 0
}

func buildMiddlewareConfigContent(env, files []map[string]any, command, args []string) map[string]any {
	content := map[string]any{}
	if len(env) > 0 {
		content["env"] = env
	}
	if len(files) > 0 {
		content["files"] = files
	}
	if len(command) > 0 {
		content["command"] = command
	}
	if len(args) > 0 {
		content["args"] = args
	}
	return content
}

// --- 生命周期：删除 / 停止 / 启动 ---

// DeleteMiddleware 删除外部托管应用（先删主分组，再删应用）。
// owner 鉴权由 applicationapp.DeleteGroup/Delete 内部执行；本方法额外校验 managed_by=ext_api。
func (s *Service) DeleteMiddleware(ctx context.Context, wsUUID, appUUID uuid.UUID, token *extapi.ExternalToken, actorID int64) error {
	if token != nil && !token.HasScope(extapi.ScopeMiddleware) {
		return apperr.Forbidden("scope ext:middleware required", nil)
	}
	_, app, err := s.resolveMiddlewareApp(ctx, wsUUID, appUUID, token)
	if err != nil {
		return err
	}
	// 先清理所有分组（DeleteGroup 要求应用 owner；actorID 来自 token 用户）。
	groups, _, err := s.apps.ListGroups(ctx, app.ID, "", 0, "", "", 1, 200)
	if err != nil {
		return apperr.Internal("list groups before delete", err)
	}
	for _, g := range groups {
		if err := s.apps.DeleteGroup(ctx, g.ID, actorID); err != nil {
			return err
		}
	}
	return s.apps.Delete(ctx, app.ID, actorID)
}

// StopMiddleware 停止（关机）：scale 到 0，原副本数存入 metadata.shutdown_replicas。
func (s *Service) StopMiddleware(ctx context.Context, wsUUID, appUUID uuid.UUID, token *extapi.ExternalToken, actorID int64) (*application.Group, error) {
	if token != nil && !token.HasScope(extapi.ScopeMiddleware) {
		return nil, apperr.Forbidden("scope ext:middleware required", nil)
	}
	_, app, err := s.resolveMiddlewareApp(ctx, wsUUID, appUUID, token)
	if err != nil {
		return nil, err
	}
	g, err := s.primaryGroup(ctx, app.ID)
	if err != nil {
		return nil, err
	}
	return s.apps.ShutdownGroup(ctx, g.ID, actorID)
}

// StartMiddleware 启动（开机）：恢复 metadata.shutdown_replicas 记录的副本数。
func (s *Service) StartMiddleware(ctx context.Context, wsUUID, appUUID uuid.UUID, token *extapi.ExternalToken, actorID int64) (*application.Group, error) {
	if token != nil && !token.HasScope(extapi.ScopeMiddleware) {
		return nil, apperr.Forbidden("scope ext:middleware required", nil)
	}
	_, app, err := s.resolveMiddlewareApp(ctx, wsUUID, appUUID, token)
	if err != nil {
		return nil, err
	}
	g, err := s.primaryGroup(ctx, app.ID)
	if err != nil {
		return nil, err
	}
	return s.apps.StartupGroup(ctx, g.ID, actorID)
}

// --- 成员管理 ---

// ListMiddlewareMembers 列出外部托管应用的成员。
func (s *Service) ListMiddlewareMembers(ctx context.Context, wsUUID, appUUID uuid.UUID, token *extapi.ExternalToken, page, size int) ([]*application.Member, int64, error) {
	if token != nil && !token.HasScope(extapi.ScopeMiddleware) {
		return nil, 0, apperr.Forbidden("scope ext:middleware required", nil)
	}
	_, app, err := s.resolveMiddlewareApp(ctx, wsUUID, appUUID, token)
	if err != nil {
		return nil, 0, err
	}
	if page <= 0 {
		page = 1
	}
	if size <= 0 || size > 200 {
		size = 50
	}
	return s.apps.ListMembers(ctx, app.ID, page, size)
}

// AddMiddlewareMemberInput 添加成员请求。
type AddMiddlewareMemberInput struct {
	WorkspaceUUID uuid.UUID
	AppUUID       uuid.UUID
	UserID        int64
	RoleID        int64
	ActorID       int64
	Token         *extapi.ExternalToken
}

// AddMiddlewareMember 添加外部托管应用成员。
func (s *Service) AddMiddlewareMember(ctx context.Context, in AddMiddlewareMemberInput) (*application.Member, error) {
	if in.Token != nil && !in.Token.HasScope(extapi.ScopeMiddleware) {
		return nil, apperr.Forbidden("scope ext:middleware required", nil)
	}
	_, app, err := s.resolveMiddlewareApp(ctx, in.WorkspaceUUID, in.AppUUID, in.Token)
	if err != nil {
		return nil, err
	}
	return s.apps.AddMember(ctx, applicationapp.AddMemberInput{
		ApplicationID: app.ID, UserID: in.UserID, RoleID: in.RoleID, ActorID: in.ActorID,
	})
}

// UpdateMiddlewareMemberRole 更新外部托管应用成员角色。
func (s *Service) UpdateMiddlewareMemberRole(ctx context.Context, wsUUID, appUUID uuid.UUID, userID, roleID int64, token *extapi.ExternalToken, actorID int64) error {
	if token != nil && !token.HasScope(extapi.ScopeMiddleware) {
		return apperr.Forbidden("scope ext:middleware required", nil)
	}
	_, app, err := s.resolveMiddlewareApp(ctx, wsUUID, appUUID, token)
	if err != nil {
		return err
	}
	return s.apps.UpdateMemberRole(ctx, app.ID, userID, roleID, actorID)
}

// RemoveMiddlewareMember 移除外部托管应用成员。
func (s *Service) RemoveMiddlewareMember(ctx context.Context, wsUUID, appUUID uuid.UUID, userID int64, token *extapi.ExternalToken, actorID int64) error {
	if token != nil && !token.HasScope(extapi.ScopeMiddleware) {
		return apperr.Forbidden("scope ext:middleware required", nil)
	}
	_, app, err := s.resolveMiddlewareApp(ctx, wsUUID, appUUID, token)
	if err != nil {
		return err
	}
	return s.apps.RemoveMember(ctx, app.ID, userID, actorID)
}

// --- 状态 / Pod / 发布历史（appUuid 键）---

// GetMiddlewareStatus 查询外部托管应用主分组状态 + 运行态。
func (s *Service) GetMiddlewareStatus(ctx context.Context, wsUUID, appUUID uuid.UUID, token *extapi.ExternalToken) (map[string]any, error) {
	if token != nil && !token.HasScope(extapi.ScopeMiddleware) {
		return nil, apperr.Forbidden("scope ext:middleware required", nil)
	}
	_, app, err := s.resolveMiddlewareApp(ctx, wsUUID, appUUID, token)
	if err != nil {
		return nil, err
	}
	g, err := s.primaryGroup(ctx, app.ID)
	if err != nil {
		return nil, err
	}
	return s.GetGroupStatus(ctx, wsUUID, g.UUID, token)
}

// ListMiddlewarePods 列出外部托管应用主分组的 Pod。
func (s *Service) ListMiddlewarePods(ctx context.Context, wsUUID, appUUID uuid.UUID, token *extapi.ExternalToken) ([]*k8s.PodSummary, error) {
	if token != nil && !token.HasScope(extapi.ScopeMiddleware) {
		return nil, apperr.Forbidden("scope ext:middleware required", nil)
	}
	_, app, err := s.resolveMiddlewareApp(ctx, wsUUID, appUUID, token)
	if err != nil {
		return nil, err
	}
	g, err := s.primaryGroup(ctx, app.ID)
	if err != nil {
		return nil, err
	}
	return s.ListGroupPods(ctx, wsUUID, g.UUID, token)
}

// RollbackMiddleware 回滚外部托管应用主分组到上一成功发布。
func (s *Service) RollbackMiddleware(ctx context.Context, wsUUID, appUUID uuid.UUID, token *extapi.ExternalToken, actorID int64) (*release.Release, error) {
	if token != nil && !token.HasScope(extapi.ScopeMiddleware) {
		return nil, apperr.Forbidden("scope ext:middleware required", nil)
	}
	_, app, err := s.resolveMiddlewareApp(ctx, wsUUID, appUUID, token)
	if err != nil {
		return nil, err
	}
	g, err := s.primaryGroup(ctx, app.ID)
	if err != nil {
		return nil, err
	}
	return s.releases.Rollback(ctx, g.ID, actorID)
}

// GetCurrentMiddlewareRelease 查询外部托管应用主分组的当前发布。
func (s *Service) GetCurrentMiddlewareRelease(ctx context.Context, wsUUID, appUUID uuid.UUID, token *extapi.ExternalToken) (*release.Release, error) {
	if token != nil && !token.HasScope(extapi.ScopeMiddleware) {
		return nil, apperr.Forbidden("scope ext:middleware required", nil)
	}
	_, app, err := s.resolveMiddlewareApp(ctx, wsUUID, appUUID, token)
	if err != nil {
		return nil, err
	}
	g, err := s.primaryGroup(ctx, app.ID)
	if err != nil {
		return nil, err
	}
	if s.releaseRO == nil {
		return nil, apperr.Internal("release reader not configured", nil)
	}
	rel, err := s.releaseRO.GetCurrentRelease(ctx, g.ID)
	if err != nil {
		if errors.Is(err, release.ErrReleaseNotFound) {
			return nil, apperr.NotFound("current release", appUUID.String())
		}
		return nil, apperr.Internal("get current release", err)
	}
	return rel, nil
}

// ListMiddlewareReleases 列出外部托管应用主分组的发布历史。
func (s *Service) ListMiddlewareReleases(ctx context.Context, wsUUID, appUUID uuid.UUID, status string, page, size int, token *extapi.ExternalToken) ([]*release.Release, int64, error) {
	if token != nil && !token.HasScope(extapi.ScopeMiddleware) {
		return nil, 0, apperr.Forbidden("scope ext:middleware required", nil)
	}
	_, app, err := s.resolveMiddlewareApp(ctx, wsUUID, appUUID, token)
	if err != nil {
		return nil, 0, err
	}
	g, err := s.primaryGroup(ctx, app.ID)
	if err != nil {
		return nil, 0, err
	}
	if page <= 0 {
		page = 1
	}
	if size <= 0 || size > 200 {
		size = 50
	}
	return s.releases.ListReleases(ctx, g.ID, release.Status(status), page, size)
}

// --- 镜像管理 ---

// ListMiddlewareImages 列出外部托管应用已登记的镜像（含外部 Source=manual）。
func (s *Service) ListMiddlewareImages(ctx context.Context, wsUUID, appUUID uuid.UUID, page, size int, token *extapi.ExternalToken) ([]*build.Image, int64, error) {
	if token != nil && !token.HasScope(extapi.ScopeMiddleware) {
		return nil, 0, apperr.Forbidden("scope ext:middleware required", nil)
	}
	_, app, err := s.resolveMiddlewareApp(ctx, wsUUID, appUUID, token)
	if err != nil {
		return nil, 0, err
	}
	if page <= 0 {
		page = 1
	}
	if size <= 0 || size > 200 {
		size = 50
	}
	return s.builds.ListImages(ctx, app.ID, page, size)
}

// RetireMiddlewareImage 退役外部托管应用下的镜像。
func (s *Service) RetireMiddlewareImage(ctx context.Context, wsUUID, appUUID uuid.UUID, imageID int64, token *extapi.ExternalToken) error {
	if token != nil && !token.HasScope(extapi.ScopeMiddleware) {
		return apperr.Forbidden("scope ext:middleware required", nil)
	}
	_, app, err := s.resolveMiddlewareApp(ctx, wsUUID, appUUID, token)
	if err != nil {
		return err
	}
	// 校验镜像归属本应用，避免越权退役其他应用镜像。
	img, err := s.builds.GetImage(ctx, imageID)
	if err != nil {
		return err
	}
	if img.ApplicationID != app.ID {
		return apperr.NotFound("image", strconv.FormatInt(imageID, 10))
	}
	return s.builds.RetireImage(ctx, imageID)
}

// StreamMiddlewarePodLogs 流式拉取外部托管应用主分组指定 Pod 的日志。
// 调用方需保证 out 为可写的响应流（如 http.ResponseWriter）。
func (s *Service) StreamMiddlewarePodLogs(ctx context.Context, wsUUID, appUUID uuid.UUID, podName, container string, tailLines int64, token *extapi.ExternalToken, out io.Writer) error {
	if token != nil && !token.HasScope(extapi.ScopeMiddleware) {
		return apperr.Forbidden("scope ext:middleware required", nil)
	}
	_, app, err := s.resolveMiddlewareApp(ctx, wsUUID, appUUID, token)
	if err != nil {
		return err
	}
	if s.podLogs == nil {
		return apperr.Internal("pod log streamer not configured", nil)
	}
	g, err := s.primaryGroup(ctx, app.ID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(podName) == "" {
		return apperr.Validation("pod name is required", nil)
	}
	return s.podLogs.StreamPodLogs(ctx, PodLogsInput{
		ClusterID: g.ClusterID, Namespace: g.Namespace, Pod: podName,
		Container: container, TailLines: tailLines, Follow: false,
	}, out)
}

