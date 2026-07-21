/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_API_BASE: string;
  readonly VITE_WS_BASE: string;
  // 开发环境直连 apiserver 的 WebSocket 地址，绕过 Vite 代理对 WS 升级的不稳定支持。
  // 形如 "http://localhost:8080"（协议自动转 ws/wss）。生产环境留空走同源 nginx。
  readonly VITE_DEV_WS_TARGET: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
