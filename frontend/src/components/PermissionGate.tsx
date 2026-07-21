import type { ReactNode } from 'react';
import { useAuthStore } from '@/stores/authStore';

export function PermissionGate({ code, children, fallback = null }: { code: string; children: ReactNode; fallback?: ReactNode }) {
  const has = useAuthStore((s) => s.hasPermission);
  if (!has(code)) return <>{fallback}</>;
  return <>{children}</>;
}

export function AnyPermissionGate({ codes, children, fallback = null }: { codes: string[]; children: ReactNode; fallback?: ReactNode }) {
  const hasAny = useAuthStore((s) => s.hasAnyPermission);
  if (!hasAny(codes)) return <>{fallback}</>;
  return <>{children}</>;
}
