import axios, { AxiosError, AxiosInstance, InternalAxiosRequestConfig } from 'axios';
import type { ApiEnvelope, Paged } from '@/types';

const baseURL = import.meta.env.VITE_API_BASE || '/api/v1';

export class ApiError extends Error {
  code: string;
  httpStatus: number;
  details?: any;
  constructor(code: string, message: string, httpStatus: number, details?: any) {
    super(message);
    this.name = 'ApiError';
    this.code = code;
    this.httpStatus = httpStatus;
    this.details = details;
  }
}

let onUnauthorized: (() => void) | null = null;
export function setUnauthorizedHandler(fn: () => void) {
  onUnauthorized = fn;
}

// Refresh-token plumbing: a single in-flight refresh promise to dedupe concurrent 401s.
let refreshPromise: Promise<string> | null = null;
let getRefreshToken: (() => string | null) | null = null;
let onRefreshed: ((access: string, refresh: string) => void) | null = null;

export function configureRefresh(opts: {
  getRefreshToken: () => string | null;
  onRefreshed: (access: string, refresh: string) => void;
}) {
  getRefreshToken = opts.getRefreshToken;
  onRefreshed = opts.onRefreshed;
}

async function doRefresh(): Promise<string> {
  if (refreshPromise) return refreshPromise;
  const refreshToken = getRefreshToken?.();
  if (!refreshToken) throw new ApiError('UNAUTHENTICATED', 'session expired', 401);
  refreshPromise = (async () => {
    try {
      const res = await axios.post(
        `${baseURL}/auth/refresh`,
        { refresh_token: refreshToken },
        { headers: { 'Content-Type': 'application/json' } },
      );
      const env = res.data as ApiEnvelope<{ access_token: string; refresh_token: string }>;
      if (!env.success || !env.data) throw new ApiError('UNAUTHENTICATED', 'refresh failed', 401);
      onRefreshed?.(env.data.access_token, env.data.refresh_token);
      return env.data.access_token;
    } finally {
      refreshPromise = null;
    }
  })();
  return refreshPromise;
}

export const api: AxiosInstance = axios.create({
  baseURL,
  timeout: 30000,
  headers: { 'Content-Type': 'application/json' },
});

let getAccessToken: (() => string | null) | null = null;
export function setAccessTokenGetter(fn: () => string | null) {
  getAccessToken = fn;
}

// getAccessTokenStream 供非 axios 调用（如 fetch SSE 流式接口）获取当前 access token。
// 在 setAccessTokenGetter 之后才有效；未配置时返回 null。
export function getAccessTokenStream(): string | null {
  return getAccessToken?.() ?? null;
}

api.interceptors.request.use((config: InternalAxiosRequestConfig) => {
  const token = getAccessToken?.();
  if (token && config.headers) {
    config.headers.Authorization = `Bearer ${token}`;
  }
  return config;
});

api.interceptors.response.use(
  (response) => {
    const env = response.data as ApiEnvelope<unknown>;
    if (env && typeof env === 'object' && 'success' in env) {
      if (env.success) {
        return env.data as never;
      }
      const err = env.error || { code: 'UNKNOWN', message: 'unknown error' };
      throw new ApiError(err.code, err.message, response.status);
    }
    return response.data as never;
  },
  async (error: AxiosError) => {
    const original = error.config as InternalAxiosRequestConfig & { _retried?: boolean };
    if (error.response?.status === 401 && original && !original._retried && !original.url?.includes('/auth/')) {
      try {
        const newAccess = await doRefresh();
        original._retried = true;
        if (original.headers) original.headers.Authorization = `Bearer ${newAccess}`;
        return api(original);
      } catch {
        onUnauthorized?.();
        return Promise.reject(new ApiError('UNAUTHENTICATED', 'session expired', 401));
      }
    }
    if (error.response) {
      const env = error.response.data as ApiEnvelope<unknown> | undefined;
      if (env && typeof env === 'object' && 'success' in env && env.error) {
        return Promise.reject(new ApiError(env.error.code, env.error.message, error.response.status));
      }
      return Promise.reject(
        new ApiError('HTTP_ERROR', `request failed: ${error.response.status}`, error.response.status),
      );
    }
    if (error.request) {
      return Promise.reject(new ApiError('NETWORK', 'network error: no response from server', 0));
    }
    return Promise.reject(new ApiError('UNKNOWN', error.message || 'unknown error', 0));
  },
);

// Typed helpers — the response interceptor unwraps to the data payload directly.
// Named with `api` prefix to avoid shadowing zustand's `get()` inside stores.
export async function apiGet<T>(url: string, params?: Record<string, any>): Promise<T> {
  return api.get(url, { params }) as unknown as Promise<T>;
}

// Back-compat alias.
export const get = apiGet;

export async function getPaged<T>(
  url: string,
  params?: Record<string, any>,
): Promise<Paged<T>> {
  return api.get(url, { params }) as unknown as Promise<Paged<T>>;
}

export async function post<T>(url: string, body?: Record<string, any>): Promise<T> {
  return api.post(url, body) as unknown as Promise<T>;
}

export async function put<T>(url: string, body?: Record<string, any>): Promise<T> {
  return api.put(url, body) as unknown as Promise<T>;
}

export async function del<T = void>(url: string): Promise<T> {
  return api.delete(url) as unknown as Promise<T>;
}
