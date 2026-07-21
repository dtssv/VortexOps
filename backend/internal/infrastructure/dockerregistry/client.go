// Package dockerregistry 实现 Docker Registry v2 API 客户端，作为 RegistryAdapter 的一种实现。
// 鉴权：用户名 + 密码（Basic Auth），凭证 payload 为 JSON {username, password}。
// 不支持镜像扫描，GetImageScanResult 返回 ErrScanNotSupported。
package dockerregistry

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

// Client Docker Registry v2 客户端。
type Client struct {
	baseURL  string
	username string
	password string
	http     *http.Client
}

// New 创建 Docker Registry 客户端。rawCredential 为凭证解密后的 JSON。
func New(baseURL string, rawCredential []byte) (*Client, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		return nil, errors.New("docker registry url is required")
	}
	var cred struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal(rawCredential, &cred); err != nil {
		return nil, fmt.Errorf("parse docker registry credential: %w", err)
	}
	// 匿名仓库允许空凭证。
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
	if c.username != "" {
		req.SetBasicAuth(c.username, c.password)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("docker registry check connection: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("docker registry check connection: unexpected status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// GetImageMeta 查询镜像元信息。
// Docker Registry v2：HEAD /v2/{name}/manifests/{tag} 取 digest；
// GET /v2/{name}/manifests/{tag}（Accept: application/vnd.docker.distribution.manifest.v2+json）取 size。
func (c *Client) GetImageMeta(ctx context.Context, repository, tag string) (build.ImageMeta, error) {
	digest, sizeBytes, err := c.fetchManifest(ctx, repository, tag)
	if err != nil {
		return build.ImageMeta{}, err
	}
	return build.ImageMeta{
		Repository: repository,
		Tag:        tag,
		Digest:     digest,
		SizeBytes:  sizeBytes,
		Labels:     map[string]string{},
	}, nil
}

func (c *Client) fetchManifest(ctx context.Context, repository, tag string) (string, int64, error) {
	endpoint := fmt.Sprintf("%s/v2/%s/manifests/%s", c.baseURL, repository, tag)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", 0, err
	}
	if c.username != "" {
		req.SetBasicAuth(c.username, c.password)
	}
	// 同时请求 v2 manifest 与 OCI image manifest。
	req.Header.Set("Accept",
		"application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("docker registry get manifest: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", 0, fmt.Errorf("docker registry get manifest: unexpected status %d: %s", resp.StatusCode, string(body))
	}
	// Docker-Content-Digest header 即镜像 digest。
	digest := resp.Header.Get("Docker-Content-Digest")
	if digest == "" {
		digest = resp.Header.Get("Etag")
		digest = strings.Trim(digest, "\"")
	}
	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", 0, fmt.Errorf("docker registry read manifest: %w", err)
	}
	var manifest struct {
		Config struct {
			Size     int64             `json:"size"`
			MediaType string           `json:"mediaType"`
			Digest   string            `json:"digest"`
			Annotations map[string]string `json:"annotations"`
		} `json:"config"`
		Layers []struct {
			Size int64 `json:"size"`
		} `json:"layers"`
	}
	if err := json.Unmarshal(bodyBytes, &manifest); err != nil {
		// 非 JSON manifest（可能是 v1 schema），仅返回 digest。
		return digest, 0, nil
	}
	var totalSize int64
	totalSize += manifest.Config.Size
	for _, l := range manifest.Layers {
		totalSize += l.Size
	}
	return digest, totalSize, nil
}

// GetImageScanResult Docker Registry 不支持镜像扫描。
func (c *Client) GetImageScanResult(ctx context.Context, repository, tag string) (build.ImageScanStatus, map[string]any, error) {
	return build.ImgScanSkipped, map[string]any{"reason": "docker registry does not support scanning"}, build.ErrScanNotSupported
}
