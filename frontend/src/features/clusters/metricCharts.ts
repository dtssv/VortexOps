// 趋势图 ECharts option 构造器。
// 所有趋势图共享统一的时间轴与 tooltip，仅 series 不同。
import type { EChartsOption } from 'echarts';

export interface SeriesSpec {
  name: string;
  data: [number, number][]; // [timestamp_ms, value]
}

// buildTrendOption 构造一个折线趋势图 option。
// unit 为 Y 轴单位（如 '%'、' MB/s'、' 核'），tooltip 自动附加。
// stack=true 时多 series 叠加为面积图（用于集群总量）。
export function buildTrendOption(
  series: SeriesSpec[],
  unit: string,
  opts?: { stack?: boolean; area?: boolean; height?: number },
): EChartsOption {
  const stack = opts?.stack ?? false;
  const area = opts?.area ?? stack;
  return {
    tooltip: {
      trigger: 'axis',
      axisPointer: { type: 'cross' },
      valueFormatter: (v: any) => `${Number(v).toFixed(2)}${unit}`,
    },
    legend: { data: series.map((s) => s.name), top: 0, type: 'scroll' },
    grid: { left: 56, right: 24, bottom: 48, top: 36 },
    xAxis: {
      type: 'time',
      axisLabel: { fontSize: 11 },
    },
    yAxis: {
      type: 'value',
      axisLabel: { formatter: `{value}${unit}` },
    },
    series: series.map((s) => ({
      name: s.name,
      type: 'line',
      smooth: true,
      showSymbol: false,
      stack: stack ? 'total' : undefined,
      areaStyle: area ? { opacity: 0.15 } : undefined,
      data: s.data,
    })),
  };
}

// 把采样点转为 ECharts 数据点 [timestamp_ms, value]。
export function toPoints<T extends { ts: string }>(
  samples: T[],
  pick: (s: T) => number,
): [number, number][] {
  return samples.map((s) => [new Date(s.ts).getTime(), pick(s)]);
}
