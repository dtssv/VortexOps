import { post } from './client';

// Prometheus 即时查询结果。
export interface MonitoringQueryResult {
  metric: Record<string, string>;
  value: [number, string]; // [timestamp, "value"]
}

// Prometheus 范围查询结果点。
export interface MonitoringQueryRangeResult {
  metric: Record<string, string>;
  values: [number, string][]; // [[timestamp, "value"], ...]
}

export const monitoringApi = {
  // POST /api/v1/monitoring/query
  query: (query: string, time?: string) =>
    post<MonitoringQueryResult[]>('/monitoring/query', { query, time }),

  // POST /api/v1/monitoring/query-range
  queryRange: (params: { query: string; start?: string; end?: string; step?: string }) =>
    post<MonitoringQueryRangeResult[]>('/monitoring/query-range', params),

  // POST /api/v1/monitoring/evaluate-rules
  evaluateRules: () => post<{ triggered: number }>('/monitoring/evaluate-rules'),
};
