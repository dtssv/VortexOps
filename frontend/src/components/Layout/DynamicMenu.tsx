import { useEffect, useMemo, useState } from 'react';
import { Menu as AntMenu } from 'antd';
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

// Fallback static menu shown when dynamic menus are empty (e.g. 未配置权限的用户).
// 结构与 0018_menu_grouping 迁移后的 DB 菜单一致：两级分组（目录 → 菜单）。
const FALLBACK_ITEMS: MenuType[] = [
  // 顶级
  { id: 1, uuid: '1', parent_id: 0, code: 'dashboard', name: '总览', path: '/', icon: 'dashboard', menu_type: 'menu', scope: 'platform', visible: true, sort_order: 10, keep_alive: true, version: 1, created_at: '' },

  // 空间管理
  { id: 100, uuid: '100', parent_id: 0, code: 'grp-workspace', name: '空间管理', path: '', icon: 'application', menu_type: 'directory', scope: 'platform', visible: true, sort_order: 100, keep_alive: false, version: 1, created_at: '' },
  { id: 101, uuid: '101', parent_id: 100, code: 'workspace', name: '空间', path: '/workspaces', icon: 'application', menu_type: 'menu', scope: 'platform', visible: true, sort_order: 110, keep_alive: true, version: 1, created_at: '' },
  { id: 102, uuid: '102', parent_id: 100, code: 'application', name: '应用', path: '/applications', icon: 'application', menu_type: 'menu', scope: 'platform', visible: true, sort_order: 120, keep_alive: true, version: 1, created_at: '' },
  { id: 103, uuid: '103', parent_id: 100, code: 'config', name: '配置管理', path: '/configs', icon: 'config', menu_type: 'menu', scope: 'platform', visible: true, sort_order: 130, keep_alive: true, version: 1, created_at: '' },

  // 应用交付
  { id: 200, uuid: '200', parent_id: 0, code: 'grp-delivery', name: '应用交付', path: '', icon: 'build', menu_type: 'directory', scope: 'platform', visible: true, sort_order: 200, keep_alive: false, version: 1, created_at: '' },
  { id: 201, uuid: '201', parent_id: 200, code: 'builds', name: '构建中心', path: '/builds', icon: 'build', menu_type: 'menu', scope: 'platform', visible: true, sort_order: 210, keep_alive: true, version: 1, created_at: '' },
  { id: 202, uuid: '202', parent_id: 200, code: 'pipeline', name: 'CI/CD 流水线', path: '/pipelines', icon: 'pipeline', menu_type: 'menu', scope: 'platform', visible: true, sort_order: 220, keep_alive: true, version: 1, created_at: '' },
  { id: 203, uuid: '203', parent_id: 200, code: 'model', name: '大模型服务', path: '/inference', icon: 'inference', menu_type: 'menu', scope: 'platform', visible: true, sort_order: 230, keep_alive: true, version: 1, created_at: '' },
  { id: 2031, uuid: '2031', parent_id: 200, code: 'inference-services', name: '推理服务', path: '/inference/services', icon: 'inference', menu_type: 'menu', scope: 'platform', visible: true, sort_order: 235, keep_alive: true, version: 1, created_at: '' },
  { id: 2032, uuid: '2032', parent_id: 200, code: 'inference-routes', name: '推理路由', path: '/inference/routes', icon: 'inference', menu_type: 'menu', scope: 'platform', visible: true, sort_order: 238, keep_alive: true, version: 1, created_at: '' },
  { id: 205, uuid: '205', parent_id: 200, code: 'release', name: '发布中心', path: '/releases', icon: 'release', menu_type: 'menu', scope: 'platform', visible: true, sort_order: 250, keep_alive: true, version: 1, created_at: '' },
  { id: 206, uuid: '206', parent_id: 200, code: 'release-orch', name: '多集群发布', path: '/releases/orchestrations', icon: 'release', menu_type: 'menu', scope: 'platform', visible: true, sort_order: 260, keep_alive: true, version: 1, created_at: '' },
  { id: 207, uuid: '207', parent_id: 200, code: 'approvals', name: '发布审批', path: '/approvals', icon: 'approval', menu_type: 'menu', scope: 'platform', visible: true, sort_order: 270, keep_alive: true, version: 1, created_at: '' },

  // 集群运维
  { id: 300, uuid: '300', parent_id: 0, code: 'grp-cluster', name: '集群运维', path: '', icon: 'cluster', menu_type: 'directory', scope: 'platform', visible: true, sort_order: 300, keep_alive: false, version: 1, created_at: '' },
  { id: 301, uuid: '301', parent_id: 300, code: 'clusters', name: '集群管理', path: '/admin/clusters', icon: 'cluster', menu_type: 'menu', scope: 'platform', visible: true, sort_order: 310, keep_alive: true, version: 1, created_at: '' },
  { id: 302, uuid: '302', parent_id: 300, code: 'k8s_console', name: 'K8s 运维', path: '/k8s/workloads', icon: 'cluster', menu_type: 'menu', scope: 'platform', visible: true, sort_order: 320, keep_alive: true, version: 1, created_at: '' },
  { id: 303, uuid: '303', parent_id: 300, code: 'monitor', name: '容器监控', path: '/monitor', icon: 'monitor', menu_type: 'menu', scope: 'platform', visible: true, sort_order: 330, keep_alive: true, version: 1, created_at: '' },
  { id: 304, uuid: '304', parent_id: 300, code: 'alert', name: '告警中心', path: '/alerts', icon: 'alert', menu_type: 'menu', scope: 'platform', visible: true, sort_order: 340, keep_alive: true, version: 1, created_at: '' },

  // 运维工具
  { id: 500, uuid: '500', parent_id: 0, code: 'grp-ops-tools', name: '运维工具', path: '', icon: 'ops', menu_type: 'directory', scope: 'platform', visible: true, sort_order: 500, keep_alive: false, version: 1, created_at: '' },
  { id: 501, uuid: '501', parent_id: 500, code: 'ops-terminal', name: 'Pod 终端', path: '/ops/terminal', icon: 'ops-terminal', menu_type: 'menu', scope: 'platform', visible: true, sort_order: 510, keep_alive: true, version: 1, created_at: '' },
  { id: 502, uuid: '502', parent_id: 500, code: 'port-forward', name: '端口转发', path: '/ops/port-forward', icon: 'port-forward', menu_type: 'menu', scope: 'platform', visible: true, sort_order: 520, keep_alive: true, version: 1, created_at: '' },
  { id: 503, uuid: '503', parent_id: 500, code: 'ops-sessions', name: '运维会话', path: '/ops/sessions', icon: 'ops-sessions', menu_type: 'menu', scope: 'platform', visible: true, sort_order: 530, keep_alive: true, version: 1, created_at: '' },
  { id: 504, uuid: '504', parent_id: 500, code: 'ops-logs', name: '运维日志', path: '/ops/logs', icon: 'ops', menu_type: 'menu', scope: 'platform', visible: true, sort_order: 540, keep_alive: true, version: 1, created_at: '' },

  // 安全审计
  { id: 600, uuid: '600', parent_id: 0, code: 'grp-security', name: '安全审计', path: '', icon: 'audit', menu_type: 'directory', scope: 'platform', visible: true, sort_order: 600, keep_alive: false, version: 1, created_at: '' },
  { id: 601, uuid: '601', parent_id: 600, code: 'audit', name: '审计日志', path: '/audit', icon: 'audit', menu_type: 'menu', scope: 'platform', visible: true, sort_order: 610, keep_alive: true, version: 1, created_at: '' },
  { id: 602, uuid: '602', parent_id: 600, code: 'behavior-audit', name: '行为审计', path: '/audit/behavior', icon: 'behavior-audit', menu_type: 'menu', scope: 'platform', visible: true, sort_order: 620, keep_alive: true, version: 1, created_at: '' },

  // 系统管理
  { id: 700, uuid: '700', parent_id: 0, code: 'grp-admin', name: '系统管理', path: '', icon: 'setting', menu_type: 'directory', scope: 'platform', visible: true, sort_order: 700, keep_alive: false, version: 1, created_at: '' },
  { id: 701, uuid: '701', parent_id: 700, code: 'rbac', name: '权限管理', path: '/admin/roles', icon: 'rbac', menu_type: 'menu', scope: 'platform', visible: true, sort_order: 710, keep_alive: true, version: 1, created_at: '' },
  { id: 702, uuid: '702', parent_id: 700, code: 'admin_users', name: '用户管理', path: '/admin/users', icon: 'user', menu_type: 'menu', scope: 'platform', visible: true, sort_order: 720, keep_alive: true, version: 1, created_at: '' },
  { id: 703, uuid: '703', parent_id: 700, code: 'system_settings', name: '系统设置', path: '/admin/settings', icon: 'setting', menu_type: 'menu', scope: 'platform', visible: true, sort_order: 730, keep_alive: true, version: 1, created_at: '' },
  { id: 704, uuid: '704', parent_id: 700, code: 'base-images', name: '基础镜像', path: '/admin/base-images', icon: 'container', menu_type: 'menu', scope: 'platform', visible: true, sort_order: 740, keep_alive: true, version: 1, created_at: '' },
  { id: 705, uuid: '705', parent_id: 700, code: 'knowledge-base', name: 'AI 知识库', path: '/admin/knowledge-base', icon: 'book', menu_type: 'menu', scope: 'platform', visible: true, sort_order: 750, keep_alive: true, version: 1, created_at: '' },

  // AI 诊断：已迁移为「构建失败/启动失败」场景下的按钮入口 + 全局浮窗 AI 助手，
  // 不再作为独立菜单入口。详见 DiagnosisDrawer 与 AIAssistantWidget。

  // 个人
  { id: 800, uuid: '800', parent_id: 0, code: 'grp-personal', name: '个人', path: '', icon: 'user', menu_type: 'directory', scope: 'platform', visible: true, sort_order: 1000, keep_alive: false, version: 1, created_at: '' },
  { id: 801, uuid: '801', parent_id: 800, code: 'profile', name: '个人中心', path: '/me', icon: 'user', menu_type: 'menu', scope: 'platform', visible: true, sort_order: 810, keep_alive: true, version: 1, created_at: '' },
  { id: 802, uuid: '802', parent_id: 800, code: 'tokens', name: 'API Token', path: '/me/tokens', icon: 'token', menu_type: 'menu', scope: 'platform', visible: true, sort_order: 820, keep_alive: true, version: 1, created_at: '' },
];

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

  const source = menus.length > 0 ? menus : FALLBACK_ITEMS;
  const tree = buildMenuTree(source);
  const items = toAntdItems(tree);

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
