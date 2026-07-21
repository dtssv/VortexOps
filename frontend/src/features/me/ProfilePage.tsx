import { App, Button, Card, Descriptions, Form, Input, Modal, Select, Space, Switch, Tag, Alert, Typography } from 'antd';
import { KeyOutlined, SafetyOutlined, QrcodeOutlined, RobotOutlined } from '@ant-design/icons';
import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { PageContainer } from '@/components/PageContainer';
import { authApi } from '@/api/auth';
import { profileApi, type UserProfile } from '@/api/diagnosis';
import { useAuthStore } from '@/stores/authStore';
import { formatTime } from '@/utils/format';
import { confirmDanger } from '@/utils/action';

const { Text } = Typography;

export default function ProfilePage() {
  const { message } = App.useApp();
  const user = useAuthStore((s) => s.user);
  const fetchProfileAndMenus = useAuthStore((s) => s.fetchProfileAndMenus);
  const deleteAccount = useAuthStore((s) => s.reset);
  const queryClient = useQueryClient();
  const [open, setOpen] = useState(false);
  const [form] = Form.useForm();

  // MFA setup state
  const [mfaSetup, setMfaSetup] = useState<{ secret: string; otpauth_url: string; backup_codes: string[] } | null>(null);
  const [mfaVerifyCode, setMfaVerifyCode] = useState('');
  const [disableOpen, setDisableOpen] = useState(false);
  const [disableForm] = Form.useForm();

  const changePwdMutation = useMutation({
    mutationFn: (v: { old_password: string; new_password: string }) =>
      authApi.changePassword(v.old_password, v.new_password),
    onSuccess: () => {
      message.success('密码已修改');
      setOpen(false);
      form.resetFields();
    },
    onError: (e: any) => message.error(e?.message || '修改失败'),
  });

  const deleteMutation = useMutation({
    mutationFn: () => authApi.deleteAccount(),
    onSuccess: () => {
      message.success('账号已删除');
      deleteAccount();
    },
    onError: (e: any) => message.error(e?.message || '删除失败'),
  });

  const generateMFAMutation = useMutation({
    mutationFn: () => authApi.generateMFA(),
    onSuccess: (res) => {
      setMfaSetup(res);
      setMfaVerifyCode('');
    },
    onError: (e: any) => message.error(e?.message || '生成失败'),
  });

  const enableMFAMutation = useMutation({
    mutationFn: (code: string) => authApi.enableMFA(code),
    onSuccess: () => {
      message.success('MFA 已启用');
      setMfaSetup(null);
      setMfaVerifyCode('');
      fetchProfileAndMenus();
      queryClient.invalidateQueries({ queryKey: ['me'] });
    },
    onError: (e: any) => message.error(e?.message || '验证码错误'),
  });

  const disableMFAMutation = useMutation({
    mutationFn: (v: { code: string; use_password: boolean }) =>
      authApi.disableMFA(v.code, v.use_password),
    onSuccess: () => {
      message.success('MFA 已禁用');
      setDisableOpen(false);
      disableForm.resetFields();
      fetchProfileAndMenus();
      queryClient.invalidateQueries({ queryKey: ['me'] });
    },
    onError: (e: any) => message.error(e?.message || '禁用失败'),
  });

  return (
    <PageContainer title="个人中心" subtitle="查看并管理你的账户">
      <Card title="基本信息" style={{ marginBottom: 16 }}>
        <Descriptions bordered column={2} size="small">
          <Descriptions.Item label="用户名">{user?.username || '-'}</Descriptions.Item>
          <Descriptions.Item label="显示名称">{user?.display_name || '-'}</Descriptions.Item>
          <Descriptions.Item label="邮箱">{user?.email || '-'}</Descriptions.Item>
          <Descriptions.Item label="手机">{user?.phone || '-'}</Descriptions.Item>
          <Descriptions.Item label="UUID">{user?.uuid || '-'}</Descriptions.Item>
          <Descriptions.Item label="认证来源">
            <Tag>{user?.auth_source || '-'}</Tag>
          </Descriptions.Item>
          <Descriptions.Item label="状态">
            <Tag color={user?.status === 'active' ? 'success' : 'default'}>{user?.status || '-'}</Tag>
          </Descriptions.Item>
          <Descriptions.Item label="两步验证">
            <Tag color={user?.mfa_enabled ? 'green' : 'default'}>
              {user?.mfa_enabled ? '已启用' : '未启用'}
            </Tag>
          </Descriptions.Item>
          <Descriptions.Item label="语言">{user?.locale || '-'}</Descriptions.Item>
          <Descriptions.Item label="注册时间">{formatTime(user?.created_at)}</Descriptions.Item>
        </Descriptions>
        <Space style={{ marginTop: 16 }}>
          <Button type="primary" icon={<KeyOutlined />} onClick={() => setOpen(true)}>
            修改密码
          </Button>
          <Button
            danger
            onClick={() =>
              confirmDanger({
                title: '删除账号',
                content: '此操作不可逆，将永久删除你的账号及相关数据，确定吗？',
                okText: '确认删除',
                onOk: () => deleteMutation.mutateAsync(),
              })
            }
          >
            删除账号
          </Button>
        </Space>
      </Card>

      {/* AI 助手用户画像：基于多轮对话推断的用户特征，用于个性化回答 */}
      <AIProfileCard />

      {/* MFA 两步验证管理 */}
      <Card title="两步验证 (MFA)" style={{ marginBottom: 16 }}>
        {user?.mfa_enabled ? (
          <>
            <Alert
              type="success"
              showIcon
              icon={<SafetyOutlined />}
              message="两步验证已启用"
              description="登录时需要输入 TOTP 验证码或备份码。建议妥善保存备份码。"
              style={{ marginBottom: 16 }}
            />
            <Button danger icon={<SafetyOutlined />} onClick={() => setDisableOpen(true)}>
              禁用两步验证
            </Button>
          </>
        ) : mfaSetup ? (
          <>
            <Alert
              type="info"
              showIcon
              message="使用验证器应用扫描二维码"
              description={
                <span>
                  推荐使用 Google Authenticator、Microsoft Authenticator 或 1Password。若无法扫码，可手动输入密钥：
                  <Typography.Text code copyable>
                    {mfaSetup.secret}
                  </Typography.Text>
                </span>
              }
              style={{ marginBottom: 16 }}
            />
            <div style={{ textAlign: 'center', marginBottom: 16 }}>
              <img
                src={`https://api.qrserver.com/v1/create-qr-code/?size=200x200&data=${encodeURIComponent(mfaSetup.otpauth_url)}`}
                alt="TOTP QR Code"
                style={{ border: '1px solid #eee' }}
              />
            </div>
            <Alert
              type="warning"
              showIcon
              message="请保存以下备份码（每条仅可使用一次）"
              description={
                <Typography.Text code style={{ whiteSpace: 'pre-wrap' }}>
                  {mfaSetup.backup_codes.join('\n')}
                </Typography.Text>
              }
              style={{ marginBottom: 16 }}
            />
            <Space>
              <Input
                prefix={<QrcodeOutlined />}
                placeholder="输入验证器显示的 6 位验证码"
                style={{ width: 240 }}
                value={mfaVerifyCode}
                onChange={(e) => setMfaVerifyCode(e.target.value)}
              />
              <Button
                type="primary"
                loading={enableMFAMutation.isPending}
                onClick={() => enableMFAMutation.mutate(mfaVerifyCode.trim())}
              >
                确认启用
              </Button>
              <Button onClick={() => setMfaSetup(null)}>取消</Button>
            </Space>
          </>
        ) : (
          <>
            <Alert
              type="info"
              showIcon
              message="未启用两步验证"
              description="启用后，登录时除密码外还需输入 TOTP 验证码，显著提升账户安全性。"
              style={{ marginBottom: 16 }}
            />
            <Button type="primary" icon={<SafetyOutlined />} loading={generateMFAMutation.isPending} onClick={() => generateMFAMutation.mutate()}>
              开始启用
            </Button>
          </>
        )}
      </Card>

      {open && (
        <Card title="修改密码">
          <Form
            layout="vertical"
            form={form}
            onFinish={(v) => changePwdMutation.mutate(v)}
            style={{ maxWidth: 480 }}
          >
            <Form.Item name="old_password" label="当前密码" rules={[{ required: true, message: '请输入当前密码' }]}>
              <Input.Password placeholder="当前密码" />
            </Form.Item>
            <Form.Item name="new_password" label="新密码" rules={[{ required: true, message: '请输入新密码' }, { min: 8, message: '密码至少 8 位' }]}>
              <Input.Password placeholder="新密码" />
            </Form.Item>
            <Form.Item
              name="confirm"
              label="确认新密码"
              dependencies={['new_password']}
              rules={[
                { required: true, message: '请确认新密码' },
                ({ getFieldValue }) => ({
                  validator(_, value) {
                    if (!value || getFieldValue('new_password') === value) return Promise.resolve();
                    return Promise.reject(new Error('两次输入的密码不一致'));
                  },
                }),
              ]}
            >
              <Input.Password placeholder="确认新密码" />
            </Form.Item>
            <Space>
              <Button type="primary" htmlType="submit" loading={changePwdMutation.isPending}>
                确认修改
              </Button>
              <Button onClick={() => { setOpen(false); form.resetFields(); }}>取消</Button>
            </Space>
          </Form>
        </Card>
      )}

      <Modal
        title="禁用两步验证"
        open={disableOpen}
        onCancel={() => { setDisableOpen(false); disableForm.resetFields(); }}
        onOk={() => disableForm.submit()}
        confirmLoading={disableMFAMutation.isPending}
        destroyOnClose
      >
        <Form
          form={disableForm}
          layout="vertical"
          initialValues={{ use_password: false }}
          onFinish={(v) => disableMFAMutation.mutate({ code: v.code, use_password: v.use_password })}
        >
          <Alert
            type="warning"
            showIcon
            message="禁用后账户安全性将降低"
            description="请输入 TOTP 验证码、备份码，或切换为账户密码验证。"
            style={{ marginBottom: 16 }}
          />
          <Form.Item name="use_password" label="使用账户密码验证" valuePropName="checked">
            <Switch checkedChildren="密码" unCheckedChildren="验证码" />
          </Form.Item>
          <Form.Item
            name="code"
            label="验证码 / 密码"
            rules={[{ required: true, message: '请输入验证码或密码' }]}
          >
            <Input.Password placeholder="TOTP 验证码 / 备份码 / 账户密码" />
          </Form.Item>
        </Form>
      </Modal>
    </PageContainer>
  );
}

// AIProfileCard 展示 AI 助手通过多轮对话推断的用户画像。
// 用户可手动调整画像字段（覆盖 AI 推断结果）。
function AIProfileCard() {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const [editOpen, setEditOpen] = useState(false);
  const [form] = Form.useForm();

  const { data: profile, isLoading } = useQuery({
    queryKey: ['ai-profile'],
    queryFn: () => profileApi.get(),
    staleTime: 60_000,
  });

  const updateMutation = useMutation({
    mutationFn: (v: Partial<UserProfile>) => profileApi.update(v),
    onSuccess: () => {
      message.success('画像已更新');
      setEditOpen(false);
      queryClient.invalidateQueries({ queryKey: ['ai-profile'] });
    },
    onError: (e: any) => message.error(e?.message || '更新失败'),
  });

  const levelMap: Record<string, { label: string; color: string }> = {
    beginner: { label: '初学者', color: 'default' },
    intermediate: { label: '中级', color: 'blue' },
    advanced: { label: '高级', color: 'gold' },
    expert: { label: '资深', color: 'purple' },
    unknown: { label: '未知', color: 'default' },
  };
  const levelInfo = levelMap[profile?.expertise_level || 'unknown'] || levelMap.unknown;

  return (
    <Card
      title={
        <Space>
          <RobotOutlined style={{ color: '#1677ff' }} />
          <span>AI 助手用户画像</span>
        </Space>
      }
      extra={
        <Button size="small" onClick={() => {
          form.setFieldsValue({
            expertise_level: profile?.expertise_level || 'unknown',
            roles: profile?.roles || [],
            domains: profile?.domains || [],
            communication_style: profile?.communication_style || 'balanced',
            summary: profile?.summary || '',
          });
          setEditOpen(true);
        }}>
          编辑
        </Button>
      }
      style={{ marginBottom: 16 }}
    >
      {isLoading ? (
        <Text type="secondary">加载中…</Text>
      ) : (
        <>
          <Alert
            type="info"
            showIcon
            style={{ marginBottom: 12 }}
            message="AI 助手会根据你的对话内容持续学习用户特征（角色、擅长领域、专业水平），用于个性化回答。"
            description="画像在多轮对话中自动更新；你也可以手动调整。"
          />
          <Descriptions bordered column={2} size="small">
            <Descriptions.Item label="专业水平">
              <Tag color={levelInfo.color}>{levelInfo.label}</Tag>
            </Descriptions.Item>
            <Descriptions.Item label="沟通偏好">
              <Tag>{profile?.communication_style || 'balanced'}</Tag>
            </Descriptions.Item>
            <Descriptions.Item label="角色" span={2}>
              {(profile?.roles || []).length > 0 ? (
                <Space wrap>
                  {profile!.roles.map((r) => <Tag key={r} color="cyan">{r}</Tag>)}
                </Space>
              ) : <Text type="secondary">暂无</Text>}
            </Descriptions.Item>
            <Descriptions.Item label="擅长领域" span={2}>
              {(profile?.domains || []).length > 0 ? (
                <Space wrap>
                  {profile!.domains.map((d) => <Tag key={d} color="geekblue">{d}</Tag>)}
                </Space>
              ) : <Text type="secondary">暂无</Text>}
            </Descriptions.Item>
            <Descriptions.Item label="画像摘要" span={2}>
              {profile?.summary || <Text type="secondary">尚无摘要，多与 AI 助手对话后将自动生成</Text>}
            </Descriptions.Item>
            <Descriptions.Item label="累计对话轮次">
              {profile?.interaction_count ?? 0}
            </Descriptions.Item>
            <Descriptions.Item label="最近更新">
              {formatTime(profile?.last_updated_at)}
            </Descriptions.Item>
          </Descriptions>
        </>
      )}

      <Modal
        open={editOpen}
        title="编辑 AI 用户画像"
        onCancel={() => setEditOpen(false)}
        onOk={() => form.submit()}
        confirmLoading={updateMutation.isPending}
        destroyOnClose
      >
        <Form form={form} layout="vertical" onFinish={(v) => updateMutation.mutate(v)}>
          <Form.Item name="expertise_level" label="专业水平">
            <Select
              options={[
                { label: '初学者', value: 'beginner' },
                { label: '中级', value: 'intermediate' },
                { label: '高级', value: 'advanced' },
                { label: '资深', value: 'expert' },
                { label: '未知', value: 'unknown' },
              ]}
            />
          </Form.Item>
          <Form.Item name="roles" label="角色（如 java_engineer / sre / devops）">
            <Select mode="tags" placeholder="输入角色后回车" tokenSeparators={[',']} />
          </Form.Item>
          <Form.Item name="domains" label="擅长领域（如 kubernetes / spring / redis）">
            <Select mode="tags" placeholder="输入领域后回车" tokenSeparators={[',']} />
          </Form.Item>
          <Form.Item name="communication_style" label="沟通偏好">
            <Select
              options={[
                { label: '简洁', value: 'concise' },
                { label: '详细', value: 'detailed' },
                { label: '平衡', value: 'balanced' },
              ]}
            />
          </Form.Item>
          <Form.Item name="summary" label="画像摘要">
            <Input.TextArea rows={2} placeholder="一句话描述用户特征，如「资深 Java 工程师，擅长分布式系统」" />
          </Form.Item>
        </Form>
      </Modal>
    </Card>
  );
}
