// Package executor 的 SBOM 生成执行器：调用 syft 对镜像生成 SBOM，
// 上传到 S3（通过 SBOMUploader），并记录到 pipeline.ArtifactSignature。
//
// 依赖外部 syft 二进制（默认 PATH，可通过系统设置 platform.syft_path 覆盖）。
// 支持两种输出格式：cyclonedx-json（默认）与 spdx-json。
package executor

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/vortexops/vortexops/internal/domain/pipeline"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// SBOMExecutor 生成 SBOM 并上传 S3。
type SBOMExecutor struct {
	imageRepo ImageResolver
	uploader  SBOMUploader
	recorder  SignatureRecorder
	syftPath  string // 可空：空则使用 PATH 中的 syft
}

// SBOMUploader 上传 SBOM 内容到对象存储。由 s3.LogStore 实现（Upload 方法）。
type SBOMUploader interface {
	Upload(ctx context.Context, key string, data []byte) error
}

// NewSBOMExecutor 创建 SBOM 执行器。
// syftPath 为空时使用系统 PATH 中的 syft。
func NewSBOMExecutor(imageRepo ImageResolver, uploader SBOMUploader, recorder SignatureRecorder, syftPath string) *SBOMExecutor {
	return &SBOMExecutor{imageRepo: imageRepo, uploader: uploader, recorder: recorder, syftPath: syftPath}
}

// Kind 步骤类型。
func (e *SBOMExecutor) Kind() StepKind { return StepSBOM }

// Execute 生成 SBOM。
//
// step 参数：
//   - image_id（可选，默认取上游产物 image_id）
//   - format（string，cyclonedx | spdx，默认 cyclonedx）
//   - timeout_sec（int，默认 180）
//   - upload_prefix（string，S3 key 前缀，默认 "sbom/"）
//
// 生成后上传 S3，并把 storage_key/format 记录到 ArtifactSignature（merging 到已有签名记录）。
func (e *SBOMExecutor) Execute(ctx context.Context, stageCtx *StageContext, step map[string]any) (*StepResult, error) {
	imageID := pickImageID(stageCtx, step)
	if imageID == 0 {
		return failed(step, "no image for SBOM"), nil
	}
	img, err := e.imageRepo.GetImageByID(ctx, imageID)
	if err != nil {
		return failed(step, "get image: "+err.Error()), nil
	}
	if img.FullReference == "" {
		return failed(step, "image has no full reference"), nil
	}

	// 解析格式：syft 输出格式名为 cyclonedx-json / spdx-json。
	formatStr, _ := step["format"].(string)
	var sbomFormat pipeline.SBOMFormat
	var syftFormat string
	switch strings.ToLower(formatStr) {
	case "", "cyclonedx":
		sbomFormat = pipeline.SBOMCycloneDX
		syftFormat = "cyclonedx-json"
	case "spdx":
		sbomFormat = pipeline.SBOMSPDX
		syftFormat = "spdx-json"
	default:
		return failed(step, "unsupported sbom format: "+formatStr), nil
	}

	binary := e.syftPath
	if binary == "" {
		binary = "syft"
	}
	timeoutSec := 180
	if t, ok := step["timeout_sec"].(float64); ok && t > 0 {
		timeoutSec = int(t)
	}

	ref := img.FullReference
	if img.Digest != "" && !strings.Contains(ref, "@") {
		ref = ref + "@" + img.Digest
	}

	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, binary, ref, "-o", syftFormat)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return failed(step, fmt.Sprintf("syft sbom failed: %v; stderr: %s", err, stderr.String())), nil
	}
	sbomBytes := stdout.Bytes()
	if len(sbomBytes) == 0 {
		return failed(step, "syft produced empty SBOM"), nil
	}

	// 上传 S3。
	prefix, _ := step["upload_prefix"].(string)
	if prefix == "" {
		prefix = "sbom/"
	}
	storageKey := fmt.Sprintf("%simage-%d-%d.json", prefix, imageID, time.Now().Unix())
	if e.uploader != nil {
		if err := e.uploader.Upload(ctx, storageKey, sbomBytes); err != nil {
			return failed(step, "upload sbom to s3: "+err.Error()), nil
		}
	}

	// 记录到 ArtifactSignature（与签名记录 merge：sbom 字段补充到已存在签名）。
	prov := map[string]any{
		"tool":       "syft",
		"format":     syftFormat,
		"ref":        ref,
		"size_bytes": len(sbomBytes),
		"generated_at": time.Now().UTC().Format(time.RFC3339),
	}
	if e.recorder != nil {
		_, err := e.recorder.RecordSignature(ctx, RecordSignatureInput{
			ImageID:        imageID,
			SBOMStorageKey: storageKey,
			SBOMFormat:     sbomFormat,
			Provenance:     prov,
			// 签名字段保持空：若已有 cosign 签名记录，UpdateSignature 会保留原值；
			// 若无，则仅记录 SBOM（SignatureType 留空，由 service 层默认处理）。
			CreatedBy: stageCtx.Run.TriggerBy,
		})
		if err != nil {
			if ae, ok := apperr.As(err); ok {
				return failed(step, "record sbom: "+ae.Message), nil
			}
			return failed(step, "record sbom: "+err.Error()), nil
		}
	}

	return &StepResult{
		Kind: StepSBOM, Name: nameOf(step), Status: "succeeded",
		Outputs: map[string]any{
			"image_id":        float64(imageID),
			"sbom_storage_key": storageKey,
			"sbom_format":     string(sbomFormat),
			"sbom_size":       len(sbomBytes),
		},
		Message: fmt.Sprintf("SBOM (%s) generated and uploaded to %s", sbomFormat, storageKey),
	}, nil
}
