import { get, post, put, del, getAccessTokenStream } from './client';

export interface DiagnosisResult {
  resource_type: string;
  cluster_id: number;
  namespace: string;
  name: string;
  summary: string;
  suggestions: string;
  raw_context: string;
  model: string;
  provider: string;
  latency_ms: number;
}

export interface AnalyzeInput {
  resource_type: 'pod' | 'deployment' | 'node';
  cluster_id: number;
  namespace: string;
  name: string;
  container?: string;
}

// 日志诊断来源：与后端 LogSource 对齐。
export type LogSource = 'build' | 'pod_startup' | 'pod_crash';

export interface LogAnalyzeInput {
  source: LogSource;
  title: string;
  cluster_id?: number;
  namespace?: string;
  name?: string;
  container?: string;
  build_id?: number;
  error_reason?: string;
  logs: string;
}

export type ChatRole = 'system' | 'user' | 'assistant';

export interface ChatMessage {
  role: ChatRole;
  content: string;
}

export interface ToolCall {
  name: string;
  arguments: string;
  result: string;
}

export interface Reference {
  title: string;
  url?: string;
  snippet: string;
}

// 意图识别结果：AI 自动从用户消息中识别问题类别与推荐工具。
export interface IntentTool {
  name: string;
  args: Record<string, string>;
}

export interface Intent {
  category: 'build_failure' | 'pod_failure' | 'release_issue' | 'k8s_ops' | 'general_question' | string;
  reasoning: string;
  tools: IntentTool[];
}

export interface ChatResult {
  answer: string;
  model: string;
  provider: string;
  latency_ms: number;
  tools?: ToolCall[];
  references?: Reference[];
  intent?: Intent;
  // 持久化会话 ID（前端可保存用于历史回看）。
  session_id?: number;
  // 本轮使用的用户画像摘要（前端可展示个性化标签）。
  profile_summary?: string;
}

export interface FAQItem {
  question: string;
  answer: string;
  category: string;
}

export interface ChatInput {
  messages: ChatMessage[];
  // scene 已废弃：后端通过 LLM 意图识别自动判断。保留字段以兼容。
  scene?: string;
  // 可选会话 ID：传入后消息会持久化，并使用会话上下文（摘要 + 实体记忆）。
  session_id?: number;
}

export const diagnosisApi = {
  // 上下文诊断：选择集群/资源 → 后端收集 K8s 上下文 → LLM 分析。
  analyze: (body: AnalyzeInput) => post<DiagnosisResult>('/diagnosis/analyze', body),
  // 日志诊断：前端已收集日志（构建失败/Pod 启动失败）→ LLM 分析。
  analyzeLogs: (body: LogAnalyzeInput) => post<DiagnosisResult>('/diagnosis/analyze-logs', body),
  // 流式日志诊断：通过 fetch + ReadableStream 解析 SSE 事件。
  // 回调：onDelta（增量文本，多次）、onDone（完整结果，一次）、onError。
  analyzeLogsStream: async (
    body: LogAnalyzeInput,
    handlers: {
      onDelta?: (delta: string) => void;
      onDone?: (result: DiagnosisResult) => void;
      onError?: (err: Error) => void;
    },
    signal?: AbortSignal,
  ): Promise<void> => {
    const baseURL = (import.meta.env.VITE_API_BASE as string) || '/api/v1';
    const token = getAccessTokenStream?.();
    const resp = await fetch(`${baseURL}/diagnosis/analyze-logs/stream`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Accept: 'text/event-stream',
        ...(token ? { Authorization: `Bearer ${token}` } : {}),
      },
      body: JSON.stringify(body),
      signal,
    });
    if (!resp.ok || !resp.body) {
      const text = await resp.text().catch(() => '');
      throw new Error(`analyze-logs stream HTTP ${resp.status}: ${text}`);
    }
    const reader = resp.body.getReader();
    const decoder = new TextDecoder('utf-8');
    let buffer = '';
    try {
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });
        // SSE 事件以空行分隔；逐块解析。
        const parts = buffer.split('\n\n');
        buffer = parts.pop() || '';
        for (const part of parts) {
          const lines = part.split('\n');
          let event = 'message';
          let data = '';
          for (const line of lines) {
            if (line.startsWith('event: ')) event = line.slice(7).trim();
            else if (line.startsWith('data: ')) data += line.slice(6);
          }
          if (!data) continue;
          try {
            const payload = JSON.parse(data);
            if (event === 'delta') handlers.onDelta?.((payload as any).delta || '');
            else if (event === 'done') handlers.onDone?.(payload as DiagnosisResult);
            else if (event === 'error') handlers.onError?.(new Error((payload as any).message || 'stream error'));
          } catch {
            // 忽略解析失败的行。
          }
        }
      }
    } catch (e: any) {
      if (e?.name === 'AbortError') return;
      handlers.onError?.(e instanceof Error ? e : new Error(String(e)));
    }
  },
  // 多轮对话式 AI 助手（一次性返回完整结果，兼容旧调用）。
  chat: (body: ChatInput) => post<ChatResult>('/diagnosis/chat', body),
  // 流式对话：通过 fetch + ReadableStream 解析 SSE 事件。
  // 回调：onMeta（元信息，一次）、onDelta（增量文本，多次）、onDone（完整结果，一次）、onError。
  chatStream: async (
    body: ChatInput,
    handlers: {
      onMeta?: (meta: ChatResult) => void;
      onDelta?: (delta: string) => void;
      onDone?: (result: ChatResult) => void;
      onError?: (err: Error) => void;
    },
    signal?: AbortSignal,
  ): Promise<void> => {
    const baseURL = (import.meta.env.VITE_API_BASE as string) || '/api/v1';
    const token = getAccessTokenStream?.();
    const resp = await fetch(`${baseURL}/diagnosis/chat/stream`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Accept: 'text/event-stream',
        ...(token ? { Authorization: `Bearer ${token}` } : {}),
      },
      body: JSON.stringify(body),
      signal,
    });
    if (!resp.ok || !resp.body) {
      const text = await resp.text().catch(() => '');
      throw new Error(`chat stream HTTP ${resp.status}: ${text}`);
    }
    const reader = resp.body.getReader();
    const decoder = new TextDecoder('utf-8');
    let buffer = '';
    let currentEvent = 'message';
    try {
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });
        // SSE 事件以空行分隔；逐块解析。
        const parts = buffer.split('\n\n');
        buffer = parts.pop() || '';
        for (const part of parts) {
          const lines = part.split('\n');
          let event = 'message';
          let data = '';
          for (const line of lines) {
            if (line.startsWith('event: ')) event = line.slice(7).trim();
            else if (line.startsWith('data: ')) data += line.slice(6);
          }
          if (!data) continue;
          try {
            const payload = JSON.parse(data);
            if (event === 'meta') handlers.onMeta?.(payload as ChatResult);
            else if (event === 'delta') handlers.onDelta?.((payload as any).delta || '');
            else if (event === 'done') handlers.onDone?.(payload as ChatResult);
            else if (event === 'error') handlers.onError?.(new Error((payload as any).message || 'stream error'));
          } catch {
            // 忽略解析失败的行。
          }
          currentEvent = event;
        }
      }
      // 处理 buffer 中剩余的最后一块（若以空行结尾则已被 pop，否则忽略不完整事件）。
      void currentEvent;
    } catch (e: any) {
      if (e?.name === 'AbortError') return;
      handlers.onError?.(e instanceof Error ? e : new Error(String(e)));
    }
  },
  // 常见问题列表（场景已废弃，后端始终返回通用 FAQ；意图识别由 /chat 自动完成）。
  listFAQ: (scene?: string) => get<FAQItem[]>('/diagnosis/faq', scene ? { scene } : undefined),
};

// ===== 知识库（KB）API =====
export interface KBCategory {
  id: number;
  uuid: string;
  name: string;
  code: string;
  description: string;
  sort_order: number;
}

export interface KBDocument {
  id: number;
  uuid: string;
  category_id: number;
  title: string;
  source_type: string;
  source_url: string;
  content: string;
  tags: string[];
  chunk_count: number;
  status: string;
  version: number;
  created_at: string;
  updated_at: string;
}

export interface KBChunkResult {
  id: number;
  document_id: number;
  chunk_index: number;
  content: string;
  document_title: string;
  category_code: string;
  score: number;
}

export interface KBDocumentListParams {
  category?: string;
  search?: string;
  status?: string;
  page?: number;
  size?: number;
}

export interface PagedResult<T> {
  items: T[];
  total: number;
  page: number;
  size: number;
}

export const kbApi = {
  listCategories: () => get<KBCategory[]>('/kb/categories'),
  listDocuments: (params: KBDocumentListParams) => get<PagedResult<KBDocument>>('/kb/documents', params),
  getDocument: (id: number) => get<KBDocument>(`/kb/documents/${id}`),
  createDocument: (body: Partial<KBDocument>) => post<KBDocument>('/kb/documents', body),
  updateDocument: (id: number, body: Partial<KBDocument>) => put<KBDocument>(`/kb/documents/${id}`, body),
  deleteDocument: (id: number) => del<void>(`/kb/documents/${id}`),
  reindexDocument: (id: number) => post<{ reindexed: boolean }>(`/kb/documents/${id}/reindex`, {}),
  search: (body: { query: string; top_k?: number; category_code?: string }) =>
    post<KBChunkResult[]>('/kb/search', body),
};

// ===== 用户画像 API =====
export interface UserProfile {
  id?: number;
  uuid?: string;
  user_id?: number;
  expertise_level: string;
  roles: string[];
  domains: string[];
  communication_style: string;
  preferred_language: string;
  summary: string;
  interaction_count: number;
  last_updated_at?: string;
}

export const profileApi = {
  get: () => get<UserProfile>('/user-profile'),
  update: (body: Partial<UserProfile>) => put<UserProfile>('/user-profile', body),
};

// ===== 对话会话 API =====
export interface ChatSession {
  id: number;
  uuid: string;
  user_id: number;
  title: string;
  scene: string;
  summary?: string;
  entities?: Record<string, unknown>;
  message_count: number;
  last_intent?: string;
  last_active_at: string;
  created_at: string;
  updated_at: string;
}

export interface ChatSessionMessage {
  id: number;
  uuid: string;
  session_id: number;
  role: string;
  content: string;
  intent?: Intent;
  tools?: ToolCall[];
  references?: Reference[];
  latency_ms?: number;
  created_at: string;
}

export const chatSessionApi = {
  listSessions: (limit?: number) => get<ChatSession[]>('/chat/sessions', limit ? { limit } : undefined),
  createSession: (body: { title?: string; scene?: string }) => post<ChatSession>('/chat/sessions', body),
  getSession: (id: number) => get<ChatSession>(`/chat/sessions/${id}`),
  deleteSession: (id: number) => del<void>(`/chat/sessions/${id}`),
  listMessages: (id: number, limit?: number) =>
    get<ChatSessionMessage[]>(`/chat/sessions/${id}/messages`, limit ? { limit } : undefined),
};
