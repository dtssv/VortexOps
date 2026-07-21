// Package auditapp 是审计日志领域的应用服务层。
// 编排：记录审计日志（同步写 PG，Phase 11 切 Kafka 异步）、查询审计日志。
package auditapp

import (
	"context"
	"time"

	"github.com/vortexops/vortexops/internal/domain/audit"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// Service 审计应用服务。
type Service struct {
	repo audit.Repository
}

// New 创建审计服务。
func New(repo audit.Repository) *Service {
	return &Service{repo: repo}
}

// RecordInput 记录审计日志输入。
type RecordInput struct {
	UserID          int64
	UserName        string
	WorkspaceID     int64
	ResourceType    string
	ResourceID      int64
	ResourceName    string
	Action          audit.Action
	Operation       string
	RequestID       string
	Method          string
	Path            string
	StatusCode      int
	ClientIP        string
	UserAgent       string
	RequestBody     map[string]any
	ResponseSummary map[string]any
	DurationMs      int
	ErrorMessage    string
}

// Record 记录一条审计日志（同步写库；大规模场景 Phase 11 切 Kafka 异步）。
func (s *Service) Record(ctx context.Context, in RecordInput) {
	log := &audit.Log{
		UserID: in.UserID, UserName: in.UserName, WorkspaceID: in.WorkspaceID,
		ResourceType: in.ResourceType, ResourceID: in.ResourceID, ResourceName: in.ResourceName,
		Action: in.Action, Operation: in.Operation, RequestID: in.RequestID, Method: in.Method, Path: in.Path,
		StatusCode: in.StatusCode, ClientIP: in.ClientIP, UserAgent: in.UserAgent,
		RequestBody: in.RequestBody, ResponseSummary: in.ResponseSummary, DurationMs: in.DurationMs,
		ErrorMessage: in.ErrorMessage, CreatedAt: time.Now(),
	}
	// 审计日志写入失败不应影响主流程，仅记录错误（日志层）。
	_ = s.repo.Append(ctx, log)
}

// GetByID 按 ID 查询审计日志。
func (s *Service) GetByID(ctx context.Context, id int64) (*audit.Log, error) {
	log, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, apperr.Internal("get audit log", err)
	}
	return log, nil
}

// List 分页查询审计日志。
func (s *Service) List(ctx context.Context, q audit.Query) ([]*audit.Log, int64, error) {
	items, total, err := s.repo.List(ctx, q)
	if err != nil {
		return nil, 0, apperr.Internal("list audit logs", err)
	}
	return items, total, nil
}
