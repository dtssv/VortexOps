import { create } from 'zustand';
import { persist, createJSONStorage } from 'zustand/middleware';
import type { User, Menu, LoginResponse } from '@/types';
import { apiGet, post, configureRefresh, setAccessTokenGetter, setUnauthorizedHandler } from '@/api/client';

interface AuthState {
  user: User | null;
  accessToken: string | null;
  refreshToken: string | null;
  permissions: string[];
  menus: Menu[];
  loading: boolean;
  initialized: boolean;
  mfaChallenge: { mfaToken: string; username: string } | null;
  login: (username: string, password: string) => Promise<void>;
  loginWithMFA: (code: string) => Promise<void>;
  fetchProfileAndMenus: () => Promise<void>;
  logout: () => Promise<void>;
  setSession: (access: string, refresh: string, user?: User) => void;
  hasPermission: (code: string) => boolean;
  hasAnyPermission: (codes: string[]) => boolean;
  clearMFAChallenge: () => void;
  reset: () => void;
}

export const useAuthStore = create<AuthState>()(
  persist(
    (set, get) => {
      // Wire refresh + unauthorized handling back into the store.
      configureRefresh({
        getRefreshToken: () => get().refreshToken,
        onRefreshed: (access, refresh) => set({ accessToken: access, refreshToken: refresh }),
      });
      setAccessTokenGetter(() => get().accessToken);
      setUnauthorizedHandler(() => get().reset());

      return {
        user: null,
        accessToken: null,
        refreshToken: null,
        permissions: [],
        menus: [],
        loading: false,
        initialized: false,
        mfaChallenge: null,

        async login(username, password) {
          const res = await post<LoginResponse & { mfa_required?: boolean; mfa_token?: string; username?: string }>(
            '/auth/login',
            { username, password },
          );
          if (res.mfa_required && res.mfa_token) {
            set({ mfaChallenge: { mfaToken: res.mfa_token, username: res.username || username } });
            throw new Error('MFA_REQUIRED');
          }
          set({
            accessToken: res.access_token,
            refreshToken: res.refresh_token,
            user: res.user ?? null,
          });
          await get().fetchProfileAndMenus();
        },

        async loginWithMFA(code) {
          const challenge = get().mfaChallenge;
          if (!challenge) {
            throw new Error('No active MFA challenge');
          }
          const res = await post<LoginResponse>('/auth/login/mfa', {
            mfa_token: challenge.mfaToken,
            code,
          });
          set({
            accessToken: res.access_token,
            refreshToken: res.refresh_token,
            user: res.user ?? null,
            mfaChallenge: null,
          });
          await get().fetchProfileAndMenus();
        },

        async fetchProfileAndMenus() {
          set({ loading: true });
          // 分别请求，避免一个失败拖垮整次初始化。
          // 权限码必须走 /me/permissions（含 action），不能从菜单树反推。
          // menus/permissions 失败时降级为空：无权限不展示，禁止全量兜底菜单。
          let user: User | null = get().user;
          let menus: Menu[] = [];
          let permissions: string[] = [];
          try {
            user = await apiGet<User>('/users/me');
          } catch {
            // user 拉取失败可能是 token 过期；若 refresh 也失败，全局 401 拦截会跳登录。
          }
          try {
            menus = (await apiGet<Menu[]>('/me/menus')) || [];
          } catch {
            menus = [];
          }
          try {
            permissions = (await apiGet<string[]>('/me/permissions')) || [];
          } catch {
            permissions = [];
          }
          set({ user, menus, permissions, initialized: true, loading: false });
        },

        async logout() {
          const rt = get().refreshToken;
          try {
            if (rt) await post('/auth/logout', { refresh_token: rt });
          } catch {
            // ignore — clear local state regardless
          }
          get().reset();
        },

        setSession(access, refresh, user) {
          set({ accessToken: access, refreshToken: refresh, user: user ?? get().user });
        },

        hasPermission(code) {
          const perms = get().permissions;
          if (perms.includes(code) || perms.includes('*')) return true;
          // 与后端一致：支持 'prefix:*' 前缀通配。
          for (const c of perms) {
            if (c.endsWith(':*') && code.startsWith(c.slice(0, -1))) return true;
          }
          return false;
        },

        hasAnyPermission(codes) {
          return codes.some((c) => get().hasPermission(c));
        },

        clearMFAChallenge() {
          set({ mfaChallenge: null });
        },

        reset() {
          set({
            user: null,
            accessToken: null,
            refreshToken: null,
            permissions: [],
            menus: [],
            initialized: false,
            mfaChallenge: null,
          });
        },
      };
    },
    {
      name: 'vortexops-auth',
      storage: createJSONStorage(() => localStorage),
      // 只持久化 token；user/menus/permissions/initialized 每次刷新后通过
      // fetchProfileAndMenus() 重新拉取，避免使用过期数据。
      partialize: (s) => ({ accessToken: s.accessToken, refreshToken: s.refreshToken }),
    },
  ),
);
