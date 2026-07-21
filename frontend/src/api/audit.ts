import { get, getPaged, post } from './client';
import type { AuditLog, Notification, Paged } from '@/types';

export const auditApi = {
  list: (params?: {
    user_id?: number;
    workspace_id?: number;
    resource_type?: string;
    action?: string;
    start_time?: string;
    end_time?: string;
    page?: number;
    size?: number;
  }) => getPaged<AuditLog>('/audit-logs', params),
  get: (id: number) => get<AuditLog>(`/audit-logs/${id}`),
};

export const notificationApi = {
  list: (params?: { page?: number; size?: number; read?: boolean }) =>
    getPaged<Notification>('/notifications', params),
  unreadCount: () => get<{ count: number }>('/notifications/unread-count'),
  markAllRead: () => post('/notifications/read-all'),
};
