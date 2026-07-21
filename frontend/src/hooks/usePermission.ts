import { useAuthStore } from '@/stores/authStore';

export function usePermission(code: string): { can: boolean } {
  const has = useAuthStore((s) => s.hasPermission);
  return { can: has(code) };
}

export function useAnyPermission(codes: string[]): { can: boolean } {
  const hasAny = useAuthStore((s) => s.hasAnyPermission);
  return { can: hasAny(codes) };
}
