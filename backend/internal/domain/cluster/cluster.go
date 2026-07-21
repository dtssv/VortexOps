// Package cluster 是集群接入与运行态同步领域的核心实体与仓储接口。
// 一个 Cluster 表示一个被纳管的 Kubernetes 集群。kubeconfig 以加密形态存于 vo_clusters，
// 凭证（registry/jenkins/git 等）存于 vo_credentials。
package cluster

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/vortexops/vortexops/internal/domain"
)

// Status 集群健康状态。
type Status string

const (
	StatusHealthy    Status = "healthy"
	StatusDegraded   Status = "degraded"
	StatusUnreachable Status = "unreachable"
	StatusDisabled   Status = "disabled"
)

// IPPoolProvider IP 池提供者（用于 keep_pod_ip）。
type IPPoolProvider string

const (
	IPPoolMetalLB     IPPoolProvider = "metallb"
	IPPoolCalicoIPAM  IPPoolProvider = "calico-ipam"
	IPPoolWhereabouts IPPoolProvider = "whereabouts"
	IPPoolKubeOVN     IPPoolProvider = "kube-ovn"
	// IPPoolMacvlan Macvlan Underlay：Pod 独立 MAC，交换机当终端学习，PC 同网段直连。
	IPPoolMacvlan     IPPoolProvider = "macvlan"
	// IPPoolIPVLAN IPVLAN L2 Underlay：共享父接口 MAC，高密度场景省交换机 MAC 表。
	IPPoolIPVLAN      IPPoolProvider = "ipvlan"
)

// IPAllocResourceType 稳定 IP 分配的资源类型。
type IPAllocResourceType string

const (
	IPAllocGroup        IPAllocResourceType = "group"
	IPAllocInferenceSvc IPAllocResourceType = "service"
)

// IPAllocStatus IP 分配状态。
type IPAllocStatus string

const (
	IPAllocAllocated IPAllocStatus = "allocated"
	IPAllocReleased  IPAllocStatus = "released"
)

// CredentialKind 凭证类型。
type CredentialKind string

const (
	CredKindKubeconfig    CredentialKind = "kubeconfig"
	CredKindBasicAuth     CredentialKind = "basic"
	CredKindBearerToken   CredentialKind = "bearer"
	CredKindSSHKey        CredentialKind = "ssh_key"
	CredKindRegistryPull  CredentialKind = "registry_pull"
	CredKindJenkins       CredentialKind = "jenkins"
	CredKindGitToken      CredentialKind = "git_token"
	CredKindGeneric       CredentialKind = "generic"
)

// CredentialScope 凭证作用域。
type CredentialScope string

const (
	CredScopePlatform CredentialScope = "platform"
	CredScopeWorkspace CredentialScope = "workspace"
	CredScopeApplication CredentialScope = "application"
)

// Cluster 集群实体。kubeconfig/CA 以加密字节存储。
type Cluster struct {
	ID                    int64
	UUID                  uuid.UUID
	Name                  string
	DisplayName           string
	Description           string
	APIServer             string
	KubeconfigEncrypted   []byte
	CACertEncrypted       []byte
	DefaultNamespacePrefix string
	InsecureSkipTLS       bool
	Region                string
	Environment           string
	K8sVersion            string
	NodeCount             int
	Status                Status
	LastCheckedAt         *time.Time
	LastError             string
	// 可调度资源总量缓存：由 Probe 时聚合所有节点的 Allocatable 写回。
	AllocatableCPUM        int
	AllocatableMemoryBytes int64
	AllocatableGPU         int
	CapacitySyncedAt       *time.Time
	Labels                map[string]string
	Metadata              map[string]any
	domain.Audit
}

// Credential 通用凭证（加密 payload）。可被 registry/jenkins/git/cluster 复用。
type Credential struct {
	ID              int64
	UUID            uuid.UUID
	Name            string
	Kind            CredentialKind
	Scope           CredentialScope
	ScopeID         int64
	PayloadEncrypted []byte
	ExpiresAt       *time.Time
	LastRotatedAt   *time.Time
	domain.Audit
}

// IPPool 集群 IP 池（keep_pod_ip / underlay 用）。
type IPPool struct {
	ID             int64
	UUID           uuid.UUID
	ClusterID      int64
	Name           string
	CIDR           string
	Gateway        string
	Provider       IPPoolProvider
	TotalCount     int
	AllocatedCount int
	ReservedIPs    []string
	// Metadata 扩展配置（JSONB）。Underlay 场景存 vlan_id/parent_interface/exclude_ranges 等。
	Metadata       map[string]any
	domain.Audit
}

// IPAllocation 稳定 IP 分配记录。
type IPAllocation struct {
	ID           int64
	IPPoolID     int64
	ClusterID    int64
	IPAddress    string
	ResourceType IPAllocResourceType
	ResourceID   int64
	ReplicaIndex int
	Status       IPAllocStatus
	AllocatedAt  time.Time
	ReleasedAt   *time.Time
}

// 领域错误。
var (
	ErrClusterNotFound      = errors.New("cluster not found")
	ErrClusterNameExists    = errors.New("cluster name already exists")
	ErrClusterInUse         = errors.New("cluster in use")
	ErrCredentialNotFound   = errors.New("credential not found")
	ErrCredentialNameExists = errors.New("credential name already exists")
	ErrIPPoolNotFound       = errors.New("ip pool not found")
	ErrIPAllocationNotFound = errors.New("ip allocation not found")
	ErrIPExhausted          = errors.New("ip pool exhausted")
	ErrIPAlreadyAllocated   = errors.New("ip already allocated")
	ErrClusterUnreachable   = errors.New("cluster unreachable")
	ErrClusterDisabled      = errors.New("cluster disabled")
)

// CreateClusterInput 创建集群输入。
type CreateClusterInput struct {
	Name                   string
	DisplayName            string
	Description            string
	APIServer              string
	KubeconfigEncrypted    []byte
	CACertEncrypted        []byte
	DefaultNamespacePrefix string
	InsecureSkipTLS        bool
	Region                 string
	Environment            string
	Labels                 map[string]string
	Metadata               map[string]any
	CreatedBy              int64
}

// UpdateClusterInput 更新集群输入（乐观锁）。
type UpdateClusterInput struct {
	ID                     int64
	DisplayName            *string
	Description            *string
	KubeconfigEncrypted    []byte
	CACertEncrypted        []byte
	DefaultNamespacePrefix *string
	InsecureSkipTLS        *bool
	Region                 *string
	Environment            *string
	Status                 *Status
	K8sVersion             *string
	NodeCount              *int
	LastCheckedAt          *time.Time
	LastError              *string
	AllocatableCPUM        *int
	AllocatableMemoryBytes *int64
	AllocatableGPU         *int
	CapacitySyncedAt       *time.Time
	Labels                 *map[string]string
	Metadata               *map[string]any
	Version                int
	UpdatedBy              int64
}

// ClusterQuery 集群查询。
type ClusterQuery struct {
	Status  Status
	Region  string
	Search  string
	Offset  int
	Limit   int
}

// Repository 集群仓储接口。
type Repository interface {
	CreateCluster(ctx context.Context, c *Cluster) error
	GetClusterByID(ctx context.Context, id int64) (*Cluster, error)
	GetClusterByUUID(ctx context.Context, id uuid.UUID) (*Cluster, error)
	GetClusterByName(ctx context.Context, name string) (*Cluster, error)
	UpdateCluster(ctx context.Context, in UpdateClusterInput) (*Cluster, error)
	ListClusters(ctx context.Context, q ClusterQuery) ([]*Cluster, int64, error)
	DeleteCluster(ctx context.Context, id, deletedBy int64) error
	// 列出所有未禁用集群（供 syncer 加载分片）。
	ListActiveClusters(ctx context.Context) ([]*Cluster, error)
	// 列出某集群在所有 workspace 中绑定的 namespace（去重，供 syncer 分片 Watch）。
	ListNamespacesByCluster(ctx context.Context, clusterID int64) ([]string, error)
	// 统计某集群在所有 workspace 中的绑定数（删除前关联校验）。
	CountWorkspaceBindings(ctx context.Context, clusterID int64) (int64, error)
	// 统计某集群下未删除的分组数（删除前关联校验）。
	CountGroupsByCluster(ctx context.Context, clusterID int64) (int64, error)

	// 凭证
	CreateCredential(ctx context.Context, c *Credential) error
	GetCredentialByID(ctx context.Context, id int64) (*Credential, error)
	GetCredentialByName(ctx context.Context, name string, scope CredentialScope, scopeID int64) (*Credential, error)
	ListCredentials(ctx context.Context, scope CredentialScope, scopeID int64, kind CredentialKind, offset, limit int) ([]*Credential, int64, error)
	UpdateCredential(ctx context.Context, c *Credential) error
	DeleteCredential(ctx context.Context, id, deletedBy int64) error

	// IP 池
	CreateIPPool(ctx context.Context, p *IPPool) error
	GetIPPoolByID(ctx context.Context, id int64) (*IPPool, error)
	ListIPPools(ctx context.Context, clusterID int64) ([]*IPPool, error)
	UpdateIPPool(ctx context.Context, p *IPPool) error
	DeleteIPPool(ctx context.Context, id, deletedBy int64) error

	// IP 分配
	AllocateIP(ctx context.Context, poolID, clusterID int64, ip string, rtype IPAllocResourceType, resourceID, replicaIndex int64) (*IPAllocation, error)
	ReleaseIP(ctx context.Context, poolID int64, ip string) error
	GetAllocation(ctx context.Context, poolID int64, ip string) (*IPAllocation, error)
	ListAllocationsByResource(ctx context.Context, rtype IPAllocResourceType, resourceID int64) ([]*IPAllocation, error)
	// ListAvailableIPs 列出池中未分配的 IP（用于 keep_pod_ip 按需分配）。
	ListAvailableIPs(ctx context.Context, poolID int64, limit int) ([]string, error)

	// Phase 1: 10万+规模 IPAM。
	// PreGenerateEntries 建池时预生成所有可用 IP 条目到 vo_cluster_ip_pool_entries
	// （分批 INSERT，避免单事务过大）。幂等：已存在条目跳过。
	PreGenerateEntries(ctx context.Context, poolID int64, cidr string, reserved []string) (int, error)
	// AllocateIPsBatch 单事务批量分配 N 个 IP（SELECT FOR UPDATE SKIP LOCKED LIMIT n）。
	// 返回分配的 IP 列表与分配记录。资源绑定（resource_type/resource_id）在预占时写入。
	AllocateIPsBatch(ctx context.Context, poolID, clusterID int64, n int, rtype IPAllocResourceType, resourceID int64) ([]*IPAllocation, error)
	// ReleaseByResource 释放某资源的所有 IP 分配（group 删除时调用）。
	ReleaseByResource(ctx context.Context, poolID int64, rtype IPAllocResourceType, resourceID int64) (int, error)
}

// WorkspaceClusterBindingShard syncer 分片描述：一个 cluster 下的 namespace 列表。
type WorkspaceClusterBindingShard struct {
	ClusterID  int64
	Namespaces []string
}

// ShardLoader syncer 启动时加载本实例负责的分片。
type ShardLoader interface {
	LoadShards(ctx context.Context) ([]WorkspaceClusterBindingShard, error)
}
