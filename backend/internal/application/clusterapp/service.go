// Package clusterapp 是集群领域的应用服务层。
// 编排集群实体、kubeconfig 加密、K8s 连通性探测、IP 池与运行态缓存查询。
package clusterapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"

	"github.com/vortexops/vortexops/internal/domain"
	"github.com/vortexops/vortexops/internal/domain/cluster"
	"github.com/vortexops/vortexops/internal/domain/networkprofile"
	"github.com/vortexops/vortexops/internal/infrastructure/k8s"
	"github.com/vortexops/vortexops/internal/infrastructure/k8s/podnet"
	"github.com/vortexops/vortexops/internal/platform/security"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// Service 集群应用服务。
type Service struct {
	repo    cluster.Repository
	cipher  *security.FieldCipher
	pool    *k8s.ClientPool
}

// New 创建集群服务。pool 用于连通性探测与解密后客户端缓存。
func New(repo cluster.Repository, cipher *security.FieldCipher, pool *k8s.ClientPool) *Service {
	return &Service{repo: repo, cipher: cipher, pool: pool}
}

// --- 集群 CRUD ---

// CreateInput 创建集群请求。Kubeconfig 为明文字节，服务层加密后入库。
type CreateInput struct {
	Name                   string
	DisplayName            string
	Description            string
	APIServer              string
	Kubeconfig             []byte
	CACert                 []byte
	DefaultNamespacePrefix string
	InsecureSkipTLS        bool
	Region                 string
	Environment            string
	Labels                 map[string]string
	Metadata               map[string]any
	CreatedBy              int64
}

// Create 创建集群（加密 kubeconfig 后入库，并预探测连通性）。
func (s *Service) Create(ctx context.Context, in CreateInput) (*cluster.Cluster, error) {
	if err := validateClusterName(in.Name); err != nil {
		return nil, err
	}
	if len(in.Kubeconfig) == 0 {
		return nil, apperr.Validation("kubeconfig is required", nil)
	}
	// api_server 可由 kubeconfig 自动解析（用户无需手工填写）。
	if in.APIServer == "" {
		in.APIServer = k8s.ExtractAPIServerFromKubeconfig(in.Kubeconfig)
	}
	if in.APIServer == "" {
		return nil, apperr.Validation("api_server is required and could not be extracted from kubeconfig", nil)
	}
	// 名称唯一性预检。
	if _, err := s.repo.GetClusterByName(ctx, in.Name); err == nil {
		return nil, apperr.Conflict("cluster name already exists", cluster.ErrClusterNameExists)
	} else if !errors.Is(err, cluster.ErrClusterNotFound) {
		return nil, apperr.Internal("check cluster name", err)
	}

	kubeEnc, err := s.cipher.Encrypt(in.Kubeconfig)
	if err != nil {
		return nil, apperr.Internal("encrypt kubeconfig", err)
	}
	var caEnc []byte
	if len(in.CACert) > 0 {
		caEnc, err = s.cipher.Encrypt(in.CACert)
		if err != nil {
			return nil, apperr.Internal("encrypt ca cert", err)
		}
	}

	c := &cluster.Cluster{
		Name:                   in.Name,
		DisplayName:            in.DisplayName,
		Description:            in.Description,
		APIServer:              in.APIServer,
		KubeconfigEncrypted:    kubeEnc,
		CACertEncrypted:        caEnc,
		DefaultNamespacePrefix: in.DefaultNamespacePrefix,
		InsecureSkipTLS:        in.InsecureSkipTLS,
		Region:                 in.Region,
		Environment:            in.Environment,
		Status:                 cluster.StatusHealthy,
		Labels:                 in.Labels,
		Metadata:               in.Metadata,
	}
	// 校验网络方案配置（若提供）。
	if err := validateNetworkProfileMeta(c.Metadata); err != nil {
		return nil, err
	}
	c.CreatedBy = in.CreatedBy
	c.UpdatedBy = in.CreatedBy

	if err := s.repo.CreateCluster(ctx, c); err != nil {
		if errors.Is(err, cluster.ErrClusterNameExists) {
			return nil, apperr.Conflict("cluster name already exists", err)
		}
		return nil, apperr.Internal("create cluster", err)
	}

	// 异步探测连通性，不阻塞创建返回。
	go s.probeAndUpdate(context.Background(), c.ID, in.Kubeconfig, in.InsecureSkipTLS)
	return c, nil
}

// Get 按 ID 获取集群。
func (s *Service) Get(ctx context.Context, id int64) (*cluster.Cluster, error) {
	c, err := s.repo.GetClusterByID(ctx, id)
	if err != nil {
		if errors.Is(err, cluster.ErrClusterNotFound) {
			return nil, apperr.NotFound("cluster", strconv.FormatInt(id, 10))
		}
		return nil, apperr.Internal("get cluster", err)
	}
	return c, nil
}

// GetNetworkProfile 解析集群的网络方案配置（从 cluster.metadata.network_profile 反序列化）。
// 供 releaseapp/renderer 决定 CNI annotation 注入方式与是否建 Service。
// 未配置时返回 dev-single 默认值（兼容老集群）。
func (s *Service) GetNetworkProfile(ctx context.Context, clusterID int64) (*networkprofile.ProfileConfig, error) {
	c, err := s.repo.GetClusterByID(ctx, clusterID)
	if err != nil {
		if errors.Is(err, cluster.ErrClusterNotFound) {
			return nil, apperr.NotFound("cluster", strconv.FormatInt(clusterID, 10))
		}
		return nil, apperr.Internal("get cluster", err)
	}
	return ParseNetworkProfile(c.Metadata)
}

// SupportsUnderlay 实现 applicationapp.NetworkProfileResolver 接口。
// 仅 large-underlay profile 集群支持 Underlay 直连（Pod 拿物理局域网 IP）。
func (s *Service) SupportsUnderlay(ctx context.Context, clusterID int64) (bool, error) {
	profile, err := s.GetNetworkProfile(ctx, clusterID)
	if err != nil {
		return false, err
	}
	return profile.SupportsUnderlay(), nil
}

// ParseNetworkProfile 从 cluster metadata 解析网络方案配置。
// 导出供 renderer（纯函数，无 repo 依赖）直接使用：releaseapp 拿到 cluster 后调用此函数，
// 把结果传给 renderer，避免 renderer 反向依赖 clusterapp/repo。
// 未配置时返回 dev-single 默认值（兼容老集群，不破坏既有行为）。
func ParseNetworkProfile(meta map[string]any) (*networkprofile.ProfileConfig, error) {
	if meta == nil {
		return &networkprofile.ProfileConfig{Profile: networkprofile.ProfileDevSingle}, nil
	}
	raw, ok := meta["network_profile"]
	if !ok {
		return &networkprofile.ProfileConfig{Profile: networkprofile.ProfileDevSingle}, nil
	}
	// network_profile 可能是字符串（仅 profile 名）或对象（完整 ProfileConfig）。
	if s, ok := raw.(string); ok {
		p, err := networkprofile.ParseProfile(s)
		if err != nil {
			return nil, apperr.Validation("invalid network_profile: "+err.Error(), err)
		}
		cfg := &networkprofile.ProfileConfig{Profile: p}
		if err := cfg.Validate(); err != nil {
			return nil, apperr.Validation("network_profile config invalid: "+err.Error(), err)
		}
		return cfg, nil
	}
	// 对象形式：转 JSON 再反序列化（map[string]any → struct）。
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, apperr.Internal("marshal network_profile", err)
	}
	var cfg networkprofile.ProfileConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		return nil, apperr.Validation("invalid network_profile object: "+err.Error(), err)
	}
	if cfg.Profile == "" {
		cfg.Profile = networkprofile.ProfileDevSingle
	}
	if err := cfg.Validate(); err != nil {
		return nil, apperr.Validation("network_profile config invalid: "+err.Error(), err)
	}
	return &cfg, nil
}

// validateNetworkProfileMeta 创建/更新集群时校验 metadata.network_profile（若提供）。
func validateNetworkProfileMeta(meta map[string]any) error {
	if meta == nil {
		return nil
	}
	if _, ok := meta["network_profile"]; !ok {
		return nil
	}
	_, err := ParseNetworkProfile(meta)
	return err
}

// UpdateInput 更新集群请求。
type UpdateInput struct {
	ID                     int64
	DisplayName            *string
	Description            *string
	Kubeconfig             []byte
	CACert                 []byte
	DefaultNamespacePrefix *string
	InsecureSkipTLS        *bool
	Region                 *string
	Environment            *string
	Labels                 *map[string]string
	Metadata               *map[string]any
	Version                int
	ActorID                int64
}

// Update 更新集群（乐观锁）。kubeconfig 变更时重新加密并失效客户端池缓存。
func (s *Service) Update(ctx context.Context, in UpdateInput) (*cluster.Cluster, error) {
	var kubeEnc, caEnc []byte
	var err error
	if len(in.Kubeconfig) > 0 {
		kubeEnc, err = s.cipher.Encrypt(in.Kubeconfig)
		if err != nil {
			return nil, apperr.Internal("encrypt kubeconfig", err)
		}
	}
	if len(in.CACert) > 0 {
		caEnc, err = s.cipher.Encrypt(in.CACert)
		if err != nil {
			return nil, apperr.Internal("encrypt ca cert", err)
		}
	}
	// 校验网络方案配置（若传入新 metadata）。
	if in.Metadata != nil {
		if err := validateNetworkProfileMeta(*in.Metadata); err != nil {
			return nil, err
		}
	}
	updated, err := s.repo.UpdateCluster(ctx, cluster.UpdateClusterInput{
		ID: in.ID, DisplayName: in.DisplayName, Description: in.Description,
		KubeconfigEncrypted: kubeEnc, CACertEncrypted: caEnc,
		DefaultNamespacePrefix: in.DefaultNamespacePrefix, InsecureSkipTLS: in.InsecureSkipTLS,
		Region: in.Region, Environment: in.Environment, Labels: in.Labels, Metadata: in.Metadata,
		Version: in.Version, UpdatedBy: in.ActorID,
	})
	if err != nil {
		return nil, mapUpdateErr(err, "cluster", in.ID)
	}
	// kubeconfig / TLS 选项变更：失效客户端缓存，下次访问重建。
	if len(in.Kubeconfig) > 0 || in.InsecureSkipTLS != nil {
		s.pool.Remove(in.ID)
	}
	return updated, nil
}

// List 分页列出集群。
func (s *Service) List(ctx context.Context, status cluster.Status, region, search string, page, size int) ([]*cluster.Cluster, int64, error) {
	items, total, err := s.repo.ListClusters(ctx, cluster.ClusterQuery{
		Status: status, Region: region, Search: search,
		Offset: (page - 1) * size, Limit: size,
	})
	if err != nil {
		return nil, 0, apperr.Internal("list clusters", err)
	}
	return items, total, nil
}

// Delete 软删除集群（同时失效客户端缓存）。
// 关联校验：集群仍被工作空间绑定或存在分组时禁止删除，避免运行中资源失去归属。
func (s *Service) Delete(ctx context.Context, id, actorID int64) error {
	bindCount, err := s.repo.CountWorkspaceBindings(ctx, id)
	if err != nil {
		return apperr.Internal("count workspace bindings before delete", err)
	}
	if bindCount > 0 {
		return apperr.BusinessRule(
			fmt.Sprintf("cluster is bound to %d workspace(s); unbind them before deleting", bindCount),
			cluster.ErrClusterInUse,
		)
	}
	groupCount, err := s.repo.CountGroupsByCluster(ctx, id)
	if err != nil {
		return apperr.Internal("count groups before delete", err)
	}
	if groupCount > 0 {
		return apperr.BusinessRule(
			fmt.Sprintf("cluster has %d group(s); remove them before deleting", groupCount),
			cluster.ErrClusterInUse,
		)
	}
	if err := s.repo.DeleteCluster(ctx, id, actorID); err != nil {
		if errors.Is(err, cluster.ErrClusterNotFound) {
			return apperr.NotFound("cluster", strconv.FormatInt(id, 10))
		}
		return apperr.Internal("delete cluster", err)
	}
	s.pool.Remove(id)
	return nil
}

// --- 连通性探测 ---

// Probe 探测集群连通性，返回版本与节点数，并更新集群状态。
// 同时聚合所有节点的 Allocatable（CPU/内存/GPU）写回 vo_clusters，供容量预估使用。
func (s *Service) Probe(ctx context.Context, id int64) (*ProbeResult, error) {
	c, err := s.repo.GetClusterByID(ctx, id)
	if err != nil {
		if errors.Is(err, cluster.ErrClusterNotFound) {
			return nil, apperr.NotFound("cluster", strconv.FormatInt(id, 10))
		}
		return nil, apperr.Internal("get cluster", err)
	}
	if c.Status == cluster.StatusDisabled {
		return nil, apperr.BusinessRule("cluster is disabled", cluster.ErrClusterDisabled)
	}
	raw, err := s.cipher.Decrypt(c.KubeconfigEncrypted)
	if err != nil {
		return nil, apperr.Internal("decrypt kubeconfig", err)
	}
	result, err := s.probeWithKubeconfig(ctx, id, raw, c.InsecureSkipTLS)
	if err != nil {
		// 探测失败：更新为 unreachable。
		_, _ = s.repo.UpdateCluster(ctx, cluster.UpdateClusterInput{
			ID: id, Status: ptrStatus(cluster.StatusUnreachable),
			LastCheckedAt: ptrTime(time.Now()), LastError: ptrStr(truncateErr(err.Error())),
			Version: c.Version, UpdatedBy: 0,
		})
		return nil, apperr.BusinessRule("cluster unreachable: "+truncateErr(err.Error()), cluster.ErrClusterUnreachable)
	}
	// 探测成功：更新状态与摘要 + 可调度资源总量。
	_, _ = s.repo.UpdateCluster(ctx, cluster.UpdateClusterInput{
		ID: id, Status: ptrStatus(cluster.StatusHealthy),
		K8sVersion: ptrStr(result.K8sVersion), NodeCount: ptrInt(result.NodeCount),
		LastCheckedAt: ptrTime(time.Now()), LastError: ptrStr(""),
		AllocatableCPUM: ptrInt(result.AllocatableCPUM),
		AllocatableMemoryBytes: ptrInt64(result.AllocatableMemoryBytes),
		AllocatableGPU:  ptrInt(result.AllocatableGPU),
		CapacitySyncedAt: ptrTime(time.Now()),
		Version: c.Version, UpdatedBy: 0,
	})
	return result, nil
}

// ProbeResult 连通性探测结果。
type ProbeResult struct {
	K8sVersion             string `json:"k8s_version"`
	NodeCount              int    `json:"node_count"`
	APIServer              string `json:"api_server"`
	AllocatableCPUM        int    `json:"allocatable_cpu_m"`
	AllocatableMemoryBytes int64  `json:"allocatable_memory_bytes"`
	AllocatableGPU         int    `json:"allocatable_gpu"`
}

// GetDynamicClient 返回指定集群的 dynamic client（Cilium/Mesh CRD apply）。
func (s *Service) GetDynamicClient(ctx context.Context, clusterID int64) (dynamic.Interface, error) {
	c, err := s.repo.GetClusterByID(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	if len(c.KubeconfigEncrypted) == 0 {
		return nil, fmt.Errorf("cluster %d has no kubeconfig", clusterID)
	}
	raw, err := s.cipher.Decrypt(c.KubeconfigEncrypted)
	if err != nil {
		return nil, fmt.Errorf("decrypt kubeconfig: %w", err)
	}
	entry, err := s.pool.GetOrCreate(clusterID, raw, c.InsecureSkipTLS)
	if err != nil {
		return nil, err
	}
	return entry.Dynamic, nil
}

// GetClient 返回指定集群的 K8s clientset（解密 kubeconfig → 缓存或创建客户端）。
// 供 releaseapp 等需要直接操作 K8s 的应用服务调用。
func (s *Service) GetClient(ctx context.Context, clusterID int64) (kubernetes.Interface, error) {
	entry, err := s.getClientEntry(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	return entry.Clientset, nil
}

// getClientEntry 解密 kubeconfig 并获取缓存的 K8s 客户端入口（含 RestConfig，供 exec 探活使用）。
func (s *Service) getClientEntry(ctx context.Context, clusterID int64) (*k8s.ClientEntry, error) {
	c, err := s.repo.GetClusterByID(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	if len(c.KubeconfigEncrypted) == 0 {
		return nil, fmt.Errorf("cluster %d has no kubeconfig", clusterID)
	}
	raw, err := s.cipher.Decrypt(c.KubeconfigEncrypted)
	if err != nil {
		return nil, fmt.Errorf("decrypt kubeconfig: %w", err)
	}
	return s.pool.GetOrCreate(clusterID, raw, c.InsecureSkipTLS)
}

// GetDecryptedKubeconfig 返回指定集群解密后的原始 kubeconfig 字节，供 Helm 等需要原始配置的组件使用。
func (s *Service) GetDecryptedKubeconfig(ctx context.Context, clusterID int64) ([]byte, error) {
	c, err := s.repo.GetClusterByID(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	if len(c.KubeconfigEncrypted) == 0 {
		return nil, fmt.Errorf("cluster %d has no kubeconfig", clusterID)
	}
	return s.cipher.Decrypt(c.KubeconfigEncrypted)
}

// GetClusterInsecureSkipTLS 返回集群是否跳过 TLS 校验（实现 inferenceapp.KubeconfigProvider）。
func (s *Service) GetClusterInsecureSkipTLS(ctx context.Context, clusterID int64) (bool, error) {
	c, err := s.repo.GetClusterByID(ctx, clusterID)
	if err != nil {
		return false, err
	}
	return c.InsecureSkipTLS, nil
}

// AllocateForGroup 为 group 分配 N 个稳定 IP（keep_pod_ip / underlay 场景）。
// 选择集群首个有可用 IP 的池，按 replica 顺序分配；若该 group 已有分配则复用。
// Underlay profile（large-underlay）下优先选 macvlan/ipvlan provider 的池；
// 其它 profile 沿用既有池选择逻辑。
// 跨集群全局唯一由 DB 部分唯一索引 uq_ip_allocations_ip_active 兜底（allocated 状态下 ip 全局唯一）。
//
// 扩缩容处理：
//   - 缩容（existing > replicas）：返回前 replicas 个 IP（多余 IP 不释放，保留供回滚/重建复用）。
//   - 扩容（existing < replicas）：复用已有 IP + 补充分配 (replicas - existing) 个新 IP。
//   - 首次（existing == 0）：批量分配 replicas 个。
func (s *Service) AllocateForGroup(ctx context.Context, groupID, clusterID int64, replicas int) ([]string, error) {
	// 复用已有分配（幂等 + 扩缩容）。
	existing, err := s.repo.ListAllocationsByResource(ctx, cluster.IPAllocGroup, groupID)
	if err == nil && len(existing) >= replicas {
		ips := make([]string, 0, replicas)
		for i := 0; i < replicas && i < len(existing); i++ {
			ips = append(ips, existing[i].IPAddress)
		}
		return ips, nil
	}
	// 扩容：复用已有 + 补充分配。
	if err == nil && len(existing) > 0 && len(existing) < replicas {
		need := replicas - len(existing)
		existingIPs := make([]string, 0, replicas)
		for _, a := range existing {
			existingIPs = append(existingIPs, a.IPAddress)
		}
		// 分配增量 IP（从已有最大 replica_index+1 开始）。
		maxIdx := 0
		for _, a := range existing {
			if a.ReplicaIndex > maxIdx {
				maxIdx = a.ReplicaIndex
			}
		}
		newIPs, err := s.allocateIncremental(ctx, groupID, clusterID, need, maxIdx+1)
		if err != nil {
			// 增量分配失败：降级返回已有 IP（不足 replicas 个，由软降级处理）。
			return existingIPs, nil
		}
		return append(existingIPs, newIPs...), nil
	}
	// 首次分配：批量分配 replicas 个。
	pools, err := s.repo.ListIPPools(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	if len(pools) == 0 {
		return nil, fmt.Errorf("no ip pool for cluster %s", s.clusterLabel(ctx, clusterID))
	}
	// Underlay profile：优先选 underlay provider（macvlan/ipvlan）的池。
	// 解析集群网络方案；解析失败时按既有逻辑（不排序），兼容老集群。
	profile, perr := s.GetNetworkProfile(ctx, clusterID)
	if perr == nil && profile.SupportsUnderlay() {
		pools = sortPoolsUnderlayFirst(pools)
	}
	// Phase 1: 单事务批量分配（SELECT FOR UPDATE SKIP LOCKED LIMIT n）。
	// 逐池尝试，首个能分配到 n 个的池即用。
	for _, pool := range pools {
		allocs, aerr := s.repo.AllocateIPsBatch(ctx, pool.ID, clusterID, replicas, cluster.IPAllocGroup, groupID)
		if aerr != nil {
			if errors.Is(aerr, cluster.ErrIPExhausted) {
				continue // 当前池可用不足，试下一个池
			}
			return nil, aerr
		}
		if len(allocs) >= replicas {
			ips := make([]string, 0, replicas)
			for i := 0; i < replicas && i < len(allocs); i++ {
				ips = append(ips, allocs[i].IPAddress)
			}
			return ips, nil
		}
		// 批量分配返回少于请求数（池部分可用）：释放已分配的，试下一个池。
		if len(allocs) > 0 {
			_, _ = s.repo.ReleaseByResource(ctx, pool.ID, cluster.IPAllocGroup, groupID)
		}
	}
	return nil, fmt.Errorf("no ip pool with %d available ips for cluster %s", replicas, s.clusterLabel(ctx, clusterID))
}

// allocateIncremental 为 group 分配增量 IP（扩容时用），从 startReplicaIndex 开始编号。
func (s *Service) allocateIncremental(ctx context.Context, groupID, clusterID int64, count, startReplicaIndex int) ([]string, error) {
	pools, err := s.repo.ListIPPools(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	if len(pools) == 0 {
		return nil, fmt.Errorf("no ip pool for cluster %s", s.clusterLabel(ctx, clusterID))
	}
	profile, perr := s.GetNetworkProfile(ctx, clusterID)
	if perr == nil && profile.SupportsUnderlay() {
		pools = sortPoolsUnderlayFirst(pools)
	}
	for _, pool := range pools {
		allocs, aerr := s.repo.AllocateIPsBatch(ctx, pool.ID, clusterID, count, cluster.IPAllocGroup, groupID)
		if aerr != nil {
			if errors.Is(aerr, cluster.ErrIPExhausted) {
				continue
			}
			return nil, aerr
		}
		if len(allocs) >= count {
			ips := make([]string, 0, count)
			for i, a := range allocs {
				if i >= count {
					break
				}
				// AllocateIPsBatch 用 0-based replica_index，补写正确的起始序号。
				_, _ = s.repo.AllocateIP(ctx, pool.ID, clusterID, a.IPAddress, cluster.IPAllocGroup, groupID, int64(startReplicaIndex+i))
				ips = append(ips, a.IPAddress)
			}
			return ips, nil
		}
		if len(allocs) > 0 {
			_, _ = s.repo.ReleaseByResource(ctx, pool.ID, cluster.IPAllocGroup, groupID)
		}
	}
	return nil, fmt.Errorf("no ip pool with %d available ips for cluster %s (incremental)", count, s.clusterLabel(ctx, clusterID))
}

// sortPoolsUnderlayFirst 把 macvlan/ipvlan provider 的池排到前面（不改原切片）。
func sortPoolsUnderlayFirst(pools []*cluster.IPPool) []*cluster.IPPool {
	out := make([]*cluster.IPPool, 0, len(pools))
	for _, p := range pools {
		if p.Provider == cluster.IPPoolMacvlan || p.Provider == cluster.IPPoolIPVLAN {
			out = append(out, p)
		}
	}
	for _, p := range pools {
		if p.Provider != cluster.IPPoolMacvlan && p.Provider != cluster.IPPoolIPVLAN {
			out = append(out, p)
		}
	}
	return out
}

// clusterLabel 错误/事件文案用：优先显示名，其次 name。
func (s *Service) clusterLabel(ctx context.Context, clusterID int64) string {
	c, err := s.repo.GetClusterByID(ctx, clusterID)
	if err != nil || c == nil {
		return "unknown-cluster"
	}
	if c.DisplayName != "" {
		return c.DisplayName
	}
	if c.Name != "" {
		return c.Name
	}
	return "unknown-cluster"
}

func (s *Service) probeWithKubeconfig(ctx context.Context, clusterID int64, raw []byte, insecureSkipTLS bool) (*ProbeResult, error) {
	entry, err := s.pool.GetOrCreate(clusterID, raw, insecureSkipTLS)
	if err != nil {
		return nil, err
	}
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	versionInfo, err := entry.Clientset.Discovery().ServerVersion()
	if err != nil {
		return nil, err
	}
	nodes, err := entry.Clientset.CoreV1().Nodes().List(probeCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	result := &ProbeResult{
		K8sVersion: versionInfo.GitVersion, NodeCount: len(nodes.Items), APIServer: entry.APIServer,
	}
	// 聚合所有可调度节点的 Allocatable（不计 NoSchedule/NoExecute 污染的节点）。
	for _, node := range nodes.Items {
		if isNodeUnschedulable(node) {
			continue
		}
		result.AllocatableCPUM += cpuQuantityToMilli(node.Status.Allocatable.Cpu())
		result.AllocatableMemoryBytes += node.Status.Allocatable.Memory().Value()
		if gpu := node.Status.Allocatable.Name(gpuResourceName, resource.DecimalSI); !gpu.IsZero() {
			result.AllocatableGPU += int(gpu.Value())
		}
	}
	return result, nil
}

// probeAndUpdate 后台探测并更新（创建集群时调用）。
func (s *Service) probeAndUpdate(ctx context.Context, clusterID int64, rawKubeconfig []byte, insecureSkipTLS bool) {
	result, err := s.probeWithKubeconfig(ctx, clusterID, rawKubeconfig, insecureSkipTLS)
	c, gerr := s.repo.GetClusterByID(ctx, clusterID)
	if gerr != nil {
		return
	}
	if err != nil {
		_, _ = s.repo.UpdateCluster(ctx, cluster.UpdateClusterInput{
			ID: clusterID, Status: ptrStatus(cluster.StatusUnreachable),
			LastCheckedAt: ptrTime(time.Now()), LastError: ptrStr(truncateErr(err.Error())),
			Version: c.Version,
		})
		return
	}
	_, _ = s.repo.UpdateCluster(ctx, cluster.UpdateClusterInput{
		ID: clusterID, Status: ptrStatus(cluster.StatusHealthy),
		K8sVersion: ptrStr(result.K8sVersion), NodeCount: ptrInt(result.NodeCount),
		LastCheckedAt: ptrTime(time.Now()), LastError: ptrStr(""),
		AllocatableCPUM: ptrInt(result.AllocatableCPUM),
		AllocatableMemoryBytes: ptrInt64(result.AllocatableMemoryBytes),
		AllocatableGPU:  ptrInt(result.AllocatableGPU),
		CapacitySyncedAt: ptrTime(time.Now()),
		Version: c.Version,
	})
}

// --- 凭证 ---

// CreateCredentialInput 创建凭证请求。Payload 为明文字节，服务层加密。
type CreateCredentialInput struct {
	Name      string
	Kind      cluster.CredentialKind
	Scope     cluster.CredentialScope
	ScopeID   int64
	Payload   []byte
	ExpiresAt *time.Time
	CreatedBy int64
}

// CreateCredential 创建凭证（加密 payload）。
func (s *Service) CreateCredential(ctx context.Context, in CreateCredentialInput) (*cluster.Credential, error) {
	if in.Name == "" {
		return nil, apperr.Validation("credential name is required", nil)
	}
	if in.Kind == "" {
		return nil, apperr.Validation("credential kind is required", nil)
	}
	if len(in.Payload) == 0 {
		return nil, apperr.Validation("payload is required", nil)
	}
	enc, err := s.cipher.Encrypt(in.Payload)
	if err != nil {
		return nil, apperr.Internal("encrypt credential", err)
	}
	c := &cluster.Credential{
		Name: in.Name, Kind: in.Kind, Scope: in.Scope, ScopeID: in.ScopeID,
		PayloadEncrypted: enc, ExpiresAt: in.ExpiresAt,
	}
	c.CreatedBy = in.CreatedBy
	if err := s.repo.CreateCredential(ctx, c); err != nil {
		if errors.Is(err, cluster.ErrCredentialNameExists) {
			return nil, apperr.Conflict("credential name already exists", err)
		}
		return nil, apperr.Internal("create credential", err)
	}
	return c, nil
}

// GetDecryptedPayload 取凭证并解密 payload（仅授权场景调用）。
func (s *Service) GetDecryptedPayload(ctx context.Context, id int64) ([]byte, *cluster.Credential, error) {
	c, err := s.repo.GetCredentialByID(ctx, id)
	if err != nil {
		if errors.Is(err, cluster.ErrCredentialNotFound) {
			return nil, nil, apperr.NotFound("credential", strconv.FormatInt(id, 10))
		}
		return nil, nil, apperr.Internal("get credential", err)
	}
	if c.ExpiresAt != nil && time.Now().After(*c.ExpiresAt) {
		return nil, nil, apperr.BusinessRule("credential has expired", nil)
	}
	payload, err := s.cipher.Decrypt(c.PayloadEncrypted)
	if err != nil {
		return nil, nil, apperr.Internal("decrypt credential", err)
	}
	return payload, c, nil
}

// RotateCredential 轮换凭证 payload。
func (s *Service) RotateCredential(ctx context.Context, id int64, newPayload []byte, actorID int64) error {
	c, err := s.repo.GetCredentialByID(ctx, id)
	if err != nil {
		if errors.Is(err, cluster.ErrCredentialNotFound) {
			return apperr.NotFound("credential", strconv.FormatInt(id, 10))
		}
		return apperr.Internal("get credential", err)
	}
	enc, err := s.cipher.Encrypt(newPayload)
	if err != nil {
		return apperr.Internal("encrypt credential", err)
	}
	c.PayloadEncrypted = enc
	c.UpdatedBy = actorID
	if err := s.repo.UpdateCredential(ctx, c); err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return apperr.Conflict("credential was modified concurrently, please refresh", err)
		}
		return apperr.Internal("rotate credential", err)
	}
	return nil
}

// DeleteCredential 软删除凭证。
func (s *Service) DeleteCredential(ctx context.Context, id, actorID int64) error {
	if err := s.repo.DeleteCredential(ctx, id, actorID); err != nil {
		if errors.Is(err, cluster.ErrCredentialNotFound) {
			return apperr.NotFound("credential", strconv.FormatInt(id, 10))
		}
		return apperr.Internal("delete credential", err)
	}
	return nil
}

// ListCredentials 分页列出凭证。
func (s *Service) ListCredentials(ctx context.Context, scope cluster.CredentialScope, scopeID int64, kind cluster.CredentialKind, page, size int) ([]*cluster.Credential, int64, error) {
	items, total, err := s.repo.ListCredentials(ctx, scope, scopeID, kind, (page-1)*size, size)
	if err != nil {
		return nil, 0, apperr.Internal("list credentials", err)
	}
	return items, total, nil
}

// --- IP 池 ---

// CreateIPPoolInput 创建 IP 池请求。
type CreateIPPoolInput struct {
	ClusterID int64
	Name      string
	CIDR      string
	Gateway   string
	Provider  cluster.IPPoolProvider
	ReservedIPs []string
	// Metadata 扩展配置（Underlay 场景存 vlan_id/parent_interface/exclude_ranges 等）。
	Metadata  map[string]any
	CreatedBy int64
}

// CreateIPPool 创建 IP 池。
func (s *Service) CreateIPPool(ctx context.Context, in CreateIPPoolInput) (*cluster.IPPool, error) {
	if in.ClusterID == 0 {
		return nil, apperr.Validation("cluster_id is required", nil)
	}
	if in.Name == "" || in.CIDR == "" {
		return nil, apperr.Validation("name and cidr are required", nil)
	}
	if in.Provider == "" {
		in.Provider = cluster.IPPoolMetalLB
	}
	p := &cluster.IPPool{
		ClusterID: in.ClusterID, Name: in.Name, CIDR: in.CIDR, Gateway: in.Gateway,
		Provider: in.Provider, ReservedIPs: in.ReservedIPs, Metadata: in.Metadata,
	}
	p.CreatedBy = in.CreatedBy
	if err := s.repo.CreateIPPool(ctx, p); err != nil {
		return nil, apperr.Internal("create ip pool", err)
	}
	return p, nil
}

// ListIPPools 列出集群的 IP 池。
func (s *Service) ListIPPools(ctx context.Context, clusterID int64) ([]*cluster.IPPool, error) {
	items, err := s.repo.ListIPPools(ctx, clusterID)
	if err != nil {
		return nil, apperr.Internal("list ip pools", err)
	}
	return items, nil
}

// DeleteIPPool 软删除 IP 池。
func (s *Service) DeleteIPPool(ctx context.Context, id, actorID int64) error {
	if err := s.repo.DeleteIPPool(ctx, id, actorID); err != nil {
		if errors.Is(err, cluster.ErrIPPoolNotFound) {
			return apperr.NotFound("ip pool", strconv.FormatInt(id, 10))
		}
		return apperr.Internal("delete ip pool", err)
	}
	return nil
}

// AllocateIP 为资源分配稳定 IP（keep_pod_ip 用）。
func (s *Service) AllocateIP(ctx context.Context, poolID, clusterID int64, ip string, rtype cluster.IPAllocResourceType, resourceID, replicaIndex int64) (*cluster.IPAllocation, error) {
	if ip == "" {
		return nil, apperr.Validation("ip is required", nil)
	}
	a, err := s.repo.AllocateIP(ctx, poolID, clusterID, ip, rtype, resourceID, replicaIndex)
	if err != nil {
		if errors.Is(err, cluster.ErrIPAlreadyAllocated) {
			return nil, apperr.Conflict("ip already allocated", err)
		}
		return nil, apperr.Internal("allocate ip", err)
	}
	return a, nil
}

// ReleaseIP 释放稳定 IP。
func (s *Service) ReleaseIP(ctx context.Context, poolID int64, ip string) error {
	if err := s.repo.ReleaseIP(ctx, poolID, ip); err != nil {
		if errors.Is(err, cluster.ErrIPAllocationNotFound) {
			return apperr.NotFound("ip allocation", ip)
		}
		return apperr.Internal("release ip", err)
	}
	return nil
}

// ReleaseForGroup 释放 group 的所有稳定 IP（group 删除时调用）。
// 遍历集群所有池，对每个池调用 ReleaseByResource。best-effort：失败不阻塞删除。
func (s *Service) ReleaseForGroup(ctx context.Context, groupID, clusterID int64) (int, error) {
	pools, err := s.repo.ListIPPools(ctx, clusterID)
	if err != nil {
		return 0, err
	}
	total := 0
	for _, pool := range pools {
		n, err := s.repo.ReleaseByResource(ctx, pool.ID, cluster.IPAllocGroup, groupID)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

// --- Pod 级 IP 分配（Phase 2 webhook 用） ---

// AllocateForPodInput 单 Pod 稳定 IP 分配入参。
type AllocateForPodInput struct {
	GroupID    int64
	ClusterID  int64
	Replicas   int // group 期望副本数（用于确定 replica_index 上界）
	// InUseIPs 当前已被活跃 Pod 实际占用的 IP 集合（webhook 查 K8s Pod status.podIP 得到）。
	// webhook 把该 group 下所有现存 Running Pod 的 PodIP 传入。
	// 为空表示该 group 下尚无活跃 Pod（首次发布）。
	// 兼容：同时也接受 OccupiedReplicaIndices（旧版注解解析，优先级低于 InUseIPs）。
	InUseIPs               []string
	OccupiedReplicaIndices []int
}

// AllocateForPod 为单个 Pod 分配稳定 IP（webhook 调用）。
//
// 策略（Deployment 多副本 IP 复用）：
//  1. 查 group 已有的 IP 分配（按 replica_index 排序）。
//  2. 构建占用集合：
//     - InUseIPs：当前活跃 Pod 实际持有的 IP（最可靠，直接看 Pod status.podIP）。
//     - OccupiedReplicaIndices：旧版注解解析（兼容，优先级低）。
//  3. 在 [0, replicas) 范围内寻找首个可用槽位：
//     - 已分配 IP 且该 IP 不在 InUseIPs 中（对应 Pod 已删，可复用）→ 返回该 IP。
//     - 已分配 IP 且在 InUseIPs 中 → 槽位被占，跳过。
//     - 未分配 IP 的槽位 → 分配新 IP。
//  4. 若 replicas 内已无空槽：拒绝分配。
//
// 幂等性：release 时预分配的 IP 复用，webhook 按需补充分配。
func (s *Service) AllocateForPod(ctx context.Context, in AllocateForPodInput) (ip string, replicaIndex int, err error) {
	if in.GroupID == 0 {
		return "", 0, apperr.Validation("group_id is required", nil)
	}
	if in.Replicas < 1 {
		in.Replicas = 1
	}
	inUseSet := make(map[string]bool, len(in.InUseIPs))
	for _, ip := range in.InUseIPs {
		inUseSet[ip] = true
	}
	occupiedIdx := make(map[int]bool, len(in.OccupiedReplicaIndices))
	for _, idx := range in.OccupiedReplicaIndices {
		occupiedIdx[idx] = true
	}

	// 1) 查 group 已有分配（按 replica_index 升序）。
	existing, err := s.repo.ListAllocationsByResource(ctx, cluster.IPAllocGroup, in.GroupID)
	if err != nil {
		return "", 0, apperr.Internal("list allocations for pod", err)
	}

	// 2) 找首个可复用槽位：已分配 IP 但未被活跃 Pod 占据（IP 不在 inUseSet）。
	for _, alloc := range existing {
		if alloc.ReplicaIndex < 0 || alloc.ReplicaIndex >= in.Replicas {
			continue
		}
		if inUseSet[alloc.IPAddress] {
			continue // 该 IP 正被活跃 Pod 使用，不可复用
		}
		if occupiedIdx[alloc.ReplicaIndex] {
			continue // 该槽位被注解标记为占用（兼容旧版）
		}
		return alloc.IPAddress, alloc.ReplicaIndex, nil
	}

	// 3) 找首个空槽位（未分配 IP 的 replica_index），按需分配。
	for idx := 0; idx < in.Replicas; idx++ {
		if occupiedIdx[idx] {
			continue
		}
		// 检查该 idx 是否已有分配（上面复用逻辑已覆盖，此处防御性跳过）。
		hasAlloc := false
		for _, alloc := range existing {
			if alloc.ReplicaIndex == idx {
				hasAlloc = true
				break
			}
		}
		if hasAlloc {
			continue
		}
		// 分配新 IP 到该槽位。
		ip, err := s.allocateSingleIPForGroup(ctx, in.GroupID, in.ClusterID, idx)
		if err != nil {
			return "", 0, err
		}
		return ip, idx, nil
	}

	// 4) replicas 内无空槽：拒绝（并发创建超出副本数，或所有 IP 都在用）。
	// 发布侧已设 maxSurge=0、maxUnavailable=1，旧 Pod 先终止释放 IP 后新 Pod 再创建，正常不应走到此处。
	return "", 0, apperr.BusinessRule(
		fmt.Sprintf("no available replica slot for group (replicas=%d, in_use=%d, occupied=%d)",
			in.Replicas, len(inUseSet), len(occupiedIdx)), cluster.ErrIPExhausted)
}

// allocateSingleIPForGroup 为 group 在指定 replica_index 槽位分配单个 IP。
// 从集群首个有可用 IP 的池分配，绑定 (group_id, replica_index)。
func (s *Service) allocateSingleIPForGroup(ctx context.Context, groupID, clusterID int64, replicaIndex int) (string, error) {
	pools, err := s.repo.ListIPPools(ctx, clusterID)
	if err != nil {
		return "", apperr.Internal("list ip pools", err)
	}
	if len(pools) == 0 {
		return "", apperr.BusinessRule(fmt.Sprintf("no ip pool for cluster %s", s.clusterLabel(ctx, clusterID)), cluster.ErrIPExhausted)
	}
	// Underlay profile：优先 underlay 池。
	profile, perr := s.GetNetworkProfile(ctx, clusterID)
	if perr == nil && profile.SupportsUnderlay() {
		pools = sortPoolsUnderlayFirst(pools)
	}
	for _, pool := range pools {
		allocs, aerr := s.repo.AllocateIPsBatch(ctx, pool.ID, clusterID, 1, cluster.IPAllocGroup, groupID)
		if aerr != nil {
			if errors.Is(aerr, cluster.ErrIPExhausted) {
				continue
			}
			return "", apperr.Internal("allocate single ip", aerr)
		}
		if len(allocs) > 0 {
			// AllocateIPsBatch 不写 replica_index（默认 0），这里补写。
			// 复用 AllocateIP 的 upsert 逻辑把 replica_index 绑到该 IP。
			if _, err := s.repo.AllocateIP(ctx, pool.ID, clusterID, allocs[0].IPAddress, cluster.IPAllocGroup, groupID, int64(replicaIndex)); err != nil {
				// 已分配给本 group 的 IP 补写 replica_index 失败：忽略（不影响 IP 复用，仅排序可能不准）。
				_ = err
			}
			return allocs[0].IPAddress, nil
		}
	}
	return "", apperr.BusinessRule(
		fmt.Sprintf("no ip pool with available ip for cluster %s", s.clusterLabel(ctx, clusterID)), cluster.ErrIPExhausted)
}

// ReleaseOnGroupDelete 释放 group 的所有稳定 IP（group 删除时调用）。
// 与 ReleaseForGroup 等价，仅为语义清晰提供独立方法名（Phase 2 计划要求）。
func (s *Service) ReleaseOnGroupDelete(ctx context.Context, groupID, clusterID int64) (int, error) {
	return s.ReleaseForGroup(ctx, groupID, clusterID)
}

// GroupIPAllocator 是 webhook 依赖的 IP 分配接口（解耦 webhook 对 Service 具体类型的依赖）。
type GroupIPAllocator interface {
	AllocateForPod(ctx context.Context, in AllocateForPodInput) (ip string, replicaIndex int, err error)
	GetNetworkProfile(ctx context.Context, clusterID int64) (*networkprofile.ProfileConfig, error)
}

// --- 校验 ---

func validateClusterName(name string) error {
	name = strings.TrimSpace(name)
	if len(name) < 2 || len(name) > 64 {
		return apperr.Validation("cluster name must be 2-64 characters", nil)
	}
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			return apperr.Validation("cluster name may only contain letters, digits, '-', '_'", nil)
		}
	}
	return nil
}

func mapUpdateErr(err error, resource string, id int64) error {
	if errors.Is(err, domain.ErrConflict) {
		return apperr.Conflict("resource was modified concurrently, please refresh", err)
	}
	if errors.Is(err, cluster.ErrClusterNotFound) {
		return apperr.NotFound(resource, strconv.FormatInt(id, 10))
	}
	return apperr.Internal("update "+resource, err)
}

func truncateErr(s string) string {
	if len(s) > 500 {
		return s[:500]
	}
	return s
}

func ptrStatus(s cluster.Status) *cluster.Status { return &s }
func ptrTime(t time.Time) *time.Time { return &t }
func ptrStr(s string) *string { return &s }
func ptrInt(i int) *int { return &i }
func ptrInt64(i int64) *int64 { return &i }

// gpuResourceName 是默认的 NVIDIA GPU 资源名；集群可能用其他名称，这里取最常见的。
const gpuResourceName = "nvidia.com/gpu"

// isNodeUnschedulable 判断节点是否不可调度（Unschedulable=true 或带 NoSchedule/NoExecute 污点）。
func isNodeUnschedulable(node corev1.Node) bool {
	if node.Spec.Unschedulable {
		return true
	}
	for _, taint := range node.Spec.Taints {
		if taint.Effect == corev1.TaintEffectNoSchedule || taint.Effect == corev1.TaintEffectNoExecute {
			return true
		}
	}
	return false
}

// cpuQuantityToMilli 把 corev1 资源 quantity（如 "2" 或 "500m"）转成 millicores 整数。
func cpuQuantityToMilli(q *resource.Quantity) int {
	if q == nil {
		return 0
	}
	return int(q.MilliValue())
}

// --- 集群容量预估 ---

// CapacityProvider 按集群 ID 与单副本资源需求预估可调度副本数。
// 由 Service 实现，供 releaseapp 发布前预校验使用（避免 releaseapp 直接耦合 Service 具体类型）。
type CapacityProvider interface {
	GetClusterCapacity(ctx context.Context, q CapacityQuery) (*ClusterCapacity, error)
}

// ClusterCapacity 集群可调度容量预估结果。
type ClusterCapacity struct {
	ClusterID             int64  `json:"cluster_id"`
	AllocatableCPUM       int    `json:"allocatable_cpu_m"`
	AllocatableMemoryBytes int64 `json:"allocatable_memory_bytes"`
	AllocatableGPU        int    `json:"allocatable_gpu"`
	UsedCPUM              int    `json:"used_cpu_m"`
	UsedMemoryBytes       int64  `json:"used_memory_bytes"`
	UsedGPU               int    `json:"used_gpu"`
	// MaxReplicas 按传入的单副本资源需求（cpu_m/memory_bytes/gpu）计算的理论最大可调度副本数。
	MaxReplicas int `json:"max_replicas"`
	// Source 容量数据来源："k8s_api"（实时）或 "db_cache"（最近一次 probe 缓存）。
	Source string `json:"source"`
}

// CapacityQuery 容量预估入参。
type CapacityQuery struct {
	ClusterID    int64
	PerCPUM      int
	PerMemBytes  int64
	PerGPU       int
}

// GetClusterCapacity 预估集群在指定单副本资源需求下的可调度副本数。
// 优先实时调 K8s API 聚合 Allocatable 与已用（所有 namespace 的 Running Pod requests 总和）；
// K8s API 不可达时回退到 vo_clusters 缓存的 allocatable（上次 probe 写入）。
func (s *Service) GetClusterCapacity(ctx context.Context, q CapacityQuery) (*ClusterCapacity, error) {
	c, err := s.repo.GetClusterByID(ctx, q.ClusterID)
	if err != nil {
		if errors.Is(err, cluster.ErrClusterNotFound) {
			return nil, apperr.NotFound("cluster", strconv.FormatInt(q.ClusterID, 10))
		}
		return nil, apperr.Internal("get cluster", err)
	}

	cap := &ClusterCapacity{
		ClusterID:              q.ClusterID,
		AllocatableCPUM:        c.AllocatableCPUM,
		AllocatableMemoryBytes: c.AllocatableMemoryBytes,
		AllocatableGPU:         c.AllocatableGPU,
		Source:                 "db_cache",
	}

	// 尝试实时取 K8s API。
	if client, err := s.GetClient(ctx, q.ClusterID); err == nil {
		listCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if nodes, err := client.CoreV1().Nodes().List(listCtx, metav1.ListOptions{}); err == nil {
			var allocCPU, allocGPU int
			var allocMem int64
			for _, node := range nodes.Items {
				if isNodeUnschedulable(node) {
					continue
				}
				allocCPU += cpuQuantityToMilli(node.Status.Allocatable.Cpu())
				allocMem += node.Status.Allocatable.Memory().Value()
				if gpu := node.Status.Allocatable.Name(gpuResourceName, resource.DecimalSI); !gpu.IsZero() {
					allocGPU += int(gpu.Value())
				}
			}
			cap.AllocatableCPUM = allocCPU
			cap.AllocatableMemoryBytes = allocMem
			cap.AllocatableGPU = allocGPU
			cap.Source = "k8s_api"

			// 已用：聚合所有 namespace 的 Running Pod requests 总和。
			if pods, err := client.CoreV1().Pods("").List(listCtx, metav1.ListOptions{FieldSelector: "status.phase=Running"}); err == nil {
				for _, pod := range pods.Items {
					for _, ctn := range pod.Spec.Containers {
						req := ctn.Resources.Requests
						cap.UsedCPUM += cpuQuantityToMilli(req.Cpu())
						cap.UsedMemoryBytes += req.Memory().Value()
						if gpu := req.Name(gpuResourceName, resource.DecimalSI); !gpu.IsZero() {
							cap.UsedGPU += int(gpu.Value())
						}
					}
				}
			}
		}
	}

	// 计算可调度副本数：剩余资源 / 单副本需求。
	availCPU := cap.AllocatableCPUM - cap.UsedCPUM
	availMem := cap.AllocatableMemoryBytes - cap.UsedMemoryBytes
	availGPU := cap.AllocatableGPU - cap.UsedGPU
	maxByCPU := int(^uint(0) >> 1) // max int
	maxByMem := maxByCPU
	maxByGPU := maxByCPU
	if q.PerCPUM > 0 {
		if availCPU <= 0 {
			maxByCPU = 0
		} else {
			maxByCPU = availCPU / q.PerCPUM
		}
	}
	if q.PerMemBytes > 0 {
		if availMem <= 0 {
			maxByMem = 0
		} else {
			maxByMem = int(availMem / q.PerMemBytes)
		}
	}
	if q.PerGPU > 0 {
		if availGPU <= 0 {
			maxByGPU = 0
		} else {
			maxByGPU = availGPU / q.PerGPU
		}
	}
	cap.MaxReplicas = minInt(minInt(maxByCPU, maxByMem), maxByGPU)
	if cap.MaxReplicas < 0 {
		cap.MaxReplicas = 0
	}
	return cap, nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// --- Group 运维（基于 K8s clientset） ---

// PodInfo 分组下 Pod 摘要。
type PodInfo struct {
	Name           string          `json:"name"`
	Namespace      string          `json:"namespace"`
	Status         string          `json:"status"`
	Phase          string          `json:"phase"`
	Ready          bool            `json:"ready"`
	RestartCount   int             `json:"restart_count"`
	NodeName       string          `json:"node_name"`
	PodIP          string          `json:"pod_ip"`
	AgeSeconds     int64           `json:"age_seconds"`
	Containers     []ContainerInfo `json:"containers"`
	// AppReady 应用探活主动拨测结果；nil 表示未配置探活或未拨测。
	AppReady *bool `json:"app_ready,omitempty"`
	// AppReadyDetail 未就绪/失败时的简要原因。
	AppReadyDetail string `json:"app_ready_detail,omitempty"`
}

// ContainerInfo 容器摘要。
// Ready 表示容器内应用是否就绪（readiness probe 通过）。
type ContainerInfo struct {
	Name          string `json:"name"`
	Image         string `json:"image"`
	Ready         bool   `json:"ready"`
	RestartCount  int    `json:"restart_count"`
	Started       bool   `json:"started"`
}

// ListGroupPods 列出分组（按 label selector）下的 Pod。
// selector 通常为 "app.kubernetes.io/instance=<group.name>" 或基于 group ID 的标签。
func (s *Service) ListGroupPods(ctx context.Context, clusterID int64, namespace, selector string) ([]PodInfo, error) {
	cli, err := s.GetClient(ctx, clusterID)
	if err != nil {
		return nil, apperr.Internal("get k8s client", err)
	}
	listOpts := metav1.ListOptions{}
	if selector != "" {
		listOpts.LabelSelector = selector
	}
	podList, err := cli.CoreV1().Pods(namespace).List(ctx, listOpts)
	if err != nil {
		return nil, apperr.Internal("list pods", err)
	}
	out := make([]PodInfo, 0, len(podList.Items))
	now := time.Now()
	for _, p := range podList.Items {
		pi := PodInfo{
			Name:       p.Name,
			Namespace:  p.Namespace,
			Status:     string(p.Status.Phase),
			Phase:      string(p.Status.Phase),
			NodeName:   p.Spec.NodeName,
			PodIP:      podnet.DisplayIP(&p),
			AgeSeconds: int64(now.Sub(p.CreationTimestamp.Time).Seconds()),
		}
		ready := true
		restarts := 0
		// 按 spec.containers 顺序对齐 containerStatuses，取镜像与就绪状态。
		statusByName := make(map[string]corev1.ContainerStatus, len(p.Status.ContainerStatuses))
		for _, cs := range p.Status.ContainerStatuses {
			statusByName[cs.Name] = cs
		}
		for _, specC := range p.Spec.Containers {
			info := ContainerInfo{Name: specC.Name, Image: specC.Image}
			if cs, ok := statusByName[specC.Name]; ok {
				info.Ready = cs.Ready
				info.RestartCount = int(cs.RestartCount)
				// Started 为 nil（旧集群/未上报）时：容器 Running 或 Ready 即视为已启动，避免误标 started=false。
				if cs.Started != nil {
					info.Started = *cs.Started
				} else {
					info.Started = cs.State.Running != nil || cs.Ready
				}
				restarts += int(cs.RestartCount)
				if !cs.Ready {
					ready = false
				}
			} else {
				// 缺失状态：视为未就绪。
				ready = false
			}
			pi.Containers = append(pi.Containers, info)
		}
		pi.RestartCount = restarts
		pi.Ready = ready && len(p.Status.ContainerStatuses) > 0
		if p.Status.Phase != corev1.PodRunning {
			pi.Ready = false
		}
		out = append(out, pi)
	}
	return out, nil
}

// PodLogsInput 拉取 Pod 日志参数。
type PodLogsInput struct {
	ClusterID   int64
	Namespace   string
	Pod         string
	Container   string
	TailLines   int64
	Follow      bool
}

// StreamPodLogs 流式拉取 Pod 日志，写入 out。
func (s *Service) StreamPodLogs(ctx context.Context, in PodLogsInput, out io.Writer) error {
	cli, err := s.GetClient(ctx, in.ClusterID)
	if err != nil {
		return apperr.Internal("get k8s client", err)
	}
	opts := &corev1.PodLogOptions{
		Container: in.Container,
		Follow:    in.Follow,
	}
	if in.TailLines > 0 {
		tl := in.TailLines
		opts.TailLines = &tl
	}
	req := cli.CoreV1().Pods(in.Namespace).GetLogs(in.Pod, opts)
	stream, err := req.Stream(ctx)
	if err != nil {
		return apperr.Internal("stream pod logs", err)
	}
	defer stream.Close()
	if _, err := io.Copy(out, stream); err != nil {
		return apperr.Internal("copy pod logs", err)
	}
	return nil
}

// PodEvent Pod 相关事件摘要。
type PodEvent struct {
	Type      string `json:"type"`
	Reason    string `json:"reason"`
	Message   string `json:"message"`
	Count     int32  `json:"count"`
	LastTime  string `json:"last_time"`
	FirstTime string `json:"first_time"`
}

// ListGroupEvents 列出命名空间下与 selector 相关的 Events。
func (s *Service) ListGroupEvents(ctx context.Context, clusterID int64, namespace, selector string) ([]PodEvent, error) {
	cli, err := s.GetClient(ctx, clusterID)
	if err != nil {
		return nil, apperr.Internal("get k8s client", err)
	}
	listOpts := metav1.ListOptions{}
	if selector != "" {
		listOpts.FieldSelector = "involvedObject.namespace=" + namespace
	}
	evList, err := cli.CoreV1().Events(namespace).List(ctx, listOpts)
	if err != nil {
		return nil, apperr.Internal("list events", err)
	}
	out := make([]PodEvent, 0, len(evList.Items))
	for _, e := range evList.Items {
		out = append(out, PodEvent{
			Type: e.Type, Reason: e.Reason, Message: e.Message, Count: e.Count,
			LastTime: e.LastTimestamp.Format(time.RFC3339), FirstTime: e.FirstTimestamp.Format(time.RFC3339),
		})
	}
	return out, nil
}

// RenderGroupYAML 渲染分组对应工作负载的 K8s 资源清单（只读，从集群读取当前状态）。
// 返回 (kind, yaml) 列表。
type RenderedResource struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
	YAML string `json:"yaml"`
}

// RenderGroupYAML 返回分组对应工作负载当前的 YAML。
// workloadType: deployment/statefulset/cronjob/job；name 为资源名。
func (s *Service) RenderGroupYAML(ctx context.Context, clusterID int64, namespace, workloadType, name string) ([]RenderedResource, error) {
	cli, err := s.GetClient(ctx, clusterID)
	if err != nil {
		return nil, apperr.Internal("get k8s client", err)
	}
	var out []RenderedResource
	switch strings.ToLower(workloadType) {
	case "deployment":
		d, err := cli.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, k8sGetErr("deployment", namespace, name, err)
		}
		y, _ := toYAML(d)
		out = append(out, RenderedResource{Kind: "Deployment", Name: name, YAML: y})
	case "statefulset":
		d, err := cli.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, k8sGetErr("statefulset", namespace, name, err)
		}
		y, _ := toYAML(d)
		out = append(out, RenderedResource{Kind: "StatefulSet", Name: name, YAML: y})
	case "cronjob":
		d, err := cli.BatchV1().CronJobs(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, k8sGetErr("cronjob", namespace, name, err)
		}
		y, _ := toYAML(d)
		out = append(out, RenderedResource{Kind: "CronJob", Name: name, YAML: y})
	case "job":
		d, err := cli.BatchV1().Jobs(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, k8sGetErr("job", namespace, name, err)
		}
		y, _ := toYAML(d)
		out = append(out, RenderedResource{Kind: "Job", Name: name, YAML: y})
	default:
		return nil, apperr.Validation("unsupported workload type: "+workloadType, nil)
	}
	// 附加关联 Service（同名）。
	svc, err := cli.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		y, _ := toYAML(svc)
		out = append(out, RenderedResource{Kind: "Service", Name: name, YAML: y})
	}
	return out, nil
}

// k8sGetErr 把 K8s 资源查询错误转换为应用错误：
// NotFound（namespace/资源不存在）→ 404，提示工作负载尚未部署；
// 其他错误（连通性、鉴权等）→ 500，附带原始错误便于排查。
func k8sGetErr(kind, namespace, name string, err error) error {
	if apierrors.IsNotFound(err) {
		return apperr.NotFound(kind, fmt.Sprintf("%s/%s (工作负载尚未部署到集群)", namespace, name))
	}
	return apperr.Internal("get "+kind, err)
}

// toYAML 把 K8s runtime.Object 序列化为 YAML（去除 status 以减小体积）。
func toYAML(obj runtime.Object) (string, error) {
	b, err := yaml.Marshal(obj)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

