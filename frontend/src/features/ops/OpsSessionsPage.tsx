import { useEffect, useRef, useState } from 'react';
import { App, Button, Card, Modal, Select, Space, Table, Tag } from 'antd';
import { PlayCircleOutlined } from '@ant-design/icons';
import { useMutation, useQuery } from '@tanstack/react-query';
import type { ColumnsType } from 'antd/es/table';
import { create as createAsciinemaPlayer, type Player } from 'asciinema-player';
import 'asciinema-player/dist/bundle/asciinema-player.css';
import { PageContainer } from '@/components/PageContainer';
import { EmptyState } from '@/components/EmptyState';
import { opsApi, type OpsSession } from '@/api/ops';
import { useUIStore } from '@/stores/uiStore';
import { useAuthStore } from '@/stores/authStore';
import { formatTime } from '@/utils/format';

const STATUS_COLOR: Record<string, string> = {
  active: 'processing',
  closed: 'default',
};

function formatDuration(ms?: number) {
  if (!ms) return '-';
  const sec = Math.floor(ms / 1000);
  if (sec < 60) return `${sec}s`;
  const min = Math.floor(sec / 60);
  if (min < 60) return `${min}m${sec % 60}s`;
  const hr = Math.floor(min / 60);
  return `${hr}h${min % 60}m`;
}

export function OpsSessionsPage() {
  const { message } = App.useApp();
  const wsId = useUIStore((s) => s.currentWorkspaceId);
  const accessToken = useAuthStore((s) => s.accessToken);
  const [status, setStatus] = useState<string>('');
  const [replayModal, setReplayModal] = useState<{ open: boolean; sessionId?: number }>({ open: false });

  const { data, isLoading } = useQuery({
    queryKey: ['ops-sessions', wsId, status],
    queryFn: () =>
      opsApi.listSessions({
        workspace_id: wsId || undefined,
        status: status || undefined,
        page: 1,
        size: 200,
      }),
    // 不限制 wsId：平台管理员未选 workspace 时查全部。
    refetchInterval: 15000,
  });

  const columns: ColumnsType<OpsSession> = [
    { title: 'ID', dataIndex: 'id', key: 'id', width: 70 },
    { title: '集群', dataIndex: 'cluster_id', key: 'cluster_id', width: 90 },
    { title: '命名空间', dataIndex: 'namespace', key: 'namespace', width: 140 },
    { title: 'Pod', dataIndex: 'pod', key: 'pod', width: 200 },
    { title: '容器', dataIndex: 'container', key: 'container', width: 140 },
    { title: '用户', dataIndex: 'user_name', key: 'user_name', width: 120 },
    { title: '来源IP', dataIndex: 'client_ip', key: 'client_ip', width: 130 },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 90,
      render: (v: string) => <Tag color={STATUS_COLOR[v] || 'default'}>{v === 'active' ? '进行中' : '已结束'}</Tag>,
    },
    { title: '开始', dataIndex: 'started_at', key: 'started_at', width: 170, render: formatTime },
    { title: '结束', dataIndex: 'ended_at', key: 'ended_at', width: 170, render: (v?: string) => (v ? formatTime(v) : '-') },
    { title: '时长', dataIndex: 'duration_ms', key: 'duration_ms', width: 90, render: formatDuration },
    {
      title: '录像',
      key: 'recording',
      width: 90,
      render: (_: any, r: OpsSession) =>
        r.recording_key ? <Tag color="blue">有</Tag> : <Tag>无</Tag>,
    },
    {
      title: '操作',
      key: 'actions',
      width: 120,
      render: (_: any, r: OpsSession) =>
        r.status === 'closed' && r.recording_key ? (
          <Button
            size="small"
            icon={<PlayCircleOutlined />}
            onClick={() => setReplayModal({ open: true, sessionId: r.id })}
          >
            回放
          </Button>
        ) : (
          '-'
        ),
    },
  ];

  return (
    <PageContainer
      title="运维会话"
      extra={
        <Select
          allowClear
          placeholder="状态"
          style={{ width: 120 }}
          value={status || undefined}
          onChange={(v) => setStatus(v || '')}
          options={[
            { label: '进行中', value: 'active' },
            { label: '已结束', value: 'closed' },
          ]}
        />
      }
    >
      <Card>
        <Table
          rowKey="id"
          loading={isLoading}
          columns={columns}
          dataSource={data?.items || []}
          pagination={false}
          locale={{ emptyText: <EmptyState title="暂无运维会话" description="通过「Pod 终端」发起 WebSSH 会话后会在此记录，含操作录像与命令审计" /> }}
        />
      </Card>

      <ReplayModal
        open={replayModal.open}
        sessionId={replayModal.sessionId}
        accessToken={accessToken}
        onClose={() => setReplayModal({ open: false })}
      />
    </PageContainer>
  );
}

// ReplayModal 用 asciinema-player 在线播放会话录像（cast 文件由后端 /cast 端点流式代理）。
function ReplayModal({
  open,
  sessionId,
  accessToken,
  onClose,
}: {
  open: boolean;
  sessionId?: number;
  accessToken: string | null;
  onClose: () => void;
}) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const playerRef = useRef<Player | null>(null);
  const [status, setStatus] = useState<'loading' | 'ready' | 'empty' | 'error'>('loading');
  const [error, setError] = useState<string>('');

  useEffect(() => {
    if (!open || !sessionId || !accessToken) return;
    setStatus('loading');
    setError('');

    let disposed = false;

    // 先自行 fetch cast 内容，校验后再交给 player。
    // 这样能区分「加载失败」「无内容」「正常播放」三种状态，避免 asciinema-player
    // 静默吞掉 fetch/解析错误导致空白无提示。
    const base = import.meta.env.VITE_API_BASE || '/api/v1';
    const devWsTarget = import.meta.env.VITE_DEV_WS_TARGET;
    let host = '';
    if (devWsTarget) {
      const m = devWsTarget.match(/^https?:\/\/(.+)$/i);
      host = m ? m[1] : devWsTarget;
    }
    const castUrl = host
      ? `${location.protocol}//${host}${base}/ops/sessions/history/${sessionId}/cast`
      : `${base}/ops/sessions/history/${sessionId}/cast`;

    let raf = 0;
    let attempts = 0;
    const waitForContainerAndPlay = (castText: string) => {
      if (disposed) return;
      if (containerRef.current) {
        try {
          const player = createAsciinemaPlayer(
            { data: castText },
            containerRef.current,
            {
              cols: 120,
              rows: 32,
              autoPlay: true,
              controls: 'auto',
              fit: 'width',
              terminalFontFamily: 'Menlo, Consolas, "DejaVu Sans Mono", monospace',
              theme: 'monokai',
            },
          );
          playerRef.current = player;
          setStatus('ready');
        } catch (e: any) {
          setStatus('error');
          setError('播放器初始化失败：' + (e?.message || String(e)));
        }
        return;
      }
      attempts++;
      if (attempts < 30) {
        raf = requestAnimationFrame(() => waitForContainerAndPlay(castText));
      } else {
        setStatus('error');
        setError('录像容器初始化超时，请重试');
      }
    };

    (async () => {
      try {
        const resp = await fetch(castUrl, {
          headers: { Authorization: `Bearer ${accessToken}` },
        });
        if (!resp.ok) {
          setStatus('error');
          setError(`录像加载失败：HTTP ${resp.status}`);
          return;
        }
        const text = await resp.text();
        // asciicast v2：首行是 header JSON，后续每行是事件 [time, code, data]。
        // 只有 header、没有事件行 → 会话过短无输出，提示而非空白。
        const lines = text.split('\n').filter((l) => l.trim() !== '');
        const eventLines = lines.slice(1); // 去掉 header
        if (eventLines.length === 0) {
          setStatus('empty');
          return;
        }
        if (disposed) return;
        // 等容器挂载后创建 player（Modal destroyOnClose 时序）。
        raf = requestAnimationFrame(() => waitForContainerAndPlay(text));
      } catch (e: any) {
        if (disposed) return;
        setStatus('error');
        setError('录像加载失败：' + (e?.message || String(e)));
      }
    })();

    return () => {
      disposed = true;
      cancelAnimationFrame(raf);
      if (playerRef.current) {
        playerRef.current.dispose();
        playerRef.current = null;
      }
    };
  }, [open, sessionId, accessToken]);

  return (
    <Modal
      title="会话录像回放"
      open={open}
      onCancel={onClose}
      footer={null}
      width={920}
      destroyOnClose
    >
      {status === 'loading' && (
        <div style={{ background: '#1e1e1e', minHeight: 360, display: 'flex', alignItems: 'center', justifyContent: 'center', color: '#888' }}>
          录像加载中…
        </div>
      )}
      {status === 'empty' && (
        <div style={{ background: '#1e1e1e', minHeight: 360, display: 'flex', alignItems: 'center', justifyContent: 'center', color: '#faad14' }}>
          该会话时长过短，无录像内容（仅含会话头信息）
        </div>
      )}
      {status === 'error' && (
        <div style={{ background: '#1e1e1e', minHeight: 360, display: 'flex', alignItems: 'center', justifyContent: 'center', color: '#ff4d4f' }}>
          {error}
        </div>
      )}
      {/* ready 时容器由 player 挂载；其余状态隐藏容器避免占位 */}
      <div
        ref={containerRef}
        style={{
          background: '#1e1e1e',
          minHeight: 360,
          padding: 4,
          display: status === 'ready' ? 'block' : 'none',
        }}
      />
      <p style={{ marginTop: 12, color: '#888', fontSize: 12 }}>
        录像以 asciinema cast 格式存储，包含完整终端输入/输出。播放器支持播放/暂停、拖动进度、倍速。
        对应的命令审计可在「行为审计」页按会话 ID 检索。
      </p>
    </Modal>
  );
}

export default OpsSessionsPage;
