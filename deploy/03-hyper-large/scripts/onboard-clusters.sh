#!/usr/bin/env bash
# =============================================================================
# 批量接入业务集群（100k+ 应用所在的业务 K8s 集群）
# 用法:
#   ./scripts/onboard-clusters.sh --file clusters.csv
#   ./scripts/onboard-clusters.sh --name biz-prod-01 --kubeconfig /path/to/kc --shard auto
#   ./scripts/onboard-clusters.sh --list
#   ./scripts/onboard-clusters.sh --deploy-log-proxy --cluster biz-prod-01
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
LAYER_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
MANIFESTS_DIR="$LAYER_DIR/manifests"

NAMESPACE="vortexops"
RELEASE_NAME="vortexops"
APISERVER_SVC="${RELEASE_NAME}-apiserver"
LOG_PROXY_MANIFEST="$MANIFESTS_DIR/log-proxy/log-proxy-daemonset.yaml"

ACTION=""
CLUSTERS_FILE=""
CLUSTER_NAME=""
KUBECONFIG_PATH=""
SHARD_ID="auto"
DEPLOY_LOG_PROXY=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --file)             CLUSTERS_FILE="$2"; ACTION="batch"; shift 2 ;;
    --name)             CLUSTER_NAME="$2"; ACTION="single"; shift 2 ;;
    --kubeconfig)       KUBECONFIG_PATH="$2"; shift 2 ;;
    --shard)            SHARD_ID="$2"; shift 2 ;;
    --list)             ACTION="list"; shift ;;
    --deploy-log-proxy) DEPLOY_LOG_PROXY=true; ACTION="log-proxy"; shift ;;
    --cluster)          CLUSTER_NAME="$2"; shift 2 ;;
    --namespace|-n)     NAMESPACE="$2"; shift 2 ;;
    -h|--help)
      cat <<EOF
用法: $0 [OPTIONS]
批量接入:
  --file <csv>                从 CSV 批量接入（格式: name,kubeconfig_path,shard_id）
  --name <n>                  接入单个集群
  --kubeconfig <path>         集群 kubeconfig 文件路径
  --shard <id|auto>           指定 shard（auto 自动分配）
查询:
  --list                      列出已接入集群
log-proxy:
  --deploy-log-proxy          在业务集群部署 log-proxy
  --cluster <name>            目标业务集群名
EOF
      exit 0 ;;
    *) echo "未知参数: $1"; exit 1 ;;
  esac
done

command -v kubectl >/dev/null 2>&1 || { echo "ERROR: 需安装 kubectl"; exit 1; }

# 调用 apiserver API 的封装
api_call() {
  local method="$1"
  local path="$2"
  local data="${3:-}"
  if [[ -n "$data" ]]; then
    kubectl -n "$NAMESPACE" exec deploy/"$APISERVER_SVC" -- \
      wget -qO- --header="Content-Type: application/json" \
      --post-data="$data" \
      "http://localhost:8080/api/v1/admin$path"
  else
    kubectl -n "$NAMESPACE" exec deploy/"$APISERVER_SVC" -- \
      wget -qO- "$method" "http://localhost:8080/api/v1/admin$path"
  fi
}

# 在业务集群部署 log-proxy
deploy_log_proxy() {
  local cluster_name="$1"
  local kc="$2"

  echo "[log-proxy] 在业务集群 [$cluster_name] 部署 log-proxy..."
  if [[ ! -f "$kc" ]]; then
    echo "ERROR: kubeconfig 不存在: $kc"
    return 1
  fi

  KUBECONFIG="$kc" kubectl apply -f "$LOG_PROXY_MANIFEST"
  KUBECONFIG="$kc" kubectl -n vortexops rollout status daemonset/log-proxy --timeout=180s

  # 回调 apiserver 注册 log-proxy 端点
  echo "[log-proxy] 注册到 apiserver..."
  api_call POST "/clusters/$cluster_name/log-proxy" \
    "{\"enabled\":true,\"namespace\":\"vortexops\"}"
}

case "$ACTION" in
  single)
    [[ -n "$CLUSTER_NAME" ]] || { echo "ERROR: --name 必填"; exit 1; }
    [[ -f "$KUBECONFIG_PATH" ]] || { echo "ERROR: kubeconfig 不存在: $KUBECONFIG_PATH"; exit 1; }

    echo "============================================"
    echo " 接入业务集群: $CLUSTER_NAME"
    echo "============================================"
    echo " Kubeconfig: $KUBECONFIG_PATH"
    echo " Shard:      $SHARD_ID"
    echo "--------------------------------------------"

    # 1. 读取 kubeconfig 内容
    KC_CONTENT=$(base64 -w 0 "$KUBECONFIG_PATH" 2>/dev/null || base64 "$KUBECONFIG_PATH" | tr -d '\n')

    # 2. 调用 apiserver 接入集群
    echo "[onboard] 调用 apiserver 接入..."
    PAYLOAD=$(cat <<EOF
{
  "name": "$CLUSTER_NAME",
  "kubeconfig": "$KC_CONTENT",
  "shardAssignment": "$SHARD_ID",
  "deployLogProxy": true,
  "deployServiceAccount": true
}
EOF
)
    api_call POST "/clusters" "$PAYLOAD"

    echo ""
    echo "[onboard] 集群 [$CLUSTER_NAME] 接入成功"
    echo "  → 平台已自动在该集群部署 ServiceAccount/RBAC"
    echo "  → 平台已自动部署 log-proxy DaemonSet"
    ;;

  batch)
    [[ -f "$CLUSTERS_FILE" ]] || { echo "ERROR: CSV 文件不存在: $CLUSTERS_FILE"; exit 1; }

    echo "============================================"
    echo " 批量接入业务集群"
    echo "============================================"
    echo " 文件: $CLUSTERS_FILE"
    echo "--------------------------------------------"

    # CSV 格式: name,kubeconfig_path,shard_id
    SUCCESS=0
    FAILED=0
    while IFS=, read -r NAME KC_PATH SHARD; do
      [[ "$NAME" =~ ^# ]] && continue
      [[ -z "$NAME" ]] && continue

      echo ""
      echo "→ 接入: $NAME (shard=$SHARD, kc=$KC_PATH)"
      if "$0" --name "$NAME" --kubeconfig "$KC_PATH" --shard "$SHARD" -n "$NAMESPACE"; then
        SUCCESS=$((SUCCESS + 1))
      else
        echo "  ⚠️  接入失败: $NAME"
        FAILED=$((FAILED + 1))
      fi
    done < "$CLUSTERS_FILE"

    echo ""
    echo "--------------------------------------------"
    echo " 批量接入完成: 成功 $SUCCESS, 失败 $FAILED"
    ;;

  list)
    echo "[list] 已接入业务集群:"
    api_call GET "/clusters" | head -200 || true
    ;;

  log-proxy)
    [[ -n "$CLUSTER_NAME" ]] || { echo "ERROR: --cluster 必填"; exit 1; }
    # 查找集群的 kubeconfig
    KC_PATH=$(api_call GET "/clusters/$CLUSTER_NAME/kubeconfig" | jq -r '.path' 2>/dev/null || echo "")
    if [[ -z "$KC_PATH" ]]; then
      echo "ERROR: 未找到集群 [$CLUSTER_NAME] 的 kubeconfig，请手动指定"
      read -r -p "请输入 kubeconfig 路径: " KC_PATH
    fi
    deploy_log_proxy "$CLUSTER_NAME" "$KC_PATH"
    ;;

  *)
    echo "ERROR: 请指定操作（--file / --name / --list / --deploy-log-proxy）"
    "$0" --help
    exit 1
    ;;
esac
