// Package main 是 VortexOps syncer 进程入口。
// syncer 负责：按分片 Watch 被管集群的 K8s 资源，把运行态写入 Redis 缓存。
// 分片策略：通过环境变量 SYNCER_SHARD_ID 标识本实例负责的分片；
// 所有 active 集群按 id 哈希均匀分配到 N 个分片（N 由 SYNCER_SHARD_COUNT 指定）。
// Phase 12：ShardRegistry 在 Redis 登记实例分片归属，rebalance 周期检测集群数变化并触发再平衡。
// 每个分片通过 K8s Lease 选举单主，保证同一分片同时只有一个 syncer 运行 Informer。
//
// 运行：vortexops-syncer serve
// 关停：SIGTERM/SIGINT → 停止 Informer → 释放 Lease → 退出。
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	goredis "github.com/redis/go-redis/v9"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/vortexops/vortexops/internal/application/clusteropsapp"
	"github.com/vortexops/vortexops/internal/application/collabapp"
	"github.com/vortexops/vortexops/internal/config"
	"github.com/vortexops/vortexops/internal/domain/application"
	"github.com/vortexops/vortexops/internal/domain/cluster"
	applicationrepo "github.com/vortexops/vortexops/internal/infrastructure/postgres/applicationrepo"
	collabrepo "github.com/vortexops/vortexops/internal/infrastructure/postgres/collabrepo"
	clusteropsrepo "github.com/vortexops/vortexops/internal/infrastructure/postgres/clusteropsrepo"
	clusterrepo "github.com/vortexops/vortexops/internal/infrastructure/postgres/clusterrepo"
	"github.com/vortexops/vortexops/internal/application/clusterapp"
	"github.com/vortexops/vortexops/internal/application/k8sapp"
	"github.com/vortexops/vortexops/internal/infrastructure/k8s"
	"github.com/vortexops/vortexops/internal/infrastructure/redis/runtime"
	"github.com/vortexops/vortexops/internal/platform/db"
	"github.com/vortexops/vortexops/internal/platform/logger"
	"github.com/vortexops/vortexops/internal/platform/redis"
	"github.com/vortexops/vortexops/internal/platform/security"
	"github.com/vortexops/vortexops/internal/version"
)

const envPrefix = "VORTEXOPS"

func main() {
	root := &cobra.Command{
		Use:   "vortexops-syncer",
		Short: "VortexOps cluster runtime syncer",
	}
	root.AddCommand(serveCmd(), versionCmd())
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print build version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(version.String())
		},
	}
}

func serveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the syncer",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(envPrefix, "")
			if err != nil {
				return err
			}
			log := logger.New(cfg.Log.Level, cfg.Log.Format)
			log.Info("starting vortexops syncer",
				"env", cfg.App.Environment, "version", version.Version, "commit", version.Commit)

			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()

			dbPool, err := db.New(ctx, cfg.DB)
			if err != nil {
				return fmt.Errorf("init db: %w", err)
			}
			defer dbPool.Close()

			rc, err := redis.New(ctx, cfg.Redis)
			if err != nil {
				return fmt.Errorf("init redis: %w", err)
			}
			defer rc.Close()

			cipher, err := security.NewFieldCipher(cfg.Security.EncryptionKey)
			if err != nil {
				return fmt.Errorf("init field cipher: %w", err)
			}

			clusterRepo := clusterrepo.New(dbPool.Pool)
			rtCache := runtime.New(rc.Universal)

			// 构建 Pod 异常通知依赖：syncer 复用 application.Repository（读应用元数据/成员）
			// 与 collabapp.Service（站内通知）。探活改为原生 K8s Probe，由 K8s 自身执行，
			// syncer 仅订阅 Pod/Event informer 事件经 hook 推送通知，不再主动拨测。
			appRepo := applicationrepo.New(dbPool.Pool)
			collabRepo := collabrepo.New(dbPool.Pool)
			collabSvc := collabapp.New(collabRepo)
			cooldown := runtime.NewCooldownStore(rc.Universal, 10*time.Minute)
			anomalyAdapter := buildPodAnomalyAdapter(appRepo, collabSvc, cooldown)

			// 节点/Pod 指标采样依赖：syncer 每 60s 对本分片集群调 Kubelet Summary API 写入采样表。
			clusterAppSvc := clusterapp.New(clusterRepo, cipher, k8s.NewClientPool())
			k8sAppSvc := k8sapp.New(clusterAppSvc)
			clusterOpsRepo := clusteropsrepo.New(dbPool.Pool)
			clusterOpsSvc := clusteropsapp.New(clusterOpsRepo, clusterAppSvc, k8sAppSvc, collabSvc, appRepo)
			clusterOpsSvc.SetMetricsRepo(clusterOpsRepo)

			// dev 模式：跳过 in-cluster Lease 选举，直接以单实例运行（本地 docker-compose 无 K8s ServiceAccount）。
			// 生产环境仍走 Lease 选举，保证同一分片同时只有一个 syncer 运行 Informer。
			if os.Getenv("SYNCER_DISABLE_LEADER_ELECTION") == "true" {
				log.Warn("leader election disabled (dev mode), running as standalone")
				identity := identity()
				shardID, shardCount := shardConfig()
				log.Info("syncer shard config", "shard_id", shardID, "shard_count", shardCount, "identity", identity)
				syncer := &Syncer{
					log: log, repo: clusterRepo, cipher: cipher, rtCache: rtCache,
					resolver: &noopGroupResolver{}, shardID: shardID, shardCount: shardCount,
					registry: nil, managers: make(map[int64]*k8s.InformerManager),
					appRepo: appRepo, anomalyAdapter: anomalyAdapter, cooldown: cooldown,
					clusterOpsSvc: clusterOpsSvc,
				}
				sigCh := make(chan os.Signal, 1)
				signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
				ctx2, cancel2 := context.WithCancel(ctx)
				go func() {
					sig := <-sigCh
					log.Info("signal received, shutting down syncer", "signal", sig.String())
					cancel2()
				}()
				return syncer.Run(ctx2)
			}

			// 平台所在集群的 in-cluster config（用于 Lease 选举）。
			platformCfg, err := rest.InClusterConfig()
			if err != nil {
				return fmt.Errorf("syncer must run in-cluster for leader election: %w", err)
			}
			platformClient, err := kubernetes.NewForConfig(platformCfg)
			if err != nil {
				return fmt.Errorf("build platform clientset: %w", err)
			}

			identity := identity()
			shardID, shardCount := shardConfig()
			leaseNS := os.Getenv("SYNCER_LEASE_NAMESPACE")
			if leaseNS == "" {
				leaseNS = "vortexops"
			}
			lockName := fmt.Sprintf("syncer-shard-%d", shardID)
			log.Info("syncer shard config", "shard_id", shardID, "shard_count", shardCount, "identity", identity, "lock", lockName)

			registry := newShardRegistry(rc.Universal, identity, shardID, shardCount)
			if err := registry.Register(ctx); err != nil {
				log.Warn("shard registry register failed", "err", err)
			}
			go func() {
				ticker := time.NewTicker(30 * time.Second)
				defer ticker.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
						if err := registry.Heartbeat(ctx); err != nil {
							log.Warn("shard registry heartbeat failed", "err", err)
						}
					}
				}
			}()

			syncer := &Syncer{
				log: log, repo: clusterRepo, cipher: cipher, rtCache: rtCache,
				resolver: &noopGroupResolver{}, shardID: shardID, shardCount: shardCount,
				registry: registry, managers: make(map[int64]*k8s.InformerManager),
				appRepo: appRepo, anomalyAdapter: anomalyAdapter, cooldown: cooldown,
				clusterOpsSvc: clusterOpsSvc,
			}

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				sig := <-sigCh
				log.Info("signal received, shutting down syncer", "signal", sig.String())
				cancel()
			}()

			lec := k8s.DefaultLeaderElectionConfig(leaseNS, lockName, identity)
			return k8s.RunLeaderElection(ctx, platformClient, lec,
				func(leadCtx context.Context) {
					log.Info("acquired shard leadership, starting informers")
					if err := syncer.Run(leadCtx); err != nil {
						log.Error("syncer run error", "err", err)
					}
				},
				func() {
					log.Info("lost shard leadership")
				},
			)
		},
	}
}

// identity 返回本实例唯一标识（pod 名优先，其次 hostname）。
func identity() string {
	if pod := os.Getenv("HOSTNAME"); pod != "" {
		return pod
	}
	host, err := os.Hostname()
	if err != nil {
		return fmt.Sprintf("syncer-%d", os.Getpid())
	}
	return host
}

func shardConfig() (shardID, shardCount int) {
	shardID = envInt("SYNCER_SHARD_ID", 0)
	shardCount = envInt("SYNCER_SHARD_COUNT", 1)
	if shardCount < 1 {
		shardCount = 1
	}
	return
}

func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	var n int
	fmt.Sscanf(v, "%d", &n)
	if n < 0 {
		return def
	}
	return n
}

// --- Syncer ---

// Syncer 协调分片内所有集群的 Informer 生命周期。
type Syncer struct {
	log        *logger.Logger
	repo       cluster.Repository
	cipher     *security.FieldCipher
	rtCache    *runtime.Cache
	resolver   k8s.GroupResolver
	shardID    int
	shardCount int
	registry   *ShardRegistry
	mu         sync.Mutex
	managers   map[int64]*k8s.InformerManager

	// Pod 异常通知依赖：informer 检测异常后经 anomalyAdapter（k8s.PodAnomalyHook）→ notifier 通知。
	// 探活改为原生 K8s Probe，syncer 不再主动拨测，故无需 probeReader/evaluator。
	appRepo        application.Repository
	anomalyAdapter *podAnomalyAdapter // 实现 k8s.PodAnomalyHook
	cooldown       *runtime.CooldownStore

	// 节点/Pod 指标采样：每 60s 对本分片集群调 CollectNodeMetrics；每日清理过期采样。
	clusterOpsSvc *clusteropsapp.Service
}

// ShardRegistry 在 Redis 登记 syncer 分片实例，供运维查询与 rebalance 协调。
type ShardRegistry struct {
	redis      goredis.UniversalClient
	instanceID string
	shardID    int
	shardCount int
}

func newShardRegistry(client goredis.UniversalClient, instanceID string, shardID, shardCount int) *ShardRegistry {
	return &ShardRegistry{redis: client, instanceID: instanceID, shardID: shardID, shardCount: shardCount}
}

func (sr *ShardRegistry) Register(ctx context.Context) error {
	key := fmt.Sprintf("syncer:shard:%d:instances", sr.shardID)
	return sr.redis.SAdd(ctx, key, sr.instanceID).Err()
}

func (sr *ShardRegistry) Heartbeat(ctx context.Context) error {
	key := fmt.Sprintf("syncer:instance:%s", sr.instanceID)
	return sr.redis.Set(ctx, key, fmt.Sprintf("shard=%d,count=%d", sr.shardID, sr.shardCount), 2*time.Minute).Err()
}

// RebalancePlan 描述分片再平衡计划（集群 ID 按 shardCount 重新分配）。
type RebalancePlan struct {
	ShardCount int
	Shards     map[int][]int64
}

func computeRebalancePlan(clusters []*cluster.Cluster, shardCount int) RebalancePlan {
	if shardCount < 1 {
		shardCount = 1
	}
	plan := RebalancePlan{ShardCount: shardCount, Shards: make(map[int][]int64)}
	for _, c := range clusters {
		shard := int(c.ID) % shardCount
		plan.Shards[shard] = append(plan.Shards[shard], c.ID)
	}
	for s := range plan.Shards {
		sort.Slice(plan.Shards[s], func(i, j int) bool { return plan.Shards[s][i] < plan.Shards[s][j] })
	}
	return plan
}

// Run 加载本分片负责的集群，为每个集群启动 InformerManager，阻塞直到 ctx 取消。
func (s *Syncer) Run(ctx context.Context) error {
	clusters, err := s.repo.ListActiveClusters(ctx)
	if err != nil {
		return fmt.Errorf("load active clusters: %w", err)
	}
	// 分片：cluster_id % shardCount == shardID。
	var mine []*cluster.Cluster
	for _, c := range clusters {
		if int(c.ID)%s.shardCount == s.shardID {
			mine = append(mine, c)
		}
	}
	s.log.Info("loaded clusters for shard", "total", len(clusters), "mine", len(mine), "shard_id", s.shardID)

	if len(mine) == 0 {
		s.log.Info("no clusters assigned to this shard, waiting")
		<-ctx.Done()
		return nil
	}

	pool := k8s.NewClientPool()
	errCh := make(chan error, len(mine))

	for _, c := range mine {
		raw, err := s.cipher.Decrypt(c.KubeconfigEncrypted)
		if err != nil {
			s.log.Error("decrypt kubeconfig failed, skipping cluster", "cluster_id", c.ID, "err", err)
			continue
		}
		entry, err := pool.GetOrCreate(c.ID, raw, c.InsecureSkipTLS)
		if err != nil {
			s.log.Error("build client failed, skipping cluster", "cluster_id", c.ID, "err", err)
			continue
		}
		namespaces, err := s.repo.ListNamespacesByCluster(ctx, c.ID)
		if err != nil {
			s.log.Error("load namespaces failed, falling back to all-namespaces watch", "cluster_id", c.ID, "err", err)
			namespaces = nil
		}
		mgr := k8s.NewInformerManager(c.ID, entry.Clientset, s.rtCache, s.resolver, namespaces)
		if s.anomalyAdapter != nil {
			mgr.WithPodAnomalyHook(s.anomalyAdapter)
		}
		s.mu.Lock()
		s.managers[c.ID] = mgr
		s.mu.Unlock()
		go func(clusterID int64, m *k8s.InformerManager) {
			s.log.Info("starting informer for cluster", "cluster_id", clusterID)
			if err := m.Start(ctx); err != nil {
				errCh <- fmt.Errorf("informer cluster %d: %w", clusterID, err)
			}
		}(c.ID, mgr)
	}

	go s.reconcileLoop(ctx, pool)
	// 节点/Pod 指标采样：60s 一次，对本分片所有集群调 CollectNodeMetrics。
	go s.metricsLoop(ctx)
	// 启动时清理一次过期采样，之后每 24h 清理一次。
	go s.metricsCleanupLoop(ctx)

	// 探活已改为原生 K8s Probe（由 K8s 自身执行），syncer 不再启动主动探活评估器。
	// Pod/Event 异常经 InformerManager.onPod/onEvent → podAnomalyAdapter → PodAnomalyNotifier 推送通知。

	select {
	case <-ctx.Done():
		s.log.Info("syncer context done, stopping")
	case err := <-errCh:
		s.log.Error("informer error", "err", err)
	}
	return nil
}

// reconcileLoop 周期校正本分片集群集合并输出 rebalance 计划。
// 当 SYNCER_SHARD_COUNT 变更时，运维按 computeRebalancePlan 输出调整各实例 SYNCER_SHARD_ID。
func (s *Syncer) reconcileLoop(ctx context.Context, pool *k8s.ClientPool) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	var lastTotal int
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			clusters, err := s.repo.ListActiveClusters(ctx)
			if err != nil {
				s.log.Error("reconcile: load clusters failed", "err", err)
				continue
			}
			if len(clusters) != lastTotal {
				plan := computeRebalancePlan(clusters, s.shardCount)
				s.log.Info("rebalance plan updated", "total_clusters", len(clusters), "shard_count", plan.ShardCount,
					"this_shard_clusters", len(plan.Shards[s.shardID]))
				lastTotal = len(clusters)
			}
			liveIDs := make(map[int64]bool)
			for _, c := range clusters {
				if int(c.ID)%s.shardCount != s.shardID {
					continue
				}
				liveIDs[c.ID] = true
				if _, err := pool.Get(c.ID); err != nil {
					raw, derr := s.cipher.Decrypt(c.KubeconfigEncrypted)
					if derr == nil {
						if _, err := pool.GetOrCreate(c.ID, raw, c.InsecureSkipTLS); err == nil {
							s.log.Info("reconcile: added cluster client", "cluster_id", c.ID)
						}
					}
				}
			}
			s.mu.Lock()
			for id := range s.managers {
				if !liveIDs[id] {
					delete(s.managers, id)
					pool.Remove(id)
					s.log.Info("reconcile: removed stale cluster", "cluster_id", id)
				}
			}
			s.mu.Unlock()
			if s.registry != nil {
				_ = s.registry.Heartbeat(ctx)
			}
			s.log.Info("reconcile complete", "live_in_shard", len(liveIDs))
		}
	}
}

// metricsLoop 每 60s 对本分片所有集群采集一次节点/Pod 指标。
// 采集失败仅记日志，不中断循环；单集群失败不影响其他集群。
func (s *Syncer) metricsLoop(ctx context.Context) {
	if s.clusterOpsSvc == nil {
		return
	}
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	// 启动后先采集一次（不阻塞，给 informer 一点初始化时间）。
	time.Sleep(10 * time.Second)
	s.collectOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.collectOnce(ctx)
		}
	}
}

func (s *Syncer) collectOnce(ctx context.Context) {
	clusters, err := s.repo.ListActiveClusters(ctx)
	if err != nil {
		s.log.Error("metrics: load clusters failed", "err", err)
		return
	}
	for _, c := range clusters {
		if int(c.ID)%s.shardCount != s.shardID {
			continue
		}
		// 每个集群独立超时，避免单个慢集群拖累整体。
		cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
		if err := s.clusterOpsSvc.CollectNodeMetrics(cctx, c.ID); err != nil {
			s.log.Warn("metrics: collect failed", "cluster_id", c.ID, "err", err)
		}
		cancel()
	}
}

// metricsCleanupLoop 启动时清理一次过期采样，之后每 24h 清理一次。
func (s *Syncer) metricsCleanupLoop(ctx context.Context) {
	if s.clusterOpsSvc == nil {
		return
	}
	if err := s.clusterOpsSvc.CleanupOldMetrics(ctx); err != nil {
		s.log.Warn("metrics: initial cleanup failed", "err", err)
	}
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.clusterOpsSvc.CleanupOldMetrics(ctx); err != nil {
				s.log.Warn("metrics: cleanup failed", "err", err)
			}
		}
	}
}

// noopGroupResolver 占位解析器（Phase 5 releaseapp 提供完整实现，按 deployment_name 反查 group_id）。
type noopGroupResolver struct{}

func (n *noopGroupResolver) ResolveByWorkload(ctx context.Context, clusterID int64, namespace, name string) (int64, bool) {
	return 0, false
}
