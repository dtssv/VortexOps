#!/usr/bin/env bash
# =============================================================================
# 触发数据库 schema 迁移
# 用法: ./scripts/migrate.sh
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
LAYER_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
DEPLOY_DIR="$(cd "$LAYER_DIR/.." && pwd)"
ENV_FILE="$LAYER_DIR/config/dev.env"

if [[ ! -f "$ENV_FILE" ]]; then
  cp "$LAYER_DIR/config/dev.env.template" "$ENV_FILE"
fi

cd "$DEPLOY_DIR"

echo "[migrate] 触发 migrate 服务（执行 schema up）..."
docker compose \
  --env-file "$ENV_FILE" \
  -f docker-compose.dev.yml \
  run --rm migrate

echo "[migrate] 完成。可执行 ./scripts/seed.sh 注入 Mock 用户。"
