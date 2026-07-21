package inference

import (
	"fmt"
	"strconv"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/vortexops/vortexops/internal/domain/inference"
)

const (
	vortexDownloadLabel       = "app.vortexops.io/model-version-download"
	vortexModelVersionLabel   = "app.vortexops.io/model-version-id"
	downloadMountPath         = "/models"
	downloadHFImage           = "huggingface/huggingface-cli:0.26.2-python3.11"
	downloadS3Image           = "amazon/aws-cli:2.15"
	downloadOSSImage          = "registry.aliyuncs.com/acs/ossutil:1.7"
	downloadGenericImage      = "curlimages/curl:8.10"
)

// DownloadJobInput 渲染下载 Job 的输入。
type DownloadJobInput struct {
	JobName       string
	Namespace     string
	Registry      *inference.ModelRegistry
	ModelVersion  *inference.ModelVersion
	ModelName     string
	ClusterID     int64
	CredentialRef string
}

// RenderDownloadJob 渲染一个 K8s Job，将权重拉取到 registry 的 cache PVC 中。
func RenderDownloadJob(in DownloadJobInput) (*batchv1.Job, error) {
	if in.Registry == nil || in.ModelVersion == nil {
		return nil, fmt.Errorf("registry and model version are required")
	}
	pvc := in.Registry.CachePVCName
	if pvc == "" {
		return nil, fmt.Errorf("registry has no cache_pvc_name configured")
	}
	cachePath := in.Registry.CachePath
	if cachePath == "" {
		cachePath = "/models"
	}

	labels := map[string]string{
		vortexManagedLabel:      "true",
		vortexDownloadLabel:     "true",
		vortexModelVersionLabel: strconv.FormatInt(in.ModelVersion.ID, 10),
	}

	ttl := int32(3600)
	backoff := int32(2)
	parallelism := int32(1)
	completions := int32(1)

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      in.JobName,
			Namespace: in.Namespace,
			Labels:    labels,
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: &ttl,
			BackoffLimit:            &backoff,
			Parallelism:             &parallelism,
			Completions:             &completions,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyOnFailure,
					Containers: []corev1.Container{
						buildDownloadContainer(in.Registry, in.ModelVersion, in.ModelName, cachePath),
					},
					Volumes: []corev1.Volume{
						{
							Name: "model-cache",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: pvc,
								},
							},
						},
					},
				},
			},
		},
	}
	return job, nil
}

func buildDownloadContainer(reg *inference.ModelRegistry, mv *inference.ModelVersion, modelName, cachePath string) corev1.Container {
	mountPath := downloadMountPath
	targetDir := fmt.Sprintf("%s/%s/%s", mountPath, reg.Name, safeName(modelName))
	if mv.WeightsPath != "" {
		targetDir = fmt.Sprintf("%s/%s", mountPath, mv.WeightsPath)
	}

	var args []string
	var image string
	switch reg.Provider {
	case inference.ProviderHuggingFace:
		image = downloadHFImage
		repo := modelName
		if repo == "" {
			repo = mv.WeightsPath
		}
		args = []string{
			"download", repo,
			"--local-dir", targetDir,
			"--local-dir-use-symlinks", "False",
		}
		revision := mv.Version
		if revision != "" && revision != "latest" {
			args = append(args, "--revision", revision)
		}
	case inference.ProviderS3:
		image = downloadS3Image
		args = []string{"s3", "sync", mv.WeightsPath, targetDir}
	case inference.ProviderOSS:
		image = downloadOSSImage
		args = []string{"sync", mv.WeightsPath, targetDir}
	default:
		image = downloadGenericImage
		args = []string{"-fL", mv.WeightsPath, "-o", targetDir + "/weights.bin"}
	}

	resources := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("512Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("2"),
			corev1.ResourceMemory: resource.MustParse("2Gi"),
		},
	}

	return corev1.Container{
		Name:            "downloader",
		Image:           image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Args:            args,
		Resources:       resources,
		VolumeMounts: []corev1.VolumeMount{
			{Name: "model-cache", MountPath: mountPath, SubPath: stripLeadingSlash(cachePath)},
		},
	}
}

func safeName(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '/' {
			out = append(out, c)
		} else if c == ' ' {
			out = append(out, '-')
		}
	}
	return string(out)
}

func stripLeadingSlash(s string) string {
	for len(s) > 0 && s[0] == '/' {
		s = s[1:]
	}
	return s
}
