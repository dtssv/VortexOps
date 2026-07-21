// Package buildinfra 提供构建领域跨基础设施的连接器：
// 用凭证仓储解密 Jenkins / 镜像仓库凭证，构建真实客户端。
package buildinfra

import (
	"context"
	"fmt"

	"github.com/vortexops/vortexops/internal/application/buildapp"
	"github.com/vortexops/vortexops/internal/domain/build"
	"github.com/vortexops/vortexops/internal/domain/cluster"
	"github.com/vortexops/vortexops/internal/infrastructure/acr"
	"github.com/vortexops/vortexops/internal/infrastructure/dockerregistry"
	"github.com/vortexops/vortexops/internal/infrastructure/ecr"
	"github.com/vortexops/vortexops/internal/infrastructure/harbor"
	"github.com/vortexops/vortexops/internal/infrastructure/jenkins"
	"github.com/vortexops/vortexops/internal/platform/security"
)

// Connector 跨基础设施连接器，按实例构建真实 Jenkins / RegistryAdapter 客户端。
type Connector struct {
	credRepo cluster.Repository
	cipher   *security.FieldCipher
}

// NewConnector 创建连接器。
func NewConnector(credRepo cluster.Repository, cipher *security.FieldCipher) *Connector {
	return &Connector{credRepo: credRepo, cipher: cipher}
}

// JenkinsClient 工厂方法：解密 Jenkins 凭证并构建客户端。
func (c *Connector) JenkinsClient(ctx context.Context, jk *build.JenkinsInstance) (build.JenkinsClient, error) {
	if jk.CredentialID == 0 {
		return nil, fmt.Errorf("jenkins 实例 %d 未关联凭证", jk.ID)
	}
	cred, err := c.credRepo.GetCredentialByID(ctx, jk.CredentialID)
	if err != nil {
		return nil, fmt.Errorf("查找 Jenkins 凭证(id=%d)失败: %w", jk.CredentialID, err)
	}
	payload, err := c.cipher.Decrypt(cred.PayloadEncrypted)
	if err != nil {
		return nil, fmt.Errorf("解密 Jenkins 凭证失败: %w", err)
	}
	client, err := jenkins.New(jk.URL, payload)
	if err != nil {
		return nil, fmt.Errorf("构建 Jenkins 客户端失败（请检查 URL 或凭证格式 username/api_token）: %w", err)
	}
	return client, nil
}

// RegistryAdapter 工厂方法：按 reg.Type 路由到对应实现，解密凭证并构建适配器。
// 满足 build.RegistryAdapterFactory 签名。
func (c *Connector) RegistryAdapter(ctx context.Context, reg *build.Registry) (build.RegistryAdapter, error) {
	if reg == nil {
		return nil, fmt.Errorf("registry is nil")
	}
	var payload []byte
	if reg.CredentialID != 0 {
		cred, err := c.credRepo.GetCredentialByID(ctx, reg.CredentialID)
		if err != nil {
			return nil, fmt.Errorf("查找镜像仓库凭证(id=%d)失败: %w", reg.CredentialID, err)
		}
		payload, err = c.cipher.Decrypt(cred.PayloadEncrypted)
		if err != nil {
			return nil, fmt.Errorf("解密镜像仓库凭证失败: %w", err)
		}
	} else {
		return nil, fmt.Errorf("镜像仓库实例 %d 未关联凭证", reg.ID)
	}
	switch reg.Type {
	case build.RegistryHarbor:
		client, err := harbor.New(reg.URL, payload)
		if err != nil {
			return nil, fmt.Errorf("构建 Harbor 适配器失败（请检查 URL 或凭证格式 username/password）: %w", err)
		}
		return client, nil
	case build.RegistryDocker:
		client, err := dockerregistry.New(reg.URL, payload)
		if err != nil {
			return nil, fmt.Errorf("构建 Docker Registry 适配器失败: %w", err)
		}
		return client, nil
	case build.RegistryACR:
		client, err := acr.New(reg.URL, payload)
		if err != nil {
			return nil, fmt.Errorf("构建 ACR 适配器失败: %w", err)
		}
		return client, nil
	case build.RegistryECR:
		client, err := ecr.New(reg.URL, payload)
		if err != nil {
			return nil, fmt.Errorf("构建 ECR 适配器失败: %w", err)
		}
		return client, nil
	default:
		return nil, fmt.Errorf("不支持的镜像仓库类型: %s", reg.Type)
	}
}

// HarborClient 兼容方法：返回默认 Harbor 适配器（仅当 reg.Type == harbor 时可用）。
// 新代码请使用 RegistryAdapter。
func (c *Connector) HarborClient(ctx context.Context, reg *build.Registry) (build.RegistryAdapter, error) {
	return c.RegistryAdapter(ctx, reg)
}

// Compile-time assertions。
var (
	_ buildapp.JenkinsClientFactory  = (*Connector)(nil).JenkinsClient
	_ build.RegistryAdapterFactory   = (*Connector)(nil).RegistryAdapter
)
