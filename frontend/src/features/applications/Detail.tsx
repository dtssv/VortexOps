import { useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate, useParams, useSearchParams } from 'react-router-dom';
import { App, Alert, Button, Card, Descriptions, Form, Input, InputNumber, Modal, Radio, Select, Space, Switch, Table, Tabs, Tag, Typography } from 'antd';
import { DeleteOutlined, DiffOutlined, HistoryOutlined, PlusOutlined, ReloadOutlined, DeploymentUnitOutlined } from '@ant-design/icons';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { ColumnsType } from 'antd/es/table';
import { PageContainer } from '@/components/PageContainer';
import { BreadcrumbSwitcher } from '@/components/BreadcrumbSwitcher';
import { ResourceStatus } from '@/components/ResourceStatus';
import { EmptyState } from '@/components/EmptyState';
import { PublishModal } from '@/components/PublishModal';
import { ConfigContentEditor, ConfigContentPreview, buildConfigContentFromForm, parseConfigContent, populateFormFromContent } from '@/components/ConfigContentEditor';
import { DiffViewer } from '@/components/DiffViewer';
import { applicationApi, groupApi, type CreateApplicationInput } from '@/api/applications';
import { buildApi, LANGUAGE_OPTIONS } from '@/api/builds';
import { configApi } from '@/api/configs';
import { rbacApi } from '@/api/rbac';
import { workspaceApi } from '@/api/workspaces';
import { releaseApi } from '@/api/releases';
import { PermissionGate } from '@/components/PermissionGate';
import { usePermission } from '@/hooks/usePermission';
import { confirmDanger } from '@/utils/action';
import { formatRelative, formatDuration, formatTime, shortSha, formatBytes } from '@/utils/format';
import type { Application, ApplicationMember, BaseImage, Build, BuildTool, ConfigSet, ConfigContentSnapshot, GitRef, Group, Image, ProbeConfig, Release, WorkspaceMember } from '@/types';
import { isExternalManaged } from '@/types';

export default function ApplicationDetailPage() {
  const params = useParams();
  const appId = Number(params.appId);
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const canBuild = usePermission('build:trigger').can;
  const canRelease = usePermission('release:trigger').can;
  const canViewRelease = usePermission('menu:release:view').can;
  const canViewConfig = usePermission('menu:config:view').can;
  const canManageApp = usePermission('application:manage').can;

  const { data: app, isLoading } = useQuery({
    queryKey: ['application', appId],
    queryFn: () => applicationApi.get(appId),
    enabled: !!appId,
  });

  const workspaceId = app?.workspace_id;
  const { data: ws } = useQuery({
    queryKey: ['workspace', workspaceId],
    queryFn: () => workspaceApi.get(workspaceId!),
    enabled: !!workspaceId,
  });

  const externalManaged = isExternalManaged(app);

  const tabItems = [
    { key: 'groups', label: '分组', children: <GroupsTab appId={appId} readOnly={externalManaged || !canManageApp} /> },
    ...(!externalManaged && (canBuild || canViewRelease)
      ? [{ key: 'images', label: '镜像', children: <ImagesTab appId={appId} canRelease={canRelease} canManage={canManageApp} /> }]
      : []),
    ...(!externalManaged && canBuild
      ? [{ key: 'builds', label: '构建', children: <BuildsTab appId={appId} canRelease={canRelease} /> }]
      : []),
    ...(!externalManaged && canViewRelease
      ? [{ key: 'releases', label: '发布', children: <ReleasesTab appId={appId} canRelease={canRelease} /> }]
      : []),
    ...(!externalManaged && canManageApp
      ? [{ key: 'git', label: 'Git 源', children: <GitTab appId={appId} /> }]
      : []),
    ...(canViewConfig
      ? [{ key: 'configs', label: '配置', children: <ConfigsTab appId={appId} readOnly={externalManaged || !canManageApp} /> }]
      : []),
    ...(canManageApp
      ? [
          { key: 'members', label: '成员', children: <MembersTab appId={appId} ownerId={app?.owner_id} /> },
          { key: 'settings', label: '设置', children: <SettingsTab app={app} readOnly={externalManaged} /> },
        ]
      : []),
  ];
  const allowedTabKeys = tabItems.map((t) => t.key);
  const activeTab = allowedTabKeys.includes(searchParams.get('tab') || '')
    ? (searchParams.get('tab') as string)
    : 'groups';
  const onTabChange = (key: string) => {
    const next = new URLSearchParams(searchParams);
    next.set('tab', key);
    setSearchParams(next, { replace: true });
  };

  return (
    <PageContainer
      title={app?.display_name || app?.name || '应用'}
      subtitle={app?.description}
      breadcrumb={[
        { title: '空间', path: '/workspaces' },
        {
          switcher: (
            <BreadcrumbSwitcher
              currentLabel={ws?.display_name || ws?.name}
              currentValue={workspaceId}
              currentPath={workspaceId ? `/workspaces/${workspaceId}` : undefined}
              queryKeyPrefix={['workspaces']}
              loadOptions={(search) =>
                workspaceApi
                  .list({ search: search || undefined, page: 1, size: 50 })
                  .then((p) =>
                    p.items.map((w) => ({
                      label: w.display_name || w.name,
                      value: w.id,
                      path: `/workspaces/${w.id}`,
                    })),
                  )
              }
            />
          ),
        },
        {
          switcher: (
            <BreadcrumbSwitcher
              currentLabel={app?.display_name || app?.name}
              currentValue={appId}
              currentPath={appId ? `/applications/${appId}` : undefined}
              queryKeyPrefix={['applications', 'ws', workspaceId ?? 0]}
              loadOptions={(search) =>
                workspaceId
                  ? applicationApi
                      .list(workspaceId, { search: search || undefined, page: 1, size: 50 })
                      .then((p) =>
                        p.items.map((a) => ({
                          label: a.display_name || a.name,
                          value: a.id,
                          path: `/applications/${a.id}`,
                        })),
                      )
                  : Promise.resolve([])
              }
            />
          ),
        },
      ]}
    >
      {externalManaged && (
        <Alert
          type="info"
          showIcon
          style={{ marginBottom: 16 }}
          message="此应用由外部 API 管理（中间件团队），构建/镜像/发布等操作请通过开放 API 进行。"
        />
      )}
      <Card loading={isLoading}>
        <Tabs activeKey={activeTab} onChange={onTabChange} items={tabItems} />
      </Card>
    </PageContainer>
  );

  function GroupsTab({ appId, readOnly }: { appId: number; readOnly?: boolean }) {
    const { message } = App.useApp();
    const queryClient = useQueryClient();

    const { data, isLoading } = useQuery({
      queryKey: ['application', appId, 'groups'],
      queryFn: () => groupApi.list(appId),
    });

    const deleteMutation = useMutation({
      mutationFn: (id: number) => groupApi.delete(id),
      onSuccess: () => {
        message.success('分组已删除');
        queryClient.invalidateQueries({ queryKey: ['application', appId, 'groups'] });
      },
      onError: (e: any) => message.error(e?.message || '删除失败'),
    });

    const columns: ColumnsType<Group> = [
      {
        title: '名称',
        dataIndex: 'name',
        render: (_, r) => (
          <a onClick={() => navigate(`/groups/${r.id}`)}>{r.display_name || r.name}</a>
        ),
      },
      { title: '环境', dataIndex: 'environment', width: 120 },
      { title: '集群', dataIndex: 'cluster_name', width: 140 },
      { title: '命名空间', dataIndex: 'namespace', width: 160 },
      { title: '副本', dataIndex: ['workload', 'replicas'], width: 80, align: 'center' },
      {
        title: '状态',
        dataIndex: 'status',
        width: 100,
        render: (s?: string) => (s ? <ResourceStatus status={s} /> : '-'),
      },
      ...(!readOnly
        ? [
            {
              title: '操作',
              key: 'actions',
              width: 120,
              render: (_: unknown, r: Group) => (
                <Button
                  type="link"
                  size="small"
                  danger
                  icon={<DeleteOutlined />}
                  onClick={() =>
                    confirmDanger({
                      title: '删除分组',
                      content: `确定删除分组「${r.display_name || r.name}」吗？删除后该分组下的发布历史将一并清除，此操作不可恢复。`,
                      okText: '删除',
                      onOk: () => deleteMutation.mutateAsync(r.id),
                    })
                  }
                >
                  删除
                </Button>
              ),
            },
          ]
        : []),
    ];

    return (
      <>
        {!readOnly && (
          <div style={{ marginBottom: 16, textAlign: 'right' }}>
            <Button type="primary" icon={<PlusOutlined />} onClick={() => navigate(`/groups/new?appId=${appId}`)}>
              新建分组
            </Button>
          </div>
        )}
        <Table<Group>
          rowKey="id"
          loading={isLoading}
          columns={columns}
          dataSource={data?.items}
          pagination={false}
          locale={{
            emptyText: (
              <EmptyState
                title="暂无分组"
                actionText={readOnly ? undefined : '新建分组'}
                onAction={readOnly ? undefined : () => navigate(`/groups/new?appId=${appId}`)}
              />
            ),
          }}
        />
      </>
    );
  }

  function ImagesTab({ appId, canRelease, canManage }: { appId: number; canRelease: boolean; canManage: boolean }) {
    const { message } = App.useApp();
    const queryClient = useQueryClient();
    const [tagOpen, setTagOpen] = useState(false);
    const [tagForm] = Form.useForm<{ image_id: number; tag: string; alias?: string }>();
    const [tagTarget, setTagTarget] = useState<Image | undefined>();
    // 发布：选任意镜像发布到分组。
    const [publishOpen, setPublishOpen] = useState(false);
    const [publishImage, setPublishImage] = useState<Image | undefined>();

    const { data: imagesPage, isLoading } = useQuery({
      queryKey: ['application', appId, 'images'],
      queryFn: () => buildApi.listImages(appId, { page: 1, size: 200 }),
      enabled: !!appId,
    });
    const images = imagesPage?.items;

    const { data: tags } = useQuery({
      queryKey: ['application', appId, 'image-tags'],
      queryFn: () => buildApi.listImageTags(appId),
      enabled: !!appId,
    });

    const retireMutation = useMutation({
      mutationFn: (id: number) => buildApi.retireImage(id),
      onSuccess: () => {
        message.success('镜像已标记为退役');
        queryClient.invalidateQueries({ queryKey: ['application', appId, 'images'] });
      },
      onError: (e: any) => message.error(e?.message || '操作失败'),
    });

    const tagMutation = useMutation({
      mutationFn: (v: { image_id: number; tag: string; alias?: string }) =>
        buildApi.createImageTag(appId, { image_id: v.image_id, tag: v.tag, alias: v.alias }),
      onSuccess: () => {
        message.success('标签已创建');
        setTagOpen(false);
        tagForm.resetFields();
        queryClient.invalidateQueries({ queryKey: ['application', appId, 'image-tags'] });
      },
      onError: (e: any) => message.error(e?.message || '创建失败'),
    });

    const columns: ColumnsType<Image> = [
      {
        title: '仓库',
        dataIndex: 'repository',
        render: (v: string) => <Typography.Text code>{v}</Typography.Text>,
      },
      {
        title: '标签',
        dataIndex: 'tag',
        width: 180,
        render: (v: string, r) => {
          const extra = (tags || []).filter((t) => t.image_id === r.id).map((t) => t.tag);
          return (
            <Space direction="vertical" size={0}>
              <Typography.Text strong>{v}</Typography.Text>
              {extra.length > 0 && (
                <Space size={4} wrap>
                  {extra.map((t) => <Tag key={t} color="blue">{t}</Tag>)}
                </Space>
              )}
            </Space>
          );
        },
      },
      {
        title: 'Digest',
        dataIndex: 'digest',
        width: 180,
        ellipsis: true,
        render: (v?: string) => v ? <Typography.Text code>{shortSha(v)}</Typography.Text> : '-',
      },
      {
        title: '大小',
        dataIndex: 'size_bytes',
        width: 100,
        align: 'right',
        render: (v?: number) => (v ? formatBytes(v) : '-'),
      },
      {
        title: '构建',
        dataIndex: 'build_id',
        width: 90,
        render: (id?: number) => (id ? <a onClick={() => navigate(`/builds/${id}`)}>#{id}</a> : '-'),
      },
      {
        title: 'Commit',
        dataIndex: 'git_commit',
        width: 110,
        render: (v?: string) => (v ? <Typography.Text code>{shortSha(v)}</Typography.Text> : '-'),
      },
      {
        title: '扫描',
        dataIndex: 'scan_status',
        width: 100,
        render: (s?: string) => (s ? <Tag color={s === 'clean' ? 'green' : s === 'failed' ? 'red' : 'default'}>{s}</Tag> : '-'),
      },
      {
        title: '状态',
        dataIndex: 'retired',
        width: 90,
        render: (r?: boolean) => (r ? <Tag>已退役</Tag> : <Tag color="green">可用</Tag>),
      },
      { title: '时间', dataIndex: 'created_at', width: 150, render: formatRelative },
      {
        title: '操作',
        key: 'actions',
        width: 200,
        render: (_, r) => (
          <Space>
            {canManage && (
              <Button
                type="link"
                size="small"
                disabled={r.retired}
                onClick={() => {
                  setTagTarget(r);
                  tagForm.setFieldsValue({ image_id: r.id });
                  setTagOpen(true);
                }}
              >
                打标签
              </Button>
            )}
            {canRelease && (
              <Button
                type="link"
                size="small"
                disabled={r.retired}
                onClick={() => {
                  setPublishImage(r);
                  setPublishOpen(true);
                }}
              >
                发布
              </Button>
            )}
            {canManage && (
            <Button
              type="link"
              size="small"
              danger
              disabled={r.retired}
              onClick={() =>
                confirmDanger({
                  title: '退役镜像',
                  content: `确认将镜像 ${r.repository}:${r.tag} 标记为退役？退役后不可用于发布。`,
                  onOk: () => retireMutation.mutateAsync(r.id),
                })
              }
            >
              退役
            </Button>
            )}
          </Space>
        ),
      },
    ];

    return (
      <>
        <Table<Image>
          rowKey="id"
          loading={isLoading}
          columns={columns}
          dataSource={images}
          pagination={false}
          locale={{ emptyText: <EmptyState title="暂无镜像" description="完成构建后将自动产生镜像" /> }}
        />
        <Modal
          title="创建标签"
          open={tagOpen}
          onCancel={() => setTagOpen(false)}
          onOk={() => tagForm.submit()}
          confirmLoading={tagMutation.isPending}
          destroyOnHidden
        >
          <Form layout="vertical" form={tagForm} onFinish={(v) => tagMutation.mutate(v)}>
            <Form.Item label="镜像">
              <Input value={tagTarget ? `${tagTarget.repository}:${tagTarget.tag}` : ''} disabled />
            </Form.Item>
            <Form.Item name="image_id" hidden><Input /></Form.Item>
            <Form.Item
              name="tag"
              label="标签名"
              rules={[{ required: true, message: '请输入标签名' }]}
            >
              <Input placeholder="例如 v1.2.0 / stable" />
            </Form.Item>
            <Form.Item name="alias" label="别名（可选）">
              <Input placeholder="例如 latest" />
            </Form.Item>
          </Form>
        </Modal>
        <PublishModal
          open={publishOpen}
          onClose={() => setPublishOpen(false)}
          applicationId={appId}
          fixedImageId={publishImage?.id}
          onPublished={(relId) => navigate(`/releases/${relId}`)}
        />
      </>
    );
  }

  function BuildsTab({ appId, canRelease }: { appId: number; canRelease: boolean }) {
    const { message } = App.useApp();
    const queryClient = useQueryClient();
    const [triggerOpen, setTriggerOpen] = useState(false);
    // editOpen 提前声明：baseImages 查询的 enabled 依赖它，需在此前定义。
    const [editOpen, setEditOpen] = useState(false);
    const [branchSearch, setBranchSearch] = useState('');
    const [commit, setCommit] = useState<{ sha?: string; message?: string } | null>(null);
    // 发布：成功构建可发布其产物镜像。
    const [publishOpen, setPublishOpen] = useState(false);
    const [publishImageId, setPublishImageId] = useState<number | undefined>();
    const [form] = Form.useForm<{
      ref_value: string;
      commit_sha?: string;
      commit_message?: string;
      target_tag?: string;
      build_mode: 'baseImage' | 'custom';
      base_image_id?: number;
      build_tool?: string;
      build_command?: string;
      artifact_path?: string;
      build_args?: string;
      custom_repo_url?: string;
      custom_branch?: string;
      dockerfile_path?: string;
    }>();

    const { data, isLoading } = useQuery({
      queryKey: ['application', appId, 'builds'],
      queryFn: () => buildApi.list(appId, { page: 1, size: 20 }),
    });

    const { data: app } = useQuery({
      queryKey: ['application', appId],
      queryFn: () => applicationApi.get(appId),
      enabled: !!appId,
    });

    // 系统变量化构建集成：展示当前默认 Jenkins/Registry（只读 Tag），触发构建时自动使用。
    const { data: integration } = useQuery({
      queryKey: ['build-integration'],
      queryFn: () => buildApi.getBuildIntegration(),
    });

    // 按应用语言过滤基础镜像列表，新建构建与编辑构建弹窗共用。
    const { data: baseImagesPage } = useQuery({
      queryKey: ['base-images', 'build-modal', app?.language ?? ''],
      queryFn: () => buildApi.listBaseImages({ page: 1, size: 200, runtime: app?.language || undefined }),
      enabled: !!appId && (triggerOpen || editOpen),
    });
    const baseImages = baseImagesPage?.items ?? [];

    // 按应用语言过滤构建工具列表（可配置化：从后端查询，新增构建工具零前端改动）。
    const { data: buildToolsPage } = useQuery({
      queryKey: ['build-tools', 'build-modal', app?.language ?? ''],
      queryFn: () => buildApi.listBuildTools({ page: 1, size: 200, runtime: app?.language || undefined }),
      enabled: !!appId && (triggerOpen || editOpen),
    });
    const buildTools = buildToolsPage?.items ?? [];

    // 选中的基础镜像：用于在新建构建时预览并预填构建配置（构建工具可改选，其余可编辑）。
    const [selectedBaseImage, setSelectedBaseImage] = useState<BaseImage | null>(null);

    // 构建模式：baseImage=基础镜像构建（渲染模板）；custom=自定义镜像构建（自带 git 仓库 + Dockerfile）。
    const [buildMode, setBuildMode] = useState<'baseImage' | 'custom'>('baseImage');

    // 选中基础镜像后，默认选第一个匹配语言的构建工具，并预填命令/制品路径。
    // 构建工具的默认值来自 vo_build_tools（可配置化），不再从 BaseImage 读取。
    const handleBaseImageChange = (id: number) => {
      const bi = baseImages.find((x) => x.id === id) || null;
      setSelectedBaseImage(bi);
      if (bi && buildTools.length > 0) {
        const firstTool = buildTools[0];
        form.setFieldsValue({
          build_tool: firstTool.tool,
          build_command: firstTool.default_build_command,
          artifact_path: firstTool.default_artifact_path,
        });
      }
    };

    // 改选构建工具时，按工具自带默认命令与制品路径联动预填（数据来自 vo_build_tools）。
    const handleBuildToolChange = (tool?: string) => {
      if (!tool) {
        form.setFieldsValue({ build_command: '', artifact_path: '' });
        return;
      }
      const bt = buildTools.find((x) => x.tool === tool);
      if (bt) {
        form.setFieldsValue({
          build_command: bt.default_build_command,
          artifact_path: bt.default_artifact_path,
        });
      }
    };

    // 模式B：用户填的自定义 git 地址保存为该应用的 GitSource 记录，拿到 ID 后传给 trigger。
    // 后端 TriggerBuild 校验 git_source_id 归属应用，故必须先落库。
    // 错误向上抛给 triggerMutation 统一提示，这里不单独 onError。
    const createGitSourceMutation = useMutation({
      mutationFn: (v: { repo_url: string; default_branch: string }) =>
        buildApi.createGitSource(appId, {
          name: `custom-${Date.now()}`,
          provider: 'git',
          repo_url: v.repo_url,
          default_branch: v.default_branch,
        }),
    });

    // 远程分支搜索（防抖）。
    const [debouncedBranch, setDebouncedBranch] = useState('');
    useEffect(() => {
      const t = setTimeout(() => setDebouncedBranch(branchSearch), 400);
      return () => clearTimeout(t);
    }, [branchSearch]);

    const { data: refs, isFetching: refsFetching } = useQuery({
      queryKey: ['application', appId, 'git-refs', debouncedBranch],
      queryFn: () => buildApi.listGitRefs(appId, debouncedBranch),
      enabled: !!appId && !!app?.git_url && triggerOpen,
    });

    // 选定分支后自动获取 commit。
    const selectedBranch = Form.useWatch('ref_value', form);
    useEffect(() => {
      if (!selectedBranch) { setCommit(null); return; }
      let cancelled = false;
      buildApi.getGitCommit(appId, selectedBranch)
        .then((c) => { if (!cancelled) { setCommit({ sha: c.sha, message: c.message }); form.setFieldsValue({ commit_sha: c.sha, commit_message: c.message }); } })
        .catch((e: any) => { if (!cancelled) { setCommit(null); message.warning(e?.message || '获取 commit 失败'); } });
      return () => { cancelled = true; };
    }, [selectedBranch, appId, form, message]);

    const triggerMutation = useMutation({
      mutationFn: async (v: {
        ref_value: string;
        commit_sha?: string;
        commit_message?: string;
        target_tag?: string;
        build_mode: 'baseImage' | 'custom';
        base_image_id?: number;
        build_tool?: string;
        build_command?: string;
        artifact_path?: string;
        build_args?: string;
        custom_repo_url?: string;
        custom_branch?: string;
        dockerfile_path?: string;
      }) => {
        // 解析 build_args JSON（两种模式共用）。
        let parsedBuildArgs: Record<string, string> | undefined;
        if (v.build_args && v.build_args.trim() !== '') {
          try {
            parsedBuildArgs = JSON.parse(v.build_args);
          } catch {
            return Promise.reject(new Error('构建参数不是合法的 JSON 对象'));
          }
        }

        if (v.build_mode === 'custom') {
          // 模式B：先创建 GitSource（后端校验 git_source_id 归属应用），再用 repo 模式构建。
          if (!v.custom_repo_url) {
            return Promise.reject(new Error('请填写 Git 仓库地址'));
          }
          const branch = v.custom_branch || v.ref_value;
          const gs = await createGitSourceMutation.mutateAsync({
            repo_url: v.custom_repo_url,
            default_branch: branch,
          });
          return buildApi.trigger(appId, {
            ref_type: 'branch',
            ref_value: branch,
            commit_sha: v.commit_sha,
            commit_message: v.commit_message,
            target_tag: v.target_tag,
            git_source_id: gs.id,
            dockerfile_source: 'repo',
            dockerfile_path: v.dockerfile_path,
            build_command: v.build_command,
            build_args: parsedBuildArgs,
            trigger_source: 'manual',
          });
        }

        // 模式A：基础镜像构建，后端用基础镜像的单阶段 Dockerfile 模板渲染。
        // build_tool/artifact_path 传给后端，后端从 vo_build_tools 取 builder_image 并渲染。
        return buildApi.trigger(appId, {
          ref_type: 'branch',
          ref_value: v.ref_value,
          commit_sha: v.commit_sha,
          commit_message: v.commit_message,
          target_tag: v.target_tag,
          base_image_id: v.base_image_id,
          build_tool: v.build_tool,
          build_command: v.build_command,
          artifact_path: v.artifact_path,
          build_args: parsedBuildArgs,
          dockerfile_source: 'template',
          trigger_source: 'manual',
        });
      },
      onSuccess: (b) => {
        message.success(`构建 #${b.id} 已触发`);
        setTriggerOpen(false);
        form.resetFields();
        setCommit(null);
        setBranchSearch('');
        setSelectedBaseImage(null);
        setBuildMode('baseImage');
        queryClient.invalidateQueries({ queryKey: ['application', appId, 'builds'] });
        navigate(`/builds/${b.id}`);
      },
      onError: (e: any) => message.error(e?.message || '触发失败'),
    });

    // 行内「构建」：在原构建记录上重新拉取代码并构建（不生成新记录）。
    const buildMutation = useMutation({
      mutationFn: (id: number) => buildApi.rebuild(id),
      onSuccess: () => {
        message.success('构建已重新触发，正在拉取代码');
        queryClient.invalidateQueries({ queryKey: ['application', appId, 'builds'] });
      },
      onError: (e: any) => message.error(e?.message || '构建失败'),
    });

    // 终止运行中的构建。
    const cancelMutation = useMutation({
      mutationFn: (id: number) => buildApi.cancel(id),
      onSuccess: () => {
        message.success('已发送终止指令');
        queryClient.invalidateQueries({ queryKey: ['application', appId, 'builds'] });
      },
      onError: (e: any) => message.error(e?.message || '终止失败'),
    });

    // 删除构建（仅终态可删）。
    const deleteMutation = useMutation({
      mutationFn: (id: number) => buildApi.remove(id),
      onSuccess: () => {
        message.success('构建已删除');
        queryClient.invalidateQueries({ queryKey: ['application', appId, 'builds'] });
      },
      onError: (e: any) => message.error(e?.message || '删除失败'),
    });

    // 编辑构建（全量字段，与新建构建对齐；仅终态构建可编辑）。
    // editOpen 已在上方提前声明（baseImages 查询依赖）。
    const [editTarget, setEditTarget] = useState<Build | undefined>();
    const [editBuildMode, setEditBuildMode] = useState<'baseImage' | 'custom'>('baseImage');
    const [editSelectedBaseImage, setEditSelectedBaseImage] = useState<BaseImage | null>(null);
    const [editBranchSearch, setEditBranchSearch] = useState('');
    const [editCommit, setEditCommit] = useState<{ sha: string; message: string } | null>(null);
    type EditFormValues = {
      version: number;
      build_mode?: 'baseImage' | 'custom';
      ref_value?: string;
      commit_message?: string;
      target_tag?: string;
      base_image_id?: number;
      build_tool?: string;
      build_command?: string;
      artifact_path?: string;
      build_args?: string;
      custom_repo_url?: string;
      custom_branch?: string;
      dockerfile_path?: string;
    };
    const [editForm] = Form.useForm<EditFormValues>();
    // 记录已回显的 editTarget.id，避免 baseImages 重新拉取时覆盖用户在弹窗内的编辑。
    const editPopulatedRef = useRef<number | undefined>(undefined);

    // 打开编辑弹窗时根据 editTarget 与基础镜像列表回显全量字段。
    // baseImages 为异步查询（弹窗打开后才 enable），需等其就绪再回显；
    // 同一 editTarget 只回显一次，避免重新拉取覆盖用户编辑。
    useEffect(() => {
      if (!editOpen || !editTarget) { return; }
      if (editPopulatedRef.current === editTarget.id) { return; }
      // baseImage 模式需要基础镜像列表就绪才能正确回显 base_image_id/build_tool；custom 模式无需等待。
      const isCustom = editTarget.dockerfile_source === 'repo';
      if (!isCustom && baseImages.length === 0) { return; }
      editPopulatedRef.current = editTarget.id;

      const r = editTarget;
      const mode: 'baseImage' | 'custom' = isCustom ? 'custom' : 'baseImage';
      setEditBuildMode(mode);
      // 选中现有基础镜像（baseImage 模式），用于镜像引用预览与构建工具回显。
      const bi = !isCustom && r.base_image_id
        ? baseImages.find((b) => b.id === r.base_image_id) ?? null
        : null;
      setEditSelectedBaseImage(bi);
      // build_args 序列化为 JSON 字符串供 TextArea 展示。
      let buildArgsStr = '';
      if (r.build_args && Object.keys(r.build_args).length > 0) {
        try { buildArgsStr = JSON.stringify(r.build_args); } catch { buildArgsStr = ''; }
      }
      editForm.setFieldsValue({
        version: r.version,
        build_mode: mode,
        ref_value: r.ref_value ?? r.branch,
        commit_message: r.commit_message,
        target_tag: r.target_tag ?? r.image_tag,
        base_image_id: isCustom ? undefined : r.base_image_id,
        build_tool: !isCustom ? r.build_tool : undefined,
        build_command: r.build_command,
        artifact_path: isCustom ? undefined : (r.artifact_path ?? ''),
        build_args: buildArgsStr,
        custom_branch: isCustom ? (r.ref_value ?? r.branch) : undefined,
        dockerfile_path: isCustom ? (r.dockerfile_path ?? r.context_path ?? '') : undefined,
      });
      setEditCommit(r.commit_sha ? { sha: r.commit_sha, message: r.commit_message ?? '' } : null);
      setEditBranchSearch(r.ref_value ?? r.branch ?? '');
    }, [editOpen, editTarget, baseImages, editForm]);

    // 弹窗关闭时重置回显标记，下次打开同一构建可重新回显。
    useEffect(() => {
      if (!editOpen) { editPopulatedRef.current = undefined; }
    }, [editOpen]);

    // 编辑模式下复用 git refs 查询（仅 baseImage 模式需要分支选择）。
    const [editDebouncedBranch, setEditDebouncedBranch] = useState('');
    useEffect(() => {
      const t = setTimeout(() => setEditDebouncedBranch(editBranchSearch), 400);
      return () => clearTimeout(t);
    }, [editBranchSearch]);
    const { data: editRefs, isFetching: editRefsFetching } = useQuery({
      queryKey: ['application', appId, 'git-refs', editDebouncedBranch],
      queryFn: () => buildApi.listGitRefs(appId, editDebouncedBranch),
      enabled: !!appId && !!app?.git_url && editOpen && editBuildMode === 'baseImage',
    });

    const editSelectedBranch = Form.useWatch('ref_value', editForm);
    useEffect(() => {
      if (!editOpen || editBuildMode !== 'baseImage') { return; }
      if (!editSelectedBranch) { setEditCommit(null); return; }
      let cancelled = false;
      buildApi.getGitCommit(appId, editSelectedBranch)
        .then((c) => { if (!cancelled) { setEditCommit({ sha: c.sha, message: c.message ?? '' }); editForm.setFieldsValue({ commit_message: c.message }); } })
        .catch((e: any) => { if (!cancelled) { setEditCommit(null); message.warning(e?.message || '获取 commit 失败'); } });
      return () => { cancelled = true; };
    }, [editSelectedBranch, editOpen, editBuildMode, appId, editForm, message]);

    // 编辑模式下：选基础镜像联动构建工具/命令/制品路径（数据来自 vo_build_tools）。
    const handleEditBaseImageChange = (id: number) => {
      const bi = baseImages.find((x) => x.id === id) || null;
      setEditSelectedBaseImage(bi);
      if (bi && buildTools.length > 0) {
        const firstTool = buildTools[0];
        editForm.setFieldsValue({
          build_tool: firstTool.tool,
          build_command: firstTool.default_build_command,
          artifact_path: firstTool.default_artifact_path,
        });
      }
    };
    const handleEditBuildToolChange = (tool?: string) => {
      if (!tool) {
        editForm.setFieldsValue({ build_command: '', artifact_path: '' });
        return;
      }
      const bt = buildTools.find((x) => x.tool === tool);
      if (bt) {
        editForm.setFieldsValue({ build_command: bt.default_build_command, artifact_path: bt.default_artifact_path });
      }
    };

    const updateMutation = useMutation({
      mutationFn: async (v: EditFormValues) => {
        if (!editTarget) { return; }
        // 解析 build_args JSON。
        let parsedBuildArgs: Record<string, string> | undefined;
        if (v.build_args && v.build_args.trim() !== '') {
          try {
            parsedBuildArgs = JSON.parse(v.build_args);
          } catch {
            return Promise.reject(new Error('构建参数不是合法的 JSON 对象'));
          }
        }
        const patch: import('@/api/builds').UpdateBuildInput = {
          version: v.version,
          commit_message: v.commit_message,
          target_tag: v.target_tag,
          build_command: v.build_command,
          build_args: parsedBuildArgs,
        };
        if (v.build_mode === 'custom') {
          // 自定义镜像构建：以分支+dockerfile路径+构建命令更新；后端需 dockerfile_source=repo。
          patch.ref_value = v.custom_branch || v.ref_value;
          patch.dockerfile_source = 'repo';
          // custom_branch 仅前端字段，不进 patch；dockerfile_path 即仓库内 Dockerfile 路径。
          patch.dockerfile_path = v.dockerfile_path;
        } else {
          // 基础镜像构建：保留 dockerfile_source=template；带出 base_image_id/ref_value/build_tool/artifact_path。
          patch.ref_value = v.ref_value;
          patch.dockerfile_source = 'template';
          patch.base_image_id = v.base_image_id;
          patch.build_tool = v.build_tool;
          patch.artifact_path = v.artifact_path;
        }
        return buildApi.update(editTarget.id, patch);
      },
      onSuccess: () => {
        message.success('构建已更新');
        setEditOpen(false);
        setEditTarget(undefined);
        editForm.resetFields();
        setEditSelectedBaseImage(null);
        setEditCommit(null);
        setEditBranchSearch('');
        setEditBuildMode('baseImage');
        queryClient.invalidateQueries({ queryKey: ['application', appId, 'builds'] });
      },
      onError: (e: any) => message.error(e?.message || '更新失败'),
    });

    const isRunning = (s?: Build['status']) => s === 'pending' || s === 'running';
    const isTerminal = (s?: Build['status']) => s === 'success' || s === 'failed' || s === 'cancelled';

    const columns: ColumnsType<Build> = [
      {
        title: '构建',
        dataIndex: 'id',
        width: 80,
        render: (id: number) => <a onClick={() => navigate(`/builds/${id}`)}>#{id}</a>,
      },
      { title: '分支', dataIndex: 'branch', width: 140 },
      {
        title: 'Commit',
        dataIndex: 'commit_sha',
        width: 120,
        render: (v?: string) => <code>{shortSha(v)}</code>,
      },
      {
        title: '状态',
        dataIndex: 'status',
        width: 100,
        render: (s: Build['status']) => <ResourceStatus status={s} />,
      },
      { title: '镜像', dataIndex: 'image_tag', width: 160, render: (v?: string) => v || '-' },
      { title: '耗时', dataIndex: 'duration_ms', width: 100, render: formatDuration },
      { title: '触发人', dataIndex: 'triggered_by_name', width: 120 },
      { title: '时间', dataIndex: 'created_at', width: 140, render: formatRelative },
      {
        title: '操作',
        key: 'actions',
        width: 240,
        fixed: 'right' as const,
        render: (_, r) => (
          <Space size={0} wrap>
            <Button type="link" size="small" onClick={() => navigate(`/builds/${r.id}`)}>
              日志
            </Button>
            {isRunning(r.status) ? (
              <Button
                type="link"
                size="small"
                danger
                loading={cancelMutation.isPending && cancelMutation.variables === r.id}
                onClick={() =>
                  confirmDanger({
                    title: '终止构建',
                    content: `确定终止构建 #${r.id}？运行中的构建任务将被取消。`,
                    okText: '终止',
                    onOk: () => cancelMutation.mutateAsync(r.id),
                  })
                }
              >
                终止
              </Button>
            ) : (
              <Button
                type="link"
                size="small"
                icon={<ReloadOutlined />}
                disabled={!app?.git_url}
                loading={buildMutation.isPending && buildMutation.variables === r.id}
                onClick={() =>
                  confirmDanger({
                    title: '构建',
                    content: `将在原构建 #${r.id} 记录上重新拉取代码并构建，不会生成新记录。`,
                    okText: '构建',
                    onOk: () => buildMutation.mutateAsync(r.id),
                  })
                }
              >
                构建
              </Button>
            )}
            <Button
              type="link"
              size="small"
              disabled={!isTerminal(r.status)}
              onClick={() => {
                setEditTarget(r);
                setEditOpen(true);
              }}
            >
              编辑
            </Button>
            {canRelease && (
              <Button
                type="link"
                size="small"
                disabled={r.status !== 'success' || !r.output_image_id}
                onClick={() => {
                  setPublishImageId(r.output_image_id);
                  setPublishOpen(true);
                }}
              >
                发布
              </Button>
            )}
            <Button
              type="link"
              size="small"
              danger
              disabled={!isTerminal(r.status)}
              loading={deleteMutation.isPending && deleteMutation.variables === r.id}
              onClick={() =>
                confirmDanger({
                  title: '删除构建',
                  content: `确认删除构建 #${r.id}？此操作不可恢复。`,
                  okText: '删除',
                  onOk: () => deleteMutation.mutateAsync(r.id),
                })
              }
            >
              删除
            </Button>
          </Space>
        ),
      },
    ];

    return (
      <>
        <div style={{ marginBottom: 16, textAlign: 'right' }}>
          <Button
            type="primary"
            icon={<PlusOutlined />}
            disabled={!app?.git_url}
            onClick={() => setTriggerOpen(true)}
          >
            新建构建
          </Button>
        </div>
        <Table<Build>
          rowKey="id"
          loading={isLoading}
          columns={columns}
          dataSource={data?.items}
          pagination={false}
          locale={{ emptyText: <EmptyState title="暂无构建记录" description={app?.git_url ? '点击右上角「新建构建」触发首次构建' : '请先在「设置」中配置 git_url'} /> }}
        />
        <Modal
          title="新建构建"
          open={triggerOpen}
          onCancel={() => {
            setTriggerOpen(false);
            form.resetFields();
            setCommit(null);
            setBranchSearch('');
            setSelectedBaseImage(null);
            setBuildMode('baseImage');
          }}
          onOk={() => form.submit()}
          confirmLoading={triggerMutation.isPending}
          destroyOnHidden
          width={680}
        >
          {!app?.git_url && (
            <Typography.Paragraph type="warning">
              当前应用未配置 git_url，无法构建。请先在「设置」Tab 填写 Git 仓库地址。
            </Typography.Paragraph>
          )}
          {/* 系统变量化构建集成：只读展示当前默认 Jenkins/Registry */}
          <Descriptions column={2} size="small" bordered style={{ marginBottom: 16 }}>
            <Descriptions.Item label="Jenkins">
              {integration?.jenkins ? (
                <Space size={4}>
                  <Tag color="blue">{integration.jenkins.name}</Tag>
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>{integration.jenkins.url}</Typography.Text>
                </Space>
              ) : <Tag>未配置</Tag>}
            </Descriptions.Item>
            <Descriptions.Item label="镜像仓库">
              {integration?.registry ? (
                <Space size={4}>
                  <Tag color="blue">{integration.registry.name}</Tag>
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>{integration.registry.type}</Typography.Text>
                </Space>
              ) : <Tag>未配置</Tag>}
            </Descriptions.Item>
          </Descriptions>
          {(!integration?.jenkins || !integration?.registry) && (
            <Typography.Paragraph type="warning" style={{ marginBottom: 16 }}>
              系统默认 Jenkins 或镜像仓库未配置，构建将失败。请前往「系统设置 {'>'} 构建集成」配置。
            </Typography.Paragraph>
          )}
          <Form
            layout="vertical"
            form={form}
            onFinish={(v) => triggerMutation.mutate({ ...v, build_mode: buildMode })}
            initialValues={{ build_mode: 'baseImage' }}
          >
            <Form.Item
              name="build_mode"
              label="构建模式"
              tooltip="基础镜像构建：按应用语言选基础镜像，后端渲染 Dockerfile 模板；自定义镜像构建：自带 Git 仓库与 Dockerfile"
            >
              <Radio.Group
                value={buildMode}
                onChange={(e) => setBuildMode(e.target.value)}
                options={[
                  { label: '基础镜像构建', value: 'baseImage' },
                  { label: '自定义镜像构建', value: 'custom' },
                ]}
              />
            </Form.Item>

            {buildMode === 'baseImage' ? (
              <>
                <Form.Item
                  name="ref_value"
                  label="分支"
                  rules={[{ required: true, message: '请选择分支' }]}
                  extra="通过 git 协议远程获取，支持模糊搜索"
                >
                  <Select
                    showSearch
                    placeholder="输入分支名搜索"
                    filterOption={false}
                    onSearch={setBranchSearch}
                    loading={refsFetching}
                    options={(refs || []).map((r) => ({ label: r.name, value: r.name }))}
                    notFoundContent={refsFetching ? '搜索中...' : '无匹配分支'}
                  />
                </Form.Item>
                <Form.Item label="Commit">
                  {commit?.sha ? (
                    <Space>
                      <Typography.Text code>{shortSha(commit.sha)}</Typography.Text>
                      {commit.message && <Typography.Text type="secondary">{commit.message}</Typography.Text>}
                    </Space>
                  ) : <Typography.Text type="secondary">选择分支后自动获取</Typography.Text>}
                </Form.Item>
                <Form.Item name="commit_sha" hidden><Input /></Form.Item>
                <Form.Item name="commit_message" hidden><Input /></Form.Item>
                <Form.Item name="target_tag" label="镜像 Tag（可选）" extra="留空则使用 commit SHA 前 12 位">
                  <Input placeholder="例如 v1.0.0" />
                </Form.Item>

                {/* 构建配置：根据应用语言选择基础镜像，预填并允许编辑（构建工具可选可改） */}
                <Typography.Title level={5} style={{ marginTop: 8, marginBottom: 8 }}>构建配置</Typography.Title>
                <Descriptions column={1} size="small" style={{ marginBottom: 12 }}>
                  <Descriptions.Item label="应用语言">
                    {app?.language ? (
                      <Tag color="blue">{LANGUAGE_OPTIONS.find((o) => o.value === app.language)?.label ?? app.language}</Tag>
                    ) : <Typography.Text type="warning">未设置</Typography.Text>}
                  </Descriptions.Item>
                </Descriptions>
                {!app?.language && (
                  <Typography.Paragraph type="warning" style={{ marginBottom: 12 }}>
                    当前应用未设置开发语言，将在「设置」Tab 配置语言后才能按语言过滤基础镜像；当前列出所有基础镜像。
                  </Typography.Paragraph>
                )}
                <Form.Item
                  name="base_image_id"
                  label="基础镜像"
                  rules={[{ required: true, message: '请选择基础镜像' }]}
                  extra="运行时 Docker 镜像；选中后自动预填构建工具、命令与制品路径"
                >
                  <Select
                    placeholder="选择基础镜像"
                    onChange={handleBaseImageChange}
                    options={baseImages.map((b) => ({
                      label: `${b.name} (${b.image_ref})${b.is_recommended ? ' ★推荐' : ''}`,
                      value: b.id,
                    }))}
                  />
                </Form.Item>
                {selectedBaseImage && (
                  <Descriptions column={1} size="small" bordered style={{ marginBottom: 12 }}>
                    <Descriptions.Item label="镜像引用">
                      <Typography.Text code>{selectedBaseImage.image_ref}</Typography.Text>
                    </Descriptions.Item>
                    {selectedBaseImage.description && (
                      <Descriptions.Item label="说明">{selectedBaseImage.description}</Descriptions.Item>
                    )}
                  </Descriptions>
                )}
                <Form.Item
                  name="build_tool"
                  label="构建工具"
                  extra="按应用语言列出候选（来自系统设置 > 构建工具）；改选后自动带出对应构建命令与制品路径"
                >
                  <Select
                    allowClear
                    placeholder="选择构建工具"
                    onChange={handleBuildToolChange}
                    options={buildTools.map((t) => ({ label: `${t.name} (${t.tool})`, value: t.tool }))}
                  />
                </Form.Item>
                <Form.Item
                  name="build_command"
                  label="构建命令"
                  rules={[{ required: true, message: '请输入构建命令' }]}
                  extra="随构建工具自动带出，可编辑"
                >
                  <Input placeholder="例如 mvn -B clean package -DskipTests" />
                </Form.Item>
                <Form.Item
                  name="artifact_path"
                  label="制品路径"
                  rules={[{ required: true, message: '请输入制品路径' }]}
                  extra="随构建工具自动带出，构建产出后 COPY 进运行时镜像；可编辑"
                >
                  <Input placeholder="例如 target/*.jar" />
                </Form.Item>
                <Form.Item
                  name="build_args"
                  label="构建参数（JSON）"
                  extra='键值对 JSON，如 {"PROFILE":"prod"}；留空表示无'
                >
                  <Input.TextArea
                    rows={2}
                    placeholder='{"PROFILE":"prod"}'
                    style={{ fontFamily: 'monospace', fontSize: 12 }}
                  />
                </Form.Item>
              </>
            ) : (
              <>
                <Typography.Paragraph type="secondary" style={{ marginBottom: 12 }}>
                  自定义镜像构建：从指定 Git 仓库的指定分支拉取代码，使用仓库内 Dockerfile（位于 dockerfile 路径）构建镜像并推送到默认 Registry。
                </Typography.Paragraph>
                <Form.Item
                  name="custom_repo_url"
                  label="Git 仓库地址"
                  rules={[{ required: true, message: '请输入 Git 仓库地址' }]}
                  extra="构建时从该地址拉取代码，会保存为应用的 Git 源"
                >
                  <Input placeholder="https://github.com/org/repo.git" />
                </Form.Item>
                <Form.Item
                  name="custom_branch"
                  label="分支"
                  rules={[{ required: true, message: '请输入分支名' }]}
                  extra="手动输入分支名，如 main / master / develop"
                >
                  <Input placeholder="main" />
                </Form.Item>
                <Form.Item
                  name="dockerfile_path"
                  label="Dockerfile 路径"
                  rules={[{ required: true, message: '请输入 Dockerfile 路径' }]}
                  extra="仓库内 Dockerfile 的相对路径，如 ./Dockerfile 或 build/Dockerfile"
                >
                  <Input placeholder="./Dockerfile" />
                </Form.Item>
                <Form.Item name="target_tag" label="镜像 Tag（可选）" extra="留空则使用分支名 + 时间戳">
                  <Input placeholder="例如 v1.0.0" />
                </Form.Item>
                <Form.Item
                  name="build_command"
                  label="构建命令（可选）"
                  extra="自定义镜像构建通常 Dockerfile 内已含构建步骤，留空即可"
                >
                  <Input placeholder="可选，如 make build" />
                </Form.Item>
                <Form.Item
                  name="build_args"
                  label="构建参数（JSON，可选）"
                  extra='键值对 JSON，作为 docker build 的 build-arg；留空表示无'
                >
                  <Input.TextArea
                    rows={2}
                    placeholder='{"VERSION":"1.0.0"}'
                    style={{ fontFamily: 'monospace', fontSize: 12 }}
                  />
                </Form.Item>
              </>
            )}
          </Form>
        </Modal>
        <Modal
          title={`编辑构建 #${editTarget?.id ?? ''}`}
          open={editOpen}
          onCancel={() => {
            setEditOpen(false);
            setEditTarget(undefined);
            editForm.resetFields();
            setEditSelectedBaseImage(null);
            setEditCommit(null);
            setEditBranchSearch('');
            setEditBuildMode('baseImage');
          }}
          onOk={() => editForm.submit()}
          confirmLoading={updateMutation.isPending}
          destroyOnHidden
          width={680}
        >
          <Form layout="vertical" form={editForm} onFinish={(v) => updateMutation.mutate(v)}>
            <Form.Item name="version" hidden><Input /></Form.Item>
            <Form.Item
              name="build_mode"
              label="构建模式"
              tooltip="基础镜像构建：按应用语言选基础镜像，后端渲染 Dockerfile 模板；自定义镜像构建：自带 Git 仓库与 Dockerfile"
            >
              <Radio.Group
                value={editBuildMode}
                onChange={(e) => setEditBuildMode(e.target.value)}
                options={[
                  { label: '基础镜像构建', value: 'baseImage' },
                  { label: '自定义镜像构建', value: 'custom' },
                ]}
              />
            </Form.Item>

            {editBuildMode === 'baseImage' ? (
              <>
                <Form.Item
                  name="ref_value"
                  label="分支"
                  rules={[{ required: true, message: '请选择分支' }]}
                  extra="通过 git 协议远程获取，支持模糊搜索"
                >
                  <Select
                    showSearch
                    placeholder="输入分支名搜索"
                    filterOption={false}
                    onSearch={setEditBranchSearch}
                    loading={editRefsFetching}
                    options={(editRefs || []).map((r) => ({ label: r.name, value: r.name }))}
                    notFoundContent={editRefsFetching ? '搜索中...' : '无匹配分支'}
                  />
                </Form.Item>
                <Form.Item label="Commit">
                  {editCommit?.sha ? (
                    <Space>
                      <Typography.Text code>{shortSha(editCommit.sha)}</Typography.Text>
                      {editCommit.message && <Typography.Text type="secondary">{editCommit.message}</Typography.Text>}
                    </Space>
                  ) : <Typography.Text type="secondary">选择分支后自动获取</Typography.Text>}
                </Form.Item>
                <Form.Item name="commit_message" hidden><Input /></Form.Item>
                <Form.Item name="target_tag" label="镜像 Tag（可选）" extra="留空则使用 commit SHA 前 12 位">
                  <Input placeholder="例如 v1.0.0" />
                </Form.Item>

                <Typography.Title level={5} style={{ marginTop: 8, marginBottom: 8 }}>构建配置</Typography.Title>
                <Descriptions column={1} size="small" style={{ marginBottom: 12 }}>
                  <Descriptions.Item label="应用语言">
                    {app?.language ? (
                      <Tag color="blue">{LANGUAGE_OPTIONS.find((o) => o.value === app.language)?.label ?? app.language}</Tag>
                    ) : <Typography.Text type="warning">未设置</Typography.Text>}
                  </Descriptions.Item>
                </Descriptions>
                <Form.Item
                  name="base_image_id"
                  label="基础镜像"
                  rules={[{ required: true, message: '请选择基础镜像' }]}
                  extra="运行时 Docker 镜像；选中后自动预填构建工具、命令与制品路径"
                >
                  <Select
                    placeholder="选择基础镜像"
                    onChange={handleEditBaseImageChange}
                    options={baseImages.map((b) => ({
                      label: `${b.name} (${b.image_ref})${b.is_recommended ? ' ★推荐' : ''}`,
                      value: b.id,
                    }))}
                  />
                </Form.Item>
                {editSelectedBaseImage && (
                  <Descriptions column={1} size="small" bordered style={{ marginBottom: 12 }}>
                    <Descriptions.Item label="镜像引用">
                      <Typography.Text code>{editSelectedBaseImage.image_ref}</Typography.Text>
                    </Descriptions.Item>
                    {editSelectedBaseImage.description && (
                      <Descriptions.Item label="说明">{editSelectedBaseImage.description}</Descriptions.Item>
                    )}
                  </Descriptions>
                )}
                <Form.Item
                  name="build_tool"
                  label="构建工具"
                  extra="按应用语言列出候选（来自系统设置 > 构建工具）；改选后自动带出对应构建命令与制品路径"
                >
                  <Select
                    allowClear
                    placeholder="选择构建工具"
                    onChange={handleEditBuildToolChange}
                    options={buildTools.map((t) => ({ label: `${t.name} (${t.tool})`, value: t.tool }))}
                  />
                </Form.Item>
                <Form.Item
                  name="build_command"
                  label="构建命令"
                  rules={[{ required: true, message: '请输入构建命令' }]}
                  extra="随构建工具自动带出，可编辑"
                >
                  <Input placeholder="例如 mvn -B clean package -DskipTests" />
                </Form.Item>
                <Form.Item
                  name="artifact_path"
                  label="制品路径"
                  rules={[{ required: true, message: '请输入制品路径' }]}
                  extra="随构建工具自动带出，构建产出后 COPY 进运行时镜像；可编辑"
                >
                  <Input placeholder="例如 target/*.jar" />
                </Form.Item>
                <Form.Item
                  name="build_args"
                  label="构建参数（JSON）"
                  extra='键值对 JSON，如 {"PROFILE":"prod"}；留空表示无'
                >
                  <Input.TextArea
                    rows={2}
                    placeholder='{"PROFILE":"prod"}'
                    style={{ fontFamily: 'monospace', fontSize: 12 }}
                  />
                </Form.Item>
              </>
            ) : (
              <>
                <Typography.Paragraph type="secondary" style={{ marginBottom: 12 }}>
                  自定义镜像构建：从指定 Git 仓库的指定分支拉取代码，使用仓库内 Dockerfile（位于 dockerfile 路径）构建镜像并推送到默认 Registry。
                </Typography.Paragraph>
                <Form.Item name="custom_repo_url" label="Git 仓库地址" extra="编辑模式下不修改仓库地址，仅用于展示">
                  <Input disabled placeholder="https://github.com/org/repo.git" />
                </Form.Item>
                <Form.Item
                  name="custom_branch"
                  label="分支"
                  rules={[{ required: true, message: '请输入分支名' }]}
                  extra="手动输入分支名，如 main / master / develop"
                >
                  <Input placeholder="main" />
                </Form.Item>
                <Form.Item
                  name="dockerfile_path"
                  label="Dockerfile 路径"
                  rules={[{ required: true, message: '请输入 Dockerfile 路径' }]}
                  extra="仓库内 Dockerfile 的相对路径，如 ./Dockerfile 或 build/Dockerfile"
                >
                  <Input placeholder="./Dockerfile" />
                </Form.Item>
                <Form.Item name="target_tag" label="镜像 Tag（可选）" extra="留空则使用分支名 + 时间戳">
                  <Input placeholder="例如 v1.0.0" />
                </Form.Item>
                <Form.Item
                  name="build_command"
                  label="构建命令（可选）"
                  extra="自定义镜像构建通常 Dockerfile 内已含构建步骤，留空即可"
                >
                  <Input placeholder="可选，如 make build" />
                </Form.Item>
                <Form.Item
                  name="build_args"
                  label="构建参数（JSON，可选）"
                  extra='键值对 JSON，作为 docker build 的 build-arg；留空表示无'
                >
                  <Input.TextArea
                    rows={2}
                    placeholder='{"VERSION":"1.0.0"}'
                    style={{ fontFamily: 'monospace', fontSize: 12 }}
                  />
                </Form.Item>
              </>
            )}
          </Form>
        </Modal>

        <PublishModal
          open={publishOpen}
          onClose={() => setPublishOpen(false)}
          applicationId={appId}
          fixedImageId={publishImageId}
          defaultStrategy="recreate"
          onPublished={(relId) => navigate(`/releases/${relId}`)}
        />
      </>
    );
  }

  function ReleasesTab({ appId, canRelease }: { appId: number; canRelease: boolean }) {
    const { data: groups } = useQuery({
      queryKey: ['application', appId, 'groups'],
      queryFn: () => groupApi.list(appId),
      enabled: !!appId,
    });

    const groupIds = (groups?.items || []).map((g) => g.id);
    const { data: releasesByGroup, isLoading } = useQuery({
      queryKey: ['application', appId, 'releases-overview'],
      queryFn: async () => {
        const results = await Promise.all(
          groupIds.map((gid) => releaseApi.list(gid, { page: 1, size: 5 }).then((p) => ({ gid, items: p.items }))),
        );
        return results;
      },
      enabled: groupIds.length > 0,
    });

    const groupName = (gid: number) => groups?.items?.find((g) => g.id === gid)?.display_name
      || groups?.items?.find((g) => g.id === gid)?.name
      || `分组 ${gid}`;

    const rows: (Release & { group_id: number; group_name: string })[] = [];
    (releasesByGroup || []).forEach((r) => {
      (r.items || []).forEach((rel) => {
        rows.push({ ...rel, group_id: r.gid, group_name: groupName(r.gid) });
      });
    });
    rows.sort((a, b) => (a.created_at < b.created_at ? 1 : -1));

    const columns: ColumnsType<(Release & { group_id: number; group_name: string })> = [
      {
        title: '分组',
        dataIndex: 'group_name',
        width: 160,
        render: (v: string, r) => <a onClick={() => navigate(`/groups/${r.group_id}`)}>{v}</a>,
      },
      {
        title: '发布',
        dataIndex: 'release_number',
        width: 80,
        render: (n: number, r) => <a onClick={() => navigate(`/releases/${r.id}`)}>#{n}</a>,
      },
      {
        title: '状态',
        dataIndex: 'status',
        width: 110,
        render: (s: Release['status']) => <ResourceStatus status={s} />,
      },
      { title: '策略', dataIndex: 'strategy', width: 120 },
      { title: '副本', dataIndex: 'replicas', width: 80, align: 'center' },
      { title: '进度', dataIndex: 'progress_percent', width: 100, render: (v: number) => `${v}%` },
      { title: '镜像', dataIndex: 'image_ref', ellipsis: true, render: (v?: string) => v ? <code>{v}</code> : '-' },
      { title: '耗时', dataIndex: 'duration_ms', width: 100, render: formatDuration },
      { title: '时间', dataIndex: 'created_at', width: 150, render: formatRelative },
    ];

    const hasGroups = (groups?.items?.length ?? 0) > 0;
    const firstGroupId = groups?.items?.[0]?.id;

    return (
      <>
        {canRelease && (
          <div style={{ marginBottom: 16, textAlign: 'right' }}>
            <Space>
              <PermissionGate code="menu:release:orch:view">
                <Button
                  icon={<DeploymentUnitOutlined />}
                  disabled={!hasGroups}
                  onClick={() => navigate(`/applications/${appId}/multi-release`)}
                >
                  多集群发布
                </Button>
              </PermissionGate>
              <Button
                type="primary"
                icon={<PlusOutlined />}
                disabled={!hasGroups}
                onClick={() => firstGroupId && navigate(`/groups/${firstGroupId}`)}
              >
                新建发布
              </Button>
            </Space>
          </div>
        )}
        <Table
          rowKey="id"
          loading={isLoading}
          columns={columns}
          dataSource={rows}
          pagination={{ pageSize: 20 }}
          locale={{ emptyText: <EmptyState title="暂无发布记录" description={hasGroups ? '在各分组详情中触发首次发布' : '请先创建分组'} /> }}
        />
      </>
    );
  }

  function ConfigsTab({ appId, readOnly }: { appId: number; readOnly?: boolean }) {
    const { message } = App.useApp();
    const queryClient = useQueryClient();
    const [createOpen, setCreateOpen] = useState(false);
    const [createForm] = Form.useForm<{ name: string; description?: string }>();
    const [editTarget, setEditTarget] = useState<ConfigSet | undefined>();
    const [editOpen, setEditOpen] = useState(false);
    const [editForm] = Form.useForm();
    const [historyTarget, setHistoryTarget] = useState<ConfigSet | undefined>();
    const [historyOpen, setHistoryOpen] = useState(false);
    const [diffSnapshotId, setDiffSnapshotId] = useState<number>();
    const [diffFilePath, setDiffFilePath] = useState<string>();
    // 记录已回显的 editTarget.id，避免重复 setFieldsValue 覆盖用户编辑。
    const editPopulatedRef = useRef<number | undefined>(undefined);

    useEffect(() => {
      if (!editOpen || !editTarget) { return; }
      if (editPopulatedRef.current === editTarget.id) { return; }
      editPopulatedRef.current = editTarget.id;
      const content = parseConfigContent(editTarget.content);
      populateFormFromContent(editForm, content);
      editForm.setFieldsValue({ version: editTarget.version });
    }, [editOpen, editTarget, editForm]);

    useEffect(() => {
      if (!editOpen) { editPopulatedRef.current = undefined; }
    }, [editOpen]);

    // 应用维度的配置集列表（含 content）。一个配置集即可包含文件/环境变量/启动参数全部内容。
    const { data: configSets, isLoading } = useQuery({
      queryKey: ['application', appId, 'config-sets'],
      queryFn: () => configApi.listAppConfigSets(appId),
      enabled: !!appId,
    });

    const createMutation = useMutation({
      mutationFn: (v: { name: string; description?: string }) =>
        configApi.createAppConfigSet(appId, { name: v.name, description: v.description, content: {} }),
      onSuccess: () => {
        message.success('配置已创建');
        setCreateOpen(false);
        createForm.resetFields();
        queryClient.invalidateQueries({ queryKey: ['application', appId, 'config-sets'] });
      },
      onError: (e: any) => message.error(e?.message || '创建失败'),
    });

    const deleteMutation = useMutation({
      mutationFn: (id: number) => configApi.deleteConfigSet(id),
      onSuccess: () => {
        message.success('配置已删除');
        queryClient.invalidateQueries({ queryKey: ['application', appId, 'config-sets'] });
      },
      onError: (e: any) => message.error(e?.message || '删除失败'),
    });

    // 保存配置内容（整体覆盖 content JSONB）。
    const saveContentMutation = useMutation({
      mutationFn: (v: { content: Record<string, any>; version: number }) =>
        configApi.updateConfigSet(editTarget!.id, { content: v.content, version: v.version }),
      onSuccess: () => {
        message.success('配置内容已保存');
        setEditOpen(false);
        queryClient.invalidateQueries({ queryKey: ['application', appId, 'config-sets'] });
        if (editTarget) {
          queryClient.invalidateQueries({ queryKey: ['config-set-snapshots', editTarget.id] });
        }
      },
      onError: (e: any) => message.error(e?.message || '保存失败'),
    });

    const { data: configSetSnapshots = [] } = useQuery({
      queryKey: ['config-set-snapshots', historyTarget?.id],
      queryFn: () => configApi.listConfigSetSnapshots(historyTarget!.id),
      enabled: !!historyTarget?.id && historyOpen,
    });

    const historyContent = useMemo(
      () => parseConfigContent(historyTarget?.content),
      [historyTarget],
    );

    const historyFileOptions = useMemo(
      () => (historyContent.files || []).map((f) => f.path).filter(Boolean).map((p) => ({ label: p, value: p })),
      [historyContent],
    );

    const { data: configSetFileDiff, isLoading: configSetDiffLoading } = useQuery({
      queryKey: ['config-set-file-diff', historyTarget?.id, diffSnapshotId, diffFilePath],
      queryFn: () =>
        configApi.diffConfigFile(diffSnapshotId!, {
          file_path: diffFilePath!,
          target_type: 'config_set',
          target_id: historyTarget!.id,
        }),
      enabled: !!historyOpen && !!historyTarget?.id && !!diffSnapshotId && !!diffFilePath,
    });

    const SNAPSHOT_REASON: Record<string, string> = { update: '配置更新', bind: '绑定配置', unbind: '解绑配置' };

    const snapshotColumns: ColumnsType<ConfigContentSnapshot> = [
      { title: '版本', dataIndex: 'snapshot_no', width: 72, align: 'center' },
      { title: '原因', dataIndex: 'change_reason', width: 100, render: (v: string) => SNAPSHOT_REASON[v] || v },
      { title: '文件数', dataIndex: 'file_count', width: 72, align: 'center' },
      { title: '时间', dataIndex: 'created_at', width: 180, render: formatTime },
      {
        title: '操作',
        width: 88,
        render: (_, row) => (
          <Button
            type="link"
            size="small"
            icon={<DiffOutlined />}
            onClick={() => {
              setDiffSnapshotId(row.id);
              setDiffFilePath(undefined);
            }}
          >
            比对
          </Button>
        ),
      },
    ];

    const columns: ColumnsType<ConfigSet> = [
      { title: '名称', dataIndex: 'name' },
      { title: '描述', dataIndex: 'description', render: (v?: string) => v || '-' },
      { title: '当前版本', dataIndex: 'version', width: 100, align: 'center' },
      { title: '创建时间', dataIndex: 'created_at', width: 160, render: formatTime },
      ...(!readOnly
        ? [
            {
              title: '操作',
              key: 'actions',
              width: 260,
              render: (_: unknown, r: ConfigSet) => (
                <Space size={0}>
                  <Button
                    type="link"
                    size="small"
                    onClick={() => {
                      setEditTarget(r);
                      setEditOpen(true);
                    }}
                  >
                    编辑内容
                  </Button>
                  <Button
                    type="link"
                    size="small"
                    icon={<HistoryOutlined />}
                    onClick={() => {
                      setHistoryTarget(r);
                      setDiffSnapshotId(undefined);
                      setDiffFilePath(undefined);
                      setHistoryOpen(true);
                    }}
                  >
                    历史
                  </Button>
                  <Button
                    type="link"
                    size="small"
                    danger
                    onClick={() =>
                      confirmDanger({
                        title: '删除配置',
                        content: `确认删除配置「${r.name}」？若仍有分组绑定该配置，需先解绑后才能删除。`,
                        onOk: () => deleteMutation.mutateAsync(r.id),
                      })
                    }
                  >
                    删除
                  </Button>
                </Space>
              ),
            },
          ]
        : []),
    ];

    return (
      <>
        <Typography.Paragraph type="secondary" style={{ marginBottom: 16 }}>
          配置是应用维度的统一配置实体，可同时包含配置文件、环境变量与启动参数。分组可绑定配置，绑定后该配置内容覆盖分组的当前配置；解绑后分组沿用解绑时的配置内容，不再随配置变更。
          {readOnly && ' 此外部托管应用的配置由开放 API 管理，平台仅只读展示。'}
        </Typography.Paragraph>
        {!readOnly && (
          <div style={{ marginBottom: 16, textAlign: 'right' }}>
            <Button type="primary" icon={<PlusOutlined />} onClick={() => { setCreateOpen(true); createForm.resetFields(); }}>
              新建配置
            </Button>
          </div>
        )}
        <Table
          rowKey="id"
          loading={isLoading}
          columns={columns}
          dataSource={configSets}
          pagination={false}
          locale={{
            emptyText: (
              <EmptyState
                title="暂无配置"
                actionText={readOnly ? undefined : '新建配置'}
                onAction={readOnly ? undefined : () => setCreateOpen(true)}
              />
            ),
          }}
        />
        <Modal
          title="新建配置"
          open={createOpen}
          onCancel={() => setCreateOpen(false)}
          onOk={() => createForm.submit()}
          confirmLoading={createMutation.isPending}
          destroyOnHidden
        >
          <Form layout="vertical" form={createForm} onFinish={(v) => createMutation.mutate(v)}>
            <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入名称' }]}>
              <Input placeholder="例如 生产配置 / 灰度配置" />
            </Form.Item>
            <Form.Item name="description" label="描述">
              <Input.TextArea rows={2} placeholder="可选" />
            </Form.Item>
          </Form>
        </Modal>
        <Modal
          title={editTarget ? `编辑配置内容 - ${editTarget.name}` : '编辑配置内容'}
          open={editOpen}
          onCancel={() => { setEditOpen(false); setEditTarget(undefined); }}
          onOk={() => editForm.submit()}
          confirmLoading={saveContentMutation.isPending}
          destroyOnHidden
          width={780}
        >
          <Form
            layout="vertical"
            form={editForm}
            onFinish={(v) => {
              saveContentMutation.mutate({
                content: buildConfigContentFromForm(v) as unknown as Record<string, any>,
                version: v.version,
              });
            }}
          >
            {/* editTarget 内容在上方 useEffect 中回显 */}
            <Form.Item name="version" hidden><Input /></Form.Item>
            <ConfigContentEditor />
          </Form>
        </Modal>
        <Modal
          title={historyTarget ? `配置历史 - ${historyTarget.name}` : '配置历史'}
          open={historyOpen}
          onCancel={() => {
            setHistoryOpen(false);
            setHistoryTarget(undefined);
            setDiffSnapshotId(undefined);
            setDiffFilePath(undefined);
          }}
          footer={null}
          width={920}
          destroyOnHidden
        >
          <Table<ConfigContentSnapshot>
            rowKey="id"
            size="small"
            columns={snapshotColumns}
            dataSource={configSetSnapshots}
            pagination={{ pageSize: 8, hideOnSinglePage: true }}
            locale={{ emptyText: <EmptyState title="暂无历史快照" description="保存配置内容后将自动生成" /> }}
            style={{ marginBottom: 16 }}
          />
          {diffSnapshotId ? (
            <>
              <Select
                placeholder="选择要比对的文件"
                style={{ width: '100%', marginBottom: 12 }}
                value={diffFilePath}
                onChange={setDiffFilePath}
                options={historyFileOptions}
                showSearch
                optionFilterProp="label"
              />
              {diffFilePath ? (
                configSetDiffLoading ? (
                  <EmptyState title="加载比对中..." />
                ) : configSetFileDiff ? (
                  <DiffViewer
                    original={configSetFileDiff.original}
                    modified={configSetFileDiff.modified}
                    language={configSetFileDiff.language}
                    height={420}
                  />
                ) : (
                  <EmptyState title="无法加载比对结果" />
                )
              ) : (
                <EmptyState title="请选择文件" description="左侧为历史快照，右侧为当前配置集版本" />
              )}
            </>
          ) : (
            <EmptyState title="点击快照行的「比对」开始" />
          )}
        </Modal>
      </>
    );
  }

  function GitTab({ appId }: { appId: number }) {
    const { message } = App.useApp();
    const [refSearch, setRefSearch] = useState('');
    const [debouncedSearch, setDebouncedSearch] = useState('');

    // 防抖搜索。
    useEffect(() => {
      const t = setTimeout(() => setDebouncedSearch(refSearch), 400);
      return () => clearTimeout(t);
    }, [refSearch]);

    const { data: app } = useQuery({
      queryKey: ['application', appId],
      queryFn: () => applicationApi.get(appId),
      enabled: !!appId,
    });

    const gitURL = app?.git_url;
    const defaultBranch = app?.default_branch;

    const { data: refs, isLoading: refsLoading, refetch } = useQuery({
      queryKey: ['application', appId, 'git-refs', debouncedSearch],
      queryFn: () => buildApi.listGitRefs(appId, debouncedSearch),
      enabled: !!appId && !!gitURL,
    });

    const onTestConnection = async () => {
      try {
        await refetch();
        message.success('Git 远程连接正常');
      } catch (e: any) {
        message.error(e?.message || '连接失败，请检查 git_url');
      }
    };

    if (!gitURL) {
      return (
        <EmptyState
          title="未配置 Git 源"
          description="请在「设置」Tab 填写应用的 Git 仓库地址与默认分支，构建时将自动使用该 Git 源。"
        />
      );
    }

    return (
      <Space direction="vertical" size="middle" style={{ width: '100%' }}>
        <Card title="应用 Git 源（只读）" size="small" extra={<Button size="small" icon={<ReloadOutlined />} onClick={onTestConnection}>测试连接</Button>}>
          <Descriptions column={1} size="small" bordered>
            <Descriptions.Item label="仓库地址">
              <Typography.Text code copyable>{gitURL}</Typography.Text>
            </Descriptions.Item>
            <Descriptions.Item label="默认分支">
              <Tag color="blue">{defaultBranch || '-'}</Tag>
            </Descriptions.Item>
            <Descriptions.Item label="说明">
              该 Git 源由应用设置中的 git_url 派生，构建时自动使用，无需单独维护。
              如需修改请前往「设置」Tab（git_url 创建应用后不可更改，仅默认分支可调整）。
            </Descriptions.Item>
          </Descriptions>
        </Card>

        <Card title="远程分支" size="small">
          <Input.Search
            placeholder="模糊搜索分支名"
            allowClear
            value={refSearch}
            onChange={(e) => setRefSearch(e.target.value)}
            style={{ width: 320, marginBottom: 16 }}
          />
          <Table<GitRef>
            rowKey="name"
            loading={refsLoading}
            columns={[
              { title: '分支名', dataIndex: 'name' },
              { title: '类型', dataIndex: 'type', width: 100, render: (v: string) => <Tag>{v}</Tag> },
            ]}
            dataSource={refs}
            pagination={{ pageSize: 20, size: 'small' }}
            locale={{ emptyText: <EmptyState title="暂无分支" description="请确认 git_url 可访问且包含分支" /> }}
          />
        </Card>
      </Space>
    );
  }

  function MembersTab({ appId, ownerId }: { appId: number; ownerId?: number }) {
    const { message } = App.useApp();
    const queryClient = useQueryClient();
    const [addOpen, setAddOpen] = useState(false);
    const [form] = Form.useForm<{ user_id: number; role_id: number }>();

    const { data: membersPage, isLoading } = useQuery({
      queryKey: ['application', appId, 'members'],
      queryFn: () => applicationApi.listMembers(appId, { page: 1, size: 100 }),
      enabled: !!appId,
    });

    // 角色（application scope）
    const { data: rolesPage } = useQuery({
      queryKey: ['roles', 'application'],
      queryFn: () => rbacApi.listRoles({ scope: 'application', page: 1, size: 100 }),
    });

    // 当前应用所在 workspace 的成员作为候选用户源
    const { data: app } = useQuery({
      queryKey: ['application', appId],
      queryFn: () => applicationApi.get(appId),
      enabled: !!appId,
    });
    const workspaceId = app?.workspace_id;
    const { data: wsMembers } = useQuery({
      queryKey: ['workspace', workspaceId, 'members'],
      queryFn: () => workspaceApi.listMembers(workspaceId!),
      enabled: !!workspaceId,
    });

    const memberUserIds = new Set((membersPage?.items || []).map((m) => m.user_id));

    const addMutation = useMutation({
      mutationFn: (v: { user_id: number; role_id: number }) => applicationApi.addMember(appId, v),
      onSuccess: () => {
        message.success('成员已添加');
        setAddOpen(false);
        form.resetFields();
        queryClient.invalidateQueries({ queryKey: ['application', appId, 'members'] });
      },
      onError: (e: any) => message.error(e?.message || '添加失败'),
    });

    const removeMutation = useMutation({
      mutationFn: (userId: number) => applicationApi.removeMember(appId, userId),
      onSuccess: () => {
        message.success('成员已移除');
        queryClient.invalidateQueries({ queryKey: ['application', appId, 'members'] });
      },
      onError: (e: any) => message.error(e?.message || '移除失败'),
    });

    const columns: ColumnsType<ApplicationMember> = [
      {
        title: '用户',
        dataIndex: 'user_id',
        render: (_, r) => (
          <Space direction="vertical" size={0}>
            <Typography.Text strong>{r.display_name || r.username || `用户 ${r.user_id}`}</Typography.Text>
            {r.email && <Typography.Text type="secondary" style={{ fontSize: 12 }}>{r.email}</Typography.Text>}
          </Space>
        ),
      },
      { title: '角色', dataIndex: 'role_name', width: 160, render: (v?: string, r?: ApplicationMember) => v || (r?.role_id ? `角色 ${r.role_id}` : '-') },
      { title: '状态', dataIndex: 'status', width: 100, render: (s: string) => <ResourceStatus status={s} text={s} /> },
      { title: '加入时间', dataIndex: 'joined_at', width: 180, render: formatTime },
      {
        title: '操作',
        key: 'actions',
        width: 100,
        render: (_, r) => (
          <Button
            type="link"
            size="small"
            danger
            icon={<DeleteOutlined />}
            disabled={r.user_id === ownerId}
            onClick={() =>
              confirmDanger({
                title: '移除成员',
                content: `确认将「${r.display_name || r.username || r.user_id}」移出应用？`,
                onOk: () => removeMutation.mutateAsync(r.user_id),
              })
            }
          >
            移除
          </Button>
        ),
      },
    ];

    return (
      <>
        <div style={{ marginBottom: 16, textAlign: 'right' }}>
          <Button type="primary" icon={<PlusOutlined />} onClick={() => setAddOpen(true)}>
            添加成员
          </Button>
        </div>
        <Table<ApplicationMember>
          rowKey="id"
          loading={isLoading}
          columns={columns}
          dataSource={membersPage?.items}
          pagination={false}
          locale={{ emptyText: <EmptyState title="暂无成员" actionText="添加成员" onAction={() => setAddOpen(true)} /> }}
        />
        <Modal
          title="添加成员"
          open={addOpen}
          onCancel={() => setAddOpen(false)}
          onOk={() => form.submit()}
          confirmLoading={addMutation.isPending}
          destroyOnHidden
        >
          <Form layout="vertical" form={form} onFinish={(v) => addMutation.mutate(v)}>
            <Form.Item name="user_id" label="用户" rules={[{ required: true, message: '请选择用户' }]}>
              <Select
                placeholder="从工作空间成员中选择"
                showSearch
                optionFilterProp="label"
                options={(wsMembers || [])
                  .filter((m: WorkspaceMember) => !memberUserIds.has(m.user_id))
                  .map((m: WorkspaceMember) => ({
                    label: m.display_name || m.username || `用户 ${m.user_id}`,
                    value: m.user_id,
                  }))}
              />
            </Form.Item>
            <Form.Item name="role_id" label="角色" rules={[{ required: true, message: '请选择角色' }]}>
              <Select
                placeholder="选择应用角色"
                options={(rolesPage?.items || []).map((r) => ({ label: r.name, value: r.id }))}
              />
            </Form.Item>
          </Form>
        </Modal>
      </>
    );
  }

  // 应用探活配置卡片：就绪由端口连通或进程关键字判定。
  // 发布时注入原生 Readiness+Liveness Probe；分组 Pod 列表会按同策略主动复核。
  function ProbeConfigCard({ app, onSaved }: { app: Application; onSaved: () => void }) {
    const { message } = App.useApp();
    const [form] = Form.useForm<ProbeConfig>();
    const enabled = Form.useWatch('enabled', form);

    const saveMutation = useMutation({
      mutationFn: (v: ProbeConfig) => applicationApi.update(app.id, { probe: v }),
      onSuccess: () => {
        message.success('探活配置已保存');
        onSaved();
      },
      onError: (e: any) => message.error(e?.message || '保存失败'),
    });

    // 同步表单初始值：应用返回的 probe 可能为 null（未配置）。
    useEffect(() => {
      const p = app.probe || { enabled: false };
      form.setFieldsValue({
        enabled: !!p.enabled,
        method: p.method || 'tcp',
        port: p.port,
        process_keyword: p.process_keyword,
      });
    }, [form, app.probe]);

    const onFinish = (v: ProbeConfig) => {
      // 关闭探活时只传 enabled，清空其它字段避免后端校验失败。
      if (!v.enabled) {
        saveMutation.mutate({ enabled: false });
        return;
      }
      // 按 method 校验必填项。
      if ((v.method === 'tcp' || v.method === 'both') && !v.port) {
        message.error('TCP 探活方式下端口必填');
        return;
      }
      if ((v.method === 'process' || v.method === 'both') && !v.process_keyword) {
        message.error('进程探活方式下进程关键字必填');
        return;
      }
      // 仅提交策略字段；超时/周期等由后端固定默认，不向用户暴露。
      saveMutation.mutate({
        enabled: true,
        method: v.method,
        port: v.port,
        process_keyword: v.process_keyword,
      });
    };

    return (
      <Form
        form={form}
        layout="vertical"
        style={{ maxWidth: 600 }}
        initialValues={{
          enabled: false,
          method: 'tcp',
        }}
        onFinish={onFinish}
      >
        <Form.Item
          name="enabled"
          label="启用探活"
          valuePropName="checked"
          extra="就绪由端口连通或进程关键字判定；发布注入原生 Readiness/Liveness Probe，分组页列表会主动复核"
        >
          <Switch checkedChildren="开" unCheckedChildren="关" />
        </Form.Item>
        {enabled && (
          <>
            <Form.Item name="method" label="探活方式">
              <Select
                options={[
                  { label: 'TCP 端口（连接本机端口成功即就绪）', value: 'tcp' },
                  { label: '进程关键字（容器内 pgrep -f <关键字> 命中即就绪）', value: 'process' },
                  { label: '两者（TCP 与进程同时通过才就绪）', value: 'both' },
                ]}
              />
            </Form.Item>
            <Form.Item shouldUpdate noStyle>
              {({ getFieldValue }) => {
                const m = getFieldValue('method');
                return (m === 'tcp' || m === 'both') ? (
                  <Form.Item name="port" label="TCP 端口" rules={[{ required: true, message: '请输入端口' }]}>
                    <InputNumber min={1} max={65535} placeholder="8080" style={{ width: '100%' }} />
                  </Form.Item>
                ) : null;
              }}
            </Form.Item>
            <Form.Item shouldUpdate noStyle>
              {({ getFieldValue }) => {
                const m = getFieldValue('method');
                return (m === 'process' || m === 'both') ? (
                  <Form.Item name="process_keyword" label="进程关键字" rules={[{ required: true, message: '请输入进程关键字' }]} extra="如 java / nginx / python，容器内执行 pgrep -f <关键字>">
                    <Input placeholder="java" />
                  </Form.Item>
                ) : null;
              }}
            </Form.Item>
          </>
        )}
        <Button type="primary" htmlType="submit" loading={saveMutation.isPending}>
          保存探活配置
        </Button>
      </Form>
    );
  }

  // 注：应用级默认端口模板（default_ports）已删除——分组不再配置网络端口，所有端口默认暴露，外部直连稳定 Pod IP。

  function SettingsTab({ app, readOnly }: { app?: Application; readOnly?: boolean }) {
    const { message } = App.useApp();
    const queryClient = useQueryClient();
    const [form] = Form.useForm();

    const updateMutation = useMutation({
      mutationFn: (v: Partial<CreateApplicationInput>) => applicationApi.update(app!.id, v),
      onSuccess: () => {
        message.success('应用已更新');
        queryClient.invalidateQueries({ queryKey: ['application', app!.id] });
      },
      onError: (e: any) => message.error(e?.message || '更新失败'),
    });

    const deleteMutation = useMutation({
      mutationFn: () => applicationApi.delete(app!.id),
      onSuccess: () => {
        message.success('应用已删除');
        navigate('/workspaces');
      },
      onError: (e: any) => message.error(e?.message || '删除失败'),
    });

    if (!app) return null;

    return (
      <Space direction="vertical" size="large" style={{ width: '100%' }}>
        <Card title="基本信息" size="small">
          <Form
            layout="vertical"
            form={form}
            initialValues={{
              display_name: app.display_name,
              description: app.description,
              app_type: app.app_type,
              workload_type: app.workload_type,
              git_url: app.git_url,
              default_branch: app.default_branch,
              version: app.version,
            }}
            onFinish={(v) => updateMutation.mutate(v)}
            style={{ maxWidth: 600 }}
            disabled={readOnly}
          >
            <Form.Item label="标识名" extra="创建后不可修改">
              <Input value={app.name} disabled />
            </Form.Item>
            <Form.Item name="display_name" label="显示名称">
              <Input />
            </Form.Item>
            <Form.Item name="description" label="描述">
              <Input.TextArea rows={2} />
            </Form.Item>
            <Form.Item name="app_type" label="应用类型">
              <Select
                allowClear
                options={[
                  { label: 'Web 服务', value: 'web' },
                  { label: 'Worker', value: 'worker' },
                  { label: '定时任务', value: 'job' },
                  { label: 'AI 推理', value: 'inference' },
                ]}
              />
            </Form.Item>
            <Form.Item name="workload_type" label="工作负载类型">
              <Select
                allowClear
                options={[
                  { label: 'Deployment', value: 'deployment' },
                  { label: 'StatefulSet', value: 'statefulset' },
                  { label: 'CronJob', value: 'cronjob' },
                ]}
              />
            </Form.Item>
            <Form.Item label="开发语言" extra="创建应用时选定，用于新建构建时过滤基础镜像；如需变更请新建应用">
              <Select
                disabled
                value={app.language}
                placeholder="未设置"
                options={LANGUAGE_OPTIONS.map((o) => ({ label: o.label, value: o.value }))}
              />
            </Form.Item>
            <Form.Item name="git_url" label="Git 仓库地址" extra="创建应用后不可修改">
              <Input disabled placeholder="https://github.com/org/repo.git" />
            </Form.Item>
            <Form.Item name="default_branch" label="默认分支">
              <Input placeholder="main" />
            </Form.Item>
            <Form.Item name="version" hidden>
              <Input />
            </Form.Item>
            {!readOnly && (
              <Button type="primary" htmlType="submit" loading={updateMutation.isPending}>
                保存
              </Button>
            )}
          </Form>
        </Card>

        {!readOnly && (
        <>
        <Card title="应用探活" size="small" extra={<Tag color="blue">Readiness + 列表复核</Tag>}>
          <ProbeConfigCard app={app} onSaved={() => queryClient.invalidateQueries({ queryKey: ['application', app.id] })} />
        </Card>
        </>
        )}

        <Card title="元信息" size="small">
          <Descriptions column={2} size="small" bordered>
            <Descriptions.Item label="ID">{app.id}</Descriptions.Item>
            <Descriptions.Item label="UUID">{app.uuid}</Descriptions.Item>
            <Descriptions.Item label="编号">{app.code || '-'}</Descriptions.Item>
            <Descriptions.Item label="应用类型">{app.app_type || '-'}</Descriptions.Item>
            <Descriptions.Item label="生命周期">{app.lifecycle || '-'}</Descriptions.Item>
            <Descriptions.Item label="版本">{app.version}</Descriptions.Item>
            <Descriptions.Item label="创建时间">{formatTime(app.created_at)}</Descriptions.Item>
            <Descriptions.Item label="更新时间">{formatRelative(app.updated_at)}</Descriptions.Item>
          </Descriptions>
        </Card>

        {!readOnly && (
        <Card title="危险操作" size="small" style={{ borderColor: '#ffccc7' }}>
          <Button
            danger
            icon={<DeleteOutlined />}
            onClick={() =>
              confirmDanger({
                title: '删除应用',
                content: `确认删除应用「${app.display_name || app.name}」？此操作将级联删除其下所有分组、构建与发布记录，且不可恢复。`,
                okText: '删除',
                onOk: () => deleteMutation.mutateAsync(),
              })
            }
          >
            删除应用
          </Button>
        </Card>
        )}
      </Space>
    );
  }
}
