// Package authhttp 是身份认证的 HTTP handlers。
package authhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/vortexops/vortexops/internal/application/auditapp"
	"github.com/vortexops/vortexops/internal/application/identityapp"
	"github.com/vortexops/vortexops/internal/domain/audit"
	"github.com/vortexops/vortexops/internal/domain/identity"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpauth"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpx"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// Handler 处理 /api/v1/auth 与 /api/v1/users 路由。
type Handler struct {
	svc      *identityapp.Service
	auditSvc *auditapp.Service
}

// NewHandler 创建认证 handler。auditSvc 用于显式记录登录/登出审计。
func NewHandler(svc *identityapp.Service, auditSvc *auditapp.Service) *Handler {
	return &Handler{svc: svc, auditSvc: auditSvc}
}

// Register POST /api/v1/auth/register
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username    string `json:"username"`
		Email       string `json:"email"`
		Phone       string `json:"phone"`
		DisplayName string `json:"display_name"`
		Password    string `json:"password"`
		Locale      string `json:"locale"`
		Timezone    string `json:"timezone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	res, err := h.svc.Register(r.Context(), identityapp.RegisterInput{
		Username:    req.Username,
		Email:       req.Email,
		Phone:       req.Phone,
		DisplayName: req.DisplayName,
		Password:    req.Password,
		Locale:      req.Locale,
		Timezone:    req.Timezone,
		CreatedBy:   0,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Created(w, toUserDTO(res.User))
}

// ListLoginProviders GET /api/v1/auth/providers （公开）
// 返回当前平台启用的登录方式，供登录页渲染（默认 local + 已注册的 OIDC/LDAP）。
func (h *Handler) ListLoginProviders(w http.ResponseWriter, r *http.Request) {
	httpx.OK(w, h.svc.ListLoginProviders())
}

// Login POST /api/v1/auth/login
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UsernameOrEmail string `json:"username"`
		Password        string `json:"password"`
		DeviceID        string `json:"device_id"`
		DeviceName      string `json:"device_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	ip := identityapp.ClientIPFromRequest(r.RemoteAddr)
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		ip = fwd
	}
	outcome, err := h.svc.Login(r.Context(), identityapp.LoginInput{
		UsernameOrEmail: req.UsernameOrEmail,
		Password:        req.Password,
		IP:              ip,
		UserAgent:       r.UserAgent(),
		DeviceID:        req.DeviceID,
		DeviceName:      req.DeviceName,
	})
	if err != nil {
		// 记录失败的登录尝试（用户名未知时 uid=0）。
		h.recordLoginAudit(0, req.UsernameOrEmail, ip, r, http.StatusUnauthorized, err.Error())
		httpx.WriteError(w, err)
		return
	}
	if outcome.Challenge != nil {
		httpx.OK(w, map[string]any{
			"mfa_required": true,
			"mfa_token":    outcome.Challenge.MFAToken,
			"user_id":      outcome.Challenge.UserID,
			"username":     outcome.Challenge.Username,
		})
		return
	}
	h.recordLoginAudit(outcome.Result.User.ID, outcome.Result.User.Username, ip, r, http.StatusOK, "")
	httpx.OK(w, tokenResponse{
		AccessToken:  outcome.Result.AccessToken,
		RefreshToken: outcome.Result.RefreshToken,
		TokenType:    "Bearer",
		ExpiresAt:    outcome.Result.AccessExp,
		User:         toUserDTO(outcome.Result.User),
	})
}

// LoginWithMFA POST /api/v1/auth/login/mfa
func (h *Handler) LoginWithMFA(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MFAToken   string `json:"mfa_token"`
		Code       string `json:"code"`
		DeviceID   string `json:"device_id"`
		DeviceName string `json:"device_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	ip := identityapp.ClientIPFromRequest(r.RemoteAddr)
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		ip = fwd
	}
	res, err := h.svc.LoginWithMFA(r.Context(), identityapp.LoginWithMFAInput{
		MFAToken: req.MFAToken, Code: req.Code, IP: ip,
		UserAgent: r.UserAgent(), DeviceID: req.DeviceID, DeviceName: req.DeviceName,
	})
	if err != nil {
		h.recordLoginAudit(0, "", ip, r, http.StatusUnauthorized, err.Error())
		httpx.WriteError(w, err)
		return
	}
	h.recordLoginAudit(res.User.ID, res.User.Username, ip, r, http.StatusOK, "")
	httpx.OK(w, tokenResponse{
		AccessToken:  res.AccessToken,
		RefreshToken: res.RefreshToken,
		TokenType:    "Bearer",
		ExpiresAt:    res.AccessExp,
		User:         toUserDTO(res.User),
	})
}

// Refresh POST /api/v1/auth/refresh
func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
		DeviceID     string `json:"device_id"`
		DeviceName   string `json:"device_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	ip := identityapp.ClientIPFromRequest(r.RemoteAddr)
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		ip = fwd
	}
	res, err := h.svc.Refresh(r.Context(), identityapp.RefreshInput{
		RefreshToken: req.RefreshToken,
		IP:           ip,
		UserAgent:    r.UserAgent(),
		DeviceID:     req.DeviceID,
		DeviceName:   req.DeviceName,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, tokenResponse{
		AccessToken:  res.AccessToken,
		RefreshToken: res.RefreshToken,
		TokenType:    "Bearer",
		ExpiresAt:    res.AccessExp,
	})
}

// Logout POST /api/v1/auth/logout
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if err := h.svc.Logout(r.Context(), req.RefreshToken); err != nil {
		httpx.WriteError(w, err)
		return
	}
	uid := httpauth.UserID(r.Context())
	h.recordLogoutAudit(uid, r, http.StatusNoContent)
	httpx.NoContent(w)
}

// LogoutAll POST /api/v1/auth/logout-all  （需鉴权）
func (h *Handler) LogoutAll(w http.ResponseWriter, r *http.Request) {
	uid := httpauth.UserID(r.Context())
	if uid == 0 {
		httpx.WriteError(w, apperr.Unauthorized("not authenticated", nil))
		return
	}
	if err := h.svc.LogoutAll(r.Context(), uid); err != nil {
		httpx.WriteError(w, err)
		return
	}
	h.recordLogoutAudit(uid, r, http.StatusNoContent)
	httpx.NoContent(w)
}

// ChangePassword POST /api/v1/users/me/password （需鉴权）
func (h *Handler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	uid := httpauth.UserID(r.Context())
	if uid == 0 {
		httpx.WriteError(w, apperr.Unauthorized("not authenticated", nil))
		return
	}
	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	if err := h.svc.ChangePassword(r.Context(), identityapp.ChangePasswordInput{
		UserID:      uid,
		OldPassword: req.OldPassword,
		NewPassword: req.NewPassword,
	}); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// GetMe GET /api/v1/users/me （需鉴权）
func (h *Handler) GetMe(w http.ResponseWriter, r *http.Request) {
	uid := httpauth.UserID(r.Context())
	if uid == 0 {
		httpx.WriteError(w, apperr.Unauthorized("not authenticated", nil))
		return
	}
	u, err := h.svc.GetByID(r.Context(), uid)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toUserDTO(u))
}

// ListUsers GET /api/v1/users?search=&status=&page=&size= （需 user:manage）
func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	size, _ := strconv.Atoi(r.URL.Query().Get("size"))
	users, total, err := h.svc.ListUsers(r.Context(), identityapp.ListUsersInput{
		Search: r.URL.Query().Get("search"),
		Status: r.URL.Query().Get("status"),
		Page:   page, Size: size,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	items := make([]*userDTO, 0, len(users))
	for _, u := range users {
		items = append(items, toUserDTO(u))
	}
	httpx.OK(w, map[string]any{"items": items, "total": total, "page": page, "size": size})
}

// CreateUser POST /api/v1/users （需 user:manage）
func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
	uid := httpauth.UserID(r.Context())
	var req struct {
		Username    string `json:"username"`
		Email       string `json:"email"`
		Phone       string `json:"phone"`
		DisplayName string `json:"display_name"`
		Password    string `json:"password"`
		Locale      string `json:"locale"`
		Timezone    string `json:"timezone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	u, err := h.svc.CreateUser(r.Context(), identityapp.CreateUserInput{
		Username: req.Username, Email: req.Email, Phone: req.Phone, DisplayName: req.DisplayName,
		Password: req.Password, Locale: req.Locale, Timezone: req.Timezone, CreatedBy: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Created(w, toUserDTO(u))
}

// UpdateUserStatus PUT /api/v1/users/{id}/status （需 user:manage）
func (h *Handler) UpdateUserStatus(w http.ResponseWriter, r *http.Request) {
	uid := httpauth.UserID(r.Context())
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		httpx.WriteError(w, apperr.Validation("invalid id", err))
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	if err := h.svc.UpdateUserStatus(r.Context(), identityapp.UpdateUserStatusInput{
		UserID: id, Status: identity.UserStatus(req.Status), ActorID: uid,
	}); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// UpdateUser PUT /api/v1/users/{id} （需 user:manage）
// 全量更新用户可编辑资料（email/phone/display_name/locale/timezone/status），乐观锁。
func (h *Handler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	uid := httpauth.UserID(r.Context())
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		httpx.WriteError(w, apperr.Validation("invalid id", err))
		return
	}
	var req struct {
		Email       *string `json:"email"`
		Phone       *string `json:"phone"`
		DisplayName *string `json:"display_name"`
		Locale      *string `json:"locale"`
		Timezone    *string `json:"timezone"`
		Status      *string `json:"status"`
		Version     int     `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	var status *identity.UserStatus
	if req.Status != nil {
		s := identity.UserStatus(*req.Status)
		status = &s
	}
	u, err := h.svc.UpdateUser(r.Context(), identityapp.UpdateUserInput{
		UserID: id, Email: req.Email, Phone: req.Phone, DisplayName: req.DisplayName,
		Locale: req.Locale, Timezone: req.Timezone, Status: status,
		Version: req.Version, ActorID: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toUserDTO(u))
}

// ResetUserPassword PUT /api/v1/users/{id}/password （需 user:manage）
// 管理员重置用户密码，可选择强制下次登录改密。
func (h *Handler) ResetUserPassword(w http.ResponseWriter, r *http.Request) {
	uid := httpauth.UserID(r.Context())
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		httpx.WriteError(w, apperr.Validation("invalid id", err))
		return
	}
	var req struct {
		NewPassword        string `json:"new_password"`
		MustChangePassword *bool  `json:"must_change_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	if err := h.svc.ResetPassword(r.Context(), identityapp.ResetPasswordInput{
		UserID: id, NewPassword: req.NewPassword, MustChangePassword: req.MustChangePassword, ActorID: uid,
	}); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// DeleteUser DELETE /api/v1/users/{id} （需 user:manage）
func (h *Handler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	uid := httpauth.UserID(r.Context())
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		httpx.WriteError(w, apperr.Validation("invalid id", err))
		return
	}
	if err := h.svc.DeleteUser(r.Context(), id, uid); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// ============================================================================
// MFA (TOTP) 两步验证端点
// ============================================================================

// GenerateMFA POST /api/v1/users/me/mfa/generate
func (h *Handler) GenerateMFA(w http.ResponseWriter, r *http.Request) {
	uid := httpauth.UserID(r.Context())
	if uid == 0 {
		httpx.WriteError(w, apperr.Unauthorized("not authenticated", nil))
		return
	}
	res, err := h.svc.GenerateMFA(r.Context(), uid)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, map[string]any{
		"secret":        res.Secret,
		"otpauth_url":   res.OTPAuthURL,
		"backup_codes":  res.BackupCodes,
	})
}

// EnableMFA POST /api/v1/users/me/mfa/enable
func (h *Handler) EnableMFA(w http.ResponseWriter, r *http.Request) {
	uid := httpauth.UserID(r.Context())
	if uid == 0 {
		httpx.WriteError(w, apperr.Unauthorized("not authenticated", nil))
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	if err := h.svc.EnableMFA(r.Context(), identityapp.EnableMFAInput{UserID: uid, Code: req.Code}); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// DisableMFA POST /api/v1/users/me/mfa/disable
func (h *Handler) DisableMFA(w http.ResponseWriter, r *http.Request) {
	uid := httpauth.UserID(r.Context())
	if uid == 0 {
		httpx.WriteError(w, apperr.Unauthorized("not authenticated", nil))
		return
	}
	var req struct {
		Code       string `json:"code"`
		UsePassword bool  `json:"use_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	if err := h.svc.DisableMFA(r.Context(), identityapp.DisableMFAInput{
		UserID: uid, Code: req.Code, UsePassword: req.UsePassword,
	}); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// recordLoginAudit 异步记录登录审计日志（成功或失败）。
func (h *Handler) recordLoginAudit(uid int64, username, ip string, r *http.Request, status int, errMsg string) {
	if h.auditSvc == nil {
		return
	}
	go h.auditSvc.Record(context.Background(), auditapp.RecordInput{
		UserID:       uid,
		UserName:     username,
		ResourceType: "auth",
		Action:       audit.ActionLogin,
		Operation:    "POST /auth/login",
		Method:       r.Method,
		Path:         r.URL.Path,
		StatusCode:   status,
		ClientIP:     ip,
		UserAgent:    r.UserAgent(),
		ErrorMessage: errMsg,
	})
}

// recordLogoutAudit 异步记录登出审计日志。
func (h *Handler) recordLogoutAudit(uid int64, r *http.Request, status int) {
	if h.auditSvc == nil {
		return
	}
	ip := identityapp.ClientIPFromRequest(r.RemoteAddr)
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		ip = fwd
	}
	go h.auditSvc.Record(context.Background(), auditapp.RecordInput{
		UserID:       uid,
		ResourceType: "auth",
		Action:       audit.ActionLogout,
		Operation:    "POST " + r.URL.Path,
		Method:       r.Method,
		Path:         r.URL.Path,
		StatusCode:   status,
		ClientIP:     ip,
		UserAgent:    r.UserAgent(),
	})
}

type tokenResponse struct {
	AccessToken  string   `json:"access_token"`
	RefreshToken string   `json:"refresh_token"`
	TokenType    string   `json:"token_type"`
	ExpiresAt    any      `json:"expires_at"`
	User         *userDTO `json:"user,omitempty"`
}

type userDTO struct {
	ID          int64  `json:"id"`
	UUID        string `json:"uuid"`
	Username    string `json:"username"`
	Email       string `json:"email"`
	Phone       string `json:"phone"`
	DisplayName string `json:"display_name"`
	AvatarURL   string `json:"avatar_url"`
	AuthSource  string `json:"auth_source"`
	Status      string `json:"status"`
	Locale      string `json:"locale"`
	Timezone    string `json:"timezone"`
	MFAEnabled  bool   `json:"mfa_enabled"`
	Version     int    `json:"version"`
	CreatedAt   string `json:"created_at"`
}

func toUserDTO(u *identity.User) *userDTO {
	if u == nil {
		return nil
	}
	return &userDTO{
		ID:          u.ID,
		UUID:        u.UUID.String(),
		Username:    u.Username,
		Email:       u.Email,
		Phone:       u.Phone,
		DisplayName: u.DisplayName,
		AvatarURL:   u.AvatarURL,
		AuthSource:  string(u.AuthSource),
		Status:      string(u.Status),
		Locale:      u.Locale,
		Timezone:    u.Timezone,
		MFAEnabled:  u.MFAEnabled,
		Version:     u.Version,
		CreatedAt:   u.CreatedAt.Format(time.RFC3339),
	}
}
