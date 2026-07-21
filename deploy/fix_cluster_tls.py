"""Patch cluster 4 to skip TLS verification (dev only: kind cert lacks host.docker.internal SAN)."""
import json
import urllib.request
import urllib.error

BASE = "http://localhost:8080"


def post(path, body, token=None):
    data = json.dumps(body).encode("utf-8")
    req = urllib.request.Request(BASE + path, data=data, method="POST",
                                 headers={"Content-Type": "application/json"})
    if token:
        req.add_header("Authorization", "Bearer " + token)
    try:
        with urllib.request.urlopen(req, timeout=30) as r:
            raw = r.read().decode("utf-8") or "{}"
            body_obj = json.loads(raw)
            if isinstance(body_obj, dict) and "data" in body_obj:
                return r.status, body_obj["data"]
            return r.status, body_obj
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode("utf-8", errors="replace")


def put(path, body, token):
    data = json.dumps(body).encode("utf-8")
    req = urllib.request.Request(BASE + path, data=data, method="PUT",
                                 headers={"Content-Type": "application/json"})
    req.add_header("Authorization", "Bearer " + token)
    try:
        with urllib.request.urlopen(req, timeout=30) as r:
            raw = r.read().decode("utf-8") or "{}"
            body_obj = json.loads(raw)
            if isinstance(body_obj, dict) and "data" in body_obj:
                return r.status, body_obj["data"]
            return r.status, body_obj
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode("utf-8", errors="replace")


# 1. Login
st, body = post("/api/v1/auth/login", {"username": "admin", "password": "admin123"})
token = body.get("access_token")
print(f"[login] status={st}")

# 2. Get current version of cluster 4
st, body = post("/api/v1/clusters/4/probe", {}, token)  # just to confirm 422
print(f"[probe-before] status={st}")

# Use GET to fetch cluster 4 with version
import urllib.request as ur
req = ur.Request(BASE + "/api/v1/clusters/4", method="GET")
req.add_header("Authorization", "Bearer " + token)
with ur.urlopen(req, timeout=30) as r:
    obj = json.loads(r.read().decode("utf-8"))
    c = obj.get("data", obj)
    version = c.get("version")
    print(f"[get] cluster 4 version={version} status={c.get('status')}")

# 3. PUT to set insecure_skip_tls=true
st, body = put("/api/v1/clusters/4", {
    "insecure_skip_tls": True,
    "version": version,
}, token)
print(f"[put] status={st}")
print("  -> " + json.dumps(body, ensure_ascii=False)[:300])

# 4. Re-probe
st, body = post("/api/v1/clusters/4/probe", {}, token)
print(f"[probe-after] status={st}")
print("  -> " + json.dumps(body, ensure_ascii=False)[:500])
