// Package collab 是协作领域的核心实体与仓储接口（通知）。
// 注：favorites 表未在 schema 中定义，本期不实现；如需可在后续 schema 迭代补表。
package collab

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// NotificationChannel 通知渠道。
type NotificationChannel string

const (
	ChannelInApp   NotificationChannel = "in_app"
	ChannelEmail   NotificationChannel = "email"
	ChannelWebhook NotificationChannel = "webhook"
	ChannelDingTalk NotificationChannel = "dingtalk"
)

// NotificationStatus 通知状态。
type NotificationStatus string

const (
	NotifPending NotificationStatus = "pending"
	NotifSent    NotificationStatus = "sent"
	NotifFailed  NotificationStatus = "failed"
	NotifRead    NotificationStatus = "read"
)

// Notification 通知条目。
type Notification struct {
	ID           int64
	UUID         uuid.UUID
	UserID       int64
	Channel      NotificationChannel
	TemplateCode string
	Recipient    string
	Subject      string
	Body         string
	Payload      map[string]any
	Status       NotificationStatus
	SentAt       *time.Time
	ReadAt       *time.Time
	ErrorMessage string
	CreatedAt    time.Time
}

// NotificationQuery 通知查询。
type NotificationQuery struct {
	UserID     int64
	Status     NotificationStatus
	UnreadOnly bool
	Offset     int
	Limit      int
}

// 领域错误。
var (
	ErrNotificationNotFound = errors.New("notification not found")
)

// Repository 协作领域仓储接口。
type Repository interface {
	CreateNotification(ctx context.Context, n *Notification) error
	GetNotification(ctx context.Context, id int64) (*Notification, error)
	ListNotifications(ctx context.Context, q NotificationQuery) ([]*Notification, int64, error)
	MarkRead(ctx context.Context, id int64) error
	MarkAllRead(ctx context.Context, userID int64) error
	CountUnread(ctx context.Context, userID int64) (int64, error)
}
