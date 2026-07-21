package build

import (
	"context"
	"errors"
)

// ErrScanNotSupported 表示当前镜像仓库类型不支持镜像扫描。
var ErrScanNotSupported = errors.New("registry adapter: image scan not supported")

// RegistryAdapter 镜像仓库适配器抽象，按 Registry.Type 走不同实现。
// 实现位于 infrastructure/{harbor,dockerregistry,acr,ecr}。
type RegistryAdapter interface {
	// GetImageMeta 查询镜像元信息（digest/size/labels）。
	// 构建成功后由 BuildPoller 调用填充 ImageVersion 元数据。
	GetImageMeta(ctx context.Context, repository, tag string) (ImageMeta, error)

	// CheckConnection 测试连通性（系统设置页「测试连接」按钮）。
	CheckConnection(ctx context.Context) error

	// GetImageScanResult 查询镜像扫描结果（仅 Harbor/ACR 等支持扫描的仓库）。
	// 不支持扫描的仓库返回 ErrScanNotSupported。
	GetImageScanResult(ctx context.Context, repository, tag string) (status ImageScanStatus, result map[string]any, err error)
}

// ImageMeta 镜像元数据。
type ImageMeta struct {
	Repository string
	Tag        string
	Digest     string
	SizeBytes  int64
	Labels     map[string]string
}

// RegistryAdapterFactory 按实例构建 RegistryAdapter。
// 由 infrastructure/buildinfra/connector 提供实现，按 Registry.Type 路由。
type RegistryAdapterFactory func(ctx context.Context, reg *Registry) (RegistryAdapter, error)
