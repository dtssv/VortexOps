import { useState } from 'react';
import {
  App,
  Button,
  Card,
  Form,
  Input,
  Modal,
  Select,
  Space,
  Table,
  Tag,
  Tooltip,
  Typography,
} from 'antd';
import {
  PlusOutlined,
  EditOutlined,
  DeleteOutlined,
  ReloadOutlined,
  SearchOutlined,
  BookOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { ColumnsType } from 'antd/es/table';
import { PageContainer } from '@/components/PageContainer';
import { kbApi, type KBDocument, type KBCategory } from '@/api/diagnosis';
import { confirmDanger } from '@/utils/action';
import { formatTime } from '@/utils/format';

const { Text, Paragraph } = Typography;

/**
 * KnowledgeBasePage AI 助手知识库管理。
 *
 * 功能：
 * - 文档 CRUD（按分类过滤、关键词搜索）
 * - 自动分块向量化（创建/更新时后台触发）
 * - 手动重建索引
 * - RAG 检索测试
 */
export default function KnowledgeBasePage() {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const [modalOpen, setModalOpen] = useState(false);
  const [editTarget, setEditTarget] = useState<KBDocument | null>(null);
  const [form] = Form.useForm();
  const [filters, setFilters] = useState<{ category?: string; search?: string; status?: string }>({});
  const [searchModal, setSearchModal] = useState(false);
  const [searchQuery, setSearchQuery] = useState('');
  const [searchResults, setSearchResults] = useState<{ id: number; document_title: string; content: string; score: number; category_code: string }[] | null>(null);

  const { data: categories } = useQuery({
    queryKey: ['kb-categories'],
    queryFn: () => kbApi.listCategories(),
    staleTime: 5 * 60_000,
  });

  const { data, isLoading } = useQuery({
    queryKey: ['kb-documents', filters],
    queryFn: () => kbApi.listDocuments({ ...filters, page: 1, size: 100 }),
  });
  const items = data?.items ?? [];

  const categoryMap = new Map<number, KBCategory>();
  (categories || []).forEach((c) => categoryMap.set(c.id, c));

  const openCreate = () => {
    setEditTarget(null);
    form.resetFields();
    form.setFieldsValue({
      source_type: 'manual',
      category_id: (categories && categories[0]?.id) || undefined,
      tags: [],
    });
    setModalOpen(true);
  };

  const openEdit = (record: KBDocument) => {
    setEditTarget(record);
    form.setFieldsValue({
      category_id: record.category_id,
      title: record.title,
      source_type: record.source_type,
      source_url: record.source_url,
      content: record.content,
      tags: record.tags,
      status: record.status,
    });
    setModalOpen(true);
  };

  const submitMutation = useMutation({
    mutationFn: async (v: any) => {
      const body: Partial<KBDocument> = {
        category_id: v.category_id,
        title: v.title,
        source_type: v.source_type || 'manual',
        source_url: v.source_url,
        content: v.content,
        tags: v.tags || [],
        status: v.status,
      };
      if (editTarget) {
        return kbApi.updateDocument(editTarget.id, body);
      }
      return kbApi.createDocument(body);
    },
    onSuccess: () => {
      message.success(editTarget ? '已更新，正在后台重新分块向量化' : '已创建，正在后台分块向量化');
      setModalOpen(false);
      queryClient.invalidateQueries({ queryKey: ['kb-documents'] });
    },
    onError: (e: any) => {
      message.error(e?.message || '保存失败');
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (id: number) => kbApi.deleteDocument(id),
    onSuccess: () => {
      message.success('已删除');
      queryClient.invalidateQueries({ queryKey: ['kb-documents'] });
    },
    onError: (e: any) => message.error(e?.message || '删除失败'),
  });

  const reindexMutation = useMutation({
    mutationFn: (id: number) => kbApi.reindexDocument(id),
    onSuccess: () => {
      message.success('已重新索引');
      queryClient.invalidateQueries({ queryKey: ['kb-documents'] });
    },
    onError: (e: any) => message.error(e?.message || '索引失败'),
  });

  const searchMutation = useMutation({
    mutationFn: (q: string) => kbApi.search({ query: q, top_k: 5 }),
    onSuccess: (res) => {
      setSearchResults(res || []);
    },
    onError: (e: any) => message.error(e?.message || '检索失败'),
  });

  const columns: ColumnsType<KBDocument> = [
    {
      title: '标题',
      dataIndex: 'title',
      width: 240,
      render: (v: string, r) => (
        <Space direction="vertical" size={0}>
          <Text strong>{v}</Text>
          {r.tags && r.tags.length > 0 && (
            <Space size={2} wrap>
              {r.tags.map((t) => <Tag key={t} style={{ fontSize: 10 }}>{t}</Tag>)}
            </Space>
          )}
        </Space>
      ),
    },
    {
      title: '分类',
      dataIndex: 'category_id',
      width: 120,
      render: (v: number) => {
        const c = categoryMap.get(v);
        return c ? <Tag color="blue">{c.name}</Tag> : <Text type="secondary">-</Text>;
      },
    },
    {
      title: '来源',
      dataIndex: 'source_type',
      width: 100,
      render: (v: string) => <Tag>{v}</Tag>,
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 100,
      render: (v: string) => {
        const color = v === 'active' ? 'green' : v === 'indexing' ? 'orange' : 'default';
        return <Tag color={color}>{v}</Tag>;
      },
    },
    {
      title: '分块',
      dataIndex: 'chunk_count',
      width: 80,
      render: (v: number) => <Text>{v}</Text>,
    },
    {
      title: '更新时间',
      dataIndex: 'updated_at',
      width: 160,
      render: (v: string) => formatTime(v),
    },
    {
      title: '操作',
      width: 200,
      render: (_, r) => (
        <Space size={4}>
          <Tooltip title="编辑">
            <Button type="link" size="small" icon={<EditOutlined />} onClick={() => openEdit(r)} />
          </Tooltip>
          <Tooltip title="重建索引">
            <Button
              type="link"
              size="small"
              icon={<ThunderboltOutlined />}
              loading={reindexMutation.isPending}
              onClick={() => reindexMutation.mutate(r.id)}
            />
          </Tooltip>
          <Tooltip title="删除">
            <Button
              type="link"
              size="small"
              danger
              icon={<DeleteOutlined />}
              onClick={() =>
                confirmDanger({
                  title: `删除文档「${r.title}」？`,
                  content: '将同时删除其分块与向量索引。',
                  onOk: () => deleteMutation.mutate(r.id),
                })
              }
            />
          </Tooltip>
        </Space>
      ),
    },
  ];

  return (
    <PageContainer>
      <Card
        title={
          <Space>
            <BookOutlined />
            <span>AI 助手知识库</span>
            <Text type="secondary" style={{ fontSize: 12 }}>
              向量 RAG 检索 · 自动分块向量化
            </Text>
          </Space>
        }
        extra={
          <Space>
            <Select
              placeholder="分类"
              allowClear
              style={{ width: 140 }}
              value={filters.category}
              onChange={(v) => setFilters((f) => ({ ...f, category: v }))}
              options={(categories || []).map((c) => ({ label: c.name, value: c.code }))}
            />
            <Input
              placeholder="搜索标题/内容"
              allowClear
              prefix={<SearchOutlined />}
              style={{ width: 200 }}
              onPressEnter={(e) => setFilters((f) => ({ ...f, search: (e.target as HTMLInputElement).value }))}
            />
            <Button icon={<ReloadOutlined />} onClick={() => queryClient.invalidateQueries({ queryKey: ['kb-documents'] })}>
              刷新
            </Button>
            <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
              新建文档
            </Button>
            <Button onClick={() => { setSearchModal(true); setSearchResults(null); setSearchQuery(''); }}>
              检索测试
            </Button>
          </Space>
        }
      >
        <Table
          rowKey="id"
          loading={isLoading}
          columns={columns}
          dataSource={items}
          pagination={{ pageSize: 20, showSizeChanger: true }}
        />
      </Card>

      <Modal
        open={modalOpen}
        title={editTarget ? '编辑文档' : '新建文档'}
        onCancel={() => setModalOpen(false)}
        onOk={() => form.submit()}
        confirmLoading={submitMutation.isPending}
        width={720}
        destroyOnClose
      >
        <Form
          form={form}
          layout="vertical"
          onFinish={(v) => submitMutation.mutate(v)}
        >
          <Form.Item name="category_id" label="分类" rules={[{ required: true, message: '请选择分类' }]}>
            <Select
              placeholder="选择分类"
              options={(categories || []).map((c) => ({ label: c.name, value: c.id }))}
            />
          </Form.Item>
          <Form.Item name="title" label="标题" rules={[{ required: true, message: '请输入标题' }]}>
            <Input placeholder="文档标题" />
          </Form.Item>
          <Space style={{ display: 'flex' }} size={12}>
            <Form.Item name="source_type" label="来源类型" style={{ width: 160 }}>
              <Select
                options={[
                  { label: '手动', value: 'manual' },
                  { label: 'Markdown', value: 'markdown' },
                  { label: 'URL', value: 'url' },
                  { label: 'FAQ', value: 'faq' },
                ]}
              />
            </Form.Item>
            <Form.Item name="source_url" label="来源 URL" style={{ flex: 1 }}>
              <Input placeholder="可选，来源链接" />
            </Form.Item>
          </Space>
          <Form.Item name="tags" label="标签">
            <Select mode="tags" placeholder="输入标签后回车" tokenSeparators={[',']} />
          </Form.Item>
          {editTarget && (
            <Form.Item name="status" label="状态">
              <Select
                options={[
                  { label: '启用', value: 'active' },
                  { label: '归档', value: 'archived' },
                ]}
              />
            </Form.Item>
          )}
          <Form.Item name="content" label="内容（支持 Markdown）" rules={[{ required: true, message: '请输入内容' }]}>
            <Input.TextArea rows={12} placeholder="文档全文，建议按段落组织（双换行分段）。系统会自动分块并向量化。" />
          </Form.Item>
          <Paragraph type="secondary" style={{ fontSize: 12 }}>
            保存后系统会自动分块（每块约 800 字符，100 字符重叠）并调用向量嵌入服务向量化。
            若内容较长，向量化可能需要数秒。
          </Paragraph>
        </Form>
      </Modal>

      <Modal
        open={searchModal}
        title="RAG 检索测试"
        onCancel={() => setSearchModal(false)}
        footer={null}
        width={680}
        destroyOnClose
      >
        <Paragraph type="secondary" style={{ fontSize: 12 }}>
          输入查询语句，系统会将其向量化后从知识库召回 Top 5 相似分块（余弦相似度）。
        </Paragraph>
        <Input.Search
          placeholder="输入查询，例如：Pod 启动失败如何排查"
          enterButton="检索"
          value={searchQuery}
          onChange={(e) => setSearchQuery(e.target.value)}
          loading={searchMutation.isPending}
          onSearch={(v) => searchMutation.mutate(v)}
        />
        {searchResults && (
          <div style={{ marginTop: 16 }}>
            <Text type="secondary" style={{ fontSize: 12 }}>
              命中 {searchResults.length} 条
            </Text>
            {searchResults.map((r) => (
              <Card
                key={r.id}
                size="small"
                style={{ marginTop: 8 }}
                title={
                  <Space>
                    <Text strong style={{ fontSize: 13 }}>{r.document_title}</Text>
                    <Tag color="blue">{r.category_code || 'general'}</Tag>
                    <Tag color="green">score: {(r.score * 100).toFixed(1)}%</Tag>
                  </Space>
                }
              >
                <Text style={{ fontSize: 12, whiteSpace: 'pre-wrap' }}>
                  {r.content.length > 300 ? r.content.slice(0, 300) + '...' : r.content}
                </Text>
              </Card>
            ))}
          </div>
        )}
      </Modal>
    </PageContainer>
  );
}
