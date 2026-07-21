import { Card, Col, Row, Statistic, Typography, List, Tag, Empty } from 'antd';
import {
  AppstoreOutlined,
  BuildOutlined,
  CloudUploadOutlined,
  TeamOutlined,
  CheckCircleOutlined,
  ExclamationCircleOutlined,
} from '@ant-design/icons';
import { useQuery } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';
import { PageContainer } from '@/components/PageContainer';
import { ResourceStatus } from '@/components/ResourceStatus';
import { workspaceApi } from '@/api/workspaces';
import { useAuthStore } from '@/stores/authStore';
import { formatRelative } from '@/utils/format';

export default function DashboardPage() {
  const navigate = useNavigate();
  const user = useAuthStore((s) => s.user);

  const { data: wsPage } = useQuery({
    queryKey: ['workspaces', 'dashboard'],
    queryFn: () => workspaceApi.list({ page: 1, size: 5 }),
  });

  return (
    <PageContainer
      title={`欢迎，${user?.display_name || user?.username || ''}`}
      subtitle="在这里查看你的工作概览"
    >
      <Row gutter={16} style={{ marginBottom: 16 }}>
        <Col span={6}>
          <Card>
            <Statistic title="我的空间" value={wsPage?.total ?? 0} prefix={<TeamOutlined />} />
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Statistic title="进行中构建" value={0} prefix={<BuildOutlined />} valueStyle={{ color: '#1677ff' }} />
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Statistic title="进行中发布" value={0} prefix={<CloudUploadOutlined />} valueStyle={{ color: '#1677ff' }} />
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Statistic title="待审批" value={0} prefix={<ExclamationCircleOutlined />} valueStyle={{ color: '#faad14' }} />
          </Card>
        </Col>
      </Row>

      <Row gutter={16}>
        <Col span={12}>
          <Card title="最近空间" extra={<a onClick={() => navigate('/workspaces')}>全部</a>}>
            {wsPage?.items?.length ? (
              <List
                dataSource={wsPage.items}
                renderItem={(ws) => (
                  <List.Item
                    actions={[<ResourceStatus status={ws.status} />]}
                    onClick={() => navigate(`/workspaces/${ws.id}`)}
                    style={{ cursor: 'pointer' }}
                  >
                    <List.Item.Meta
                      avatar={<AppstoreOutlined style={{ fontSize: 24, color: '#1677ff' }} />}
                      title={ws.display_name || ws.name}
                      description={`${ws.application_count ?? 0} 应用 · ${ws.group_count ?? 0} 分组 · ${formatRelative(ws.updated_at)}`}
                    />
                  </List.Item>
                )}
              />
            ) : (
              <Empty description="暂无空间，点击左侧「空间」创建" />
            )}
          </Card>
        </Col>
        <Col span={12}>
          <Card title="快捷入口">
            <Row gutter={[16, 16]}>
              {[
                { title: '新建空间', icon: <TeamOutlined />, path: '/workspaces', color: '#1677ff' },
                { title: '构建中心', icon: <BuildOutlined />, path: '/builds', color: '#52c41a' },
                { title: '发布中心', icon: <CloudUploadOutlined />, path: '/releases', color: '#722ed1' },
                { title: '配置管理', icon: <CheckCircleOutlined />, path: '/configs', color: '#fa8c16' },
              ].map((item) => (
                <Col span={12} key={item.title}>
                  <Card
                    hoverable
                    size="small"
                    onClick={() => navigate(item.path)}
                    style={{ textAlign: 'center' }}
                  >
                    <div style={{ fontSize: 28, color: item.color, marginBottom: 8 }}>{item.icon}</div>
                    <Typography.Text>{item.title}</Typography.Text>
                  </Card>
                </Col>
              ))}
            </Row>
          </Card>
        </Col>
      </Row>
    </PageContainer>
  );
}
