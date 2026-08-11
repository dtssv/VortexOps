# ---------- Stage 1: build ----------
# Pin to patch tag — Aliyun mirror blocks rolling tags (node:20-alpine) with 403.
FROM node:20.18.1-alpine AS builder
WORKDIR /app

# 国内网络访问 npm 官方源较慢，切换淘宝镜像加速依赖安装。
RUN npm config set registry https://registry.npmmirror.com

COPY package.json package-lock.json* ./
RUN npm install --no-audit --no-fund

COPY tsconfig.json tsconfig.node.json vite.config.ts index.html ./
COPY src ./src
COPY public ./public

ARG VITE_API_BASE=/api/v1
ARG VITE_WS_BASE=/ws
ENV VITE_API_BASE=$VITE_API_BASE
ENV VITE_WS_BASE=$VITE_WS_BASE

RUN npm run build

# ---------- Stage 2: runtime ----------
FROM nginx:1.27-alpine AS runtime

COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=builder /app/dist /usr/share/nginx/html

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
