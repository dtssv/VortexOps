package confighttp

import (
	"time"

	configdomain "github.com/vortexops/vortexops/internal/domain/config"
)

type configDTO struct {
	ID               int64  `json:"id"`
	UUID             string `json:"uuid"`
	Scope            string `json:"scope"`
	ScopeID          int64  `json:"scope_id"`
	GroupID          int64  `json:"group_id"`
	Name             string `json:"name"`
	ConfigType       string `json:"config_type"`
	ConfigVersion    int    `json:"config_version"`
	Description      string `json:"description,omitempty"`
	RenderedContent  string `json:"rendered_content,omitempty"`
	DiffWithPrevious string `json:"diff_with_previous,omitempty"`
	Checksum         string `json:"checksum"`
	Status           string `json:"status"`
	Version          int    `json:"version"`
	CreatedAt        string `json:"created_at"`
}

func toConfigDTO(c *configdomain.Config) *configDTO {
	if c == nil {
		return nil
	}
	return &configDTO{
		ID: c.ID, UUID: c.UUID.String(), Scope: string(c.Scope), ScopeID: c.ScopeID, GroupID: c.GroupID,
		Name: c.Name, ConfigType: string(c.ConfigType), ConfigVersion: c.ConfigVersion,
		Description: c.Description, RenderedContent: c.RenderedContent, DiffWithPrevious: c.DiffWithPrevious,
		Checksum: c.Checksum, Status: string(c.Status), Version: c.Version,
		CreatedAt: c.CreatedAt.Format(time.RFC3339),
	}
}

func toConfigDTOs(items []*configdomain.Config) []configDTO {
	out := make([]configDTO, 0, len(items))
	for _, c := range items {
		out = append(out, *toConfigDTO(c))
	}
	return out
}

type configSetDTO struct {
	ID          int64          `json:"id"`
	UUID        string         `json:"uuid"`
	WorkspaceID   int64          `json:"workspace_id"`
	ApplicationID int64          `json:"application_id,omitempty"`
	Name          string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Content     map[string]any `json:"content"`
	Version     int            `json:"version"`
	CreatedAt   string         `json:"created_at"`
}

func toConfigSetDTO(cs *configdomain.ConfigSet) *configSetDTO {
	if cs == nil {
		return nil
	}
	return &configSetDTO{
		ID: cs.ID, UUID: cs.UUID.String(), WorkspaceID: cs.WorkspaceID, ApplicationID: cs.ApplicationID, Name: cs.Name,
		Description: cs.Description, Content: cs.Content, Version: cs.Version,
		CreatedAt: cs.CreatedAt.Format(time.RFC3339),
	}
}

func toConfigSetDTOs(items []*configdomain.ConfigSet) []configSetDTO {
	out := make([]configSetDTO, 0, len(items))
	for _, cs := range items {
		out = append(out, *toConfigSetDTO(cs))
	}
	return out
}

type bindingDTO struct {
	ID            int64  `json:"id"`
	GroupID       int64  `json:"group_id"`
	ConfigID      int64  `json:"config_id"`
	ConfigSetID   int64  `json:"config_set_id"`
	Priority      int    `json:"priority"`
	PinnedVersion *int   `json:"pinned_version,omitempty"`
	MountPath     string `json:"mount_path,omitempty"`
	SubPath       string `json:"sub_path,omitempty"`
	CreatedAt     string `json:"created_at"`
}

func toBindingDTO(b *configdomain.GroupConfigBinding) *bindingDTO {
	if b == nil {
		return nil
	}
	return &bindingDTO{
		ID: b.ID, GroupID: b.GroupID, ConfigID: b.ConfigID, ConfigSetID: b.ConfigSetID,
		Priority: b.Priority, PinnedVersion: b.PinnedVersion,
		MountPath: b.MountPath, SubPath: b.SubPath,
		CreatedAt: b.CreatedAt.Format(time.RFC3339),
	}
}

func toBindingDTOs(items []*configdomain.GroupConfigBinding) []bindingDTO {
	out := make([]bindingDTO, 0, len(items))
	for _, b := range items {
		out = append(out, *toBindingDTO(b))
	}
	return out
}

type groupLocalConfigDTO struct {
	ID          int64          `json:"id"`
	UUID        string         `json:"uuid"`
	GroupID     int64          `json:"group_id"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Content     map[string]any `json:"content"`
	Version     int            `json:"version"`
	CreatedAt   string         `json:"created_at"`
	UpdatedAt   string         `json:"updated_at"`
}

func toGroupLocalConfigDTO(c *configdomain.GroupLocalConfig) *groupLocalConfigDTO {
	if c == nil {
		return nil
	}
	return &groupLocalConfigDTO{
		ID: c.ID, UUID: c.UUID.String(), GroupID: c.GroupID, Name: c.Name,
		Description: c.Description, Content: c.Content, Version: c.Version,
		CreatedAt: c.CreatedAt.Format(time.RFC3339),
		UpdatedAt: c.UpdatedAt.Format(time.RFC3339),
	}
}

type contentSnapshotDTO struct {
	ID           int64  `json:"id"`
	TargetType   string `json:"target_type"`
	TargetID     int64  `json:"target_id"`
	SnapshotNo   int    `json:"snapshot_no"`
	ChangeReason string `json:"change_reason"`
	FilesHash    string `json:"files_hash,omitempty"`
	FileCount    int    `json:"file_count"`
	CreatedAt    string `json:"created_at"`
}

func toSnapshotDTO(s *configdomain.ContentSnapshot) contentSnapshotDTO {
	fileCount := 0
	if s != nil && s.Content != nil {
		if files, ok := s.Content["files"].([]any); ok {
			fileCount = len(files)
		}
	}
	return contentSnapshotDTO{
		ID: s.ID, TargetType: string(s.TargetType), TargetID: s.TargetID,
		SnapshotNo: s.SnapshotNo, ChangeReason: s.ChangeReason, FilesHash: s.FilesHash,
		FileCount: fileCount, CreatedAt: s.CreatedAt.Format(time.RFC3339),
	}
}

func toSnapshotDTOs(items []*configdomain.ContentSnapshot) []contentSnapshotDTO {
	out := make([]contentSnapshotDTO, 0, len(items))
	for _, s := range items {
		out = append(out, toSnapshotDTO(s))
	}
	return out
}
