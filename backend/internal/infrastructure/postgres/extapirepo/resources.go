package extapirepo

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// GetClusterByUUID 按 UUID 查询集群 ID。
func (r *Repository) GetClusterByUUID(ctx context.Context, clusterUUID uuid.UUID) (int64, error) {
	var id int64
	err := r.pool.QueryRow(ctx,
		`SELECT id FROM vo_clusters WHERE uuid=$1 AND deleted=false`, clusterUUID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("cluster not found")
	}
	return id, err
}

// GetRegistryByUUID 按 UUID 查询镜像仓库 ID。
func (r *Repository) GetRegistryByUUID(ctx context.Context, registryUUID uuid.UUID) (int64, error) {
	var id int64
	err := r.pool.QueryRow(ctx,
		`SELECT id FROM vo_registries WHERE uuid=$1 AND deleted=false`, registryUUID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("registry not found")
	}
	return id, err
}
