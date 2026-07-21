// Package harbor 实现 Harbor v2 REST 客户端，用于查询镜像 CVE 扫描结果。
// 鉴权：用户名 + 密码（Basic Auth），凭证 payload 为 JSON {username, password}。
package harbor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/vortexops/vortexops/internal/domain/build"
)

// Client Harbor REST 客户端。
type Client struct {
	baseURL  string
	username string
	password string
	http     *http.Client
}

// New 创建 Harbor 客户端。rawCredential 为凭证解密后的 JSON。
func New(baseURL string, rawCredential []byte) (*Client, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		return nil, errors.New("harbor url is required")
	}
	var cred struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal(rawCredential, &cred); err != nil {
		return nil, fmt.Errorf("parse harbor credential: %w", err)
	}
	if cred.Username == "" || cred.Password == "" {
		return nil, errors.New("harbor credential requires username and password")
	}
	return &Client{
		baseURL:  baseURL,
		username: cred.Username,
		password: cred.Password,
		http:     &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// scanReport Harbor 扫描报告。
type scanReport struct {
	ScanStatus string `json:"scan_status"`
	Summary    struct {
		Summary map[string]int `json:"summary"`
	} `json:"summary"`
	Vulnerabilities []struct {
		ID       string `json:"id"`
		Severity string `json:"severity"`
		Package  string `json:"package"`
		Version  string `json:"version"`
	} `json:"vulnerabilities"`
}

// GetImageScanResult 查询镜像扫描结果。
// 返回扫描状态与结果摘要（漏洞计数按严重等级）。
func (c *Client) GetImageScanResult(ctx context.Context, repository, tag string) (build.ImageScanStatus, map[string]any, error) {
	endpoint := fmt.Sprintf("%s/api/v2.0/projects/%s/repositories/%s/artifacts/%s?with_scan_overview=true&with_vulnerabilities=true",
		c.baseURL, projectOf(repository), repoNameOf(repository), urlEscape(tag))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", nil, err
	}
	req.SetBasicAuth(c.username, c.password)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("get scan result: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return build.ImgScanSkipped, map[string]any{"reason": "artifact not found"}, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", nil, fmt.Errorf("get scan result: unexpected status %d: %s", resp.StatusCode, string(body))
	}
	var art struct {
		ScanOverview map[string]scanReport `json:"scan_overview"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&art); err != nil {
		return "", nil, fmt.Errorf("decode scan result: %w", err)
	}
	if len(art.ScanOverview) == 0 {
		return build.ImgScanPending, map[string]any{"reason": "scan not started"}, nil
	}
	// 取第一个扫描器报告（通常只有 MIME 类型 key）。
	var report scanReport
	for _, r := range art.ScanOverview {
		report = r
		break
	}
	result := map[string]any{
		"scan_status": report.ScanStatus,
		"summary":     report.Summary.Summary,
		"vuln_count":  len(report.Vulnerabilities),
	}
	status := build.ImgScanPassed
	switch strings.ToLower(report.ScanStatus) {
	case "running", "pending", "error":
		status = build.ImgScanPending
	case "success":
		// 若有 Critical/High 漏洞则判失败。
		for _, v := range report.Vulnerabilities {
		 sev := strings.ToLower(v.Severity)
			if sev == "critical" || sev == "high" {
				status = build.ImgScanFailed
				break
			}
		}
	}
	return status, result, nil
}

// projectOf 从 repository（形如 project/repo 或 project/sub/repo）取 project。
func projectOf(repo string) string {
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) < 2 {
		return repo
	}
	return parts[0]
}

// repoNameOf 取 repo 部分（去掉 project 前缀）。
func repoNameOf(repo string) string {
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) < 2 {
		return repo
	}
	return parts[1]
}

func urlEscape(s string) string {
	// Harbor tag 可能含特殊字符，但通常安全；保留 URL 编码。
	return s
}

// CheckConnection 测试 Harbor 连通性（GET /api/v2.0/health）。
func (c *Client) CheckConnection(ctx context.Context) error {
	endpoint := c.baseURL + "/api/v2.0/health"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.username, c.password)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("harbor check connection: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("harbor check connection: unexpected status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// GetImageMeta 查询镜像元信息（digest/size/labels）。
// 通过 Harbor v2 API：GET /api/v2.0/projects/{project}/repositories/{repo}/artifacts/{tag}
func (c *Client) GetImageMeta(ctx context.Context, repository, tag string) (build.ImageMeta, error) {
	endpoint := fmt.Sprintf("%s/api/v2.0/projects/%s/repositories/%s/artifacts/%s?with_tag=true",
		c.baseURL, projectOf(repository), repoNameOf(repository), urlEscape(tag))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return build.ImageMeta{}, err
	}
	req.SetBasicAuth(c.username, c.password)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return build.ImageMeta{}, fmt.Errorf("harbor get image meta: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return build.ImageMeta{}, fmt.Errorf("harbor get image meta: unexpected status %d: %s", resp.StatusCode, string(body))
	}
	var art struct {
		Digest string `json:"digest"`
		Size   int64  `json:"size"`
		Tags   []struct {
			Name string `json:"name"`
		} `json:"tags"`
		ExtraAttrs map[string]string `json:"extra_attrs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&art); err != nil {
		return build.ImageMeta{}, fmt.Errorf("harbor decode image meta: %w", err)
	}
	return build.ImageMeta{
		Repository: repository,
		Tag:        tag,
		Digest:     art.Digest,
		SizeBytes:  art.Size,
		Labels:     art.ExtraAttrs,
	}, nil
}
