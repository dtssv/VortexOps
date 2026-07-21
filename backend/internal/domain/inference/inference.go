// Package inference 是大模型服务领域的核心实体与仓储接口。
// 覆盖：ModelRegistry（模型仓库）、Model、ModelVersion、ModelAdapter、
// InferenceService、InferenceRelease、InferenceAPIKey、InferenceRoute、InferenceUsage（计量）。
package inference

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/vortexops/vortexops/internal/domain"
)

// --- 枚举 ---

type RegistryProvider string

const (
	ProviderHuggingFace RegistryProvider = "huggingface"
	ProviderOSS         RegistryProvider = "oss"
	ProviderS3          RegistryProvider = "s3"
	ProviderLocal       RegistryProvider = "local"
	ProviderCustom      RegistryProvider = "custom"
)

type Framework string

const (
	FrameworkVLLM   Framework = "vllm"
	FrameworkTGI    Framework = "tgi"
	FrameworkTriton Framework = "triton"
	FrameworkSGLang Framework = "sglang"
	FrameworkOllama Framework = "ollama"
	FrameworkCustom Framework = "custom"
)

type Precision string

const (
	PrecisionFP32 Precision = "fp32"
	PrecisionFP16 Precision = "fp16"
	PrecisionBF16 Precision = "bf16"
	PrecisionINT8 Precision = "int8"
	PrecisionINT4 Precision = "int4"
)

type Quantization string

const (
	QuantGPTQ      Quantization = "gptq"
	QuantAWQ       Quantization = "awq"
	QuantSqueezeLLM Quantization = "squeezellm"
	QuantNone      Quantization = "none"
)

type DownloadStatus string

const (
	DownloadNotDownloaded DownloadStatus = "not_downloaded"
	DownloadDownloading   DownloadStatus = "downloading"
	DownloadReady         DownloadStatus = "ready"
	DownloadFailed        DownloadStatus = "failed"
)

type AdapterType string

const (
	AdapterLoRA   AdapterType = "lora"
	AdapterQLoRA  AdapterType = "qlora"
	AdapterPrefix AdapterType = "prefix"
)

type ServiceStatus string

const (
	SvcStopped   ServiceStatus = "stopped"
	SvcStarting  ServiceStatus = "starting"
	SvcRunning   ServiceStatus = "running"
	SvcUpdating  ServiceStatus = "updating"
	SvcFailed    ServiceStatus = "failed"
)

type Readiness string

const (
	ReadinessUnknown       Readiness = "unknown"
	ReadinessNotReady      Readiness = "not_ready"
	ReadinessPartialReady  Readiness = "partial_ready"
	ReadinessReady         Readiness = "ready"
)

type AccessMode string

const (
	AccessInternal AccessMode = "internal"
	AccessExternal AccessMode = "external"
)

type ReleaseStrategy string

const (
	RelStrategyRolling   ReleaseStrategy = "rolling"
	RelStrategyBlueGreen ReleaseStrategy = "blue_green"
	RelStrategyCanary    ReleaseStrategy = "canary"
)

type ReleaseStatus string

const (
	RelStatusPending     ReleaseStatus = "pending"
	RelStatusRunning     ReleaseStatus = "running"
	RelStatusVerifying   ReleaseStatus = "verifying"
	RelStatusSucceeded   ReleaseStatus = "succeeded"
	RelStatusFailed      ReleaseStatus = "failed"
	RelStatusAborted     ReleaseStatus = "aborted"
	RelStatusRolledBack  ReleaseStatus = "rolled_back"
)

func (s ReleaseStatus) IsTerminal() bool {
	switch s {
	case RelStatusSucceeded, RelStatusFailed, RelStatusAborted, RelStatusRolledBack:
		return true
	}
	return false
}

type APIKeyStatus string

const (
	APIKeyActive  APIKeyStatus = "active"
	APIKeyRevoked APIKeyStatus = "revoked"
)

type RouteStrategy string

const (
	RouteWeighted RouteStrategy = "weighted"
	RouteHeader   RouteStrategy = "header"
	RouteFailover RouteStrategy = "failover"
)

// --- 实体 ---

// ModelRegistry 模型仓库（HuggingFace/OSS/S3 等）。
type ModelRegistry struct {
	ID              int64
	UUID            uuid.UUID
	WorkspaceID     int64
	Name            string
	Provider        RegistryProvider
	Endpoint        string
	CredentialID    int64
	CachePVCName    string
	CachePath       string
	CacheSizeBytes  int64
	Status          string
	domain.Audit
}

// Model 模型元数据。
type Model struct {
	ID               int64
	UUID             uuid.UUID
	WorkspaceID      int64
	RegistryID       int64
	Name             string
	DisplayName      string
	Description      string
	BaseArchitecture string
	ParameterCount   string
	License          string
	Tags             []string
	domain.Audit
}

// ModelVersion 模型版本（权重 + 框架配置）。
type ModelVersion struct {
	ID                  int64
	UUID                uuid.UUID
	ModelID             int64
	Version             string
	Precision           Precision
	Quantization        Quantization
	WeightsPath         string
	WeightsSizeBytes    int64
	WeightsChecksum     string
	Framework           Framework
	FrameworkConfig     map[string]any
	MinGPUMemoryBytes   int64
	RecommendedGPUCount int
	DownloadStatus      DownloadStatus
	DownloadProgress    int
	IsDefault           bool
	domain.Audit
}

// ModelAdapter LoRA/QLoRA 适配器。
type ModelAdapter struct {
	ID                int64
	UUID              uuid.UUID
	BaseModelVersionID int64
	Name              string
	AdapterType       AdapterType
	WeightsPath       string
	Rank              int
	Scale             float64
	domain.Audit
}

// InferenceService 推理服务定义。
type InferenceService struct {
	ID                  int64
	UUID                uuid.UUID
	WorkspaceID         int64
	ApplicationID       int64
	GroupID             int64
	Name                string
	DisplayName         string
	Description         string
	ClusterID           int64
	Namespace           string
	WorkloadName        string
	ServiceName         string
	BaseModelVersionID  int64
	AdapterIDs          []int64
	Framework           Framework
	FrameworkConfig     map[string]any
	Replicas            int
	Resources           map[string]any
	GPUCount            int
	GPUType             string
	TensorParallelSize  int
	PipelineParallelSize int
	StorageSizeBytes    int64
	CurrentReleaseID    int64
	CurrentStatus       ServiceStatus
	Readiness           Readiness
	AutoscalingEnabled  bool
	HPAMinReplicas      int
	HPAMaxReplicas      int
	HPAMetrics          map[string]any
	AccessMode          AccessMode
	ExternalEndpoint    string
	Labels              map[string]any
	Metadata            map[string]any
	domain.Audit
}

// InferenceRelease 推理发布记录。
type InferenceRelease struct {
	ID                   int64
	UUID                 uuid.UUID
	InferenceServiceID   int64
	GroupID              int64
	ReleaseNumber        int
	PreviousReleaseID   int64
	TargetModelVersionID int64
	TargetAdapterIDs     []int64
	Strategy             ReleaseStrategy
	Replicas             int
	Status               ReleaseStatus
	ProgressPercent      int
	FailureReason        string
	StartedBy            int64
	StartedAt            time.Time
	FinishedAt           *time.Time
	DurationMs           int64
	domain.Audit
}

// InferenceAPIKey 推理 API Key。
type InferenceAPIKey struct {
	ID                 int64
	UUID               uuid.UUID
	InferenceServiceID int64
	Name               string
	KeyPrefix          string
	KeyHash            string
	DailyTokenQuota    int64
	RateLimitPerMin    int
	ExpiresAt          *time.Time
	LastUsedAt         *time.Time
	Status             APIKeyStatus
	domain.Audit
}

// InferenceRoute 推理路由（多服务加权/头部/故障转移）。
type InferenceRoute struct {
	ID                int64
	UUID              uuid.UUID
	WorkspaceID       int64
	Name              string
	Description       string
	Strategy          RouteStrategy
	Rules             map[string]any
	DefaultServiceID  int64
	Status            string
	domain.Audit
}

// InferenceUsage 计量记录（分区表）。
type InferenceUsage struct {
	ID                 int64
	UUID               uuid.UUID
	InferenceServiceID int64
	APIKeyID           int64
	CallerID           int64
	PromptTokens       int
	CompletionTokens   int
	TotalTokens        int
	DurationMs         int
	StatusCode         int
	ModelVersionID     int64
	CreatedAt          time.Time
}

// 领域错误。
var (
	ErrRegistryNotFound       = errors.New("model registry not found")
	ErrRegistryNameUsed       = errors.New("model registry name already used in workspace")
	ErrModelNotFound          = errors.New("model not found")
	ErrModelNameUsed          = errors.New("model name already used in workspace")
	ErrModelVersionNotFound   = errors.New("model version not found")
	ErrModelVersionUsed       = errors.New("model version already exists")
	ErrAdapterNotFound        = errors.New("model adapter not found")
	ErrServiceNotFound        = errors.New("inference service not found")
	ErrServiceNameUsed        = errors.New("inference service name already used in workspace")
	ErrReleaseNotFound        = errors.New("inference release not found")
	ErrAPIKeyNotFound         = errors.New("inference api key not found")
	ErrAPIKeyRevoked          = errors.New("inference api key revoked")
	ErrRouteNotFound          = errors.New("inference route not found")
)

// ServiceQuery 推理服务查询。
type ServiceQuery struct {
	WorkspaceID int64
	ClusterID   int64
	Status      ServiceStatus
	Offset      int
	Limit       int
}

// ReleaseQuery 发布查询。
type ReleaseQuery struct {
	ServiceID int64
	Status    ReleaseStatus
	Offset    int
	Limit     int
}

// UsageQuery 计量查询。
type UsageQuery struct {
	ServiceID     int64
	APIKeyID      int64
	StartTime     time.Time
	EndTime       time.Time
	Offset        int
	Limit         int
}

// UsageSummary 计量汇总。
type UsageSummary struct {
	ServiceID        int64
	TotalRequests    int64
	TotalPromptTokens   int64
	TotalCompletionTokens int64
	TotalTokens      int64
	AvgDurationMs    float64
}

// Repository 推理领域仓储接口。
type Repository interface {
	// registry
	CreateRegistry(ctx context.Context, r *ModelRegistry) error
	GetRegistryByID(ctx context.Context, id int64) (*ModelRegistry, error)
	GetRegistryByName(ctx context.Context, workspaceID int64, name string) (*ModelRegistry, error)
	ListRegistries(ctx context.Context, workspaceID int64, offset, limit int) ([]*ModelRegistry, int64, error)
	DeleteRegistry(ctx context.Context, id, actorID int64) error

	// model
	CreateModel(ctx context.Context, m *Model) error
	GetModelByID(ctx context.Context, id int64) (*Model, error)
	GetModelByName(ctx context.Context, workspaceID int64, name string) (*Model, error)
	ListModels(ctx context.Context, workspaceID, registryID int64, offset, limit int) ([]*Model, int64, error)
	DeleteModel(ctx context.Context, id, actorID int64) error

	// model version
	CreateModelVersion(ctx context.Context, v *ModelVersion) error
	GetModelVersionByID(ctx context.Context, id int64) (*ModelVersion, error)
	ListModelVersions(ctx context.Context, modelID int64) ([]*ModelVersion, error)
	UpdateModelVersionDownload(ctx context.Context, id int64, status DownloadStatus, progress int) error
	DeleteModelVersion(ctx context.Context, id, actorID int64) error

	// adapter
	CreateAdapter(ctx context.Context, a *ModelAdapter) error
	GetAdapterByID(ctx context.Context, id int64) (*ModelAdapter, error)
	ListAdapters(ctx context.Context, baseModelVersionID int64) ([]*ModelAdapter, error)
	DeleteAdapter(ctx context.Context, id, actorID int64) error

	// inference service
	CreateService(ctx context.Context, s *InferenceService) error
	GetServiceByID(ctx context.Context, id int64) (*InferenceService, error)
	GetServiceByName(ctx context.Context, workspaceID int64, name string) (*InferenceService, error)
	ListServices(ctx context.Context, q ServiceQuery) ([]*InferenceService, int64, error)
	UpdateService(ctx context.Context, s *InferenceService) error
	UpdateServiceStatus(ctx context.Context, id int64, status ServiceStatus, readiness Readiness, releaseID int64, version int) (*InferenceService, error)
	DeleteService(ctx context.Context, id, actorID int64) error

	// release
	CreateRelease(ctx context.Context, r *InferenceRelease) error
	GetReleaseByID(ctx context.Context, id int64) (*InferenceRelease, error)
	ListReleases(ctx context.Context, q ReleaseQuery) ([]*InferenceRelease, int64, error)
	UpdateRelease(ctx context.Context, r *InferenceRelease) error
	NextReleaseNumber(ctx context.Context, serviceID int64) (int, error)

	// api key
	CreateAPIKey(ctx context.Context, k *InferenceAPIKey) error
	GetAPIKeyByHash(ctx context.Context, hash string) (*InferenceAPIKey, error)
	GetAPIKeyByID(ctx context.Context, id int64) (*InferenceAPIKey, error)
	ListAPIKeys(ctx context.Context, serviceID int64) ([]*InferenceAPIKey, error)
	UpdateAPIKeyLastUsed(ctx context.Context, id int64, lastUsed time.Time) error
	RevokeAPIKey(ctx context.Context, id, actorID int64) error

	// route
	CreateRoute(ctx context.Context, r *InferenceRoute) error
	GetRouteByID(ctx context.Context, id int64) (*InferenceRoute, error)
	ListRoutes(ctx context.Context, workspaceID int64) ([]*InferenceRoute, error)
	UpdateRoute(ctx context.Context, r *InferenceRoute) error
	DeleteRoute(ctx context.Context, id, actorID int64) error

	// usage
	AppendUsage(ctx context.Context, u *InferenceUsage) error
	ListUsage(ctx context.Context, q UsageQuery) ([]*InferenceUsage, int64, error)
	SummarizeUsage(ctx context.Context, q UsageQuery) (*UsageSummary, error)
}
