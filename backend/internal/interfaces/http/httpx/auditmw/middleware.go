// Package auditmw 提供全链路审计中间件：自动记录所有鉴权用户的写操作（POST/PUT/DELETE/PATCH）。
// GET 请求默认不记录（避免日志膨胀），敏感操作（如查询导出）可通过 WithAuditAction 显式标记。
package auditmw

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/vortexops/vortexops/internal/application/auditapp"
	"github.com/vortexops/vortexops/internal/domain/audit"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpauth"
)

// 请求体最大审计快照长度（超出截断）。
const maxBodySnapshot = 4096

// Middleware 返回审计中间件。auditSvc 为 nil 时直接放行（审计降级）。
// 自动从 JWT 上下文提取 UserID/UserName，从请求方法映射 Action，从 chi 路由提取 resourceType。
func Middleware(auditSvc *auditapp.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 仅审计写操作（GET/HEAD/OPTIONS 跳过）。
			if !isMutation(r.Method) {
				next.ServeHTTP(w, r)
				return
			}

			start := time.Now()
			reqID := r.Header.Get("X-Request-ID")
			if reqID == "" {
				reqID = uuid.NewString()
			}

			// 读取完整请求体并缓存（恢复 body 供后续 handler 读取）。
			// 注意：审计快照单独截断到 maxBodySnapshot，但下游 handler 必须拿到完整 body，
			// 否则 kubeconfig 等大字段被截断会导致 JSON 解析失败（invalid request body）。
			var fullBody []byte
			if r.Body != nil {
				fullBody, _ = io.ReadAll(r.Body)
				r.Body.Close()
				r.Body = io.NopCloser(bytes.NewReader(fullBody))
			}
			var bodySnapshot []byte
			if len(fullBody) > maxBodySnapshot {
				bodySnapshot = fullBody[:maxBodySnapshot]
			} else {
				bodySnapshot = fullBody
			}

			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)

			// 审计服务为 nil 时降级跳过。
			if auditSvc == nil {
				return
			}

			uid := httpauth.UserID(r.Context())
			if uid == 0 {
				// 未鉴权的写操作（如登录/注册）由各 handler 显式审计。
				return
			}

			action := audit.ActionFromContext(r.Context())
			if action == "" {
				action = methodToAction(r.Method)
			}
			resourceType := audit.ResourceFromContext(r.Context())
			if resourceType == "" {
				resourceType = resourceTypeFromRoute(r)
			}
			resourceID := audit.ResourceIDFromContext(r.Context())
			wsID := httpauth.WorkspaceID(r.Context())

			var reqBody map[string]any
			if len(bodySnapshot) > 0 {
				_ = json.Unmarshal(bodySnapshot, &reqBody)
				sanitizeSensitive(reqBody)
			}

			ip := clientIP(r)

			// 异步写入审计日志，避免阻塞响应。
			go func() {
				auditSvc.Record(context.Background(), auditapp.RecordInput{
					UserID:       uid,
					UserName:     "", // 由 audit repo 根据 userID 补全
					WorkspaceID:  wsID,
					ResourceType: resourceType,
					ResourceID:   resourceID,
					Action:       action,
					Operation:    r.Method + " " + r.URL.Path,
					RequestID:    reqID,
					Method:       r.Method,
					Path:         r.URL.Path,
					StatusCode:   rec.status,
					ClientIP:     ip,
					UserAgent:    r.UserAgent(),
					RequestBody:  reqBody,
					DurationMs:   int(time.Since(start).Milliseconds()),
					ErrorMessage: errorMessage(rec.status),
				})
			}()
		})
	}
}

// WithAction 在请求上下文注入显式审计动作（覆盖方法默认映射）。
func WithAction(action audit.Action) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := audit.WithActionContext(r.Context(), action)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// WithResource 在请求上下文注入资源类型与 ID（用于显式标注）。
func WithResource(resourceType string, resourceID int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := audit.WithResourceContext(r.Context(), resourceType, resourceID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// statusRecorder 包装 ResponseWriter 以捕获状态码。
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Flush 透传 http.Flusher，确保流式响应（如 netcmd）能实时刷新到客户端。
// 若不实现，下游 handler 拿到的 flusher 为 nil，Go http server 会改用 Content-Length
// 模式缓冲整个响应，破坏流式。
func (r *statusRecorder) Flush() {
	if fl, ok := r.ResponseWriter.(http.Flusher); ok {
		fl.Flush()
	}
}

// Hijack 透传 http.Hijacker，供 WebSocket 升级使用。
func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := r.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

// Push 透传 http.Pusher（HTTP/2 server push）。
func (r *statusRecorder) Push(target string, opts *http.PushOptions) error {
	if ps, ok := r.ResponseWriter.(http.Pusher); ok {
		return ps.Push(target, opts)
	}
	return http.ErrNotSupported
}

func isMutation(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

func methodToAction(method string) audit.Action {
	switch method {
	case http.MethodPost:
		return audit.ActionCreate
	case http.MethodPut, http.MethodPatch:
		return audit.ActionUpdate
	case http.MethodDelete:
		return audit.ActionDelete
	}
	return audit.ActionRead
}

// resourceTypeFromRoute 从 chi 路由模式推断资源类型（取第一个路径段）。
func resourceTypeFromRoute(r *http.Request) string {
	pattern := chi.RouteContext(r.Context()).RoutePattern()
	if pattern == "" {
		pattern = r.URL.Path
	}
	// 例如 /api/v1/workspaces/{id}/members → workspaces
	parts := strings.Split(strings.Trim(pattern, "/"), "/")
	// 跳过 api/v1 前缀
	for _, p := range parts {
		if p == "api" || p == "v1" || strings.HasPrefix(p, "{") {
			continue
		}
		return p
	}
	return ""
}

func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		if idx := strings.IndexByte(fwd, ','); idx > 0 {
			return strings.TrimSpace(fwd[:idx])
		}
		return fwd
	}
	host := r.RemoteAddr
	if idx := strings.LastIndexByte(host, ':'); idx > 0 {
		return host[:idx]
	}
	return host
}

func errorMessage(status int) string {
	if status >= 400 {
		return http.StatusText(status)
	}
	return ""
}

// sanitizeSensitive 移除请求体中的敏感字段（密码、密钥等）。
func sanitizeSensitive(body map[string]any) {
	sensitiveKeys := map[string]bool{
		"password": true, "old_password": true, "new_password": true,
		"secret": true, "mfa_secret": true, "token": true, "access_token": true,
		"refresh_token": true, "mfa_token": true, "code": true,
		"kubeconfig": true, "api_key": true, "client_secret": true,
	}
	for k := range body {
		if sensitiveKeys[strings.ToLower(k)] {
			body[k] = "***"
		}
	}
}
