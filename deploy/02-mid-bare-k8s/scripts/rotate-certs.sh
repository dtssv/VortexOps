#!/usr/bin/env bash
# =============================================================================
# 证书轮换（Nginx TLS / webhook TLS / Ingress TLS）
# 用法:
#   ./scripts/rotate-certs.sh --mode <bare|k8s>
# =============================================================================
set -euo pipefail

MODE="bare"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --mode) MODE="$2"; shift 2 ;;
    -h|--help) echo "用法: $0 --mode <bare|k8s>"; exit 0 ;;
    *) echo "未知参数: $1"; exit 1 ;;
  esac
done

echo "============================================"
echo " VortexOps Cert Rotation ($MODE)"
echo "============================================"

if [[ "$MODE" == "bare" ]]; then
  # 物理机：手动放置证书后 reload nginx / 重启 webhook
  CERT_DIR="/etc/ssl/certs"
  KEY_DIR="/etc/ssl/private"

  for f in "$CERT_DIR/vortexops.pem" "$KEY_DIR/vortexops-key.pem"; do
    [[ -f "$f" ]] || { echo "ERROR: 证书不存在 $f"; exit 1; }
  done

  echo "[rotate] 1/2 reload nginx..."
  nginx -t && systemctl reload nginx

  echo "[rotate] 2/2 restart webhook (TLS 证书已更新)..."
  systemctl restart vortexops-webhook

elif [[ "$MODE" == "k8s" ]]; then
  # K8s：cert-manager 自动续期，需手动重启引用证书的 Pod
  echo "[rotate] 重启引用 TLS Secret 的 Pod..."
  kubectl -n vortexops rollout restart deploy/vortexos-apiserver
  kubectl -n vortexops rollout restart deploy/vortexops-ws-gateway
  kubectl -n vortexops rollout restart deploy/vortexops-webhook
  echo "[rotate] 触发 cert-manager 续期（如未到期会跳过）..."
  kubectl get certificate -A -o custom-columns=NS:.metadata.namespace,NAME:.metadata.name,RENEWAL:.status.renewalTime | head
else
  echo "ERROR: 未知 mode: $MODE"
  exit 1
fi

echo "[rotate] 完成。"
