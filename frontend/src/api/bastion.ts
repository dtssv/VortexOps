import { get, getPaged, post, put, del } from './client';
import type { BastionAsset, BastionSession, Paged } from '@/types';

export interface BastionAssetQuery {
  workspace_id?: number;
  protocol?: 'ssh' | 'rdp';
  search?: string;
  page?: number;
  size?: number;
}

export interface BastionSessionQuery {
  workspace_id?: number;
  asset_id?: number;
  status?: 'active' | 'closed';
  page?: number;
  size?: number;
}

export interface CreateBastionAssetInput {
  workspace_id: number;
  name: string;
  host: string;
  port: number;
  protocol: 'ssh' | 'rdp';
  platform: string;
  username: string;
  credential_id?: number;
  tags?: string[];
  comment?: string;
}

export type UpdateBastionAssetInput = Omit<CreateBastionAssetInput, 'workspace_id'> & { is_active: boolean };

export const bastionApi = {
  listAssets: (params?: BastionAssetQuery) => getPaged<BastionAsset>('/bastion/assets', params),
  getAsset: (id: number) => get<BastionAsset>(`/bastion/assets/${id}`),
  createAsset: (body: CreateBastionAssetInput) => post<BastionAsset>('/bastion/assets', body),
  updateAsset: (id: number, body: UpdateBastionAssetInput) =>
    put<BastionAsset>(`/bastion/assets/${id}`, body),
  deleteAsset: (id: number) => del(`/bastion/assets/${id}`),
  connect: (id: number) => post<{ login_url: string }>(`/bastion/assets/${id}/connect`),
  syncAssets: (workspaceId?: number) =>
    post<{ synced: number }>(`/bastion/sync${workspaceId ? `?workspace_id=${workspaceId}` : ''}`),

  listSessions: (params?: BastionSessionQuery) =>
    getPaged<BastionSession>('/bastion/sessions', params),
  getReplay: (id: number) => get<{ replay_url: string }>(`/bastion/sessions/${id}/replay`),
};

export type { BastionAsset, BastionSession, Paged };
