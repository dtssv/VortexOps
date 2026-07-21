#!/bin/sh
CFG=/etc/containerd/config.toml
if grep -q 'config_path' "$CFG"; then
  echo "ALREADY_SET"
else
  python3 - <<'PY'
p='/etc/containerd/config.toml'
s=open(p).read()
needle='[plugins."io.containerd.grpc.v1.cri".containerd]'
s=s.replace(needle, needle+'\n  config_path = "/etc/containerd/certs.d"',1)
open(p,'w').write(s)
PY
  echo "ADDED_CONFIG_PATH"
fi
grep -n 'config_path' "$CFG"
echo "---RESTART---"
kill -HUP $(pidof containerd) 2>/dev/null || true
sleep 3
echo "DONE"