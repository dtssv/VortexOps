import React from 'react';
import ReactDOM from 'react-dom/client';
import App from './App';
import './i18n';

const base = `
* { box-sizing: border-box; }
html, body, #root { height: 100%; margin: 0; }
body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, 'PingFang SC', 'Microsoft YaHei', sans-serif; }
::-webkit-scrollbar { width: 8px; height: 8px; }
::-webkit-scrollbar-thumb { background: rgba(0,0,0,0.2); border-radius: 4px; }
::-webkit-scrollbar-track { background: transparent; }
.ant-layout { min-height: 100%; }
.page-container { padding: 24px; }
.page-container .page-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px; }
.flex-between { display: flex; justify-content: space-between; align-items: center; }
.mb-16 { margin-bottom: 16px; }
.mt-16 { margin-top: 16px; }
`;

const styleEl = document.createElement('style');
styleEl.textContent = base;
document.head.appendChild(styleEl);

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
