package extapiapp

import (
	"context"

	"github.com/vortexops/vortexops/internal/application/configapp"
	configdomain "github.com/vortexops/vortexops/internal/domain/config"
)
// LocalConfigWriter 分组本地配置写入（供中间件开放 API 使用）。
type LocalConfigWriter interface {
	UpsertLocalConfig(ctx context.Context, in configapp.UpsertLocalConfigInput) (*configdomain.GroupLocalConfig, error)
}
