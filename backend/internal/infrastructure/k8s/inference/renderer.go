// Package inference 把 InferenceService 渲染为 vLLM/TGI/Triton Deployment + Service + 可选 HPA。
package inference

import (
	"fmt"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/vortexops/vortexops/internal/domain/inference"
)

const (
	vortexManagedLabel     = "app.vortexops.io/managed"
	vortexInferenceSvcLabel = "app.vortexops.io/inference-service-id"
)

// RenderResult 渲染输出。
type RenderResult struct {
	Deployment      *appsv1.Deployment
	Service         *corev1.Service
	HPA             *autoscalingv2.HorizontalPodAutoscaler
	Ingress         *networkingv1.Ingress
	ExternalService *corev1.Service
}

// RenderInput 渲染输入。
type RenderInput struct {
	Service      *inference.InferenceService
	ModelVersion *inference.ModelVersion
	Adapters     []*inference.ModelAdapter
	Registry     *inference.ModelRegistry
}

// Render 渲染推理服务 K8s 资源。
func Render(in RenderInput) (*RenderResult, error) {
	if in.Service == nil {
		return nil, fmt.Errorf("inference service is required")
	}
	if in.ModelVersion == nil {
		return nil, fmt.Errorf("model version is required")
	}
	svc := in.Service
	workloadName := svc.WorkloadName
	if workloadName == "" {
		workloadName = svc.Name
	}
	serviceName := svc.ServiceName
	if serviceName == "" {
		serviceName = workloadName
	}

	labels := buildLabels(svc, workloadName)
	selector := &metav1.LabelSelector{MatchLabels: map[string]string{vortexInferenceSvcLabel: strconv.FormatInt(svc.ID, 10)}}
	port := frameworkPort(svc.Framework)
	container := buildContainer(svc, in.ModelVersion, in.Adapters, in.Registry, port)
	podSpec := corev1.PodSpec{
		Containers:    []corev1.Container{container},
		RestartPolicy: corev1.RestartPolicyAlways,
	}
	// 挂载 registry cache PVC（若配置）。
	if in.Registry != nil && in.Registry.CachePVCName != "" {
		cachePath := in.Registry.CachePath
		if cachePath == "" {
			cachePath = "/models"
		}
		mountPath := "/models"
		vol := corev1.Volume{
			Name: "model-cache",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: in.Registry.CachePVCName,
				},
			},
		}
		podSpec.Volumes = append(podSpec.Volumes, vol)
		if len(container.VolumeMounts) == 0 {
			container.VolumeMounts = []corev1.VolumeMount{
				{Name: "model-cache", MountPath: mountPath, SubPath: stripLeadingSlash(cachePath)},
			}
		}
		// 容器已构建，需重新赋值（指针字段已设置）。
		podSpec.Containers[0] = container
	}
	if svc.GPUType != "" {
		if podSpec.NodeSelector == nil {
			podSpec.NodeSelector = map[string]string{}
		}
		podSpec.NodeSelector["gpu_type"] = svc.GPUType
	}
	podSpec.Tolerations = []corev1.Toleration{
		{
			Key:      "nvidia.com/gpu",
			Operator: corev1.TolerationOpExists,
			Effect:   corev1.TaintEffectNoSchedule,
		},
	}
	if svc.GPUType != "" {
		podSpec.Tolerations = append(podSpec.Tolerations, corev1.Toleration{
			Key:      "gpu_type",
			Operator: corev1.TolerationOpEqual,
			Value:    svc.GPUType,
			Effect:   corev1.TaintEffectNoSchedule,
		})
	}

	replicas := int32(svc.Replicas)
	if replicas == 0 {
		replicas = 1
	}
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      workloadName,
			Namespace: svc.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: selector,
			Strategy: appsv1.DeploymentStrategy{Type: appsv1.RollingUpdateDeploymentStrategyType},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec:       podSpec,
			},
		},
	}

	result := &RenderResult{
		Deployment: deployment,
		Service: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      serviceName,
				Namespace: svc.Namespace,
				Labels:    labels,
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeClusterIP,
				Selector: map[string]string{vortexInferenceSvcLabel: strconv.FormatInt(svc.ID, 10)},
				Ports: []corev1.ServicePort{{
					Name:       "http",
					Port:       port,
					TargetPort: intstr.FromInt32(port),
					Protocol:   corev1.ProtocolTCP,
				}},
			},
		},
	}
	if svc.AutoscalingEnabled {
		result.HPA = buildHPA(svc, workloadName, labels)
	}
	if svc.AccessMode == inference.AccessExternal {
		result.Ingress, result.ExternalService = buildExternalAccess(svc, workloadName, serviceName, port, labels)
	}
	return result, nil
}

// buildExternalAccess 在 external 模式下构建 Ingress + LoadBalancer Service。
// 若 svc.Metadata.host 已配置则用 host 路由，否则仅暴露 LoadBalancer。
func buildExternalAccess(svc *inference.InferenceService, workloadName, internalServiceName string, port int32, labels map[string]string) (*networkingv1.Ingress, *corev1.Service) {
	extServiceName := workloadName + "-ext"
	extSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      extServiceName,
			Namespace: svc.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeLoadBalancer,
			Selector: map[string]string{vortexInferenceSvcLabel: strconv.FormatInt(svc.ID, 10)},
			Ports: []corev1.ServicePort{{
				Name:       "http",
				Port:       80,
				TargetPort: intstr.FromInt32(port),
				Protocol:   corev1.ProtocolTCP,
			}},
		},
	}
	var ing *networkingv1.Ingress
	host := ""
	if svc.Metadata != nil {
		if v, ok := svc.Metadata["host"].(string); ok {
			host = v
		}
	}
	if host != "" {
		pathType := networkingv1.PathTypePrefix
		ing = &networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:        workloadName,
				Namespace:   svc.Namespace,
				Labels:      labels,
				Annotations: map[string]string{"nginx.ingress.kubernetes.io/proxy-body-size": "100m"},
			},
			Spec: networkingv1.IngressSpec{
				Rules: []networkingv1.IngressRule{{
					Host: host,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{{
								Path:     "/",
								PathType: &pathType,
								Backend: networkingv1.IngressBackend{
									Service: &networkingv1.IngressServiceBackend{
										Name: extServiceName,
										Port: networkingv1.ServiceBackendPort{Number: 80},
									},
								},
							}},
						},
					},
				}},
			},
		}
	}
	return ing, extSvc
}

func buildLabels(svc *inference.InferenceService, appName string) map[string]string {
	labels := map[string]string{
		vortexManagedLabel:      "true",
		vortexInferenceSvcLabel: strconv.FormatInt(svc.ID, 10),
		"app":                   appName,
		"framework":             string(svc.Framework),
	}
	for k, v := range svc.Labels {
		labels[k] = fmt.Sprintf("%v", v)
	}
	return labels
}

func buildContainer(svc *inference.InferenceService, mv *inference.ModelVersion, adapters []*inference.ModelAdapter, reg *inference.ModelRegistry, port int32) corev1.Container {
	image := frameworkImage(svc.Framework, svc.FrameworkConfig)
	modelPath := resolveModelPath(svc, mv, reg)
	env := []corev1.EnvVar{
		{Name: "MODEL_PATH", Value: modelPath},
		{Name: "TENSOR_PARALLEL_SIZE", Value: strconv.Itoa(defaultInt(svc.TensorParallelSize, 1))},
		{Name: "PIPELINE_PARALLEL_SIZE", Value: strconv.Itoa(defaultInt(svc.PipelineParallelSize, 1))},
	}
	for k, v := range svc.FrameworkConfig {
		env = append(env, corev1.EnvVar{Name: k, Value: fmt.Sprintf("%v", v)})
	}
	if len(adapters) > 0 {
		paths := make([]string, 0, len(adapters))
		for _, a := range adapters {
			paths = append(paths, a.WeightsPath)
		}
		env = append(env, corev1.EnvVar{Name: "LORA_MODULES", Value: joinPaths(paths)})
	}
	args, cmd := frameworkArgs(svc.Framework, modelPath, svc, port)
	container := corev1.Container{
		Name:            svc.Name,
		Image:           image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Ports:           []corev1.ContainerPort{{Name: "http", ContainerPort: port, Protocol: corev1.ProtocolTCP}},
		Env:             env,
		Resources:       buildResources(svc),
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{Path: frameworkHealthPath(svc.Framework), Port: intstr.FromInt32(port)},
			},
			InitialDelaySeconds: 60,
			PeriodSeconds:       10,
			FailureThreshold:    30,
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{Path: frameworkHealthPath(svc.Framework), Port: intstr.FromInt32(port)},
			},
			InitialDelaySeconds: 120,
			PeriodSeconds:       30,
			FailureThreshold:    5,
		},
	}
	if cmd != nil {
		container.Command = cmd
	}
	if len(args) > 0 {
		container.Args = args
	}
	return container
}

// resolveModelPath 解析容器内模型路径：
// 若 registry 配置了 cache_pvc，则路径相对于挂载点；否则使用 weights_path 原值。
func resolveModelPath(svc *inference.InferenceService, mv *inference.ModelVersion, reg *inference.ModelRegistry) string {
	if reg != nil && reg.CachePVCName != "" {
		// 容器挂载点为 /models，对应 registry.cache_path
		// 下载器写入 /models/<registry.name>/<model.name> 或 weights_path
		modelName := ""
		if reg.Name != "" {
			modelName = safeName(mv.WeightsPath)
			if modelName == "" {
				modelName = fmt.Sprintf("model-%d/weights", mv.ModelID)
			}
		}
		return fmt.Sprintf("/models/%s", modelName)
	}
	if mv.WeightsPath != "" {
		return mv.WeightsPath
	}
	return fmt.Sprintf("/models/model-%d", mv.ModelID)
}

// frameworkArgs 根据框架生成正确的启动 CLI 参数。
// 返回 (args, command)。command 通常为 nil（用镜像默认 entrypoint）。
func frameworkArgs(fw inference.Framework, modelPath string, svc *inference.InferenceService, port int32) (args []string, command []string) {
	tp := defaultInt(svc.TensorParallelSize, 1)
	pp := defaultInt(svc.PipelineParallelSize, 1)
	switch fw {
	case inference.FrameworkVLLM:
		// vLLM vllm-openai 镜像默认入口：vllm serve
		args = []string{
			"--model", modelPath,
			"--served-model-name", svc.Name,
			"--host", "0.0.0.0",
			"--port", strconv.Itoa(int(port)),
			"--tensor-parallel-size", strconv.Itoa(tp),
			"--pipeline-parallel-size", strconv.Itoa(pp),
			"--trust-remote-code",
		}
		if svc.FrameworkConfig != nil {
			if v, ok := svc.FrameworkConfig["gpu_memory_utilization"].(float64); ok {
				args = append(args, "--gpu-memory-utilization", strconv.FormatFloat(v, 'f', -1, 64))
			}
			if v, ok := svc.FrameworkConfig["max_model_len"].(float64); ok {
				args = append(args, "--max-model-len", strconv.Itoa(int(v)))
			}
			if v, ok := svc.FrameworkConfig["dtype"].(string); ok && v != "" {
				args = append(args, "--dtype", v)
			}
			if v, ok := svc.FrameworkConfig["quantization"].(string); ok && v != "" {
				args = append(args, "--quantization", v)
			}
		}
	case inference.FrameworkTGI:
		args = []string{
			"--model-id", modelPath,
			"--hostname", "0.0.0.0",
			"--port", strconv.Itoa(int(port)),
			"--sharded", strconv.Itoa(tp),
		}
		if svc.FrameworkConfig != nil {
			if v, ok := svc.FrameworkConfig["dtype"].(string); ok && v != "" {
				args = append(args, "--dtype", v)
			}
			if v, ok := svc.FrameworkConfig["max_input_length"].(float64); ok {
				args = append(args, "--max-input-length", strconv.Itoa(int(v)))
			}
			if v, ok := svc.FrameworkConfig["max_total_tokens"].(float64); ok {
				args = append(args, "--max-total-tokens", strconv.Itoa(int(v)))
			}
		}
	case inference.FrameworkTriton:
		// Triton 需要模型仓库目录结构，modelPath 指向 repository 根目录
		command = []string{"tritonserver"}
		args = []string{
			"--model-repository", modelPath,
			"--http-port", strconv.Itoa(int(port)),
			"--grpc-port", "8001",
			"--metrics-port", "8002",
		}
	case inference.FrameworkSGLang:
		args = []string{
			"--model-path", modelPath,
			"--port", strconv.Itoa(int(port)),
			"--tp", strconv.Itoa(tp),
			"--host", "0.0.0.0",
		}
	case inference.FrameworkOllama:
		command = []string{"ollama", "serve"}
	default:
		// custom：透传 framework_config.args
		if svc.FrameworkConfig != nil {
			if a, ok := svc.FrameworkConfig["args"].([]any); ok {
				for _, x := range a {
					args = append(args, fmt.Sprintf("%v", x))
				}
			}
		}
	}
	return args, command
}

// frameworkHealthPath 返回各框架的 readiness 端点。
func frameworkHealthPath(fw inference.Framework) string {
	switch fw {
	case inference.FrameworkTGI:
		return "/health"
	case inference.FrameworkTriton:
		return "/v2/health/ready"
	case inference.FrameworkOllama:
		return "/api/tags"
	default:
		return "/health"
	}
}

func buildResources(svc *inference.InferenceService) corev1.ResourceRequirements {
	req := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{},
		Limits:   corev1.ResourceList{},
	}
	if svc.Resources != nil {
		if v, ok := svc.Resources["cpu"].(string); ok && v != "" {
			if q, err := resource.ParseQuantity(v); err == nil {
				req.Requests[corev1.ResourceCPU] = q
			}
		}
		if v, ok := svc.Resources["memory"].(string); ok && v != "" {
			if q, err := resource.ParseQuantity(v); err == nil {
				req.Requests[corev1.ResourceMemory] = q
			}
		}
		if v, ok := svc.Resources["cpu_limit"].(string); ok && v != "" {
			if q, err := resource.ParseQuantity(v); err == nil {
				req.Limits[corev1.ResourceCPU] = q
			}
		}
		if v, ok := svc.Resources["memory_limit"].(string); ok && v != "" {
			if q, err := resource.ParseQuantity(v); err == nil {
				req.Limits[corev1.ResourceMemory] = q
			}
		}
	}
	if svc.GPUCount > 0 {
		gpuName := corev1.ResourceName("nvidia.com/gpu")
		if v, ok := svc.Resources["gpu_resource"].(string); ok && v != "" {
			gpuName = corev1.ResourceName(v)
		}
		req.Limits[gpuName] = *resource.NewQuantity(int64(svc.GPUCount), resource.DecimalSI)
	}
	return req
}

func buildHPA(svc *inference.InferenceService, workloadName string, labels map[string]string) *autoscalingv2.HorizontalPodAutoscaler {
	minRep := int32(defaultInt(svc.HPAMinReplicas, 1))
	maxRep := int32(defaultInt(svc.HPAMaxReplicas, svc.Replicas*2))
	if maxRep < minRep {
		maxRep = minRep
	}
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      workloadName,
			Namespace: svc.Namespace,
			Labels:    labels,
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       workloadName,
			},
			MinReplicas: &minRep,
			MaxReplicas: maxRep,
		},
	}
	if svc.HPAMetrics != nil {
		if metrics, ok := svc.HPAMetrics["metrics"].([]any); ok {
			for _, m := range metrics {
				if mm, ok := m.(map[string]any); ok {
					hpaMetric := autoscalingv2.MetricSpec{}
					if t, ok := mm["type"].(string); ok {
						hpaMetric.Type = autoscalingv2.MetricSourceType(t)
					}
					if r, ok := mm["resource"].(map[string]any); ok {
						resName := corev1.ResourceName(fmt.Sprintf("%v", r["name"]))
						avgUtil := int32(toInt(r["average_utilization"]))
						hpaMetric.Resource = &autoscalingv2.ResourceMetricSource{
							Name: resName,
							Target: autoscalingv2.MetricTarget{
								Type:               autoscalingv2.UtilizationMetricType,
								AverageUtilization: &avgUtil,
							},
						}
					}
					hpa.Spec.Metrics = append(hpa.Spec.Metrics, hpaMetric)
				}
			}
		}
	}
	if len(hpa.Spec.Metrics) == 0 {
		avgUtil := int32(70)
		hpa.Spec.Metrics = []autoscalingv2.MetricSpec{{
			Type: autoscalingv2.ResourceMetricSourceType,
			Resource: &autoscalingv2.ResourceMetricSource{
				Name: corev1.ResourceCPU,
				Target: autoscalingv2.MetricTarget{
					Type:               autoscalingv2.UtilizationMetricType,
					AverageUtilization: &avgUtil,
				},
			},
		}}
	}
	return hpa
}

func frameworkImage(fw inference.Framework, cfg map[string]any) string {
	if cfg != nil {
		if v, ok := cfg["image"].(string); ok && v != "" {
			return v
		}
	}
	switch fw {
	case inference.FrameworkTGI:
		return "ghcr.io/huggingface/text-generation-inference:latest"
	case inference.FrameworkTriton:
		return "nvcr.io/nvidia/tritonserver:24.01-py3"
	case inference.FrameworkSGLang:
		return "lmsysorg/sglang:latest"
	case inference.FrameworkOllama:
		return "ollama/ollama:latest"
	default:
		return "vllm/vllm-openai:latest"
	}
}

func frameworkPort(fw inference.Framework) int32 {
	switch fw {
	case inference.FrameworkTGI:
		return 80
	case inference.FrameworkTriton:
		return 8000
	case inference.FrameworkOllama:
		return 11434
	default:
		return 8000
	}
}

func defaultInt(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}

func toInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return 0
	}
}

func joinPaths(paths []string) string {
	out := ""
	for i, p := range paths {
		if i > 0 {
			out += ","
		}
		out += p
	}
	return out
}
