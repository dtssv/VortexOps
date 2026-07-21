#!/bin/sh
# VortexOps Underlay 集群自动配置（host-net 开发模式）。
# 等 apiserver 就绪后：
#   1. 将所有集群 network_profile 设为 large-underlay + macvlan
#   2. 创建 macvlan IP 池（物理网段）
#   3. 释放既有 group 稳定 IP 分配（便于重新发布时从 underlay 池分配）
#
# 运行（host-net 模式启动后）：
#   docker compose -f docker-compose.dev.yml -f docker-compose.host-net.yml run --rm underlay-seed
set -eu

API_BASE="${VORTEXOPS_API_BASE:-http://apiserver:8080/api/v1}"
ADMIN_USER="${VORTEXOPS_ADMIN_USER:-admin}"
ADMIN_PASS="${VORTEXOPS_ADMIN_PASSWORD:-admin123}"

SUBNET="${K3S_UNDERLAY_SUBNET:-192.168.1.0/24}"
GATEWAY="${K3S_UNDERLAY_GATEWAY:-192.168.1.1}"
PARENT_IFACE="${K3S_UNDERLAY_PARENT_IFACE:-eth0}"
RANGE_START="${K3S_UNDERLAY_RANGE_START:-192.168.1.200}"
POOL_CIDR="${VORTEXOPS_UNDERLAY_POOL_CIDR:-192.168.1.192/26}"
POOL_NAME="${VORTEXOPS_UNDERLAY_POOL_NAME:-underlay-pod-pool}"
SUPERNET_CIDR="${VORTEXOPS_UNDERLAY_SUPERNET_CIDR:-192.168.0.0/16}"

PGHOST="${VORTEXOPS_DB_HOST:-postgres}"
PGPORT="${VORTEXOPS_DB_PORT:-5432}"
PGUSER="${VORTEXOPS_DB_USERNAME:-vortexops}"
PGPASSWORD="${VORTEXOPS_DB_PASSWORD:-vortexops_dev}"
PGDATABASE="${VORTEXOPS_DB_DATABASE:-vortexops}"
export PGHOST PGPORT PGUSER PGPASSWORD PGDATABASE

if ! command -v curl >/dev/null 2>&1; then
  apk add --no-cache curl >/dev/null
fi

echo "[underlay-seed] Waiting for apiserver up to 180s ..."
elapsed=0
TOKEN=""
while [ "$elapsed" -lt 180 ]; do
  LOGIN_RESP=$(curl -sf -X POST "${API_BASE}/auth/login" \
    -H "Content-Type: application/json" \
    -d "{\"username\":\"${ADMIN_USER}\",\"password\":\"${ADMIN_PASS}\"}" 2>/dev/null || true)
  if [ -n "$LOGIN_RESP" ]; then
    TOKEN=$(echo "$LOGIN_RESP" | sed -n 's/.*"access_token":"\([^"]*\)".*/\1/p')
    if [ -n "$TOKEN" ]; then
      echo "[underlay-seed] apiserver ready."
      break
    fi
  fi
  sleep 5
  elapsed=$((elapsed + 5))
done

if [ -z "$TOKEN" ]; then
  echo "[underlay-seed] ERROR: apiserver not ready or login failed."
  exit 1
fi

AUTH="Authorization: Bearer ${TOKEN}"

echo "[underlay-seed] Updating cluster network_profile via DB (merge metadata) ..."
psql -v ON_ERROR_STOP=1 <<SQL
UPDATE vo_clusters
SET metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object(
      'network_profile', jsonb_build_object(
        'profile', 'large-underlay',
        'cni', 'macvlan',
        'cidr', '${SUBNET}',
        'supernet_cidr', '${SUPERNET_CIDR}',
        'parent_interface', '${PARENT_IFACE}',
        'gateway', '${GATEWAY}',
        'multus_enabled', true
      )
    ),
    version = version + 1,
    updated_at = now()
WHERE deleted = false;
SQL

echo "[underlay-seed] Listing clusters for IP pool setup ..."
CLUSTER_IDS=$(curl -sf "${API_BASE}/clusters?size=200" -H "${AUTH}" 2>/dev/null \
  | grep -o '"id":[0-9]*' | cut -d: -f2 | sort -u || true)

if [ -z "$CLUSTER_IDS" ]; then
  echo "[underlay-seed] WARN: no clusters found; register a cluster first, then re-run underlay-seed."
else
  for CLUSTER_ID in $CLUSTER_IDS; do
    POOLS=$(curl -sf "${API_BASE}/clusters/${CLUSTER_ID}/ip-pools" -H "${AUTH}" 2>/dev/null || echo '{"items":[]}')
    if echo "$POOLS" | grep -q '"provider":"macvlan"'; then
      echo "[underlay-seed] Cluster ${CLUSTER_ID}: macvlan IP pool exists, skip."
      continue
    fi
    echo "[underlay-seed] Cluster ${CLUSTER_ID}: creating macvlan IP pool (cidr=${POOL_CIDR}) ..."
    curl -sf -X POST "${API_BASE}/clusters/${CLUSTER_ID}/ip-pools" \
      -H "${AUTH}" -H "Content-Type: application/json" \
      -d "{\"name\":\"${POOL_NAME}\",\"cidr\":\"${POOL_CIDR}\",\"gateway\":\"${GATEWAY}\",\"provider\":\"macvlan\",\"metadata\":{\"parent_interface\":\"${PARENT_IFACE}\",\"range_start\":\"${RANGE_START}\"}}" \
      >/dev/null || echo "[underlay-seed] WARN: create IP pool on cluster ${CLUSTER_ID} failed"
  done
fi

echo "[underlay-seed] Releasing existing group stable IP allocations ..."
psql -v ON_ERROR_STOP=1 <<'SQL'
UPDATE vo_cluster_ip_pool_entries
SET status='free', resource_type=NULL, resource_id=NULL, replica_index=NULL, allocated_at=NULL, updated_at=now()
WHERE resource_type='group' AND status='allocated';

UPDATE vo_cluster_ip_allocations
SET status='released', released_at=now()
WHERE resource_type='group' AND status='allocated';

UPDATE vo_cluster_ip_pools p
SET allocated_count = COALESCE((
  SELECT COUNT(*)::int FROM vo_cluster_ip_pool_entries e
  WHERE e.ip_pool_id = p.id AND e.status = 'allocated'
), 0);
SQL

echo "[underlay-seed] Done."
echo "[underlay-seed] Next steps:"
echo "  1. Stack must run in host-net mode:"
echo "     docker compose -f docker-compose.dev.yml -f docker-compose.host-net.yml up -d"
echo "  2. Re-publish groups — Pods will get physical IPs from ${RANGE_START}+ (macvlan)."
