// Package httpauth 提供 HTTP 鉴权中间件：JWT 校验与用户上下文注入。
package httpauth

import (
	"context"
	"net/http"
	"strings"

	"github.com/vortexops/vortexops/internal/platform/security"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpx"
	"github.com/vortexops/vortexops/pkg/apperr"
)

type contextKey string

const (
	ctxKeyUserID   contextKey = "uid"
	ctxKeyUsername contextKey = "username"
)

// Middleware 校验 Bearer JWT，把用户信息注入 context。
// 未携带 token 的请求被拒绝（401）。可选 public 路径在前置路由跳过本中间件。
// 对于浏览器无法自定义 header 的 WebSocket 握手，也接受 ?token=<jwt> 查询参数。
func Middleware(issuer *security.JWTIssuer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokenStr := ""
			if authHeader := r.Header.Get("Authorization"); authHeader != "" {
				const prefix = "Bearer "
				if !strings.HasPrefix(authHeader, prefix) {
					httpx.WriteError(w, apperr.Unauthorized("invalid authorization scheme", nil))
					return
				}
				tokenStr = strings.TrimSpace(strings.TrimPrefix(authHeader, prefix))
			} else if q := r.URL.Query().Get("token"); q != "" {
				// WebSocket 握手降级：浏览器无法设置自定义 header。
				tokenStr = q
			}
			if tokenStr == "" {
				httpx.WriteError(w, apperr.Unauthorized("missing token", nil))
				return
			}
			claims, err := issuer.Parse(tokenStr)
			if err != nil {
				httpx.WriteError(w, apperr.Unauthorized("invalid or expired token", err))
				return
			}
			ctx := context.WithValue(r.Context(), ctxKeyUserID, claims.UserID)
			ctx = context.WithValue(ctx, ctxKeyUsername, claims.Username)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// UserID 从 context 取出当前用户 ID。未鉴权时返回 0。
func UserID(ctx context.Context) int64 {
	v, _ := ctx.Value(ctxKeyUserID).(int64)
	return v
}

// Username 从 context 取出当前用户名。
func Username(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyUsername).(string)
	return v
}

// PermissionChecker 权限校验器（rbacapp.Service 实现）。
type PermissionChecker interface {
	HasPermission(ctx context.Context, userID int64, workspaceID int64, permCode string) (bool, error)
}

// WorkspaceResolver 从请求解析当前 workspace ID（路径参数或查询参数）。
// 返回 0 表示无 workspace 上下文（平台级操作）。
type WorkspaceResolver func(r *http.Request) int64

// RequirePermission 返回一个中间件，校验当前用户是否拥有指定权限。
// 未通过返回 403。permCode 为空时不校验（仅鉴权）。
func RequirePermission(checker PermissionChecker, resolver WorkspaceResolver, permCode string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if permCode == "" {
				next.ServeHTTP(w, r)
				return
			}
			uid := UserID(r.Context())
			if uid == 0 {
				httpx.WriteError(w, apperr.Unauthorized("not authenticated", nil))
				return
			}
			wsID := int64(0)
			if resolver != nil {
				wsID = resolver(r)
			}
			ok, err := checker.HasPermission(r.Context(), uid, wsID, permCode)
			if err != nil {
				httpx.WriteError(w, apperr.Internal("check permission", err))
				return
			}
			if !ok {
				httpx.WriteError(w, apperr.Forbidden("permission denied: "+permCode, nil))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// WithWorkspaceID 把 workspace ID 注入 context（供后续 handler/校验使用）。
func WithWorkspaceID(ctx context.Context, workspaceID int64) context.Context {
	return context.WithValue(ctx, ctxKeyWorkspaceID, workspaceID)
}

const ctxKeyWorkspaceID contextKey = "wsid"

// WorkspaceID 从 context 取出 workspace ID。
func WorkspaceID(ctx context.Context) int64 {
	v, _ := ctx.Value(ctxKeyWorkspaceID).(int64)
	return v
}
