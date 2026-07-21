import { get, getPaged, post } from './client';

export interface Approval {
  id: number;
  uuid: string;
  workspace_id: number;
  resource_type: string;
  resource_id: number;
  operation: string;
  requested_by: number;
  requested_at: string;
  approver_role: string;
  status: string;
  approver_id: number;
  approved_at?: string;
  comment: string;
  expires_at?: string;
  created_at: string;
}

export const approvalApi = {
  list: (params?: { workspace_id?: number; resource_type?: string; status?: string; page?: number; size?: number }) =>
    getPaged<Approval>('/approvals', params),
  get: (id: number) => get<Approval>(`/approvals/${id}`),
  approve: (id: number, body?: { comment?: string }) => post<Approval>(`/approvals/${id}/approve`, body || {}),
  reject: (id: number, body?: { comment?: string }) => post<Approval>(`/approvals/${id}/reject`, body || {}),
};
