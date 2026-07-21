#!/usr/bin/env bash
# =============================================================================
# 初始化数据库 schema + 创建 admin 用户
# 用法:
#   物理机: ./scripts/init-db.sh --pg-host 192.168.1.20 --pg-user vortexops --pg-password '...' --admin-password '...'
#   K8s:    ./scripts/init-db.sh --k8s --namespace vortexops
# =============================================================================
set -euo pipefail

PG_HOST=""
PG_PORT="5432"
PG_USER="vortexops"
PG_PASSWORD=""
PG_DB="vortexops"
ADMIN_USER="admin"
ADMIN_PASSWORD=""
ADMIN_EMAIL="admin@corp"
K8S_MODE=false
K8S_NAMESPACE="vortexops"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --pg-host)       PG_HOST="$2"; shift 2 ;;
    --pg-port)       PG_PORT="$2"; shift 2 ;;
    --pg-user)       PG_USER="$2"; shift 2 ;;
    --pg-password)   PG_PASSWORD="$2"; shift 2 ;;
    --pg-db)         PG_DB="$2"; shift 2 ;;
    --admin-user)    ADMIN_USER="$2"; shift 2 ;;
    --admin-password) ADMIN_PASSWORD="$2"; shift 2 ;;
    --admin-email)   ADMIN_EMAIL="$2"; shift 2 ;;
    --k8s)           K8S_MODE=true; shift ;;
    --namespace)     K8S_NAMESPACE="$2"; shift 2 ;;
    -h|--help)
      cat <<EOF
用法:
  物理机: $0 --pg-host <ip> --pg-password <pw> --admin-password <pw>
  K8s:    $0 --k8s --namespace vortexops
EOF
      exit 0 ;;
    *) echo "未知参数: $1"; exit 1 ;;
  esac
done

REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
SCHEMA_SQL="$REPO_ROOT/schema.sql"

if [[ ! -f "$SCHEMA_SQL" ]]; then
  echo "ERROR: schema.sql 不存在: $SCHEMA_SQL"
  exit 1
fi

echo "============================================"
echo " VortexOps DB Init"
echo "============================================"

if [[ "$K8S_MODE" == "true" ]]; then
  echo "[init] K8s 模式，通过 apiserver Pod 执行..."
  echo "[init] 1/2 migrate up..."
  kubectl -n "$K8S_NAMESPACE" exec deploy/vortexops-apiserver -- \
    /app/vortexops migrate up

  echo "[init] 2/2 bootstrap-admin..."
  kubectl -n "$K8S_NAMESPACE" exec deploy/vortexops-apiserver -- \
    /app/vortexops bootstrap-admin \
      --username "$ADMIN_USER" \
      --password "${ADMIN_PASSWORD:-$(openssl rand -base64 12)}" \
      --email "$ADMIN_EMAIL"
else
  if [[ -z "$PG_HOST" || -z "$PG_PASSWORD" ]]; then
    echo "ERROR: 物理机模式需 --pg-host 与 --pg-password"
    exit 1
  fi

  export PGPASSWORD="$PG_PASSWORD"

  echo "[init] 1/3 创建数据库与扩展..."
  psql -h "$PG_HOST" -p "$PG_PORT" -U "$PG_USER" -d postgres <<EOF
CREATE DATABASE "$PG_DB" WITH OWNER "$PG_USER" ENCODING 'UTF8';
EOF
  psql -h "$PG_HOST" -p "$PG_PORT" -U "$PG_USER" -d "$PG_DB" <<EOF
CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
EOF

  echo "[init] 2/3 应用 schema.sql..."
  psql -h "$PG_HOST" -p "$PG_PORT" -U "$PG_USER" -d "$PG_DB" -f "$SCHEMA_SQL"

  echo "[init] 3/3 创建 admin 用户..."
  ADMIN_HASH="$(openssl rand -base64 16)"
  psql -h "$PG_HOST" -p "$PG_PORT" -U "$PG_USER" -d "$PG_DB" <<EOF
INSERT INTO vo_users (username, email, password_hash, status, created_at, updated_at)
VALUES ('$ADMIN_USER', '$ADMIN_EMAIL', crypt('${ADMIN_PASSWORD:-CHANGE_ME}', gen_salt('bf')), 'active', now(), now())
ON CONFLICT (username) DO NOTHING;
EOF
fi

echo ""
echo "============================================"
echo " 数据库初始化完成"
echo "============================================"
echo " admin 用户: $ADMIN_USER"
[[ -z "$ADMIN_PASSWORD" ]] && echo " admin 密码: 已随机生成（见上方输出）"
echo "============================================"
