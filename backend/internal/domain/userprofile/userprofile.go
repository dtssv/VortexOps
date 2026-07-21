// Package userprofile 定义用户画像领域模型。
// 用户画像在 AI 助手多轮对话中持续更新，用于个性化回答。
package userprofile

import (
	"context"
	"errors"
	"time"

	"github.com/vortexops/vortexops/internal/domain"
)

// Profile 用户画像。
type Profile struct {
	ID                 int64
	UUID               string
	UserID             int64
	ExpertiseLevel     string   // beginner / intermediate / advanced / expert / unknown
	Roles              []string // ["java_engineer","sre","devops"]
	Domains            []string // ["kubernetes","spring","redis"]
	CommunicationStyle string   // concise / detailed / balanced
	PreferredLanguage  string   // zh-CN / en
	Summary            string   // LLM 生成的人物画像摘要
	InteractionCount   int
	LastUpdatedAt      *time.Time
	domain.Audit
}

// Repository 用户画像仓储接口。
type Repository interface {
	GetByUserID(ctx context.Context, userID int64) (*Profile, error)
	Upsert(ctx context.Context, p *Profile) (*Profile, error)
}

// 领域错误。
var (
	ErrProfileNotFound = errors.New("user profile not found")
)
