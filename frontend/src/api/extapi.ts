import { get, getPaged, post, del } from './client';
import type { ExternalToken, ExternalCallLog, Paged } from '@/types';

export const extApi = {
  listTokens: (params?: { page?: number; size?: number }) => getPaged<ExternalToken>('/ext/tokens', params),
  createToken: (body: {
    name: string;
    scopes: string[];
    token_type?: string;
    allowed_workspaces?: number[];
    allowed_apps?: number[];
    rate_limit_per_min?: number;
    ip_allowlist?: string[];
    webhook_url?: string;
    expires_at?: string;
  }) => post<ExternalToken>('/ext/tokens', body),
  revokeToken: (id: number) => post(`/ext/tokens/${id}/revoke`),
  deleteToken: (id: number) => del(`/ext/tokens/${id}`),
  listCallLogs: (params?: { token_id?: number; operation?: string; page?: number; size?: number }) =>
    getPaged<ExternalCallLog>('/ext/call-logs', params),
  selfCreateWorkspace: (body: { name: string; display_name?: string; description?: string }) =>
    post('/ext/workspaces', body),
};
