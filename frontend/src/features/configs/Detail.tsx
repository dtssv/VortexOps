import { useMemo, useState } from 'react';
import {
  Button,
  Card,
  Col,
  Descriptions,
  Drawer,
  Empty,
  Form,
  Input,
  List,
  Modal,
  Row,
  Select,
  Space,
  Table,
  Tag,
  Typography,
  App,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import {
  ArrowLeftOutlined,
  DiffOutlined,
  PlusOutlined,
  DeleteOutlined,
  FileTextOutlined,
} from '@ant-design/icons';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate, useParams } from 'react-router-dom';
import { PageContainer } from '@/components/PageContainer';
import { DiffViewer } from '@/components/DiffViewer';
import { JsonEditor } from '@/components/JsonEditor';
import { configApi } from '@/api/configs';
import type { ConfigVersion } from '@/types';
import { formatTime } from '@/utils/format';

interface FileEntry {
  path: string;
  content: string;
  mode?: string;
  is_secret?: boolean;
}

export default function ConfigDetailPage() {
  const { id } = useParams<{ id: string }>();
  const configId = Number(id);
  const navigate = useNavigate();
  const { message } = App.useApp();
  const queryClient = useQueryClient();

  const [selectedVersion, setSelectedVersion] = useState<number | undefined>();
  const [diffOpen, setDiffOpen] = useState(false);
  const [fromVersion, setFromVersion] = useState<number | undefined>();
  const [toVersion, setToVersion] = useState<number | undefined>();
  const [createOpen, setCreateOpen] = useState(false);
  const [form] = Form.useForm();
  const [files, setFiles] = useState<FileEntry[]>([{ path: '', content: '' }]);

  const { data: config, isLoading } = useQuery({
    queryKey: ['config', configId],
    queryFn: () => configApi.get(configId),
    enabled: !!configId,
  });

  const { data: versions } = useQuery({
    queryKey: ['config', configId, 'versions'],
    queryFn: () => configApi.listVersions(configId),
    enabled: !!configId,
  });

  const sortedVersions = useMemo(() => {
    return [...(versions ?? [])].sort((a, b) => b.version - a.version);
  }, [versions]);

  const currentVersionNumber = selectedVersion ?? config?.current_version ?? sortedVersions[0]?.version;
  const currentVersion = sortedVersions.find((v) => v.version === currentVersionNumber);

  const { data: diffResult, isLoading: diffLoading } = useQuery({
    queryKey: ['config', configId, 'diff', fromVersion, toVersion],
    queryFn: () => configApi.diff({ config_id: configId, from_version: fromVersion!, to_version: toVersion! }),
    enabled: !!fromVersion && !!toVersion && diffOpen,
  });

  const createVersionMutation = useMutation({
    mutationFn: (payload: Partial<ConfigVersion>) => configApi.createVersion(configId, payload),
    onSuccess: (data) => {
      message.success(`已创建版本 v${data.version}`);
      setCreateOpen(false);
      form.resetFields();
      setFiles([{ path: '', content: '' }]);
      void queryClient.invalidateQueries({ queryKey: ['config', configId, 'versions'] });
      setSelectedVersion(data.version);
    },
    onError: (e: any) => message.error(e?.message || '创建版本失败'),
  });

  const versionOptions = sortedVersions.map((v) => ({ label: `v${v.version}`, value: v.version }));

  const fileColumns: ColumnsType<FileEntry> = [
    { title: '路径', dataIndex: 'path', render: (v) => <Typography.Text code>{v}</Typography.Text> },
    {
      title: '内容',
      dataIndex: 'content',
      ellipsis: true,
      render: (v, r) =>
        r.is_secret ? (
          <Tag color="red">敏感信息</Tag>
        ) : (
          <Typography.Text type="secondary">{(v ?? '').slice(0, 80)}</Typography.Text>
        ),
    },
    { title: '权限', dataIndex: 'mode', width: 90, render: (v) => v ?? '-' },
  ];

  const envColumns = [
    {
      title: '名称',
      dataIndex: 'name',
      render: (v: string) => <Typography.Text code>{v}</Typography.Text>,
    },
    {
      title: '值',
      dataIndex: 'value',
      render: (v: string, r: any) =>
        r.is_secret ? <Tag color="red">敏感</Tag> : <Typography.Text>{v}</Typography.Text>,
    },
  ];

  const onAddFile = () => setFiles((f) => [...f, { path: '', content: '' }]);
  const onRemoveFile = (idx: number) => setFiles((f) => f.filter((_, i) => i !== idx));
  const onFileChange = (idx: number, key: keyof FileEntry, val: any) =>
    setFiles((f) => f.map((fi, i) => (i === idx ? { ...fi, [key]: val } : fi)));

  const onSubmitVersion = async () => {
    const vals = await form.validateFields();
    const validFiles = files.filter((f) => f.path.trim());
    const payload: Partial<ConfigVersion> = {
      change_summary: vals.change_summary,
      files: validFiles,
      command: vals.command ? (vals.command as string).split(/\s+/).filter(Boolean) : undefined,
      args: vals.args ? (vals.args as string).split(/\s+/).filter(Boolean) : undefined,
      env: vals.env
        ? (vals.env as string)
            .split('\n')
            .map((l) => l.trim())
            .filter(Boolean)
            .map((l) => {
              const [name, ...rest] = l.split('=');
              return { name, value: rest.join('=') };
            })
        : undefined,
    };
    createVersionMutation.mutate(payload);
  };

  const diffOriginal = useMemo(() => {
    if (!diffResult) return '';
    return JSON.stringify(diffResult.original ?? diffResult.from ?? '', null, 2);
  }, [diffResult]);
  const diffModified = useMemo(() => {
    if (!diffResult) return '';
    return JSON.stringify(diffResult.modified ?? diffResult.to ?? '', null, 2);
  }, [diffResult]);

  return (
    <PageContainer
      title={config?.name ?? '配置详情'}
      subtitle={config?.description}
      breadcrumb={[
        { title: '配置管理', path: '/configs' },
        { title: config?.name ?? '...' },
      ]}
      extra={
        <Space>
          <Button icon={<ArrowLeftOutlined />} onClick={() => navigate('/configs')}>
            返回
          </Button>
          <Button icon={<DiffOutlined />} onClick={() => setDiffOpen(true)}>
            版本对比
          </Button>
          <Button
            type="primary"
            icon={<PlusOutlined />}
            disabled={config?.archived}
            onClick={() => {
              form.resetFields();
              setFiles([{ path: '', content: '' }]);
              setCreateOpen(true);
            }}
          >
            新建版本
          </Button>
        </Space>
      }
    >
      <Row gutter={16}>
        <Col xs={24} md={8} lg={6}>
          <Card
            title="版本列表"
            loading={isLoading}
            bodyStyle={{ padding: 0 }}
          >
            {sortedVersions.length === 0 ? (
              <Empty description="暂无版本" style={{ padding: 24 }} />
            ) : (
              <List
                dataSource={sortedVersions}
                renderItem={(v) => {
                  const isCurrent = v.version === config?.current_version;
                  const isSelected = v.version === currentVersionNumber;
                  return (
                    <List.Item
                      onClick={() => setSelectedVersion(v.version)}
                      style={{
                        cursor: 'pointer',
                        padding: '10px 16px',
                        background: isSelected ? '#e6f4ff' : undefined,
                        borderLeft: isCurrent ? '3px solid #1677ff' : '3px solid transparent',
                      }}
                    >
                      <Space direction="vertical" size={2} style={{ width: '100%' }}>
                        <Space>
                          <Typography.Text strong>v{v.version}</Typography.Text>
                          {isCurrent && <Tag color="blue">当前</Tag>}
                        </Space>
                        {v.change_summary && (
                          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                            {v.change_summary}
                          </Typography.Text>
                        )}
                        <Typography.Text type="secondary" style={{ fontSize: 11 }}>
                          {v.created_by_name ?? `用户 ${v.created_by}`} · {formatTime(v.created_at)}
                        </Typography.Text>
                      </Space>
                    </List.Item>
                  );
                }}
              />
            )}
          </Card>
        </Col>

        <Col xs={24} md={16} lg={18}>
          <Card
            title={currentVersion ? `版本 v${currentVersion.version}` : '版本详情'}
            extra={currentVersion ? <Tag color="blue">v{currentVersion.version}</Tag> : undefined}
          >
            {!currentVersion ? (
              <Empty description="请选择左侧版本" />
            ) : (
              <Space direction="vertical" size={16} style={{ width: '100%' }}>
                <Descriptions size="small" column={2}>
                  <Descriptions.Item label="变更说明" span={2}>
                    {currentVersion.change_summary || '-'}
                  </Descriptions.Item>
                  <Descriptions.Item label="创建者">
                    {currentVersion.created_by_name ?? `用户 ${currentVersion.created_by}`}
                  </Descriptions.Item>
                  <Descriptions.Item label="创建时间">{formatTime(currentVersion.created_at)}</Descriptions.Item>
                  <Descriptions.Item label="启动命令" span={2}>
                    {currentVersion.command?.length ? (
                      <Typography.Text code>{currentVersion.command.join(' ')}</Typography.Text>
                    ) : (
                      '-'
                    )}
                  </Descriptions.Item>
                  <Descriptions.Item label="参数" span={2}>
                    {currentVersion.args?.length ? (
                      <Typography.Text code>{currentVersion.args.join(' ')}</Typography.Text>
                    ) : (
                      '-'
                    )}
                  </Descriptions.Item>
                </Descriptions>

                <div>
                  <Typography.Title level={5}>
                    <FileTextOutlined /> 配置文件
                  </Typography.Title>
                  {currentVersion.files?.length ? (
                    <Table<FileEntry>
                      rowKey="path"
                      size="small"
                      columns={fileColumns}
                      dataSource={currentVersion.files}
                      pagination={false}
                    />
                  ) : (
                    <Empty description="无文件" />
                  )}
                </div>

                {currentVersion.env?.length ? (
                  <div>
                    <Typography.Title level={5}>环境变量</Typography.Title>
                    <Table
                      rowKey="name"
                      size="small"
                      columns={envColumns}
                      dataSource={currentVersion.env}
                      pagination={false}
                    />
                  </div>
                ) : null}
              </Space>
            )}
          </Card>
        </Col>
      </Row>

      <Modal
        title="版本对比"
        open={diffOpen}
        onCancel={() => {
          setDiffOpen(false);
          setFromVersion(undefined);
          setToVersion(undefined);
        }}
        footer={null}
        width={900}
        destroyOnClose
      >
        <Space style={{ marginBottom: 12 }} wrap>
          <Select
            placeholder="起始版本"
            style={{ width: 200 }}
            value={fromVersion}
            onChange={setFromVersion}
            options={versionOptions}
          />
          <Select
            placeholder="目标版本"
            style={{ width: 200 }}
            value={toVersion}
            onChange={setToVersion}
            options={versionOptions}
          />
        </Space>
        {fromVersion && toVersion ? (
          diffLoading ? (
            <Empty description="加载中..." />
          ) : diffResult ? (
            <DiffViewer original={diffOriginal} modified={diffModified} height={500} />
          ) : (
            <Empty description="无差异" />
          )
        ) : (
          <Empty description="请选择两个版本" />
        )}
      </Modal>

      <Drawer
        title="新建版本"
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        width={720}
        destroyOnClose
        extra={
          <Space>
            <Button onClick={() => setCreateOpen(false)}>取消</Button>
            <Button type="primary" loading={createVersionMutation.isPending} onClick={onSubmitVersion}>
              创建
            </Button>
          </Space>
        }
      >
        <Form layout="vertical" form={form}>
          <Form.Item
            name="change_summary"
            label="变更说明"
            rules={[{ required: true, message: '请输入变更说明' }]}
          >
            <Input placeholder="例如：新增 REDIS_URL 环境变量" />
          </Form.Item>
          <Form.Item name="command" label="启动命令（空格分隔）">
            <Input placeholder="例如：/app/server --port 8080" />
          </Form.Item>
          <Form.Item name="args" label="参数（空格分隔）">
            <Input placeholder="例如：--debug --log-level info" />
          </Form.Item>
          <Form.Item name="env" label="环境变量（每行一个，NAME=value 格式）">
            <Input.TextArea
              rows={3}
              placeholder={'LOG_LEVEL=info\nMAX_CONN=100'}
              style={{ fontFamily: 'Menlo, Consolas, monospace' }}
            />
          </Form.Item>
        </Form>

        <div style={{ marginTop: 8 }}>
          <Space style={{ marginBottom: 8, width: '100%', justifyContent: 'space-between' }}>
            <Typography.Text strong>配置文件</Typography.Text>
            <Button size="small" icon={<PlusOutlined />} onClick={onAddFile}>
              添加文件
            </Button>
          </Space>
          {files.map((f, idx) => (
            <Card
              key={idx}
              size="small"
              style={{ marginBottom: 8 }}
              extra={
                files.length > 1 ? (
                  <Button
                    size="small"
                    type="text"
                    danger
                    icon={<DeleteOutlined />}
                    onClick={() => onRemoveFile(idx)}
                  />
                ) : null
              }
            >
              <Space direction="vertical" size={8} style={{ width: '100%' }}>
                <Input
                  placeholder="文件路径，如 config/app.yaml"
                  value={f.path}
                  onChange={(e) => onFileChange(idx, 'path', e.target.value)}
                />
                <JsonEditor
                  value={f.content}
                  language="yaml"
                  height={180}
                  onChange={(v) => onFileChange(idx, 'content', v)}
                />
              </Space>
            </Card>
          ))}
        </div>
      </Drawer>
    </PageContainer>
  );
}
