import { useEffect, useRef, useState, Suspense, lazy } from 'react';
import { Spin, Space, Button, Dropdown, Tooltip, Tag } from 'antd';
import {
  AppstoreOutlined,
  DownOutlined,
  ThunderboltOutlined,
  CopyOutlined,
  EyeOutlined,
  EditOutlined,
} from '@ant-design/icons';
import type * as Monaco from 'monaco-editor';
import { previewEffectiveEntrypoint } from '@/utils/baseImage';

const MonacoEditor = lazy(() => import('@monaco-editor/react'));

// Dockerfile 指令（用于语法高亮 + 自动补全）。
const DOCKERFILE_INSTRUCTIONS = [
  'FROM', 'RUN', 'CMD', 'LABEL', 'MAINTAINER', 'EXPOSE', 'ENV', 'ADD', 'COPY',
  'ENTRYPOINT', 'VOLUME', 'USER', 'WORKDIR', 'ARG', 'ONBUILD', 'STOPSIGNAL',
  'HEALTHCHECK', 'SHELL',
];

// 模板占位符（后端 text/template 渲染可用变量）。
const TEMPLATE_PLACEHOLDERS = [
  { label: '{{.BaseImage}}', desc: '运行时基础镜像引用（如 eclipse-temurin:17-jre）' },
  { label: '{{.ArtifactPath}}', desc: '构建产物路径（如 target/*.jar）' },
  { label: '{{.Entrypoint}}', desc: '启动命令 JSON 数组（如 ["java","-jar","/app/app.jar"]）' },
];

// 按语言/运行时预置的代码片段，一键插入。
const RUNTIME_SNIPPETS: { key: string; label: string; runtime: string; code: string }[] = [
  {
    key: 'java',
    label: 'Java (jar)',
    runtime: 'java',
    code: `FROM {{.BaseImage}}
WORKDIR /app
COPY {{.ArtifactPath}} /app/app.jar
EXPOSE 8080
ENTRYPOINT {{.Entrypoint}}`,
  },
  {
    key: 'go',
    label: 'Go (binary)',
    runtime: 'go',
    code: `FROM {{.BaseImage}}
WORKDIR /app
COPY {{.ArtifactPath}} /app/app
EXPOSE 8080
ENTRYPOINT {{.Entrypoint}}`,
  },
  {
    key: 'python',
    label: 'Python (gunicorn)',
    runtime: 'python',
    code: `FROM {{.BaseImage}}
WORKDIR /app
COPY {{.ArtifactPath}} /app/
EXPOSE 8000
ENTRYPOINT {{.Entrypoint}}`,
  },
  {
    key: 'node',
    label: 'Node (nginx)',
    runtime: 'node',
    code: `FROM {{.BaseImage}}
WORKDIR /app
COPY {{.ArtifactPath}} /app/
EXPOSE 80
ENTRYPOINT {{.Entrypoint}}`,
  },
  {
    key: 'custom',
    label: '自定义',
    runtime: 'custom',
    code: `FROM {{.BaseImage}}
WORKDIR /app
COPY {{.ArtifactPath}} /app/
ENTRYPOINT {{.Entrypoint}}`,
  },
];

// 指令级片段（仅插入单条指令骨架）。
const INSTRUCTION_SNIPPETS: { label: string; insert: string; desc: string }[] = [
  { label: 'EXPOSE 端口', insert: 'EXPOSE 8080', desc: '声明容器监听端口' },
  { label: 'ENV 环境变量', insert: 'ENV JAVA_OPTS="-Xms512m -Xmx512m"', desc: '设置环境变量' },
  { label: 'ARG 构建参数', insert: 'ARG VERSION=1.0', desc: '定义构建期参数' },
  { label: 'USER 切换用户', insert: 'USER appuser', desc: '以非 root 用户运行' },
  { label: 'HEALTHCHECK 健康检查', insert: 'HEALTHCHECK --interval=30s --timeout=3s CMD curl -f http://localhost:8080/health || exit 1', desc: '容器健康检查' },
  { label: 'VOLUME 数据卷', insert: 'VOLUME ["/data"]', desc: '声明匿名数据卷' },
  { label: 'LABEL 标签', insert: 'LABEL maintainer="ops@example.com"', desc: '镜像元数据标签' },
];

interface DockerfileEditorProps {
  // value/onChange 由 Ant Design Form.Item 注入；单独使用时也可显式传入。
  value?: string;
  onChange?: (v: string) => void;
  height?: number;
  readOnly?: boolean;
  /** 预览面板用的占位符示例值；不传则用默认。 */
  sampleValues?: { BaseImage?: string; ArtifactPath?: string; Entrypoint?: string };
  /** Web 镜像：预览时 ENTRYPOINT 额外包含 nginx 启动（与后端 EffectiveEntrypoint 一致）。 */
  isWeb?: boolean;
  /** 应用启动命令 JSON 字符串（不含 nginx），与 isWeb 合成预览用 ENTRYPOINT。 */
  entrypointRaw?: string;
  /** 当前运行时，用于高亮匹配的片段。 */
  runtime?: string;
}

const LANGUAGE_ID = 'dockerfile-template';

// 注册自定义语言只做一次（monaco 是单例）。
let languageRegistered = false;
function registerLanguage(monaco: typeof Monaco) {
  if (languageRegistered) return;
  languageRegistered = true;

  monaco.languages.register({ id: LANGUAGE_ID });

  // Tokenizer：识别指令关键字、占位符 {{.X}}、注释、字符串。
  monaco.languages.setMonarchTokensProvider(LANGUAGE_ID, {
    defaultToken: '',
    tokenPostfix: '.dockerfile',
    keywords: DOCKERFILE_INSTRUCTIONS,
    tokenizer: {
      root: [
        [/#.*$/, 'comment'],
        // 占位符 {{.Name}}
        [/\{\{\s*\.[A-Za-z_][A-Za-z0-9_]*\s*\}\}/, 'variable.predefined'],
        // 指令关键字（行首，忽略大小写）
        [/^[ \t]*(?:FROM|RUN|CMD|LABEL|MAINTAINER|EXPOSE|ENV|ADD|COPY|ENTRYPOINT|VOLUME|USER|WORKDIR|ARG|ONBUILD|STOPSIGNAL|HEALTHCHECK|SHELL)\b/i, 'keyword'],
        // 字符串
        [/"/, { token: 'string.quote', next: '@string_double' }],
        [/'/, { token: 'string.quote', next: '@string_single' }],
        [/`/, { token: 'string.quote', next: '@string_backtick' }],
        // 数字（端口等）
        [/\d+/, 'number'],
      ],
      string_double: [
        [/[^"]+/, 'string'],
        [/"/, { token: 'string.quote', next: '@pop' }],
      ],
      string_single: [
        [/[^']+/, 'string'],
        [/'/, { token: 'string.quote', next: '@pop' }],
      ],
      string_backtick: [
        [/[^`]+/, 'string'],
        [/`/, { token: 'string.quote', next: '@pop' }],
      ],
    },
  });

  // 主题配色（沿用 vs-dark，给占位符一个醒目颜色）。
  monaco.editor.defineTheme(`${LANGUAGE_ID}-theme`, {
    base: 'vs-dark',
    inherit: true,
    rules: [
      { token: 'variable.predefined', foreground: '4ec9b0', fontStyle: 'italic' },
      { token: 'keyword', foreground: '569cd6' },
      { token: 'string', foreground: 'ce9178' },
      { token: 'number', foreground: 'b5cea8' },
      { token: 'comment', foreground: '6a9955' },
    ],
    colors: {},
  });

  // 自动补全：指令 + 占位符 + 片段。
  monaco.languages.registerCompletionItemProvider(LANGUAGE_ID, {
    triggerCharacters: ['{', '.', ' '],
    provideCompletionItems(model, position) {
      const word = model.getWordUntilPosition(position);
      const range = {
        startLineNumber: position.lineNumber,
        endLineNumber: position.lineNumber,
        startColumn: word.startColumn,
        endColumn: word.endColumn,
      };
      const linePrefix = model.getValueInRange({
        startLineNumber: position.lineNumber,
        startColumn: 1,
        endLineNumber: position.lineNumber,
        endColumn: position.column,
      });
      const isLineStart = /^\s*$/.test(linePrefix);

      const suggestions: Monaco.languages.CompletionItem[] = [];

      // 行首补全指令关键字。
      if (isLineStart || /^[A-Z]*$/i.test(linePrefix.trim())) {
        DOCKERFILE_INSTRUCTIONS.forEach((kw) => {
          suggestions.push({
            label: kw,
            kind: monaco.languages.CompletionItemKind.Keyword,
            insertText: kw,
            detail: 'Dockerfile 指令',
            range,
          });
        });
      }

      // 输入 { 或 . 时补全占位符。
      if (linePrefix.endsWith('{') || linePrefix.endsWith('{{') || linePrefix.endsWith('.') || /\{\{\s*\.?[A-Za-z]*$/.test(linePrefix)) {
        TEMPLATE_PLACEHOLDERS.forEach((p) => {
          suggestions.push({
            label: p.label,
            kind: monaco.languages.CompletionItemKind.Variable,
            insertText: p.label,
            detail: p.desc,
            range,
          });
        });
      }

      // 常用指令片段（行首时）。
      if (isLineStart) {
        INSTRUCTION_SNIPPETS.forEach((s) => {
          suggestions.push({
            label: s.insert,
            kind: monaco.languages.CompletionItemKind.Snippet,
            insertText: s.insert,
            detail: s.desc,
            sortText: '1',
            range,
          });
        });
      }

      return { suggestions };
    },
  });

  // 悬停提示：占位符含义。
  monaco.languages.registerHoverProvider(LANGUAGE_ID, {
    provideHover(model, position) {
      const lineContent = model.getLineContent(position.lineNumber);
      const m = /\{\{\s*\.([A-Za-z_][A-Za-z0-9_]*)\s*\}\}/.exec(lineContent);
      if (!m) return null;
      const placeholderText = m[0];
      const varName = m[1];
      const startIdx = lineContent.indexOf(placeholderText);
      if (startIdx < 0) return null;
      const startCol = startIdx + 1;
      const endCol = startIdx + placeholderText.length + 1;
      if (position.column < startCol || position.column > endCol) return null;
      const def = TEMPLATE_PLACEHOLDERS.find((p) => p.label === placeholderText);
      const contents = [
        { value: `**${placeholderText}**` },
        { value: def ? def.desc : `模板变量 \`.${varName}\`，构建时由后端 text/template 渲染。` },
      ];
      return {
        range: {
          startLineNumber: position.lineNumber,
          endLineNumber: position.lineNumber,
          startColumn: startCol,
          endColumn: endCol,
        },
        contents,
      };
    },
  });
}

// 渲染预览：用示例值替换占位符，方便用户直观看到最终 Dockerfile。
function renderPreview(
  tmpl: string,
  sample: { BaseImage?: string; ArtifactPath?: string; Entrypoint?: string },
  isWeb?: boolean,
  entrypointRaw?: string,
): string {
  const base = sample.BaseImage || 'eclipse-temurin:17-jre';
  const artifact = sample.ArtifactPath || 'target/*.jar';
  const entrypoint =
    isWeb !== undefined
      ? previewEffectiveEntrypoint(!!isWeb, entrypointRaw ?? sample.Entrypoint ?? '')
      : sample.Entrypoint || '["java","-jar","/app/app.jar"]';
  return (tmpl || '')
    .replace(/\{\{\s*\.BaseImage\s*\}\}/g, base)
    .replace(/\{\{\s*\.ArtifactPath\s*\}\}/g, artifact)
    .replace(/\{\{\s*\.Entrypoint\s*\}\}/g, entrypoint);
}

export function DockerfileEditor({
  value,
  onChange,
  height = 320,
  readOnly,
  sampleValues,
  isWeb,
  entrypointRaw,
  runtime,
}: DockerfileEditorProps) {
  const editorRef = useRef<Monaco.editor.IStandaloneCodeEditor | null>(null);
  const [showPreview, setShowPreview] = useState(false);

  const handleMount = (editor: Monaco.editor.IStandaloneCodeEditor, monaco: typeof Monaco) => {
    editorRef.current = editor;
    registerLanguage(monaco);
  };

  const insertAtCursor = (text: string) => {
    const editor = editorRef.current;
    if (!editor) return;
    const selection = editor.getSelection();
    if (!selection) return;
    editor.executeEdits('snippet', [
      {
        range: selection,
        text,
        forceMoveMarkers: true,
      },
    ]);
    editor.focus();
  };

  const replaceAll = (text: string) => {
    const editor = editorRef.current;
    if (!editor) return;
    editor.setValue(text);
    editor.focus();
  };

  // 匹配当前 runtime 的预置模板。
  const matchedRuntimeSnippet = RUNTIME_SNIPPETS.find((s) => s.runtime === runtime);

  const preview = renderPreview(value ?? '', sampleValues || {}, isWeb, entrypointRaw);

  return (
    <div style={{ border: '1px solid #d9d9d9', borderRadius: 6, overflow: 'hidden' }}>
      {/* 工具栏 */}
      <div
        style={{
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
          padding: '6px 10px',
          background: '#fafafa',
          borderBottom: '1px solid #e8e8e8',
          flexWrap: 'wrap',
          gap: 8,
        }}
      >
        <Space size="small" wrap>
          <Dropdown
            menu={{
              items: RUNTIME_SNIPPETS.map((s) => ({
                key: s.key,
                label: (
                  <span>
                    {s.label}
                    {s.runtime === runtime && <Tag color="blue" style={{ marginInlineStart: 6 }}>当前</Tag>}
                  </span>
                ),
              })),
              onClick: ({ key }) => {
                const s = RUNTIME_SNIPPETS.find((x) => x.key === key);
                if (s) replaceAll(s.code);
              },
            }}
          >
            <Button size="small" icon={<AppstoreOutlined />}>
              运行时模板 <DownOutlined />
            </Button>
          </Dropdown>
          <Dropdown
            menu={{
              items: INSTRUCTION_SNIPPETS.map((s, i) => ({
                key: String(i),
                label: <span>{s.label} <span style={{ color: '#999', fontSize: 11 }}>{s.desc}</span></span>,
              })),
              onClick: ({ key }) => {
                const s = INSTRUCTION_SNIPPETS[Number(key)];
                if (s) insertAtCursor(s.insert + '\n');
              },
            }}
          >
            <Button size="small" icon={<ThunderboltOutlined />}>
              插入指令 <DownOutlined />
            </Button>
          </Dropdown>
          <Dropdown
            menu={{
              items: TEMPLATE_PLACEHOLDERS.map((p) => ({
                key: p.label,
                label: <span><code>{p.label}</code> <span style={{ color: '#999', fontSize: 11 }}>{p.desc}</span></span>,
              })),
              onClick: ({ key }) => insertAtCursor(key),
            }}
          >
            <Button size="small" icon={<CopyOutlined />}>
              占位符 <DownOutlined />
            </Button>
          </Dropdown>
          {matchedRuntimeSnippet && (
            <Tooltip title="用当前运行时的标准模板覆盖（可继续编辑）">
              <Button size="small" type="link" onClick={() => replaceAll(matchedRuntimeSnippet.code)}>
                套用「{matchedRuntimeSnippet.label}」模板
              </Button>
            </Tooltip>
          )}
        </Space>
        <Tooltip title={showPreview ? '切换到编辑' : '切换到预览（占位符已替换示例值）'}>
          <Button
            size="small"
            type={showPreview ? 'default' : 'primary'}
            ghost={showPreview}
            icon={showPreview ? <EditOutlined /> : <EyeOutlined />}
            onClick={() => setShowPreview((v) => !v)}
          >
            {showPreview ? '编辑' : '预览'}
          </Button>
        </Tooltip>
      </div>

      {/* 占位符提示条 */}
      <div style={{ padding: '4px 10px', background: '#f6f8fa', fontSize: 11, color: '#57606a', borderBottom: '1px solid #e8e8e8' }}>
        {showPreview && isWeb ? (
          <span>
            <Tag color="cyan" style={{ marginInlineEnd: 8 }}>Web 镜像</Tag>
            预览中 <code>ENTRYPOINT</code> 已包含 <code>nginx</code> 启动（应用命令之前执行）。
          </span>
        ) : (
          <>
            可用占位符：
            {TEMPLATE_PLACEHOLDERS.map((p) => (
              <code key={p.label} style={{ marginInlineStart: 8, color: '#1f6feb' }}>{p.label}</code>
            ))}
            <span style={{ marginInlineStart: 8 }}>— 输入 <code>{'{{'}</code> 触发补全，悬停占位符查看说明</span>
          </>
        )}
      </div>

      <Suspense fallback={<div style={{ height, display: 'flex', alignItems: 'center', justifyContent: 'center' }}><Spin /></div>}>
        {showPreview ? (
          <MonacoEditor
            height={height}
            language="shell"
            value={preview}
            theme="vs-dark"
            options={{
              readOnly: true,
              minimap: { enabled: false },
              fontSize: 13,
              wordWrap: 'on',
              scrollBeyondLastLine: false,
              automaticLayout: true,
              lineNumbers: 'on',
            }}
          />
        ) : (
          <MonacoEditor
            height={height}
            language={LANGUAGE_ID}
            value={value}
            theme={`${LANGUAGE_ID}-theme`}
            onChange={(v) => onChange?.(v ?? '')}
            onMount={handleMount}
            options={{
              readOnly,
              minimap: { enabled: false },
              fontSize: 13,
              wordWrap: 'on',
              scrollBeyondLastLine: false,
              automaticLayout: true,
              lineNumbers: 'on',
              tabSize: 2,
              suggestOnTriggerCharacters: true,
              quickSuggestions: { other: true, comments: false, strings: true },
            }}
          />
        )}
      </Suspense>
    </div>
  );
}
