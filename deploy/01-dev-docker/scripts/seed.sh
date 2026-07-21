#!/usr/bin/env bash
# =============================================================================
# 注入 Mock 用户与默认 Jenkins/Registry 集成
# 用法: ./scripts/seed.sh
# 产物: admin / admin123 (登录前端)
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

echo "[seed] 注入 Mock 用户 (admin / admin123)..."
docker compose \
  --env-file "$ENV_FILE" \
  -f docker-compose.dev.yml \
  run --rm db-seed

echo ""
echo "[seed] 注入默认 Jenkins / Registry 集成..."
docker compose \
  --env-file "$ENV_FILE" \
  -f docker-compose.dev.yml \
  run --rm integration-seed

echo ""
echo "[seed] 完成。"
echo "       登录前端: http://localhost:8088  (admin / admin123)"
