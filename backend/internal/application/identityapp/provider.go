package identityapp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/vortexops/vortexops/internal/domain/identity"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// AuthProvider 抽象一种登录方式（local 密码、OIDC、LDAP……）。
//
// 设计目标：登录流程可扩展。新增登录方式时实现此接口并通过 ProviderRegistry 注册，
// 无需改动 Login/ExternalLogin 主流程。每种 provider 对应 vo_users.auth_source 的一个枚举值。
//
//  - LocalProvider：用户名/密码（默认），auth_source=local。
//  - 外部 provider（OIDC/LDAP）：通过 ExternalLogin 入口，登录后基于外部身份信息
//    创建或更新 vo_users 行（auth_source=oidc/ldap，external_id=provider 返回的唯一 ID）。
type AuthProvider interface {
	// Source 返回此 provider 对应的 vo_users.auth_source 值（local/oidc/ldap...）。
	Source() identity.AuthSource
	// Code 返回 provider 标识码（如 "local"、"oidc-keycloak"），用于路由与配置。
	Code() string
	// DisplayName 返回前端展示名（如"默认账号密码"、"企业 SSO"）。
	DisplayName() string
	// IsExternal 是否为外部认证（OIDC/LDAP）；local 为 false。
	IsExternal() bool
}

// LocalProvider 默认账号密码登录方式。封装既有 Login 流程的密码校验语义。
// 实际的凭证校验仍由 Service.Login 调用 hasher 完成；此类型仅作为 provider 元数据载体，
// 便于统一注册与前端展示。
type LocalProvider struct {
	code        string
	displayName string
}

// NewLocalProvider 创建默认登录 provider。code/displayName 可通过系统设置覆盖（"自定义"命名）。
func NewLocalProvider(code, displayName string) *LocalProvider {
	if code == "" {
		code = "local"
	}
	if displayName == "" {
		displayName = "默认账号密码"
	}
	return &LocalProvider{code: code, displayName: displayName}
}

func (p *LocalProvider) Source() identity.AuthSource { return identity.AuthSourceLocal }
func (p *LocalProvider) Code() string                { return p.code }
func (p *LocalProvider) DisplayName() string         { return p.displayName }
func (p *LocalProvider) IsExternal() bool            { return false }

// ExternalUserInfo 外部认证 provider 返回的标准化用户信息。
// ExternalLogin 据此查找/创建/更新 vo_users 行，实现"登录后基于登录信息写入用户表"。
type ExternalUserInfo struct {
	Source      identity.AuthSource // oidc / ldap
	ExternalID  string              // provider 侧唯一 ID（必须非空）
	Username    string              // 建议用户名（可被 sanitize）
	Email       string
	DisplayName string
	Phone       string
	// RawAttrs 原始属性，便于审计或扩展（写入 user.Metadata）。
	RawAttrs map[string]any
}

// ProviderRegistry 登录方式注册表。线程安全只读使用（注册在启动期完成）。
type ProviderRegistry struct {
	providers []AuthProvider
	byCode    map[string]AuthProvider
	bySource  map[identity.AuthSource]AuthProvider
}

// NewProviderRegistry 创建空注册表。
func NewProviderRegistry() *ProviderRegistry {
	return &ProviderRegistry{byCode: map[string]AuthProvider{}, bySource: map[identity.AuthSource]AuthProvider{}}
}

// Register 注册一个 provider。同 code/source 重复注册返回错误。
func (r *ProviderRegistry) Register(p AuthProvider) error {
	if p == nil {
		return errors.New("nil provider")
	}
	if _, ok := r.byCode[p.Code()]; ok {
		return fmt.Errorf("provider code %q already registered", p.Code())
	}
	if _, ok := r.bySource[p.Source()]; ok {
		return fmt.Errorf("provider source %q already registered", p.Source())
	}
	r.providers = append(r.providers, p)
	r.byCode[p.Code()] = p
	r.bySource[p.Source()] = p
	return nil
}

// Get 按 code 查找 provider。
func (r *ProviderRegistry) Get(code string) (AuthProvider, bool) {
	p, ok := r.byCode[code]
	return p, ok
}

// GetBySource 按 auth_source 查找 provider。
func (r *ProviderRegistry) GetBySource(source identity.AuthSource) (AuthProvider, bool) {
	p, ok := r.bySource[source]
	return p, ok
}

// All 返回全部已注册 provider（用于前端展示可选登录方式）。
func (r *ProviderRegistry) All() []AuthProvider {
	out := make([]AuthProvider, len(r.providers))
	copy(out, r.providers)
	return out
}

// DefaultProvider 返回默认（local）provider；未注册返回 nil。
func (r *ProviderRegistry) DefaultProvider() AuthProvider {
	if p, ok := r.bySource[identity.AuthSourceLocal]; ok {
		return p
	}
	return nil
}

// LoginProviderDTO 登录方式展示信息（前端登录页渲染可选登录方式）。
type LoginProviderDTO struct {
	Code        string `json:"code"`
	Source      string `json:"source"`
	DisplayName string `json:"display_name"`
	IsExternal  bool   `json:"is_external"`
	IsDefault   bool   `json:"is_default"`
}

// ListLoginProviders 返回全部已注册登录方式。未配置注册表时返回默认 local 一项。
func (s *Service) ListLoginProviders() []LoginProviderDTO {
	if s.providers == nil {
		return []LoginProviderDTO{{
			Code: "local", Source: string(identity.AuthSourceLocal),
			DisplayName: "默认账号密码", IsExternal: false, IsDefault: true,
		}}
	}
	all := s.providers.All()
	out := make([]LoginProviderDTO, 0, len(all))
	for _, p := range all {
		out = append(out, LoginProviderDTO{
			Code: p.Code(), Source: string(p.Source()),
			DisplayName: p.DisplayName(), IsExternal: p.IsExternal(),
			IsDefault: p.Source() == identity.AuthSourceLocal,
		})
	}
	return out
}

// ExternalLoginInput 外部登录请求。Provider 已在外部完成身份认证（如 OIDC 回调已换 token），
// 此处仅传入标准化的 ExternalUserInfo。
type ExternalLoginInput struct {
	Info       ExternalUserInfo
	IP         string
	UserAgent  string
	DeviceID   string
	DeviceName string
}

// ExternalLogin 基于 external 身份信息登录：查找已有用户，不存在则创建；
// 存在则更新展示信息。然后签发令牌。实现"登录后基于登录信息写入用户表"。
func (s *Service) ExternalLogin(ctx context.Context, in ExternalLoginInput) (*LoginResult, error) {
	info := in.Info
	if info.Source == identity.AuthSourceLocal {
		return nil, apperr.Validation("external login requires non-local source", nil)
	}
	if strings.TrimSpace(info.ExternalID) == "" {
		return nil, apperr.Validation("external_id is required", nil)
	}

	user, err := s.users.GetByExternalID(ctx, info.Source, info.ExternalID)
	if err != nil {
		if !errors.Is(err, identity.ErrUserNotFound) {
			return nil, apperr.Internal("lookup external user", err)
		}
		// 首次登录：创建用户。
		user, err = s.createExternalUser(ctx, info)
		if err != nil {
			return nil, err
		}
	} else {
		// 已存在：更新展示信息（email/display_name 可能随 IdP 变化）。
		updated, uerr := s.syncExternalUser(ctx, user, info)
		if uerr != nil {
			return nil, uerr
		}
		user = updated
	}

	if !user.IsActive() {
		if user.Status == identity.UserStatusLocked {
			return nil, apperr.Unauthorized("user is locked", identity.ErrUserLocked)
		}
		return nil, apperr.Unauthorized("user is disabled", identity.ErrUserDisabled)
	}
	return s.issueTokens(ctx, user, in.IP, in.DeviceID, in.DeviceName, in.UserAgent)
}

// createExternalUser 首次外部登录时创建用户。用户名做 sanitize 保证合法与唯一。
func (s *Service) createExternalUser(ctx context.Context, info ExternalUserInfo) (*identity.User, error) {
	username := sanitizeExternalUsername(string(info.Source), info.ExternalID, info.Username)
	// 若用户名冲突，追加 external id 后缀直到唯一（最多 5 次）。
	candidate := username
	for i := 0; i < 5; i++ {
		if existing, err := s.users.GetByUsername(ctx, candidate); err != nil {
			if !errors.Is(err, identity.ErrUserNotFound) {
				return nil, apperr.Internal("check username uniqueness", err)
			}
			break
		} else if existing == nil {
			break
		}
		candidate = fmt.Sprintf("%s-%d", username, i+2)
	}

	user := &identity.User{
		Username:    candidate,
		Email:       strings.ToLower(strings.TrimSpace(info.Email)),
		Phone:       info.Phone,
		DisplayName: info.DisplayName,
		AuthSource:  info.Source,
		ExternalID:  info.ExternalID,
		Status:      identity.UserStatusActive,
		Locale:      "zh-CN",
		Timezone:    "Asia/Shanghai",
		Metadata:    info.RawAttrs,
		Version:     1,
	}
	if err := s.users.Create(ctx, user); err != nil {
		if errors.Is(err, identity.ErrUsernameExists) {
			return nil, apperr.Conflict("username collision, retry login", err)
		}
		if errors.Is(err, identity.ErrEmailExists) {
			// email 已被其他账号占用：清空 email 仍创建用户，避免阻断登录。
			user.Email = ""
			if err2 := s.users.Create(ctx, user); err2 != nil {
				return nil, apperr.Internal("create external user", err2)
			}
		} else {
			return nil, apperr.Internal("create external user", err)
		}
	}
	return user, nil
}

// syncExternalUser 已有外部用户登录时刷新展示信息（email/display_name/phone/metadata）。
func (s *Service) syncExternalUser(ctx context.Context, user *identity.User, info ExternalUserInfo) (*identity.User, error) {
	changed := false
	if info.Email != "" && info.Email != user.Email {
		user.Email = strings.ToLower(strings.TrimSpace(info.Email))
		changed = true
	}
	if info.DisplayName != "" && info.DisplayName != user.DisplayName {
		user.DisplayName = info.DisplayName
		changed = true
	}
	if info.Phone != "" && info.Phone != user.Phone {
		user.Phone = info.Phone
		changed = true
	}
	if info.RawAttrs != nil {
		user.Metadata = info.RawAttrs
		changed = true
	}
	if !changed {
		return user, nil
	}
	if err := s.users.Update(ctx, user); err != nil {
		// email 冲突等不阻断登录，仅记录；返回原 user 继续签发令牌。
		if errors.Is(err, identity.ErrEmailExists) {
			return user, nil
		}
		return user, nil
	}
	return user, nil
}

// sanitizeExternalUsername 把 external 登录信息转为合法用户名（3-64 位，[a-z0-9_.-]）。
// 优先用 suggested；为空则用 <source>-<externalID 截断>。
func sanitizeExternalUsername(source, externalID, suggested string) string {
	clean := func(s string) string {
		var b strings.Builder
		for _, c := range strings.ToLower(s) {
			if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' || c == '-' || c == '.' {
				b.WriteRune(c)
			} else {
				b.WriteRune('-')
			}
		}
		return b.String()
	}
	name := strings.TrimSpace(suggested)
	if name == "" {
		name = source + "-" + externalID
	}
	name = clean(name)
	// 去除连续/首尾 '-'
	for strings.Contains(name, "--") {
		name = strings.ReplaceAll(name, "--", "-")
	}
	name = strings.Trim(name, "-.")
	if len(name) < 3 {
		name = source + "-" + name
		name = strings.Trim(name, "-.")
	}
	if len(name) > 64 {
		name = name[:64]
	}
	if len(name) < 3 {
		name = name + "-user"
	}
	return name
}
