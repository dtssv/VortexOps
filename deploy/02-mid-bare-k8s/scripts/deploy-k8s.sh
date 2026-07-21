#!/usr/bin/env bash
# =============================================================================
# VortexOps K8s 部署脚本（Helm）
# 用法:
#   ./scripts/deploy-k8s.sh --namespace vortexops --tag 1.0.0 \
#       --values manifests/helm-values/values-k8s-mid.yaml
#   ./scripts/deploy-k8s.sh --status
#   ./scripts/deploy-k8s.sh --uninstall
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
LAYER_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
DEPLOY_DIR="$(cd "$LAYER_DIR/.." && pwd)"
HELM_DIR="$DEPLOY_DIR/helm"

NAMESPACE="vortexops"
RELEASE_NAME="vortexops"
IMAGE_TAG="1.0.0"
VALUES_FILE=""
DRY_RUN=false
ACTION="upgrade"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --namespace|-n) NAMESPACE="$2"; shift 2 ;;
    --tag)          IMAGE_TAG="$2"; shift 2 ;;
    --values|-f)    VALUES_FILE="$2"; shift 2 ;;
    --release)      RELEASE_NAME="$2"; shift 2 ;;
    --dry-run)      DRY_RUN=true; shift ;;
    --status)       ACTION="status"; shift ;;
    --uninstall)    ACTION="uninstall"; shift ;;
    -h|--help)
      cat <<EOF
用法: $0 [OPTIONS]
选项:
  --namespace, -n <ns>    命名空间 (默认: vortexops)
  --tag <tag>             镜像 tag (默认: 1.0.0)
  --values, -f <file>     values 文件
  --release <name>        Helm release 名称 (默认: vortexops)
  --dry-run               仅渲染，不部署
  --status                查看当前部署状态
  --uninstall             卸载
EOF
      exit 0 ;;
    *) echo "未知参数: $1"; exit 1 ;;
  esac
done

command -v helm >/dev/null 2>&1 || { echo "ERROR: 需安装 Helm >= 3.12"; exit 1; }
command -v kubectl >/dev/null 2>&1 || { echo "ERROR: 需安装 kubectl"; exit 1; }

echo "============================================"
echo " VortexOps K8s Deploy (Helm)"
echo "============================================"
echo " Action:     $ACTION"
echo " Release:    $RELEASE_NAME"
echo " Namespace:  $NAMESPACE"
echo " Tag:        $IMAGE_TAG"
[[ -n "$VALUES_FILE" ]] && echo " Values:     $VALUES_FILE"
echo "--------------------------------------------"

case "$ACTION" in
  upgrade)
    HELM_ARGS=(upgrade --install "$RELEASE_NAME" "$HELM_DIR"
      --namespace "$NAMESPACE" --create-namespace
      --set "global.imageTag=$IMAGE_TAG")

    [[ -n "$VALUES_FILE" ]] && HELM_ARGS+=(-f "$VALUES_FILE")
    [[ "$DRY_RUN" == "true" ]] && HELM_ARGS+=(--dry-run --debug)

    echo "执行: helm ${HELM_ARGS[*]}"
    helm "${HELM_ARGS[@]}"

    if [[ "$DRY_RUN" == "false" ]]; then
      echo ""
      echo "[deploy] 等待 apiserver rollout 就绪..."
      kubectl -n "$NAMESPACE" rollout status deployment/"$RELEASE_NAME"-apiserver --timeout=300s || true

      echo ""
      echo "[deploy] Pod 状态:"
      kubectl -n "$NAMESPACE" get pods -l "app.kubernetes.io/instance=$RELEASE_NAME"

      echo ""
      echo "[deploy] Service:"
      kubectl -n "$NAMESPACE" get svc -l "app.kubernetes.io/instance=$RELEASE_NAME"

      echo ""
      echo "[deploy] 后续步骤:"
      echo "  1. 初始化数据库: ./scripts/init-db.sh --k8s --namespace $NAMESPACE"
      echo "  2. 接入业务集群: 系统管理 → 集群 → 接入（上传 kubeconfig）"
      echo "  3. 健康检查: kubectl -n $NAMESPACE exec deploy/$RELEASE_NAME-apiserver -- wget -qO- http://localhost:8080/api/v1/healthz"
    fi
    ;;

  uninstall)
    echo "卸载 release: $RELEASE_NAME"
    helm uninstall "$RELEASE_NAME" --namespace "$NAMESPACE"
    echo ""
    echo "[uninstall] 保留 namespace 与依赖组件（vortexops-infra）。"
    echo "            如需彻底清理: kubectl delete ns $NAMESPACE"
    ;;

  status)
    echo "[status] Helm Release:"
    helm status "$RELEASE_NAME" --namespace "$NAMESPACE" 2>/dev/null || echo "  未找到 release"
    echo ""
    echo "[status] Pods:"
    kubectl -n "$NAMESPACE" get pods -l "app.kubernetes.io/instance=$RELEASE_NAME" 2>/dev/null || echo "  未找到 pods"
    echo ""
    echo "[status] Services:"
    kubectl -n "$NAMESPACE" get svc -l "app.kubernetes.io/instance=$RELEASE_NAME" 2>/dev/null || echo "  未找到 svc"
    echo ""
    echo "[status] Ingress:"
    kubectl -n "$NAMESPACE" get ingress 2>/dev/null || true
    ;;
esac
