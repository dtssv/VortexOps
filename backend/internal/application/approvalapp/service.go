// Package approvalapp 是审批领域的应用服务层。
// 负责创建审批、批准/拒绝、待审列表；审批通过后触发回调（如继续发布）。
package approvalapp

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/vortexops/vortexops/internal/domain"
	"github.com/vortexops/vortexops/internal/domain/approval"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// ApprovedCallback 审批通过后的回调（如继续执行被挂起的发布）。
// resourceType/resourceID 对应审批实体；回调应幂等。
type ApprovedCallback func(ctx context.Context, rt approval.ResourceType, resourceID int64, approverID int64) error

// Service 审批应用服务。
type Service struct {
	repo      approval.Repository
	callbacks map[approval.ResourceType]ApprovedCallback
}

// New 创建审批服务。
func New(repo approval.Repository) *Service {
	return &Service{repo: repo, callbacks: make(map[approval.ResourceType]ApprovedCallback)}
}

// RegisterCallback 注册某资源类型审批通过后的回调。
func (s *Service) RegisterCallback(rt approval.ResourceType, cb ApprovedCallback) {
	s.callbacks[rt] = cb
}

// CreateApproval 创建审批。
func (s *Service) CreateApproval(ctx context.Context, in approval.CreateInput) (*approval.Approval, error) {
	if in.WorkspaceID == 0 {
		return nil, apperr.Validation("workspace_id is required", nil)
	}
	if in.ResourceType == "" {
		return nil, apperr.Validation("resource_type is required", nil)
	}
	if in.Operation == "" {
		return nil, apperr.Validation("operation is required", nil)
	}
	// 同一资源已有 pending 审批则拒绝重复创建。
	if existing, _ := s.repo.GetPendingByResource(ctx, in.ResourceType, in.ResourceID); existing != nil {
		return nil, apperr.Conflict("an approval is already pending for this resource", approval.ErrAlreadyPending)
	}
	a := &approval.Approval{
		WorkspaceID:  in.WorkspaceID,
		ResourceType: in.ResourceType,
		ResourceID:   in.ResourceID,
		Operation:    in.Operation,
		RequestedBy:  in.RequestedBy,
		ApproverRole: in.ApproverRole,
		Comment:      in.Comment,
		ExpiresAt:    in.ExpiresAt,
		CreatedBy:    in.RequestedBy,
		UpdatedBy:    in.RequestedBy,
	}
	if err := s.repo.Create(ctx, a); err != nil {
		return nil, apperr.Internal("create approval", err)
	}
	return a, nil
}

// Approve 批准审批并触发回调。
func (s *Service) Approve(ctx context.Context, id, approverID int64, comment string) (*approval.Approval, error) {
	a, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, approval.ErrApprovalNotFound) {
			return nil, apperr.NotFound("approval", strconv.FormatInt(id, 10))
		}
		return nil, apperr.Internal("get approval", err)
	}
	if a.Status != approval.StatusPending {
		return nil, apperr.BusinessRule("approval is not pending", nil)
	}
	now := time.Now()
	a.Status = approval.StatusApproved
	a.ApproverID = approverID
	a.ApprovedAt = &now
	a.Comment = comment
	a.UpdatedBy = approverID
	if err := s.repo.Update(ctx, a); err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return nil, apperr.Conflict("approval was modified concurrently, please refresh", err)
		}
		return nil, apperr.Internal("approve approval", err)
	}
	// 触发回调（失败不回滚审批状态，仅记录）。
	if cb, ok := s.callbacks[a.ResourceType]; ok {
		_ = cb(context.Background(), a.ResourceType, a.ResourceID, approverID)
	}
	return a, nil
}

// Reject 拒绝审批。
func (s *Service) Reject(ctx context.Context, id, approverID int64, comment string) (*approval.Approval, error) {
	a, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, approval.ErrApprovalNotFound) {
			return nil, apperr.NotFound("approval", strconv.FormatInt(id, 10))
		}
		return nil, apperr.Internal("get approval", err)
	}
	if a.Status != approval.StatusPending {
		return nil, apperr.BusinessRule("approval is not pending", nil)
	}
	now := time.Now()
	a.Status = approval.StatusRejected
	a.ApproverID = approverID
	a.ApprovedAt = &now
	a.Comment = comment
	a.UpdatedBy = approverID
	if err := s.repo.Update(ctx, a); err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return nil, apperr.Conflict("approval was modified concurrently, please refresh", err)
		}
		return nil, apperr.Internal("reject approval", err)
	}
	return a, nil
}

// GetByID 查询审批。
func (s *Service) GetByID(ctx context.Context, id int64) (*approval.Approval, error) {
	a, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, approval.ErrApprovalNotFound) {
			return nil, apperr.NotFound("approval", strconv.FormatInt(id, 10))
		}
		return nil, apperr.Internal("get approval", err)
	}
	return a, nil
}

// List 分页查询审批。
func (s *Service) List(ctx context.Context, q approval.Query, page, size int) ([]*approval.Approval, int64, error) {
	q.Offset = (page - 1) * size
	q.Limit = size
	items, total, err := s.repo.List(ctx, q)
	if err != nil {
		return nil, 0, apperr.Internal("list approvals", err)
	}
	return items, total, nil
}
