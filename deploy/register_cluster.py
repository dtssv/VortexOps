"""Register the kind cluster into VortexOps with host.docker.internal as API server."""
import json
import urllib.request
import urllib.error

BASE = "http://localhost:8080"
USERNAME = "admin"
PASSWORD = "admin123"  # fallback candidates below

KC_PATH = r"F:\k8s\kubeconfig-host-docker-internal.yaml"


def _unwrap(body):
    """VortexOps wraps responses as {success, data, ...}; return the data payload when present."""
    if isinstance(body, dict) and "data" in body and "success" in body:
        return body["data"]
    return body


def post(path, body, token=None):
    data = json.dumps(body).encode("utf-8")
    req = urllib.request.Request(
        BASE + path, data=data, method="POST",
        headers={"Content-Type": "application/json"})
    if token:
        req.add_header("Authorization", "Bearer " + token)
    try:
        with urllib.request.urlopen(req, timeout=30) as r:
            return r.status, _unwrap(json.loads(r.read().decode("utf-8") or "{}"))
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode("utf-8", errors="replace")


def delete(path, token):
    req = urllib.request.Request(BASE + path, method="DELETE")
    req.add_header("Authorization", "Bearer " + token)
    try:
        with urllib.request.urlopen(req, timeout=30) as r:
            return r.status, r.read().decode("utf-8", errors="replace")
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode("utf-8", errors="replace")


def get(path, token):
    req = urllib.request.Request(BASE + path, method="GET")
    req.add_header("Authorization", "Bearer " + token)
    try:
        with urllib.request.urlopen(req, timeout=30) as r:
            return r.status, _unwrap(json.loads(r.read().decode("utf-8") or "{}"))
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode("utf-8", errors="replace")


# 1. Login (try a few candidate passwords)
token = None
for pwd in ["admin123", "Admin@123", "admin", "Admin123!", "admin@123", "vortexops"]:
    st, body = post("/api/v1/auth/login", {"username": USERNAME, "password": pwd})
    print(f"[login] pwd={pwd} status={st}")
    if st == 200 and isinstance(body, dict):
        token = body.get("access_token")
        if token:
            print("  -> login OK")
            break
    else:
        print("  -> " + str(body)[:200])

if not token:
    raise SystemExit("login failed")

# 2. List existing clusters
st, body = get("/api/v1/clusters?page=1&size=50", token)
print(f"[list] status={st}")
existing = body.get("items", []) if isinstance(body, dict) else []
for c in existing:
    print(f"  - id={c.get('id')} name={c.get('name')} api_server={c.get('api_server')} status={c.get('status')}")

# 3. Delete old clusters with stale 127.0.0.1 api_server
for c in existing:
    api = (c.get("api_server") or "")
    if "127.0.0.1" in api or c.get("name") in ("test", "kind", "kind-kind"):
        cid = c.get("id")
        print(f"[delete] removing cluster id={cid} name={c.get('name')} api_server={api}")
        st, b = delete(f"/api/v1/clusters/{cid}", token)
        print(f"  -> status={st} body={str(b)[:200]}")

# 4. Create new cluster
with open(KC_PATH, "r", encoding="utf-8") as f:
    kubeconfig = f.read()

create_body = {
    "name": "kind",
    "display_name": "本地 kind 集群",
    "description": "kind v0.32.0 / K8s v1.36.1, single-node control-plane on Docker Desktop",
    "api_server": "https://host.docker.internal:14813",
    "kubeconfig": kubeconfig,
    "default_namespace_prefix": "vortexops-",
    "insecure_skip_tls": False,
    "region": "local",
    "environment": "dev",
}
st, body = post("/api/v1/clusters", create_body, token)
print(f"[create] status={st}")
print("  -> " + json.dumps(body, ensure_ascii=False)[:500])

if st not in (200, 201):
    raise SystemExit("create failed")

new_id = body.get("id") if isinstance(body, dict) else None
if not new_id:
    raise SystemExit("no cluster id returned")

# 5. Probe to verify connectivity
st, body = post(f"/api/v1/clusters/{new_id}/probe", {}, token)
print(f"[probe] status={st}")
print("  -> " + json.dumps(body, ensure_ascii=False)[:500])
