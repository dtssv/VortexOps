import { useState } from 'react';
import { App, Button, Input, Select, Space, Tabs, Tag, Tooltip } from 'antd';
import { FileSearchOutlined, ReloadOutlined, SearchOutlined } from '@ant-design/icons';
import { useQuery, useMutation } from '@tanstack/react-query';
import { LogViewer, type LogLine } from '@/components/LogViewer';
import { groupApi } from '@/api/applications';

interface PodLogPanelProps {
  groupId: number;
  pod: string;
  container?: string;
  clusterId: number;
  namespace: string;
  logLines: LogLine[];
}

const LOG_PATTERNS = [
  { label: '*.log', value: '*.log' },
  { label: '/var/log/*', value: '/var/log/*' },
  { label: '*.log (递归)', value: '**/*.log' },
  { label: '应用日志 (apps)', value: '/opt/**/*.log' },
  { label: '自定义', value: '' },
];

export function PodLogPanel({ groupId, pod, container, clusterId, namespace, logLines }: PodLogPanelProps) {
  const { message: msg } = App.useApp();
  const [activeTab, setActiveTab] = useState('container');
  const [logPattern, setLogPattern] = useState('*.log');
  const [customPattern, setCustomPattern] = useState('');
  const [selectedLogPath, setSelectedLogPath] = useState<string>('');

  // Search log paths
  const { data: logPaths, isLoading: searchingPaths } = useQuery({
    queryKey: ['pod-log-paths', groupId, pod, container, logPattern || customPattern],
    queryFn: () => groupApi.searchLogPaths(groupId, pod, {
      pattern: logPattern || customPattern,
      container,
    }),
    enabled: activeTab === 'subscribe' && !!pod,
  });

  // Read selected log file content
  const { data: logFileContent, isLoading: readingFile } = useQuery({
    queryKey: ['pod-log-file', groupId, pod, container, selectedLogPath],
    queryFn: () => groupApi.readFile(groupId, pod, { path: selectedLogPath, container, max_lines: 2000 }),
    enabled: activeTab === 'subscribe' && !!selectedLogPath,
  });

  const logFileLines: LogLine[] = (logFileContent?.content || '')
    .split('\n')
    .filter(Boolean)
    .map((line, i) => ({ sequence: i + 1, message: line }));

  const effectivePattern = logPattern || customPattern;

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
      <Tabs
        activeKey={activeTab}
        onChange={setActiveTab}
        size="small"
        items={[
          {
            key: 'container',
            label: '容器日志',
            children: (
              <div style={{ height: 'calc(100vh - 220px)' }}>
                <LogViewer lines={logLines} height={window.innerHeight - 260} downloadName={`${pod}-container.log`} />
              </div>
            ),
          },
          {
            key: 'subscribe',
            label: '订阅路径',
            children: (
              <div style={{ display: 'flex', flexDirection: 'column', gap: 12, height: 'calc(100vh - 220px)' }}>
                <Space wrap size="small">
                  <span style={{ fontSize: 13, fontWeight: 500 }}>搜索模式:</span>
                  <Select
                    size="small"
                    value={logPattern}
                    onChange={(v) => { setLogPattern(v); setSelectedLogPath(''); }}
                    style={{ width: 180 }}
                    options={LOG_PATTERNS.map((p) => ({ label: p.label, value: p.value }))}
                  />
                  {logPattern === '' && (
                    <Input
                      size="small"
                      style={{ width: 260 }}
                      placeholder="输入 glob 模式，如 **/*.log"
                      value={customPattern}
                      onChange={(e) => setCustomPattern(e.target.value)}
                      onPressEnter={() => setSelectedLogPath('')}
                    />
                  )}
                  <Tag color="blue">{effectivePattern || '*.log'}</Tag>
                  <Tooltip title="重新搜索">
                    <Button size="small" icon={<ReloadOutlined />} />
                  </Tooltip>
                </Space>

                <div style={{ display: 'flex', gap: 12, flex: 1, minHeight: 0 }}>
                  {/* Left: path list */}
                  <div style={{ width: 280, overflow: 'auto', border: '1px solid #303030', borderRadius: 6, padding: 8 }}>
                    <div style={{ fontSize: 12, color: '#8c8c8c', marginBottom: 8 }}>
                      <FileSearchOutlined /> 匹配路径 ({logPaths?.length || 0})
                    </div>
                    {searchingPaths ? (
                      <div style={{ color: '#8c8c8c', fontSize: 12 }}>搜索中...</div>
                    ) : (logPaths || []).length === 0 ? (
                      <div style={{ color: '#8c8c8c', fontSize: 12 }}>未找到匹配的日志文件</div>
                    ) : (
                      <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
                        {(logPaths || []).map((p) => (
                          <div
                            key={p}
                            onClick={() => setSelectedLogPath(p)}
                            style={{
                              padding: '4px 8px',
                              cursor: 'pointer',
                              borderRadius: 4,
                              fontSize: 12,
                              fontFamily: 'monospace',
                              background: selectedLogPath === p ? '#1677ff22' : 'transparent',
                              border: selectedLogPath === p ? '1px solid #1677ff' : '1px solid transparent',
                              color: selectedLogPath === p ? '#1677ff' : '#d9d9d9',
                              wordBreak: 'break-all',
                            }}
                          >
                            {p}
                          </div>
                        ))}
                      </div>
                    )}
                  </div>

                  {/* Right: file content */}
                  <div style={{ flex: 1, minWidth: 0 }}>
                    {selectedLogPath ? (
                      <div>
                        <div style={{ fontSize: 12, color: '#8c8c8c', marginBottom: 4 }}>
                          {selectedLogPath}
                        </div>
                        <LogViewer
                          lines={logFileLines}
                          height={window.innerHeight - 340}
                          downloadName={selectedLogPath.split('/').pop() || 'file.log'}
                        />
                      </div>
                    ) : (
                      <div style={{ color: '#8c8c8c', fontSize: 12, padding: 20, textAlign: 'center' }}>
                        点击左侧路径查看文件内容
                      </div>
                    )}
                  </div>
                </div>
              </div>
            ),
          },
        ]}
      />
    </div>
  );
}
