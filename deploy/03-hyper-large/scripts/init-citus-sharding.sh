#!/usr/bin/env bash
# =============================================================================
# Citus 分片初始化与扩容
# 用法:
#   ./scripts/init-citus-sharding.sh --coordinator <svc> --workers <n>
#   ./scripts/init-citus-sharding.sh --add-worker <pod>
#   ./scripts/init-citus-sharding.sh --rebalance
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
LAYER_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
MANIFESTS_DIR="$LAYER_DIR/manifests"

NAMESPACE="vortexops-infra"
COORDINATOR_SVC="vortexops-citus-coordinator"
PG_USER="vortexops"
PG_DB="vortexops"
ACTION="init"
WORKER_COUNT=8
ADD_WORKER_POD=""
DRY_RUN=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --coordinator)  COORDINATOR_SVC="$2"; shift 2 ;;
    --workers)      WORKER_COUNT="$2"; shift 2 ;;
    --add-worker)   ADD_WORKER_POD="$2"; ACTION="add-worker"; shift 2 ;;
    --rebalance)    ACTION="rebalance"; shift ;;
    --namespace|-n) NAMESPACE="$2"; shift 2 ;;
    --user)         PG_USER="$2"; shift 2 ;;
    --db)           PG_DB="$2"; shift 2 ;;
    --dry-run)      DRY_RUN=true; shift ;;
    -h|--help)
      cat <<EOF
用法: $0 [OPTIONS]
选项:
  --coordinator <svc>   Coordinator Service 名 (默认: vortexops-citus-coordinator)
  --workers <n>         Worker 数量 (默认: 8)
  --add-worker <pod>    添加新 worker 节点
  --rebalance           触发数据再平衡
  --namespace, -n <ns>  命名空间 (默认: vortexops-infra)
  --user <u>            PG 用户 (默认: vortexops)
  --db <d>              PG 数据库 (默认: vortexops)
  --dry-run             仅打印 SQL，不执行
EOF
      exit 0 ;;
    *) echo "未知参数: $1"; exit 1 ;;
  esac
done

command -v kubectl >/dev/null 2>&1 || { echo "ERROR: 需安装 kubectl"; exit 1; }

# 获取 coordinator pod
COORD_POD=$(kubectl -n "$NAMESPACE" get pod -l app.kubernetes.io/name=citus,app.kubernetes.io/component=coordinator -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")
if [[ -z "$COORD_POD" ]]; then
  echo "ERROR: 未找到 coordinator pod，请确认 Citus 已部署"
  exit 1
fi

echo "============================================"
echo " Citus 分片管理"
echo "============================================"
echo " Coordinator Pod: $COORD_POD"
echo " Action:          $ACTION"
echo " Namespace:       $NAMESPACE"
echo "--------------------------------------------"

# 执行 SQL 的封装函数
exec_sql() {
  local sql="$1"
  if [[ "$DRY_RUN" == "true" ]]; then
    echo "[dry-run] SQL:"
    echo "$sql"
  else
    kubectl -n "$NAMESPACE" exec -i "$COORD_POD" -- \
      psql -U "$PG_USER" -d "$PG_DB" -c "$sql"
  fi
}

case "$ACTION" in
  init)
    # 1. 添加 worker 节点
    echo ""
    echo "[init] 添加 $WORKER_COUNT 个 worker 节点..."
    for i in $(seq 1 "$WORKER_COUNT"); do
      WORKER_HOST="vortexops-citus-worker-$((i-1)).vortexops-citus-worker.$NAMESPACE.svc"
      echo "  → 添加 worker $i: $WORKER_HOST"
      exec_sql "SELECT * FROM citus_add_node('$WORKER_HOST', 5432);" || \
        echo "  ⚠️  worker $i 可能已存在，跳过"
    done

    # 2. 查看集群拓扑
    echo ""
    echo "[init] Citus 集群拓扑:"
    exec_sql "SELECT nodeid, nodename, nodetype, isactive FROM pg_dist_node ORDER BY nodeid;"

    # 3. 配置分片表（核心业务表）
    echo ""
    echo "[init] 配置分片表..."
    cat <<'SQL'
-- apps 表按 tenant_id 哈希分片（48 分片）
SELECT create_distributed_table('apps', 'tenant_id', shard_count := 48);
-- deployments 表按 tenant_id 哈希分片
SELECT create_distributed_table('deployments', 'tenant_id', shard_count := 48);
-- pods 表按 tenant_id 哈希分片（高频写入）
SELECT create_distributed_table('pods', 'tenant_id', shard_count := 96);
-- events 表按 tenant_id 哈希分片（最大写入量）
SELECT create_distributed_table('events', 'tenant_id', shard_count := 96);
-- audit_logs 按 created_at 范围分片（时序数据）
SELECT create_distributed_table('audit_logs', 'created_at', shard_count := 48, partition_type := 'range');

-- 共享维表（小表广播到所有节点）
SELECT create_reference_table('tenants');
SELECT create_reference_table('clusters');
SELECT create_reference_table('users');
SELECT create_reference_table('roles');
SQL
    SQL_CONTENT=$(cat <<'SQL'
SELECT create_distributed_table('apps', 'tenant_id', shard_count := 48);
SELECT create_distributed_table('deployments', 'tenant_id', shard_count := 48);
SELECT create_distributed_table('pods', 'tenant_id', shard_count := 96);
SELECT create_distributed_table('events', 'tenant_id', shard_count := 96);
SELECT create_reference_table('tenants');
SELECT create_reference_table('clusters');
SELECT create_reference_table('users');
SELECT create_reference_table('roles');
SQL
)
    if [[ "$DRY_RUN" == "true" ]]; then
      echo "$SQL_CONTENT"
    else
      echo "$SQL_CONTENT" | kubectl -n "$NAMESPACE" exec -i "$COORD_POD" -- \
        psql -U "$PG_USER" -d "$PG_DB"
    fi

    # 4. 验证分片
    echo ""
    echo "[init] 分片分布:"
    exec_sql "SELECT logicalrelid, count(*) AS shards FROM pg_dist_shard GROUP BY logicalrelid ORDER BY logicalrelid;"

    echo ""
    echo "[init] Citus 分片初始化完成"
    ;;

  add-worker)
    if [[ -z "$ADD_WORKER_POD" ]]; then
      echo "ERROR: 请通过 --add-worker 指定 pod"
      exit 1
    fi
    WORKER_HOST="$ADD_WORKER_POD.vortexops-citus-worker.$NAMESPACE.svc"
    echo "[add-worker] 添加 worker: $WORKER_HOST"
    exec_sql "SELECT * FROM citus_add_node('$WORKER_HOST', 5432);"
    echo "[add-worker] 触发再平衡..."
    exec_sql "SELECT rebalance_table_shards();"
    ;;

  rebalance)
    echo "[rebalance] 触发数据再平衡（可能耗时较长）..."
    exec_sql "SELECT rebalance_table_shards();"
    echo "[rebalance] 再平衡完成"
    echo ""
    echo "[rebalance] 当前分片分布:"
    exec_sql "SELECT logicalrelid, nodename, count(*) AS shards FROM pg_dist_shard_placement JOIN pg_dist_shard USING(shardid) JOIN pg_dist_node USING(nodeid) GROUP BY logicalrelid, nodename ORDER BY logicalrelid, nodename;"
    ;;
esac
