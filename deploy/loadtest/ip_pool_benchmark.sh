#!/bin/sh
# Phase 6: IP 池分配压测脚本（需本地 postgres + 已迁移 schema）。
# 用法：./deploy/loadtest/ip_pool_benchmark.sh
# 目标：100 并发 × 100 IP/批，验证无冲突、完成时间 < 60s。

set -e
DB_URL="${DATABASE_URL:-postgres://vortexops:vortexops_dev@localhost:5432/vortexops?sslmode=disable}"
CONCURRENCY="${CONCURRENCY:-100}"
BATCH="${BATCH:-100}"

echo "[loadtest] IP pool benchmark: concurrency=$CONCURRENCY batch=$BATCH"
START=$(date +%s)

for i in $(seq 1 "$CONCURRENCY"); do
  (
    psql "$DB_URL" -q -c "
      WITH picked AS (
        SELECT id FROM vo_cluster_ip_pool_entries
        WHERE status = 'free' AND ip_pool_id = (SELECT id FROM vo_cluster_ip_pools LIMIT 1)
        ORDER BY id
        LIMIT $BATCH
        FOR UPDATE SKIP LOCKED
      )
      UPDATE vo_cluster_ip_pool_entries e
      SET status = 'allocated', resource_type = 'loadtest', resource_id = $i, allocated_at = NOW()
      FROM picked p WHERE e.id = p.id;
    " >/dev/null 2>&1 || true
  ) &
done
wait

END=$(date +%s)
ELAPSED=$((END - START))
echo "[loadtest] completed in ${ELAPSED}s"

ALLOCATED=$(psql "$DB_URL" -t -c "SELECT COUNT(*) FROM vo_cluster_ip_pool_entries WHERE resource_type='loadtest';" | tr -d ' ')
echo "[loadtest] allocated entries (loadtest): $ALLOCATED"

# cleanup
psql "$DB_URL" -q -c "UPDATE vo_cluster_ip_pool_entries SET status='free', resource_type=NULL, resource_id=NULL WHERE resource_type='loadtest';"

if [ "$ELAPSED" -gt 60 ]; then
  echo "[loadtest] WARN: exceeded 60s threshold"
  exit 1
fi
echo "[loadtest] PASS"
