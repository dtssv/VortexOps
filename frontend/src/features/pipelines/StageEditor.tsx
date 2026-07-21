import { useState } from 'react';
import { Button, Card, Form, Input, Select, Space, Table, Tag, Modal, InputNumber, Switch } from 'antd';
import { PlusOutlined, DeleteOutlined } from '@ant-design/icons';
import type { ColumnsType } from 'antd/es/table';

// 流水线阶段定义（与后端 pipelineapp.StageInput 对齐）。
export interface PipelineStage {
  seq: number;
  name: string;
  type: 'sequential' | 'parallel';
  on_failure: 'abort' | 'manual_retry' | 'continue';
  gate?: Record<string, any>;
  params?: Record<string, any>;
}

// 步骤类型常量（与后端 executor.StepKind 对齐）。
const STEP_KIND_OPTIONS = [
  { label: '构建 (build)', value: 'build' },
  { label: '扫描 (scan)', value: 'scan' },
  { label: '部署 (deploy)', value: 'deploy' },
  { label: '验证 (verify)', value: 'verify' },
  { label: '晋升 (promote)', value: 'promote' },
];

const ON_FAILURE_OPTIONS = [
  { label: '中止', value: 'abort' },
  { label: '手动重试', value: 'manual_retry' },
  { label: '继续', value: 'continue' },
];

const STAGE_TYPE_OPTIONS = [
  { label: '顺序', value: 'sequential' },
  { label: '并行', value: 'parallel' },
];

// 单个步骤的参数定义，按 step kind 不同展示不同字段。
interface StepParamDef {
  key: string;
  label: string;
  type: 'number' | 'string' | 'switch' | 'group_ids';
  placeholder?: string;
}

const STEP_PARAM_DEFS: Record<string, StepParamDef[]> = {
  build: [
    { key: 'application_id', label: '应用 ID', type: 'number' },
    { key: 'git_source_id', label: 'Git 源 ID', type: 'number' },
    { key: 'ref_value', label: '分支/Tag', type: 'string', placeholder: 'main' },
    { key: 'build_template_id', label: '构建模板 ID', type: 'number' },
    { key: 'wait', label: '等待构建完成', type: 'switch' },
  ],
  scan: [
    { key: 'image_id', label: '镜像 ID（留空取上游）', type: 'number' },
    { key: 'max_critical', label: 'Critical CVE 阈值', type: 'number' },
    { key: 'max_high', label: 'High CVE 阈值', type: 'number' },
  ],
  deploy: [
    { key: 'group_id', label: '目标分组 ID', type: 'number' },
    { key: 'image_id', label: '镜像 ID（留空取上游）', type: 'number' },
    { key: 'release_type', label: '发布类型', type: 'string', placeholder: 'rolling' },
  ],
  verify: [
    { key: 'timeout_sec', label: '超时（秒）', type: 'number' },
  ],
  promote: [
    { key: 'target_env', label: '目标环境', type: 'string', placeholder: 'staging' },
    { key: 'group_ids', label: '目标分组 ID（逗号分隔）', type: 'group_ids' },
    { key: 'image_id', label: '镜像 ID（留空取上游）', type: 'number' },
    { key: 'release_type', label: '发布类型', type: 'string', placeholder: 'rolling' },
  ],
};

interface StageEditorProps {
  value?: PipelineStage[];
  onChange?: (stages: PipelineStage[]) => void;
}

// StageEditor 流水线阶段编辑器：以卡片列表形式编辑阶段，每个阶段含步骤参数与门禁。
export function StageEditor({ value = [], onChange }: StageEditorProps) {
  const [editIndex, setEditIndex] = useState<number | null>(null);
  const [modalOpen, setModalOpen] = useState(false);
  const [form] = Form.useForm();

  const notify = (stages: PipelineStage[]) => {
    onChange?.(stages);
  };

  const openAdd = () => {
    setEditIndex(null);
    form.resetFields();
    form.setFieldsValue({
      type: 'sequential',
      on_failure: 'abort',
      step_kind: 'build',
      params: {},
      gate: {},
    });
    setModalOpen(true);
  };

  const openEdit = (index: number) => {
    setEditIndex(index);
    const stage = value[index];
    const stepKind = (stage.params?.kind as string) || 'build';
    form.setFieldsValue({
      name: stage.name,
      type: stage.type,
      on_failure: stage.on_failure,
      step_kind: stepKind,
      ...flattenParams(stage.params, stepKind),
    });
    setModalOpen(true);
  };

  const removeStage = (index: number) => {
    const next = value.filter((_, i) => i !== index).map((s, i) => ({ ...s, seq: i + 1 }));
    notify(next);
  };

  const submit = () => {
    form.validateFields().then((v) => {
      const stepKind = v.step_kind;
      const params = unflattenParams(v, stepKind);
      params['kind'] = stepKind;
      const stage: PipelineStage = {
        seq: 0,
        name: v.name,
        type: v.type,
        on_failure: v.on_failure,
        params,
        gate: v.gate_enabled ? { required: true } : undefined,
      };
      if (editIndex !== null) {
        const next = [...value];
        next[editIndex] = { ...stage, seq: editIndex + 1 };
        notify(next);
      } else {
        notify([...value, { ...stage, seq: value.length + 1 }]);
      }
      setModalOpen(false);
    });
  };

  const columns: ColumnsType<PipelineStage> = [
    { title: '序', dataIndex: 'seq', key: 'seq', width: 50 },
    { title: '名称', dataIndex: 'name', key: 'name' },
    {
      title: '步骤类型',
      key: 'kind',
      width: 120,
      render: (_, r) => <Tag color="blue">{r.params?.kind ?? '-'}</Tag>,
    },
    { title: '类型', dataIndex: 'type', key: 'type', width: 80 },
    { title: '失败策略', dataIndex: 'on_failure', key: 'on_failure', width: 100 },
    {
      title: '门禁',
      key: 'gate',
      width: 80,
      render: (_, r) => (r.gate ? <Tag color="gold">是</Tag> : '-'),
    },
    {
      title: '操作',
      key: 'actions',
      width: 120,
      render: (_, _r, i) => (
        <Space>
          <a onClick={() => openEdit(i)}>编辑</a>
          <a onClick={() => removeStage(i)} style={{ color: '#ff4d4f' }}>
            删除
          </a>
        </Space>
      ),
    },
  ];

  return (
    <>
      <Card
        size="small"
        title="阶段定义"
        extra={
          <Button size="small" icon={<PlusOutlined />} onClick={openAdd}>
            添加阶段
          </Button>
        }
      >
        <Table
          rowKey="seq"
          size="small"
          columns={columns}
          dataSource={value}
          pagination={false}
          locale={{ emptyText: '暂无阶段，点击「添加阶段」' }}
        />
      </Card>

      <Modal
        title={editIndex !== null ? '编辑阶段' : '添加阶段'}
        open={modalOpen}
        onCancel={() => setModalOpen(false)}
        onOk={submit}
        destroyOnHidden
        width={560}
      >
        <Form form={form} layout="vertical">
          <Form.Item name="name" label="阶段名称" rules={[{ required: true, message: '请输入阶段名称' }]}>
            <Input placeholder="如：构建" />
          </Form.Item>
          <Space style={{ display: 'flex' }} size="middle">
            <Form.Item name="step_kind" label="步骤类型" rules={[{ required: true }]}>
              <Select
                options={STEP_KIND_OPTIONS}
                style={{ width: 180 }}
                onChange={() => {
                  // 切换步骤类型时重置参数字段。
                  const kind = form.getFieldValue('step_kind');
                  form.setFieldsValue(unflattenParams({}, kind));
                }}
              />
            </Form.Item>
            <Form.Item name="type" label="阶段类型">
              <Select options={STAGE_TYPE_OPTIONS} style={{ width: 120 }} />
            </Form.Item>
            <Form.Item name="on_failure" label="失败策略">
              <Select options={ON_FAILURE_OPTIONS} style={{ width: 140 }} />
            </Form.Item>
          </Space>
          <Form.Item shouldUpdate={(prev, cur) => prev.step_kind !== cur.step_kind} noStyle>
            {({ getFieldValue }) => {
              const kind = getFieldValue('step_kind') || 'build';
              const defs = STEP_PARAM_DEFS[kind] ?? [];
              return (
                <Card size="small" type="inner" title="步骤参数" style={{ marginBottom: 16 }}>
                  {defs.map((def) => (
                    <Form.Item key={def.key} name={def.key} label={def.label}>
                      {def.type === 'number' ? (
                        <InputNumber style={{ width: '100%' }} placeholder={def.placeholder} />
                      ) : def.type === 'switch' ? (
                        <Switch />
                      ) : def.type === 'group_ids' ? (
                        <Input placeholder={def.placeholder || '1,2,3'} />
                      ) : (
                        <Input placeholder={def.placeholder} />
                      )}
                    </Form.Item>
                  ))}
                </Card>
              );
            }}
          </Form.Item>
          <Form.Item name="gate_enabled" label="启用人工门禁" valuePropName="checked">
            <Switch />
          </Form.Item>
        </Form>
      </Modal>
    </>
  );
}

// flattenParams 将 params map 展开为表单字段（按 step kind 取对应字段）。
function flattenParams(params: Record<string, any> | undefined, kind: string): Record<string, any> {
  const flat: Record<string, any> = {};
  if (!params) return flat;
  const defs = STEP_PARAM_DEFS[kind] ?? [];
  for (const def of defs) {
    if (params[def.key] !== undefined) {
      if (def.type === 'group_ids' && Array.isArray(params[def.key])) {
        flat[def.key] = params[def.key].join(',');
      } else {
        flat[def.key] = params[def.key];
      }
    }
  }
  return flat;
}

// unflattenParams 将表单字段收拢为 params map。
function unflattenParams(values: Record<string, any>, kind: string): Record<string, any> {
  const params: Record<string, any> = {};
  const defs = STEP_PARAM_DEFS[kind] ?? [];
  for (const def of defs) {
    const v = values[def.key];
    if (v === undefined || v === null || v === '') continue;
    if (def.type === 'group_ids' && typeof v === 'string') {
      params[def.key] = v.split(',').map((s) => Number(s.trim())).filter((n) => n > 0);
    } else {
      params[def.key] = v;
    }
  }
  return params;
}
