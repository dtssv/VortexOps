import { useEffect, useRef, useState, useCallback } from 'react';

type WSStatus = 'connecting' | 'open' | 'closed';

interface UseWebSocketOptions {
  onMessage?: (data: any) => void;
  autoReconnect?: boolean;
  reconnectInterval?: number;
  maxRetries?: number;
}

/**
 * Generic WebSocket hook with auto-reconnect.
 * Pass a full ws:// or wss:// URL, or a path starting with /ws.
 */
export function useWebSocket<T = any>(
  url: string | null,
  options: UseWebSocketOptions = {},
): { data: T | null; status: WSStatus; send: (msg: any) => void; close: () => void } {
  const { onMessage, autoReconnect = true, reconnectInterval = 3000, maxRetries = 10 } = options;
  const [data, setData] = useState<T | null>(null);
  const [status, setStatus] = useState<WSStatus>('closed');
  const wsRef = useRef<WebSocket | null>(null);
  const retryRef = useRef(0);
  const onMessageRef = useRef(onMessage);
  onMessageRef.current = onMessage;

  const close = useCallback(() => {
    if (wsRef.current) {
      wsRef.current.onclose = null;
      wsRef.current.close();
      wsRef.current = null;
    }
    setStatus('closed');
  }, []);

  const send = useCallback((msg: any) => {
    if (wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
      wsRef.current.send(typeof msg === 'string' ? msg : JSON.stringify(msg));
    }
  }, []);

  useEffect(() => {
    if (!url) return;
    let closed = false;
    const fullUrl = url.startsWith('ws')
      ? url
      : `${window.location.protocol === 'https:' ? 'wss:' : 'ws:'}//${window.location.host}${url}`;

    const connect = () => {
      if (closed) return;
      setStatus('connecting');
      const token = new URLSearchParams(window.location.search).get('token') || '';
      const ws = new WebSocket(token ? `${fullUrl}?token=${encodeURIComponent(token)}` : fullUrl);
      wsRef.current = ws;
      ws.onopen = () => {
        retryRef.current = 0;
        setStatus('open');
      };
      ws.onmessage = (ev) => {
        let payload: any = ev.data;
        try {
          payload = JSON.parse(ev.data);
        } catch {
          // keep raw string
        }
        setData(payload);
        onMessageRef.current?.(payload);
      };
      ws.onerror = () => {
        // errors usually precede close
      };
      ws.onclose = () => {
        setStatus('closed');
        wsRef.current = null;
        if (!closed && autoReconnect && retryRef.current < maxRetries) {
          retryRef.current += 1;
          setTimeout(connect, reconnectInterval * Math.min(retryRef.current, 5));
        }
      };
    };

    connect();
    return () => {
      closed = true;
      if (wsRef.current) {
        wsRef.current.onclose = null;
        wsRef.current.close();
        wsRef.current = null;
      }
    };
  }, [url, autoReconnect, reconnectInterval, maxRetries]);

  return { data, status, send, close };
}
