#!/usr/bin/env sh
# 在 kind 集群安装 Tekton Pipeline CRD 并创建 vo-builds 命名空间。
# 幂等：重复执行不会报错。
set -e

export KUBECONFIG="${KUBECONFIG:-/etc/vortexops/admin.conf}"

echo "==> 等待 k8s API 就绪..."
for i in $(seq 1 30); do
  if kubectl get nodes >/dev/null 2>&1; then
    break
  fi
  echo "    k8s 未就绪，重试 ($i/30)..."
  sleep 5
done

echo "==> 安装 Tekton Pipeline CRD..."
if ! kubectl get crd pipelineruns.tekton.dev >/dev/null 2>&1; then
  kubectl apply --filename https://storage.googleapis.com/tekton-releases/pipeline/latest/release.yaml
else
  echo "    Tekton Pipeline CRD 已存在，跳过安装。"
fi

echo "==> 等待 Tekton 控制器就绪..."
kubectl rollout status deployment tekton-pipelines-controller -n tekton-pipelines --timeout=300s || true
kubectl rollout status deployment tekton-pipelines-webhook -n tekton-pipelines --timeout=300s || true

echo "==> 创建 vo-builds 命名空间..."
kubectl create namespace vo-builds --dry-run=client -o yaml | kubectl apply -f -

echo "==> Tekton 安装完成。"
kubectl get pods -n tekton-pipelines
