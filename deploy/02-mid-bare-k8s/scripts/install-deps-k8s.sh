#!/usr/bin/env bash
# =============================================================================
# 一键安装依赖组件到 K8s vortexops-infra namespace
# 用法:
#   ./scripts/install-deps-k8s.sh --all
#   ./scripts/install-deps-k8s.sh --component postgres
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
LAYER_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
DEPS_DIR="$LAYER_DIR/manifests/deps"
NAMESPACE="vortexops-infra"

COMPONENTS=()
INSTALL_ALL=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --all)           INSTALL_ALL=true; shift ;;
    --component)     COMPONENTS+=("$2"); shift 2 ;;
    --namespace|-n)  NAMESPACE="$2"; shift 2 ;;
    -h|--help)
      cat <<EOF
用法: $0 [--all | --component <name>] [--namespace <ns>]
组件:
  postgres pgbouncer redis minio nats opensearch jenkins harbor
EOF
      exit 0 ;;
    *) echo "未知参数: $1"; exit 1 ;;
  esac
done

if [[ "$INSTALL_ALL" == "true" ]]; then
  COMPONENTS=(postgres pgbouncer redis minio nats opensearch jenkins harbor)
fi

if [[ ${#COMPONENTS[@]} -eq 0 ]]; then
  echo "ERROR: 请指定 --all 或 --component <name>"
  exit 1
fi

command -v helm >/dev/null 2>&1 || { echo "ERROR: 需安装 Helm"; exit 1; }
command -v kubectl >/dev/null 2>&1 || { echo "ERROR: 需安装 kubectl"; exit 1; }

# 添加 Helm 仓库
echo "[init] 添加 Helm 仓库..."
helm repo add bitnami https://charts.bitnami.com/bitnami 2>/dev/null || true
helm repo add minio https://charts.min.io/ 2>/dev/null || true
helm repo add nats https://nats-io.github.io/k8s/helm/charts/ 2>/dev/null || true
helm repo add opensearch https://opensearch-project.github.io/helm-charts/ 2>/dev/null || true
helm repo add jenkins https://charts.jenkins.io 2>/dev/null || true
helm repo add harbor https://helm.goharbor.io 2>/dev/null || true
helm repo update

# 创建 namespace
kubectl get ns "$NAMESPACE" >/dev/null 2>&1 || kubectl create ns "$NAMESPACE"

# Chart 名映射
declare -A CHART_MAP=(
  [postgres]="bitnami/postgresql"
  [pgbouncer]="bitnami/pgbouncer"
  [redis]="bitnami/redis"
  [minio]="minio/minio"
  [nats]="nats/nats"
  [opensearch]="opensearch/opensearch"
  [jenkins]="jenkins/jenkins"
  [harbor]="harbor/harbor"
)

declare -A RELEASE_MAP=(
  [postgres]="vortexops-pg"
  [pgbouncer]="pgbouncer"
  [redis]="vortexops-redis"
  [minio]="minio"
  [nats]="nats"
  [opensearch]="opensearch"
  [jenkins]="jenkins"
  [harbor]="harbor"
)

declare -A VALUES_MAP=(
  [postgres]="postgres-replication.yaml"
  [pgbouncer]="pgbouncer.yaml"
  [redis]="redis-sentinel.yaml"
  [minio]="minio-distributed.yaml"
  [nats]="nats-jetstream.yaml"
  [opensearch]="opensearch-3node.yaml"
  [jenkins]="jenkins-k8s-agent.yaml"
  [harbor]="harbor.yaml"
)

for COMP in "${COMPONENTS[@]}"; do
  CHART="${CHART_MAP[$COMP]:-}"
  RELEASE="${RELEASE_MAP[$COMP]:-}"
  VALUES="${VALUES_MAP[$COMP]:-}"

  if [[ -z "$CHART" ]]; then
    echo "ERROR: 未知组件 $COMP"
    exit 1
  fi

  VALUES_PATH="$DEPS_DIR/$VALUES"
  if [[ ! -f "$VALUES_PATH" ]]; then
    echo "ERROR: values 文件不存在: $VALUES_PATH"
    exit 1
  fi

  echo ""
  echo "============================================"
  echo " 安装 $COMP"
  echo "============================================"
  echo " Chart:   $CHART"
  echo " Release: $RELEASE"
  echo " Values:  $VALUES"
  echo "--------------------------------------------"

  helm upgrade --install "$RELEASE" "$CHART" \
    -n "$NAMESPACE" \
    -f "$VALUES_PATH"

  echo "[install] 等待 $COMP 就绪..."
  case "$COMP" in
    postgres)
      kubectl -n "$NAMESPACE" rollout status statefulset/"$RELEASE"-postgresql --timeout=300s
      ;;
    redis)
      kubectl -n "$NAMESPACE" rollout status statefulset/"$RELEASE"-master --timeout=300s
      ;;
    minio)
      kubectl -n "$NAMESPACE" rollout status statefulset/"RELEASE" --timeout=300s || true
      ;;
  esac
done

echo ""
echo "============================================"
echo " 全部安装完成"
echo "============================================"
echo ""
echo " 连接串（填入 values-k8s-mid.yaml）:"
echo ""
echo " postgresql:"
echo "   host: vortexops-pg-postgresql.$NAMESPACE.svc"
echo "   port: 5432"
echo ""
echo " redis:"
echo "   host: vortexops-redis-master.$NAMESPACE.svc"
echo "   port: 6379"
echo ""
echo " objectStorage:"
echo "   endpoint: http://minio.$NAMESPACE.svc:9000"
echo ""
echo " 请先创建 Secret（pg-creds / redis-creds / s3-creds / jwt-secret / kms-key）"
echo "============================================"
