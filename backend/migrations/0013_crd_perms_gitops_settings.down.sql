-- 0013 down: 回滚 CRD 权限 + GitOps/cosign/syft 设置
DELETE FROM vo_role_permissions
WHERE permission_id IN (SELECT id FROM vo_permissions WHERE code IN ('k8s:crd:view','k8s:crd:manage'))
  AND role_id IN (SELECT id FROM vo_roles WHERE code = 'platform_auditor');

DELETE FROM vo_permissions WHERE code IN ('k8s:crd:view','k8s:crd:manage');

DELETE FROM vo_system_settings WHERE key IN (
  'platform.cosign_path','platform.syft_path',
  'gitops.repo_url','gitops.branch','gitops.username','gitops.password','gitops.work_dir'
);
