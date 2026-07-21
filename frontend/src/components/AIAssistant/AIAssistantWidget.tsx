import { useEffect, useMemo, useRef, useState } from 'react';
import {
  App,
  Button,
  Collapse,
  Drawer,
  Input,
  Space,
  Spin,
  Tag,
  Tooltip,
  Typography,
} from 'antd';
import {
  RobotOutlined,
  CloseOutlined,
  SendOutlined,
  ClearOutlined,
  BulbOutlined,
  LinkOutlined,
  ToolOutlined,
  ReloadOutlined,
  AimOutlined,
  UserOutlined,
  HistoryOutlined,
  PlusOutlined,
} from '@ant-design/icons';
import { useQuery } from '@tanstack/react-query';
import {
  diagnosisApi,
  chatSessionApi,
  profileApi,
  type ChatMessage,
  type FAQItem,
  type Intent,
  type ChatSession,
} from '@/api/diagnosis';
import { renderMarkdown } from '@/components/DiagnosisResultCard';

const { Text, Paragraph } = Typography;

const STORAGE_KEY = 'vortexops.ai-assistant.history';

interface StoredMessage extends ChatMessage {
  id: string;
  ts: number;
  references?: { title: string; snippet: string; url?: string }[];
  tools?: { name: string; arguments: string; result: string }[];
  intent?: Intent;
  pending?: boolean;
}

/**
 * AIAssistantWidget 全局 AI 助手浮窗。
 *
 * 核心能力（用户无需手动选择场景，由 AI 自动识别意图）：
 * 1. 右下角浮动按钮，点击展开右侧抽屉对话窗口
 * 2. 意图识别：AI 自动从用户问题中识别类别（构建失败/Pod 失败/发布问题/K8s 运维/通用问答）
 * 3. 工具调用：根据识别的意图自动调用平台工具（获取构建日志、Pod 日志、事件等）收集上下文
 * 4. RAG 知识库：向量检索知识库增强回答，附带引用来源
 * 5. 用户画像：多轮对话中持续学习用户特征，个性化回答（如「资深 Java 工程师」）
 * 6. 对话会话持久化：跨会话保留历史、上下文摘要、实体记忆
 * 7. 透明展示：用户可看到 AI 的推理过程、调用的工具与结果
 * 8. 快捷问题区：常见问题点击直接提问
 */
export function AIAssistantWidget() {
  const { message } = App.useApp();
  const [open, setOpen] = useState(false);
  const [input, setInput] = useState('');
  const [history, setHistory] = useState<StoredMessage[]>(() => loadHistory());
  const [sessionId, setSessionId] = useState<number | undefined>(undefined);
  const [showSessions, setShowSessions] = useState(false);
  const listRef = useRef<HTMLDivElement>(null);

  // FAQ 列表（通用）。
  const { data: faqItems, refetch: refetchFAQ } = useQuery({
    queryKey: ['ai-faq'],
    queryFn: () => diagnosisApi.listFAQ(),
    staleTime: 5 * 60_000,
  });

  // 用户画像：用于在标题展示个性化标签。
  const { data: profile } = useQuery({
    queryKey: ['ai-profile'],
    queryFn: () => profileApi.get(),
    staleTime: 60_000,
    enabled: open,
  });

  // 会话列表。
  const { data: sessions, refetch: refetchSessions } = useQuery({
    queryKey: ['ai-sessions'],
    queryFn: () => chatSessionApi.listSessions(20),
    staleTime: 30_000,
    enabled: open && showSessions,
  });

  // 持久化对话历史。
  useEffect(() => {
    saveHistory(history);
  }, [history]);

  // 自动滚动到底部。
  useEffect(() => {
    if (listRef.current) {
      listRef.current.scrollTop = listRef.current.scrollHeight;
    }
  }, [history, open]);

  const [streaming, setStreaming] = useState(false);
  const abortRef = useRef<AbortController | null>(null);

  // 用流式接口发送对话。SSE 事件：
  //   meta  → 先把意图/工具/引用填到 pending 占位上
  //   delta → 增量追加到 pending 占位的 content
  //   done  → 标记完成，落盘 session_id
  //   error → 显示错误
  const sendStream = async (msgs: ChatMessage[], sid?: number) => {
    setStreaming(true);
    const ctrl = new AbortController();
    abortRef.current = ctrl;

    const updatePending = (updater: (m: StoredMessage) => StoredMessage) => {
      setHistory((prev) => {
        const next = [...prev];
        for (let i = next.length - 1; i >= 0; i--) {
          if (next[i].pending) {
            next[i] = updater(next[i]);
            break;
          }
        }
        return next;
      });
    };

    try {
      await diagnosisApi.chatStream(
        { messages: msgs, session_id: sid },
        {
          onMeta: (meta) => {
            updatePending((m) => ({
              ...m,
              role: 'assistant',
              pending: true,
              references: meta.references,
              tools: meta.tools,
              intent: meta.intent,
            }));
            if (meta.session_id && !sid) {
              setSessionId(meta.session_id);
            }
          },
          onDelta: (delta) => {
            updatePending((m) => ({
              ...m,
              role: 'assistant',
              content: m.content + delta,
              pending: true,
            }));
          },
          onDone: (result) => {
            updatePending((m) => ({
              ...m,
              role: 'assistant',
              content: result.answer || m.content,
              pending: false,
              references: result.references,
              tools: result.tools,
              intent: result.intent,
            }));
            if (result.session_id && !sid) {
              setSessionId(result.session_id);
            }
          },
          onError: (err) => {
            updatePending((m) => ({
              ...m,
              role: 'assistant',
              content: m.content || `⚠️ 调用失败：${err.message}`,
              pending: false,
            }));
            message.error(err.message || 'AI 助手调用失败');
          },
        },
        ctrl.signal,
      );
    } catch (e: any) {
      updatePending((m) => ({
        ...m,
        role: 'assistant',
        content: m.content || `⚠️ 调用失败：${e?.message || '未知错误'}`,
        pending: false,
      }));
      message.error(e?.message || 'AI 助手调用失败');
    } finally {
      setStreaming(false);
      abortRef.current = null;
    }
  };

  const send = (text: string) => {
    const trimmed = text.trim();
    if (!trimmed || streaming) return;
    const userMsg: StoredMessage = {
      id: `m-${Date.now()}`,
      ts: Date.now(),
      role: 'user',
      content: trimmed,
    };
    const pendingMsg: StoredMessage = {
      id: `m-${Date.now() + 1}`,
      ts: Date.now() + 1,
      role: 'assistant',
      content: '',
      pending: true,
    };
    // 构造发送给后端的消息（不含 pending 占位，含历史真实消息）。
    const apiMessages: ChatMessage[] = [
      ...history.filter((m) => !m.pending).map((m) => ({ role: m.role, content: m.content })),
      { role: 'user' as const, content: trimmed },
    ];
    setHistory((prev) => [...prev, userMsg, pendingMsg]);
    setInput('');
    sendStream(apiMessages, sessionId);
  };

  const clearHistory = () => {
    setHistory([]);
    setSessionId(undefined);
    message.success('已开启新对话');
  };

  // 切换到历史会话：加载该会话的消息。
  const switchSession = async (sess: ChatSession) => {
    try {
      const msgs = await chatSessionApi.listMessages(sess.id, 100);
      setHistory(
        msgs.map((m) => ({
          id: `m-${m.id}`,
          ts: new Date(m.created_at).getTime(),
          role: m.role as 'user' | 'assistant',
          content: m.content,
          intent: m.intent,
          tools: m.tools,
          references: m.references,
        })),
      );
      setSessionId(sess.id);
      setShowSessions(false);
    } catch (e: any) {
      message.error(e?.message || '加载会话失败');
    }
  };

  const quickQuestions = useMemo(() => (faqItems || []).slice(0, 6), [faqItems]);

  return (
    <>
      {/* 浮动按钮：固定在右下角。 */}
      <Tooltip title="AI 助手" placement="left">
        <Button
          type="primary"
          shape="circle"
          size="large"
          icon={<RobotOutlined />}
          onClick={() => setOpen(true)}
          style={{
            position: 'fixed',
            right: 24,
            bottom: 24,
            width: 52,
            height: 52,
            zIndex: 1000,
            boxShadow: '0 6px 16px rgba(0,0,0,0.18)',
          }}
        />
      </Tooltip>

      <Drawer
        title={
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
            <Space>
              <RobotOutlined style={{ color: '#1677ff' }} />
              <span>AI 助手</span>
              <Tag color="blue" style={{ fontSize: 11 }}>意图识别</Tag>
              {profile && profile.expertise_level && profile.expertise_level !== 'unknown' && (
                <Tooltip title={profile.summary || '基于对话推断的用户画像'}>
                  <Tag color="cyan" style={{ fontSize: 11 }} icon={<UserOutlined />}>
                    {profileLabel(profile.expertise_level)}
                    {profile.roles && profile.roles.length > 0 ? ` · ${profile.roles[0]}` : ''}
                  </Tag>
                </Tooltip>
              )}
            </Space>
            <Space size={4}>
              <Tooltip title="历史会话">
                <Button
                  type="text"
                  size="small"
                  icon={<HistoryOutlined />}
                  onClick={() => { setShowSessions((v) => !v); refetchSessions(); }}
                />
              </Tooltip>
              <Tooltip title="刷新常见问题">
                <Button type="text" size="small" icon={<ReloadOutlined />} onClick={() => refetchFAQ()} />
              </Tooltip>
              <Tooltip title="新对话">
                <Button type="text" size="small" icon={<PlusOutlined />} onClick={clearHistory} disabled={history.length === 0} />
              </Tooltip>
              <Button type="text" size="small" icon={<CloseOutlined />} onClick={() => setOpen(false)} />
            </Space>
          </div>
        }
        open={open}
        onClose={() => setOpen(false)}
        width={460}
        styles={{ body: { padding: 0, display: 'flex', flexDirection: 'column', height: '100%' } }}
        closable={false}
      >
        {/* 历史会话面板 */}
        {showSessions && (
          <div style={{ padding: '8px 12px', borderBottom: '1px solid #f0f0f0', background: '#fafafa', maxHeight: 240, overflowY: 'auto' }}>
            <Text type="secondary" style={{ fontSize: 12 }}>
              <HistoryOutlined /> 历史会话（点击恢复）
            </Text>
            <div style={{ marginTop: 6 }}>
              {(sessions || []).length === 0 ? (
                <Text type="secondary" style={{ fontSize: 12 }}>暂无历史会话</Text>
              ) : (
                (sessions || []).map((s) => (
                  <div
                    key={s.id}
                    onClick={() => switchSession(s)}
                    style={{
                      padding: '6px 8px',
                      background: '#fff',
                      border: '1px solid #f0f0f0',
                      borderRadius: 4,
                      marginBottom: 4,
                      cursor: 'pointer',
                      fontSize: 12,
                    }}
                  >
                    <Text strong style={{ fontSize: 12 }}>{s.title || '未命名对话'}</Text>
                    <div style={{ color: '#8c8c8c', fontSize: 11 }}>
                      {s.message_count} 条消息 · {new Date(s.last_active_at).toLocaleString()}
                      {s.scene && <Tag style={{ fontSize: 10, marginLeft: 6 }}>{s.scene}</Tag>}
                    </div>
                  </div>
                ))
              )}
            </div>
          </div>
        )}

        {/* 顶部说明 */}
        <div style={{ padding: '10px 16px', borderBottom: '1px solid #f0f0f0', background: '#fafafa' }}>
          <Text type="secondary" style={{ fontSize: 12 }}>
            <AimOutlined /> 直接描述问题，AI 会自动识别意图、调用平台工具（构建日志/Pod 日志/事件）+ RAG 知识库排查原因并给出解决方案
          </Text>
          {profile && profile.summary && (
            <div style={{ marginTop: 4, fontSize: 11, color: '#8c8c8c' }}>
              <UserOutlined /> {profile.summary}
            </div>
          )}
        </div>

        {/* 消息列表 */}
        <div ref={listRef} style={{ flex: 1, overflowY: 'auto', padding: '16px' }}>
          {history.length === 0 ? (
            <EmptyState onPick={send} questions={quickQuestions} />
          ) : (
            <>
              {history.map((m) => (
                <MessageBubble key={m.id} msg={m} />
              ))}
              {streaming && history[history.length - 1]?.pending && (
                <div style={{ textAlign: 'center', padding: 8 }}>
                  <Spin size="small" />
                </div>
              )}
            </>
          )}
        </div>

        {/* 快捷问题（对话中也可点击） */}
        {history.length > 0 && quickQuestions.length > 0 && (
          <div style={{ padding: '8px 16px', borderTop: '1px solid #f0f0f0', background: '#fafafa' }}>
            <div style={{ display: 'flex', gap: 6, overflowX: 'auto', paddingBottom: 4 }}>
              {quickQuestions.slice(0, 4).map((q) => (
                <Tag
                  key={q.question}
                  style={{ cursor: 'pointer', whiteSpace: 'nowrap', fontSize: 12 }}
                  color="blue"
                  onClick={() => send(q.question)}
                >
                  <BulbOutlined /> {q.question}
                </Tag>
              ))}
            </div>
          </div>
        )}

        {/* 输入区 */}
        <div style={{ padding: '12px 16px', borderTop: '1px solid #f0f0f0', background: '#fff' }}>
          <Input.TextArea
            value={input}
            onChange={(e) => setInput(e.target.value)}
            placeholder="描述你的问题，例如：构建 #123 失败了、Pod 一直 CrashLoopBackOff、发布卡住了…  Enter 发送，Shift+Enter 换行"
            autoSize={{ minRows: 1, maxRows: 4 }}
            onPressEnter={(e) => {
              if (!e.shiftKey) {
                e.preventDefault();
                send(input);
              }
            }}
            disabled={streaming}
          />
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginTop: 8 }}>
            <Text type="secondary" style={{ fontSize: 11 }}>
              回答由 LLM 生成，请核对后再操作
            </Text>
            <Button
              type="primary"
              icon={<SendOutlined />}
              size="small"
              loading={streaming}
              disabled={!input.trim()}
              onClick={() => send(input)}
            >
              发送
            </Button>
          </div>
        </div>
      </Drawer>
    </>
  );
}

function profileLabel(level: string): string {
  const m: Record<string, string> = {
    beginner: '初学者',
    intermediate: '中级',
    advanced: '高级',
    expert: '资深',
    unknown: '',
  };
  return m[level] || '';
}

function MessageBubble({ msg }: { msg: StoredMessage }) {
  const isUser = msg.role === 'user';
  if (msg.pending) {
    return (
      <div style={{ display: 'flex', justifyContent: 'flex-start', margin: '8px 0' }}>
        <div style={{ maxWidth: '85%', background: '#fff', border: '1px solid #f0f0f0', borderRadius: 8, padding: '10px 12px' }}>
          <Spin size="small" /> <Text type="secondary" style={{ fontSize: 12, marginLeft: 8 }}>识别意图并调用工具中…</Text>
        </div>
      </div>
    );
  }
  return (
    <div style={{ display: 'flex', justifyContent: isUser ? 'flex-end' : 'flex-start', margin: '8px 0' }}>
      <div
        style={{
          maxWidth: '88%',
          background: isUser ? '#e6f4ff' : '#fff',
          border: `1px solid ${isUser ? '#91caff' : '#f0f0f0'}`,
          borderRadius: 8,
          padding: '10px 12px',
          wordBreak: 'break-word',
        }}
      >
        {!isUser && (
          <div style={{ marginBottom: 4 }}>
            <RobotOutlined style={{ color: '#1677ff', fontSize: 12 }} />
            <Text type="secondary" style={{ fontSize: 11, marginLeft: 4 }}>AI 助手</Text>
          </div>
        )}

        {/* 意图识别展示（仅在非用户消息且识别到意图时展示） */}
        {!isUser && msg.intent && <IntentCard intent={msg.intent} />}

        {isUser ? (
          <div style={{ whiteSpace: 'pre-wrap' }}>{msg.content}</div>
        ) : (
          <div dangerouslySetInnerHTML={{ __html: renderMarkdown(msg.content) }} />
        )}

        {/* 工具调用结果展示 */}
        {!isUser && msg.tools && msg.tools.length > 0 && <ToolCallList tools={msg.tools} />}

        {/* 引用来源 */}
        {msg.references && msg.references.length > 0 && (
          <div style={{ marginTop: 8, paddingTop: 8, borderTop: '1px dashed #f0f0f0' }}>
            <Text type="secondary" style={{ fontSize: 11 }}>
              <LinkOutlined /> 引用来源
            </Text>
            {msg.references.map((r, i) => (
              <div key={i} style={{ fontSize: 12, marginTop: 4, color: '#595959' }}>
                <Text strong style={{ fontSize: 12 }}>{r.title}</Text>
                {r.url && <a href={r.url} target="_blank" rel="noreferrer" style={{ marginLeft: 6, fontSize: 11 }}>查看</a>}
                <div style={{ color: '#8c8c8c' }}>{r.snippet}</div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

/**
 * IntentCard 意图识别展示卡片。
 * 让用户看到 AI 如何理解问题、调用了哪些工具，提升透明度与可调试性。
 */
function IntentCard({ intent }: { intent: Intent }) {
  const intentLabel = intentLabelOf(intent.category);
  const intentColor = intentColorOf(intent.category);
  return (
    <Collapse
      size="small"
      ghost
      defaultActiveKey={[]}
      style={{ marginBottom: 8, background: '#fafafa', borderRadius: 6 }}
      items={[{
        key: 'intent',
        label: (
          <Space size={6}>
            <AimOutlined style={{ color: '#1677ff' }} />
            <Text style={{ fontSize: 12 }}>意图识别</Text>
            <Tag color={intentColor} style={{ fontSize: 11, margin: 0 }}>{intentLabel}</Tag>
            {intent.tools && intent.tools.length > 0 && (
              <Tag style={{ fontSize: 11, margin: 0 }}>
                <ToolOutlined /> {intent.tools.length} 个工具
              </Tag>
            )}
          </Space>
        ),
        children: (
          <div style={{ fontSize: 12 }}>
            {intent.reasoning && (
              <div style={{ marginBottom: 6, color: '#595959' }}>
                <Text type="secondary" style={{ fontSize: 11 }}>推理：</Text>
                {intent.reasoning}
              </div>
            )}
            {intent.tools && intent.tools.length > 0 && (
              <div>
                <Text type="secondary" style={{ fontSize: 11 }}>调用工具：</Text>
                <ul style={{ margin: '4px 0 0 16px', padding: 0 }}>
                  {intent.tools.map((t, i) => (
                    <li key={i} style={{ fontSize: 11, color: '#595959' }}>
                      <code>{t.name}</code>
                      {t.args && Object.keys(t.args).length > 0 && (
                        <span style={{ color: '#8c8c8c' }}>
                          {' '}({Object.entries(t.args).map(([k, v]) => `${k}=${v}`).join(', ')})
                        </span>
                      )}
                    </li>
                  ))}
                </ul>
              </div>
            )}
          </div>
        ),
      }]}
    />
  );
}

/**
 * ToolCallList 工具调用结果展示（在回答之后）。
 * 用户可展开查看每个工具的返回结果，便于审计与调试。
 */
export function ToolCallList({ tools }: { tools: { name: string; arguments: string; result: string }[] }) {
  if (!tools || tools.length === 0) return null;
  return (
    <Collapse
      size="small"
      ghost
      style={{ marginTop: 8, background: '#fafafa', borderRadius: 6 }}
      items={[{
        key: 'tools',
        label: (
          <Space size={6}>
            <ToolOutlined style={{ color: '#1677ff' }} />
            <Text style={{ fontSize: 12 }}>工具调用结果</Text>
            <Tag style={{ fontSize: 11, margin: 0 }}>{tools.length}</Tag>
          </Space>
        ),
        children: (
          <div style={{ fontSize: 12 }}>
            {tools.map((t, i) => (
              <div key={i} style={{ marginBottom: 8, padding: 6, background: '#fff', borderRadius: 4, border: '1px solid #f0f0f0' }}>
                <div>
                  <Text strong style={{ fontSize: 12 }}>
                    <code>{t.name}</code>
                  </Text>
                  {t.arguments && (
                    <Text type="secondary" style={{ fontSize: 11, marginLeft: 8 }}>{t.arguments}</Text>
                  )}
                </div>
                {t.result && (
                  <pre style={{ fontSize: 11, marginTop: 4, maxHeight: 200, overflow: 'auto', whiteSpace: 'pre-wrap', color: '#595959', background: '#fafafa', padding: 6, borderRadius: 4 }}>
                    {t.result}
                  </pre>
                )}
              </div>
            ))}
          </div>
        ),
      }]}
    />
  );
}

function EmptyState({ onPick, questions }: { onPick: (q: string) => void; questions: FAQItem[] }) {
  return (
    <div style={{ padding: '24px 8px', textAlign: 'center' }}>
      <RobotOutlined style={{ fontSize: 40, color: '#1677ff', marginBottom: 12 }} />
      <Paragraph>
        <Text strong>你好，我是 VortexOps AI 助手</Text>
      </Paragraph>
      <Paragraph type="secondary" style={{ fontSize: 13 }}>
        直接描述你遇到的问题，我会自动识别问题类别（构建失败、Pod 异常、发布问题等），
        调用平台工具收集日志与上下文，给出根因分析与修复建议。
      </Paragraph>
      <Paragraph type="secondary" style={{ fontSize: 12 }}>
        例如：「构建 #123 失败了帮我看看」、「api-server Pod 一直 CrashLoopBackOff」、「发布卡在滚动更新」
      </Paragraph>
      {questions.length > 0 && (
        <div style={{ textAlign: 'left', marginTop: 16 }}>
          <Text type="secondary" style={{ fontSize: 12 }}>
            <BulbOutlined /> 常见问题（点击直接提问）
          </Text>
          <div style={{ marginTop: 8, display: 'flex', flexDirection: 'column', gap: 6 }}>
            {questions.map((q) => (
              <div
                key={q.question}
                onClick={() => onPick(q.question)}
                style={{
                  padding: '8px 10px',
                  background: '#fafafa',
                  border: '1px solid #f0f0f0',
                  borderRadius: 6,
                  cursor: 'pointer',
                  fontSize: 13,
                  transition: 'all 0.2s',
                }}
                onMouseEnter={(e) => { e.currentTarget.style.borderColor = '#91caff'; e.currentTarget.style.background = '#e6f4ff'; }}
                onMouseLeave={(e) => { e.currentTarget.style.borderColor = '#f0f0f0'; e.currentTarget.style.background = '#fafafa'; }}
              >
                <Text strong style={{ fontSize: 13 }}>{q.question}</Text>
                <div style={{ color: '#8c8c8c', fontSize: 12, marginTop: 2 }}>{q.answer.slice(0, 60)}…</div>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

function intentLabelOf(category: string): string {
  switch (category) {
    case 'build_failure': return '镜像构建失败';
    case 'pod_failure': return 'Pod 异常';
    case 'release_issue': return '发布问题';
    case 'k8s_ops': return 'K8s 运维';
    case 'general_question': return '通用问答';
    default: return category;
  }
}

function intentColorOf(category: string): string {
  switch (category) {
    case 'build_failure': return 'orange';
    case 'pod_failure': return 'red';
    case 'release_issue': return 'purple';
    case 'k8s_ops': return 'blue';
    default: return 'default';
  }
}

function loadHistory(): StoredMessage[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return [];
    const arr = JSON.parse(raw) as StoredMessage[];
    // 过滤掉 pending（刷新前未完成的请求）。
    return arr.filter((m) => !m.pending);
  } catch {
    return [];
  }
}

function saveHistory(history: StoredMessage[]) {
  try {
    // 仅保留最近 50 条避免 localStorage 膨胀。
    const trimmed = history.slice(-50);
    localStorage.setItem(STORAGE_KEY, JSON.stringify(trimmed));
  } catch {
    // 忽略 quota 错误。
  }
}
