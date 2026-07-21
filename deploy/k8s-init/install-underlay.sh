#!/bin/sh
# Underlay CNI 瀹夎锛歁ultus + Macvlan NetworkAttachmentDefinition
# -----------------------------------------------------------------------------
# 瑙﹀彂鏉′欢锛欿3S_NETWORK_MODE=host-net
# 浣滅敤锛?
#   1. 瀹夎 Multus CNI meta-plugin锛堣 Pod 鍙檮鍔犲寮犵綉鍗★級
#   2. 鍒涘缓 macvlan NAD锛屼娇甯?underlay 娉ㄨВ鐨?Pod 鎷垮埌鐗╃悊灞€鍩熺綉 IP
#   3. 瀹夎 Whereabouts 浣滀负 macvlan 鐨?IPAM锛堥潤鎬?IP 鍒嗛厤锛?
#
# 鍙橀噺鏉ユ簮锛歞ocker-compose.host-net.yml 鐨?environment

KUBECONFIG="${KUBECONFIG:-/etc/rancher/k3s/k3s.yaml}"
KUBECTL="/bin/kubectl"

PARENT_IFACE="${K3S_UNDERLAY_PARENT_IFACE:-eth0}"
SUBNET="${K3S_UNDERLAY_SUBNET:-192.168.1.0/24}"
GATEWAY="${K3S_UNDERLAY_GATEWAY:-192.168.1.1}"
RANGE_START="${K3S_UNDERLAY_RANGE_START:-192.168.1.200}"
RANGE_END="${K3S_UNDERLAY_RANGE_END:-192.168.1.250}"
# Multus NAD 名须与平台 networkprofile.MultusNADName 一致（无 VLAN 时为 macvlan）。
NAD_NAME="${K3S_UNDERLAY_NAD_NAME:-macvlan}"

echo "[underlay] Installing Multus + Macvlan (parent=${PARENT_IFACE}, subnet=${SUBNET})..."

# --- 1. 妫€鏌?Multus 鏄惁宸插畨瑁?---
if $KUBECTL --kubeconfig="${KUBECONFIG}" get pods -n kube-system -l app=multus -l component=multus \
    --no-headers 2>/dev/null | grep -q Running; then
  echo "[underlay] Multus already running, skip install."
else
  echo "[underlay] Applying Multus daemonset..."
  # 浣跨敤 Multus 瀹樻柟 thick daemonset锛堝吋瀹?k3s v1.31锛夈€?
  # 鐩存帴 apply 杩滅 manifest锛岀绾跨幆澧冨彲鏀逛负鎸傝浇鏈湴鍓湰銆?
  if ! $KUBECTL --kubeconfig="${KUBECONFIG}" apply -f \
      "https://raw.githubusercontent.com/k8snetworkplumbingwg/multus-cni/v4.2.0/deployments/multus-daemonset-thick.yml" \
      >/dev/null 2>&1; then
    echo "[underlay] WARN: failed to fetch multus manifest online, underlay disabled"
    exit 1
  fi

  # 绛夊緟 Multus daemonset 灏辩华
  echo "[underlay] Waiting for Multus daemonset ready (up to 120s)..."
  elapsed=0
  while [ "$elapsed" -lt 120 ]; do
    ready=$($KUBECTL --kubeconfig="${KUBECONFIG}" -n kube-system get ds kube-multus-ds \
            -o jsonpath='{.status.numberReady}' 2>/dev/null || echo 0)
    desired=$($KUBECTL --kubeconfig="${KUBECONFIG}" -n kube-system get ds kube-multus-ds \
              -o jsonpath='{.status.desiredNumberScheduled}' 2>/dev/null || echo 0)
    if [ -n "$ready" ] && [ -n "$desired" ] && [ "$ready" = "$desired" ] && [ "$ready" != "0" ]; then
      echo "[underlay] Multus ready ($ready/$desired)."
      break
    fi
    sleep 5
    elapsed=$((elapsed + 5))
  done
  if [ "$elapsed" -ge 120 ]; then
    echo "[underlay] WARN: Multus not ready after 120s, underlay may not function."
  fi
fi

# --- 2. 瀹夎 Whereabouts IPAM锛坢acvlan 闈欐€?IP 鍒嗛厤锛?--
if ! $KUBECTL --kubeconfig="${KUBECONFIG}" get ds whereabouts -n kube-system >/dev/null 2>&1; then
  echo "[underlay] Installing Whereabouts IPAM..."
  $KUBECTL --kubeconfig="${KUBECONFIG}" apply -f \
    "https://raw.githubusercontent.com/k8snetworkplumbingwg/whereabouts/v0.7.0/doc/daemonset-install.yaml" \
    -f "https://raw.githubusercontent.com/k8snetworkplumbingwg/whereabouts/v0.7.0/doc/whereabouts.cni.cncf.io_networkattachmentdefinitions_crds.yaml" \
    >/dev/null 2>&1 || echo "[underlay] WARN: whereabouts install partial"
fi

# --- 3. 鍒涘缓 Macvlan NetworkAttachmentDefinition ---
# 甯?underlay 娉ㄨВ鐨?Pod 浼氳 VortexOps 鎸囧畾闄勫姞姝ょ綉缁滐紝
# macvlan 妗ユ帴鍒扮埗鎺ュ彛锛孭od 浠?RANGE_START~RANGE_END 鎷跨墿鐞嗗眬鍩熺綉 IP銆?
echo "[underlay] Creating macvlan NAD in vortexops-dev..."
cat <<EOF | $KUBECTL --kubeconfig="${KUBECONFIG}" apply -f -
apiVersion: k8s.cni.cncf.io/v1
kind: NetworkAttachmentDefinition
metadata:
  name: ${NAD_NAME}
  namespace: vortexops-dev
  annotations:
    vortexops.io/network-profile: underlay
spec:
  config: |
    {
      "cniVersion": "0.4.0",
      "name": "${NAD_NAME}",
      "type": "macvlan",
      "mode": "bridge",
      "master": "${PARENT_IFACE}",
      "ipam": {
        "type": "whereabouts",
        "range": "${SUBNET}",
        "range_start": "${RANGE_START}",
        "range_end": "${RANGE_END}",
        "gateway": "${GATEWAY}"
      }
    }
EOF

# 鍦?vortexops namespace 涔熷缓涓€浠斤紝渚夸簬璺ㄥ懡鍚嶇┖闂存祴璇曘€?
cat <<EOF | $KUBECTL --kubeconfig="${KUBECONFIG}" apply -f -
apiVersion: k8s.cni.cncf.io/v1
kind: NetworkAttachmentDefinition
metadata:
  name: ${NAD_NAME}
  namespace: vortexops
  annotations:
    vortexops.io/network-profile: underlay
spec:
  config: |
    {
      "cniVersion": "0.4.0",
      "name": "${NAD_NAME}",
      "type": "macvlan",
      "mode": "bridge",
      "master": "${PARENT_IFACE}",
      "ipam": {
        "type": "whereabouts",
        "range": "${SUBNET}",
        "range_start": "${RANGE_START}",
        "range_end": "${RANGE_END}",
        "gateway": "${GATEWAY}"
      }
    }
EOF

echo "[underlay] Done. Underlay macvlan NAD '${NAD_NAME}' ready (parent=${PARENT_IFACE}, pool=${RANGE_START}~${RANGE_END})."
