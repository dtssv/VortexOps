// Package helm 提供 Helm v3 客户端封装：基于 kubeconfig 对目标集群执行 install/upgrade/rollback/uninstall。
// chart 通过 repo URL + name + version 由 ChartDownloader 解析下载到临时缓存目录后加载。
package helm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/downloader"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/release"
)

// Deployer 执行 Helm 部署操作。
type Deployer struct{}

// NewDeployer 创建部署器。
func NewDeployer() *Deployer { return &Deployer{} }

// InstallOptions 安装/升级参数。
type InstallOptions struct {
	ReleaseName  string
	Namespace    string
	ChartRepo    string
	ChartName    string
	ChartVersion string
	Values       map[string]any
	Kubeconfig   []byte
}

// Result 部署结果。
type Result struct {
	ReleaseName string
	Namespace   string
	Revision    int
	Status      string
	Notes       string
}

// session 持有一次 Helm 操作所需的临时目录、settings 与 action.Configuration。
type session struct {
	dir      string
	settings *cli.EnvSettings
	ac       *action.Configuration
}

func newSession(kubeconfig []byte, namespace string) (*session, error) {
	dir, err := os.MkdirTemp("", "helm-sess-")
	if err != nil {
		return nil, err
	}
	kubePath := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(kubePath, kubeconfig, 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	settings := cli.New()
	settings.KubeConfig = kubePath
	settings.SetNamespace(namespace)
	settings.RepositoryCache = filepath.Join(dir, "repo")
	settings.RepositoryConfig = filepath.Join(dir, "repositories.yaml")
	ac := &action.Configuration{}
	if err := ac.Init(settings.RESTClientGetter(), namespace, "secrets", nopLog); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("init helm action config: %w", err)
	}
	return &session{dir: dir, settings: settings, ac: ac}, nil
}

func (s *session) close() { _ = os.RemoveAll(s.dir) }

// loadChart 通过 ChartDownloader 从 repo URL 解析下载并加载 chart。
func (s *session) loadChart(repoURL, name, version string) (*chart.Chart, error) {
	if err := os.MkdirAll(s.settings.RepositoryCache, 0o755); err != nil {
		return nil, err
	}
	providers := getter.All(s.settings)
	cd := &downloader.ChartDownloader{
		Out:              os.Stderr,
		Getters:          providers,
		Options:          []getter.Option{getter.WithURL(repoURL)},
		RepositoryConfig: s.settings.RepositoryConfig,
		RepositoryCache:  s.settings.RepositoryCache,
	}
	ref := name
	if repoURL != "" {
		ref = fmt.Sprintf("%s/%s", repoURL, name)
	}
	saved, _, err := cd.DownloadTo(ref, version, s.settings.RepositoryCache)
	if err != nil {
		return nil, fmt.Errorf("download chart %s:%s: %w", ref, version, err)
	}
	return loader.Load(saved)
}

// InstallOrUpgrade 安装或升级 release。
func (d *Deployer) InstallOrUpgrade(ctx context.Context, opts InstallOptions) (*Result, error) {
	s, err := newSession(opts.Kubeconfig, opts.Namespace)
	if err != nil {
		return nil, err
	}
	defer s.close()

	chrt, err := s.loadChart(opts.ChartRepo, opts.ChartName, opts.ChartVersion)
	if err != nil {
		return nil, err
	}

	histClient := action.NewHistory(s.ac)
	histClient.Max = 1
	var rel *release.Release
	if _, herr := histClient.Run(opts.ReleaseName); herr == nil {
		upg := action.NewUpgrade(s.ac)
		upg.Namespace = opts.Namespace
		upg.Timeout = 10 * time.Minute
		upg.Wait = true
		rel, err = upg.RunWithContext(ctx, opts.ReleaseName, chrt, opts.Values)
	} else {
		install := action.NewInstall(s.ac)
		install.ReleaseName = opts.ReleaseName
		install.Namespace = opts.Namespace
		install.CreateNamespace = true
		install.Timeout = 10 * time.Minute
		install.Wait = true
		rel, err = install.RunWithContext(ctx, chrt, opts.Values)
	}
	if err != nil {
		return nil, err
	}
	return &Result{
		ReleaseName: rel.Name, Namespace: rel.Namespace,
		Revision: rel.Version, Status: rel.Info.Status.String(), Notes: rel.Info.Notes,
	}, nil
}

// Uninstall 卸载 release。
func (d *Deployer) Uninstall(ctx context.Context, releaseName, namespace string, kubeconfig []byte) error {
	s, err := newSession(kubeconfig, namespace)
	if err != nil {
		return err
	}
	defer s.close()
	un := action.NewUninstall(s.ac)
	un.Timeout = 5 * time.Minute
	un.Wait = true
	_, err = un.Run(releaseName)
	return err
}

// Rollback 回滚到指定 revision（0 表示上一个版本）。
func (d *Deployer) Rollback(ctx context.Context, releaseName, namespace string, revision int, kubeconfig []byte) error {
	s, err := newSession(kubeconfig, namespace)
	if err != nil {
		return err
	}
	defer s.close()
	rb := action.NewRollback(s.ac)
	rb.Version = revision
	rb.Timeout = 5 * time.Minute
	rb.Wait = true
	return rb.Run(releaseName)
}

// Status 获取 release 状态。
func (d *Deployer) Status(ctx context.Context, releaseName, namespace string, kubeconfig []byte) (*Result, error) {
	s, err := newSession(kubeconfig, namespace)
	if err != nil {
		return nil, err
	}
	defer s.close()
	st := action.NewStatus(s.ac)
	rel, err := st.Run(releaseName)
	if err != nil {
		return nil, err
	}
	return &Result{
		ReleaseName: rel.Name, Namespace: rel.Namespace,
		Revision: rel.Version, Status: rel.Info.Status.String(), Notes: rel.Info.Notes,
	}, nil
}

func nopLog(string, ...any) {}
