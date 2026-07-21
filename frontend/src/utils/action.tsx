import { message, Modal } from 'antd';

export function downloadText(filename: string, text: string, mime = 'text/plain') {
  const blob = new Blob([text], { type: mime });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  URL.revokeObjectURL(url);
}

export function confirmDanger(opts: {
  title: string;
  content: React.ReactNode;
  okText?: string;
  onOk: () => Promise<any> | any;
}): void {
  Modal.confirm({
    title: opts.title,
    content: opts.content,
    okText: opts.okText || '确认',
    okType: 'danger',
    cancelText: '取消',
    async onOk() {
      try {
        await opts.onOk();
        message.success('操作成功');
      } catch (e: any) {
        message.error(e?.message || '操作失败');
        throw e;
      }
    },
  });
}

export function confirmTyped(opts: {
  title: string;
  content: React.ReactNode;
  confirmText: string;
  onOk: () => Promise<any> | any;
}): void {
  let input = '';
  Modal.confirm({
    title: opts.title,
    content: (
      <div>
        <div style={{ marginBottom: 8 }}>{opts.content}</div>
        <input
          placeholder={opts.confirmText}
          onChange={(e) => {
            input = e.target.value;
          }}
          style={{ width: '100%', padding: '4px 8px' }}
        />
      </div>
    ),
    okText: '确认',
    okType: 'danger',
    cancelText: '取消',
    async onOk() {
      if (input !== opts.confirmText) {
        message.error(`请输入 ${opts.confirmText} 以确认`);
        return Promise.reject();
      }
      try {
        await opts.onOk();
        message.success('操作成功');
      } catch (e: any) {
        message.error(e?.message || '操作失败');
        throw e;
      }
    },
  });
}
