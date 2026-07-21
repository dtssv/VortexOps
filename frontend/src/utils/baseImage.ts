/** 各语言默认应用启动命令（不含 nginx，Web 镜像由 is_web 在渲染时额外启动 nginx）。 */
export const RUNTIME_DEFAULT_ENTRYPOINTS: Record<string, string> = {
  java: '["sh","-c","exec java $JAVA_OPTS -jar /app/artifacts/*.jar"]',
  go: '["/app/artifacts/app"]',
  python: '["sh","-c","cd /app/artifacts && exec gunicorn --bind 0.0.0.0:8000 app:app"]',
  node: '',
  custom: '["./artifacts/app"]',
};

export function entrypointForRuntime(runtime: string): string {
  return RUNTIME_DEFAULT_ENTRYPOINTS[runtime] ?? '';
}

export function parseEntrypointRaw(raw: string | undefined): string[] {
  if (!raw) return [];
  try {
    const v = JSON.parse(raw);
    if (Array.isArray(v)) return v.map(String);
  } catch {
    // ignore
  }
  return [];
}

/** 编辑回显：从存储的 entrypoint 还原为「仅应用」启动命令（剥离历史 nginx 包装）。 */
export function appEntrypointRawForEdit(record: { is_web?: boolean; entrypoint?: string[] }): string {
  const ep = record.entrypoint;
  if (!ep || ep.length === 0) return '';
  if (ep[0] === 'nginx') return '';
  if (ep.length >= 3 && ep[0] === 'sh' && ep[1] === '-c') {
    const cmd = ep[2];
    if (cmd.startsWith('nginx && ')) {
      return JSON.stringify(['sh', '-c', cmd.slice('nginx && '.length)]);
    }
  }
  return JSON.stringify(ep);
}

/** 预览 Dockerfile 渲染后的有效 ENTRYPOINT（与后端 EffectiveEntrypoint 对齐）。 */
export function previewEffectiveEntrypoint(isWeb: boolean, entrypointRaw: string): string {
  const ep = parseEntrypointRaw(entrypointRaw);
  if (!isWeb) {
    if (ep.length === 0) return '["sh","-c","exec \\"$@\\""]';
    return JSON.stringify(ep);
  }
  const appShell = entrypointToShell(ep);
  if (!appShell) return '["nginx","-g","daemon off;"]';
  return JSON.stringify(['sh', '-c', `nginx && ${appShell}`]);
}

function entrypointToShell(ep: string[]): string {
  if (ep.length === 0) return '';
  if (ep.length >= 3 && ep[0] === 'sh' && ep[1] === '-c') return ep[2];
  if (ep.length === 1) return `exec ${ep[0]}`;
  return `exec ${ep.join(' ')}`;
}
