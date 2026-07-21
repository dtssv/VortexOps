#!/bin/sh
# VortexOps k8s init

KUBECONFIG="${KUBECONFIG:-/etc/rancher/k3s/k3s.yaml}"
KUBECTL="/bin/kubectl"

echo "[k8s-init] Waiting for K8s API server up to 180s ..."
elapsed=0
while [ "$elapsed" -lt 180 ]; do
    if $KUBECTL --kubeconfig="${KUBECONFIG}" get nodes >/dev/null 2>&1; then
        echo "[k8s-init] K8s API server ready."
        break
    fi
    sleep 5
    elapsed=$((elapsed + 5))
done

if [ "$elapsed" -ge 180 ]; then
    echo "[k8s-init] ERROR: K8s API server not ready after 180s."
    exit 1
fi

# 创建 vortexops namespace
if ! $KUBECTL --kubeconfig="${KUBECONFIG}" get namespace vortexops >/dev/null 2>&1; then
    $KUBECTL --kubeconfig="${KUBECONFIG}" create namespace vortexops
    echo "[k8s-init] Created namespace: vortexops"
fi

# 创建 vortexops-dev namespace（应用分组默认部署命名空间）。
# 若 init.sh 清空 k3s 状态后未创建该 ns，已存在的分组（namespace=vortexops-dev）
# 在扩容/重新发布会因 "namespaces not found" 失败。
if ! $KUBECTL --kubeconfig="${KUBECONFIG}" get namespace vortexops-dev >/dev/null 2>&1; then
    $KUBECTL --kubeconfig="${KUBECONFIG}" create namespace vortexops-dev
    echo "[k8s-init] Created namespace: vortexops-dev"
fi

# 导出 kubeconfig（供 apiserver / tekton-init 挂载读取）
# server 地址根据网络模式选择：
#   - bridge (默认) : compose 服务名 k8s（apiserver 容器经 compose 网络访问）
#   - host-net      : Docker VM IP（默认 192.168.65.3，在 k3s TLS SAN 内）
#   - external      : 不在此处理，由外部注入脚本写入 EXPORT_DIR
EXPORT_DIR="${EXPORT_DIR:-/etc/vortexops}"
mkdir -p "${EXPORT_DIR}"

NETWORK_MODE="${K3S_NETWORK_MODE:-bridge}"
case "$NETWORK_MODE" in
  host-net)
    # Docker Desktop：apiserver 在 bridge 网段，经 VM IP 访问 host 网络上的 k3s（须在 TLS SAN 内）。
    # 172.22.0.1 仅为 compose 网关，通常不在 k3s 证书中；默认用 192.168.65.3（Docker VM）。
    # 可通过 K3S_KUBE_SERVER / K3S_INSECURE_TLS 覆盖。
    KUBE_SERVER="${K3S_KUBE_SERVER:-https://192.168.65.3:6443}"
    ;;
  bridge|*)
    KUBE_SERVER="https://k8s:6443"
    ;;
esac
echo "[k8s-init] Exporting kubeconfig with server=${KUBE_SERVER}"

for dest in kubeconfig.yaml admin.conf; do
  if [ "${K3S_INSECURE_TLS:-false}" = "true" ] && [ "$NETWORK_MODE" = "host-net" ]; then
    awk -v server="${KUBE_SERVER}" '
      /^    server:/ { print "    server: " server; print "    insecure-skip-tls-verify: true"; next }
      /certificate-authority-data:/ { next }
      { print }
    ' "${KUBECONFIG}" > "${EXPORT_DIR}/${dest}"
  else
    sed "s|server: https://[^:]*:[0-9]*|server: ${KUBE_SERVER}|" "${KUBECONFIG}" > "${EXPORT_DIR}/${dest}"
  fi
  chmod 644 "${EXPORT_DIR}/${dest}"
  echo "[k8s-init] Exported kubeconfig to ${EXPORT_DIR}/${dest}"
done

echo "[k8s-init] Done."
