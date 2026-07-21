package identity

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// RefreshToken 是刷新令牌实体。TokenHash 存储令牌的 SHA-256 哈希，明文不落库。
type RefreshToken struct {
	ID         int64
	UserID     int64
	TokenHash  string
	DeviceID   string
	DeviceName string
	IP         string
	UserAgent  string
	ExpiresAt  time.Time
	RevokedAt  *time.Time
	CreatedAt  time.Time
}

// IsExpired 是否已过期。
func (t *RefreshToken) IsExpired() bool {
	return !time.Now().Before(t.ExpiresAt)
}

// IsRevoked 是否已撤销。
func (t *RefreshToken) IsRevoked() bool {
	return t.RevokedAt != nil
}

// IsValid 是否有效（未过期且未撤销）。
func (t *RefreshToken) IsValid() bool {
	return !t.IsExpired() && !t.IsRevoked()
}

// CreateUserInput 创建用户的输入。
type CreateUserInput struct {
	Username     string
	Email        string
	Phone        string
	DisplayName  string
	Password     string
	AuthSource   AuthSource
	ExternalID   string
	Locale       string
	Timezone     string
	CreatedBy    int64
}

// UserQuery 查询用户的过滤条件。
type UserQuery struct {
	Username string
	Email    string
	Status   UserStatus
	Search   string
	Offset   int
	Limit    int
}

// UserRepository 用户仓储接口，由 infrastructure/postgres 实现。
type UserRepository interface {
	Create(ctx context.Context, u *User) error
	GetByID(ctx context.Context, id int64) (*User, error)
	GetByUUID(ctx context.Context, id uuid.UUID) (*User, error)
	GetByUsername(ctx context.Context, username string) (*User, error)
	GetByEmail(ctx context.Context, email string) (*User, error)
	// GetByExternalID 按认证来源与外部 ID 查询用户（SSO 登录回查）。
	// 找不到时返回 ErrUserNotFound。
	GetByExternalID(ctx context.Context, source AuthSource, externalID string) (*User, error)
	Update(ctx context.Context, u *User) error
	UpdateLastLogin(ctx context.Context, userID int64, at time.Time, ip string) error
	List(ctx context.Context, q UserQuery) ([]*User, int64, error)
	Delete(ctx context.Context, id int64, deletedBy int64) error
}

// RefreshTokenRepository 刷新令牌仓储接口。
type RefreshTokenRepository interface {
	Create(ctx context.Context, t *RefreshToken) error
	GetByHash(ctx context.Context, hash string) (*RefreshToken, error)
	Revoke(ctx context.Context, id int64) error
	RevokeAllForUser(ctx context.Context, userID int64) error
	RevokeExpired(ctx context.Context) (int64, error)
}
