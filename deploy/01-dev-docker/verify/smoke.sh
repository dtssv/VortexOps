#!/usr/bin/env bash
# =============================================================================
# 端到端冒烟测试：登录 → 建空间 → 建应用 → 建分组 → 发布 → 验证 Pod
# 用法: ./verify/smoke.sh
# 依赖: curl / jq；前置：migrate.sh + seed.sh 已执行
# =============================================================================
set -euo pipefail

API="http://localhost:8080/api/v1"
USER="admin"
PASS="admin123"

need() { command -v "$1" >/dev/null 2>&1 || { echo "ERROR: 需要安装 $1"; exit 1; }; }
need curl
need jq

echo "============================================"
echo " VortexOps Smoke Test"
echo "============================================"

# 1. 登录
echo "[1/7] 登录获取 token..."
TOKEN=$(curl -sf -X POST "$API/auth/login" \
  -H "Content-Type: application/json" \
  -d "{\"username\":\"$USER\",\"password\":\"$PASS\"}" | jq -r .data.accessToken)
[[ -n "$TOKEN" && "$TOKEN" != "null" ]] || { echo "FAIL: 登录失败"; exit 1; }
echo "  token: ${TOKEN:0:32}..."

AUTH=(-H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json")

# 2. 创建工作空间
echo "[2/7] 创建工作空间 smoke-ws..."
WS_ID=$(curl -sf -X POST "$API/workspaces" "${AUTH[@]}" \
  -d '{"name":"smoke-ws","description":"smoke test"}' | jq -r .data.uuid)
[[ -n "$WS_ID" && "$WS_ID" != "null" ]] || { echo "FAIL: 创建工作空间失败"; exit 1; }
echo "  ws: $WS_ID"

# 3. 创建应用
echo "[3/7] 创建应用 smoke-app..."
APP_ID=$(curl -sf -X POST "$API/applications" "${AUTH[@]}" \
  -d "{\"name\":\"smoke-app\",\"workspace_uuid\":\"$WS_ID\"}" | jq -r .data.uuid)
echo "  app: $APP_ID"

# 4. 创建分组（Deployment，1 副本，nginx 测试镜像）
echo "[4/7] 创建分组 smoke-group..."
GROUP_ID=$(curl -sf -X POST "$API/groups" "${AUTH[@]}" \
  -d "{\"name\":\"smoke-group\",\"application_uuid\":\"$APP_ID\",\"image\":\"nginx:1.27-alpine\",\"replicas\":1,\"resources_cpu_m\":100,\"resources_memory_bytes\":134217728}" \
  | jq -r .data.uuid)
echo "  group: $GROUP_ID"

# 5. 触发整组发布
echo "[5/7] 触发整组发布..."
REL_ID=$(curl -sf -X POST "$API/releases" "${AUTH[@]}" \
  -d "{\"group_uuid\":\"$GROUP_ID\",\"type\":\"rolling\"}" | jq -r .data.uuid)
echo "  release: $REL_ID"

# 6. 等待发布完成
echo "[6/7] 等待发布完成（最长 90s）..."
for i in $(seq 1 18); do
  STATUS=$(curl -sf "$API/releases/$REL_ID" "${AUTH[@]}" | jq -r .data.status)
  if [[ "$STATUS" == "succeeded" ]]; then
    echo "  发布完成 (status=$STATUS)"
    break
  fi
  if [[ "$STATUS" == "failed" || "$STATUS" == "rolled_back" ]]; then
    echo "FAIL: 发布状态=$STATUS"
    exit 1
  fi
  echo "  ...当前状态=$STATUS，等待 5s"
  sleep 5
done
[[ "$STATUS" == "succeeded" ]] || { echo "FAIL: 发布超时未完成"; exit 1; }

# 7. 查询 Pod 列表
echo "[7/7] 查询 Pod 列表..."
PODS=$(curl -sf "$API/groups/$GROUP_ID/pods" "${AUTH[@]}" | jq -r '.data.items | length')
READY=$(curl -sf "$API/groups/$GROUP_ID/pods" "${AUTH[@]}" | jq -r '[.data.items[] | select(.ready==true)] | length')
echo "  Pod 总数: $PODS，就绪: $READY"
[[ "$PODS" -ge 1 && "$READY" -ge 1 ]] || { echo "FAIL: 无就绪 Pod"; exit 1; }

# 清理（可选，注释保留供排查）
echo "--------------------------------------------"
echo " 清理测试数据..."
curl -sf -X DELETE "$API/groups/$GROUP_ID" "${AUTH[@]}" >/dev/null || true
curl -sf -X DELETE "$API/applications/$APP_ID" "${AUTH[@]}" >/dev/null || true
curl -sf -X DELETE "$API/workspaces/$WS_ID" "${AUTH[@]}" >/dev/null || true
echo "============================================"
echo " PASS — 冒烟测试通过"
echo "============================================"
