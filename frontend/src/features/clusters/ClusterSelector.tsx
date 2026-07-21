import { Select } from 'antd';
import type { CSSProperties } from 'react';
import type { Cluster } from '@/types';

interface ClusterSelectorProps {
  clusters: Cluster[];
  value?: number;
  onChange: (clusterId: number | undefined) => void;
  style?: CSSProperties;
}

export function ClusterSelector({ clusters, value, onChange, style }: ClusterSelectorProps) {
  return (
    <Select
      placeholder="选择集群"
      style={{ width: 280, ...style }}
      showSearch
      allowClear
      optionFilterProp="label"
      options={clusters.map((c) => ({
        label: c.display_name || c.name,
        value: c.id,
      }))}
      value={value}
      onChange={onChange}
    />
  );
}
