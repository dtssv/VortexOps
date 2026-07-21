// Package domain 提供跨领域共享的基础类型与错误。
package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Audit 是所有实体的通用审计字段（与 schema.sql 的通用列对齐）。
type Audit struct {
	Version    int
	CreatedAt  time.Time
	CreatedBy  int64
	UpdatedAt  time.Time
	UpdatedBy  int64
	Deleted    bool
	DeletedAt  *time.Time
	DeletedBy  int64
}

// ErrNotFound 通用未找到错误。领域层可定义更具体的版本。
var ErrNotFound = errors.New("entity not found")

// ErrConflict 通用并发冲突（乐观锁版本不匹配）。
var ErrConflict = errors.New("concurrent modification conflict")

// ErrAlreadyExists 通用已存在错误。
var ErrAlreadyExists = errors.New("entity already exists")

// ErrValidation 通用校验错误。
var ErrValidation = errors.New("validation failed")

// NewUUID 生成 UUID。
func NewUUID() uuid.UUID { return uuid.New() }
