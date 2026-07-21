import { Layout } from 'antd';
import { Outlet } from 'react-router-dom';
import { DynamicMenu } from './DynamicMenu';
import { HeaderBar } from './HeaderBar';
import { AIAssistantWidget } from '@/components/AIAssistant/AIAssistantWidget';

const { Sider, Header, Content } = Layout;

export function MainLayout() {
  return (
    <Layout style={{ height: '100vh', overflow: 'hidden' }}>
      <Sider
        width={220}
        theme="dark"
        breakpoint="lg"
        collapsedWidth={64}
        style={{ height: '100vh', overflow: 'hidden' }}
      >
        <div
          style={{
            height: 56,
            minHeight: 56,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            color: '#fff',
            fontWeight: 700,
            fontSize: 18,
            letterSpacing: 1,
          }}
        >
          VortexOps
        </div>
        <div style={{ height: 'calc(100vh - 56px)', overflowY: 'auto', overflowX: 'hidden' }}>
          <DynamicMenu />
        </div>
      </Sider>
      <Layout style={{ height: '100vh', overflow: 'hidden' }}>
        <Header
          style={{
            background: '#fff',
            padding: 0,
            borderBottom: '1px solid #f0f0f0',
            height: 56,
            minHeight: 56,
            flex: '0 0 56px',
          }}
        >
          <HeaderBar />
        </Header>
        <Content style={{ background: '#f5f5f5', overflow: 'auto', minHeight: 0 }}>
          <Outlet />
        </Content>
      </Layout>
      {/* 全局 AI 助手浮窗：右下角按钮 + 抽屉式对话窗口，所有已登录页面可见。 */}
      <AIAssistantWidget />
    </Layout>
  );
}
