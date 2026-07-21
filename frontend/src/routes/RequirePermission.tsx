import { Navigate } from 'react-router-dom';
import { useAuthStore } from '@/stores/authStore';
import { Result } from 'antd';
import type { ReactNode } from 'react';

export function RequirePermission({ code, children }: { code: string; children: ReactNode }) {
  const has = useAuthStore((s) => s.hasPermission);
  if (!has(code)) {
    return <Result status="403" title="403" subTitle="抱歉，您无权访问此页面。" />;
  }
  return <>{children}</>;
}

export function RequireAnyPermission({ codes, children }: { codes: string[]; children: ReactNode }) {
  const hasAny = useAuthStore((s) => s.hasAnyPermission);
  if (!hasAny(codes)) {
    return <Result status="403" title="403" subTitle="抱歉，您无权访问此页面。" />;
  }
  return <>{children}</>;
}
