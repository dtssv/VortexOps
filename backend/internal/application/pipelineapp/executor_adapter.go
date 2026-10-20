// Package pipelineapp 的执行器适配层：把 pipelineapp.Service 适配为
// executor 所需的 SignatureRecorder / ImageResolver 接口，供 pipeline-worker 装配使用。
package pipelineapp

import (
	"context"

	"github.com/vortexops/vortexops/internal/domain/build"
	"github.com/vortexops/vortexops/internal/domain/pipeline"
	"github.com/vortexops/vortexops/internal/infrastructure/pipeline/executor"
)

// SignatureRecorderAdapter 把 pipelineapp.Service.RecordSignature 适配为
// executor.SignatureRecorder，供 SignExecutor / SBOMExecutor 使用。
type SignatureRecorderAdapter struct {
	svc *Service
}

// NewSignatureRecorderAdapter 创建适配器。
func NewSignatureRecorderAdapter(svc *Service) *SignatureRecorderAdapter {
	return &SignatureRecorderAdapter{svc: svc}
}

// RecordSignature 实现 executor.SignatureRecorder。
func (a *SignatureRecorderAdapter) RecordSignature(ctx context.Context, in executor.RecordSignatureInput) (*pipeline.ArtifactSignature, error) {
	return a.svc.RecordSignature(ctx, RecordSignatureInput{
		ImageID:            in.ImageID,
		SignatureType:      in.SignatureType,
		SignaturePayload:   in.SignaturePayload,
		PublicKeyRef:       in.PublicKeyRef,
		SignedBy:           in.SignedBy,
		SBOMStorageKey:     in.SBOMStorageKey,
		SBOMFormat:         in.SBOMFormat,
		Provenance:         in.Provenance,
		VerificationStatus: in.VerificationStatus,
		CreatedBy:          in.CreatedBy,
	})
}

// 编译期断言。
var _ executor.SignatureRecorder = (*SignatureRecorderAdapter)(nil)

// ImageResolverAdapter 把 build.Repository（GetImageByID）适配为
// executor.ImageResolver，供 SignExecutor / SBOMExecutor 使用。
type ImageResolverAdapter struct {
	repo build.Repository
}

// NewImageResolverAdapter 创建适配器。
func NewImageResolverAdapter(repo build.Repository) *ImageResolverAdapter {
	return &ImageResolverAdapter{repo: repo}
}

// GetImageByID 实现 executor.ImageResolver。
func (a *ImageResolverAdapter) GetImageByID(ctx context.Context, id int64) (*build.Image, error) {
	return a.repo.GetImageByID(ctx, id)
}

// 编译期断言。
var _ executor.ImageResolver = (*ImageResolverAdapter)(nil)

// SBOMUploaderAdapter 把 s3.LogStore.Upload 适配为 executor.SBOMUploader。
// 由于 s3.LogStore 已有 Upload(ctx, key, data) 方法，这里只是接口满足声明，
// 实际可直接传 *s3.LogStore 给 executor.NewSBOMExecutor，无需适配。
// 保留此注释说明装配关系，避免误用。
