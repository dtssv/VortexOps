import { get, getPaged, post, put, del } from './client';
import type { User, Menu, Paged } from '@/types';

export const authApi = {
  login: (username: string, password: string) =>
    post<{ access_token: string; refresh_token: string; token_type: string; expires_at: string; user?: User }>(
      '/auth/login',
      { username, password },
    ),
  register: (body: {
    username: string;
    email?: string;
    phone?: string;
    display_name?: string;
    password: string;
    locale?: string;
    timezone?: string;
  }) => post<User>('/auth/register', body),
  refresh: (refreshToken: string) =>
    post<{ access_token: string; refresh_token: string }>('/auth/refresh', { refresh_token: refreshToken }),
  logout: (refreshToken: string) => post('/auth/logout', { refresh_token: refreshToken }),
  logoutAll: () => post('/auth/logout-all'),
  me: () => get<User>('/users/me'),
  myMenus: () => get<Menu[]>('/me/menus'),
  myPermissions: () => get<string[]>('/me/permissions'),
  changePassword: (oldPassword: string, newPassword: string) =>
    post('/users/me/password', { old_password: oldPassword, new_password: newPassword }),
  deleteAccount: () => del(`/users/me`),

  // 用户管理（需 user:manage 权限）
  listUsers: (params?: { search?: string; status?: string; page?: number; size?: number }) =>
    getPaged<User>('/users', params),
  createUser: (body: {
    username: string;
    email: string;
    phone?: string;
    display_name?: string;
    password: string;
    locale?: string;
    timezone?: string;
  }) => post<User>('/users', body),
  updateUser: (id: number, body: {
    email?: string;
    phone?: string;
    display_name?: string;
    locale?: string;
    timezone?: string;
    status?: string;
    version: number;
  }) => put<User>(`/users/${id}`, body),
  resetUserPassword: (id: number, body: { new_password: string; must_change_password?: boolean }) =>
    put(`/users/${id}/password`, body),
  updateUserStatus: (id: number, status: 'active' | 'disabled' | 'locked') =>
    put(`/users/${id}/status`, { status }),
  deleteUser: (id: number) => del(`/users/${id}`),

  // 登录方式（公开）
  listLoginProviders: () =>
    get<{ code: string; source: string; display_name: string; is_external: boolean; is_default: boolean }[]>(
      '/auth/providers',
    ),

  // MFA (TOTP) 两步验证
  loginWithMFA: (mfaToken: string, code: string) =>
    post<{ access_token: string; refresh_token: string; token_type: string; expires_at: string; user?: User }>(
      '/auth/login/mfa',
      { mfa_token: mfaToken, code },
    ),
  generateMFA: () =>
    post<{ secret: string; otpauth_url: string; backup_codes: string[] }>('/users/me/mfa/generate'),
  enableMFA: (code: string) => post('/users/me/mfa/enable', { code }),
  disableMFA: (code: string, usePassword = false) =>
    post('/users/me/mfa/disable', { code, use_password: usePassword }),
};
