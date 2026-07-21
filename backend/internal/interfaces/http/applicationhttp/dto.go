package applicationhttp

import (
	"github.com/vortexops/vortexops/internal/domain/application"
)

// --- 应用 DTO ---

type applicationDTO struct {
	ID                   int64             `json:"id"`
	UUID                 string            `json:"uuid"`
	WorkspaceID          int64             `json:"workspace_id"`
	Name                 string            `json:"name"`
	Code                 string            `json:"code"`
	DisplayName          string            `json:"display_name"`
	Description          string            `json:"description"`
	Icon                 string            `json:"icon"`
	DefaultGitSourceID   int64             `json:"default_git_source_id"`
	DefaultRegistryID    int64             `json:"default_registry_id"`
	Lifecycle            string            `json:"lifecycle"`
	OwnerID              int64             `json:"owner_id"`
	// 应用配置项：存于 metadata，输出时透传到顶层，便于前端直接消费。
	AppType     string `json:"app_type"`
	WorkloadType string `json:"workload_type"`
	GitURL      string `json:"git_url"`
	DefaultBranch string `json:"default_branch"`
	Language    string `json:"language"`
	// 应用探活配置：存于 metadata["probe"]，输出时透传到顶层。
	Probe       *application.ProbeConfig `json:"probe,omitempty"`
	GroupCount  int   `json:"group_count"`
	MemberCount int   `json:"member_count"`
	Labels               map[string]string `json:"labels"`
	Metadata             map[string]any    `json:"metadata"`
	Version              int               `json:"version"`
	CreatedAt            string            `json:"created_at"`
	UpdatedAt            string            `json:"updated_at"`
}

func toApplicationDTO(a *application.Application) *applicationDTO {
	if a == nil {
		return nil
	}
	dto := &applicationDTO{
		ID: a.ID, UUID: a.UUID.String(), WorkspaceID: a.WorkspaceID, Name: a.Name,
		Code: a.Code,
		DisplayName: a.DisplayName, Description: a.Description, Icon: a.Icon,
		DefaultGitSourceID: a.DefaultGitSourceID, DefaultRegistryID: a.DefaultRegistryID,
		Lifecycle: string(a.Lifecycle), OwnerID: a.OwnerID, Labels: a.Labels, Metadata: a.Metadata,
		Version: a.Version,
		CreatedAt: a.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt: a.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
	// 从 metadata 透传应用配置项到顶层。
	if a.Metadata != nil {
		if v, ok := a.Metadata["app_type"].(string); ok {
			dto.AppType = v
		}
		if v, ok := a.Metadata["workload_type"].(string); ok {
			dto.WorkloadType = v
		}
		if v, ok := a.Metadata["git_url"].(string); ok {
			dto.GitURL = v
		}
		if v, ok := a.Metadata["default_branch"].(string); ok {
			dto.DefaultBranch = v
		}
		if v, ok := a.Metadata["language"].(string); ok {
			dto.Language = v
		}
	}
	dto.Probe = application.ProbeFromApplication(a)
	return dto
}

func toApplicationDTOs(items []*application.Application) []applicationDTO {
	out := make([]applicationDTO, 0, len(items))
	for _, a := range items {
		out = append(out, *toApplicationDTO(a))
	}
	return out
}

type appMemberDTO struct {
	ID            int64  `json:"id"`
	ApplicationID int64  `json:"application_id"`
	UserID        int64  `json:"user_id"`
	RoleID        int64  `json:"role_id"`
	UserName      string `json:"username"`
	DisplayName   string `json:"display_name"`
	Email         string `json:"email"`
	RoleName      string `json:"role_name"`
	InvitedBy     int64  `json:"invited_by"`
	JoinedAt      string `json:"joined_at"`
	Status        string `json:"status"`
	Version       int    `json:"version"`
}

func toAppMemberDTO(m *application.Member) *appMemberDTO {
	if m == nil {
		return nil
	}
	return &appMemberDTO{
		ID: m.ID, ApplicationID: m.ApplicationID, UserID: m.UserID, RoleID: m.RoleID,
		UserName: m.UserName, DisplayName: m.DisplayName, Email: m.Email, RoleName: m.RoleName,
		InvitedBy: m.InvitedBy, JoinedAt: m.JoinedAt.Format("2006-01-02T15:04:05Z07:00"),
		Status: m.Status, Version: m.Version,
	}
}

func toAppMemberDTOs(items []*application.Member) []appMemberDTO {
	out := make([]appMemberDTO, 0, len(items))
	for _, m := range items {
		out = append(out, *toAppMemberDTO(m))
	}
	return out
}

// --- 分组 DTO ---

type groupDTO struct {
	ID                      int64                  `json:"id"`
	UUID                    string                 `json:"uuid"`
	ApplicationID           int64                  `json:"application_id"`
	Name                    string                 `json:"name"`
	DisplayName             string                 `json:"display_name"`
	Description             string                 `json:"description"`
	AppType                 string                 `json:"app_type"`
	Environment             string                 `json:"environment"`
	ClusterID               int64                  `json:"cluster_id"`
	Namespace               string                 `json:"namespace"`
	DeploymentName          string                 `json:"deployment_name"`
	ServiceName             string                 `json:"service_name"`
	Replicas                int                    `json:"replicas"`
	CurrentImageID          int64                  `json:"current_image_id"`
	CurrentConfigID         int64                  `json:"current_config_id"`
	CurrentReleaseID        int64                  `json:"current_release_id"`
	CandidateImageID        int64                  `json:"candidate_image_id"`
	CandidateReleaseID      int64                  `json:"candidate_release_id"`
	CandidateReplicas       int                    `json:"candidate_replicas"`
	Resources               resourcesDTO           `json:"resources"`
	Storage                 storageDTO             `json:"storage"`
	MeshEnabled             bool                   `json:"mesh_enabled"`
	Scheduling              schedulingDTO          `json:"scheduling"`
	Workload                workloadDTO            `json:"workload"`
	HealthCheck             *healthCheckDTO        `json:"health_check,omitempty"`
	Autoscaling             *autoscalingDTO        `json:"autoscaling,omitempty"`
	ReleaseRequiresApproval bool                   `json:"release_requires_approval"`
	Labels                  map[string]string      `json:"labels"`
	Metadata                map[string]any         `json:"metadata"`
	Version                 int                    `json:"version"`
	CreatedAt               string                 `json:"created_at"`
	UpdatedAt               string                 `json:"updated_at"`
}

func toGroupDTO(g *application.Group) *groupDTO {
	if g == nil {
		return nil
	}
	dto := &groupDTO{
		ID: g.ID, UUID: g.UUID.String(), ApplicationID: g.ApplicationID, Name: g.Name,
		DisplayName: g.DisplayName, Description: g.Description, AppType: g.AppType, Environment: string(g.Environment),
		ClusterID: g.ClusterID, Namespace: g.Namespace, DeploymentName: g.DeploymentName,
		ServiceName: g.ServiceName, Replicas: g.Replicas,
		CurrentImageID: g.CurrentImageID, CurrentConfigID: g.CurrentConfigID, CurrentReleaseID: g.CurrentReleaseID,
		CandidateImageID: g.CandidateImageID, CandidateReleaseID: g.CandidateReleaseID, CandidateReplicas: g.CandidateReplicas,
		Resources:               toResourcesDTO(g.Resources),
		Storage:                 toStorageDTO(g.Storage),
		MeshEnabled:             g.MeshEnabled,
		Scheduling:              toSchedulingDTO(g.Scheduling),
		Workload:                toWorkloadDTO(g.Workload),
		ReleaseRequiresApproval: g.ReleaseRequiresApproval,
		Labels: g.Labels, Metadata: g.Metadata, Version: g.Version,
		CreatedAt: g.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt: g.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
	if g.HealthCheck != nil {
		dto.HealthCheck = toHealthCheckDTO(g.HealthCheck)
	}
	if g.Autoscaling != nil {
		dto.Autoscaling = toAutoscalingDTO(g.Autoscaling)
	}
	return dto
}

func toGroupDTOs(items []*application.Group) []groupDTO {
	out := make([]groupDTO, 0, len(items))
	for _, g := range items {
		out = append(out, *toGroupDTO(g))
	}
	return out
}

type resourcesDTO struct {
	CPUm                int    `json:"cpu_m"`
	CPULimitM           int    `json:"cpu_limit_m,omitempty"`
	MemoryBytes         int64  `json:"memory_bytes"`
	MemoryLimitBytes    int64  `json:"memory_limit_bytes,omitempty"`
	GPU                 int    `json:"gpu,omitempty"`
	GPUType             string `json:"gpu_type,omitempty"`
	GPUResourceName     string `json:"gpu_resource_name,omitempty"`
}

func toResourcesDTO(r application.Resources) resourcesDTO {
	return resourcesDTO{
		CPUm: r.CPUm, CPULimitM: r.CPULimitM, MemoryBytes: r.MemoryBytes,
		MemoryLimitBytes: r.MemoryLimitBytes, GPU: r.GPU, GPUType: r.GPUType, GPUResourceName: r.GPUResourceName,
	}
}

type storageDTO struct {
	StorageSizeBytes             int64  `json:"storage_size_bytes,omitempty"`
	StorageClass                 string `json:"storage_class,omitempty"`
	EphemeralStorageRequestBytes int64  `json:"ephemeral_storage_request_bytes,omitempty"`
	EphemeralStorageLimitBytes   int64  `json:"ephemeral_storage_limit_bytes,omitempty"`
	ResourceTemplateID           int64  `json:"resource_template_id,omitempty"`
}

func toStorageDTO(s application.Storage) storageDTO {
	return storageDTO{
		StorageSizeBytes: s.StorageSizeBytes, StorageClass: s.StorageClass,
		EphemeralStorageRequestBytes: s.EphemeralStorageRequestBytes,
		EphemeralStorageLimitBytes:   s.EphemeralStorageLimitBytes,
		ResourceTemplateID:           s.ResourceTemplateID,
	}
}

type schedulingDTO struct {
	NodeSelector  map[string]string `json:"node_selector,omitempty"`
	NodeAffinity  map[string]any    `json:"node_affinity,omitempty"`
	Tolerations   []map[string]any  `json:"tolerations,omitempty"`
	PriorityClass string            `json:"priority_class,omitempty"`
}

func toSchedulingDTO(s application.Scheduling) schedulingDTO {
	return schedulingDTO{
		NodeSelector: s.NodeSelector, NodeAffinity: s.NodeAffinity,
		Tolerations: s.Tolerations, PriorityClass: s.PriorityClass,
	}
}

type workloadDTO struct {
	Type           string           `json:"type"`
	CronSchedule   string           `json:"cron_schedule,omitempty"`
	JobPolicy      map[string]any   `json:"job_policy,omitempty"`
	Strategy       string           `json:"strategy"`
	MaxSurge       string           `json:"max_surge"`
	MaxUnavailable string           `json:"max_unavailable"`
}

func toWorkloadDTO(w application.Workload) workloadDTO {
	return workloadDTO{
		Type: string(w.Type), CronSchedule: w.CronSchedule, JobPolicy: w.JobPolicy,
		Strategy: string(w.Strategy), MaxSurge: w.MaxSurge, MaxUnavailable: w.MaxUnavailable,
	}
}

type healthCheckDTO struct {
	LivenessProbe  map[string]any `json:"liveness_probe,omitempty"`
	ReadinessProbe map[string]any `json:"readiness_probe,omitempty"`
	StartupProbe   map[string]any `json:"startup_probe,omitempty"`
}

func toHealthCheckDTO(h *application.HealthCheck) *healthCheckDTO {
	if h == nil {
		return nil
	}
	return &healthCheckDTO{
		LivenessProbe: h.LivenessProbe, ReadinessProbe: h.ReadinessProbe, StartupProbe: h.StartupProbe,
	}
}

type autoscalingDTO struct {
	Enabled     bool              `json:"enabled"`
	MinReplicas int               `json:"min_replicas,omitempty"`
	MaxReplicas int               `json:"max_replicas,omitempty"`
	Metrics     []map[string]any  `json:"metrics,omitempty"`
	Behavior    map[string]any    `json:"behavior,omitempty"`
}

func toAutoscalingDTO(a *application.Autoscaling) *autoscalingDTO {
	if a == nil {
		return nil
	}
	return &autoscalingDTO{
		Enabled: a.Enabled, MinReplicas: a.MinReplicas, MaxReplicas: a.MaxReplicas,
		Metrics: a.Metrics, Behavior: a.Behavior,
	}
}
