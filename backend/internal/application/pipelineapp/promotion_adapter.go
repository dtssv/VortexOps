// Package pipelineapp 的晋升适配器：把 releaseapp/approvalapp/pipelineapp 自身能力
// 适配为 PromotionChecker + PromotionDeployer，注入 Service 后状态机校验链生效。
//
// 依赖（通过接口注入，避免循环依赖）：
//   - SignatureReader：读镜像签名状态（pipelineapp.Service.GetSignature 实现）
//   - ReleaseWindowChecker：发布窗口校验（releaseapp.WindowChecker 实现，按 appID）
//   - ApprovalReader：审批查询（approvalapp.Service.GetPendingByResource 实现）
//   - ReleaseTrigger：触发部署（releaseapp.Service.TriggerRelease 实现）
package pipelineapp

import (
	"context"
	"fmt"
	"time"

	"github.com/vortexops/vortexops/internal/domain/approval"
	"github.com/vortexops/vortexops/internal/domain/pipeline"
)

// --- 适配器依赖接口 ---

// SignatureReader 读镜像签名状态。
type SignatureReader interface {
	GetSignature(ctx context.Context, imageID int64) (*pipeline.ArtifactSignature, error)
}

// ReleaseWindowChecker 发布窗口校验器（按 appID）。
type ReleaseWindowChecker interface {
	IsWithinWindow(ctx context.Context, appID int64, now time.Time) (bool, string, error)
}

// ApprovalReader 审批查询器。
type ApprovalReader interface {
	GetPendingByResource(ctx context.Context, rt approval.ResourceType, resourceID int64) (*approval.Approval, error)
}

// ReleaseTrigger 发布触发器（对目标 group 触发部署）。
type ReleaseTrigger interface {
	// TriggerPromotionRelease 触发一次晋升部署。
	// 返回 releaseID 与错误。triggerBy 为操作人。
	TriggerPromotionRelease(ctx context.Context, groupID, imageID, triggerBy int64) (int64, error)
}

// --- PromotionCheckerImpl ---

// PromotionCheckerImpl 晋升前置校验器实现。
type PromotionCheckerImpl struct {
	signatureReader SignatureReader
	windowChecker   ReleaseWindowChecker
	approvalReader  ApprovalReader
}

// NewPromotionChecker 创建晋升校验器。
func NewPromotionChecker(sig SignatureReader, wc ReleaseWindowChecker, ar ApprovalReader) *PromotionCheckerImpl {
	return &PromotionCheckerImpl{signatureReader: sig, windowChecker: wc, approvalReader: ar}
}

// CheckSignature 检查镜像是否已签名且验证通过。
// 未找到签名记录或验证状态非 verified 视为校验失败。
func (c *PromotionCheckerImpl) CheckSignature(ctx context.Context, imageID int64) error {
	if c.signatureReader == nil {
		return nil // 未注入签名读取器，跳过
	}
	sig, err := c.signatureReader.GetSignature(ctx, imageID)
	if err != nil {
		// 签名记录不存在 → 未签名 → 阻断晋升。
		return fmt.Errorf("image %d has no signature record: %w", imageID, err)
	}
	if sig.VerificationStatus != pipeline.VerifyVerified {
		return fmt.Errorf("image %d signature not verified (status: %s)", imageID, sig.VerificationStatus)
	}
	return nil
}

// CheckReleaseWindow 检查指定应用是否在发布窗口内。
// 调用 ReleaseWindowChecker.IsWithinWindow 判断；无活跃窗口视为放行。
func (c *PromotionCheckerImpl) CheckReleaseWindow(ctx context.Context, appID int64, targetEnv string) error {
	if c.windowChecker == nil {
		return nil // 未注入窗口校验器，跳过
	}
	ok, reason, err := c.windowChecker.IsWithinWindow(ctx, appID, time.Now())
	if err != nil {
		// 校验出错不阻断（与 releaseapp.EnsureWithinReleaseWindow 一致的软降级策略）。
		return nil
	}
	if !ok {
		return fmt.Errorf("target env %s outside release window: %s", targetEnv, reason)
	}
	return nil
}

// CheckApproval 检查是否已获审批。
// 查询 promotion 关联的审批记录：若存在 pending 审批则未通过；存在 approved 则通过。
func (c *PromotionCheckerImpl) CheckApproval(ctx context.Context, promotionID int64) error {
	if c.approvalReader == nil {
		return nil // 未注入审批读取器，跳过
	}
	pending, err := c.approvalReader.GetPendingByResource(ctx, approval.ResourcePromotion, promotionID)
	if err != nil {
		// 无 pending 记录 → 视为无需审批或已审批，放行。
		return nil
	}
	if pending != nil && pending.Status == approval.StatusPending {
		return fmt.Errorf("promotion %d has pending approval (approval id: %d)", promotionID, pending.ID)
	}
	return nil
}

// 编译期断言。
var _ PromotionChecker = (*PromotionCheckerImpl)(nil)

// --- PromotionDeployerImpl ---

// PromotionDeployerImpl 晋升部署执行器实现。
type PromotionDeployerImpl struct {
	releaseTrigger ReleaseTrigger
}

// NewPromotionDeployer 创建晋升部署器。
func NewPromotionDeployer(rt ReleaseTrigger) *PromotionDeployerImpl {
	return &PromotionDeployerImpl{releaseTrigger: rt}
}

// ExecutePromotionDeploy 对目标环境每个 group 触发部署。
func (d *PromotionDeployerImpl) ExecutePromotionDeploy(ctx context.Context, imageID int64, targetGroupIDs []int64, triggerBy int64) ([]int64, error) {
	if d.releaseTrigger == nil {
		return nil, nil // 未注入触发器，跳过（由外部异步部署）
	}
	releaseIDs := make([]int64, 0, len(targetGroupIDs))
	for _, gid := range targetGroupIDs {
		relID, err := d.releaseTrigger.TriggerPromotionRelease(ctx, gid, imageID, triggerBy)
		if err != nil {
			return releaseIDs, fmt.Errorf("trigger release for group %d: %w", gid, err)
		}
		releaseIDs = append(releaseIDs, relID)
	}
	return releaseIDs, nil
}

// 编译期断言。
var _ PromotionDeployer = (*PromotionDeployerImpl)(nil)
