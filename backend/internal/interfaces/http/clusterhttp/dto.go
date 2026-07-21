package clusterhttp

import (
	"time"

	"github.com/vortexops/vortexops/internal/domain/cluster"
)

type clusterDTO struct {
	ID                     int64             `json:"id"`
	UUID                   string            `json:"uuid"`
	Name                   string            `json:"name"`
	DisplayName            string            `json:"display_name"`
	Description            string            `json:"description"`
	APIServer              string            `json:"api_server"`
	DefaultNamespacePrefix string            `json:"default_namespace_prefix"`
	InsecureSkipTLS        bool              `json:"insecure_skip_tls"`
	Region                 string            `json:"region"`
	Environment            string            `json:"environment"`
	K8sVersion             string            `json:"k8s_version"`
	NodeCount              int               `json:"node_count"`
	Status                 string            `json:"status"`
	LastCheckedAt          *time.Time        `json:"last_checked_at,omitempty"`
	LastError              string            `json:"last_error,omitempty"`
	AllocatableCPUM        int               `json:"allocatable_cpu_m"`
	AllocatableMemoryBytes int64             `json:"allocatable_memory_bytes"`
	AllocatableGPU         int               `json:"allocatable_gpu"`
	CapacitySyncedAt       *time.Time        `json:"capacity_synced_at,omitempty"`
	Labels                 map[string]string `json:"labels"`
	Metadata               map[string]any    `json:"metadata"`
	Version                int               `json:"version"`
	CreatedAt              string            `json:"created_at"`
	UpdatedAt              string            `json:"updated_at"`
}

func toClusterDTO(c *cluster.Cluster) *clusterDTO {
	if c == nil {
		return nil
	}
	return &clusterDTO{
		ID: c.ID, UUID: c.UUID.String(), Name: c.Name, DisplayName: c.DisplayName, Description: c.Description,
		APIServer: c.APIServer, DefaultNamespacePrefix: c.DefaultNamespacePrefix, InsecureSkipTLS: c.InsecureSkipTLS,
		Region: c.Region, Environment: c.Environment, K8sVersion: c.K8sVersion, NodeCount: c.NodeCount,
		Status: string(c.Status), LastCheckedAt: c.LastCheckedAt, LastError: c.LastError,
		AllocatableCPUM: c.AllocatableCPUM, AllocatableMemoryBytes: c.AllocatableMemoryBytes,
		AllocatableGPU: c.AllocatableGPU, CapacitySyncedAt: c.CapacitySyncedAt,
		Labels: c.Labels, Metadata: c.Metadata, Version: c.Version,
		CreatedAt: c.CreatedAt.Format(time.RFC3339), UpdatedAt: c.UpdatedAt.Format(time.RFC3339),
	}
}

func toClusterDTOs(items []*cluster.Cluster) []clusterDTO {
	out := make([]clusterDTO, 0, len(items))
	for _, c := range items {
		out = append(out, *toClusterDTO(c))
	}
	return out
}

type probeResultDTO struct {
	K8sVersion             string `json:"k8s_version"`
	NodeCount              int    `json:"node_count"`
	APIServer              string `json:"api_server"`
	AllocatableCPUM        int    `json:"allocatable_cpu_m"`
	AllocatableMemoryBytes int64  `json:"allocatable_memory_bytes"`
	AllocatableGPU         int    `json:"allocatable_gpu"`
}

type credentialDTO struct {
	ID            int64      `json:"id"`
	UUID          string     `json:"uuid"`
	Name          string     `json:"name"`
	Kind          string     `json:"kind"`
	Scope         string     `json:"scope"`
	ScopeID       int64      `json:"scope_id"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	LastRotatedAt *time.Time `json:"last_rotated_at,omitempty"`
	Version       int        `json:"version"`
	CreatedAt     string     `json:"created_at"`
}

func toCredentialDTO(c *cluster.Credential) *credentialDTO {
	if c == nil {
		return nil
	}
	return &credentialDTO{
		ID: c.ID, UUID: c.UUID.String(), Name: c.Name, Kind: string(c.Kind),
		Scope: string(c.Scope), ScopeID: c.ScopeID, ExpiresAt: c.ExpiresAt, LastRotatedAt: c.LastRotatedAt,
		Version: c.Version, CreatedAt: c.CreatedAt.Format(time.RFC3339),
	}
}

func toCredentialDTOs(items []*cluster.Credential) []credentialDTO {
	out := make([]credentialDTO, 0, len(items))
	for _, c := range items {
		out = append(out, *toCredentialDTO(c))
	}
	return out
}

type ipPoolDTO struct {
	ID             int64          `json:"id"`
	UUID           string         `json:"uuid"`
	ClusterID      int64          `json:"cluster_id"`
	Name           string         `json:"name"`
	CIDR           string         `json:"cidr"`
	Gateway        string         `json:"gateway"`
	Provider       string         `json:"provider"`
	TotalCount     int            `json:"total_count"`
	AllocatedCount int            `json:"allocated_count"`
	ReservedIPs    []string       `json:"reserved_ips,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	Version        int            `json:"version"`
	CreatedAt      string         `json:"created_at"`
}

func toIPPoolDTO(p *cluster.IPPool) *ipPoolDTO {
	if p == nil {
		return nil
	}
	return &ipPoolDTO{
		ID: p.ID, UUID: p.UUID.String(), ClusterID: p.ClusterID, Name: p.Name, CIDR: p.CIDR,
		Gateway: p.Gateway, Provider: string(p.Provider), TotalCount: p.TotalCount,
		AllocatedCount: p.AllocatedCount, ReservedIPs: p.ReservedIPs, Metadata: p.Metadata,
		Version: p.Version, CreatedAt: p.CreatedAt.Format(time.RFC3339),
	}
}

func toIPPoolDTOs(items []*cluster.IPPool) []ipPoolDTO {
	out := make([]ipPoolDTO, 0, len(items))
	for _, p := range items {
		out = append(out, *toIPPoolDTO(p))
	}
	return out
}
