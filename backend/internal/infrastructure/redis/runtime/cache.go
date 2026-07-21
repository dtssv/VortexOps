// Package runtime 提供 Pod/Group 运行态的 Redis 缓存实现。
// syncer 通过 Informer 写入；apiserver 读出返回给前端。
// 键规范：
//   rt:pod:{clusterID}:{namespace}:{podName}      → PodSummary JSON
//   rt:podidx:{clusterID}:{namespace}             → SET of pod names（按 ns 索引）
//   rt:group:{clusterID}:{groupID}                → GroupRuntime JSON
//   rt:ip:{clusterID}:{ip}                        → pod name（IP 反查）
//   rt:cluster:{clusterID}                        → HASH {pod_count, updated_at}（集群摘要）
// 所有键设 TTL（默认 10 分钟），Informer 事件持续刷新；宕机后由 resync 重建。
package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/vortexops/vortexops/internal/infrastructure/k8s"
)

const (
	keyTTL          = 10 * time.Minute
	podKeyPrefix    = "rt:pod:"
	podIdxPrefix    = "rt:podidx:"
	groupKeyPrefix  = "rt:group:"
	ipKeyPrefix     = "rt:ip:"
	clusterKeyPrefix = "rt:cluster:"
)

// Cache 实现 k8s.RuntimeCache 的 Redis 版本。
type Cache struct {
	rdb goredis.UniversalClient
}

// New 创建运行态缓存。
func New(rdb goredis.UniversalClient) *Cache {
	return &Cache{rdb: rdb}
}

func podKey(clusterID int64, ns, name string) string {
	return fmt.Sprintf("%s%d:%s:%s", podKeyPrefix, clusterID, ns, name)
}

func podIdxKey(clusterID int64, ns string) string {
	return fmt.Sprintf("%s%d:%s", podIdxPrefix, clusterID, ns)
}

func groupKey(clusterID, groupID int64) string {
	return fmt.Sprintf("%s%d:%d", groupKeyPrefix, clusterID, groupID)
}

func ipKey(clusterID int64, ip string) string {
	return fmt.Sprintf("%s%d:%s", ipKeyPrefix, clusterID, ip)
}

func clusterKey(clusterID int64) string {
	return fmt.Sprintf("%s%d", clusterKeyPrefix, clusterID)
}

// UpsertPod 写入 Pod 摘要 + 维护 namespace 索引 + IP 反查索引。
func (c *Cache) UpsertPod(ctx context.Context, clusterID int64, ns string, pod *k8s.PodSummary) error {
	if pod == nil {
		return nil
	}
	pipe := c.rdb.TxPipeline()
	data, _ := json.Marshal(pod)
	pipe.Set(ctx, podKey(clusterID, ns, pod.Name), data, keyTTL)
	pipe.SAdd(ctx, podIdxKey(clusterID, ns), pod.Name)
	pipe.Expire(ctx, podIdxKey(clusterID, ns), keyTTL)
	if pod.PodIP != "" {
		pipe.Set(ctx, ipKey(clusterID, pod.PodIP), pod.Name, keyTTL)
	}
	_, err := pipe.Exec(ctx)
	return err
}

// DeletePod 删除 Pod 摘要 + 索引。
func (c *Cache) DeletePod(ctx context.Context, clusterID int64, ns, name string) error {
	pipe := c.rdb.TxPipeline()
	pipe.Del(ctx, podKey(clusterID, ns, name))
	pipe.SRem(ctx, podIdxKey(clusterID, ns), name)
	_, err := pipe.Exec(ctx)
	return err
}

// UpsertGroupRuntime 写入分组运行态摘要。
func (c *Cache) UpsertGroupRuntime(ctx context.Context, clusterID, groupID int64, summary *k8s.GroupRuntime) error {
	if summary == nil {
		return nil
	}
	data, _ := json.Marshal(summary)
	return c.rdb.Set(ctx, groupKey(clusterID, groupID), data, keyTTL).Err()
}

// DeleteGroupRuntime 删除分组运行态。
func (c *Cache) DeleteGroupRuntime(ctx context.Context, clusterID, groupID int64) error {
	return c.rdb.Del(ctx, groupKey(clusterID, groupID)).Err()
}

// --- 读 API（供 apiserver 用） ---

// GetGroupRuntime 读分组运行态。
func (c *Cache) GetGroupRuntime(ctx context.Context, clusterID, groupID int64) (*k8s.GroupRuntime, error) {
	data, err := c.rdb.Get(ctx, groupKey(clusterID, groupID)).Bytes()
	if err != nil {
		if err == goredis.Nil {
			return nil, nil
		}
		return nil, err
	}
	var gr k8s.GroupRuntime
	if err := json.Unmarshal(data, &gr); err != nil {
		return nil, err
	}
	return &gr, nil
}

// ListPodsByNamespace 列出某 namespace 下所有 Pod 摘要。
func (c *Cache) ListPodsByNamespace(ctx context.Context, clusterID int64, ns string) ([]*k8s.PodSummary, error) {
	names, err := c.rdb.SMembers(ctx, podIdxKey(clusterID, ns)).Result()
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return []*k8s.PodSummary{}, nil
	}
	keys := make([]string, 0, len(names))
	for _, n := range names {
		keys = append(keys, podKey(clusterID, ns, n))
	}
	vals, err := c.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}
	out := make([]*k8s.PodSummary, 0, len(vals))
	for _, v := range vals {
		s, ok := v.(string)
		if !ok {
			continue
		}
		var p k8s.PodSummary
		if err := json.Unmarshal([]byte(s), &p); err == nil {
			out = append(out, &p)
		}
	}
	return out, nil
}

// GetPodByIP 按 Pod IP 反查 Pod 名（keep_pod_ip 与运维定位用）。
func (c *Cache) GetPodByIP(ctx context.Context, clusterID int64, ip string) (string, error) {
	name, err := c.rdb.Get(ctx, ipKey(clusterID, ip)).Result()
	if err != nil {
		if err == goredis.Nil {
			return "", nil
		}
		return "", err
	}
	return name, nil
}

// GetClusterSummary 读集群 Pod 计数摘要。
func (c *Cache) GetClusterSummary(ctx context.Context, clusterID int64) (map[string]string, error) {
	return c.rdb.HGetAll(ctx, clusterKey(clusterID)).Result()
}
