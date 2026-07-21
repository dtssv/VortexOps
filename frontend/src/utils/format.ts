import dayjs from 'dayjs';
import relativeTime from 'dayjs/plugin/relativeTime';
import duration from 'dayjs/plugin/duration';

dayjs.extend(relativeTime);
dayjs.extend(duration);

export function formatTime(t?: string | null): string {
  if (!t) return '-';
  return dayjs(t).format('YYYY-MM-DD HH:mm:ss');
}

export function formatRelative(t?: string | null): string {
  if (!t) return '-';
  return dayjs(t).fromNow();
}

export function formatDuration(ms?: number | null): string {
  if (ms == null) return '-';
  if (ms < 1000) return `${ms}ms`;
  const d = dayjs.duration(ms);
  const h = Math.floor(d.asHours());
  if (h > 0) return `${h}h ${d.minutes()}m ${d.seconds()}s`;
  if (d.minutes() > 0) return `${d.minutes()}m ${d.seconds()}s`;
  return `${d.seconds()}s`;
}

export function formatBytes(bytes?: number | null): string {
  if (bytes == null) return '-';
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(2))} ${sizes[i]}`;
}

export function shortSha(sha?: string): string {
  if (!sha) return '-';
  return sha.slice(0, 8);
}

export function truncate(s?: string | null, n = 60): string {
  if (!s) return '-';
  return s.length > n ? `${s.slice(0, n)}...` : s;
}

// 速率格式化：bytes/sec → 人类可读（如 1.2 MB/s）。
export function formatRate(bytesPerSec?: number | null): string {
  if (bytesPerSec == null || bytesPerSec < 0) return '-';
  if (bytesPerSec === 0) return '0 B/s';
  const k = 1024;
  const sizes = ['B/s', 'KB/s', 'MB/s', 'GB/s', 'TB/s'];
  const i = Math.floor(Math.log(bytesPerSec) / Math.log(k));
  return `${parseFloat((bytesPerSec / Math.pow(k, i)).toFixed(2))} ${sizes[i]}`;
}

// 百分比格式化：0.85 → "85%"，或 85 → "85%"（兼容小数与已乘 100 的值）。
// 输入 <= 1 视为小数比例；> 1 视为已乘 100 的百分比。
export function formatPct(v?: number | null, digits = 1): string {
  if (v == null || isNaN(v)) return '-';
  let pct = v;
  if (Math.abs(v) <= 1) pct = v * 100;
  return `${pct.toFixed(digits)}%`;
}

// 负载格式化：load1/5/15 保留 2 位小数。
export function formatLoad(v?: number | null): string {
  if (v == null || isNaN(v)) return '-';
  return v.toFixed(2);
}

// CPU millicores → 核数：1500m → "1.5 核"。
export function formatCpuM(m?: number | null): string {
  if (m == null) return '-';
  return `${(m / 1000).toFixed(2)} 核`;
}
