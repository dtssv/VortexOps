import { useState } from 'react';
import { Button, Form, Input, InputNumber, Modal, Select, Switch, Table, Tag, App, Space } from 'antd';
import { PlusOutlined } from '@ant-design/icons';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { ColumnsType } from 'antd/es/table';
import { PageContainer } from '@/components/PageContainer';
import { EmptyState } from '@/components/EmptyState';
import { rbacApi } from '@/api/rbac';
import type { Permission } from '@/types';
import { confirmDanger } from '@/utils/action';
import { formatTime } from '@/utils/format';

const CATEGORY_OPTIONS = [
  { label: '菜单', value: 'menu' },
  { label: '操作', value: 'action' },
  { label: '数据', value: 'data' },
];

const SCOPE_OPTIONS = [
  { label: '平台', value: 'platform' },
  { label: '工作空间', value: 'workspace' },
  { label: '应用', value: 'application' },
];

export default function PermissionsPage() {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const [page, setPage] = useState(1);
  const [size, setSize] = useState(20);
  const [createOpen, setCreateOpen] = useState(false);
  const [createForm] = Form.useForm();

  const { data, isLoading } = useQuery({
    queryKey: ['permissions', { page, size }],
    queryFn: () => rbacApi.listPermissions({ page, size }),
  });

  const createMutation = useMutation({
    mutationFn: (body: Partial<Permission>) => rbacApi.createPermission(body),
    onSuccess: () => {
      message.success('权限已创建');
      setCreateOpen(false);
      createForm.resetFields();
      queryClient.invalidateQueries({ queryKey: ['permissions'] });
    },
    onError: (e: any) => message.error(e?.message || '创建失败'),
  });

  const deleteMutation = useMutation({
    mutationFn: (id: number) => rbacApi.deletePermission(id),
    onSuccess: () => {
      message.success('权限已删除');
      queryClient.invalidateQueries({ queryKey: ['permissions'] });
    },
    onError: (e: any) => message.error(e?.message || '删除失败'),
  });

  const columns: ColumnsType<Permission> = [
    { title: '编码', dataIndex: 'code', key: 'code' },
    { title: '名称', dataIndex: 'name', key: 'name' },
    { title: '分类', dataIndex: 'category', key: 'category', width: 100 },
    { title: '范围', dataIndex: 'scope', key: 'scope', width: 100 },
    {
      title: '启用',
      dataIndex: 'enabled',
      key: 'enabled',
      width: 80,
      render: (v: boolean) => (v ? <Tag color="success">启用</Tag> : <Tag>停用</Tag>),
    },
    { title: '排序', dataIndex: 'sort_order', key: 'sort_order', width: 80 },
    { title: '创建时间', dataIndex: 'created_at', key: 'created_at', width: 180, render: (t: string) => formatTime(t) },
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
              title: '删除权限',
              content: `确定删除权限「${record.name}」吗？`,
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
      title="权限管理"
      subtitle="管理平台权限定义"
      extra={
        <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)}>
          新建权限
        </Button>
      }
    >
      <Table
        rowKey="id"
        loading={isLoading}
        columns={columns}
        dataSource={data?.items || []}
        locale={{ emptyText: <EmptyState title="暂无权限" /> }}
        pagination={{
          current: page,
          pageSize: size,
          total: data?.total || 0,
          showSizeChanger: true,
          showTotal: (t) => `共 ${t} 条`,
          onChange: (p, s) => {
            setPage(p);
            setSize(s);
          },
        }}
      />

      <Modal
        title="新建权限"
        open={createOpen}
        onCancel={() => setCreateOpen(false)}
        onOk={() => createForm.submit()}
        confirmLoading={createMutation.isPending}
        destroyOnHidden
      >
        <Form layout="vertical" form={createForm} onFinish={(v) => createMutation.mutate(v)}>
          <Form.Item name="code" label="编码" rules={[{ required: true, message: '请输入编码' }]}>
            <Input placeholder="如：cluster:create" />
          </Form.Item>
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入名称' }]}>
            <Input placeholder="如：创建集群" />
          </Form.Item>
          <Space style={{ width: '100%' }} size="middle">
            <Form.Item name="category" label="分类" rules={[{ required: true, message: '请选择分类' }]} style={{ flex: 1 }}>
              <Select options={CATEGORY_OPTIONS} placeholder="选择分类" />
            </Form.Item>
            <Form.Item name="scope" label="范围" rules={[{ required: true, message: '请选择范围' }]} style={{ flex: 1 }}>
              <Select options={SCOPE_OPTIONS} placeholder="选择范围" />
            </Form.Item>
            <Form.Item name="sort_order" label="排序" initialValue={0} style={{ width: 120 }}>
              <InputNumber min={0} style={{ width: '100%' }} />
            </Form.Item>
          </Space>
          <Form.Item name="description" label="描述">
            <Input.TextArea rows={2} />
          </Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked" initialValue={true}>
            <Switch />
          </Form.Item>
        </Form>
      </Modal>
    </PageContainer>
  );
}
