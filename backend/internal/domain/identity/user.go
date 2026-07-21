// Package identity 是身份与权限领域的根包。
// 领域层定义实体、值对象、领域错误与仓储接口，不依赖任何 IO 实现。
package identity

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// AuthSource 用户认证来源。
type AuthSource string

const (
	AuthSourceLocal AuthSource = "local"
	AuthSourceOIDC  AuthSource = "oidc"
	AuthSourceLDAP  AuthSource = "ldap"
)

// UserStatus 用户状态。
type UserStatus string

const (
	UserStatusActive   UserStatus = "active"
	UserStatusDisabled UserStatus = "disabled"
	UserStatusLocked   UserStatus = "locked"
)

// User 是用户领域实体。PasswordHash 仅在基础设施层流转，不对外暴露。
type User struct {
	ID                 int64
	UUID               uuid.UUID
	Username           string
	Email              string
	Phone              string
	DisplayName        string
	AvatarURL          string
	PasswordHash       string
	AuthSource         AuthSource
	ExternalID         string
	Status             UserStatus
	LastLoginAt        time.Time
	LastLoginIP        string
	PasswordChangedAt  time.Time
	MustChangePassword bool
	Locale             string
	Timezone           string
	Metadata           map[string]any
	// MFA (TOTP) 字段。MfaSecret 在基础设施层以 AES-256-GCM 密文存储与流转。
	MFAEnabled     bool
	MFASecret      string
	MFABackupCodes []string
	Version        int
	CreatedAt      time.Time
	CreatedBy      int64
	UpdatedAt      time.Time
	UpdatedBy      int64
}

// IsLocal 是否本地认证用户。
func (u *User) IsLocal() bool { return u.AuthSource == AuthSourceLocal }

// IsActive 是否处于 active 状态。
func (u *User) IsActive() bool { return u.Status == UserStatusActive }

// CanLogin 是否允许登录。
func (u *User) CanLogin() bool {
	return u.Status == UserStatusActive
}

// 领域错误。

var (
	// ErrUserNotFound 用户不存在。
	ErrUserNotFound = errors.New("user not found")
	// ErrUsernameExists 用户名已存在。
	ErrUsernameExists = errors.New("username already exists")
	// ErrEmailExists 邮箱已存在。
	ErrEmailExists = errors.New("email already exists")
	// ErrUserDisabled 用户已禁用。
	ErrUserDisabled = errors.New("user is disabled")
	// ErrUserLocked 用户已锁定。
	ErrUserLocked = errors.New("user is locked")
	// ErrInvalidCredentials 凭证无效。
	ErrInvalidCredentials = errors.New("invalid credentials")
	// ErrRefreshTokenNotFound 刷新令牌不存在。
	ErrRefreshTokenNotFound = errors.New("refresh token not found")
	// ErrRefreshTokenExpired 刷新令牌已过期。
	ErrRefreshTokenExpired = errors.New("refresh token expired")
	// ErrRefreshTokenRevoked 刷新令牌已撤销。
	ErrRefreshTokenRevoked = errors.New("refresh token revoked")
)
