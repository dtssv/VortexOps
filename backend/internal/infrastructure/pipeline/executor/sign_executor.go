// Package executor 的制品签名执行器：调用 cosign 对镜像签名，
// 签名结果记录到 pipeline.ArtifactSignature（通过 SignatureRecorder）。
//
// 依赖外部 cosign 二进制（默认 PATH，可通过系统设置 platform.cosign_path 覆盖）。
// 密钥支持两种方式：
//   - keyless：通过 OIDC（cosign keyless，需要环境变量配置 OIDC issuer）
//   - key-pair：通过 COSIGN_PRIVATE_KEY / COSIGN_PASSWORD 环境变量
//
// 本执行器不绑定具体密钥后端，只负责拼装命令并执行；密钥分发由部署环境保证。
package executor

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/vortexops/vortexops/internal/domain/build"
	"github.com/vortexops/vortexops/internal/domain/pipeline"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// SignExecutor 对镜像执行 cosign 签名。
type SignExecutor struct {
	imageRepo  ImageResolver
	recorder   SignatureRecorder
	cosignPath string // 可空：空则使用 PATH 中的 cosign
}

// ImageResolver 解析 image_id 到 build.Image（取 FullReference）。
type ImageResolver interface {
	GetImageByID(ctx context.Context, id int64) (*build.Image, error)
}

// SignatureRecorder 记录签名结果到 pipeline.ArtifactSignature。
// 由 pipelineapp.Service 实现（RecordSignature 方法）。
type SignatureRecorder interface {
	RecordSignature(ctx context.Context, in RecordSignatureInput) (*pipeline.ArtifactSignature, error)
}

// RecordSignatureInput 与 pipelineapp.RecordSignatureInput 字段一致，避免直接依赖 application 层（防止循环）。
type RecordSignatureInput struct {
	ImageID            int64
	SignatureType      pipeline.SignatureType
	SignaturePayload   string
	PublicKeyRef       string
	SignedBy           string
	SBOMStorageKey     string
	SBOMFormat         pipeline.SBOMFormat
	Provenance         map[string]any
	VerificationStatus pipeline.VerificationStatus
	CreatedBy          int64
}

// NewSignExecutor 创建签名执行器。
// cosignPath 为空时使用系统 PATH 中的 cosign。
func NewSignExecutor(imageRepo ImageResolver, recorder SignatureRecorder, cosignPath string) *SignExecutor {
	return &SignExecutor{imageRepo: imageRepo, recorder: recorder, cosignPath: cosignPath}
}

// Kind 步骤类型。
func (e *SignExecutor) Kind() StepKind { return StepSign }

// Execute 执行签名。
//
// step 参数：
//   - image_id（可选，默认取上游产物 image_id）
//   - keyless（bool，默认 false）：是否使用 keyless 模式
//   - identity（string，keyless 模式下的 identity，可选）
//   - issuer（string，keyless 模式下的 OIDC issuer，可选）
//   - timeout_sec（int，默认 120）
//
// 签名成功后记录到 ArtifactSignature（VerificationStatus=verified）。
func (e *SignExecutor) Execute(ctx context.Context, stageCtx *StageContext, step map[string]any) (*StepResult, error) {
	imageID := pickImageID(stageCtx, step)
	if imageID == 0 {
		return failed(step, "no image to sign"), nil
	}
	img, err := e.imageRepo.GetImageByID(ctx, imageID)
	if err != nil {
		return failed(step, "get image: "+err.Error()), nil
	}
	if img.FullReference == "" {
		return failed(step, "image has no full reference"), nil
	}

	binary := e.cosignPath
	if binary == "" {
		binary = "cosign"
	}

	keyless, _ := step["keyless"].(bool)
	timeoutSec := 120
	if t, ok := step["timeout_sec"].(float64); ok && t > 0 {
		timeoutSec = int(t)
	}

	// 构造命令参数。digest 钉死版本，避免 tag 漂移。
	ref := img.FullReference
	if img.Digest != "" && !strings.Contains(ref, "@") {
		ref = ref + "@" + img.Digest
	}
	args := []string{"sign", "--yes", ref}
	publicKeyRef := ""
	signedBy := "cosign-keypair"
	if keyless {
		signedBy = "cosign-keyless"
		args = append(args, "--keyless=true")
		if identity, ok := step["identity"].(string); ok && identity != "" {
			args = append(args, "--identity", identity)
		}
		if issuer, ok := step["issuer"].(string); ok && issuer != "" {
			args = append(args, "--oidc-issuer", issuer)
		}
	} else {
		// key-pair 模式：公钥引用从环境变量 COSIGN_PUBLIC_KEY 读取（可选）。
		publicKeyRef = os.Getenv("COSIGN_PUBLIC_KEY")
	}

	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, binary, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// 继承当前进程环境（COSIGN_PRIVATE_KEY/PASSWORD/_oidc 等已由部署环境注入）。
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		return failed(step, fmt.Sprintf("cosign sign failed: %v; stderr: %s", err, stderr.String())), nil
	}

	// 记录签名到 ArtifactSignature。
	prov := map[string]any{
		"tool":      "cosign",
		"keyless":   keyless,
		"ref":       ref,
		"stdout":    stdout.String(),
		"signed_at": time.Now().UTC().Format(time.RFC3339),
	}
	if e.recorder != nil {
		_, err := e.recorder.RecordSignature(ctx, RecordSignatureInput{
			ImageID:            imageID,
			SignatureType:      pipeline.SigCosign,
			SignaturePayload:   stderr.String(), // cosign 把签名 bundle 输出到 stderr
			PublicKeyRef:       publicKeyRef,
			SignedBy:           signedBy,
			Provenance:         prov,
			VerificationStatus: pipeline.VerifyVerified,
			CreatedBy:          stageCtx.Run.TriggerBy,
		})
		if err != nil {
			if ae, ok := apperr.As(err); ok {
				return failed(step, "record signature: "+ae.Message), nil
			}
			return failed(step, "record signature: "+err.Error()), nil
		}
	}

	return &StepResult{
		Kind: StepSign, Name: nameOf(step), Status: "succeeded",
		Outputs: map[string]any{
			"image_id":       float64(imageID),
			"signed":         true,
			"signature_type": string(pipeline.SigCosign),
			"keyless":        keyless,
		},
		Message: fmt.Sprintf("image %s signed with cosign", ref),
	}, nil
}
