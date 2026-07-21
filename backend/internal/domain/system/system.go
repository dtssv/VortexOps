// Package system 是平台系统设置领域（vo_system_settings）的实体与仓储接口。
// 用于全局配置项：平台名称、默认 Jenkins/Registry 实例 ID 等。
// value 以 JSONB 存储，领域层用 any 持有（具体类型由调用方按 key 约定解释）。
package system

import (
	"context"
	"errors"

	"github.com/vortexops/vortexops/internal/domain"
)

// Setting 系统设置实体。
type Setting struct {
	ID          int64
	Key         string
	Value       any
	Description string
	IsPublic    bool
	domain.Audit
}

// 领域错误。
var (
	ErrSettingNotFound = errors.New("system setting not found")
	ErrSettingExists   = errors.New("system setting already exists")
)

// Query 系统设置查询。
type Query struct {
	PublicOnly bool
	Search     string
}

// Repository 系统设置仓储接口。
type Repository interface {
	GetByKey(ctx context.Context, key string) (*Setting, error)
	List(ctx context.Context, q Query) ([]*Setting, error)
	Upsert(ctx context.Context, s *Setting) (*Setting, error)
	Delete(ctx context.Context, key string, deletedBy int64) error
}
