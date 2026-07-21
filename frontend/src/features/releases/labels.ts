import type { ReleaseStrategy } from '@/types';

/**
 * 发布策略编码 → 中文名映射。
 * 后端 release.Strategy 枚举：rolling/recreate/blue_green/canary/percentage/machine_count。
 * 用于发布详情、发布列表、分组发布历史等所有展示位置，保持一致。
 */
export const STRATEGY_LABEL: Record<ReleaseStrategy, string> = {
  rolling: '滚动更新',
  recreate: '一次性发布',
  blue_green: '蓝绿',
  canary: '金丝雀',
  percentage: '按百分比',
  machine_count: '按机器数（分批）',
};

/** 取发布策略中文名，未知编码回退原值。 */
export function strategyLabel(strategy?: string): string {
  if (!strategy) return '-';
  return (STRATEGY_LABEL as Record<string, string>)[strategy] ?? strategy;
}
