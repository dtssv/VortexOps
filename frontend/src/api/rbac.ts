import { get, getPaged, post, put, del } from './client';
import type { Permission, Role, Menu, Paged } from '@/types';

export const rbacApi = {
  listPermissions: (params?: { category?: string; scope?: string; page?: number; size?: number }) =>
    getPaged<Permission>('/permissions', params),
  createPermission: (body: Partial<Permission>) => post<Permission>('/permissions', body),
  deletePermission: (id: number) => del(`/permissions/${id}`),
  listMenus: () => get<Menu[]>('/menus'),
  createMenu: (body: Partial<Menu>) => post<Menu>('/menus', body),
  deleteMenu: (id: number) => del(`/menus/${id}`),
  myMenuTree: () => get<Menu[]>('/me/menus'),
  myPermissions: () => get<string[]>('/me/permissions'),
  listRoles: (params?: { scope?: string; scope_id?: number; page?: number; size?: number }) =>
    getPaged<Role>('/roles', params),
  createRole: (body: Partial<Role>) => post<Role>('/roles', body),
  deleteRole: (id: number) => del(`/roles/${id}`),
  listPermissionsByRole: (roleId: number) => get<Permission[]>(`/roles/${roleId}/permissions`),
  grantPermissions: (roleId: number, body: { permission_codes: string[] }) =>
    post(`/roles/${roleId}/permissions`, body),
  listMenusByRole: (roleId: number) => get<Menu[]>(`/roles/${roleId}/menus`),
  bindRoleMenus: (roleId: number, body: { menu_ids: number[]; clear?: boolean }) =>
    post(`/roles/${roleId}/menus`, body),
  bindPlatformRole: (userId: number, body: { role_id: number }) =>
    post(`/users/${userId}/platform-roles`, body),
  listPlatformRolesByUser: (userId: number) => get<Role[]>(`/users/${userId}/platform-roles`),
  addWorkspaceMember: (workspaceId: number, body: { user_id: number; role: string }) =>
    post(`/workspaces/${workspaceId}/members`, body),
  listWorkspaceMembers: (workspaceId: number) => get<any[]>(`/workspaces/${workspaceId}/members`),
  removeWorkspaceMember: (id: number) => del(`/workspace-members/${id}`),
};
