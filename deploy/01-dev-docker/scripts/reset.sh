#!/usr/bin/env bash
# =============================================================================
# 重置开发栈（停止容器 + 删除所有 volume，数据全丢）
# 用法: ./scripts/reset.sh   （慎用）
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
LAYER_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
DEPLOY_DIR="$(cd "$LAYER_DIR/.." && pwd)"
ENV_FILE="$LAYER_DIR/config/dev.env"

if [[ ! -f "$ENV_FILE" ]]; then
  cp "$LAYER_DIR/config/dev.env.template" "$ENV_FILE"
fi

echo "!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!"
echo "! 危险操作：将删除所有 docker volume"
echo "! 数据库 / MinIO / Jenkins / k3s 状态全部丢失"
echo "!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!"
read -r -p "确认继续？输入 yes 执行：" CONFIRM
if [[ "$CONFIRM" != "yes" ]]; then
  echo "已取消。"
  exit 0
fi

cd "$DEPLOY_DIR"

# 同时清理 bridge 与 host-net 的可能残留
docker compose \
  --env-file "$ENV_FILE" \
  -f docker-compose.dev.yml \
  -f docker-compose.host-net.yml \
  down -v --remove-orphans 2>/dev/null || \
docker compose \
  --env-file "$ENV_FILE" \
  -f docker-compose.dev.yml \
  down -v --remove-orphans

echo "[reset] 完成。所有 volume 已删除。"
echo "        下次启动需重新执行：migrate.sh + seed.sh"
