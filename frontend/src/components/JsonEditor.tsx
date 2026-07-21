import { lazy, Suspense } from 'react';
import { Spin } from 'antd';

const MonacoEditor = lazy(() => import('@monaco-editor/react'));

interface JsonEditorProps {
  value: string;
  onChange?: (v: string) => void;
  language?: string;
  height?: number;
  readOnly?: boolean;
}

export function JsonEditor({ value, onChange, language = 'json', height = 300, readOnly }: JsonEditorProps) {
  return (
    <Suspense fallback={<Spin />}>
      <MonacoEditor
        height={height}
        language={language}
        value={value}
        onChange={(v) => onChange?.(v ?? '')}
        theme="vs-dark"
        options={{
          readOnly,
          minimap: { enabled: false },
          fontSize: 13,
          wordWrap: 'on',
          scrollBeyondLastLine: false,
          automaticLayout: true,
        }}
      />
    </Suspense>
  );
}
