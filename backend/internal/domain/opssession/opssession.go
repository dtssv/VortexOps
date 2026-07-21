// Package opssession 是运维会话（Pod WebSSH / 端口转发）的领域模型与仓储接口。
package opssession

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Type 会话类型。
type Type string

const (
	TypeExec        Type = "exec"
	TypePortForward Type = "portforward"
)

// Status 会话状态。
type Status string

const (
	StatusActive Status = "active"
	StatusClosed Status = "closed"
)

// Session 运维会话元数据。
type Session struct {
	ID            int64
	UUID          uuid.UUID
	WorkspaceID   int64
	ClusterID     int64
	Namespace     string
	Pod           string
	Container     string
	Type          Type
	Status        Status
	UserID        int64
	UserName      string
	ClientIP      string
	RecordingKey  string // MinIO 对象 key（asciinema cast）
	StartedAt     time.Time
	EndedAt       *time.Time
	DurationMs    int64
	Version       int
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// CreateInput 创建会话输入。
type CreateInput struct {
	WorkspaceID int64
	ClusterID   int64
	Namespace   string
	Pod         string
	Container   string
	Type        Type
	UserID      int64
	UserName    string
	ClientIP    string
}

// Query 会话查询。
type Query struct {
	WorkspaceID int64
	ClusterID   int64
	UserID      int64
	Type        Type
	Status      Status
	Offset      int
	Limit       int
}

var ErrSessionNotFound = errors.New("ops session not found")

// Repository 运维会话仓储接口。
type Repository interface {
	Create(ctx context.Context, s *Session) error
	GetByID(ctx context.Context, id int64) (*Session, error)
	List(ctx context.Context, q Query) ([]*Session, int64, error)
	Update(ctx context.Context, s *Session) error
}
