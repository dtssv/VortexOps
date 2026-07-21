import { useState } from 'react';
import { App, Button, Form, Input, InputNumber, Modal, Select, Space, Switch, Table, Tabs } from 'antd';
import { PlusOutlined } from '@ant-design/icons';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { ColumnsType } from 'antd/es/table';
import { PageContainer } from '@/components/PageContainer';
import { ResourceStatus } from '@/components/ResourceStatus';
import { EmptyState } from '@/components/EmptyState';
import { opsApi } from '@/api/ops';
import type { AlertEvent, AlertRule } from '@/types';
import { confirmDanger } from '@/utils/action';
import { formatTime, formatDuration } from '@/utils/format';

const SCOPE_OPTIONS = [
  { label: '平台', value: 'platform' },
  { label: '工作空间', value: 'workspace' },
  { label: '集群', value: 'cluster' },
];

const CONDITION_OPTIONS = [
  { label: '大于 (>)', value: '>' },
  { label: '大于等于 (>=)', value: '>=' },
  { label: '小于 (<)', value: '<' },
  { label: '小于等于 (<=)', value: '<=' },
  { label: '等于 (=)', value: '=' },
];

const SEVERITY_OPTIONS = [
  { label: '信息', value: 'info' },
  { label: '警告', value: 'warning' },
  { label: '严重', value: 'critical' },
];

export default function AlertsPage() {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const [tab, setTab] = useState('rules');

  const [rulePage, setRulePage] = useState(1);
  const [ruleSize, setRuleSize] = useState(20);
  const [createOpen, setCreateOpen] = useState(false);
  const [editTarget, setEditTarget] = useState<AlertRule | null>(null);
  const [form] = Form.useForm();

  const [evtPage, setEvtPage] = useState(1);
  const [evtSize, setEvtSize] = useState(20);

  const { data: ruleData, isLoading: ruleLoading } = useQuery({
    queryKey: ['alert-rules', { page: rulePage, size: ruleSize }],
    queryFn: () => opsApi.listAlertRules({ page: rulePage, size: ruleSize }),
    enabled: tab === 'rules',
  });

  const { data: evtData, isLoading: evtLoading } = useQuery({
    queryKey: ['alert-events', { page: evtPage, size: evtSize }],
    queryFn: () => opsApi.listAlertEvents({ page: evtPage, size: evtSize }),
    enabled: tab === 'events',
  });

  const upsertMutation = useMutation({
    mutationFn: (body: Partial<AlertRule>) =>
      editTarget ? opsApi.updateAlertRule(editTarget.id, body) : opsApi.createAlertRule(body),
    onSuccess: () => {
      message.success(editTarget ? '规则已更新' : '规则已创建');
      setCreateOpen(false);
      setEditTarget(null);
      form.resetFields();
      queryClient.invalidateQueries({ queryKey: ['alert-rules'] });
    },
    onError: (e: any) => message.error(e?.message || '保存失败'),
  });

  const deleteMutation = useMutation({
    mutationFn: (id: number) => opsApi.deleteAlertRule(id),
    onSuccess: () => {
      message.success('规则已删除');
      queryClient.invalidateQueries({ queryKey: ['alert-rules'] });
    },
    onError: (e: any) => message.error(e?.message || '删除失败'),
  });

  const openCreate = () => {
    setEditTarget(null);
    form.resetFields();
    setCreateOpen(true);
  };

  const openEdit = (rule: AlertRule) => {
    setEditTarget(rule);
    form.setFieldsValue(rule);
    setCreateOpen(true);
  };

  const ruleColumns: ColumnsType<AlertRule> = [
    { title: '名称', dataIndex: 'name', key: 'name' },
    { title: '指标', dataIndex: 'metric', key: 'metric', width: 160 },
    { title: '条件', dataIndex: 'condition', key: 'condition', width: 80 },
    { title: '阈值', dataIndex: 'threshold', key: 'threshold', width: 90, render: (v?: number) => v ?? '-' },
    {
      title: '严重度',
      dataIndex: 'severity',
      key: 'severity',
      width: 100,
      render: (v: string) => <ResourceStatus status={v} />,
    },
    { title: '窗口(分钟)', dataIndex: 'window_minutes', key: 'window_minutes', width: 110 },
    {
      title: '启用',
      dataIndex: 'enabled',
      key: 'enabled',
      width: 80,
      render: (v: boolean) => (v ? '是' : '否'),
    },
    {
      title: '操作',
      key: 'actions',
      width: 160,
      render: (_, record) => (
        <Space>
          <Button type="link" size="small" onClick={() => openEdit(record)}>
            编辑
          </Button>
          <Button
            type="link"
            size="small"
            danger
            onClick={() =>
              confirmDanger({
                title: '删除规则',
                content: `确定删除规则「${record.name}」吗？`,
                onOk: () => deleteMutation.mutateAsync(record.id),
              })
            }
          >
            删除
          </Button>
        </Space>
      ),
    },
  ];

  const evtColumns: ColumnsType<AlertEvent> = [
    { title: '触发时间', dataIndex: 'fired_at', key: 'fired_at', width: 180, render: (t: string) => formatTime(t) },
    { title: '规则 ID', dataIndex: 'rule_id', key: 'rule_id', width: 100 },
    {
      title: '严重度',
      dataIndex: 'severity',
      key: 'severity',
      width: 100,
      render: (v: string) => <ResourceStatus status={v} />,
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 100,
      render: (v: string) => <ResourceStatus status={v} />,
    },
    { title: '当前值', dataIndex: 'current_value', key: 'current_value', width: 100, render: (v?: number) => v ?? '-' },
    { title: '消息', dataIndex: 'message', key: 'message', ellipsis: true, render: (v?: string) => v || '-' },
    { title: '恢复时间', dataIndex: 'resolved_at', key: 'resolved_at', width: 180, render: (t?: string) => (t ? formatTime(t) : '-') },
  ];

  return (
    <PageContainer title="告警中心" subtitle="配置告警规则并查看告警事件">
      <Tabs
        activeKey={tab}
        onChange={setTab}
        items={[
          {
            key: 'rules',
            label: '告警规则',
            children: (
              <>
                <Space style={{ marginBottom: 16 }}>
                  <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
                    新建规则
                  </Button>
                </Space>
                <Table
                  rowKey="id"
                  loading={ruleLoading}
                  columns={ruleColumns}
                  dataSource={ruleData?.items || []}
                  locale={{ emptyText: <EmptyState title="暂无告警规则" /> }}
                  pagination={{
                    current: rulePage,
                    pageSize: ruleSize,
                    total: ruleData?.total || 0,
                    showSizeChanger: true,
                    showTotal: (t) => `共 ${t} 条`,
                    onChange: (p, s) => { setRulePage(p); setRuleSize(s); },
                  }}
                />
              </>
            ),
          },
          {
            key: 'events',
            label: '告警事件',
            children: (
              <Table
                rowKey="id"
                loading={evtLoading}
                columns={evtColumns}
                dataSource={evtData?.items || []}
                locale={{ emptyText: <EmptyState title="暂无告警事件" /> }}
                pagination={{
                  current: evtPage,
                  pageSize: evtSize,
                  total: evtData?.total || 0,
                  showSizeChanger: true,
                  showTotal: (t) => `共 ${t} 条`,
                  onChange: (p, s) => { setEvtPage(p); setEvtSize(s); },
                }}
              />
            ),
          },
        ]}
      />

      <Modal
        title={editTarget ? '编辑规则' : '新建规则'}
        open={createOpen}
        onCancel={() => { setCreateOpen(false); setEditTarget(null); }}
        onOk={() => form.submit()}
        confirmLoading={upsertMutation.isPending}
        destroyOnHidden
        width={560}
      >
        <Form layout="vertical" form={form} onFinish={(v) => upsertMutation.mutate(v)} initialValues={{ scope: 'platform', condition: '>', severity: 'warning', enabled: true, window_minutes: 5, cooldown_minutes: 10 }}>
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入名称' }]}>
            <Input placeholder="如：high-cpu" />
          </Form.Item>
          <Form.Item name="scope" label="范围" rules={[{ required: true }]}>
            <Select options={SCOPE_OPTIONS} />
          </Form.Item>
          <Form.Item name="metric" label="指标" rules={[{ required: true, message: '请输入指标' }]}>
            <Input placeholder="如：cpu_usage" />
          </Form.Item>
          <Space style={{ width: '100%' }} size="middle">
            <Form.Item name="condition" label="条件" rules={[{ required: true }]} style={{ flex: 1 }}>
              <Select options={CONDITION_OPTIONS} />
            </Form.Item>
            <Form.Item name="threshold" label="阈值" style={{ flex: 1 }}>
              <InputNumber style={{ width: '100%' }} />
            </Form.Item>
            <Form.Item name="severity" label="严重度" rules={[{ required: true }]} style={{ flex: 1 }}>
              <Select options={SEVERITY_OPTIONS} />
            </Form.Item>
          </Space>
          <Space style={{ width: '100%' }} size="middle">
            <Form.Item name="window_minutes" label="窗口(分钟)" style={{ flex: 1 }}>
              <InputNumber min={1} style={{ width: '100%' }} />
            </Form.Item>
            <Form.Item name="cooldown_minutes" label="冷却(分钟)" style={{ flex: 1 }}>
              <InputNumber min={0} style={{ width: '100%' }} />
            </Form.Item>
          </Space>
          <Form.Item name="description" label="描述">
            <Input.TextArea rows={2} />
          </Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked">
            <Switch />
          </Form.Item>
        </Form>
      </Modal>
    </PageContainer>
  );
}
