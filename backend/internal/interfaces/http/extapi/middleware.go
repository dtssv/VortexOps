// Package extapi 提供对外 API HTTP 中间件：Bearer voe_ 鉴权、scope、IP 白名单、限流、幂等与审计。
package extapi

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/vortexops/vortexops/internal/application/extapiapp"
	"github.com/vortexops/vortexops/internal/domain/extapi"
	"github.com/vortexops/vortexops/pkg/apperr"
)

type ctxKey string

const (
	ctxKeyToken    ctxKey = "ext_token"
	ctxKeyUserID   ctxKey = "ext_uid"
	ctxKeyScope    ctxKey = "ext_scope"
	ctxKeyOperation ctxKey = "ext_op"
)

// TokenFromContext 从 context 取出 external Token。
func TokenFromContext(ctx context.Context) *extapi.ExternalToken {
	t, _ := ctx.Value(ctxKeyToken).(*extapi.ExternalToken)
	return t
}

// UserIDFromContext 从 context 取出 Token 所属用户 ID。
func UserIDFromContext(ctx context.Context) int64 {
	v, _ := ctx.Value(ctxKeyUserID).(int64)
	return v
}

// AuthMiddleware Bearer voe_ Token 鉴权。
func AuthMiddleware(svc *extapiapp.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			if auth == "" {
				WriteError(w, r, errUnauthorized("missing authorization header"))
				return
			}
			const prefix = "Bearer "
			if !strings.HasPrefix(auth, prefix) {
				WriteError(w, r, errUnauthorized("invalid authorization scheme"))
				return
			}
			tokenStr := strings.TrimSpace(strings.TrimPrefix(auth, prefix))
			t, err := svc.AuthenticateToken(r.Context(), tokenStr)
			if err != nil {
				WriteError(w, r, err)
				return
			}
			if err := svc.CheckIPAllowlist(t, clientIP(r)); err != nil {
				WriteError(w, r, err)
				return
			}
			if err := svc.CheckRateLimit(r.Context(), t); err != nil {
				WriteError(w, r, err)
				return
			}
			svc.TouchToken(r.Context(), t, clientIP(r))
			ctx := context.WithValue(r.Context(), ctxKeyToken, t)
			ctx = context.WithValue(ctx, ctxKeyUserID, t.UserID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireScope 校验 Token scope。
func RequireScope(scope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t := TokenFromContext(r.Context())
			if t == nil {
				WriteError(w, r, errUnauthorized("not authenticated"))
				return
			}
			if !t.HasScope(scope) {
				WriteError(w, r, errForbidden("insufficient scope: "+scope))
				return
			}
			ctx := context.WithValue(r.Context(), ctxKeyScope, scope)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// IdempotencyMiddleware 写操作幂等（Idempotency-Key 头）。
func IdempotencyMiddleware(svc *extapiapp.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost && r.Method != http.MethodPut && r.Method != http.MethodPatch {
				next.ServeHTTP(w, r)
				return
			}
			key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
			if key == "" {
				next.ServeHTTP(w, r)
				return
			}
			t := TokenFromContext(r.Context())
			if t == nil {
				next.ServeHTTP(w, r)
				return
			}
			compositeKey := idempotencyCompositeKey(t.ID, r.Method, r.URL.Path, key)
			if rec, err := svc.GetIdempotency(r.Context(), compositeKey); err == nil && rec != nil {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.Header().Set("X-Idempotent-Replay", "true")
				w.WriteHeader(rec.StatusCode)
				_, _ = w.Write(rec.ResponseBody)
				return
			}
			capture := &responseCapture{ResponseWriter: w, status: http.StatusOK, buf: &bytes.Buffer{}}
			next.ServeHTTP(capture, r)
			if capture.status >= 200 && capture.status < 300 {
				body := capture.buf.Bytes()
				if len(body) == 0 {
					body = capture.raw
				}
				_ = svc.SetIdempotency(r.Context(), &extapi.IdempotencyRecord{
					Key: compositeKey, TokenID: t.ID, Method: r.Method, Path: r.URL.Path,
					StatusCode: capture.status, ResponseBody: body, CreatedAt: time.Now(),
				}, 24*time.Hour)
			}
		})
	}
}

// AuditMiddleware 调用审计落库。
func AuditMiddleware(svc *extapiapp.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			capture := &statusCapture{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(capture, r)
			t := TokenFromContext(r.Context())
			var tokenID int64
			var prefix string
			if t != nil {
				tokenID = t.ID
				prefix = t.TokenPrefix
			}
			op, _ := r.Context().Value(ctxKeyOperation).(string)
			errMsg := ""
			if capture.status >= 400 {
				errMsg = http.StatusText(capture.status)
			}
			wsID := parseWorkspaceIDFromPath(r.URL.Path)
			svc.AppendCallLog(r.Context(), &extapi.ExternalCallLog{
				TokenID: tokenID, TokenPrefix: prefix, Method: r.Method, Path: r.URL.Path,
				Operation: op, WorkspaceID: wsID, RequestID: chimw.GetReqID(r.Context()),
				StatusCode: capture.status, DurationMs: int(time.Since(start).Milliseconds()),
				ClientIP: clientIP(r), UserAgent: r.UserAgent(), ErrorMessage: errMsg,
			})
		})
	}
}

// WithOperation 注入操作名供审计。
func WithOperation(op string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), ctxKeyOperation, op)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

type responseCapture struct {
	http.ResponseWriter
	status int
	buf    *bytes.Buffer
	raw    []byte
}

func (c *responseCapture) WriteHeader(code int) {
	c.status = code
	c.ResponseWriter.WriteHeader(code)
}

func (c *responseCapture) Write(b []byte) (int, error) {
	c.raw = append(c.raw, b...)
	if c.buf != nil {
		_, _ = c.buf.Write(b)
	}
	return c.ResponseWriter.Write(b)
}

type statusCapture struct {
	http.ResponseWriter
	status int
}

func (c *statusCapture) WriteHeader(code int) {
	c.status = code
	c.ResponseWriter.WriteHeader(code)
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i >= 0 {
		return host[:i]
	}
	return host
}

func idempotencyCompositeKey(tokenID int64, method, path, key string) string {
	return strings.Join([]string{
		strings.TrimSpace(key),
		method,
		path,
		fmt.Sprintf("t%d", tokenID),
	}, ":")
}

func parseWorkspaceIDFromPath(path string) int64 {
	_ = path
	return 0
}

func errUnauthorized(msg string) error {
	return apperr.Unauthorized(msg, nil)
}

func errForbidden(msg string) error {
	return apperr.Forbidden(msg, nil)
}

// DrainBody 读取并恢复 request body（幂等/日志用）。
func DrainBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	b, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	r.Body = io.NopCloser(bytes.NewReader(b))
	return b, nil
}
