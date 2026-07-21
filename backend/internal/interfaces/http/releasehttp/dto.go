package releasehttp

import (
	"time"

	"github.com/vortexops/vortexops/internal/domain/release"
)

type releaseDTO struct {
	ID                    int64  `json:"id"`
	UUID                  string `json:"uuid"`
	GroupID               int64  `json:"group_id"`
	ReleaseNumber         int    `json:"release_number"`
	PreviousReleaseID     int64  `json:"previous_release_id"`
	ImageID               int64  `json:"image_id"`
	ConfigVersion         int    `json:"config_version"`
	ReleaseType           string `json:"release_type"`
	Replicas              int    `json:"replicas"`
	Strategy              string `json:"strategy"`
	MaxSurge              string `json:"max_surge,omitempty"`
	MaxUnavailable        string `json:"max_unavailable,omitempty"`
	BatchSize             int      `json:"batch_size,omitempty"`
	BatchIntervalSec      int      `json:"batch_interval_sec,omitempty"`
	TargetPercentage      int      `json:"target_percentage,omitempty"`
	TargetPodNames        []string `json:"target_pod_names,omitempty"`
	Status                string   `json:"status"`
	ProgressPercent       int    `json:"progress_percent"`
	FailureReason         string `json:"failure_reason,omitempty"`
	StartedAt             string `json:"started_at"`
	FinishedAt            string `json:"finished_at,omitempty"`
	DurationMs            int64  `json:"duration_ms"`
	TriggeredBy           int64  `json:"triggered_by"`
	TriggerSource         string `json:"trigger_source"`
	AutoRollbackOnFailure bool   `json:"auto_rollback_on_failure"`
	RollbackOfReleaseID   int64  `json:"rollback_of_release_id,omitempty"`
	Version               int    `json:"version"`
	CreatedAt             string `json:"created_at"`
}

func toReleaseDTO(r *release.Release) *releaseDTO {
	if r == nil {
		return nil
	}
	dto := &releaseDTO{
		ID: r.ID, UUID: r.UUID.String(), GroupID: r.GroupID, ReleaseNumber: r.ReleaseNumber,
		PreviousReleaseID: r.PreviousReleaseID, ImageID: r.ImageID, ConfigVersion: r.ConfigVersion,
		ReleaseType: string(r.ReleaseType), Replicas: r.Replicas, Strategy: string(r.Strategy),
		MaxSurge: r.MaxSurge, MaxUnavailable: r.MaxUnavailable, BatchSize: r.BatchSize,
		BatchIntervalSec: r.BatchIntervalSec, TargetPercentage: r.TargetPercentage, TargetPodNames: r.TargetPodNames,
		Status: string(r.Status), ProgressPercent: r.ProgressPercent,
		FailureReason: r.FailureReason, StartedAt: r.StartedAt.Format(time.RFC3339),
		DurationMs: r.DurationMs, TriggeredBy: r.TriggeredBy, TriggerSource: string(r.TriggerSource),
		AutoRollbackOnFailure: r.AutoRollbackOnFailure, RollbackOfReleaseID: r.RollbackOfReleaseID,
		Version: r.Version, CreatedAt: r.CreatedAt.Format(time.RFC3339),
	}
	if r.FinishedAt != nil {
		dto.FinishedAt = r.FinishedAt.Format(time.RFC3339)
	}
	return dto
}

func toReleaseDTOs(items []*release.Release) []releaseDTO {
	out := make([]releaseDTO, 0, len(items))
	for _, r := range items {
		out = append(out, *toReleaseDTO(r))
	}
	return out
}

type releaseEventDTO struct {
	ID           int64  `json:"id"`
	ReleaseID    int64  `json:"release_id"`
	Seq          int    `json:"seq"`
	EventType    string `json:"event_type"`
	Message      string `json:"message"`
	OperatorID   int64  `json:"operator_id"`
	OperatorName string `json:"operator_name,omitempty"`
	OccurredAt   string `json:"occurred_at"`
}

func toReleaseEventDTO(e *release.ReleaseEvent) *releaseEventDTO {
	if e == nil {
		return nil
	}
	return &releaseEventDTO{
		ID: e.ID, ReleaseID: e.ReleaseID, Seq: e.Seq, EventType: e.EventType, Message: e.Message,
		OperatorID: e.OperatorID, OperatorName: e.OperatorName, OccurredAt: e.OccurredAt.Format(time.RFC3339),
	}
}

func toReleaseEventDTOs(items []*release.ReleaseEvent) []releaseEventDTO {
	out := make([]releaseEventDTO, 0, len(items))
	for _, e := range items {
		out = append(out, *toReleaseEventDTO(e))
	}
	return out
}

type batchRecordDTO struct {
	ID         int64  `json:"id"`
	ReleaseID  int64  `json:"release_id"`
	BatchIndex int    `json:"batch_index"`
	Status     string `json:"status"`
	StartedAt  string `json:"started_at,omitempty"`
	FinishedAt string `json:"finished_at,omitempty"`
	Message    string `json:"message,omitempty"`
}

func toBatchRecordDTO(b *release.ReleaseBatchRecord) *batchRecordDTO {
	if b == nil {
		return nil
	}
	dto := &batchRecordDTO{
		ID: b.ID, ReleaseID: b.ReleaseID, BatchIndex: b.BatchIndex, Status: string(b.Status), Message: b.Message,
	}
	if b.StartedAt != nil {
		dto.StartedAt = b.StartedAt.Format(time.RFC3339)
	}
	if b.FinishedAt != nil {
		dto.FinishedAt = b.FinishedAt.Format(time.RFC3339)
	}
	return dto
}

func toBatchRecordDTOs(items []*release.ReleaseBatchRecord) []batchRecordDTO {
	out := make([]batchRecordDTO, 0, len(items))
	for _, b := range items {
		out = append(out, *toBatchRecordDTO(b))
	}
	return out
}

type presetDTO struct {
	ID                    int64  `json:"id"`
	UUID                  string `json:"uuid"`
	Scope                 string `json:"scope"`
	ScopeID               int64  `json:"scope_id"`
	Name                  string `json:"name"`
	Description           string `json:"description,omitempty"`
	Strategy              string `json:"strategy"`
	MaxSurge              string `json:"max_surge,omitempty"`
	MaxUnavailable        string `json:"max_unavailable,omitempty"`
	BatchSize             int    `json:"batch_size,omitempty"`
	BatchIntervalSec      int    `json:"batch_interval_sec,omitempty"`
	AutoRollbackOnFailure bool   `json:"auto_rollback_on_failure"`
	IsDefault             bool   `json:"is_default"`
	Version               int    `json:"version"`
	CreatedAt             string `json:"created_at"`
}

func toPresetDTO(p *release.ReleasePreset) *presetDTO {
	if p == nil {
		return nil
	}
	return &presetDTO{
		ID: p.ID, UUID: p.UUID.String(), Scope: string(p.Scope), ScopeID: p.ScopeID, Name: p.Name,
		Description: p.Description, Strategy: string(p.Strategy), MaxSurge: p.MaxSurge,
		MaxUnavailable: p.MaxUnavailable, BatchSize: p.BatchSize, BatchIntervalSec: p.BatchIntervalSec,
		AutoRollbackOnFailure: p.AutoRollbackOnFailure, IsDefault: p.IsDefault,
		Version: p.Version, CreatedAt: p.CreatedAt.Format(time.RFC3339),
	}
}

func toPresetDTOs(items []*release.ReleasePreset) []presetDTO {
	out := make([]presetDTO, 0, len(items))
	for _, p := range items {
		out = append(out, *toPresetDTO(p))
	}
	return out
}

type windowDTO struct {
	ID              int64  `json:"id"`
	UUID            string `json:"uuid"`
	ApplicationID   int64  `json:"application_id"`
	Name            string `json:"name"`
	Timezone        string `json:"timezone"`
	Crontab         string `json:"crontab"`
	DurationMinutes int    `json:"duration_minutes"`
	IsActive        bool   `json:"is_active"`
	Version         int    `json:"version"`
	CreatedAt       string `json:"created_at"`
}

func toWindowDTO(w *release.ReleaseWindow) *windowDTO {
	if w == nil {
		return nil
	}
	return &windowDTO{
		ID: w.ID, UUID: w.UUID.String(), ApplicationID: w.ApplicationID, Name: w.Name, Timezone: w.Timezone,
		Crontab: w.Crontab, DurationMinutes: w.DurationMinutes, IsActive: w.IsActive,
		Version: w.Version, CreatedAt: w.CreatedAt.Format(time.RFC3339),
	}
}

func toWindowDTOs(items []*release.ReleaseWindow) []windowDTO {
	out := make([]windowDTO, 0, len(items))
	for _, w := range items {
		out = append(out, *toWindowDTO(w))
	}
	return out
}
