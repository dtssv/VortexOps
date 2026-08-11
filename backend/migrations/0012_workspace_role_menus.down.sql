DELETE FROM vo_role_permissions rp
USING vo_roles r, vo_permissions p
WHERE rp.role_id = r.id AND rp.permission_id = p.id
  AND r.code IN ('workspace_owner', 'workspace_developer')
  AND p.code IN (
    'menu:dashboard:view',
    'menu:workspace:view',
    'menu:application:view',
    'menu:release:view',
    'menu:config:view',
    'menu:pipeline:view',
    'menu:model:view',
    'menu:approval:view',
    'menu:diagnosis:view'
  );

DELETE FROM vo_role_permissions rp
USING vo_roles r, vo_permissions p
WHERE rp.role_id = r.id AND rp.permission_id = p.id
  AND r.code = 'workspace_viewer'
  AND p.code IN ('menu:dashboard:view', 'menu:workspace:view');

UPDATE vo_menus SET permission_code = NULL WHERE code IN ('builds', 'ops-logs');
