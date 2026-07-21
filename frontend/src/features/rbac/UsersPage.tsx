import { useState } from 'react';
import { Button, Drawer, Form, Input, Modal, Select, Table, Tag, App, Space, Input as AntInput, Switch } from 'antd';
import { PlusOutlined, ReloadOutlined, LockOutlined, UnlockOutlined, StopOutlined, DeleteOutlined, EditOutlined, KeyOutlined, TeamOutlined } from '@ant-design/icons';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { ColumnsType } from 'antd/es/table';
import { PageContainer } from '@/components/PageContainer';
import { EmptyState } from '@/components/EmptyState';
import { authApi } from '@/api/auth';
import { rbacApi } from '@/api/rbac';
import type { User, Role } from '@/types';
import { confirmDanger } from '@/utils/action';
import { formatTime } from '@/utils/format';

const STATUS_TAG: Record<string, { color: string; label: string }> = {
  active: { color: 'green', label: '正常' },
  disabled: { color: 'default', label: '已禁用' },
  locked: { color: 'red', label: '已锁定' },
};

const STATUS_OPTIONS = [
  { label: '正常', value: 'active' },
  { label: '已禁用', value: 'disabled' },
  { label: '已锁定', value: 'locked' },
];

export default function UsersPage() {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const [createOpen, setCreateOpen] = useState(false);
  const [createForm] = Form.useForm();
  const [editTarget, setEditTarget] = useState<User | null>(null);
  const [editForm] = Form.useForm();
  const [resetTarget, setResetTarget] = useState<User | null>(null);
  const [resetForm] = Form.useForm();
  const [rolesDrawer, setRolesDrawer] = useState<{ open: boolean; user?: User }>({ open: false });
  const [search, setSearch] = useState('');
  const [statusFilter, setStatusFilter] = useState<string>();
  const [page, setPage] = useState(1);
  const [size, setSize] = useState(20);

  const { data, isLoading, refetch } = useQuery({
    queryKey: ['users', { search, status: statusFilter, page, size }],
    queryFn: () => authApi.listUsers({ search, status: statusFilter, page, size }),
  });
  const users = data?.items ?? [];

  // 平台角色列表（用于授权抽屉的多选）。
  const { data: rolesPage } = useQuery({
    queryKey: ['roles', 'platform', { page: 1, size: 200 }],
    queryFn: () => rbacApi.listRoles({ scope: 'platform', page: 1, size: 200 }),
    enabled: rolesDrawer.open,
  });
  const platformRoles: Role[] = rolesPage?.items ?? [];

  // 当前授权用户的已绑定平台角色。
  const { data: userBoundRoles } = useQuery({
    queryKey: ['user-platform-roles', rolesDrawer.user?.id],
    queryFn: () => rbacApi.listPlatformRolesByUser(rolesDrawer.user!.id),
    enabled: rolesDrawer.open && !!rolesDrawer.user?.id,
  });
  const [selectedRoleIds, setSelectedRoleIds] = useState<number[]>([]);

  const createMutation = useMutation({
    mutationFn: (body: Parameters<typeof authApi.createUser>[0]) => authApi.createUser(body),
    onSuccess: () => {
      message.success('用户已创建');
      setCreateOpen(false);
      createForm.resetFields();
      queryClient.invalidateQueries({ queryKey: ['users'] });
    },
    onError: (e: any) => message.error(e?.message || '创建失败'),
  });

  const updateMutation = useMutation({
    mutationFn: ({ id, body }: { id: number; body: Parameters<typeof authApi.updateUser>[1] }) =>
      authApi.updateUser(id, body),
    onSuccess: () => {
      message.success('用户已更新');
      setEditTarget(null);
      queryClient.invalidateQueries({ queryKey: ['users'] });
    },
    onError: (e: any) => message.error(e?.message || '更新失败'),
  });

  const resetPwdMutation = useMutation({
    mutationFn: ({ id, body }: { id: number; body: Parameters<typeof authApi.resetUserPassword>[1] }) =>
      authApi.resetUserPassword(id, body),
    onSuccess: () => {
      message.success('密码已重置，该用户的全部会话已失效');
      setResetTarget(null);
      resetForm.resetFields();
    },
    onError: (e: any) => message.error(e?.message || '重置失败'),
  });

  const statusMutation = useMutation({
    mutationFn: ({ id, status }: { id: number; status: 'active' | 'disabled' | 'locked' }) =>
      authApi.updateUserStatus(id, status),
    onSuccess: () => {
      message.success('状态已更新');
      queryClient.invalidateQueries({ queryKey: ['users'] });
    },
    onError: (e: any) => message.error(e?.message || '更新失败'),
  });

  const deleteMutation = useMutation({
    mutationFn: (id: number) => authApi.deleteUser(id),
    onSuccess: () => {
      message.success('用户已删除');
      queryClient.invalidateQueries({ queryKey: ['users'] });
    },
    onError: (e: any) => message.error(e?.message || '删除失败'),
  });

  const bindRoleMutation = useMutation({
    mutationFn: ({ userId, roleId }: { userId: number; roleId: number }) =>
      rbacApi.bindPlatformRole(userId, { role_id: roleId }),
    onSuccess: () => {
      message.success('角色已授权');
      queryClient.invalidateQueries({ queryKey: ['user-platform-roles', rolesDrawer.user?.id] });
    },
    onError: (e: any) => message.error(e?.message || '授权失败'),
  });

  const openEdit = (u: User) => {
    setEditTarget(u);
    editForm.setFieldsValue({
      email: u.email,
      phone: u.phone,
      display_name: u.display_name,
      status: u.status,
      version: u.version,
    });
  };

  const openRolesDrawer = (u: User) => {
    setRolesDrawer({ open: true, user: u });
    setSelectedRoleIds([]);
  };

  const columns: ColumnsType<User> = [
    { title: 'ID', dataIndex: 'id', width: 70 },
    { title: '用户名', dataIndex: 'username', width: 140 },
    { title: '显示名', dataIndex: 'display_name', width: 140 },
    { title: '邮箱', dataIndex: 'email' },
    { title: '手机', dataIndex: 'phone', width: 140 },
    {
      title: '认证来源',
      dataIndex: 'auth_source',
      width: 100,
      render: (v: string) => <Tag>{v}</Tag>,
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 100,
      render: (v: string) => {
        const cfg = STATUS_TAG[v] || { color: 'default', label: v };
        return <Tag color={cfg.color}>{cfg.label}</Tag>;
      },
    },
    {
      title: '创建时间',
      dataIndex: 'created_at',
      width: 180,
      render: (v: string) => formatTime(v),
    },
    {
      title: '操作',
      key: 'actions',
      width: 360,
      render: (_, u) => (
        <Space size="small" wrap>
          <Button size="small" type="link" icon={<EditOutlined />} onClick={() => openEdit(u)}>
            编辑
          </Button>
          <Button size="small" type="link" icon={<TeamOutlined />} onClick={() => openRolesDrawer(u)}>
            授权
          </Button>
          <Button size="small" type="link" icon={<KeyOutlined />} onClick={() => { setResetTarget(u); resetForm.resetFields(); }}>
            重置密码
          </Button>
          {u.status !== 'active' && (
            <Button
              size="small"
              type="link"
              icon={<UnlockOutlined />}
              loading={statusMutation.isPending}
              onClick={() => statusMutation.mutate({ id: u.id, status: 'active' })}
            >
              启用
            </Button>
          )}
          {u.status === 'active' && (
            <Button
              size="small"
              type="link"
              icon={<StopOutlined />}
              loading={statusMutation.isPending}
              onClick={() => statusMutation.mutate({ id: u.id, status: 'disabled' })}
            >
              禁用
            </Button>
          )}
          {u.status !== 'locked' && (
            <Button
              size="small"
              type="link"
              danger
              icon={<LockOutlined />}
              loading={statusMutation.isPending}
              onClick={() => statusMutation.mutate({ id: u.id, status: 'locked' })}
            >
              锁定
            </Button>
          )}
          <Button
            size="small"
            type="link"
            danger
            icon={<DeleteOutlined />}
            onClick={() =>
              confirmDanger({
                title: `删除用户 ${u.username}`,
                content: '删除后该用户将无法登录，且其历史数据中的创建者引用将保留。此操作不可撤销。',
                onOk: () => deleteMutation.mutateAsync(u.id),
              })
            }
          >
            删除
          </Button>
        </Space>
      ),
    },
  ];

  return (
    <PageContainer
      title="用户管理"
      subtitle="管理平台用户、状态、资料与访问权限"
      extra={
        <Space>
          <Button icon={<ReloadOutlined />} onClick={() => refetch()}>
            刷新
          </Button>
          <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)}>
            新建用户
          </Button>
        </Space>
      }
    >
      <Space style={{ marginBottom: 16 }} wrap>
        <AntInput.Search
          placeholder="搜索用户名 / 邮箱 / 显示名"
          allowClear
          style={{ width: 280 }}
          onSearch={(v) => {
            setSearch(v);
            setPage(1);
          }}
        />
        <Select
          placeholder="状态筛选"
          allowClear
          style={{ width: 140 }}
          onChange={(v) => {
            setStatusFilter(v);
            setPage(1);
          }}
          options={STATUS_OPTIONS}
        />
      </Space>

      <Table<User>
        rowKey="id"
        loading={isLoading}
        dataSource={users}
        columns={columns}
        pagination={{
          current: page,
          pageSize: size,
          total: data?.total ?? 0,
          showSizeChanger: true,
          showTotal: (t) => `共 ${t} 条`,
          onChange: (p, s) => {
            setPage(p);
            setSize(s);
          },
        }}
        locale={{ emptyText: <EmptyState title="暂无用户" /> }}
      />

      <Modal
        title="新建用户"
        open={createOpen}
        onCancel={() => setCreateOpen(false)}
        onOk={() => createForm.submit()}
        confirmLoading={createMutation.isPending}
        destroyOnClose
      >
        <Form
          form={createForm}
          layout="vertical"
          onFinish={(v) => createMutation.mutate(v)}
          initialValues={{ locale: 'zh-CN', timezone: 'Asia/Shanghai' }}
        >
          <Form.Item name="username" label="用户名" rules={[{ required: true, min: 3, max: 64 }]}>
            <Input placeholder="3-64 位，字母数字 _ - ." />
          </Form.Item>
          <Form.Item name="email" label="邮箱" rules={[{ required: true, type: 'email' }]}>
            <Input />
          </Form.Item>
          <Form.Item name="display_name" label="显示名">
            <Input />
          </Form.Item>
          <Form.Item name="phone" label="手机">
            <Input />
          </Form.Item>
          <Form.Item name="password" label="初始密码" rules={[{ required: true, min: 8 }]}>
            <Input.Password placeholder="至少 8 位" />
          </Form.Item>
          <Form.Item name="locale" label="语言" hidden>
            <Input />
          </Form.Item>
          <Form.Item name="timezone" label="时区" hidden>
            <Input />
          </Form.Item>
        </Form>
      </Modal>

      {/* 编辑用户 */}
      <Modal
        title={editTarget ? `编辑用户 - ${editTarget.username}` : '编辑用户'}
        open={!!editTarget}
        onCancel={() => setEditTarget(null)}
        onOk={() => editForm.submit()}
        confirmLoading={updateMutation.isPending}
        destroyOnClose
      >
        {editTarget && (
          <Form
            form={editForm}
            layout="vertical"
            onFinish={(v) =>
              updateMutation.mutate({
                id: editTarget.id,
                body: {
                  email: v.email,
                  phone: v.phone,
                  display_name: v.display_name,
                  status: v.status,
                  version: v.version,
                },
              })
            }
          >
            <Form.Item name="email" label="邮箱" rules={[{ required: true, type: 'email' }]}>
              <Input />
            </Form.Item>
            <Form.Item name="display_name" label="显示名">
              <Input />
            </Form.Item>
            <Form.Item name="phone" label="手机">
              <Input />
            </Form.Item>
            <Form.Item name="status" label="状态" rules={[{ required: true }]}>
              <Select options={STATUS_OPTIONS} />
            </Form.Item>
            <Form.Item name="version" hidden>
              <Input />
            </Form.Item>
            <div style={{ color: '#8c8c8c', fontSize: 12 }}>
              用户名与认证来源不可修改；如需修改密码请使用「重置密码」。
            </div>
          </Form>
        )}
      </Modal>

      {/* 重置密码 */}
      <Modal
        title={resetTarget ? `重置密码 - ${resetTarget.username}` : '重置密码'}
        open={!!resetTarget}
        onCancel={() => setResetTarget(null)}
        onOk={() => resetForm.submit()}
        confirmLoading={resetPwdMutation.isPending}
        destroyOnClose
      >
        {resetTarget && (
          <Form
            form={resetForm}
            layout="vertical"
            onFinish={(v) =>
              resetPwdMutation.mutate({
                id: resetTarget.id,
                body: { new_password: v.new_password, must_change_password: v.must_change_password },
              })
            }
            initialValues={{ must_change_password: true }}
          >
            <Form.Item
              name="new_password"
              label="新密码"
              rules={[{ required: true, min: 8, message: '至少 8 位' }]}
            >
              <Input.Password placeholder="至少 8 位" />
            </Form.Item>
            <Form.Item name="must_change_password" label="强制下次登录修改密码" valuePropName="checked">
              <Switch />
            </Form.Item>
            <div style={{ color: '#8c8c8c', fontSize: 12 }}>
              重置后该用户的全部会话将立即失效，需用新密码重新登录。
            </div>
          </Form>
        )}
      </Modal>

      {/* 授权（分配平台角色） */}
      <Drawer
        title={rolesDrawer.user ? `授权平台角色 - ${rolesDrawer.user.username}` : '授权平台角色'}
        open={rolesDrawer.open}
        width={560}
        onClose={() => setRolesDrawer({ open: false })}
        destroyOnHidden
      >
        <div style={{ marginBottom: 12, color: '#8c8c8c', fontSize: 12 }}>
          当前已绑定角色：
          {userBoundRoles && userBoundRoles.length > 0 ? (
            userBoundRoles.map((r) => (
              <Tag key={r.id} color="blue" style={{ marginInlineStart: 4 }}>
                {r.name}
              </Tag>
            ))
          ) : (
            <span style={{ marginInlineStart: 4 }}>无</span>
          )}
        </div>
        <div style={{ fontWeight: 600, marginBottom: 8 }}>为该用户绑定平台角色</div>
        <Select
          style={{ width: '100%' }}
          placeholder="选择要绑定的平台角色"
          value={selectedRoleIds[0]}
          onChange={(v) => setSelectedRoleIds(v ? [v] : [])}
          options={platformRoles.map((r) => ({ label: `${r.name} (${r.code})`, value: r.id, disabled: (userBoundRoles || []).some((ub) => ub.id === r.id) }))}
        />
        <div style={{ marginTop: 16 }}>
          <Button
            type="primary"
            disabled={selectedRoleIds.length === 0 || !rolesDrawer.user}
            loading={bindRoleMutation.isPending}
            onClick={() => {
              if (rolesDrawer.user && selectedRoleIds[0]) {
                bindRoleMutation.mutate({ userId: rolesDrawer.user.id, roleId: selectedRoleIds[0] });
              }
            }}
          >
            绑定角色
          </Button>
        </div>
        <div style={{ marginTop: 24, color: '#8c8c8c', fontSize: 12 }}>
          角色解绑请前往「权限管理 → 角色」维护，或联系平台管理员。菜单可见性由角色绑定的菜单与权限共同决定。
        </div>
      </Drawer>
    </PageContainer>
  );
}
