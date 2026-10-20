-- 0013: CRD 权限种子 + GitOps/cosign/syft 系统设置
-- 新增 K8s CRD 管理权限，并给 platform_auditor 补只读授权。

-- 1. 新增 CRD 权限
INSERT INTO vo_permissions (code, name, category, scope, description, sort_order, enabled) VALUES
 ('k8s:crd:view',   'K8s-CRD 查看', 'action', 'platform', '查看 CRD 定义与自定义资源实例', 441, true),
 ('k8s:crd:manage', 'K8s-CRD 管理', 'action', 'platform', '删除自定义资源实例',           442, true)
ON CONFLICT (code) DO NOTHING;

-- 2. platform_auditor 补 k8s:crd:view 只读授权
INSERT INTO vo_role_permissions (role_id, permission_id, granted, created_by)
SELECT r.id, p.id, true, NULL
FROM vo_roles r JOIN vo_permissions p ON p.code = 'k8s:crd:view'
WHERE r.code = 'platform_auditor'
ON CONFLICT DO NOTHING;

-- 3. 制品签名 / SBOM 工具路径
INSERT INTO vo_system_settings (key, value, description, is_public) VALUES
 ('platform.cosign_path', '""'::jsonb, 'cosign 二进制路径（空则用 PATH）', false),
 ('platform.syft_path',   '""'::jsonb, 'syft 二进制路径（空则用 PATH）', false)
ON CONFLICT (key) DO NOTHING;

-- 4. GitOps manifest 仓库配置
INSERT INTO vo_system_settings (key, value, description, is_public) VALUES
 ('gitops.repo_url',  '""'::jsonb, 'GitOps manifest 仓库 URL（为空则不启用）', false),
 ('gitops.branch',    '"main"',    'GitOps manifest 仓库分支', false),
 ('gitops.username',  '""'::jsonb, 'GitOps 仓库认证用户名', false),
 ('gitops.password',  '""'::jsonb, 'GitOps 仓库认证密码/token', false),
 ('gitops.work_dir',  '""'::jsonb, 'GitOps 本地工作目录（空则每次 clone 到临时目录）', false)
ON CONFLICT (key) DO NOTHING;
