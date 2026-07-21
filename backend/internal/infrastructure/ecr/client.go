// Package ecr 实现 AWS Elastic Container Registry (ECR) 客户端，作为 RegistryAdapter 的一种实现。
//
// 实现说明：
//   ECR 镜像元信息查询需通过 AWS API（BatchGetImage）并使用 AWS SigV4 签名鉴权，
//   生产实现建议引入 github.com/aws/aws-sdk-go-v2/service/ecr。
//   为避免在后端引入重型 AWS SDK 依赖（开发环境无 ECR），本实现先以桩函数形式提供，
//   返回 ErrECRNotConfigured。前端 type 选择器对 ECR 标注「敬请期待」并禁用保存。
//
// 凭证 payload：JSON {username, password, region, registry_id}。
package ecr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/vortexops/vortexops/internal/domain/build"
)

// ErrECRNotConfigured 表示 ECR 适配器尚未配置 AWS 凭证/SDK，暂不可用。
var ErrECRNotConfigured = errors.New("ecr adapter: not configured (requires aws sdk and credentials)")

// Client ECR 客户端（桩实现）。
type Client struct {
	baseURL  string
	username string
	password string
	region   string
}

// New 创建 ECR 客户端。rawCredential 为凭证解密后的 JSON。
func New(baseURL string, rawCredential []byte) (*Client, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		return nil, errors.New("ecr url is required")
	}
	var cred struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Region   string `json:"region"`
	}
	_ = json.Unmarshal(rawCredential, &cred)
	return &Client{
		baseURL:  baseURL,
		username: cred.Username,
		password: cred.Password,
		region:   cred.Region,
	}, nil
}

// CheckConnection 测试连通性。ECR 桩实现暂不可用。
func (c *Client) CheckConnection(ctx context.Context) error {
	return fmt.Errorf("ecr check connection: %w", ErrECRNotConfigured)
}

// GetImageMeta 查询镜像元信息。ECR 桩实现暂不可用。
func (c *Client) GetImageMeta(ctx context.Context, repository, tag string) (build.ImageMeta, error) {
	return build.ImageMeta{}, fmt.Errorf("ecr get image meta: %w", ErrECRNotConfigured)
}

// GetImageScanResult ECR 镜像扫描。桩实现暂不可用。
func (c *Client) GetImageScanResult(ctx context.Context, repository, tag string) (build.ImageScanStatus, map[string]any, error) {
	return build.ImgScanSkipped, map[string]any{"reason": "ecr adapter not configured"}, build.ErrScanNotSupported
}
