import { useState } from 'react';
import { Select, Modal, Input, Form, App } from 'antd';
import { PlusOutlined } from '@ant-design/icons';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { buildApi } from '@/api/builds';
import type { Credential } from '@/types';

interface CredentialPickerProps {
  /** 凭证 kind：jenkins / basic / registry_pull 等 */
  kind: string;
  /** 凭证字段结构：决定「新建凭证」表单需要哪些字段 */
  fields: CredentialField[];
  value?: number;
  onChange?: (value: number | undefined) => void;
  placeholder?: string;
  scope?: string;
}

export interface CredentialField {
  key: string;
  label: string;
  required: boolean;
  placeholder?: string;
}

/**
 * 凭证选择器：
 * 1. 下拉展示已存在的凭证（按 kind 过滤）
 * 2. 下拉内嵌「+ 新建凭证」选项，点击弹出表单创建并自动选中
 */
export function CredentialPicker({
  kind,
  fields,
  value,
  onChange,
  placeholder,
  scope = 'platform',
}: CredentialPickerProps) {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const [createOpen, setCreateOpen] = useState(false);
  const [createForm] = Form.useForm();

  const { data, isLoading } = useQuery({
    queryKey: ['credentials', kind],
    queryFn: () => buildApi.listCredentials({ kind, scope, page: 1, size: 200 }),
  });
  const items: Credential[] = data?.items ?? [];

  const createMutation = useMutation({
    mutationFn: async (v: any) => {
      const payload: Record<string, string> = {};
      for (const f of fields) {
        payload[f.key] = v[f.key];
      }
      return buildApi.createCredential({
        name: v.name,
        kind,
        scope,
        payload,
        description: v.description,
      });
    },
    onSuccess: (cred) => {
      message.success('凭证已创建');
      onChange?.(cred.id);
      setCreateOpen(false);
      createForm.resetFields();
      queryClient.invalidateQueries({ queryKey: ['credentials', kind] });
    },
    onError: (e: any) => message.error(e?.message || '创建凭证失败'),
  });

  return (
    <>
      <Select
        style={{ width: '100%' }}
        loading={isLoading}
        value={value}
        onChange={onChange}
        allowClear
        placeholder={placeholder ?? '选择凭证（必填，测试连接与构建都需要）'}
        options={items.map((c) => ({ label: `${c.name}（id=${c.id}）`, value: c.id }))}
        dropdownRender={(menu) => (
          <>
            {menu}
            <div style={{ padding: '4px 8px', borderTop: '1px solid #f0f0f0' }}>
              <a
                onClick={() => {
                  createForm.resetFields();
                  setCreateOpen(true);
                }}
              >
                <PlusOutlined /> 新建凭证
              </a>
            </div>
          </>
        )}
        notFoundContent={items.length === 0 ? '暂无凭证，请点下方「新建凭证」' : undefined}
      />

      <Modal
        title="新建凭证"
        open={createOpen}
        onCancel={() => setCreateOpen(false)}
        onOk={() => createForm.submit()}
        confirmLoading={createMutation.isPending}
        destroyOnClose
      >
        <Form form={createForm} layout="vertical" onFinish={(v) => createMutation.mutate(v)}>
          <Form.Item name="name" label="凭证名称" rules={[{ required: true, message: '请输入凭证名称' }]}>
            <Input placeholder="例如 prod-jenkins-token" />
          </Form.Item>
          {fields.map((f) => (
            <Form.Item
              key={f.key}
              name={f.key}
              label={f.label}
              rules={f.required ? [{ required: true, message: `请输入${f.label}` }] : []}
            >
              <Input.Password autoComplete="new-password" placeholder={f.placeholder ?? `请输入${f.label}`} />
            </Form.Item>
          ))}
          <Form.Item name="description" label="描述">
            <Input.TextArea rows={2} placeholder="可选" />
          </Form.Item>
        </Form>
      </Modal>
    </>
  );
}
