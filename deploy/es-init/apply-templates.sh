#!/bin/sh
# VortexOps ES 初始化：创建索引模板（幂等）。
# 开发环境 xpack.security=false，无需认证。
set -eu

ES_URL="${VORTEXOPS_ES_URL:-http://elasticsearch:9200}"
DIR="$(cd "$(dirname "$0")" && pwd)"

echo "[es-init] Applying index templates to ${ES_URL} ..."

# 索引模板 v1（兼容 ES 8.x _index_template API）。
apply_template() {
  name="$1"
  file="$2"
  echo "[es-init] PUT _index_template/${name}"
  # --fail-with-body 在 curl 7.76+ 可用；此处用 -sS -w 状态码兜底。
  code=$(curl -sS -o /tmp/es_resp -w '%{http_code}' \
    -X PUT "${ES_URL}/_index_template/${name}" \
    -H 'Content-Type: application/json' \
    --data-binary "@${file}" || true)
  echo "[es-init]   -> HTTP ${code}"
  cat /tmp/es_resp; echo
  if [ "${code}" -ge 200 ] && [ "${code}" -lt 300 ]; then
    return 0
  fi
  return 1
}

apply_template "vortexops-audit"       "${DIR}/template_audit.json"
apply_template "vortexops-build-logs"  "${DIR}/template_logs.json"

# 创建当月占位索引，便于开发期在没有数据时也能在 Kibana/前端看到索引存在。
MONTH=$(date -u +%Y-%m)
echo "[es-init] Creating placeholder indices for ${MONTH} ..."
curl -sS -X PUT "${ES_URL}/vortexops-audit-${MONTH}" \
  -H 'Content-Type: application/json' -d '{}' || true
echo
curl -sS -X PUT "${ES_URL}/vortexops-build-logs-${MONTH}" \
  -H 'Content-Type: application/json' -d '{}' || true
echo

echo "[es-init] Done."
