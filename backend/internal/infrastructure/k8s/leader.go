package k8s

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

// LeaderElectionConfig Leader 选举配置。
type LeaderElectionConfig struct {
	// LockNamespace Lease 所在 namespace（平台所在集群）。
	LockNamespace string
	// LockName 租约名，通常按分片命名（如 syncer-shard-0）。
	LockName string
	// Identity 本实例唯一标识（pod 名或 hostname）。
	Identity string
	// LeaseDuration 租约时长。
	LeaseDuration time.Duration
	// RenewDeadline 续约截止。
	RenewDeadline time.Duration
	// RetryPeriod 重试周期。
	RetryPeriod time.Duration
}

// DefaultLeaderElectionConfig 返回生产级默认配置。
func DefaultLeaderElectionConfig(lockNS, lockName, identity string) LeaderElectionConfig {
	return LeaderElectionConfig{
		LockNamespace: lockNS,
		LockName:      lockName,
		Identity:      identity,
		LeaseDuration: 15 * time.Second,
		RenewDeadline: 10 * time.Second,
		RetryPeriod:   2 * time.Second,
	}
}

// RunLeaderElection 在 platformClient（平台所在集群的 clientset）上运行 Leader 选举。
// 成为 leader 时调用 onStartedLeading（阻塞运行，直到失去主或 ctx 取消）；
// 失去主时调用 onStoppedLeading。本函数阻塞直到 ctx 取消。
func RunLeaderElection(ctx context.Context, platformClient kubernetes.Interface, cfg LeaderElectionConfig, onStartedLeading func(context.Context), onStoppedLeading func()) error {
	if cfg.LockNamespace == "" {
		return fmt.Errorf("lock namespace is required")
	}
	if cfg.LockName == "" {
		return fmt.Errorf("lock name is required")
	}
	if cfg.Identity == "" {
		return fmt.Errorf("identity is required")
	}

	lock := &resourcelock.LeaseLock{
		LeaseMeta: metav1.ObjectMeta{
			Name:      cfg.LockName,
			Namespace: cfg.LockNamespace,
		},
		Client:     platformClient.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{Identity: cfg.Identity},
	}

	leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
		Lock:            lock,
		ReleaseOnCancel: true,
		LeaseDuration:   cfg.LeaseDuration,
		RenewDeadline:   cfg.RenewDeadline,
		RetryPeriod:     cfg.RetryPeriod,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading:  onStartedLeading,
			OnStoppedLeading:  onStoppedLeading,
		},
	})
	return nil
}
