package server

import (
	"context"
	"strconv"

	"github.com/vortexops/vortexops/internal/application/auditapp"
	"github.com/vortexops/vortexops/internal/domain/audit"
)

// bastionAuditRecorder 适配 bastionapp.AuditRecorder → auditapp.Record。
type bastionAuditRecorder struct {
	auditSvc *auditapp.Service
}

func (r *bastionAuditRecorder) Record(ctx context.Context, actorID int64, action, resourceType, resourceID string, detail map[string]any) error {
	r.auditSvc.Record(ctx, auditapp.RecordInput{
		UserID:       actorID,
		Action:       audit.Action(action),
		Operation:    action,
		ResourceType: resourceType,
		ResourceID:   parseInt64(resourceID),
		RequestBody:  detail,
	})
	return nil
}

func parseInt64(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}
