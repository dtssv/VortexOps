package buildhttp

import (
	"time"

	"github.com/vortexops/vortexops/internal/application/buildapp"
	"github.com/vortexops/vortexops/internal/domain/build"
)

type gitSourceDTO struct {
	ID             int64  `json:"id"`
	UUID           string `json:"uuid"`
	ApplicationID  int64  `json:"application_id"`
	Name           string `json:"name"`
	Provider       string `json:"provider"`
	RepoURL        string `json:"repo_url"`
	DefaultBranch  string `json:"default_branch"`
	CredentialID   int64  `json:"credential_id"`
	WebhookEnabled bool   `json:"webhook_enabled"`
	LastSyncedAt   string `json:"last_synced_at,omitempty"`
	Version        int    `json:"version"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

func toGitSourceDTO(g *build.GitSource) *gitSourceDTO {
	if g == nil {
		return nil
	}
	dto := &gitSourceDTO{
		ID: g.ID, UUID: g.UUID.String(), ApplicationID: g.ApplicationID, Name: g.Name,
		Provider: string(g.Provider), RepoURL: g.RepoURL, DefaultBranch: g.DefaultBranch,
		CredentialID: g.CredentialID, WebhookEnabled: g.WebhookEnabled,
		Version: g.Version, CreatedAt: g.CreatedAt.Format(time.RFC3339), UpdatedAt: g.UpdatedAt.Format(time.RFC3339),
	}
	if g.LastSyncedAt != nil {
		dto.LastSyncedAt = g.LastSyncedAt.Format(time.RFC3339)
	}
	return dto
}

func toGitSourceDTOs(items []*build.GitSource) []gitSourceDTO {
	out := make([]gitSourceDTO, 0, len(items))
	for _, g := range items {
		out = append(out, *toGitSourceDTO(g))
	}
	return out
}

type buildDTO struct {
	ID                int64            `json:"id"`
	UUID              string           `json:"uuid"`
	ApplicationID     int64            `json:"application_id"`
	BuildNumber       int              `json:"build_number"`
	GitSourceID       int64            `json:"git_source_id"`
	RefType           string           `json:"ref_type"`
	RefValue          string           `json:"ref_value"`
	// Branch 为 RefValue 的兼容别名（ref_type=branch 时即分支名），便于前端直接展示。
	Branch            string           `json:"branch"`
	CommitSHA         string           `json:"commit_sha"`
	CommitMessage     string           `json:"commit_message"`
	BuildTemplateID   int64            `json:"build_template_id"`
	BuildStrategy     string           `json:"build_strategy"`
	BuildCommand      string           `json:"build_command"`
	ContextPath       string           `json:"context_path"`
	ArtifactPath      string           `json:"artifact_path,omitempty"`
	DockerfilePath    string           `json:"dockerfile_path,omitempty"`
	BaseImageID       int64            `json:"base_image_id"`
	BuildTool         string           `json:"build_tool,omitempty"`
	BuilderImage      string           `json:"builder_image,omitempty"`
	DockerfileSource  string           `json:"dockerfile_source"`
	DockerfileContent string           `json:"dockerfile_content,omitempty"`
	BuildArgs         map[string]string `json:"build_args"`
	TargetRegistryID  int64            `json:"target_registry_id"`
	TargetRepository  string           `json:"target_repository"`
	TargetTag         string           `json:"target_tag"`
	OutputImageID     int64            `json:"output_image_id"`
	JenkinsInstanceID int64            `json:"jenkins_instance_id"`
	JenkinsBuildNumber int             `json:"jenkins_build_number"`
	JenkinsJobName    string           `json:"jenkins_job_name"`
	Status            string           `json:"status"`
	ProgressPercent   int              `json:"progress_percent"`
	CurrentStep       string           `json:"current_step"`
	DurationMs        int64            `json:"duration_ms"`
	StartedAt         string           `json:"started_at,omitempty"`
	FinishedAt        string           `json:"finished_at,omitempty"`
	LogExcerpt        string           `json:"log_excerpt,omitempty"`
	FailureReason     string           `json:"failure_reason,omitempty"`
	TriggeredBy       int64            `json:"triggered_by"`
	TriggerSource     string           `json:"trigger_source"`
	IdempotencyKey    string           `json:"idempotency_key,omitempty"`
	Metadata          map[string]any   `json:"metadata"`
	Version           int              `json:"version"`
	CreatedAt         string           `json:"created_at"`
	UpdatedAt         string           `json:"updated_at"`
}

func toBuildDTO(b *build.Build) *buildDTO {
	if b == nil {
		return nil
	}
	dto := &buildDTO{
		ID: b.ID, UUID: b.UUID.String(), ApplicationID: b.ApplicationID, BuildNumber: b.BuildNumber,
		GitSourceID: b.GitSourceID, RefType: string(b.RefType), RefValue: b.RefValue, Branch: b.RefValue, CommitSHA: b.CommitSHA,
		CommitMessage: b.CommitMessage, BuildTemplateID: b.BuildTemplateID, BuildStrategy: string(b.BuildStrategy),
		BuildCommand: b.BuildCommand, ContextPath: b.ContextPath, ArtifactPath: b.ArtifactPath,
		DockerfilePath: b.DockerfilePath, BaseImageID: b.BaseImageID,
		BuildTool: b.BuildTool, BuilderImage: b.BuilderImage,
		DockerfileSource: string(b.DockerfileSource), DockerfileContent: b.DockerfileContent, BuildArgs: b.BuildArgs,
		TargetRegistryID: b.TargetRegistryID, TargetRepository: b.TargetRepository, TargetTag: b.TargetTag,
		OutputImageID: b.OutputImageID, JenkinsInstanceID: b.JenkinsInstanceID,
		JenkinsBuildNumber: b.JenkinsBuildNumber, JenkinsJobName: b.JenkinsJobName,
		Status: string(b.Status), ProgressPercent: b.ProgressPercent, CurrentStep: b.CurrentStep,
		DurationMs: b.DurationMs, LogExcerpt: b.LogExcerpt, FailureReason: b.FailureReason,
		TriggeredBy: b.TriggeredBy, TriggerSource: string(b.TriggerSource),
		IdempotencyKey: b.IdempotencyKey, Metadata: b.Metadata, Version: b.Version,
		CreatedAt: b.CreatedAt.Format(time.RFC3339), UpdatedAt: b.UpdatedAt.Format(time.RFC3339),
	}
	if b.StartedAt != nil {
		dto.StartedAt = b.StartedAt.Format(time.RFC3339)
	}
	if b.FinishedAt != nil {
		dto.FinishedAt = b.FinishedAt.Format(time.RFC3339)
	}
	return dto
}

func toBuildDTOs(items []*build.Build) []buildDTO {
	out := make([]buildDTO, 0, len(items))
	for _, b := range items {
		out = append(out, *toBuildDTO(b))
	}
	return out
}

type buildStepDTO struct {
	ID             int64  `json:"id"`
	BuildID        int64  `json:"build_id"`
	Seq            int    `json:"seq"`
	Name           string `json:"name"`
	Status         string `json:"status"`
	StartedAt      string `json:"started_at,omitempty"`
	FinishedAt     string `json:"finished_at,omitempty"`
	DurationMs     int64  `json:"duration_ms"`
	Message        string `json:"message"`
	LogSizeBytes   int64  `json:"log_size_bytes"`
	ErrorLine      string `json:"error_line,omitempty"`
}

func toBuildStepDTO(s *build.BuildStep) *buildStepDTO {
	if s == nil {
		return nil
	}
	dto := &buildStepDTO{
		ID: s.ID, BuildID: s.BuildID, Seq: s.Seq, Name: s.Name, Status: string(s.Status),
		DurationMs: s.DurationMs, Message: s.Message, LogSizeBytes: s.LogSizeBytes, ErrorLine: s.ErrorLine,
	}
	if s.StartedAt != nil {
		dto.StartedAt = s.StartedAt.Format(time.RFC3339)
	}
	if s.FinishedAt != nil {
		dto.FinishedAt = s.FinishedAt.Format(time.RFC3339)
	}
	return dto
}

func toBuildStepDTOs(items []*build.BuildStep) []buildStepDTO {
	out := make([]buildStepDTO, 0, len(items))
	for _, s := range items {
		out = append(out, *toBuildStepDTO(s))
	}
	return out
}

type registryDTO struct {
	ID           int64  `json:"id"`
	UUID         string `json:"uuid"`
	Name         string `json:"name"`
	Type         string `json:"type"`
	URL          string `json:"url"`
	CredentialID int64  `json:"credential_id"`
	IsDefault    bool   `json:"is_default"`
	Status       string `json:"status"`
	Version      int    `json:"version"`
	CreatedAt    string `json:"created_at"`
}

func toRegistryDTO(r *build.Registry) *registryDTO {
	if r == nil {
		return nil
	}
	return &registryDTO{
		ID: r.ID, UUID: r.UUID.String(), Name: r.Name, Type: string(r.Type), URL: r.URL,
		CredentialID: r.CredentialID, IsDefault: r.IsDefault, Status: string(r.Status),
		Version: r.Version, CreatedAt: r.CreatedAt.Format(time.RFC3339),
	}
}

func toRegistryDTOs(items []*build.Registry) []registryDTO {
	out := make([]registryDTO, 0, len(items))
	for _, r := range items {
		out = append(out, *toRegistryDTO(r))
	}
	return out
}

type jenkinsDTO struct {
	ID               int64  `json:"id"`
	UUID             string `json:"uuid"`
	Name             string `json:"name"`
	URL              string `json:"url"`
	CredentialID     int64  `json:"credential_id"`
	DefaultJobFolder string `json:"default_job_folder"`
	IsDefault        bool   `json:"is_default"`
	Status           string `json:"status"`
	Version          int    `json:"version"`
	CreatedAt        string `json:"created_at"`
}

func toJenkinsDTO(j *build.JenkinsInstance) *jenkinsDTO {
	if j == nil {
		return nil
	}
	return &jenkinsDTO{
		ID: j.ID, UUID: j.UUID.String(), Name: j.Name, URL: j.URL, CredentialID: j.CredentialID,
		DefaultJobFolder: j.DefaultJobFolder, IsDefault: j.IsDefault, Status: string(j.Status),
		Version: j.Version, CreatedAt: j.CreatedAt.Format(time.RFC3339),
	}
}

func toJenkinsDTOs(items []*build.JenkinsInstance) []jenkinsDTO {
	out := make([]jenkinsDTO, 0, len(items))
	for _, j := range items {
		out = append(out, *toJenkinsDTO(j))
	}
	return out
}

// buildIntegrationDTO 构建集成配置（系统变量化默认 Jenkins + Registry）。
type buildIntegrationDTO struct {
	Jenkins  *jenkinsDTO  `json:"jenkins"`
	Registry *registryDTO `json:"registry"`
}

func toBuildIntegrationDTO(in *buildapp.BuildIntegration) *buildIntegrationDTO {
	if in == nil {
		return nil
	}
	return &buildIntegrationDTO{
		Jenkins:  toJenkinsDTO(in.Jenkins),
		Registry: toRegistryDTO(in.Registry),
	}
}

type baseImageDTO struct {
	ID                  int64             `json:"id"`
	UUID                string            `json:"uuid"`
	Name                string            `json:"name"`
	Runtime             string            `json:"runtime"`
	Registry            string            `json:"registry"`
	ImageRef            string            `json:"image_ref"`
	Digest              string            `json:"digest"`
	IsSystem            bool              `json:"is_system"`
	IsRecommended       bool              `json:"is_recommended"`
	Description         string            `json:"description"`
	DockerfileTemplate  string            `json:"dockerfile_template,omitempty"`
	BuildTool           string            `json:"build_tool,omitempty"`
	DefaultBuildCommand string            `json:"default_build_command,omitempty"`
	DefaultArtifactPath string            `json:"default_artifact_path,omitempty"`
	DefaultBuildArgs    map[string]string `json:"default_build_args,omitempty"`
	Entrypoint          []string          `json:"entrypoint,omitempty"`
	IsWeb               bool              `json:"is_web"`
	Version             int               `json:"version"`
	CreatedAt           string            `json:"created_at"`
}

func toBaseImageDTO(b *build.BaseImage) *baseImageDTO {
	if b == nil {
		return nil
	}
	return &baseImageDTO{
		ID: b.ID, UUID: b.UUID.String(), Name: b.Name, Runtime: string(b.Runtime), Registry: b.Registry,
		ImageRef: b.ImageRef, Digest: b.Digest, IsSystem: b.IsSystem, IsRecommended: b.IsRecommended,
		Description: b.Description, DockerfileTemplate: b.DockerfileTemplate,
		BuildTool: b.BuildTool, DefaultBuildCommand: b.DefaultBuildCommand,
		DefaultArtifactPath: b.DefaultArtifactPath, DefaultBuildArgs: b.DefaultBuildArgs,
		Entrypoint: b.Entrypoint, IsWeb: b.IsWeb, Version: b.Version, CreatedAt: b.CreatedAt.Format(time.RFC3339),
	}
}

func toBaseImageDTOs(items []*build.BaseImage) []baseImageDTO {
	out := make([]baseImageDTO, 0, len(items))
	for _, b := range items {
		out = append(out, *toBaseImageDTO(b))
	}
	return out
}

type buildToolDTO struct {
	ID                  int64  `json:"id"`
	UUID                string `json:"uuid"`
	Name                string `json:"name"`
	Runtime             string `json:"runtime"`
	Tool                string `json:"tool"`
	DefaultBuildCommand string `json:"default_build_command,omitempty"`
	DefaultArtifactPath string `json:"default_artifact_path,omitempty"`
	BuilderImage        string `json:"builder_image"`
	IsSystem            bool   `json:"is_system"`
	Description         string `json:"description,omitempty"`
	Version             int    `json:"version"`
	CreatedAt           string `json:"created_at"`
}

func toBuildToolDTO(bt *build.BuildTool) *buildToolDTO {
	if bt == nil {
		return nil
	}
	return &buildToolDTO{
		ID: bt.ID, UUID: bt.UUID.String(), Name: bt.Name, Runtime: string(bt.Runtime), Tool: bt.Tool,
		DefaultBuildCommand: bt.DefaultBuildCommand, DefaultArtifactPath: bt.DefaultArtifactPath,
		BuilderImage: bt.BuilderImage, IsSystem: bt.IsSystem, Description: bt.Description,
		Version: bt.Version, CreatedAt: bt.CreatedAt.Format(time.RFC3339),
	}
}

func toBuildToolDTOs(items []*build.BuildTool) []buildToolDTO {
	out := make([]buildToolDTO, 0, len(items))
	for _, bt := range items {
		out = append(out, *toBuildToolDTO(bt))
	}
	return out
}

type templateDTO struct {
	ID                int64              `json:"id"`
	UUID              string             `json:"uuid"`
	Scope             string             `json:"scope"`
	ScopeID           int64              `json:"scope_id"`
	Name              string             `json:"name"`
	Description       string             `json:"description"`
	BuildStrategy     string             `json:"build_strategy"`
	BuildCommand      string             `json:"build_command"`
	BaseImageID       int64              `json:"base_image_id"`
	DockerfileSource  string             `json:"dockerfile_source"`
	DockerfileContent string             `json:"dockerfile_content"`
	ContextPath       string             `json:"context_path"`
	BuildArgs         map[string]string  `json:"build_args"`
	EnvVars           map[string]string  `json:"env_vars"`
	IsDefault         bool               `json:"is_default"`
	UsageCount        int                `json:"usage_count"`
	Version           int                `json:"version"`
	CreatedAt         string             `json:"created_at"`
}

func toTemplateDTO(t *build.BuildTemplate) *templateDTO {
	if t == nil {
		return nil
	}
	return &templateDTO{
		ID: t.ID, UUID: t.UUID.String(), Scope: string(t.Scope), ScopeID: t.ScopeID, Name: t.Name,
		Description: t.Description, BuildStrategy: string(t.BuildStrategy), BuildCommand: t.BuildCommand,
		BaseImageID: t.BaseImageID, DockerfileSource: string(t.DockerfileSource),
		DockerfileContent: t.DockerfileContent, ContextPath: t.ContextPath, BuildArgs: t.BuildArgs,
		EnvVars: t.EnvVars, IsDefault: t.IsDefault, UsageCount: t.UsageCount,
		Version: t.Version, CreatedAt: t.CreatedAt.Format(time.RFC3339),
	}
}

func toTemplateDTOs(items []*build.BuildTemplate) []templateDTO {
	out := make([]templateDTO, 0, len(items))
	for _, t := range items {
		out = append(out, *toTemplateDTO(t))
	}
	return out
}

type imageDTO struct {
	ID               int64          `json:"id"`
	UUID             string         `json:"uuid"`
	ApplicationID    int64          `json:"application_id"`
	RegistryID       int64          `json:"registry_id"`
	Repository       string         `json:"repository"`
	Tag              string         `json:"tag"`
	Digest           string         `json:"digest"`
	FullReference    string         `json:"full_reference"`
	VersionNumber    int            `json:"version_number"`
	VersionLabel     string         `json:"version_label,omitempty"`
	Source           string         `json:"source"`
	BuildID          int64          `json:"build_id"`
	GitCommitSHA     string         `json:"git_commit_sha,omitempty"`
	GitBranch        string         `json:"git_branch,omitempty"`
	GitCommitMessage string         `json:"git_commit_message,omitempty"`
	GitAuthor        string         `json:"git_author,omitempty"`
	SizeBytes        int64          `json:"size_bytes"`
	ScanStatus       string         `json:"scan_status"`
	ScanResult       map[string]any `json:"scan_result,omitempty"`
	Status           string         `json:"status"`
	IsRollbackTarget bool           `json:"is_rollback_target"`
	Labels           map[string]string `json:"labels"`
	Version          int            `json:"version"`
	CreatedAt        string         `json:"created_at"`
}

func toImageDTO(img *build.Image) *imageDTO {
	if img == nil {
		return nil
	}
	return &imageDTO{
		ID: img.ID, UUID: img.UUID.String(), ApplicationID: img.ApplicationID, RegistryID: img.RegistryID,
		Repository: img.Repository, Tag: img.Tag, Digest: img.Digest, FullReference: img.FullReference,
		VersionNumber: img.VersionNumber, VersionLabel: img.VersionLabel, Source: string(img.Source),
		BuildID: img.BuildID, GitCommitSHA: img.GitCommitSHA, GitBranch: img.GitBranch,
		GitCommitMessage: img.GitCommitMessage, GitAuthor: img.GitAuthor, SizeBytes: img.SizeBytes,
		ScanStatus: string(img.ScanStatus), ScanResult: img.ScanResult, Status: string(img.Status),
		IsRollbackTarget: img.IsRollbackTarget, Labels: img.Labels,
		Version: img.Version, CreatedAt: img.CreatedAt.Format(time.RFC3339),
	}
}

func toImageDTOs(items []*build.Image) []imageDTO {
	out := make([]imageDTO, 0, len(items))
	for _, img := range items {
		out = append(out, *toImageDTO(img))
	}
	return out
}

type imageTagDTO struct {
	ID            int64  `json:"id"`
	UUID          string `json:"uuid"`
	ApplicationID int64  `json:"application_id"`
	Name          string `json:"name"`
	ImageID       int64  `json:"image_id"`
	Description   string `json:"description"`
	Version       int    `json:"version"`
	CreatedAt     string `json:"created_at"`
}

func toImageTagDTO(t *build.ImageVersionTag) *imageTagDTO {
	if t == nil {
		return nil
	}
	return &imageTagDTO{
		ID: t.ID, UUID: t.UUID.String(), ApplicationID: t.ApplicationID, Name: t.Name, ImageID: t.ImageID,
		Description: t.Description, Version: t.Version, CreatedAt: t.CreatedAt.Format(time.RFC3339),
	}
}

func toImageTagDTOs(items []*build.ImageVersionTag) []imageTagDTO {
	out := make([]imageTagDTO, 0, len(items))
	for _, t := range items {
		out = append(out, *toImageTagDTO(t))
	}
	return out
}
