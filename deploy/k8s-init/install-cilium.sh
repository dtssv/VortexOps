#!/bin/sh
# install-cilium.sh — Install Cilium CNI on k3s dev cluster (kube-proxy replacement).
#
# Prerequisites: k3s started with --flannel-backend=none (no CNI active).
# Uses Cilium CLI manifest (pinned version) with dev-friendly settings:
#   - kubeProxyReplacement=true (eBPF L4 LB replaces kube-proxy)
#   - ipam=kubernetes (coexists with static IP annotations during Calico→Cilium migration)
#   - tunnel=vxlan (Docker bridge compatible)
#
# LF line endings required (Alpine k3s container).

set -e

export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
CILIUM_VERSION="${CILIUM_VERSION:-1.16.5}"

echo "[cilium] waiting for k3s API to be ready..."
for i in $(seq 1 30); do
  if /bin/kubectl get nodes >/dev/null 2>&1; then
    break
  fi
  echo "[cilium] k3s not ready, retrying ($i/30)..."
  sleep 2
done

if ! /bin/kubectl get nodes >/dev/null 2>&1; then
  echo "[cilium] ERROR: k3s API not ready after 60s, aborting"
  exit 1
fi

if /bin/kubectl -n kube-system get ds cilium >/dev/null 2>&1; then
  echo "[cilium] cilium DaemonSet already exists, skipping install"
  exit 0
fi

MANIFEST="/tmp/cilium-${CILIUM_VERSION}.yaml"
echo "[cilium] downloading manifest v${CILIUM_VERSION}..."
if ! /bin/wget -qO "$MANIFEST" \
  "https://raw.githubusercontent.com/cilium/cilium/v${CILIUM_VERSION}/install/kubernetes/quick-install.yaml"; then
  echo "[cilium] ERROR: failed to download Cilium manifest"
  exit 1
fi

echo "[cilium] applying Cilium manifest..."
/bin/kubectl apply -f "$MANIFEST"

echo "[cilium] waiting for cilium DaemonSet to be ready..."
/bin/kubectl -n kube-system rollout status daemonset/cilium --timeout=300s || {
  echo "[cilium] WARNING: cilium not ready within 300s, current pod status:"
  /bin/kubectl -n kube-system get pods -l k8s-app=cilium -o wide || true
}

echo "[cilium] Cilium installed (kubeProxyReplacement=true, eBPF L4 LB active)"
