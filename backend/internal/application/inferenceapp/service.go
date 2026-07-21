// Package inferenceapp 是大模型推理服务的应用层：模型仓库/版本 CRUD、服务部署、API Key、计量。
package inferenceapp

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"k8s.io/client-go/kubernetes"

	"github.com/vortexops/vortexops/internal/domain/application"
	"github.com/vortexops/vortexops/internal/domain/inference"
	infk8s "github.com/vortexops/vortexops/internal/infrastructure/k8s"
	infrender "github.com/vortexops/vortexops/internal/infrastructure/k8s/inference"
	"github.com/vortexops/vortexops/internal/infrastructure/kafka"
	"github.com/vortexops/vortexops/internal/platform/security"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// KubeconfigProvider 解密集群 kubeconfig（clusterapp.Service 实现）。
type KubeconfigProvider interface {
	GetDecryptedKubeconfig(ctx context.Context, clusterID int64) ([]byte, error)
	GetClusterInsecureSkipTLS(ctx context.Context, clusterID int64) (bool, error)
}

// ApplicationCreator 由 applicationapp.Service 实现（通过接口注入避免循环依赖）。
// 用于推理服务统一为应用分组时自动创建对应的 application + group。
type ApplicationCreator interface {
	CreateTypedApplicationForInfra(ctx context.Context, appType, name, code string, workspaceID, ownerID int64) (appID int64, err error)
	CreateGroupForTypedAppForInfra(ctx context.Context, applicationID int64, name string, clusterID int64, namespace string, appType string, actorID int64) (groupID int64, err error)
}

// Service 推理应用服务。
type Service struct {
	repo      inference.Repository
	pool      *infk8s.ClientPool
	cluster   KubeconfigProvider
	producer  *kafka.Producer
	brokers   []string
	topicKey  string
	topicName string
	appSvc    ApplicationCreator // 可为 nil：未启用统一应用分组时跳过
}

// New 创建服务。producer 可为 nil（未启用 Kafka）。
func New(repo inference.Repository, pool *infk8s.ClientPool, cluster KubeconfigProvider, producer *kafka.Producer, brokers []string, topicKey, topicName string) *Service {
	return &Service{
		repo: repo, pool: pool, cluster: cluster, producer: producer,
		brokers: brokers, topicKey: topicKey, topicName: topicName,
	}
}

// WithApplicationCreator 注入应用创建能力（统一为应用分组时用）。
func (s *Service) WithApplicationCreator(appSvc ApplicationCreator) *Service {
	s.appSvc = appSvc
	return s
}

// --- registry ---

type CreateRegistryInput struct {
	WorkspaceID    int64
	Name           string
	Provider       inference.RegistryProvider
	Endpoint       string
	CredentialID   int64
	CachePVCName   string
	CachePath      string
	CacheSizeBytes int64
	CreatedBy      int64
}

func (s *Service) CreateRegistry(ctx context.Context, in CreateRegistryInput) (*inference.ModelRegistry, error) {
	exist, err := s.repo.GetRegistryByName(ctx, in.WorkspaceID, in.Name)
	if err != nil {
		return nil, apperr.Internal("check registry name", err)
	}
	if exist != nil {
		return nil, apperr.Conflict("registry name exists", inference.ErrRegistryNameUsed)
	}
	reg := &inference.ModelRegistry{
		WorkspaceID: in.WorkspaceID, Name: in.Name, Provider: in.Provider, Endpoint: in.Endpoint,
		CredentialID: in.CredentialID, CachePVCName: in.CachePVCName, CachePath: in.CachePath,
		CacheSizeBytes: in.CacheSizeBytes, Status: "active",
	}
	reg.CreatedBy = in.CreatedBy
	reg.UpdatedBy = in.CreatedBy
	if err := s.repo.CreateRegistry(ctx, reg); err != nil {
		return nil, apperr.Internal("create registry", err)
	}
	return reg, nil
}

func (s *Service) GetRegistry(ctx context.Context, id int64) (*inference.ModelRegistry, error) {
	reg, err := s.repo.GetRegistryByID(ctx, id)
	if err != nil {
		if errors.Is(err, inference.ErrRegistryNotFound) {
			return nil, apperr.NotFound("model registry", fmt.Sprint(id))
		}
		return nil, apperr.Internal("get registry", err)
	}
	return reg, nil
}

func (s *Service) ListRegistries(ctx context.Context, workspaceID int64, page, size int) ([]*inference.ModelRegistry, int64, error) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 50
	}
	return s.repo.ListRegistries(ctx, workspaceID, (page-1)*size, size)
}

func (s *Service) DeleteRegistry(ctx context.Context, id, actorID int64) error {
	if err := s.repo.DeleteRegistry(ctx, id, actorID); err != nil {
		if errors.Is(err, inference.ErrRegistryNotFound) {
			return apperr.NotFound("model registry", fmt.Sprint(id))
		}
		return apperr.Internal("delete registry", err)
	}
	return nil
}

// --- model ---

type CreateModelInput struct {
	WorkspaceID      int64
	RegistryID       int64
	Name             string
	DisplayName      string
	Description      string
	BaseArchitecture string
	ParameterCount   string
	License          string
	Tags             []string
	CreatedBy        int64
}

func (s *Service) CreateModel(ctx context.Context, in CreateModelInput) (*inference.Model, error) {
	exist, err := s.repo.GetModelByName(ctx, in.WorkspaceID, in.Name)
	if err != nil {
		return nil, apperr.Internal("check model name", err)
	}
	if exist != nil {
		return nil, apperr.Conflict("model name exists", inference.ErrModelNameUsed)
	}
	m := &inference.Model{
		WorkspaceID: in.WorkspaceID, RegistryID: in.RegistryID, Name: in.Name, DisplayName: in.DisplayName,
		Description: in.Description, BaseArchitecture: in.BaseArchitecture, ParameterCount: in.ParameterCount,
		License: in.License, Tags: in.Tags,
	}
	m.CreatedBy = in.CreatedBy
	m.UpdatedBy = in.CreatedBy
	if err := s.repo.CreateModel(ctx, m); err != nil {
		return nil, apperr.Internal("create model", err)
	}
	return m, nil
}

func (s *Service) GetModel(ctx context.Context, id int64) (*inference.Model, error) {
	m, err := s.repo.GetModelByID(ctx, id)
	if err != nil {
		if errors.Is(err, inference.ErrModelNotFound) {
			return nil, apperr.NotFound("model", fmt.Sprint(id))
		}
		return nil, apperr.Internal("get model", err)
	}
	return m, nil
}

func (s *Service) ListModels(ctx context.Context, workspaceID, registryID int64, page, size int) ([]*inference.Model, int64, error) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 50
	}
	return s.repo.ListModels(ctx, workspaceID, registryID, (page-1)*size, size)
}

func (s *Service) DeleteModel(ctx context.Context, id, actorID int64) error {
	if err := s.repo.DeleteModel(ctx, id, actorID); err != nil {
		if errors.Is(err, inference.ErrModelNotFound) {
			return apperr.NotFound("model", fmt.Sprint(id))
		}
		return apperr.Internal("delete model", err)
	}
	return nil
}

// --- model version ---

type CreateModelVersionInput struct {
	ModelID             int64
	Version             string
	Precision           inference.Precision
	Quantization        inference.Quantization
	WeightsPath         string
	WeightsSizeBytes    int64
	WeightsChecksum     string
	Framework           inference.Framework
	FrameworkConfig     map[string]any
	MinGPUMemoryBytes   int64
	RecommendedGPUCount int
	IsDefault           bool
	CreatedBy           int64
}

func (s *Service) CreateModelVersion(ctx context.Context, in CreateModelVersionInput) (*inference.ModelVersion, error) {
	if _, err := s.repo.GetModelByID(ctx, in.ModelID); err != nil {
		return nil, apperr.NotFound("model", fmt.Sprint(in.ModelID))
	}
	v := &inference.ModelVersion{
		ModelID: in.ModelID, Version: in.Version, Precision: in.Precision, Quantization: in.Quantization,
		WeightsPath: in.WeightsPath, WeightsSizeBytes: in.WeightsSizeBytes, WeightsChecksum: in.WeightsChecksum,
		Framework: in.Framework, FrameworkConfig: in.FrameworkConfig, MinGPUMemoryBytes: in.MinGPUMemoryBytes,
		RecommendedGPUCount: in.RecommendedGPUCount, DownloadStatus: inference.DownloadNotDownloaded, IsDefault: in.IsDefault,
	}
	if v.Precision == "" {
		v.Precision = inference.PrecisionFP16
	}
	if v.Framework == "" {
		v.Framework = inference.FrameworkVLLM
	}
	v.CreatedBy = in.CreatedBy
	v.UpdatedBy = in.CreatedBy
	if err := s.repo.CreateModelVersion(ctx, v); err != nil {
		return nil, apperr.Internal("create model version", err)
	}
	return v, nil
}

func (s *Service) GetModelVersion(ctx context.Context, id int64) (*inference.ModelVersion, error) {
	v, err := s.repo.GetModelVersionByID(ctx, id)
	if err != nil {
		if errors.Is(err, inference.ErrModelVersionNotFound) {
			return nil, apperr.NotFound("model version", fmt.Sprint(id))
		}
		return nil, apperr.Internal("get model version", err)
	}
	return v, nil
}

func (s *Service) ListModelVersions(ctx context.Context, modelID int64) ([]*inference.ModelVersion, error) {
	return s.repo.ListModelVersions(ctx, modelID)
}

func (s *Service) DeleteModelVersion(ctx context.Context, id, actorID int64) error {
	if err := s.repo.DeleteModelVersion(ctx, id, actorID); err != nil {
		if errors.Is(err, inference.ErrModelVersionNotFound) {
			return apperr.NotFound("model version", fmt.Sprint(id))
		}
		return apperr.Internal("delete model version", err)
	}
	return nil
}

// --- adapter ---

type CreateAdapterInput struct {
	BaseModelVersionID int64
	Name               string
	AdapterType        inference.AdapterType
	WeightsPath        string
	Rank               int
	Scale              float64
	CreatedBy          int64
}

func (s *Service) CreateAdapter(ctx context.Context, in CreateAdapterInput) (*inference.ModelAdapter, error) {
	if _, err := s.repo.GetModelVersionByID(ctx, in.BaseModelVersionID); err != nil {
		return nil, apperr.NotFound("model version", fmt.Sprint(in.BaseModelVersionID))
	}
	a := &inference.ModelAdapter{
		BaseModelVersionID: in.BaseModelVersionID, Name: in.Name, AdapterType: in.AdapterType,
		WeightsPath: in.WeightsPath, Rank: in.Rank, Scale: in.Scale,
	}
	if a.AdapterType == "" {
		a.AdapterType = inference.AdapterLoRA
	}
	a.CreatedBy = in.CreatedBy
	a.UpdatedBy = in.CreatedBy
	if err := s.repo.CreateAdapter(ctx, a); err != nil {
		return nil, apperr.Internal("create adapter", err)
	}
	return a, nil
}

func (s *Service) GetAdapter(ctx context.Context, id int64) (*inference.ModelAdapter, error) {
	a, err := s.repo.GetAdapterByID(ctx, id)
	if err != nil {
		if errors.Is(err, inference.ErrAdapterNotFound) {
			return nil, apperr.NotFound("adapter", fmt.Sprint(id))
		}
		return nil, apperr.Internal("get adapter", err)
	}
	return a, nil
}

func (s *Service) ListAdapters(ctx context.Context, baseModelVersionID int64) ([]*inference.ModelAdapter, error) {
	return s.repo.ListAdapters(ctx, baseModelVersionID)
}

func (s *Service) DeleteAdapter(ctx context.Context, id, actorID int64) error {
	if err := s.repo.DeleteAdapter(ctx, id, actorID); err != nil {
		if errors.Is(err, inference.ErrAdapterNotFound) {
			return apperr.NotFound("adapter", fmt.Sprint(id))
		}
		return apperr.Internal("delete adapter", err)
	}
	return nil
}

// --- inference service ---

type CreateServiceInput struct {
	WorkspaceID          int64
	Name                 string
	DisplayName          string
	Description          string
	ClusterID            int64
	Namespace            string
	BaseModelVersionID   int64
	AdapterIDs           []int64
	Framework            inference.Framework
	FrameworkConfig      map[string]any
	Replicas             int
	Resources            map[string]any
	GPUCount             int
	GPUType              string
	TensorParallelSize   int
	PipelineParallelSize int
	StorageSizeBytes     int64
	AutoscalingEnabled   bool
	HPAMinReplicas       int
	HPAMaxReplicas       int
	HPAMetrics           map[string]any
	AccessMode           inference.AccessMode
	Labels               map[string]any
	Metadata             map[string]any
	CreatedBy            int64
}

func (s *Service) CreateService(ctx context.Context, in CreateServiceInput) (*inference.InferenceService, error) {
	exist, err := s.repo.GetServiceByName(ctx, in.WorkspaceID, in.Name)
	if err != nil {
		return nil, apperr.Internal("check service name", err)
	}
	if exist != nil {
		return nil, apperr.Conflict("service name exists", inference.ErrServiceNameUsed)
	}
	if _, err := s.repo.GetModelVersionByID(ctx, in.BaseModelVersionID); err != nil {
		return nil, apperr.NotFound("model version", fmt.Sprint(in.BaseModelVersionID))
	}
	svc := &inference.InferenceService{
		WorkspaceID: in.WorkspaceID, Name: in.Name, DisplayName: in.DisplayName, Description: in.Description,
		ClusterID: in.ClusterID, Namespace: in.Namespace, WorkloadName: in.Name, ServiceName: in.Name,
		BaseModelVersionID: in.BaseModelVersionID, AdapterIDs: in.AdapterIDs, Framework: in.Framework,
		FrameworkConfig: in.FrameworkConfig, Replicas: defaultInt(in.Replicas, 1), Resources: in.Resources,
		GPUCount: defaultInt(in.GPUCount, 1), GPUType: in.GPUType, TensorParallelSize: defaultInt(in.TensorParallelSize, 1),
		PipelineParallelSize: defaultInt(in.PipelineParallelSize, 1), StorageSizeBytes: in.StorageSizeBytes,
		CurrentStatus: inference.SvcStopped, Readiness: inference.ReadinessUnknown,
		AutoscalingEnabled: in.AutoscalingEnabled, HPAMinReplicas: in.HPAMinReplicas, HPAMaxReplicas: in.HPAMaxReplicas,
		HPAMetrics: in.HPAMetrics, AccessMode: in.AccessMode, Labels: in.Labels, Metadata: in.Metadata,
	}
	if svc.Framework == "" {
		svc.Framework = inference.FrameworkVLLM
	}
	if svc.AccessMode == "" {
		svc.AccessMode = inference.AccessInternal
	}
	svc.CreatedBy = in.CreatedBy
	svc.UpdatedBy = in.CreatedBy

	// 统一为应用分组：自动创建对应的 application + group，并记录 group_id/application_id。
	if s.appSvc != nil {
		appID, err := s.appSvc.CreateTypedApplicationForInfra(ctx, application.AppTypeInference, in.Name, in.Name, in.WorkspaceID, in.CreatedBy)
		if err != nil {
			return nil, err
		}
		svc.ApplicationID = appID
		groupID, err := s.appSvc.CreateGroupForTypedAppForInfra(ctx, appID, in.Name, in.ClusterID, in.Namespace, application.AppTypeInference, in.CreatedBy)
		if err != nil {
			return nil, err
		}
		svc.GroupID = groupID
		// 对齐命名：WorkloadName = k8sName(app.Name, group.Name)。
		svc.WorkloadName = inferenceWorkloadName(in.Name, in.Name)
		svc.ServiceName = svc.WorkloadName
	}

	if err := s.repo.CreateService(ctx, svc); err != nil {
		return nil, apperr.Internal("create inference service", err)
	}
	return svc, nil
}

func (s *Service) GetService(ctx context.Context, id int64) (*inference.InferenceService, error) {
	svc, err := s.repo.GetServiceByID(ctx, id)
	if err != nil {
		if errors.Is(err, inference.ErrServiceNotFound) {
			return nil, apperr.NotFound("inference service", fmt.Sprint(id))
		}
		return nil, apperr.Internal("get inference service", err)
	}
	return svc, nil
}

func (s *Service) ListServices(ctx context.Context, q inference.ServiceQuery, page, size int) ([]*inference.InferenceService, int64, error) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 50
	}
	q.Offset = (page - 1) * size
	q.Limit = size
	return s.repo.ListServices(ctx, q)
}

func (s *Service) UpdateService(ctx context.Context, svc *inference.InferenceService) error {
	if err := s.repo.UpdateService(ctx, svc); err != nil {
		if errors.Is(err, inference.ErrServiceNotFound) {
			return apperr.NotFound("inference service", fmt.Sprint(svc.ID))
		}
		return apperr.Internal("update inference service", err)
	}
	return nil
}

func (s *Service) DeleteService(ctx context.Context, id, actorID int64) error {
	if err := s.repo.DeleteService(ctx, id, actorID); err != nil {
		if errors.Is(err, inference.ErrServiceNotFound) {
			return apperr.NotFound("inference service", fmt.Sprint(id))
		}
		return apperr.Internal("delete inference service", err)
	}
	return nil
}

// DeployInput 部署输入。
type DeployInput struct {
	ServiceID        int64
	ModelVersionID   int64
	AdapterIDs       []int64
	Strategy         inference.ReleaseStrategy
	Replicas         int
	StartedBy        int64
}

// Deploy 创建 release、渲染 K8s 并应用到集群。
func (s *Service) Deploy(ctx context.Context, in DeployInput) (*inference.InferenceRelease, error) {
	svc, err := s.repo.GetServiceByID(ctx, in.ServiceID)
	if err != nil {
		return nil, apperr.NotFound("inference service", fmt.Sprint(in.ServiceID))
	}
	mvID := in.ModelVersionID
	if mvID == 0 {
		mvID = svc.BaseModelVersionID
	}
	mv, err := s.repo.GetModelVersionByID(ctx, mvID)
	if err != nil {
		return nil, apperr.NotFound("model version", fmt.Sprint(mvID))
	}
	adapterIDs := in.AdapterIDs
	if adapterIDs == nil {
		adapterIDs = svc.AdapterIDs
	}
	adapters, err := s.loadAdapters(ctx, adapterIDs)
	if err != nil {
		return nil, err
	}
	releaseNum, err := s.repo.NextReleaseNumber(ctx, svc.ID)
	if err != nil {
		return nil, apperr.Internal("allocate release number", err)
	}
	replicas := in.Replicas
	if replicas == 0 {
		replicas = svc.Replicas
	}
	strategy := in.Strategy
	if strategy == "" {
		strategy = inference.RelStrategyRolling
	}
	rel := &inference.InferenceRelease{
		InferenceServiceID: svc.ID, GroupID: svc.GroupID, ReleaseNumber: releaseNum, PreviousReleaseID: svc.CurrentReleaseID,
		TargetModelVersionID: mvID, TargetAdapterIDs: adapterIDs, Strategy: strategy, Replicas: replicas,
		Status: inference.RelStatusRunning, ProgressPercent: 10, StartedBy: in.StartedBy, StartedAt: time.Now(),
	}
	rel.CreatedBy = in.StartedBy
	rel.UpdatedBy = in.StartedBy
	if err := s.repo.CreateRelease(ctx, rel); err != nil {
		return nil, apperr.Internal("create release", err)
	}
	svc.CurrentStatus = inference.SvcStarting
	svc.Readiness = inference.ReadinessNotReady
	svc.UpdatedBy = in.StartedBy
	_, _ = s.repo.UpdateServiceStatus(ctx, svc.ID, svc.CurrentStatus, svc.Readiness, rel.ID, svc.Audit.Version)
	go s.runDeploy(context.Background(), svc, rel, mv, adapters, in.StartedBy)
	return rel, nil
}

func (s *Service) runDeploy(ctx context.Context, svc *inference.InferenceService, rel *inference.InferenceRelease, mv *inference.ModelVersion, adapters []*inference.ModelAdapter, actorID int64) {
	started := rel.StartedAt
	// 加载 registry 以获取 cache_pvc 配置。
	model, err := s.repo.GetModelByID(ctx, mv.ModelID)
	if err != nil {
		s.failRelease(ctx, rel, svc, fmt.Sprintf("load model: %v", err), actorID, started)
		return
	}
	registry, err := s.repo.GetRegistryByID(ctx, model.RegistryID)
	if err != nil {
		s.failRelease(ctx, rel, svc, fmt.Sprintf("load registry: %v", err), actorID, started)
		return
	}
	rendered, err := infrender.Render(infrender.RenderInput{Service: svc, ModelVersion: mv, Adapters: adapters, Registry: registry})
	if err != nil {
		s.failRelease(ctx, rel, svc, fmt.Sprintf("render: %v", err), actorID, started)
		return
	}
	clientset, err := s.getClient(ctx, svc.ClusterID)
	if err != nil {
		s.failRelease(ctx, rel, svc, fmt.Sprintf("k8s client: %v", err), actorID, started)
		return
	}
	applier := infrender.NewApplier(clientset)
	if err := applier.Apply(ctx, rendered); err != nil {
		s.failRelease(ctx, rel, svc, fmt.Sprintf("apply: %v", err), actorID, started)
		return
	}
	now := time.Now()
	rel.Status = inference.RelStatusSucceeded
	rel.ProgressPercent = 100
	rel.FinishedAt = &now
	rel.DurationMs = now.Sub(started).Milliseconds()
	rel.UpdatedBy = actorID
	_ = s.repo.UpdateRelease(ctx, rel)
	_, _ = s.repo.UpdateServiceStatus(ctx, svc.ID, inference.SvcStarting, inference.ReadinessNotReady, rel.ID, svc.Audit.Version)
	// 异步轮询真实 Deployment readiness。
	go s.syncServiceReadiness(context.Background(), svc.ID, svc.ClusterID, svc.Namespace, svc.WorkloadName, rel.ID, 30*time.Minute)
}

// syncServiceReadiness 轮询 Deployment 直到 ReadyReplicas 达到期望副本，或超时。
func (s *Service) syncServiceReadiness(ctx context.Context, serviceID, clusterID int64, namespace, workloadName string, releaseID int64, timeout time.Duration) {
	if workloadName == "" {
		// 回查服务取 workloadName
		svc, err := s.repo.GetServiceByID(ctx, serviceID)
		if err != nil {
			return
		}
		workloadName = svc.WorkloadName
		if workloadName == "" {
			workloadName = svc.Name
		}
	}
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		if time.Now().After(deadline) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		clientset, err := s.getClient(ctx, clusterID)
		if err != nil {
			continue
		}
		dep, err := infrender.NewApplier(clientset).GetDeployment(ctx, namespace, workloadName)
		if err != nil {
			continue
		}
		svc, err := s.repo.GetServiceByID(ctx, serviceID)
		if err != nil {
			return
		}
		// 如果 release 已被新发布替换，停止轮询。
		if svc.CurrentReleaseID != releaseID {
			return
		}
		desired := int32(svc.Replicas)
		if desired == 0 {
			desired = 1
		}
		ready := dep.Status.ReadyReplicas
		updated := dep.Status.UpdatedReplicas
		available := dep.Status.AvailableReplicas
		// 检测失败：replicas 期望非零但 ReadyReplicas 长期为 0 且有未就绪 Pod。
		switch {
		case ready >= desired && updated >= desired:
			_, _ = s.repo.UpdateServiceStatus(ctx, serviceID, inference.SvcRunning, inference.ReadinessReady, releaseID, svc.Audit.Version)
			// 若启用外部访问，回填 ExternalEndpoint。
			if svc.AccessMode == inference.AccessExternal && svc.ExternalEndpoint == "" {
				if ep, _ := infrender.NewApplier(clientset).GetLoadBalancerIngress(ctx, svc.Namespace, workloadName+"-ext"); ep != "" {
					upd, _ := s.repo.GetServiceByID(ctx, serviceID)
					if upd != nil && upd.ExternalEndpoint == "" {
						upd.ExternalEndpoint = ep
						_ = s.repo.UpdateService(ctx, upd)
					}
				}
			}
			return
		case ready > 0:
			_, _ = s.repo.UpdateServiceStatus(ctx, serviceID, inference.SvcRunning, inference.ReadinessPartialReady, releaseID, svc.Audit.Version)
		case available == 0 && updated == 0:
			// 仍在滚动更新中
			_, _ = s.repo.UpdateServiceStatus(ctx, serviceID, inference.SvcStarting, inference.ReadinessNotReady, releaseID, svc.Audit.Version)
		}
	}
}

// DownloadModelVersionInput 下载模型版本权重输入。
type DownloadModelVersionInput struct {
	ModelVersionID int64
	ClusterID      int64
	Namespace      string
	StartedBy      int64
}

// DownloadModelVersion 触发一个 K8s Job 从 registry 拉取权重到 cache PVC。
func (s *Service) DownloadModelVersion(ctx context.Context, in DownloadModelVersionInput) error {
	mv, err := s.repo.GetModelVersionByID(ctx, in.ModelVersionID)
	if err != nil {
		return apperr.NotFound("model version", fmt.Sprint(in.ModelVersionID))
	}
	model, err := s.repo.GetModelByID(ctx, mv.ModelID)
	if err != nil {
		return apperr.Internal("load model", err)
	}
	reg, err := s.repo.GetRegistryByID(ctx, model.RegistryID)
	if err != nil {
		return apperr.Internal("load registry", err)
	}
	if reg.CachePVCName == "" {
		return apperr.Validation("registry has no cache_pvc_name configured", nil)
	}
	jobName := fmt.Sprintf("dl-mv-%d-%d", mv.ID, time.Now().Unix())
	job, err := infrender.RenderDownloadJob(infrender.DownloadJobInput{
		JobName:      jobName,
		Namespace:    in.Namespace,
		Registry:     reg,
		ModelVersion: mv,
		ModelName:    model.Name,
		ClusterID:    in.ClusterID,
	})
	if err != nil {
		return apperr.Internal("render download job", err)
	}
	clientset, err := s.getClient(ctx, in.ClusterID)
	if err != nil {
		return apperr.Internal("get k8s client", err)
	}
	if err := infrender.NewApplier(clientset).ApplyJob(ctx, job); err != nil {
		return apperr.Internal("apply download job", err)
	}
	_ = s.repo.UpdateModelVersionDownload(ctx, mv.ID, inference.DownloadDownloading, 5)
	// 异步追踪 Job 完成状态。
	go s.syncDownloadJob(context.Background(), in.ClusterID, in.Namespace, jobName, mv.ID)
	return nil
}

// syncDownloadJob 轮询下载 Job 状态并更新 ModelVersion.download_status/progress。
func (s *Service) syncDownloadJob(ctx context.Context, clusterID int64, namespace, jobName string, modelVersionID int64) {
	deadline := time.Now().Add(2 * time.Hour)
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		if time.Now().After(deadline) {
			_ = s.repo.UpdateModelVersionDownload(ctx, modelVersionID, inference.DownloadFailed, 0)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		clientset, err := s.getClient(ctx, clusterID)
		if err != nil {
			continue
		}
		job, err := infrender.NewApplier(clientset).GetJob(ctx, namespace, jobName)
		if err != nil {
			continue
		}
		if job.Status.Succeeded > 0 {
			_ = s.repo.UpdateModelVersionDownload(ctx, modelVersionID, inference.DownloadReady, 100)
			return
		}
		if job.Status.Failed > 0 {
			_ = s.repo.UpdateModelVersionDownload(ctx, modelVersionID, inference.DownloadFailed, 0)
			return
		}
		// 估算进度（active → 50%）
		progress := 50
		if job.Status.Active > 0 {
			progress = 50
		} else if job.Status.StartTime != nil {
			progress = 10
		}
		_ = s.repo.UpdateModelVersionDownload(ctx, modelVersionID, inference.DownloadDownloading, progress)
	}
}

func (s *Service) failRelease(ctx context.Context, rel *inference.InferenceRelease, svc *inference.InferenceService, reason string, actorID int64, started time.Time) {
	now := time.Now()
	rel.Status = inference.RelStatusFailed
	rel.FailureReason = reason
	rel.FinishedAt = &now
	rel.DurationMs = now.Sub(started).Milliseconds()
	rel.UpdatedBy = actorID
	_ = s.repo.UpdateRelease(ctx, rel)
	_, _ = s.repo.UpdateServiceStatus(ctx, svc.ID, inference.SvcFailed, inference.ReadinessNotReady, rel.ID, svc.Audit.Version)
}

// Scale 调整推理服务副本数。
func (s *Service) Scale(ctx context.Context, serviceID int64, replicas int, actorID int64) (*inference.InferenceService, error) {
	svc, err := s.repo.GetServiceByID(ctx, serviceID)
	if err != nil {
		return nil, apperr.NotFound("inference service", fmt.Sprint(serviceID))
	}
	if replicas <= 0 {
		return nil, apperr.Validation("replicas must be positive", nil)
	}
	svc.Replicas = replicas
	svc.UpdatedBy = actorID
	if err := s.repo.UpdateService(ctx, svc); err != nil {
		return nil, apperr.Internal("update service replicas", err)
	}
	workloadName := svc.WorkloadName
	if workloadName == "" {
		workloadName = svc.Name
	}
	clientset, err := s.getClient(ctx, svc.ClusterID)
	if err != nil {
		return nil, apperr.Internal("get k8s client", err)
	}
	if err := infrender.NewApplier(clientset).ScaleDeployment(ctx, svc.Namespace, workloadName, int32(replicas)); err != nil {
		return nil, apperr.Internal("scale deployment", err)
	}
	return svc, nil
}

// Rollback 回滚到上一成功 release 并重新部署。
func (s *Service) Rollback(ctx context.Context, serviceID, actorID int64) (*inference.InferenceRelease, error) {
	svc, err := s.repo.GetServiceByID(ctx, serviceID)
	if err != nil {
		return nil, apperr.NotFound("inference service", fmt.Sprint(serviceID))
	}
	if svc.CurrentReleaseID == 0 {
		return nil, apperr.BusinessRule("no current release to rollback from", nil)
	}
	curr, err := s.repo.GetReleaseByID(ctx, svc.CurrentReleaseID)
	if err != nil {
		return nil, apperr.Internal("get current release", err)
	}
	if curr.PreviousReleaseID == 0 {
		return nil, apperr.BusinessRule("no previous release", nil)
	}
	prev, err := s.repo.GetReleaseByID(ctx, curr.PreviousReleaseID)
	if err != nil {
		return nil, apperr.Internal("get previous release", err)
	}
	return s.Deploy(ctx, DeployInput{
		ServiceID: serviceID, ModelVersionID: prev.TargetModelVersionID, AdapterIDs: prev.TargetAdapterIDs,
		Strategy: inference.RelStrategyRolling, Replicas: prev.Replicas, StartedBy: actorID,
	})
}

// --- release query ---

func (s *Service) GetRelease(ctx context.Context, id int64) (*inference.InferenceRelease, error) {
	rel, err := s.repo.GetReleaseByID(ctx, id)
	if err != nil {
		if errors.Is(err, inference.ErrReleaseNotFound) {
			return nil, apperr.NotFound("inference release", fmt.Sprint(id))
		}
		return nil, apperr.Internal("get release", err)
	}
	return rel, nil
}

func (s *Service) ListReleases(ctx context.Context, q inference.ReleaseQuery, page, size int) ([]*inference.InferenceRelease, int64, error) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 50
	}
	q.Offset = (page - 1) * size
	q.Limit = size
	return s.repo.ListReleases(ctx, q)
}

// --- api key ---

type CreateAPIKeyInput struct {
	InferenceServiceID int64
	Name               string
	DailyTokenQuota    int64
	RateLimitPerMin    int
	ExpiresAt          *time.Time
	CreatedBy          int64
}

type CreateAPIKeyResult struct {
	Key    *inference.InferenceAPIKey
	Secret string
}

func (s *Service) CreateAPIKey(ctx context.Context, in CreateAPIKeyInput) (*CreateAPIKeyResult, error) {
	if _, err := s.repo.GetServiceByID(ctx, in.InferenceServiceID); err != nil {
		return nil, apperr.NotFound("inference service", fmt.Sprint(in.InferenceServiceID))
	}
	secret, prefix, err := generateAPIKey()
	if err != nil {
		return nil, apperr.Internal("generate api key", err)
	}
	k := &inference.InferenceAPIKey{
		InferenceServiceID: in.InferenceServiceID, Name: in.Name, KeyPrefix: prefix,
		KeyHash: security.HashTokenSHA256(secret), DailyTokenQuota: in.DailyTokenQuota,
		RateLimitPerMin: in.RateLimitPerMin, ExpiresAt: in.ExpiresAt, Status: inference.APIKeyActive,
	}
	k.CreatedBy = in.CreatedBy
	k.UpdatedBy = in.CreatedBy
	if err := s.repo.CreateAPIKey(ctx, k); err != nil {
		return nil, apperr.Internal("create api key", err)
	}
	return &CreateAPIKeyResult{Key: k, Secret: secret}, nil
}

func (s *Service) ListAPIKeys(ctx context.Context, serviceID int64) ([]*inference.InferenceAPIKey, error) {
	return s.repo.ListAPIKeys(ctx, serviceID)
}

func (s *Service) RevokeAPIKey(ctx context.Context, id, actorID int64) error {
	if err := s.repo.RevokeAPIKey(ctx, id, actorID); err != nil {
		if errors.Is(err, inference.ErrAPIKeyNotFound) {
			return apperr.NotFound("api key", fmt.Sprint(id))
		}
		return apperr.Internal("revoke api key", err)
	}
	return nil
}

func (s *Service) TouchAPIKeyLastUsed(ctx context.Context, id int64) {
	_ = s.repo.UpdateAPIKeyLastUsed(ctx, id, time.Now())
}

func (s *Service) ValidateAPIKey(ctx context.Context, secret string) (*inference.InferenceAPIKey, error) {
	hash := security.HashTokenSHA256(secret)
	k, err := s.repo.GetAPIKeyByHash(ctx, hash)
	if err != nil {
		if errors.Is(err, inference.ErrAPIKeyNotFound) {
			return nil, apperr.Unauthorized("invalid api key", inference.ErrAPIKeyNotFound)
		}
		return nil, apperr.Internal("lookup api key", err)
	}
	if k.Status == inference.APIKeyRevoked {
		return nil, apperr.Unauthorized("api key revoked", inference.ErrAPIKeyRevoked)
	}
	if k.ExpiresAt != nil && k.ExpiresAt.Before(time.Now()) {
		return nil, apperr.Unauthorized("api key expired", nil)
	}
	return k, nil
}

// --- route ---

type CreateRouteInput struct {
	WorkspaceID       int64
	Name              string
	Description       string
	Strategy          inference.RouteStrategy
	Rules             map[string]any
	DefaultServiceID  int64
	CreatedBy         int64
}

func (s *Service) CreateRoute(ctx context.Context, in CreateRouteInput) (*inference.InferenceRoute, error) {
	rt := &inference.InferenceRoute{
		WorkspaceID: in.WorkspaceID, Name: in.Name, Description: in.Description,
		Strategy: in.Strategy, Rules: in.Rules, DefaultServiceID: in.DefaultServiceID, Status: "active",
	}
	if rt.Strategy == "" {
		rt.Strategy = inference.RouteWeighted
	}
	rt.CreatedBy = in.CreatedBy
	rt.UpdatedBy = in.CreatedBy
	if err := s.repo.CreateRoute(ctx, rt); err != nil {
		return nil, apperr.Internal("create route", err)
	}
	return rt, nil
}

func (s *Service) GetRoute(ctx context.Context, id int64) (*inference.InferenceRoute, error) {
	rt, err := s.repo.GetRouteByID(ctx, id)
	if err != nil {
		if errors.Is(err, inference.ErrRouteNotFound) {
			return nil, apperr.NotFound("inference route", fmt.Sprint(id))
		}
		return nil, apperr.Internal("get route", err)
	}
	return rt, nil
}

func (s *Service) ListRoutes(ctx context.Context, workspaceID int64) ([]*inference.InferenceRoute, error) {
	return s.repo.ListRoutes(ctx, workspaceID)
}

func (s *Service) UpdateRoute(ctx context.Context, rt *inference.InferenceRoute) error {
	if err := s.repo.UpdateRoute(ctx, rt); err != nil {
		if errors.Is(err, inference.ErrRouteNotFound) {
			return apperr.NotFound("inference route", fmt.Sprint(rt.ID))
		}
		return apperr.Internal("update route", err)
	}
	return nil
}

func (s *Service) DeleteRoute(ctx context.Context, id, actorID int64) error {
	if err := s.repo.DeleteRoute(ctx, id, actorID); err != nil {
		if errors.Is(err, inference.ErrRouteNotFound) {
			return apperr.NotFound("inference route", fmt.Sprint(id))
		}
		return apperr.Internal("delete route", err)
	}
	return nil
}

// --- usage ---

func (s *Service) RecordUsage(ctx context.Context, u *inference.InferenceUsage) error {
	if u.UUID == uuid.Nil {
		u.UUID = uuid.New()
	}
	if u.CreatedAt.IsZero() {
		u.CreatedAt = time.Now()
	}
	if err := s.repo.AppendUsage(ctx, u); err != nil {
		return apperr.Internal("record usage", err)
	}
	if s.producer != nil && s.producer.Enabled() {
		_ = s.producer.Publish(ctx, s.brokers, s.topicKey, s.topicName, fmt.Sprintf("usage-%d", u.ID),
			kafka.NewEvent("inference.usage.recorded", "apiserver", map[string]any{
				"service_id": u.InferenceServiceID, "api_key_id": u.APIKeyID,
				"prompt_tokens": u.PromptTokens, "completion_tokens": u.CompletionTokens, "total_tokens": u.TotalTokens,
			}))
	}
	return nil
}

func (s *Service) ListUsage(ctx context.Context, q inference.UsageQuery, page, size int) ([]*inference.InferenceUsage, int64, error) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 50
	}
	q.Offset = (page - 1) * size
	q.Limit = size
	return s.repo.ListUsage(ctx, q)
}

func (s *Service) SummarizeUsage(ctx context.Context, q inference.UsageQuery) (*inference.UsageSummary, error) {
	summary, err := s.repo.SummarizeUsage(ctx, q)
	if err != nil {
		return nil, apperr.Internal("summarize usage", err)
	}
	return summary, nil
}

// InternalEndpoint 返回推理服务集群内访问地址。
func (s *Service) InternalEndpoint(svc *inference.InferenceService) string {
	if svc.AccessMode == inference.AccessExternal && svc.ExternalEndpoint != "" {
		return svc.ExternalEndpoint
	}
	serviceName := svc.ServiceName
	if serviceName == "" {
		serviceName = svc.Name
	}
	port := 8000
	switch svc.Framework {
	case inference.FrameworkTGI:
		port = 80
	case inference.FrameworkOllama:
		port = 11434
	}
	return fmt.Sprintf("http://%s.%s.svc.cluster.local:%d", serviceName, svc.Namespace, port)
}

// --- helpers ---

func (s *Service) getClient(ctx context.Context, clusterID int64) (kubernetes.Interface, error) {
	kube, err := s.cluster.GetDecryptedKubeconfig(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	insecure, err := s.cluster.GetClusterInsecureSkipTLS(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	entry, err := s.pool.GetOrCreate(clusterID, kube, insecure)
	if err != nil {
		return nil, err
	}
	return entry.Clientset, nil
}

func (s *Service) loadAdapters(ctx context.Context, ids []int64) ([]*inference.ModelAdapter, error) {
	out := make([]*inference.ModelAdapter, 0, len(ids))
	for _, id := range ids {
		a, err := s.repo.GetAdapterByID(ctx, id)
		if err != nil {
			return nil, apperr.NotFound("adapter", fmt.Sprint(id))
		}
		out = append(out, a)
	}
	return out, nil
}

func generateAPIKey() (secret, prefix string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	secret = "voi_" + base64.RawURLEncoding.EncodeToString(b)
	if len(secret) >= 12 {
		prefix = secret[:12]
	} else {
		prefix = secret
	}
	return secret, prefix, nil
}

func defaultInt(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}

// inferenceWorkloadName 推理服务统一命名：与 applicationapp.k8sName 一致。
// 此处复刻一份避免跨包循环依赖；保持与 k8sName("App", "group") 同语义。
func inferenceWorkloadName(appName, groupName string) string {
	// 委托到 application 包导出的同名函数（已在 applicationapp 内实现，此处复用其语义）。
	// 直接复刻实现以避免循环依赖。
	return appK8sName(appName, groupName)
}

// appK8sName 生成 DNS-1035 兼容资源名：小写字母/数字/-，字母开头。
func appK8sName(parts ...string) string {
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
	for strings.Contains(out, "--") {
		out = strings.ReplaceAll(out, "--", "-")
	}
	out = strings.Trim(out, "-")
	if len(out) > 63 {
		out = out[:63]
		out = strings.Trim(out, "-")
	}
	if out == "" || !(out[0] >= 'a' && out[0] <= 'z') {
		out = "a" + out
	}
	return out
}

// 引用 application 包以避免未使用导入（AppTypeInference 等常量在此使用）。
var _ = application.AppTypeInference
