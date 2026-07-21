// Package acr 实现阿里云容器镜像服务（ACR）客户端，作为 RegistryAdapter 的一种实现。
// 鉴权：用户名 + 密码（Basic Auth），凭证 payload 为 JSON {username, password}。
// ACR 兼容 Docker Registry v2 API，因此镜像元信息查询走 /v2/{name}/manifests/{tag}。
// 扫描结果查询走 ACR REST API（需 AK，开发环境暂不支持，返回 ErrScanNotSupported）。
package acr

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

// Client 阿里云 ACR 客户端（兼容 Registry v2 协议拉取 manifest）。
type Client struct {
	baseURL  string
	username string
	password string
	http     *http.Client
}

// New 创建 ACR 客户端。rawCredential 为凭证解密后的 JSON。
func New(baseURL string, rawCredential []byte) (*Client, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		return nil, errors.New("acr url is required")
	}
	var cred struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal(rawCredential, &cred); err != nil {
		return nil, fmt.Errorf("parse acr credential: %w", err)
	}
	if cred.Username == "" || cred.Password == "" {
		return nil, errors.New("acr credential requires username and password")
	}
	return &Client{
		baseURL:  baseURL,
		username: cred.Username,
		password: cred.Password,
		http:     &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// CheckConnection 测试连通性（GET /v2/）。
func (c *Client) CheckConnection(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v2/", nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.username, c.password)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("acr check connection: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("acr check connection: unexpected status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// GetImageMeta 查询镜像元信息（ACR 兼容 Registry v2 协议）。
func (c *Client) GetImageMeta(ctx context.Context, repository, tag string) (build.ImageMeta, error) {
	endpoint := fmt.Sprintf("%s/v2/%s/manifests/%s", c.baseURL, repository, tag)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return build.ImageMeta{}, err
	}
	req.SetBasicAuth(c.username, c.password)
	req.Header.Set("Accept",
		"application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json")
	resp, err := c.http.Do(req)
	if err != nil {
		return build.ImageMeta{}, fmt.Errorf("acr get image meta: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return build.ImageMeta{}, fmt.Errorf("acr get image meta: unexpected status %d: %s", resp.StatusCode, string(body))
	}
	digest := resp.Header.Get("Docker-Content-Digest")
	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return build.ImageMeta{}, fmt.Errorf("acr read manifest: %w", err)
	}
	var manifest struct {
		Config struct {
			Size        int64             `json:"size"`
			Annotations map[string]string `json:"annotations"`
		} `json:"config"`
		Layers []struct {
			Size int64 `json:"size"`
		} `json:"layers"`
	}
	var totalSize int64
	if err := json.Unmarshal(bodyBytes, &manifest); err == nil {
		totalSize += manifest.Config.Size
		for _, l := range manifest.Layers {
			totalSize += l.Size
		}
		return build.ImageMeta{
			Repository: repository,
			Tag:        tag,
			Digest:     digest,
			SizeBytes:  totalSize,
			Labels:     manifest.Config.Annotations,
		}, nil
	}
	return build.ImageMeta{
		Repository: repository,
		Tag:        tag,
		Digest:     digest,
		SizeBytes:  0,
		Labels:     map[string]string{},
	}, nil
}

// GetImageScanResult ACR 扫描结果查询需调用阿里云 OpenAPI（需 AK/SK）。
// 开发环境暂不支持，返回 ErrScanNotSupported。生产环境可通过扩展此实现对接。
func (c *Client) GetImageScanResult(ctx context.Context, repository, tag string) (build.ImageScanStatus, map[string]any, error) {
	return build.ImgScanSkipped, map[string]any{"reason": "acr scan requires aliyun openapi (not configured)"}, build.ErrScanNotSupported
}
