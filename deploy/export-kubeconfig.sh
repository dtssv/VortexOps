#!/bin/sh
# 从运行中的 k8s 容器导出 kubeconfig，供 VortexOps UI 注册集群或本机 kubectl 使用。
#
# 用法（在 deploy 目录）：
#   sh export-kubeconfig.sh
#   sh export-kubeconfig.sh /path/to/out-dir
#
# 输出：
#   kubeconfig-vortexops.yaml  — 供 VortexOps UI 粘贴（apiserver 容器内可访问的 server 地址）
#   kubeconfig-localhost.yaml    — 供宿主机 kubectl（bridge 模式 localhost:6443；host-net 见注释）
set -eu

OUT_DIR="${1:-$(dirname "$0")/export}"
K8S_CONTAINER="${K8S_CONTAINER:-deploy-k8s-1}"
COMPOSE_NETWORK="${COMPOSE_NETWORK:-deploy_default}"

mkdir -p "$OUT_DIR"

if ! docker ps --format '{{.Names}}' | grep -qx "$K8S_CONTAINER"; then
  echo "[export-kubeconfig] ERROR: container $K8S_CONTAINER not running."
  echo "  Start stack first: docker compose -f docker-compose.dev.yml -f docker-compose.host-net.yml up -d k8s"
  exit 1
fi

RAW=$(docker exec "$K8S_CONTAINER" cat /etc/vortexops/kubeconfig.yaml)

# host-net 模式：apiserver 在 bridge 网段，server 须为 k3s TLS 证书 SAN 内的地址。
# Docker Desktop 默认 VM IP 为 192.168.65.3；compose 网关 172.22.0.1 通常不在 SAN 内。
if docker inspect "$K8S_CONTAINER" --format '{{.HostConfig.NetworkMode}}' 2>/dev/null | grep -q host; then
  VM_IP="${K3S_KUBE_SERVER_HOST:-192.168.65.3}"
  VM_IP=$(printf '%s' "$VM_IP" | sed 's|https\?://||;s|:.*||')
  printf '%s\n' "$RAW" | sed "s|server: https://[^[:space:]]*|server: https://${VM_IP}:6443|" \
    > "$OUT_DIR/kubeconfig-vortexops.yaml"
  echo "[export-kubeconfig] host-net mode → server https://${VM_IP}:6443 (Docker VM IP, must be in TLS SAN)"
else
  printf '%s\n' "$RAW" > "$OUT_DIR/kubeconfig-vortexops.yaml"
  echo "[export-kubeconfig] bridge mode → server $(grep 'server:' "$OUT_DIR/kubeconfig-vortexops.yaml" | head -1 | sed 's/^[[:space:]]*//')"
fi

# 宿主机 kubectl：bridge 模式映射了 6443 端口；host-net 在 Docker Desktop 上通常不可用。
if docker inspect "$K8S_CONTAINER" --format '{{.HostConfig.NetworkMode}}' 2>/dev/null | grep -qv host; then
  printf '%s\n' "$RAW" | sed 's|server: https://k8s:6443|server: https://127.0.0.1:6443|' \
    > "$OUT_DIR/kubeconfig-localhost.yaml"
  echo "[export-kubeconfig] Wrote kubeconfig-localhost.yaml (127.0.0.1:6443)"
else
  cat > "$OUT_DIR/kubeconfig-localhost.yaml.README" <<EOF
host-net 模式下 Docker Desktop 通常无法从 Windows 宿主机直连 6443。
请使用 bridge 模式开发，或在 Linux 宿主机上使用 host-net。
VortexOps UI 注册请使用 kubeconfig-vortexops.yaml。
EOF
  echo "[export-kubeconfig] host-net: skipped kubeconfig-localhost.yaml (see .README)"
fi

echo ""
echo "文件目录: $OUT_DIR"
echo "VortexOps UI → 集群管理 → 新建集群 → 粘贴 kubeconfig-vortexops.yaml 全文"
echo "API Server 地址可填: $(grep 'server:' "$OUT_DIR/kubeconfig-vortexops.yaml" | head -1 | sed 's/.*server: //')"
