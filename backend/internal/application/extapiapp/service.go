// Package extapiapp 是对外 API 应用服务：Token 管理、自助建空间、操作编排与审计。
package extapiapp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/vortexops/vortexops/internal/application/applicationapp"
	"github.com/vortexops/vortexops/internal/application/buildapp"
	"github.com/vortexops/vortexops/internal/application/inferenceapp"
	"github.com/vortexops/vortexops/internal/application/pipelineapp"
	"github.com/vortexops/vortexops/internal/application/rbacapp"
	"github.com/vortexops/vortexops/internal/application/releaseapp"
	"github.com/vortexops/vortexops/internal/application/workspaceapp"
	"github.com/vortexops/vortexops/internal/domain/application"
	"github.com/vortexops/vortexops/internal/domain/build"
	"github.com/vortexops/vortexops/internal/domain/extapi"
	"github.com/vortexops/vortexops/internal/domain/inference"
	"github.com/vortexops/vortexops/internal/domain/pipeline"
	"github.com/vortexops/vortexops/internal/domain/release"
	"github.com/vortexops/vortexops/internal/domain/workspace"
	"github.com/vortexops/vortexops/internal/infrastructure/k8s"
	extapiredis "github.com/vortexops/vortexops/internal/infrastructure/redis/extapi"
	"github.com/vortexops/vortexops/internal/infrastructure/redis/runtime"
	extapirepo "github.com/vortexops/vortexops/internal/infrastructure/postgres/extapirepo"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// PGRepo 扩展 Postgres 仓储能力（UUID 解析、推理服务）。
type PGRepo interface {
	extapi.Repository
	GetImageByUUID(ctx context.Context, wsID int64, imageUUID uuid.UUID) (imageID, appID int64, err error)
	GetPipelineByUUID(ctx context.Context, wsID int64, pipelineUUID uuid.UUID) (pipelineID int64, err error)
	GetRunByUUID(ctx context.Context, wsID int64, runUUID uuid.UUID) (runID int64, err error)
	GetInferenceServiceByUUID(ctx context.Context, wsID int64, svcUUID uuid.UUID) (*inference.InferenceService, error)
	UpdateInferenceServiceReplicas(ctx context.Context, id int64, replicas int, actorID int64) error
	CreateInferenceService(ctx context.Context, s *inference.InferenceService) error
	ResolveModelVersionUUID(ctx context.Context, versionUUID uuid.UUID) (int64, error)
	GetClusterByUUID(ctx context.Context, clusterUUID uuid.UUID) (int64, error)
	GetRegistryByUUID(ctx context.Context, registryUUID uuid.UUID) (int64, error)
}

// ReleaseReader 读取当前发布。
type ReleaseReader interface {
	GetCurrentRelease(ctx context.Context, groupID int64) (*release.Release, error)
}

// PodLogStreamer 流式拉取 Pod 日志（由 clusterapp.Service 实现）。
type PodLogStreamer interface {
	StreamPodLogs(ctx context.Context, in PodLogsInput, out io.Writer) error
}

// PodLogsInput Pod 日志拉取参数（镜像 clusterapp.PodLogsInput，避免直接依赖 clusterapp）。
type PodLogsInput struct {
	ClusterID int64
	Namespace string
	Pod       string
	Container string
	TailLines int64
	Follow    bool
}

// Service 对外 API 应用服务。
type Service struct {
	repo       extapi.Repository
	pg         PGRepo
	rateLimit  *extapiredis.RateLimiter
	releases   *releaseapp.Service
	releaseRO  ReleaseReader
	builds     *buildapp.Service
	jenkins    buildapp.JenkinsClientFactory
	buildRO    build.Repository
	pipelines  *pipelineapp.Service
	inference  *inferenceapp.Service
	apps       *applicationapp.Service
	workspaces *workspaceapp.Service
	rbac       *rbacapp.Service
	rtCache    *runtime.Cache
	configs    LocalConfigWriter
	podLogs    PodLogStreamer
}

// New 创建对外 API 服务。
func New(
	repo extapi.Repository,
	pg PGRepo,
	rateLimit *extapiredis.RateLimiter,
	releases *releaseapp.Service,
	releaseRO ReleaseReader,
	builds *buildapp.Service,
	jenkins buildapp.JenkinsClientFactory,
	buildRO build.Repository,
	pipelines *pipelineapp.Service,
	inference *inferenceapp.Service,
	apps *applicationapp.Service,
	workspaces *workspaceapp.Service,
	rbac *rbacapp.Service,
	rtCache *runtime.Cache,
	configs LocalConfigWriter,
	podLogs PodLogStreamer,
) *Service {
	return &Service{
		repo: repo, pg: pg, rateLimit: rateLimit,
		releases: releases, releaseRO: releaseRO, builds: builds, jenkins: jenkins, buildRO: buildRO,
		pipelines: pipelines, inference: inference, apps: apps, workspaces: workspaces, rbac: rbac, rtCache: rtCache,
		configs: configs, podLogs: podLogs,
	}
}

// --- Token CRUD ---

// CreateTokenInput 创建 external Token。
type CreateTokenInput struct {
	UserID            int64
	Name              string
	Scopes            []string
	AllowedWorkspaces []int64
	AllowedApps       []int64
	RateLimitPerMin   *int
	IPAllowlist       []string
	WebhookURL        string
	ExpiresAt         *time.Time
	ActorID           int64
}

// CreateTokenResult 含一次性明文 Token。
type CreateTokenResult struct {
	Token     *extapi.ExternalToken
	Plaintext string
}

// CreateToken 创建 external Token（voe_ 前缀）。
func (s *Service) CreateToken(ctx context.Context, in CreateTokenInput) (*CreateTokenResult, error) {
	if in.Name == "" {
		return nil, apperr.Validation("token name is required", nil)
	}
	if err := validateScopes(in.Scopes); err != nil {
		return nil, err
	}
	plain, prefix, hash, err := generateExternalToken()
	if err != nil {
		return nil, apperr.Internal("generate token", err)
	}
	t := &extapi.ExternalToken{
		UserID: in.UserID, Name: in.Name, TokenPrefix: prefix, TokenHash: hash,
		Scopes: in.Scopes, AllowedWorkspaces: in.AllowedWorkspaces, AllowedApps: in.AllowedApps,
		RateLimitPerMin: in.RateLimitPerMin, IPAllowlist: in.IPAllowlist, WebhookURL: in.WebhookURL,
		ExpiresAt: in.ExpiresAt, Status: extapi.TokenStatusActive,
	}
	t.CreatedBy = in.ActorID
	t.UpdatedBy = in.ActorID
	if err := s.repo.CreateToken(ctx, t); err != nil {
		return nil, apperr.Internal("create token", err)
	}
	return &CreateTokenResult{Token: t, Plaintext: plain}, nil
}

// ListTokens 列出用户 external Token。
func (s *Service) ListTokens(ctx context.Context, userID int64, page, size int) ([]*extapi.ExternalToken, int64, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	items, total, err := s.repo.ListTokensByUser(ctx, extapi.TokenQuery{
		UserID: userID, Offset: (page - 1) * size, Limit: size,
	})
	if err != nil {
		return nil, 0, apperr.Internal("list tokens", err)
	}
	return items, total, nil
}

// RevokeToken 撤销 Token。
func (s *Service) RevokeToken(ctx context.Context, id, actorID int64) error {
	if err := s.repo.RevokeToken(ctx, id, actorID); err != nil {
		if errors.Is(err, extapi.ErrTokenNotFound) {
			return apperr.NotFound("external token", fmt.Sprint(id))
		}
		return apperr.Internal("revoke token", err)
	}
	return nil
}

// AuthenticateToken 校验 Bearer Token 并返回实体。
func (s *Service) AuthenticateToken(ctx context.Context, bearer string) (*extapi.ExternalToken, error) {
	bearer = strings.TrimSpace(bearer)
	if !strings.HasPrefix(bearer, extapi.TokenPrefix) {
		return nil, apperr.Unauthorized("invalid external token format", nil)
	}
	hash := hashToken(bearer)
	t, err := s.repo.GetTokenByHash(ctx, hash)
	if err != nil {
		if errors.Is(err, extapi.ErrTokenNotFound) {
			return nil, apperr.Unauthorized("invalid or expired token", err)
		}
		return nil, apperr.Internal("lookup token", err)
	}
	now := time.Now()
	if !t.IsActive(now) {
		if t.Status == extapi.TokenStatusRevoked {
			return nil, apperr.Unauthorized("token revoked", extapi.ErrTokenRevoked)
		}
		return nil, apperr.Unauthorized("token expired", extapi.ErrTokenExpired)
	}
	return t, nil
}

// CheckRateLimit 校验 Token 限流。
func (s *Service) CheckRateLimit(ctx context.Context, t *extapi.ExternalToken) error {
	if s.rateLimit == nil || t.RateLimitPerMin == nil {
		return nil
	}
	limit := *t.RateLimitPerMin
	ok, retryAfter, err := s.rateLimit.Allow(ctx, t.ID, limit)
	if err != nil {
		return apperr.Internal("rate limit check", err)
	}
	if !ok {
		return apperr.RateLimited(fmt.Sprintf("rate limit exceeded, retry after %ds", int(retryAfter.Seconds())), nil)
	}
	return nil
}

// CheckIPAllowlist 校验客户端 IP。
func (s *Service) CheckIPAllowlist(t *extapi.ExternalToken, clientIP string) error {
	if len(t.IPAllowlist) == 0 {
		return nil
	}
	ip := net.ParseIP(clientIP)
	if ip == nil {
		return apperr.Forbidden("client ip not allowed", extapi.ErrIPNotAllowed)
	}
	for _, cidr := range t.IPAllowlist {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			continue
		}
		if strings.Contains(cidr, "/") {
			_, n, err := net.ParseCIDR(cidr)
			if err == nil && n.Contains(ip) {
				return nil
			}
		} else if ip.String() == cidr {
			return nil
		}
	}
	return apperr.Forbidden("client ip not in allowlist", extapi.ErrIPNotAllowed)
}

// CheckScope 校验 scope。
func (s *Service) CheckScope(t *extapi.ExternalToken, scope string) error {
	if !t.HasScope(scope) {
		return apperr.Forbidden("insufficient scope: "+scope, extapi.ErrScopeDenied)
	}
	return nil
}

// CheckWorkspaceAccess 校验 Token 空间范围。
func (s *Service) CheckWorkspaceAccess(t *extapi.ExternalToken, wsID int64) error {
	if len(t.AllowedWorkspaces) == 0 {
		return nil
	}
	for _, id := range t.AllowedWorkspaces {
		if id == wsID {
			return nil
		}
	}
	return apperr.Forbidden("workspace not allowed for this token", nil)
}

// TouchToken 更新 Token 最后使用信息。
func (s *Service) TouchToken(ctx context.Context, t *extapi.ExternalToken, ip string) {
	_ = s.repo.UpdateTokenLastUsed(ctx, t.ID, ip, time.Now())
}

// AppendCallLog 写入调用审计。
func (s *Service) AppendCallLog(ctx context.Context, log *extapi.ExternalCallLog) {
	if err := s.repo.AppendCallLog(ctx, log); err != nil {
		// 审计失败不阻塞响应
		_ = err
	}
}

// GetIdempotency 读取幂等缓存。
func (s *Service) GetIdempotency(ctx context.Context, key string) (*extapi.IdempotencyRecord, error) {
	return s.repo.GetIdempotency(ctx, key)
}

// SetIdempotency 写入幂等缓存。
func (s *Service) SetIdempotency(ctx context.Context, rec *extapi.IdempotencyRecord, ttl time.Duration) error {
	return s.repo.SetIdempotency(ctx, rec, ttl)
}

// --- 自助建空间 ---

// SelfCreateWorkspaceInput 自助创建空间。
type SelfCreateWorkspaceInput struct {
	Name        string
	DisplayName string
	Description string
	ActorID     int64
}

// SelfCreateWorkspace 按 vo_workspace_creation_policies 自助建空间。
func (s *Service) SelfCreateWorkspace(ctx context.Context, in SelfCreateWorkspaceInput) (*workspace.Workspace, error) {
	policy, err := s.matchCreationPolicy(ctx, in.ActorID)
	if err != nil {
		return nil, err
	}
	if !policy.AllowSelfCreate {
		return nil, apperr.Forbidden("self workspace creation not allowed", extapi.ErrSelfCreateDenied)
	}
	if policy.RequireApproval {
		return nil, apperr.BusinessRule("workspace creation requires approval", nil)
	}
	count, err := s.repo.CountUserWorkspaces(ctx, in.ActorID)
	if err != nil {
		return nil, apperr.Internal("count workspaces", err)
	}
	if int(count) >= policy.MaxWorkspacesPerUser {
		return nil, apperr.BusinessRule("max workspaces per user exceeded", workspace.ErrQuotaExceeded)
	}

	maxApps, maxGroups, maxMembers := 50, 200, 100
	if v, ok := policy.DefaultQuota["max_applications"].(float64); ok {
		maxApps = int(v)
	}
	if v, ok := policy.DefaultQuota["max_groups"].(float64); ok {
		maxGroups = int(v)
	}
	if v, ok := policy.DefaultQuota["max_members"].(float64); ok {
		maxMembers = int(v)
	}

	w, err := s.workspaces.Create(ctx, workspaceapp.CreateInput{
		Name: in.Name, DisplayName: in.DisplayName, Description: in.Description,
		OwnerID: in.ActorID, MaxApplications: maxApps, MaxGroups: maxGroups, MaxMembers: maxMembers,
	})
	if err != nil {
		return nil, err
	}
	for _, clusterID := range policy.DefaultClusters {
		_, _ = s.workspaces.AddClusterBinding(ctx, workspaceapp.AddClusterBindingInput{
			WorkspaceID: w.ID, ClusterID: clusterID, Namespace: w.Name, ActorID: in.ActorID,
		})
	}
	return w, nil
}

func (s *Service) matchCreationPolicy(ctx context.Context, userID int64) (*extapi.WorkspaceCreationPolicy, error) {
	policies, err := s.repo.ListWorkspaceCreationPolicies(ctx)
	if err != nil {
		return nil, apperr.Internal("list creation policies", err)
	}
	if len(policies) == 0 {
		return nil, apperr.BusinessRule("no workspace creation policy configured", extapi.ErrPolicyNotFound)
	}
	roles, err := s.rbac.ListPlatformRolesByUser(ctx, userID)
	if err != nil {
		return nil, apperr.Internal("list user roles", err)
	}
	roleCodes := make(map[string]struct{}, len(roles))
	for _, r := range roles {
		roleCodes[r.Code] = struct{}{}
	}
	for _, p := range policies {
		if len(p.AppliesToRoles) == 0 {
			return p, nil
		}
		for _, rc := range p.AppliesToRoles {
			if _, ok := roleCodes[rc]; ok {
				return p, nil
			}
		}
	}
	return policies[0], nil
}

// --- 部署 / 回滚 ---

// DeployInput 部署请求。
type DeployInput struct {
	WorkspaceUUID uuid.UUID
	GroupUUID     uuid.UUID
	ImageUUID     uuid.UUID
	ConfigVersion int
	Strategy      string
	CanaryPercent int
	ChangeSummary string
	CallbackURL   string
	ActorID       int64
	Token         *extapi.ExternalToken
}

// Deploy 部署制品到分组。
func (s *Service) Deploy(ctx context.Context, in DeployInput) (*release.Release, error) {
	ws, g, err := s.resolveGroup(ctx, in.WorkspaceUUID, in.GroupUUID, in.Token)
	if err != nil {
		return nil, err
	}
	imageID, _, err := s.pg.GetImageByUUID(ctx, ws.ID, in.ImageUUID)
	if err != nil {
		return nil, apperr.NotFound("image", in.ImageUUID.String())
	}
	strategy := release.StrategyRolling
	if in.Strategy == "canary" {
		strategy = release.StrategyCanary
	}
	rel, err := s.releases.TriggerRelease(ctx, releaseapp.TriggerReleaseInput{
		GroupID: g.ID, ImageID: imageID, ConfigVersion: in.ConfigVersion,
		ReleaseType: release.ReleaseRolling, Strategy: strategy, TriggeredBy: in.ActorID,
		TriggerSource: release.TriggerAPI,
	})
	if err != nil {
		return nil, err
	}
	return rel, nil
}

// RollbackInput 回滚请求。
type RollbackInput struct {
	WorkspaceUUID uuid.UUID
	GroupUUID     uuid.UUID
	ActorID       int64
	Token         *extapi.ExternalToken
}

// Rollback 回滚分组到上一成功发布。
func (s *Service) Rollback(ctx context.Context, in RollbackInput) (*release.Release, error) {
	_, g, err := s.resolveGroup(ctx, in.WorkspaceUUID, in.GroupUUID, in.Token)
	if err != nil {
		return nil, err
	}
	return s.releases.Rollback(ctx, g.ID, in.ActorID)
}

// GetCurrentRelease 查询分组当前发布。
func (s *Service) GetCurrentRelease(ctx context.Context, wsUUID, groupUUID uuid.UUID, token *extapi.ExternalToken) (*release.Release, error) {
	_, g, err := s.resolveGroup(ctx, wsUUID, groupUUID, token)
	if err != nil {
		return nil, err
	}
	if s.releaseRO == nil {
		return nil, apperr.Internal("release reader not configured", nil)
	}
	rel, err := s.releaseRO.GetCurrentRelease(ctx, g.ID)
	if err != nil {
		if errors.Is(err, release.ErrReleaseNotFound) {
			return nil, apperr.NotFound("current release", groupUUID.String())
		}
		return nil, apperr.Internal("get current release", err)
	}
	return rel, nil
}

// --- 扩缩容 ---

// ScaleGroupInput 分组扩缩容。
type ScaleGroupInput struct {
	WorkspaceUUID uuid.UUID
	GroupUUID     uuid.UUID
	Replicas      int
	ActorID       int64
	Token         *extapi.ExternalToken
}

// ScaleGroup 调整分组副本数（委托给 applicationapp.ScaleGroup，确保同步 K8s）。
func (s *Service) ScaleGroup(ctx context.Context, in ScaleGroupInput) (*application.Group, error) {
	_, g, err := s.resolveGroup(ctx, in.WorkspaceUUID, in.GroupUUID, in.Token)
	if err != nil {
		return nil, err
	}
	return s.apps.ScaleGroup(ctx, applicationapp.ScaleGroupInput{
		ID: g.ID, Replicas: in.Replicas, Version: g.Version, ActorID: in.ActorID,
	})
}

// --- 构建 ---

// TriggerBuildInput 触发构建。
type TriggerBuildInput struct {
	WorkspaceUUID   uuid.UUID
	AppUUID         uuid.UUID
	GitSourceID     int64
	RefType         string
	RefValue        string
	IdempotencyKey  string
	ActorID         int64
	Token           *extapi.ExternalToken
}

// TriggerBuild 触发应用构建。
func (s *Service) TriggerBuild(ctx context.Context, in TriggerBuildInput) (*build.Build, error) {
	ws, err := s.workspaces.GetByUUID(ctx, in.WorkspaceUUID)
	if err != nil {
		return nil, err
	}
	if err := s.CheckWorkspaceAccess(in.Token, ws.ID); err != nil {
		return nil, err
	}
	app, err := s.apps.GetByUUID(ctx, in.AppUUID)
	if err != nil {
		return nil, err
	}
	if app.WorkspaceID != ws.ID {
		return nil, apperr.NotFound("application", in.AppUUID.String())
	}
	refType := build.RefBranch
	if in.RefType != "" {
		refType = build.RefType(in.RefType)
	}
	return s.builds.TriggerBuild(ctx, buildapp.TriggerBuildInput{
		ApplicationID: app.ID, GitSourceID: in.GitSourceID, RefType: refType, RefValue: in.RefValue,
		TriggeredBy: in.ActorID, TriggerSource: build.TriggerAPI, IdempotencyKey: in.IdempotencyKey,
	}, s.jenkins)
}

// GetBuild 查询构建状态。
func (s *Service) GetBuild(ctx context.Context, wsUUID, buildUUID uuid.UUID, token *extapi.ExternalToken) (*build.Build, error) {
	ws, err := s.workspaces.GetByUUID(ctx, wsUUID)
	if err != nil {
		return nil, err
	}
	if err := s.CheckWorkspaceAccess(token, ws.ID); err != nil {
		return nil, err
	}
	b, err := s.buildRO.GetBuildByUUID(ctx, buildUUID)
	if err != nil {
		return nil, apperr.NotFound("build", buildUUID.String())
	}
	app, err := s.apps.Get(ctx, b.ApplicationID)
	if err != nil {
		return nil, err
	}
	if app.WorkspaceID != ws.ID {
		return nil, apperr.NotFound("build", buildUUID.String())
	}
	return b, nil
}

// --- 流水线 ---

// TriggerPipelineInput 触发流水线。
type TriggerPipelineInput struct {
	WorkspaceUUID uuid.UUID
	PipelineUUID  uuid.UUID
	TriggerRef    string
	ActorID       int64
	Token         *extapi.ExternalToken
}

// TriggerPipeline 触发流水线运行。
func (s *Service) TriggerPipeline(ctx context.Context, in TriggerPipelineInput) (*pipeline.Run, error) {
	ws, err := s.workspaces.GetByUUID(ctx, in.WorkspaceUUID)
	if err != nil {
		return nil, err
	}
	if err := s.CheckWorkspaceAccess(in.Token, ws.ID); err != nil {
		return nil, err
	}
	pipelineID, err := s.pg.GetPipelineByUUID(ctx, ws.ID, in.PipelineUUID)
	if err != nil {
		return nil, apperr.NotFound("pipeline", in.PipelineUUID.String())
	}
	return s.pipelines.TriggerRun(ctx, pipelineapp.TriggerRunInput{
		PipelineID: pipelineID, TriggerRef: in.TriggerRef, TriggerBy: in.ActorID,
	})
}

// GetPipelineRun 查询流水线运行。
func (s *Service) GetPipelineRun(ctx context.Context, wsUUID, runUUID uuid.UUID, token *extapi.ExternalToken) (*pipeline.Run, error) {
	ws, err := s.workspaces.GetByUUID(ctx, wsUUID)
	if err != nil {
		return nil, err
	}
	if err := s.CheckWorkspaceAccess(token, ws.ID); err != nil {
		return nil, err
	}
	runID, err := s.pg.GetRunByUUID(ctx, ws.ID, runUUID)
	if err != nil {
		return nil, apperr.NotFound("pipeline run", runUUID.String())
	}
	return s.pipelines.GetRun(ctx, runID)
}

// --- 推理服务 ---

// DeployInferenceInput 部署推理服务。
type DeployInferenceInput struct {
	WorkspaceUUID    uuid.UUID
	Name             string
	ClusterID        int64
	Namespace        string
	ModelVersionUUID uuid.UUID
	Replicas         int
	GPUCount         int
	Framework        string
	ActorID          int64
	Token            *extapi.ExternalToken
}

// DeployInference 创建并部署推理服务。
func (s *Service) DeployInference(ctx context.Context, in DeployInferenceInput) (*inference.InferenceService, error) {
	ws, err := s.workspaces.GetByUUID(ctx, in.WorkspaceUUID)
	if err != nil {
		return nil, err
	}
	if err := s.CheckWorkspaceAccess(in.Token, ws.ID); err != nil {
		return nil, err
	}
	modelVersionID, err := s.pg.ResolveModelVersionUUID(ctx, in.ModelVersionUUID)
	if err != nil {
		return nil, apperr.NotFound("model version", in.ModelVersionUUID.String())
	}
	fw := inference.FrameworkVLLM
	if in.Framework != "" {
		fw = inference.Framework(in.Framework)
	}
	replicas := in.Replicas
	if replicas <= 0 {
		replicas = 1
	}
	gpu := in.GPUCount
	if gpu <= 0 {
		gpu = 1
	}
	svc, err := s.inference.CreateService(ctx, inferenceapp.CreateServiceInput{
		WorkspaceID: ws.ID, Name: in.Name, ClusterID: in.ClusterID, Namespace: in.Namespace,
		BaseModelVersionID: modelVersionID, Framework: fw, Replicas: replicas, GPUCount: gpu,
		Resources: map[string]any{"gpu": gpu}, CreatedBy: in.ActorID,
	})
	if err != nil {
		return nil, err
	}
	if _, err := s.inference.Deploy(ctx, inferenceapp.DeployInput{
		ServiceID: svc.ID, ModelVersionID: modelVersionID, Replicas: replicas, StartedBy: in.ActorID,
	}); err != nil {
		return nil, err
	}
	return s.inference.GetService(ctx, svc.ID)
}

// ScaleInferenceInput 推理服务扩缩容。
type ScaleInferenceInput struct {
	WorkspaceUUID uuid.UUID
	ServiceUUID   uuid.UUID
	Replicas      int
	ActorID       int64
	Token         *extapi.ExternalToken
}

// ScaleInference 调整推理服务副本。
func (s *Service) ScaleInference(ctx context.Context, in ScaleInferenceInput) (*inference.InferenceService, error) {
	ws, err := s.workspaces.GetByUUID(ctx, in.WorkspaceUUID)
	if err != nil {
		return nil, err
	}
	if err := s.CheckWorkspaceAccess(in.Token, ws.ID); err != nil {
		return nil, err
	}
	svc, err := s.pg.GetInferenceServiceByUUID(ctx, ws.ID, in.ServiceUUID)
	if err != nil {
		return nil, apperr.NotFound("inference service", in.ServiceUUID.String())
	}
	if in.Replicas < 0 {
		return nil, apperr.Validation("replicas must be non-negative", nil)
	}
	return s.inference.Scale(ctx, svc.ID, in.Replicas, in.ActorID)
}

// GetInferenceService 查询推理服务状态。
func (s *Service) GetInferenceService(ctx context.Context, wsUUID, svcUUID uuid.UUID, token *extapi.ExternalToken) (*inference.InferenceService, error) {
	ws, err := s.workspaces.GetByUUID(ctx, wsUUID)
	if err != nil {
		return nil, err
	}
	if err := s.CheckWorkspaceAccess(token, ws.ID); err != nil {
		return nil, err
	}
	svc, err := s.pg.GetInferenceServiceByUUID(ctx, ws.ID, svcUUID)
	if err != nil {
		return nil, apperr.NotFound("inference service", svcUUID.String())
	}
	return s.inference.GetService(ctx, svc.ID)
}

// --- 状态查询 ---

// GetGroupStatus 分组详情 + 运行态。
func (s *Service) GetGroupStatus(ctx context.Context, wsUUID, groupUUID uuid.UUID, token *extapi.ExternalToken) (map[string]any, error) {
	ws, g, err := s.resolveGroup(ctx, wsUUID, groupUUID, token)
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"groupUuid":  g.UUID.String(),
		"name":       g.Name,
		"replicas":   g.Replicas,
		"namespace":  g.Namespace,
		"clusterId":  g.ClusterID,
		"workspaceId": ws.ID,
	}
	if s.rtCache != nil {
		rt, _ := s.rtCache.GetGroupRuntime(ctx, g.ClusterID, g.ID)
		if rt != nil {
			out["runtime"] = rt
		}
	}
	return out, nil
}

// ListGroupPods 列出分组 Pod 运行态。
func (s *Service) ListGroupPods(ctx context.Context, wsUUID, groupUUID uuid.UUID, token *extapi.ExternalToken) ([]*k8s.PodSummary, error) {
	_, g, err := s.resolveGroup(ctx, wsUUID, groupUUID, token)
	if err != nil {
		return nil, err
	}
	if s.rtCache == nil {
		return []*k8s.PodSummary{}, nil
	}
	pods, err := s.rtCache.ListPodsByNamespace(ctx, g.ClusterID, g.Namespace)
	if err != nil {
		return nil, apperr.Internal("list pods", err)
	}
	prefix := g.DeploymentName
	if prefix == "" {
		prefix = g.Name
	}
	filtered := make([]*k8s.PodSummary, 0)
	for _, p := range pods {
		if prefix == "" || strings.HasPrefix(p.Name, prefix) {
			filtered = append(filtered, p)
		}
	}
	return filtered, nil
}

// --- helpers ---

func (s *Service) resolveGroup(ctx context.Context, wsUUID, groupUUID uuid.UUID, token *extapi.ExternalToken) (*workspace.Workspace, *application.Group, error) {
	ws, err := s.workspaces.GetByUUID(ctx, wsUUID)
	if err != nil {
		return nil, nil, err
	}
	if err := s.CheckWorkspaceAccess(token, ws.ID); err != nil {
		return nil, nil, err
	}
	g, err := s.apps.GetGroupByUUID(ctx, groupUUID)
	if err != nil {
		return nil, nil, err
	}
	app, err := s.apps.Get(ctx, g.ApplicationID)
	if err != nil {
		return nil, nil, err
	}
	if app.WorkspaceID != ws.ID {
		return nil, nil, apperr.NotFound("group", groupUUID.String())
	}
	if len(token.AllowedApps) > 0 {
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
	return ws, g, nil
}

func validateScopes(scopes []string) error {
	valid := map[string]struct{}{}
	for _, sc := range extapi.AllScopes() {
		valid[sc] = struct{}{}
	}
	for _, sc := range scopes {
		if _, ok := valid[sc]; !ok {
			return apperr.Validation("invalid scope: "+sc, nil)
		}
	}
	return nil
}

func generateExternalToken() (plaintext, prefix, hash string, err error) {
	buf := make([]byte, 24)
	if _, err = rand.Read(buf); err != nil {
		return "", "", "", err
	}
	secret := hex.EncodeToString(buf)
	plaintext = extapi.TokenPrefix + secret
	if len(plaintext) > 16 {
		prefix = plaintext[:16]
	} else {
		prefix = plaintext
	}
	hash = hashToken(plaintext)
	return plaintext, prefix, hash, nil
}

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// Ensure extapirepo implements PGRepo at compile time.
var _ PGRepo = (*extapirepo.Repository)(nil)
