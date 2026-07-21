#!/usr/bin/env bash
# =============================================================================
# 停止开发栈（保留数据）
# 用法: ./scripts/down.sh
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
LAYER_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
DEPLOY_DIR="$(cd "$LAYER_DIR/.." && pwd)"
ENV_FILE="$LAYER_DIR/config/dev.env"

if [[ ! -f "$ENV_FILE" ]]; then
  cp "$LAYER_DIR/config/dev.env.template" "$ENV_FILE"
fi

# 自动检测当前使用哪种模式（通过检查 k8s 容器是否 host 网络）
MODE="bridge"
if docker inspect vortexops-k8s 2>/dev/null | grep -q '"NetworkMode": "host"' 2>/dev/null; then
  MODE="host-net"
fi

echo "[down] 检测到当前模式: $MODE"
echo "[down] 停止容器（保留 volume）..."

cd "$DEPLOY_DIR"

case "$MODE" in
  host-net)
    docker compose \
      --env-file "$ENV_FILE" \
      -f docker-compose.dev.yml \
      -f docker-compose.host-net.yml \
      down
    ;;
  bridge|*)
    docker compose \
      --env-file "$ENV_FILE" \
      -f docker-compose.dev.yml \
      down
    ;;
esac

echo "[down] 完成。volume 保留，下次 up.sh 数据不丢。"
echo "       如需彻底清空：./scripts/reset.sh"
