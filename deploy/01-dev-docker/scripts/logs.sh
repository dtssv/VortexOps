#!/usr/bin/env bash
# =============================================================================
# 跟踪某服务日志
# 用法: ./scripts/logs.sh <service-name>
#   ./scripts/logs.sh apiserver
#   ./scripts/logs.sh k8s
#   ./scripts/logs.sh postgres
# =============================================================================
set -euo pipefail

SERVICE="${1:-}"
if [[ -z "$SERVICE" ]]; then
  echo "用法: $0 <service-name>"
  echo ""
  echo "可用服务（来自 docker-compose.dev.yml）："
  echo "  apiserver  ws-gateway  frontend  syncer  webhook  pipeline-worker"
  echo "  postgres   redis       kafka     minio   elasticsearch"
  echo "  jenkins    builder     registry  k8s     prometheus-server"
  echo "  jumpserver-core  jumpserver-koko  jumpserver-lion  jumpserver-web  jumpserver-mysql"
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
LAYER_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
DEPLOY_DIR="$(cd "$LAYER_DIR/.." && pwd)"
ENV_FILE="$LAYER_DIR/config/dev.env"

if [[ ! -f "$ENV_FILE" ]]; then
  cp "$LAYER_DIR/config/dev.env.template" "$ENV_FILE"
fi

cd "$DEPLOY_DIR"
docker compose --env-file "$ENV_FILE" -f docker-compose.dev.yml logs -f "$SERVICE"
