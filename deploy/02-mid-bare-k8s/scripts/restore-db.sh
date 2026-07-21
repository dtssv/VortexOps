#!/usr/bin/env bash
# =============================================================================
# PostgreSQL 逻辑恢复（pg_restore）
# 用法:
#   ./scripts/restore-db.sh --pg-host 192.168.1.20 --dump-file /backups/vortexops_20260716.dump
# =============================================================================
set -euo pipefail

PG_HOST=""
PG_PORT="5432"
PG_USER="vortexops"
PG_PASSWORD=""
PG_DB="vortexops"
DUMP_FILE=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --pg-host)     PG_HOST="$2"; shift 2 ;;
    --pg-port)     PG_PORT="$2"; shift 2 ;;
    --pg-user)     PG_USER="$2"; shift 2 ;;
    --pg-password) PG_PASSWORD="$2"; shift 2 ;;
    --pg-db)       PG_DB="$2"; shift 2 ;;
    --dump-file)   DUMP_FILE="$2"; shift 2 ;;
    -h|--help) echo "用法: $0 --pg-host <ip> --dump-file <path>"; exit 0 ;;
    *) echo "未知参数: $1"; exit 1 ;;
  esac
done

if [[ -z "$PG_HOST" || -z "$DUMP_FILE" ]]; then
  echo "ERROR: --pg-host 与 --dump-file 必填"
  exit 1
fi

if [[ ! -f "$DUMP_FILE" ]]; then
  echo "ERROR: 备份文件不存在: $DUMP_FILE"
  exit 1
fi

echo "!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!"
echo "! 危险操作：将覆盖目标数据库"
echo "! PG_HOST: $PG_HOST"
echo "! PG_DB:   $PG_DB"
echo "! DUMP:    $DUMP_FILE"
echo "!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!"
read -r -p "确认继续？输入 yes 执行：" CONFIRM
[[ "$CONFIRM" == "yes" ]] || { echo "已取消。"; exit 0; }

export PGPASSWORD="${PG_PASSWORD:-}"

echo "[restore] 1/3 断开活跃连接..."
psql -h "$PG_HOST" -p "$PG_PORT" -U "$PG_USER" -d postgres <<EOF
SELECT pg_terminate_backend(pid) FROM pg_stat_activity
WHERE datname='$PG_DB' AND pid <> pg_backend_pid();
EOF

echo "[restore] 2/3 重建数据库..."
psql -h "$PG_HOST" -p "$PG_PORT" -U "$PG_USER" -d postgres <<EOF
DROP DATABASE IF EXISTS "$PG_DB";
CREATE DATABASE "$PG_DB" WITH OWNER "$PG_USER";
EOF
psql -h "$PG_HOST" -p "$PG_PORT" -U "$PG_USER" -d "$PG_DB" <<EOF
CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
EOF

echo "[restore] 3/3 pg_restore..."
pg_restore -h "$PG_HOST" -p "$PG_PORT" -U "$PG_USER" -d "$PG_DB" --no-owner --no-privileges "$DUMP_FILE"

echo ""
echo "[restore] 完成。请重启平台组件使缓存失效。"
