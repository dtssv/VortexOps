import { useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Dropdown, Select, Spin } from 'antd';
import { DownOutlined } from '@ant-design/icons';
import { useQuery } from '@tanstack/react-query';

export interface BreadcrumbOption {
  label: string;
  value: number;
  path: string;
}

interface BreadcrumbSwitcherProps {
  /** 当前实体名称；未加载时传 undefined 显示占位 ... */
  currentLabel?: string;
  /** 当前实体详情路径：点击标签文本时跳转。 */
  currentPath?: string;
  /** 远程加载候选项；search 为空时传 undefined 由后端返回首页 */
  loadOptions: (search: string) => Promise<BreadcrumbOption[]>;
  /** React Query 缓存键前缀，必须在该层级唯一（如 ['workspaces'] / ['apps', wsId]） */
  queryKeyPrefix: (string | number)[];
  /** 当前实体 id，用于忽略重复选择 */
  currentValue?: number;
  /** 选择后回调；默认 navigate(path) */
  onSelect?: (value: number, path: string) => void;
  /** 搜索防抖毫秒数 */
  debounceMs?: number;
}

/**
 * 可切换 + 模糊搜索的面包屑节点。
 *
 * 渲染分两块：
 *   - 标签文本：点击 navigate 到当前实体详情页（currentPath）。
 *   - 下拉 caret：点击展开 Antd Select（showSearch + filterOption=false
 *     + onSearch 防抖 + useQuery 远程搜索）。选择候选项后 navigate 到对应详情页。
 *
 * 仅在下拉打开时发起请求；以 [prefix, search] 为 query key 缓存，相同搜索词重复打开即时返回。
 */
export function BreadcrumbSwitcher({
  currentLabel,
  currentPath,
  loadOptions,
  queryKeyPrefix,
  currentValue,
  onSelect,
  debounceMs = 350,
}: BreadcrumbSwitcherProps) {
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState('');
  const [debounced, setDebounced] = useState('');

  useEffect(() => {
    if (!open) return;
    const t = setTimeout(() => setDebounced(search), debounceMs);
    return () => clearTimeout(t);
  }, [search, open, debounceMs]);

  const queryKey = useMemo(
    () => [...queryKeyPrefix, 'crumb-search', debounced],
    [queryKeyPrefix, debounced],
  );

  const { data, isFetching } = useQuery({
    queryKey,
    queryFn: () => loadOptions(debounced),
    enabled: open,
    staleTime: 60_000,
  });

  const options = data ?? [];
  // 尚未拉取过（data 为 undefined）时也展示加载态，避免初次打开闪现「无匹配项」。
  const showLoading = isFetching || data === undefined;

  const closeDropdown = () => {
    setOpen(false);
    setSearch('');
    setDebounced('');
  };

  const handleSelect = (value: number) => {
    const opt = options.find((o) => o.value === value);
    closeDropdown();
    if (!opt) return;
    if (onSelect) {
      onSelect(value, opt.path);
    } else {
      navigate(opt.path);
    }
  };

  // 点击标签文本：跳转到当前实体详情页。
  const onLabelClick = () => {
    if (currentPath) {
      navigate(currentPath);
    }
  };

  const menu = (
    <div
      style={{
        width: 280,
        padding: 8,
        background: '#fff',
        borderRadius: 6,
        boxShadow: '0 6px 16px rgba(0,0,0,0.12)',
      }}
      onClick={(e) => e.stopPropagation()}
    >
      {/* 用 key 强制每次打开重建 Select，避免上一次选择残留受控值 */}
      <Select<string | number>
        key={`crumb-${open ? 'open' : 'closed'}`}
        showSearch
        autoFocus
        defaultOpen
        suffixIcon={null}
        style={{ width: '100%' }}
        placeholder="搜索并选择"
        filterOption={false}
        onSearch={setSearch}
        onSelect={(v) => handleSelect(Number(v))}
        notFoundContent={showLoading ? <Spin size="small" /> : '无匹配项'}
        options={options.map((o) => ({
          label: o.value === currentValue ? `${o.label}（当前）` : o.label,
          value: o.value,
        }))}
      />
    </div>
  );

  return (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 2 }}>
      {/* 标签文本：点击跳转到当前实体详情页 */}
      <span
        style={{ cursor: currentPath ? 'pointer' : 'default' }}
        onClick={onLabelClick}
      >
        {currentLabel ?? '...'}
      </span>
      {/* 下拉 caret：点击展开切换器 */}
      <Dropdown
        overlay={menu}
        trigger={['click']}
        open={open}
        onOpenChange={(v) => {
          if (!v) closeDropdown();
          else setOpen(true);
        }}
      >
        <DownOutlined style={{ fontSize: 10, color: '#8c8c8c', cursor: 'pointer' }} />
      </Dropdown>
    </span>
  );
}
