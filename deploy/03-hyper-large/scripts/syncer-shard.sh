#!/usr/bin/env bash
# =============================================================================
# syncer 分片配置管理（动态扩缩 syncer shard）
# 用法:
#   ./scripts/syncer-shard.sh --init --shards 16
#   ./scripts/syncer-shard.sh --scale --shards 32
#   ./scripts/syncer-shard.sh --status
#   ./scripts/syncer-shard.sh --rebalance
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
LAYER_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
MANIFESTS_DIR="$LAYER_DIR/manifests/syncer-shard"

NAMESPACE="vortexops"
RELEASE_NAME="vortexops"
ACTION="status"
SHARD_COUNT=16

while [[ $# -gt 0 ]]; do
  case "$1" in
    --init)         ACTION="init"; shift ;;
    --scale)        ACTION="scale"; shift ;;
    --status)       ACTION="status"; shift ;;
    --rebalance)    ACTION="rebalance"; shift ;;
    --shards)       SHARD_COUNT="$2"; shift 2 ;;
    --namespace|-n) NAMESPACE="$2"; shift 2 ;;
    --release)      RELEASE_NAME="$2"; shift 2 ;;
    -h|--help)
      cat <<EOF
用法: $0 [OPTIONS]
选项:
  --init              初始化分片（创建 ConfigMap + StatefulSet）
  --scale             扩容分片数（需配合 --shards）
  --status            查看当前分片状态
  --rebalance         触发分片再平衡
  --shards <n>        分片数（默认: 16，建议: 业务集群数 / 32）
  --namespace, -n <ns> 命名空间 (默认: vortexops)
  --release <name>    Helm release 名 (默认: vortexops)
EOF
      exit 0 ;;
    *) echo "未知参数: $1"; exit 1 ;;
  esac
done

command -v kubectl >/dev/null 2>&1 || { echo "ERROR: 需安装 kubectl"; exit 1; }

echo "============================================"
echo " syncer 分片管理"
echo "============================================"
echo " Action:    $ACTION"
echo " Shards:    $SHARD_COUNT"
echo " Namespace: $NAMESPACE"
echo "--------------------------------------------"

case "$ACTION" in
  init)
    # 1. 部署分片 ConfigMap
    echo "[init] 部署 syncer-shard-configmap..."
    kubectl apply -f "$MANIFESTS_DIR/syncer-shard-configmap.yaml" -n "$NAMESPACE"

    # 2. 修改 ConfigMap 中的 shardCount
    kubectl -n "$NAMESPACE" patch configmap syncer-shard-config \
      --type merge -p "{\"data\":{\"SHARD_COUNT\":\"$SHARD_COUNT\"}}"

    # 3. 生成 syncer StatefulSet（每个 shard 一个副本）
    echo "[init] 生成 $SHARD_COUNT 个 syncer shard Pod..."
    for i in $(seq 0 $((SHARD_COUNT - 1))); do
      cat <<EOF | kubectl apply -f -
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: ${RELEASE_NAME}-syncer-shard-$i
  namespace: $NAMESPACE
  labels:
    app.kubernetes.io/name: vortexops-syncer
    app.kubernetes.io/instance: ${RELEASE_NAME}
    vortexops.io/shard-id: "$i"
spec:
  serviceName: ${RELEASE_NAME}-syncer-headless
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: vortexops-syncer
      vortexops.io/shard-id: "$i"
  template:
    metadata:
      labels:
        app.kubernetes.io/name: vortexops-syncer
        app.kubernetes.io/instance: ${RELEASE_NAME}
        vortexops.io/shard-id: "$i"
    spec:
      serviceAccountName: ${RELEASE_NAME}-syncer
      containers:
        - name: syncer
          image: vortexops/syncer:\${IMAGE_TAG}
          env:
            - name: SHARD_ID
              value: "$i"
            - name: SHARD_COUNT
              value: "$SHARD_COUNT"
            - name: LEASE_NAME
              value: "syncer-shard-$i"
          envFrom:
            - configMapRef:
                name: syncer-shard-config
            - secretRef:
                name: vortexops-secrets
          resources:
            requests: { cpu: "500m", memory: "1Gi" }
            limits:   { cpu: "2",    memory: "4Gi" }
EOF
    done

    # 4. 生成 Lease 资源（leader election）
    echo "[init] 生成 Lease 资源..."
    kubectl apply -f "$MANIFESTS_DIR/syncer-lease.yaml" -n "$NAMESPACE"

    echo ""
    echo "[init] syncer 分片初始化完成"
    echo "[init] 共 $SHARD_COUNT 个 shard，每个 shard 对应一个 syncer Pod"
    kubectl -n "$NAMESPACE" get pods -l app.kubernetes.io/name=vortexops-syncer
    ;;

  scale)
    CURRENT=$(kubectl -n "$NAMESPACE" get pods -l app.kubernetes.io/name=vortexops-syncer --no-headers 2>/dev/null | wc -l | tr -d ' ')
    echo "[scale] 当前分片数: $CURRENT, 目标: $SHARD_COUNT"

    if [[ "$SHARD_COUNT" -gt "$CURRENT" ]]; then
      echo "[scale] 扩容: 新增 $((SHARD_COUNT - CURRENT)) 个 shard"
      kubectl -n "$NAMESPACE" patch configmap syncer-shard-config \
        --type merge -p "{\"data\":{\"SHARD_COUNT\":\"$SHARD_COUNT\"}}"
      for i in $(seq "$CURRENT" $((SHARD_COUNT - 1))); do
        echo "  → 创建 shard-$i"
        # 复用 init 中的 StatefulSet 创建逻辑（此处省略，可调用 init 函数）
        echo "    kubectl create -f ${RELEASE_NAME}-syncer-shard-$i (略)"
      done
    elif [[ "$SHARD_COUNT" -lt "$CURRENT" ]]; then
      echo "[scale] 缩容: 移除 $((CURRENT - SHARD_COUNT)) 个 shard"
      for i in $(seq "$SHARD_COUNT" $((CURRENT - 1))); do
        echo "  → 删除 shard-$i"
        kubectl -n "$NAMESPACE" delete statefulset "${RELEASE_NAME}-syncer-shard-$i" --ignore-not-found
      done
      kubectl -n "$NAMESPACE" patch configmap syncer-shard-config \
        --type merge -p "{\"data\":{\"SHARD_COUNT\":\"$SHARD_COUNT\"}}"
    else
      echo "[scale] 无需调整"
    fi

    echo ""
    echo "[scale] 触发分片再平衡..."
    "$0" --rebalance --namespace "$NAMESPACE"
    ;;

  rebalance)
    echo "[rebalance] 查看各 shard 当前的 lease 持有者..."
    kubectl -n "$NAMESPACE" get lease -l vortexops.io/component=syncer-shard

    echo ""
    echo "[rebalance] 触发 shard 重新分配（通过 apiserver API）..."
    APISERVER_SVC="${RELEASE_NAME}-apiserver"
    # 调用 apiserver 的 rebalance API
    kubectl -n "$NAMESPACE" exec deploy/"$APISERVER_SVC" -- \
      wget -qO- --post-data='{"action":"rebalance"}' \
      http://localhost:8080/api/v1/admin/syncer/rebalance || \
      echo "  ⚠️  rebalance API 不可用，请通过 UI 触发"

    echo ""
    echo "[rebalance] 等待 30s 让 lease 重新选举..."
    sleep 30
    kubectl -n "$NAMESPACE" get lease -l vortexops.io/component=syncer-shard
    ;;

  status)
    echo "[status] syncer shard Pod 状态:"
    kubectl -n "$NAMESPACE" get pods -l app.kubernetes.io/name=vortexops-syncer \
      -o custom-columns=NAME:.metadata.name,SHARD:.metadata.labels.vortexops\.io/shard-id,STATUS:.status.phase,RESTARTS:.status.containerStatuses[0].restartCount,AGE:.metadata.creationTimestamp

    echo ""
    echo "[status] Lease 持有者（各 shard 的 leader）:"
    kubectl -n "$NAMESPACE" get lease -l vortexops.io/component=syncer-shard \
      -o custom-columns=NAME:.metadata.name,HOLDER:.spec.holderIdentity,AGE:.metadata.creationTimestamp

    echo ""
    echo "[status] 分片配置:"
    kubectl -n "$NAMESPACE" get configmap syncer-shard-config -o yaml | grep -E "SHARD_COUNT|SHARD_KEY|STRATEGY" || true

    echo ""
    echo "[status] 业务集群与 shard 映射:"
    kubectl -n "$NAMESPACE" exec deploy/"${RELEASE_NAME}-apiserver" -- \
      wget -qO- http://localhost:8080/api/v1/admin/syncer/shard-mapping || true
    ;;
esac
