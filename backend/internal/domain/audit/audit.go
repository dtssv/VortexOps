// Package audit 是审计日志领域的核心实体与仓储接口。
package audit

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Action 审计动作类型。
type Action string

const (
	ActionCreate  Action = "create"
	ActionUpdate  Action = "update"
	ActionDelete  Action = "delete"
	ActionRead    Action = "read"
	ActionLogin   Action = "login"
	ActionLogout  Action = "logout"
	ActionDeploy  Action = "deploy"
	ActionRollback Action = "rollback"
	ActionConfig  Action = "config"
	ActionScale   Action = "scale"
	ActionApproval Action = "approval"
	ActionExport  Action = "export"
	ActionPodLogin Action = "pod_login" // WebSSH 登录 Pod（运维会话审计）
	ActionExecute Action = "execute"    // 在 Pod 内执行命令（netcmd/exec 等带命令审计的场景）
)

// Log 审计日志条目。
type Log struct {
	ID              int64
	UUID            uuid.UUID
	UserID          int64
	UserName        string
	WorkspaceID     int64
	ResourceType    string
	ResourceID      int64
	ResourceName    string
	Action          Action
	Operation       string
	RequestID       string
	Method          string
	Path            string
	StatusCode      int
	ClientIP        string
	UserAgent       string
	RequestBody     map[string]any
	ResponseSummary map[string]any
	DurationMs      int
	ErrorMessage    string
	CreatedAt       time.Time
}

// Query 审计日志查询。
type Query struct {
	UserID       int64
	WorkspaceID  int64
	ResourceType string
	Action       Action
	StartTime    time.Time
	EndTime      time.Time
	Offset       int
	Limit        int
}

// 领域错误。
var (
	ErrLogNotFound = errors.New("audit log not found")
)

// Repository 审计日志仓储接口。
type Repository interface {
	Append(ctx context.Context, log *Log) error
	GetByID(ctx context.Context, id int64) (*Log, error)
	List(ctx context.Context, q Query) ([]*Log, int64, error)
}

// --- 上下文辅助：用于审计中间件与 handler 注入显式动作/资源 ---

type auditCtxKey string

const (
	ctxKeyAction     auditCtxKey = "action"
	ctxKeyResource   auditCtxKey = "resource"
	ctxKeyResourceID auditCtxKey = "resource_id"
)

// WithActionContext 注入审计动作到 context。
func WithActionContext(ctx context.Context, action Action) context.Context {
	return context.WithValue(ctx, ctxKeyAction, action)
}

// ActionFromContext 从 context 读取显式审计动作（无则返回空串）。
func ActionFromContext(ctx context.Context) Action {
	v, _ := ctx.Value(ctxKeyAction).(Action)
	return v
}

// WithResourceContext 注入资源类型与 ID 到 context。
func WithResourceContext(ctx context.Context, resourceType string, resourceID int64) context.Context {
	ctx = context.WithValue(ctx, ctxKeyResource, resourceType)
	return context.WithValue(ctx, ctxKeyResourceID, resourceID)
}

// ResourceFromContext 从 context 读取资源类型。
func ResourceFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyResource).(string)
	return v
}

// ResourceIDFromContext 从 context 读取资源 ID。
func ResourceIDFromContext(ctx context.Context) int64 {
	v, _ := ctx.Value(ctxKeyResourceID).(int64)
	return v
}
