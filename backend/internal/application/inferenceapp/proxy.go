package inferenceapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/vortexops/vortexops/internal/domain/inference"
	"github.com/vortexops/vortexops/internal/platform/redis"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// Proxy OpenAI 兼容反向代理：校验 API Key、Redis 限流、转发并记录用量。
type Proxy struct {
	svc   *Service
	redis *redis.Client
}

// NewProxy 创建代理。
func NewProxy(svc *Service, rc *redis.Client) *Proxy {
	return &Proxy{svc: svc, redis: rc}
}

// ServeHTTP 处理 /inference-services/{id}/v1/* 请求。
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	serviceID, err := parseServiceID(r)
	if err != nil {
		writeProxyError(w, apperr.Validation("invalid service id", err))
		return
	}
	secret := extractBearerToken(r)
	if secret == "" {
		writeProxyError(w, apperr.Unauthorized("missing bearer token", nil))
		return
	}
	apiKey, err := p.svc.ValidateAPIKey(r.Context(), secret)
	if err != nil {
		writeProxyError(w, err)
		return
	}
	if apiKey.InferenceServiceID != serviceID {
		writeProxyError(w, apperr.Unauthorized("api key not valid for service", nil))
		return
	}
	if err := p.checkRateLimit(r.Context(), apiKey); err != nil {
		writeProxyError(w, err)
		return
	}
	svc, err := p.svc.GetService(r.Context(), serviceID)
	if err != nil {
		writeProxyError(w, err)
		return
	}
	targetBase, err := url.Parse(p.svc.InternalEndpoint(svc))
	if err != nil {
		writeProxyError(w, apperr.Internal("parse upstream", err))
		return
	}
	suffix := chi.URLParam(r, "*")
	targetPath := "/v1/" + suffix
	if r.URL.RawQuery != "" {
		targetPath += "?" + r.URL.RawQuery
	}
	targetURL := targetBase.ResolveReference(&url.URL{Path: targetPath})

	bodyBytes, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	// 检测是否为流式请求：流式请求不缓冲响应体（否则会破坏 SSE）。
	isStream := isStreamRequest(bodyBytes)

	proxy := httputil.NewSingleHostReverseProxy(targetBase)
	proxy.Director = func(req *http.Request) {
		req.URL = targetURL
		req.Host = targetBase.Host
		req.Header = r.Header.Clone()
		req.Method = r.Method
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		req.ContentLength = int64(len(bodyBytes))
	}

	if isStream {
		// 流式：直接透传，不记录响应体；用量解析跳过（流式响应 usage 在末尾 chunk）。
		proxy.FlushInterval = -1 // 立即 flush，保证 SSE 实时性
		proxy.ServeHTTP(w, r)
		durationMs := int(time.Since(start).Milliseconds())
		_ = p.svc.RecordUsage(context.Background(), &inference.InferenceUsage{
			InferenceServiceID: serviceID,
			APIKeyID:           apiKey.ID,
			DurationMs:         durationMs,
			StatusCode:         http.StatusOK,
			ModelVersionID:     svc.BaseModelVersionID,
			CreatedAt:          time.Now(),
		})
		p.svc.TouchAPIKeyLastUsed(context.Background(), apiKey.ID)
		return
	}

	rec := &responseRecorder{ResponseWriter: w, status: http.StatusOK}
	proxy.ServeHTTP(rec, r)

	durationMs := int(time.Since(start).Milliseconds())
	prompt, completion, total := parseUsageFromBody(rec.body)
	usage := &inference.InferenceUsage{
		InferenceServiceID: serviceID,
		APIKeyID:           apiKey.ID,
		PromptTokens:       prompt,
		CompletionTokens:   completion,
		TotalTokens:        total,
		DurationMs:         durationMs,
		StatusCode:         rec.status,
		ModelVersionID:     svc.BaseModelVersionID,
		CreatedAt:          time.Now(),
	}
	_ = p.svc.RecordUsage(context.Background(), usage)
	p.svc.TouchAPIKeyLastUsed(context.Background(), apiKey.ID)
}

// isStreamRequest 检测请求体是否带 stream:true（OpenAI 兼容）。
func isStreamRequest(body []byte) bool {
	var partial struct {
		Stream bool `json:"stream"`
	}
	if err := json.Unmarshal(body, &partial); err != nil {
		return false
	}
	return partial.Stream
}

func (p *Proxy) checkRateLimit(ctx context.Context, key *inference.InferenceAPIKey) error {
	if key.RateLimitPerMin <= 0 || p.redis == nil {
		return nil
	}
	bucket := time.Now().UTC().Format("200601021504")
	redisKey := fmt.Sprintf("inf:ratelimit:%d:%s", key.ID, bucket)
	count, err := p.redis.Universal.Incr(ctx, redisKey).Result()
	if err != nil {
		return apperr.Internal("rate limit check", err)
	}
	if count == 1 {
		_ = p.redis.Universal.Expire(ctx, redisKey, 2*time.Minute).Err()
	}
	if int(count) > key.RateLimitPerMin {
		return apperr.BusinessRule("rate limit exceeded", nil)
	}
	return nil
}

type responseRecorder struct {
	http.ResponseWriter
	status int
	body   []byte
}

func (r *responseRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	r.body = append(r.body, b...)
	return r.ResponseWriter.Write(b)
}

func parseUsageFromBody(body []byte) (prompt, completion, total int) {
	var resp struct {
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return 0, 0, 0
	}
	return resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens
}

func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
	}
	return r.Header.Get("X-API-Key")
}

func parseServiceID(r *http.Request) (int64, error) {
	raw := chi.URLParam(r, "id")
	var id int64
	_, err := fmt.Sscan(raw, &id)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid id %q", raw)
	}
	return id, nil
}

func writeProxyError(w http.ResponseWriter, err error) {
	var ae *apperr.Error
	if errors.As(err, &ae) {
		http.Error(w, ae.Message, ae.HTTPStatus)
		return
	}
	http.Error(w, err.Error(), http.StatusInternalServerError)
}
