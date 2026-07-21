import { useEffect, useState } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Button, Dropdown, Input, Badge, Avatar, Space, Typography, Modal } from 'antd';
import {
  BellOutlined,
  MenuFoldOutlined,
  MenuUnfoldOutlined,
  UserOutlined,
  LogoutOutlined,
  SettingOutlined,
  SearchOutlined,
} from '@ant-design/icons';
import { useAuthStore } from '@/stores/authStore';
import { useUIStore } from '@/stores/uiStore';
import { workspaceApi } from '@/api/workspaces';
import { notificationApi } from '@/api/audit';

// 仅在空间相关页面（空间列表/详情、应用列表/详情、分组详情）展示空间切换器。
// 其它页面（构建、发布、运维、系统设置等）均为平台级，不应展示空间上下文。
function isWorkspaceContext(pathname: string): boolean {
  return (
    pathname === '/workspaces' ||
    pathname.startsWith('/workspaces/') ||
    pathname === '/applications' ||
    pathname.startsWith('/applications/') ||
    pathname.startsWith('/groups/')
  );
}

export function HeaderBar() {
  const navigate = useNavigate();
  const location = useLocation();
  const queryClient = useQueryClient();
  const toggleSider = useUIStore((s) => s.toggleSider);
  const siderCollapsed = useUIStore((s) => s.siderCollapsed);
  const currentWorkspace = useUIStore((s) => ({
    id: s.currentWorkspaceId,
    uuid: s.currentWorkspaceUuid,
    name: s.currentWorkspaceName,
  }));
  const setCurrentWorkspace = useUIStore((s) => s.setCurrentWorkspace);
  const user = useAuthStore((s) => s.user);
  const logout = useAuthStore((s) => s.logout);

  const showWorkspaceSwitcher = isWorkspaceContext(location.pathname);

  const { data: wsPage } = useQuery({
    queryKey: ['workspaces', 'header'],
    queryFn: () => workspaceApi.list({ page: 1, size: 50 }),
    staleTime: 60_000,
    // 仅在可能展示空间切换器时拉取，避免不必要的请求。
    enabled: showWorkspaceSwitcher,
  });

  const { data: unread } = useQuery({
    queryKey: ['notifications', 'unread-count'],
    queryFn: () => notificationApi.unreadCount(),
    refetchInterval: 60_000,
  });
  const unreadCount = (unread as any)?.count ?? 0;

  const markAllRead = useMutation({
    mutationFn: () => notificationApi.markAllRead(),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['notifications'] }),
  });

  const [paletteOpen, setPaletteOpen] = useState(false);

  // Ctrl+K 切换搜索面板；ESC 关闭。注意不要在面板已打开时被 onFocus 再次打开。
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && paletteOpen) {
        setPaletteOpen(false);
        return;
      }
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault();
        setPaletteOpen((v) => !v);
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [paletteOpen]);

  const userMenu = {
    items: [
      { key: 'profile', icon: <UserOutlined />, label: '个人中心', onClick: () => navigate('/me') },
      { key: 'tokens', icon: <SettingOutlined />, label: 'API Token', onClick: () => navigate('/me/tokens') },
      { type: 'divider' as const },
      {
        key: 'logout',
        icon: <LogoutOutlined />,
        label: '退出登录',
        onClick: async () => {
          await logout();
          navigate('/login');
        },
      },
    ],
  };

  return (
    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '0 16px', height: 56 }}>
      <Space>
        <Button type="text" icon={siderCollapsed ? <MenuUnfoldOutlined /> : <MenuFoldOutlined />} onClick={toggleSider} />
        {showWorkspaceSwitcher && (
          <Dropdown
            menu={{
              items: [
                ...(wsPage?.items || []).map((w) => ({
                  key: w.uuid,
                  label: w.display_name || w.name,
                  onClick: () => {
                    setCurrentWorkspace(w.id, w.uuid, w.display_name || w.name);
                    navigate(`/workspaces/${w.id}`);
                  },
                })),
              ],
            }}
          >
            <Button type="text">
              {currentWorkspace.name || '选择空间'}
            </Button>
          </Dropdown>
        )}
      </Space>

      <Space size={16}>
        {/* 用 Button 而非 readOnly Input 触发搜索面板，避免 Input onFocus 与 Modal 抢焦导致面板关不掉。 */}
        <Button
          type="text"
          icon={<SearchOutlined />}
          onClick={() => setPaletteOpen(true)}
        >
          搜索 (Ctrl+K)
        </Button>
        <Badge count={unreadCount} size="small">
          <Button
            type="text"
            icon={<BellOutlined />}
            onClick={() => {
              void markAllRead.mutateAsync();
              navigate('/notifications');
            }}
          />
        </Badge>
        <Dropdown menu={userMenu} placement="bottomRight">
          <Space style={{ cursor: 'pointer' }}>
            <Avatar size="small" icon={<UserOutlined />} src={user?.avatar_url} />
            <Typography.Text>{user?.display_name || user?.username}</Typography.Text>
          </Space>
        </Dropdown>
      </Space>

      <Modal
        open={paletteOpen}
        onCancel={() => setPaletteOpen(false)}
        footer={null}
        title="全局搜索"
        width={600}
        destroyOnClose
        maskClosable
      >
        <Input.Search
          placeholder="搜索空间/应用/构建..."
          enterButton
          autoFocus
          onSearch={(v) => {
            setPaletteOpen(false);
            navigate(`/search?q=${encodeURIComponent(v)}`);
          }}
        />
        <Typography.Text type="secondary" style={{ display: 'block', marginTop: 12, fontSize: 12 }}>
          搜索功能将跳转到全局搜索页。快捷键：Ctrl+K 切换，Esc 关闭
        </Typography.Text>
      </Modal>
    </div>
  );
}
