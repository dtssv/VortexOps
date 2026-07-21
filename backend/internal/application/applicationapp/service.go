// Package applicationapp 是应用与分组领域的应用服务层。
// 编排应用、分组、成员，跨 workspace 仓储执行配额校验与业务规则。
package applicationapp

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/vortexops/vortexops/internal/domain"
	"github.com/vortexops/vortexops/internal/domain/application"
	"github.com/vortexops/vortexops/internal/domain/workspace"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// GroupScaler 按 workload 类型同步 K8s 工作负载副本数。
// 由 k8sapp.Service 实现，避免 applicationapp 直接依赖 k8s client。
type GroupScaler interface {
	ScaleDeployment(ctx context.Context, clusterID int64, namespace, name string, replicas int32) error
	ScaleStatefulSet(ctx context.Context, clusterID int64, namespace, name string, replicas int32) error
}

// PodOperator 提供 Pod 列出/删除能力（机器运维用）。
// 由 k8sapp.Service 实现（ListPods 返回 []corev1.Pod，DeletePod 删 Pod）。
// applicationapp 通过此接口避免直接依赖 k8s corev1。
type PodOperator interface {
	// ListGroupPodNames 列出分组（按 group-id 标签选择器）下所有 Pod 名。
	ListGroupPodNames(ctx context.Context, clusterID int64, namespace, labelSelector string) ([]string, error)
	// DeletePod 删除单个 Pod（控制器自动重建）。
	DeletePod(ctx context.Context, clusterID int64, namespace, name string) error
}

// Service 应用与分组应用服务。
// NetworkProfileResolver 解析集群网络方案（由 clusterapp.Service 实现，避免 applicationapp 直接依赖 cluster repo）。
// CreateGroup/UpdateGroup 用它校验 network_mode=underlay 时集群是否支持。
type NetworkProfileResolver interface {
	// SupportsUnderlay 返回该集群是否支持 Underlay 直连（large-underlay profile）。
	SupportsUnderlay(ctx context.Context, clusterID int64) (bool, error)
}

// GroupIPReleaser 释放 group 的稳定 IP（由 clusterapp.Service 实现）。
// DeleteGroup 时调用，避免 IP 泄漏。可为 nil：跳过释放（兼容老部署）。
type GroupIPReleaser interface {
	ReleaseForGroup(ctx context.Context, groupID, clusterID int64) (int, error)
}

type Service struct {
	apps           application.Repository
	wsRepo         workspace.Repository
	scaler         GroupScaler // 可为 nil：未部署分组时跳过 K8s 同步
	podOps         PodOperator // 可为 nil：跳过机器运维（restart 等）
	typedWSFactory TypedWorkspaceFactory // 可为 nil：未启用统一应用分组时跳过
	profileResolver NetworkProfileResolver // 可为 nil：跳过 underlay profile 校验
	ipReleaser     GroupIPReleaser // 可为 nil：DeleteGroup 时跳过 IP 释放
}

// New 创建应用服务。wsRepo 用于配额校验（统计应用/分组数）。
// scaler 用于副本数变更时同步 K8s（修复"编辑副本数不生效"）。
// podOps 用于机器运维（重启/关机/开机）。
func New(apps application.Repository, wsRepo workspace.Repository, scaler GroupScaler) *Service {
	return &Service{apps: apps, wsRepo: wsRepo, scaler: scaler, podOps: toPodOperator(scaler)}
}

// WithPodOperator 注入 Pod 运维能力（机器运维端点用）。
func (s *Service) WithPodOperator(po PodOperator) *Service {
	s.podOps = po
	return s
}

// WithNetworkProfileResolver 注入集群网络方案解析器（用于校验分组 network_mode=underlay）。
// 由 clusterapp.Service 实现（通过 GetNetworkProfile + SupportsUnderlay）。
// 未注入时跳过 underlay 校验（兼容老部署，underlay 模式仍可设但发布时由 renderer 降级处理）。
func (s *Service) WithNetworkProfileResolver(r NetworkProfileResolver) *Service {
	s.profileResolver = r
	return s
}

// WithGroupIPReleaser 注入 group IP 释放器（由 clusterapp.Service 实现）。
// DeleteGroup 时调用 ReleaseForGroup 释放稳定 IP，避免泄漏。未注入时跳过。
func (s *Service) WithGroupIPReleaser(r GroupIPReleaser) *Service {
	s.ipReleaser = r
	return s
}

// toPodOperator 若 scaler 同时实现 PodOperator 则返回它，否则 nil。
func toPodOperator(scaler GroupScaler) PodOperator {
	if po, ok := scaler.(PodOperator); ok {
		return po
	}
	return nil
}

// --- 应用 ---

// CreateInput 创建应用请求。
type CreateInput struct {
	WorkspaceID       int64
	Name              string
	Code              string
	DisplayName       string
	Description       string
	Icon              string
	DefaultRegistryID int64
	OwnerID           int64
	Labels            map[string]string
	Metadata          map[string]any
	// 应用配置项：存入 metadata（不新增 schema 列）。
	AppType       string
	WorkloadType  string
	GitURL        string
	DefaultBranch string
	Language      string
	// 应用探活配置：存入 metadata["probe"]。
	Probe *application.ProbeConfig
}

// Create 创建应用（校验名称与配额，创建者成为 owner）。
func (s *Service) Create(ctx context.Context, in CreateInput) (*application.Application, error) {
	if err := validateAppName(in.Name); err != nil {
		return nil, err
	}
	if in.WorkspaceID == 0 {
		return nil, apperr.Validation("workspace_id is required", nil)
	}
	if in.OwnerID == 0 {
		return nil, apperr.Validation("owner is required", nil)
	}
	// code 默认回填为 name（workspace 内唯一）。
	if in.Code == "" {
		in.Code = in.Name
	}
	// 工作空间存在性与状态校验。
	ws, err := s.wsRepo.GetByID(ctx, in.WorkspaceID)
	if err != nil {
		if errors.Is(err, workspace.ErrWorkspaceNotFound) {
			return nil, apperr.NotFound("workspace", strconv.FormatInt(in.WorkspaceID, 10))
		}
		return nil, apperr.Internal("get workspace", err)
	}
	if !ws.IsActive() {
		return nil, apperr.BusinessRule("workspace is not active", workspace.ErrWorkspaceArchived)
	}
	// 名称唯一性预检。
	if _, err := s.apps.GetApplicationByName(ctx, in.WorkspaceID, in.Name); err == nil {
		return nil, apperr.Conflict("application name already exists in workspace", application.ErrApplicationNameExists)
	} else if !errors.Is(err, application.ErrApplicationNotFound) {
		return nil, apperr.Internal("check application name", err)
	}
	// code 唯一性预检。
	if _, err := s.apps.GetApplicationByCode(ctx, in.WorkspaceID, in.Code); err == nil {
		return nil, apperr.Conflict("application code already exists in workspace", application.ErrApplicationCodeExists)
	} else if !errors.Is(err, application.ErrApplicationNotFound) {
		return nil, apperr.Internal("check application code", err)
	}
	// 配额校验。
	quota, err := s.wsRepo.GetQuota(ctx, in.WorkspaceID)
	if err != nil {
		return nil, apperr.Internal("get workspace quota", err)
	}
	count, err := s.wsRepo.CountApplications(ctx, in.WorkspaceID)
	if err != nil {
		return nil, apperr.Internal("count applications", err)
	}
	if count >= int64(quota.MaxApplications) {
		return nil, apperr.BusinessRule("workspace application quota exceeded", workspace.ErrQuotaExceeded)
	}

	a := &application.Application{
		WorkspaceID:       in.WorkspaceID,
		Name:              in.Name,
		Code:              in.Code,
		DisplayName:       in.DisplayName,
		Description:       in.Description,
		Icon:              in.Icon,
		DefaultRegistryID: in.DefaultRegistryID,
		Lifecycle:         application.LifecycleActive,
		OwnerID:           in.OwnerID,
		Labels:            in.Labels,
		Metadata:          mergeAppConfigMetadata(in.Metadata, in.AppType, in.WorkloadType, in.GitURL, in.DefaultBranch, in.Language, in.Probe),
	}
	a.CreatedBy = in.OwnerID
	a.UpdatedBy = in.OwnerID
	if err := s.apps.CreateApplication(ctx, a); err != nil {
		if errors.Is(err, application.ErrApplicationNameExists) {
			return nil, apperr.Conflict("application name already exists in workspace", err)
		}
		if errors.Is(err, application.ErrApplicationCodeExists) {
			return nil, apperr.Conflict("application code already exists in workspace", err)
		}
		return nil, apperr.Internal("create application", err)
	}
	return a, nil
}

// Get 按 ID 获取应用。
func (s *Service) Get(ctx context.Context, id int64) (*application.Application, error) {
	a, err := s.apps.GetApplicationByID(ctx, id)
	if err != nil {
		if errors.Is(err, application.ErrApplicationNotFound) {
			return nil, apperr.NotFound("application", strconv.FormatInt(id, 10))
		}
		return nil, apperr.Internal("get application", err)
	}
	return a, nil
}

// GetByUUID 按 UUID 获取应用。
func (s *Service) GetByUUID(ctx context.Context, id uuid.UUID) (*application.Application, error) {
	a, err := s.apps.GetApplicationByUUID(ctx, id)
	if err != nil {
		if errors.Is(err, application.ErrApplicationNotFound) {
			return nil, apperr.NotFound("application", id.String())
		}
		return nil, apperr.Internal("get application", err)
	}
	return a, nil
}

// UpdateInput 更新应用请求。
type UpdateInput struct {
	ID                int64
	DisplayName       *string
	Description       *string
	Icon              *string
	Lifecycle         *application.Lifecycle
	DefaultGitSourceID *int64
	DefaultRegistryID  *int64
	Labels            *map[string]string
	Metadata          *map[string]any
	// 应用配置项（可选更新；为 nil 表示不修改）。
	AppType       *string
	WorkloadType  *string
	GitURL        *string
	DefaultBranch *string
	Language      *string
	// 应用探活配置（可选更新；为 nil 表示不修改；传入 Enabled=false 的非 nil 值表示禁用探活）。
	Probe *application.ProbeConfig
	Version       int
	ActorID       int64
}

// Update 更新应用（需 owner，乐观锁）。
func (s *Service) Update(ctx context.Context, in UpdateInput) (*application.Application, error) {
	a, err := s.apps.GetApplicationByID(ctx, in.ID)
	if err != nil {
		if errors.Is(err, application.ErrApplicationNotFound) {
			return nil, apperr.NotFound("application", strconv.FormatInt(in.ID, 10))
		}
		return nil, apperr.Internal("get application", err)
	}
	if a.OwnerID != in.ActorID {
		return nil, apperr.Forbidden("only application owner can update", application.ErrNotAppOwner)
	}
	if in.Lifecycle != nil && *in.Lifecycle == application.LifecycleArchived && a.Lifecycle != application.LifecycleArchived {
		// 归档前可在此处加入"无活跃分组"等校验（后续 Phase 接入运行时状态后补）。
	}
	// 若传入任一应用配置项，合并到 metadata（不影响其他 metadata key）。
	metadata := in.Metadata
	if in.AppType != nil || in.WorkloadType != nil || in.GitURL != nil || in.DefaultBranch != nil || in.Language != nil || in.Probe != nil {
		merged := cloneStringAnyMap(a.Metadata)
		if in.AppType != nil {
			merged["app_type"] = *in.AppType
		}
		if in.WorkloadType != nil {
			merged["workload_type"] = *in.WorkloadType
		}
		if in.GitURL != nil {
			merged["git_url"] = *in.GitURL
		}
		if in.DefaultBranch != nil {
			merged["default_branch"] = *in.DefaultBranch
		}
		if in.Language != nil {
			merged["language"] = *in.Language
		}
		if in.Probe != nil {
			if err := in.Probe.Validate(); err != nil {
				return nil, apperr.Validation(err.Error(), nil)
			}
			// 显式传入 Probe（即便 Enabled=false）也覆盖原值：禁用探活时写入 enabled=false。
			merged["probe"] = application.MarshalProbe(in.Probe)
		}
		metadata = &merged
	}
	updated, err := s.apps.UpdateApplication(ctx, application.UpdateApplicationInput{
		ID: in.ID, DisplayName: in.DisplayName, Description: in.Description, Icon: in.Icon,
		Lifecycle: in.Lifecycle, DefaultGitSourceID: in.DefaultGitSourceID, DefaultRegistryID: in.DefaultRegistryID,
		Labels: in.Labels, Metadata: metadata, Version: in.Version, UpdatedBy: in.ActorID,
	})
	if err != nil {
		return nil, mapUpdateErr(err, "application", in.ID)
	}
	return updated, nil
}

// List 分页列出应用。
func (s *Service) List(ctx context.Context, workspaceID int64, ownerID int64, lifecycle application.Lifecycle, appType string, search string, page, size int) ([]*application.Application, int64, error) {
	items, total, err := s.apps.ListApplications(ctx, application.ApplicationQuery{
		WorkspaceID: workspaceID, OwnerID: ownerID, Lifecycle: lifecycle, AppType: appType, Search: search,
		Offset: (page - 1) * size, Limit: size,
	})
	if err != nil {
		return nil, 0, apperr.Internal("list applications", err)
	}
	return items, total, nil
}

// Delete 软删除应用（需 owner）。
// 关联校验：应用下存在分组时禁止删除，避免运行中的分组失去归属。
func (s *Service) Delete(ctx context.Context, id, actorID int64) error {
	a, err := s.apps.GetApplicationByID(ctx, id)
	if err != nil {
		if errors.Is(err, application.ErrApplicationNotFound) {
			return apperr.NotFound("application", strconv.FormatInt(id, 10))
		}
		return apperr.Internal("get application", err)
	}
	if a.OwnerID != actorID {
		return apperr.Forbidden("only application owner can delete", application.ErrNotAppOwner)
	}
	groups, _, err := s.apps.ListGroups(ctx, application.GroupQuery{
		ApplicationID: id, Offset: 0, Limit: 1,
	})
	if err != nil {
		return apperr.Internal("list groups before delete", err)
	}
	if len(groups) > 0 {
		return apperr.BusinessRule(
			fmt.Sprintf("application has groups; remove them before deleting the application"),
			application.ErrApplicationNotEmpty,
		)
	}
	if err := s.apps.DeleteApplication(ctx, id, actorID); err != nil {
		if errors.Is(err, application.ErrApplicationNotFound) {
			return apperr.NotFound("application", strconv.FormatInt(id, 10))
		}
		return apperr.Internal("delete application", err)
	}
	return nil
}

// --- 应用成员 ---

// AddMemberInput 添加应用成员请求。
type AddMemberInput struct {
	ApplicationID int64
	UserID        int64
	RoleID        int64
	ActorID       int64
}

// AddMember 添加应用成员（需 owner）。
func (s *Service) AddMember(ctx context.Context, in AddMemberInput) (*application.Member, error) {
	a, err := s.apps.GetApplicationByID(ctx, in.ApplicationID)
	if err != nil {
		if errors.Is(err, application.ErrApplicationNotFound) {
			return nil, apperr.NotFound("application", strconv.FormatInt(in.ApplicationID, 10))
		}
		return nil, apperr.Internal("get application", err)
	}
	if a.OwnerID != in.ActorID {
		return nil, apperr.Forbidden("only application owner can add members", application.ErrNotAppOwner)
	}
	if in.UserID == 0 || in.RoleID == 0 {
		return nil, apperr.Validation("user_id and role_id are required", nil)
	}
	if in.UserID == in.ActorID {
		return nil, apperr.Validation("owner is already a member", nil)
	}
	m := &application.Member{
		ApplicationID: in.ApplicationID,
		UserID:        in.UserID,
		RoleID:        in.RoleID,
		InvitedBy:     in.ActorID,
		Status:        application.MemberStatusActive,
	}
	m.CreatedBy = in.ActorID
	if err := s.apps.AddAppMember(ctx, m); err != nil {
		if errors.Is(err, application.ErrAppMemberExists) {
			return nil, apperr.Conflict("member already exists in application", err)
		}
		return nil, apperr.Internal("add app member", err)
	}
	return m, nil
}

// ListMembers 分页列出应用成员。
func (s *Service) ListMembers(ctx context.Context, applicationID int64, page, size int) ([]*application.Member, int64, error) {
	items, total, err := s.apps.ListAppMembers(ctx, applicationID, (page-1)*size, size)
	if err != nil {
		return nil, 0, apperr.Internal("list app members", err)
	}
	return items, total, nil
}

// UpdateMemberRole 更新应用成员角色（需 owner，不能改自己）。
func (s *Service) UpdateMemberRole(ctx context.Context, applicationID, userID, roleID, actorID int64) error {
	a, err := s.apps.GetApplicationByID(ctx, applicationID)
	if err != nil {
		if errors.Is(err, application.ErrApplicationNotFound) {
			return apperr.NotFound("application", strconv.FormatInt(applicationID, 10))
		}
		return apperr.Internal("get application", err)
	}
	if a.OwnerID != actorID {
		return apperr.Forbidden("only application owner can update member roles", application.ErrNotAppOwner)
	}
	if userID == actorID {
		return apperr.Validation("cannot change own role", nil)
	}
	if err := s.apps.UpdateAppMemberRole(ctx, applicationID, userID, roleID, actorID); err != nil {
		if errors.Is(err, application.ErrAppMemberNotFound) {
			return apperr.NotFound("member", strconv.FormatInt(userID, 10))
		}
		return apperr.Internal("update app member role", err)
	}
	return nil
}

// RemoveMember 移除应用成员（需 owner，不能移除自己）。
func (s *Service) RemoveMember(ctx context.Context, applicationID, userID, actorID int64) error {
	a, err := s.apps.GetApplicationByID(ctx, applicationID)
	if err != nil {
		if errors.Is(err, application.ErrApplicationNotFound) {
			return apperr.NotFound("application", strconv.FormatInt(applicationID, 10))
		}
		return apperr.Internal("get application", err)
	}
	if a.OwnerID != actorID {
		return apperr.Forbidden("only application owner can remove members", application.ErrNotAppOwner)
	}
	if userID == actorID {
		return apperr.Validation("owner cannot remove themselves; transfer ownership first", nil)
	}
	if err := s.apps.RemoveAppMember(ctx, applicationID, userID, actorID); err != nil {
		if errors.Is(err, application.ErrAppMemberNotFound) {
			return apperr.NotFound("member", strconv.FormatInt(userID, 10))
		}
		return apperr.Internal("remove app member", err)
	}
	return nil
}

// --- 分组 ---

// CreateGroupInput 创建分组请求。
type CreateGroupInput struct {
	ApplicationID int64
	Name          string
	DisplayName   string
	Description   string
	Environment   application.Environment
	ClusterID     int64
	Namespace     string
	Replicas      int
	Resources     application.Resources
	Storage       application.Storage
	MeshEnabled   bool
	Scheduling    application.Scheduling
	Workload      application.Workload
	HealthCheck   *application.HealthCheck
	Autoscaling   *application.Autoscaling
	ReleaseRequiresApproval bool
	Labels        map[string]string
	Metadata      map[string]any
	ActorID       int64
}

// CreateGroup 创建分组（校验应用归属、名称、配额、资源合法性）。
func (s *Service) CreateGroup(ctx context.Context, in CreateGroupInput) (*application.Group, error) {
	if err := validateGroupName(in.Name); err != nil {
		return nil, err
	}
	if in.ApplicationID == 0 {
		return nil, apperr.Validation("application_id is required", nil)
	}
	if in.ClusterID == 0 {
		return nil, apperr.Validation("cluster_id is required", nil)
	}
	if in.Namespace == "" {
		return nil, apperr.Validation("namespace is required", nil)
	}
	a, err := s.apps.GetApplicationByID(ctx, in.ApplicationID)
	if err != nil {
		if errors.Is(err, application.ErrApplicationNotFound) {
			return nil, apperr.NotFound("application", strconv.FormatInt(in.ApplicationID, 10))
		}
		return nil, apperr.Internal("get application", err)
	}
	if !a.IsActive() {
		return nil, apperr.BusinessRule("application is not active", application.ErrApplicationArchived)
	}
	// 名称唯一性预检。
	if _, err := s.apps.GetGroupByName(ctx, in.ApplicationID, in.Name); err == nil {
		return nil, apperr.Conflict("group name already exists in application", application.ErrGroupNameExists)
	} else if !errors.Is(err, application.ErrGroupNotFound) {
		return nil, apperr.Internal("check group name", err)
	}
	// 配额校验（按工作空间统计分组总数）。
	quota, err := s.wsRepo.GetQuota(ctx, a.WorkspaceID)
	if err != nil {
		return nil, apperr.Internal("get workspace quota", err)
	}
	count, err := s.wsRepo.CountGroups(ctx, a.WorkspaceID)
	if err != nil {
		return nil, apperr.Internal("count groups", err)
	}
	if count >= int64(quota.MaxGroups) {
		return nil, apperr.BusinessRule("workspace group quota exceeded", workspace.ErrQuotaExceeded)
	}
	// 资源合法性。
	if err := validateGroupSpec(in.Replicas, in.Resources, in.Workload); err != nil {
		return nil, err
	}

	g := &application.Group{
		ApplicationID:          in.ApplicationID,
		Name:                   in.Name,
		DisplayName:            in.DisplayName,
		Description:            in.Description,
		AppType:                deriveAppType(a),
		Environment:            in.Environment,
		ClusterID:              in.ClusterID,
		Namespace:              in.Namespace,
		Replicas:               in.Replicas,
		Resources:              in.Resources,
		Storage:                in.Storage,
		MeshEnabled:            in.MeshEnabled,
		Scheduling:             in.Scheduling,
		Workload:               in.Workload,
		HealthCheck:            in.HealthCheck,
		Autoscaling:            in.Autoscaling,
		ReleaseRequiresApproval: in.ReleaseRequiresApproval,
		Labels:                 in.Labels,
		Metadata:               in.Metadata,
	}
	// 生成 K8s 资源名（Deployment/Service）：{app-slug}-{group-slug}。
	// 未显式指定时由应用名+分组名派生，确保 DNS-1035 兼容（小写/数字/-，字母开头）。
	if g.DeploymentName == "" {
		g.DeploymentName = k8sName(a.Name, g.Name)
	}
	if g.ServiceName == "" {
		g.ServiceName = g.DeploymentName
	}
	g.CreatedBy = in.ActorID
	g.UpdatedBy = in.ActorID
	if err := s.apps.CreateGroup(ctx, g); err != nil {
		if errors.Is(err, application.ErrGroupNameExists) {
			return nil, apperr.Conflict("group name already exists in application", err)
		}
		return nil, apperr.Internal("create group", err)
	}
	return g, nil
}

// GetGroup 按 ID 获取分组。
func (s *Service) GetGroup(ctx context.Context, id int64) (*application.Group, error) {
	g, err := s.apps.GetGroupByID(ctx, id)
	if err != nil {
		if errors.Is(err, application.ErrGroupNotFound) {
			return nil, apperr.NotFound("group", strconv.FormatInt(id, 10))
		}
		return nil, apperr.Internal("get group", err)
	}
	return g, nil
}

// GetApplicationByID 按 ID 获取应用（供 releaseapp 解析应用级探活配置注入原生 K8s Probe）。
func (s *Service) GetApplicationByID(ctx context.Context, id int64) (*application.Application, error) {
	a, err := s.apps.GetApplicationByID(ctx, id)
	if err != nil {
		if errors.Is(err, application.ErrApplicationNotFound) {
			return nil, apperr.NotFound("application", strconv.FormatInt(id, 10))
		}
		return nil, apperr.Internal("get application", err)
	}
	return a, nil
}

// GetGroupByUUID 按 UUID 获取分组。
func (s *Service) GetGroupByUUID(ctx context.Context, id uuid.UUID) (*application.Group, error) {
	g, err := s.apps.GetGroupByUUID(ctx, id)
	if err != nil {
		if errors.Is(err, application.ErrGroupNotFound) {
			return nil, apperr.NotFound("group", id.String())
		}
		return nil, apperr.Internal("get group", err)
	}
	return g, nil
}

// UpdateGroupInput 更新分组请求。
type UpdateGroupInput struct {
	ID                     int64
	DisplayName            *string
	Description            *string
	Replicas               *int
	Resources              *application.Resources
	Storage                *application.Storage
	MeshEnabled            *bool
	Scheduling             *application.Scheduling
	Workload               *application.Workload
	HealthCheck            *application.HealthCheck
	Autoscaling            *application.Autoscaling
	ReleaseRequiresApproval *bool
	Labels                 *map[string]string
	Metadata               *map[string]any
	// ClusterID 仅用于校验：若非 0 且与现有集群不同则报错（集群创建后不可更换）。
	ClusterID              *int64
	Version                int
	ActorID                int64
}

// UpdateGroup 更新分组（需应用 owner，乐观锁）。
func (s *Service) UpdateGroup(ctx context.Context, in UpdateGroupInput) (*application.Group, error) {
	g, err := s.apps.GetGroupByID(ctx, in.ID)
	if err != nil {
		if errors.Is(err, application.ErrGroupNotFound) {
			return nil, apperr.NotFound("group", strconv.FormatInt(in.ID, 10))
		}
		return nil, apperr.Internal("get group", err)
	}
	// 集群不可更换：若请求体显式传入 cluster_id 且与现有不同，直接拒绝。
	if in.ClusterID != nil && *in.ClusterID != 0 && *in.ClusterID != g.ClusterID {
		return nil, apperr.Validation("cluster_id cannot be changed after group creation", nil)
	}
	// 鉴权：取应用，校验 owner。
	a, err := s.apps.GetApplicationByID(ctx, g.ApplicationID)
	if err != nil {
		return nil, apperr.Internal("get application for authz", err)
	}
	if a.OwnerID != in.ActorID {
		return nil, apperr.Forbidden("only application owner can update groups", application.ErrNotAppOwner)
	}
	// 资源合法性（若传入新副本数/资源/工作负载）。
	replicas := g.Replicas
	if in.Replicas != nil {
		replicas = *in.Replicas
	}
	res := g.Resources
	if in.Resources != nil {
		res = *in.Resources
	}
	wl := g.Workload
	if in.Workload != nil {
		wl = *in.Workload
	}
	if err := validateGroupSpec(replicas, res, wl); err != nil {
		return nil, err
	}
	updated, err := s.apps.UpdateGroup(ctx, application.UpdateGroupInput{
		ID: in.ID, DisplayName: in.DisplayName, Description: in.Description, Replicas: in.Replicas,
		Resources: in.Resources, Storage: in.Storage, MeshEnabled: in.MeshEnabled, Scheduling: in.Scheduling,
		Workload: in.Workload, HealthCheck: in.HealthCheck, Autoscaling: in.Autoscaling,
		ReleaseRequiresApproval: in.ReleaseRequiresApproval, Labels: in.Labels, Metadata: in.Metadata,
		Version: in.Version, UpdatedBy: in.ActorID,
	})
	if err != nil {
		return nil, mapUpdateErr(err, "group", in.ID)
	}
	// 副本数变更且分组已部署：同步到 K8s（修复"编辑副本数不生效"）。
	// 其他字段（资源/网络/工作负载策略）变更需触发新发布才会重新 apply，
	// 此处仅同步 replicas（K8s /scale subresource，快路径，不重建 Pod）。
	if in.Replicas != nil && updated.CurrentReleaseID != 0 && s.scaler != nil {
		if serr := s.syncK8sReplicas(ctx, updated); serr != nil {
			// K8s 同步失败不回滚 DB（已落库），仅记录并返回警告：DB 已更新但 K8s 未生效。
			log.Printf("[applicationapp] UpdateGroup %d sync k8s replicas failed: %v", updated.ID, serr)
			return updated, k8sSyncBusinessError(updated, serr)
		}
	}
	return updated, nil
}

// --- 机器运维（Pod 级）---

// RestartGroup 重启分组所有 Pod（逐个删除，控制器自动重建）。
func (s *Service) RestartGroup(ctx context.Context, groupID, actorID int64) error {
	g, err := s.apps.GetGroupByID(ctx, groupID)
	if err != nil {
		if errors.Is(err, application.ErrGroupNotFound) {
			return apperr.NotFound("group", strconv.FormatInt(groupID, 10))
		}
		return apperr.Internal("get group", err)
	}
	if s.podOps == nil {
		return apperr.BusinessRule("pod operations not configured", nil)
	}
	selector := fmt.Sprintf("app.vortexops.io/group-id=%d", g.ID)
	pods, err := s.podOps.ListGroupPodNames(ctx, g.ClusterID, g.Namespace, selector)
	if err != nil {
		return apperr.Internal("list group pods", err)
	}
	// 逐个删除（控制器重建）。忽略 not found。
	for _, name := range pods {
		_ = s.podOps.DeletePod(ctx, g.ClusterID, g.Namespace, name)
	}
	return nil
}

// ShutdownGroup 关机：scale 到 0（保留 desired replicas 于 metadata 便于开机恢复）。
func (s *Service) ShutdownGroup(ctx context.Context, groupID, actorID int64) (*application.Group, error) {
	g, err := s.apps.GetGroupByID(ctx, groupID)
	if err != nil {
		if errors.Is(err, application.ErrGroupNotFound) {
			return nil, apperr.NotFound("group", strconv.FormatInt(groupID, 10))
		}
		return nil, apperr.Internal("get group", err)
	}
	// 记录关机前副本数到 metadata，开机时恢复。
	meta := cloneStringAnyMap(g.Metadata)
	meta["shutdown_replicas"] = g.Replicas
	zero := 0
	updated, err := s.apps.UpdateGroup(ctx, application.UpdateGroupInput{
		ID: g.ID, Replicas: &zero, Metadata: &meta, Version: g.Version, UpdatedBy: actorID,
	})
	if err != nil {
		return nil, mapUpdateErr(err, "group", g.ID)
	}
	if s.scaler != nil && updated.DeploymentName != "" {
		if serr := s.syncK8sReplicas(ctx, updated); serr != nil {
			return updated, k8sSyncBusinessError(updated, serr)
		}
	}
	return updated, nil
}

// StartupGroup 开机：scale 回关机前副本数（metadata.shutdown_replicas），无则用 group.replicas。
// 注意：关机后 group.replicas 已为 0，故需从 metadata 恢复。
func (s *Service) StartupGroup(ctx context.Context, groupID, actorID int64) (*application.Group, error) {
	g, err := s.apps.GetGroupByID(ctx, groupID)
	if err != nil {
		if errors.Is(err, application.ErrGroupNotFound) {
			return nil, apperr.NotFound("group", strconv.FormatInt(groupID, 10))
		}
		return nil, apperr.Internal("get group", err)
	}
	// 从 metadata 取关机前副本数。
	target := 0
	if v, ok := g.Metadata["shutdown_replicas"]; ok {
		switch n := v.(type) {
		case float64:
			target = int(n)
		case int:
			target = n
		}
	}
	if target <= 0 {
		// 无记录，回退到 group.replicas（关机后为 0，则默认 1）。
		target = g.Replicas
		if target <= 0 {
			target = 1
		}
	}
	// 清除 shutdown_replicas 元数据。
	meta := cloneStringAnyMap(g.Metadata)
	delete(meta, "shutdown_replicas")
	updated, err := s.apps.UpdateGroup(ctx, application.UpdateGroupInput{
		ID: g.ID, Replicas: &target, Metadata: &meta, Version: g.Version, UpdatedBy: actorID,
	})
	if err != nil {
		return nil, mapUpdateErr(err, "group", g.ID)
	}
	if s.scaler != nil && updated.DeploymentName != "" {
		if serr := s.syncK8sReplicas(ctx, updated); serr != nil {
			return updated, k8sSyncBusinessError(updated, serr)
		}
	}
	return updated, nil
}

// RestartPod 重启单个 Pod（删除后控制器重建）。
func (s *Service) RestartPod(ctx context.Context, groupID int64, podName string, actorID int64) error {
	g, err := s.apps.GetGroupByID(ctx, groupID)
	if err != nil {
		if errors.Is(err, application.ErrGroupNotFound) {
			return apperr.NotFound("group", strconv.FormatInt(groupID, 10))
		}
		return apperr.Internal("get group", err)
	}
	if s.podOps == nil {
		return apperr.BusinessRule("pod operations not configured", nil)
	}
	if err := s.podOps.DeletePod(ctx, g.ClusterID, g.Namespace, podName); err != nil {
		return apperr.Internal("delete pod", err)
	}
	return nil
}

// syncK8sReplicas 按 workload 类型 scale K8s 工作负载到 g.Replicas。
func (s *Service) syncK8sReplicas(ctx context.Context, g *application.Group) error {
	if s.scaler == nil || g.ClusterID == 0 || g.DeploymentName == "" || g.CurrentReleaseID == 0 {
		return nil
	}
	r := int32(g.Replicas)
	switch g.Workload.Type {
	case application.WorkloadStatefulSet:
		return s.scaler.ScaleStatefulSet(ctx, g.ClusterID, g.Namespace, g.DeploymentName, r)
	default:
		// deployment / 未声明类型默认按 Deployment 处理。
		return s.scaler.ScaleDeployment(ctx, g.ClusterID, g.Namespace, g.DeploymentName, r)
	}
}

// k8sSyncBusinessError 把 K8s 副本同步失败转换为对用户友好的业务错误。
// 区分「Deployment 不存在」/「K8s API 不可达」/其它错误，便于前端展示。
func k8sSyncBusinessError(g *application.Group, serr error) *apperr.Error {
	hint := serr.Error()
	switch {
	case strings.Contains(hint, "not found") || strings.Contains(hint, "no such resource"):
		hint = fmt.Sprintf("K8s 中未找到工作负载 %s/%s（可能尚未部署或已被删除），副本数仅更新到数据库。",
			g.Namespace, g.DeploymentName)
	case strings.Contains(hint, "connection refused") || strings.Contains(hint, "no such host") ||
		strings.Contains(hint, "i/o timeout") || strings.Contains(hint, "context deadline exceeded"):
		hint = fmt.Sprintf("无法连接 K8s API（集群 %d 可能离线或不可达），副本数仅更新到数据库。详情: %s",
			g.ClusterID, serr.Error())
	}
	return apperr.BusinessRule(hint, serr)
}

// ScaleGroupInput 分组扩缩容请求。
type ScaleGroupInput struct {
	ID       int64
	Replicas int
	Version  int // 乐观锁版本（0 表示不校验）
	ActorID  int64
	// RemovePodNames 缩容场景：指定要删除的 Pod 名（缩容所选 Pod）。
	// 非空时：目标副本数 = max(0, 当前副本数 - len(RemovePodNames))，并显式删除这些 Pod。
	// 此时 Replicas 字段被忽略（按所选 Pod 数推导）。
	RemovePodNames []string
}

// ScaleGroup 调整分组副本数：写 DB + 同步 K8s（/scale subresource）。
// 与 UpdateGroup 区别：专用于扩缩容，仅改 replicas，强制同步 K8s（即使未部署也尝试 scale，便于关机后开机）。
// HPA 冲突处理：若分组启用 autoscaling，clamp 到 [min,max] 并提示。
func (s *Service) ScaleGroup(ctx context.Context, in ScaleGroupInput) (*application.Group, error) {
	if in.Replicas < 0 && len(in.RemovePodNames) == 0 {
		return nil, apperr.Validation("replicas must be non-negative", nil)
	}
	g, err := s.apps.GetGroupByID(ctx, in.ID)
	if err != nil {
		if errors.Is(err, application.ErrGroupNotFound) {
			return nil, apperr.NotFound("group", strconv.FormatInt(in.ID, 10))
		}
		return nil, apperr.Internal("get group", err)
	}
	// 鉴权：取应用，校验 owner。
	a, err := s.apps.GetApplicationByID(ctx, g.ApplicationID)
	if err != nil {
		return nil, apperr.Internal("get application for authz", err)
	}
	if a.OwnerID != in.ActorID {
		return nil, apperr.Forbidden("only application owner can scale groups", application.ErrNotAppOwner)
	}

	// 目标副本数推导：缩容所选 Pod 时按所选数推导，否则用 in.Replicas。
	replicas := in.Replicas
	if len(in.RemovePodNames) > 0 {
		current := g.Replicas
		target := current - len(in.RemovePodNames)
		if target < 0 {
			target = 0
		}
		replicas = target
	}

	// HPA 启用时 clamp 到 [min,max]。
	if g.Autoscaling != nil && g.Autoscaling.Enabled {
		if g.Autoscaling.MinReplicas > 0 && replicas < g.Autoscaling.MinReplicas {
			replicas = g.Autoscaling.MinReplicas
		}
		if g.Autoscaling.MaxReplicas > 0 && replicas > g.Autoscaling.MaxReplicas {
			replicas = g.Autoscaling.MaxReplicas
		}
	}
	// 写 DB（乐观锁：传入 version 时校验）。
	updated, err := s.apps.UpdateGroup(ctx, application.UpdateGroupInput{
		ID: in.ID, Replicas: &replicas, Version: in.Version, UpdatedBy: in.ActorID,
	})
	if err != nil {
		return nil, mapUpdateErr(err, "group", in.ID)
	}
	// 同步 K8s（即使未部署也尝试，关机后开机场景需要）。
	if s.scaler != nil && updated.ClusterID != 0 && updated.DeploymentName != "" {
		if serr := s.syncK8sReplicas(ctx, updated); serr != nil {
			log.Printf("[applicationapp] ScaleGroup %d sync k8s replicas failed: %v", updated.ID, serr)
			return updated, k8sSyncBusinessError(updated, serr)
		}
	}
	// 缩容所选 Pod：显式删除选中的 Pod（Controller 在 replicas 收敛后会重建到目标数；
	// 先 scale 再删选中 Pod 可保证最终态=目标副本数，且优先移除用户选中的 Pod）。
	if len(in.RemovePodNames) > 0 && s.podOps != nil && updated.ClusterID != 0 {
		for _, podName := range in.RemovePodNames {
			if derr := s.podOps.DeletePod(ctx, updated.ClusterID, updated.Namespace, podName); derr != nil {
				log.Printf("[applicationapp] ScaleGroup %d delete pod %s failed: %v", updated.ID, podName, derr)
			}
		}
	}
	return updated, nil
}

// ListGroups 分页列出分组。
func (s *Service) ListGroups(ctx context.Context, applicationID int64, env application.Environment, clusterID int64, appType string, search string, page, size int) ([]*application.Group, int64, error) {
	items, total, err := s.apps.ListGroups(ctx, application.GroupQuery{
		ApplicationID: applicationID, Environment: env, ClusterID: clusterID, AppType: appType, Search: search,
		Offset: (page - 1) * size, Limit: size,
	})
	if err != nil {
		return nil, 0, apperr.Internal("list groups", err)
	}
	return items, total, nil
}

// DeleteGroup 软删除分组（需应用 owner）。
func (s *Service) DeleteGroup(ctx context.Context, id, actorID int64) error {
	g, err := s.apps.GetGroupByID(ctx, id)
	if err != nil {
		if errors.Is(err, application.ErrGroupNotFound) {
			return apperr.NotFound("group", strconv.FormatInt(id, 10))
		}
		return apperr.Internal("get group", err)
	}
	a, err := s.apps.GetApplicationByID(ctx, g.ApplicationID)
	if err != nil {
		return apperr.Internal("get application for authz", err)
	}
	if a.OwnerID != actorID {
		return apperr.Forbidden("only application owner can delete groups", application.ErrNotAppOwner)
	}
	// 释放 group 的稳定 IP（best-effort：失败不阻塞删除，仅记录）。
	if s.ipReleaser != nil {
		_, _ = s.ipReleaser.ReleaseForGroup(ctx, id, g.ClusterID)
	}
	if err := s.apps.DeleteGroup(ctx, id, actorID); err != nil {
		if errors.Is(err, application.ErrGroupNotFound) {
			return apperr.NotFound("group", strconv.FormatInt(id, 10))
		}
		return apperr.Internal("delete group", err)
	}
	return nil
}

// --- 校验 ---

func validateAppName(name string) error {
	name = strings.TrimSpace(name)
	if len(name) < 2 || len(name) > 64 {
		return apperr.Validation("application name must be 2-64 characters", nil)
	}
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			return apperr.Validation("application name may only contain letters, digits, '-', '_'", nil)
		}
	}
	return nil
}

func validateGroupName(name string) error {
	name = strings.TrimSpace(name)
	if len(name) < 2 || len(name) > 64 {
		return apperr.Validation("group name must be 2-64 characters", nil)
	}
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			return apperr.Validation("group name may only contain letters, digits, '-', '_'", nil)
		}
	}
	return nil
}

// k8sName 生成 DNS-1035 兼容资源名：小写字母/数字/-，字母开头。
// 多个输入片段用 '-' 连接，去除非法字符，截断到 63 字符，并保证首字符为字母。
// 例如 k8sName("App-1", "group_2") -> "app-1-group-2"。
func k8sName(parts ...string) string {
	var b strings.Builder
	for i, p := range parts {
		p = strings.ToLower(strings.TrimSpace(p))
		for _, r := range p {
			switch {
			case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
				b.WriteRune(r)
			case r == '-' || r == '_':
				b.WriteRune('-')
			default:
				b.WriteRune('-')
			}
		}
		if i < len(parts)-1 {
			b.WriteRune('-')
		}
	}
	out := b.String()
	// 合并连续 '-'。
	for strings.Contains(out, "--") {
		out = strings.ReplaceAll(out, "--", "-")
	}
	out = strings.Trim(out, "-")
	if len(out) > 63 {
		out = out[:63]
		out = strings.Trim(out, "-")
	}
	// 首字符必须为字母；否则前缀 'a'。
	if out == "" || !((out[0] >= 'a' && out[0] <= 'z')) {
		out = "a" + out
	}
	return out
}

func validateGroupSpec(replicas int, res application.Resources, wl application.Workload) error {
	if replicas < 0 {
		return apperr.Validation("replicas must be non-negative", nil)
	}
	if res.CPUm <= 0 {
		return apperr.Validation("resources.cpu_m must be positive", nil)
	}
	if res.MemoryBytes <= 0 {
		return apperr.Validation("resources.memory_bytes must be positive", nil)
	}
	if res.CPULimitM > 0 && res.CPULimitM < res.CPUm {
		return apperr.Validation("resources.cpu_limit_m must be >= cpu_m", nil)
	}
	if res.MemoryLimitBytes > 0 && res.MemoryLimitBytes < res.MemoryBytes {
		return apperr.Validation("resources.memory_limit_bytes must be >= memory_bytes", nil)
	}
	if res.GPU < 0 {
		return apperr.Validation("resources.gpu must be non-negative", nil)
	}
	// CronJob 必须有调度表达式。
	if wl.Type == application.WorkloadCronJob {
		if strings.TrimSpace(wl.CronSchedule) == "" {
			return apperr.Validation("cron_schedule is required for cronjob workload", nil)
		}
	}
	return nil
}

func mapUpdateErr(err error, resource string, id int64) error {
	if errors.Is(err, domain.ErrConflict) {
		return apperr.Conflict("resource was modified concurrently, please refresh", err)
	}
	if errors.Is(err, application.ErrGroupNotFound) || errors.Is(err, application.ErrApplicationNotFound) {
		return apperr.NotFound(resource, strconv.FormatInt(id, 10))
	}
	return apperr.Internal("update "+resource, err)
}

// mergeAppConfigMetadata 把应用配置项（app_type/workload_type/git_url/default_branch/language/probe）合并进 metadata。
// 非空字段覆盖；空字段不写入（保持原值）。metadata 为 nil 时初始化。
func mergeAppConfigMetadata(metadata map[string]any, appType, workloadType, gitURL, defaultBranch, language string, probe *application.ProbeConfig) map[string]any {
	out := cloneStringAnyMap(metadata)
	if appType != "" {
		out["app_type"] = appType
	}
	if workloadType != "" {
		out["workload_type"] = workloadType
	}
	if gitURL != "" {
		out["git_url"] = gitURL
	}
	if defaultBranch != "" {
		out["default_branch"] = defaultBranch
	}
	if language != "" {
		out["language"] = language
	}
	if probe != nil && probe.Enabled {
		out["probe"] = application.MarshalProbe(probe)
	}
	return out
}

// cloneStringAnyMap 深拷贝 metadata map；nil 返回新空 map。
func cloneStringAnyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// deriveAppType 从 application.metadata.app_type 派生 app_type；缺失默认 web。
func deriveAppType(a *application.Application) string {
	if a == nil || a.Metadata == nil {
		return application.AppTypeWeb
	}
	if v, ok := a.Metadata["app_type"]; ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return application.AppTypeWeb
}

// groupExposePortsDisabled 与 parseDefaultPorts 已删除：分组不再配置网络端口（所有端口默认暴露，外部直连 Pod IP）。

// asString 宽松把 any 转 string（JSONB 里可能是 string 或其他类型）。
func asString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// asInt 宽松把 any 转 int（JSON number 反序列化为 float64）。
func asInt(v any) int {
	if v == nil {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	case string:
		i, _ := strconv.Atoi(n)
		return i
	}
	return 0
}

// appTypeToWorkspaceType 应用类型 → 工作空间类型映射。
func appTypeToWorkspaceType(appType string) workspace.Type {
	switch appType {
	case application.AppTypeInference:
		return workspace.TypeInference
	default:
		return workspace.TypeApp
	}
}
