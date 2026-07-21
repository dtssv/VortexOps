// Package build 是构建与镜像领域的核心实体与仓储接口。
// 覆盖：Git 源、基础镜像、镜像仓库、Jenkins 实例、构建模板、构建任务、构建步骤、制品版本、制品别名。
package build

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/vortexops/vortexops/internal/domain"
)

// --- 枚举 ---

type GitProvider string

const (
	GitGitHub  GitProvider = "github"
	GitGitLab  GitProvider = "gitlab"
	GitGitea   GitProvider = "gitea"
	GitGeneric GitProvider = "generic"
)

type RegistryType string

const (
	RegistryHarbor   RegistryType = "harbor"
	RegistryDocker   RegistryType = "docker_registry"
	RegistryACR      RegistryType = "acr"
	RegistryECR      RegistryType = "ecr"
)

type RegistryStatus string

const (
	RegistryActive   RegistryStatus = "active"
	RegistryDisabled RegistryStatus = "disabled"
)

type JenkinsStatus string

const (
	JenkinsActive   JenkinsStatus = "active"
	JenkinsDisabled JenkinsStatus = "disabled"
)

type BaseImageRuntime string

const (
	RuntimeJava   BaseImageRuntime = "java"
	RuntimePython BaseImageRuntime = "python"
	RuntimeGo     BaseImageRuntime = "go"
	RuntimeNode   BaseImageRuntime = "node"
	RuntimeCustom BaseImageRuntime = "custom"
)

// ParseBaseImageRuntime 将语言字符串解析为 BaseImageRuntime，未知语言返回 false。
func ParseBaseImageRuntime(s string) (BaseImageRuntime, bool) {
	switch BaseImageRuntime(s) {
	case RuntimeJava, RuntimePython, RuntimeGo, RuntimeNode, RuntimeCustom:
		return BaseImageRuntime(s), true
	}
	return "", false
}

type BuildStrategy string

const (
	BuildDockerBuild BuildStrategy = "docker_build"
	BuildKaniko      BuildStrategy = "kaniko"
	BuildBuildpacks  BuildStrategy = "buildpacks"
	BuildCustom      BuildStrategy = "custom"
)

type DockerfileSource string

const (
	DockerfileFromTemplate DockerfileSource = "template"
	DockerfileFromRepo     DockerfileSource = "repo"
	DockerfileFromInline   DockerfileSource = "inline"
)

type TemplateScope string

const (
	TmplScopePlatform    TemplateScope = "platform"
	TmplScopeWorkspace   TemplateScope = "workspace"
	TmplScopeApplication TemplateScope = "application"
)

type RefType string

const (
	RefBranch RefType = "branch"
	RefTag    RefType = "tag"
	RefCommit RefType = "commit"
)

type BuildStatus string

const (
	BuildPending  BuildStatus = "pending"
	BuildQueued   BuildStatus = "queued"
	BuildRunning  BuildStatus = "running"
	BuildSuccess  BuildStatus = "success"
	BuildFailed   BuildStatus = "failed"
	BuildCanceled BuildStatus = "canceled"
	BuildTimeout  BuildStatus = "timeout"
)

type TriggerSource string

const (
	TriggerManual  TriggerSource = "manual"
	TriggerWebhook TriggerSource = "webhook"
	TriggerAPI     TriggerSource = "api"
	TriggerSchedule TriggerSource = "schedule"
)

type ImageSource string

const (
	ImgSourceBuild  ImageSource = "build"
	ImgSourceManual ImageSource = "manual"
	ImgSourceImport ImageSource = "import"
)

type ImageScanStatus string

const (
	ImgScanPending ImageScanStatus = "pending"
	ImgScanPassed  ImageScanStatus = "passed"
	ImgScanFailed  ImageScanStatus = "failed"
	ImgScanSkipped ImageScanStatus = "skipped"
)

type ImageStatus string

const (
	ImgStatusAvailable ImageStatus = "available"
	ImgStatusRetired   ImageStatus = "retired"
	ImgStatusDeleted   ImageStatus = "deleted"
)

type StepStatus string

const (
	StepPending  StepStatus = "pending"
	StepRunning  StepStatus = "running"
	StepSuccess  StepStatus = "success"
	StepFailed   StepStatus = "failed"
	StepSkipped  StepStatus = "skipped"
)

// IsTerminal 判断构建状态是否为终态（不可再变更）。
func (s BuildStatus) IsTerminal() bool {
	switch s {
	case BuildSuccess, BuildFailed, BuildCanceled, BuildTimeout:
		return true
	}
	return false
}

// IsActive 判断构建是否处于运行态（可取消）。
func (s BuildStatus) IsActive() bool {
	switch s {
	case BuildPending, BuildQueued, BuildRunning:
		return true
	}
	return false
}

// --- 实体 ---

// GitSource 代码源。
type GitSource struct {
	ID                int64
	UUID              uuid.UUID
	ApplicationID     int64
	Name              string
	Provider          GitProvider
	RepoURL           string
	DefaultBranch     string
	CredentialID      int64
	WebhookEnabled    bool
	WebhookSecretHash string
	LastSyncedAt      *time.Time
	domain.Audit
}

// Registry 镜像仓库。
type Registry struct {
	ID           int64
	UUID         uuid.UUID
	Name         string
	Type         RegistryType
	URL          string
	CredentialID int64
	IsDefault    bool
	Status       RegistryStatus
	domain.Audit
}

// JenkinsInstance Jenkins 实例。
type JenkinsInstance struct {
	ID               int64
	UUID             uuid.UUID
	Name             string
	URL              string
	CredentialID     int64
	DefaultJobFolder string
	IsDefault        bool
	Status           JenkinsStatus
	LastCheckedAt    *time.Time
	domain.Audit
}

// BaseImage 基础镜像。
// BuildTool/DefaultBuildCommand/DefaultArtifactPath/DefaultBuildArgs 为语言驱动的构建默认值，
// 前端新建构建时按 runtime 过滤列表，选中后预填这些默认值（构建工具只读，其余可编辑）。
// DockerfileTemplate 为多阶段构建模板，TriggerBuild 时用 text/template 渲染为完整 Dockerfile。
type BaseImage struct {
	ID                  int64
	UUID                uuid.UUID
	Name                string
	Runtime             BaseImageRuntime
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
	// Entrypoint 是运行时镜像的启动命令（JSON 数组形式，如 ["java","-jar","/app/app.jar"]）。
	// 空数组表示使用基础镜像自带的 CMD/ENTRYPOINT。在 Dockerfile 模板渲染时通过 {{.Entrypoint}} 注入。
	Entrypoint []string
	// IsWeb 为 true 时除应用启动命令外额外启动 nginx（渲染 Dockerfile 时自动包装）。
	IsWeb bool
	domain.Audit
}

// BuildTool 构建工具配置：可配置化的构建工具元数据（runtime+tool+命令+制品路径+builder_image）。
// builder_image 供 Tekton build Task 与 Jenkins docker run 使用，新增构建工具无需改代码。
type BuildTool struct {
	ID                  int64
	UUID                uuid.UUID
	Name                string
	Runtime             BaseImageRuntime
	Tool                string
	DefaultBuildCommand string
	DefaultArtifactPath string
	BuilderImage        string
	IsSystem            bool
	Description         string
	domain.Audit
}

// ErrBuildToolNotFound 构建工具不存在。
var ErrBuildToolNotFound = errors.New("build tool not found")

// BuildTemplate 构建模板。
type BuildTemplate struct {
	ID               int64
	UUID             uuid.UUID
	Scope            TemplateScope
	ScopeID          int64
	Name             string
	Description      string
	BuildStrategy    BuildStrategy
	BuildCommand     string
	BaseImageID      int64
	DockerfileSource DockerfileSource
	DockerfileContent string
	ContextPath      string
	BuildArgs        map[string]string
	EnvVars          map[string]string
	IsDefault        bool
	UsageCount       int
	domain.Audit
}

// Build 构建任务。
type Build struct {
	ID                int64
	UUID              uuid.UUID
	ApplicationID     int64
	BuildNumber       int
	GitSourceID       int64
	RefType           RefType
	RefValue          string
	CommitSHA         string
	CommitMessage     string
	BuildTemplateID   int64
	BuildStrategy     BuildStrategy
	BuildCommand      string
	ContextPath       string
	ArtifactPath      string // template 模式：制品路径（COPY 进运行时镜像）
	DockerfilePath    string // repo 模式：仓库内 Dockerfile 相对路径
	BaseImageID       int64
	BuildTool         string // 构建工具标识（maven/gradle/npm/go/custom），持久化以便 rebuild 恢复
	BuilderImage      string // 构建工具链镜像（供 Jenkins docker run / Tekton build Task），持久化以便 rebuild 恢复
	DockerfileSource  DockerfileSource
	DockerfileContent string
	BuildArgs         map[string]string
	TargetRegistryID  int64
	TargetRepository  string
	TargetTag         string
	OutputImageID     int64
	JenkinsInstanceID int64
	JenkinsQueueID    string
	JenkinsBuildNumber int
	JenkinsJobName    string
	PipelineRunName   string // Tekton PipelineRun 名称（tekton 模式）；Jenkins 模式留空
	Status            BuildStatus
	ProgressPercent   int
	CurrentStep       string
	DurationMs        int64
	StartedAt         *time.Time
	FinishedAt        *time.Time
	LogStorageKey     string
	LogExcerpt        string
	FailureReason     string
	TriggeredBy       int64
	TriggerSource     TriggerSource
	IdempotencyKey    string
	Metadata          map[string]any
	domain.Audit
}

// BuildStep 构建步骤。
type BuildStep struct {
	ID             int64
	BuildID        int64
	Seq            int
	Name           string
	Status         StepStatus
	StartedAt      *time.Time
	FinishedAt     *time.Time
	DurationMs     int64
	Message        string
	LogOffsetStart int64
	LogOffsetEnd   int64
	LogStorageKey  string
	LogSizeBytes   int64
	ErrorLine      string
}

// Image 制品版本。
type Image struct {
	ID               int64
	UUID             uuid.UUID
	ApplicationID    int64
	RegistryID       int64
	Repository       string
	Tag              string
	Digest           string
	FullReference    string
	VersionNumber    int
	VersionLabel     string
	Source           ImageSource
	BuildID          int64
	GitCommitSHA     string
	GitBranch        string
	GitCommitMessage string
	GitAuthor        string
	SizeBytes        int64
	ScanStatus       ImageScanStatus
	ScanResult       map[string]any
	Status           ImageStatus
	IsRollbackTarget bool
	Labels           map[string]string
	domain.Audit
}

// ImageVersionTag 制品别名（如 stable、latest-v1）。
type ImageVersionTag struct {
	ID            int64
	UUID          uuid.UUID
	ApplicationID int64
	Name          string
	ImageID       int64
	Description   string
	domain.Audit
}

// 领域错误。
var (
	ErrGitSourceNotFound    = errors.New("git source not found")
	ErrRegistryNotFound     = errors.New("registry not found")
	ErrRegistryNameExists   = errors.New("registry name already exists")
	ErrJenkinsNotFound      = errors.New("jenkins instance not found")
	ErrJenkinsNameExists    = errors.New("jenkins name already exists")
	ErrBaseImageNotFound    = errors.New("base image not found")
	ErrTemplateNotFound     = errors.New("build template not found")
	ErrBuildNotFound        = errors.New("build not found")
	ErrImageNotFound        = errors.New("image not found")
	ErrImageTagNotFound     = errors.New("image tag not found")
	ErrImageTagExists       = errors.New("image tag already exists")
	ErrBuildNotCancellable  = errors.New("build cannot be cancelled in current state")
	ErrIdempotencyConflict  = errors.New("build with same idempotency key already exists")
)

// --- 仓储接口 ---

// GitSourceInput Git 源创建/更新输入。
type GitSourceInput struct {
	ApplicationID   int64
	Name            string
	Provider        GitProvider
	RepoURL         string
	DefaultBranch   string
	CredentialID    int64
	WebhookEnabled  bool
	WebhookSecret   string // 明文，仓储层哈希
	ActorID         int64
}

// TriggerBuildInput 触发构建输入。
type TriggerBuildInput struct {
	ApplicationID    int64
	GitSourceID      int64
	RefType          RefType
	RefValue         string
	CommitSHA        string
	CommitMessage    string
	BuildTemplateID  int64
	BuildStrategy    BuildStrategy
	BuildCommand     string
	ContextPath      string
	BaseImageID      int64
	DockerfileSource DockerfileSource
	DockerfileContent string
	BuildArgs        map[string]string
	TargetRegistryID int64
	TargetRepository string
	TargetTag        string
	JenkinsInstanceID int64
	JenkinsJobName   string
	TriggeredBy      int64
	TriggerSource    TriggerSource
	IdempotencyKey   string
	Metadata         map[string]any
}

// BuildQuery 构建查询。
type BuildQuery struct {
	ApplicationID int64
	Status        BuildStatus
	TriggeredBy   int64
	Offset        int
	Limit         int
}

// Repository 构建领域仓储接口。
type Repository interface {
	// Git 源
	CreateGitSource(ctx context.Context, g *GitSource) error
	GetGitSourceByID(ctx context.Context, id int64) (*GitSource, error)
	GetGitSourceByName(ctx context.Context, appID int64, name string) (*GitSource, error)
	ListGitSources(ctx context.Context, appID int64) ([]*GitSource, error)
	UpdateGitSource(ctx context.Context, g *GitSource) error
	DeleteGitSource(ctx context.Context, id, actorID int64) error

	// 镜像仓库
	CreateRegistry(ctx context.Context, r *Registry) error
	GetRegistryByID(ctx context.Context, id int64) (*Registry, error)
	GetRegistryByName(ctx context.Context, name string) (*Registry, error)
	GetDefaultRegistry(ctx context.Context) (*Registry, error)
	ListRegistries(ctx context.Context, offset, limit int) ([]*Registry, int64, error)
	UpdateRegistry(ctx context.Context, r *Registry) error
	DeleteRegistry(ctx context.Context, id, actorID int64) error

	// Jenkins 实例
	CreateJenkins(ctx context.Context, j *JenkinsInstance) error
	GetJenkinsByID(ctx context.Context, id int64) (*JenkinsInstance, error)
	GetJenkinsByName(ctx context.Context, name string) (*JenkinsInstance, error)
	GetDefaultJenkins(ctx context.Context) (*JenkinsInstance, error)
	ListJenkins(ctx context.Context, offset, limit int) ([]*JenkinsInstance, int64, error)
	UpdateJenkins(ctx context.Context, j *JenkinsInstance) error
	DeleteJenkins(ctx context.Context, id, actorID int64) error

	// 基础镜像
	CreateBaseImage(ctx context.Context, b *BaseImage) error
	GetBaseImageByID(ctx context.Context, id int64) (*BaseImage, error)
	ListBaseImages(ctx context.Context, runtime BaseImageRuntime, offset, limit int) ([]*BaseImage, int64, error)
	UpdateBaseImage(ctx context.Context, b *BaseImage) error
	DeleteBaseImage(ctx context.Context, id, actorID int64) error

	// 构建工具
	CreateBuildTool(ctx context.Context, bt *BuildTool) error
	GetBuildToolByID(ctx context.Context, id int64) (*BuildTool, error)
	GetBuildToolByRuntimeTool(ctx context.Context, runtime BaseImageRuntime, tool string) (*BuildTool, error)
	ListBuildTools(ctx context.Context, runtime BaseImageRuntime, offset, limit int) ([]*BuildTool, int64, error)
	UpdateBuildTool(ctx context.Context, bt *BuildTool) error
	DeleteBuildTool(ctx context.Context, id, actorID int64) error

	// 构建模板
	CreateTemplate(ctx context.Context, t *BuildTemplate) error
	GetTemplateByID(ctx context.Context, id int64) (*BuildTemplate, error)
	ListTemplates(ctx context.Context, scope TemplateScope, scopeID int64, offset, limit int) ([]*BuildTemplate, int64, error)
	UpdateTemplate(ctx context.Context, t *BuildTemplate) error
	DeleteTemplate(ctx context.Context, id, actorID int64) error
	IncrementUsage(ctx context.Context, id int64) error

	// 构建任务
	CreateBuild(ctx context.Context, b *Build) error
	GetBuildByID(ctx context.Context, id int64) (*Build, error)
	GetBuildByUUID(ctx context.Context, id uuid.UUID) (*Build, error)
	GetBuildByIdempotencyKey(ctx context.Context, appID int64, key string) (*Build, error)
	NextBuildNumber(ctx context.Context, appID int64) (int, error)
	ListBuilds(ctx context.Context, q BuildQuery) ([]*Build, int64, error)
	UpdateBuildStatus(ctx context.Context, id int64, status BuildStatus, progress int, currentStep string, version int) (*Build, error)
	CompleteBuild(ctx context.Context, id int64, status BuildStatus, outputImageID int64, durationMs int64, logKey, logExcerpt, failureReason string, finishedAt time.Time, version int) (*Build, error)
	SetJenkinsInfo(ctx context.Context, id int64, queueID string, buildNum int, jobName string) error
	// SetJenkinsBuildNumber 在队列项被调度后回填 jenkins_build_number。
	// 仅当当前 jenkins_build_number 为 0 时写入，避免覆盖已解析的构建号；不改变 status。
	SetJenkinsBuildNumber(ctx context.Context, id int64, buildNumber int) error
	SetPipelineRunName(ctx context.Context, id int64, pipelineRunName string) error
	// SetBuildTargetTag 回填/更新构建的 target_tag（rebuild 重新生成 tag 后持久化）。
	SetBuildTargetTag(ctx context.Context, id int64, tag string) error
	// UpdateBuild 更新构建可编辑元信息（commit_message/target_tag/metadata），乐观锁。
	UpdateBuild(ctx context.Context, b *Build) (*Build, error)
	// DeleteBuild 软删除构建（仅终态构建可删）。
	DeleteBuild(ctx context.Context, id, actorID int64) error
	// ResetBuildForRebuild 重置构建为 pending 以便重新拉取代码并构建（同一条记录）。
	// 清空运行态字段（started_at/finished_at/duration/jenkins 运行号/产物/error/log），
	// 写入新的 commit_sha/commit_message，version+1。
	ResetBuildForRebuild(ctx context.Context, id int64, commitSHA, commitMessage string, version int) (*Build, error)

	// 构建步骤
	CreateStep(ctx context.Context, s *BuildStep) error
	UpdateStep(ctx context.Context, s *BuildStep) error
	ListSteps(ctx context.Context, buildID int64) ([]*BuildStep, error)

	// 制品版本
	CreateImage(ctx context.Context, img *Image) error
	GetImageByID(ctx context.Context, id int64) (*Image, error)
	ListImages(ctx context.Context, appID int64, offset, limit int) ([]*Image, int64, error)
	UpdateImageScan(ctx context.Context, id int64, status ImageScanStatus, result map[string]any) error
	RetireImage(ctx context.Context, id int64) error
	NextImageVersion(ctx context.Context, appID int64) (int, error)

	// 制品别名
	CreateImageTag(ctx context.Context, t *ImageVersionTag) error
	GetImageTagByName(ctx context.Context, appID int64, name string) (*ImageVersionTag, error)
	ListImageTags(ctx context.Context, appID int64) ([]*ImageVersionTag, error)
	UpdateImageTag(ctx context.Context, t *ImageVersionTag) error
	DeleteImageTag(ctx context.Context, id, actorID int64) error
}

// JenkinsClient Jenkins REST 客户端抽象（infrastructure/jenkins 实现）。
type JenkinsClient interface {
	TriggerBuild(ctx context.Context, jobName string, params map[string]string) (queueID string, err error)
	// GetQueueItemBuildNumber 查询队列项对应的 Jenkins 构建号。
	// 构建已被调度执行时返回 (number, true)；仍在排队时返回 (0, false, nil)。
	GetQueueItemBuildNumber(ctx context.Context, queueID string) (buildNumber int, ready bool, err error)
	GetBuildStatus(ctx context.Context, jobName string, buildNumber int) (status BuildStatus, building bool, err error)
	GetConsoleLog(ctx context.Context, jobName string, buildNumber int, start int64) (text string, hasMore bool, err error)
	StopBuild(ctx context.Context, jobName string, buildNumber int) error
	// EnsureJob 确保 job 存在，不存在则按 configXML 创建（含 folder 兜底创建）。
	// jobName 形如 "vortexops/app-1"，先探测 folder 与 job，404 时分别创建。
	EnsureJob(ctx context.Context, jobName, configXML string) error
	// GetLastBuildNumber 返回 job 最新构建号；job 无构建时返回 (0, nil)。
	// 用于队列项已被 Jenkins GC（404）后回溯构建号。
	GetLastBuildNumber(ctx context.Context, jobName string) (int, error)
}

// BuildEngineClient 统一构建引擎客户端抽象，Jenkins 与 Tekton 均实现此接口。
// Trigger 返回引擎特定的运行标识（Jenkins: queueID；Tekton: PipelineRun 名称）。
// GetStatus 返回归一化构建状态与是否仍在运行。
// GetLog 返回从 start 偏移开始的日志增量；hasMore 表示引擎侧仍有增量可拉。
// ListSteps 返回引擎侧分步信息（Tekton: 每个 TaskRun 一步；Jenkins: 单步）。
// Stop 终止运行中的构建。
type BuildEngineClient interface {
	Trigger(ctx context.Context, buildID int64, params map[string]string) (runID string, err error)
	GetStatus(ctx context.Context, runID string) (status BuildStatus, building bool, err error)
	GetLog(ctx context.Context, runID string, start int64) (text string, hasMore bool, err error)
	ListSteps(ctx context.Context, runID string) ([]EngineStep, error)
	Stop(ctx context.Context, runID string) error
}

// EngineStep 引擎侧分步信息，用于同步到 vo_build_steps。
type EngineStep struct {
	Name       string
	Status     StepStatus
	StartedAt  *time.Time
	FinishedAt *time.Time
	Message    string
	LogKey     string
}

// HarborClient 已废弃：镜像仓库相关能力统一由 RegistryAdapter 提供。
// 保留此类型仅为兼容历史代码引用，新代码请使用 RegistryAdapter / RegistryAdapterFactory。
type HarborClient = RegistryAdapter

// LogStore 构建日志存储抽象（infrastructure/s3 实现：进行中走 Jenkins 流式，完成后归档 S3）。
type LogStore interface {
	Upload(ctx context.Context, key string, data []byte) error
	Download(ctx context.Context, key string) ([]byte, error)
	DownloadRange(ctx context.Context, key string, start, end int64) ([]byte, error)
}
