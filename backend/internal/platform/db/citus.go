// Package db 的 Citus 分片路由辅助（workspace_id 分片键）。
package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ShardRouter Citus 分片路由辅助。当 ReadReplicaHost 配置时，读操作可走副本。
type ShardRouter struct {
	primary *pgxpool.Pool
	replica *pgxpool.Pool
	enabled bool
}

// NewShardRouter 创建分片路由器。replica 可为 nil（仅主库）。
func NewShardRouter(primary *pgxpool.Pool, replica *pgxpool.Pool) *ShardRouter {
	return &ShardRouter{primary: primary, replica: replica, enabled: replica != nil}
}

// Primary 返回主库连接池（写操作与强一致读）。
func (r *ShardRouter) Primary() *pgxpool.Pool { return r.primary }

// Reader 返回读库连接池；未配置副本时回落主库。
func (r *ShardRouter) Reader() *pgxpool.Pool {
	if r.replica != nil {
		return r.replica
	}
	return r.primary
}

// HasReadReplica 是否配置了读副本。
func (r *ShardRouter) HasReadReplica() bool { return r.enabled }

// WorkspaceShardKey 返回 Citus 分片键（workspace_id）。
func WorkspaceShardKey(workspaceID int64) int64 {
	if workspaceID < 0 {
		return 0
	}
	return workspaceID
}

// SetLocalShard 在事务/连接上设置 Citus 本地分片上下文（按 workspace_id 路由）。
// 调用方应在写操作前执行，确保路由到正确 worker。
func SetLocalShard(ctx context.Context, pool *pgxpool.Pool, workspaceID int64) error {
	key := WorkspaceShardKey(workspaceID)
	_, err := pool.Exec(ctx, fmt.Sprintf("SET LOCAL citus.shard_key = '%d'", key))
	if err != nil {
		return fmt.Errorf("set citus shard key: %w", err)
	}
	return nil
}

// DistributionHint 返回 create_distributed_table 建议的分片键列名。
func DistributionHint(table string) string {
	switch table {
	case "vo_applications", "vo_application_groups", "vo_alert_rules", "vo_alert_events":
		return "workspace_id"
	case "vo_clusters", "vo_cluster_namespaces":
		return "cluster_id"
	default:
		return "workspace_id"
	}
}
