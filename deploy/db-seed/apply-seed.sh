#!/bin/sh
# VortexOps 数据库 Mock 数据注入。
# 等待 schema 迁移就绪（vo_users 表存在）后执行 seed SQL。
# 依赖：postgres 服务 healthy，且 apiserver 已执行过 `migrate up`。
set -eu

PGHOST="${VORTEXOPS_DB_HOST:-postgres}"
PGPORT="${VORTEXOPS_DB_PORT:-5432}"
PGUSER="${VORTEXOPS_DB_USERNAME:-vortexops}"
PGPASSWORD="${VORTEXOPS_DB_PASSWORD:-vortexops_dev}"
PGDATABASE="${VORTEXOPS_DB_DATABASE:-vortexops}"
export PGHOST PGPORT PGUSER PGPASSWORD PGDATABASE

DIR="$(cd "$(dirname "$0")" && pwd)"
MAX_WAIT=${MAX_WAIT_SECONDS:-120}
elapsed=0

echo "[db-seed] Waiting for schema (vo_users table) up to ${MAX_WAIT}s ..."
while [ "$elapsed" -lt "$MAX_WAIT" ]; do
    if psql -tAc "SELECT to_regclass('public.vo_users')" | grep -q '^vo_users$'; then
        echo "[db-seed] vo_users table exists."
        break
    fi
    echo "[db-seed]   schema not ready (${elapsed}s), retrying in 3s ..."
    sleep 3
    elapsed=$((elapsed + 3))
done

if [ "$elapsed" -ge "$MAX_WAIT" ]; then
    echo "[db-seed] ERROR: schema not ready after ${MAX_WAIT}s."
    echo "[db-seed] Ensure 'apiserver migrate up' has been run."
    exit 1
fi

echo "[db-seed] Applying seed SQL ..."
for f in "${DIR}"/*.sql; do
    [ -e "$f" ] || continue
    echo "[db-seed] psql -f $(basename "$f")"
    psql -v ON_ERROR_STOP=1 -f "$f"
done

echo "[db-seed] Done. Mock login: admin / admin123"
