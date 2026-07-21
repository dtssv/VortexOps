#!/usr/bin/env bash
# =============================================================================
# 构建 VortexOps 镜像 / 二进制并推送
# 用法:
#   ./scripts/build-images.sh --target k8s --registry registry.vortexops.io --version 1.0.0
#   ./scripts/build-images.sh --target bare --version 1.0.0
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
LAYER_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
DEPLOY_DIR="$(cd "$LAYER_DIR/.." && pwd)"
REPO_ROOT="$(cd "$DEPLOY_DIR/.." && pwd)"

TARGET="k8s"
REGISTRY="registry.vortexops.io"
VERSION="latest"
PUSH=false
MULTI_ARCH=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --target)      TARGET="$2"; shift 2 ;;
    --registry)    REGISTRY="$2"; shift 2 ;;
    --version)     VERSION="$2"; shift 2 ;;
    --push)        PUSH=true; shift ;;
    --multi-arch)  MULTI_ARCH=true; shift ;;
    -h|--help)
      cat <<EOF
用法: $0 [OPTIONS]
选项:
  --target <k8s|bare>     构建目标：k8s 镜像 / bare 二进制 (默认: k8s)
  --registry <url>        镜像仓库 (默认: registry.vortexops.io)
  --version <ver>         版本号 (默认: latest)
  --push                  构建后推送到镜像仓库
  --multi-arch            多架构 (amd64 + arm64)，需 buildx
EOF
      exit 0 ;;
    *) echo "未知参数: $1"; exit 1 ;;
  esac
done

cd "$REPO_ROOT"

BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"

echo "============================================"
echo " VortexOps Build"
echo "============================================"
echo " Target:     $TARGET"
echo " Version:    $VERSION"
echo " Commit:     $COMMIT"
echo " Build Date: $BUILD_DATE"
[[ "$TARGET" == "k8s" ]] && echo " Registry:   $REGISTRY"
echo "--------------------------------------------"

if [[ "$TARGET" == "bare" ]]; then
  # ---------- 二进制构建 ----------
  mkdir -p dist
  cd backend

  for CMD in vortexops syncer ws-gateway webhook pipeline-worker; do
    echo "[bare] 构建 $CMD..."
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
      -trimpath \
      -ldflags "-s -w \
        -X github.com/vortexops/vortexops/internal/version.Version=$VERSION \
        -X github.com/vortexops/vortexops/internal/version.BuildDate=$BUILD_DATE \
        -X github.com/vortexops/vortexops/internal/version.Commit=$COMMIT" \
      -o "../dist/vortexops-${CMD}-${VERSION}-linux-amd64" \
      ./cmd/$CMD
  done

  echo "[bare] 构建前端..."
  cd ../frontend
  npm ci --no-audit --no-fund
  npm run build
  tar -czf "../dist/frontend-${VERSION}.tar.gz" -C dist .

  echo ""
  echo "============================================"
  echo " 构建产物（dist/）"
  echo "============================================"
  ls -lh "$REPO_ROOT/dist/" | tail -n +2

elif [[ "$TARGET" == "k8s" ]]; then
  # ---------- 镜像构建 ----------
  IMAGES=(
    "apiserver:Dockerfile"
    "syncer:Dockerfile.syncer"
    "ws-gateway:Dockerfile.ws-gateway"
    "webhook:Dockerfile.webhook"
    "pipeline-worker:Dockerfile.pipeline-worker"
    "frontend:frontend/Dockerfile"
  )

  for entry in "${IMAGES[@]}"; do
    NAME="${entry%%:*}"
    DOCKERFILE="${entry##*:}"
    IMAGE="$REGISTRY/vortexops/$NAME:$VERSION"
    echo "[k8s] 构建 $IMAGE (Dockerfile=$DOCKERFILE)..."

    BUILD_ARGS=(
      --build-arg "VERSION=$VERSION"
      --build-arg "COMMIT=$COMMIT"
      --build-arg "BUILD_DATE=$BUILD_DATE"
      -f "$DOCKERFILE"
      -t "$IMAGE"
    )

    if [[ "$MULTI_ARCH" == "true" ]]; then
      docker buildx build --platform linux/amd64,linux/arm64 \
        "${BUILD_ARGS[@]}" \
        $([[ "$PUSH" == "true" ]] && echo --push) .
    else
      docker build "${BUILD_ARGS[@]}" .
      if [[ "$PUSH" == "true" ]]; then
        docker push "$IMAGE"
      fi
    fi
  done

  echo ""
  echo "============================================"
  echo " 镜像列表"
  echo "============================================"
  docker images "$REGISTRY/vortexops" --format "table {{.Repository}}\t{{.Tag}}\t{{.Size}}" | head -n 10
fi

echo ""
echo " 构建完成。"
