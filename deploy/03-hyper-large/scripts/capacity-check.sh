#!/usr/bin/env bash
# =============================================================================
# 容量巡检与扩缩容建议（XL 大规模集群）
# 用法:
#   ./scripts/capacity-check.sh                    # 巡检并输出报告
#   ./scripts/capacity-check.sh --threshold 80     # 自定义告警阈值（百分比）
#   ./scripts/capacity-check.sh --json             # 输出 JSON
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

NAMESPACE="vortexops"
INFRA_NS="vortexops-infra"
THRESHOLD=80
OUTPUT="text"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --threshold)   THRESHOLD="$2"; shift 2 ;;
    --json)        OUTPUT="json"; shift ;;
    --namespace|-n) NAMESPACE="$2"; shift 2 ;;
    -h|--help)
      cat <<EOF
用法: $0 [OPTIONS]
选项:
  --threshold <pct>   告警阈值百分比 (默认: 80)
  --json              输出 JSON 格式
  --namespace, -n <ns> 命名空间
EOF
      exit 0 ;;
    *) echo "未知参数: $1"; exit 1 ;;
  esac
done

command -v kubectl >/dev/null 2>&1 || { echo "ERROR: 需安装 kubectl"; exit 1; }

# 巡检函数：返回某指标的当前值与上限
check_metric() {
  local name="$1"
  local current="$2"
  local limit="$3"
  local unit="${4:-}"
  local pct=0
  if [[ -n "$limit" && "$limit" != "0" ]]; then
    pct=$((current * 100 / limit))
  fi
  local status="OK"
  [[ "$pct" -ge "$THRESHOLD" ]] && status="WARN"
  [[ "$pct" -ge 95 ]] && status="CRITICAL"

  if [[ "$OUTPUT" == "json" ]]; then
    printf '  {"name":"%s","current":%s,"limit":%s,"pct":%s,"status":"%s","unit":"%s"},\n' \
      "$name" "$current" "$limit" "$pct" "$status" "$unit"
  else
    printf "  %-30s %10s / %-10s %-5s  [%3s%%]  %s\n" \
      "$name" "$current$unit" "$limit$unit" "" "$pct" "$status"
  fi
}

echo "============================================"
echo " VortexOps XL 容量巡检"
echo "============================================"
echo " 时间:      $(date -u '+%Y-%m-%d %H:%M:%S UTC')"
echo " 阈值:      WARN ≥ ${THRESHOLD}%  CRITICAL ≥ 95%"
echo "--------------------------------------------"

if [[ "$OUTPUT" == "json" ]]; then
  echo "{"
  echo '  "timestamp": "'$(date -u '+%Y-%m-%dT%H:%M:%SZ')'",'
  echo '  "threshold": '$THRESHOLD','
  echo '  "metrics": ['
fi

# 1. 业务集群规模
echo ""
if [[ "$OUTPUT" == "text" ]]; then
  echo "[1] 业务集群规模"
fi
TOTAL_CLUSTERS=$(kubectl -n "$NAMESPACE" exec deploy/vortexops-apiserver -- \
  wget -qO- http://localhost:8080/api/v1/admin/clusters/count 2>/dev/null | grep -oE '[0-9]+' || echo "0")
TOTAL_APPS=$(kubectl -n "$NAMESPACE" exec deploy/vortexops-apiserver -- \
  wget -qO- http://localhost:8080/api/v1/admin/apps/count 2>/dev/null | grep -oE '[0-9]+' || echo "0")
check_metric "业务集群数" "$TOTAL_CLUSTERS" "10000" ""
check_metric "托管应用数" "$TOTAL_APPS" "100000" ""

# 2. Citus 分片负载
echo ""
if [[ "$OUTPUT" == "text" ]]; then
  echo "[2] Citus 数据库"
fi
PG_SIZE_MB=$(kubectl -n "$INFRA_NS" exec deploy/vortexops-citus-coordinator -- \
  psql -U vortexops -d vortexops -t -c \
  "SELECT pg_size_pgm(pg_database_size('vortexops'));" 2>/dev/null | tr -d ' ' || echo "0")
check_metric "PG 数据量" "${PG_SIZE_MB}" "102400" "MB"

# 3. Redis 内存
echo ""
if [[ "$OUTPUT" == "text" ]]; then
  echo "[3] Redis Cluster"
fi
REDIS_MEM_MB=$(kubectl -n "$INFRA_NS" exec deploy/vortexops-redis-cluster -- \
  redis-cli -a "${REDIS_PASSWORD:-CHANGE_ME}" INFO memory 2>/dev/null | \
  grep used_memory: | awk -F: '{print int($2/1024/1024)}' || echo "0")
check_metric "Redis 已用内存" "${REDIS_MEM_MB}" "8192" "MB"

# 4. Kafka 消息堆积
echo ""
if [[ "$OUTPUT" == "text" ]]; then
  echo "[4] Kafka 消息堆积"
fi
KAFKA_LAG=$(kubectl -n "$INFRA_NS" exec deploy/vortexops-kafka-bootstrap -- \
  kafka-consumer-groups --bootstrap-server localhost:9092 --describe --group vortexops-syncer 2>/dev/null | \
  awk '{sum += $5} END {print sum}' || echo "0")
check_metric "syncer 消费延迟" "${KAFKA_LAG}" "100000" "msgs"

# 5. 对象存储
echo ""
if [[ "$OUTPUT" == "text" ]]; then
  echo "[5] 对象存储（MinIO）"
fi
MINIO_USAGE_GB=$(kubectl -n "$INFRA_NS" exec deploy/minio -- \
  mc admin info local 2>/dev/null | grep -oE '[0-9]+ GiB' | head -1 | grep -oE '[0-9]+' || echo "0")
check_metric "MinIO 已用" "${MINIO_USAGE_GB}" "10240" "GB"

# 6. OpenSearch 索引
echo ""
if [[ "$OUTPUT" == "text" ]]; then
  echo "[6] OpenSearch"
fi
OS_DOCS=$(kubectl -n "$INFRA_NS" exec deploy/vortexops-opensearch -- \
  curl -s -u admin:admin http://localhost:9200/_cat/count?v 2>/dev/null | tail -1 | awk '{print $3}' || echo "0")
check_metric "OS 文档数" "${OS_DOCS}" "1000000000" ""

# 7. Pod 资源使用
echo ""
if [[ "$OUTPUT" == "text" ]]; then
  echo "[7] 控制平面 Pod 资源"
fi
APISERVER_REPLICAS=$(kubectl -n "$NAMESPACE" get deploy vortexops-apiserver -o jsonpath='{.status.readyReplicas}' 2>/dev/null || echo "0")
check_metric "apiserver 副本" "${APISERVER_REPLICAS}" "30" ""
SYNCER_SHARDS=$(kubectl -n "$NAMESPACE" get pods -l app.kubernetes.io/name=vortexops-syncer --no-headers 2>/dev/null | wc -l | tr -d ' ')
check_metric "syncer 分片" "${SYNCER_SHARDS}" "64" ""
WS_GATEWAY_REPLICAS=$(kubectl -n "$NAMESPACE" get deploy vortexops-ws-gateway -o jsonpath='{.status.readyReplicas}' 2>/dev/null || echo "0")
check_metric "ws-gateway 副本" "${WS_GATEWAY_REPLICAS}" "50" ""

# 8. 节点资源
echo ""
if [[ "$OUTPUT" == "text" ]]; then
  echo "[8] K8s 节点资源"
fi
TOTAL_NODES=$(kubectl get nodes --no-headers 2>/dev/null | wc -l | tr -d ' ')
check_metric "平台集群节点数" "${TOTAL_NODES}" "200" ""

if [[ "$OUTPUT" == "json" ]]; then
  echo "  {}"
  echo "  ]"
  echo "}"
fi

echo ""
echo "--------------------------------------------"
echo " 扩容建议:"
echo "  - 任一指标 CRITICAL → 立即扩容"
echo "  - 业务集群数 ≥ 80% → 增加 syncer shard: ./scripts/syncer-shard.sh --scale --shards 32"
echo "  - 托管应用数 ≥ 80% → 增加 Citus worker: ./scripts/init-citus-sharding.sh --add-worker <pod>"
echo "  - apiserver CPU ≥ 80% → 扩容副本: kubectl -n $NAMESPACE scale deploy vortexops-apiserver --replicas=N"
echo "  - ws-gateway 连接数 ≥ 80% → 扩容副本 + 调整 Ingress sticky session"
echo "  - PG 数据量 ≥ 80% → 增加 Citus worker + rebalance: ./scripts/init-citus-sharding.sh --rebalance"
echo "  - Redis 内存 ≥ 80% → 扩容 Redis Cluster 分片"
echo "--------------------------------------------"
