package releaseapp

import (
	"context"

	"github.com/vortexops/vortexops/internal/application/approvalapp"
	"github.com/vortexops/vortexops/internal/domain/approval"
	"github.com/vortexops/vortexops/internal/domain/application"
)

// ApplicationResolver 解析应用（取 workspace_id）。
type ApplicationResolver interface {
	Get(ctx context.Context, id int64) (*application.Application, error)
}

// GroupResolverFull 解析 group（含 ReleaseRequiresApproval）。
type GroupResolverFull interface {
	GetGroup(ctx context.Context, id int64) (*application.Group, error)
}

// ReleaseApprovalBridge 连接 releaseapp 与 approvalapp：
// - 实现 ApprovalChecker（查询 group 是否需要审批、创建审批）
// - 审批通过后调用 ExecutePendingRelease 恢复发布。
type ReleaseApprovalBridge struct {
	groupRepo GroupResolverFull
	appRepo   ApplicationResolver
	approval  *approvalapp.Service
}

// NewReleaseApprovalBridge 创建审批桥接器。
func NewReleaseApprovalBridge(groupRepo GroupResolverFull, appRepo ApplicationResolver, approvalSvc *approvalapp.Service) *ReleaseApprovalBridge {
	b := &ReleaseApprovalBridge{groupRepo: groupRepo, appRepo: appRepo, approval: approvalSvc}
	approvalSvc.RegisterCallback(approval.ResourceRelease, b.onApproved)
	return b
}

// RequireApproval 返回 group 是否需要发布审批。
func (b *ReleaseApprovalBridge) RequireApproval(ctx context.Context, groupID int64) (bool, error) {
	g, err := b.groupRepo.GetGroup(ctx, groupID)
	if err != nil {
		return false, err
	}
	return g.ReleaseRequiresApproval, nil
}

// CreateForRelease 为发布创建审批记录。
func (b *ReleaseApprovalBridge) CreateForRelease(ctx context.Context, workspaceID, groupID, releaseID int64, requestedBy int64) (int64, error) {
	if workspaceID == 0 {
		// 从 group.application 解析 workspace_id。
		g, err := b.groupRepo.GetGroup(ctx, groupID)
		if err == nil {
			if app, aerr := b.appRepo.Get(ctx, g.ApplicationID); aerr == nil {
				workspaceID = app.WorkspaceID
			}
		}
	}
	a, err := b.approval.CreateApproval(ctx, approval.CreateInput{
		WorkspaceID:  workspaceID,
		ResourceType: approval.ResourceRelease,
		ResourceID:   releaseID,
		Operation:    "release",
		RequestedBy:  requestedBy,
	})
	if err != nil {
		return 0, err
	}
	return a.ID, nil
}

// onApproved 审批通过回调：执行挂起的发布。
func (b *ReleaseApprovalBridge) onApproved(ctx context.Context, rt approval.ResourceType, resourceID int64, approverID int64) error {
	if rt != approval.ResourceRelease {
		return nil
	}
	// 通过 ExecutePendingReleaseFn 间接调用 releaseapp（避免循环依赖）。
	if executePendingRelease != nil {
		return executePendingRelease(ctx, resourceID, approverID)
	}
	return nil
}

// executePendingReleaseFn 由 server.go 注入（避免 releaseapp → approvalapp → releaseapp 循环）。
var executePendingRelease func(ctx context.Context, releaseID, approverID int64) error

// SetExecutePendingReleaseFn 注入审批通过后执行挂起发布的函数。
func SetExecutePendingReleaseFn(fn func(ctx context.Context, releaseID, approverID int64) error) {
	executePendingRelease = fn
}
