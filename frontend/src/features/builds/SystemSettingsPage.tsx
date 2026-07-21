import { useEffect, useState } from 'react';
import { Button, Card, Form, Input, Modal, Select, Space, Switch, Table, Tabs, Tag, App, Alert } from 'antd';
import { PlusOutlined, ApiOutlined, BuildOutlined, ThunderboltOutlined, RobotOutlined, ContainerOutlined, ToolOutlined } from '@ant-design/icons';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { ColumnsType } from 'antd/es/table';
import { PageContainer } from '@/components/PageContainer';
import { CredentialPicker } from '@/components/CredentialPicker';
import { DockerfileEditor } from '@/components/DockerfileEditor';
import { systemApi, type SystemSetting } from '@/api/system';
import { buildApi, LANGUAGE_OPTIONS } from '@/api/builds';
import { confirmDanger } from '@/utils/action';
import { formatTime } from '@/utils/format';
import {
  RUNTIME_DEFAULT_ENTRYPOINTS,
  appEntrypointRawForEdit,
  entrypointForRuntime,
  parseEntrypointRaw,
  previewEffectiveEntrypoint,
} from '@/utils/baseImage';
import type { BaseImage, BuildTool, JenkinsInstance, Registry } from '@/types';

// 平台默认 Jenkins/Registry 设置项 key。
const KEY_DEFAULT_JENKINS = 'platform.default_jenkins_id';
const KEY_DEFAULT_REGISTRY = 'platform.default_registry_id';
const KEY_BUILD_ENGINE = 'platform.build_engine';
const KEY_TEKTON_NAMESPACE = 'tekton.namespace';
const KEY_TEKTON_KUBECONFIG = 'tekton.kubeconfig';

const STATUS_COLORS: Record<string, string> = {
  active: 'green',
  disabled: 'default',
};

const REGISTRY_TYPE_OPTIONS = [
  { label: 'Harbor', value: 'harbor' },
  { label: 'Docker Registry', value: 'docker_registry' },
  { label: '阿里云 ACR', value: 'acr' },
  { label: 'AWS ECR（敬请期待）', value: 'ecr', disabled: true },
];

export default function SystemSettingsPage() {
  return (
    <PageContainer>
      <Tabs
        defaultActiveKey="general"
        items={[
          { key: 'general', label: '通用设置', children: <GeneralSettingsTab /> },
          { key: 'engine', label: <Space><ThunderboltOutlined />构建引擎</Space>, children: <BuildEngineTab /> },
          { key: 'jenkins', label: <Space><BuildOutlined />Jenkins 集成</Space>, children: <JenkinsIntegrationTab /> },
          { key: 'registry', label: <Space><ApiOutlined />镜像仓库集成</Space>, children: <RegistryIntegrationTab /> },
          { key: 'base-images', label: <Space><ContainerOutlined />基础镜像</Space>, children: <BaseImageTab /> },
          { key: 'build-tools', label: <Space><BuildOutlined />构建工具</Space>, children: <BuildToolTab /> },
          { key: 'ai-diagnosis', label: <Space><RobotOutlined />AI 诊断</Space>, children: <AIDiagnosisTab /> },
        ]}
      />
    </PageContainer>
  );
}

// ============== Tab 1：通用设置 ==============

function GeneralSettingsTab() {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const [editTarget, setEditTarget] = useState<SystemSetting | null>(null);
  const [form] = Form.useForm();
  const [modalOpen, setModalOpen] = useState(false);

  const { data, isLoading } = useQuery({
    queryKey: ['system-settings', 'all'],
    queryFn: () => systemApi.listAll(),
  });
  const items = Array.isArray(data) ? data : [];

  // 默认 Jenkins/Registry 候选列表（用于通用设置表格展示与编辑下拉）。
  const { data: jenkinsList } = useQuery({
    queryKey: ['jenkins-instances'],
    queryFn: () => buildApi.listJenkins({ page: 1, size: 200 }),
  });
  const { data: registryList } = useQuery({
    queryKey: ['registries'],
    queryFn: () => buildApi.listRegistries({ page: 1, size: 200 }),
  });
  const jenkinsItems = jenkinsList?.items ?? [];
  const registryItems = registryList?.items ?? [];

  const updateMutation = useMutation({
    mutationFn: ({ key, body }: { key: string; body: any }) => systemApi.update(key, body),
    onSuccess: () => {
      message.success('设置已保存');
      setModalOpen(false);
      form.resetFields();
      queryClient.invalidateQueries({ queryKey: ['system-settings'] });
    },
    onError: (e: any) => message.error(e?.message || '保存失败'),
  });

  const openEdit = (record: SystemSetting) => {
    setEditTarget(record);
    setModalOpen(true);
  };

  useEffect(() => {
    if (modalOpen && editTarget) {
      form.setFieldsValue({
        value: editTarget.value,
        description: editTarget.description,
        is_public: editTarget.is_public,
      });
    }
  }, [modalOpen, editTarget, form]);

  const isJenkinsKey = editTarget?.key === KEY_DEFAULT_JENKINS;
  const isRegistryKey = editTarget?.key === KEY_DEFAULT_REGISTRY;

  const submit = (v: any) => {
    let value = v.value;
    if (isJenkinsKey || isRegistryKey) {
      value = v.value === '' || v.value === undefined ? null : Number(v.value);
    }
    updateMutation.mutate({
      key: editTarget!.key,
      body: { value, description: v.description, is_public: v.is_public ?? false },
    });
  };

  const columns: ColumnsType<SystemSetting> = [
    { title: 'Key', dataIndex: 'key', key: 'key', render: (k) => <code>{k}</code> },
    {
      title: 'Value',
      dataIndex: 'value',
      key: 'value',
      render: (v, r) => {
        if (r.key === KEY_DEFAULT_JENKINS) {
          const j = jenkinsItems.find((x: any) => x.id === Number(v));
          return j ? `${j.name} (id=${v})` : <Tag>未配置</Tag>;
        }
        if (r.key === KEY_DEFAULT_REGISTRY) {
          const reg = registryItems.find((x: any) => x.id === Number(v));
          return reg ? `${reg.name} (id=${v})` : <Tag>未配置</Tag>;
        }
        return <code>{JSON.stringify(v)}</code>;
      },
    },
    { title: '说明', dataIndex: 'description', key: 'description' },
    {
      title: '公开',
      dataIndex: 'is_public',
      key: 'is_public',
      render: (v) => (v ? <Tag color="green">是</Tag> : <Tag>否</Tag>),
    },
    { title: '更新时间', dataIndex: 'updated_at', key: 'updated_at', render: formatTime },
    {
      title: '操作',
      key: 'actions',
      render: (_, r) => (
        <Space>
          <a onClick={() => openEdit(r)}>编辑</a>
        </Space>
      ),
    },
  ];

  return (
    <>
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 16 }}
        message="Jenkins 与镜像仓库已改为系统级配置（单一活跃实例）。"
        description="请前往「Jenkins 集成」「镜像仓库集成」标签页管理实例，本页仅维护平台通用配置与默认实例绑定。"
      />
      <Card title="通用系统设置">
        <Table
          rowKey="id"
          loading={isLoading}
          columns={columns}
          dataSource={items}
          pagination={false}
        />
      </Card>

      <Modal
        title={`编辑：${editTarget?.key ?? ''}`}
        open={modalOpen}
        onCancel={() => setModalOpen(false)}
        onOk={() => form.submit()}
        confirmLoading={updateMutation.isPending}
        destroyOnClose
      >
        <Form layout="vertical" form={form} onFinish={submit}>
          {isJenkinsKey ? (
            <Form.Item name="value" label="默认 Jenkins 实例">
              <Select
                allowClear
                placeholder="选择 Jenkins 实例"
                options={jenkinsItems.map((j: any) => ({ label: j.name, value: j.id }))}
              />
            </Form.Item>
          ) : isRegistryKey ? (
            <Form.Item name="value" label="默认镜像仓库">
              <Select
                allowClear
                placeholder="选择镜像仓库"
                options={registryItems.map((r: any) => ({ label: r.name, value: r.id }))}
              />
            </Form.Item>
          ) : (
            <Form.Item name="value" label="Value">
              <Input placeholder='JSON 值，例如 "VortexOps" 或 3600' />
            </Form.Item>
          )}
          <Form.Item name="description" label="说明">
            <Input />
          </Form.Item>
        </Form>
      </Modal>
    </>
  );
}

// ============== Tab 2：Jenkins 集成 ==============

function JenkinsIntegrationTab() {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const [modalOpen, setModalOpen] = useState(false);
  const [editTarget, setEditTarget] = useState<JenkinsInstance | null>(null);
  const [form] = Form.useForm();
  const [testing, setTesting] = useState(false);

  const { data, isLoading } = useQuery({
    queryKey: ['jenkins-instances'],
    queryFn: () => buildApi.listJenkins({ page: 1, size: 200 }),
  });
  const items = data?.items ?? [];

  const openCreate = () => {
    setEditTarget(null);
    form.resetFields();
    setModalOpen(true);
  };

  const openEdit = (record: JenkinsInstance) => {
    setEditTarget(record);
    form.setFieldsValue({
      name: record.name,
      url: record.url ?? record.endpoint,
      credential_id: record.credential_id,
      default_job_folder: record.default_job_folder,
      is_default: record.is_default,
      status: record.status ?? 'active',
    });
    setModalOpen(true);
  };

  const submitMutation = useMutation({
    mutationFn: async (v: any) => {
      const body: Partial<JenkinsInstance> = {
        name: v.name,
        url: v.url,
        credential_id: v.credential_id ? Number(v.credential_id) : undefined,
        default_job_folder: v.default_job_folder,
        is_default: v.is_default ?? false,
        status: v.status ?? 'active',
      };
      if (editTarget) {
        body.version = editTarget.version;
        return buildApi.updateJenkins(editTarget.id, body);
      }
      return buildApi.createJenkins(body);
    },
    onSuccess: () => {
      message.success(editTarget ? 'Jenkins 已更新' : 'Jenkins 已创建');
      setModalOpen(false);
      form.resetFields();
      queryClient.invalidateQueries({ queryKey: ['jenkins-instances'] });
    },
    onError: (e: any) => message.error(e?.message || '操作失败'),
  });

  const deleteMutation = useMutation({
    mutationFn: (id: number) => buildApi.deleteJenkins(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['jenkins-instances'] });
    },
    onError: (e: any) => message.error(e?.message || '删除失败'),
  });

  const handleTest = async () => {
    try {
      const v = await form.validateFields();
      setTesting(true);
      await buildApi.testJenkinsConnection({
        id: editTarget?.id,
        url: v.url,
        credential_id: v.credential_id ? Number(v.credential_id) : undefined,
      });
      message.success('Jenkins 连接成功');
    } catch (e: any) {
      message.error(e?.message || '连接测试失败');
    } finally {
      setTesting(false);
    }
  };

  const columns: ColumnsType<JenkinsInstance> = [
    { title: '名称', dataIndex: 'name', key: 'name' },
    { title: 'URL', dataIndex: 'url', key: 'url', render: (v, r) => v ?? r.endpoint },
    { title: '默认 Job 目录', dataIndex: 'default_job_folder', key: 'default_job_folder' },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      render: (s) => <Tag color={STATUS_COLORS[s] ?? 'blue'}>{s ?? 'active'}</Tag>,
    },
    {
      title: '默认',
      dataIndex: 'is_default',
      key: 'is_default',
      render: (v) => (v ? <Tag color="gold">默认</Tag> : '-'),
    },
    { title: '创建时间', dataIndex: 'created_at', key: 'created_at', render: formatTime },
    {
      title: '操作',
      key: 'actions',
      render: (_, r) => (
        <Space>
          <a onClick={() => openEdit(r)}>编辑</a>
          <a
            onClick={() =>
              confirmDanger({
                title: '删除 Jenkins 实例',
                content: `确定删除 "${r.name}" 吗？`,
                onOk: () => deleteMutation.mutate(r.id),
              })
            }
          >
            删除
          </a>
        </Space>
      ),
    },
  ];

  return (
    <>
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 16 }}
        message="系统级配置：全局唯一默认 Jenkins 实例。"
        description="应用构建自动使用默认 Jenkins（is_default=true 仅允许一条）。可通过下方表单新建或编辑实例，并在保存前测试连接。"
      />
      <Card
        title="Jenkins 实例"
        extra={
          <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
            新建
          </Button>
        }
      >
        <Table
          rowKey="id"
          loading={isLoading}
          columns={columns}
          dataSource={items}
          pagination={false}
        />
      </Card>

      <Modal
        title={editTarget ? '编辑 Jenkins 实例' : '新建 Jenkins 实例'}
        open={modalOpen}
        onCancel={() => setModalOpen(false)}
        onOk={() => form.submit()}
        confirmLoading={submitMutation.isPending}
        destroyOnClose
        footer={[
          <Button key="test" onClick={handleTest} loading={testing}>测试连接</Button>,
          <Button key="cancel" onClick={() => setModalOpen(false)}>取消</Button>,
          <Button key="ok" type="primary" loading={submitMutation.isPending} onClick={() => form.submit()}>
            保存
          </Button>,
        ]}
      >
        <Form
          layout="vertical"
          form={form}
          onFinish={(v) => submitMutation.mutate(v)}
          initialValues={{ status: 'active', is_default: false }}
        >
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入名称' }]}>
            <Input placeholder="例如 prod-jenkins" />
          </Form.Item>
          <Form.Item name="url" label="URL" rules={[{ required: true, message: '请输入 URL' }]}>
            <Input placeholder="https://jenkins.example.com" />
          </Form.Item>
          <Form.Item
            name="credential_id"
            label="凭证"
            rules={[{ required: true, message: '请选择或新建 Jenkins 凭证（含 username + api_token）' }]}
          >
            <CredentialPicker
              kind="jenkins"
              fields={[
                { key: 'username', label: '用户名', required: true, placeholder: '例如 admin' },
                { key: 'api_token', label: 'API Token', required: true, placeholder: 'Jenkins 用户配置页生成的 API Token' },
              ]}
            />
          </Form.Item>
          <Form.Item name="default_job_folder" label="默认 Job 目录">
            <Input placeholder="例如 vortexops" />
          </Form.Item>
          <Form.Item name="status" label="状态">
            <Select
              options={[
                { label: '启用', value: 'active' },
                { label: '禁用', value: 'disabled' },
              ]}
            />
          </Form.Item>
          <Form.Item name="is_default" label="设为默认" valuePropName="checked">
            <Switch />
          </Form.Item>
        </Form>
      </Modal>
    </>
  );
}

// ============== Tab 3：镜像仓库集成 ==============

function RegistryIntegrationTab() {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const [modalOpen, setModalOpen] = useState(false);
  const [editTarget, setEditTarget] = useState<Registry | null>(null);
  const [form] = Form.useForm();
  const [testing, setTesting] = useState(false);

  const { data, isLoading } = useQuery({
    queryKey: ['registries'],
    queryFn: () => buildApi.listRegistries({ page: 1, size: 200 }),
  });
  const items = data?.items ?? [];

  const openCreate = () => {
    setEditTarget(null);
    form.resetFields();
    setModalOpen(true);
  };

  const openEdit = (record: Registry) => {
    setEditTarget(record);
    form.setFieldsValue({
      name: record.name,
      type: record.type,
      url: record.url ?? record.endpoint,
      credential_id: record.credential_id,
      is_default: record.is_default,
      status: record.status ?? 'active',
    });
    setModalOpen(true);
  };

  const submitMutation = useMutation({
    mutationFn: async (v: any) => {
      const body: Partial<Registry> = {
        name: v.name,
        type: v.type,
        url: v.url,
        credential_id: v.credential_id ? Number(v.credential_id) : undefined,
        is_default: v.is_default ?? false,
        status: v.status ?? 'active',
      };
      if (editTarget) {
        body.version = editTarget.version;
        return buildApi.updateRegistry(editTarget.id, body);
      }
      return buildApi.createRegistry(body);
    },
    onSuccess: () => {
      message.success(editTarget ? '镜像仓库已更新' : '镜像仓库已创建');
      setModalOpen(false);
      form.resetFields();
      queryClient.invalidateQueries({ queryKey: ['registries'] });
    },
    onError: (e: any) => message.error(e?.message || '操作失败'),
  });

  const deleteMutation = useMutation({
    mutationFn: (id: number) => buildApi.deleteRegistry(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['registries'] });
    },
    onError: (e: any) => message.error(e?.message || '删除失败'),
  });

  const handleTest = async () => {
    try {
      const v = await form.validateFields();
      setTesting(true);
      await buildApi.testRegistryConnection({
        id: editTarget?.id,
        type: v.type,
        url: v.url,
        credential_id: v.credential_id ? Number(v.credential_id) : undefined,
      });
      message.success('镜像仓库连接成功');
    } catch (e: any) {
      message.error(e?.message || '连接测试失败');
    } finally {
      setTesting(false);
    }
  };

  const columns: ColumnsType<Registry> = [
    { title: '名称', dataIndex: 'name', key: 'name' },
    { title: '类型', dataIndex: 'type', key: 'type', render: (t) => <Tag>{t}</Tag> },
    { title: 'URL', dataIndex: 'url', key: 'url', render: (v, r) => v ?? r.endpoint },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      render: (s) => <Tag color={STATUS_COLORS[s] ?? 'blue'}>{s ?? 'active'}</Tag>,
    },
    {
      title: '默认',
      dataIndex: 'is_default',
      key: 'is_default',
      render: (v) => (v ? <Tag color="gold">默认</Tag> : '-'),
    },
    { title: '创建时间', dataIndex: 'created_at', key: 'created_at', render: formatTime },
    {
      title: '操作',
      key: 'actions',
      render: (_, r) => (
        <Space>
          <a onClick={() => openEdit(r)}>编辑</a>
          <a
            onClick={() =>
              confirmDanger({
                title: '删除镜像仓库',
                content: `确定删除 "${r.name}" 吗？`,
                onOk: () => deleteMutation.mutate(r.id),
              })
            }
          >
            删除
          </a>
        </Space>
      ),
    },
  ];

  return (
    <>
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 16 }}
        message="系统级配置：全局唯一默认镜像仓库（支持 Harbor / Docker Registry / ACR / ECR）。"
        description="应用构建推送镜像与发布拉取镜像均使用默认仓库（is_default=true 仅允许一条）。ECR 暂未实现，敬请期待。"
      />
      <Card
        title="镜像仓库实例"
        extra={
          <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
            新建
          </Button>
        }
      >
        <Table
          rowKey="id"
          loading={isLoading}
          columns={columns}
          dataSource={items}
          pagination={false}
        />
      </Card>

      <Modal
        title={editTarget ? '编辑镜像仓库' : '新建镜像仓库'}
        open={modalOpen}
        onCancel={() => setModalOpen(false)}
        confirmLoading={submitMutation.isPending}
        destroyOnClose
        footer={[
          <Button key="test" onClick={handleTest} loading={testing}>测试连接</Button>,
          <Button key="cancel" onClick={() => setModalOpen(false)}>取消</Button>,
          <Button key="ok" type="primary" loading={submitMutation.isPending} onClick={() => form.submit()}>
            保存
          </Button>,
        ]}
      >
        <Form
          layout="vertical"
          form={form}
          onFinish={(v) => submitMutation.mutate(v)}
          initialValues={{ type: 'harbor', status: 'active', is_default: false }}
        >
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入名称' }]}>
            <Input placeholder="例如 harbor-prod" />
          </Form.Item>
          <Form.Item name="type" label="类型" rules={[{ required: true }]}>
            <Select options={REGISTRY_TYPE_OPTIONS} />
          </Form.Item>
          <Form.Item name="url" label="URL" rules={[{ required: true, message: '请输入 URL' }]}>
            <Input placeholder="https://harbor.example.com" />
          </Form.Item>
          <Form.Item
            name="credential_id"
            label="凭证"
            rules={[{ required: true, message: '请选择或新建镜像仓库凭证（含 username + password）' }]}
          >
            <CredentialPicker
              kind="registry"
              fields={[
                { key: 'username', label: '用户名', required: true, placeholder: '例如 admin' },
                { key: 'password', label: '密码', required: true, placeholder: 'Harbor/Docker Registry 登录密码' },
              ]}
            />
          </Form.Item>
          <Form.Item name="status" label="状态">
            <Select
              options={[
                { label: '启用', value: 'active' },
                { label: '禁用', value: 'disabled' },
              ]}
            />
          </Form.Item>
          <Form.Item name="is_default" label="设为默认" valuePropName="checked">
            <Switch />
          </Form.Item>
        </Form>
      </Modal>
    </>
  );
}

// ============== Tab：构建引擎（Jenkins / Tekton 切换）==============

function BuildEngineTab() {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const [form] = Form.useForm();

  const { data: settings } = useQuery({
    queryKey: ['system-settings', 'all'],
    queryFn: () => systemApi.listAll(),
  });
  const items = Array.isArray(settings) ? settings : [];
  const get = (key: string) => {
    const s = items.find((x) => x.key === key);
    return s?.value;
  };

  const engine = get(KEY_BUILD_ENGINE) ?? 'jenkins';
  const tektonNamespace = get(KEY_TEKTON_NAMESPACE) ?? 'vo-builds';
  const tektonKubeconfig = get(KEY_TEKTON_KUBECONFIG) ?? '';

  useEffect(() => {
    form.setFieldsValue({
      engine,
      tekton_namespace: tektonNamespace,
      tekton_kubeconfig: tektonKubeconfig,
    });
  }, [engine, tektonNamespace, tektonKubeconfig, form]);

  const saveMutation = useMutation({
    mutationFn: async (v: any) => {
      await systemApi.update(KEY_BUILD_ENGINE, { value: v.engine, description: '构建引擎：jenkins 或 tekton' });
      await systemApi.update(KEY_TEKTON_NAMESPACE, { value: v.tekton_namespace, description: 'Tekton 运行命名空间' });
      await systemApi.update(KEY_TEKTON_KUBECONFIG, {
        value: v.tekton_kubeconfig,
        description: 'Tekton 构建集群 kubeconfig（base64 或 PEM）',
      });
    },
    onSuccess: () => {
      message.success('构建引擎配置已保存');
      queryClient.invalidateQueries({ queryKey: ['system-settings'] });
    },
    onError: (e: any) => message.error(e?.message || '保存失败'),
  });

  return (
    <>
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 16 }}
        message="构建引擎切换：Jenkins（默认）或 Tekton（全局构建集群）"
        description="选择 Tekton 后，构建在平台自有 Tekton 集群运行（git-clone + BuildKit 推送镜像），与托管工作负载集群解耦。kubeconfig 为空时使用 in-cluster 配置。"
      />
      <Card title="构建引擎配置">
        <Form layout="vertical" form={form} onFinish={(v) => saveMutation.mutate(v)}>
          <Form.Item name="engine" label="构建引擎" rules={[{ required: true }]}>
            <Select
              options={[
                { label: 'Jenkins（默认）', value: 'jenkins' },
                { label: 'Tekton', value: 'tekton' },
              ]}
            />
          </Form.Item>
          <Form.Item shouldUpdate={(prev, cur) => prev.engine !== cur.engine} noStyle>
            {({ getFieldValue }) =>
              getFieldValue('engine') === 'tekton' ? (
                <>
                  <Form.Item name="tekton_namespace" label="Tekton 命名空间">
                    <Input placeholder="vo-builds" />
                  </Form.Item>
                  <Form.Item
                    name="tekton_kubeconfig"
                    label="构建集群 kubeconfig"
                    tooltip="base64 或 PEM 文本；留空使用 in-cluster 配置"
                  >
                    <Input.TextArea
                      rows={8}
                      placeholder="粘贴构建集群 kubeconfig 内容（base64 或 PEM 文本）"
                      style={{ fontFamily: 'monospace', fontSize: 12 }}
                    />
                  </Form.Item>
                </>
              ) : null
            }
          </Form.Item>
          <Form.Item>
            <Button type="primary" htmlType="submit" loading={saveMutation.isPending}>
              保存
            </Button>
          </Form.Item>
        </Form>
      </Card>
    </>
  );
}

// ============== Tab：基础镜像维护（Docker 基础镜像 CRUD）==============

function BaseImageTab() {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const [modalOpen, setModalOpen] = useState(false);
  const [editTarget, setEditTarget] = useState<BaseImage | null>(null);
  const [form] = Form.useForm();
  const watchedRuntime = Form.useWatch('runtime', form);
  const watchedIsWeb = Form.useWatch('is_web', form);
  const watchedEntrypointRaw = Form.useWatch('entrypoint_raw', form);
  const watchedImageRef = Form.useWatch('image_ref', form);

  const { data, isLoading } = useQuery({
    queryKey: ['base-images', 'all'],
    queryFn: () => buildApi.listBaseImages({ page: 1, size: 200 }),
  });
  const items = data?.items ?? [];

  const openCreate = () => {
    setEditTarget(null);
    form.resetFields();
    form.setFieldsValue({
      runtime: 'custom',
      is_recommended: false,
      is_web: false,
      entrypoint_raw: RUNTIME_DEFAULT_ENTRYPOINTS.custom,
    });
    setModalOpen(true);
  };

  const openEdit = (record: BaseImage) => {
    setEditTarget(record);
    form.setFieldsValue({
      name: record.name,
      runtime: record.runtime,
      image_ref: record.image_ref,
      description: record.description,
      dockerfile_template: record.dockerfile_template,
      is_recommended: record.is_recommended,
      is_web: record.is_web ?? false,
      entrypoint_raw: appEntrypointRawForEdit(record),
    });
    setModalOpen(true);
  };

  const submitMutation = useMutation({
    mutationFn: async (v: any) => {
      const entrypoint = parseEntrypointRaw(v.entrypoint_raw);
      const body: Partial<BaseImage> = {
        name: v.name,
        runtime: v.runtime,
        image_ref: v.image_ref,
        description: v.description,
        dockerfile_template: v.dockerfile_template,
        is_recommended: v.is_recommended ?? false,
        is_web: v.is_web ?? false,
        entrypoint,
      };
      if (editTarget) {
        return buildApi.updateBaseImage(editTarget.id, { ...body, version: editTarget.version });
      }
      return buildApi.createBaseImage(body);
    },
    onSuccess: () => {
      message.success(editTarget ? '基础镜像已更新' : '基础镜像已创建');
      setModalOpen(false);
      form.resetFields();
      queryClient.invalidateQueries({ queryKey: ['base-images'] });
    },
    onError: (e: any) => message.error(e?.message || '操作失败'),
  });

  const deleteMutation = useMutation({
    mutationFn: (id: number) => buildApi.deleteBaseImage(id),
    onSuccess: () => {
      message.success('基础镜像已删除');
      queryClient.invalidateQueries({ queryKey: ['base-images'] });
    },
    onError: (e: any) => message.error(e?.message || '删除失败'),
  });

  const columns: ColumnsType<BaseImage> = [
    { title: '名称', dataIndex: 'name', key: 'name' },
    {
      title: '语言',
      dataIndex: 'runtime',
      key: 'runtime',
      width: 100,
      render: (v: string) => <Tag color="blue">{v}</Tag>,
    },
    { title: '镜像引用', dataIndex: 'image_ref', key: 'image_ref', render: (v: string) => <code>{v}</code> },
    {
      title: '类型',
      dataIndex: 'is_web',
      key: 'is_web',
      width: 90,
      render: (v: boolean | undefined) => (v ? <Tag color="cyan">Web + nginx</Tag> : <Tag>后端</Tag>),
    },
    {
      title: '启动命令',
      dataIndex: 'entrypoint',
      key: 'entrypoint',
      render: (v: string[] | undefined, r: BaseImage) => {
        const appEp = v && v.length > 0 ? JSON.stringify(v) : null;
        if (!appEp) return <span style={{ color: '#bfbfbf' }}>基础镜像默认</span>;
        return (
          <Space direction="vertical" size={0}>
            <code style={{ fontSize: 12 }}>{appEp}</code>
            {r.is_web ? <Tag color="cyan" style={{ marginTop: 2 }}>+ nginx</Tag> : null}
          </Space>
        );
      },
    },
    {
      title: '推荐',
      dataIndex: 'is_recommended',
      key: 'is_recommended',
      width: 80,
      render: (v: boolean) => (v ? <Tag color="gold">推荐</Tag> : '-'),
    },
    { title: '创建时间', dataIndex: 'created_at', key: 'created_at', width: 160, render: formatTime },
    {
      title: '操作',
      key: 'actions',
      width: 120,
      render: (_, r) => (
        <Space>
          <a onClick={() => openEdit(r)}>编辑</a>
          <a
            onClick={() =>
              confirmDanger({
                title: '删除基础镜像',
                content: `确定删除 "${r.name}" 吗？`,
                onOk: () => deleteMutation.mutate(r.id),
              })
            }
          >
            删除
          </a>
        </Space>
      ),
    },
  ];

  return (
    <>
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 16 }}
        message="基础镜像：维护各语言可用的 Docker 运行时镜像与单阶段运行时模板。"
        description="传统 CI 模式下，基础镜像仅作为运行时镜像（不含构建工具）。Web 镜像会在应用启动命令之外额外启动 nginx（用于反向代理/静态资源）；nginx 配置请在应用「配置」中挂载。Dockerfile 模板为单阶段，仅 COPY 制品。"
      />
      <Card
        title="基础镜像"
        extra={
          <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
            新建
          </Button>
        }
      >
        <Table
          rowKey="id"
          loading={isLoading}
          columns={columns}
          dataSource={items}
          pagination={false}
        />
      </Card>

      <Modal
        title={editTarget ? '编辑基础镜像' : '新建基础镜像'}
        open={modalOpen}
        onCancel={() => setModalOpen(false)}
        onOk={() => form.submit()}
        confirmLoading={submitMutation.isPending}
        destroyOnClose
        width={720}
      >
        <Form
          layout="vertical"
          form={form}
          onFinish={(v) => submitMutation.mutate(v)}
          initialValues={{ runtime: 'custom', is_recommended: false, is_web: false }}
        >
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入名称' }]}>
            <Input placeholder="例如 OpenJDK 17" />
          </Form.Item>
          <Form.Item name="runtime" label="语言/运行时" rules={[{ required: true, message: '请选择语言' }]}>
            <Select
              options={LANGUAGE_OPTIONS.map((o) => ({ label: o.label, value: o.value }))}
              placeholder="选择语言"
              onChange={(v: string) => {
                form.setFieldValue('entrypoint_raw', entrypointForRuntime(v));
              }}
            />
          </Form.Item>
          <Form.Item
            name="is_web"
            label="Web 镜像"
            valuePropName="checked"
            extra="开启后除下方应用启动命令外，额外启动 nginx。纯静态站点可留空启动命令（仅 nginx）；有后端进程时填写应用启动命令。nginx 配置请在应用「配置」中挂载（如 /etc/nginx/conf.d/default.conf）。"
          >
            <Switch />
          </Form.Item>
          <Form.Item
            name="image_ref"
            label="镜像引用"
            rules={[{ required: true, message: '请输入镜像引用' }]}
            extra={watchedIsWeb ? 'Web 镜像需基础镜像内已安装 nginx（可在 Dockerfile 模板中 RUN 安装）' : '运行时基础镜像，如 eclipse-temurin:17-jre'}
          >
            <Input placeholder="eclipse-temurin:17-jre" />
          </Form.Item>
          <Form.Item
            name="entrypoint_raw"
            label="应用启动命令 (ENTRYPOINT)"
            extra={
              <span>
                JSON 数组，仅配置<strong>应用</strong>启动命令，不含 nginx。
                {watchedIsWeb ? (
                  <> 开启 Web 镜像后，渲染 Dockerfile 时自动在应用启动前执行 <code>nginx</code>。</>
                ) : (
                  <> 按语言预填，留空使用基础镜像默认 CMD。</>
                )}
              </span>
            }
            rules={[
              {
                validator: (_, value) => {
                  if (!value) return Promise.resolve();
                  try {
                    const v = JSON.parse(value);
                    if (Array.isArray(v)) return Promise.resolve();
                  } catch {
                    // fallthrough
                  }
                  return Promise.reject(new Error('请输入合法的 JSON 数组'));
                },
              },
            ]}
          >
            <Input.TextArea
              rows={2}
              placeholder='["sh","-c","exec java $JAVA_OPTS -jar /app/artifacts/*.jar"]'
              style={{ fontFamily: 'monospace', fontSize: 12 }}
            />
          </Form.Item>
          {watchedIsWeb && (
            <Alert
              type="info"
              showIcon
              style={{ marginBottom: 16 }}
              message="有效启动命令预览"
              description={
                <code style={{ fontSize: 12 }}>
                  {previewEffectiveEntrypoint(true, watchedEntrypointRaw || '')}
                </code>
              }
            />
          )}
          <Form.Item
            name="dockerfile_template"
            label="Dockerfile 模板"
            extra={'单阶段运行时模板，可用占位符：{{.BaseImage}}、{{.ArtifactPath}}、{{.Entrypoint}}（输入 { 触发补全，悬停查看说明）'}
            getValueFromEvent={(v: string) => v ?? ''}
          >
            <DockerfileEditor
              height={340}
              runtime={watchedRuntime}
              isWeb={!!watchedIsWeb}
              entrypointRaw={watchedEntrypointRaw ?? ''}
              sampleValues={{ BaseImage: watchedImageRef || undefined, ArtifactPath: 'target/*.jar' }}
            />
          </Form.Item>
          <Form.Item name="description" label="描述">
            <Input.TextArea rows={2} placeholder="可选" />
          </Form.Item>
          <Form.Item name="is_recommended" label="设为推荐" valuePropName="checked">
            <Switch />
          </Form.Item>
        </Form>
      </Modal>
    </>
  );
}

// ============== Tab：构建工具维护（可配置化构建工具 CRUD）==============

function BuildToolTab() {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const [modalOpen, setModalOpen] = useState(false);
  const [editTarget, setEditTarget] = useState<BuildTool | null>(null);
  const [form] = Form.useForm();

  const { data, isLoading } = useQuery({
    queryKey: ['build-tools', 'all'],
    queryFn: () => buildApi.listBuildTools({ page: 1, size: 200 }),
  });
  const items = data?.items ?? [];

  const openCreate = () => {
    setEditTarget(null);
    form.resetFields();
    setModalOpen(true);
  };

  const openEdit = (record: BuildTool) => {
    setEditTarget(record);
    form.setFieldsValue({
      name: record.name,
      runtime: record.runtime,
      tool: record.tool,
      default_build_command: record.default_build_command,
      default_artifact_path: record.default_artifact_path,
      builder_image: record.builder_image,
      description: record.description,
      is_system: record.is_system,
    });
    setModalOpen(true);
  };

  const submitMutation = useMutation({
    mutationFn: async (v: any) => {
      const body: Partial<BuildTool> = {
        name: v.name,
        runtime: v.runtime,
        tool: v.tool,
        default_build_command: v.default_build_command,
        default_artifact_path: v.default_artifact_path,
        builder_image: v.builder_image,
        description: v.description,
        is_system: v.is_system ?? false,
      };
      if (editTarget) {
        return buildApi.updateBuildTool(editTarget.id, { ...body, version: editTarget.version });
      }
      return buildApi.createBuildTool(body);
    },
    onSuccess: () => {
      message.success(editTarget ? '构建工具已更新' : '构建工具已创建');
      setModalOpen(false);
      form.resetFields();
      queryClient.invalidateQueries({ queryKey: ['build-tools'] });
    },
    onError: (e: any) => message.error(e?.message || '操作失败'),
  });

  const deleteMutation = useMutation({
    mutationFn: (id: number) => buildApi.deleteBuildTool(id),
    onSuccess: () => {
      message.success('构建工具已删除');
      queryClient.invalidateQueries({ queryKey: ['build-tools'] });
    },
    onError: (e: any) => message.error(e?.message || '删除失败'),
  });

  const columns: ColumnsType<BuildTool> = [
    { title: '名称', dataIndex: 'name', key: 'name' },
    {
      title: '语言',
      dataIndex: 'runtime',
      key: 'runtime',
      width: 90,
      render: (v: string) => <Tag color="blue">{v}</Tag>,
    },
    { title: '工具', dataIndex: 'tool', key: 'tool', width: 100 },
    { title: '默认构建命令', dataIndex: 'default_build_command', key: 'default_build_command' },
    { title: '默认制品路径', dataIndex: 'default_artifact_path', key: 'default_artifact_path', width: 150 },
    { title: 'Builder 镜像', dataIndex: 'builder_image', key: 'builder_image', render: (v: string) => <code>{v}</code> },
    {
      title: '系统',
      dataIndex: 'is_system',
      key: 'is_system',
      width: 70,
      render: (v: boolean) => (v ? <Tag color="purple">系统</Tag> : '-'),
    },
    { title: '创建时间', dataIndex: 'created_at', key: 'created_at', width: 160, render: formatTime },
    {
      title: '操作',
      key: 'actions',
      width: 120,
      render: (_, r) => (
        <Space>
          <a onClick={() => openEdit(r)}>编辑</a>
          <a
            onClick={() =>
              confirmDanger({
                title: '删除构建工具',
                content: `确定删除 "${r.name}" 吗？`,
                onOk: () => deleteMutation.mutate(r.id),
              })
            }
          >
            删除
          </a>
        </Space>
      ),
    },
  ];

  return (
    <>
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 16 }}
        message="构建工具：可配置化的构建工具元数据，新增构建工具零代码改动。"
        description="传统 CI 模式下，构建在 Jenkins/Tekton 引擎侧用 builder_image 容器执行默认构建命令产出制品。每条构建工具含语言+工具名+默认命令+默认制品路径+builder 镜像。新建构建时按应用语言过滤候选工具，选中后自动预填。"
      />
      <Card
        title="构建工具"
        extra={
          <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
            新建
          </Button>
        }
      >
        <Table
          rowKey="id"
          loading={isLoading}
          columns={columns}
          dataSource={items}
          pagination={false}
        />
      </Card>

      <Modal
        title={editTarget ? '编辑构建工具' : '新建构建工具'}
        open={modalOpen}
        onCancel={() => setModalOpen(false)}
        onOk={() => form.submit()}
        confirmLoading={submitMutation.isPending}
        destroyOnClose
        width={640}
      >
        <Form
          layout="vertical"
          form={form}
          onFinish={(v) => submitMutation.mutate(v)}
          initialValues={{ runtime: 'java', is_system: false }}
        >
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入名称' }]}>
            <Input placeholder="例如 Java Maven" />
          </Form.Item>
          <Form.Item name="runtime" label="语言/运行时" rules={[{ required: true, message: '请选择语言' }]}>
            <Select
              options={LANGUAGE_OPTIONS.map((o) => ({ label: o.label, value: o.value }))}
              placeholder="选择语言"
            />
          </Form.Item>
          <Form.Item name="tool" label="工具名" rules={[{ required: true, message: '请输入工具名' }]} extra="如 maven、gradle、npm、yarn、pnpm、go、pip、poetry">
            <Input placeholder="maven" />
          </Form.Item>
          <Form.Item name="default_build_command" label="默认构建命令" extra="新建构建时预填，用户可编辑">
            <Input placeholder="例如 mvn -B clean package -DskipTests" />
          </Form.Item>
          <Form.Item name="default_artifact_path" label="默认制品路径" extra="构建产出的制品路径，COPY 进运行时镜像">
            <Input placeholder="例如 target/*.jar" />
          </Form.Item>
          <Form.Item
            name="builder_image"
            label="Builder 镜像"
            rules={[{ required: true, message: '请输入 builder 镜像' }]}
            extra="引擎侧执行构建命令的工具链镜像，如 maven:3.9-eclipse-temurin-17"
          >
            <Input placeholder="maven:3.9-eclipse-temurin-17" />
          </Form.Item>
          <Form.Item name="description" label="描述">
            <Input.TextArea rows={2} placeholder="可选" />
          </Form.Item>
          <Form.Item name="is_system" label="系统内置" valuePropName="checked" extra="标记为系统内置工具">
            <Switch />
          </Form.Item>
        </Form>
      </Modal>
    </>
  );
}

// ============== Tab：AI 诊断可配置 Provider ==============

const KEY_AI_PROVIDER = 'ai.diagnosis.provider';
const KEY_AI_URL = 'ai.diagnosis.url';
const KEY_AI_API_KEY = 'ai.diagnosis.api_key';
const KEY_AI_MODEL = 'ai.diagnosis.model';

// 向量嵌入配置（RAG 知识库向量化用）。
const KEY_EMBED_PROVIDER = 'ai.embedding.provider';
const KEY_EMBED_URL = 'ai.embedding.url';
const KEY_EMBED_API_KEY = 'ai.embedding.api_key';
const KEY_EMBED_MODEL = 'ai.embedding.model';
const KEY_EMBED_DIMENSIONS = 'ai.embedding.dimensions';

function AIDiagnosisTab() {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const [form] = Form.useForm();
  const [embedForm] = Form.useForm();

  const { data: settings } = useQuery({
    queryKey: ['system-settings', 'all'],
    queryFn: () => systemApi.listAll(),
  });
  const items = Array.isArray(settings) ? settings : [];
  const get = (key: string) => items.find((x) => x.key === key)?.value;

  const provider = get(KEY_AI_PROVIDER) ?? 'openai';
  const url = get(KEY_AI_URL) ?? '';
  const apiKey = get(KEY_AI_API_KEY) ?? '';
  const model = get(KEY_AI_MODEL) ?? 'gpt-4o-mini';

  const embedProvider = get(KEY_EMBED_PROVIDER) ?? 'openai';
  const embedURL = get(KEY_EMBED_URL) ?? '';
  const embedAPIKey = get(KEY_EMBED_API_KEY) ?? '';
  const embedModel = get(KEY_EMBED_MODEL) ?? 'text-embedding-3-small';
  const embedDim = get(KEY_EMBED_DIMENSIONS) ?? '1536';

  useEffect(() => {
    form.setFieldsValue({ provider, url, api_key: apiKey, model });
  }, [provider, url, apiKey, model, form]);

  useEffect(() => {
    embedForm.setFieldsValue({
      provider: embedProvider, url: embedURL, api_key: embedAPIKey,
      model: embedModel, dimensions: embedDim,
    });
  }, [embedProvider, embedURL, embedAPIKey, embedModel, embedDim, embedForm]);

  const saveMutation = useMutation({
    mutationFn: async (v: any) => {
      await systemApi.update(KEY_AI_PROVIDER, { value: v.provider, description: 'AI 诊断 Provider' });
      await systemApi.update(KEY_AI_URL, { value: v.url, description: 'LLM 服务基础 URL' });
      await systemApi.update(KEY_AI_API_KEY, { value: v.api_key, description: 'LLM API Key' });
      await systemApi.update(KEY_AI_MODEL, { value: v.model, description: '诊断所用模型名' });
    },
    onSuccess: () => {
      message.success('AI 诊断配置已保存');
      queryClient.invalidateQueries({ queryKey: ['system-settings'] });
    },
    onError: (e: any) => message.error(e?.message || '保存失败'),
  });

  const saveEmbedMutation = useMutation({
    mutationFn: async (v: any) => {
      await systemApi.update(KEY_EMBED_PROVIDER, { value: v.provider, description: '向量嵌入 Provider' });
      await systemApi.update(KEY_EMBED_URL, { value: v.url, description: '向量嵌入 API 基地址' });
      await systemApi.update(KEY_EMBED_API_KEY, { value: v.api_key, description: '向量嵌入 API Key' });
      await systemApi.update(KEY_EMBED_MODEL, { value: v.model, description: '向量嵌入模型名' });
      await systemApi.update(KEY_EMBED_DIMENSIONS, { value: v.dimensions, description: '向量维度' });
    },
    onSuccess: () => {
      message.success('向量嵌入配置已保存（需重启 apiserver 生效）');
      queryClient.invalidateQueries({ queryKey: ['system-settings'] });
    },
    onError: (e: any) => message.error(e?.message || '保存失败'),
  });

  return (
    <>
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 16 }}
        message="AI 诊断：可配置 LLM Provider，收集 K8s 上下文（events/logs/describe）后调用 LLM 给出根因与修复建议。"
        description="支持 OpenAI 兼容（含 vLLM/LM Studio）、Anthropic Claude、Ollama 本地模型。配置后可在「AI 诊断」页面针对 Pod/Deployment/Node 发起诊断。"
      />
      <Card title="AI 诊断配置">
        <Form layout="vertical" form={form} onFinish={(v) => saveMutation.mutate(v)}>
          <Form.Item name="provider" label="Provider" rules={[{ required: true }]}>
            <Select
              options={[
                { label: 'OpenAI 兼容（OpenAI / vLLM / LM Studio）', value: 'openai' },
                { label: 'Anthropic Claude', value: 'anthropic' },
                { label: 'Ollama（本地）', value: 'ollama' },
              ]}
            />
          </Form.Item>
          <Form.Item name="url" label="服务 URL" rules={[{ required: true, message: '请输入服务 URL' }]}>
            <Input placeholder="https://api.openai.com 或 http://ollama:11434" />
          </Form.Item>
          <Form.Item name="api_key" label="API Key" tooltip="Ollama 可留空">
            <Input.Password placeholder="sk-..." />
          </Form.Item>
          <Form.Item name="model" label="模型名" rules={[{ required: true }]}>
            <Input placeholder="gpt-4o-mini / claude-3-5-sonnet / qwen2.5:7b" />
          </Form.Item>
          <Form.Item>
            <Button type="primary" htmlType="submit" loading={saveMutation.isPending}>
              保存
            </Button>
          </Form.Item>
        </Form>
      </Card>

      <Card title="向量嵌入配置（RAG 知识库）" style={{ marginTop: 16 }}>
        <Alert
          type="info"
          showIcon
          style={{ marginBottom: 16 }}
          message="向量嵌入用于将知识库文档向量化，供 AI 助手 RAG 检索。"
          description="通常可与 LLM 共用同一服务（OpenAI 兼容服务同时提供 /v1/chat/completions 与 /v1/embeddings）。修改后需重启 apiserver 生效。"
        />
        <Form layout="vertical" form={embedForm} onFinish={(v) => saveEmbedMutation.mutate(v)}>
          <Form.Item name="provider" label="Provider" rules={[{ required: true }]}>
            <Select
              options={[
                { label: 'OpenAI 兼容（OpenAI / vLLM / LM Studio）', value: 'openai' },
                { label: 'Anthropic Claude', value: 'anthropic' },
                { label: 'Ollama（本地）', value: 'ollama' },
              ]}
            />
          </Form.Item>
          <Form.Item name="url" label="服务 URL" rules={[{ required: true, message: '请输入服务 URL' }]}>
            <Input placeholder="https://api.openai.com 或 http://ollama:11434" />
          </Form.Item>
          <Form.Item name="api_key" label="API Key" tooltip="Ollama 可留空">
            <Input.Password placeholder="sk-..." />
          </Form.Item>
          <Form.Item name="model" label="嵌入模型名" rules={[{ required: true }]}>
            <Input placeholder="text-embedding-3-small / bge-large-zh / nomic-embed-text" />
          </Form.Item>
          <Form.Item name="dimensions" label="向量维度" rules={[{ required: true }]} tooltip="需与 vo_kb_chunks.embedding 维度一致，默认 1536（OpenAI text-embedding-3-small）。修改维度需重建索引。">
            <Input placeholder="1536" />
          </Form.Item>
          <Form.Item>
            <Button type="primary" htmlType="submit" loading={saveEmbedMutation.isPending}>
              保存
            </Button>
          </Form.Item>
        </Form>
      </Card>
    </>
  );
}

// ============== Tab：堡垒机（JumpServer）集成 ==============

const KEY_JMS_BASE_URL = 'jumpserver.base_url';
const KEY_JMS_ACCESS_KEY = 'jumpserver.access_key';
const KEY_JMS_SECRET_KEY = 'jumpserver.secret_key';

function JumpServerTab() {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const [form] = Form.useForm();

  const { data: settings } = useQuery({
    queryKey: ['system-settings', 'all'],
    queryFn: () => systemApi.listAll(),
  });
  const items = Array.isArray(settings) ? settings : [];
  const get = (key: string) => items.find((x) => x.key === key)?.value;

  const baseUrl = get(KEY_JMS_BASE_URL) ?? '';
  const accessKey = get(KEY_JMS_ACCESS_KEY) ?? '';
  const secretKey = get(KEY_JMS_SECRET_KEY) ?? '';

  useEffect(() => {
    form.setFieldsValue({ base_url: baseUrl, access_key: accessKey, secret_key: secretKey });
  }, [baseUrl, accessKey, secretKey, form]);

  const saveMutation = useMutation({
    mutationFn: async (v: any) => {
      await systemApi.update(KEY_JMS_BASE_URL, { value: v.base_url, description: 'JumpServer 基础 URL' });
      await systemApi.update(KEY_JMS_ACCESS_KEY, { value: v.access_key, description: 'JumpServer API Access Key' });
      await systemApi.update(KEY_JMS_SECRET_KEY, { value: v.secret_key, description: 'JumpServer API Secret Key' });
    },
    onSuccess: () => {
      message.success('JumpServer 配置已保存');
      queryClient.invalidateQueries({ queryKey: ['system-settings'] });
    },
    onError: (e: any) => message.error(e?.message || '保存失败'),
  });

  return (
    <>
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 16 }}
        message="堡垒机集成：通过 JumpServer API 签发 SSO 连接 URL 并同步资产/会话。"
        description="配置 JumpServer 基础 URL 与 API Access/Secret Key（在 JumpServer 管理后台创建 API 密钥）。连接资产时由后端用 HMAC-SHA256 签名调用 JMS /api/v1/authentication/connection-token/ 签发登录 URL，全程不接触明文密码。"
      />
      <Card title="JumpServer 连接配置">
        <Form layout="vertical" form={form} onFinish={(v) => saveMutation.mutate(v)}>
          <Form.Item name="base_url" label="JumpServer Base URL" rules={[{ required: true, message: '请输入 Base URL' }]}>
            <Input placeholder="http://jumpserver-web:80 或 http://localhost:8090" />
          </Form.Item>
          <Form.Item name="access_key" label="Access Key" rules={[{ required: true, message: '请输入 Access Key' }]}>
            <Input placeholder="JumpServer API Access Key" />
          </Form.Item>
          <Form.Item name="secret_key" label="Secret Key" rules={[{ required: true, message: '请输入 Secret Key' }]}>
            <Input.Password placeholder="JumpServer API Secret Key" />
          </Form.Item>
          <Form.Item>
            <Button type="primary" htmlType="submit" loading={saveMutation.isPending}>
              保存
            </Button>
          </Form.Item>
        </Form>
      </Card>
    </>
  );
}
