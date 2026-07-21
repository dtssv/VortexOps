import { useEffect, useRef, useState, useCallback } from 'react';
import { Input, Button, Space, Tag, Tooltip } from 'antd';
import { FixedSizeList as List } from 'react-window';
import { PauseCircleOutlined, PlayCircleOutlined, DownloadOutlined, SearchOutlined } from '@ant-design/icons';
import { downloadText } from '@/utils/action';

export interface LogLine {
  sequence: number;
  step?: string;
  timestamp?: string;
  stream?: string;
  message: string;
}

interface LogViewerProps {
  lines: LogLine[];
  height?: number;
  autoScroll?: boolean;
  searchable?: boolean;
  downloadName?: string;
}

const LEVEL_COLORS: Record<string, string> = {
  ERROR: '#ff4d4f',
  WARN: '#faad14',
  WARNING: '#faad14',
  FAIL: '#ff4d4f',
  INFO: '#1677ff',
  DEBUG: '#8c8c8c',
};

function detectLevel(line: string): string | null {
  const upper = line.toUpperCase();
  for (const lvl of ['ERROR', 'WARN', 'WARNING', 'FAIL', 'INFO', 'DEBUG']) {
    if (upper.includes(lvl)) return lvl;
  }
  return null;
}

export function LogViewer({
  lines,
  height = 400,
  autoScroll = true,
  searchable = true,
  downloadName = 'logs.txt',
}: LogViewerProps) {
  const [paused, setPaused] = useState(false);
  const [keyword, setKeyword] = useState('');
  const listRef = useRef<List>(null);
  const containerRef = useRef<HTMLDivElement>(null);

  const filtered = keyword
    ? lines.filter((l) => l.message.toLowerCase().includes(keyword.toLowerCase()))
    : lines;

  useEffect(() => {
    if (!paused && autoScroll && filtered.length > 0 && listRef.current) {
      listRef.current.scrollToItem(filtered.length - 1);
    }
  }, [filtered.length, paused, autoScroll]);

  const Row = useCallback(({ index, style }: { index: number; style: React.CSSProperties }) => {
    const line = filtered[index];
    const level = detectLevel(line.message);
    const color = level ? LEVEL_COLORS[level] : line.stream === 'stderr' ? '#ff4d4f' : '#d9d9d9';
    const time = line.timestamp ? new Date(line.timestamp).toLocaleTimeString() : '';
    return (
      <div style={{ ...style, display: 'flex', fontFamily: 'Menlo, Consolas, monospace', fontSize: 12, lineHeight: '20px' }}>
        <span style={{ color: '#595959', width: 50, flexShrink: 0, textAlign: 'right', paddingRight: 8 }}>{line.sequence}</span>
        {time && <span style={{ color: '#8c8c8c', width: 90, flexShrink: 0, paddingRight: 8 }}>{time}</span>}
        {line.step && <Tag style={{ marginRight: 8, height: 18, fontSize: 11, lineHeight: '16px' }}>{line.step}</Tag>}
        <span style={{ color, whiteSpace: 'pre-wrap', wordBreak: 'break-all' }}>{line.message}</span>
      </div>
    );
  }, [filtered]);

  return (
    <div ref={containerRef}>
      {searchable && (
        <Space style={{ marginBottom: 8, width: '100%', justifyContent: 'space-between' }}>
          <Input
            placeholder="搜索日志..."
            prefix={<SearchOutlined />}
            value={keyword}
            onChange={(e) => setKeyword(e.target.value)}
            style={{ width: 280 }}
            allowClear
          />
          <Space>
            <Tooltip title={paused ? '继续滚动' : '暂停滚动'}>
              <Button
                size="small"
                icon={paused ? <PlayCircleOutlined /> : <PauseCircleOutlined />}
                onClick={() => setPaused((p) => !p)}
              />
            </Tooltip>
            <Tooltip title="下载日志">
              <Button
                size="small"
                icon={<DownloadOutlined />}
                onClick={() => downloadText(downloadName, lines.map((l) => l.message).join('\n'))}
              />
            </Tooltip>
          </Space>
        </Space>
      )}
      <div
        style={{
          height,
          overflow: 'auto',
          background: '#141414',
          padding: '8px 12px',
          borderRadius: 4,
        }}
      >
        {filtered.length === 0 ? (
          <div style={{ color: '#8c8c8c', fontSize: 12 }}>暂无日志</div>
        ) : (
          <List
            ref={listRef}
            height={height - 16}
            itemCount={filtered.length}
            itemSize={20}
            width="100%"
          >
            {Row}
          </List>
        )}
      </div>
    </div>
  );
}
