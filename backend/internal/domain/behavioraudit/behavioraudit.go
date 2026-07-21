// Package behavioraudit 是 WebSSH 行为审计领域。
package behavioraudit

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// RiskLevel 命令风险级别。
type RiskLevel string

const (
	RiskInfo    RiskLevel = "info"
	RiskWarn    RiskLevel = "warn"
	RiskDanger  RiskLevel = "danger"
)

// Log 行为审计条目（WebSSH 捕获的命令行）。
type Log struct {
	ID          int64
	UUID        uuid.UUID
	WorkspaceID int64
	SessionID   int64
	ClusterID   int64
	Namespace   string
	Pod         string
	UserID      int64
	UserName    string
	Command     string
	RiskLevel   RiskLevel
	CreatedAt   time.Time
}

// Repository 行为审计仓储。
type Repository interface {
	Append(ctx context.Context, l *Log) error
	List(ctx context.Context, q Query) ([]*Log, int64, error)
}

// Query 行为审计查询。
type Query struct {
	WorkspaceID int64
	SessionID   int64
	UserID      int64
	ClusterID   int64
	Offset      int
	Limit       int
}
