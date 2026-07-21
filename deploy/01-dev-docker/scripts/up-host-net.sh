#!/usr/bin/env bash
# =============================================================================
# VortexOps 开发栈启动脚本（host-net 模式，underlay 验证）
# 用法: ./scripts/up-host-net.sh
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
LAYER_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
DEPLOY_DIR="$(cd "$LAYER_DIR/.." && pwd)"
ENV_FILE="$LAYER_DIR/config/dev.env"

if [[ ! -f "$ENV_FILE" ]]; then
  cp "$LAYER_DIR/config/dev.env.template" "$ENV_FILE"
  echo "[up-host-net] 已生成 config/dev.env，请确认 K3S_UNDERLAY_* 参数后重试。"
  exit 0
fi

if ! command -v docker >/dev/null 2>&1; then
  echo "ERROR: 未安装 docker"
  exit 1
fi

echo "============================================"
echo " VortexOps Dev Stack (host-net mode)"
echo "============================================"
echo " Underlay iface:  ${K3S_UNDERLAY_PARENT_IFACE:-eth0}"
echo " Underlay subnet: ${K3S_UNDERLAY_SUBNET:-192.168.1.0/24}"
echo "--------------------------------------------"

if [[ -z "${K3S_UNDERLAY_PARENT_IFACE:-}" ]]; then
  echo "WARNING: 未设置 K3S_UNDERLAY_PARENT_IFACE，使用默认 eth0"
  echo "         如宿主机网卡名不同，请编辑 $ENV_FILE"
fi

cd "$DEPLOY_DIR"

docker compose \
  --env-file "$ENV_FILE" \
  -f docker-compose.dev.yml \
  -f docker-compose.host-net.yml \
  up -d

echo ""
echo "[up-host-net] 容器状态:"
docker compose -f docker-compose.dev.yml -f docker-compose.host-net.yml ps

cat <<EOF

============================================
 host-net 启动完成
============================================
 首次启动后需初始化集群网络画像与 IP 池：

   docker compose -f docker-compose.dev.yml -f docker-compose.host-net.yml \\
     --env-file $ENV_FILE \\
     run --rm underlay-seed

 完成后重新发布分组，Pod 才会拿到物理 IP。
============================================
EOF
