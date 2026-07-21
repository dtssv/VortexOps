import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';

const resources = {
  zh: {
    translation: {
      'common.create': '新建',
      'common.edit': '编辑',
      'common.delete': '删除',
      'common.cancel': '取消',
      'common.save': '保存',
      'common.confirm': '确认',
      'common.search': '搜索',
      'common.actions': '操作',
      'common.status': '状态',
      'common.name': '名称',
      'common.description': '描述',
      'common.created_at': '创建时间',
      'common.updated_at': '更新时间',
      'common.loading': '加载中...',
      'common.empty': '暂无数据',
      'common.success': '操作成功',
      'common.failed': '操作失败',
      'menu.dashboard': '工作台',
      'menu.workspaces': '空间',
      'menu.applications': '应用',
      'menu.builds': '构建中心',
      'menu.releases': '发布中心',
      'menu.configs': '配置管理',
      'menu.clusters': '集群管理',
      'menu.pipelines': 'CI/CD 流水线',
      'menu.inference': '大模型服务',
      'menu.audit': '审计日志',
      'menu.alerts': '告警中心',
      'menu.ops': '运维观测',
      'menu.rbac': '权限管理',
      'menu.profile': '个人中心',
      'menu.tokens': 'API Token',
      'menu.logout': '退出登录',
    },
  },
  en: {
    translation: {
      'common.create': 'Create',
      'common.delete': 'Delete',
      'common.cancel': 'Cancel',
      'common.save': 'Save',
      'common.search': 'Search',
      'menu.dashboard': 'Dashboard',
    },
  },
};

void i18n.use(initReactI18next).init({
  resources,
  lng: 'zh',
  fallbackLng: 'zh',
  interpolation: { escapeValue: false },
});

export default i18n;
