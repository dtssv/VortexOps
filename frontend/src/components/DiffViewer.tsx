import { lazy, Suspense } from 'react';
import { Spin } from 'antd';

const MonacoDiffEditor = lazy(() =>
  import('@monaco-editor/react').then((m) => ({ default: m.DiffEditor })),
);

interface DiffViewerProps {
  original: string;
  modified: string;
  language?: string;
  height?: number;
}

export function DiffViewer({ original, modified, language = 'json', height = 400 }: DiffViewerProps) {
  return (
    <Suspense fallback={<Spin />}>
      <MonacoDiffEditor
        height={height}
        language={language}
        original={original}
        modified={modified}
        theme="vs-dark"
        options={{
          readOnly: true,
          renderSideBySide: true,
          minimap: { enabled: false },
          fontSize: 13,
          automaticLayout: true,
        }}
      />
    </Suspense>
  );
}
