package buildapp

import (
	"context"
	"errors"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/client"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"

	"github.com/vortexops/vortexops/internal/domain/application"
	"github.com/vortexops/vortexops/internal/domain/build"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// ApplicationRepo 应用仓储接口（只读 git_url 等元信息）。
// 由 application.Repository 实现，避免 buildapp 反向依赖 applicationapp。
type ApplicationRepo interface {
	GetApplicationByID(ctx context.Context, id int64) (*application.Application, error)
}

// defaultGitSourceName 应用默认 Git 源的固定名称（从 application.git_url 派生）。
const defaultGitSourceName = "default"

// ensureDefaultGitSource 确保应用存在一条基于 application.git_url 的默认 Git 源；
// 已存在则复用，不存在则创建。返回该 Git 源。
func (s *Service) ensureDefaultGitSource(ctx context.Context, applicationID int64) (*build.GitSource, error) {
	if s.appRepo == nil {
		return nil, apperr.BusinessRule("application repo not configured for git source auto-creation", nil)
	}
	app, err := s.appRepo.GetApplicationByID(ctx, applicationID)
	if err != nil {
		return nil, apperr.Internal("get application", err)
	}
	gitURL, _ := app.Metadata["git_url"].(string)
	if gitURL == "" {
		return nil, apperr.BusinessRule("application has no git_url configured; set git_url in application settings first", nil)
	}
	defaultBranch, _ := app.Metadata["default_branch"].(string)
	if defaultBranch == "" {
		defaultBranch = "main"
	}
	// 复用同名 Git 源。
	if gs, err := s.repo.GetGitSourceByName(ctx, applicationID, defaultGitSourceName); err == nil {
		// 若 url/分支变化则更新。
		if gs.RepoURL != gitURL || gs.DefaultBranch != defaultBranch {
			gs.RepoURL = gitURL
			gs.DefaultBranch = defaultBranch
			_ = s.repo.UpdateGitSource(ctx, gs)
		}
		return gs, nil
	} else if !errors.Is(err, build.ErrGitSourceNotFound) {
		return nil, apperr.Internal("get default git source", err)
	}
	gs := &build.GitSource{
		ApplicationID: applicationID, Name: defaultGitSourceName,
		Provider:      build.GitGeneric, RepoURL: gitURL, DefaultBranch: defaultBranch,
		WebhookEnabled: false,
	}
	gs.CreatedBy = 0
	if err := s.repo.CreateGitSource(ctx, gs); err != nil {
		return nil, apperr.Internal("create default git source", err)
	}
	return gs, nil
}

// GitRef Git 引用（分支）摘要。
type GitRef struct {
	Name string `json:"name"`
	Type string `json:"type"` // branch
}

// GitCommit commit 摘要。
type GitCommit struct {
	SHA     string `json:"sha"`
	Message string `json:"message"`
	Author  string `json:"author"`
	Date    string `json:"date"`
}

// ListGitRefs 通过 git smart http 协议列出远端分支，支持模糊搜索。
// query 为空时返回全部分支；非空时按子串匹配（大小写不敏感）。
func (s *Service) ListGitRefs(ctx context.Context, applicationID int64, query string) ([]GitRef, error) {
	ep, auth, err := s.gitEndpoint(ctx, applicationID)
	if err != nil {
		return nil, err
	}
	refs, err := listRemoteRefs(ctx, ep, auth)
	if err != nil {
		return nil, apperr.BusinessRule("list git refs failed: "+truncateErr(err.Error()), err)
	}
	q := strings.ToLower(strings.TrimSpace(query))
	out := make([]GitRef, 0, 64)
	for _, r := range refs {
		// 仅返回分支。
		if r.Type != "branch" {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(r.Name), q) {
			continue
		}
		out = append(out, GitRef{Name: r.Name, Type: r.Type})
	}
	return out, nil
}

// GetGitCommit 获取指定 ref 的最新 commit SHA。
// 完整 commit message 在实际构建拉代码时由 Jenkins 侧获取并回写。
func (s *Service) GetGitCommit(ctx context.Context, applicationID int64, ref string) (*GitCommit, error) {
	if ref == "" {
		return nil, apperr.Validation("ref is required", nil)
	}
	ep, auth, err := s.gitEndpoint(ctx, applicationID)
	if err != nil {
		return nil, err
	}
	refs, err := listRemoteRefs(ctx, ep, auth)
	if err != nil {
		return nil, apperr.BusinessRule("fetch git refs failed: "+truncateErr(err.Error()), err)
	}
	for _, r := range refs {
		if r.Name == ref {
			return &GitCommit{SHA: r.Hash}, nil
		}
	}
	return nil, apperr.NotFound("git ref", ref)
}

// gitEndpoint 解析 application 的 git_url 为 transport.Endpoint，并附加凭证（如有）。
func (s *Service) gitEndpoint(ctx context.Context, applicationID int64) (*transport.Endpoint, *githttp.BasicAuth, error) {
	app, err := s.appRepo.GetApplicationByID(ctx, applicationID)
	if err != nil {
		return nil, nil, apperr.Internal("get application", err)
	}
	gitURL, _ := app.Metadata["git_url"].(string)
	if gitURL == "" {
		return nil, nil, apperr.BusinessRule("application has no git_url configured", nil)
	}
	ep, err := transport.NewEndpoint(gitURL)
	if err != nil {
		return nil, nil, apperr.Validation("invalid git_url: "+err.Error(), err)
	}
	if ep.Protocol != "http" && ep.Protocol != "https" {
		return nil, nil, apperr.BusinessRule("only http/https git url is supported for remote ref listing", nil)
	}
	var auth *githttp.BasicAuth
	if ep.User != "" {
		auth = &githttp.BasicAuth{Username: ep.User, Password: ep.Password}
	}
	// 去除 endpoint 内嵌凭证，避免泄露。
	ep.User = ""
	ep.Password = ""
	return ep, auth, nil
}

// listRemoteRefs 通过 git smart http 列出远端 refs，返回分支与 tag。
type remoteRef struct {
	Name string
	Type string // branch | tag
	Hash string
}

func listRemoteRefs(ctx context.Context, ep *transport.Endpoint, auth transport.AuthMethod) ([]remoteRef, error) {
	cli, err := client.NewClient(ep)
	if err != nil {
		return nil, err
	}
	sess, err := cli.NewUploadPackSession(ep, auth)
	if err != nil {
		return nil, err
	}
	defer sess.Close()
	// 使用 AdvRefs 直接获取引用列表（不发 pack 请求，开销最小）。
	ar, err := sess.AdvertisedReferencesContext(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]remoteRef, 0, len(ar.References))
	for name, hash := range ar.References {
		r := remoteRef{Hash: hash.String()}
		switch {
		case strings.HasPrefix(name, "refs/heads/"):
			r.Name = strings.TrimPrefix(name, "refs/heads/")
			r.Type = "branch"
		case strings.HasPrefix(name, "refs/tags/"):
			r.Name = strings.TrimPrefix(name, "refs/tags/")
			r.Type = "tag"
		default:
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

// ApplicationRepo 应用仓储接口（只读 git_url 等元信息）。
// 由 application.Repository 实现，避免 buildapp 反向依赖 applicationapp。

func truncateErr(s string) string {
	if len(s) > 200 {
		return s[:200]
	}
	return s
}
