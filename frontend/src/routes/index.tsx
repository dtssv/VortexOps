import { lazy, Suspense } from 'react';
import { Routes, Route, Navigate } from 'react-router-dom';
import { MainLayout } from '@/components/Layout/MainLayout';
import { RequireAuth } from './RequireAuth';
import { RequirePermission } from './RequirePermission';
import { Spin } from 'antd';

// Tier 1
const LoginPage = lazy(() => import('@/features/auth/LoginPage'));
const DashboardPage = lazy(() => import('@/features/dashboard/DashboardPage'));
const WorkspaceListPage = lazy(() => import('@/features/workspaces/List'));
const WorkspaceDetailPage = lazy(() => import('@/features/workspaces/Detail'));
const ApplicationDetailPage = lazy(() => import('@/features/applications/Detail'));
const ApplicationListPage = lazy(() => import('@/features/applications/ListPage'));
const GroupDetailPage = lazy(() => import('@/features/groups/Detail'));
const GroupCreatePage = lazy(() => import('@/features/groups/Create'));
const BuildListPage = lazy(() => import('@/features/builds/List'));
const BuildDetailPage = lazy(() => import('@/features/builds/Detail'));
const ReleaseListPage = lazy(() => import('@/features/releases/List'));
const ReleaseDetailPage = lazy(() => import('@/features/releases/Detail'));
const MultiReleasePage = lazy(() => import('@/features/releases/MultiReleasePage'));
const ApprovalsPage = lazy(() => import('@/features/approvals/ApprovalsPage'));
const BastionAssetsPage = lazy(() => import('@/features/bastion/AssetsPage'));
const BastionSessionsPage = lazy(() => import('@/features/bastion/SessionsPage'));
// 注：堡垒机（JumpServer）集成已下线，页面与路由保留以避免 import 断裂，但不再挂载路由/菜单。
void BastionAssetsPage;
void BastionSessionsPage;
const WebTerminalPage = lazy(() => import('@/features/ops/WebTerminalPage'));
const OpsSessionsPage = lazy(() => import('@/features/ops/OpsSessionsPage'));
const BehaviorAuditPage = lazy(() => import('@/features/ops/BehaviorAuditPage'));
const PortForwardPage = lazy(() => import('@/features/ops/PortForwardPage'));
const ConfigListPage = lazy(() => import('@/features/configs/List'));
const ConfigDetailPage = lazy(() => import('@/features/configs/Detail'));
const RolesPage = lazy(() => import('@/features/rbac/RolesPage'));
const PermissionsPage = lazy(() => import('@/features/rbac/PermissionsPage'));
const MenusPage = lazy(() => import('@/features/rbac/MenusPage'));
const UsersPage = lazy(() => import('@/features/rbac/UsersPage'));
const SystemSettingsPage = lazy(() => import('@/features/builds/SystemSettingsPage'));
const BaseImagesPage = lazy(() => import('@/features/builds/BaseImagesPage'));
const KnowledgeBasePage = lazy(() => import('@/features/kb/KnowledgeBasePage'));

// Tier 2
const ClustersPage = lazy(() => import('@/features/clusters/ClustersPage'));
const K8sConsolePage = lazy(() => import('@/features/k8s/K8sConsolePage'));
const MonitoringPage = lazy(() => import('@/features/monitoring/MonitoringPage'));
const AuditPage = lazy(() => import('@/features/audit/AuditPage'));
const NotificationsPage = lazy(() => import('@/features/notifications/NotificationsPage'));
const PipelineListPage = lazy(() => import('@/features/pipelines/List'));
const PipelineRunDetailPage = lazy(() => import('@/features/pipelines/RunDetail'));
const InferenceModelsPage = lazy(() => import('@/features/inference/ModelsPage'));
const InferenceServicesPage = lazy(() => import('@/features/inference/ServicesPage'));
const InferenceServiceDetailPage = lazy(() => import('@/features/inference/ServiceDetail'));
const InferenceRoutesPage = lazy(() => import('@/features/inference/RoutesPage'));
const ExtTokensPage = lazy(() => import('@/features/extapi/TokensPage'));
const AlertsPage = lazy(() => import('@/features/ops/AlertsPage'));
const LogsPage = lazy(() => import('@/features/ops/LogsPage'));

// Tier 3
const ProfilePage = lazy(() => import('@/features/me/ProfilePage'));
const MeTokensPage = lazy(() => import('@/features/me/TokensPage'));

function Lazy({ children }: { children: React.ReactNode }) {
  return (
    <Suspense
      fallback={
        <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', minHeight: 320 }}>
          <Spin size="large" />
        </div>
      }
    >
      {children}
    </Suspense>
  );
}

export default function AppRoutes() {
  return (
    <Routes>
      <Route path="/login" element={<Lazy><LoginPage /></Lazy>} />
      <Route
        path="/"
        element={
          <RequireAuth>
            <MainLayout />
          </RequireAuth>
        }
      >
        <Route
          index
          element={
            <Lazy>
              <DashboardPage />
            </Lazy>
          }
        />
        <Route path="workspaces" element={<Lazy><WorkspaceListPage /></Lazy>} />
        <Route path="workspaces/:id" element={<Lazy><WorkspaceDetailPage /></Lazy>} />
        <Route path="applications" element={<Lazy><ApplicationListPage /></Lazy>} />
        <Route path="applications/:appId" element={<Lazy><ApplicationDetailPage /></Lazy>} />
        <Route path="groups/new" element={<Lazy><GroupCreatePage /></Lazy>} />
        <Route path="groups/:groupId" element={<Lazy><GroupDetailPage /></Lazy>} />
        <Route path="builds" element={<Lazy><BuildListPage /></Lazy>} />
        <Route path="builds/:id" element={<Lazy><BuildDetailPage /></Lazy>} />
        <Route path="releases" element={<Lazy><ReleaseListPage /></Lazy>} />
        <Route path="releases/:id" element={<Lazy><ReleaseDetailPage /></Lazy>} />
        <Route path="applications/:appId/multi-release" element={<RequirePermission code="menu:release:orch:view"><Lazy><MultiReleasePage /></Lazy></RequirePermission>} />
        <Route path="approvals" element={<RequirePermission code="menu:approval:view"><Lazy><ApprovalsPage /></Lazy></RequirePermission>} />
        <Route path="ops/terminal" element={<RequirePermission code="ops:exec"><Lazy><WebTerminalPage /></Lazy></RequirePermission>} />
        <Route path="ops/sessions" element={<RequirePermission code="ops:session:view"><Lazy><OpsSessionsPage /></Lazy></RequirePermission>} />
        <Route path="ops/port-forward" element={<RequirePermission code="ops:portforward"><Lazy><PortForwardPage /></Lazy></RequirePermission>} />
        <Route path="audit/behavior" element={<RequirePermission code="audit:behavior:view"><Lazy><BehaviorAuditPage /></Lazy></RequirePermission>} />
        <Route path="configs" element={<Lazy><ConfigListPage /></Lazy>} />
        <Route path="configs/:id" element={<Lazy><ConfigDetailPage /></Lazy>} />
        <Route path="admin/roles" element={<RequirePermission code="menu:admin:role"><Lazy><RolesPage /></Lazy></RequirePermission>} />
        <Route path="admin/permissions" element={<RequirePermission code="menu:admin:role"><Lazy><PermissionsPage /></Lazy></RequirePermission>} />
        <Route path="admin/menus" element={<RequirePermission code="menu:admin:role"><Lazy><MenusPage /></Lazy></RequirePermission>} />
        <Route path="admin/users" element={<RequirePermission code="menu:admin:user"><Lazy><UsersPage /></Lazy></RequirePermission>} />
        <Route path="admin/clusters" element={<RequirePermission code="menu:cluster:view"><Lazy><ClustersPage /></Lazy></RequirePermission>} />
        <Route path="k8s/workloads" element={<RequirePermission code="menu:k8s:view"><Lazy><K8sConsolePage /></Lazy></RequirePermission>} />
        <Route path="monitor" element={<RequirePermission code="menu:monitor:view"><Lazy><MonitoringPage /></Lazy></RequirePermission>} />
        <Route path="admin/settings" element={<RequirePermission code="menu:admin:role"><Lazy><SystemSettingsPage /></Lazy></RequirePermission>} />
        <Route path="admin/base-images" element={<RequirePermission code="menu:admin:role"><Lazy><BaseImagesPage /></Lazy></RequirePermission>} />
        <Route path="admin/knowledge-base" element={<RequirePermission code="kb:manage"><Lazy><KnowledgeBasePage /></Lazy></RequirePermission>} />
        <Route path="pipelines" element={<Lazy><PipelineListPage /></Lazy>} />
        <Route path="pipeline-runs/:id" element={<Lazy><PipelineRunDetailPage /></Lazy>} />
        <Route path="inference" element={<Lazy><InferenceModelsPage /></Lazy>} />
        <Route path="inference/services" element={<Lazy><InferenceServicesPage /></Lazy>} />
        <Route path="inference/services/:id" element={<Lazy><InferenceServiceDetailPage /></Lazy>} />
        <Route path="inference/routes" element={<Lazy><InferenceRoutesPage /></Lazy>} />
        <Route path="ext/tokens" element={<Lazy><ExtTokensPage /></Lazy>} />
        <Route path="alerts" element={<Lazy><AlertsPage /></Lazy>} />
        <Route path="ops/logs" element={<Lazy><LogsPage /></Lazy>} />
        <Route path="audit" element={<Lazy><AuditPage /></Lazy>} />
        <Route path="notifications" element={<Lazy><NotificationsPage /></Lazy>} />
        <Route path="me" element={<Lazy><ProfilePage /></Lazy>} />
        <Route path="me/tokens" element={<Lazy><MeTokensPage /></Lazy>} />
        <Route path="search" element={<Lazy><DashboardPage /></Lazy>} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}
