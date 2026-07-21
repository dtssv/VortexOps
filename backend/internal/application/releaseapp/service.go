// Package releaseapp 是发布领域的应用服务层。
// 编排：触发发布（解析 group/image/config → 渲染 K8s 工作负载 → 应用到集群 → 记录事件/进度）；
// 回滚（取上一成功发布重新应用）；发布预设/窗口管理。
package releaseapp

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/vortexops/vortexops/internal/domain"
	"github.com/vortexops/vortexops/internal/domain/application"
	"github.com/vortexops/vortexops/internal/domain/build"
	"github.com/vortexops/vortexops/internal/domain/cluster"
	configdomain "github.com/vortexops/vortexops/internal/domain/config"
	"github.com/vortexops/vortexops/internal/domain/networkprofile"
	"github.com/vortexops/vortexops/internal/domain/release"
	dnsdomain "github.com/vortexops/vortexops/internal/domain/dns"
	"github.com/vortexops/vortexops/internal/application/clusterapp"
	"github.com/vortexops/vortexops/internal/infrastructure/k8s/workload"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// errReleaseInterrupted 表示发布已被同分组新发布抢占，协程应安静退出。
var errReleaseInterrupted = errors.New("release interrupted")

// GroupResolver 解析 group 实体（解耦避免循环依赖）。
type GroupResolver interface {
	GetGroup(ctx context.Context, id int64) (*application.Group, error)
}

// ImageResolver 取镜像完整引用。
type ImageResolver interface {
	GetImage(ctx context.Context, id int64) (*build.Image, error)
}

// AppProbeResolver 取应用的探活配置（由 application.Repository 实现）。
// 用于发布渲染时把应用级 ProbeConfig 注入原生 K8s Readiness+Liveness Probe。
type AppProbeResolver interface {
	GetApplicationByID(ctx context.Context, id int64) (*application.Application, error)
}

// RegistryResolver 按镜像仓库 ID 取仓库实例（用于解析私有仓库并注入 imagePullSecret）。
type RegistryResolver interface {
	GetRegistryByID(ctx context.Context, id int64) (*build.Registry, error)
}

// ConfigResolver 取 group 的配置挂载列表。
// 互斥语义：分组绑定配置集时用配置集内容，否则用本地配置（发布渲染时绑定优先，本地配置被覆盖）。
type ConfigResolver interface {
	ListBindings(ctx context.Context, groupID int64) ([]*configdomain.GroupConfigBinding, error)
	GetConfig(ctx context.Context, id int64) (*configdomain.Config, error)
	GetConfigSet(ctx context.Context, id int64) (*configdomain.ConfigSet, error)
	GetLocalConfig(ctx context.Context, groupID int64) (*configdomain.GroupLocalConfig, error)
}

// IPAllocator 按 group 分配稳定 IP（keep_pod_ip）。
type IPAllocator interface {
	AllocateForGroup(ctx context.Context, groupID, clusterID int64, replicas int) ([]string, error)
}

// K8sClientProvider 按集群 ID 提供 K8s clientset。
type K8sClientProvider interface {
	GetClient(ctx context.Context, clusterID int64) (kubernetes.Interface, error)
}

// DynamicClientProvider 按集群 ID 提供 dynamic client（Cilium/Mesh CRD apply）。
type DynamicClientProvider interface {
	GetDynamicClient(ctx context.Context, clusterID int64) (dynamic.Interface, error)
}

// DNSMapper 域名→Pod IP 映射（Phase 4）。
type DNSMapper interface {
	UpsertDomainMapping(ctx context.Context, g *application.Group, podIPs []string) (*dnsdomain.Record, error)
}

// CapacityProvider 按集群 ID 与单副本资源需求预估可调度副本数。
// 直接使用 clusterapp 类型（releaseapp 已在 server.go 与 clusterapp 共存，无循环依赖）。
type CapacityProvider = clusterapp.CapacityProvider

// CapacityQuery 容量预估入参（别名 clusterapp.CapacityQuery）。
type CapacityQuery = clusterapp.CapacityQuery

// ClusterCapacity 容量预估结果（别名 clusterapp.ClusterCapacity）。
type ClusterCapacity = clusterapp.ClusterCapacity

// Service 发布应用服务。
type Service struct {
	repo             release.Repository
	orchRepo         release.OrchestrationRepository
	groupRepo        GroupResolver
	imageRepo        ImageResolver
	configRepo       ConfigResolver
	clusterRepo      cluster.Repository
	ipAllocator      IPAllocator
	clientProvider   K8sClientProvider
	capacityProvider CapacityProvider
	registryResolver RegistryResolver
	windowChecker    WindowChecker
	approvalChecker  ApprovalChecker
	appProbeResolver AppProbeResolver
	dnsMapper        DNSMapper
	dynamicProvider  DynamicClientProvider
}

// New 创建发布服务。clientProvider 用于按集群 ID 解析 K8s clientset。
// capacityProvider 用于发布前容量预校验，可为 nil（跳过预校验）。
// registryResolver 用于解析镜像所属仓库以注入 imagePullSecret，可为 nil（公开仓库场景）。
func New(repo release.Repository, groupRepo GroupResolver, imageRepo ImageResolver, configRepo ConfigResolver, clusterRepo cluster.Repository, ipAllocator IPAllocator, clientProvider K8sClientProvider, capacityProvider CapacityProvider, registryResolver RegistryResolver) *Service {
	return &Service{
		repo: repo, groupRepo: groupRepo, imageRepo: imageRepo, configRepo: configRepo,
		clusterRepo: clusterRepo, ipAllocator: ipAllocator, clientProvider: clientProvider,
		capacityProvider: capacityProvider, registryResolver: registryResolver,
	}
}

// WithOrchestrationRepo 注入编排仓储（多集群发布功能）。
func (s *Service) WithOrchestrationRepo(r release.OrchestrationRepository) *Service {
	s.orchRepo = r
	return s
}

// WithAppProbeResolver 注入应用探活配置解析器（可选）。
// 发布渲染时按 group.ApplicationID 取应用 metadata.probe，注入原生 K8s Readiness+Liveness Probe。
func (s *Service) WithAppProbeResolver(r AppProbeResolver) *Service {
	s.appProbeResolver = r
	return s
}

// WithDNSMapper 注入 DNS 映射服务（Phase 4，发布成功后写 A 记录）。
func (s *Service) WithDNSMapper(m DNSMapper) *Service {
	s.dnsMapper = m
	return s
}

// WithDynamicClientProvider 注入 dynamic client 提供者（Cilium/Mesh CRD apply）。
func (s *Service) WithDynamicClientProvider(p DynamicClientProvider) *Service {
	s.dynamicProvider = p
	return s
}

// resolveAppProbe 解析 group 所属应用的探活配置（供渲染为原生 K8s Readiness+Liveness Probe）。
// resolver 未注入或应用未配置探活时返回 nil（渲染器将跳过探针注入）。
func (s *Service) resolveAppProbe(ctx context.Context, g *application.Group) *application.ProbeConfig {
	if s.appProbeResolver == nil || g == nil {
		return nil
	}
	app, err := s.appProbeResolver.GetApplicationByID(ctx, g.ApplicationID)
	if err != nil || app == nil {
		return nil
	}
	return application.ProbeFromApplication(app)
}

// resolveNetworkProfile 解析 group 所属集群的网络方案配置（供渲染器决定 CNI annotation 注入方式）。
// 集群未登记 network_profile 时返回 dev-single 默认值（兼容老集群，行为不变）。
// 解析失败不阻断发布（降级为默认 profile，仅记录日志），避免 profile 配置错误导致整个发布不可用。
func (s *Service) resolveNetworkProfile(ctx context.Context, g *application.Group) *networkprofile.ProfileConfig {
	if g == nil || s.clusterRepo == nil {
		return &networkprofile.ProfileConfig{Profile: networkprofile.ProfileDevSingle}
	}
	c, err := s.clusterRepo.GetClusterByID(ctx, g.ClusterID)
	if err != nil || c == nil {
		return &networkprofile.ProfileConfig{Profile: networkprofile.ProfileDevSingle}
	}
	cfg, err := clusterapp.ParseNetworkProfile(c.Metadata)
	if err != nil {
		// 降级：用默认 profile 继续，避免配置错误阻塞发布。错误会在后续集群校验中暴露。
		return &networkprofile.ProfileConfig{Profile: networkprofile.ProfileDevSingle}
	}
	return cfg
}

// TriggerReleaseInput 触发发布输入。
type TriggerReleaseInput struct {
	GroupID               int64
	ImageID               int64
	ConfigVersion         int
	ReleaseType           release.ReleaseType
	Replicas              int
	Strategy              release.Strategy
	MaxSurge              string
	MaxUnavailable        string
	BatchSize             int
	BatchIntervalSec      int
	TriggeredBy           int64
	TriggerSource         release.TriggerSource
	AutoRollbackOnFailure bool
	// 多版本分批发布参数（percentage/machine_count 策略）：
	// TargetPercentage：百分比（1-100），候选副本数=ceil(group.replicas*pct/100)。
	// TargetPodNames：机器数策略目标 Pod 名列表，候选副本数=len()，钉到这些 Pod。
	TargetPercentage  int
	TargetPodNames    []string
}

// TriggerRelease 触发发布。
func (s *Service) TriggerRelease(ctx context.Context, in TriggerReleaseInput) (*release.Release, error) {
	if in.GroupID == 0 {
		return nil, apperr.Validation("group_id is required", nil)
	}
	if in.ReleaseType == "" {
		in.ReleaseType = release.ReleaseRolling
	}
	if in.Strategy == "" {
		in.Strategy = release.StrategyRolling
	}
	if in.TriggerSource == "" {
		in.TriggerSource = release.TriggerManual
	}

	// 解析 group。
	g, err := s.groupRepo.GetGroup(ctx, in.GroupID)
	if err != nil {
		if errors.Is(err, application.ErrGroupNotFound) {
			return nil, apperr.NotFound("group", strconv.FormatInt(in.GroupID, 10))
		}
		return nil, apperr.Internal("get group", err)
	}

	// 发布窗口强制：校验当前时间是否在应用的活跃发布窗口内。
	if err := s.EnsureWithinReleaseWindow(ctx, g.ApplicationID, time.Now()); err != nil {
		return nil, err
	}

	// 中断同分组下仍在进行中（running/paused）的前序发布：
	// 新发布会抢占候选 Deployment/批次推进，前序发布的 executeRelease 协程会在后续步骤检测到状态变化后退出。
	if err := s.interruptInProgressReleases(ctx, in.GroupID, in.TriggeredBy); err != nil {
		return nil, err
	}

	// 解析镜像（若未指定，用 group 当前镜像）。
	imageID := in.ImageID
	if imageID == 0 {
		imageID = g.CurrentImageID
	}
	if imageID == 0 {
		return nil, apperr.BusinessRule("no image specified and group has no current image", nil)
	}
	img, err := s.imageRepo.GetImage(ctx, imageID)
	if err != nil {
		if errors.Is(err, build.ErrImageNotFound) {
			return nil, apperr.NotFound("image", strconv.FormatInt(imageID, 10))
		}
		return nil, apperr.Internal("get image", err)
	}

	// 当前发布作为 previous。
	prevRelease, _ := s.repo.GetCurrentRelease(ctx, in.GroupID)
	var prevID int64
	if prevRelease != nil {
		prevID = prevRelease.ID
	}

	releaseNumber, err := s.repo.NextReleaseNumber(ctx, in.GroupID)
	if err != nil {
		return nil, apperr.Internal("allocate release number", err)
	}

	replicas := in.Replicas
	if replicas == 0 {
		replicas = g.Replicas
	}

	// 发布前容量预校验：按 group.resources 计算集群可调度副本数，不足则拒绝发布。
	if s.capacityProvider != nil && replicas > 0 && g.Resources.CPUm > 0 && g.Resources.MemoryBytes > 0 {
		cap, cerr := s.capacityProvider.GetClusterCapacity(ctx, CapacityQuery{
			ClusterID:   g.ClusterID,
			PerCPUM:     g.Resources.CPUm,
			PerMemBytes: g.Resources.MemoryBytes,
			PerGPU:      g.Resources.GPU,
		})
		if cerr == nil && cap != nil && replicas > cap.MaxReplicas {
			return nil, apperr.Validation(
				fmt.Sprintf("replicas exceeds cluster capacity: requested=%d, max=%d (source=%s)",
					replicas, cap.MaxReplicas, cap.Source), nil)
		}
	}

	// 审批预检查：若 group 需要审批，则创建 pending_approval 发布并挂起等待审批。
	needApproval := false
	if s.approvalChecker != nil {
		if need, err := s.approvalChecker.RequireApproval(ctx, in.GroupID); err == nil && need {
			needApproval = true
		}
	}

	initialStatus := release.StatusRunning
	initialProgress := 10
	if needApproval {
		initialStatus = release.StatusPendingApproval
		initialProgress = 0
	}

	rel := &release.Release{
		GroupID:               in.GroupID,
		ReleaseNumber:         releaseNumber,
		PreviousReleaseID:     prevID,
		ImageID:               imageID,
		ConfigVersion:         in.ConfigVersion,
		ReleaseType:           in.ReleaseType,
		Replicas:              replicas,
		Strategy:              in.Strategy,
		MaxSurge:              in.MaxSurge,
		MaxUnavailable:        in.MaxUnavailable,
		BatchSize:             in.BatchSize,
		BatchIntervalSec:      in.BatchIntervalSec,
		TargetPercentage:      in.TargetPercentage,
		TargetPodNames:        in.TargetPodNames,
		Status:                initialStatus,
		ProgressPercent:       initialProgress,
		TriggeredBy:           in.TriggeredBy,
		TriggerSource:         in.TriggerSource,
		AutoRollbackOnFailure: in.AutoRollbackOnFailure,
		StartedAt:             time.Now(),
	}
	rel.CreatedBy = in.TriggeredBy
	rel.UpdatedBy = in.TriggeredBy

	if err := s.repo.CreateRelease(ctx, rel); err != nil {
		if errors.Is(err, domain.ErrAlreadyExists) {
			return nil, apperr.Conflict("release number conflict", err)
		}
		return nil, apperr.Internal("create release", err)
	}

	// 追加初始事件（消息用资源名称，避免裸 ID）。
	if needApproval {
		s.appendEvent(ctx, rel.ID, "pending_approval",
			fmt.Sprintf("group %s #%d pending approval, image=%s", groupLabel(g), rel.ReleaseNumber, img.FullReference), in.TriggeredBy)
		if s.approvalChecker != nil {
			if _, aerr := s.approvalChecker.CreateForRelease(ctx, 0, g.ID, rel.ID, in.TriggeredBy); aerr != nil {
				s.appendEvent(ctx, rel.ID, "approval_create_failed", aerr.Error(), in.TriggeredBy)
			}
		}
		return rel, nil
	}

	s.appendEvent(ctx, rel.ID, "release_started",
		fmt.Sprintf("group %s #%d started, image=%s", groupLabel(g), rel.ReleaseNumber, img.FullReference), in.TriggeredBy)

	// 异步执行实际发布（不阻塞 API）。
	go s.executeRelease(context.Background(), rel, g, img, in.TriggeredBy)
	return rel, nil
}

// executeRelease 执行发布：按策略分支。
//   - rolling/recreate：一次性 apply 全量副本（向后兼容原逻辑）。
//   - percentage/machine_count：candidate Deployment 分批发布（多版本共存）。
//
// 统一应用分组：当 group.AppType 为 inference 时，部署由 inferenceapp.runDeploy 独立完成，
// releaseapp 不重复执行 K8s apply，仅记录 release 状态。本函数对这类直接标记成功并返回。
func (s *Service) executeRelease(ctx context.Context, rel *release.Release, g *application.Group, img *build.Image, operatorID int64) {
	if g.AppType == application.AppTypeInference {
		// 推理有独立部署路径，releaseapp 仅做状态记录。
		s.completeExternalRelease(ctx, rel, g, operatorID)
		return
	}
	switch rel.Strategy {
	case release.StrategyPercentage, release.StrategyMachineCount:
		s.executeCandidateRelease(ctx, rel, g, img, operatorID)
	default:
		s.executeRollingRelease(ctx, rel, g, img, operatorID)
	}
}

// completeExternalRelease 推理部署由 inference 服务异步完成，releaseapp 仅把 release 标记为成功。
// 这样统一应用分组下 vo_releases 仍保留记录，且不会与 inferenceapp 的部署冲突。
func (s *Service) completeExternalRelease(ctx context.Context, rel *release.Release, g *application.Group, operatorID int64) {
	now := time.Now()
	// 标记 release 成功（推理部署由 inference 服务异步完成，此处仅记录发布状态）。
	_, _ = s.repo.UpdateReleaseStatus(ctx, rel.ID, release.StatusSucceeded, 100, "", rel.Version)
	_ = rel // rel 已通过 UpdateReleaseStatus 落库
	rel.FinishedAt = &now
	rel.DurationMs = now.Sub(rel.StartedAt).Milliseconds()
	// 同步 group.CurrentReleaseID（推理 release 关联到 group）。
	if err := s.repo.UpdateGroupCurrentRelease(ctx, g.ID, rel.ID, rel.ImageID, rel.ConfigVersion); err != nil {
		// 非致命：发布已记录。
	}
	s.appendEvent(ctx, rel.ID, "succeeded", "external (inference) release recorded", operatorID)
}

// isInterrupted 检测发布是否已被中断（被同分组新发布抢占）。
// 被 interrupted 后 executeRelease 协程应立即退出，不再 apply/scale/写状态。
func (s *Service) isInterrupted(ctx context.Context, releaseID int64) bool {
	cur, err := s.repo.GetReleaseByID(ctx, releaseID)
	if err != nil {
		return false
	}
	return cur.Status != release.StatusRunning && cur.Status != release.StatusPaused
}

// executeRollingRelease 一次性 apply 全量副本（rolling/recreate/blue_green/canary 现有逻辑）。
func (s *Service) executeRollingRelease(ctx context.Context, rel *release.Release, g *application.Group, img *build.Image, operatorID int64) {
	startedAt := rel.StartedAt

	// 1. 分配稳定 IP（始终分配；软降级：失败不阻断发布）。
	stableIPs := s.allocateStableIPs(ctx, rel, g, operatorID)

	// 2. 解析生效配置（互斥：绑定配置集优先，否则本地配置）。
	cfg, err := s.resolveGroupConfig(ctx, g)
	if err != nil {
		s.failRelease(ctx, rel, fmt.Sprintf("resolve config: %v", err), operatorID, startedAt)
		return
	}

	// 3. 渲染工作负载。
	// 解析镜像所属仓库：私有仓库注入 imagePullSecret（命名约定 vortexops-registry-{id}）。
	var imagePullSecrets []string
	if s.registryResolver != nil && img.RegistryID > 0 {
		if reg, rerr := s.registryResolver.GetRegistryByID(ctx, img.RegistryID); rerr == nil && reg != nil {
			// 仅当仓库非公开（有凭证）时注入 imagePullSecret。
			if reg.CredentialID > 0 {
				imagePullSecrets = append(imagePullSecrets, fmt.Sprintf("vortexops-registry-%d", reg.ID))
			}
		}
	}
	maxSurge, maxUnavailable := stableIPRollingOverrides(stableIPs, rel.Replicas, rel.MaxSurge, rel.MaxUnavailable)
	if len(stableIPs) > 0 && rel.MaxSurge == "" && rel.MaxUnavailable == "" {
		s.appendEvent(ctx, rel.ID, "stable_ip_rolling",
			fmt.Sprintf("rolling maxSurge=%s maxUnavailable=%s (逐台替换以复用稳定 IP，发布过程允许短暂少于全量副本在线)",
				maxSurge, maxUnavailable), operatorID)
	}
	renderResult, err := workload.Render(workload.RenderInput{
		Group: g, ImageRef: img.FullReference, Config: cfg, StableIPs: stableIPs,
		ImagePullSecrets: imagePullSecrets,
		DeploymentStrategyOverride: releaseDeployStrategy(rel),
		MaxSurgeOverride:           maxSurge,
		MaxUnavailableOverride:     maxUnavailable,
		AppProbe:                   s.resolveAppProbe(ctx, g),
		NetworkProfile:             s.resolveNetworkProfile(ctx, g),
	})
	if err != nil {
		s.failRelease(ctx, rel, fmt.Sprintf("render workload: %v", err), operatorID, startedAt)
		return
	}

	// 4. 更新进度。
	s.appendEvent(ctx, rel.ID, "rendered", "workload rendered, applying to cluster", operatorID)
	s.bumpRunningProgress(ctx, rel, 30)

	// 被同分组新发布抢占则退出，不再 apply。
	if s.isInterrupted(ctx, rel.ID) {
		return
	}

	// 5. 应用到 K8s。
	clientset, err := s.clientProvider.GetClient(ctx, g.ClusterID)
	if err != nil {
		s.failRelease(ctx, rel, fmt.Sprintf("get k8s client: %v", err), operatorID, startedAt)
		return
	}
	applier := s.newWorkloadApplier(ctx, g.ClusterID, clientset)
	if err := applier.Apply(ctx, renderResult); err != nil {
		// 失败：若开启自动回滚，则触发回滚。
		s.appendEvent(ctx, rel.ID, "apply_failed", err.Error(), operatorID)
		if rel.AutoRollbackOnFailure && prevReleaseID(rel) != 0 {
			s.failRelease(ctx, rel, fmt.Sprintf("apply workload: %v", err), operatorID, startedAt)
			s.autoRollback(ctx, rel.GroupID, operatorID)
			return
		}
		s.failRelease(ctx, rel, fmt.Sprintf("apply workload: %v", err), operatorID, startedAt)
		return
	}

	// 6. 等待 rollout 就绪（仅 Deployment/StatefulSet）。
	s.appendEvent(ctx, rel.ID, "applied", "workload applied, waiting for rollout", operatorID)
	s.bumpRunningProgress(ctx, rel, 70)
	if g.Workload.Type == application.WorkloadDeployment || g.Workload.Type == application.WorkloadStatefulSet {
		if err := s.waitForRollout(ctx, clientset, g, rel.Replicas, rel.ID); err != nil {
			if errors.Is(err, errReleaseInterrupted) || s.isInterrupted(ctx, rel.ID) {
				return
			}
			s.failRelease(ctx, rel, fmt.Sprintf("rollout incomplete: %v", err), operatorID, startedAt)
			return
		}
	}

	// 7. 成功：回写 group current_release_id → 完成。
	if err := s.repo.UpdateGroupCurrentRelease(ctx, g.ID, rel.ID, rel.ImageID, rel.ConfigVersion); err != nil {
		// 非致命：发布已成功，仅记录。
	}
	s.appendEvent(ctx, rel.ID, "succeeded", "release succeeded", operatorID)
	s.syncDNSAfterRelease(ctx, rel, g, stableIPs, operatorID)
	s.completeReleaseWithRetry(ctx, rel.ID, release.StatusSucceeded, time.Since(startedAt).Milliseconds())
}

// executeCandidateRelease 多版本共存分批发布（percentage/machine_count）。
// 流程：渲染主(旧版本, replicas=0)+候选(新版本, replicas=0) → 分批 scale up 候选 → 达目标后晋升（候选镜像 apply 为全量主, 删候选, 删旧主）。
// 注意：为简化实现，主 Deployment 在发布期间保持旧版本全量副本（不缩容），候选独立扩容，
// 流量通过共享 selector 分流到两版本；晋升时主 Deployment 镜像更新为新版本，候选删除。
func (s *Service) executeCandidateRelease(ctx context.Context, rel *release.Release, g *application.Group, img *build.Image, operatorID int64) {
	startedAt := rel.StartedAt

	// 仅 Deployment 支持 candidate 模式；StatefulSet/Job 退化为一次性 apply。
	if g.Workload.Type != application.WorkloadDeployment {
		s.appendEvent(ctx, rel.ID, "candidate_unsupported", "workload type does not support candidate, fallback to rolling", operatorID)
		s.executeRollingRelease(ctx, rel, g, img, operatorID)
		return
	}

	// 计算候选目标副本数。
	targetReplicas := s.candidateTargetReplicas(rel, g)
	if targetReplicas <= 0 {
		s.failRelease(ctx, rel, "candidate target replicas is 0 (check percentage/target_pod_names)", operatorID, startedAt)
		return
	}

	// 批次大小：默认候选数/4，至少 1。
	batchSize := rel.BatchSize
	if batchSize <= 0 {
		batchSize = (targetReplicas + 3) / 4
	}
	if batchSize > targetReplicas {
		batchSize = targetReplicas
	}
	batchInterval := rel.BatchIntervalSec
	if batchInterval <= 0 {
		batchInterval = 10
	}

	// 1. 分配稳定 IP / 解析生效配置 / imagePullSecret。
	stableIPs, cfg, imagePullSecrets, err := s.prepareRelease(ctx, rel, g)
	if err != nil {
		s.failRelease(ctx, rel, err.Error(), operatorID, startedAt)
		return
	}

	// 2. 渲染：候选 Deployment（新版本, replicas=0 起步），主 Deployment 保持旧版本全量。
	// 主 Deployment 镜像：优先用 group 当前镜像（保持旧版本），否则用发布镜像（首次发布）。
	primaryImageRef := s.currentImageRef(ctx, g)
	if primaryImageRef == "" {
		primaryImageRef = img.FullReference
	}
	maxSurge, maxUnavailable := stableIPRollingOverrides(stableIPs, rel.Replicas, rel.MaxSurge, rel.MaxUnavailable)
	renderResult, err := workload.Render(workload.RenderInput{
		Group: g, ImageRef: primaryImageRef,
		Config: cfg, StableIPs: stableIPs, ImagePullSecrets: imagePullSecrets,
		CandidateImageRef: img.FullReference, CandidateReplicas: 0, CandidateReleaseID: rel.ID,
		CandidatePodNames: rel.TargetPodNames,
		DeploymentStrategyOverride: releaseDeployStrategy(rel),
		MaxSurgeOverride:           maxSurge,
		MaxUnavailableOverride:     maxUnavailable,
		AppProbe:                   s.resolveAppProbe(ctx, g),
		NetworkProfile:             s.resolveNetworkProfile(ctx, g),
	})
	if err != nil {
		s.failRelease(ctx, rel, fmt.Sprintf("render workload: %v", err), operatorID, startedAt)
		return
	}
	s.appendEvent(ctx, rel.ID, "rendered", fmt.Sprintf("candidate deployment rendered, target=%d batch=%d", targetReplicas, batchSize), operatorID)
	s.bumpRunningProgress(ctx, rel, 20)

	// 被同分组新发布抢占则退出，不再 apply。
	if s.isInterrupted(ctx, rel.ID) {
		return
	}

	// 3. 应用到 K8s（主+候选均 apply，候选 replicas=0）。
	clientset, err := s.clientProvider.GetClient(ctx, g.ClusterID)
	if err != nil {
		s.failRelease(ctx, rel, fmt.Sprintf("get k8s client: %v", err), operatorID, startedAt)
		return
	}
	applier := s.newWorkloadApplier(ctx, g.ClusterID, clientset)
	if err := applier.Apply(ctx, renderResult); err != nil {
		s.appendEvent(ctx, rel.ID, "apply_failed", err.Error(), operatorID)
		if rel.AutoRollbackOnFailure && prevReleaseID(rel) != 0 {
			s.failRelease(ctx, rel, fmt.Sprintf("apply workload: %v", err), operatorID, startedAt)
			s.autoRollback(ctx, rel.GroupID, operatorID)
			return
		}
		s.failRelease(ctx, rel, fmt.Sprintf("apply workload: %v", err), operatorID, startedAt)
		return
	}
	// 记录候选版本到 group。
	_ = s.repo.UpdateGroupCandidate(ctx, g.ID, rel.ID, rel.ImageID, 0)

	// 4. 分批 scale up 候选 Deployment。
	candName := g.DeploymentName + "-candidate"
	currentCand := 0
	batchIdx := 0
	for currentCand < targetReplicas {
		// 被同分组新发布抢占则退出，不再推进批次。
		if s.isInterrupted(ctx, rel.ID) {
			return
		}
		batchIdx++
		next := currentCand + batchSize
		if next > targetReplicas {
			next = targetReplicas
		}
		if err := scaleDeployment(ctx, clientset, g.Namespace, candName, int32(next)); err != nil {
			s.failCandidateRelease(ctx, rel, g, fmt.Sprintf("scale up candidate batch %d: %v", batchIdx, err), operatorID, startedAt)
			return
		}
		currentCand = next
		_ = s.repo.UpdateGroupCandidate(ctx, g.ID, rel.ID, rel.ImageID, currentCand)

		// 写批次记录。
		now := time.Now()
		_ = s.repo.CreateBatchRecord(ctx, &release.ReleaseBatchRecord{
			ReleaseID: rel.ID, BatchIndex: batchIdx, Status: release.StatusRunning, StartedAt: &now,
			Message: fmt.Sprintf("batch %d: candidate scaled to %d/%d", batchIdx, currentCand, targetReplicas),
		})

		// 等候选 Pod ready。
		if err := s.waitForDeploymentRollout(ctx, clientset, g.Namespace, candName, currentCand, rel.ID); err != nil {
			if errors.Is(err, errReleaseInterrupted) || s.isInterrupted(ctx, rel.ID) {
				return
			}
			s.failCandidateRelease(ctx, rel, g, fmt.Sprintf("candidate rollout batch %d: %v", batchIdx, err), operatorID, startedAt)
			return
		}

		// 进度：20(渲染) + 70*(currentCand/targetReplicas)。
		progress := 20 + int(70*currentCand/targetReplicas)
		s.bumpRunningProgress(ctx, rel, progress)
		now2 := time.Now()
		_ = s.repo.UpdateBatchRecord(ctx, &release.ReleaseBatchRecord{
			ReleaseID: rel.ID, BatchIndex: batchIdx, Status: release.StatusSucceeded, FinishedAt: &now2,
			Message: fmt.Sprintf("batch %d done: candidate=%d", batchIdx, currentCand),
		})

		if currentCand < targetReplicas {
			if !s.sleepOrInterrupt(ctx, rel.ID, batchInterval) {
				return
			}
		}
	}

	// 被抢占则不再晋升。
	if s.isInterrupted(ctx, rel.ID) {
		return
	}

	// 5. 晋升：主 Deployment 镜像更新为新版本全量副本 → 删候选 → 删旧主（同 Deployment 名，更新即覆盖）。
	s.appendEvent(ctx, rel.ID, "promoting", "candidate reached target, promoting to primary", operatorID)
	s.bumpRunningProgress(ctx, rel, 92)

	// 重新渲染主 Deployment 为新镜像全量副本（无候选），apply 覆盖。
	maxSurge, maxUnavailable = stableIPRollingOverrides(stableIPs, rel.Replicas, rel.MaxSurge, rel.MaxUnavailable)
	promoteResult, err := workload.Render(workload.RenderInput{
		Group: g, ImageRef: img.FullReference, Config: cfg, StableIPs: stableIPs, ImagePullSecrets: imagePullSecrets,
		DeploymentStrategyOverride: releaseDeployStrategy(rel),
		MaxSurgeOverride:           maxSurge,
		MaxUnavailableOverride:     maxUnavailable,
		AppProbe:                   s.resolveAppProbe(ctx, g),
		NetworkProfile:             s.resolveNetworkProfile(ctx, g),
	})
	if err != nil {
		s.failCandidateRelease(ctx, rel, g, fmt.Sprintf("render promote: %v", err), operatorID, startedAt)
		return
	}
	if err := applier.Apply(ctx, promoteResult); err != nil {
		s.failCandidateRelease(ctx, rel, g, fmt.Sprintf("apply promote: %v", err), operatorID, startedAt)
		return
	}
	// 等主 rollout。
	if err := s.waitForDeploymentRollout(ctx, clientset, g.Namespace, g.DeploymentName, rel.Replicas, rel.ID); err != nil {
		if errors.Is(err, errReleaseInterrupted) || s.isInterrupted(ctx, rel.ID) {
			return
		}
		s.failCandidateRelease(ctx, rel, g, fmt.Sprintf("promote rollout: %v", err), operatorID, startedAt)
		return
	}
	// 删候选 Deployment。
	if err := applier.DeleteDeployment(ctx, g.Namespace, candName); err != nil {
		s.appendEvent(ctx, rel.ID, "candidate_cleanup_failed", err.Error(), operatorID)
	}

	// 6. 回写 group current + 清空候选。
	if err := s.repo.UpdateGroupCurrentRelease(ctx, g.ID, rel.ID, rel.ImageID, rel.ConfigVersion); err != nil {
		s.appendEvent(ctx, rel.ID, "update_current_failed", err.Error(), operatorID)
	}
	_ = s.repo.ClearGroupCandidate(ctx, g.ID)

	s.appendEvent(ctx, rel.ID, "succeeded", "candidate release promoted and succeeded", operatorID)
	s.syncDNSAfterRelease(ctx, rel, g, stableIPs, operatorID)
	s.completeReleaseWithRetry(ctx, rel.ID, release.StatusSucceeded, time.Since(startedAt).Milliseconds())
}

// candidateTargetReplicas 计算候选目标副本数。
// - percentage：候选副本数 = ceil(group.replicas * pct/100)（分母=分组固定副本数）。
// - machine_count：候选副本数 = group.replicas（全量发布），分批推进，每批 batch_size 台机器。
//   语义：machine_count 策略下 batch_size 即「每批次机器数」，batch_interval_sec 即批次间隔。
func (s *Service) candidateTargetReplicas(rel *release.Release, g *application.Group) int {
	switch rel.Strategy {
	case release.StrategyPercentage:
		pct := rel.TargetPercentage
		if pct <= 0 {
			pct = 100
		}
		if pct > 100 {
			pct = 100
		}
		n := g.Replicas * pct / 100
		if n < 1 {
			n = 1
		}
		return n
	case release.StrategyMachineCount:
		if len(rel.TargetPodNames) > 0 {
			return len(rel.TargetPodNames)
		}
		// 全量发布：目标=分组副本数；batch_size 决定每批机器数。
		return g.Replicas
	}
	return 0
}

// currentImageRef 取 group 当前镜像引用（用于发布期间主 Deployment 保持旧版本）。
func (s *Service) currentImageRef(ctx context.Context, g *application.Group) string {
	if g.CurrentImageID == 0 {
		return ""
	}
	img, err := s.imageRepo.GetImage(ctx, g.CurrentImageID)
	if err != nil || img == nil {
		return ""
	}
	return img.FullReference
}

// stableIPRollingOverrides 稳定 IP 发布的默认滚动参数。
// maxSurge=0%：禁止先起新 Pod，避免新旧 Pod 争抢同一稳定 IP。
// maxUnavailable=ceil(100/replicas)%：逐台下线旧 Pod（3 副本时为 34%，即 1 台下线、2 台可用）。
// 用户显式传入 rel.MaxSurge / rel.MaxUnavailable 时不覆盖。
func stableIPRollingOverrides(stableIPs []string, replicas int, relMaxSurge, relMaxUnavailable string) (maxSurge, maxUnavailable string) {
	maxSurge = relMaxSurge
	maxUnavailable = relMaxUnavailable
	if len(stableIPs) == 0 {
		return maxSurge, maxUnavailable
	}
	if maxSurge == "" {
		maxSurge = "0%"
	}
	if maxUnavailable == "" {
		if replicas < 1 {
			replicas = 1
		}
		// ceil(100/replicas)：3 副本 → 34%（向下取整后 1 台不可用）。
		pct := (100 + replicas - 1) / replicas
		if pct > 100 {
			pct = 100
		}
		maxUnavailable = fmt.Sprintf("%d%%", pct)
	}
	return maxSurge, maxUnavailable
}

// newWorkloadApplier 创建带 dynamic client 的工作负载应用器（Cilium/Mesh CRD）。
func (s *Service) newWorkloadApplier(ctx context.Context, clusterID int64, clientset kubernetes.Interface) *workload.Applier {
	applier := workload.NewApplier(clientset)
	if s.dynamicProvider != nil {
		if dyn, err := s.dynamicProvider.GetDynamicClient(ctx, clusterID); err == nil && dyn != nil {
			applier.WithDynamic(dyn)
		}
	}
	return applier
}

// syncDNSAfterRelease 发布成功后更新域名→Pod IP 映射（软降级）。
func (s *Service) syncDNSAfterRelease(ctx context.Context, rel *release.Release, g *application.Group, stableIPs []string, operatorID int64) {
	if s.dnsMapper == nil || g == nil || len(stableIPs) == 0 {
		return
	}
	rec, err := s.dnsMapper.UpsertDomainMapping(ctx, g, stableIPs)
	if err != nil {
		s.appendEvent(ctx, rel.ID, "dns_sync_failed", err.Error(), operatorID)
		return
	}
	if rec != nil {
		s.appendEvent(ctx, rel.ID, "dns_synced",
			fmt.Sprintf("domain %s → %d Pod IPs", rec.FQDN, len(stableIPs)), operatorID)
	}
}

// allocateStableIPs 始终尝试分配稳定 IP；软降级：失败不阻断发布，仅记录事件。
// 分配成功后同时清理该 group 历史遗留的 Service/Ingress/NetworkPolicy（架构迁移期回收旧资源）。
func (s *Service) allocateStableIPs(ctx context.Context, rel *release.Release, g *application.Group, operatorID int64) []string {
	if s.ipAllocator == nil {
		s.appendEvent(ctx, rel.ID, "stable_ip_skip", "ipAllocator not configured, IP will NOT be preserved", operatorID)
		return nil
	}
	ips, err := s.ipAllocator.AllocateForGroup(ctx, g.ID, g.ClusterID, rel.Replicas)
	if err != nil {
		s.appendEvent(ctx, rel.ID, "stable_ip_alloc_failed",
			fmt.Sprintf("allocate stable IPs for group %s failed: %v (IP will NOT be preserved, release continues)", groupLabel(g), err), operatorID)
		return nil
	}
	profile := s.resolveNetworkProfile(ctx, g)
	cni := "unknown"
	if profile != nil && profile.CNI != "" {
		cni = string(profile.CNI)
	}
	s.appendEvent(ctx, rel.ID, "stable_ip_allocated",
		fmt.Sprintf("allocated %d stable IPs %v (cni=%s); 注解生效需集群已安装对应 CNI，Flannel 不支持静态 IP",
			len(ips), ips, cni), operatorID)
	// 清理历史遗留的 Service/Ingress/NetworkPolicy（架构迁移：不再创建这些资源）。
	s.cleanupLegacyNetworkResources(ctx, rel, g, operatorID)
	return ips
}

// cleanupLegacyNetworkResources 删除该 group 历史遗留的 Service/Ingress/NetworkPolicy。
// 幂等：不存在则忽略。仅在首次迁移时实际删除资源，之后调用为空操作。
func (s *Service) cleanupLegacyNetworkResources(ctx context.Context, rel *release.Release, g *application.Group, operatorID int64) {
	clientset, err := s.clientProvider.GetClient(ctx, g.ClusterID)
	if err != nil {
		return
	}
	ns := g.Namespace
	var deleted []string
	// Service（按 group ServiceName 命名）。
	if g.ServiceName != "" {
		if err := clientset.CoreV1().Services(ns).Delete(ctx, g.ServiceName, metav1.DeleteOptions{}); err == nil {
			deleted = append(deleted, "Service/"+g.ServiceName)
		}
	}
	// Ingress（按 {deployment}-ingress 命名）。
	ingName := g.DeploymentName + "-ingress"
	if err := clientset.NetworkingV1().Ingresses(ns).Delete(ctx, ingName, metav1.DeleteOptions{}); err == nil {
		deleted = append(deleted, "Ingress/"+ingName)
	}
	// NetworkPolicy（按 {deployment}-netpol 命名）。
	npName := g.DeploymentName + "-netpol"
	if err := clientset.NetworkingV1().NetworkPolicies(ns).Delete(ctx, npName, metav1.DeleteOptions{}); err == nil {
		deleted = append(deleted, "NetworkPolicy/"+npName)
	}
	if len(deleted) > 0 {
		s.appendEvent(ctx, rel.ID, "legacy_resources_cleaned",
			fmt.Sprintf("removed legacy K8s resources (no longer created): %v", deleted), operatorID)
	}
}

// prepareRelease 分配稳定 IP + 解析生效配置 + imagePullSecret（候选与主共享）。
func (s *Service) prepareRelease(ctx context.Context, rel *release.Release, g *application.Group) (stableIPs []string, cfg *workload.ResolvedConfig, imagePullSecrets []string, err error) {
	// 始终分配稳定 IP；软降级：失败不阻断发布。
	stableIPs = s.allocateStableIPs(ctx, rel, g, rel.TriggeredBy)
	cfg, merr := s.resolveGroupConfig(ctx, g)
	if merr != nil {
		err = fmt.Errorf("resolve config: %w", merr)
		return
	}
	// 取镜像仓库注入 imagePullSecret（用发布镜像解析）。
	if s.registryResolver != nil {
		if img, ierr := s.imageRepo.GetImage(ctx, rel.ImageID); ierr == nil && img != nil && img.RegistryID > 0 {
			if reg, rerr := s.registryResolver.GetRegistryByID(ctx, img.RegistryID); rerr == nil && reg != nil && reg.CredentialID > 0 {
				imagePullSecrets = append(imagePullSecrets, fmt.Sprintf("vortexops-registry-%d", reg.ID))
			}
		}
	}
	return
}

// failCandidateRelease 标记候选发布失败并清理候选 Deployment。
func (s *Service) failCandidateRelease(ctx context.Context, rel *release.Release, g *application.Group, reason string, operatorID int64, startedAt time.Time) {
	s.appendEvent(ctx, rel.ID, "failed", reason, operatorID)
	// 清理候选 Deployment（避免残留）。
	if clientset, err := s.clientProvider.GetClient(ctx, g.ClusterID); err == nil {
		applier := workload.NewApplier(clientset)
		_ = applier.DeleteDeployment(ctx, g.Namespace, g.DeploymentName+"-candidate")
	}
	_ = s.repo.ClearGroupCandidate(ctx, g.ID)
	s.completeReleaseWithRetry(ctx, rel.ID, release.StatusFailed, time.Since(startedAt).Milliseconds())
}

// waitForDeploymentRollout 等待指定 Deployment ready 副本数达标。
func (s *Service) waitForDeploymentRollout(ctx context.Context, clientset kubernetes.Interface, namespace, name string, replicas int, releaseID int64) error {
	deadline := time.Now().Add(10 * time.Minute)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if releaseID > 0 && s.isInterrupted(ctx, releaseID) {
				return errReleaseInterrupted
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("rollout %s/%s timed out after 10m", namespace, name)
			}
			d, err := clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if d.Status.ReadyReplicas >= int32(replicas) && d.Status.UpdatedReplicas >= int32(replicas) {
				return nil
			}
		}
	}
}

// scaleDeployment 通过 /scale subresource 调整 Deployment 副本数。
// 带冲突重试：刚 apply 的 Deployment 可能被 controller 并发更新 resourceVersion。
func scaleDeployment(ctx context.Context, clientset kubernetes.Interface, namespace, name string, replicas int32) error {
	const maxRetries = 5
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		scale, err := clientset.AppsV1().Deployments(namespace).GetScale(ctx, name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("get scale: %w", err)
		}
		if scale.Spec.Replicas == replicas {
			return nil
		}
		scale.Spec.Replicas = replicas
		if _, err := clientset.AppsV1().Deployments(namespace).UpdateScale(ctx, name, scale, metav1.UpdateOptions{}); err != nil {
			if apierrors.IsConflict(err) {
				lastErr = err
				select {
				case <-ctx.Done():
					return fmt.Errorf("update scale: %w", ctx.Err())
				case <-time.After(time.Duration(200*(i+1)) * time.Millisecond):
					continue
				}
			}
			return fmt.Errorf("update scale: %w", err)
		}
		return nil
	}
	return fmt.Errorf("update scale: conflict after %d retries: %w", maxRetries, lastErr)
}


// resolveGroupConfig 解析分组的生效配置（互斥：绑定配置集优先，否则本地配置）。
// 绑定配置集时只用配置集内容，本地配置被完全覆盖（不合并）。返回结构化配置供渲染器消费。
func (s *Service) resolveGroupConfig(ctx context.Context, g *application.Group) (*workload.ResolvedConfig, error) {
	if s.configRepo == nil {
		return &workload.ResolvedConfig{}, nil
	}
	// 互斥取值：绑定配置集优先。
	bindings, err := s.configRepo.ListBindings(ctx, g.ID)
	if err != nil {
		return nil, fmt.Errorf("list config bindings: %w", err)
	}
	for _, b := range bindings {
		if b.ConfigSetID != 0 {
			cs, err := s.configRepo.GetConfigSet(ctx, b.ConfigSetID)
			if err != nil {
				return nil, fmt.Errorf("get config set %d: %w", b.ConfigSetID, err)
			}
			return workload.ParseResolvedContent(cs.Content), nil
		}
	}
	// 无绑定 → 取本地配置（可能为空）。
	lc, err := s.configRepo.GetLocalConfig(ctx, g.ID)
	if err != nil {
		// 本地配置不存在视为空配置（分组未配置任何内容）。
		// configapp.GetLocalConfig 把领域 ErrLocalConfigNotFound 转为 apperr.NotFound（丢弃领域错误），
		// 故此处按 apperr code 判定，而非 errors.Is 领域错误。
		if apperr.CodeOf(err) == apperr.CodeNotFound {
			return &workload.ResolvedConfig{}, nil
		}
		return nil, fmt.Errorf("get local config: %w", err)
	}
	return workload.ParseResolvedContent(lc.Content), nil
}

// waitForRollout 等待 Deployment/StatefulSet 滚动完成。
func (s *Service) waitForRollout(ctx context.Context, clientset kubernetes.Interface, g *application.Group, replicas int, releaseID int64) error {
	deadline := time.Now().Add(10 * time.Minute)
	check := time.NewTicker(5 * time.Second)
	defer check.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-check.C:
			if releaseID > 0 && s.isInterrupted(ctx, releaseID) {
				return errReleaseInterrupted
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("rollout timed out after 10m")
			}
			ready, err := s.checkRolloutReady(ctx, clientset, g, replicas)
			if err != nil {
				return err
			}
			if ready {
				return nil
			}
		}
	}
}

func (s *Service) checkRolloutReady(ctx context.Context, clientset kubernetes.Interface, g *application.Group, replicas int) (bool, error) {
	switch g.Workload.Type {
	case application.WorkloadDeployment:
		d, err := clientset.AppsV1().Deployments(g.Namespace).Get(ctx, g.DeploymentName, metav1GetOpts())
		if err != nil {
			return false, err
		}
		return d.Status.ReadyReplicas >= int32(replicas) && d.Status.UpdatedReplicas >= int32(replicas), nil
	case application.WorkloadStatefulSet:
		ss, err := clientset.AppsV1().StatefulSets(g.Namespace).Get(ctx, g.DeploymentName, metav1GetOpts())
		if err != nil {
			return false, err
		}
		return ss.Status.ReadyReplicas >= int32(replicas) && ss.Status.UpdatedReplicas >= int32(replicas), nil
	}
	return true, nil
}

// failRelease 标记发布失败并记录事件。
func (s *Service) failRelease(ctx context.Context, rel *release.Release, reason string, operatorID int64, startedAt time.Time) {
	s.appendEvent(ctx, rel.ID, "failed", reason, operatorID)
	s.completeReleaseWithRetry(ctx, rel.ID, release.StatusFailed, time.Since(startedAt).Milliseconds())
}

// interruptInProgressReleases 中断同分组进行中的发布：清理候选/部分批次容器，标记 interrupted，
// 确保后续新发布独占集群资源（永远最新发布覆盖旧的）。
func (s *Service) interruptInProgressReleases(ctx context.Context, groupID, operatorID int64) error {
	var g *application.Group
	// 先清理 K8s 上该分组残留的候选 Deployment 与候选副本，避免旧批次与新发布并发。
	if gg, err := s.groupRepo.GetGroup(ctx, groupID); err == nil && gg != nil {
		g = gg
		if g.DeploymentName != "" {
			if clientset, err := s.clientProvider.GetClient(ctx, g.ClusterID); err == nil {
				applier := workload.NewApplier(clientset)
				candName := g.DeploymentName + "-candidate"
				_ = scaleDeployment(ctx, clientset, g.Namespace, candName, 0)
				_ = applier.DeleteDeployment(ctx, g.Namespace, candName)
			}
		}
		_ = s.repo.ClearGroupCandidate(ctx, g.ID)
	}

	for _, st := range []release.Status{release.StatusRunning, release.StatusPaused} {
		rels, err := s.repo.GetReleasesByStatus(ctx, groupID, st)
		if err != nil {
			return apperr.Internal("list in-progress releases", err)
		}
		for _, r := range rels {
			s.appendEvent(ctx, r.ID, "interrupted",
				fmt.Sprintf("group %s #%d interrupted: candidate workloads cleaned, superseded by newer release", groupLabel(g), r.ReleaseNumber), operatorID)
			s.completeReleaseWithRetry(ctx, r.ID, release.StatusInterrupted, 0)
		}
	}
	return nil
}

// groupLabel 发布轨迹用：优先显示名，其次 name。
func groupLabel(g *application.Group) string {
	if g == nil {
		return "unknown-group"
	}
	if g.DisplayName != "" {
		return g.DisplayName
	}
	if g.Name != "" {
		return g.Name
	}
	return "unknown-group"
}

// releaseDeployStrategy 将发布策略映射为 Deployment 滚动/一次性策略（rolling/recreate）。
func releaseDeployStrategy(rel *release.Release) string {
	switch rel.Strategy {
	case release.StrategyRecreate:
		return string(release.StrategyRecreate)
	case release.StrategyRolling:
		return string(release.StrategyRolling)
	default:
		return ""
	}
}

// sleepOrInterrupt 批次间隔等待；若被新发布抢占则返回 false。
func (s *Service) sleepOrInterrupt(ctx context.Context, releaseID int64, sec int) bool {
	if sec <= 0 {
		return !s.isInterrupted(ctx, releaseID)
	}
	deadline := time.Now().Add(time.Duration(sec) * time.Second)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if s.isInterrupted(ctx, releaseID) {
			return false
		}
		if time.Now().After(deadline) {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
		}
	}
}

// bumpRunningProgress 更新 running 进度并回写 rel.Version，避免连续乐观锁更新因 version 过期静默失败。
func (s *Service) bumpRunningProgress(ctx context.Context, rel *release.Release, progress int) {
	if rel == nil {
		return
	}
	updated, err := s.repo.UpdateReleaseStatus(ctx, rel.ID, release.StatusRunning, progress, "", rel.Version)
	if err != nil {
		if !errors.Is(err, domain.ErrConflict) {
			return
		}
		cur, gerr := s.repo.GetReleaseByID(ctx, rel.ID)
		if gerr != nil {
			return
		}
		if cur.Status != release.StatusRunning && cur.Status != release.StatusPaused {
			return
		}
		updated, err = s.repo.UpdateReleaseStatus(ctx, rel.ID, release.StatusRunning, progress, "", cur.Version)
		if err != nil {
			return
		}
	}
	if updated != nil {
		rel.Version = updated.Version
		rel.Status = updated.Status
		rel.ProgressPercent = updated.ProgressPercent
	}
}

// completeReleaseWithRetry 完成/终止发布，自动重取最新 version 以规避乐观锁冲突。
// executeRelease 协程内会多次 UpdateReleaseStatus（递增 version），rel.Version 可能已过期，
// 直接用旧 version 调 CompleteRelease 会因 WHERE version=$6 不匹配而静默失败，导致发布卡在 running。
func (s *Service) completeReleaseWithRetry(ctx context.Context, releaseID int64, status release.Status, durationMs int64) {
	const maxRetries = 3
	for i := 0; i < maxRetries; i++ {
		cur, err := s.repo.GetReleaseByID(ctx, releaseID)
		if err != nil {
			return
		}
		if cur.Status != release.StatusRunning && cur.Status != release.StatusPaused && status != release.StatusInterrupted {
			// 已是终态，无需再完成。
			return
		}
		if _, err := s.repo.CompleteRelease(ctx, releaseID, status, durationMs, time.Now(), cur.Version); err != nil {
			if errors.Is(err, domain.ErrConflict) {
				select {
				case <-ctx.Done():
					return
				case <-time.After(time.Duration(100*(i+1)) * time.Millisecond):
					continue
				}
			}
			return
		}
		return
	}
}

func (s *Service) appendEvent(ctx context.Context, releaseID int64, eventType, message string, operatorID int64) {
	// 简化 seq：用时间戳纳秒转 int（实际应查 max seq +1，但事件追加允许近似）。
	now := time.Now()
	_ = s.repo.AppendEvent(ctx, &release.ReleaseEvent{
		ReleaseID: releaseID, Seq: int(now.UnixNano() % 1e9), EventType: eventType,
		Message: message, OperatorID: operatorID, OccurredAt: now,
	})
}

// autoRollback 自动回滚到上一成功发布。
func (s *Service) autoRollback(ctx context.Context, groupID, operatorID int64) {
	prev, err := s.repo.GetLastSuccessfulRelease(ctx, groupID)
	if err != nil {
		return
	}
	_, _ = s.TriggerRelease(ctx, TriggerReleaseInput{
		GroupID: groupID, ImageID: prev.ImageID, ConfigVersion: prev.ConfigVersion,
		ReleaseType: release.ReleaseRollback, Replicas: prev.Replicas, Strategy: prev.Strategy,
		MaxSurge: prev.MaxSurge, MaxUnavailable: prev.MaxUnavailable,
		TriggeredBy: operatorID, TriggerSource: release.TriggerAPI,
	})
}

// Rollback 手动回滚到上一成功发布。
func (s *Service) Rollback(ctx context.Context, groupID, operatorID int64) (*release.Release, error) {
	prev, err := s.repo.GetLastSuccessfulRelease(ctx, groupID)
	if err != nil {
		if errors.Is(err, release.ErrNoPreviousRelease) {
			return nil, apperr.NotFound("previous release", "")
		}
		return nil, apperr.Internal("get last release", err)
	}
	return s.TriggerRelease(ctx, TriggerReleaseInput{
		GroupID: groupID, ImageID: prev.ImageID, ConfigVersion: prev.ConfigVersion,
		ReleaseType: release.ReleaseRollback, Replicas: prev.Replicas, Strategy: prev.Strategy,
		MaxSurge: prev.MaxSurge, MaxUnavailable: prev.MaxUnavailable,
		TriggeredBy: operatorID, TriggerSource: release.TriggerManual,
	})
}

// AbortRelease 中止运行中的发布。
func (s *Service) AbortRelease(ctx context.Context, id, operatorID int64) (*release.Release, error) {
	rel, err := s.repo.GetReleaseByID(ctx, id)
	if err != nil {
		if errors.Is(err, release.ErrReleaseNotFound) {
			return nil, apperr.NotFound("release", strconv.FormatInt(id, 10))
		}
		return nil, apperr.Internal("get release", err)
	}
	if rel.Status != release.StatusRunning && rel.Status != release.StatusPaused {
		return nil, apperr.BusinessRule("release cannot be aborted in current state", release.ErrReleaseNotCancellable)
	}
	s.appendEvent(ctx, id, "aborted", "release aborted by operator", operatorID)
	updated, err := s.repo.CompleteRelease(ctx, id, release.StatusAborted, 0, time.Now(), rel.Version)
	if err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return nil, apperr.Conflict("release was modified concurrently, please refresh", err)
		}
		return nil, apperr.Internal("abort release", err)
	}
	return updated, nil
}

// GetRelease 按 ID 查询发布。
func (s *Service) GetRelease(ctx context.Context, id int64) (*release.Release, error) {
	rel, err := s.repo.GetReleaseByID(ctx, id)
	if err != nil {
		if errors.Is(err, release.ErrReleaseNotFound) {
			return nil, apperr.NotFound("release", strconv.FormatInt(id, 10))
		}
		return nil, apperr.Internal("get release", err)
	}
	return rel, nil
}

// ListReleases 分页查询发布。
func (s *Service) ListReleases(ctx context.Context, groupID int64, status release.Status, page, size int) ([]*release.Release, int64, error) {
	items, total, err := s.repo.ListReleases(ctx, release.ReleaseQuery{
		GroupID: groupID, Status: status, Offset: (page - 1) * size, Limit: size,
	})
	if err != nil {
		return nil, 0, apperr.Internal("list releases", err)
	}
	return items, total, nil
}

// ListReleaseEvents 列出发布事件。
func (s *Service) ListReleaseEvents(ctx context.Context, releaseID int64) ([]*release.ReleaseEvent, error) {
	items, err := s.repo.ListEvents(ctx, releaseID)
	if err != nil {
		return nil, apperr.Internal("list release events", err)
	}
	return items, nil
}

// ListBatchRecords 列出批次记录。
func (s *Service) ListBatchRecords(ctx context.Context, releaseID int64) ([]*release.ReleaseBatchRecord, error) {
	items, err := s.repo.ListBatchRecords(ctx, releaseID)
	if err != nil {
		return nil, apperr.Internal("list batch records", err)
	}
	return items, nil
}

// --- 预设 ---

// CreatePresetInput 创建预设输入。
type CreatePresetInput struct {
	Scope                 release.PresetScope
	ScopeID               int64
	Name                  string
	Description           string
	Strategy              release.Strategy
	MaxSurge              string
	MaxUnavailable        string
	BatchSize             int
	BatchIntervalSec      int
	AutoRollbackOnFailure bool
	IsDefault             bool
	CreatedBy             int64
}

// CreatePreset 创建预设。
func (s *Service) CreatePreset(ctx context.Context, in CreatePresetInput) (*release.ReleasePreset, error) {
	if in.Name == "" {
		return nil, apperr.Validation("preset name is required", nil)
	}
	if in.Strategy == "" {
		in.Strategy = release.StrategyRolling
	}
	p := &release.ReleasePreset{
		Scope: in.Scope, ScopeID: in.ScopeID, Name: in.Name, Description: in.Description,
		Strategy: in.Strategy, MaxSurge: in.MaxSurge, MaxUnavailable: in.MaxUnavailable,
		BatchSize: in.BatchSize, BatchIntervalSec: in.BatchIntervalSec,
		AutoRollbackOnFailure: in.AutoRollbackOnFailure, IsDefault: in.IsDefault,
	}
	p.CreatedBy = in.CreatedBy
	p.UpdatedBy = in.CreatedBy
	if err := s.repo.CreatePreset(ctx, p); err != nil {
		return nil, apperr.Internal("create preset", err)
	}
	return p, nil
}

// ListPresets 分页列出预设。
func (s *Service) ListPresets(ctx context.Context, scope release.PresetScope, scopeID int64, page, size int) ([]*release.ReleasePreset, int64, error) {
	items, total, err := s.repo.ListPresets(ctx, scope, scopeID, (page-1)*size, size)
	if err != nil {
		return nil, 0, apperr.Internal("list presets", err)
	}
	return items, total, nil
}

// DeletePreset 软删除预设。
func (s *Service) DeletePreset(ctx context.Context, id, actorID int64) error {
	if err := s.repo.DeletePreset(ctx, id, actorID); err != nil {
		if errors.Is(err, release.ErrPresetNotFound) {
			return apperr.NotFound("release preset", strconv.FormatInt(id, 10))
		}
		return apperr.Internal("delete preset", err)
	}
	return nil
}

// --- 窗口 ---

// CreateWindowInput 创建窗口输入。
type CreateWindowInput struct {
	ApplicationID   int64
	Name            string
	Timezone        string
	Crontab         string
	DurationMinutes int
	IsActive        bool
	CreatedBy       int64
}

// CreateWindow 创建发布窗口。
func (s *Service) CreateWindow(ctx context.Context, in CreateWindowInput) (*release.ReleaseWindow, error) {
	if in.Name == "" || in.Crontab == "" {
		return nil, apperr.Validation("name and crontab are required", nil)
	}
	w := &release.ReleaseWindow{
		ApplicationID: in.ApplicationID, Name: in.Name, Timezone: in.Timezone,
		Crontab: in.Crontab, DurationMinutes: in.DurationMinutes, IsActive: in.IsActive,
	}
	w.CreatedBy = in.CreatedBy
	w.UpdatedBy = in.CreatedBy
	if err := s.repo.CreateWindow(ctx, w); err != nil {
		return nil, apperr.Internal("create window", err)
	}
	return w, nil
}

// ListWindows 列出应用的窗口。
func (s *Service) ListWindows(ctx context.Context, appID int64) ([]*release.ReleaseWindow, error) {
	items, err := s.repo.ListWindows(ctx, appID)
	if err != nil {
		return nil, apperr.Internal("list windows", err)
	}
	return items, nil
}

// DeleteWindow 软删除窗口。
func (s *Service) DeleteWindow(ctx context.Context, id, actorID int64) error {
	if err := s.repo.DeleteWindow(ctx, id, actorID); err != nil {
		if errors.Is(err, release.ErrWindowNotFound) {
			return apperr.NotFound("release window", strconv.FormatInt(id, 10))
		}
		return apperr.Internal("delete window", err)
	}
	return nil
}

// --- helpers ---

func prevReleaseID(r *release.Release) int64 { return r.PreviousReleaseID }

// metav1GetOpts 避免 import 冗余。
func metav1GetOpts() metav1.GetOptions { return metav1.GetOptions{} }

// 确保 kubernetes import 被使用（waitForRollout/checkRolloutReady 签名引用）。
var _ kubernetes.Interface = (kubernetes.Interface)(nil)
