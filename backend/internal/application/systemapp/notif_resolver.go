// Package systemapp 的通知渠道默认地址解析器适配器。
// 实现 notif.DefaultRecipientResolver 与 notif.EmailConfigProvider（结构化接口）。
// 当外发通知 Recipient 为空时，按渠道回退到系统设置中配置的默认 webhook URL。
package systemapp

import (
	"context"

	"github.com/vortexops/vortexops/internal/domain/collab"
	"github.com/vortexops/vortexops/internal/infrastructure/notif"
)

// NotifDefaultResolver 把 systemapp.Service 适配为通知默认 Recipient 解析器。
type NotifDefaultResolver struct {
	svc *Service
}

// NewNotifDefaultResolver 创建解析器。
func NewNotifDefaultResolver(svc *Service) *NotifDefaultResolver {
	return &NotifDefaultResolver{svc: svc}
}

// ResolveDefault 按渠道返回系统设置中配置的默认 webhook URL；未配置返回空。
func (r *NotifDefaultResolver) ResolveDefault(ctx context.Context, channel collab.NotificationChannel) string {
	cfg, err := r.svc.GetNotifChannelConfig(ctx)
	if err != nil {
		return ""
	}
	switch channel {
	case collab.ChannelDingTalk:
		return cfg.DingTalkWebhook
	case collab.ChannelFeishu:
		return cfg.FeishuWebhook
	case collab.ChannelWeCom:
		return cfg.WeComWebhook
	case collab.ChannelWebhook:
		return cfg.WebhookURL
	default:
		return ""
	}
}

// EmailConfigProviderImpl 把 systemapp.Service 适配为 notif.EmailConfigProvider。
// SMTP 系统设置优先；为空时回退到环境变量配置（fallback）。
type EmailConfigProviderImpl struct {
	svc      *Service
	fallback notif.EmailConfig
}

// NewEmailConfigProvider 创建邮件配置提供者。
// fallback 为环境变量配置（系统设置为空时回退使用）。
func NewEmailConfigProvider(svc *Service, fallback notif.EmailConfig) *EmailConfigProviderImpl {
	return &EmailConfigProviderImpl{svc: svc, fallback: fallback}
}

// EmailConfig 返回当前生效的 SMTP 配置（系统设置优先）。
func (p *EmailConfigProviderImpl) EmailConfig(ctx context.Context) notif.EmailConfig {
	cfg, err := p.svc.GetNotifChannelConfig(ctx)
	if err != nil {
		return p.fallback
	}
	out := p.fallback
	if cfg.SMTPHost != "" {
		out.Host = cfg.SMTPHost
		out.Port = cfg.SMTPPort
		out.Username = cfg.SMTPUsername
		out.Password = cfg.SMTPPassword
		out.Enabled = true
	}
	if cfg.SMTPFrom != "" {
		out.From = cfg.SMTPFrom
	}
	return out
}
