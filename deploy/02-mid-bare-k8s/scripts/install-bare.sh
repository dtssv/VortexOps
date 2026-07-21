#!/usr/bin/env bash
# =============================================================================
# 物理机裸机安装单个 VortexOps 服务（systemd）
# 用法:
#   sudo ./scripts/install-bare.sh \
#       --binary /tmp/vortexops-1.0.0-linux-amd64 \
#       --service apiserver \
#       --env-file /etc/vortexops/vortexops.env
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
LAYER_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

BINARY=""
SERVICE=""
ENV_FILE=""
INSTALL_DIR="/opt/vortexops"
SERVICE_USER="vortexops"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --binary)     BINARY="$2"; shift 2 ;;
    --service)    SERVICE="$2"; shift 2 ;;
    --env-file)   ENV_FILE="$2"; shift 2 ;;
    --install-dir) INSTALL_DIR="$2"; shift 2 ;;
    -h|--help)
      cat <<EOF
用法: $0 --binary <path> --service <name> [--env-file <path>] [--install-dir <dir>]
选项:
  --binary <path>        二进制文件路径
  --service <name>       服务名: apiserver / ws-gateway / syncer / webhook / pipeline-worker / frontend
  --env-file <path>      环境变量文件 (默认: 从模板复制)
  --install-dir <dir>    安装目录 (默认: /opt/vortexops)
EOF
      exit 0 ;;
    *) echo "未知参数: $1"; exit 1 ;;
  esac
done

# 校验
if [[ $EUID -ne 0 ]]; then
  echo "ERROR: 请使用 sudo 执行"
  exit 1
fi

if [[ -z "$BINARY" || -z "$SERVICE" ]]; then
  echo "ERROR: --binary 与 --service 必填"
  exit 1
fi

case "$SERVICE" in
  apiserver|ws-gateway|syncer|webhook|pipeline-worker|frontend) ;;
  *) echo "ERROR: 未知 service: $SERVICE"; exit 1 ;;
esac

if [[ ! -f "$BINARY" ]]; then
  echo "ERROR: 二进制文件不存在: $BINARY"
  exit 1
fi

SYSTEMD_UNIT="$LAYER_DIR/bare/systemd/vortexops-${SERVICE}.service"
if [[ ! -f "$SYSTEMD_UNIT" ]]; then
  echo "ERROR: systemd unit 模板不存在: $SYSTEMD_UNIT"
  exit 1
fi

echo "============================================"
echo " VortexOps Bare Install"
echo "============================================"
echo " Service:    $SERVICE"
echo " Binary:     $BINARY"
echo " Install:    $INSTALL_DIR"
echo " User:       $SERVICE_USER"
echo "--------------------------------------------"

# 1. 创建用户
echo "[1/5] 创建用户 $SERVICE_USER..."
id "$SERVICE_USER" &>/dev/null || useradd --system --home-dir "$INSTALL_DIR" --shell /sbin/nologin "$SERVICE_USER"

# 2. 创建目录
echo "[2/5] 创建目录..."
mkdir -p "$INSTALL_DIR"/{bin,conf,logs,secrets}

# 3. 复制二进制
echo "[3/5] 安装二进制..."
cp "$BINARY" "$INSTALL_DIR/bin/vortexops-${SERVICE}"
chmod 755 "$INSTALL_DIR/bin/vortexops-${SERVICE}"

# 4. 复制环境变量文件
echo "[4/5] 配置环境变量..."
if [[ -n "$ENV_FILE" && -f "$ENV_FILE" ]]; then
  cp "$ENV_FILE" "$INSTALL_DIR/conf/vortexops.env"
elif [[ ! -f "$INSTALL_DIR/conf/vortexops.env" ]]; then
  echo "  ⚠  未提供 --env-file，请手动创建 $INSTALL_DIR/conf/vortexops.env"
fi
chmod 600 "$INSTALL_DIR/conf/vortexops.env" 2>/dev/null || true

# 5. 安装 systemd unit
echo "[5/5] 安装 systemd unit..."
cp "$SYSTEMD_UNIT" "/etc/systemd/system/vortexops-${SERVICE}.service"
systemctl daemon-reload
systemctl enable "vortexops-${SERVICE}"

chown -R "$SERVICE_USER:$SERVICE_USER" "$INSTALL_DIR"

echo ""
echo "============================================"
echo " 安装完成 — 后续操作"
echo "============================================"
echo " 1. 编辑配置（如未提供 --env-file）:"
echo "    sudo vim $INSTALL_DIR/conf/vortexops.env"
echo ""
echo " 2. 启动服务:"
echo "    sudo systemctl start vortexops-$SERVICE"
echo ""
echo " 3. 查看状态 / 日志:"
echo "    sudo systemctl status vortexops-$SERVICE"
echo "    sudo journalctl -u vortexops-$SERVICE -f"
echo "============================================"
