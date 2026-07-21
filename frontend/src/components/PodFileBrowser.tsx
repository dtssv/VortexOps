import { useState, useEffect } from 'react';
import { App, Breadcrumb, Button, Dropdown, Input, Modal, Popconfirm, Select, Space, Table, Tag, Tooltip, Upload, message } from 'antd';
import {
  ArrowLeftOutlined,
  DeleteOutlined,
  DownloadOutlined,
  EyeOutlined,
  FileOutlined,
  FolderOutlined,
  HomeOutlined,
  InboxOutlined,
  ReloadOutlined,
  ThunderboltOutlined,
  UploadOutlined,
} from '@ant-design/icons';
import type { ColumnsType } from 'antd/es/table';
import type { UploadProps } from 'antd';
import type { PodFileEntry } from '@/types';
import { groupApi } from '@/api/applications';

interface PodFileBrowserProps {
  groupId: number;
  pod: string;
  containers?: Array<{ name: string }>;
  defaultContainer?: string;
}

const PRESETS: Array<{ key: 'tmp' | 'logs' | 'cache'; label: string; desc: string }> = [
  { key: 'tmp', label: '清理 /tmp', desc: '删除 /tmp 下所有文件' },
  { key: 'logs', label: '清理日志', desc: '清空容器内常见日志路径' },
  { key: 'cache', label: '清理缓存', desc: '清理应用缓存目录' },
];

const TEXT_EXTENSIONS = new Set([
  // 纯文本/配置
  'log', 'txt', 'xml', 'properties', 'yml', 'yaml', 'json', 'conf', 'cfg', 'ini',
  'env', 'toml', 'md', 'csv', 'tsv',
  // 脚本
  'sh', 'bash', 'zsh', 'ksh', 'bat', 'ps1', 'py', 'js', 'ts', 'jsx', 'tsx',
  'go', 'java', 'c', 'cpp', 'cc', 'h', 'hpp', 'rs', 'rb', 'php', 'sql',
  // web
  'html', 'htm', 'css', 'scss', 'less', 'svg',
  // 构建/包描述
  'gradle', 'gitignore', 'dockerignore', 'editorconfig', 'lock',
]);

// 无后缀但确定是纯文本的常见文件名（大小写不敏感）。
// 严格白名单：避免把二进制可执行文件、coredump 等无后缀文件误判为可查看。
const TEXT_FILENAMES_NO_EXT = new Set([
  'dockerfile', 'makefile', 'readme', 'license', 'licence', 'changelog',
  'authors', 'contributors', 'notice', 'todo', 'gemfile', 'rakefile',
  'procfile', 'hosts', 'hostname', 'resolv.conf', 'fstab', 'passwd', 'group',
  'shadow', 'issue', 'os-release', 'lsb-release', 'profile', 'bashrc',
  'bash_profile', 'vimrc', 'gitconfig', 'npmrc', 'yarnrc',
]);

function joinPath(base: string, name: string): string {
  if (base === '/') return `/${name}`;
  return `${base}/${name}`;
}

function parentOf(path: string): string {
  if (path === '/' || !path) return '/';
  const idx = path.lastIndexOf('/');
  if (idx <= 0) return '/';
  return path.slice(0, idx);
}

function humanSize(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  if (n < 1024 * 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MB`;
  return `${(n / 1024 / 1024 / 1024).toFixed(2)} GB`;
}

function isTextFile(name: string): boolean {
  const lower = name.toLowerCase();
  // 1) 有后缀：严格按白名单后缀判断。
  const dotIdx = lower.lastIndexOf('.');
  if (dotIdx > 0) {
    const ext = lower.slice(dotIdx + 1);
    if (TEXT_EXTENSIONS.has(ext)) return true;
  }
  // 2) 无后缀：仅常见纯文本文件名白名单才允许查看，其他一律不给查看按钮
  //    （避免二进制可执行文件、coredump、nohup.out 等被误判为可查看）。
  if (!lower.includes('.')) {
    return TEXT_FILENAMES_NO_EXT.has(lower);
  }
  return false;
}

export function PodFileBrowser({ groupId, pod, containers, defaultContainer }: PodFileBrowserProps) {
  const { message: msg } = App.useApp();
  const [container, setContainer] = useState<string>(defaultContainer || containers?.[0]?.name || '');
  const [path, setPath] = useState<string>('/');
  const [history, setHistory] = useState<string[]>(['/']);

  const [list, setList] = useState<PodFileEntry[]>([]);
  const [loading, setLoading] = useState(false);
  const [uploadOpen, setUploadOpen] = useState(false);
  const [cleanupOpen, setCleanupOpen] = useState(false);

  // File viewer state
  const [viewFile, setViewFile] = useState<{ name: string; path: string; content: string } | null>(null);
  const [viewLoading, setViewLoading] = useState(false);

  // 组件挂载或容器就绪时自动展开根目录，无需用户点击刷新。
  useEffect(() => {
    if (container) {
      loadPath('/');
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [container]);

  async function loadPath(target: string) {
    if (!container) {
      msg.warning('请先选择容器');
      return;
    }
    setLoading(true);
    try {
      const items = await groupApi.listFiles(groupId, pod, { path: target, container });
      setList(items || []);
      setPath(target);
    } catch (e: any) {
      msg.error(e?.message || '读取目录失败');
    } finally {
      setLoading(false);
    }
  }

  function navigateTo(target: string) {
    if (target === path) return;
    setHistory((h) => [...h, target]);
    loadPath(target);
  }

  function goBack() {
    if (history.length <= 1) return;
    const next = history[history.length - 2];
    setHistory((h) => h.slice(0, -1));
    loadPath(next);
  }

  async function handleView(entry: PodFileEntry) {
    const fullPath = joinPath(path, entry.name);
    setViewLoading(true);
    try {
      const result = await groupApi.readFile(groupId, pod, { path: fullPath, container, max_lines: 2000 });
      setViewFile({ name: entry.name, path: fullPath, content: result.content });
    } catch (e: any) {
      msg.error(e?.message || '读取文件失败');
    } finally {
      setViewLoading(false);
    }
  }

  async function handleDownload(entry: PodFileEntry) {
    const fullPath = joinPath(path, entry.name);
    try {
      const url = groupApi.downloadFileUrl(groupId, pod, { path: fullPath, container });
      const token = localStorage.getItem('access_token') || '';
      const res = await fetch(url, { headers: { Authorization: `Bearer ${token}` } });
      if (!res.ok) throw new Error(`下载失败: ${res.status}`);
      const blob = await res.blob();
      const objUrl = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = objUrl;
      a.download = entry.name;
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(objUrl);
      msg.success(`已下载 ${entry.name}`);
    } catch (e: any) {
      msg.error(e?.message || '下载失败');
    }
  }

  async function handleDelete(entry: PodFileEntry) {
    const fullPath = joinPath(path, entry.name);
    try {
      await groupApi.deleteFile(groupId, pod, { path: fullPath, container });
      msg.success(`已删除 ${entry.name}`);
      loadPath(path);
    } catch (e: any) {
      msg.error(e?.message || '删除失败');
    }
  }

  async function handleCleanup(preset: 'tmp' | 'logs' | 'cache') {
    try {
      await groupApi.cleanupFiles(groupId, pod, { preset, container });
      msg.success(`清理完成: ${preset}`);
      setCleanupOpen(false);
      loadPath(path);
    } catch (e: any) {
      msg.error(e?.message || '清理失败');
    }
  }

  const uploadProps: UploadProps = {
    multiple: false,
    showUploadList: true,
    beforeUpload: (file) => {
      (async () => {
        try {
          await groupApi.uploadFile(groupId, pod, file, { path, container });
          msg.success(`已上传 ${file.name}`);
          setUploadOpen(false);
          loadPath(path);
        } catch (e: any) {
          msg.error(e?.message || '上传失败');
        }
      })();
      return false;
    },
  };

  const columns: ColumnsType<PodFileEntry> = [
    {
      title: '名称',
      dataIndex: 'name',
      render: (name: string, r) => (
        <Space>
          {r.is_dir ? <FolderOutlined style={{ color: '#faad14' }} /> : <FileOutlined />}
          {r.is_dir ? (
            <Button type="link" size="small" style={{ padding: 0 }} onClick={() => navigateTo(joinPath(path, name))}>
              {name}
            </Button>
          ) : (
            <span>{name}</span>
          )}
        </Space>
      ),
    },
    { title: '大小', dataIndex: 'size', width: 110, render: (s: number, r) => (r.is_dir ? '-' : humanSize(s)) },
    { title: '权限', dataIndex: 'mode', width: 100, render: (m: string) => <Tag>{m}</Tag> },
    {
      title: '修改时间',
      dataIndex: 'mod_time',
      width: 180,
      render: (t: string) => {
        if (!t) return '-';
        const num = Number(t);
        if (!isNaN(num) && num > 1e9 && num < 2e10) return new Date(num * 1000).toLocaleString();
        return t;
      },
    },
    {
      title: '操作',
      key: 'actions',
      width: 200,
      render: (_, r) => (
        <Space size="small">
          {!r.is_dir && isTextFile(r.name) && (
            <Tooltip title="查看">
              <Button type="link" size="small" icon={<EyeOutlined />} loading={viewLoading} onClick={() => handleView(r)} />
            </Tooltip>
          )}
          {!r.is_dir && (
            <Tooltip title="下载">
              <Button type="link" size="small" icon={<DownloadOutlined />} onClick={() => handleDownload(r)} />
            </Tooltip>
          )}
          <Popconfirm
            title="确定删除？"
            description={joinPath(path, r.name)}
            okText="删除"
            okButtonProps={{ danger: true }}
            cancelText="取消"
            onConfirm={() => handleDelete(r)}
          >
            <Tooltip title="删除">
              <Button type="link" size="small" danger icon={<DeleteOutlined />} />
            </Tooltip>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  const breadcrumbItems = path
    .split('/')
    .filter(Boolean)
    .reduce<
      Array<{ title: React.ReactNode; onClick?: () => void }>
    >((acc, part, i, arr) => {
      const fullPath = '/' + arr.slice(0, i + 1).join('/');
      acc.push({
        title: (
          <Button type="link" size="small" style={{ padding: 0 }} onClick={() => navigateTo(fullPath)}>
            {part}
          </Button>
        ),
      });
      return acc;
    }, [{ title: <Button type="link" size="small" style={{ padding: 0 }} onClick={() => navigateTo('/')}><HomeOutlined /></Button> }]);

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
      <Space style={{ marginBottom: 8, flexWrap: 'wrap' }} size="small">
        {containers && containers.length > 0 && (
          <Select
            size="small"
            value={container}
            onChange={(v) => {
              setContainer(v);
              setPath('/');
              setHistory(['/']);
              loadPath('/');
            }}
            style={{ width: 160 }}
            options={containers.map((c) => ({ label: c.name, value: c.name }))}
            placeholder="选择容器"
          />
        )}
        <Button size="small" icon={<ArrowLeftOutlined />} disabled={history.length <= 1} onClick={goBack}>
          上级
        </Button>
        <Button size="small" icon={<ReloadOutlined />} loading={loading} onClick={() => loadPath(path)}>
          刷新
        </Button>
        <Button size="small" icon={<UploadOutlined />} onClick={() => setUploadOpen(true)}>
          上传
        </Button>
        <Dropdown.Button
          size="small"
          icon={<ThunderboltOutlined />}
          onClick={() => setCleanupOpen(true)}
          menu={{
            items: PRESETS.map((p) => ({
              key: p.key,
              label: p.label,
            })),
            onClick: ({ key }) => handleCleanup(key as 'tmp' | 'logs' | 'cache'),
          }}
        >
          清理预设
        </Dropdown.Button>
      </Space>

      <Breadcrumb items={breadcrumbItems} style={{ marginBottom: 8 }} />

      <Table<PodFileEntry>
        rowKey={(r) => `${r.is_dir ? 'd' : 'f'}-${r.name}`}
        size="small"
        loading={loading}
        columns={columns}
        dataSource={list}
        pagination={false}
        scroll={{ y: 320 }}
        style={{ flex: 1 }}
      />

      <Modal
        title={viewFile ? `查看: ${viewFile.name}` : '查看文件'}
        open={!!viewFile}
        onCancel={() => setViewFile(null)}
        footer={[
          <Button key="close" onClick={() => setViewFile(null)}>关闭</Button>,
          <Button key="download" icon={<DownloadOutlined />} onClick={() => {
            if (viewFile) {
              const blob = new Blob([viewFile.content], { type: 'text/plain' });
              const url = URL.createObjectURL(blob);
              const a = document.createElement('a');
              a.href = url;
              a.download = viewFile.name;
              document.body.appendChild(a);
              a.click();
              a.remove();
              URL.revokeObjectURL(url);
            }
          }}>
            下载
          </Button>,
        ]}
        width={800}
        styles={{ body: { maxHeight: '60vh', overflow: 'auto' } }}
      >
        <div style={{ marginBottom: 8, color: '#888', fontSize: 12 }}>{viewFile?.path}</div>
        <pre style={{
          background: '#1e1e1e', color: '#d4d4d4', padding: 12, borderRadius: 6,
          fontSize: 12, lineHeight: 1.5, whiteSpace: 'pre-wrap', wordBreak: 'break-all',
          maxHeight: '50vh', overflow: 'auto', margin: 0,
        }}>
          {viewFile?.content || '(空文件)'}
        </pre>
      </Modal>

      <Modal title="上传文件" open={uploadOpen} onCancel={() => setUploadOpen(false)} footer={null} width={480}>
        <Upload.Dragger {...uploadProps} accept="*">
          <p className="ant-upload-drag-icon">
            <InboxOutlined />
          </p>
          <p className="ant-upload-text">点击或拖拽文件到此处上传</p>
          <p className="ant-upload-hint">上传到: {path}</p>
        </Upload.Dragger>
      </Modal>

      <Modal title="清理预设" open={cleanupOpen} onCancel={() => setCleanupOpen(false)} footer={null} width={480}>
        <Space direction="vertical" style={{ width: '100%' }} size="middle">
          {PRESETS.map((p) => (
            <Button
              key={p.key}
              block
              icon={<ThunderboltOutlined />}
              onClick={() => handleCleanup(p.key)}
              style={{ textAlign: 'left' }}
            >
              <span style={{ fontWeight: 600 }}>{p.label}</span>
              <span style={{ color: '#888', marginLeft: 8, fontSize: 12 }}>{p.desc}</span>
            </Button>
          ))}
        </Space>
      </Modal>
    </div>
  );
}

export default PodFileBrowser;
