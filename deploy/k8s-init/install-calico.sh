#!/bin/sh
# install-calico.sh — Install Calico CNI on a single-node k3s dev cluster.
#
# Prerequisites: k3s started with --flannel-backend=none (no CNI active).
# Uses the embedded OFFICIAL Calico manifest (v3.27.3, calico.yaml) patched
# at apply-time for the Docker bridge environment:
#   - VXLAN=Always, IPIP=Never (Docker bridge compatible, no BGP peering)
#   - IP_AUTODETECTION_METHOD=interface=eth0 (Docker bridge interface)
#   - CALICO_IPV4POOL_CIDR=10.42.0.0/16 (k3s default pod CIDR)
#
# IPAM=calico-ipam (official default) supports the annotation
#   cni.projectcalico.org/ipAddrs=["10.42.x.y"]
# which VortexOps uses to pin stable Pod IPs.
#
# LF line endings required (executed in Alpine k3s container, busybox sed/awk).

set -e

export KUBECONFIG=/etc/rancher/k3s/k3s.yaml

SRC="/k8s-init/calico.yaml"
MANIFEST="/tmp/calico-patched.yaml"

echo "[calico] waiting for k3s API to be ready..."
for i in $(seq 1 30); do
  if /bin/kubectl get nodes >/dev/null 2>&1; then
    break
  fi
  echo "[calico] k3s not ready, retrying ($i/30)..."
  sleep 2
done

if ! /bin/kubectl get nodes >/dev/null 2>&1; then
  echo "[calico] ERROR: k3s API not ready after 60s, aborting"
  exit 1
fi

if [ ! -f "$SRC" ]; then
  echo "[calico] ERROR: embedded manifest $SRC not found in image"
  exit 1
fi

echo "[calico] patching embedded manifest for Docker bridge..."
cp "$SRC" "$MANIFEST"

# 1) Enable CALICO_IPV4POOL_CIDR (commented out in upstream) and set to k3s pod CIDR.
#    Upstream lines (under calico-node env:):
#      "            # - name: CALICO_IPV4POOL_CIDR"
#      "            #   value: \"192.168.0.0/16\""
#    We strip the '# ' / '#   ' comment prefix (preserving indent) and set the CIDR.
sed -i 's/# - name: CALICO_IPV4POOL_CIDR/- name: CALICO_IPV4POOL_CIDR/' "$MANIFEST"
sed -i 's@#   value: "192.168.0.0/16"@  value: "10.42.0.0/16"@' "$MANIFEST"

# 2) Switch IPIP->Never, VXLAN->Always (Docker bridge has no BGP/L2 peering).
#    Each sed targets only the value line immediately following the named env var.
sed -i '/name: CALICO_IPV4POOL_IPIP/{n;s/value: "Always"/value: "Never"/}' "$MANIFEST"
sed -i '/name: CALICO_IPV4POOL_VXLAN/{n;s/value: "Never"/value: "Always"/}' "$MANIFEST"

# Note: IP autodetection uses upstream default "autodetect", which selects the
# first non-loopback interface (eth0 in the Docker bridge container) — sufficient
# for single-node dev k3s, so no IP_AUTODETECTION_METHOD override is needed.

echo "[calico] applying patched Calico manifest..."
/bin/kubectl apply -f "$MANIFEST"

echo "[calico] waiting for calico-node DaemonSet to be ready..."
/bin/kubectl -n kube-system rollout status daemonset/calico-node --timeout=300s || {
  echo "[calico] WARNING: calico-node not ready within 300s, current pod status:"
  /bin/kubectl -n kube-system get pods -l k8s-app=calico-node -o wide || true
}

echo "[calico] Calico CNI installed (IPAM=calico-ipam, supports cni.projectcalico.org/ipAddrs annotation)"
