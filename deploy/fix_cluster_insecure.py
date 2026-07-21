"""Re-register cluster with insecure-skip-tls-verify in kubeconfig itself.

Kind's API server cert is signed for: kind-control-plane, kubernetes, kubernetes.default,
kubernetes.default.svc, kubernetes.default.svc.cluster.local, localhost.
It does NOT include host.docker.internal, so when the apiserver container (inside Docker)
connects to host.docker.internal:14813, TLS verification fails.

Two options:
  A. Modify kubeconfig: remove certificate-authority-data, set insecure-skip-tls-verify: true.
     client-go's RESTConfigFromKubeConfig honors this flag. Immediate, no rebuild.
  B. Patch backend so probeWithKubeconfig respects cluster.insecure_skip_tls (real bug fix).

Using option A here for immediate resolution; option B is a follow-up.
"""
import json
import urllib.request
import urllib.error

BASE = "http://localhost:8080"
KC_IN = r"F:\k8s\kubeconfig-host-docker-internal.yaml"
KC_OUT = r"F:\k8s\kubeconfig-insecure.yaml"


def _unwrap(body):
    if isinstance(body, dict) and "data" in body and "success" in body:
        return body["data"]
    return body


def post(path, body, token=None):
    data = json.dumps(body).encode("utf-8")
    req = urllib.request.Request(BASE + path, data=data, method="POST",
                                 headers={"Content-Type": "application/json"})
    if token:
        req.add_header("Authorization", "Bearer " + token)
    try:
        with urllib.request.urlopen(req, timeout=60) as r:
            return r.status, _unwrap(json.loads(r.read().decode("utf-8") or "{}"))
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode("utf-8", errors="replace")


def put(path, body, token):
    data = json.dumps(body).encode("utf-8")
    req = urllib.request.Request(BASE + path, data=data, method="PUT",
                                 headers={"Content-Type": "application/json"})
    req.add_header("Authorization", "Bearer " + token)
    try:
        with urllib.request.urlopen(req, timeout=60) as r:
            return r.status, _unwrap(json.loads(r.read().decode("utf-8") or "{}"))
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode("utf-8", errors="replace")


def get(path, token):
    req = urllib.request.Request(BASE + path, method="GET")
    req.add_header("Authorization", "Bearer " + token)
    with urllib.request.urlopen(req, timeout=30) as r:
        return _unwrap(json.loads(r.read().decode("utf-8") or "{}"))


# 1. Transform kubeconfig: drop certificate-authority-data, add insecure-skip-tls-verify: true
with open(KC_IN, "r", encoding="utf-8") as f:
    text = f.read()

import re
# Remove certificate-authority-data line entirely
text = re.sub(r"\n\s*certificate-authority-data: [^\n]+", "", text)
# Ensure insecure-skip-tls-verify: true is set inside the cluster block
if "insecure-skip-tls-verify" not in text:
    text = text.replace(
        "    server: https://host.docker.internal:14813",
        "    insecure-skip-tls-verify: true\n    server: https://host.docker.internal:14813",
    )

with open(KC_OUT, "w", encoding="utf-8") as f:
    f.write(text)
print(f"[kubeconfig] wrote insecure variant to {KC_OUT}")
# sanity check
assert "insecure-skip-tls-verify: true" in text
assert "certificate-authority-data" not in text
print("[kubeconfig] verified: insecure-skip-tls-verify=true, no CA data")

# 2. Login
st, body = post("/api/v1/auth/login", {"username": "admin", "password": "admin123"})
token = body.get("access_token")
print(f"[login] status={st}")

# 3. Get cluster 4 current version
c = get("/api/v1/clusters/4", token)
version = c.get("version")
print(f"[get] cluster 4 version={version} status={c.get('status')}")

# 4. PUT updated kubeconfig (this also clears client pool cache)
with open(KC_OUT, "r", encoding="utf-8") as f:
    new_kc = f.read()

st, body = put("/api/v1/clusters/4", {
    "kubeconfig": new_kc,
    "version": version,
}, token)
print(f"[put] status={st} new_version={body.get('version') if isinstance(body, dict) else '?'}")

# 5. Probe
st, body = post("/api/v1/clusters/4/probe", {}, token)
print(f"[probe] status={st}")
print("  -> " + json.dumps(body, ensure_ascii=False)[:600])
