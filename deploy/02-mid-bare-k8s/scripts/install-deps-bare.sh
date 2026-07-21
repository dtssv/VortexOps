#!/usr/bin/env bash
# =============================================================================
# 物理机方案：安装依赖组件（PostgreSQL 主从 / Redis Sentinel / MinIO / Jenkins / Harbor）
# 用法: sudo ./scripts/install-deps-bare.sh --role <pg-master|pg-replica|redis-master|redis-replica|redis-sentinel|minio|jenkins|harbor>
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
LAYER_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

ROLE=""
HOST_IP=""
CLUSTER_NODES=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --role)          ROLE="$2"; shift 2 ;;
    --host-ip)       HOST_IP="$2"; shift 2 ;;
    --cluster-nodes) CLUSTER_NODES="$2"; shift 2 ;;
    -h|--help)
      cat <<EOF
用法: $0 --role <role> [OPTIONS]
角色:
  pg-master         PostgreSQL 主库
  pg-replica        PostgreSQL 从库
  redis-master      Redis 主
  redis-replica     Redis 从
  redis-sentinel    Redis Sentinel
  minio             MinIO 节点（4 节点之一）
  jenkins           Jenkins controller
  harbor            Harbor 镜像仓库
选项:
  --host-ip <ip>           本机 IP
  --cluster-nodes <list>   集群节点 IP 列表（逗号分隔，MinIO 用）
EOF
      exit 0 ;;
    *) echo "未知参数: $1"; exit 1 ;;
  esac
done

if [[ $EUID -ne 0 ]]; then
  echo "ERROR: 请使用 sudo 执行"
  exit 1
fi

if [[ -z "$ROLE" ]]; then
  echo "ERROR: --role 必填"
  exit 1
fi

echo "============================================"
echo " VortexOps Bare Deps Install"
echo "============================================"
echo " Role: $ROLE"
echo " Host: ${HOST_IP:-未指定}"
echo "--------------------------------------------"

case "$ROLE" in
  pg-master|pg-replica)
    echo "[pg] 安装 PostgreSQL 16 + pgvector..."
    if ! command -v psql >/dev/null 2>&1; then
      sh -c 'echo "deb https://apt.postgresql.org/pub/repos/apt $(lsb_release -cs)-pgdg main" > /etc/apt/sources.list.d/pgdg.list'
      curl -fsSL https://www.postgresql.org/media/keys/ACCC4CF8.asc | gpg --dearmor -o /etc/apt/trusted.gpg.d/postgresql.gpg
      apt-get update
      apt-get install -y postgresql-16 postgresql-16-pgvector
    fi

    echo "[pg] 部署配置文件..."
    cp "$LAYER_DIR/bare/postgres/postgresql.conf" /etc/postgresql/16/main/postgresql.conf
    cp "$LAYER_DIR/bare/postgres/pg_hba.conf" /etc/postgresql/16/main/pg_hba.conf
    chown postgres:postgres /etc/postgresql/16/main/{postgresql,pg_hba}.conf

    if [[ "$ROLE" == "pg-master" ]]; then
      echo "[pg-master] 创建复制用户..."
      sudo -u postgres psql -c "CREATE ROLE replication WITH REPLICATION LOGIN PASSWORD 'CHANGE_ME_REPL_PASSWORD';" || true
      sudo -u postgres psql -c "CREATE EXTENSION IF NOT EXISTS vector;" -d postgres || true
      sudo -u postgres psql -c "CREATE EXTENSION IF NOT EXISTS pg_trgm;" -d postgres || true
    else
      echo "[pg-replica] 用 pg_basebackup 初始化从库..."
      echo "  手动执行（替换 IP）:"
      echo "  sudo -u postgres pg_basebackup -h <master-ip> -U replication -D /var/lib/postgresql/16/main -Fp -Xs -P -R"
      echo "  sudo systemctl start postgresql@16-main"
    fi

    systemctl restart postgresql@16-main
    systemctl enable postgresql@16-main
    ;;

  redis-master|redis-replica|redis-sentinel)
    echo "[redis] 安装 Redis 7..."
    if ! command -v redis-server >/dev/null 2>&1; then
      apt-get update
      apt-get install -y redis-server
    fi

    case "$ROLE" in
      redis-master)
        cat > /etc/redis/redis.conf <<EOF
bind ${HOST_IP:-0.0.0.0}
port 6379
requirepass CHANGE_ME_REDIS_PASSWORD
masterauth CHANGE_ME_REDIS_PASSWORD
appendonly yes
appendfsync everysec
save 60 1000
maxmemory 2gb
maxmemory-policy allkeys-lru
EOF
        ;;
      redis-replica)
        cat > /etc/redis/redis.conf <<EOF
bind ${HOST_IP:-0.0.0.0}
port 6379
replicaof <master-ip>:6379
masterauth CHANGE_ME_REDIS_PASSWORD
requirepass CHANGE_ME_REDIS_PASSWORD
appendonly yes
EOF
        ;;
      redis-sentinel)
        cat > /etc/redis/sentinel.conf <<EOF
port 26379
bind ${HOST_IP:-0.0.0.0}
sentinel monitor vortexops-redis-master <master-ip> 6379 2
sentinel auth-pass vortexops-redis-master CHANGE_ME_REDIS_PASSWORD
sentinel down-after-milliseconds vortexops-redis-master 5000
sentinel failover-timeout vortexops-redis-master 30000
sentinel parallel-syncs vortexops-redis-master 1
EOF
        ;;
    esac

    systemctl restart redis-server
    systemctl enable redis-server
    ;;

  minio)
    echo "[minio] 安装 MinIO..."
    if ! command -v minio >/dev/null 2>&1; then
      curl -fsSL https://dl.min.io/server/minio/release/linux-amd64/minio -o /usr/local/bin/minio
      chmod +x /usr/local/bin/minio
    fi

    useradd -r -s /sbin/nologin minio 2>/dev/null || true
    mkdir -p /data/minio
    chown minio:minio /data/minio

    cat > /etc/systemd/system/minio.service <<EOF
[Unit]
Description=MinIO Object Storage
After=network.target

[Service]
User=minio
Group=minio
Environment="MINIO_ROOT_USER=admin"
Environment="MINIO_ROOT_PASSWORD=CHANGE_ME_S3_SECRET"
ExecStart=/usr/local/bin/minio server /data/minio \\
  --address ":9000" \\
  --console-address ":9001"
Restart=on-failure

[Install]
WantedBy=multi-user.target
EOF
    systemctl daemon-reload
    systemctl enable minio
    systemctl restart minio
    echo "[minio] 4 节点需分别执行后，用 mc 配置 erasure coding"
    ;;

  jenkins|harbor)
    echo "[$ROLE] 建议使用官方安装文档:"
    [[ "$ROLE" == "jenkins" ]] && echo "  https://www.jenkins.io/doc/book/installing/linux/"
    [[ "$ROLE" == "harbor" ]]  && echo "  https://goharbor.io/docs/latest/install-config/"
    ;;

  *)
    echo "ERROR: 未知 role: $ROLE"
    exit 1
    ;;
esac

echo ""
echo "[$ROLE] 安装完成。请验证服务状态: systemctl status <service>"
