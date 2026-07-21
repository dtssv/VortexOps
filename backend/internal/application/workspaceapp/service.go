// Package workspaceapp 是空间领域的应用服务层。
// 编排空间实体、成员、配额与集群绑定，执行配额校验与业务规则。
package workspaceapp

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/vortexops/vortexops/internal/domain"
	"github.com/vortexops/vortexops/internal/domain/workspace"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// Service 空间应用服务。
type Service struct {
	repo workspace.Repository
}

// New 创建空间服务。
func New(repo workspace.Repository) *Service {
	return &Service{repo: repo}
}

// CreateInput 创建空间请求。
type CreateInput struct {
	Name              string
	DisplayName       string
	Description       string
	LogoURL           string
	OwnerID           int64
	DefaultRegistryID int64
	DefaultJenkinsID  int64
	Labels            map[string]string
	Metadata          map[string]any
	MaxApplications   int
	MaxGroups         int
	MaxMembers        int
}

// Create 创建空间，自动建立默认配额并把创建者设为 owner 成员。
func (s *Service) Create(ctx context.Context, in CreateInput) (*workspace.Workspace, error) {
	if err := validateName(in.Name); err != nil {
		return nil, err
	}
	if in.OwnerID == 0 {
		return nil, apperr.Validation("owner is required", nil)
	}
	// 名称唯一性预检（友好错误）。
	if _, err := s.repo.GetByName(ctx, in.Name); err == nil {
		return nil, apperr.Conflict("workspace name already exists", workspace.ErrWorkspaceNameExists)
	} else if !errors.Is(err, workspace.ErrWorkspaceNotFound) {
		return nil, apperr.Internal("check workspace name", err)
	}

	quota := &workspace.Quota{
		MaxApplications:     orDefaultInt(in.MaxApplications, 50),
		MaxGroups:           orDefaultInt(in.MaxGroups, 200),
		MaxConcurrentBuilds: 10,
		MaxImagesRetained:   100,
		MaxMembers:          orDefaultInt(in.MaxMembers, 100),
	}

	w := &workspace.Workspace{
		Name:              in.Name,
		DisplayName:       in.DisplayName,
		Description:       in.Description,
		LogoURL:           in.LogoURL,
		Status:            workspace.StatusActive,
		OwnerID:           in.OwnerID,
		DefaultRegistryID: in.DefaultRegistryID,
		DefaultJenkinsID:  in.DefaultJenkinsID,
		Labels:            in.Labels,
		Metadata:          in.Metadata,
	}
	w.CreatedBy = in.OwnerID
	w.UpdatedBy = in.OwnerID

	if err := s.repo.Create(ctx, w, quota); err != nil {
		if errors.Is(err, workspace.ErrWorkspaceNameExists) {
			return nil, apperr.Conflict("workspace name already exists", err)
		}
		return nil, apperr.Internal("create workspace", err)
	}
	return w, nil
}

// Get 按 ID 获取空间。
func (s *Service) Get(ctx context.Context, id int64) (*workspace.Workspace, error) {
	w, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, workspace.ErrWorkspaceNotFound) {
			return nil, apperr.NotFound("workspace", stringifyID(id))
		}
		return nil, apperr.Internal("get workspace", err)
	}
	return w, nil
}

// GetByUUID 按 UUID 获取空间。
func (s *Service) GetByUUID(ctx context.Context, id uuid.UUID) (*workspace.Workspace, error) {
	w, err := s.repo.GetByUUID(ctx, id)
	if err != nil {
		if errors.Is(err, workspace.ErrWorkspaceNotFound) {
			return nil, apperr.NotFound("workspace", id.String())
		}
		return nil, apperr.Internal("get workspace", err)
	}
	return w, nil
}

// UpdateInput 更新空间请求。
type UpdateInput struct {
	ID                int64
	DisplayName       *string
	Description       *string
	LogoURL           *string
	Status            *workspace.Status
	DefaultRegistryID *int64
	DefaultJenkinsID  *int64
	Labels            *map[string]string
	Metadata          *map[string]any
	Version           int
	ActorID           int64
}

// Update 更新空间（需 owner，乐观锁）。
func (s *Service) Update(ctx context.Context, in UpdateInput) (*workspace.Workspace, error) {
	w, err := s.repo.GetByID(ctx, in.ID)
	if err != nil {
		if errors.Is(err, workspace.ErrWorkspaceNotFound) {
			return nil, apperr.NotFound("workspace", stringifyID(in.ID))
		}
		return nil, apperr.Internal("get workspace", err)
	}
	if !w.IsOwner(in.ActorID) {
		return nil, apperr.Forbidden("only workspace owner can update", workspace.ErrNotOwner)
	}
	updated, err := s.repo.Update(ctx, workspace.UpdateInput{
		ID:                in.ID,
		DisplayName:       in.DisplayName,
		Description:       in.Description,
		LogoURL:           in.LogoURL,
		Status:            in.Status,
		DefaultRegistryID: in.DefaultRegistryID,
		DefaultJenkinsID:  in.DefaultJenkinsID,
		Labels:            in.Labels,
		Metadata:          in.Metadata,
		Version:           in.Version,
		UpdatedBy:         in.ActorID,
	})
	if err != nil {
		return nil, mapUpdateErr(err, "workspace", in.ID)
	}
	return updated, nil
}

// List 分页列出空间。
func (s *Service) List(ctx context.Context, ownerID int64, status workspace.Status, search string, page, size int) ([]*workspace.Workspace, int64, error) {
	items, total, err := s.repo.List(ctx, workspace.Query{
		OwnerID: ownerID, Status: status, Search: search,
		Offset: (page - 1) * size, Limit: size,
	})
	if err != nil {
		return nil, 0, apperr.Internal("list workspaces", err)
	}
	return items, total, nil
}

// Delete 软删除空间（仅 owner）。
// 关联校验：空间下存在应用或集群绑定时禁止删除，避免悬挂引用。
func (s *Service) Delete(ctx context.Context, id, actorID int64) error {
	w, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, workspace.ErrWorkspaceNotFound) {
			return apperr.NotFound("workspace", stringifyID(id))
		}
		return apperr.Internal("get workspace", err)
	}
	if !w.IsOwner(actorID) {
		return apperr.Forbidden("only workspace owner can delete", workspace.ErrNotOwner)
	}
	appCount, err := s.repo.CountApplications(ctx, id)
	if err != nil {
		return apperr.Internal("count applications before delete", err)
	}
	if appCount > 0 {
		return apperr.BusinessRule(
			fmt.Sprintf("workspace has %d application(s); remove them before deleting the workspace", appCount),
			workspace.ErrWorkspaceNotEmpty,
		)
	}
	bindings, err := s.repo.ListClusterBindings(ctx, id)
	if err != nil {
		return apperr.Internal("list cluster bindings before delete", err)
	}
	if len(bindings) > 0 {
		return apperr.BusinessRule(
			fmt.Sprintf("workspace has %d cluster binding(s); unbind them before deleting the workspace", len(bindings)),
			workspace.ErrWorkspaceNotEmpty,
		)
	}
	if err := s.repo.Delete(ctx, id, actorID); err != nil {
		if errors.Is(err, workspace.ErrWorkspaceNotFound) {
			return apperr.NotFound("workspace", stringifyID(id))
		}
		return apperr.Internal("delete workspace", err)
	}
	return nil
}

// GetQuota 获取空间配额。
func (s *Service) GetQuota(ctx context.Context, workspaceID int64) (*workspace.Quota, error) {
	q, err := s.repo.GetQuota(ctx, workspaceID)
	if err != nil {
		if errors.Is(err, workspace.ErrWorkspaceNotFound) {
			return nil, apperr.NotFound("workspace quota", stringifyID(workspaceID))
		}
		return nil, apperr.Internal("get quota", err)
	}
	return q, nil
}

// UpdateQuotaInput 更新配额请求。
type UpdateQuotaInput struct {
	WorkspaceID int64
	Quota       workspace.Quota
	Version     int
	ActorID     int64
}

// UpdateQuota 更新空间配额（仅 owner，乐观锁）。
func (s *Service) UpdateQuota(ctx context.Context, in UpdateQuotaInput) error {
	w, err := s.repo.GetByID(ctx, in.WorkspaceID)
	if err != nil {
		if errors.Is(err, workspace.ErrWorkspaceNotFound) {
			return apperr.NotFound("workspace", stringifyID(in.WorkspaceID))
		}
		return apperr.Internal("get workspace", err)
	}
	if !w.IsOwner(in.ActorID) {
		return apperr.Forbidden("only workspace owner can update quota", workspace.ErrNotOwner)
	}
	if err := validateQuota(in.Quota); err != nil {
		return err
	}
	if err := s.repo.UpdateQuota(ctx, in.WorkspaceID, in.Quota, in.Version, in.ActorID); err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return apperr.Conflict("quota was modified concurrently, please refresh", err)
		}
		return apperr.Internal("update quota", err)
	}
	return nil
}

// AddMemberInput 添加成员请求。
type AddMemberInput struct {
	WorkspaceID int64
	UserID      int64
	RoleID      int64
	ActorID     int64
}

// AddMember 添加成员（仅 owner，校验成员配额）。
func (s *Service) AddMember(ctx context.Context, in AddMemberInput) (*workspace.Member, error) {
	w, err := s.repo.GetByID(ctx, in.WorkspaceID)
	if err != nil {
		if errors.Is(err, workspace.ErrWorkspaceNotFound) {
			return nil, apperr.NotFound("workspace", stringifyID(in.WorkspaceID))
		}
		return nil, apperr.Internal("get workspace", err)
	}
	if !w.IsOwner(in.ActorID) {
		return nil, apperr.Forbidden("only workspace owner can add members", workspace.ErrNotOwner)
	}
	if in.UserID == 0 || in.RoleID == 0 {
		return nil, apperr.Validation("user_id and role_id are required", nil)
	}
	if in.UserID == in.ActorID {
		return nil, apperr.Validation("owner is already a member", nil)
	}
	quota, err := s.repo.GetQuota(ctx, in.WorkspaceID)
	if err != nil {
		return nil, apperr.Internal("get quota", err)
	}
	count, err := s.repo.CountMembers(ctx, in.WorkspaceID)
	if err != nil {
		return nil, apperr.Internal("count members", err)
	}
	if count >= int64(quota.MaxMembers) {
		return nil, apperr.BusinessRule("workspace member quota exceeded", workspace.ErrQuotaExceeded)
	}
	m := &workspace.Member{
		WorkspaceID: in.WorkspaceID,
		UserID:      in.UserID,
		RoleID:      in.RoleID,
		InvitedBy:   in.ActorID,
		Status:      workspace.MemberStatusActive,
	}
	m.CreatedBy = in.ActorID
	if err := s.repo.AddMember(ctx, m); err != nil {
		if errors.Is(err, workspace.ErrMemberExists) {
			return nil, apperr.Conflict("member already exists in workspace", err)
		}
		return nil, apperr.Internal("add member", err)
	}
	return m, nil
}

// ListMembers 分页列出成员。
func (s *Service) ListMembers(ctx context.Context, workspaceID int64, page, size int) ([]*workspace.Member, int64, error) {
	items, total, err := s.repo.ListMembers(ctx, workspace.MemberQuery{
		WorkspaceID: workspaceID, Status: workspace.MemberStatusActive,
		Offset: (page - 1) * size, Limit: size,
	})
	if err != nil {
		return nil, 0, apperr.Internal("list members", err)
	}
	return items, total, nil
}

// UpdateMemberRole 更新成员角色（仅 owner）。
func (s *Service) UpdateMemberRole(ctx context.Context, workspaceID, userID, roleID, actorID int64) error {
	w, err := s.repo.GetByID(ctx, workspaceID)
	if err != nil {
		if errors.Is(err, workspace.ErrWorkspaceNotFound) {
			return apperr.NotFound("workspace", stringifyID(workspaceID))
		}
		return apperr.Internal("get workspace", err)
	}
	if !w.IsOwner(actorID) {
		return apperr.Forbidden("only workspace owner can update member roles", workspace.ErrNotOwner)
	}
	if userID == actorID {
		return apperr.Validation("cannot change own role", nil)
	}
	if err := s.repo.UpdateMemberRole(ctx, workspaceID, userID, roleID, actorID); err != nil {
		if errors.Is(err, workspace.ErrMemberNotFound) {
			return apperr.NotFound("member", stringifyID(userID))
		}
		return apperr.Internal("update member role", err)
	}
	return nil
}

// RemoveMember 移除成员（仅 owner，不能移除自己）。
func (s *Service) RemoveMember(ctx context.Context, workspaceID, userID, actorID int64) error {
	w, err := s.repo.GetByID(ctx, workspaceID)
	if err != nil {
		if errors.Is(err, workspace.ErrWorkspaceNotFound) {
			return apperr.NotFound("workspace", stringifyID(workspaceID))
		}
		return apperr.Internal("get workspace", err)
	}
	if !w.IsOwner(actorID) {
		return apperr.Forbidden("only workspace owner can remove members", workspace.ErrNotOwner)
	}
	if userID == actorID {
		return apperr.Validation("owner cannot remove themselves; transfer ownership first", nil)
	}
	if err := s.repo.RemoveMember(ctx, workspaceID, userID, actorID); err != nil {
		if errors.Is(err, workspace.ErrMemberNotFound) {
			return apperr.NotFound("member", stringifyID(userID))
		}
		return apperr.Internal("remove member", err)
	}
	return nil
}

// AddClusterBindingInput 绑定集群请求。
type AddClusterBindingInput struct {
	WorkspaceID  int64
	ClusterID    int64
	Namespace    string
	Role         workspace.ClusterRole
	AutoCreateNS bool
	ResourceQuota map[string]any
	ActorID      int64
}

// AddClusterBinding 绑定集群到空间（仅 owner）。
func (s *Service) AddClusterBinding(ctx context.Context, in AddClusterBindingInput) (*workspace.ClusterBinding, error) {
	w, err := s.repo.GetByID(ctx, in.WorkspaceID)
	if err != nil {
		if errors.Is(err, workspace.ErrWorkspaceNotFound) {
			return nil, apperr.NotFound("workspace", stringifyID(in.WorkspaceID))
		}
		return nil, apperr.Internal("get workspace", err)
	}
	if !w.IsOwner(in.ActorID) {
		return nil, apperr.Forbidden("only workspace owner can bind clusters", workspace.ErrNotOwner)
	}
	if in.ClusterID == 0 {
		return nil, apperr.Validation("cluster_id is required", nil)
	}
	if in.Namespace == "" {
		return nil, apperr.Validation("namespace is required", nil)
	}
	if in.Role == "" {
		in.Role = workspace.ClusterRolePrimary
	}
	b := &workspace.ClusterBinding{
		WorkspaceID:   in.WorkspaceID,
		ClusterID:     in.ClusterID,
		Namespace:     in.Namespace,
		Role:          in.Role,
		AutoCreateNS:  in.AutoCreateNS,
		ResourceQuota: in.ResourceQuota,
	}
	b.CreatedBy = in.ActorID
	if err := s.repo.AddClusterBinding(ctx, b); err != nil {
		if errors.Is(err, domain.ErrAlreadyExists) {
			return nil, apperr.Conflict("cluster already bound to workspace", err)
		}
		return nil, apperr.Internal("add cluster binding", err)
	}
	return b, nil
}

// ListClusterBindings 列出空间绑定的集群。
func (s *Service) ListClusterBindings(ctx context.Context, workspaceID int64) ([]*workspace.ClusterBinding, error) {
	items, err := s.repo.ListClusterBindings(ctx, workspaceID)
	if err != nil {
		return nil, apperr.Internal("list cluster bindings", err)
	}
	return items, nil
}

// RemoveClusterBinding 解绑集群（仅 owner）。
func (s *Service) RemoveClusterBinding(ctx context.Context, workspaceID, clusterID, actorID int64) error {
	w, err := s.repo.GetByID(ctx, workspaceID)
	if err != nil {
		if errors.Is(err, workspace.ErrWorkspaceNotFound) {
			return apperr.NotFound("workspace", stringifyID(workspaceID))
		}
		return apperr.Internal("get workspace", err)
	}
	if !w.IsOwner(actorID) {
		return apperr.Forbidden("only workspace owner can unbind clusters", workspace.ErrNotOwner)
	}
	if err := s.repo.RemoveClusterBinding(ctx, workspaceID, clusterID, actorID); err != nil {
		if errors.Is(err, workspace.ErrClusterBindingNotFound) {
			return apperr.NotFound("cluster binding", stringifyID(clusterID))
		}
		return apperr.Internal("remove cluster binding", err)
	}
	return nil
}

// --- 校验 ---

func validateName(name string) error {
	name = strings.TrimSpace(name)
	if len(name) < 2 || len(name) > 64 {
		return apperr.Validation("workspace name must be 2-64 characters", nil)
	}
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			return apperr.Validation("workspace name may only contain letters, digits, '-', '_'", nil)
		}
	}
	return nil
}

func validateQuota(q workspace.Quota) error {
	if q.MaxApplications < 0 || q.MaxGroups < 0 || q.MaxConcurrentBuilds < 0 || q.MaxImagesRetained < 0 || q.MaxMembers < 0 {
		return apperr.Validation("quota values must be non-negative", nil)
	}
	if q.MaxGroups < q.MaxApplications && q.MaxApplications > 0 {
		return apperr.Validation("max_groups must be >= max_applications", nil)
	}
	return nil
}

func mapUpdateErr(err error, resource string, id int64) error {
	if errors.Is(err, domain.ErrConflict) {
		return apperr.Conflict("resource was modified concurrently, please refresh", err)
	}
	if errors.Is(err, workspace.ErrWorkspaceNotFound) {
		return apperr.NotFound(resource, stringifyID(id))
	}
	return apperr.Internal("update "+resource, err)
}

func stringifyID(id int64) string {
	return strconv.FormatInt(id, 10)
}

func orDefaultInt(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}
