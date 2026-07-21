import { useEffect, useRef, useState } from 'react';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { WebLinksAddon } from '@xterm/addon-web-links';
import '@xterm/xterm/css/xterm.css';
import { App, Button, Space, Tag } from 'antd';
import { useAuthStore } from '@/stores/authStore';

interface PodTerminalProps {
  clusterId: number;
  namespace: string;
  pod: string;
  container?: string;
  command?: string;
  onClose?: () => void;
}

interface ServerMsg {
  type: 'stdout' | 'stderr' | 'exit' | 'error';
  data?: string;
  code?: number;
}

/**
 * PodTerminal 交互式 WebSSH 终端。
 * 通过 /api/v1/ops/exec/ws WebSocket 连接 apiserver，使用 xterm.js 渲染 TTY。
 * 鉴权：从 authStore 取 access_token 作为 ?token= 查询参数（浏览器 WS 无法自定义 header）。
 */
export function PodTerminal({ clusterId, namespace, pod, container, command, onClose }: PodTerminalProps) {
  const { message } = App.useApp();
  const containerRef = useRef<HTMLDivElement>(null);
  const termRef = useRef<Terminal | null>(null);
  const fitRef = useRef<FitAddon | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const [connected, setConnected] = useState(false);
  const [exited, setExited] = useState(false);
  const accessToken = useAuthStore((s) => s.accessToken);

  useEffect(() => {
    if (!containerRef.current || !accessToken) return;

    const term = new Terminal({
      cursorBlink: true,
      fontSize: 13,
      fontFamily: 'Menlo, Consolas, "DejaVu Sans Mono", monospace',
      theme: { background: '#1e1e1e', foreground: '#e6e6e6', cursor: '#ffffff' },
      scrollback: 5000,
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.loadAddon(new WebLinksAddon());
    term.open(containerRef.current);
    try { fit.fit(); } catch { /* ignore */ }
    termRef.current = term;
    fitRef.current = fit;

    const cmd = command || '/bin/sh';
    const base = import.meta.env.VITE_API_BASE || '/api/v1';
    // 开发环境 Vite 对 WebSocket 升级代理不稳定（升级请求被静默挂起），
    // 通过 VITE_DEV_WS_TARGET 让 WS 直连 apiserver；生产环境留空走同源。
    const devWsTarget = import.meta.env.VITE_DEV_WS_TARGET;
    let wsHost: string;
    let wsProto: string;
    if (devWsTarget) {
      const m = devWsTarget.match(/^(https?):\/\/(.+)$/i);
      if (m) {
        wsProto = m[1].toLowerCase() === 'https' ? 'wss:' : 'ws:';
        wsHost = m[2];
      } else {
        wsProto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        wsHost = devWsTarget;
      }
    } else {
      wsProto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
      wsHost = window.location.host;
    }
    const wsUrl = `${wsProto}//${wsHost}${base}/ops/exec/ws?` +
      `cluster_id=${clusterId}&namespace=${encodeURIComponent(namespace)}&pod=${encodeURIComponent(pod)}` +
      `&container=${encodeURIComponent(container || '')}&command=${encodeURIComponent(cmd)}` +
      `&token=${encodeURIComponent(accessToken)}`;

    const ws = new WebSocket(wsUrl);
    wsRef.current = ws;

    ws.onopen = () => {
      setConnected(true);
      term.writeln(`\x1b[32m已连接 ${namespace}/${pod}${container ? '/' + container : ''}\x1b[0m`);
      // 发送初始尺寸。
      sendResize(term.cols, term.rows);
    };

    ws.onmessage = (ev) => {
      let msg: ServerMsg;
      try {
        msg = JSON.parse(ev.data);
      } catch {
        return;
      }
      switch (msg.type) {
        case 'stdout':
        case 'stderr':
          term.write(msg.data || '');
          break;
        case 'exit':
          setExited(true);
          term.writeln(`\x1b[33m\r\n[进程退出，代码 ${msg.code ?? 0}]\x1b[0m`);
          break;
        case 'error':
          term.writeln(`\x1b[31m${msg.data}\x1b[0m`);
          break;
      }
    };

    ws.onerror = () => {
      term.writeln('\x1b[31m[连接错误]\x1b[0m');
    };

    ws.onclose = () => {
      setConnected(false);
      if (!exited) {
        term.writeln('\x1b[33m[连接已关闭]\x1b[0m');
      }
    };

    // 终端输入 → stdin 帧。
    const disposable = term.onData((data) => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: 'stdin', data }));
      }
    });

    // 终端尺寸变化 → resize 帧。
    const resizeDisposable = term.onResize(({ cols, rows }) => sendResize(cols, rows));
    const resizeObserver = new ResizeObserver(() => {
      try { fit.fit(); } catch { /* ignore */ }
    });
    resizeObserver.observe(containerRef.current);

    function sendResize(cols: number, rows: number) {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: 'resize', cols, rows }));
      }
    }

    return () => {
      disposable.dispose();
      resizeDisposable.dispose();
      resizeObserver.disconnect();
      ws.onclose = null;
      ws.close();
      term.dispose();
      termRef.current = null;
      fitRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [clusterId, namespace, pod, container, command, accessToken]);

  const handleReconnect = () => {
    setExited(false);
    // 触发重连：重置状态后由 effect 重新建立。
    if (wsRef.current) {
      wsRef.current.close();
    }
    // 简单做法：reload 组件。
    setConnected(false);
  };

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
      <Space style={{ marginBottom: 8, flexShrink: 0 }}>
        <Tag color={connected ? 'success' : 'default'}>{connected ? '已连接' : exited ? '已退出' : '连接中'}</Tag>
        <span style={{ color: '#888', fontSize: 12 }}>{namespace}/{pod}{container ? `/${container}` : ''}</span>
        <Button size="small" onClick={handleReconnect} disabled={connected}>重连</Button>
        {onClose && <Button size="small" onClick={onClose}>关闭终端</Button>}
      </Space>
      <div
        ref={containerRef}
        style={{ flex: 1, background: '#1e1e1e', padding: 4, overflow: 'hidden', minHeight: 360 }}
      />
    </div>
  );
}

export default PodTerminal;
