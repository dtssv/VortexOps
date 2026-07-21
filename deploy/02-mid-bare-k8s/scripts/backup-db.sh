#!/usr/bin/env bash
# =============================================================================
# PostgreSQL 逻辑备份（pg_dump）
# 用法:
#   ./scripts/backup-db.sh --pg-host 192.168.1.20 --backup-dir /backups
#   K8s: ./scripts/backup-db.sh --k8s --namespace vortexops-infra --backup-dir /tmp
# =============================================================================
set -euo pipefail

PG_HOST=""
PG_PORT="5432"
PG_USER="vortexops"
PG_PASSWORD=""
PG_DB="vortexops"
BACKUP_DIR="/backups"
K8S_MODE=false
K8S_NAMESPACE="vortexops-infra"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --pg-host)     PG_HOST="$2"; shift 2 ;;
    --pg-port)     PG_PORT="$2"; shift 2 ;;
    --pg-user)     PG_USER="$2"; shift 2 ;;
    --pg-password) PG_PASSWORD="$2"; shift 2 ;;
    --pg-db)       PG_DB="$2"; shift 2 ;;
    --backup-dir)  BACKUP_DIR="$2"; shift 2 ;;
    --k8s)         K8S_MODE=true; shift ;;
    --namespace)   K8S_NAMESPACE="$2"; shift 2 ;;
    -h|--help) echo "用法: $0 --pg-host <ip> --backup-dir <dir> [--k8s]"; exit 0 ;;
    *) echo "未知参数: $1"; exit 1 ;;
  esac
done

TIMESTAMP="$(date +%Y%m%d-%H%M%S)"
FILE_NAME="vortexops_${TIMESTAMP}.dump"

echo "============================================"
echo " VortexOps DB Backup"
echo "============================================"

if [[ "$K8S_MODE" == "true" ]]; then
  POD="$(kubectl -n "$K8S_NAMESPACE" get pod -l app.kubernetes.io/name=postgresql -o jsonpath='{.items[0].metadata.name}')"
  echo "[backup] K8s 模式，从 Pod $POD 导出..."
  kubectl -n "$K8S_NAMESPACE" exec "$POD" -- pg_dump -U "$PG_USER" -d "$PG_DB" -Fc -f "/tmp/$FILE_NAME"
  kubectl -n "$K8S_NAMESPACE" cp "$POD:/tmp/$FILE_NAME" "$BACKUP_DIR/$FILE_NAME"
  kubectl -n "$K8S_NAMESPACE" exec "$POD" -- rm "/tmp/$FILE_NAME"
else
  if [[ -z "$PG_HOST" ]]; then echo "ERROR: --pg-host 必填"; exit 1; fi
  export PGPASSWORD="${PG_PASSWORD:-}"
  echo "[backup] 物理机模式，pg_dump 主库..."
  mkdir -p "$BACKUP_DIR"
  pg_dump -h "$PG_HOST" -p "$PG_PORT" -U "$PG_USER" -d "$PG_DB" -Fc -f "$BACKUP_DIR/$FILE_NAME"
fi

SIZE=$(du -h "$BACKUP_DIR/$FILE_NAME" | cut -f1)
echo ""
echo "[backup] 备份完成:"
echo "  文件: $BACKUP_DIR/$FILE_NAME"
echo "  大小: $SIZE"
echo "  时间: $TIMESTAMP"

# 清理 30 天前的备份
find "$BACKUP_DIR" -name "vortexops_*.dump" -mtime +30 -delete 2>/dev/null || true
echo "[backup] 已清理 30 天前的旧备份"
