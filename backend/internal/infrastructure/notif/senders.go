// Package notif 的各渠道发送器实现。
//
// 通用 HTTP POST（JSON body）模型覆盖 webhook/dingtalk/feishu/wecom 四类机器人：
//   - webhook：通用 JSON POST，body 为 {subject, body, payload}
//   - dingtalk：钉钉机器人，body 为 {"msgtype":"markdown","markdown":{"title","text"}}
//   - feishu：飞书自定义机器人，body 为 {"msg_type":"text","content":{"text"}}（简化，未实现卡片签名）
//   - wecom：企业微信群机器人，body 为 {"msgtype":"markdown","markdown":{"content"}}
//
// Recipient 字段统一存目标 webhook URL（或邮箱地址，email 渠道）。
package notif

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/smtp"
	"strings"
	"time"

	"github.com/vortexops/vortexops/internal/domain/collab"
)

// httpDo 通用 JSON POST。超时 10s。
func httpDo(ctx context.Context, url string, body any) error {
	if strings.TrimSpace(url) == "" {
		return fmt.Errorf("recipient url is empty")
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal body: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("http post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}
	return nil
}

// ============================================================================
// 通用 Webhook
// ============================================================================

// WebhookSender 通用 webhook 发送器。body 为标准 JSON。
type WebhookSender struct{}

// Channel 渠道。
func (WebhookSender) Channel() collab.NotificationChannel { return collab.ChannelWebhook }

// Send POST JSON 到 Recipient URL。
func (WebhookSender) Send(ctx context.Context, n *collab.Notification) error {
	return httpDo(ctx, n.Recipient, map[string]any{
		"subject": n.Subject,
		"body":    n.Body,
		"payload": n.Payload,
	})
}

// ============================================================================
// 钉钉机器人
// ============================================================================

// DingTalkSender 钉钉自定义机器人（markdown 消息）。
// 注意：若机器人开启了加签/关键词/加密，需在 Recipient URL 中携带签名参数。
type DingTalkSender struct{}

// Channel 渠道。
func (DingTalkSender) Channel() collab.NotificationChannel { return collab.ChannelDingTalk }

// Send 发送 markdown 消息。
func (DingTalkSender) Send(ctx context.Context, n *collab.Notification) error {
	text := n.Body
	if n.Subject != "" {
		text = fmt.Sprintf("### %s\n\n%s", n.Subject, n.Body)
	}
	return httpDo(ctx, n.Recipient, map[string]any{
		"msgtype":  "markdown",
		"markdown": map[string]string{"title": n.Subject, "text": text},
	})
}

// ============================================================================
// 飞书机器人
// ============================================================================

// FeishuSender 飞书自定义机器人（text 消息）。
// 支持自定义机器人签名校验：若需签名，请在 Recipient URL 上携带 sign 参数
// （飞书签名算法为 HMAC-SHA256(timestamp+"\n"+secret)，本实现暂不做签名，使用无签名机器人）。
type FeishuSender struct{}

// Channel 渠道。
func (FeishuSender) Channel() collab.NotificationChannel { return collab.ChannelFeishu }

// Send 发送 text 消息。
func (FeishuSender) Send(ctx context.Context, n *collab.Notification) error {
	text := n.Body
	if n.Subject != "" {
		text = fmt.Sprintf("%s\n%s", n.Subject, n.Body)
	}
	return httpDo(ctx, n.Recipient, map[string]any{
		"msg_type": "text",
		"content":  map[string]string{"text": text},
	})
}

// ============================================================================
// 企业微信群机器人
// ============================================================================

// WeComSender 企业微信群机器人（markdown 消息）。
type WeComSender struct{}

// Channel 渠道。
func (WeComSender) Channel() collab.NotificationChannel { return collab.ChannelWeCom }

// Send 发送 markdown 消息。
func (WeComSender) Send(ctx context.Context, n *collab.Notification) error {
	text := n.Body
	if n.Subject != "" {
		text = fmt.Sprintf("**%s**\n%s", n.Subject, n.Body)
	}
	return httpDo(ctx, n.Recipient, map[string]any{
		"msgtype":  "markdown",
		"markdown": map[string]string{"content": text},
	})
}

// ============================================================================
// Email（SMTP）
// ============================================================================

// EmailConfig SMTP 配置（部署时注入）。
type EmailConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string // 发件人地址
	Enabled  bool
}

// EmailConfigProvider SMTP 配置提供者（运行时动态读取，支持系统设置覆盖环境变量）。
type EmailConfigProvider interface {
	EmailConfig(ctx context.Context) EmailConfig
}

// EmailSender 邮件发送器。Recipient 为收件人邮箱地址。
// 支持两种模式：静态配置（NewEmailSender）或动态配置（NewEmailSenderWithProvider）。
type EmailSender struct {
	cfg      EmailConfig
	provider EmailConfigProvider
}

// NewEmailSender 创建静态配置的邮件发送器。
func NewEmailSender(cfg EmailConfig) *EmailSender {
	return &EmailSender{cfg: cfg}
}

// NewEmailSenderWithProvider 创建动态配置的邮件发送器（每次发送读取最新配置）。
func NewEmailSenderWithProvider(p EmailConfigProvider) *EmailSender {
	return &EmailSender{provider: p}
}

// Channel 渠道。
func (e *EmailSender) Channel() collab.NotificationChannel { return collab.ChannelEmail }

// Send 通过 SMTP 发送纯文本邮件。
func (e *EmailSender) Send(ctx context.Context, n *collab.Notification) error {
	cfg := e.cfg
	if e.provider != nil {
		cfg = e.provider.EmailConfig(ctx)
	}
	if cfg.Host == "" {
		return fmt.Errorf("email smtp host not configured")
	}
	to := strings.TrimSpace(n.Recipient)
	if to == "" {
		return fmt.Errorf("email recipient is empty")
	}
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	var auth smtp.Auth
	if cfg.Username != "" {
		auth = smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
	}
	from := cfg.From
	if from == "" {
		from = "noreply@vortexops.local"
	}
	msg := buildEmailMessage(from, to, n.Subject, n.Body)
	if err := smtp.SendMail(addr, auth, from, []string{to}, msg); err != nil {
		return fmt.Errorf("smtp send: %w", err)
	}
	return nil
}

// buildEmailMessage 构造 RFC 822 邮件内容。
func buildEmailMessage(from, to, subject, body string) []byte {
	header := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\n", from, to, subject)
	header += "Content-Type: text/plain; charset=UTF-8\r\n\r\n"
	return []byte(header + body)
}
