package userprofilehttp

import (
	"time"

	"github.com/vortexops/vortexops/internal/domain/userprofile"
)

type profileDTO struct {
	ID                 int64      `json:"id"`
	UUID               string     `json:"uuid"`
	UserID             int64      `json:"user_id"`
	ExpertiseLevel     string     `json:"expertise_level"`
	Roles              []string   `json:"roles"`
	Domains            []string   `json:"domains"`
	CommunicationStyle string     `json:"communication_style"`
	PreferredLanguage  string     `json:"preferred_language"`
	Summary            string     `json:"summary"`
	InteractionCount   int        `json:"interaction_count"`
	LastUpdatedAt      *time.Time `json:"last_updated_at,omitempty"`
}

func toProfileDTO(p *userprofile.Profile) profileDTO {
	return profileDTO{
		ID: p.ID, UUID: p.UUID, UserID: p.UserID,
		ExpertiseLevel:     p.ExpertiseLevel,
		Roles:              p.Roles,
		Domains:            p.Domains,
		CommunicationStyle: p.CommunicationStyle,
		PreferredLanguage:  p.PreferredLanguage,
		Summary:            p.Summary,
		InteractionCount:   p.InteractionCount,
		LastUpdatedAt:      p.LastUpdatedAt,
	}
}
