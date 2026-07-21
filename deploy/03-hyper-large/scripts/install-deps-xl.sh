#!/usr/bin/env bash
# =============================================================================
# XL 集群依赖组件安装（Citus / Redis Cluster / Kafka / OpenSearch / Thanos）
# 用法:
#   ./scripts/install-deps-xl.sh --all --region us-east-1
#   ./scripts/install-deps-xl.sh --component citus --region us-east-1
#   ./scripts/install-deps-xl.sh --component redis-cluster
#   ./scripts/install-deps-xl.sh --component kafka
#   ./scripts/install-deps-xl.sh --component opensearch-xl
#   ./scripts/install-deps-xl.sh --component monitoring
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
LAYER_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
MANIFESTS_DIR="$LAYER_DIR/manifests"
NAMESPACE="vortexops-infra"
REGION="us-east-1"

COMPONENTS=()
INSTALL_ALL=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --all)            INSTALL_ALL=true; shift ;;
    --component)      COMPONENTS+=("$2"); shift 2 ;;
    --namespace|-n)   NAMESPACE="$2"; shift 2 ;;
    --region)         REGION="$2"; shift 2 ;;
    -h|--help)
      cat <<EOF
用法: $0 [--all | --component <name>] [--namespace <ns>] [--region <r>]
组件:
  citus            分布式 PostgreSQL（Citus 8 worker + 2 coordinator HA）
  redis-cluster    Redis Cluster（12 节点 6 主 6 从）
  kafka            Strimzi Kafka（9 broker KRaft）
  opensearch-xl    OpenSearch 集群（coord + data 分离）
  monitoring       Prometheus 联邦 + Thanos + Grafana
EOF
      exit 0 ;;
    *) echo "未知参数: $1"; exit 1 ;;
  esac
done

if [[ "$INSTALL_ALL" == "true" ]]; then
  COMPONENTS=(citus redis-cluster kafka opensearch-xl monitoring)
fi

if [[ ${#COMPONENTS[@]} -eq 0 ]]; then
  echo "ERROR: 请指定 --all 或 --component <name>"
  exit 1
fi

command -v helm   >/dev/null 2>&1 || { echo "ERROR: 需安装 Helm >= 3.12";   exit 1; }
command -v kubectl>/dev/null 2>&1 || { echo "ERROR: 需安装 kubectl >= 1.28"; exit 1; }

# 添加 Helm 仓库
echo "[init] 添加 Helm 仓库..."
helm repo add citus        https://charts.citusdata.com            2>/dev/null || true
helm repo add bitnami      https://charts.bitnami.com/bitnami      2>/dev/null || true
helm repo add strimzi      https://strimzi.io/charts/              2>/dev/null || true
helm repo add opensearch   https://opensearch-project.github.io/helm-charts/ 2>/dev/null || true
helm repo add thanos       https://charts.bitnami.com/bitnami      2>/dev/null || true
helm repo add prometheus   https://prometheus-community.github.io/helm-charts 2>/dev/null || true
helm repo update

# 创建 namespace
kubectl get ns "$NAMESPACE" >/dev/null 2>&1 || kubectl create ns "$NAMESPACE"

# 命名空间打 region 标签（NetworkPolicy 依赖）
kubectl label ns "$NAMESPACE" vortexops.io/region="$REGION" --overwrite
kubectl label ns "$NAMESPACE" vortexops.io/replication-allowed=true --overwrite

# Chart 名映射
declare -A CHART_MAP=(
  [citus]="citus/citus"
  [redis-cluster]="bitnami/redis-cluster"
  [kafka]="strimzi/strimzi-kafka-operator"
  [opensearch-xl]="opensearch/opensearch"
  [monitoring]="prometheus/kube-prometheus-stack"
)

declare -A RELEASE_MAP=(
  [citus]="vortexops-citus"
  [redis-cluster]="vortexops-redis-cluster"
  [kafka]="strimzi-operator"
  [opensearch-xl]="vortexops-opensearch"
  [monitoring]="vortexops-monitoring"
)

declare -A VALUES_MAP=(
  [citus]="citus-cluster.yaml"
  [redis-cluster]="redis-cluster.yaml"
  [kafka]=""                                     # Kafka Operator + CR
  [opensearch-xl]="opensearch-cluster.yaml"
  [monitoring]=""                                # 多个 manifest 组合
)

for COMP in "${COMPONENTS[@]}"; do
  CHART="${CHART_MAP[$COMP]:-}"
  RELEASE="${RELEASE_MAP[$COMP]:-}"
  VALUES="${VALUES_MAP[$COMP]:-}"

  if [[ -z "$CHART" ]]; then
    echo "ERROR: 未知组件 $COMP"
    exit 1
  fi

  echo ""
  echo "============================================"
  echo " 安装 $COMP  (region=$REGION)"
  echo "============================================"
  echo " Chart:   $CHART"
  echo " Release: $RELEASE"
  [[ -n "$VALUES" ]] && echo " Values:  $VALUES"
  echo "--------------------------------------------"

  if [[ "$COMP" == "kafka" ]]; then
    # 1. 安装 Strimzi Operator
    helm upgrade --install "$RELEASE" "$CHART" -n "$NAMESPACE"
    # 2. 部署 Kafka 集群 CR
    kubectl apply -f "$MANIFESTS_DIR/kafka-cluster.yaml" -n "$NAMESPACE"
    echo "[install] 等待 Kafka 集群就绪（9 broker KRaft，预计 5-10 分钟）..."
    kubectl -n "$NAMESPACE" wait kafka/vortexops-kafka --for=condition=Ready --timeout=900s || true

  elif [[ "$COMP" == "monitoring" ]]; then
    # 1. kube-prometheus-stack（Prometheus Operator）
    helm upgrade --install "$RELEASE" "$CHART" -n "$NAMESPACE" \
      --set prometheus.prometheusSpec.retention=30d \
      --set prometheus.prometheusSpec.retentionSize=200GiB \
      --set prometheus.prometheusSpec.enableFeatures[0]=exemplar-storage
    # 2. Prometheus 联邦配置
    kubectl apply -f "$MANIFESTS_DIR/monitoring/prometheus-federation.yaml" -n "$NAMESPACE"
    # 3. Thanos（长期存储）
    kubectl apply -f "$MANIFESTS_DIR/monitoring/thanos.yaml" -n "$NAMESPACE"
    # 4. Grafana 数据源
    kubectl apply -f "$MANIFESTS_DIR/monitoring/grafana-datasources.yaml" -n "$NAMESPACE"

  else
    VALUES_PATH="$MANIFESTS_DIR/$VALUES"
    if [[ ! -f "$VALUES_PATH" ]]; then
      echo "ERROR: values 文件不存在: $VALUES_PATH"
      exit 1
    fi
    helm upgrade --install "$RELEASE" "$CHART" \
      -n "$NAMESPACE" \
      -f "$VALUES_PATH"
  fi

  echo "[install] 等待 $COMP 就绪..."
  case "$COMP" in
    citus)
      kubectl -n "$NAMESPACE" rollout status statefulset/"$RELEASE"-coordinator --timeout=600s || true
      ;;
    redis-cluster)
      kubectl -n "$NAMESPACE" rollout status statefulset/"$RELEASE" --timeout=600s || true
      ;;
    opensearch-xl)
      kubectl -n "$NAMESPACE" rollout status statefulset/"$RELEASE"-cluster-master --timeout=600s || true
      ;;
  esac
done

echo ""
echo "============================================"
echo " XL 依赖组件安装完成 (region=$REGION)"
echo "============================================"
echo ""
echo " 连接串（填入 values-xl-region-a.yaml）:"
echo ""
echo " citus:"
echo "   host: vortexops-citus-coordinator.$NAMESPACE.svc"
echo "   port: 5432"
echo ""
echo " redis-cluster:"
echo "   host: vortexops-redis-cluster.$NAMESPACE.svc"
echo "   port: 6379"
echo ""
echo " kafka:"
echo "   brokers: vortexops-kafka-bootstrap.$NAMESPACE.svc:9092"
echo ""
echo " opensearch:"
echo "   host: vortexops-opensearch.$NAMESPACE.svc"
echo "   port: 9200"
echo ""
echo " 请先创建 Secret:"
echo "   kubectl -n vortexops create secret generic vortexops-db-creds     --from-literal=password=..."
echo "   kubectl -n vortexops create secret generic redis-creds           --from-literal=password=..."
echo "   kubectl -n vortexops create secret generic s3-creds              --from-literal=accessKey=... --from-literal=secretKey=..."
echo "   kubectl -n vortexops create secret generic jwt-secret            --from-literal=key=..."
echo "   kubectl -n vortexops create secret generic kms-key               --from-literal=keyId=..."
echo "============================================"
