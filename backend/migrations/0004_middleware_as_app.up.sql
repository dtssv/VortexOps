-- 0004: 移除 Helm 中间件子系统，改为通过开放 API 以普通应用方式部署中间件。

DROP TABLE IF EXISTS vo_middleware_operations CASCADE;
DROP TABLE IF EXISTS vo_middleware_instances CASCADE;
DROP TABLE IF EXISTS vo_middleware_catalog CASCADE;

DELETE FROM vo_role_permissions WHERE permission_id in(select id from vo_permissions where code IN ('menu:middleware:view', 'middleware:manage'));
delete FROM vo_role_menus where menu_id in (select id FROM vo_menus WHERE permission_code IN ('menu:middleware:view', 'middleware:manage'));
DELETE FROM vo_menus WHERE permission_code IN ('menu:middleware:view', 'middleware:manage');
DELETE FROM vo_permissions WHERE code IN ('menu:middleware:view', 'middleware:manage');
delete FROM vo_role_menus where menu_id in (select id FROM vo_menus WHERE code = 'middleware');
DELETE FROM vo_menus WHERE code = 'middleware';

-- 重建空间概览视图（移除 middleware_count）。
CREATE OR REPLACE VIEW vo_v_workspace_overview AS
SELECT
    w.id, w.uuid, w.name, w.display_name, w.status, w.owner_id,
    (SELECT count(*) FROM vo_applications a WHERE a.workspace_id = w.id AND a.deleted = false) AS application_count,
    (SELECT count(*) FROM vo_groups g JOIN vo_applications a ON a.id = g.application_id
       WHERE a.workspace_id = w.id AND g.deleted = false) AS group_count,
    (SELECT count(*) FROM vo_inference_services s WHERE s.workspace_id = w.id AND s.deleted = false) AS inference_count,
    (SELECT count(*) FROM vo_workspace_members wm WHERE wm.workspace_id = w.id AND wm.deleted = false) AS member_count
FROM vo_workspaces w
WHERE w.deleted = false;
