// Package bastion 是堡垒机领域的核心实体与仓储接口。
// 与 JumpServer 集成：资产同步、会话录像、SSO 连接 URL 签发。
package bastion

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

type Protocol string

const (
	ProtocolSSH Protocol = "ssh"
	ProtocolRDP Protocol = "rdp"
)

type AssetStatus string

const (
	SessionActive AssetStatus = "active"
	SessionClosed AssetStatus = "closed"
)

// Asset 堡垒机资产（平台侧记录，与 JumpServer 同步）。
type Asset struct {
	ID           int64
	UUID         uuid.UUID
	WorkspaceID  int64
	Name         string
	Host         string
	Port         int
	Protocol     Protocol
	Platform     string
	Username     string
	CredentialID int64
	JMSAssetID   string
	JMSOrgID     string
	Tags         []string
	Comment      string
	IsActive     bool
	Version      int
	CreatedAt    time.Time
	CreatedBy    int64
	UpdatedAt    time.Time
	UpdatedBy    int64
}

// Session 堡垒机会话（同步自 JumpServer）。
type Session struct {
	ID            int64
	UUID          uuid.UUID
	WorkspaceID   int64
	AssetID       int64
	JMSSessionID  string
	UserID        int64
	Username      string
	AssetName     string
	Protocol      Protocol
	RemoteAddr    string
	LoginFrom     string
	Status        AssetStatus
	StartedAt     *time.Time
	EndedAt       *time.Time
	DurationMs    int64
	ReplayURL     string
	CommandCount  int
	Version       int
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// CreateAssetInput 创建资产输入。
type CreateAssetInput struct {
	WorkspaceID int64
	Name        string
	Host        string
	Port        int
	Protocol    Protocol
	Platform    string
	Username    string
	CredentialID int64
	Tags        []string
	Comment     string
	CreatedBy   int64
}

// AssetQuery 资产查询。
type AssetQuery struct {
	WorkspaceID int64
	Protocol    Protocol
	Search      string
	Offset      int
	Limit       int
}

// SessionQuery 会话查询。
type SessionQuery struct {
	WorkspaceID int64
	AssetID     int64
	UserID      int64
	Status      AssetStatus
	Offset      int
	Limit       int
}

var (
	ErrAssetNotFound   = errors.New("bastion asset not found")
	ErrSessionNotFound = errors.New("bastion session not found")
)

// Repository 堡垒机仓储接口。
type Repository interface {
	// 资产
	CreateAsset(ctx context.Context, a *Asset) error
	GetAssetByID(ctx context.Context, id int64) (*Asset, error)
	ListAssets(ctx context.Context, q AssetQuery) ([]*Asset, int64, error)
	UpdateAsset(ctx context.Context, a *Asset) error
	DeleteAsset(ctx context.Context, id, actorID int64) error

	// 会话
	CreateSession(ctx context.Context, s *Session) error
	GetSessionByID(ctx context.Context, id int64) (*Session, error)
	ListSessions(ctx context.Context, q SessionQuery) ([]*Session, int64, error)
	UpdateSession(ctx context.Context, s *Session) error
}
