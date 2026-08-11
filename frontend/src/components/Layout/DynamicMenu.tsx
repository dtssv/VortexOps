import { useEffect, useMemo, useState } from 'react';
import { Empty, Menu as AntMenu } from 'antd';
import { useNavigate, useLocation } from 'react-router-dom';
import { useAuthStore } from '@/stores/authStore';
import { useUIStore } from '@/stores/uiStore';
import { buildMenuTree } from '@/hooks/menu';
import {
  DashboardOutlined,
  AppstoreOutlined,
  ClusterOutlined,
  BuildOutlined,
  CloudUploadOutlined,
  SettingOutlined,
  ApartmentOutlined,
  RobotOutlined,
  AuditOutlined,
  AlertOutlined,
  MonitorOutlined,
  SafetyOutlined,
  UserOutlined,
  KeyOutlined,
  FileTextOutlined,
  SafetyCertificateOutlined,
  VideoCameraOutlined,
  CodeOutlined,
  ApiOutlined,
  ContainerOutlined,
  BookOutlined,
} from '@ant-design/icons';
import type { Menu as MenuType } from '@/types';
import type { MenuProps } from 'antd';

const ICON_MAP: Record<string, React.ReactNode> = {
  dashboard: <DashboardOutlined />,
  application: <AppstoreOutlined />,
  cluster: <ClusterOutlined />,
  build: <BuildOutlined />,
  release: <CloudUploadOutlined />,
  config: <FileTextOutlined />,
  pipeline: <ApartmentOutlined />,
  inference: <RobotOutlined />,
  audit: <AuditOutlined />,
  approval: <AuditOutlined />,
  bastion: <SafetyCertificateOutlined />,
  'bastion-sessions': <VideoCameraOutlined />,
  'ops-terminal': <CodeOutlined />,
  'ops-sessions': <VideoCameraOutlined />,
  'port-forward': <ApiOutlined />,
  'behavior-audit': <AuditOutlined />,
  diagnose: <RobotOutlined />,
  diagnosis: <RobotOutlined />,
  alert: <AlertOutlined />,
  ops: <MonitorOutlined />,
  monitor: <MonitorOutlined />,
  rbac: <SafetyOutlined />,
  user: <UserOutlined />,
  token: <KeyOutlined />,
  setting: <SettingOutlined />,
  container: <ContainerOutlined />,
  book: <BookOutlined />,
};

function toAntdItems(menus: MenuType[]): MenuProps['items'] {
  return menus
    .filter((m) => m.visible && m.menu_type !== 'button')
    .map((m) => {
      const iconKey = m.icon || m.code;
      if (m.children?.length) {
        const children = toAntdItems(m.children) ?? [];
        // 目录无可见子菜单时不渲染（避免空分组）。
        if (children.length === 0) return null;
        return {
          key: m.path || `dir-${m.id}`,
          icon: ICON_MAP[iconKey],
          label: m.name,
          children,
        };
      }
      // 叶子菜单：有 path 即可点击导航，不论 menu_type 是 menu 还是 directory。
      // （历史 seed 把部分叶子菜单标成了 directory，这里按 path 判断而非 menu_type。）
      if (!m.path) return null;
      return {
        key: m.path,
        icon: ICON_MAP[iconKey],
        label: m.name,
      };
    })
    .filter((item): item is NonNullable<typeof item> => item !== null);
}

export function DynamicMenu() {
  const navigate = useNavigate();
  const location = useLocation();
  const menus = useAuthStore((s) => s.menus);
  const siderCollapsed = useUIStore((s) => s.siderCollapsed);

  const source = menus;
  const tree = buildMenuTree(source);
  const items = toAntdItems(tree);

  if (source.length === 0) {
    return (
      <div style={{ padding: 16 }}>
        <Empty
          image={Empty.PRESENTED_IMAGE_SIMPLE}
          description={<span style={{ color: 'rgba(255,255,255,0.45)' }}>当前角色没有可见菜单，请联系管理员授权</span>}
        />
      </div>
    );
  }

  const selectedKey = (() => {
    // best-effort match: longest path prefix
    const paths = source.filter((m) => m.path).map((m) => m.path!);
    const match = paths
      .filter((p) => location.pathname === p || location.pathname.startsWith(p + '/'))
      .sort((a, b) => b.length - a.length)[0];
    return match || location.pathname;
  })();

  // 当前选中项所属的顶层目录 key：用于初始化展开。
  const activeTopKey = useMemo(() => {
    const top = tree.find((t) => {
      const tkey = t.path || `dir-${t.id}`;
      if (tkey === selectedKey) return true;
      return containsPath(t, selectedKey);
    });
    return top ? (top.path || `dir-${top.id}`) : '';
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tree, selectedKey]);

  // 受控 openKeys：手风琴模式——仅展开当前激活的顶层目录；用户手动展开其它目录时
  // 自动收起其余，避免「展开一层其它全部展开」。
  const [openKeys, setOpenKeys] = useState<string[]>(activeTopKey ? [activeTopKey] : []);
  useEffect(() => {
    setOpenKeys((prev) => {
      if (activeTopKey && !prev.includes(activeTopKey)) {
        return [activeTopKey];
      }
      return prev;
    });
  }, [activeTopKey]);

  return (
    <AntMenu
      theme="dark"
      mode="inline"
      selectedKeys={[selectedKey]}
      openKeys={openKeys}
      onOpenChange={(keys) => {
        // 手风琴：最新展开的 key 即为本次操作的目录，收起其它。
        const latest = keys.find((k) => !openKeys.includes(k));
        if (latest) {
          setOpenKeys([latest]);
        } else {
          setOpenKeys(keys);
        }
      }}
      items={items}
      onClick={({ key }) => {
        if (key.startsWith('/')) navigate(key);
      }}
      inlineCollapsed={siderCollapsed}
    />
  );
}

// containsPath 递归判断目录 node 下是否存在路径等于 path 的叶子菜单。
function containsPath(node: MenuType, path: string): boolean {
  if (!node.children?.length) return false;
  for (const c of node.children) {
    if (c.path === path) return true;
    if (containsPath(c, path)) return true;
  }
  return false;
}
