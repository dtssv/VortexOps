package dnsrepo

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	dnsdomain "github.com/vortexops/vortexops/internal/domain/dns"
)

// Repository DNS 记录仓储。
type Repository struct {
	pool *pgxpool.Pool
}

// New 创建仓储。
func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) now() time.Time { return time.Now().UTC() }

// UpsertRecord 创建或更新分组 DNS 记录。
func (r *Repository) UpsertRecord(ctx context.Context, rec *dnsdomain.Record) (*dnsdomain.Record, error) {
	now := r.now()
	if rec.TTL <= 0 {
		rec.TTL = dnsdomain.DefaultTTL
	}
	if rec.Zone == "" {
		rec.Zone = dnsdomain.DefaultZone
	}
	if rec.Type == "" {
		rec.Type = dnsdomain.RecordA
	}
	if rec.Status == "" {
		rec.Status = dnsdomain.StatusActive
	}
	row := r.pool.QueryRow(ctx, `
INSERT INTO vo_dns_records (group_id, cluster_id, zone, name, fqdn, record_type, ttl, status, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$9)
ON CONFLICT (group_id, fqdn) DO UPDATE SET
  cluster_id = EXCLUDED.cluster_id,
  zone = EXCLUDED.zone,
  name = EXCLUDED.name,
  record_type = EXCLUDED.record_type,
  ttl = EXCLUDED.ttl,
  status = EXCLUDED.status,
  updated_at = EXCLUDED.updated_at
RETURNING id, group_id, cluster_id, zone, name, fqdn, record_type, ttl, status, created_at, updated_at`,
		rec.GroupID, rec.ClusterID, rec.Zone, rec.Name, rec.FQDN, rec.Type, rec.TTL, rec.Status, now)
	return scanRecord(row)
}

// ReplaceBackends 替换记录的全部后端（先删后插）。
func (r *Repository) ReplaceBackends(ctx context.Context, recordID int64, backends []dnsdomain.Backend) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM vo_dns_backends WHERE record_id = $1`, recordID); err != nil {
		return err
	}
	now := r.now()
	for _, b := range backends {
		weight := b.Weight
		if weight <= 0 {
			weight = 100
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO vo_dns_backends (record_id, pod_ip, pod_name, healthy, weight, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$6)`,
			recordID, b.PodIP, b.PodName, b.Healthy, weight, now); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// GetRecordByGroupID 按分组 ID 取 DNS 记录。
func (r *Repository) GetRecordByGroupID(ctx context.Context, groupID int64) (*dnsdomain.Record, error) {
	row := r.pool.QueryRow(ctx, `
SELECT id, group_id, cluster_id, zone, name, fqdn, record_type, ttl, status, created_at, updated_at
FROM vo_dns_records WHERE group_id = $1 LIMIT 1`, groupID)
	return scanRecord(row)
}

// ListHealthyBackends 列出记录的健康后端 IP。
func (r *Repository) ListHealthyBackends(ctx context.Context, recordID int64) ([]dnsdomain.Backend, error) {
	rows, err := r.pool.Query(ctx, `
SELECT id, record_id, pod_ip, pod_name, healthy, weight, created_at, updated_at
FROM vo_dns_backends WHERE record_id = $1 AND healthy = true ORDER BY id`, recordID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []dnsdomain.Backend
	for rows.Next() {
		b, err := scanBackend(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *b)
	}
	return out, rows.Err()
}

// ListAllActiveRecords 列出全部活跃记录（CoreDNS 同步用）。
func (r *Repository) ListAllActiveRecords(ctx context.Context) ([]*dnsdomain.Record, error) {
	rows, err := r.pool.Query(ctx, `
SELECT id, group_id, cluster_id, zone, name, fqdn, record_type, ttl, status, created_at, updated_at
FROM vo_dns_records WHERE status = 'active' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*dnsdomain.Record
	for rows.Next() {
		rec, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// ListBackendsByRecordID 列出记录全部后端。
func (r *Repository) ListBackendsByRecordID(ctx context.Context, recordID int64) ([]dnsdomain.Backend, error) {
	rows, err := r.pool.Query(ctx, `
SELECT id, record_id, pod_ip, pod_name, healthy, weight, created_at, updated_at
FROM vo_dns_backends WHERE record_id = $1 ORDER BY id`, recordID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []dnsdomain.Backend
	for rows.Next() {
		b, err := scanBackend(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *b)
	}
	return out, rows.Err()
}

// MarkBackendHealth 更新后端健康状态。
func (r *Repository) MarkBackendHealth(ctx context.Context, recordID int64, podIP string, healthy bool) error {
	_, err := r.pool.Exec(ctx, `
UPDATE vo_dns_backends SET healthy = $3, updated_at = $4
WHERE record_id = $1 AND pod_ip = $2`, recordID, podIP, healthy, r.now())
	return err
}

// DeleteByGroupID 删除分组 DNS 记录（级联删 backends）。
func (r *Repository) DeleteByGroupID(ctx context.Context, groupID int64) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM vo_dns_records WHERE group_id = $1`, groupID)
	return err
}

func scanRecord(row pgx.Row) (*dnsdomain.Record, error) {
	rec := &dnsdomain.Record{}
	var recType, status string
	if err := row.Scan(
		&rec.ID, &rec.GroupID, &rec.ClusterID, &rec.Zone, &rec.Name, &rec.FQDN,
		&recType, &rec.TTL, &status, &rec.CreatedAt, &rec.UpdatedAt,
	); err != nil {
		return nil, err
	}
	rec.Type = dnsdomain.RecordType(recType)
	rec.Status = dnsdomain.RecordStatus(status)
	return rec, nil
}

func scanBackend(row pgx.Row) (*dnsdomain.Backend, error) {
	b := &dnsdomain.Backend{}
	if err := row.Scan(
		&b.ID, &b.RecordID, &b.PodIP, &b.PodName, &b.Healthy, &b.Weight, &b.CreatedAt, &b.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return b, nil
}
