// Package identityapp 是身份领域的应用服务层。
// 编排领域实体、仓储、安全组件，控制事务边界与领域错误转换。
package identityapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"crypto/rand"

	"github.com/google/uuid"
	"github.com/pquerna/otp/totp"

	"github.com/vortexops/vortexops/internal/config"
	"github.com/vortexops/vortexops/internal/domain/identity"
	"github.com/vortexops/vortexops/internal/platform/security"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// Service 是身份认证应用服务。
type Service struct {
	users       identity.UserRepository
	tokens      identity.RefreshTokenRepository
	hasher      *security.PasswordHasher
	jwt         *security.JWTIssuer
	cipher      *security.FieldCipher
	cfg         config.SecurityConfig
	// providers 登录方式注册表；nil 时仅支持默认 local 登录。
	providers *ProviderRegistry
}

// New 创建身份服务。cipher 用于加密 MFA 密钥，可为 nil（此时不启用 MFA 密钥持久化）。
func New(
	users identity.UserRepository,
	tokens identity.RefreshTokenRepository,
	hasher *security.PasswordHasher,
	jwt *security.JWTIssuer,
	cipher *security.FieldCipher,
	cfg config.SecurityConfig,
) *Service {
	return &Service{users: users, tokens: tokens, hasher: hasher, jwt: jwt, cipher: cipher, cfg: cfg}
}

// SetProviders 注入登录方式注册表。启动期由 server 装配调用；nil 表示仅默认 local 登录。
func (s *Service) SetProviders(reg *ProviderRegistry) {
	s.providers = reg
}

// Providers 返回登录方式注册表（可能为 nil）。
func (s *Service) Providers() *ProviderRegistry {
	return s.providers
}

// RegisterInput 注册请求。
type RegisterInput struct {
	Username    string
	Email       string
	Phone       string
	DisplayName string
	Password    string
	Locale      string
	Timezone    string
	CreatedBy   int64
}

// RegisterResult 注册结果。
type RegisterResult struct {
	User *identity.User
}

// Register 注册新用户（仅本地认证）。校验用户名/邮箱唯一性与密码强度。
func (s *Service) Register(ctx context.Context, in RegisterInput) (*RegisterResult, error) {
	username := strings.TrimSpace(in.Username)
	email := strings.TrimSpace(strings.ToLower(in.Email))
	if err := validateUsername(username); err != nil {
		return nil, err
	}
	if err := validateEmail(email); err != nil {
		return nil, err
	}
	if err := s.validatePassword(in.Password); err != nil {
		return nil, err
	}

	hash, err := s.hasher.Hash(in.Password)
	if err != nil {
		return nil, apperr.Internal("hash password", err)
	}

	user := &identity.User{
		UUID:        uuid.New(),
		Username:    username,
		Email:       email,
		Phone:       in.Phone,
		DisplayName: in.DisplayName,
		PasswordHash: hash,
		AuthSource:  identity.AuthSourceLocal,
		Status:      identity.UserStatusActive,
		Locale:      orDefault(in.Locale, "zh-CN"),
		Timezone:    orDefault(in.Timezone, "Asia/Shanghai"),
		Metadata:    map[string]any{},
		Version:     1,
		CreatedBy:   in.CreatedBy,
	}
	if err := s.users.Create(ctx, user); err != nil {
		if errors.Is(err, identity.ErrUsernameExists) {
			return nil, apperr.Conflict("username already exists", err)
		}
		if errors.Is(err, identity.ErrEmailExists) {
			return nil, apperr.Conflict("email already exists", err)
		}
		return nil, apperr.Internal("create user", err)
	}
	return &RegisterResult{User: user}, nil
}

// LoginInput 登录请求。
type LoginInput struct {
	UsernameOrEmail string
	Password        string
	IP              string
	UserAgent       string
	DeviceID        string
	DeviceName      string
}

// LoginResult 登录结果，含 access/refresh 令牌。
type LoginResult struct {
	AccessToken  string
	RefreshToken string
	AccessExp    time.Time
	RefreshExp   time.Time
	User         *identity.User
}

// MFAChallengeResult MFA 挑战结果（用户已启用 MFA 时登录第一步返回）。
type MFAChallengeResult struct {
	MFAToken string // 短期 MFA 挑战 token，供 LoginWithMFA 第二步使用
	UserID   int64
	Username string
}

// LoginOutcome 登录结果：要么直接登录成功（含令牌），要么需要 MFA 二次验证（含挑战）。
type LoginOutcome struct {
	Result    *LoginResult
	Challenge *MFAChallengeResult
}

// Login 校验凭证。若用户启用 MFA 则返回 MFA 挑战（不签发令牌）；否则直接签发令牌。
func (s *Service) Login(ctx context.Context, in LoginInput) (*LoginOutcome, error) {
	ident := strings.TrimSpace(in.UsernameOrEmail)
	if ident == "" || in.Password == "" {
		return nil, apperr.Validation("username and password are required", nil)
	}

	var user *identity.User
	var err error
	if strings.Contains(ident, "@") {
		user, err = s.users.GetByEmail(ctx, ident)
	} else {
		user, err = s.users.GetByUsername(ctx, ident)
	}
	if err != nil {
		if errors.Is(err, identity.ErrUserNotFound) {
			return nil, apperr.Unauthorized("invalid credentials", identity.ErrInvalidCredentials)
		}
		return nil, apperr.Internal("lookup user", err)
	}
	if !user.IsActive() {
		if user.Status == identity.UserStatusLocked {
			return nil, apperr.Unauthorized("user is locked", identity.ErrUserLocked)
		}
		return nil, apperr.Unauthorized("user is disabled", identity.ErrUserDisabled)
	}
	if !user.IsLocal() {
		return nil, apperr.Unauthorized("account uses external authentication, please login via SSO", nil)
	}

	if err := s.hasher.Compare(user.PasswordHash, in.Password); err != nil {
		return nil, apperr.Unauthorized("invalid credentials", identity.ErrInvalidCredentials)
	}

	// 启用 MFA 的用户返回挑战，不签发令牌。
	if user.MFAEnabled {
		mfaToken, _, err := s.jwt.IssueMFAToken(user.ID, user.Username)
		if err != nil {
			return nil, apperr.Internal("issue mfa challenge token", err)
		}
		return &LoginOutcome{Challenge: &MFAChallengeResult{
			MFAToken: mfaToken, UserID: user.ID, Username: user.Username,
		}}, nil
	}

	res, err := s.issueTokens(ctx, user, in.IP, in.DeviceID, in.DeviceName, in.UserAgent)
	if err != nil {
		return nil, err
	}
	return &LoginOutcome{Result: res}, nil
}

// LoginWithMFAInput MFA 二次验证登录请求。
type LoginWithMFAInput struct {
	MFAToken string
	Code     string
	IP       string
	UserAgent string
	DeviceID  string
	DeviceName string
}

// LoginWithMFA 校验 MFA 挑战 token 与 TOTP/备份码后签发令牌。
func (s *Service) LoginWithMFA(ctx context.Context, in LoginWithMFAInput) (*LoginResult, error) {
	claims, err := s.jwt.Parse(in.MFAToken)
	if err != nil {
		return nil, apperr.Unauthorized("invalid or expired MFA challenge token", err)
	}
	if claims.ID != "mfa_challenge" {
		return nil, apperr.Unauthorized("not an MFA challenge token", nil)
	}
	user, err := s.users.GetByID(ctx, claims.UserID)
	if err != nil {
		if errors.Is(err, identity.ErrUserNotFound) {
			return nil, apperr.Unauthorized("user not found", nil)
		}
		return nil, apperr.Internal("lookup user", err)
	}
	if !user.MFAEnabled {
		return nil, apperr.Unauthorized("MFA not enabled for this user", nil)
	}
	ok, err := s.verifyMFACode(user, in.Code)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, apperr.Unauthorized("invalid TOTP code or backup code", nil)
	}
	return s.issueTokens(ctx, user, in.IP, in.DeviceID, in.DeviceName, in.UserAgent)
}

// issueTokens 签发 access/refresh 令牌并更新最后登录信息。
func (s *Service) issueTokens(ctx context.Context, user *identity.User, ip, deviceID, deviceName, userAgent string) (*LoginResult, error) {
	access, accessExp, err := s.jwt.IssueAccessToken(user.ID, user.Username)
	if err != nil {
		return nil, apperr.Internal("issue access token", err)
	}
	refreshRaw, refreshExp, err := s.jwt.IssueRefreshToken(user.ID, user.Username)
	if err != nil {
		return nil, apperr.Internal("issue refresh token", err)
	}

	refreshHash := hashToken(refreshRaw)
	rt := &identity.RefreshToken{
		UserID:     user.ID,
		TokenHash:  refreshHash,
		DeviceID:   deviceID,
		DeviceName: deviceName,
		IP:         ip,
		UserAgent:  userAgent,
		ExpiresAt:  refreshExp,
	}
	if err := s.tokens.Create(ctx, rt); err != nil {
		return nil, apperr.Internal("persist refresh token", err)
	}

	if err := s.users.UpdateLastLogin(ctx, user.ID, time.Now().UTC(), ip); err != nil {
		_ = err
	}

	return &LoginResult{
		AccessToken:  access,
		RefreshToken: refreshRaw,
		AccessExp:    accessExp,
		RefreshExp:   refreshExp,
		User:         user,
	}, nil
}

// RefreshInput 刷新令牌请求。
type RefreshInput struct {
	RefreshToken string
	IP           string
	UserAgent    string
	DeviceID     string
	DeviceName   string
}

// RefreshResult 刷新结果。
type RefreshResult struct {
	AccessToken  string
	RefreshToken string
	AccessExp    time.Time
	RefreshExp   time.Time
}

// Refresh 用 refresh token 换取新的 access/refresh token（旋转刷新令牌）。
func (s *Service) Refresh(ctx context.Context, in RefreshInput) (*RefreshResult, error) {
	if in.RefreshToken == "" {
		return nil, apperr.Validation("refresh token is required", nil)
	}

	claims, err := s.jwt.Parse(in.RefreshToken)
	if err != nil {
		return nil, apperr.Unauthorized("invalid refresh token", err)
	}

	stored, err := s.tokens.GetByHash(ctx, hashToken(in.RefreshToken))
	if err != nil {
		if errors.Is(err, identity.ErrRefreshTokenNotFound) {
			return nil, apperr.Unauthorized("refresh token not recognized", err)
		}
		return nil, apperr.Internal("lookup refresh token", err)
	}
	if stored.IsRevoked() {
		return nil, apperr.Unauthorized("refresh token has been revoked", identity.ErrRefreshTokenRevoked)
	}
	if stored.IsExpired() {
		return nil, apperr.Unauthorized("refresh token expired", identity.ErrRefreshTokenExpired)
	}
	if stored.UserID != claims.UserID {
		return nil, apperr.Unauthorized("refresh token subject mismatch", nil)
	}

	// 旋转：撤销旧令牌，签发新令牌。
	if err := s.tokens.Revoke(ctx, stored.ID); err != nil {
		return nil, apperr.Internal("revoke old refresh token", err)
	}

	user, err := s.users.GetByID(ctx, claims.UserID)
	if err != nil {
		if errors.Is(err, identity.ErrUserNotFound) {
			return nil, apperr.Unauthorized("user no longer exists", err)
		}
		return nil, apperr.Internal("lookup user", err)
	}
	if !user.IsActive() {
		return nil, apperr.Unauthorized("user is not active", nil)
	}

	access, accessExp, err := s.jwt.IssueAccessToken(user.ID, user.Username)
	if err != nil {
		return nil, apperr.Internal("issue access token", err)
	}
	refreshRaw, refreshExp, err := s.jwt.IssueRefreshToken(user.ID, user.Username)
	if err != nil {
		return nil, apperr.Internal("issue refresh token", err)
	}
	newRT := &identity.RefreshToken{
		UserID:     user.ID,
		TokenHash:  hashToken(refreshRaw),
		DeviceID:   in.DeviceID,
		DeviceName: in.DeviceName,
		IP:         in.IP,
		UserAgent:  in.UserAgent,
		ExpiresAt:  refreshExp,
	}
	if err := s.tokens.Create(ctx, newRT); err != nil {
		return nil, apperr.Internal("persist new refresh token", err)
	}

	return &RefreshResult{
		AccessToken:  access,
		RefreshToken: refreshRaw,
		AccessExp:    accessExp,
		RefreshExp:   refreshExp,
	}, nil
}

// Logout 撤销指定刷新令牌。
func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	if refreshToken == "" {
		return nil
	}
	stored, err := s.tokens.GetByHash(ctx, hashToken(refreshToken))
	if err != nil {
		if errors.Is(err, identity.ErrRefreshTokenNotFound) {
			return nil
		}
		return apperr.Internal("lookup refresh token", err)
	}
	if err := s.tokens.Revoke(ctx, stored.ID); err != nil {
		if errors.Is(err, identity.ErrRefreshTokenNotFound) {
			return nil
		}
		return apperr.Internal("revoke refresh token", err)
	}
	return nil
}

// LogoutAll 撤销某用户全部刷新令牌。
func (s *Service) LogoutAll(ctx context.Context, userID int64) error {
	if err := s.tokens.RevokeAllForUser(ctx, userID); err != nil {
		return apperr.Internal("revoke all refresh tokens", err)
	}
	return nil
}

// ChangePasswordInput 修改密码请求。
type ChangePasswordInput struct {
	UserID      int64
	OldPassword string
	NewPassword string
}

// ChangePassword 校验旧密码后更新。
func (s *Service) ChangePassword(ctx context.Context, in ChangePasswordInput) error {
	if err := s.validatePassword(in.NewPassword); err != nil {
		return err
	}
	if in.OldPassword == in.NewPassword {
		return apperr.Validation("new password must differ from old password", nil)
	}
	user, err := s.users.GetByID(ctx, in.UserID)
	if err != nil {
		if errors.Is(err, identity.ErrUserNotFound) {
			return apperr.NotFound("user", fmt.Sprintf("%d", in.UserID))
		}
		return apperr.Internal("lookup user", err)
	}
	if err := s.hasher.Compare(user.PasswordHash, in.OldPassword); err != nil {
		return apperr.Unauthorized("old password is incorrect", identity.ErrInvalidCredentials)
	}
	hash, err := s.hasher.Hash(in.NewPassword)
	if err != nil {
		return apperr.Internal("hash password", err)
	}
	user.PasswordHash = hash
	user.PasswordChangedAt = time.Now().UTC()
	user.MustChangePassword = false
	user.UpdatedBy = in.UserID
	if err := s.users.Update(ctx, user); err != nil {
		return apperr.Internal("update user", err)
	}
	// 改密后撤销全部已有会话。
	if err := s.tokens.RevokeAllForUser(ctx, in.UserID); err != nil {
		_ = err
	}
	return nil
}

// GetByID 获取当前用户信息。
func (s *Service) GetByID(ctx context.Context, id int64) (*identity.User, error) {
	u, err := s.users.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, identity.ErrUserNotFound) {
			return nil, apperr.NotFound("user", fmt.Sprintf("%d", id))
		}
		return nil, apperr.Internal("lookup user", err)
	}
	return u, nil
}

// ListUsersInput 管理员列出用户请求。
type ListUsersInput struct {
	Search string
	Status string
	Page   int
	Size   int
}

// ListUsers 分页列出用户（管理员）。
func (s *Service) ListUsers(ctx context.Context, in ListUsersInput) ([]*identity.User, int64, error) {
	if in.Size <= 0 {
		in.Size = 20
	}
	if in.Page <= 0 {
		in.Page = 1
	}
	users, total, err := s.users.List(ctx, identity.UserQuery{
		Search: in.Search, Status: identity.UserStatus(in.Status),
		Offset: (in.Page - 1) * in.Size, Limit: in.Size,
	})
	if err != nil {
		return nil, 0, apperr.Internal("list users", err)
	}
	return users, total, nil
}

// CreateUserInput 管理员创建用户请求。
type CreateUserInput struct {
	Username    string
	Email       string
	Phone       string
	DisplayName string
	Password    string
	Locale      string
	Timezone    string
	CreatedBy   int64
}

// CreateUser 管理员创建用户（与 Register 同逻辑，但 CreatedBy 为管理员 ID）。
func (s *Service) CreateUser(ctx context.Context, in CreateUserInput) (*identity.User, error) {
	res, err := s.Register(ctx, RegisterInput{
		Username: in.Username, Email: in.Email, Phone: in.Phone, DisplayName: in.DisplayName,
		Password: in.Password, Locale: in.Locale, Timezone: in.Timezone, CreatedBy: in.CreatedBy,
	})
	if err != nil {
		return nil, err
	}
	return res.User, nil
}

// UpdateUserStatusInput 更新用户状态请求。
type UpdateUserStatusInput struct {
	UserID   int64
	Status   identity.UserStatus
	ActorID  int64
}

// UpdateUserStatus 管理员更新用户状态（active/disabled/locked）。
func (s *Service) UpdateUserStatus(ctx context.Context, in UpdateUserStatusInput) error {
	if in.Status != identity.UserStatusActive && in.Status != identity.UserStatusDisabled && in.Status != identity.UserStatusLocked {
		return apperr.Validation("invalid status, must be active/disabled/locked", nil)
	}
	u, err := s.users.GetByID(ctx, in.UserID)
	if err != nil {
		if errors.Is(err, identity.ErrUserNotFound) {
			return apperr.NotFound("user", fmt.Sprintf("%d", in.UserID))
		}
		return apperr.Internal("lookup user", err)
	}
	u.Status = in.Status
	u.UpdatedBy = in.ActorID
	if err := s.users.Update(ctx, u); err != nil {
		return apperr.Internal("update user status", err)
	}
	// 禁用/锁定时撤销全部已有会话。
	if in.Status != identity.UserStatusActive {
		_ = s.tokens.RevokeAllForUser(ctx, in.UserID)
	}
	return nil
}

// UpdateUserInput 管理员更新用户信息（全量可编辑字段，指针为 nil 表示不修改）。
// 不允许修改 username 与 auth_source（身份标识不可变更）；密码通过 ResetPassword 单独修改。
type UpdateUserInput struct {
	UserID      int64
	Email       *string
	Phone       *string
	DisplayName *string
	Locale      *string
	Timezone    *string
	Status      *identity.UserStatus
	Version     int
	ActorID     int64
}

// UpdateUser 管理员更新用户资料。乐观锁：version 不匹配返回 Conflict。
func (s *Service) UpdateUser(ctx context.Context, in UpdateUserInput) (*identity.User, error) {
	u, err := s.users.GetByID(ctx, in.UserID)
	if err != nil {
		if errors.Is(err, identity.ErrUserNotFound) {
			return nil, apperr.NotFound("user", fmt.Sprintf("%d", in.UserID))
		}
		return nil, apperr.Internal("lookup user", err)
	}
	if u.Version != in.Version {
		return nil, apperr.Conflict("user was modified concurrently, please refresh", nil)
	}
	if in.Email != nil {
		email := strings.TrimSpace(strings.ToLower(*in.Email))
		if err := validateEmail(email); err != nil {
			return nil, err
		}
		u.Email = email
	}
	if in.Phone != nil {
		u.Phone = strings.TrimSpace(*in.Phone)
	}
	if in.DisplayName != nil {
		u.DisplayName = strings.TrimSpace(*in.DisplayName)
	}
	if in.Locale != nil {
		u.Locale = *in.Locale
	}
	if in.Timezone != nil {
		u.Timezone = *in.Timezone
	}
	prevStatus := u.Status
	if in.Status != nil {
		if *in.Status != identity.UserStatusActive && *in.Status != identity.UserStatusDisabled && *in.Status != identity.UserStatusLocked {
			return nil, apperr.Validation("invalid status, must be active/disabled/locked", nil)
		}
		u.Status = *in.Status
	}
	u.UpdatedBy = in.ActorID
	if err := s.users.Update(ctx, u); err != nil {
		if errors.Is(err, identity.ErrUserNotFound) {
			return nil, apperr.NotFound("user", fmt.Sprintf("%d", in.UserID))
		}
		if errors.Is(err, identity.ErrEmailExists) {
			return nil, apperr.Conflict("email already exists", err)
		}
		return nil, apperr.Internal("update user", err)
	}
	// 状态从 active 切换到非 active 时撤销会话。
	if prevStatus == identity.UserStatusActive && u.Status != identity.UserStatusActive {
		_ = s.tokens.RevokeAllForUser(ctx, in.UserID)
	}
	return u, nil
}

// ResetPasswordInput 管理员重置用户密码。
type ResetPasswordInput struct {
	UserID          int64
	NewPassword     string
	MustChangePassword *bool // 是否强制用户下次登录改密；nil 表示不修改。
	ActorID         int64
}

// ResetPassword 管理员重置用户密码。仅 local 认证用户可重置。
func (s *Service) ResetPassword(ctx context.Context, in ResetPasswordInput) error {
	if err := s.validatePassword(in.NewPassword); err != nil {
		return err
	}
	u, err := s.users.GetByID(ctx, in.UserID)
	if err != nil {
		if errors.Is(err, identity.ErrUserNotFound) {
			return apperr.NotFound("user", fmt.Sprintf("%d", in.UserID))
		}
		return apperr.Internal("lookup user", err)
	}
	if !u.IsLocal() {
		return apperr.BusinessRule("cannot reset password for non-local account", nil)
	}
	hash, err := s.hasher.Hash(in.NewPassword)
	if err != nil {
		return apperr.Internal("hash password", err)
	}
	u.PasswordHash = hash
	u.PasswordChangedAt = time.Now().UTC()
	if in.MustChangePassword != nil {
		u.MustChangePassword = *in.MustChangePassword
	}
	u.UpdatedBy = in.ActorID
	if err := s.users.Update(ctx, u); err != nil {
		if errors.Is(err, identity.ErrUserNotFound) {
			return apperr.NotFound("user", fmt.Sprintf("%d", in.UserID))
		}
		return apperr.Internal("reset password", err)
	}
	// 重置密码后撤销全部会话，强制重新登录。
	_ = s.tokens.RevokeAllForUser(ctx, in.UserID)
	return nil
}

// DeleteUser 管理员软删除用户。
func (s *Service) DeleteUser(ctx context.Context, userID, actorID int64) error {
	if userID == actorID {
		return apperr.Validation("cannot delete yourself", nil)
	}
	if err := s.users.Delete(ctx, userID, actorID); err != nil {
		if errors.Is(err, identity.ErrUserNotFound) {
			return apperr.NotFound("user", fmt.Sprintf("%d", userID))
		}
		return apperr.Internal("delete user", err)
	}
	_ = s.tokens.RevokeAllForUser(ctx, userID)
	return nil
}

// ============================================================================
// MFA (TOTP) 两步验证
// ============================================================================

const (
	mfaIssuerName = "VortexOps"
	mfaBackupCount = 10
)

// MFAGenerateResult 生成 TOTP 密钥的结果。
type MFAGenerateResult struct {
	Secret    string   // Base32 密钥（明文，仅此次返回前端）
	OTPAuthURL string  // otpauth:// 协议 URL（供二维码生成）
	BackupCodes []string // 一次性备份码（明文，仅此次返回前端）
}

// GenerateMFA 为用户生成新的 TOTP 密钥与备份码（不立即启用，需 Verify 后启用）。
// 生成的 secret 加密后暂存于 user.MFASecret，但 MFAEnabled 仍为 false 直到 EnableMFA。
func (s *Service) GenerateMFA(ctx context.Context, userID int64) (*MFAGenerateResult, error) {
	if s.cipher == nil {
		return nil, apperr.Internal("MFA cipher not configured", nil)
	}
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		if errors.Is(err, identity.ErrUserNotFound) {
			return nil, apperr.NotFound("user", fmt.Sprintf("%d", userID))
		}
		return nil, apperr.Internal("lookup user", err)
	}
	if u.MFAEnabled {
		return nil, apperr.Conflict("MFA already enabled; disable first to regenerate", nil)
	}

	key, err := totp.Generate(totp.GenerateOpts{
		Issuer: mfaIssuerName, AccountName: u.Email,
	})
	if err != nil {
		return nil, apperr.Internal("generate totp key", err)
	}

	encrypted, err := s.cipher.EncryptString(key.Secret())
	if err != nil {
		return nil, apperr.Internal("encrypt mfa secret", err)
	}

	backupCodes := generateBackupCodes(mfaBackupCount)
	hashedBackups := make([]string, 0, len(backupCodes))
	for _, c := range backupCodes {
		h, _ := s.hasher.Hash(c)
		hashedBackups = append(hashedBackups, h)
	}

	u.MFASecret = encrypted
	u.MFABackupCodes = hashedBackups
	u.UpdatedBy = userID
	if err := s.users.Update(ctx, u); err != nil {
		return nil, apperr.Internal("persist mfa secret", err)
	}

	return &MFAGenerateResult{
		Secret: key.Secret(), OTPAuthURL: key.URL(), BackupCodes: backupCodes,
	}, nil
}

// EnableMFAInput 启用 MFA 请求。
type EnableMFAInput struct {
	UserID int64
	Code   string
}

// EnableMFA 校验首此 TOTP 码后正式启用 MFA。
func (s *Service) EnableMFA(ctx context.Context, in EnableMFAInput) error {
	if s.cipher == nil {
		return apperr.Internal("MFA cipher not configured", nil)
	}
	u, err := s.users.GetByID(ctx, in.UserID)
	if err != nil {
		if errors.Is(err, identity.ErrUserNotFound) {
			return apperr.NotFound("user", fmt.Sprintf("%d", in.UserID))
		}
		return apperr.Internal("lookup user", err)
	}
	if u.MFAEnabled {
		return apperr.Conflict("MFA already enabled", nil)
	}
	if u.MFASecret == "" {
		return apperr.Validation("no pending MFA secret; call generate first", nil)
	}
	secret, err := s.cipher.DecryptString(u.MFASecret)
	if err != nil {
		return apperr.Internal("decrypt mfa secret", err)
	}
	if !totp.Validate(in.Code, secret) {
		return apperr.Unauthorized("invalid TOTP code", nil)
	}
	u.MFAEnabled = true
	u.UpdatedBy = in.UserID
	if err := s.users.Update(ctx, u); err != nil {
		return apperr.Internal("enable mfa", err)
	}
	return nil
}

// DisableMFAInput 禁用 MFA 请求。
type DisableMFAInput struct {
	UserID  int64
	Code    string // TOTP 码或备份码
	UsePassword bool // 若 true 则 Code 为密码（用于已丢失设备的禁用流程）
}

// DisableMFA 禁用 MFA，需校验 TOTP 码 / 备份码 / 密码之一。
func (s *Service) DisableMFA(ctx context.Context, in DisableMFAInput) error {
	if s.cipher == nil {
		return apperr.Internal("MFA cipher not configured", nil)
	}
	u, err := s.users.GetByID(ctx, in.UserID)
	if err != nil {
		if errors.Is(err, identity.ErrUserNotFound) {
			return apperr.NotFound("user", fmt.Sprintf("%d", in.UserID))
		}
		return apperr.Internal("lookup user", err)
	}
	if !u.MFAEnabled {
		return apperr.Conflict("MFA not enabled", nil)
	}

	if in.UsePassword {
		if err := s.hasher.Compare(u.PasswordHash, in.Code); err != nil {
			return apperr.Unauthorized("password is incorrect", identity.ErrInvalidCredentials)
		}
	} else {
		ok, err := s.verifyMFACode(u, in.Code)
		if err != nil {
			return err
		}
		if !ok {
			return apperr.Unauthorized("invalid TOTP code or backup code", nil)
		}
	}

	u.MFAEnabled = false
	u.MFASecret = ""
	u.MFABackupCodes = []string{}
	u.UpdatedBy = in.UserID
	if err := s.users.Update(ctx, u); err != nil {
		return apperr.Internal("disable mfa", err)
	}
	return nil
}

// VerifyMFAInput 校验 MFA 码请求。
type VerifyMFAInput struct {
	UserID int64
	Code   string
}

// VerifyMFA 校验 TOTP 码或备份码（备份码使用后即失效）。
func (s *Service) VerifyMFA(ctx context.Context, in VerifyMFAInput) (bool, error) {
	u, err := s.users.GetByID(ctx, in.UserID)
	if err != nil {
		if errors.Is(err, identity.ErrUserNotFound) {
			return false, apperr.NotFound("user", fmt.Sprintf("%d", in.UserID))
		}
		return false, apperr.Internal("lookup user", err)
	}
	if !u.MFAEnabled {
		return false, apperr.Conflict("MFA not enabled", nil)
	}
	ok, err := s.verifyMFACode(u, in.Code)
	if err != nil {
		return false, err
	}
	return ok, nil
}

// verifyMFACode 校验 TOTP 码或备份码（备份码命中则从列表移除并持久化）。
func (s *Service) verifyMFACode(u *identity.User, code string) (bool, error) {
	// 1. 先尝试 TOTP 码。
	if u.MFASecret != "" {
		secret, err := s.cipher.DecryptString(u.MFASecret)
		if err != nil {
			return false, apperr.Internal("decrypt mfa secret", err)
		}
		if totp.Validate(code, secret) {
			return true, nil
		}
	}
	// 2. 再尝试备份码（bcrypt 比对，命中即消费）。
	for i, hashed := range u.MFABackupCodes {
		if s.hasher.Compare(hashed, code) == nil {
			// 移除已用备份码。
			u.MFABackupCodes = append(u.MFABackupCodes[:i], u.MFABackupCodes[i+1:]...)
			u.UpdatedBy = u.ID
			if err := s.users.Update(context.Background(), u); err != nil {
				return false, apperr.Internal("consume backup code", err)
			}
			return true, nil
		}
	}
	return false, nil
}

// generateBackupCodes 生成 n 个 8 位字母数字备份码（含连字符分组，如 AB3X-K9P2）。
func generateBackupCodes(n int) []string {
	codes := make([]string, 0, n)
	for i := 0; i < n; i++ {
		b := make([]byte, 8)
		_, _ = rand.Read(b)
		const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
		for j := range b {
			b[j] = alphabet[int(b[j])%len(alphabet)]
		}
		codes = append(codes, string(b[:4])+"-"+string(b[4:]))
	}
	return codes
}

func (s *Service) validatePassword(pw string) error {
	if len(pw) < s.cfg.PasswordMinLength {
		return apperr.Validation(fmt.Sprintf("password must be at least %d characters", s.cfg.PasswordMinLength), nil)
	}
	if len(pw) > 128 {
		return apperr.Validation("password must be at most 128 characters", nil)
	}
	return nil
}

func validateUsername(u string) error {
	if len(u) < 3 || len(u) > 64 {
		return apperr.Validation("username must be 3-64 characters", nil)
	}
	for _, c := range u {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-' || c == '.') {
			return apperr.Validation("username may only contain letters, digits, '_', '-', '.'", nil)
		}
	}
	return nil
}

func validateEmail(e string) error {
	if e == "" {
		return apperr.Validation("email is required", nil)
	}
	at := strings.IndexByte(e, '@')
	if at <= 0 || at == len(e)-1 {
		return apperr.Validation("email is invalid", nil)
	}
	if !strings.Contains(e[at+1:], ".") {
		return apperr.Validation("email is invalid", nil)
	}
	return nil
}

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// ClientIPFromRequest 从 RemoteAddr 提取客户端 IP（去除端口）。
func ClientIPFromRequest(remoteAddr string) string {
	if remoteAddr == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}
