// Package clusterops 是集群运维领域的核心实体与仓储接口。
// 涵盖计划运维任务（重启/drain/cordon/uncordon/sync_status）与节点状态缓存，
// 支撑「集群监控/运维/通知」三大能力。
package clusterops

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/vortexops/vortexops/internal/domain"
)

// OperationType 计划运维操作类型。
type OperationType string

const (
	OpRestart     OperationType = "restart"      // 计划重启节点（cordon→drain→云厂商重启→uncordon，重启动作由人工或外部触发）
	OpDrain       OperationType = "drain"        // 驱逐节点上所有 Pod
	OpCordon      OperationType = "cordon"       // 设置节点不可调度
	OpUncordon    OperationType = "uncordon"     // 设置节点可调度
	OpSyncStatus  OperationType = "sync_status"  // 强制同步节点状态
)

// OperationStatus 运维任务状态。
type OperationStatus string

const (
	StatusPending   OperationStatus = "pending"
	StatusRunning   OperationStatus = "running"
	StatusCompleted OperationStatus = "completed"
	StatusFailed    OperationStatus = "failed"
	StatusCancelled OperationStatus = "cancelled"
)

// NodeHealth 节点健康状态。
type NodeHealth string

const (
	NodeReady    NodeHealth = "ready"
	NodeNotReady NodeHealth = "not_ready"
	NodeUnknown  NodeHealth = "unknown"
)

// Operation 计划运维任务。
type Operation struct {
	ID              int64
	UUID            uuid.UUID
	ClusterID       int64
	NodeName        string // 空表示集群级操作
	OperationType   OperationType
	ScheduledAt     time.Time
	Status          OperationStatus
	ExecutedAt      *time.Time
	CompletedAt     *time.Time
	ErrorMessage    string
	NotifyAffected  bool   // 是否通知受影响应用参与人
	NotifiedUserIDs []int64
	domain.Audit
}

// NodeStatus 节点状态缓存（聚合自 K8s + Prometheus）。
type NodeStatus struct {
	ID                    int64
	UUID                  uuid.UUID
	ClusterID             int64
	NodeName              string
	Status                NodeHealth
	Unschedulable         bool
	KubeletVersion        string
	AllocatableCPUM       int
	AllocatableMemoryBytes int64
	AllocatableGPU        int
	UsedCPUM              int
	UsedMemoryBytes       int64
	UsedGPU               int
	PodCount              int
	AbnormalPodCount      int
	Roles                 []string
	Taints                []map[string]any
	Addresses             []map[string]any
	LastSyncedAt          *time.Time
	domain.Audit
}

// OperationQuery 运维任务查询。
type OperationQuery struct {
	ClusterID    int64
	Status       OperationStatus
	OperationType OperationType
	Offset       int
	Limit        int
}

// CreateOperationInput 创建运维任务输入。
type CreateOperationInput struct {
	ClusterID      int64
	NodeName       string
	OperationType  OperationType
	ScheduledAt    time.Time
	NotifyAffected bool
	CreatedBy      int64
}

// UpsertNodeStatusInput 节点状态 upsert 输入。
type UpsertNodeStatusInput struct {
	ClusterID             int64
	NodeName              string
	Status                NodeHealth
	Unschedulable         bool
	KubeletVersion        string
	AllocatableCPUM       int
	AllocatableMemoryBytes int64
	AllocatableGPU        int
	UsedCPUM              int
	UsedMemoryBytes       int64
	UsedGPU               int
	PodCount              int
	AbnormalPodCount      int
	Roles                 []string
	Taints                []map[string]any
	Addresses             []map[string]any
}

// 领域错误。
var (
	ErrOperationNotFound = errors.New("cluster operation not found")
	ErrNodeStatusNotFound = errors.New("cluster node status not found")
)

// Repository 集群运维仓储接口。
type Repository interface {
	// 运维任务
	CreateOperation(ctx context.Context, op *Operation) error
	GetOperation(ctx context.Context, id int64) (*Operation, error)
	UpdateOperation(ctx context.Context, op *Operation) error
	ListOperations(ctx context.Context, q OperationQuery) ([]*Operation, int64, error)
	// ListDueOperations 列出到期待执行的 pending 任务（scheduled_at <= before）。
	ListDueOperations(ctx context.Context, before time.Time, limit int) ([]*Operation, error)

	// 节点状态缓存
	UpsertNodeStatus(ctx context.Context, in UpsertNodeStatusInput) (*NodeStatus, error)
	ListNodeStatuses(ctx context.Context, clusterID int64) ([]*NodeStatus, error)
	DeleteNodeStatuses(ctx context.Context, clusterID int64, keep map[string]struct{}) error
}
