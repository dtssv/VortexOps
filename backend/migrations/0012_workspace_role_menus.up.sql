-- 空间角色补齐菜单可见性；构建/日志菜单挂上真实权限码。
-- 去掉前端 FALLBACK 全量菜单后，开发者不能再靠「菜单为空就展示全部」看侧栏。

UPDATE vo_menus SET permission_code = 'build:trigger' WHERE code = 'builds' AND permission_code IS NULL;
UPDATE vo_menus SET permission_code = 'ops:session:view' WHERE code = 'ops-logs' AND permission_code IS NULL;

INSERT INTO vo_role_permissions (role_id, permission_id, granted, created_by)
SELECT r.id, p.id, true, NULL
FROM vo_roles r
JOIN vo_permissions p ON p.code IN (
  'menu:dashboard:view',
  'menu:workspace:view',
  'menu:application:view',
  'menu:release:view',
  'menu:config:view',
  'menu:pipeline:view',
  'menu:model:view',
  'menu:approval:view',
  'menu:diagnosis:view'
)
WHERE r.code IN ('workspace_owner', 'workspace_developer')
  AND r.deleted = false
  AND p.deleted = false
  AND NOT EXISTS (
    SELECT 1 FROM vo_role_permissions rp
    WHERE rp.role_id = r.id AND rp.permission_id = p.id
  );

INSERT INTO vo_role_permissions (role_id, permission_id, granted, created_by)
SELECT r.id, p.id, true, NULL
FROM vo_roles r
JOIN vo_permissions p ON p.code IN ('menu:dashboard:view', 'menu:workspace:view')
WHERE r.code = 'workspace_viewer'
  AND r.deleted = false
  AND p.deleted = false
  AND NOT EXISTS (
    SELECT 1 FROM vo_role_permissions rp
    WHERE rp.role_id = r.id AND rp.permission_id = p.id
  );
