#
# VortexOps 后端镜像（多阶段构建）。
# 目标：可重复、最小化、非 root。
# 最终镜像基于 distroless static（无 shell，体积小，攻击面小），
# 适合 Go 纯静态二进制（pgx 为纯 Go 驱动，无 CGO 依赖）。
#
# 构建示例：
#   docker build -t vortexops:v0.2.0 \
#     --build-arg VERSION=v0.2.0 \
#     --build-arg COMMIT=$(git rev-parse --short HEAD) \
#     --build-arg BUILD_DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
#     -f Dockerfile .
#
# 多架构构建（需 buildx）：
#   docker buildx build --platform linux/amd64,linux/arm64 -t vortexops:v0.2.0 --push .

# ---------- Stage 1: builder ----------
FROM golang:1.26.4-alpine AS builder

# git/ca-certificates 供 go module 下载与校验。
RUN apk add --no-cache git ca-certificates

# 国内网络无法访问 proxy.golang.org，使用 goproxy.cn 加速模块下载。
ENV GOPROXY=https://goproxy.cn,direct
ENV GOSUMDB=off

WORKDIR /src

# 先拷贝依赖清单，利用层缓存加速依赖下载。
COPY backend/go.mod backend/go.sum ./
RUN go mod download

# 拷贝源码。
COPY backend/ ./

# 构建参数：版本号与构建时间，注入二进制。
ARG VERSION=dev
ARG BUILD_DATE=unknown
ARG COMMIT=unknown
ARG TARGETOS=linux
ARG TARGETARCH=amd64

# 静态构建：CGO_DISABLED=1，去除调试信息（-s -w），启用 trimpath 去除本机路径。
# -ldflags 注入版本信息到 internal/version 包变量。
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build \
      -trimpath \
      -ldflags "-s -w \
        -X github.com/vortexops/vortexops/internal/version.Version=${VERSION} \
        -X github.com/vortexops/vortexops/internal/version.BuildDate=${BUILD_DATE} \
        -X github.com/vortexops/vortexops/internal/version.Commit=${COMMIT}" \
      -o /out/vortexops \
      ./cmd/vortexops

# ---------- Stage 2: runtime ----------
FROM gcr.io/distroless/static-debian12:nonroot

ARG VERSION=dev
ARG BUILD_DATE=unknown
ARG COMMIT=unknown
LABEL org.opencontainers.image.title="vortexops" \
      org.opencontainers.image.description="VortexOps backend API server" \
      org.opencontainers.image.source="https://github.com/vortexops/vortexops" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.created="${BUILD_DATE}"

# distroless static 已含 CA 证书；nonroot 用户 uid=65532 已内置。
WORKDIR /app

COPY --from=builder /out/vortexops /app/vortexops

# 监听端口。与默认 config（SERVER_ADDR=:8080）对齐。
EXPOSE 8080

# distroless 无 shell，直接以数组形式指定 ENTRYPOINT。
ENTRYPOINT ["/app/vortexops"]

# 默认空 CMD；实际配置由环境变量注入（见 deployment.md）。
CMD []
