// Package collabapp 是协作领域的应用服务层（通知管理）。
package collabapp

import (
	"context"
	"errors"
	"strconv"

	"github.com/vortexops/vortexops/internal/domain/collab"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// Service 协作应用服务。
type Service struct {
	repo    collab.Repository
	dispatcher PendingDispatcher // 可空：未注入时外发通知保持 pending
}

// New 创建协作服务。
func New(repo collab.Repository) *Service {
	return &Service{repo: repo}
}

// CreateNotificationInput 创建通知输入。
type CreateNotificationInput struct {
	UserID       int64
	Channel      collab.NotificationChannel
	TemplateCode string
	Recipient    string
	Subject      string
	Body         string
	Payload      map[string]any
}

// CreateNotification 创建通知。
// 站内通知（in_app）立即置 sent；外发渠道（webhook/email/dingtalk/feishu/wecom）置 pending，
// 由 Dispatcher 异步投递后回写 sent/failed。
func (s *Service) CreateNotification(ctx context.Context, in CreateNotificationInput) (*collab.Notification, error) {
	if in.UserID == 0 {
		return nil, apperr.Validation("user_id is required", nil)
	}
	if in.Channel == "" {
		in.Channel = collab.ChannelInApp
	}
	status := collab.NotifPending
	if in.Channel == collab.ChannelInApp {
		status = collab.NotifSent
	}
	n := &collab.Notification{
		UserID: in.UserID, Channel: in.Channel, TemplateCode: in.TemplateCode, Recipient: in.Recipient,
		Subject: in.Subject, Body: in.Body, Payload: in.Payload, Status: status,
	}
	if err := s.repo.CreateNotification(ctx, n); err != nil {
		return nil, apperr.Internal("create notification", err)
	}
	return n, nil
}

// ListNotifications 分页查询当前用户通知。
func (s *Service) ListNotifications(ctx context.Context, userID int64, unreadOnly bool, page, size int) ([]*collab.Notification, int64, error) {
	items, total, err := s.repo.ListNotifications(ctx, collab.NotificationQuery{
		UserID: userID, UnreadOnly: unreadOnly, Offset: (page - 1) * size, Limit: size,
	})
	if err != nil {
		return nil, 0, apperr.Internal("list notifications", err)
	}
	return items, total, nil
}

// MarkRead 标记单条已读。
func (s *Service) MarkRead(ctx context.Context, id int64) error {
	if err := s.repo.MarkRead(ctx, id); err != nil {
		if errors.Is(err, collab.ErrNotificationNotFound) {
			return apperr.NotFound("notification", strconv.FormatInt(id, 10))
		}
		return apperr.Internal("mark read", err)
	}
	return nil
}

// MarkAllRead 标记当前用户全部已读。
func (s *Service) MarkAllRead(ctx context.Context, userID int64) error {
	if err := s.repo.MarkAllRead(ctx, userID); err != nil {
		return apperr.Internal("mark all read", err)
	}
	return nil
}

// CountUnread 统计未读数。
func (s *Service) CountUnread(ctx context.Context, userID int64) (int64, error) {
	count, err := s.repo.CountUnread(ctx, userID)
	if err != nil {
		return 0, apperr.Internal("count unread", err)
	}
	return count, nil
}

// PendingDispatcher 投递器接口（由 notif.Dispatcher 实现，避免 application 直接依赖 infrastructure）。
type PendingDispatcher interface {
	Dispatch(ctx context.Context, n *collab.Notification)
}

// WithDispatcher 注入通知投递器（同步投递）。
func (s *Service) WithDispatcher(d PendingDispatcher) *Service {
	s.dispatcher = d
	return s
}

// DispatchPending 拉取 pending 外发通知并投递。由后台轮询 goroutine 调用。
// batchSize 每轮最多投递条数。
func (s *Service) DispatchPending(ctx context.Context, batchSize int) error {
	if s.dispatcher == nil {
		return nil
	}
	if batchSize <= 0 {
		batchSize = 50
	}
	items, err := s.repo.ListPending(ctx, collab.PendingQuery{Limit: batchSize})
	if err != nil {
		return apperr.Internal("list pending notifications", err)
	}
	for _, n := range items {
		if ctx.Err() != nil {
			return nil
		}
		s.dispatcher.Dispatch(ctx, n)
	}
	return nil
}
