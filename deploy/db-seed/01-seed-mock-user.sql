-- ============================================================================
-- VortexOps 开发环境 Mock 数据
-- ----------------------------------------------------------------------------
-- 由 db-seed 服务执行：在 apiserver migrate up 完成（vo_users 表存在）后运行。
-- 幂等：重复执行不应报错。
--
-- Mock 用户：
--   用户名: admin
--   邮箱:   admin@vortexops.local
--   密码:   admin123  （bcrypt cost=10 哈希，与 backend PasswordHasher 兼容）
--   角色:   platform_admin（平台管理员，拥有全部权限，可看到所有菜单）
-- ============================================================================

-- ---------- 1. Mock 用户 ----------
-- bcrypt 哈希对应明文 "admin123"（$2a$10$... 由 golang.org/x/crypto/bcrypt 生成）。
-- 注意：phone/avatar_url/external_id/last_login_ip 等字段使用 '' 而非 NULL，
-- 因为 identityrepo.scanUser 将这些列扫描到 string（非指针）字段，pgx 无法将 NULL 扫入 string。
INSERT INTO vo_users (
    uuid, username, email, phone, display_name, avatar_url,
    password_hash, auth_source, external_id, status,
    last_login_at, last_login_ip, password_changed_at, must_change_password,
    locale, timezone, metadata, version, created_by, updated_by
) VALUES (
    'a0000000-0000-0000-0000-000000000001',
    'admin',
    'admin@vortexops.local',
    '',
    '平台管理员',
    '',
    '$2a$10$lHuRbTOfhb1jhEXqUavea.yywOz0f5KE0ZxiyE7hYnQADjAwfB4yy',
    'local',
    '',
    'active',
    NULL, '', now(), false,
    'zh-CN', 'Asia/Shanghai', '{}'::jsonb, 1, 0, 0
)
ON CONFLICT (username) DO NOTHING;

-- ---------- 2. 绑定平台管理员角色 ----------
-- vo_roles seed 中 platform_admin 的 scope='platform'、code='platform_admin'。
-- 通过子查询取 id，避免硬编码（迁移顺序变化时仍生效）。
INSERT INTO vo_platform_role_bindings (user_id, role_id, expires_at, version, created_by, updated_by)
SELECT u.id, r.id, NULL, 1, NULL, NULL
FROM vo_users u, vo_roles r
WHERE u.username = 'admin'
  AND r.scope = 'platform'
  AND r.code = 'platform_admin'
  AND NOT EXISTS (
      SELECT 1 FROM vo_platform_role_bindings b
      WHERE b.user_id = u.id AND b.role_id = r.id AND b.deleted = false
  )
ON CONFLICT (user_id, role_id) DO NOTHING;
