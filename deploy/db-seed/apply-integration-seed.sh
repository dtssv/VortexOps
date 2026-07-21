#!/bin/sh
# VortexOps 构建集成种子：自动创建默认 Jenkins/Registry 实例并绑定系统设置。
# 等 apiserver 就绪后通过 REST API 调用，幂等可重复执行。
# 运行：docker compose -f deploy/docker-compose.dev.yml run --rm integration-seed
set -eu

API_BASE="${VORTEXOPS_API_BASE:-http://apiserver:8080/api/v1}"
ADMIN_USER="${VORTEXOPS_ADMIN_USER:-admin}"
ADMIN_PASS="${VORTEXOPS_ADMIN_PASSWORD:-admin123}"

JENKINS_URL="${VORTEXOPS_JENKINS_URL:-http://jenkins:8080}"
JENKINS_USER="${VORTEXOPS_JENKINS_USER:-admin}"
JENKINS_TOKEN="${VORTEXOPS_JENKINS_PASSWORD:-vortexops_dev}"
REGISTRY_URL="${VORTEXOPS_HARBOR_URL:-http://registry:5000}"
REGISTRY_USER="${VORTEXOPS_HARBOR_USER:-admin}"
REGISTRY_PASS="${VORTEXOPS_HARBOR_PASSWORD:-vortexops_dev}"

echo "[integration-seed] Waiting for apiserver up to 180s ..."
elapsed=0
TOKEN=""
while [ "$elapsed" -lt 180 ]; do
    LOGIN_RESP=$(curl -sf -X POST "${API_BASE}/auth/login" \
        -H "Content-Type: application/json" \
        -d "{\"username\":\"${ADMIN_USER}\",\"password\":\"${ADMIN_PASS}\"}" 2>/dev/null || true)
    if [ -n "$LOGIN_RESP" ]; then
        TOKEN=$(echo "$LOGIN_RESP" | sed -n 's/.*"access_token":"\([^"]*\)".*/\1/p')
        if [ -n "$TOKEN" ]; then
            echo "[integration-seed] apiserver ready, got token."
            break
        fi
    fi
    sleep 5
    elapsed=$((elapsed + 5))
done

if [ -z "$TOKEN" ]; then
    echo "[integration-seed] ERROR: apiserver not ready or login failed."
    exit 1
fi

AUTH="Authorization: Bearer ${TOKEN}"

# 把 JSON 对象 base64 编码（后端 Payload []byte 期望 base64 字符串）。
b64_payload() {
    # $1 = JSON 字符串
    printf '%s' "$1" | base64 -w0 2>/dev/null || printf '%s' "$1" | base64
}

# ---------- 1. 创建/复用默认 Jenkins 实例 ----------
echo "[integration-seed] Ensuring default Jenkins credential ..."
JK_PAYLOAD=$(b64_payload "{\"username\":\"${JENKINS_USER}\",\"api_token\":\"${JENKINS_TOKEN}\"}")
JK_CRED_RESP=$(curl -sf -X POST "${API_BASE}/credentials" \
    -H "${AUTH}" -H "Content-Type: application/json" \
    -d "{\"name\":\"jenkins-default\",\"kind\":\"jenkins\",\"scope\":\"platform\",\"payload\":\"${JK_PAYLOAD}\"}" 2>/dev/null || true)
JK_CRED_ID=$(echo "$JK_CRED_RESP" | sed -n 's/.*"id":\([0-9]*\).*/\1/p' | head -1)

if [ -z "$JK_CRED_ID" ]; then
    JK_CRED_LIST=$(curl -sf "${API_BASE}/credentials?kind=jenkins&size=200" -H "${AUTH}" 2>/dev/null || echo '{"items":[]}')
    JK_CRED_ID=$(echo "$JK_CRED_LIST" | sed -n 's/.*"id":\([0-9]*\).*/\1/p' | head -1)
fi
echo "[integration-seed] Jenkins credential id=${JK_CRED_ID}"

if [ -n "$JK_CRED_ID" ]; then
    JK_RESP=$(curl -sf -X POST "${API_BASE}/jenkins-instances" \
        -H "${AUTH}" -H "Content-Type: application/json" \
        -d "{\"name\":\"default\",\"url\":\"${JENKINS_URL}\",\"credential_id\":${JK_CRED_ID},\"default_job_folder\":\"vortexops\",\"is_default\":true,\"status\":\"active\"}" 2>/dev/null || true)
    JK_ID=$(echo "$JK_RESP" | sed -n 's/.*"id":\([0-9]*\).*/\1/p' | head -1)
    if [ -z "$JK_ID" ]; then
        JK_LIST=$(curl -sf "${API_BASE}/jenkins-instances?size=200" -H "${AUTH}" 2>/dev/null || echo '{"items":[]}')
        JK_ID=$(echo "$JK_LIST" | sed -n 's/.*"id":\([0-9]*\).*/\1/p' | head -1)
    fi
    if [ -n "$JK_ID" ]; then
        curl -sf -X PUT "${API_BASE}/system-settings/platform.default_jenkins_id" \
            -H "${AUTH}" -H "Content-Type: application/json" \
            -d "{\"value\":${JK_ID},\"description\":\"系统默认 Jenkins 实例 ID\",\"is_public\":false}" >/dev/null
        echo "[integration-seed] Default Jenkins bound: id=${JK_ID}"
    fi
fi

# ---------- 2. 创建/复用默认镜像仓库 ----------
echo "[integration-seed] Ensuring default registry credential ..."
REG_PAYLOAD=$(b64_payload "{\"username\":\"${REGISTRY_USER}\",\"password\":\"${REGISTRY_PASS}\"}")
REG_CRED_RESP=$(curl -sf -X POST "${API_BASE}/credentials" \
    -H "${AUTH}" -H "Content-Type: application/json" \
    -d "{\"name\":\"registry-default\",\"kind\":\"registry\",\"scope\":\"platform\",\"payload\":\"${REG_PAYLOAD}\"}" 2>/dev/null || true)
REG_CRED_ID=$(echo "$REG_CRED_RESP" | sed -n 's/.*"id":\([0-9]*\).*/\1/p' | head -1)

if [ -z "$REG_CRED_ID" ]; then
    REG_CRED_LIST=$(curl -sf "${API_BASE}/credentials?kind=registry&size=200" -H "${AUTH}" 2>/dev/null || echo '{"items":[]}')
    REG_CRED_ID=$(echo "$REG_CRED_LIST" | sed -n 's/.*"id":\([0-9]*\).*/\1/p' | head -1)
fi
echo "[integration-seed] Registry credential id=${REG_CRED_ID}"

if [ -n "$REG_CRED_ID" ]; then
    REG_RESP=$(curl -sf -X POST "${API_BASE}/registries" \
        -H "${AUTH}" -H "Content-Type: application/json" \
        -d "{\"name\":\"default\",\"type\":\"docker_registry\",\"url\":\"${REGISTRY_URL}\",\"credential_id\":${REG_CRED_ID},\"is_default\":true,\"status\":\"active\"}" 2>/dev/null || true)
    REG_ID=$(echo "$REG_RESP" | sed -n 's/.*"id":\([0-9]*\).*/\1/p' | head -1)
    if [ -z "$REG_ID" ]; then
        REG_LIST=$(curl -sf "${API_BASE}/registries?size=200" -H "${AUTH}" 2>/dev/null || echo '{"items":[]}')
        REG_ID=$(echo "$REG_LIST" | sed -n 's/.*"id":\([0-9]*\).*/\1/p' | head -1)
    fi
    if [ -n "$REG_ID" ]; then
        curl -sf -X PUT "${API_BASE}/system-settings/platform.default_registry_id" \
            -H "${AUTH}" -H "Content-Type: application/json" \
            -d "{\"value\":${REG_ID},\"description\":\"系统默认镜像仓库 ID\",\"is_public\":false}" >/dev/null
        echo "[integration-seed] Default registry bound: id=${REG_ID}"
    fi
fi

echo "[integration-seed] Done."
