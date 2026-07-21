import { useState, useRef } from 'react';
import { App, Button, Input, Select, Space, Tag, Typography } from 'antd';
import { PlayCircleOutlined, StopOutlined } from '@ant-design/icons';
import { useAuthStore } from '@/stores/authStore';

interface PodNetCmdProps {
  groupId: number;
  pod: string;
  containers?: Array<{ name: string }>;
  defaultContainer?: string;
}

const ALLOWED_CMDS: Array<{ cmd: string; label: string; placeholder: string; example: string }> = [
  { cmd: 'ping', label: 'ping', placeholder: '主机名或 IP，如 10.0.0.1', example: '10.0.0.1 -c 4' },
  { cmd: 'curl', label: 'curl', placeholder: 'URL，如 http://example.com', example: '-s -o /dev/null -w "%{http_code}" http://example.com' },
  { cmd: 'nslookup', label: 'nslookup', placeholder: '域名，如 example.com', example: 'example.com' },
  { cmd: 'dig', label: 'dig', placeholder: '域名，如 example.com', example: 'example.com +short' },
  { cmd: 'netstat', label: 'netstat', placeholder: '参数，如 -tlnp', example: '-tlnp' },
  { cmd: 'tracepath', label: 'tracepath', placeholder: '主机名或 IP', example: 'example.com' },
  { cmd: 'ip', label: 'ip', placeholder: '参数，如 addr', example: 'addr' },
  { cmd: 'ifconfig', label: 'ifconfig', placeholder: '参数（可空）', example: '' },
];

export function PodNetCmd({ groupId, pod, containers, defaultContainer }: PodNetCmdProps) {
  const { message } = App.useApp();
  const accessToken = useAuthStore((s) => s.accessToken);
  const [container, setContainer] = useState<string>(defaultContainer || containers?.[0]?.name || '');
  const [selectedCmd, setSelectedCmd] = useState<string>('ping');
  const [argsText, setArgsText] = useState<string>('');
  const [output, setOutput] = useState<string>('');
  const [running, setRunning] = useState(false);
  const abortRef = useRef<AbortController | null>(null);

  // 流式执行：fetch POST → 读取 ReadableStream 逐块追加到输出区。
  async function runStream() {
    setRunning(true);
    setOutput('');
    const args = argsText.trim().split(/\s+/).filter(Boolean);
    const controller = new AbortController();
    abortRef.current = controller;
    try {
      // 直接走 fetch 读取流（axios 不易逐块消费 ReadableStream）。
      const base = import.meta.env.VITE_API_BASE || '/api/v1';
      const res = await fetch(`${base}/groups/${groupId}/pods/${pod}/netcmd`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${accessToken || ''}`,
          'X-Stream': 'true',
        },
        body: JSON.stringify({ cmd: selectedCmd, args, container }),
        signal: controller.signal,
      });
      if (!res.ok || !res.body) {
        const text = await res.text().catch(() => '');
        message.error(text || `执行失败: ${res.status}`);
        return;
      }
      const reader = res.body.getReader();
      const decoder = new TextDecoder('utf-8');
      let acc = '';
      let exitCode: number | null = null;
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        acc += decoder.decode(value, { stream: true });
        setOutput(acc);
      }
      // 流结束后解析末尾的退出码标记行（由后端 NetCmdStream 输出）。
      const m = acc.match(/\n__EXIT__=(-?\d+)\s*\n?$/);
      if (m) {
        exitCode = Number(m[1]);
        acc = acc.replace(/\n__EXIT__=-?\d+\s*\n?$/, '');
        setOutput(acc);
      }
      if (exitCode !== null) {
        setOutput((prev) => prev + `\n[退出码: ${exitCode}]`);
      }
    } catch (e: any) {
      if (e?.name === 'AbortError') {
        setOutput((prev) => prev + '\n[已停止]');
      } else {
        message.error(e?.message || '执行失败');
      }
    } finally {
      setRunning(false);
      abortRef.current = null;
    }
  }

  function stopRun() {
    abortRef.current?.abort();
  }

  function applyExample() {
    const meta = ALLOWED_CMDS.find((c) => c.cmd === selectedCmd);
    if (meta) setArgsText(meta.example);
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', gap: 12 }}>
      <Space wrap size="small">
        {containers && containers.length > 0 && (
          <Select
            size="small"
            value={container}
            onChange={setContainer}
            style={{ width: 160 }}
            options={containers.map((c) => ({ label: c.name, value: c.name }))}
            placeholder="选择容器"
          />
        )}
        <Select
          size="small"
          value={selectedCmd}
          onChange={(v) => {
            setSelectedCmd(v);
            setArgsText('');
          }}
          style={{ width: 140 }}
          options={ALLOWED_CMDS.map((c) => ({ label: c.label, value: c.cmd }))}
        />
        <Input
          size="small"
          style={{ width: 320 }}
          value={argsText}
          onChange={(e) => setArgsText(e.target.value)}
          placeholder={ALLOWED_CMDS.find((c) => c.cmd === selectedCmd)?.placeholder || '参数'}
          onPressEnter={() => !running && runStream()}
        />
        <Button size="small" type="primary" icon={<PlayCircleOutlined />} loading={running} onClick={runStream}>
          运行
        </Button>
        {running && (
          <Button size="small" danger icon={<StopOutlined />} onClick={stopRun}>
            停止
          </Button>
        )}
        <Button size="small" onClick={applyExample} disabled={running}>
          示例
        </Button>
      </Space>

      <Space size="small">
        <Tag color="blue">{selectedCmd}</Tag>
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          仅允许执行白名单命令，参数以空格分隔。流式输出。
        </Typography.Text>
      </Space>

      <pre
        style={{
          background: '#0b0b0b',
          color: '#e6e6e6',
          padding: 12,
          borderRadius: 6,
          flex: 1,
          overflow: 'auto',
          fontSize: 12,
          margin: 0,
          whiteSpace: 'pre-wrap',
          wordBreak: 'break-all',
        }}
      >
        {running && !output ? '执行中...' : output || '(点击「运行」执行命令)'}
      </pre>
    </div>
  );
}

export default PodNetCmd;
