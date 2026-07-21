#!/bin/sh
# VortexOps dev k3s entrypoint
#
# K3S_NETWORK_MODE selects network stack:
#   - bridge (default): docker bridge network, CNI per K3S_CNI (calico|flannel), dev-only
#   - host-net        : container shares host network, flannel + multus + macvlan,
#                       for underlay (Pod gets physical LAN IP)
#   - external        : no embedded k3s, external cluster kubeconfig injected
#                       (handled by external mode, not this script)
# K3S_CNI selects CNI plugin in bridge mode:
#   - calico (default): supports static IP via cni.projectcalico.org/ipAddrs annotation
#   - cilium          : eBPF data plane, kube-proxy replacement, L4 LB
#   - flannel        : legacy default, does NOT support static IP annotations

NETWORK_MODE="${K3S_NETWORK_MODE:-bridge}"
CNI="${K3S_CNI:-calico}"
echo "[k8s] Starting k3s server (network_mode=${NETWORK_MODE}, cni=${CNI})..."

# host-net 下 compose DNS 不可用，registry:5000 需走宿主机映射端口 8083。
# 镜像引用仍是 registry:5000/...，靠 mirror 改写到 127.0.0.1:8083。
if [ "$NETWORK_MODE" = "host-net" ]; then
  cat > /etc/rancher/k3s/registries.yaml <<'EOF'
mirrors:
  "registry:5000":
    endpoint:
      - "http://127.0.0.1:8083"
configs:
  "registry:5000":
    tls:
      insecure_skip_verify: true
  "127.0.0.1:8083":
    tls:
      insecure_skip_verify: true
EOF
  echo "[k8s] registries.yaml: registry:5000 -> http://127.0.0.1:8083 (host-net)"
fi

# Calico's mount-bpffs init container and hostPath mounts (/var/run/calico,
# /sys/fs/) require the parent mounts to be shared. Docker Desktop / DinD does
# not share / or /sys by default. Make both shared before starting k3s so
# calico-node can mount its volumes. Non-fatal on failure (flannel unaffected).
if [ "$CNI" = "calico" ]; then
  mount --make-shared / 2>/dev/null && echo "[k8s] / marked as shared (calico hostPath mounts)" || \
    echo "[k8s] WARNING: could not mark / shared, calico hostPath mounts may fail"
  mount --make-shared /sys 2>/dev/null && echo "[k8s] /sys marked as shared (calico bpffs mount)" || \
    echo "[k8s] WARNING: could not mark /sys shared, calico bpffs mount may fail"
fi

# Common args: NodePort range matches docker-compose.dev.yml port mapping,
# disable traefik/servicelb, disable QoS cgroup for DinD compatibility.
COMMON_ARGS="--write-kubeconfig-mode=644 \
  --tls-san=k8s \
  --tls-san=localhost \
  --tls-san=host.docker.internal \
  --tls-san=192.168.65.3 \
  --disable=traefik \
  --disable=servicelb \
  --service-node-port-range=30000-30099 \
  --kubelet-arg=cgroups-per-qos=false \
  --kubelet-arg=enforce-node-allocatable="

case "$NETWORK_MODE" in
  bridge)
    # CNI selection: calico disables flannel and installs Calico post-start;
    # flannel uses built-in vxlan overlay (legacy, no static IP support).
    if [ "$CNI" = "calico" ] || [ "$CNI" = "cilium" ]; then
      /bin/k3s server $COMMON_ARGS \
        --flannel-backend=none &
    else
      /bin/k3s server $COMMON_ARGS \
        --flannel-backend=vxlan &
    fi
    ;;
  host-net)
    # host network: keep flannel for cluster Service network,
    # underlay traffic via Multus macvlan attachment, Pod L2 direct to physical net.
    HOST_NET_TLS_SAN="${K3S_KUBE_SERVER_HOST:-}"
    if [ -z "$HOST_NET_TLS_SAN" ]; then
      HOST_NET_TLS_SAN=$(printf '%s' "${K3S_KUBE_SERVER:-https://192.168.65.3:6443}" | sed 's|https\?://||;s|:.*||')
    fi
    /bin/k3s server $COMMON_ARGS \
      --tls-san="${HOST_NET_TLS_SAN}" \
      --flannel-backend=vxlan \
      --kube-controller-manager-arg=node-cidr-mask-size=24 &
    ;;
  *)
    echo "[k8s] ERROR: unknown K3S_NETWORK_MODE='${NETWORK_MODE}', expected bridge|host-net"
    exit 1
    ;;
esac

K3S_PID=$!

export KUBECONFIG=/etc/rancher/k3s/k3s.yaml

if ! /bin/sh /k8s-init/init.sh; then
  echo "[k8s] init failed, stopping k3s"
  kill "$K3S_PID" 2>/dev/null || true
  wait "$K3S_PID" 2>/dev/null || true
  exit 1
fi

# bridge mode + calico/cilium: after k3s ready, install CNI for static IP / eBPF L4 LB.
if [ "$NETWORK_MODE" = "bridge" ] && [ "$CNI" = "calico" ]; then
  if ! /bin/sh /k8s-init/install-calico.sh; then
    echo "[k8s] calico CNI install failed (non-fatal, static IP annotations may not work)"
  fi
fi

if [ "$NETWORK_MODE" = "bridge" ] && [ "$CNI" = "cilium" ]; then
  if ! /bin/sh /k8s-init/install-cilium.sh; then
    echo "[k8s] cilium CNI install failed (non-fatal, eBPF L4 LB may not work)"
  fi
fi

# host-net mode: after k3s ready, install Multus + Macvlan NAD for underlay.
if [ "$NETWORK_MODE" = "host-net" ]; then
  if ! /bin/sh /k8s-init/install-underlay.sh; then
    echo "[k8s] underlay CNI install failed (non-fatal, underlay pods may not get LAN IP)"
  fi
fi

echo "[k8s] k3s ready (pid=$K3S_PID, network_mode=${NETWORK_MODE}, cni=${CNI})"
wait "$K3S_PID"
