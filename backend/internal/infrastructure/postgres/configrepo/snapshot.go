package configrepo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	configdomain "github.com/vortexops/vortexops/internal/domain/config"
)

func scanContentSnapshot(row pgx.Row) (*configdomain.ContentSnapshot, error) {
	s := &configdomain.ContentSnapshot{}
	var targetType, changeReason, filesHash string
	var content []byte
	var createdBy *int64
	if err := row.Scan(
		&s.ID, &targetType, &s.TargetID, &s.SnapshotNo, &content, &changeReason, &filesHash, &createdBy, &s.CreatedAt,
	); err != nil {
		return nil, err
	}
	s.TargetType = configdomain.SnapshotTargetType(targetType)
	s.ChangeReason = changeReason
	s.FilesHash = filesHash
	if content != nil {
		_ = json.Unmarshal(content, &s.Content)
	}
	if createdBy != nil {
		s.CreatedBy = *createdBy
	}
	return s, nil
}

// CreateContentSnapshot 写入配置内容快照。
func (r *Repository) CreateContentSnapshot(ctx context.Context, s *configdomain.ContentSnapshot) error {
	if s.Content == nil {
		s.Content = map[string]any{}
	}
	contentBytes, _ := json.Marshal(s.Content)
	if s.CreatedAt.IsZero() {
		s.CreatedAt = r.now()
	}
	err := r.pool.QueryRow(ctx, `
INSERT INTO vo_config_content_snapshots (target_type, target_id, snapshot_no, content, change_reason, files_hash, created_by, created_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`,
		string(s.TargetType), s.TargetID, s.SnapshotNo, contentBytes, s.ChangeReason, nullableStr(s.FilesHash),
		nullableInt64(s.CreatedBy), s.CreatedAt,
	).Scan(&s.ID)
	if err != nil {
		return fmt.Errorf("insert content snapshot: %w", err)
	}
	return nil
}

// ListContentSnapshots 按目标列出快照（新→旧）。
func (r *Repository) ListContentSnapshots(ctx context.Context, targetType configdomain.SnapshotTargetType, targetID int64) ([]*configdomain.ContentSnapshot, error) {
	rows, err := r.pool.Query(ctx, `
SELECT id, target_type, target_id, snapshot_no, content, change_reason, files_hash, created_by, created_at
FROM vo_config_content_snapshots
WHERE target_type=$1 AND target_id=$2
ORDER BY snapshot_no DESC`, string(targetType), targetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*configdomain.ContentSnapshot
	for rows.Next() {
		s, err := scanContentSnapshot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// GetContentSnapshot 按 ID 取快照。
func (r *Repository) GetContentSnapshot(ctx context.Context, id int64) (*configdomain.ContentSnapshot, error) {
	row := r.pool.QueryRow(ctx, `
SELECT id, target_type, target_id, snapshot_no, content, change_reason, files_hash, created_by, created_at
FROM vo_config_content_snapshots WHERE id=$1`, id)
	s, err := scanContentSnapshot(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, configdomain.ErrSnapshotNotFound
		}
		return nil, err
	}
	return s, nil
}

// NextSnapshotNo 返回下一快照序号。
func (r *Repository) NextSnapshotNo(ctx context.Context, targetType configdomain.SnapshotTargetType, targetID int64) (int, error) {
	var maxNo *int
	err := r.pool.QueryRow(ctx, `
SELECT MAX(snapshot_no) FROM vo_config_content_snapshots WHERE target_type=$1 AND target_id=$2`,
		string(targetType), targetID).Scan(&maxNo)
	if err != nil {
		return 0, err
	}
	if maxNo == nil {
		return 1, nil
	}
	return *maxNo + 1, nil
}
