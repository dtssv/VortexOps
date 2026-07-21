import { create } from 'zustand';

interface UIState {
  siderCollapsed: boolean;
  currentWorkspaceUuid: string | null;
  currentWorkspaceId: number | null;
  currentWorkspaceName: string | null;
  currentApplicationUuid: string | null;
  currentApplicationId: number | null;
  toggleSider: () => void;
  setSiderCollapsed: (v: boolean) => void;
  setCurrentWorkspace: (id: number | null, uuid: string | null, name?: string | null) => void;
  setCurrentApplication: (id: number | null, uuid: string | null) => void;
}

export const useUIStore = create<UIState>((set) => ({
  siderCollapsed: false,
  currentWorkspaceUuid: null,
  currentWorkspaceId: null,
  currentWorkspaceName: null,
  currentApplicationUuid: null,
  currentApplicationId: null,
  toggleSider: () => set((s) => ({ siderCollapsed: !s.siderCollapsed })),
  setSiderCollapsed: (v) => set({ siderCollapsed: v }),
  setCurrentWorkspace: (id, uuid, name = null) =>
    set({ currentWorkspaceId: id, currentWorkspaceUuid: uuid, currentWorkspaceName: name }),
  setCurrentApplication: (id, uuid) =>
    set({ currentApplicationId: id, currentApplicationUuid: uuid }),
}));
