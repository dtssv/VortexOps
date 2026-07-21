import { Empty, Button } from 'antd';
import { InboxOutlined } from '@ant-design/icons';
import type { ReactNode } from 'react';

export function EmptyState({
  title,
  description,
  action,
  actionText,
  onAction,
}: {
  title?: string;
  description?: ReactNode;
  action?: ReactNode;
  actionText?: string;
  onAction?: () => void;
}) {
  return (
    <Empty
      image={<InboxOutlined style={{ fontSize: 64, color: '#bfbfbf' }} />}
      imageStyle={{ height: 64 }}
      description={description || title || '暂无数据'}
    >
      {action}
      {actionText && onAction && (
        <Button type="primary" onClick={onAction}>
          {actionText}
        </Button>
      )}
    </Empty>
  );
}
