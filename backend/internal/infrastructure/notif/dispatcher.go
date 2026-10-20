// Package notif 是通知投递层：把 collab.Notification 按渠道（webhook/email/dingtalk/feishu/wecom）
// 实际发送到外部系统。in_app 渠道不经过此层（CreateNotification 时已落库）。
//
// 设计：ChannelSender 按渠道实现，Dispatcher 聚合所有 sender 并负责状态回写。
// webhook/email/dingtalk 的目标地址从 Notification.Recipient（URL/邮箱/机器人 webhook）取；
// feishu/wecom 同理使用自定义机器人 webhook URL 作为 Recipient。
package notif

import (
	"context"

	"github.com/vortexops/vortexops/internal/domain/collab"
)

// ChannelSender 单渠道发送器。返回错误时 Dispatcher 会把通知标记为 failed。
type ChannelSender interface {
	Channel() collab.NotificationChannel
	Send(ctx context.Context, n *collab.Notification) error
}

// ChannelRepo 通知仓储子集（Dispatcher 回写状态用）。
type ChannelRepo interface {
	MarkSent(ctx context.Context, id int64, status collab.NotificationStatus, errorMessage string) error
}

// DefaultRecipientResolver 按渠道解析默认 Recipient（webhook URL/邮箱）。
// 当 Notification.Recipient 为空时回退使用系统设置中配置的默认地址。
type DefaultRecipientResolver interface {
	ResolveDefault(ctx context.Context, channel collab.NotificationChannel) string
}

// Dispatcher 通知投递调度器。聚合各渠道 sender。
type Dispatcher struct {
	repo     ChannelRepo
	senders  map[collab.NotificationChannel]ChannelSender
	resolver DefaultRecipientResolver
}

// NewDispatcher 创建调度器。
func NewDispatcher(repo ChannelRepo) *Dispatcher {
	return &Dispatcher{repo: repo, senders: map[collab.NotificationChannel]ChannelSender{}}
}

// Register 注册渠道发送器。
func (d *Dispatcher) Register(s ChannelSender) {
	d.senders[s.Channel()] = s
}

// WithResolver 注入默认 Recipient 解析器。
func (d *Dispatcher) WithResolver(r DefaultRecipientResolver) *Dispatcher {
	d.resolver = r
	return d
}

// Dispatch 投递单条通知。in_app 跳过（已落库）。
// 投递成功标记 sent，失败标记 failed 并记录 error_message。
// 当 Recipient 为空且配置了 resolver 时，回退使用系统默认 webhook。
func (d *Dispatcher) Dispatch(ctx context.Context, n *collab.Notification) {
	if n == nil || n.Channel == collab.ChannelInApp {
		return
	}
	// Recipient 为空时回退默认地址（克隆避免修改原对象语义）。
	if n.Recipient == "" && d.resolver != nil {
		def := d.resolver.ResolveDefault(ctx, n.Channel)
		if def != "" {
			cp := *n
			cp.Recipient = def
			n = &cp
		}
	}
	sender, ok := d.senders[n.Channel]
	if !ok {
		_ = d.repo.MarkSent(ctx, n.ID, collab.NotifFailed, "no sender for channel: "+string(n.Channel))
		return
	}
	if err := sender.Send(ctx, n); err != nil {
		_ = d.repo.MarkSent(ctx, n.ID, collab.NotifFailed, err.Error())
		return
	}
	_ = d.repo.MarkSent(ctx, n.ID, collab.NotifSent, "")
}
