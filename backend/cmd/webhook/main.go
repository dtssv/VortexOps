// Package main 是 VortexOps Mutating Webhook 进程入口。
//
// webhook 负责：Pod 创建时从 IP 池分配稳定 IP 并注入 CNI 注解，
// 保证 Deployment 多副本重建后复用同 IP。
//
// 运行：vortexops-webhook serve
// 关停：SIGTERM/SIGINT → 停止 HTTP server → 退出。
//
// 启动时通过 registrar 向所有 active 集群注册 MutatingWebhookConfiguration，
// 周期 reconcile 保证 CA bundle / URL 始终最新。
//
// webhook 为无状态读 IP 池（分配是写但幂等），可多副本水平扩展；
// CA bundle 自签模式下每个实例独立 CA，需通过 sticky session 或共享 CA 文件保证一致。
// 生产环境建议用 cert-manager 签发统一证书 + webhook Service 负载均衡。
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/vortexops/vortexops/internal/application/clusterapp"
	"github.com/vortexops/vortexops/internal/domain/cluster"
	applicationrepo "github.com/vortexops/vortexops/internal/infrastructure/postgres/applicationrepo"
	clusterrepo "github.com/vortexops/vortexops/internal/infrastructure/postgres/clusterrepo"
	"github.com/vortexops/vortexops/internal/config"
	"github.com/vortexops/vortexops/internal/infrastructure/k8s"
	"github.com/vortexops/vortexops/internal/infrastructure/k8s/admission"
	"github.com/vortexops/vortexops/internal/interfaces/http/admissionhttp"
	"github.com/vortexops/vortexops/internal/platform/db"
	"github.com/vortexops/vortexops/internal/platform/logger"
	plattls "github.com/vortexops/vortexops/internal/platform/tls"
	"github.com/vortexops/vortexops/internal/platform/security"
	"github.com/vortexops/vortexops/internal/version"
)

const envPrefix = "VORTEXOPS"

func main() {
	root := &cobra.Command{
		Use:   "vortexops-webhook",
		Short: "VortexOps Mutating Webhook for stable Pod IP injection",
	}
	root.AddCommand(serveCmd(), versionCmd())
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print build version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(version.String())
		},
	}
}

func serveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the webhook server",
		RunE:  runServe,
	}
}

func runServe(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(envPrefix, "")
	if err != nil {
		return err
	}
	log := logger.New(cfg.Log.Level, cfg.Log.Format)
	log.Info("starting vortexops webhook",
		"env", cfg.App.Environment, "version", version.Version, "commit", version.Commit)

	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	// --- 依赖初始化 ---
	dbPool, err := db.New(ctx, cfg.DB)
	if err != nil {
		return fmt.Errorf("init db: %w", err)
	}
	defer dbPool.Close()

	cipher, err := security.NewFieldCipher(cfg.Security.EncryptionKey)
	if err != nil {
		return fmt.Errorf("init field cipher: %w", err)
	}

	clusterRepo := clusterrepo.New(dbPool.Pool)
	appRepo := applicationrepo.New(dbPool.Pool)
	clientPool := k8s.NewClientPool()
	clusterSvc := clusterapp.New(clusterRepo, cipher, clientPool)

	// --- TLS 证书加载 ---
	tlsCfg := plattls.Config{
		CertFile:    os.Getenv("WEBHOOK_TLS_CERT_FILE"),
		KeyFile:     os.Getenv("WEBHOOK_TLS_KEY_FILE"),
		CAFile:      os.Getenv("WEBHOOK_TLS_CA_FILE"),
		CommonName:  "vortexops-webhook",
		DNSNames:    parseDNSNames(os.Getenv("WEBHOOK_TLS_DNS_NAMES")),
		IPAddresses: nil,
	}
	bundle, err := plattls.Load(tlsCfg)
	if err != nil {
		return fmt.Errorf("load tls bundle: %w", err)
	}
	tlsConfig, err := bundle.TLSConfig()
	if err != nil {
		return fmt.Errorf("build tls config: %w", err)
	}

	// --- webhook URL ---
	// kube-apiserver 访问 webhook 的地址。开发环境为 https://webhook:8443/mutate（compose 服务名）。
	// 生产环境为 https://<webhook-service>.<namespace>.svc:8443/mutate。
	webhookURL := os.Getenv("WEBHOOK_URL")
	if webhookURL == "" {
		webhookURL = "https://webhook:8443/mutate"
	}
	addr := os.Getenv("WEBHOOK_ADDR")
	if addr == "" {
		addr = ":8443"
	}
	log.Info("webhook serving config", "addr", addr, "url", webhookURL, "tls_source", tlsSource(tlsCfg))

	// --- 注册 MutatingWebhookConfiguration 到所有 active 集群 ---
	registrar := admission.NewRegistrar(webhookURL, bundle.CABundlePEM, log)
	if err := registerAllClusters(ctx, clusterRepo, cipher, clientPool, registrar, log); err != nil {
		log.Warn("initial webhook registration had errors (will retry in reconcile loop)", "err", err)
	}

	// --- HTTP server ---
	mux := http.NewServeMux()
	handler := admissionhttp.NewHandler(appRepo, clusterSvc, clusterSvc, log)
	handler.Register(mux)

	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		TLSConfig:         tlsConfig,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// 启动 reconcile loop（周期保证 webhook config 存在且 CA bundle 最新）。
	go reconcileLoop(ctx, clusterRepo, cipher, clientPool, registrar, log)

	// 启动 HTTP server（TLS）。
	serverErr := make(chan error, 1)
	go func() {
		log.Info("webhook TLS server listening", "addr", addr)
		if err := server.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- fmt.Errorf("webhook server: %w", err)
		}
		close(serverErr)
	}()

	// 信号处理。
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		log.Info("signal received, shutting down webhook", "signal", sig.String())
	case err := <-serverErr:
		if err != nil {
			log.Error("webhook server error", "err", err)
		}
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Error("webhook graceful shutdown failed", "err", err)
	}
	log.Info("webhook stopped")
	return nil
}

// registerAllClusters 启动时向所有 active 集群注册 MutatingWebhookConfiguration。
func registerAllClusters(ctx context.Context, repo cluster.Repository, cipher *security.FieldCipher, pool *k8s.ClientPool, registrar *admission.Registrar, log *logger.Logger) error {
	clusters, err := repo.ListActiveClusters(ctx)
	if err != nil {
		return fmt.Errorf("list active clusters: %w", err)
	}
	if len(clusters) == 0 {
		log.Warn("no active clusters, webhook registered to no cluster (will retry in reconcile loop)")
		return nil
	}
	var firstErr error
	for _, c := range clusters {
		raw, err := cipher.Decrypt(c.KubeconfigEncrypted)
		if err != nil {
			log.Error("decrypt kubeconfig failed, skipping cluster", "cluster_id", c.ID, "err", err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		entry, err := pool.GetOrCreate(c.ID, raw, c.InsecureSkipTLS)
		if err != nil {
			log.Error("build client failed, skipping cluster", "cluster_id", c.ID, "err", err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		rctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		if err := registrar.EnsureWebhookConfig(rctx, entry.Clientset); err != nil {
			log.Error("register webhook config failed for cluster", "cluster_id", c.ID, "err", err)
			if firstErr == nil {
				firstErr = err
			}
		} else {
			log.Info("registered webhook config for cluster", "cluster_id", c.ID, "name", c.Name)
		}
		cancel()
	}
	return firstErr
}

// reconcileLoop 周期校正所有 active 集群的 MutatingWebhookConfiguration。
// 处理：新加入集群、CA bundle 轮换、配置被误删。
func reconcileLoop(ctx context.Context, repo cluster.Repository, cipher *security.FieldCipher, pool *k8s.ClientPool, registrar *admission.Registrar, log *logger.Logger) {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			clusters, err := repo.ListActiveClusters(ctx)
			if err != nil {
				log.Error("reconcile: list clusters failed", "err", err)
				continue
			}
			for _, c := range clusters {
				raw, err := cipher.Decrypt(c.KubeconfigEncrypted)
				if err != nil {
					continue
				}
				entry, err := pool.GetOrCreate(c.ID, raw, c.InsecureSkipTLS)
				if err != nil {
					continue
				}
				rctx, cancel := context.WithTimeout(ctx, 15*time.Second)
				if err := registrar.EnsureWebhookConfig(rctx, entry.Clientset); err != nil {
					log.Warn("reconcile: ensure webhook config failed", "cluster_id", c.ID, "err", err)
				}
				cancel()
			}
		}
	}
}

// parseDNSNames 解析逗号分隔的 DNS 名列表。
func parseDNSNames(s string) []string {
	if s == "" {
		return []string{"vortexops-webhook", "vortexops-webhook.vortexops", "vortexops-webhook.vortexops.svc", "localhost"}
	}
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			if i > start {
				out = append(out, s[start:i])
			}
			start = i + 1
		}
	}
	if len(out) == 0 {
		return []string{"localhost"}
	}
	return out
}

// tlsSource 返回 TLS 来源描述（日志用）。
func tlsSource(cfg plattls.Config) string {
	if cfg.CertFile != "" && cfg.KeyFile != "" {
		return "files(" + cfg.CertFile + ")"
	}
	return "self-signed(dev)"
}
