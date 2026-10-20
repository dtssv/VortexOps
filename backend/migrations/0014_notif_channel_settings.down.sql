-- 0014 rollback: 删除通知渠道默认 webhook 与 SMTP 系统设置
DELETE FROM vo_system_settings WHERE key IN (
 'notif.dingtalk_webhook',
 'notif.feishu_webhook',
 'notif.wecom_webhook',
 'notif.webhook_url',
 'notif.smtp_host',
 'notif.smtp_port',
 'notif.smtp_username',
 'notif.smtp_password',
 'notif.smtp_from'
);
