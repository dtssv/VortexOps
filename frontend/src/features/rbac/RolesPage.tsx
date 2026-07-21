import { useEffect, useState } from 'react';
import { Button, Drawer, Form, Input, Modal, Select, Table, Tag, Tree, App, Space } from 'antd';
import { PlusOutlined, KeyOutlined, MenuOutlined } from '@ant-design/icons';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { ColumnsType } from 'antd/es/table';
import { PageContainer } from '@/components/PageContainer';
import { EmptyState } from '@/components/EmptyState';
import { rbacApi } from '@/api/rbac';
import type { Permission, Role, Menu } from '@/types';
import { confirmDanger } from '@/utils/action';
import { formatTime } from '@/utils/format';

const SCOPE_OPTIONS = [
  { label: '平台', value: 'platform' },
  { label: '工作空间', value: 'workspace' },
  { label: '应用', value: 'application' },
];

export default function RolesPage() {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const [createOpen, setCreateOpen] = useState(false);
  const [permDrawer, setPermDrawer] = useState<{ open: boolean; role?: Role }>({ open: false });
  const [menuDrawer, setMenuDrawer] = useState<{ open: boolean; role?: Role }>({ open: false });
  const [createForm] = Form.useForm();
  const [selectedCodes, setSelectedCodes] = useState<string[]>([]);
  const [checkedMenuIds, setCheckedMenuIds] = useState<number[]>([]);
  const [menuInitialized, setMenuInitialized] = useState(false);

  const { data: rolesPage, isLoading } = useQuery({
    queryKey: ['roles'],
    queryFn: () => rbacApi.listRoles({ page: 1, size: 200 }),
  });
  const roles = rolesPage?.items;

  const { data: allPermissions } = useQuery({
    queryKey: ['permissions', 'all-for-roles', { page: 1, size: 500 }],
    queryFn: () => rbacApi.listPermissions({ page: 1, size: 500 }),
    enabled: permDrawer.open,
  });

  const { data: rolePermissions } = useQuery({
    queryKey: ['role-permissions', permDrawer.role?.id],
    queryFn: () => rbacApi.listPermissionsByRole(permDrawer.role!.id),
    enabled: !!permDrawer.role?.id,
  });

  // 全量平台菜单（用于菜单绑定抽屉的树）。
  const { data: allMenus } = useQuery({
    queryKey: ['menus', 'all-for-role-binding'],
    queryFn: () => rbacApi.listMenus(),
    enabled: menuDrawer.open,
  });

  // 角色已绑定的菜单 id 集合。
  const { data: roleMenus } = useQuery({
    queryKey: ['role-menus', menuDrawer.role?.id],
    queryFn: () => rbacApi.listMenusByRole(menuDrawer.role!.id),
    enabled: menuDrawer.open && !!menuDrawer.role?.id,
  });

  const createMutation = useMutation({
    mutationFn: (body: Partial<Role>) => rbacApi.createRole(body),
    onSuccess: () => {
      message.success('角色已创建');
      setCreateOpen(false);
      createForm.resetFields();
      queryClient.invalidateQueries({ queryKey: ['roles'] });
    },
    onError: (e: any) => message.error(e?.message || '创建失败'),
  });

  const deleteMutation = useMutation({
    mutationFn: (id: number) => rbacApi.deleteRole(id),
    onSuccess: () => {
      message.success('角色已删除');
      queryClient.invalidateQueries({ queryKey: ['roles'] });
    },
    onError: (e: any) => message.error(e?.message || '删除失败'),
  });

  const grantMutation = useMutation({
    mutationFn: ({ roleId, codes }: { roleId: number; codes: string[] }) =>
      rbacApi.grantPermissions(roleId, { permission_codes: codes }),
    onSuccess: () => {
      message.success('权限已保存');
      queryClient.invalidateQueries({ queryKey: ['role-permissions', permDrawer.role?.id] });
      setPermDrawer({ open: false });
    },
    onError: (e: any) => message.error(e?.message || '保存失败'),
  });

  const bindMenusMutation = useMutation({
    mutationFn: ({ roleId, menuIds }: { roleId: number; menuIds: number[] }) =>
      rbacApi.bindRoleMenus(roleId, { menu_ids: menuIds, clear: true }),
    onSuccess: () => {
      message.success('菜单绑定已保存');
      queryClient.invalidateQueries({ queryKey: ['role-menus', menuDrawer.role?.id] });
      queryClient.invalidateQueries({ queryKey: ['me', 'menus'] });
      setMenuDrawer({ open: false });
    },
    onError: (e: any) => message.error(e?.message || '保存失败'),
  });

  const openPermDrawer = (role: Role) => {
    setPermDrawer({ open: true, role });
    setSelectedCodes([]);
  };

  const openMenuDrawer = (role: Role) => {
    setMenuDrawer({ open: true, role });
    setMenuInitialized(false);
    setCheckedMenuIds([]);
  };

  // 当 roleMenus 加载完成时同步勾选集合（仅初始化一次，避免覆盖用户编辑）。
  useEffect(() => {
    if (menuDrawer.open && !menuInitialized && roleMenus) {
      setCheckedMenuIds(roleMenus.map((m) => m.id));
      setMenuInitialized(true);
    }
  }, [menuDrawer.open, menuInitialized, roleMenus]);

  // 把扁平菜单列表构建为 antd Tree 的 treeData + 展开 key。
  function buildMenuTreeData(menus: Menu[]): { treeData: any[]; expandedKeys: string[] } {
    const map = new Map<number, any>();
    const roots: any[] = [];
    const expanded: string[] = [];
    menus.forEach((m) => {
      map.set(m.id, { key: String(m.id), title: `${m.name}${m.permission_code ? ` (${m.permission_code})` : ''}`, children: [] });
    });
    menus.forEach((m) => {
      const node = map.get(m.id);
      if (!m.parent_id || !map.has(m.parent_id)) {
        roots.push(node);
        if (node.children.length === 0 && m.menu_type === 'directory') {
          // 不展开空目录
        }
      } else {
        map.get(m.parent_id).children.push(node);
      }
    });
    // 展开所有目录节点，便于勾选。
    menus.forEach((m) => {
      if (m.menu_type === 'directory') expanded.push(String(m.id));
    });
    return { treeData: roots, expandedKeys: expanded };
  }

  const { treeData: menuTreeData, expandedKeys: menuExpandedKeys } = buildMenuTreeData(allMenus || []);

  // When role permissions load, sync the checked set.
  const currentRolePermCodes = (rolePermissions || []).map((p) => p.code);
  const effectiveCodes = selectedCodes.length || !rolePermissions ? selectedCodes : currentRolePermCodes;

  const grouped = (allPermissions?.items || []).reduce<Record<string, Permission[]>>((acc, p) => {
    const key = p.category || '未分类';
    (acc[key] = acc[key] || []).push(p);
    return acc;
  }, {});

  const columns: ColumnsType<Role> = [
    { title: '编码', dataIndex: 'code', key: 'code' },
    { title: '名称', dataIndex: 'name', key: 'name' },
    { title: '范围', dataIndex: 'scope', key: 'scope', width: 100 },
    {
      title: '内置',
      dataIndex: 'is_builtin',
      key: 'is_builtin',
      width: 80,
      render: (v: boolean) => (v ? <Tag color="blue">内置</Tag> : '-'),
    },
    {
      title: '系统',
      dataIndex: 'is_system',
      key: 'is_system',
      width: 80,
      render: (v: boolean) => (v ? <Tag color="red">系统</Tag> : '-'),
    },
    {
      title: '启用',
      dataIndex: 'enabled',
      key: 'enabled',
      width: 80,
      render: (v: boolean) => (v ? <Tag color="success">启用</Tag> : <Tag>停用</Tag>),
    },
    { title: '创建时间', dataIndex: 'created_at', key: 'created_at', width: 180, render: (t: string) => formatTime(t) },
    {
      title: '操作',
      key: 'actions',
      width: 200,
      render: (_, record) => (
        <Space>
          <Button type="link" size="small" icon={<KeyOutlined />} onClick={() => openPermDrawer(record)}>
            管理权限
          </Button>
          {record.scope === 'platform' && (
            <Button type="link" size="small" icon={<MenuOutlined />} onClick={() => openMenuDrawer(record)}>
              绑定菜单
            </Button>
          )}
          {!record.is_builtin && (
            <Button
              type="link"
              size="small"
              danger
              onClick={() =>
                confirmDanger({
                  title: '删除角色',
                  content: `确定删除角色「${record.name}」吗？`,
                  onOk: () => deleteMutation.mutateAsync(record.id),
                })
              }
            >
              删除
            </Button>
          )}
        </Space>
      ),
    },
  ];

  return (
    <PageContainer
      title="角色管理"
      subtitle="管理平台、工作空间及应用角色"
      extra={
        <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)}>
          新建角色
        </Button>
      }
    >
      <Table
        rowKey="id"
        loading={isLoading}
        columns={columns}
        dataSource={roles || []}
        pagination={false}
        locale={{ emptyText: <EmptyState title="暂无角色" description="点击右上角新建" /> }}
      />

      <Modal
        title="新建角色"
        open={createOpen}
        onCancel={() => setCreateOpen(false)}
        onOk={() => createForm.submit()}
        confirmLoading={createMutation.isPending}
        destroyOnHidden
      >
        <Form
          layout="vertical"
          form={createForm}
          onFinish={(v) => createMutation.mutate(v)}
        >
          <Form.Item name="code" label="编码" rules={[{ required: true, message: '请输入编码' }]}>
            <Input placeholder="如：workspace_admin" />
          </Form.Item>
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入名称' }]}>
            <Input placeholder="如：空间管理员" />
          </Form.Item>
          <Form.Item name="scope" label="范围" rules={[{ required: true, message: '请选择范围' }]}>
            <Select options={SCOPE_OPTIONS} placeholder="选择范围" />
          </Form.Item>
          <Form.Item name="description" label="描述">
            <Input.TextArea rows={2} />
          </Form.Item>
        </Form>
      </Modal>

      <Drawer
        title={permDrawer.role ? `管理权限 - ${permDrawer.role.name}` : '管理权限'}
        open={permDrawer.open}
        width={560}
        onClose={() => setPermDrawer({ open: false })}
        destroyOnHidden
        extra={
          <Button
            type="primary"
            loading={grantMutation.isPending}
            onClick={() =>
              permDrawer.role &&
              grantMutation.mutate({ roleId: permDrawer.role.id, codes: effectiveCodes })
            }
          >
            保存
          </Button>
        }
      >
        {Object.entries(grouped).map(([category, perms]) => (
          <div key={category} style={{ marginBottom: 16 }}>
            <div style={{ fontWeight: 600, marginBottom: 8 }}>{category}</div>
            <Select
              mode="multiple"
              style={{ width: '100%' }}
              value={effectiveCodes}
              onChange={setSelectedCodes}
              options={perms.map((p) => ({ label: `${p.name} (${p.code})`, value: p.code }))}
            />
          </div>
        ))}
        {Object.keys(grouped).length === 0 && <EmptyState description="暂无权限" />}
      </Drawer>

      <Drawer
        title={menuDrawer.role ? `绑定菜单 - ${menuDrawer.role.name}` : '绑定菜单'}
        open={menuDrawer.open}
        width={560}
        onClose={() => setMenuDrawer({ open: false })}
        destroyOnHidden
        extra={
          <Button
            type="primary"
            loading={bindMenusMutation.isPending}
            onClick={() =>
              menuDrawer.role &&
              bindMenusMutation.mutate({ roleId: menuDrawer.role.id, menuIds: checkedMenuIds })
            }
          >
            保存（全量替换）
          </Button>
        }
      >
        <div style={{ marginBottom: 12, color: '#8c8c8c', fontSize: 12 }}>
          勾选的菜单将直接绑定到该角色，登录后 /me/menus 会按角色返回这些菜单。
          与「管理权限」的 permission_code 过滤为 OR 关系。保存为全量替换：未勾选的菜单将被解绑。
        </div>
        {menuTreeData.length > 0 ? (
          <Tree
            checkable
            defaultExpandedKeys={menuExpandedKeys}
            checkedKeys={checkedMenuIds.map(String)}
            onCheck={(checked) => {
              const keys = (Array.isArray(checked) ? checked : (checked as any).checked) as string[];
              setCheckedMenuIds(keys.map((k) => Number(k)));
            }}
            treeData={menuTreeData}
          />
        ) : (
          <EmptyState description="暂无菜单" />
        )}
      </Drawer>
    </PageContainer>
  );
}
