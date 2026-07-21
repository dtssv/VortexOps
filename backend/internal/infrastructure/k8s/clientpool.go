// Package k8s 提供被管 Kubernetes 集群的客户端管理与运行态同步基础设施。
// 设计要点：
//   - ClientPool 按 cluster_id 缓存 *kubernetes.Clientset + dynamic.Interface，避免重复建连。
//   - kubeconfig 在 DB 中为加密字节，由调用方（clusterapp）解密后传入 raw kubeconfig。
//   - InformerManager 按 (cluster_id, namespace) 分片 Watch，事件回调写 Redis 缓存。
//   - LeaderElector 基于 K8s coordination.k8s.io/v1 Lease，保证单分片单主。
package k8s

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// ErrClusterClientNotFound 客户端池中不存在该集群的客户端。
var ErrClusterClientNotFound = errors.New("cluster client not found in pool")

// ClientEntry 缓存的集群客户端集合。
type ClientEntry struct {
	Clientset   kubernetes.Interface
	Dynamic     dynamic.Interface
	RestConfig  *rest.Config
	APIServer   string
}

// ClientPool 管理多集群 client-go 客户端的缓存与生命周期。
// 线程安全；集群删除或 kubeconfig 轮换时调用 Remove 失效旧客户端。
type ClientPool struct {
	mu      sync.RWMutex
	clients map[int64]*ClientEntry
}

// NewClientPool 创建客户端池。
func NewClientPool() *ClientPool {
	return &ClientPool{clients: make(map[int64]*ClientEntry)}
}

// BuildFromKubeconfig 从原始 kubeconfig 字节构建 rest.Config。
// insecureSkipTLS 为 true 时跳过服务端证书校验（开发环境 host-net 等场景）。
func BuildFromKubeconfig(raw []byte, insecureSkipTLS bool) (*rest.Config, error) {
	if len(raw) == 0 {
		return nil, errors.New("empty kubeconfig")
	}
	cfg, err := clientcmd.RESTConfigFromKubeConfig(raw)
	if err != nil {
		return nil, fmt.Errorf("parse kubeconfig: %w", err)
	}
	if insecureSkipTLS {
		cfg.TLSClientConfig.Insecure = true
	}
	// client-go 不允许 Insecure 与 CA 证书同时存在（kubeconfig 可能两者都有）。
	if cfg.TLSClientConfig.Insecure {
		cfg.TLSClientConfig.CAData = nil
		cfg.TLSClientConfig.CAFile = ""
		cfg.TLSClientConfig.ServerName = ""
	}
	// 适度调高 QPS/Burst，适应 Informer + reconcile 并发。
	cfg.QPS = 50
	cfg.Burst = 100
	cfg.Timeout = 30 * time.Second
	return cfg, nil
}

// ExtractAPIServerFromKubeconfig 从 kubeconfig 字节解析当前 context 的 API Server 地址。
// 用于创建集群时自动回填 api_server 字段（用户无需手工填写）。
// 解析失败返回空字符串，由调用方决定是否报错。
func ExtractAPIServerFromKubeconfig(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	config, err := clientcmd.Load(raw)
	if err != nil {
		return ""
	}
	ctx, ok := config.Contexts[config.CurrentContext]
	if !ok || ctx == nil {
		return ""
	}
	cluster, ok := config.Clusters[ctx.Cluster]
	if !ok || cluster == nil {
		return ""
	}
	return cluster.Server
}

// BuildFromAPIServer 从 API Server 地址 + 不跳过 TLS（用于纯探测场景）。
func BuildFromAPIServer(apiServer string, insecure bool) (*rest.Config, error) {
	if apiServer == "" {
		return nil, errors.New("empty api server")
	}
	cfg := &rest.Config{Host: apiServer, QPS: 50, Burst: 100, Timeout: 30 * time.Second}
	if insecure {
		cfg.TLSClientConfig.Insecure = true
	}
	return cfg, nil
}

// GetOrCreate 为指定集群构建并缓存客户端。rawKubeconfig 为解密后的字节。
// insecureSkipTLS 对应 vo_clusters.insecure_skip_tls，开发环境可跳过证书主机名校验。
func (p *ClientPool) GetOrCreate(clusterID int64, rawKubeconfig []byte, insecureSkipTLS bool) (*ClientEntry, error) {
	p.mu.RLock()
	if e, ok := p.clients[clusterID]; ok {
		p.mu.RUnlock()
		return e, nil
	}
	p.mu.RUnlock()

	cfg, err := BuildFromKubeconfig(rawKubeconfig, insecureSkipTLS)
	if err != nil {
		return nil, err
	}
	return p.buildAndStore(clusterID, cfg)
}

// GetOrCreateFromConfig 为指定集群用已构建的 rest.Config 缓存客户端。
func (p *ClientPool) GetOrCreateFromConfig(clusterID int64, cfg *rest.Config) (*ClientEntry, error) {
	p.mu.RLock()
	if e, ok := p.clients[clusterID]; ok {
		p.mu.RUnlock()
		return e, nil
	}
	p.mu.RUnlock()
	return p.buildAndStore(clusterID, cfg)
}

func (p *ClientPool) buildAndStore(clusterID int64, cfg *rest.Config) (*ClientEntry, error) {
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("build clientset: %w", err)
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("build dynamic client: %w", err)
	}
	entry := &ClientEntry{
		Clientset:  cs,
		Dynamic:    dyn,
		RestConfig: cfg,
		APIServer:  cfg.Host,
	}
	p.mu.Lock()
	// double-check：并发场景下可能已被其他 goroutine 写入。
	if existing, ok := p.clients[clusterID]; ok {
		p.mu.Unlock()
		return existing, nil
	}
	p.clients[clusterID] = entry
	p.mu.Unlock()
	return entry, nil
}

// Get 取已缓存客户端（不构建）。
func (p *ClientPool) Get(clusterID int64) (*ClientEntry, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	e, ok := p.clients[clusterID]
	if !ok {
		return nil, ErrClusterClientNotFound
	}
	return e, nil
}

// Remove 移除并失效集群客户端（kubeconfig 轮换或集群删除时调用）。
func (p *ClientPool) Remove(clusterID int64) {
	p.mu.Lock()
	delete(p.clients, clusterID)
	p.mu.Unlock()
}

// Count 返回池中客户端数量。
func (p *ClientPool) Count() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.clients)
}
