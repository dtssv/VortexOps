import { useEffect, useRef, useState } from 'react';
import { App, Button, Drawer, Space, Spin, Tag, Typography } from 'antd';
import { RobotOutlined, ThunderboltOutlined } from '@ant-design/icons';
import { diagnosisApi, type DiagnosisResult, type LogAnalyzeInput } from '@/api/diagnosis';
import { DiagnosisResultCard } from './DiagnosisResultCard';

const { Text, Paragraph } = Typography;

interface Props {
  open: boolean;
  onClose: () => void;
  input: LogAnalyzeInput | null;
}

/**
 * DiagnosisDrawer 基于日志的 AI 诊断抽屉。
 * 调用方（构建详情/分组详情）在失败时收集好日志后传入 LogAnalyzeInput，
 * 抽屉内部调用 diagnosisApi.analyzeLogsStream 流式展示结果。
 */
export function DiagnosisDrawer({ open, onClose, input }: Props) {
  const { message } = App.useApp();
  // streaming：流式过程中已收到的文本（逐字拼接）。
  const [streaming, setStreaming] = useState<string>('');
  // result：流式结束后后端返回的完整结果（含分段 summary/suggestions）。
  const [result, setResult] = useState<DiagnosisResult | null>(null);
  const [loading, setLoading] = useState(false);
  // 取消信号：抽屉关闭/重新诊断时中止进行中的流式请求。
  const abortRef = useRef<AbortController | null>(null);

  // 抽屉关闭或 input 变化时中止进行中的流式请求并清空状态。
  useEffect(() => {
    return () => {
      abortRef.current?.abort();
    };
  }, []);

  const handleAnalyze = async () => {
    if (!input) return;
    // 中止上一次请求（若有）。
    abortRef.current?.abort();
    const ctrl = new AbortController();
    abortRef.current = ctrl;
    setStreaming('');
    setResult(null);
    setLoading(true);
    try {
      await diagnosisApi.analyzeLogsStream(
        input,
        {
          onDelta: (delta) => {
            setStreaming((prev) => prev + delta);
          },
          onDone: (res) => {
            setResult(res);
            message.success(`诊断完成（${res.provider}/${res.model}，${res.latency_ms}ms）`);
          },
          onError: (err) => {
            message.error(err?.message || '诊断失败');
          },
        },
        ctrl.signal,
      );
    } catch (e: any) {
      if (e?.name !== 'AbortError') {
        message.error(e?.message || '诊断失败');
      }
    } finally {
      setLoading(false);
      abortRef.current = null;
    }
  };

  const handleClose = () => {
    abortRef.current?.abort();
    abortRef.current = null;
    setLoading(false);
    setStreaming('');
    setResult(null);
    onClose();
  };

  return (
    <Drawer
      title={
        <Space>
          <RobotOutlined />
          AI 诊断
          {input?.title && <Text type="secondary" style={{ fontSize: 13 }}>{input.title}</Text>}
        </Space>
      }
      open={open}
      onClose={handleClose}
      width={760}
      destroyOnHidden
      extra={
        <Button
          type="primary"
          icon={<ThunderboltOutlined />}
          loading={loading}
          disabled={!input}
          onClick={handleAnalyze}
        >
          开始诊断
        </Button>
      }
    >
      {input && (
        <div style={{ marginBottom: 16, padding: 12, background: '#fafafa', borderRadius: 6, fontSize: 13 }}>
          <div><Text type="secondary">场景：</Text>{sceneLabel(input.source)}</div>
          {input.error_reason && (
            <div style={{ marginTop: 4 }}>
              <Text type="secondary">已知错误：</Text>
              <Text type="danger">{input.error_reason}</Text>
            </div>
          )}
          {input.logs && (
            <div style={{ marginTop: 4 }}>
              <Text type="secondary">已收集日志：</Text>
              {input.logs.length.toLocaleString()} 字符
            </div>
          )}
        </div>
      )}

      {/* 流式输出中：逐字渲染已收到文本 */}
      {loading && streaming && (
        <div style={{ marginTop: 16 }}>
          <Space size={6} style={{ marginBottom: 8 }}>
            <Tag color="processing">流式输出中</Tag>
            <Spin size="small" />
          </Space>
          <Paragraph style={{ whiteSpace: 'pre-wrap', background: '#fafafa', padding: 12, borderRadius: 6, marginBottom: 0 }}>
            {streaming}
            <span style={{ display: 'inline-block', width: 8, color: '#1677ff' }}>▍</span>
          </Paragraph>
        </div>
      )}

      {/* 流式中但尚未收到首个 delta：展示 Spin */}
      {loading && !streaming && (
        <div style={{ padding: '40px 0', textAlign: 'center' }}>
          <Spin tip="正在调用 AI 诊断..." size="large">
            <div style={{ height: 120 }} />
          </Spin>
        </div>
      )}

      {/* 流式结束：展示完整结果卡片（含分段 summary/suggestions） */}
      {result && <DiagnosisResultCard result={result} title="诊断结果" />}
    </Drawer>
  );
}

function sceneLabel(source: string): string {
  switch (source) {
    case 'build': return '镜像构建失败';
    case 'pod_startup': return '应用启动失败';
    case 'pod_crash': return 'Pod 崩溃/重启';
    default: return source;
  }
}
