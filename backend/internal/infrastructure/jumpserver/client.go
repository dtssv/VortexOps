// Package jumpserver 是 JumpServer REST API 客户端。
// 封装资产同步、会话查询、SSO 连接 URL 签发。
package jumpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Client JumpServer API 客户端。
type Client struct {
	baseURL  string
	accessKey string
	secretKey string
	http     *http.Client
}

// New 创建 JumpServer 客户端。
func New(baseURL, accessKey, secretKey string) *Client {
	return &Client{
		baseURL:   strings.TrimRight(baseURL, "/"),
		accessKey: accessKey,
		secretKey: secretKey,
		http:      &http.Client{Timeout: 15 * time.Second},
	}
}

// Asset JumpServer 资产。
type Asset struct {
	ID       string `json:"id"`
	Hostname string `json:"hostname"`
	IP       string `json:"ip"`
	Port     int    `json:"port"`
	Platform string `json:"platform"`
	Protocol string `json:"protocol"`
	OrgID    string `json:"org_id"`
	Comment  string `json:"comment"`
	IsActive bool   `json:"is_active"`
}

// Session JumpServer 会话。
type Session struct {
	ID         string  `json:"id"`
	User       string  `json:"user"`
	Asset      string  `json:"asset"`
	AssetInfo  string  `json:"asset_info"`
	Protocol   string  `json:"protocol"`
	LoginFrom  string  `json:"login_from"`
	RemoteAddr string  `json:"remote_addr"`
	IsFinished bool    `json:"is_finished"`
	DateStart  string  `json:"date_start"`
	DateEnd    *string `json:"date_end"`
	Duration   float64 `json:"duration"`
	CommandCnt int     `json:"command_count"`
}

// ConnectionToken 连接令牌响应。
type ConnectionToken struct {
	Token    string `json:"token"`
	LoginURL string `json:"login_url"`
}

// ListAssets 拉取 JumpServer 资产列表。
func (c *Client) ListAssets(ctx context.Context) ([]Asset, error) {
	if c.baseURL == "" {
		return nil, errors.New("jumpserver base url not configured")
	}
	var resp struct {
		Count int     `json:"count"`
		Items []Asset `json:"results"`
	}
	if err := c.get(ctx, "/api/v1/assets/assets/?limit=500", &resp); err != nil {
		return nil, err
	}
	return resp.Items, nil
}

// ListSessions 拉取会话列表（按日期倒序）。
func (c *Client) ListSessions(ctx context.Context, since time.Time) ([]Session, error) {
	if c.baseURL == "" {
		return nil, errors.New("jumpserver base url not configured")
	}
	path := fmt.Sprintf("/api/v1/terminal/sessions/?limit=200&date_from=%s", since.Format("2006-01-02"))
	var resp struct {
		Count int       `json:"count"`
		Items []Session `json:"results"`
	}
	if err := c.get(ctx, path, &resp); err != nil {
		return nil, err
	}
	return resp.Items, nil
}

// CreateConnectionToken 为用户签发连接资产的令牌与 Luna 登录 URL。
func (c *Client) CreateConnectionToken(ctx context.Context, userID, assetID, systemUserID, protocol string) (*ConnectionToken, error) {
	if c.baseURL == "" {
		return nil, errors.New("jumpserver base url not configured")
	}
	body := map[string]string{
		"user":         userID,
		"asset":        assetID,
		"system_user":  systemUserID,
		"protocol":     protocol,
		"expire_days":  "1",
	}
	var resp struct {
		ID     string `json:"id"`
		Token  string `json:"token"`
	}
	if err := c.post(ctx, "/api/v1/authentication/connection-token/", body, &resp); err != nil {
		return nil, err
	}
	// Luna 登录 URL：/luna/?login_to=token
	loginURL := fmt.Sprintf("%s/luna/?login_to=%s", c.baseURL, resp.Token)
	return &ConnectionToken{Token: resp.Token, LoginURL: loginURL}, nil
}

// GetReplayURL 获取会话录像回放 URL（直接拼接 Luna replay 地址）。
func (c *Client) GetReplayURL(sessionID string) string {
	if c.baseURL == "" {
		return ""
	}
	return fmt.Sprintf("%s/luna/replay/%s", c.baseURL, sessionID)
}

// --- HTTP 签名 ---

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	c.sign(req, "", path)
	return c.do(req, out)
}

func (c *Client) post(ctx context.Context, path string, body any, out any) error {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c.sign(req, buf.String(), path)
	return c.do(req, out)
}

func (c *Client) do(req *http.Request, out any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("jumpserver request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("jumpserver %s %d: %s", req.URL.Path, resp.StatusCode, string(raw))
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("decode jumpserver response: %w", err)
		}
	}
	return nil
}

// sign 生成 JumpServer HTTP Signature（AccessKey/SecretKey 模式）。
// JumpServer v3 使用 HTTP Signature 规范（httpsig，RFC 类似）：
//
//	Authorization: Signature keyId="...",algorithm="hmac-sha256",
//	               headers="(request-target) accept date",signature="..."
//
// 签名字符串按 headers 顺序拼接，每行 "<header>: <value>"，以 "\n" 分隔（末尾无换行）：
//
//	(request-target): <lowercase-method> <path-with-query>
//	accept: application/json
//	date: <RFC1123 GMT>
//
// 注意：path 必须包含 query string（如 /api/v1/assets/assets/?limit=500），
// 因为 request-target 是 "method path?query"。
func (c *Client) sign(req *http.Request, body, path string) {
	date := time.Now().UTC().Format(http.TimeFormat)
	accept := "application/json"
	req.Header.Set("Date", date)
	req.Header.Set("Accept", accept)
	req.Header.Set("X-JMS-ORG", "00000000-0000-0000-0000-000000000002")

	// request-target: 小写 method + path（含 query）。
	requestTarget := strings.ToLower(req.Method) + " " + path
	stringToSign := "(request-target): " + requestTarget + "\n" +
		"accept: " + accept + "\n" +
		"date: " + date

	sig := hmacSHA256(c.secretKey, stringToSign)
	req.Header.Set("Authorization", fmt.Sprintf(
		`Signature keyId=%q,algorithm="hmac-sha256",headers="(request-target) accept date",signature=%q`,
		c.accessKey, sig,
	))
}

// hmacSHA256 计算 HMAC-SHA256 并返回 base64 编码。
func hmacSHA256(key, data string) string {
	// 使用标准库实现 HMAC-SHA256 + base64。
	return hmacSHA256Impl(key, data)
}

// 避免引入额外依赖，使用 crypto/hmac + crypto/sha256 + encoding/base64。
// （实现在 sign_impl.go 中保持可测试。）

var _ = sort.Strings
var _ = url.QueryEscape
