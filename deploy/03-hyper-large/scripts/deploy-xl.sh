#!/usr/bin/env bash
# =============================================================================
# XL 集群 VortexOps 部署脚本（Helm，多 Region）
# 用法:
#   ./scripts/deploy-xl.sh --region us-east-1 --tag 1.0.0 \
#       --values manifests/helm-values/values-xl-region-a.yaml
#   ./scripts/deploy-xl.sh --region us-west-2 --tag 1.0.0 \
#       --values manifests/helm-values/values-xl-region-b-standby.yaml --role standby
#   ./scripts/deploy-xl.sh --status --region us-east-1
#   ./scripts/deploy-xl.sh --rollback --region us-east-1
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
LAYER_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
DEPLOY_DIR="$(cd "$LAYER_DIR/.." && pwd)"
HELM_DIR="$DEPLOY_DIR/helm"

REGION="us-east-1"
ROLE="active"
NAMESPACE="vortexops"
RELEASE_NAME="vortexops"
IMAGE_TAG="1.0.0"
VALUES_FILE=""
DRY_RUN=false
ACTION="upgrade"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --region)        REGION="$2"; shift 2 ;;
    --role)          ROLE="$2"; shift 2 ;;
    --namespace|-n)  NAMESPACE="$2"; shift 2 ;;
    --release)       RELEASE_NAME="$2"; shift 2 ;;
    --tag)           IMAGE_TAG="$2"; shift 2 ;;
    --values|-f)     VALUES_FILE="$2"; shift 2 ;;
    --dry-run)       DRY_RUN=true; shift ;;
    --status)        ACTION="status"; shift ;;
    --rollback)      ACTION="rollback"; shift ;;
    --diff)          ACTION="diff"; shift ;;
    -h|--help)
      cat <<EOF
用法: $0 [OPTIONS]
选项:
  --region <r>          Region 名 (默认: us-east-1)
  --role <r>            active 或 standby (默认: active)
  --namespace, -n <ns>  命名空间 (默认: vortexops)
  --release <name>      Helm release 名 (默认: vortexops)
  --tag <tag>           镜像 tag (默认: 1.0.0)
  --values, -f <file>   values 文件
  --dry-run             仅渲染，不部署
  --status              查看部署状态
  --rollback            回滚到上一版本
  --diff                对比当前与待部署版本的差异
EOF
      exit 0 ;;
    *) echo "未知参数: $1"; exit 1 ;;
  esac
done

command -v helm    >/dev/null 2>&1 || { echo "ERROR: 需安装 Helm >= 3.12";   exit 1; }
command -v kubectl >/dev/null 2>&1 || { echo "ERROR: 需安装 kubectl >= 1.28"; exit 1; }

echo "============================================"
echo " VortexOps XL Deploy (Helm)"
echo "============================================"
echo " Action:     $ACTION"
echo " Region:     $REGION"
echo " Role:       $ROLE"
echo " Release:    $RELEASE_NAME"
echo " Namespace:  $NAMESPACE"
echo " Tag:        $IMAGE_TAG"
[[ -n "$VALUES_FILE" ]] && echo " Values:     $VALUES_FILE"
echo "--------------------------------------------"

case "$ACTION" in
  upgrade)
    HELM_ARGS=(upgrade --install "$RELEASE_NAME" "$HELM_DIR"
      --namespace "$NAMESPACE" --create-namespace
      --set "global.imageTag=$IMAGE_TAG"
      --set "global.region=$REGION"
      --set "global.role=$ROLE"
      --set "global.scaleTier=xl"
      --timeout 15m
      --atomic       # 失败自动回滚
      --wait)

    [[ -n "$VALUES_FILE" ]] && HELM_ARGS+=(-f "$VALUES_FILE")
    [[ "$DRY_RUN" == "true" ]] && HELM_ARGS+=(--dry-run --debug)

    echo "执行: helm ${HELM_ARGS[*]}"
    helm "${HELM_ARGS[@]}"

    if [[ "$DRY_RUN" == "false" ]]; then
      echo ""
      echo "[deploy] 等待 apiserver rollout 就绪..."
      kubectl -n "$NAMESPACE" rollout status deployment/"$RELEASE_NAME"-apiserver --timeout=600s

      echo ""
      echo "[deploy] 各组件 Pod 状态:"
      kubectl -n "$NAMESPACE" get pods -l "app.kubernetes.io/instance=$RELEASE_NAME" \
        -o custom-columns=NAME:.metadata.name,STATUS:.status.phase,READY:.status.containerStatuses[0].ready,RESTARTS:.status.containerStatuses[0].restartCount,NODE:.spec.nodeName

      echo ""
      echo "[deploy] HPA 状态:"
      kubectl -n "$NAMESPACE" get hpa -l "app.kubernetes.io/instance=$RELEASE_NAME"

      echo ""
      echo "[deploy] 后续步骤:"
      if [[ "$ROLE" == "active" ]]; then
        echo "  1. 初始化 Citus 分片: $SCRIPT_DIR/init-citus-sharding.sh --coordinator vortexops-citus-coordinator"
        echo "  2. 配置 syncer 分片:   $SCRIPT_DIR/syncer-shard.sh --init --shards 16"
        echo "  3. 批量接入业务集群:   $SCRIPT_DIR/onboard-clusters.sh --file clusters.csv"
        echo "  4. 部署 log-proxy:     $SCRIPT_DIR/onboard-clusters.sh --deploy-log-proxy --cluster <name>"
        echo "  5. 配置跨 Region DR:   在 standby region 执行 bootstrap-region.sh --role standby"
        echo "  6. 容量巡检:           $SCRIPT_DIR/capacity-check.sh"
      else
        echo "  standby 角色:"
        echo "  1. 确认跨 Region 复制已建立（PG/Redis/MinIO）"
        echo "  2. 执行容灾演练:       $SCRIPT_DIR/dr-failover.sh --drill"
        echo "  3. 不要在 standby region 执行 Citus/syncer 初始化"
      fi
    fi
    ;;

  rollback)
    echo "[rollback] 回滚 release: $RELEASE_NAME"
    helm rollback "$RELEASE_NAME" --namespace "$NAMESPACE"

    echo ""
    echo "[rollback] 等待 rollout 就绪..."
    kubectl -n "$NAMESPACE" rollout status deployment/"$RELEASE_NAME"-apiserver --timeout=600s

    echo ""
    echo "[rollback] 当前 revision:"
    helm history "$RELEASE_NAME" --namespace "$NAMESPACE" | tail -5
    ;;

  status)
    echo "[status] Helm Release ($REGION / $ROLE):"
    helm status "$RELEASE_NAME" --namespace "$NAMESPACE" 2>/dev/null || echo "  未找到 release"

    echo ""
    echo "[status] Pods:"
    kubectl -n "$NAMESPACE" get pods -l "app.kubernetes.io/instance=$RELEASE_NAME" 2>/dev/null

    echo ""
    echo "[status] HPA:"
    kubectl -n "$NAMESPACE" get hpa -l "app.kubernetes.io/instance=$RELEASE_NAME" 2>/dev/null

    echo ""
    echo "[status] 最近 revision:"
    helm history "$RELEASE_NAME" --namespace "$NAMESPACE" 2>/dev/null | tail -5
    ;;

  diff)
    command -v helm >/dev/null 2>&1 || { echo "ERROR: 需安装 helm diff plugin"; exit 1; }
    HELM_ARGS=(diff upgrade "$RELEASE_NAME" "$HELM_DIR"
      --namespace "$NAMESPACE"
      --set "global.imageTag=$IMAGE_TAG"
      --set "global.region=$REGION"
      --set "global.role=$ROLE")

    [[ -n "$VALUES_FILE" ]] && HELM_ARGS+=(-f "$VALUES_FILE")
    helm "${HELM_ARGS[@]}"
    ;;
esac
