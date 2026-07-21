#!/usr/bin/env bash
# =============================================================================
# VortexOps 开发栈启动脚本（external 模式，连接外部 k3s 集群）
# 用法: VORTEXOPS_K8S_API_SERVER=https://10.0.0.5:6443 ./scripts/up-external.sh
# 前置：deploy/external-kubeconfig.yaml 已就位（由 config/external-kubeconfig.example 复制）
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
LAYER_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
DEPLOY_DIR="$(cd "$LAYER_DIR/.." && pwd)"
ENV_FILE="$LAYER_DIR/config/dev.env"
KUBECONFIG_FILE="$DEPLOY_DIR/external-kubeconfig.yaml"

if [[ ! -f "$ENV_FILE" ]]; then
  cp "$LAYER_DIR/config/dev.env.template" "$ENV_FILE"
fi

if [[ ! -f "$KUBECONFIG_FILE" ]]; then
  echo "ERROR: 未找到 $KUBECONFIG_FILE"
  echo "       请先复制模板并填入外部集群证书："
  echo "       cp $LAYER_DIR/config/external-kubeconfig.example $KUBECONFIG_FILE"
  exit 1
fi

if [[ -z "${VORTEXOPS_K8S_API_SERVER:-}" ]]; then
  echo "ERROR: 未设置 VORTEXOPS_K8S_API_SERVER 环境变量"
  echo "       示例: VORTEXOPS_K8S_API_SERVER=https://192.168.1.10:6443 ./scripts/up-external.sh"
  exit 1
fi

echo "============================================"
echo " VortexOps Dev Stack (external mode)"
echo "============================================"
echo " External k8s API: $VORTEXOPS_K8S_API_SERVER"
echo " Kubeconfig:       $KUBECONFIG_FILE"
echo "--------------------------------------------"

cd "$DEPLOY_DIR"

# 将环境变量 export 到 docker compose
export VORTEXOPS_K8S_API_SERVER

docker compose \
  --env-file "$ENV_FILE" \
  -f docker-compose.dev.yml \
  -f docker-compose.external.yml \
  up -d

echo ""
echo "[up-external] 容器状态:"
docker compose -f docker-compose.dev.yml -f docker-compose.external.yml ps
