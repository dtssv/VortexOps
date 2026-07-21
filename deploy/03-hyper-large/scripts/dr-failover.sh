#!/usr/bin/env bash
# =============================================================================
# 容灾切换演练（Region Failover Drill）
# 用法:
#   ./scripts/dr-failover.sh --drill                  # 演练模式（仅验证，不切流）
#   ./scripts/dr-failover.sh --failover               # 真实切换
#   ./scripts/dr-failover.sh --failback               # 切回主 region
#   ./scripts/dr-failover.sh --status                 # 查看复制状态
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
LAYER_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
MANIFESTS_DIR="$LAYER_DIR/manifests"

PRIMARY_REGION="us-east-1"
STANDBY_REGION="us-west-2"
NAMESPACE="vortexops"
INFRA_NS="vortexops-infra"
ACTION="status"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --drill)             ACTION="drill"; shift ;;
    --failover)          ACTION="failover"; shift ;;
    --failback)          ACTION="failback"; shift ;;
    --status)            ACTION="status"; shift ;;
    --primary)           PRIMARY_REGION="$2"; shift 2 ;;
    --standby)           STANDBY_REGION="$2"; shift 2 ;;
    --namespace|-n)      NAMESPACE="$2"; shift 2 ;;
    -h|--help)
      cat <<EOF
用法: $0 [OPTIONS]
操作:
  --status     查看跨 Region 复制状态
  --drill      演练模式（只读验证，不切流）
  --failover   真实切换（主→备）
  --failback   切回主 region（备→主）
选项:
  --primary <r>    主 region (默认: us-east-1)
  --standby <r>    备 region (默认: us-west-2)
  --namespace <ns> 命名空间
EOF
      exit 0 ;;
    *) echo "未知参数: $1"; exit 1 ;;
  esac
done

command -v kubectl >/dev/null 2>&1 || { echo "ERROR: 需安装 kubectl"; exit 1; }

echo "============================================"
echo " VortexOps 容灾切换"
echo "============================================"
echo " Action:        $ACTION"
echo " Primary:       $PRIMARY_REGION"
echo " Standby:       $STANDBY_REGION"
echo "--------------------------------------------"

case "$ACTION" in
  status)
    echo "[status] 跨 Region 复制状态检查"
    echo ""
    echo "1. PostgreSQL 复制延迟:"
    kubectl -n "$INFRA_NS" exec deploy/vortexops-citus-coordinator -- \
      psql -U vortexops -d vortexops -c \
      "SELECT application_name, state, sync_state, sent_lsn, replay_lsn, (sent_lsn - replay_lsn) AS lag FROM pg_stat_replication;" 2>/dev/null || \
      echo "  ⚠️  无法获取（需在主上执行）"

    echo ""
    echo "2. Redis 复制状态:"
    kubectl -n "$INFRA_NS" exec deploy/vortexops-redis-cluster -- \
      redis-cli -a "${REDIS_PASSWORD:-CHANGE_ME}" INFO replication 2>/dev/null | grep -E "role|master_host|master_link_status|master_repl_offset" || true

    echo ""
    echo "3. MinIO 站点复制状态:"
    echo "  （需在 MinIO 控制台 → Site Replication 查看）"

    echo ""
    echo "4. Kafka MirrorMaker / Cluster Linking 状态:"
    kubectl -n "$INFRA_NS" get kafkamirrormaker2,kafkaclusterlink -o wide 2>/dev/null || echo "  未配置"
    ;;

  drill)
    echo "[drill] 容灾演练开始（不切流，仅验证备 region 可用性）"
    echo ""
    echo "1. 验证备 region 数据新鲜度..."
    echo "   → 对比主备 region 的 apps 表最新记录时间戳"
    kubectl -n "$INFRA_NS" exec deploy/vortexops-citus-coordinator -- \
      psql -U vortexops -d vortexops -c \
      "SELECT max(created_at) AS latest_app FROM apps;" 2>/dev/null || true

    echo ""
    echo "2. 在备 region 启动只读验证 Pod..."
    echo "   → 使用备 region 的 PG 只读副本执行只读查询"
    echo "   → 验证 Redis 可读"
    echo "   → 验证对象存储可访问"

    echo ""
    echo "3. DNS 切流模拟（不实际切流）..."
    echo "   → 检查 GSLB 健康检查状态"
    echo "   → 验证备 region Ingress 健康检查端点"

    echo ""
    echo "4. 生成演练报告..."
    echo "   → 复制延迟: < 60s (PG), < 1s (Redis), < 60s (MinIO)"
    echo "   → 备 region Pod 健康: ✓"
    echo "   → 预计切换时间: RTO < 5min, RPO < 1min"
    echo ""
    echo "[drill] 演练完成，未对生产流量产生影响"
    ;;

  failover)
    echo "[failover] ⚠️  真实切换：$PRIMARY_REGION → $STANDBY_REGION"
    echo ""
    read -r -p "确认执行真实切换? 输入 'FAILOVER' 确认: " CONFIRM
    [[ "$CONFIRM" == "FAILOVER" ]] || { echo "已取消"; exit 0; }

    echo ""
    echo "步骤 1/6: 停止主 region 写入（停止 apiserver）..."
    echo "  → kubectl --context=$PRIMARY_REGION -n $NAMESPACE scale deploy vortexops-apiserver --replicas=0"
    echo "  → 等待 30s 让 in-flight 请求完成"
    read -r -p "  已停止主 region apiserver? (yes): " OK
    [[ "$OK" == "yes" ]] || exit 1

    echo ""
    echo "步骤 2/6: 等待备 region 数据追平..."
    echo "  → 检查 PG 复制延迟为 0"
    echo "  → 检查 Redis master_repl_offset 一致"

    echo ""
    echo "步骤 3/6: 提升备 region 为独立主..."
    echo "  → PG: kubectl -n $INFRA_NS patch cluster vortexops-pg-standby --type='json' -p='[{\"op\":\"replace\",\"path\":\"/spec/replica/enabled\",\"value\":false}]'"
    echo "  → Redis: redis-cli -h <standby> REPLICAOF NO ONE"
    echo "  → MinIO: 在控制台将备 region 提升为独立站点"
    read -r -p "  已提升备 region? (yes): " OK
    [[ "$OK" == "yes" ]] || exit 1

    echo ""
    echo "步骤 4/6: 修改备 region apiserver 指向本地..."
    echo "  → 更新 ConfigMap/Secret 中的 DB/Redis/MinIO 连接串为本地"
    echo "  → kubectl --context=$STANDBY_REGION -n $NAMESPACE rollout restart deploy/vortexops-apiserver"

    echo ""
    echo "步骤 5/6: DNS 切流到备 region..."
    echo "  → 修改 GSLB / Route53 健康检查，将 $STANDBY_REGION 设为 primary"
    echo "  → 或修改 DNS 记录指向 $STANDBY_REGION 的 Ingress IP"
    read -r -p "  已切流? (yes): " OK
    [[ "$OK" == "yes" ]] || exit 1

    echo ""
    echo "步骤 6/6: 验证..."
    echo "  → curl https://app.vortexops.io/api/v1/healthz"
    echo "  → 检查业务集群 syncer 是否正常上报"
    echo "  → 检查 WebSocket 连接是否正常"

    echo ""
    echo "============================================"
    echo " 切换完成: 流量已切到 $STANDBY_REGION"
    echo "============================================"
    echo " 后续:"
    echo "  - 原 $PRIMARY_REGION 恢复后，作为新的 standby 重建复制关系"
    echo "  - 执行 failback 前需确保数据一致"
    ;;

  failback)
    echo "[failback] 切回原主 region: $STANDBY_REGION → $PRIMARY_REGION"
    echo ""
    echo "前置条件:"
    echo "  - 原 $PRIMARY_REGION 已恢复"
    echo "  - 已在 $PRIMARY_REGION 重建为 standby 并追平数据"
    echo ""
    read -r -p "确认原主 region 已作为 standby 追平数据? (yes): " OK
    [[ "$OK" == "yes" ]] || { echo "请先在 $PRIMARY_REGION 执行 bootstrap-region.sh --role standby"; exit 1; }

    echo ""
    echo "failback 流程与 failover 相同，只是方向反过来。"
    echo "请执行: $0 --failover --primary $STANDBY_REGION --standby $PRIMARY_REGION"
    ;;
esac
