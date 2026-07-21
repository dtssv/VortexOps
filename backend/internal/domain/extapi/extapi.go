// Package extapi 是对外 API 领域的核心实体与仓储接口。
package extapi

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/vortexops/vortexops/internal/domain"
)

const (
	TokenTypeExternal = "external"
	TokenPrefix       = "voe_"
)

// Scope 常量（见 docs/api-external.md §3）。
// ScopeMiddleware 涵盖中间件应用全生命周期：部署、更新、扩缩容、停止/启动、
// 删除、状态、Pod、日志、发布历史、回滚、成员、镜像管理（统一一个 scope 简化授权）。
// ScopeConfigSet / ScopeImage 为预留 scope：当前中间件场景下配置与镜像均由
// ScopeMiddleware 端点统一处理，未来如需独立粒度可启用。
const (
	ScopeWorkspaceRead = "ext:workspace:read"
	ScopeDeploy        = "ext:deploy"
	ScopeScale         = "ext:scale"
	ScopeConfig        = "ext:config"
	ScopeConfigSet     = "ext:configset"
	ScopeBuild         = "ext:build"
	ScopeImage         = "ext:image"
	ScopeMiddleware    = "ext:middleware"
	ScopeInference     = "ext:inference"
	ScopePipeline      = "ext:pipeline"
	ScopeStatus        = "ext:status"
	ScopeRollback      = "ext:rollback"
)

// AllScopes 返回全部合法 scope（Token 创建校验用）。
func AllScopes() []string {
	return []string{
		ScopeWorkspaceRead, ScopeDeploy, ScopeScale, ScopeConfig, ScopeConfigSet,
		ScopeBuild, ScopeImage, ScopeMiddleware, ScopeInference, ScopePipeline,
		ScopeStatus, ScopeRollback,
	}
}

// TokenStatus Token 状态。
type TokenStatus string

const (
	TokenStatusActive  TokenStatus = "active"
	TokenStatusRevoked TokenStatus = "revoked"
)

// ExternalToken external 类型 API Token（vo_api_tokens.token_type='external'）。
type ExternalToken struct {
	ID                int64
	UUID              uuid.UUID
	UserID            int64
	Name              string
	TokenPrefix       string
	TokenHash         string
	Scopes            []string
	AllowedWorkspaces []int64
	AllowedApps       []int64
	RateLimitPerMin   *int
	IPAllowlist       []string
	WebhookURL        string
	ExpiresAt         *time.Time
	LastUsedAt        *time.Time
	LastUsedIP        string
	Status            TokenStatus
	domain.Audit
}

// HasScope 检查 Token 是否包含指定 scope。
func (t *ExternalToken) HasScope(scope string) bool {
	for _, s := range t.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// IsActive 检查 Token 是否可用。
func (t *ExternalToken) IsActive(now time.Time) bool {
	if t.Status != TokenStatusActive {
		return false
	}
	if t.ExpiresAt != nil && !t.ExpiresAt.After(now) {
		return false
	}
	return true
}

// Callback 异步任务完成回调配置。
type Callback struct {
	URL       string
	Event     string
	Secret    string
	RequestID string
}

// IdempotencyRecord 幂等键缓存记录（Redis TTL 存储）。
type IdempotencyRecord struct {
	Key          string
	TokenID      int64
	Method       string
	Path         string
	StatusCode   int
	ResponseBody []byte
	CreatedAt    time.Time
}

// ExternalCallLog 对外 API 调用审计（vo_external_api_call_logs）。
type ExternalCallLog struct {
	ID           int64
	UUID         uuid.UUID
	TokenID      int64
	TokenPrefix  string
	Method       string
	Path         string
	Operation    string
	WorkspaceID  int64
	ResourceType string
	ResourceUUID string
	RequestID    string
	StatusCode   int
	DurationMs   int
	ClientIP     string
	UserAgent    string
	ErrorMessage string
	CreatedAt    time.Time
}

// WorkspaceCreationPolicy 自助建空间策略（vo_workspace_creation_policies）。
type WorkspaceCreationPolicy struct {
	ID                     int64
	UUID                   uuid.UUID
	Name                   string
	AppliesToRoles         []string
	AllowSelfCreate        bool
	MaxWorkspacesPerUser   int
	DefaultQuota           map[string]any
	DefaultClusters        []int64
	RequireApproval        bool
	ApproverRole           string
	AutoBindCatalog        bool
	domain.Audit
}

// TokenQuery Token 列表查询。
type TokenQuery struct {
	UserID int64
	Offset int
	Limit  int
}

// 领域错误。
var (
	ErrTokenNotFound      = errors.New("external token not found")
	ErrTokenRevoked       = errors.New("external token revoked")
	ErrTokenExpired       = errors.New("external token expired")
	ErrScopeDenied        = errors.New("scope denied")
	ErrIPNotAllowed       = errors.New("ip not allowed")
	ErrIdempotencyConflict = errors.New("idempotency conflict")
	ErrPolicyNotFound     = errors.New("workspace creation policy not found")
	ErrSelfCreateDenied   = errors.New("self workspace creation denied")
)

// Repository 对外 API 持久化接口。
type Repository interface {
	// Token CRUD
	CreateToken(ctx context.Context, t *ExternalToken) error
	GetTokenByHash(ctx context.Context, hash string) (*ExternalToken, error)
	GetTokenByID(ctx context.Context, id int64) (*ExternalToken, error)
	ListTokensByUser(ctx context.Context, q TokenQuery) ([]*ExternalToken, int64, error)
	UpdateToken(ctx context.Context, t *ExternalToken) error
	RevokeToken(ctx context.Context, id, actorID int64) error
	UpdateTokenLastUsed(ctx context.Context, id int64, ip string, at time.Time) error

	// 调用审计
	AppendCallLog(ctx context.Context, log *ExternalCallLog) error

	// 幂等（Redis 实现，接口统一在此声明）
	GetIdempotency(ctx context.Context, key string) (*IdempotencyRecord, error)
	SetIdempotency(ctx context.Context, rec *IdempotencyRecord, ttl time.Duration) error

	// 自助建空间策略
	GetWorkspaceCreationPolicy(ctx context.Context, id int64) (*WorkspaceCreationPolicy, error)
	ListWorkspaceCreationPolicies(ctx context.Context) ([]*WorkspaceCreationPolicy, error)
	CountUserWorkspaces(ctx context.Context, userID int64) (int64, error)
}
