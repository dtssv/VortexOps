#!/usr/bin/env bash
# =============================================================================
# 新 Region 引导脚本（XL 多 Region 部署）
# 用法:
#   ./scripts/bootstrap-region.sh --region us-west-2 --role standby \
#       --values manifests/helm-values/values-xl-region-b-standby.yaml
#   ./scripts/bootstrap-region.sh --region ap-southeast-1 --role active \
#       --values manifests/helm-values/values-xl-region-a.yaml
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
LAYER_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
MANIFESTS_DIR="$LAYER_DIR/manifests"

REGION=""
ROLE="standby"        # active | standby
VALUES_FILE=""
NAMESPACE="vortexops"
INFRA_NS="vortexops-infra"
RELEASE_NAME="vortexops"
SKIP_DEPS=false
DRY_RUN=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --region)        REGION="$2"; shift 2 ;;
    --role)          ROLE="$2"; shift 2 ;;
    --values|-f)     VALUES_FILE="$2"; shift 2 ;;
    --namespace|-n)  NAMESPACE="$2"; shift 2 ;;
    --release)       RELEASE_NAME="$2"; shift 2 ;;
    --skip-deps)     SKIP_DEPS=true; shift ;;
    --dry-run)       DRY_RUN=true; shift ;;
    -h|--help)
      cat <<EOF
用法: $0 --region <r> --role <active|standby> [OPTIONS]
选项:
  --region <r>          Region 名（如 us-west-2）
  --role <r>            active（主） 或 standby（备）
  --values, -f <file>   Helm values 文件
  --namespace, -n <ns>  VortexOps 命名空间 (默认: vortexops)
  --release <name>      Helm release 名 (默认: vortexops)
  --skip-deps           跳过依赖组件安装（假定已就绪）
  --dry-run             仅渲染，不执行
EOF
      exit 0 ;;
    *) echo "未知参数: $1"; exit 1 ;;
  esac
done

[[ -n "$REGION" ]] || { echo "ERROR: --region 必填"; exit 1; }
[[ "$ROLE" == "active" || "$ROLE" == "standby" ]] || { echo "ERROR: --role 必须为 active 或 standby"; exit 1; }

if [[ "$ROLE" == "active" && -z "$VALUES_FILE" ]]; then
  VALUES_FILE="$MANIFESTS_DIR/helm-values/values-xl-region-a.yaml"
elif [[ "$ROLE" == "standby" && -z "$VALUES_FILE" ]]; then
  VALUES_FILE="$MANIFESTS_DIR/helm-values/values-xl-region-b-standby.yaml"
fi

[[ -f "$VALUES_FILE" ]] || { echo "ERROR: values 文件不存在: $VALUES_FILE"; exit 1; }

command -v kubectl >/dev/null 2>&1 || { echo "ERROR: 需安装 kubectl"; exit 1; }
command -v helm    >/dev/null 2>&1 || { echo "ERROR: 需安装 helm";    exit 1; }

echo "============================================"
echo " VortexOps XL Region 引导"
echo "============================================"
echo " Region:    $REGION"
echo " Role:      $ROLE"
echo " Values:    $VALUES_FILE"
echo " Namespace: $NAMESPACE"
echo " Skip Deps: $SKIP_DEPS"
echo " Dry Run:   $DRY_RUN"
echo "--------------------------------------------"

# 当前 kubeconfig context（用于提示）
CURRENT_CTX=$(kubectl config current-context 2>/dev/null || echo "unknown")
echo " Kube Context: $CURRENT_CTX"
echo ""
read -r -p "确认在 region [$REGION] 的集群上执行? (yes/no): " CONFIRM
[[ "$CONFIRM" == "yes" ]] || { echo "已取消"; exit 0; }

# 1. 创建 namespace
echo ""
echo "[1/6] 创建 namespace..."
kubectl get ns "$NAMESPACE"        >/dev/null 2>&1 || kubectl create ns "$NAMESPACE"
kubectl get ns "$INFRA_NS"         >/dev/null 2>&1 || kubectl create ns "$INFRA_NS"

# 命名空间打标签
kubectl label ns "$NAMESPACE" vortexops.io/region="$REGION" --overwrite
kubectl label ns "$NAMESPACE" vortexops.io/role="$ROLE" --overwrite
kubectl label ns "$INFRA_NS"  vortexops.io/region="$REGION" --overwrite

# 2. 安装依赖组件
if [[ "$SKIP_DEPS" == "false" ]]; then
  echo ""
  echo "[2/6] 安装依赖组件..."
  DEPS_CMD=("$SCRIPT_DIR/install-deps-xl.sh" --all --region "$REGION")
  if [[ "$ROLE" == "standby" ]]; then
    echo "  → standby 角色：依赖组件作为只读副本部署"
  fi
  if [[ "$DRY_RUN" == "true" ]]; then
    echo "  [dry-run] 将执行: ${DEPS_CMD[*]}"
  else
    "${DEPS_CMD[@]}"
  fi
else
  echo ""
  echo "[2/6] 跳过依赖组件安装"
fi

# 3. 创建 Secret（如未存在）
echo ""
echo "[3/6] 检查 Secret..."
SECRETS=(
  "vortexops-db-creds"
  "redis-creds"
  "s3-creds"
  "jwt-secret"
  "kms-key"
  "pg-replication-cert"
)
for S in "${SECRETS[@]}"; do
  if ! kubectl -n "$NAMESPACE" get secret "$S" >/dev/null 2>&1; then
    echo "  ⚠️  Secret $S 不存在，请手动创建"
  else
    echo "  ✓ Secret $S 已存在"
  fi
done

# 4. 部署 VortexOps（Helm）
echo ""
echo "[4/6] 部署 VortexOps..."
HELM_ARGS=(upgrade --install "$RELEASE_NAME" "$LAYER_DIR/../helm"
  --namespace "$NAMESPACE" --create-namespace
  -f "$VALUES_FILE"
  --set "global.region=$REGION"
  --set "global.role=$ROLE")

[[ "$DRY_RUN" == "true" ]] && HELM_ARGS+=(--dry-run --debug)

echo "  helm ${HELM_ARGS[*]}"
if [[ "$DRY_RUN" == "false" ]]; then
  helm "${HELM_ARGS[@]}"

  echo ""
  echo "  等待 apiserver rollout..."
  kubectl -n "$NAMESPACE" rollout status deployment/"$RELEASE_NAME"-apiserver --timeout=600s
fi

# 5. standby 角色：配置跨 Region 复制
if [[ "$ROLE" == "standby" ]]; then
  echo ""
  echo "[5/6] 配置跨 Region 复制（standby）..."
  if [[ "$DRY_RUN" == "false" ]]; then
    # PostgreSQL standby
    kubectl apply -f "$MANIFESTS_DIR/dr/postgres-cross-region.yaml" -n "$INFRA_NS"
    # Redis standby
    kubectl apply -f "$MANIFESTS_DIR/dr/redis-cross-region.yaml" -n "$INFRA_NS"
    echo "  ✓ 跨 Region 复制已配置（PG 物理 standby + Redis replicaof）"
    echo "  ⚠️  MinIO 站点复制需在控制台手动配置（参见 README.md DR 章节）"
  else
    echo "  [dry-run] 将应用 postgres-cross-region.yaml + redis-cross-region.yaml"
  fi
else
  echo ""
  echo "[5/6] active 角色：跳过跨 Region 复制配置"
  echo "  提示：请在 standby region 执行此脚本以建立复制关系"
fi

# 6. 验证
echo ""
echo "[6/6] 验证部署..."
if [[ "$DRY_RUN" == "false" ]]; then
  echo ""
  echo "  Pod 状态:"
  kubectl -n "$NAMESPACE" get pods -l "app.kubernetes.io/instance=$RELEASE_NAME"

  echo ""
  echo "  Service:"
  kubectl -n "$NAMESPACE" get svc -l "app.kubernetes.io/instance=$RELEASE_NAME"

  echo ""
  echo "  健康检查:"
  kubectl -n "$NAMESPACE" exec deploy/"$RELEASE_NAME"-apiserver -- \
    wget -qO- http://localhost:8080/api/v1/healthz || true
fi

echo ""
echo "============================================"
echo " Region [$REGION] ($ROLE) 引导完成"
echo "============================================"
if [[ "$ROLE" == "active" ]]; then
  echo ""
  echo " 后续步骤:"
  echo "  1. 初始化 Citus 分片:"
  echo "     $SCRIPT_DIR/init-citus-sharding.sh --coordinator vortexops-citus-coordinator"
  echo "  2. 配置 syncer 分片:"
  echo "     $SCRIPT_DIR/syncer-shard.sh --init --shards 16"
  echo "  3. 批量接入业务集群（通过 API 或 UI）"
  echo "  4. 在 standby region 执行:"
  echo "     $SCRIPT_DIR/bootstrap-region.sh --region <standby-region> --role standby"
  echo "  5. 配置 DNS 故障转移（参见 manifests/network/dns-failover.yaml）"
fi
