// Package buildapp 是构建领域的应用服务层。
// 编排：Git 源 CRUD、触发构建（Jenkins）、构建状态机、分阶段日志（进行中走 Jenkins 流式，完成后归档 S3）。
package buildapp

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/vortexops/vortexops/internal/domain"
	"github.com/vortexops/vortexops/internal/domain/build"
	"github.com/vortexops/vortexops/internal/domain/cluster"
	"github.com/vortexops/vortexops/internal/pkg/buildlog"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// Service 构建应用服务。
type Service struct {
	repo        build.Repository
	credRepo    cluster.Repository // 复用凭证仓储取 Jenkins/Harbor 凭证
	logStore    build.LogStore
	systemSvc   SystemSettingProvider
	poller      *BuildPoller
	appRepo     ApplicationRepo // 只读应用 git_url 等元信息
	engineFact  *BuildEngineFactory
}

// SystemSettingProvider 系统设置提供者（避免循环依赖，由 systemapp.Service 实现）。
type SystemSettingProvider interface {
	GetDefaultJenkinsID(ctx context.Context) (int64, error)
	GetDefaultRegistryID(ctx context.Context) (int64, error)
	GetBuildEngine(ctx context.Context) (string, error)
	GetTektonNamespace(ctx context.Context) (string, error)
	GetTektonKubeconfig(ctx context.Context) (string, error)
}

// BuildEngineFactory 按构建引擎类型构建对应客户端。
// tektonFactory 在 engine=tekton 时调用；jenkinsFactory 在 engine=jenkins 时调用。
type BuildEngineFactory struct {
	Jenkins JenkinsClientFactory
	Tekton  TektonClientFactory
}

// TektonClientFactory 构造 Tekton 引擎客户端（kubeconfig/namespace 从系统设置读取）。
type TektonClientFactory func(ctx context.Context) (build.BuildEngineClient, error)

// EngineClient 返回当前构建引擎客户端及引擎类型（jenkins|tekton）。
func (f *BuildEngineFactory) EngineClient(ctx context.Context, systemSvc SystemSettingProvider, jk *build.JenkinsInstance) (build.BuildEngineClient, string, error) {
	engine, err := systemSvc.GetBuildEngine(ctx)
	if err != nil {
		return nil, "", err
	}
	if engine == "tekton" {
		if f.Tekton == nil {
			return nil, engine, apperr.BusinessRule("tekton engine selected but no factory configured", nil)
		}
		client, err := f.Tekton(ctx)
		if err != nil {
			return nil, engine, err
		}
		return client, engine, nil
	}
	if f.Jenkins == nil {
		return nil, engine, apperr.BusinessRule("jenkins engine selected but no factory configured", nil)
	}
	jc, err := f.Jenkins(ctx, jk)
	if err != nil {
		return nil, engine, err
	}
	return &jenkinsEngineShim{jc: jc}, engine, nil
}

// jenkinsEngineShim 包装 JenkinsClient 为 BuildEngineClient（无状态，jobName/buildNum 由 runID 编码）。
type jenkinsEngineShim struct {
	jc build.JenkinsClient
}

func (s *jenkinsEngineShim) Trigger(ctx context.Context, buildID int64, params map[string]string) (string, error) {
	jobName := params["JENKINS_JOB_NAME"]
	if jobName == "" {
		return "", apperr.Validation("JENKINS_JOB_NAME required", nil)
	}
	queueID, err := s.jc.TriggerBuild(ctx, jobName, params)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s::queue::%s", jobName, queueID), nil
}

func (s *jenkinsEngineShim) GetStatus(ctx context.Context, runID string) (build.BuildStatus, bool, error) {
	jobName, buildNum, err := parseJenkinsRunID(runID)
	if err != nil {
		return "", false, err
	}
	if buildNum == 0 {
		return build.BuildQueued, true, nil
	}
	return s.jc.GetBuildStatus(ctx, jobName, buildNum)
}

func (s *jenkinsEngineShim) GetLog(ctx context.Context, runID string, start int64) (string, bool, error) {
	jobName, buildNum, err := parseJenkinsRunID(runID)
	if err != nil {
		return "", false, err
	}
	if buildNum == 0 {
		return "", false, nil
	}
	return s.jc.GetConsoleLog(ctx, jobName, buildNum, start)
}

func (s *jenkinsEngineShim) ListSteps(ctx context.Context, runID string) ([]build.EngineStep, error) {
	return []build.EngineStep{{Name: "jenkins-build", Status: build.StepRunning}}, nil
}

func (s *jenkinsEngineShim) Stop(ctx context.Context, runID string) error {
	jobName, buildNum, err := parseJenkinsRunID(runID)
	if err != nil {
		return err
	}
	if buildNum == 0 {
		return nil
	}
	return s.jc.StopBuild(ctx, jobName, buildNum)
}

func parseJenkinsRunID(runID string) (string, int, error) {
	parts := strings.SplitN(runID, "::", 3)
	if len(parts) < 3 {
		return "", 0, fmt.Errorf("invalid jenkins runID: %s", runID)
	}
	if parts[1] == "build" {
		n, err := strconv.Atoi(parts[2])
		if err != nil {
			return "", 0, fmt.Errorf("invalid jenkins build number: %s", parts[2])
		}
		return parts[0], n, nil
	}
	return parts[0], 0, nil
}

// New 创建构建服务。logStore 可为 nil（仅查询场景）。systemSvc 可为 nil（不读取默认 Jenkins/Registry）。
// appRepo 用于读取应用 git_url，支持基于应用 Git 源的分支列举与构建触发。
func New(repo build.Repository, credRepo cluster.Repository, logStore build.LogStore, systemSvc SystemSettingProvider, appRepo ApplicationRepo) *Service {
	s := &Service{repo: repo, credRepo: credRepo, logStore: logStore, systemSvc: systemSvc, appRepo: appRepo}
	s.poller = NewBuildPoller(repo)
	return s
}

// StartPoller 启动构建状态轮询（在 apiserver 进程后台运行）。
// 轮询运行中的构建，从构建引擎（Jenkins/Tekton）拉状态并更新 DB；构建完成时归档日志到 S3。
// registryFactory 用于构建成功后从镜像仓库拉取 digest/size 元信息。
// engineFact 同时支持 Jenkins 与 Tekton；jenkinsFactory 保留向后兼容。
func (s *Service) StartPoller(ctx context.Context, jenkinsFactory JenkinsClientFactory, registryFactory build.RegistryAdapterFactory, engineFact *BuildEngineFactory) {
	s.engineFact = engineFact
	s.poller.jenkinsFactory = jenkinsFactory
	s.poller.registryFactory = registryFactory
	s.poller.logStore = s.logStore
	s.poller.credRepo = s.credRepo
	s.poller.systemSvc = s.systemSvc
	s.poller.engineFact = engineFact
	go s.poller.Run(ctx)
}

// JenkinsClientFactory 按实例构建 Jenkins 客户端（解密凭证）。
type JenkinsClientFactory func(ctx context.Context, instance *build.JenkinsInstance) (build.JenkinsClient, error)

// --- Git 源 ---

// CreateGitSourceInput 创建 Git 源输入。
type CreateGitSourceInput struct {
	ApplicationID  int64
	Name           string
	Provider       build.GitProvider
	RepoURL        string
	DefaultBranch  string
	CredentialID   int64
	WebhookEnabled bool
	WebhookSecret  string
	ActorID        int64
}

// CreateGitSource 创建 Git 源。
func (s *Service) CreateGitSource(ctx context.Context, in CreateGitSourceInput) (*build.GitSource, error) {
	if err := validateGitSourceName(in.Name); err != nil {
		return nil, err
	}
	if in.RepoURL == "" {
		return nil, apperr.Validation("repo_url is required", nil)
	}
	if in.Provider == "" {
		in.Provider = build.GitGeneric
	}
	if in.DefaultBranch == "" {
		in.DefaultBranch = "main"
	}
	g := &build.GitSource{
		ApplicationID: in.ApplicationID, Name: in.Name, Provider: in.Provider, RepoURL: in.RepoURL,
		DefaultBranch: in.DefaultBranch, CredentialID: in.CredentialID, WebhookEnabled: in.WebhookEnabled,
	}
	g.CreatedBy = in.ActorID
	g.UpdatedBy = in.ActorID
	if err := s.repo.CreateGitSource(ctx, g); err != nil {
		return nil, apperr.Internal("create git source", err)
	}
	return g, nil
}

// ListGitSources 列出应用的 Git 源。
func (s *Service) ListGitSources(ctx context.Context, appID int64) ([]*build.GitSource, error) {
	items, err := s.repo.ListGitSources(ctx, appID)
	if err != nil {
		return nil, apperr.Internal("list git sources", err)
	}
	return items, nil
}

// DeleteGitSource 软删除 Git 源。
func (s *Service) DeleteGitSource(ctx context.Context, id, actorID int64) error {
	if err := s.repo.DeleteGitSource(ctx, id, actorID); err != nil {
		if errors.Is(err, build.ErrGitSourceNotFound) {
			return apperr.NotFound("git source", strconv.FormatInt(id, 10))
		}
		return apperr.Internal("delete git source", err)
	}
	return nil
}

// --- 镜像仓库与 Jenkins 实例 ---

// CreateRegistryInput 创建仓库输入。
type CreateRegistryInput struct {
	Name         string
	Type         build.RegistryType
	URL          string
	CredentialID int64
	IsDefault    bool
	CreatedBy    int64
}

// CreateRegistry 创建镜像仓库。
func (s *Service) CreateRegistry(ctx context.Context, in CreateRegistryInput) (*build.Registry, error) {
	if in.Name == "" {
		return nil, apperr.Validation("registry name is required", nil)
	}
	if in.URL == "" {
		return nil, apperr.Validation("registry url is required", nil)
	}
	if in.Type == "" {
		in.Type = build.RegistryHarbor
	}
	r := &build.Registry{
		Name: in.Name, Type: in.Type, URL: in.URL, CredentialID: in.CredentialID,
		IsDefault: in.IsDefault, Status: build.RegistryActive,
	}
	r.CreatedBy = in.CreatedBy
	r.UpdatedBy = in.CreatedBy
	if err := s.repo.CreateRegistry(ctx, r); err != nil {
		if errors.Is(err, build.ErrRegistryNameExists) {
			return nil, apperr.Conflict("registry name already exists", err)
		}
		return nil, apperr.Internal("create registry", err)
	}
	return r, nil
}

// ListRegistries 分页列出仓库。
func (s *Service) ListRegistries(ctx context.Context, page, size int) ([]*build.Registry, int64, error) {
	items, total, err := s.repo.ListRegistries(ctx, (page-1)*size, size)
	if err != nil {
		return nil, 0, apperr.Internal("list registries", err)
	}
	return items, total, nil
}

// GetRegistry 按 ID 获取仓库。
func (s *Service) GetRegistry(ctx context.Context, id int64) (*build.Registry, error) {
	r, err := s.repo.GetRegistryByID(ctx, id)
	if err != nil {
		if errors.Is(err, build.ErrRegistryNotFound) {
			return nil, apperr.NotFound("registry", strconv.FormatInt(id, 10))
		}
		return nil, apperr.Internal("get registry", err)
	}
	return r, nil
}

// UpdateRegistryInput 更新仓库入参。指针字段为 nil 表示不修改。
type UpdateRegistryInput struct {
	ID           int64
	Name         *string
	Type         *build.RegistryType
	URL          *string
	CredentialID *int64
	IsDefault    *bool
	Status       *build.RegistryStatus
	Version      int
	ActorID      int64
}

// UpdateRegistry 更新仓库（乐观锁）。
func (s *Service) UpdateRegistry(ctx context.Context, in UpdateRegistryInput) (*build.Registry, error) {
	existing, err := s.repo.GetRegistryByID(ctx, in.ID)
	if err != nil {
		if errors.Is(err, build.ErrRegistryNotFound) {
			return nil, apperr.NotFound("registry", strconv.FormatInt(in.ID, 10))
		}
		return nil, apperr.Internal("get registry", err)
	}
	if in.Name != nil {
		existing.Name = *in.Name
	}
	if in.Type != nil {
		existing.Type = *in.Type
	}
	if in.URL != nil {
		existing.URL = *in.URL
	}
	if in.CredentialID != nil {
		existing.CredentialID = *in.CredentialID
	}
	if in.IsDefault != nil {
		existing.IsDefault = *in.IsDefault
	}
	if in.Status != nil {
		existing.Status = *in.Status
	}
	existing.Version = in.Version
	existing.UpdatedBy = in.ActorID
	if err := s.repo.UpdateRegistry(ctx, existing); err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return nil, apperr.Conflict("registry was modified concurrently, please refresh", err)
		}
		return nil, apperr.Internal("update registry", err)
	}
	return existing, nil
}

// DeleteRegistry 软删除仓库。
func (s *Service) DeleteRegistry(ctx context.Context, id, actorID int64) error {
	if err := s.repo.DeleteRegistry(ctx, id, actorID); err != nil {
		if errors.Is(err, build.ErrRegistryNotFound) {
			return apperr.NotFound("registry", strconv.FormatInt(id, 10))
		}
		return apperr.Internal("delete registry", err)
	}
	return nil
}

// CreateJenkinsInput 创建 Jenkins 实例输入。
type CreateJenkinsInput struct {
	Name               string
	URL                string
	CredentialID       int64
	DefaultJobFolder   string
	IsDefault          bool
	CreatedBy          int64
}

// CreateJenkins 创建 Jenkins 实例。
func (s *Service) CreateJenkins(ctx context.Context, in CreateJenkinsInput) (*build.JenkinsInstance, error) {
	if in.Name == "" {
		return nil, apperr.Validation("jenkins name is required", nil)
	}
	if in.URL == "" {
		return nil, apperr.Validation("jenkins url is required", nil)
	}
	j := &build.JenkinsInstance{
		Name: in.Name, URL: in.URL, CredentialID: in.CredentialID,
		DefaultJobFolder: in.DefaultJobFolder, IsDefault: in.IsDefault, Status: build.JenkinsActive,
	}
	j.CreatedBy = in.CreatedBy
	j.UpdatedBy = in.CreatedBy
	if err := s.repo.CreateJenkins(ctx, j); err != nil {
		if errors.Is(err, build.ErrJenkinsNameExists) {
			return nil, apperr.Conflict("jenkins name already exists", err)
		}
		return nil, apperr.Internal("create jenkins", err)
	}
	return j, nil
}

// ListJenkins 分页列出 Jenkins 实例。
func (s *Service) ListJenkins(ctx context.Context, page, size int) ([]*build.JenkinsInstance, int64, error) {
	items, total, err := s.repo.ListJenkins(ctx, (page-1)*size, size)
	if err != nil {
		return nil, 0, apperr.Internal("list jenkins", err)
	}
	return items, total, nil
}

// GetJenkins 按 ID 获取 Jenkins 实例。
func (s *Service) GetJenkins(ctx context.Context, id int64) (*build.JenkinsInstance, error) {
	j, err := s.repo.GetJenkinsByID(ctx, id)
	if err != nil {
		if errors.Is(err, build.ErrJenkinsNotFound) {
			return nil, apperr.NotFound("jenkins", strconv.FormatInt(id, 10))
		}
		return nil, apperr.Internal("get jenkins", err)
	}
	return j, nil
}

// UpdateJenkinsInput 更新 Jenkins 实例入参。指针字段为 nil 表示不修改。
type UpdateJenkinsInput struct {
	ID               int64
	Name             *string
	URL              *string
	CredentialID     *int64
	DefaultJobFolder *string
	IsDefault        *bool
	Status           *build.JenkinsStatus
	Version          int
	ActorID          int64
}

// UpdateJenkins 更新 Jenkins 实例（乐观锁）。
func (s *Service) UpdateJenkins(ctx context.Context, in UpdateJenkinsInput) (*build.JenkinsInstance, error) {
	existing, err := s.repo.GetJenkinsByID(ctx, in.ID)
	if err != nil {
		if errors.Is(err, build.ErrJenkinsNotFound) {
			return nil, apperr.NotFound("jenkins", strconv.FormatInt(in.ID, 10))
		}
		return nil, apperr.Internal("get jenkins", err)
	}
	if in.Name != nil {
		existing.Name = *in.Name
	}
	if in.URL != nil {
		existing.URL = *in.URL
	}
	if in.CredentialID != nil {
		existing.CredentialID = *in.CredentialID
	}
	if in.DefaultJobFolder != nil {
		existing.DefaultJobFolder = *in.DefaultJobFolder
	}
	if in.IsDefault != nil {
		existing.IsDefault = *in.IsDefault
	}
	if in.Status != nil {
		existing.Status = *in.Status
	}
	existing.Version = in.Version
	existing.UpdatedBy = in.ActorID
	if err := s.repo.UpdateJenkins(ctx, existing); err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return nil, apperr.Conflict("jenkins was modified concurrently, please refresh", err)
		}
		return nil, apperr.Internal("update jenkins", err)
	}
	return existing, nil
}

// DeleteJenkins 软删除 Jenkins 实例。
func (s *Service) DeleteJenkins(ctx context.Context, id, actorID int64) error {
	if err := s.repo.DeleteJenkins(ctx, id, actorID); err != nil {
		if errors.Is(err, build.ErrJenkinsNotFound) {
			return apperr.NotFound("jenkins", strconv.FormatInt(id, 10))
		}
		return apperr.Internal("delete jenkins", err)
	}
	return nil
}

// --- 构建集成查询 ---

// BuildIntegration 构建集成配置（系统变量化默认 Jenkins + Registry）。
// 供前端应用详情页展示当前可用集成（只读 Tag）。
type BuildIntegration struct {
	Jenkins  *build.JenkinsInstance `json:"jenkins"`
	Registry *build.Registry        `json:"registry"`
}

// GetBuildIntegration 返回系统默认 Jenkins + Registry 实例。
// 优先取 is_default=true 的行；回退到系统设置 platform.default_*_id 指定的实例。
func (s *Service) GetBuildIntegration(ctx context.Context) (*BuildIntegration, error) {
	integration := &BuildIntegration{}

	// 默认 Jenkins。
	jk, _ := s.repo.GetDefaultJenkins(ctx)
	if jk == nil && s.systemSvc != nil {
		if jid, err := s.systemSvc.GetDefaultJenkinsID(ctx); err == nil && jid > 0 {
			jk, _ = s.repo.GetJenkinsByID(ctx, jid)
		}
	}
	integration.Jenkins = jk

	// 默认 Registry。
	reg, _ := s.repo.GetDefaultRegistry(ctx)
	if reg == nil && s.systemSvc != nil {
		if rid, err := s.systemSvc.GetDefaultRegistryID(ctx); err == nil && rid > 0 {
			reg, _ = s.repo.GetRegistryByID(ctx, rid)
		}
	}
	integration.Registry = reg

	return integration, nil
}

// TestJenkinsConnection 测试 Jenkins 实例连通性。
func (s *Service) TestJenkinsConnection(ctx context.Context, jk *build.JenkinsInstance, factory JenkinsClientFactory) error {
	if factory == nil {
		return apperr.Internal("jenkins factory not configured", nil)
	}
	client, err := factory(ctx, jk)
	if err != nil {
		// 工厂错误通常含凭证/URL 配置问题，把根因透出给用户便于排查。
		return apperr.Internal("构建 Jenkins 客户端失败: "+err.Error(), err)
	}
	// TriggerBuild 仅做参数构造，这里用一个轻量的状态查询做连通性校验。
	// JenkinsClient 没有专门的 ping，使用 GetBuildStatus 的 NotFound 作为「可达」信号。
	_, _, err = client.GetBuildStatus(ctx, jk.DefaultJobFolder+"/vortexops-ping", 1)
	if err != nil && !errors.Is(err, build.ErrBuildNotFound) {
		return apperr.Internal("Jenkins 连接测试失败: "+err.Error(), err)
	}
	return nil
}

// TestRegistryConnection 测试镜像仓库连通性。
func (s *Service) TestRegistryConnection(ctx context.Context, reg *build.Registry, factory build.RegistryAdapterFactory) error {
	if factory == nil {
		return apperr.Internal("registry factory not configured", nil)
	}
	adapter, err := factory(ctx, reg)
	if err != nil {
		return apperr.Internal("构建镜像仓库适配器失败: "+err.Error(), err)
	}
	if err := adapter.CheckConnection(ctx); err != nil {
		return apperr.Internal("镜像仓库连接测试失败: "+err.Error(), err)
	}
	return nil
}

// --- 基础镜像与模板 ---

// CreateBaseImageInput 创建基础镜像输入。
type CreateBaseImageInput struct {
	Name                string
	Runtime             build.BaseImageRuntime
	Registry            string
	ImageRef            string
	Digest              string
	IsSystem            bool
	IsRecommended       bool
	Description         string
	DockerfileTemplate  string
	BuildTool           string
	DefaultBuildCommand string
	DefaultArtifactPath string
	DefaultBuildArgs    map[string]string
	Entrypoint          []string
	IsWeb               bool
	CreatedBy           int64
}

// CreateBaseImage 创建基础镜像。
func (s *Service) CreateBaseImage(ctx context.Context, in CreateBaseImageInput) (*build.BaseImage, error) {
	if in.Name == "" || in.ImageRef == "" {
		return nil, apperr.Validation("name and image_ref are required", nil)
	}
	if in.Runtime == "" {
		in.Runtime = build.RuntimeCustom
	}
	b := &build.BaseImage{
		Name: in.Name, Runtime: in.Runtime, Registry: in.Registry, ImageRef: in.ImageRef, Digest: in.Digest,
		IsSystem: in.IsSystem, IsRecommended: in.IsRecommended, Description: in.Description,
		DockerfileTemplate:  in.DockerfileTemplate,
		BuildTool:           in.BuildTool,
		DefaultBuildCommand: in.DefaultBuildCommand,
		DefaultArtifactPath: in.DefaultArtifactPath,
		DefaultBuildArgs:    in.DefaultBuildArgs,
		Entrypoint:          in.Entrypoint,
		IsWeb:               in.IsWeb,
	}
	b.CreatedBy = in.CreatedBy
	b.UpdatedBy = in.CreatedBy
	if err := s.repo.CreateBaseImage(ctx, b); err != nil {
		return nil, apperr.Internal("create base image", err)
	}
	return b, nil
}

// ListBaseImages 分页列出基础镜像。
func (s *Service) ListBaseImages(ctx context.Context, runtime build.BaseImageRuntime, page, size int) ([]*build.BaseImage, int64, error) {
	items, total, err := s.repo.ListBaseImages(ctx, runtime, (page-1)*size, size)
	if err != nil {
		return nil, 0, apperr.Internal("list base images", err)
	}
	return items, total, nil
}

// GetBaseImage 按 ID 获取基础镜像。
func (s *Service) GetBaseImage(ctx context.Context, id int64) (*build.BaseImage, error) {
	b, err := s.repo.GetBaseImageByID(ctx, id)
	if err != nil {
		if errors.Is(err, build.ErrBaseImageNotFound) {
			return nil, apperr.NotFound("base image", strconv.FormatInt(id, 10))
		}
		return nil, apperr.Internal("get base image", err)
	}
	return b, nil
}

// UpdateBaseImageInput 更新基础镜像输入。指针字段为 nil 表示不修改。
type UpdateBaseImageInput struct {
	ID                  int64
	Name                *string
	Runtime             *build.BaseImageRuntime
	Registry            *string
	ImageRef            *string
	Digest              *string
	IsSystem            *bool
	IsRecommended       *bool
	Description         *string
	DockerfileTemplate  *string
	BuildTool           *string
	DefaultBuildCommand *string
	DefaultArtifactPath *string
	DefaultBuildArgs    *map[string]string
	Entrypoint          *[]string
	IsWeb               *bool
	Version             int
	ActorID             int64
}

// UpdateBaseImage 更新基础镜像（乐观锁）。
func (s *Service) UpdateBaseImage(ctx context.Context, in UpdateBaseImageInput) (*build.BaseImage, error) {
	b, err := s.repo.GetBaseImageByID(ctx, in.ID)
	if err != nil {
		if errors.Is(err, build.ErrBaseImageNotFound) {
			return nil, apperr.NotFound("base image", strconv.FormatInt(in.ID, 10))
		}
		return nil, apperr.Internal("get base image", err)
	}
	if in.Name != nil {
		b.Name = *in.Name
	}
	if in.Runtime != nil {
		b.Runtime = *in.Runtime
	}
	if in.Registry != nil {
		b.Registry = *in.Registry
	}
	if in.ImageRef != nil {
		b.ImageRef = *in.ImageRef
	}
	if in.Digest != nil {
		b.Digest = *in.Digest
	}
	if in.IsSystem != nil {
		b.IsSystem = *in.IsSystem
	}
	if in.IsRecommended != nil {
		b.IsRecommended = *in.IsRecommended
	}
	if in.Description != nil {
		b.Description = *in.Description
	}
	if in.DockerfileTemplate != nil {
		b.DockerfileTemplate = *in.DockerfileTemplate
	}
	if in.BuildTool != nil {
		b.BuildTool = *in.BuildTool
	}
	if in.DefaultBuildCommand != nil {
		b.DefaultBuildCommand = *in.DefaultBuildCommand
	}
	if in.DefaultArtifactPath != nil {
		b.DefaultArtifactPath = *in.DefaultArtifactPath
	}
	if in.DefaultBuildArgs != nil {
		b.DefaultBuildArgs = *in.DefaultBuildArgs
	}
	if in.Entrypoint != nil {
		b.Entrypoint = *in.Entrypoint
	}
	if in.IsWeb != nil {
		b.IsWeb = *in.IsWeb
	}
	b.UpdatedBy = in.ActorID
	if err := s.repo.UpdateBaseImage(ctx, b); err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return nil, apperr.Conflict("base image was modified concurrently, please refresh", err)
		}
		return nil, apperr.Internal("update base image", err)
	}
	return b, nil
}

// DeleteBaseImage 软删除基础镜像。
func (s *Service) DeleteBaseImage(ctx context.Context, id, actorID int64) error {
	if err := s.repo.DeleteBaseImage(ctx, id, actorID); err != nil {
		if errors.Is(err, build.ErrBaseImageNotFound) {
			return apperr.NotFound("base image", strconv.FormatInt(id, 10))
		}
		return apperr.Internal("delete base image", err)
	}
	return nil
}

// --- 构建工具（BuildTool）CRUD ---

// CreateBuildToolInput 创建构建工具输入。
type CreateBuildToolInput struct {
	Name              string
	Runtime           build.BaseImageRuntime
	Tool              string
	DefaultBuildCommand string
	DefaultArtifactPath string
	BuilderImage      string
	IsSystem          bool
	Description       string
	CreatedBy         int64
}

// ListBuildTools 分页列出构建工具（可按 runtime 过滤）。
func (s *Service) ListBuildTools(ctx context.Context, runtime build.BaseImageRuntime, page, size int) ([]*build.BuildTool, int64, error) {
	if page < 1 {
		page = 1
	}
	if size <= 0 {
		size = 50
	}
	items, total, err := s.repo.ListBuildTools(ctx, runtime, (page-1)*size, size)
	if err != nil {
		return nil, 0, apperr.Internal("list build tools", err)
	}
	return items, total, nil
}

// GetBuildTool 按 ID 查询构建工具。
func (s *Service) GetBuildTool(ctx context.Context, id int64) (*build.BuildTool, error) {
	bt, err := s.repo.GetBuildToolByID(ctx, id)
	if err != nil {
		if errors.Is(err, build.ErrBuildToolNotFound) {
			return nil, apperr.NotFound("build tool", strconv.FormatInt(id, 10))
		}
		return nil, apperr.Internal("get build tool", err)
	}
	return bt, nil
}

// CreateBuildTool 创建构建工具。
func (s *Service) CreateBuildTool(ctx context.Context, in CreateBuildToolInput) (*build.BuildTool, error) {
	if in.Name == "" || in.Runtime == "" || in.Tool == "" || in.BuilderImage == "" {
		return nil, apperr.Validation("name, runtime, tool, builder_image are required", nil)
	}
	bt := &build.BuildTool{
		Name: in.Name, Runtime: in.Runtime, Tool: in.Tool,
		DefaultBuildCommand: in.DefaultBuildCommand, DefaultArtifactPath: in.DefaultArtifactPath,
		BuilderImage: in.BuilderImage, IsSystem: in.IsSystem, Description: in.Description,
	}
	bt.CreatedBy = in.CreatedBy
	bt.UpdatedBy = in.CreatedBy
	if err := s.repo.CreateBuildTool(ctx, bt); err != nil {
		return nil, apperr.Internal("create build tool", err)
	}
	return bt, nil
}

// UpdateBuildToolInput 更新构建工具输入。
type UpdateBuildToolInput struct {
	ID                int64
	Name              *string
	Runtime           *build.BaseImageRuntime
	Tool              *string
	DefaultBuildCommand *string
	DefaultArtifactPath *string
	BuilderImage      *string
	IsSystem          *bool
	Description       *string
	Version           int
	UpdatedBy         int64
}

// UpdateBuildTool 更新构建工具，乐观锁。
func (s *Service) UpdateBuildTool(ctx context.Context, in UpdateBuildToolInput) (*build.BuildTool, error) {
	bt, err := s.repo.GetBuildToolByID(ctx, in.ID)
	if err != nil {
		if errors.Is(err, build.ErrBuildToolNotFound) {
			return nil, apperr.NotFound("build tool", strconv.FormatInt(in.ID, 10))
		}
		return nil, apperr.Internal("get build tool", err)
	}
	if in.Name != nil {
		bt.Name = *in.Name
	}
	if in.Runtime != nil {
		bt.Runtime = *in.Runtime
	}
	if in.Tool != nil {
		bt.Tool = *in.Tool
	}
	if in.DefaultBuildCommand != nil {
		bt.DefaultBuildCommand = *in.DefaultBuildCommand
	}
	if in.DefaultArtifactPath != nil {
		bt.DefaultArtifactPath = *in.DefaultArtifactPath
	}
	if in.BuilderImage != nil {
		bt.BuilderImage = *in.BuilderImage
	}
	if in.IsSystem != nil {
		bt.IsSystem = *in.IsSystem
	}
	if in.Description != nil {
		bt.Description = *in.Description
	}
	bt.Version = in.Version
	bt.UpdatedBy = in.UpdatedBy
	if err := s.repo.UpdateBuildTool(ctx, bt); err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return nil, apperr.Conflict("build tool was modified concurrently, please refresh", err)
		}
		return nil, apperr.Internal("update build tool", err)
	}
	return bt, nil
}

// DeleteBuildTool 软删除构建工具。
func (s *Service) DeleteBuildTool(ctx context.Context, id, actorID int64) error {
	if err := s.repo.DeleteBuildTool(ctx, id, actorID); err != nil {
		if errors.Is(err, build.ErrBuildToolNotFound) {
			return apperr.NotFound("build tool", strconv.FormatInt(id, 10))
		}
		return apperr.Internal("delete build tool", err)
	}
	return nil
}

// CreateTemplateInput 创建模板输入。
type CreateTemplateInput struct {
	Scope             build.TemplateScope
	ScopeID           int64
	Name              string
	Description       string
	BuildStrategy     build.BuildStrategy
	BuildCommand      string
	BaseImageID       int64
	DockerfileSource  build.DockerfileSource
	DockerfileContent string
	ContextPath       string
	BuildArgs         map[string]string
	EnvVars           map[string]string
	IsDefault         bool
	CreatedBy         int64
}

// CreateTemplate 创建构建模板。
func (s *Service) CreateTemplate(ctx context.Context, in CreateTemplateInput) (*build.BuildTemplate, error) {
	if in.Name == "" {
		return nil, apperr.Validation("template name is required", nil)
	}
	if in.BaseImageID == 0 {
		return nil, apperr.Validation("base_image_id is required", nil)
	}
	if in.BuildStrategy == "" {
		in.BuildStrategy = build.BuildDockerBuild
	}
	if in.DockerfileSource == "" {
		in.DockerfileSource = build.DockerfileFromTemplate
	}
	if in.ContextPath == "" {
		in.ContextPath = "."
	}
	t := &build.BuildTemplate{
		Scope: in.Scope, ScopeID: in.ScopeID, Name: in.Name, Description: in.Description,
		BuildStrategy: in.BuildStrategy, BuildCommand: in.BuildCommand, BaseImageID: in.BaseImageID,
		DockerfileSource: in.DockerfileSource, DockerfileContent: in.DockerfileContent, ContextPath: in.ContextPath,
		BuildArgs: in.BuildArgs, EnvVars: in.EnvVars, IsDefault: in.IsDefault,
	}
	t.CreatedBy = in.CreatedBy
	t.UpdatedBy = in.CreatedBy
	if err := s.repo.CreateTemplate(ctx, t); err != nil {
		return nil, apperr.Internal("create template", err)
	}
	return t, nil
}

// ListTemplates 分页列出模板。
func (s *Service) ListTemplates(ctx context.Context, scope build.TemplateScope, scopeID int64, page, size int) ([]*build.BuildTemplate, int64, error) {
	items, total, err := s.repo.ListTemplates(ctx, scope, scopeID, (page-1)*size, size)
	if err != nil {
		return nil, 0, apperr.Internal("list templates", err)
	}
	return items, total, nil
}

// --- 触发构建 ---

// TriggerBuildInput 触发构建输入。
// 注意：Jenkins/Registry 由系统变量化配置强制使用默认实例，不再由调用方传入。
type TriggerBuildInput struct {
	ApplicationID     int64
	GitSourceID       int64
	RefType           build.RefType
	RefValue          string
	CommitSHA         string
	CommitMessage     string
	BuildTemplateID   int64
	BuildStrategy     build.BuildStrategy
	BuildCommand      string
	BuildTool         string // 构建工具标识（maven/gradle/npm/go...），配合 app.language 查 BuildTool 配置
	BuilderImage      string // 构建工具镜像（供 Jenkins docker run / Tekton build Task 使用）
	ContextPath       string
	ArtifactPath      string // template 模式：制品路径（COPY 进运行时镜像）
	DockerfilePath    string // repo 模式：仓库内 Dockerfile 相对路径
	BaseImageID       int64
	DockerfileSource  build.DockerfileSource
	DockerfileContent string
	BuildArgs         map[string]string
	TargetRepository  string
	TargetTag         string
	TriggeredBy       int64
	TriggerSource     build.TriggerSource
	IdempotencyKey    string
	Metadata          map[string]any
}

// ensureTargetTag 在 TargetTag 为空时生成默认 tag：<app_name>-<yyyyMMddHHmmss>[-<short_sha>]。
// 应用名做 sanitize（小写、非 [a-z0-9.-] 替换为 -），时间戳保证每次构建（含 rebuild）全局唯一且按时间排序，
// 短 SHA 便于追溯 commit。避免重建同一 commit 时复用旧 tag 导致覆盖或与历史版本混淆。
// TriggerBuild 与 RebuildBuild 两条路径都调用，确保 rebuild（清空 TargetTag）也能拿到新 tag。
func (s *Service) ensureTargetTag(ctx context.Context, in *TriggerBuildInput) {
	if in.TargetTag != "" {
		return
	}
	appName := ""
	if app, aerr := s.appRepo.GetApplicationByID(ctx, in.ApplicationID); aerr == nil && app != nil {
		appName = app.Name
	}
	sanitized := sanitizeImageTag(appName)
	if sanitized == "" {
		sanitized = fmt.Sprintf("app-%d", in.ApplicationID)
	}
	ts := time.Now().Format("20060102150405")
	if in.CommitSHA != "" {
		in.TargetTag = fmt.Sprintf("%s-%s-%s", sanitized, ts, in.CommitSHA[:min(8, len(in.CommitSHA))])
	} else {
		in.TargetTag = fmt.Sprintf("%s-%s", sanitized, ts)
	}
}

// TriggerBuild 触发构建：校验输入 → 幂等检查 → 写入 pending 构建 → 异步触发 Jenkins。
// 返回创建的构建任务。Jenkins 触发在后台进行，状态由 poller 推进。
func (s *Service) TriggerBuild(ctx context.Context, in TriggerBuildInput, jenkinsFactory JenkinsClientFactory) (*build.Build, error) {
	if in.ApplicationID == 0 {
		return nil, apperr.Validation("application_id is required", nil)
	}
	if in.GitSourceID == 0 {
		// 未指定 git_source_id：从 application.git_url 自动创建/复用默认 Git 源。
		gs, err := s.ensureDefaultGitSource(ctx, in.ApplicationID)
		if err != nil {
			return nil, err
		}
		in.GitSourceID = gs.ID
	}
	if in.RefType == "" {
		in.RefType = build.RefBranch
	}
	if in.RefValue == "" {
		return nil, apperr.Validation("ref_value is required", nil)
	}
	if in.TriggerSource == "" {
		in.TriggerSource = build.TriggerManual
	}

	// 幂等检查：同 idempotency_key 已存在则返回已有构建。
	if in.IdempotencyKey != "" {
		if existing, err := s.repo.GetBuildByIdempotencyKey(ctx, in.ApplicationID, in.IdempotencyKey); err == nil {
			return existing, nil
		} else if !errors.Is(err, build.ErrBuildNotFound) {
			return nil, apperr.Internal("check idempotency", err)
		}
	}

	// 校验 Git 源归属。
	gs, err := s.repo.GetGitSourceByID(ctx, in.GitSourceID)
	if err != nil {
		if errors.Is(err, build.ErrGitSourceNotFound) {
			return nil, apperr.NotFound("git source", strconv.FormatInt(in.GitSourceID, 10))
		}
		return nil, apperr.Internal("get git source", err)
	}
	if gs.ApplicationID != in.ApplicationID {
		return nil, apperr.BusinessRule("git source does not belong to application", nil)
	}

	// 若指定模板，加载模板填充默认值。
	if in.BuildTemplateID != 0 {
		tmpl, err := s.repo.GetTemplateByID(ctx, in.BuildTemplateID)
		if err != nil {
			if errors.Is(err, build.ErrTemplateNotFound) {
				return nil, apperr.NotFound("build template", strconv.FormatInt(in.BuildTemplateID, 10))
			}
			return nil, apperr.Internal("get template", err)
		}
		if in.BuildStrategy == "" {
			in.BuildStrategy = tmpl.BuildStrategy
		}
		if in.BuildCommand == "" {
			in.BuildCommand = tmpl.BuildCommand
		}
		if in.BaseImageID == 0 {
			in.BaseImageID = tmpl.BaseImageID
		}
		if in.DockerfileSource == "" {
			in.DockerfileSource = tmpl.DockerfileSource
		}
		if in.DockerfileContent == "" {
			in.DockerfileContent = tmpl.DockerfileContent
		}
		if in.ContextPath == "" {
			in.ContextPath = tmpl.ContextPath
		}
		if in.BuildArgs == nil {
			in.BuildArgs = tmpl.BuildArgs
		}
		_ = s.repo.IncrementUsage(ctx, in.BuildTemplateID)
	}

	// 加载基础镜像 + 构建工具，填充构建默认值并渲染单阶段运行时 Dockerfile。
	// 传统 CI 模式：构建在 Jenkins/Tekton 引擎侧用 builder_image 容器执行 BUILD_COMMAND 产出制品，
	// 运行时镜像（BaseImage）只 COPY 制品，不含构建工具。
	// - template 模式：从 BuildTool（runtime+tool）取 build_command/artifact_path/builder_image，
	//   渲染 BaseImage.dockerfile_template（单阶段）为完整 Dockerfile。
	// - repo 模式：用户仓库自带 Dockerfile，context_path 是 Dockerfile 相对路径，无需渲染。
	if in.BaseImageID != 0 {
		bi, err := s.repo.GetBaseImageByID(ctx, in.BaseImageID)
		if err != nil {
			if errors.Is(err, build.ErrBaseImageNotFound) {
				return nil, apperr.NotFound("base image", strconv.FormatInt(in.BaseImageID, 10))
			}
			return nil, apperr.Internal("get base image", err)
		}
		if in.BuildStrategy == "" {
			in.BuildStrategy = build.BuildDockerBuild
		}

		// 解析 app.language（存于 application.Metadata["language"]）以查 BuildTool 配置。
		runtime := bi.Runtime
		if app, aerr := s.appRepo.GetApplicationByID(ctx, in.ApplicationID); aerr == nil && app != nil {
			if lang, ok := app.Metadata["language"].(string); ok && lang != "" {
				if rt, ok := build.ParseBaseImageRuntime(lang); ok {
					runtime = rt
				}
			}
		}

		// template 模式：从 BuildTool 填充构建命令/制品路径/builder_image，渲染单阶段 Dockerfile。
		if in.DockerfileSource == "" || in.DockerfileSource == build.DockerfileFromTemplate {
			toolName := in.BuildTool
			if toolName == "" {
				toolName = "maven"
				if runtime == build.RuntimeGo {
					toolName = "go"
				} else if runtime == build.RuntimeNode {
					toolName = "npm"
				} else if runtime == build.RuntimePython {
					toolName = "pip"
				} else if runtime == build.RuntimeCustom {
					toolName = "custom"
				}
			}
			var bt *build.BuildTool
			if toolName != "custom" {
				bt, err = s.repo.GetBuildToolByRuntimeTool(ctx, runtime, toolName)
				if err != nil && !errors.Is(err, build.ErrBuildToolNotFound) {
					return nil, apperr.Internal("get build tool", err)
				}
			}
			if bt != nil {
				if in.BuildCommand == "" {
					in.BuildCommand = bt.DefaultBuildCommand
				}
				if in.ArtifactPath == "" {
					in.ArtifactPath = bt.DefaultArtifactPath
				}
				if in.BuilderImage == "" {
					in.BuilderImage = bt.BuilderImage
				}
			}
			// 渲染单阶段运行时 Dockerfile（仅 BaseImage + ArtifactPath + Entrypoint 占位符）。
			if in.DockerfileContent == "" && bi.DockerfileTemplate != "" {
				data, derr := dockerfileTemplateData(bi, in.ArtifactPath)
				if derr != nil {
					return nil, apperr.Validation("build dockerfile template data: "+derr.Error(), nil)
				}
				rendered, rerr := renderDockerfileTemplate(bi.DockerfileTemplate, data)
				if rerr != nil {
					return nil, apperr.Validation("render dockerfile template: "+rerr.Error(), nil)
				}
				in.DockerfileContent = rendered
				in.DockerfileSource = build.DockerfileFromTemplate
			}
		}
	}

	// repo 模式：context_path 是仓库内 Dockerfile 路径，同步到 dockerfile_path。
	if in.DockerfileSource == build.DockerfileFromRepo {
		if in.DockerfilePath == "" {
			in.DockerfilePath = in.ContextPath
			if in.DockerfilePath == "" {
				in.DockerfilePath = "Dockerfile"
			}
		}
	}

	// 默认 registry：强制使用系统默认实例（系统变量化）。
	// 优先取 vo_registries.is_default=true 的行；
	// 若无则回退到系统设置 platform.default_registry_id 指定的实例。
	reg, err := s.repo.GetDefaultRegistry(ctx)
	if err != nil || reg == nil {
		if s.systemSvc != nil {
			if rid, serr := s.systemSvc.GetDefaultRegistryID(ctx); serr == nil && rid > 0 {
				if rreg, rerr := s.repo.GetRegistryByID(ctx, rid); rerr == nil {
					reg = rreg
				}
			}
		}
		if reg == nil {
			return nil, apperr.BusinessRule("no default registry configured (请在「系统设置 > 镜像仓库集成」中配置并设为默认)", err)
		}
	}
	targetRegistryID := reg.ID
	if in.TargetRepository == "" {
		in.TargetRepository = fmt.Sprintf("app-%d", in.ApplicationID)
	}
	// 生成默认镜像 tag（rebuild 时 TargetTag 为空会在此重新生成）。
	s.ensureTargetTag(ctx, &in)

	// 默认 Jenkins 实例：强制使用系统默认实例（系统变量化）。
	// 优先取 vo_jenkins_instances.is_default=true 的行；
	// 若无则回退到系统设置 platform.default_jenkins_id 指定的实例。
	jk, err := s.repo.GetDefaultJenkins(ctx)
	if err != nil || jk == nil {
		if s.systemSvc != nil {
			if jid, serr := s.systemSvc.GetDefaultJenkinsID(ctx); serr == nil && jid > 0 {
				if jjk, jerr := s.repo.GetJenkinsByID(ctx, jid); jerr == nil {
					jk = jjk
				}
			}
		}
		if jk == nil {
			return nil, apperr.BusinessRule("no default jenkins configured (请在「系统设置 > Jenkins 集成」中配置并设为默认)", err)
		}
	}
	jenkinsInstanceID := jk.ID
	jenkinsJobName := fmt.Sprintf("vortexops/app-%d", in.ApplicationID)

	buildNumber, err := s.repo.NextBuildNumber(ctx, in.ApplicationID)
	if err != nil {
		return nil, apperr.Internal("allocate build number", err)
	}

	b := &build.Build{
		ApplicationID: in.ApplicationID, BuildNumber: buildNumber, GitSourceID: in.GitSourceID,
		RefType: in.RefType, RefValue: in.RefValue, CommitSHA: in.CommitSHA, CommitMessage: in.CommitMessage,
		BuildTemplateID: in.BuildTemplateID, BuildStrategy: in.BuildStrategy, BuildCommand: in.BuildCommand,
		ContextPath: in.ContextPath, ArtifactPath: in.ArtifactPath, DockerfilePath: in.DockerfilePath,
		BaseImageID: in.BaseImageID, BuildTool: in.BuildTool, BuilderImage: in.BuilderImage,
		DockerfileSource: in.DockerfileSource,
		DockerfileContent: in.DockerfileContent, BuildArgs: in.BuildArgs,
		TargetRegistryID: targetRegistryID, TargetRepository: in.TargetRepository, TargetTag: in.TargetTag,
		JenkinsInstanceID: jenkinsInstanceID, JenkinsJobName: jenkinsJobName,
		Status: build.BuildPending, TriggeredBy: in.TriggeredBy, TriggerSource: in.TriggerSource,
		IdempotencyKey: in.IdempotencyKey, Metadata: in.Metadata,
	}
	b.CreatedBy = in.TriggeredBy
	b.UpdatedBy = in.TriggeredBy

	if err := s.repo.CreateBuild(ctx, b); err != nil {
		if errors.Is(err, build.ErrIdempotencyConflict) {
			return nil, apperr.Conflict("build with same idempotency key already exists", err)
		}
		return nil, apperr.Internal("create build", err)
	}

	// 异步触发构建引擎，不阻塞 API 响应。
	go s.triggerBuildEngineAsync(context.Background(), b.ID, in, targetRegistryID, reg, jenkinsInstanceID, jenkinsJobName, jenkinsFactory)
	return b, nil
}

// triggerBuildEngineAsync 后台触发构建引擎（Jenkins 或 Tekton，按系统设置 platform.build_engine 切换）并更新构建状态。
// registryURL 与 registryRepository 用于构建参数（IMAGE_REGISTRY / IMAGE_REPO），使构建直接 push 到目标镜像仓库。
func (s *Service) triggerBuildEngineAsync(ctx context.Context, buildID int64, in TriggerBuildInput,
	registryID int64, reg *build.Registry, jenkinsInstanceID int64, jenkinsJobName string,
	factory JenkinsClientFactory) {

	// 兜底 panic：异步 goroutine 无 recover 会导致静默退出，构建永久卡在 pending。
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[buildapp] triggerBuildEngineAsync build %d panicked: %v", buildID, r)
			s.markFailed(ctx, buildID, fmt.Sprintf("trigger panicked: %v", r))
		}
	}()
	log.Printf("[buildapp] triggerBuildEngineAsync start build %d job=%s", buildID, jenkinsJobName)

	// 修复 REPO_URL：从 GitSource 填充真实仓库地址（此前为空）。
	repoURL := ""
	if gs, err := s.repo.GetGitSourceByID(ctx, in.GitSourceID); err == nil {
		repoURL = gs.RepoURL
	}
	registryURL := ""
	if reg != nil {
		registryURL = reg.URL
	}
	params := map[string]string{
		"APPLICATION_ID":   strconv.FormatInt(in.ApplicationID, 10),
		"BUILD_ID":         strconv.FormatInt(buildID, 10),
		"REPO_URL":         repoURL,
		"REF_TYPE":         string(in.RefType),
		"REF_VALUE":        in.RefValue,
		"COMMIT_SHA":       in.CommitSHA,
		"IMAGE_REGISTRY":   registryURL,
		"IMAGE_REPO":       in.TargetRepository,
		"IMAGE_TAG":        in.TargetTag,
		"BUILD_STRATEGY":   string(in.BuildStrategy),
		"BUILD_COMMAND":    in.BuildCommand,
		"BUILD_TOOL":       in.BuildTool,
		"BUILDER_IMAGE":    in.BuilderImage,
		"CONTEXT_PATH":     in.ContextPath,
		"ARTIFACT_PATH":    in.ArtifactPath,
		"DOCKERFILE_PATH":  in.DockerfilePath,
		"DOCKERFILE":       in.DockerfileContent,
		"BUILD_ARGS_JSON":  jsonBuildArgs(in.BuildArgs),
		"JENKINS_JOB_NAME": jenkinsJobName,
	}

	// 引擎选择：优先用 engineFact（支持 tekton），回退到 jenkinsFactory。
	if s.engineFact != nil && s.systemSvc != nil {
		engine, err := s.systemSvc.GetBuildEngine(ctx)
		if err == nil && engine == "tekton" && s.engineFact.Tekton != nil {
			s.triggerTektonAsync(ctx, buildID, params)
			return
		}
	}

	// Jenkins 路径。
	if factory == nil {
		return
	}
	jk, err := s.repo.GetJenkinsByID(ctx, jenkinsInstanceID)
	if err != nil {
		s.markFailed(ctx, buildID, "get jenkins instance: "+err.Error())
		return
	}
	client, err := factory(ctx, jk)
	if err != nil {
		s.markFailed(ctx, buildID, "build jenkins client: "+err.Error())
		return
	}
	// 确保 Jenkins job 存在：首次构建时自动创建参数化 Pipeline job（含 folder 兜底）。
	// job 不存在直接 TriggerBuild 会 404 失败，构建卡死；EnsureJob 幂等，已存在则快速短路。
	if err := client.EnsureJob(ctx, jenkinsJobName, pipelineJobConfigXML); err != nil {
		log.Printf("[buildapp] build %d ensure jenkins job failed: %v", buildID, err)
		s.markFailed(ctx, buildID, "ensure jenkins job: "+err.Error())
		return
	}
	log.Printf("[buildapp] build %d ensure jenkins job ok, triggering", buildID)
	queueID, err := client.TriggerBuild(ctx, jenkinsJobName, params)
	if err != nil {
		log.Printf("[buildapp] build %d trigger jenkins failed: %v", buildID, err)
		s.markFailed(ctx, buildID, "trigger jenkins: "+err.Error())
		return
	}
	log.Printf("[buildapp] build %d trigger jenkins ok queueID=%s", buildID, queueID)
	_ = s.repo.SetJenkinsInfo(ctx, buildID, queueID, 0, jenkinsJobName)
}

// triggerTektonAsync 后台触发 Tekton PipelineRun 并记录 pipeline_run_name。
func (s *Service) triggerTektonAsync(ctx context.Context, buildID int64, params map[string]string) {
	client, err := s.engineFact.Tekton(ctx)
	if err != nil {
		s.markFailed(ctx, buildID, "build tekton client: "+err.Error())
		return
	}
	runName, err := client.Trigger(ctx, buildID, params)
	if err != nil {
		s.markFailed(ctx, buildID, "trigger tekton: "+err.Error())
		return
	}
	_ = s.repo.SetPipelineRunName(ctx, buildID, runName)
}

func (s *Service) markFailed(ctx context.Context, buildID int64, reason string) {
	b, err := s.repo.GetBuildByID(ctx, buildID)
	if err != nil {
		log.Printf("[buildapp] markFailed build %d get failed: %v", buildID, err)
		return
	}
	// 截断 reason 避免 failure_reason 列长度溢出导致 CompleteBuild 静默失败。
	safeReason := truncate(reason, 480)
	_, err = s.repo.CompleteBuild(ctx, buildID, build.BuildFailed, 0, 0, "", safeReason,
		safeReason, time.Now(), b.Version)
	if err != nil {
		log.Printf("[buildapp] markFailed build %d complete failed (version=%d): %v", buildID, b.Version, err)
	}
}

// --- 构建查询 ---

// GetBuild 按 ID 查询构建。
func (s *Service) GetBuild(ctx context.Context, id int64) (*build.Build, error) {
	b, err := s.repo.GetBuildByID(ctx, id)
	if err != nil {
		if errors.Is(err, build.ErrBuildNotFound) {
			return nil, apperr.NotFound("build", strconv.FormatInt(id, 10))
		}
		return nil, apperr.Internal("get build", err)
	}
	return b, nil
}

// WaitBuildTerminal 轮询构建状态直到终态（success/failed/canceled/timeout），供 BuildExecutor wait=true 使用。
func (s *Service) WaitBuildTerminal(ctx context.Context, buildID int64) (*build.Build, error) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		b, err := s.repo.GetBuildByID(ctx, buildID)
		if err != nil {
			return nil, apperr.Internal("wait build terminal: get build", err)
		}
		if b.Status.IsTerminal() {
			return b, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

// ListBuilds 分页查询构建。
func (s *Service) ListBuilds(ctx context.Context, appID int64, status build.BuildStatus, triggeredBy int64, page, size int) ([]*build.Build, int64, error) {
	items, total, err := s.repo.ListBuilds(ctx, build.BuildQuery{
		ApplicationID: appID, Status: status, TriggeredBy: triggeredBy,
		Offset: (page - 1) * size, Limit: size,
	})
	if err != nil {
		return nil, 0, apperr.Internal("list builds", err)
	}
	return items, total, nil
}

// ListSteps 列出构建步骤。
func (s *Service) ListSteps(ctx context.Context, buildID int64) ([]*build.BuildStep, error) {
	items, err := s.repo.ListSteps(ctx, buildID)
	if err != nil {
		return nil, apperr.Internal("list build steps", err)
	}
	return items, nil
}

// CancelBuild 取消构建（仅 pending/queued/running 可取消）。
func (s *Service) CancelBuild(ctx context.Context, id int64, jenkinsFactory JenkinsClientFactory) (*build.Build, error) {
	b, err := s.repo.GetBuildByID(ctx, id)
	if err != nil {
		if errors.Is(err, build.ErrBuildNotFound) {
			return nil, apperr.NotFound("build", strconv.FormatInt(id, 10))
		}
		return nil, apperr.Internal("get build", err)
	}
	if b.Status != build.BuildPending && b.Status != build.BuildQueued && b.Status != build.BuildRunning {
		return nil, apperr.BusinessRule("build cannot be cancelled in current state", build.ErrBuildNotCancellable)
	}
	// 若已触发 Jenkins，调用 stop。
	if jenkinsFactory != nil && b.JenkinsInstanceID != 0 && b.JenkinsBuildNumber != 0 {
		jk, jerr := s.repo.GetJenkinsByID(ctx, b.JenkinsInstanceID)
		if jerr == nil {
			if client, cerr := jenkinsFactory(ctx, jk); cerr == nil {
				_ = client.StopBuild(ctx, b.JenkinsJobName, b.JenkinsBuildNumber)
			}
		}
	}
	updated, err := s.repo.CompleteBuild(ctx, id, build.BuildCanceled, 0, 0, "", "", "cancelled by user", time.Now(), b.Version)
	if err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return nil, apperr.Conflict("build was modified concurrently, please refresh", err)
		}
		return nil, apperr.Internal("cancel build", err)
	}
	return updated, nil
}

// RebuildBuild 在原构建记录上重新拉取代码并构建（不生成新记录）。
// 仅终态构建可重新构建；运行中的构建请先取消。
// 流程：加载原构建 → 重新拉取分支最新 commit → 重置记录为 pending → 复用原 Jenkins/Registry 异步触发引擎。
func (s *Service) RebuildBuild(ctx context.Context, id int64, jenkinsFactory JenkinsClientFactory) (*build.Build, error) {
	b, err := s.repo.GetBuildByID(ctx, id)
	if err != nil {
		if errors.Is(err, build.ErrBuildNotFound) {
			return nil, apperr.NotFound("build", strconv.FormatInt(id, 10))
		}
		return nil, apperr.Internal("get build", err)
	}
	if !b.Status.IsTerminal() {
		return nil, apperr.BusinessRule("build cannot be rebuilt while running or pending, cancel it first", nil)
	}
	if b.RefValue == "" {
		return nil, apperr.BusinessRule("build has no branch/ref to rebuild from", nil)
	}

	// 重新拉取代码：获取分支最新 commit。
	commit, err := s.GetGitCommit(ctx, b.ApplicationID, b.RefValue)
	if err != nil {
		return nil, err
	}

	// 重置原记录为 pending（同一条记录）。commit message 由引擎拉码时回写，这里保留原值。
	reset, err := s.repo.ResetBuildForRebuild(ctx, id, commit.SHA, b.CommitMessage, b.Version)
	if err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return nil, apperr.Conflict("build was modified concurrently, please refresh", err)
		}
		return nil, apperr.Internal("reset build for rebuild", err)
	}

	// 复用原 Jenkins/Registry 异步触发引擎。
	var reg *build.Registry
	if b.TargetRegistryID != 0 {
		if r, rerr := s.repo.GetRegistryByID(ctx, b.TargetRegistryID); rerr == nil {
			reg = r
		}
	}
	in := TriggerBuildInput{
		ApplicationID:     b.ApplicationID,
		GitSourceID:       b.GitSourceID,
		RefType:           b.RefType,
		RefValue:          b.RefValue,
		CommitSHA:         commit.SHA,
		CommitMessage:     b.CommitMessage,
		BuildStrategy:     b.BuildStrategy,
		BuildCommand:      b.BuildCommand,
		ContextPath:       b.ContextPath,
		ArtifactPath:      b.ArtifactPath,
		DockerfilePath:    b.DockerfilePath,
		BaseImageID:       b.BaseImageID,
		BuildTool:         b.BuildTool,
		BuilderImage:      b.BuilderImage,
		DockerfileSource:  b.DockerfileSource,
		DockerfileContent: b.DockerfileContent,
		BuildArgs:         b.BuildArgs,
		TargetRepository:  b.TargetRepository,
		// rebuild 时清空 TargetTag，让 TriggerBuild 重新生成带新时间戳的唯一 tag，
		// 避免复用旧 tag 造成镜像覆盖或与历史版本混淆。
		TargetTag:         "",
		TriggeredBy:       b.TriggeredBy,
		TriggerSource:     build.TriggerManual,
		Metadata:          b.Metadata,
	}

	// 重新解析 BuilderImage 并重渲染 Dockerfile：用户可能编辑了 build_tool/base_image/artifact_path，
	// 若沿用 b.BuilderImage/b.DockerfileContent 旧值会导致编辑不生效（Jenkins Build 阶段被跳过等）。
	if b.BaseImageID != 0 {
		if bi, biErr := s.repo.GetBaseImageByID(ctx, b.BaseImageID); biErr == nil && bi != nil {
			// 解析 runtime（优先 app.language，回退 BaseImage.Runtime）。
			runtime := bi.Runtime
			if app, aerr := s.appRepo.GetApplicationByID(ctx, b.ApplicationID); aerr == nil && app != nil {
				if lang, ok := app.Metadata["language"].(string); ok && lang != "" {
					if rt, ok := build.ParseBaseImageRuntime(lang); ok {
						runtime = rt
					}
				}
			}
			toolName := in.BuildTool
			if toolName == "" {
				toolName = "maven"
				if runtime == build.RuntimeGo {
					toolName = "go"
				} else if runtime == build.RuntimeNode {
					toolName = "npm"
				} else if runtime == build.RuntimePython {
					toolName = "pip"
				} else if runtime == build.RuntimeCustom {
					toolName = "custom"
				}
			}
			if toolName != "custom" {
				if bt, btErr := s.repo.GetBuildToolByRuntimeTool(ctx, runtime, toolName); btErr == nil && bt != nil {
					if in.BuilderImage == "" {
						in.BuilderImage = bt.BuilderImage
					}
				}
			}
			// template 模式：用当前 BaseImage + ArtifactPath + Entrypoint 重渲染 Dockerfile，使编辑生效。
			if in.DockerfileSource == "" || in.DockerfileSource == build.DockerfileFromTemplate {
				if bi.DockerfileTemplate != "" {
					data, derr := dockerfileTemplateData(bi, in.ArtifactPath)
					if derr == nil {
						if rendered, rerr := renderDockerfileTemplate(bi.DockerfileTemplate, data); rerr == nil {
							in.DockerfileContent = rendered
						}
					}
				}
			}
		}
	}
	// repo 模式：补 DockerfilePath。
	if in.DockerfileSource == build.DockerfileFromRepo {
		if in.DockerfilePath == "" {
			in.DockerfilePath = in.ContextPath
			if in.DockerfilePath == "" {
				in.DockerfilePath = "Dockerfile"
			}
		}
	}

	// rebuild 时 in.TargetTag 被置空（见上），这里重新生成带新时间戳的唯一 tag，
	// 否则空 tag 会传给 Jenkins 导致 docker build -t 报 invalid reference format。
	// 生成后持久化到 DB，使构建详情/发布流程拿到与实际推送一致的镜像引用。
	s.ensureTargetTag(context.Background(), &in)
	if err := s.repo.SetBuildTargetTag(ctx, id, in.TargetTag); err != nil {
		log.Printf("[buildapp] rebuild %d set target tag failed: %v", id, err)
	}

	go s.triggerBuildEngineAsync(context.Background(), id, in, b.TargetRegistryID, reg, b.JenkinsInstanceID, b.JenkinsJobName, jenkinsFactory)
	return reset, nil
}

// UpdateBuildInput 更新构建可编辑元信息入参。
type UpdateBuildInput struct {
	ID               int64
	CommitMessage    *string
	TargetTag        *string
	Metadata         map[string]any
	// 全量可编辑字段（与新建构建对齐，nil 表示不变更）。
	RefType          *build.RefType
	RefValue         *string
	GitSourceID      *int64
	BuildCommand     *string
	BuildTool        *string
	BuilderImage     *string
	ContextPath      *string
	ArtifactPath     *string
	DockerfilePath   *string
	BaseImageID      *int64
	DockerfileSource *build.DockerfileSource
	DockerfileContent *string
	BuildArgs        map[string]string
	TargetRepository *string
	Version          int
	ActorID          int64
}

// UpdateBuild 更新构建可编辑信息（全量字段，与新建构建对齐）。
// 仅终态构建可编辑；运行中或排队中的构建不可改。
func (s *Service) UpdateBuild(ctx context.Context, in UpdateBuildInput) (*build.Build, error) {
	b, err := s.repo.GetBuildByID(ctx, in.ID)
	if err != nil {
		if errors.Is(err, build.ErrBuildNotFound) {
			return nil, apperr.NotFound("build", strconv.FormatInt(in.ID, 10))
		}
		return nil, apperr.Internal("get build", err)
	}
	if !b.Status.IsTerminal() {
		return nil, apperr.BusinessRule("build cannot be edited while running or pending", nil)
	}
	if in.CommitMessage != nil {
		b.CommitMessage = *in.CommitMessage
	}
	if in.TargetTag != nil {
		b.TargetTag = *in.TargetTag
	}
	if in.Metadata != nil {
		b.Metadata = in.Metadata
	}
	if in.RefType != nil {
		b.RefType = *in.RefType
	}
	if in.RefValue != nil {
		b.RefValue = *in.RefValue
	}
	if in.GitSourceID != nil {
		b.GitSourceID = *in.GitSourceID
	}
	if in.BuildCommand != nil {
		b.BuildCommand = *in.BuildCommand
	}
	if in.ContextPath != nil {
		b.ContextPath = *in.ContextPath
	}
	if in.ArtifactPath != nil {
		b.ArtifactPath = *in.ArtifactPath
	}
	if in.DockerfilePath != nil {
		b.DockerfilePath = *in.DockerfilePath
	}
	if in.BaseImageID != nil {
		b.BaseImageID = *in.BaseImageID
	}
	if in.BuildTool != nil {
		b.BuildTool = *in.BuildTool
	}
	if in.BuilderImage != nil {
		b.BuilderImage = *in.BuilderImage
	}
	if in.DockerfileSource != nil {
		b.DockerfileSource = *in.DockerfileSource
	}
	if in.DockerfileContent != nil {
		b.DockerfileContent = *in.DockerfileContent
	}
	if in.BuildArgs != nil {
		b.BuildArgs = in.BuildArgs
	}
	if in.TargetRepository != nil {
		b.TargetRepository = *in.TargetRepository
	}
	// 若 BuildTool/BaseImageID/ArtifactPath 任一变更且为 template 模式，重渲染 Dockerfile 使编辑生效。
	// 用户未显式传 DockerfileContent 时才重渲染（避免覆盖用户手填的 Dockerfile）。
	needRender := (in.BuildTool != nil || in.BaseImageID != nil || in.ArtifactPath != nil) &&
		in.DockerfileContent == nil &&
		(b.DockerfileSource == "" || b.DockerfileSource == build.DockerfileFromTemplate) &&
		b.BaseImageID != 0
	if needRender {
		if bi, biErr := s.repo.GetBaseImageByID(ctx, b.BaseImageID); biErr == nil && bi != nil && bi.DockerfileTemplate != "" {
			// BuildTool 变更时，从 BuildTool 配置补 BuilderImage（若用户未显式传 builder_image）。
			if in.BuildTool != nil && b.BuilderImage == "" {
				runtime := bi.Runtime
				if app, aerr := s.appRepo.GetApplicationByID(ctx, b.ApplicationID); aerr == nil && app != nil {
					if lang, ok := app.Metadata["language"].(string); ok && lang != "" {
						if rt, ok := build.ParseBaseImageRuntime(lang); ok {
							runtime = rt
						}
					}
				}
				toolName := b.BuildTool
				if toolName != "" && toolName != "custom" {
					if bt, btErr := s.repo.GetBuildToolByRuntimeTool(ctx, runtime, toolName); btErr == nil && bt != nil {
						b.BuilderImage = bt.BuilderImage
					}
				}
			}
		if rendered, rerr := func() (string, error) {
			data, derr := dockerfileTemplateData(bi, b.ArtifactPath)
			if derr != nil {
				return "", derr
			}
			return renderDockerfileTemplate(bi.DockerfileTemplate, data)
		}(); rerr == nil {
			b.DockerfileContent = rendered
		}
		}
	}
	b.Version = in.Version
	b.UpdatedBy = in.ActorID
	updated, err := s.repo.UpdateBuild(ctx, b)
	if err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return nil, apperr.Conflict("build was modified concurrently, please refresh", err)
		}
		return nil, apperr.Internal("update build", err)
	}
	return updated, nil
}

// DeleteBuild 软删除构建。仅终态构建可删；运行中的构建请先取消。
func (s *Service) DeleteBuild(ctx context.Context, id, actorID int64) error {
	b, err := s.repo.GetBuildByID(ctx, id)
	if err != nil {
		if errors.Is(err, build.ErrBuildNotFound) {
			return apperr.NotFound("build", strconv.FormatInt(id, 10))
		}
		return apperr.Internal("get build", err)
	}
	if !b.Status.IsTerminal() {
		return apperr.BusinessRule("build cannot be deleted while running or pending, cancel it first", nil)
	}
	if err := s.repo.DeleteBuild(ctx, id, actorID); err != nil {
		if errors.Is(err, build.ErrBuildNotFound) {
			return apperr.NotFound("build", strconv.FormatInt(id, 10))
		}
		return apperr.Internal("delete build", err)
	}
	return nil
}

// --- 日志 ---

// GetBuildLogs 获取构建日志。进行中从 Jenkins 流式拉取；完成后从 S3 归档读取。
// 返回日志内容与来源标识（"jenkins" 或 "archive"）。
func (s *Service) GetBuildLogs(ctx context.Context, id int64, jenkinsFactory JenkinsClientFactory, offset int64) (logs []byte, source string, hasMore bool, err error) {
	b, gerr := s.repo.GetBuildByID(ctx, id)
	if gerr != nil {
		if errors.Is(gerr, build.ErrBuildNotFound) {
			return nil, "", false, apperr.NotFound("build", strconv.FormatInt(id, 10))
		}
		return nil, "", false, apperr.Internal("get build", gerr)
	}
	// 已完成且有归档键：从 S3 读。
	if b.Status.IsTerminal() && b.LogStorageKey != "" && s.logStore != nil {
		data, derr := s.logStore.DownloadRange(ctx, b.LogStorageKey, offset, 0)
		if derr != nil {
			return nil, "", false, apperr.Internal("download archived logs", derr)
		}
		return data, "archive", false, nil
	}
	// Tekton 模式：通过 PipelineRun 聚合 TaskRun Pod 日志。
	if b.PipelineRunName != "" {
		if s.engineFact == nil || s.engineFact.Tekton == nil {
			return nil, "", false, apperr.BusinessRule("tekton engine not configured for logs", nil)
		}
		client, cerr := s.engineFact.Tekton(ctx)
		if cerr != nil {
			return nil, "", false, apperr.Internal("build tekton client", cerr)
		}
		text, more, lerr := client.GetLog(ctx, b.PipelineRunName, offset)
		if lerr != nil {
			return nil, "", false, apperr.Internal("fetch tekton logs", lerr)
		}
		return []byte(text), "tekton", more, nil
	}
	// Jenkins 模式：从 Jenkins console 拉。
	if jenkinsFactory == nil || b.JenkinsInstanceID == 0 {
		return nil, "", false, apperr.BusinessRule("logs unavailable: build is running but no jenkins configured", nil)
	}
	jk, jerr := s.repo.GetJenkinsByID(ctx, b.JenkinsInstanceID)
	if jerr != nil {
		return nil, "", false, apperr.Internal("get jenkins instance", jerr)
	}
	client, cerr := jenkinsFactory(ctx, jk)
	if cerr != nil {
		return nil, "", false, apperr.Internal("build jenkins client", cerr)
	}
	if b.JenkinsBuildNumber == 0 {
		// 构建已触发但 Jenkins 尚未分配构建号（仍在队列中）。
		// 返回空内容 + "queued" 来源，让前端轮询重试而非抛 422。
		return nil, "queued", false, nil
	}
	text, more, lerr := client.GetConsoleLog(ctx, b.JenkinsJobName, b.JenkinsBuildNumber, offset)
	if lerr != nil {
		return nil, "", false, apperr.Internal("fetch jenkins logs", lerr)
	}
	return []byte(text), "jenkins", more, nil
}

// StreamBuildLogs 流式拉取构建日志（供 SSE/Chunked handler 调用）。
// onChunk 回调在每个增量片段到达时被调用；返回错误时停止。
// source 标识片段来源（archive/jenkins/tekton），供调用方决定事件类型。
func (s *Service) StreamBuildLogs(ctx context.Context, id int64, jenkinsFactory JenkinsClientFactory, onChunk func(chunk []byte, source string) error) error {
	var offset int64
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			chunk, source, hasMore, err := s.GetBuildLogs(ctx, id, jenkinsFactory, offset)
			if err != nil {
				return err
			}
			if len(chunk) > 0 {
				if err := onChunk(chunk, source); err != nil {
					return err
				}
				offset += int64(len(chunk))
			}
			// 归档来源一次性返回完成；Jenkins/Tekton 来源按 hasMore 轮询。
			if source == "archive" {
				return nil
			}
			b, _ := s.repo.GetBuildByID(ctx, id)
			if b != nil && b.Status.IsTerminal() && b.LogStorageKey != "" {
				// 构建已完成且有归档：下次迭代切到归档读取剩余。
				continue
			}
			_ = hasMore
		}
	}
}

// --- 制品 ---

// ListImages 分页列出镜像。
func (s *Service) ListImages(ctx context.Context, appID int64, page, size int) ([]*build.Image, int64, error) {
	items, total, err := s.repo.ListImages(ctx, appID, (page-1)*size, size)
	if err != nil {
		return nil, 0, apperr.Internal("list images", err)
	}
	return items, total, nil
}

// GetImage 按 ID 查询镜像。
func (s *Service) GetImage(ctx context.Context, id int64) (*build.Image, error) {
	img, err := s.repo.GetImageByID(ctx, id)
	if err != nil {
		if errors.Is(err, build.ErrImageNotFound) {
			return nil, apperr.NotFound("image", strconv.FormatInt(id, 10))
		}
		return nil, apperr.Internal("get image", err)
	}
	return img, nil
}

// RetireImage 标记镜像退役。
func (s *Service) RetireImage(ctx context.Context, id int64) error {
	if err := s.repo.RetireImage(ctx, id); err != nil {
		if errors.Is(err, build.ErrImageNotFound) {
			return apperr.NotFound("image", strconv.FormatInt(id, 10))
		}
		return apperr.Internal("retire image", err)
	}
	return nil
}

// RegisterExternalImageInput 注册外部镜像（非平台构建）输入。
type RegisterExternalImageInput struct {
	ApplicationID int64
	RegistryID    int64
	FullReference string
	Repository    string
	Tag           string
	Digest        string
	VersionLabel  string
	Labels        map[string]string
	ActorID       int64
}

// RegisterExternalImage 将外部镜像引用登记为平台制品（Source=manual），供发布流程使用。
func (s *Service) RegisterExternalImage(ctx context.Context, in RegisterExternalImageInput) (*build.Image, error) {
	if in.ApplicationID == 0 {
		return nil, apperr.Validation("application_id is required", nil)
	}
	if in.RegistryID == 0 {
		return nil, apperr.Validation("registry_id is required", nil)
	}
	fullRef := strings.TrimSpace(in.FullReference)
	if fullRef == "" {
		return nil, apperr.Validation("full_reference is required", nil)
	}
	repo, tag := in.Repository, in.Tag
	if repo == "" || tag == "" {
		parsedRepo, parsedTag, err := parseImageReference(fullRef)
		if err != nil {
			return nil, apperr.Validation("invalid image reference", err)
		}
		if repo == "" {
			repo = parsedRepo
		}
		if tag == "" {
			tag = parsedTag
		}
	}
	version, err := s.repo.NextImageVersion(ctx, in.ApplicationID)
	if err != nil {
		return nil, apperr.Internal("next image version", err)
	}
	versionLabel := in.VersionLabel
	if versionLabel == "" {
		versionLabel = tag
	}
	labels := in.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	img := &build.Image{
		UUID:          uuid.New(),
		ApplicationID: in.ApplicationID,
		RegistryID:    in.RegistryID,
		Repository:    repo,
		Tag:           tag,
		Digest:        in.Digest,
		FullReference: fullRef,
		VersionNumber: version,
		VersionLabel:  versionLabel,
		Source:        build.ImgSourceManual,
		BuildID:       0,
		ScanStatus:    build.ImgScanPending,
		Status:        build.ImgStatusAvailable,
		Labels:        labels,
	}
	img.CreatedBy = in.ActorID
	img.UpdatedBy = in.ActorID
	if err := s.repo.CreateImage(ctx, img); err != nil {
		return nil, apperr.Internal("create external image", err)
	}
	return img, nil
}

// parseImageReference 从完整镜像引用解析 repository 与 tag。
func parseImageReference(fullRef string) (repository, tag string, err error) {
	fullRef = strings.TrimSpace(fullRef)
	if fullRef == "" {
		return "", "", fmt.Errorf("empty reference")
	}
	if at := strings.Index(fullRef, "@"); at > 0 {
		fullRef = fullRef[:at]
	}
	slash := strings.LastIndex(fullRef, "/")
	colon := strings.LastIndex(fullRef, ":")
	if colon > slash && colon > 0 {
		return fullRef[:colon], fullRef[colon+1:], nil
	}
	return fullRef, "latest", nil
}

// --- 制品别名 ---

// CreateImageTagInput 创建别名输入。
type CreateImageTagInput struct {
	ApplicationID int64
	Name          string
	ImageID       int64
	Description   string
	CreatedBy     int64
}

// CreateImageTag 创建制品别名（如 stable、latest-v1）。
func (s *Service) CreateImageTag(ctx context.Context, in CreateImageTagInput) (*build.ImageVersionTag, error) {
	if in.Name == "" {
		return nil, apperr.Validation("tag name is required", nil)
	}
	if in.ImageID == 0 {
		return nil, apperr.Validation("image_id is required", nil)
	}
	// 校验名称唯一。
	if existing, err := s.repo.GetImageTagByName(ctx, in.ApplicationID, in.Name); err == nil && existing != nil {
		return nil, apperr.Conflict("image tag already exists", build.ErrImageTagExists)
	} else if err != nil && !errors.Is(err, build.ErrImageTagNotFound) {
		return nil, apperr.Internal("check image tag", err)
	}
	t := &build.ImageVersionTag{
		ApplicationID: in.ApplicationID, Name: in.Name, ImageID: in.ImageID, Description: in.Description,
	}
	t.CreatedBy = in.CreatedBy
	t.UpdatedBy = in.CreatedBy
	if err := s.repo.CreateImageTag(ctx, t); err != nil {
		if errors.Is(err, build.ErrImageTagExists) {
			return nil, apperr.Conflict("image tag already exists", err)
		}
		return nil, apperr.Internal("create image tag", err)
	}
	return t, nil
}

// ListImageTags 列出应用的别名。
func (s *Service) ListImageTags(ctx context.Context, appID int64) ([]*build.ImageVersionTag, error) {
	items, err := s.repo.ListImageTags(ctx, appID)
	if err != nil {
		return nil, apperr.Internal("list image tags", err)
	}
	return items, nil
}

// UpdateImageTag 更新别名指向（移动 stable 到新镜像）。
func (s *Service) UpdateImageTag(ctx context.Context, tagID, imageID int64, description string, actorID int64) error {
	tags, err := s.repo.ListImageTags(ctx, 0)
	if err != nil {
		return apperr.Internal("list image tags", err)
	}
	var target *build.ImageVersionTag
	for _, t := range tags {
		if t.ID == tagID {
			target = t
			break
		}
	}
	if target == nil {
		return apperr.NotFound("image tag", strconv.FormatInt(tagID, 10))
	}
	target.ImageID = imageID
	target.Description = description
	target.UpdatedBy = actorID
	if err := s.repo.UpdateImageTag(ctx, target); err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return apperr.Conflict("image tag was modified concurrently, please refresh", err)
		}
		return apperr.Internal("update image tag", err)
	}
	return nil
}

// DeleteImageTag 软删除别名。
func (s *Service) DeleteImageTag(ctx context.Context, id, actorID int64) error {
	if err := s.repo.DeleteImageTag(ctx, id, actorID); err != nil {
		if errors.Is(err, build.ErrImageTagNotFound) {
			return apperr.NotFound("image tag", strconv.FormatInt(id, 10))
		}
		return apperr.Internal("delete image tag", err)
	}
	return nil
}

// --- 校验 ---

func validateGitSourceName(name string) error {
	name = strings.TrimSpace(name)
	if len(name) < 2 || len(name) > 64 {
		return apperr.Validation("git source name must be 2-64 characters", nil)
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// 以下类型/函数引用 buildlog 包以避免未使用 import（实际在 HTTP 层 SSE 使用）。
var _ = buildlog.StreamOpts{}

// _ uuid 引用占位（CreateImageTag 等场景未直接用，但保留供未来扩展）。
var _ uuid.UUID
