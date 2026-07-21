#!/usr/bin/env bash
# =============================================================================
# 模型权重下载与分发（XL 大规模推理服务）
# 用法:
#   ./scripts/model-weights.sh --download --model Qwen2.5-72B-Instruct --version v1.0
#   ./scripts/model-weights.sh --list
#   ./scripts/model-weights.sh --gc --keep 3
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
LAYER_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
MANIFESTS_DIR="$LAYER_DIR/manifests/gpu"

NAMESPACE="vortexops"
INFRA_NS="vortexops-infra"
ACTION="list"
MODEL_NAME="Qwen2.5-72B-Instruct"
MODEL_VERSION="v1.0"
S3_BUCKET="vortexops-model-weights"
S3_ENDPOINT="https://s3.us-east-1.vortexops.io"
KEEP_VERSIONS=3

while [[ $# -gt 0 ]]; do
  case "$1" in
    --download)      ACTION="download"; shift ;;
    --list)          ACTION="list"; shift ;;
    --gc)            ACTION="gc"; shift ;;
    --model)         MODEL_NAME="$2"; shift 2 ;;
    --version)       MODEL_VERSION="$2"; shift 2 ;;
    --keep)          KEEP_VERSIONS="$2"; shift 2 ;;
    --namespace|-n)  NAMESPACE="$2"; shift 2 ;;
    --bucket)        S3_BUCKET="$2"; shift 2 ;;
    --endpoint)      S3_ENDPOINT="$2"; shift 2 ;;
    -h|--help)
      cat <<EOF
用法: $0 [OPTIONS]
操作:
  --download    下载权重到共享 PVC
  --list        列出已下载权重
  --gc          清理旧版本（保留最近 N 个）
选项:
  --model <name>       模型名 (默认: Qwen2.5-72B-Instruct)
  --version <v>        版本 (默认: v1.0)
  --keep <n>           保留版本数 (默认: 3)
  --namespace, -n <ns> 命名空间
  --bucket <name>      S3 桶名
  --endpoint <url>     S3 endpoint
EOF
      exit 0 ;;
    *) echo "未知参数: $1"; exit 1 ;;
  esac
done

command -v kubectl >/dev/null 2>&1 || { echo "ERROR: 需安装 kubectl"; exit 1; }

case "$ACTION" in
  download)
    echo "============================================"
    echo " 下载模型权重"
    "============================================"
    echo " Model:    $MODEL_NAME"
    echo " Version:  $MODEL_VERSION"
    echo " Bucket:   $S3_BUCKET"
    echo "--------------------------------------------"

    # 1. 确认 PVC 存在
    if ! kubectl -n "$NAMESPACE" get pvc model-weights-shared >/dev/null 2>&1; then
      echo "[download] 创建共享 PVC..."
      kubectl apply -f "$MANIFESTS_DIR/shared-weights-pvc.yaml" -n "$NAMESPACE"
    fi

    # 2. 触发下载 Job
    echo "[download] 触发下载 Job..."
    cat <<EOF | kubectl -n "$NAMESPACE" apply -f -
apiVersion: batch/v1
kind: Job
metadata:
  name: download-${MODEL_NAME}-${MODEL_VERSION}-$(date +%s)
  namespace: $NAMESPACE
spec:
  ttlSecondsAfterFinished: 3600
  template:
    spec:
      restartPolicy: OnFailure
      containers:
        - name: downloader
          image: amazon/aws-cli:2.15.0
          command:
            - /bin/sh
            - -c
            - |
              set -e
              echo "[download] 开始下载: $MODEL_NAME/$MODEL_VERSION"
              aws s3 sync s3://$S3_BUCKET/$MODEL_NAME/$MODEL_VERSION /models/$MODEL_NAME/$MODEL_VERSION \\
                --endpoint-url $S3_ENDPOINT
              echo "[download] 完成"
              ls -lh /models/$MODEL_NAME/$MODEL_VERSION/
          env:
            - name: AWS_ACCESS_KEY_ID
              valueFrom:
                secretKeyRef:
                  name: s3-creds
                  key: accessKey
            - name: AWS_SECRET_ACCESS_KEY
              valueFrom:
                secretKeyRef:
                  name: s3-creds
                  key: secretKey
            - name: AWS_DEFAULT_REGION
              value: us-east-1
          volumeMounts:
            - name: model-weights
              mountPath: /models
      volumes:
        - name: model-weights
          persistentVolumeClaim:
            claimName: model-weights-shared
EOF

    # 3. 等待 Job 完成
    JOB_NAME=$(kubectl -n "$NAMESPACE" get job -l job-name --no-headers 2>/dev/null | tail -1 | awk '{print $1}')
    if [[ -n "$JOB_NAME" ]]; then
      echo "[download] 等待 Job $JOB_NAME 完成（大模型可能耗时 10-30 分钟）..."
      kubectl -n "$NAMESPACE" wait job/"$JOB_NAME" --for=condition=Complete --timeout=3600s || \
        echo "  ⚠️  Job 未在超时时间内完成，请检查: kubectl -n $NAMESPACE logs job/$JOB_NAME"
    fi
    ;;

  list)
    echo "[list] 已下载的模型权重（在共享 PVC 中）:"
    # 通过一个临时 Pod 列出 PVC 内容
    kubectl -n "$NAMESPACE" run "list-weights-$(date +%s)" --rm -i \
      --image=busybox:1.36 \
      --restart=Never \
      --overrides='{
        "spec": {
          "containers": [{
            "name": "list-weights",
            "image": "busybox:1.36",
            "command": ["sh", "-c", "find /models -maxdepth 3 -type d | sort"],
            "volumeMounts": [{"name": "model-weights", "mountPath": "/models"}]
          }],
          "volumes": [{
            "name": "model-weights",
            "persistentVolumeClaim": {"claimName": "model-weights-shared"}
          }]
        }
      }'
    ;;

  gc)
    echo "[gc] 清理旧版本，保留最近 $KEEP_VERSIONS 个版本..."
    echo "  （此操作需在 PVC 挂载的 Pod 中执行，且需人工确认）"
    echo ""
    echo "  手动操作步骤:"
    echo "    1. kubectl -n $NAMESPACE exec -it deploy/vortexops-apiserver -- ls /models/  (如已挂载)"
    echo "    2. 或起一个临时 Pod 挂载 PVC 后手动删除旧版本目录"
    echo "    3. 删除前确认无推理服务引用该版本"
    echo ""
    echo "  警告: 删除权重前请确认:"
    echo "    - 该版本未被任何推理 Deployment 引用"
    echo "    - 已在新版本上完成灰度验证"
    echo "    - 对象存储中仍有备份"
    ;;
esac
