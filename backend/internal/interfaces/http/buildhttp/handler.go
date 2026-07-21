// Package buildhttp 是构建领域的 HTTP handlers。
package buildhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/vortexops/vortexops/internal/application/buildapp"
	"github.com/vortexops/vortexops/internal/domain/build"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpauth"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpx"
	"github.com/vortexops/vortexops/internal/pkg/buildlog"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// Handler 处理构建相关路由。
type Handler struct {
	svc             *buildapp.Service
	jenkinsFactory  buildapp.JenkinsClientFactory
	registryFactory build.RegistryAdapterFactory
}

// NewHandler 创建构建 handler。jenkinsFactory 用于日志流与取消构建；
// registryFactory 用于「测试连接」与构建集成查询。
func NewHandler(svc *buildapp.Service, jenkinsFactory buildapp.JenkinsClientFactory, registryFactory build.RegistryAdapterFactory) *Handler {
	return &Handler{svc: svc, jenkinsFactory: jenkinsFactory, registryFactory: registryFactory}
}

// --- Git 源 ---

// CreateGitSource POST /api/v1/applications/{appId}/git-sources
func (h *Handler) CreateGitSource(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	appID, ok := parseID(w, chi.URLParam(r, "appId"))
	if !ok {
		return
	}
	var req struct {
		Name           string `json:"name"`
		Provider       string `json:"provider"`
		RepoURL        string `json:"repo_url"`
		DefaultBranch  string `json:"default_branch"`
		CredentialID   int64  `json:"credential_id"`
		WebhookEnabled bool   `json:"webhook_enabled"`
		WebhookSecret  string `json:"webhook_secret"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	gs, err := h.svc.CreateGitSource(r.Context(), buildapp.CreateGitSourceInput{
		ApplicationID: appID, Name: req.Name, Provider: build.GitProvider(req.Provider),
		RepoURL: req.RepoURL, DefaultBranch: req.DefaultBranch, CredentialID: req.CredentialID,
		WebhookEnabled: req.WebhookEnabled, WebhookSecret: req.WebhookSecret, ActorID: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Created(w, toGitSourceDTO(gs))
}

// ListGitSources GET /api/v1/applications/{appId}/git-sources
func (h *Handler) ListGitSources(w http.ResponseWriter, r *http.Request) {
	appID, ok := parseID(w, chi.URLParam(r, "appId"))
	if !ok {
		return
	}
	items, err := h.svc.ListGitSources(r.Context(), appID)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toGitSourceDTOs(items))
}

// DeleteGitSource DELETE /api/v1/git-sources/{id}
func (h *Handler) DeleteGitSource(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if err := h.svc.DeleteGitSource(r.Context(), id, uid); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// --- 构建任务 ---

// TriggerBuild POST /api/v1/applications/{appId}/builds
func (h *Handler) TriggerBuild(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	appID, ok := parseID(w, chi.URLParam(r, "appId"))
	if !ok {
		return
	}
	var req struct {
		GitSourceID       int64             `json:"git_source_id"`
		RefType           string            `json:"ref_type"`
		RefValue          string            `json:"ref_value"`
		CommitSHA         string            `json:"commit_sha"`
		CommitMessage     string            `json:"commit_message"`
		BuildTemplateID   int64             `json:"build_template_id"`
		BuildStrategy     string            `json:"build_strategy"`
		BuildCommand      string            `json:"build_command"`
		BuildTool         string            `json:"build_tool"`
		BuilderImage      string            `json:"builder_image"`
		ContextPath       string            `json:"context_path"`
		ArtifactPath      string            `json:"artifact_path"`
		DockerfilePath    string            `json:"dockerfile_path"`
		BaseImageID       int64             `json:"base_image_id"`
		DockerfileSource  string            `json:"dockerfile_source"`
		DockerfileContent string            `json:"dockerfile_content"`
		BuildArgs         map[string]string `json:"build_args"`
		TargetRepository  string            `json:"target_repository"`
		TargetTag         string            `json:"target_tag"`
		TriggerSource     string            `json:"trigger_source"`
		IdempotencyKey    string            `json:"idempotency_key"`
		Metadata          map[string]any    `json:"metadata"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	b, err := h.svc.TriggerBuild(r.Context(), buildapp.TriggerBuildInput{
		ApplicationID: appID, GitSourceID: req.GitSourceID, RefType: build.RefType(req.RefType),
		RefValue: req.RefValue, CommitSHA: req.CommitSHA, CommitMessage: req.CommitMessage,
		BuildTemplateID: req.BuildTemplateID, BuildStrategy: build.BuildStrategy(req.BuildStrategy),
		BuildCommand: req.BuildCommand, BuildTool: req.BuildTool, BuilderImage: req.BuilderImage,
		ContextPath: req.ContextPath, ArtifactPath: req.ArtifactPath, DockerfilePath: req.DockerfilePath,
		BaseImageID: req.BaseImageID,
		DockerfileSource: build.DockerfileSource(req.DockerfileSource), DockerfileContent: req.DockerfileContent,
		BuildArgs: req.BuildArgs, TargetRepository: req.TargetRepository, TargetTag: req.TargetTag,
		TriggeredBy: uid, TriggerSource: build.TriggerSource(req.TriggerSource),
		IdempotencyKey: req.IdempotencyKey, Metadata: req.Metadata,
	}, h.jenkinsFactory)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Created(w, toBuildDTO(b))
}

// GetBuild GET /api/v1/builds/{id}
func (h *Handler) GetBuild(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	b, err := h.svc.GetBuild(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toBuildDTO(b))
}

// ListBuilds GET /api/v1/applications/{appId}/builds?status=&triggered_by=&page=&size=
func (h *Handler) ListBuilds(w http.ResponseWriter, r *http.Request) {
	appID, ok := parseID(w, chi.URLParam(r, "appId"))
	if !ok {
		return
	}
	page, size, _ := httpx.Pagination(r)
	status := build.BuildStatus(r.URL.Query().Get("status"))
	triggeredBy, _ := strconv.ParseInt(r.URL.Query().Get("triggered_by"), 10, 64)
	items, total, err := h.svc.ListBuilds(r.Context(), appID, status, triggeredBy, page, size)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, httpx.Paged[buildDTO]{
		Items: toBuildDTOs(items), Total: total, Page: page, Size: size,
	})
}

// CancelBuild POST /api/v1/builds/{id}/cancel
func (h *Handler) CancelBuild(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	b, err := h.svc.CancelBuild(r.Context(), id, h.jenkinsFactory)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toBuildDTO(b))
}

// RebuildBuild POST /api/v1/builds/{id}/rebuild
// 在原构建记录上重新拉取代码并构建（不生成新记录）。仅终态构建可重新构建。
func (h *Handler) RebuildBuild(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	b, err := h.svc.RebuildBuild(r.Context(), id, h.jenkinsFactory)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toBuildDTO(b))
}

// UpdateBuild PUT /api/v1/builds/{id}
// 仅更新可编辑元信息：commit_message / target_tag / metadata。仅终态构建可改。
func (h *Handler) UpdateBuild(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var req struct {
		CommitMessage     *string            `json:"commit_message"`
		TargetTag         *string            `json:"target_tag"`
		Metadata          map[string]any     `json:"metadata"`
		RefType           *string            `json:"ref_type"`
		RefValue          *string            `json:"ref_value"`
		GitSourceID       *int64             `json:"git_source_id"`
		BuildCommand      *string            `json:"build_command"`
		BuildTool         *string            `json:"build_tool"`
		BuilderImage      *string            `json:"builder_image"`
		ContextPath       *string            `json:"context_path"`
		ArtifactPath      *string            `json:"artifact_path"`
		DockerfilePath    *string            `json:"dockerfile_path"`
		BaseImageID       *int64             `json:"base_image_id"`
		DockerfileSource  *string            `json:"dockerfile_source"`
		DockerfileContent *string            `json:"dockerfile_content"`
		BuildArgs         map[string]string  `json:"build_args"`
		TargetRepository  *string            `json:"target_repository"`
		Version           int                `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	in := buildapp.UpdateBuildInput{
		ID: id, CommitMessage: req.CommitMessage, TargetTag: req.TargetTag,
		Metadata: req.Metadata, Version: req.Version, ActorID: uid,
		BuildArgs: req.BuildArgs,
	}
	if req.RefType != nil {
		rt := build.RefType(*req.RefType)
		in.RefType = &rt
	}
	if req.RefValue != nil {
		in.RefValue = req.RefValue
	}
	if req.GitSourceID != nil {
		in.GitSourceID = req.GitSourceID
	}
	if req.BuildCommand != nil {
		in.BuildCommand = req.BuildCommand
	}
	if req.BuildTool != nil {
		in.BuildTool = req.BuildTool
	}
	if req.BuilderImage != nil {
		in.BuilderImage = req.BuilderImage
	}
	if req.ContextPath != nil {
		in.ContextPath = req.ContextPath
	}
	if req.ArtifactPath != nil {
		in.ArtifactPath = req.ArtifactPath
	}
	if req.DockerfilePath != nil {
		in.DockerfilePath = req.DockerfilePath
	}
	if req.BaseImageID != nil {
		in.BaseImageID = req.BaseImageID
	}
	if req.DockerfileSource != nil {
		ds := build.DockerfileSource(*req.DockerfileSource)
		in.DockerfileSource = &ds
	}
	if req.DockerfileContent != nil {
		in.DockerfileContent = req.DockerfileContent
	}
	if req.TargetRepository != nil {
		in.TargetRepository = req.TargetRepository
	}
	b, err := h.svc.UpdateBuild(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toBuildDTO(b))
}

// DeleteBuild DELETE /api/v1/builds/{id}
// 软删除构建。仅终态构建可删；运行中需先取消。
func (h *Handler) DeleteBuild(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if err := h.svc.DeleteBuild(r.Context(), id, uid); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// ListBuildSteps GET /api/v1/builds/{id}/steps
func (h *Handler) ListBuildSteps(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	items, err := h.svc.ListSteps(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toBuildStepDTOs(items))
}

// GetBuildLogs GET /api/v1/builds/{id}/logs?offset=&format=
// format=sse 返回 Server-Sent Events 流；format=text（默认）返回纯文本。
func (h *Handler) GetBuildLogs(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	offset, _ := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "text"
	}

	if format == "sse" {
		h.streamLogsSSE(w, r, id)
		return
	}

	// 一次性拉取（text）。
	logs, source, _, err := h.svc.GetBuildLogs(r.Context(), id, h.jenkinsFactory, offset)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Log-Source", source)
	w.Write(logs)
}

func (h *Handler) streamLogsSSE(w http.ResponseWriter, r *http.Request, buildID int64) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		httpx.WriteError(w, apperr.Internal("streaming not supported", nil))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ctx := r.Context()
	err := h.svc.StreamBuildLogs(ctx, buildID, h.jenkinsFactory, func(chunk []byte, source string) error {
		ev := buildlog.Event{Type: "log", Source: source, Chunk: string(chunk)}
		frame, _ := buildlog.EncodeSSE(ev)
		if _, werr := w.Write(frame); werr != nil {
			return werr
		}
		flusher.Flush()
		return nil
	})
	if err != nil {
		ev := buildlog.Event{Type: "error", Message: err.Error()}
		frame, _ := buildlog.EncodeSSE(ev)
		_, _ = w.Write(frame)
		flusher.Flush()
		return
	}
	// 结束事件。
	ev := buildlog.Event{Type: "done"}
	frame, _ := buildlog.EncodeSSE(ev)
	_, _ = w.Write(frame)
	flusher.Flush()
}

// --- 镜像仓库 ---

// CreateRegistry POST /api/v1/registries
func (h *Handler) CreateRegistry(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	var req struct {
		Name         string `json:"name"`
		Type         string `json:"type"`
		URL          string `json:"url"`
		CredentialID int64  `json:"credential_id"`
		IsDefault    bool   `json:"is_default"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	reg, err := h.svc.CreateRegistry(r.Context(), buildapp.CreateRegistryInput{
		Name: req.Name, Type: build.RegistryType(req.Type), URL: req.URL,
		CredentialID: req.CredentialID, IsDefault: req.IsDefault, CreatedBy: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Created(w, toRegistryDTO(reg))
}

// ListRegistries GET /api/v1/registries?page=&size=
func (h *Handler) ListRegistries(w http.ResponseWriter, r *http.Request) {
	page, size, _ := httpx.Pagination(r)
	items, total, err := h.svc.ListRegistries(r.Context(), page, size)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, httpx.Paged[registryDTO]{
		Items: toRegistryDTOs(items), Total: total, Page: page, Size: size,
	})
}

// DeleteRegistry DELETE /api/v1/registries/{id}
func (h *Handler) DeleteRegistry(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if err := h.svc.DeleteRegistry(r.Context(), id, uid); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// CreateJenkins POST /api/v1/jenkins-instances
func (h *Handler) CreateJenkins(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	var req struct {
		Name             string `json:"name"`
		URL              string `json:"url"`
		CredentialID     int64  `json:"credential_id"`
		DefaultJobFolder string `json:"default_job_folder"`
		IsDefault        bool   `json:"is_default"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	jk, err := h.svc.CreateJenkins(r.Context(), buildapp.CreateJenkinsInput{
		Name: req.Name, URL: req.URL, CredentialID: req.CredentialID,
		DefaultJobFolder: req.DefaultJobFolder, IsDefault: req.IsDefault, CreatedBy: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Created(w, toJenkinsDTO(jk))
}

// ListJenkins GET /api/v1/jenkins-instances?page=&size=
func (h *Handler) ListJenkins(w http.ResponseWriter, r *http.Request) {
	page, size, _ := httpx.Pagination(r)
	items, total, err := h.svc.ListJenkins(r.Context(), page, size)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, httpx.Paged[jenkinsDTO]{
		Items: toJenkinsDTOs(items), Total: total, Page: page, Size: size,
	})
}

// GetJenkins GET /api/v1/jenkins-instances/{id}
func (h *Handler) GetJenkins(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	j, err := h.svc.GetJenkins(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toJenkinsDTO(j))
}

// UpdateJenkins PUT /api/v1/jenkins-instances/{id}
func (h *Handler) UpdateJenkins(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var req struct {
		Name             *string `json:"name"`
		URL              *string `json:"url"`
		CredentialID     *int64  `json:"credential_id"`
		DefaultJobFolder *string `json:"default_job_folder"`
		IsDefault        *bool   `json:"is_default"`
		Status           *string `json:"status"`
		Version          int     `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	var status *build.JenkinsStatus
	if req.Status != nil {
		s := build.JenkinsStatus(*req.Status)
		status = &s
	}
	j, err := h.svc.UpdateJenkins(r.Context(), buildapp.UpdateJenkinsInput{
		ID: id, Name: req.Name, URL: req.URL, CredentialID: req.CredentialID,
		DefaultJobFolder: req.DefaultJobFolder, IsDefault: req.IsDefault, Status: status,
		Version: req.Version, ActorID: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toJenkinsDTO(j))
}

// DeleteJenkins DELETE /api/v1/jenkins-instances/{id}
func (h *Handler) DeleteJenkins(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if err := h.svc.DeleteJenkins(r.Context(), id, uid); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// GetRegistry GET /api/v1/registries/{id}
func (h *Handler) GetRegistry(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	reg, err := h.svc.GetRegistry(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toRegistryDTO(reg))
}

// UpdateRegistry PUT /api/v1/registries/{id}
func (h *Handler) UpdateRegistry(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var req struct {
		Name         *string `json:"name"`
		Type         *string `json:"type"`
		URL          *string `json:"url"`
		CredentialID *int64  `json:"credential_id"`
		IsDefault    *bool   `json:"is_default"`
		Status       *string `json:"status"`
		Version      int     `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	var t *build.RegistryType
	if req.Type != nil {
		v := build.RegistryType(*req.Type)
		t = &v
	}
	var status *build.RegistryStatus
	if req.Status != nil {
		s := build.RegistryStatus(*req.Status)
		status = &s
	}
	reg, err := h.svc.UpdateRegistry(r.Context(), buildapp.UpdateRegistryInput{
		ID: id, Name: req.Name, Type: t, URL: req.URL, CredentialID: req.CredentialID,
		IsDefault: req.IsDefault, Status: status, Version: req.Version, ActorID: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toRegistryDTO(reg))
}

// --- 基础镜像 ---

// CreateBaseImage POST /api/v1/base-images
func (h *Handler) CreateBaseImage(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	var req struct {
		Name                string            `json:"name"`
		Runtime             string            `json:"runtime"`
		Registry            string            `json:"registry"`
		ImageRef            string            `json:"image_ref"`
		Digest              string            `json:"digest"`
		IsSystem            bool              `json:"is_system"`
		IsRecommended       bool              `json:"is_recommended"`
		Description         string            `json:"description"`
		DockerfileTemplate  string            `json:"dockerfile_template"`
		BuildTool           string            `json:"build_tool"`
		DefaultBuildCommand string            `json:"default_build_command"`
		DefaultArtifactPath string            `json:"default_artifact_path"`
		DefaultBuildArgs    map[string]string `json:"default_build_args"`
		Entrypoint          []string          `json:"entrypoint"`
		IsWeb               bool              `json:"is_web"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	b, err := h.svc.CreateBaseImage(r.Context(), buildapp.CreateBaseImageInput{
		Name: req.Name, Runtime: build.BaseImageRuntime(req.Runtime), Registry: req.Registry,
		ImageRef: req.ImageRef, Digest: req.Digest, IsSystem: req.IsSystem, IsRecommended: req.IsRecommended,
		Description: req.Description, DockerfileTemplate: req.DockerfileTemplate,
		BuildTool: req.BuildTool, DefaultBuildCommand: req.DefaultBuildCommand,
		DefaultArtifactPath: req.DefaultArtifactPath, DefaultBuildArgs: req.DefaultBuildArgs,
		Entrypoint: req.Entrypoint, IsWeb: req.IsWeb, CreatedBy: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Created(w, toBaseImageDTO(b))
}

// ListBaseImages GET /api/v1/base-images?runtime=&page=&size=
func (h *Handler) ListBaseImages(w http.ResponseWriter, r *http.Request) {
	page, size, _ := httpx.Pagination(r)
	runtime := build.BaseImageRuntime(r.URL.Query().Get("runtime"))
	items, total, err := h.svc.ListBaseImages(r.Context(), runtime, page, size)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, httpx.Paged[baseImageDTO]{
		Items: toBaseImageDTOs(items), Total: total, Page: page, Size: size,
	})
}

// GetBaseImage GET /api/v1/base-images/{id}
func (h *Handler) GetBaseImage(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	b, err := h.svc.GetBaseImage(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toBaseImageDTO(b))
}

// UpdateBaseImage PUT /api/v1/base-images/{id}
func (h *Handler) UpdateBaseImage(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var req struct {
		Name                *string            `json:"name"`
		Runtime             *string            `json:"runtime"`
		Registry            *string            `json:"registry"`
		ImageRef            *string            `json:"image_ref"`
		Digest              *string            `json:"digest"`
		IsSystem            *bool              `json:"is_system"`
		IsRecommended       *bool              `json:"is_recommended"`
		Description         *string            `json:"description"`
		DockerfileTemplate  *string            `json:"dockerfile_template"`
		BuildTool           *string            `json:"build_tool"`
		DefaultBuildCommand *string            `json:"default_build_command"`
		DefaultArtifactPath *string            `json:"default_artifact_path"`
		DefaultBuildArgs    *map[string]string `json:"default_build_args"`
		Entrypoint          *[]string          `json:"entrypoint"`
		IsWeb               *bool              `json:"is_web"`
		Version             int                `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	var runtime *build.BaseImageRuntime
	if req.Runtime != nil {
		v := build.BaseImageRuntime(*req.Runtime)
		runtime = &v
	}
	b, err := h.svc.UpdateBaseImage(r.Context(), buildapp.UpdateBaseImageInput{
		ID: id, Name: req.Name, Runtime: runtime, Registry: req.Registry, ImageRef: req.ImageRef,
		Digest: req.Digest, IsSystem: req.IsSystem, IsRecommended: req.IsRecommended,
		Description: req.Description, DockerfileTemplate: req.DockerfileTemplate,
		BuildTool: req.BuildTool, DefaultBuildCommand: req.DefaultBuildCommand,
		DefaultArtifactPath: req.DefaultArtifactPath, DefaultBuildArgs: req.DefaultBuildArgs,
		Entrypoint: req.Entrypoint, IsWeb: req.IsWeb, Version: req.Version, ActorID: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toBaseImageDTO(b))
}

// DeleteBaseImage DELETE /api/v1/base-images/{id}
func (h *Handler) DeleteBaseImage(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if err := h.svc.DeleteBaseImage(r.Context(), id, uid); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// --- 构建工具（BuildTool） ---

// CreateBuildTool POST /api/v1/build-tools
func (h *Handler) CreateBuildTool(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	var req struct {
		Name                string `json:"name"`
		Runtime             string `json:"runtime"`
		Tool                string `json:"tool"`
		DefaultBuildCommand string `json:"default_build_command"`
		DefaultArtifactPath string `json:"default_artifact_path"`
		BuilderImage        string `json:"builder_image"`
		IsSystem            bool   `json:"is_system"`
		Description         string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	bt, err := h.svc.CreateBuildTool(r.Context(), buildapp.CreateBuildToolInput{
		Name: req.Name, Runtime: build.BaseImageRuntime(req.Runtime), Tool: req.Tool,
		DefaultBuildCommand: req.DefaultBuildCommand, DefaultArtifactPath: req.DefaultArtifactPath,
		BuilderImage: req.BuilderImage, IsSystem: req.IsSystem, Description: req.Description,
		CreatedBy: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Created(w, toBuildToolDTO(bt))
}

// ListBuildTools GET /api/v1/build-tools?runtime=&page=&size=
func (h *Handler) ListBuildTools(w http.ResponseWriter, r *http.Request) {
	page, size, _ := httpx.Pagination(r)
	runtime := build.BaseImageRuntime(r.URL.Query().Get("runtime"))
	items, total, err := h.svc.ListBuildTools(r.Context(), runtime, page, size)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, httpx.Paged[buildToolDTO]{
		Items: toBuildToolDTOs(items), Total: total, Page: page, Size: size,
	})
}

// GetBuildTool GET /api/v1/build-tools/{id}
func (h *Handler) GetBuildTool(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	bt, err := h.svc.GetBuildTool(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toBuildToolDTO(bt))
}

// UpdateBuildTool PUT /api/v1/build-tools/{id}
func (h *Handler) UpdateBuildTool(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var req struct {
		Name                *string `json:"name"`
		Runtime             *string `json:"runtime"`
		Tool                *string `json:"tool"`
		DefaultBuildCommand *string `json:"default_build_command"`
		DefaultArtifactPath *string `json:"default_artifact_path"`
		BuilderImage        *string `json:"builder_image"`
		IsSystem            *bool   `json:"is_system"`
		Description         *string `json:"description"`
		Version             int     `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	var runtime *build.BaseImageRuntime
	if req.Runtime != nil {
		v := build.BaseImageRuntime(*req.Runtime)
		runtime = &v
	}
	bt, err := h.svc.UpdateBuildTool(r.Context(), buildapp.UpdateBuildToolInput{
		ID: id, Name: req.Name, Runtime: runtime, Tool: req.Tool,
		DefaultBuildCommand: req.DefaultBuildCommand, DefaultArtifactPath: req.DefaultArtifactPath,
		BuilderImage: req.BuilderImage, IsSystem: req.IsSystem, Description: req.Description,
		Version: req.Version, UpdatedBy: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toBuildToolDTO(bt))
}

// DeleteBuildTool DELETE /api/v1/build-tools/{id}
func (h *Handler) DeleteBuildTool(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if err := h.svc.DeleteBuildTool(r.Context(), id, uid); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// --- 构建模板 ---

// CreateTemplate POST /api/v1/build-templates
func (h *Handler) CreateTemplate(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	var req struct {
		Scope             string            `json:"scope"`
		ScopeID           int64             `json:"scope_id"`
		Name              string            `json:"name"`
		Description       string            `json:"description"`
		BuildStrategy     string            `json:"build_strategy"`
		BuildCommand      string            `json:"build_command"`
		BaseImageID       int64             `json:"base_image_id"`
		DockerfileSource  string            `json:"dockerfile_source"`
		DockerfileContent string            `json:"dockerfile_content"`
		ContextPath       string            `json:"context_path"`
		BuildArgs         map[string]string `json:"build_args"`
		EnvVars           map[string]string `json:"env_vars"`
		IsDefault         bool              `json:"is_default"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	t, err := h.svc.CreateTemplate(r.Context(), buildapp.CreateTemplateInput{
		Scope: build.TemplateScope(req.Scope), ScopeID: req.ScopeID, Name: req.Name, Description: req.Description,
		BuildStrategy: build.BuildStrategy(req.BuildStrategy), BuildCommand: req.BuildCommand, BaseImageID: req.BaseImageID,
		DockerfileSource: build.DockerfileSource(req.DockerfileSource), DockerfileContent: req.DockerfileContent,
		ContextPath: req.ContextPath, BuildArgs: req.BuildArgs, EnvVars: req.EnvVars, IsDefault: req.IsDefault,
		CreatedBy: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Created(w, toTemplateDTO(t))
}

// ListTemplates GET /api/v1/build-templates?scope=&scope_id=&page=&size=
func (h *Handler) ListTemplates(w http.ResponseWriter, r *http.Request) {
	page, size, _ := httpx.Pagination(r)
	scope := build.TemplateScope(r.URL.Query().Get("scope"))
	scopeID, _ := strconv.ParseInt(r.URL.Query().Get("scope_id"), 10, 64)
	items, total, err := h.svc.ListTemplates(r.Context(), scope, scopeID, page, size)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, httpx.Paged[templateDTO]{
		Items: toTemplateDTOs(items), Total: total, Page: page, Size: size,
	})
}

// --- 制品 ---

// ListImages GET /api/v1/applications/{appId}/images?page=&size=
func (h *Handler) ListImages(w http.ResponseWriter, r *http.Request) {
	appID, ok := parseID(w, chi.URLParam(r, "appId"))
	if !ok {
		return
	}
	page, size, _ := httpx.Pagination(r)
	items, total, err := h.svc.ListImages(r.Context(), appID, page, size)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, httpx.Paged[imageDTO]{
		Items: toImageDTOs(items), Total: total, Page: page, Size: size,
	})
}

// GetImage GET /api/v1/images/{id}
func (h *Handler) GetImage(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	img, err := h.svc.GetImage(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toImageDTO(img))
}

// RetireImage POST /api/v1/images/{id}/retire
func (h *Handler) RetireImage(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if err := h.svc.RetireImage(r.Context(), id); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// --- 制品别名 ---

// CreateImageTag POST /api/v1/applications/{appId}/image-tags
func (h *Handler) CreateImageTag(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	appID, ok := parseID(w, chi.URLParam(r, "appId"))
	if !ok {
		return
	}
	var req struct {
		Name        string `json:"name"`
		ImageID     int64  `json:"image_id"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	t, err := h.svc.CreateImageTag(r.Context(), buildapp.CreateImageTagInput{
		ApplicationID: appID, Name: req.Name, ImageID: req.ImageID, Description: req.Description, CreatedBy: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Created(w, toImageTagDTO(t))
}

// ListImageTags GET /api/v1/applications/{appId}/image-tags
func (h *Handler) ListImageTags(w http.ResponseWriter, r *http.Request) {
	appID, ok := parseID(w, chi.URLParam(r, "appId"))
	if !ok {
		return
	}
	items, err := h.svc.ListImageTags(r.Context(), appID)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toImageTagDTOs(items))
}

// ListGitRefs GET /api/v1/applications/{appId}/git/refs?query=
// 通过 git smart http 协议列出远端分支，支持模糊搜索。
func (h *Handler) ListGitRefs(w http.ResponseWriter, r *http.Request) {
	appID, ok := parseID(w, chi.URLParam(r, "appId"))
	if !ok {
		return
	}
	refs, err := h.svc.ListGitRefs(r.Context(), appID, r.URL.Query().Get("query"))
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, map[string]any{"items": refs})
}

// GetGitCommit GET /api/v1/applications/{appId}/git/commit?ref=
// 获取指定分支的最新 commit 信息。
func (h *Handler) GetGitCommit(w http.ResponseWriter, r *http.Request) {
	appID, ok := parseID(w, chi.URLParam(r, "appId"))
	if !ok {
		return
	}
	ref := r.URL.Query().Get("ref")
	c, err := h.svc.GetGitCommit(r.Context(), appID, ref)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, c)
}

// UpdateImageTag PUT /api/v1/image-tags/{id}
func (h *Handler) UpdateImageTag(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var req struct {
		ImageID     int64  `json:"image_id"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	if err := h.svc.UpdateImageTag(r.Context(), id, req.ImageID, req.Description, uid); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// DeleteImageTag DELETE /api/v1/image-tags/{id}
func (h *Handler) DeleteImageTag(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if err := h.svc.DeleteImageTag(r.Context(), id, uid); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// --- 构建集成（系统变量化） ---

// GetBuildIntegration GET /api/v1/system-settings/build-integration
// 返回当前系统默认 Jenkins + Registry 配置（供前端应用详情页展示只读 Tag）。
func (h *Handler) GetBuildIntegration(w http.ResponseWriter, r *http.Request) {
	if uid := httpauth.UserID(r.Context()); uid == 0 {
		httpx.WriteError(w, apperr.Unauthorized("authentication required", nil))
		return
	}
	integration, err := h.svc.GetBuildIntegration(r.Context())
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toBuildIntegrationDTO(integration))
}

// TestJenkinsConnection POST /api/v1/jenkins-instances/test
// 请求体可选 id（已存在实例）或 url+credential_id（临时配置，未保存前测试）。
func (h *Handler) TestJenkinsConnection(w http.ResponseWriter, r *http.Request) {
	if uid := httpauth.UserID(r.Context()); uid == 0 {
		httpx.WriteError(w, apperr.Unauthorized("authentication required", nil))
		return
	}
	var req struct {
		ID           int64  `json:"id"`
		URL          string `json:"url"`
		CredentialID int64  `json:"credential_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	jk, err := h.resolveJenkinsForTest(r.Context(), req.ID, req.URL, req.CredentialID)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	if err := h.svc.TestJenkinsConnection(r.Context(), jk, h.jenkinsFactory); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, map[string]any{"ok": true})
}

// TestRegistryConnection POST /api/v1/registries/test
// 请求体可选 id（已存在实例）或 type+url+credential_id（临时配置，未保存前测试）。
func (h *Handler) TestRegistryConnection(w http.ResponseWriter, r *http.Request) {
	if uid := httpauth.UserID(r.Context()); uid == 0 {
		httpx.WriteError(w, apperr.Unauthorized("authentication required", nil))
		return
	}
	var req struct {
		ID           int64  `json:"id"`
		Type         string `json:"type"`
		URL          string `json:"url"`
		CredentialID int64  `json:"credential_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	reg, err := h.resolveRegistryForTest(r.Context(), req.ID, req.Type, req.URL, req.CredentialID)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	if err := h.svc.TestRegistryConnection(r.Context(), reg, h.registryFactory); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, map[string]any{"ok": true})
}

// resolveJenkinsForTest 解析测试连接的 Jenkins 实例：优先用已存在的实例，否则用临时配置构造。
func (h *Handler) resolveJenkinsForTest(ctx context.Context, id int64, url string, credID int64) (*build.JenkinsInstance, error) {
	if id > 0 {
		jk, err := h.svc.GetJenkins(ctx, id)
		if err != nil {
			return nil, err
		}
		// 测试时允许用请求里的 credID 覆盖已保存实例的凭证，方便快速验证新凭证。
		if credID > 0 {
			jk.CredentialID = credID
		}
		if jk.CredentialID == 0 {
			return nil, apperr.Validation("jenkins 实例未关联凭证，请先在「凭证管理」创建并选择凭证", nil)
		}
		return jk, nil
	}
	if url == "" {
		return nil, apperr.Validation("url is required", nil)
	}
	if credID == 0 {
		return nil, apperr.Validation("credential_id is required for ad-hoc test", nil)
	}
	return &build.JenkinsInstance{
		URL:              url,
		CredentialID:     credID,
		DefaultJobFolder: "vortexops",
	}, nil
}

// resolveRegistryForTest 解析测试连接的 Registry 实例。
func (h *Handler) resolveRegistryForTest(ctx context.Context, id int64, regType, url string, credID int64) (*build.Registry, error) {
	if id > 0 {
		reg, err := h.svc.GetRegistry(ctx, id)
		if err != nil {
			return nil, err
		}
		if credID > 0 {
			reg.CredentialID = credID
		}
		if reg.CredentialID == 0 {
			return nil, apperr.Validation("镜像仓库实例未关联凭证，请先在「凭证管理」创建并选择凭证", nil)
		}
		return reg, nil
	}
	if url == "" || regType == "" {
		return nil, apperr.Validation("type and url are required", nil)
	}
	if credID == 0 {
		return nil, apperr.Validation("credential_id is required for ad-hoc test", nil)
	}
	return &build.Registry{
		Type:         build.RegistryType(regType),
		URL:          url,
		CredentialID: credID,
	}, nil
}

// --- helpers ---

func mustAuth(w http.ResponseWriter, r *http.Request) int64 {
	uid := httpauth.UserID(r.Context())
	if uid == 0 {
		httpx.WriteError(w, apperr.Unauthorized("not authenticated", nil))
		return 0
	}
	return uid
}

func parseID(w http.ResponseWriter, raw string) (int64, bool) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		httpx.WriteError(w, apperr.Validation("invalid id", err))
		return 0, false
	}
	return id, true
}
