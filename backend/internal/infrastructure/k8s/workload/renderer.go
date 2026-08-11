// Package workload 把 Group 期望态渲染为 K8s 工作负载对象（Deployment/StatefulSet/CronJob/Job）
// 及附属资源（Service/HPA/NetworkPolicy/Ingress）。
// 渲染器为纯函数，无 IO；调用方（releaseapp）拿到对象后通过 ClientPool 应用到集群。
package workload

import (
	"encoding/json"
	"fmt"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/vortexops/vortexops/internal/domain/application"
	"github.com/vortexops/vortexops/internal/domain/networkprofile"
	ciliuminfra "github.com/vortexops/vortexops/internal/infrastructure/k8s/cilium"
	"github.com/vortexops/vortexops/internal/infrastructure/k8s/mesh"
)

const (
	vortexManagedLabel = "app.vortexops.io/managed"
	vortexGroupLabel   = "app.vortexops.io/group-id"
	keepPodIPAnnotation = "app.vortexops.io/keep-pod-ip"
	// 多版本共存标签：区分主/候选 Deployment 的 Pod。
	vortexRoleLabel     = "app.vortexops.io/role"       // "primary" | "candidate"
	vortexReleaseLabel  = "app.vortexops.io/release-id" // 发布 ID，候选 Pod 钉版本
)

// RenderResult 渲染输出：主工作负载 + 附属资源。
// 多版本共存模式下 CandidateWorkload 为候选 Deployment（与主共存，共享 selector）。
// 注：不再创建 K8s Service/Ingress/NetworkPolicy——所有端口默认暴露，外部通过稳定 Pod IP 直连。
type RenderResult struct {
	Workload      any // *appsv1.Deployment | *appsv1.StatefulSet | *batchv1.CronJob | *batchv1.Job
	CandidateWorkload any // 仅 percentage/machine_count 策略：*appsv1.Deployment（{name}-candidate）
	ConfigMap     *corev1.ConfigMap // 分组配置文件（group-<id>-config），需在 Workload 之前 apply
	HPA           *autoscalingv2.HorizontalPodAutoscaler
	// CiliumResources Cilium eBPF L4 LB 等资源（Phase 3，替代 K8s Service）。
	CiliumResources []*unstructured.Unstructured
	// MeshResources 分组 Mesh CRD（Phase 5，MeshEnabled=true 时渲染）。
	MeshResources []*unstructured.Unstructured
}

// RenderInput 渲染输入。
type RenderInput struct {
	Group       *application.Group
	ImageRef    string // 完整镜像引用（registry/repo:tag）
	ConfigMounts []ConfigMount // 配置挂载（历史，逐步被 Config 取代）
	Config      *ResolvedConfig // 分组生效配置（env/command/args/files），优先于 ConfigMounts
	StableIPs   []string // keep_pod_ip 分配的稳定 IP 列表（按 replica 顺序）
	// ImagePullSecrets 私有镜像仓库拉取凭证名称列表。
	// 非公开镜像仓库发布时由 releaseapp 注入（系统默认 registry 对应的 K8s Secret）。
	ImagePullSecrets []string

	// --- 多版本共存（candidate Deployment 模式）---
	// CandidateImageRef 非空时渲染候选 Deployment（与主 Deployment 共存）。
	// 用于 percentage/machine_count 分批发布：候选承载新版本，分批 scale up；
	// 主 Deployment 承载旧版本，对应 scale down。晋升后候选→主，删旧主。
	CandidateImageRef string
	// CandidateReplicas 候选 Deployment 初始副本数（通常 0 起步，分批推进时由 releaseapp scale）。
	CandidateReplicas int
	// CandidateReleaseID 候选发布 ID（写入 Pod 标签 vortexReleaseLabel）。
	CandidateReleaseID int64
	// PrimaryReplicasOverride 主 Deployment 副本数覆盖（多版本模式下主缩为 N-candidate）。
	// 0 表示用 Group.Replicas。
	PrimaryReplicasOverride int
	// CandidatePodNames machine_count 策略：候选 Pod 钉到这些 Pod 名（通过 hostname/podName 约束）。
	// K8s 无法强制 Pod 名，改用 nodeAffinity 把候选调度到目标 Pod 所在节点（简化实现）。
	CandidatePodNames []string

	// DeploymentStrategyOverride 发布级 Deployment 策略（rolling/recreate），覆盖 group.workload.strategy。
	DeploymentStrategyOverride string
	// MaxSurgeOverride / MaxUnavailableOverride 发布级滚动参数，非空时覆盖 group.workload。
	MaxSurgeOverride       string
	MaxUnavailableOverride string

	// AppProbe 应用级探活配置（从 application.Metadata.probe 解析）。
	// 当 group 自身 HealthCheck 为空时，作为兜底同时注入 Readiness+Liveness Probe。
	AppProbe *application.ProbeConfig

	// NetworkProfile 集群网络方案配置（从 cluster.metadata.network_profile 解析）。
	// 决定 CNI annotation 注入方式（一律不建 Service，对外以固定 Pod IP 直连）：
	//   - large-underlay：注入 Macvlan/IPVLAN/Kube-OVN annotation，Pod 拿物理 IP。
	//   - xlarge-bgp：注入 Calico/Cilium 静态 IP annotation，路由由 BGP 宣告。
	//   - dev-single / medium-overlay：注入 Multus NAD + Underlay 固定 IP（副网卡直连）。
	// 为 nil 时按旧逻辑（兼容未登记 profile 的老集群）。
	NetworkProfile *networkprofile.ProfileConfig
}

// ConfigMount 配置挂载（来自 config binding）。
type ConfigMount struct {
	Name      string
	MountPath string
	SubPath   string
	EnvFrom   bool // 是否作为 envFrom 注入
}

// ResolvedConfig 发布时解析出的分组生效配置（互斥：绑定配置集优先，否则本地配置）。
// content 结构与 vo_config_sets.content / vo_group_local_configs.content 同构。
type ResolvedConfig struct {
	Files   []ResolvedFile
	Env     []ResolvedEnv
	Command []string
	Args    []string
}

// ResolvedFile 配置文件（按文件自身 path 挂载到容器内同路径，替换镜像内同名文件）。
type ResolvedFile struct {
	Path    string
	Content string
	Mode    string // 默认 0644
}

// ResolvedEnv 环境变量（注入为容器环境变量）。
type ResolvedEnv struct {
	Name  string
	Value string
}

// ParseResolvedConfig 把 ConfigSet/GroupLocalConfig 的 Content（JSONB）解析为 ResolvedConfig。
// content 结构：{files:[{path,content,mode,is_secret}], env:[{name,value,is_secret}], command:[...], args:[...]}
func ParseResolvedContent(raw map[string]any) *ResolvedConfig {
	rc := &ResolvedConfig{}
	if raw == nil {
		return rc
	}
	if files, ok := raw["files"].([]any); ok {
		for _, f := range files {
			m, ok := f.(map[string]any)
			if !ok {
				continue
			}
			path, _ := m["path"].(string)
			if path == "" {
				continue
			}
			content, _ := m["content"].(string)
			mode, _ := m["mode"].(string)
			if mode == "" {
				mode = "0644"
			}
			rc.Files = append(rc.Files, ResolvedFile{Path: path, Content: content, Mode: mode})
		}
	}
	if env, ok := raw["env"].([]any); ok {
		for _, e := range env {
			m, ok := e.(map[string]any)
			if !ok {
				continue
			}
			name, _ := m["name"].(string)
			if name == "" {
				continue
			}
			value, _ := m["value"].(string)
			rc.Env = append(rc.Env, ResolvedEnv{Name: name, Value: value})
		}
	}
	if cmd, ok := raw["command"].([]any); ok {
		for _, c := range cmd {
			if s, ok := c.(string); ok {
				rc.Command = append(rc.Command, s)
			}
		}
	}
	if args, ok := raw["args"].([]any); ok {
		for _, a := range args {
			if s, ok := a.(string); ok {
				rc.Args = append(rc.Args, s)
			}
		}
	}
	return rc
}

// Render 渲染 Group 为 K8s 资源集合。
func Render(in RenderInput) (*RenderResult, error) {
	if in.Group == nil {
		return nil, fmt.Errorf("group is required")
	}
	if in.ImageRef == "" {
		return nil, fmt.Errorf("image_ref is required")
	}
	g := in.Group

	result := &RenderResult{}

	// 多版本共存：渲染候选 Deployment（仅 Deployment 工作负载支持，StatefulSet 退化为 rolling）。
	candidateMode := in.CandidateImageRef != "" && g.Workload.Type == application.WorkloadDeployment
	if candidateMode {
		candLabels := buildLabels(g)
		candLabels[vortexRoleLabel] = "candidate"
		if in.CandidateReleaseID != 0 {
			candLabels[vortexReleaseLabel] = strconv.FormatInt(in.CandidateReleaseID, 10)
		}
		// 候选 selector 共享 group-id（便于多版本 Pod 识别），但 pod template 加 role=candidate 区分。
		// 注：不再创建 Service，流量分流由 Cilium eBPF L4 LB（Phase 3）或域名映射（Phase 4）接管。
		candSelector := &metav1.LabelSelector{MatchLabels: map[string]string{vortexGroupLabel: strconv.FormatInt(g.ID, 10)}}
		candTemplate := buildPodTemplate(g, in.CandidateImageRef, candLabels, in.ConfigMounts, in.Config, in.StableIPs, in.ImagePullSecrets, in.AppProbe, in.NetworkProfile)
		// machine_count：用 nodeAffinity 把候选钉到目标 Pod 所在节点（通过 podName 反查节点，由调用方注入 nodeSelector）。
		// 此处仅设置 role 标签，节点亲和由 releaseapp 在 scale 阶段动态设置（候选 Pod 数=目标 Pod 数）。
		result.CandidateWorkload = buildCandidateDeployment(g, candLabels, candSelector, candTemplate, in.CandidateReplicas, in.CandidateReleaseID)
	}

	primaryLabels := buildLabels(g)
	primaryLabels[vortexRoleLabel] = "primary"
	if candidateMode {
		// 多版本模式下主副本数=覆盖值（N-candidate）；默认用 Group.Replicas。
		// buildDeployment 读 g.Replicas，故临时设置 PrimaryReplicasOverride 到 g 上不安全（共享指针），
		// 改为构建后覆盖 replicas。
	}
	selector := &metav1.LabelSelector{MatchLabels: map[string]string{vortexGroupLabel: strconv.FormatInt(g.ID, 10)}}
	podTemplate := buildPodTemplate(g, in.ImageRef, primaryLabels, in.ConfigMounts, in.Config, in.StableIPs, in.ImagePullSecrets, in.AppProbe, in.NetworkProfile)

	deployStrategy := g.Workload.Strategy
	if in.DeploymentStrategyOverride != "" {
		deployStrategy = application.Strategy(in.DeploymentStrategyOverride)
	}
	maxSurge := g.Workload.MaxSurge
	if in.MaxSurgeOverride != "" {
		maxSurge = in.MaxSurgeOverride
	}
	maxUnavailable := g.Workload.MaxUnavailable
	if in.MaxUnavailableOverride != "" {
		maxUnavailable = in.MaxUnavailableOverride
	}

	switch g.Workload.Type {
	case application.WorkloadDeployment:
		dep := buildDeployment(g, deployStrategy, maxSurge, maxUnavailable, primaryLabels, selector, podTemplate)
		if candidateMode && in.PrimaryReplicasOverride >= 0 {
			r := int32(in.PrimaryReplicasOverride)
			dep.Spec.Replicas = &r
		}
		result.Workload = dep
	case application.WorkloadStatefulSet:
		result.Workload = buildStatefulSet(g, primaryLabels, selector, podTemplate)
	case application.WorkloadCronJob:
		result.Workload = buildCronJob(g, primaryLabels, podTemplate)
	case application.WorkloadJob:
		result.Workload = buildJob(g, primaryLabels, podTemplate)
	default:
		return nil, fmt.Errorf("unsupported workload type: %s", g.Workload.Type)
	}

	// HPA
	if g.Autoscaling != nil && g.Autoscaling.Enabled {
		result.HPA = buildHPA(g, primaryLabels, selector)
	}
	// ConfigMap（分组配置文件）。需在 Workload 之前 apply（Applier 保证顺序）。
	if in.Config != nil && len(in.Config.Files) > 0 {
		result.ConfigMap = buildConfigMap(g, in.Config)
	}
	// Phase 3: Cilium eBPF L4 LB（有稳定 IP 且数据面为 Cilium 时渲染）。
	if len(in.StableIPs) > 0 && in.NetworkProfile != nil && in.NetworkProfile.EffectiveDataPlane() == networkprofile.DataPlaneCilium {
		if lb := ciliuminfra.RenderL4LoadBalancer(ciliuminfra.L4LBInput{Group: g, BackendIPs: in.StableIPs}); lb != nil {
			result.CiliumResources = append(result.CiliumResources, lb)
		}
	}
	// Phase 5: 分组 Mesh CRD。
	if g.MeshEnabled {
		result.MeshResources = mesh.RenderAll(mesh.RenderInput{Group: g, StableIPs: in.StableIPs, Namespace: g.Namespace})
	}
	return result, nil
}

// buildCandidateDeployment 构建候选 Deployment（{name}-candidate）。
// 与主 Deployment 共享 selector（group-id），但 Pod template 带 role=candidate + release-id 标签。
// 初始 replicas 通常为 0，分批推进时由 releaseapp 通过 /scale subresource 递增。
func buildCandidateDeployment(g *application.Group, labels map[string]string, selector *metav1.LabelSelector, template corev1.PodTemplateSpec, replicas int, releaseID int64) *appsv1.Deployment {
	r := int32(replicas)
	name := g.DeploymentName + "-candidate"
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: g.Namespace,
			Labels:    labels,
			Annotations: map[string]string{
				"app.vortexops.io/candidate": "true",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &r,
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDeployment{
					MaxUnavailable: ptrIntStr(intstr.FromInt(0)),
					MaxSurge:       intstrPtrOrDefault("", "25%"),
				},
			},
			Selector: selector,
			Template: template,
		},
	}
	if releaseID != 0 {
		dep.Annotations["app.vortexops.io/release-id"] = strconv.FormatInt(releaseID, 10)
	}
	return dep
}

func buildLabels(g *application.Group) map[string]string {
	labels := map[string]string{
		vortexManagedLabel: "true",
		vortexGroupLabel:   strconv.FormatInt(g.ID, 10),
		"app":              g.DeploymentName,
	}
	if g.MeshEnabled {
		labels["app.vortexops.io/mesh-enabled"] = "true"
	}
	for k, v := range g.Labels {
		labels[k] = v
	}
	return labels
}

func buildPodTemplate(g *application.Group, imageRef string, labels map[string]string, mounts []ConfigMount, cfg *ResolvedConfig, stableIPs []string, imagePullSecrets []string, appProbe *application.ProbeConfig, profile *networkprofile.ProfileConfig) corev1.PodTemplateSpec {
	container := corev1.Container{
		Name:            g.DeploymentName,
		Image:           imageRef,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Resources:       buildResources(g),
	}
	// 注：不再注入容器端口声明——所有端口默认暴露，外部通过稳定 Pod IP 直连。
	// 健康检查：group 自身 HealthCheck 优先；为空时兜底用应用级 AppProbe
	// 同时注入 Readiness+Liveness（就绪判定与失败重启共用同一策略）。
	if g.HealthCheck != nil {
		container.LivenessProbe = buildProbe(g.HealthCheck.LivenessProbe)
		container.ReadinessProbe = buildProbe(g.HealthCheck.ReadinessProbe)
		container.StartupProbe = buildProbe(g.HealthCheck.StartupProbe)
	} else if appProbe != nil && appProbe.Enabled {
		probe := buildProbeFromAppConfig(appProbe)
		container.ReadinessProbe = probe
		container.LivenessProbe = probe
	}
	// 生效配置（env/command/args/files）。Config 优先于历史 ConfigMounts。
	var volumeMounts []corev1.VolumeMount
	var volumes []corev1.Volume
	var envFrom []corev1.EnvFromSource
	if cfg != nil {
		// env → 注入容器环境变量。
		for _, e := range cfg.Env {
			container.Env = append(container.Env, corev1.EnvVar{Name: e.Name, Value: e.Value})
		}
		// command/args → 非空时覆盖镜像 ENTRYPOINT/CMD。
		if len(cfg.Command) > 0 {
			container.Command = cfg.Command
		}
		if len(cfg.Args) > 0 {
			container.Args = cfg.Args
		}
		// files → 按文件自身 path 挂载，替换容器内同名文件。
		// 单个 ConfigMap（group-<id>-config）承载所有文件，每文件以 basename 作为 key，
		// 通过 SubPath 挂载到其绝对路径（避免覆盖目录下其他文件）。
		if len(cfg.Files) > 0 {
			cmName := fmt.Sprintf("group-%d-config", g.ID)
			volName := sanitizeVolumeName(cmName)
			volumes = append(volumes, corev1.Volume{
				Name: volName,
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: cmName},
					},
				},
			})
			for _, f := range cfg.Files {
				key := fileBaseName(f.Path)
				if key == "" {
					key = "config"
				}
				volumeMounts = append(volumeMounts, corev1.VolumeMount{
					Name:      volName,
					MountPath: f.Path,
					SubPath:   key,
				})
			}
		}
	}
	// 历史 ConfigMounts（兼容旧绑定，逐步废弃）。
	for _, m := range mounts {
		if m.EnvFrom {
			envFrom = append(envFrom, corev1.EnvFromSource{
				ConfigMapRef: &corev1.ConfigMapEnvSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: m.Name},
				},
			})
			continue
		}
		volumeName := sanitizeVolumeName(m.Name)
		volumes = append(volumes, corev1.Volume{
			Name: volumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: m.Name},
				},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      volumeName,
			MountPath: m.MountPath,
			SubPath:   m.SubPath,
		})
	}
	container.VolumeMounts = volumeMounts
	container.EnvFrom = envFrom

	podSpec := corev1.PodSpec{
		Containers:    []corev1.Container{container},
		NodeSelector:  g.Scheduling.NodeSelector,
		Tolerations:   buildTolerations(g.Scheduling.Tolerations),
		// 注：不再设置 DNSPolicy/HostNetwork——统一走集群默认 DNS（ClusterFirst）与 Pod 网络。
		// 稳定 IP 由 CNI 注解保证（Calico/Cilium），不依赖 hostNetwork。
		RestartPolicy: corev1.RestartPolicyAlways,
	}
	if g.Scheduling.PriorityClass != "" {
		podSpec.PriorityClassName = g.Scheduling.PriorityClass
	}
	if len(volumes) > 0 {
		podSpec.Volumes = volumes
	}
	if len(imagePullSecrets) > 0 {
		refs := make([]corev1.LocalObjectReference, 0, len(imagePullSecrets))
		for _, name := range imagePullSecrets {
			if name != "" {
				refs = append(refs, corev1.LocalObjectReference{Name: name})
			}
		}
		podSpec.ImagePullSecrets = refs
	}

	template := corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      labels,
			Annotations: buildAnnotations(g, stableIPs, profile),
		},
		Spec: podSpec,
	}
	return template
}

func buildAnnotations(g *application.Group, stableIPs []string, profile *networkprofile.ProfileConfig) map[string]string {
	ann := map[string]string{}
	// Phase 2 架构：稳定 IP 注入由 Mutating Webhook 在 Pod 创建时完成（单 Pod 单 IP，支持多副本复用）。
	// renderer 仅注入标记注解，webhook 检测到后从 group 的预分配 IP 池中选择对应槽位的 IP。
	// 若 webhook 未部署（Phase 0/1 兼容）：保留旧逻辑注入首个 IP 作为降级（多副本仍走 CNI 默认 IPAM）。
	if len(stableIPs) > 0 {
		ann[keepPodIPAnnotation] = "true"
		// 标记：webhook 见此注解且无 stable-ip-0 时执行分配。
		ann["app.vortexops.io/stable-ip-needed"] = "true"
		// 兼容降级（webhook 未部署）：注入首个 IP。webhook 部署后会覆盖为正确的单 Pod IP。
		// 多副本场景下此降级不完美（所有 Pod 拿同 IP），生产环境必须部署 webhook。
		ann["app.vortexops.io/stable-ip-0"] = stableIPs[0]
		injectCNIAnnotations(ann, []string{stableIPs[0]}, profile)
	}
	for k, v := range g.Metadata {
		ann[k] = fmt.Sprintf("%v", v)
	}
	return ann
}

// injectCNIAnnotations 按 CNI provider 注入固定 IP 的 annotation。
// Overlay/dev：业务口走 Multus Underlay 副网卡（物理固定 IP），默认网卡仍走 Overlay。
// underlay / BGP：按主 CNI 注入静态 IP 注解。
func injectCNIAnnotations(ann map[string]string, stableIPs []string, profile *networkprofile.ProfileConfig) {
	if len(stableIPs) == 0 {
		return
	}
	// Overlay/开发集群：固定 IP 必须打在 Multus 副网卡上，才能对外直连。
	if profile != nil && profile.RequiresUnderlaySecondary() {
		ann["k8s.v1.cni.cncf.io/networks"] = networksJSON(profile.NADName(), stableIPs)
		return
	}
	cni := networkprofile.CNIWhereabouts // 默认（兼容旧 whereabouts 池）
	if profile != nil && profile.CNI != "" {
		cni = profile.CNI
	}
	switch cni {
	case networkprofile.CNIWhereabouts:
		ann["k8s.v1.cni.cncf.io/ipAddrs"] = jsonArray(stableIPs)
	case networkprofile.CNICalico:
		ann["cni.projectcalico.org/ipAddrs"] = jsonArray(stableIPs)
	case networkprofile.CNICilium:
		for k, v := range ciliuminfra.BuildStaticIPAnnotations(stableIPs[0], ciliuminfra.DefaultIPPoolName) {
			ann[k] = v
		}
		// Calico 链式兼容（开发环境 Calico+Cilium 迁移期）。
		ciliuminfra.MergeCalicoCompat(ann, stableIPs[0])
	case networkprofile.CNIKubeOVN:
		ann["ovn.kubernetes.io/ip_address"] = stableIPs[0]
	case networkprofile.CNIMacvlan, networkprofile.CNIIPVLAN:
		nadName := profile.NADName()
		ann["k8s.v1.cni.cncf.io/networks"] = networksJSON(nadName, stableIPs)
	}
}

// jsonArray 把字符串切片序列化为 JSON 数组字符串（如 ["10.1.1.5"]）。
func jsonArray(items []string) string {
	b, _ := json.Marshal(items)
	return string(b)
}

// networksJSON 生成 Multus networks annotation（指定 NAD + 固定 IP）。
// 格式参考 Multus：[{"name":"macvlan-100","ips":["10.1.1.5"]}]
func networksJSON(nadName string, ips []string) string {
	type netEntry struct {
		Name string   `json:"name"`
		IPs  []string `json:"ips,omitempty"`
	}
	entries := []netEntry{{Name: nadName, IPs: ips}}
	b, _ := json.Marshal(entries)
	return string(b)
}

func buildResources(g *application.Group) corev1.ResourceRequirements {
	req := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{},
		Limits:   corev1.ResourceList{},
	}
	if g.Resources.CPUm > 0 {
		req.Requests[corev1.ResourceCPU] = *resource.NewMilliQuantity(int64(g.Resources.CPUm), resource.DecimalSI)
	}
	if g.Resources.MemoryBytes > 0 {
		req.Requests[corev1.ResourceMemory] = *resource.NewQuantity(g.Resources.MemoryBytes, resource.BinarySI)
	}
	if g.Resources.CPULimitM > 0 {
		req.Limits[corev1.ResourceCPU] = *resource.NewMilliQuantity(int64(g.Resources.CPULimitM), resource.DecimalSI)
	}
	if g.Resources.MemoryLimitBytes > 0 {
		req.Limits[corev1.ResourceMemory] = *resource.NewQuantity(g.Resources.MemoryLimitBytes, resource.BinarySI)
	}
	if g.Resources.GPU > 0 {
		gpuName := corev1.ResourceName("nvidia.com/gpu")
		if g.Resources.GPUResourceName != "" {
			gpuName = corev1.ResourceName(g.Resources.GPUResourceName)
		}
		req.Limits[gpuName] = *resource.NewQuantity(int64(g.Resources.GPU), resource.DecimalSI)
	}
	if g.Storage.EphemeralStorageRequestBytes > 0 {
		req.Requests[corev1.ResourceEphemeralStorage] = *resource.NewQuantity(g.Storage.EphemeralStorageRequestBytes, resource.BinarySI)
	}
	if g.Storage.EphemeralStorageLimitBytes > 0 {
		req.Limits[corev1.ResourceEphemeralStorage] = *resource.NewQuantity(g.Storage.EphemeralStorageLimitBytes, resource.BinarySI)
	}
	return req
}

// buildProbeFromAppConfig 把应用级 ProbeConfig 渲染为原生 K8s Probe（Readiness/Liveness 共用）。
// tcp → TCPSocket；process → Exec pgrep；both → 单 Exec 内先查本机端口再 pgrep（K8s Probe 不能同时挂 TCP+Exec）。
// period/timeout/failure_threshold 使用配置值或固定默认。
func buildProbeFromAppConfig(p *application.ProbeConfig) *corev1.Probe {
	if p == nil || !p.Enabled {
		return nil
	}
	probe := &corev1.Probe{}
	switch p.Method {
	case application.ProbeMethodTCP:
		probe.TCPSocket = &corev1.TCPSocketAction{
			Port: intstr.FromInt(p.Port),
		}
	case application.ProbeMethodProcess:
		probe.Exec = &corev1.ExecAction{
			Command: []string{"sh", "-c", fmt.Sprintf("pgrep -f %q >/dev/null 2>&1", p.ProcessKeyword)},
		}
	case application.ProbeMethodBoth:
		// 本机 TCP + 进程关键字均成功才通过。
		probe.Exec = &corev1.ExecAction{
			Command: []string{"sh", "-c", fmt.Sprintf(
				"exec 3<>/dev/tcp/127.0.0.1/%d && exec 3<&- && pgrep -f %q >/dev/null 2>&1",
				p.Port, p.ProcessKeyword,
			)},
		}
	}
	if probe.TCPSocket == nil && probe.Exec == nil {
		return nil
	}
	period := p.PeriodSeconds
	if period <= 0 {
		period = 30
	}
	timeout := p.TimeoutSeconds
	if timeout <= 0 {
		timeout = 5
	}
	failure := p.FailureThreshold
	if failure <= 0 {
		failure = 3
	}
	probe.PeriodSeconds = int32(period)
	probe.TimeoutSeconds = int32(timeout)
	probe.FailureThreshold = int32(failure)
	return probe
}

func buildProbe(probeMap map[string]any) *corev1.Probe {
	if len(probeMap) == 0 {
		return nil
	}
	p := &corev1.Probe{}
	if v, ok := probeMap["http_get"].(map[string]any); ok {
		p.HTTPGet = &corev1.HTTPGetAction{
			Path: getString(v, "path"),
			Port: intstr.FromInt(getInt(v, "port")),
		}
	} else if v, ok := probeMap["tcp_socket"].(map[string]any); ok {
		p.TCPSocket = &corev1.TCPSocketAction{
			Port: intstr.FromInt(getInt(v, "port")),
		}
	} else if v, ok := probeMap["exec"].(map[string]any); ok {
		if cmd, ok := v["command"].([]any); ok {
			command := make([]string, 0, len(cmd))
			for _, c := range cmd {
				command = append(command, fmt.Sprintf("%v", c))
			}
			p.Exec = &corev1.ExecAction{Command: command}
		}
	}
	if v, ok := probeMap["initial_delay_seconds"].(float64); ok {
		p.InitialDelaySeconds = int32(v)
	}
	if v, ok := probeMap["period_seconds"].(float64); ok {
		p.PeriodSeconds = int32(v)
	}
	if v, ok := probeMap["timeout_seconds"].(float64); ok {
		p.TimeoutSeconds = int32(v)
	}
	if v, ok := probeMap["failure_threshold"].(float64); ok {
		p.FailureThreshold = int32(v)
	}
	if v, ok := probeMap["success_threshold"].(float64); ok {
		p.SuccessThreshold = int32(v)
	}
	return p
}

func buildTolerations(raw []map[string]any) []corev1.Toleration {
	var tols []corev1.Toleration
	for _, t := range raw {
		tol := corev1.Toleration{
			Key:      getString(t, "key"),
			Operator: corev1.TolerationOperator(getString(t, "operator")),
			Value:    getString(t, "value"),
			Effect:   corev1.TaintEffect(getString(t, "effect")),
		}
		if v, ok := t["toleration_seconds"].(float64); ok {
			tol.TolerationSeconds = ptrInt64(int64(v))
		}
		tols = append(tols, tol)
	}
	return tols
}

func buildDeployment(g *application.Group, strategy application.Strategy, maxSurge, maxUnavailable string, labels map[string]string, selector *metav1.LabelSelector, template corev1.PodTemplateSpec) *appsv1.Deployment {
	replicas := int32(g.Replicas)
	depStrategy := appsv1.DeploymentStrategy{
		Type: appsv1.RollingUpdateDeploymentStrategyType,
	}
	if strategy == application.StrategyRecreate {
		depStrategy.Type = appsv1.RecreateDeploymentStrategyType
	} else {
		if maxSurge != "" || maxUnavailable != "" {
			depStrategy.RollingUpdate = &appsv1.RollingUpdateDeployment{
				MaxUnavailable: intstrPtrOrDefault(maxUnavailable, "25%"),
				MaxSurge:       intstrPtrOrDefault(maxSurge, "25%"),
			}
		}
	}
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      g.DeploymentName,
			Namespace: g.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Strategy: depStrategy,
			Selector: selector,
			Template: template,
		},
	}
}

func buildStatefulSet(g *application.Group, labels map[string]string, selector *metav1.LabelSelector, template corev1.PodTemplateSpec) *appsv1.StatefulSet {
	replicas := int32(g.Replicas)
	ss := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      g.DeploymentName,
			Namespace: g.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas:       &replicas,
			Selector:       selector,
			Template:       template,
			ServiceName:    g.ServiceName,
		},
	}
	// 存储：若配置 storage_class/size，生成 volumeClaimTemplates。
	if g.Storage.StorageSizeBytes > 0 {
		pvcName := "data"
		ss.Spec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{
			{
				ObjectMeta: metav1.ObjectMeta{Name: pvcName},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: *resource.NewQuantity(g.Storage.StorageSizeBytes, resource.BinarySI),
						},
					},
				},
			},
		}
		if g.Storage.StorageClass != "" {
			ss.Spec.VolumeClaimTemplates[0].Spec.StorageClassName = &g.Storage.StorageClass
		}
	}
	return ss
}

func buildCronJob(g *application.Group, labels map[string]string, template corev1.PodTemplateSpec) *batchv1.CronJob {
	cj := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      g.DeploymentName,
			Namespace: g.Namespace,
			Labels:    labels,
		},
		Spec: batchv1.CronJobSpec{
			Schedule: g.Workload.CronSchedule,
			JobTemplate: batchv1.JobTemplateSpec{
				Spec: batchv1.JobSpec{Template: template},
			},
		},
	}
	// CronJob 的 Pod 不能用 Always 重启策略。
	cj.Spec.JobTemplate.Spec.Template.Spec.RestartPolicy = corev1.RestartPolicyOnFailure
	if v, ok := g.Workload.JobPolicy["concurrency_policy"].(string); ok {
		cj.Spec.ConcurrencyPolicy = batchv1.ConcurrencyPolicy(v)
	}
	if v, ok := g.Workload.JobPolicy["successful_jobs_history_limit"].(float64); ok {
		cj.Spec.SuccessfulJobsHistoryLimit = ptrInt32(int32(v))
	}
	if v, ok := g.Workload.JobPolicy["failed_jobs_history_limit"].(float64); ok {
		cj.Spec.FailedJobsHistoryLimit = ptrInt32(int32(v))
	}
	return cj
}

func buildJob(g *application.Group, labels map[string]string, template corev1.PodTemplateSpec) *batchv1.Job {
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      g.DeploymentName,
			Namespace: g.Namespace,
			Labels:    labels,
		},
		Spec: batchv1.JobSpec{Template: template},
	}
	job.Spec.Template.Spec.RestartPolicy = corev1.RestartPolicyOnFailure
	if v, ok := g.Workload.JobPolicy["completions"].(float64); ok {
		job.Spec.Completions = ptrInt32(int32(v))
	}
	if v, ok := g.Workload.JobPolicy["parallelism"].(float64); ok {
		job.Spec.Parallelism = ptrInt32(int32(v))
	}
	if v, ok := g.Workload.JobPolicy["backoff_limit"].(float64); ok {
		job.Spec.BackoffLimit = ptrInt32(int32(v))
	}
	return job
}

func buildHPA(g *application.Group, labels map[string]string, selector *metav1.LabelSelector) *autoscalingv2.HorizontalPodAutoscaler {
	minRep := int32(g.Autoscaling.MinReplicas)
	maxRep := int32(g.Autoscaling.MaxReplicas)
	if minRep == 0 {
		minRep = 1
	}
	if maxRep == 0 {
		maxRep = 10
	}
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      g.DeploymentName,
			Namespace: g.Namespace,
			Labels:    labels,
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       g.DeploymentName,
			},
			MinReplicas: &minRep,
			MaxReplicas: maxRep,
		},
	}
	// 把 metrics（map）序列化为 HPA metrics（简化：CPU/内存内置，自定义透传）。
	for _, m := range g.Autoscaling.Metrics {
		hpaMetric := autoscalingv2.MetricSpec{}
		if t, ok := m["type"].(string); ok {
			hpaMetric.Type = autoscalingv2.MetricSourceType(t)
		}
		if r, ok := m["resource"].(map[string]any); ok {
			resName := corev1.ResourceName(getString(r, "name"))
			avgUtil := int32(getInt(r, "average_utilization"))
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
	return hpa
}

// --- helpers ---

func getString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getInt(m map[string]any, key string) int {
	if v, ok := m[key].(float64); ok {
		return int(v)
	}
	return 0
}

func orStr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func intstrPtrOrDefault(s string, def string) *intstr.IntOrString {
	if s == "" {
		s = def
	}
	v := intstr.FromString(s)
	return &v
}

func ptrInt32(v int32) *int32 { return &v }
func ptrInt64(v int64) *int64 { return &v }

func ptrIntStr(v intstr.IntOrString) *intstr.IntOrString { return &v }

func sanitizeVolumeName(name string) string {
	// K8s 卷名须小写字母/数字/-。
	out := []rune{}
	for _, c := range name {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			out = append(out, c)
		} else if c >= 'A' && c <= 'Z' {
			out = append(out, c+32)
		} else {
			out = append(out, '-')
		}
	}
	if len(out) == 0 {
		return "config"
	}
	return string(out)
}

// fileBaseName 取路径的 basename 作为 ConfigMap key（避免路径分隔符非法）。
// 如 /etc/app/app.conf → app.conf；config/app.yaml → app.yaml。
func fileBaseName(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[i+1:]
		}
	}
	return path
}

// buildConfigMap 构建分组配置文件的 ConfigMap（group-<id>-config）。
// Data 的 key 为文件 basename，value 为文件内容。is_secret 不区分（{{...}} 由应用启动时解密）。
func buildConfigMap(g *application.Group, cfg *ResolvedConfig) *corev1.ConfigMap {
	data := make(map[string]string, len(cfg.Files))
	for _, f := range cfg.Files {
		key := fileBaseName(f.Path)
		if key == "" {
			key = "config"
		}
		// basename 冲突时后者覆盖前者（同目录下不应有同名文件，跨目录同名属用户责任）。
		data[key] = f.Content
	}
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("group-%d-config", g.ID),
			Namespace: g.Namespace,
			Labels: map[string]string{
				vortexManagedLabel: "true",
				vortexGroupLabel:   strconv.FormatInt(g.ID, 10),
			},
		},
		Data: data,
	}
}

// InferenceRenderInput 是 RenderInference 的输入，转译 infrender.RenderInput。
// 引入此类型让 releaseapp 在统一发布流中可调度推理部署，无需直接依赖 inference 渲染包。
type InferenceRenderInput struct {
	Group        *application.Group
	InferenceSvc any // *inference.InferenceService（用 any 避免此包直接依赖 inference domain）
	ModelVersion any // *inference.ModelVersion
	Adapters     any // []*inference.ModelAdapter
	Registry     any // *inference.ModelRegistry
}

// RenderInference 渲染推理服务 K8s 资源。
// 此为统一发布流的入口：releaseapp 检测 group.AppType=inference 时调用本函数。
// 实际渲染逻辑委托给 infrender.Render（保持单一真相源），本函数仅做参数透传与类型桥接。
//
// 注意：调用方需自行把 any 类型的字段断言回具体类型后传给 infrender.Render；
// 为避免本包对 inference domain 的循环依赖，这里返回 any 类型结果（*infrender.RenderResult）。
//
// 为简化首版统一发布，当前推理部署仍由 inferenceapp.runDeploy 直接执行，
// 本函数保留为可扩展点，供后续 releaseapp 完全接管推理发布时使用。
func RenderInference(_ InferenceRenderInput) (any, error) {
	// 占位实现：返回错误提示调用方应直接使用 infrender.Render。
	// 完整桥接在 releaseapp 集成阶段补全；此处保留接口稳定性。
	return nil, fmt.Errorf("RenderInference: use infrender.Render directly for now")
}
