import { useEffect, useMemo, useState } from 'react';
import { App, Form, InputNumber, Modal, Radio, Select, Slider, Space, Switch, Table, Tag, Typography } from 'antd';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { groupApi } from '@/api/applications';
import { releaseApi } from '@/api/releases';
import { buildApi } from '@/api/builds';
import type { Image, PodSummary, ReleaseStrategy, TriggerReleaseInput } from '@/types';

export interface PublishModalProps {
  open: boolean;
  onClose: () => void;
  applicationId: number;
  fixedImageId?: number;
  selectableImages?: Image[];
  excludeImageId?: number;
  fixedGroupId?: number;
  /** 默认发布策略（构建页可默认一次性发布） */
  defaultStrategy?: ReleaseStrategy;
  onPublished?: (releaseId: number) => void;
}

type Strategy = ReleaseStrategy;

export function PublishModal({
  open,
  onClose,
  applicationId,
  fixedImageId,
  selectableImages,
  excludeImageId,
  fixedGroupId,
  defaultStrategy = 'rolling',
  onPublished,
}: PublishModalProps) {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const [form] = Form.useForm();
  const [strategy, setStrategy] = useState<Strategy>(defaultStrategy);
  const [groupId, setGroupId] = useState<number | undefined>(fixedGroupId);
  const [imageId, setImageId] = useState<number | undefined>(fixedImageId);
  const [selectedPods, setSelectedPods] = useState<string[]>([]);

  const effectiveGroupId = fixedGroupId ?? groupId;

  const { data: groupsPage } = useQuery({
    queryKey: ['app', applicationId, 'groups-for-publish'],
    queryFn: () => groupApi.list(applicationId, { size: 200 }),
    enabled: !!applicationId && open && !fixedGroupId,
  });

  const { data: fetchedImages } = useQuery({
    queryKey: ['app', applicationId, 'images-for-publish'],
    queryFn: () => buildApi.listImages(applicationId, { size: 200 }),
    enabled: !!applicationId && open && !fixedImageId && !selectableImages,
  });

  const { data: pods, isLoading: podsLoading } = useQuery({
    queryKey: ['group', effectiveGroupId, 'pods-for-publish'],
    queryFn: () => groupApi.listPods(effectiveGroupId!),
    enabled: !!effectiveGroupId && open,
  });

  // 注：稳定 IP 始终由发布服务分配（不再可选）；CNI 支持情况由集群 network_profile 决定。

  const images = useMemo(() => {
    const list = selectableImages ?? fetchedImages?.items ?? [];
    return excludeImageId ? list.filter((i: Image) => i.id !== excludeImageId) : list;
  }, [selectableImages, fetchedImages, excludeImageId]);

  useEffect(() => {
    if (open) {
      setStrategy(defaultStrategy);
      setGroupId(fixedGroupId);
      setImageId(fixedImageId);
      setSelectedPods([]);
      form.resetFields();
      form.setFieldsValue({
        strategy: defaultStrategy,
        batch_size: 1,
        batch_interval_sec: 30,
        target_percentage: 100,
        auto_rollback_on_failure: false,
      });
    }
  }, [open, fixedGroupId, fixedImageId, defaultStrategy, form]);

  const triggerMutation = useMutation({
    mutationFn: (input: TriggerReleaseInput) => releaseApi.trigger(input.group_id, input),
    onSuccess: (rel, input) => {
      message.success('发布已触发');
      queryClient.invalidateQueries({ queryKey: ['group', input.group_id] });
      onPublished?.(rel.id);
      onClose();
    },
    onError: (e: any) => message.error(e?.message || '发布失败'),
  });

  const handleSubmit = async () => {
    const v = await form.validateFields();
    const gid = effectiveGroupId;
    if (!gid) {
      message.error('请选择目标分组');
      return;
    }
    const img = imageId ?? fixedImageId;
    if (!img) {
      message.error('请选择镜像');
      return;
    }
    const input: TriggerReleaseInput = {
      group_id: gid,
      image_id: img,
      strategy: v.strategy,
      max_surge: v.max_surge != null ? String(v.max_surge) : undefined,
      max_unavailable: v.max_unavailable != null ? String(v.max_unavailable) : undefined,
      batch_size: v.batch_size,
      batch_interval_sec: v.batch_interval_sec,
      auto_rollback_on_failure: v.auto_rollback_on_failure,
    };
    if (v.strategy === 'percentage') {
      input.target_percentage = v.target_percentage;
    }
    if (v.strategy === 'machine_count' && selectedPods.length > 0) {
      input.target_pod_names = selectedPods;
    }
    triggerMutation.mutate(input);
  };

  const isBatch = strategy === 'percentage' || strategy === 'machine_count';
  const showPodPicker = !!effectiveGroupId && strategy === 'machine_count';

  return (
    <Modal
      title="发布"
      open={open}
      onCancel={onClose}
      onOk={handleSubmit}
      confirmLoading={triggerMutation.isPending}
      destroyOnClose
      width={showPodPicker ? 760 : 640}
    >
      <Form form={form} layout="vertical" initialValues={{ strategy: defaultStrategy, batch_size: 1, batch_interval_sec: 30, target_percentage: 100, auto_rollback_on_failure: false }}>
        {!fixedGroupId && (
          <Form.Item label="目标分组" required>
            <Select
              placeholder="选择要发布到的分组"
              value={groupId}
              onChange={(v) => {
                setGroupId(v);
                setSelectedPods([]);
              }}
              options={(groupsPage?.items ?? []).map((g) => ({
                label: `${g.display_name || g.name}（${g.environment}）`,
                value: g.id,
              }))}
            />
          </Form.Item>
        )}
        {!fixedImageId && (
          <Form.Item label="镜像版本" required>
            <Select<Image['id']>
              placeholder="选择镜像版本"
              value={imageId}
              onChange={setImageId}
              options={images.map((i) => ({ label: i.version_label || i.tag, value: i.id }))}
              optionFilterProp="label"
              showSearch
              optionRender={(option) => {
                const i = images.find((img) => img.id === option.value);
                if (!i) return option.label;
                return (
                  <Space size={4} wrap align="center" style={{ padding: '2px 0' }}>
                    <span style={{ fontFamily: 'monospace' }}>{i.version_label || i.tag}</span>
                    {i.git_branch ? <Tag color="blue" style={{ margin: 0 }}>{i.git_branch}</Tag> : null}
                    {i.git_commit ? <Tag color="geekblue" style={{ margin: 0 }}>{i.git_commit.slice(0, 8)}</Tag> : null}
                    {i.git_commit_message ? (
                      <Typography.Text type="secondary" ellipsis style={{ maxWidth: 320, margin: 0 }}>
                        {i.git_commit_message}
                      </Typography.Text>
                    ) : null}
                  </Space>
                );
              }}
              labelRender={(props) => {
                const i = images.find((img) => img.id === props.value);
                if (!i) return props.label;
                return (
                  <Space size={6} align="center">
                    <span style={{ fontFamily: 'monospace' }}>{i.version_label || i.tag}</span>
                    {i.git_commit ? <Tag color="geekblue" style={{ margin: 0 }}>{i.git_commit.slice(0, 8)}</Tag> : null}
                  </Space>
                );
              }}
            />
          </Form.Item>
        )}
        <Form.Item name="strategy" label="发布策略">
          <Radio.Group onChange={(e) => { setStrategy(e.target.value); setSelectedPods([]); }}>
            <Radio.Button value="recreate">一次性发布</Radio.Button>
            <Radio.Button value="rolling">滚动更新</Radio.Button>
            <Radio.Button value="percentage">按百分比</Radio.Button>
            <Radio.Button value="machine_count">按机器数（分批）</Radio.Button>
          </Radio.Group>
        </Form.Item>

        {strategy === 'recreate' && (
          <p style={{ color: '#888', marginTop: -8, marginBottom: 16 }}>
            一次性全量替换：先停止旧 Pod，再启动新版本（非滚动）。
          </p>
        )}

        {strategy === 'percentage' && (
          <Form.Item name="target_percentage" label="目标百分比（分母=分组副本数）" rules={[{ required: true }]} extra="候选副本数=分组副本数×百分比，按批次逐步扩容候选并晋升">
            <Slider min={1} max={100} marks={{ 1: '1%', 25: '25%', 50: '50%', 100: '100%' }} />
          </Form.Item>
        )}

        {strategy === 'machine_count' && (
          <Form.Item name="batch_size" label="每批次机器数" rules={[{ required: true }]} extra="每一批次发布的机器（Pod）数量；按批次间隔逐批发布直到全量（或所选 Pod 全部完成）">
            <InputNumber min={1} style={{ width: '100%' }} />
          </Form.Item>
        )}

        {isBatch && strategy === 'percentage' && (
          <Form.Item name="batch_size" label="批次大小（每批扩容候选数，0=自动）">
            <InputNumber min={0} style={{ width: '100%' }} />
          </Form.Item>
        )}

        {isBatch && (
          <Form.Item name="batch_interval_sec" label="批次间隔（秒）" rules={[{ required: true }]} extra="每批次发布完成后等待该秒数再发布下一批；期间若有新发布将中断当前批次">
            <InputNumber min={0} style={{ width: '100%' }} />
          </Form.Item>
        )}

        {showPodPicker && (
          <Form.Item label="目标 Pod（可选）" extra="不选则发布到全部分组副本；勾选后仅对所选 Pod 分批发布">
            <Table<PodSummary>
              rowKey="name"
              size="small"
              loading={podsLoading}
              dataSource={pods ?? []}
              pagination={false}
              scroll={{ y: 200 }}
              rowSelection={{
                selectedRowKeys: selectedPods,
                onChange: (keys) => setSelectedPods(keys.map(String)),
              }}
              columns={[
                { title: 'Pod', dataIndex: 'name' },
                { title: '状态', dataIndex: 'status', width: 90 },
                { title: '节点', dataIndex: 'node_name', width: 120, render: (v?: string) => v || '-' },
              ]}
              locale={{ emptyText: '暂无 Pod，将按分组副本数全量发布' }}
            />
          </Form.Item>
        )}

        {strategy === 'rolling' && (
          <>
            <Form.Item name="max_surge" label="Max Surge">
              <InputNumber style={{ width: '100%' }} placeholder="如 25" addonAfter="%" />
            </Form.Item>
            <Form.Item name="max_unavailable" label="Max Unavailable">
              <InputNumber style={{ width: '100%' }} placeholder="如 25" addonAfter="%" />
            </Form.Item>
          </>
        )}

        <Form.Item name="auto_rollback_on_failure" label="失败自动回滚" valuePropName="checked">
          <Switch />
        </Form.Item>
      </Form>
    </Modal>
  );
}

export default PublishModal;
