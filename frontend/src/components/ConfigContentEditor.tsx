// 配置内容编辑器与只读预览（共享组件）。
// Content 结构与后端 vo_config_sets.content / vo_group_local_configs.content 对齐：
//   { files:[{path,content,mode,is_secret}], env:[{name,value,is_secret}], command:[...], args:[...] }
//
// 文件编辑：目录树展示文件名 + 弹框 Monaco 编辑器（按扩展名选 language，可在弹框内切换加密）。
// 底层仍用 Form.List("files") 存储数组，目录树为视图层渲染；调用方 onFinish 调 buildConfigContentFromForm。
import { useState } from 'react';
import { Button, Checkbox, Collapse, Descriptions, Form, Input, Modal, Space, Switch, Table, Tag, Tree, Typography, Tooltip } from 'antd';
import { DeleteOutlined, EditOutlined, FileOutlined, FolderOutlined, PlusOutlined } from '@ant-design/icons';
import type { ColumnsType } from 'antd/es/table';
import type { FormInstance } from 'antd/es/form';
import type { DataNode } from 'rc-tree/lib/interface';
import { EmptyState } from '@/components/EmptyState';
import { JsonEditor } from '@/components/JsonEditor';

export interface ConfigFileItem { path: string; content: string; mode?: string; is_secret?: boolean }
export interface ConfigEnvItem { name: string; value: string; is_secret?: boolean }
export interface ConfigContent {
  files?: ConfigFileItem[];
  env?: ConfigEnvItem[];
  command?: string[];
  args?: string[];
}

export function parseConfigContent(raw?: Record<string, any> | null): ConfigContent {
  if (!raw) return { files: [], env: [], command: [], args: [] };
  return {
    files: Array.isArray(raw.files) ? raw.files : [],
    env: Array.isArray(raw.env) ? raw.env : [],
    command: Array.isArray(raw.command) ? raw.command : [],
    args: Array.isArray(raw.args) ? raw.args : [],
  };
}

// 将 Form 编辑值整理为可保存的 ConfigContent（过滤空行、按空格切分 command/args）。
// env 不强制必填：name 为空的行视为未填写，自动过滤。
export function buildConfigContentFromForm(v: any): ConfigContent {
  return {
    files: (v.files || []).filter((f: ConfigFileItem) => f.path).map((f: ConfigFileItem) => ({
      path: f.path, content: f.content || '', mode: f.mode || '0644', is_secret: !!f.is_secret,
    })),
    env: (v.env || []).filter((e: ConfigEnvItem) => e.name).map((e: ConfigEnvItem) => ({
      name: e.name, value: e.value || '', is_secret: !!e.is_secret,
    })),
    command: v.command ? String(v.command).split(/\s+/).filter(Boolean) : [],
    args: v.args ? String(v.args).split(/\s+/).filter(Boolean) : [],
  };
}

// populateFormFromContent 将已有 ConfigContent 回填到指定 Form。
// env 可为空（不预置空行，避免触发必填校验）。
export function populateFormFromContent(form: FormInstance, content: ConfigContent) {
  form.setFieldsValue({
    files: content.files && content.files.length ? content.files : [],
    env: content.env && content.env.length ? content.env : [],
    command: (content.command || []).join(' '),
    args: (content.args || []).join(' '),
  });
}

// 按文件扩展名选 Monaco language。
function languageForPath(path: string): string {
  const ext = path.split('.').pop()?.toLowerCase();
  switch (ext) {
    case 'yaml': case 'yml': return 'yaml';
    case 'json': return 'json';
    case 'properties': case 'conf': case 'ini': return 'ini';
    case 'sh': case 'bash': return 'shell';
    case 'xml': return 'xml';
    case 'html': return 'html';
    case 'md': return 'markdown';
    case 'py': return 'python';
    case 'go': return 'go';
    case 'js': return 'javascript';
    case 'ts': return 'typescript';
    case 'sql': return 'sql';
    default: return 'plaintext';
  }
}

// 把扁平 files 数组按目录拆分为 antd Tree treeData。
// 每个文件叶子节点的 key = 其在 Form.List 中的索引（字符串），便于点击编辑。
interface FileTreeNode {
  title: React.ReactNode;
  key: string;
  icon?: React.ReactNode;
  isLeaf?: boolean;
  children?: FileTreeNode[];
}
function buildFileTree(files: ConfigFileItem[]): FileTreeNode[] {
  const root: FileTreeNode = { title: '/', key: 'root', icon: <FolderOutlined />, children: [] };
  files.forEach((f, idx) => {
    const parts = f.path.replace(/^\/+/, '').split('/').filter(Boolean);
    if (parts.length === 0) { return; }
    let node = root;
    parts.forEach((part, i) => {
      const isLeaf = i === parts.length - 1;
      const key = isLeaf ? `file-${idx}` : `dir-${parts.slice(0, i + 1).join('/')}`;
      let child = node.children!.find((c) => c.key === key);
      if (!child) {
        child = isLeaf
          ? { title: part, key, icon: <FileOutlined />, isLeaf: true }
          : { title: part, key, icon: <FolderOutlined />, children: [] };
        node.children!.push(child);
      }
      if (!isLeaf) { node = child; }
    });
  });
  // 目录在前、文件在后排序。
  const sortNode = (n: FileTreeNode) => {
    if (n.children && n.children.length) {
      n.children.sort((a, b) => {
        if (!!a.isLeaf !== !!b.isLeaf) { return a.isLeaf ? 1 : -1; }
        return String(a.title).localeCompare(String(b.title));
      });
      n.children.forEach(sortNode);
    }
  };
  sortNode(root);
  return root.children || [];
}

// ConfigContentEditor 表单内嵌编辑器：文件目录树 + Monaco 弹框 + env Form.List + command/args。
// 调用方需在自己的 <Form> 内使用本组件，并在 onFinish 中调用 buildConfigContentFromForm。
export function ConfigContentEditor() {
  const form = Form.useFormInstance();
  const files = Form.useWatch('files', form) as ConfigFileItem[] | undefined;
  // addOpen：新增文件弹框（路径 + 内容一起编辑，确定后写入 Form.List）。
  const [addOpen, setAddOpen] = useState(false);
  // editIdx：当前编辑的文件索引（Form.List 中的位置）；null 表示未打开编辑弹框。
  const [editIdx, setEditIdx] = useState<number | null>(null);
  // 弹框内编辑态（独立于 Form.List，确定后再写回）。
  const [draftPath, setDraftPath] = useState('');
  const [draftMode, setDraftMode] = useState('0644');
  const [draftSecret, setDraftSecret] = useState(false);
  const [draftContent, setDraftContent] = useState('');

  const fileList: ConfigFileItem[] = files || [];
  const treeData = buildFileTree(fileList);

  const resetDraft = () => {
    setDraftPath('');
    setDraftMode('0644');
    setDraftSecret(false);
    setDraftContent('');
  };

  // 打开新增弹框：清空草稿。
  const openAdd = () => {
    resetDraft();
    setAddOpen(true);
  };

  // 确定新增：将草稿写入 Form.List 末尾。
  const doAdd = () => {
    if (!draftPath.trim()) { return; }
    const next: ConfigFileItem[] = [...fileList, {
      path: draftPath.trim(), content: draftContent, mode: draftMode || '0644', is_secret: draftSecret,
    }];
    form.setFieldValue('files', next);
    setAddOpen(false);
    resetDraft();
  };

  const openEdit = (idx: number) => {
    const f = fileList[idx];
    if (!f) { return; }
    setEditIdx(idx);
    setDraftPath(f.path);
    setDraftMode(f.mode || '0644');
    setDraftSecret(!!f.is_secret);
    setDraftContent(f.content || '');
  };

  const closeEdit = () => {
    setEditIdx(null);
    resetDraft();
  };

  // 保存编辑弹框内容到 Form.List 对应索引。
  const saveEdit = () => {
    if (editIdx === null) { return; }
    if (!draftPath.trim()) { return; }
    form.setFieldValue(['files', editIdx], {
      path: draftPath.trim(), content: draftContent, mode: draftMode || '0644', is_secret: draftSecret,
    });
    closeEdit();
  };

  const removeFile = (idx: number) => {
    const next = fileList.filter((_, i) => i !== idx);
    form.setFieldValue('files', next);
  };

  const onSelect = (keys: React.Key[]) => {
    const k = keys[0] as string | undefined;
    if (k && k.startsWith('file-')) {
      openEdit(Number(k.slice(5)));
    }
  };

  return (
    <>
      <Typography.Text strong>配置文件</Typography.Text>
      <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginTop: 4 }}>
        按目录树展示，点击文件名编辑（弹框 Monaco 编辑器），替换镜像内同路径文件。新增时路径与内容一并填写。
      </Typography.Paragraph>
      <Form.List name="files">
        {(fields) => (
          <>
            {/* 隐藏字段承载，由目录树与弹框驱动 */}
            {fields.map((field) => (
              <Form.Item key={field.key} name={[field.name]} hidden>
                <Input />
              </Form.Item>
            ))}
          </>
        )}
      </Form.List>
      <div style={{ border: '1px solid #f0f0f0', borderRadius: 6, padding: 8, minHeight: 120 }}>
        {fileList.length === 0 ? (
          <EmptyState title="无配置文件" description="点击下方按钮新增配置文件" />
        ) : (
          <Tree
            showIcon
            blockNode
            treeData={treeData as DataNode[]}
            onSelect={onSelect}
            titleRender={(node) => {
              const key = (node as FileTreeNode).key as string;
              if (key.startsWith('file-')) {
                const idx = Number(key.slice(5));
                const f = fileList[idx];
                return (
                  <Space size={4}>
                    <span>{String(node.title)}</span>
                    {f?.is_secret ? <Tag color="red" style={{ marginInlineStart: 4 }}>密</Tag> : null}
                    <Tooltip title="编辑">
                      <Button type="text" size="small" icon={<EditOutlined />} onClick={(e) => { e.stopPropagation(); openEdit(idx); }} />
                    </Tooltip>
                    <Tooltip title="删除">
                      <Button type="text" size="small" danger icon={<DeleteOutlined />} onClick={(e) => { e.stopPropagation(); removeFile(idx); }} />
                    </Tooltip>
                  </Space>
                );
              }
              return <span>{String(node.title)}</span>;
            }}
          />
        )}
      </div>
      <Button type="dashed" block icon={<PlusOutlined />} style={{ marginTop: 8 }} onClick={openAdd}>
        新增配置文件
      </Button>

      <Typography.Text strong style={{ display: 'block', marginTop: 16 }}>环境变量</Typography.Text>
      <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginTop: 4 }}>
        注入为容器环境变量，可选。例如 JAVA_HOME=/opt/java
      </Typography.Paragraph>
      <Form.List name="env">
        {(fields, { add, remove }) => (
          <>
            {fields.map((field) => (
              <Space key={field.key} align="baseline" style={{ display: 'flex', marginBottom: 8 }}>
                <Form.Item name={[field.name, 'name']} noStyle>
                  <Input placeholder="变量名" style={{ width: 200 }} />
                </Form.Item>
                <Form.Item name={[field.name, 'value']} noStyle>
                  <Input placeholder="变量值" style={{ width: 280 }} />
                </Form.Item>
                <Form.Item name={[field.name, 'is_secret']} noStyle valuePropName="checked">
                  <Checkbox>密文</Checkbox>
                </Form.Item>
                <DeleteOutlined onClick={() => remove(field.name)} style={{ color: '#ff4d4f' }} />
              </Space>
            ))}
            <Button type="dashed" block icon={<PlusOutlined />} onClick={() => add({ name: '', value: '' })}>
              新增环境变量
            </Button>
          </>
        )}
      </Form.List>

      <Typography.Text strong style={{ display: 'block', marginTop: 16 }}>启动参数</Typography.Text>
      <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginTop: 4 }}>
        覆盖镜像 ENTRYPOINT/CMD。空格分隔。例如 command=java，args=-Xmx2g -jar /app/artifacts/*.jar
      </Typography.Paragraph>
      <Space style={{ width: '100%' }} size="middle">
        <Form.Item name="command" label="command" style={{ flex: 1 }}>
          <Input placeholder="如 java" />
        </Form.Item>
        <Form.Item name="args" label="args" style={{ flex: 1 }}>
          <Input placeholder="如 -Xmx2g -jar /app/artifacts/*.jar" />
        </Form.Item>
      </Space>

      {/* 新增/编辑文件弹框：路径 + 权限 + 加密 + Monaco 内容编辑器，确定后写回 Form.List */}
      <Modal
        title={editIdx !== null ? `编辑文件 - ${draftPath}` : '新增配置文件'}
        open={addOpen || editIdx !== null}
        onCancel={() => { setAddOpen(false); closeEdit(); }}
        onOk={() => { if (editIdx !== null) { saveEdit(); } else { doAdd(); } }}
        destroyOnHidden
        width={780}
        okText={editIdx !== null ? '保存' : '新增'}
      >
        <Space direction="vertical" size="middle" style={{ width: '100%' }}>
          <Space wrap>
            <Typography.Text>路径：</Typography.Text>
            <Input
              value={draftPath}
              onChange={(e) => setDraftPath(e.target.value)}
              style={{ width: 360 }}
              placeholder="如 /etc/app/app.conf 或 config/app.yaml"
            />
            <Typography.Text>权限：</Typography.Text>
            <Input value={draftMode} onChange={(e) => setDraftMode(e.target.value)} style={{ width: 90 }} />
            <Space size={4}>
              <Switch checked={draftSecret} onChange={setDraftSecret} />
              <Typography.Text>加密</Typography.Text>
            </Space>
          </Space>
          <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginBottom: 0 }}>
            加密标记表示文件内含 <code>{'{{...}}'}</code> 占位符，由应用启动时解密。内容原样写入 ConfigMap。
          </Typography.Paragraph>
          <div style={{ border: '1px solid #f0f0f0', borderRadius: 4 }}>
            <JsonEditor
              value={draftContent}
              language={languageForPath(draftPath)}
              height={360}
              onChange={setDraftContent}
            />
          </div>
        </Space>
      </Modal>
    </>
  );
}

// ConfigContentPreview 只读展示配置内容（files/env/command/args 摘要）。
// files 按目录树展示（与编辑器一致），env/启动参数仍用表格/Descriptions。
export function ConfigContentPreview({ content }: { content: ConfigContent }) {
  const files = content.files || [];
  const env = content.env || [];
  const command = content.command || [];
  const args = content.args || [];
  const empty = files.length === 0 && env.length === 0 && command.length === 0 && args.length === 0;
  if (empty) {
    return <EmptyState title="配置为空" description="点击「编辑内容」添加配置文件、环境变量或启动参数" />;
  }
  const treeData = buildFileTree(files);
  return (
    <Collapse
      size="small"
      defaultActiveKey={['files', 'env', 'args']}
      items={[
        files.length ? {
          key: 'files',
          label: `配置文件 (${files.length})`,
          children: (
            <div style={{ border: '1px solid #f0f0f0', borderRadius: 6, padding: 8 }}>
              <Tree
                showIcon
                blockNode
                treeData={treeData as DataNode[]}
                titleRender={(node) => {
                  const key = (node as FileTreeNode).key as string;
                  if (key.startsWith('file-')) {
                    const idx = Number(key.slice(5));
                    const f = files[idx];
                    return (
                      <Space size={4}>
                        <span>{String(node.title)}</span>
                        {f?.is_secret ? <Tag color="red" style={{ marginInlineStart: 4 }}>密</Tag> : null}
                        {f?.mode && f.mode !== '0644' ? (
                          <Typography.Text type="secondary" style={{ fontSize: 12 }}>{f.mode}</Typography.Text>
                        ) : null}
                      </Space>
                    );
                  }
                  return <span>{String(node.title)}</span>;
                }}
              />
            </div>
          ),
        } : null,
        env.length ? {
          key: 'env',
          label: `环境变量 (${env.length})`,
          children: (
            <Table<ConfigEnvItem>
              rowKey="name"
              size="small"
              pagination={false}
              dataSource={env}
              columns={[
                { title: '名称', dataIndex: 'name', render: (v: string) => <code>{v}</code> },
                { title: '值', dataIndex: 'value', render: (v: string) => v ? <code>{v}</code> : '-' },
                { title: '密文', dataIndex: 'is_secret', width: 60, render: (v?: boolean) => (v ? <Tag color="red">密</Tag> : '-') },
              ] as ColumnsType<ConfigEnvItem>}
            />
          ),
        } : null,
        (command.length || args.length) ? {
          key: 'args',
          label: '启动参数',
          children: (
            <Descriptions column={1} size="small" bordered>
              <Descriptions.Item label="command">{command.length ? <code>{command.join(' ')}</code> : '-'}</Descriptions.Item>
              <Descriptions.Item label="args">{args.length ? <code>{args.join(' ')}</code> : '-'}</Descriptions.Item>
            </Descriptions>
          ),
        } : null,
      ].filter(Boolean) as any}
    />
  );
}
