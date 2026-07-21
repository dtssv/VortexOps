import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Card, Form, Input, Button, Typography, App, Space, Divider } from 'antd';
import { LockOutlined, UserOutlined, SafetyOutlined } from '@ant-design/icons';
import { useQuery } from '@tanstack/react-query';
import { useAuthStore } from '@/stores/authStore';
import { authApi } from '@/api/auth';

type LoginProvider = {
  code: string;
  source: string;
  display_name: string;
  is_external: boolean;
  is_default: boolean;
};

export default function LoginPage() {
  const navigate = useNavigate();
  const { message } = App.useApp();
  const login = useAuthStore((s) => s.login);
  const loginWithMFA = useAuthStore((s) => s.loginWithMFA);
  const clearMFAChallenge = useAuthStore((s) => s.clearMFAChallenge);
  const accessToken = useAuthStore((s) => s.accessToken);
  const loading = useAuthStore((s) => s.loading);
  const mfaChallenge = useAuthStore((s) => s.mfaChallenge);
  const [mfaCode, setMfaCode] = useState('');

  // 拉取平台启用的登录方式（扩展点）。后端 GET /auth/providers 公开返回。
  const { data: providers } = useQuery<LoginProvider[]>({
    queryKey: ['login-providers'],
    queryFn: () => authApi.listLoginProviders(),
  });
  const externalProviders = (providers || []).filter((p) => p.is_external);
  const defaultProvider = (providers || []).find((p) => p.is_default);

  useEffect(() => {
    if (accessToken) navigate('/', { replace: true });
  }, [accessToken, navigate]);

  const onLogin = async (values: { username: string; password: string }) => {
    try {
      await login(values.username, values.password);
      message.success('登录成功');
      navigate('/', { replace: true });
    } catch (e: any) {
      if (e?.message === 'MFA_REQUIRED') {
        // MFA challenge 已写入 store，下方 UI 自动切换到验证码输入。
        return;
      }
      message.error(e?.message || '登录失败');
    }
  };

  const onLoginMFA = async () => {
    if (!mfaCode.trim()) {
      message.warning('请输入验证码');
      return;
    }
    try {
      await loginWithMFA(mfaCode.trim());
      message.success('登录成功');
      navigate('/', { replace: true });
    } catch (e: any) {
      message.error(e?.message || '验证码错误');
    }
  };

  const onExternalLogin = (p: LoginProvider) => {
    // 扩展点：此处应跳转到对应 IdP 的授权端点（OIDC authorize URL / LDAP 登录页）。
    // 当前仅注册了 local provider，外部 provider 的回调 handler 待后端实现后接入。
    message.info(`${p.display_name} 登录尚未启用，请联系管理员配置`);
  };

  return (
    <div
      style={{
        minHeight: '100vh',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        background: 'linear-gradient(135deg, #1677ff 0%, #0958d9 100%)',
      }}
    >
      <Card style={{ width: 400, boxShadow: '0 8px 24px rgba(0,0,0,0.15)' }}>
        <div style={{ textAlign: 'center', marginBottom: 24 }}>
          <Typography.Title level={3} style={{ margin: 0, color: '#1677ff' }}>
            VortexOps
          </Typography.Title>
          <Typography.Text type="secondary">Kubernetes 应用管理平台</Typography.Text>
        </div>

        {mfaChallenge ? (
          <>
            <Typography.Paragraph style={{ textAlign: 'center' }}>
              <SafetyOutlined style={{ fontSize: 32, color: '#1677ff' }} />
              <br />
              请输入两步验证码
              <br />
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                用户：{mfaChallenge.username}
              </Typography.Text>
            </Typography.Paragraph>
            <Form layout="vertical">
              <Form.Item label="验证码" required>
                <Input
                  prefix={<SafetyOutlined />}
                  placeholder="6 位 TOTP 码 或 备份码"
                  size="large"
                  value={mfaCode}
                  onChange={(e) => setMfaCode(e.target.value)}
                  onPressEnter={onLoginMFA}
                  autoFocus
                />
              </Form.Item>
              <Form.Item>
                <Space style={{ width: '100%', justifyContent: 'space-between' }}>
                  <Button
                    type="link"
                    onClick={() => {
                      clearMFAChallenge();
                      setMfaCode('');
                    }}
                  >
                    返回登录
                  </Button>
                  <Button type="primary" size="large" loading={loading} onClick={onLoginMFA}>
                    验证并登录
                  </Button>
                </Space>
              </Form.Item>
            </Form>
          </>
        ) : (
          <>
            <Form layout="vertical" onFinish={onLogin} autoComplete="off">
              <Form.Item name="username" label="用户名" rules={[{ required: true, message: '请输入用户名' }]}>
                <Input prefix={<UserOutlined />} placeholder="用户名" size="large" />
              </Form.Item>
              <Form.Item name="password" label="密码" rules={[{ required: true, message: '请输入密码' }]}>
                <Input.Password prefix={<LockOutlined />} placeholder="密码" size="large" />
              </Form.Item>
              <Form.Item>
                <Button type="primary" htmlType="submit" block size="large" loading={loading}>
                  登录
                </Button>
              </Form.Item>
            </Form>
            {externalProviders.length > 0 && (
              <>
                <Divider style={{ margin: '12px 0', fontSize: 12 }}>其他登录方式</Divider>
                <Space direction="vertical" style={{ width: '100%' }}>
                  {externalProviders.map((p) => (
                    <Button key={p.code} block size="large" onClick={() => onExternalLogin(p)}>
                      {p.display_name}
                    </Button>
                  ))}
                </Space>
              </>
            )}
            {defaultProvider && (
              <Typography.Text type="secondary" style={{ display: 'block', textAlign: 'center', fontSize: 12, marginTop: 12 }}>
                登录方式：{defaultProvider.display_name}
              </Typography.Text>
            )}
          </>
        )}
      </Card>
    </div>
  );
}
