// Package pipelineapp 的环境晋升状态机实现。
// 把 Promotion 从 pending 推进到 succeeded/failed，校验链：
//   pending → deploying（校验通过，触发目标环境部署）
//   deploying → verifying（部署完成，进入验证）
//   verifying → succeeded（验证通过）/ failed（验证失败）
//
// 校验项（按环境级别递进）：
//   - dev→staging：签名验证（若已配置 cosign 签名）+ 发布窗口
//   - staging→prod：签名验证 + 发布窗口 + 审批（PromoManual 策略必须人工审批）
//
// 状态机方法：
//   - StartPromotion：pending → deploying（执行前置校验）
//   - CompleteDeployment：deploying → verifying
//   - CompleteVerify：verifying → succeeded/failed
//   - AbortPromotion：任意非终态 → aborted
package pipelineapp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/vortexops/vortexops/internal/domain/pipeline"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// 环境级别（用于校验递进）。
const (
	EnvDev      = "dev"
	EnvStaging  = "staging"
	EnvProd     = "prod"
	EnvTest     = "test"
	EnvUAT      = "uat"
	EnvPre      = "pre"
)

// envLevel 环境级别排序（数字越大越严格）。
var envLevel = map[string]int{
	EnvDev:     1,
	EnvTest:    1,
	EnvUAT:     2,
	EnvStaging: 2,
	EnvPre:     3,
	EnvProd:    3,
}

// PromotionChecker 晋升前置校验器（可选注入）。
// 各方法返回 nil 表示校验通过，返回 error 表示阻断晋升。
type PromotionChecker interface {
	// CheckSignature 检查镜像是否已签名且验证通过。
	CheckSignature(ctx context.Context, imageID int64) error
	// CheckReleaseWindow 检查指定应用是否在发布窗口内（prod 环境必须）。
	CheckReleaseWindow(ctx context.Context, appID int64, targetEnv string) error
	// CheckApproval 检查是否已获审批（PromoManual 策略必须）。
	CheckApproval(ctx context.Context, promotionID int64) error
}

// PromotionDeployer 晋升部署执行器（可选注入，由 releaseapp 实现）。
// ExecutePromotionDeploy 对目标环境 group 触发实际部署，返回 releaseID。
type PromotionDeployer interface {
	ExecutePromotionDeploy(ctx context.Context, imageID int64, targetGroupIDs []int64, triggerBy int64) (releaseIDs []int64, err error)
}

// promotionChecker / promotionDeployer 可选注入字段（在 Service 上扩展）。
// 为避免改动 Service 结构体（已在 service.go 定义），这里用包级变量 + Setter 注入。
// 单实例部署足够；多实例时每个实例注入相同实现。

var promotionChecker PromotionChecker
var promotionDeployer PromotionDeployer

// WithPromotionChecker 注入晋升前置校验器。
func (s *Service) WithPromotionChecker(c PromotionChecker) *Service {
	promotionChecker = c
	return s
}

// WithPromotionDeployer 注入晋升部署执行器。
func (s *Service) WithPromotionDeployer(d PromotionDeployer) *Service {
	promotionDeployer = d
	return s
}

// StartPromotion 启动晋升：pending → deploying。
// 执行前置校验（签名 + 窗口 + 审批），校验通过后若注入了 deployer 则触发部署。
func (s *Service) StartPromotion(ctx context.Context, promotionID, operatorID int64) (*pipeline.Promotion, error) {
	p, err := s.repo.GetPromotionByID(ctx, promotionID)
	if err != nil {
		if errors.Is(err, pipeline.ErrPromotionNotFound) {
			return nil, apperr.NotFound("promotion", fmt.Sprint(promotionID))
		}
		return nil, apperr.Internal("get promotion", err)
	}
	if p.Status != pipeline.PromoStatusPending {
		return nil, apperr.BusinessRule("promotion not in pending state", fmt.Errorf("current status: %s", p.Status))
	}

	// 前置校验链。
	if err := s.validatePromotion(ctx, p); err != nil {
		// 校验失败直接标记 failed。
		p.Status = pipeline.PromoStatusFailed
		p.Audit.UpdatedBy = operatorID
		now := time.Now()
		p.FinishedAt = &now
		_ = s.repo.UpdatePromotion(ctx, p)
		return nil, err
	}

	// 校验通过 → deploying。
	p.Status = pipeline.PromoStatusDeploying
	p.Audit.UpdatedBy = operatorID
	if err := s.repo.UpdatePromotion(ctx, p); err != nil {
		return nil, apperr.Internal("update promotion to deploying", err)
	}

	// 触发部署（若注入了 deployer）。
	if promotionDeployer != nil {
		releaseIDs, err := promotionDeployer.ExecutePromotionDeploy(ctx, p.ArtifactImageID, p.TargetGroupIDs, operatorID)
		if err != nil {
			p.Status = pipeline.PromoStatusFailed
			now := time.Now()
			p.FinishedAt = &now
			_ = s.repo.UpdatePromotion(ctx, p)
			return nil, apperr.Internal("execute promotion deploy", err)
		}
		// 部署已触发，记录 releaseIDs（存入 metadata 或直接进入 verifying）。
		// 简化：部署触发即进入 verifying，由调用方后续调 CompleteVerify。
		_ = releaseIDs
		p.Status = pipeline.PromoStatusVerifying
		_ = s.repo.UpdatePromotion(ctx, p)
	}

	return p, nil
}

// CompleteDeployment 部署完成：deploying → verifying。
// 用于未注入 deployer 的场景（外部异步部署完成后手动推进）。
func (s *Service) CompleteDeployment(ctx context.Context, promotionID, operatorID int64) (*pipeline.Promotion, error) {
	p, err := s.repo.GetPromotionByID(ctx, promotionID)
	if err != nil {
		return nil, apperr.NotFound("promotion", fmt.Sprint(promotionID))
	}
	if p.Status != pipeline.PromoStatusDeploying {
		return nil, apperr.BusinessRule("promotion not in deploying state", fmt.Errorf("current: %s", p.Status))
	}
	p.Status = pipeline.PromoStatusVerifying
	p.Audit.UpdatedBy = operatorID
	if err := s.repo.UpdatePromotion(ctx, p); err != nil {
		return nil, apperr.Internal("update promotion to verifying", err)
	}
	return p, nil
}

// CompleteVerify 验证完成：verifying → succeeded/failed。
func (s *Service) CompleteVerify(ctx context.Context, promotionID, operatorID int64, passed bool, reason string) (*pipeline.Promotion, error) {
	p, err := s.repo.GetPromotionByID(ctx, promotionID)
	if err != nil {
		return nil, apperr.NotFound("promotion", fmt.Sprint(promotionID))
	}
	if p.Status != pipeline.PromoStatusVerifying {
		return nil, apperr.BusinessRule("promotion not in verifying state", fmt.Errorf("current: %s", p.Status))
	}
	now := time.Now()
	p.FinishedAt = &now
	p.Audit.UpdatedBy = operatorID
	if passed {
		p.Status = pipeline.PromoStatusSucceeded
	} else {
		p.Status = pipeline.PromoStatusFailed
	}
	if err := s.repo.UpdatePromotion(ctx, p); err != nil {
		return nil, apperr.Internal("update promotion final status", err)
	}
	return p, nil
}

// AbortPromotion 中止晋升：任意非终态 → aborted。
func (s *Service) AbortPromotion(ctx context.Context, promotionID, operatorID int64) (*pipeline.Promotion, error) {
	p, err := s.repo.GetPromotionByID(ctx, promotionID)
	if err != nil {
		return nil, apperr.NotFound("promotion", fmt.Sprint(promotionID))
	}
	if isPromotionTerminal(p.Status) {
		return nil, apperr.BusinessRule("promotion already terminal", fmt.Errorf("status: %s", p.Status))
	}
	p.Status = pipeline.PromoStatusAborted
	now := time.Now()
	p.FinishedAt = &now
	p.Audit.UpdatedBy = operatorID
	if err := s.repo.UpdatePromotion(ctx, p); err != nil {
		return nil, apperr.Internal("abort promotion", err)
	}
	return p, nil
}

// validatePromotion 执行前置校验链（按环境级别递进）。
func (s *Service) validatePromotion(ctx context.Context, p *pipeline.Promotion) error {
	if promotionChecker == nil {
		return nil // 未注入校验器，跳过（兼容）
	}
	targetLevel := envLevel[p.TargetEnv]
	sourceLevel := envLevel[p.SourceEnv]

	// 禁止降级晋升（prod→dev 等）。
	if targetLevel < sourceLevel {
		return apperr.BusinessRule("promotion target env level lower than source",
			fmt.Errorf("%s(level %d) → %s(level %d)", p.SourceEnv, sourceLevel, p.TargetEnv, targetLevel))
	}

	// 签名验证：跨环境晋升必须验证镜像签名（dev 内部晋升可跳过）。
	if targetLevel > sourceLevel && p.ArtifactImageID != 0 {
		if err := promotionChecker.CheckSignature(ctx, p.ArtifactImageID); err != nil {
			return apperr.BusinessRule("signature verification failed", err)
		}
	}

	// 发布窗口：staging/prod 级别必须检查发布窗口。
	if targetLevel >= 2 {
		if err := promotionChecker.CheckReleaseWindow(ctx, p.ApplicationID, p.TargetEnv); err != nil {
			return apperr.BusinessRule("release window check failed", err)
		}
	}

	// 审批：manual 策略必须人工审批；prod 级别强制审批。
	if p.Strategy == pipeline.PromoManual || targetLevel >= 3 {
		if err := promotionChecker.CheckApproval(ctx, p.ID); err != nil {
			return apperr.BusinessRule("approval check failed", err)
		}
	}

	return nil
}

// isPromotionTerminal 判断晋升状态是否终态。
func isPromotionTerminal(s pipeline.PromotionStatus) bool {
	switch s {
	case pipeline.PromoStatusSucceeded, pipeline.PromoStatusFailed, pipeline.PromoStatusAborted:
		return true
	}
	return false
}
