import { useState } from 'react';
import { Button, Card, Form, Input, Modal, Select, Space, Switch, Table, Tag, App, Alert } from 'antd';
import { PlusOutlined } from '@ant-design/icons';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { ColumnsType } from 'antd/es/table';
import { PageContainer } from '@/components/PageContainer';
import { DockerfileEditor } from '@/components/DockerfileEditor';
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
import type { BaseImage } from '@/types';

export default function BaseImagesPage() {
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
    queryKey: ['base-images', 'page'],
    queryFn: () => buildApi.listBaseImages({ page: 1, size: 200 }),
  });
  const items = data?.items ?? [];

  const openCreate = () => {
    setEditTarget(null);
    form.resetFields();
    form.setFieldsValue({ runtime: 'custom', is_recommended: false, is_web: false, entrypoint_raw: RUNTIME_DEFAULT_ENTRYPOINTS.custom });
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
    <PageContainer
      title="基础镜像维护"
      subtitle="维护各语言可用的 Docker 运行时镜像、单阶段运行时模板与启动命令"
      breadcrumb={[{ title: '系统管理' }, { title: '基础镜像' }]}
      extra={
        <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
          新建基础镜像
        </Button>
      }
    >
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 16 }}
        message="基础镜像：维护各语言可用的 Docker 运行时镜像与单阶段运行时模板。"
        description={
          <span>
            模板占位符：<code>{'{{.BaseImage}}'}</code>、<code>{'{{.ArtifactPath}}'}</code>、<code>{'{{.Entrypoint}}'}</code>（启动命令 JSON 数组）。
            Web 镜像会在应用启动命令之外额外启动 nginx；nginx 配置通过应用「配置」挂载。
          </span>
        }
      />
      <Card>
        <Table rowKey="id" loading={isLoading} columns={columns} dataSource={items} pagination={false} />
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
            extra="开启后除应用启动命令外额外启动 nginx。纯静态可留空启动命令；nginx 配置请在应用「配置」中挂载。"
          >
            <Switch />
          </Form.Item>
          <Form.Item
            name="image_ref"
            label="镜像引用"
            rules={[{ required: true, message: '请输入镜像引用' }]}
            extra={watchedIsWeb ? 'Web 镜像需基础镜像内已安装 nginx' : '运行时基础镜像，如 eclipse-temurin:17-jre'}
          >
            <Input placeholder="eclipse-temurin:17-jre" />
          </Form.Item>
          <Form.Item
            name="entrypoint_raw"
            label="应用启动命令 (ENTRYPOINT)"
            extra={
              <span>
                JSON 数组，仅配置应用启动命令，不含 nginx。
                {watchedIsWeb ? <> 渲染时自动在应用启动前执行 <code>nginx</code>。</> : <> 按语言预填，留空使用基础镜像默认 CMD。</>}
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
    </PageContainer>
  );
}
