import { Card, Collapse, Space, Tag, Typography } from 'antd';
import { RobotOutlined } from '@ant-design/icons';
import type { DiagnosisResult } from '@/api/diagnosis';

const { Paragraph, Text } = Typography;

interface Props {
  result: DiagnosisResult;
  title?: string;
  extra?: React.ReactNode;
}

/**
 * DiagnosisResultCard 诊断结果展示卡片。
 * 上下文诊断（K8s 资源）与日志诊断（构建/启动失败）共用此组件。
 */
export function DiagnosisResultCard({ result, title, extra }: Props) {
  return (
    <Card
      style={{ marginTop: 16 }}
      title={
        <Space>
          <RobotOutlined />
          {title || '诊断结果'}
        </Space>
      }
      extra={extra}
    >
      <Space style={{ marginBottom: 12 }} wrap>
        <Tag color="blue">{result.provider}</Tag>
        <Tag>{result.model}</Tag>
        <Tag color="default">{result.latency_ms}ms</Tag>
        {result.namespace && result.name && (
          <Text type="secondary">{result.namespace}/{result.name}</Text>
        )}
      </Space>
      <Paragraph>
        <Text strong>根因分析：</Text>
      </Paragraph>
      <Paragraph style={{ whiteSpace: 'pre-wrap', background: '#fafafa', padding: 12, borderRadius: 6 }}>
        {result.summary}
      </Paragraph>
      {result.suggestions && (
        <Paragraph>
          <Text strong>修复建议：</Text>
        </Paragraph>
      )}
      {result.suggestions && (
        <div
          style={{ background: '#f6ffed', padding: 12, borderRadius: 6 }}
          dangerouslySetInnerHTML={{ __html: renderMarkdown(result.suggestions) }}
        />
      )}
      <Collapse
        style={{ marginTop: 16 }}
        items={[
          {
            key: 'context',
            label: '收集到的上下文（原始）',
            children: (
              <pre style={{ fontSize: 12, maxHeight: 400, overflow: 'auto', whiteSpace: 'pre-wrap' }}>
                {result.raw_context}
              </pre>
            ),
          },
        ]}
      />
    </Card>
  );
}

// 极简 markdown 渲染（标题/列表/代码块），避免引入额外依赖。
export function renderMarkdown(md: string): string {
  let html = md
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;');
  html = html.replace(/```([\s\S]*?)```/g, '<pre style="background:#1e1e1e;color:#eee;padding:8px;border-radius:4px;overflow:auto">$1</pre>');
  html = html.replace(/^### (.+)$/gm, '<h4>$1</h4>');
  html = html.replace(/^## (.+)$/gm, '<h3>$1</h3>');
  html = html.replace(/^- (.+)$/gm, '<li>$1</li>');
  html = html.replace(/(<li>[\s\S]*?<\/li>)/g, '<ul>$1</ul>');
  html = html.replace(/`([^`]+)`/g, '<code>$1</code>');
  return html;
}
