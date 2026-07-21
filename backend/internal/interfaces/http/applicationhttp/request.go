package applicationhttp

import "github.com/vortexops/vortexops/internal/domain/application"

// createGroupReq 创建分组请求体。
type createGroupReq struct {
	Name                   string            `json:"name"`
	DisplayName            string            `json:"display_name"`
	Description            string            `json:"description"`
	Environment            string            `json:"environment"`
	ClusterID              int64             `json:"cluster_id"`
	Namespace              string            `json:"namespace"`
	Replicas               int               `json:"replicas"`
	Resources              resourcesReq      `json:"resources"`
	Storage                storageReq        `json:"storage"`
	MeshEnabled            bool              `json:"mesh_enabled"`
	Scheduling             schedulingReq     `json:"scheduling"`
	Workload               workloadReq       `json:"workload"`
	HealthCheck            healthCheckReq    `json:"health_check"`
	Autoscaling            autoscalingReq    `json:"autoscaling"`
	ReleaseRequiresApproval bool             `json:"release_requires_approval"`
	Labels                 map[string]string `json:"labels"`
	Metadata               map[string]any    `json:"metadata"`
}

// updateGroupReq 更新分组请求体。所有字段为指针以区分"未提供"与"零值"。
type updateGroupReq struct {
	DisplayName            *string            `json:"display_name"`
	Description            *string            `json:"description"`
	Replicas               *int               `json:"replicas"`
	Resources              *resourcesReq      `json:"resources"`
	Storage                *storageReq        `json:"storage"`
	MeshEnabled            *bool              `json:"mesh_enabled"`
	Scheduling             *schedulingReq     `json:"scheduling"`
	Workload               *workloadReq       `json:"workload"`
	HealthCheck            *healthCheckReq    `json:"health_check"`
	Autoscaling            *autoscalingReq    `json:"autoscaling"`
	ReleaseRequiresApproval *bool             `json:"release_requires_approval"`
	Labels                 *map[string]string `json:"labels"`
	Metadata               *map[string]any    `json:"metadata"`
	// ClusterID 仅用于校验：创建后不可更换；若传入不同值会返回 422。
	ClusterID              *int64             `json:"cluster_id"`
	Version                int                `json:"version"`
}

type resourcesReq struct {
	CPUm                int    `json:"cpu_m"`
	CPULimitM           int    `json:"cpu_limit_m"`
	MemoryBytes         int64  `json:"memory_bytes"`
	MemoryLimitBytes    int64  `json:"memory_limit_bytes"`
	GPU                 int    `json:"gpu"`
	GPUType             string `json:"gpu_type"`
	GPUResourceName     string `json:"gpu_resource_name"`
}

func (r resourcesReq) toDomain() application.Resources {
	return application.Resources{
		CPUm: r.CPUm, CPULimitM: r.CPULimitM, MemoryBytes: r.MemoryBytes,
		MemoryLimitBytes: r.MemoryLimitBytes, GPU: r.GPU, GPUType: r.GPUType, GPUResourceName: r.GPUResourceName,
	}
}

func (r *resourcesReq) toDomainPtr() *application.Resources {
	if r == nil {
		return nil
	}
	v := r.toDomain()
	return &v
}

type storageReq struct {
	StorageSizeBytes             int64  `json:"storage_size_bytes"`
	StorageClass                 string `json:"storage_class"`
	EphemeralStorageRequestBytes int64  `json:"ephemeral_storage_request_bytes"`
	EphemeralStorageLimitBytes   int64  `json:"ephemeral_storage_limit_bytes"`
	ResourceTemplateID           int64  `json:"resource_template_id"`
}

func (s storageReq) toDomain() application.Storage {
	return application.Storage{
		StorageSizeBytes: s.StorageSizeBytes, StorageClass: s.StorageClass,
		EphemeralStorageRequestBytes: s.EphemeralStorageRequestBytes,
		EphemeralStorageLimitBytes:   s.EphemeralStorageLimitBytes,
		ResourceTemplateID:           s.ResourceTemplateID,
	}
}

func (s *storageReq) toDomainPtr() *application.Storage {
	if s == nil {
		return nil
	}
	v := s.toDomain()
	return &v
}

type schedulingReq struct {
	NodeSelector  map[string]string `json:"node_selector"`
	NodeAffinity  map[string]any    `json:"node_affinity"`
	Tolerations   []map[string]any  `json:"tolerations"`
	PriorityClass string            `json:"priority_class"`
}

func (s schedulingReq) toDomain() application.Scheduling {
	return application.Scheduling{
		NodeSelector: s.NodeSelector, NodeAffinity: s.NodeAffinity,
		Tolerations: s.Tolerations, PriorityClass: s.PriorityClass,
	}
}

func (s *schedulingReq) toDomainPtr() *application.Scheduling {
	if s == nil {
		return nil
	}
	v := s.toDomain()
	return &v
}

type workloadReq struct {
	Type           string         `json:"type"`
	CronSchedule   string         `json:"cron_schedule"`
	JobPolicy      map[string]any `json:"job_policy"`
	Strategy       string         `json:"strategy"`
	MaxSurge       string         `json:"max_surge"`
	MaxUnavailable string         `json:"max_unavailable"`
}

func (w workloadReq) toDomain() application.Workload {
	return application.Workload{
		Type: application.WorkloadType(w.Type), CronSchedule: w.CronSchedule, JobPolicy: w.JobPolicy,
		Strategy: application.Strategy(w.Strategy), MaxSurge: w.MaxSurge, MaxUnavailable: w.MaxUnavailable,
	}
}

func (w *workloadReq) toDomainPtr() *application.Workload {
	if w == nil {
		return nil
	}
	v := w.toDomain()
	return &v
}

type healthCheckReq struct {
	LivenessProbe  map[string]any `json:"liveness_probe"`
	ReadinessProbe map[string]any `json:"readiness_probe"`
	StartupProbe   map[string]any `json:"startup_probe"`
}

func (h healthCheckReq) toDomain() *application.HealthCheck {
	if h.LivenessProbe == nil && h.ReadinessProbe == nil && h.StartupProbe == nil {
		return nil
	}
	return &application.HealthCheck{
		LivenessProbe: h.LivenessProbe, ReadinessProbe: h.ReadinessProbe, StartupProbe: h.StartupProbe,
	}
}

func (h *healthCheckReq) toDomainPtr() *application.HealthCheck {
	if h == nil {
		return nil
	}
	return healthCheckReq(*h).toDomain()
}

type autoscalingReq struct {
	Enabled     bool              `json:"enabled"`
	MinReplicas int               `json:"min_replicas"`
	MaxReplicas int               `json:"max_replicas"`
	Metrics     []map[string]any  `json:"metrics"`
	Behavior    map[string]any    `json:"behavior"`
}

func (a autoscalingReq) toDomain() *application.Autoscaling {
	if !a.Enabled {
		return &application.Autoscaling{Enabled: false}
	}
	return &application.Autoscaling{
		Enabled: true, MinReplicas: a.MinReplicas, MaxReplicas: a.MaxReplicas,
		Metrics: a.Metrics, Behavior: a.Behavior,
	}
}

func (a *autoscalingReq) toDomainPtr() *application.Autoscaling {
	if a == nil {
		return nil
	}
	return autoscalingReq(*a).toDomain()
}
