-- 0014: 通知渠道默认 webhook 与 SMTP 系统设置
-- 外发通知在 Recipient 为空时回退使用系统设置中配置的默认地址。

INSERT INTO vo_system_settings (key, value, description, is_public) VALUES
 ('notif.dingtalk_webhook', '""'::jsonb, '钉钉机器人默认 webhook URL（外发通知 Recipient 为空时回退）', false),
 ('notif.feishu_webhook',   '""'::jsonb, '飞书自定义机器人默认 webhook URL', false),
 ('notif.wecom_webhook',    '""'::jsonb, '企业微信群机器人默认 webhook URL', false),
 ('notif.webhook_url',      '""'::jsonb, '通用 webhook 默认 URL', false),
 ('notif.smtp_host',        '""'::jsonb, 'SMTP 邮件服务器主机（为空则回退环境变量 SMTP_HOST）', false),
 ('notif.smtp_port',        '587',       'SMTP 端口（默认 587）', false),
 ('notif.smtp_username',    '""'::jsonb, 'SMTP 用户名', false),
 ('notif.smtp_password',    '""'::jsonb, 'SMTP 密码', false),
 ('notif.smtp_from',        '""'::jsonb, '发件人地址（为空回退环境变量 SMTP_FROM）', false)
ON CONFLICT (key) DO NOTHING;
