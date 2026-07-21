package systemhttp

import (
	"time"

	"github.com/vortexops/vortexops/internal/domain/system"
)

type settingDTO struct {
	ID          int64  `json:"id"`
	Key         string `json:"key"`
	Value       any    `json:"value"`
	Description string `json:"description"`
	IsPublic    bool   `json:"is_public"`
	Version     int    `json:"version"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

func toSettingDTO(s *system.Setting) *settingDTO {
	if s == nil {
		return nil
	}
	return &settingDTO{
		ID: s.ID, Key: s.Key, Value: s.Value, Description: s.Description, IsPublic: s.IsPublic,
		Version: s.Version, CreatedAt: s.CreatedAt.Format(time.RFC3339), UpdatedAt: s.UpdatedAt.Format(time.RFC3339),
	}
}

func toSettingDTOs(items []*system.Setting) []settingDTO {
	out := make([]settingDTO, 0, len(items))
	for _, s := range items {
		out = append(out, *toSettingDTO(s))
	}
	return out
}
