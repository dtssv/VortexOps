#!/usr/bin/env bash
# =============================================================================
# VortexOps 开发栈启动脚本（bridge 模式，默认）
# 用法: ./scripts/up.sh
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
LAYER_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
DEPLOY_DIR="$(cd "$LAYER_DIR/.." && pwd)"
ENV_FILE="$LAYER_DIR/config/dev.env"

# 校验 env 文件
if [[ ! -f "$ENV_FILE" ]]; then
  echo "[up] 首次启动，复制模板: config/dev.env.template -> config/dev.env"
  cp "$LAYER_DIR/config/dev.env.template" "$ENV_FILE"
  echo "[up] 请按需编辑 $ENV_FILE 后重新运行（开发默认值可直接启动）。"
fi

# 校验 docker compose
if ! command -v docker >/dev/null 2>&1; then
  echo "ERROR: 未安装 docker，请先安装 Docker Desktop 或 docker-ce + docker-compose-plugin"
  exit 1
fi

echo "============================================"
echo " VortexOps Dev Stack (bridge mode)"
echo "============================================"
echo " Layer Dir:  $LAYER_DIR"
echo " Compose:    $DEPLOY_DIR/docker-compose.dev.yml"
echo " Env File:   $ENV_FILE"
echo "--------------------------------------------"

cd "$DEPLOY_DIR"

docker compose \
  --env-file "$ENV_FILE" \
  -f docker-compose.dev.yml \
  up -d

echo ""
echo "[up] 容器状态:"
docker compose -f docker-compose.dev.yml ps

cat <<EOF

============================================
 启动完成，后续步骤
============================================
 1. 等待依赖健康就绪（约 1-3 分钟）：
    ./scripts/healthcheck.sh

 2. 初始化数据库 schema（首次必做）：
    ./scripts/migrate.sh

 3. 注入 Mock 用户与默认 Jenkins/Registry 集成：
    ./scripts/seed.sh

 4. 访问前端：  http://localhost:8088  (admin / admin123)
    API:        http://localhost:8080/api/v1
    Jenkins:    http://localhost:8082  (admin / vortexops_dev)
    MinIO:      http://localhost:9001  (admin / vortexops_dev)
============================================
EOF
