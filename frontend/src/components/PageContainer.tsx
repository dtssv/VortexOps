import type { ReactNode } from 'react';
import { useNavigate } from 'react-router-dom';
import { Breadcrumb } from 'antd';

/**
 * 面包屑项：
 * - `title` + 可选 `path`：普通文本节点，提供 `path` 时渲染为可点击链接
 * - `switcher`：由调用方渲染好的可切换节点（BreadcrumbSwitcher），原样放入 title
 */
export type BreadcrumbItem =
  | { title: ReactNode; path?: string }
  | { switcher: ReactNode };

interface PageContainerProps {
  title?: string;
  subtitle?: string;
  extra?: ReactNode;
  breadcrumb?: BreadcrumbItem[];
  children: ReactNode;
}

export function PageContainer({ title, subtitle, extra, breadcrumb, children }: PageContainerProps) {
  const navigate = useNavigate();

  return (
    <div className="page-container">
      {breadcrumb && (
        <Breadcrumb
          style={{ marginBottom: 12 }}
          items={breadcrumb.map((b) => {
            if ('switcher' in b) {
              return { title: b.switcher };
            }
            const { title, path } = b;
            if (path) {
              return {
                title: (
                  <a
                    onClick={(e) => {
                      e.preventDefault();
                      navigate(path);
                    }}
                  >
                    {title}
                  </a>
                ),
              };
            }
            return { title };
          })}
        />
      )}
      {(title || extra) && (
        <div className="page-header">
          <div>
            {title && <h2 style={{ margin: 0 }}>{title}</h2>}
            {subtitle && <div style={{ color: '#8c8c8c', fontSize: 13, marginTop: 4 }}>{subtitle}</div>}
          </div>
          {extra}
        </div>
      )}
      {children}
    </div>
  );
}
