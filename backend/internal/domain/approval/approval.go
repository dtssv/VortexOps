// Package approval 是审批领域的核心实体与仓储接口。
// 覆盖发布、晋升、配置变更等操作的审批流。
package approval

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

type ResourceType string

const (
	ResourceRelease           ResourceType = "release"
	ResourcePromotion         ResourceType = "promotion"
	ResourceConfig            ResourceType = "config"
	ResourceInferenceRelease  ResourceType = "inference_release"
	ResourceWorkspaceCreation ResourceType = "workspace_creation"
)

type Status string

const (
	StatusPending  Status = "pending"
	StatusApproved Status = "approved"
	StatusRejected Status = "rejected"
	StatusExpired  Status = "expired"
	StatusCanceled Status = "canceled"
)

// Approval 审批记录。
type Approval struct {
	ID            int64
	UUID          uuid.UUID
	WorkspaceID   int64
	ResourceType  ResourceType
	ResourceID    int64
	Operation     string
	RequestedBy   int64
	RequestedAt   time.Time
	ApproverRole  string
	Status        Status
	ApproverID    int64
	ApprovedAt    *time.Time
	Comment       string
	ExpiresAt     *time.Time
	Version       int
	CreatedAt     time.Time
	CreatedBy     int64
	UpdatedAt     time.Time
	UpdatedBy     int64
}

// CreateInput 创建审批输入。
type CreateInput struct {
	WorkspaceID  int64
	ResourceType ResourceType
	ResourceID   int64
	Operation    string
	RequestedBy  int64
	ApproverRole string
	Comment      string
	ExpiresAt    *time.Time
}

// Query 审批查询。
type Query struct {
	WorkspaceID  int64
	ResourceType ResourceType
	Status       Status
	Offset       int
	Limit        int
}

var (
	ErrApprovalNotFound = errors.New("approval not found")
	ErrAlreadyPending   = errors.New("an approval is already pending for this resource")
)

// Repository 审批仓储接口。
type Repository interface {
	Create(ctx context.Context, a *Approval) error
	GetByID(ctx context.Context, id int64) (*Approval, error)
	List(ctx context.Context, q Query) ([]*Approval, int64, error)
	Update(ctx context.Context, a *Approval) error
	GetPendingByResource(ctx context.Context, rt ResourceType, resourceID int64) (*Approval, error)
}
