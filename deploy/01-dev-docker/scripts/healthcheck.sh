#!/usr/bin/env bash
# =============================================================================
# 全栈健康巡检
# 用法: ./scripts/healthcheck.sh
# =============================================================================
set -uo pipefail

BASE_API="http://localhost:8080"

check() {
  local name="$1"
  local cmd="$2"
  if eval "$cmd" >/dev/null 2>&1; then
    printf "  [ OK ]   %s\n" "$name"
    return 0
  else
    printf "  [FAIL]   %s\n" "$name"
    return 1
  fi
}

echo "============================================"
echo " VortexOps Dev Stack Health Check"
echo "============================================"

FAIL=0

check "PostgreSQL (5432)"      "docker exec -i $(docker ps -q -f name=postgres) pg_isready -U vortexops -d vortexops 2>/dev/null" || FAIL=1
check "Redis (6379)"           "docker exec -i $(docker ps -q -f name=redis) redis-cli ping 2>/dev/null | grep -q PONG" || FAIL=1
check "Kafka (9092)"           "docker exec -i $(docker ps -q -f name=kafka) /opt/kafka/bin/kafka-topics.sh --bootstrap-server localhost:9092 --list >/dev/null 2>&1" || FAIL=1
check "MinIO (9000)"           "curl -sf http://localhost:9000/minio/health/live >/dev/null" || FAIL=1
check "OpenSearch (9200)"      "curl -sf http://localhost:9200/_cluster/health >/dev/null" || FAIL=1
check "Jenkins (8082)"         "curl -sf http://localhost:8082/login >/dev/null" || FAIL=1
check "Registry (8083)"        "curl -sf http://localhost:8083/v2/ >/dev/null" || FAIL=1
check "k3s API (6443)"         "docker exec -i $(docker ps -q -f name=k8s) /bin/kubectl get nodes >/dev/null 2>&1" || FAIL=1
check "Prometheus (9090)"      "curl -sf http://localhost:9090/-/healthy >/dev/null" || FAIL=1
check "apiserver (8080)"       "curl -sf ${BASE_API}/api/v1/healthz >/dev/null" || FAIL=1
check "frontend (8088)"        "curl -sf http://localhost:8088 >/dev/null" || FAIL=1

echo "--------------------------------------------"
if [[ "$FAIL" -eq 0 ]]; then
  echo " 结果: 全部就绪"
  exit 0
else
  echo " 结果: 有组件未就绪，请查日志: ./scripts/logs.sh <service>"
  exit 1
fi
