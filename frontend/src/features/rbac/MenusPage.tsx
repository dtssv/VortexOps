import { useState } from 'react';
import { Button, Form, Input, InputNumber, Modal, Select, Switch, Table, Tag, App, Space } from 'antd';
import { PlusOutlined } from '@ant-design/icons';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { ColumnsType } from 'antd/es/table';
import { PageContainer } from '@/components/PageContainer';
import { EmptyState } from '@/components/EmptyState';
import { rbacApi } from '@/api/rbac';
import type { Menu } from '@/types';
import { confirmDanger } from '@/utils/action';

const MENU_TYPE_OPTIONS = [
  { label: '目录', value: 'directory' },
  { label: '菜单', value: 'menu' },
  { label: '按钮', value: 'button' },
  { label: '链接', value: 'link' },
];

const SCOPE_OPTIONS = [
  { label: '平台', value: 'platform' },
  { label: '工作空间', value: 'workspace' },
  { label: '应用', value: 'application' },
];

const MENU_TYPE_COLOR: Record<string, string> = {
  directory: 'blue',
  menu: 'green',
  button: 'orange',
  link: 'purple',
};

function flattenMenus(menus: Menu[]): { id: number; label: string }[] {
  const out: { id: number; label: string }[] = [];
  const walk = (nodes: Menu[], depth = 0) => {
    for (const n of nodes || []) {
      out.push({ id: n.id, label: `${'— '.repeat(depth)}${n.name}` });
      if (n.children?.length) walk(n.children, depth + 1);
    }
  };
  walk(menus);
  return out;
}

export default function MenusPage() {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const [createOpen, setCreateOpen] = useState(false);
  const [createForm] = Form.useForm();

  const { data: menus, isLoading } = useQuery({
    queryKey: ['menus'],
    queryFn: () => rbacApi.listMenus(),
  });

  const createMutation = useMutation({
    mutationFn: (body: Partial<Menu>) => rbacApi.createMenu(body),
    onSuccess: () => {
      message.success('菜单已创建');
      setCreateOpen(false);
      createForm.resetFields();
      queryClient.invalidateQueries({ queryKey: ['menus'] });
    },
    onError: (e: any) => message.error(e?.message || '创建失败'),
  });

  const deleteMutation = useMutation({
    mutationFn: (id: number) => rbacApi.deleteMenu(id),
    onSuccess: () => {
      message.success('菜单已删除');
      queryClient.invalidateQueries({ queryKey: ['menus'] });
    },
    onError: (e: any) => message.error(e?.message || '删除失败'),
  });

  const columns: ColumnsType<Menu> = [
    { title: '名称', dataIndex: 'name', key: 'name' },
    { title: '编码', dataIndex: 'code', key: 'code', width: 200 },
    { title: '路径', dataIndex: 'path', key: 'path', width: 200, render: (v?: string) => v || '-' },
    { title: '图标', dataIndex: 'icon', key: 'icon', width: 100, render: (v?: string) => v || '-' },
    {
      title: '类型',
      dataIndex: 'menu_type',
      key: 'menu_type',
      width: 90,
      render: (v: string) => <Tag color={MENU_TYPE_COLOR[v] || 'default'}>{v}</Tag>,
    },
    { title: '范围', dataIndex: 'scope', key: 'scope', width: 100 },
    { title: '权限编码', dataIndex: 'permission_code', key: 'permission_code', width: 180, render: (v?: string) => v || '-' },
    {
      title: '可见',
      dataIndex: 'visible',
      key: 'visible',
      width: 80,
      render: (v: boolean) => (v ? <Tag color="success">可见</Tag> : <Tag>隐藏</Tag>),
    },
    { title: '排序', dataIndex: 'sort_order', key: 'sort_order', width: 80 },
    {
      title: '操作',
      key: 'actions',
      width: 100,
      render: (_, record) => (
        <Button
          type="link"
          size="small"
          danger
          onClick={() =>
            confirmDanger({
              title: '删除菜单',
              content: `确定删除菜单「${record.name}」吗？子菜单也会受影响。`,
              onOk: () => deleteMutation.mutateAsync(record.id),
            })
          }
        >
          删除
        </Button>
      ),
    },
  ];

  return (
    <PageContainer
      title="菜单管理"
      subtitle="管理系统导航菜单树"
      extra={
        <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)}>
          新建菜单
        </Button>
      }
    >
      <Table
        rowKey="id"
        loading={isLoading}
        columns={columns}
        dataSource={menus || []}
        pagination={false}
        expandable={{ childrenColumnName: 'children' }}
        locale={{ emptyText: <EmptyState title="暂无菜单" /> }}
      />

      <Modal
        title="新建菜单"
        open={createOpen}
        onCancel={() => setCreateOpen(false)}
        onOk={() => createForm.submit()}
        confirmLoading={createMutation.isPending}
        destroyOnHidden
        width={600}
      >
        <Form layout="vertical" form={createForm} onFinish={(v) => createMutation.mutate(v)} initialValues={{ visible: true, sort_order: 0, menu_type: 'menu', scope: 'platform' }}>
          <Space style={{ width: '100%' }} size="middle">
            <Form.Item name="parent_id" label="父菜单" style={{ flex: 1 }}>
              <Select
                allowClear
                placeholder="顶级菜单"
                options={flattenMenus(menus || []).map((m) => ({ label: m.label, value: m.id }))}
              />
            </Form.Item>
            <Form.Item name="menu_type" label="类型" rules={[{ required: true }]} style={{ width: 140 }}>
              <Select options={MENU_TYPE_OPTIONS} />
            </Form.Item>
            <Form.Item name="scope" label="范围" rules={[{ required: true }]} style={{ width: 140 }}>
              <Select options={SCOPE_OPTIONS} />
            </Form.Item>
          </Space>
          <Space style={{ width: '100%' }} size="middle">
            <Form.Item name="code" label="编码" rules={[{ required: true, message: '请输入编码' }]} style={{ flex: 1 }}>
              <Input placeholder="如：menu:admin:role" />
            </Form.Item>
            <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入名称' }]} style={{ flex: 1 }}>
              <Input placeholder="如：角色管理" />
            </Form.Item>
          </Space>
          <Space style={{ width: '100%' }} size="middle">
            <Form.Item name="path" label="路径" style={{ flex: 1 }}>
              <Input placeholder="/admin/roles" />
            </Form.Item>
            <Form.Item name="icon" label="图标" style={{ width: 160 }}>
              <Input placeholder="如：SettingOutlined" />
            </Form.Item>
            <Form.Item name="sort_order" label="排序" style={{ width: 120 }}>
              <InputNumber min={0} style={{ width: '100%' }} />
            </Form.Item>
          </Space>
          <Form.Item name="permission_code" label="权限编码">
            <Input placeholder="关联的权限 code" />
          </Form.Item>
          <Form.Item name="visible" label="可见" valuePropName="checked">
            <Switch />
          </Form.Item>
        </Form>
      </Modal>
    </PageContainer>
  );
}
