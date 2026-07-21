import { get, put } from './client';

export interface SystemSetting {
  id: number;
  key: string;
  value: any;
  description: string;
  is_public: boolean;
  version: number;
  created_at: string;
  updated_at: string;
}

export const systemApi = {
  listPublic: (search?: string) => get<SystemSetting[]>('/system-settings', search ? { search } : undefined),
  listAll: (search?: string) => get<SystemSetting[]>('/system-settings/all', search ? { search } : undefined),
  get: (key: string) => get<SystemSetting>(`/system-settings/${key}`),
  update: (key: string, body: { value: any; description?: string; is_public?: boolean }) =>
    put<SystemSetting>(`/system-settings/${key}`, body),
};
