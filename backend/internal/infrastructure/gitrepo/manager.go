// Package gitrepo 提供 Git 仓库操作能力（基于 go-git）。
// 用于 GitOps：把渲染好的 K8s manifest 推送到 Git 仓库（声明式状态源）。
//
// 设计：RepositoryManager 维护一个本地临时工作目录（clone 到内存或磁盘），
// 每次提交：clone（或复用）→ 写文件 → commit → push。
// 凭证支持 HTTPS basic auth 与 SSH（通过 go-git transport）。
package gitrepo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
)

// Auth Git 认证配置（HTTPS 或 SSH 二选一）。
type Auth struct {
	Username string // HTTPS basic auth 用户名（SSH 模式忽略）
	Password string // HTTPS basic auth 密码 / token
	// SSHPrivateKey PEM 格式私钥（为空则用 HTTPS）。可选带 passphrase。
	SSHPrivateKey string
	SSHPassphrase string
}

// RepositoryManager 管理 GitOps manifest 仓库的 clone/commit/push。
type RepositoryManager struct {
	url      string
	branch   string
	auth     Auth
	workDir  string // 本地工作目录（clone 缓存），为空则每次 clone 到临时目录
	author   object.Signature
}

// New 创建仓库管理器。
// workDir 为空时每次操作 clone 到临时目录；非空时复用（增量 pull）。
func New(url, branch string, auth Auth, workDir string) *RepositoryManager {
	return &RepositoryManager{
		url:     url,
		branch:  branch,
		auth:    auth,
		workDir: workDir,
		author: object.Signature{
			Name:  "VortexOps GitOps",
			Email: "gitops@vortexops.local",
			When:  time.Now(),
		},
	}
}

// authMethod 构造 go-git 认证方法。
func (m *RepositoryManager) authMethod() (transport.AuthMethod, error) {
	if m.auth.SSHPrivateKey != "" {
		// SSH 认证（内存私钥）。
		publicKeys, err := ssh.NewPublicKeys("git", []byte(m.auth.SSHPrivateKey), m.auth.SSHPassphrase)
		if err != nil {
			return nil, fmt.Errorf("build ssh auth: %w", err)
		}
		return publicKeys, nil
	}
	// HTTPS basic auth。
	return &http.BasicAuth{Username: m.auth.Username, Password: m.auth.Password}, nil
}

// CommitAndPush 把 files（相对路径→内容）写入仓库、commit、push。
// files 中的路径相对于仓库根；同名文件覆盖，不删除其他文件。
// message 为 commit message。
func (m *RepositoryManager) CommitAndPush(ctx context.Context, files map[string][]byte, message string) error {
	authMethod, err := m.authMethod()
	if err != nil {
		return err
	}

	var repo *git.Repository
	var dir string

	if m.workDir != "" {
		// 复用工作目录：首次 clone，后续 pull。
		dir = m.workDir
		if _, err := os.Stat(filepath.Join(dir, ".git")); os.IsNotExist(err) {
			repo, err = git.PlainClone(dir, false, &git.CloneOptions{
				URL:           m.url,
				Auth:          authMethod,
				ReferenceName: plumbing.NewBranchReferenceName(m.branch),
				SingleBranch:  true,
				Depth:         1,
			})
			if err != nil {
				return fmt.Errorf("clone repo: %w", err)
			}
		} else {
			repo, err = git.PlainOpen(dir)
			if err != nil {
				return fmt.Errorf("open repo: %w", err)
			}
			wt, err := repo.Worktree()
			if err != nil {
				return fmt.Errorf("get worktree: %w", err)
			}
			if err := wt.Pull(&git.PullOptions{
				Auth:          authMethod,
				ReferenceName: plumbing.NewBranchReferenceName(m.branch),
				SingleBranch:  true,
				Force:         true,
			}); err != nil && err != git.NoErrAlreadyUpToDate {
				return fmt.Errorf("pull: %w", err)
			}
		}
	} else {
		// 临时目录：每次 clone。
		dir, err = os.MkdirTemp("", "vortexops-gitops-")
		if err != nil {
			return fmt.Errorf("create temp dir: %w", err)
		}
		defer os.RemoveAll(dir)
		repo, err = git.PlainClone(dir, false, &git.CloneOptions{
			URL:           m.url,
			Auth:          authMethod,
			ReferenceName: plumbing.NewBranchReferenceName(m.branch),
			SingleBranch:  true,
			Depth:         1,
		})
		if err != nil {
			return fmt.Errorf("clone repo: %w", err)
		}
	}

	wt, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("get worktree: %w", err)
	}

	// 写文件。
	for relPath, content := range files {
		fullPath := filepath.Join(dir, relPath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", filepath.Dir(fullPath), err)
		}
		if err := os.WriteFile(fullPath, content, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", relPath, err)
		}
		_, err := wt.Add(relPath)
		if err != nil {
			return fmt.Errorf("git add %s: %w", relPath, err)
		}
	}

	// commit。
	commit, err := wt.Commit(message, &git.CommitOptions{
		Author: &m.author,
	})
	if err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	// push。
	if err := repo.Push(&git.PushOptions{
		Auth:     authMethod,
		RefSpecs: []config.RefSpec{config.RefSpec("HEAD:refs/heads/" + m.branch)},
		Force:    false,
	}); err != nil {
		return fmt.Errorf("push commit %s: %w", commit.String()[:8], err)
	}
	return nil
}
