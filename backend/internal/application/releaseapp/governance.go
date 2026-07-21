package releaseapp

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/vortexops/vortexops/internal/domain/release"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// PauseRelease 暂停运行中的发布：标记 status=paused，并将工作负载副本缩为 0（保留模板）。
func (s *Service) PauseRelease(ctx context.Context, id, operatorID int64) (*release.Release, error) {
	rel, err := s.repo.GetReleaseByID(ctx, id)
	if err != nil {
		if errors.Is(err, release.ErrReleaseNotFound) {
			return nil, apperr.NotFound("release", strconv.FormatInt(id, 10))
		}
		return nil, apperr.Internal("get release", err)
	}
	if rel.Status != release.StatusRunning {
		return nil, apperr.BusinessRule("release cannot be paused in current state", nil)
	}

	// 缩容到 0（暂停调度），不删除工作负载。
	if g, gerr := s.groupRepo.GetGroup(ctx, rel.GroupID); gerr == nil {
		if clientset, cerr := s.clientProvider.GetClient(ctx, g.ClusterID); cerr == nil {
			_ = scaleWorkload(ctx, clientset, g, 0)
		}
	}

	s.appendEvent(ctx, id, "paused", "release paused by operator", operatorID)
	updated, err := s.repo.UpdateReleaseStatus(ctx, id, release.StatusPaused, rel.ProgressPercent, "", rel.Version)
	if err != nil {
		if errors.Is(err, fmt.Errorf("conflict")) {
			return nil, apperr.Conflict("release was modified concurrently, please refresh", err)
		}
		return nil, apperr.Internal("pause release", err)
	}
	return updated, nil
}

// ResumeRelease 恢复已暂停的发布：恢复副本并标记 status=running。
func (s *Service) ResumeRelease(ctx context.Context, id, operatorID int64) (*release.Release, error) {
	rel, err := s.repo.GetReleaseByID(ctx, id)
	if err != nil {
		if errors.Is(err, release.ErrReleaseNotFound) {
			return nil, apperr.NotFound("release", strconv.FormatInt(id, 10))
		}
		return nil, apperr.Internal("get release", err)
	}
	if rel.Status != release.StatusPaused {
		return nil, apperr.BusinessRule("release is not paused", nil)
	}

	// 恢复副本数。
	if g, gerr := s.groupRepo.GetGroup(ctx, rel.GroupID); gerr == nil {
		replicas := rel.Replicas
		if replicas == 0 {
			replicas = g.Replicas
		}
		if clientset, cerr := s.clientProvider.GetClient(ctx, g.ClusterID); cerr == nil {
			if serr := scaleWorkload(ctx, clientset, g, replicas); serr != nil {
				s.appendEvent(ctx, id, "resume_failed", fmt.Sprintf("scale workload: %v", serr), operatorID)
			}
		}
	}

	s.appendEvent(ctx, id, "resumed", "release resumed by operator", operatorID)
	updated, err := s.repo.UpdateReleaseStatus(ctx, id, release.StatusRunning, rel.ProgressPercent, "", rel.Version)
	if err != nil {
		return nil, apperr.Internal("resume release", err)
	}
	return updated, nil
}

// --- 发布窗口强制 ---

// WindowChecker 发布窗口校验器（按应用查询活跃窗口并判断当前是否在窗口内）。
type WindowChecker interface {
	// IsWithinWindow 返回是否在任意活跃窗口内；无活跃窗口视为不限制（返回 true）。
	IsWithinWindow(ctx context.Context, appID int64, now time.Time) (bool, string, error)
}

// EnsureWithinReleaseWindow 校验当前时间是否在应用的发布窗口内。
// 无活跃窗口时直接放行；存在活跃窗口但不在窗口内则返回业务错误。
func (s *Service) EnsureWithinReleaseWindow(ctx context.Context, appID int64, now time.Time) error {
	if s.windowChecker == nil {
		return nil
	}
	ok, reason, err := s.windowChecker.IsWithinWindow(ctx, appID, now)
	if err != nil {
		// 校验失败不阻塞发布，仅记录。
		return nil
	}
	if !ok {
		return apperr.BusinessRule("current time is outside the release window: "+reason, nil)
	}
	return nil
}

// WithWindowChecker 注入发布窗口校验器。
func (s *Service) WithWindowChecker(wc WindowChecker) *Service {
	s.windowChecker = wc
	return s
}

// --- 审批集成 ---

// ApprovalChecker 发布审批校验器：返回是否需要审批，以及创建审批后是否阻塞。
type ApprovalChecker interface {
	// RequireApproval 返回该 group 是否需要审批。
	RequireApproval(ctx context.Context, groupID int64) (bool, error)
	// CreateForRelease 为发布创建审批记录，返回 pending 审批 ID。
	CreateForRelease(ctx context.Context, workspaceID, groupID, releaseID int64, requestedBy int64) (int64, error)
}

// WithApprovalChecker 注入审批校验器。
func (s *Service) WithApprovalChecker(ac ApprovalChecker) *Service {
	s.approvalChecker = ac
	return s
}

// ExecutePendingRelease 审批通过后执行挂起的发布（status=pending_approval → running）。
func (s *Service) ExecutePendingRelease(ctx context.Context, releaseID, approverID int64) error {
	rel, err := s.repo.GetReleaseByID(ctx, releaseID)
	if err != nil {
		return err
	}
	if rel.Status != release.StatusPendingApproval {
		return nil // 幂等：非挂起状态忽略
	}
	g, err := s.groupRepo.GetGroup(ctx, rel.GroupID)
	if err != nil {
		return err
	}
	img, err := s.imageRepo.GetImage(ctx, rel.ImageID)
	if err != nil {
		return err
	}
	// 推进状态为 running。
	updated, err := s.repo.UpdateReleaseStatus(ctx, rel.ID, release.StatusRunning, 10, "", rel.Version)
	if err != nil {
		return err
	}
	rel.Version = updated.Version
	s.appendEvent(ctx, rel.ID, "approved",
		fmt.Sprintf("group %s #%d approved, executing", groupLabel(g), rel.ReleaseNumber), approverID)
	go s.executeRelease(context.Background(), rel, g, img, approverID)
	return nil
}
