// Command pipeline-worker 是流水线执行 worker 的独立二进制（可水平扩展、独立 HPA）。
// 与 apiserver 内嵌 worker 逻辑一致；大规模部署时使用此独立进程，避免 apiserver 承担执行负载。
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/vortexops/vortexops/internal/application/applicationapp"
	"github.com/vortexops/vortexops/internal/application/buildapp"
	"github.com/vortexops/vortexops/internal/application/clusterapp"
	"github.com/vortexops/vortexops/internal/application/configapp"
	"github.com/vortexops/vortexops/internal/application/dnsapp"
	"github.com/vortexops/vortexops/internal/application/k8sapp"
	"github.com/vortexops/vortexops/internal/application/releaseapp"
	"github.com/vortexops/vortexops/internal/application/systemapp"
	"github.com/vortexops/vortexops/internal/config"
	applicationrepo "github.com/vortexops/vortexops/internal/infrastructure/postgres/applicationrepo"
	buildrepo "github.com/vortexops/vortexops/internal/infrastructure/postgres/buildrepo"
	clusterrepo "github.com/vortexops/vortexops/internal/infrastructure/postgres/clusterrepo"
	configrepo "github.com/vortexops/vortexops/internal/infrastructure/postgres/configrepo"
	dnsrepo "github.com/vortexops/vortexops/internal/infrastructure/postgres/dnsrepo"
	pipelinerepo "github.com/vortexops/vortexops/internal/infrastructure/postgres/pipelinerepo"
	releaserepo "github.com/vortexops/vortexops/internal/infrastructure/postgres/releaserepo"
	systemrepo "github.com/vortexops/vortexops/internal/infrastructure/postgres/systemrepo"
	workspacerepo "github.com/vortexops/vortexops/internal/infrastructure/postgres/workspacerepo"
	"github.com/vortexops/vortexops/internal/infrastructure/buildinfra"
	"github.com/vortexops/vortexops/internal/infrastructure/k8s"
	"github.com/vortexops/vortexops/internal/infrastructure/kafka"
	"github.com/vortexops/vortexops/internal/infrastructure/pipeline/executor"
	"github.com/vortexops/vortexops/internal/infrastructure/pipeline/pipelineworker"
	"github.com/vortexops/vortexops/internal/platform/db"
	"github.com/vortexops/vortexops/internal/platform/logger"
	"github.com/vortexops/vortexops/internal/platform/redis"
	"github.com/vortexops/vortexops/internal/platform/security"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "pipeline-worker exited:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load("", "")
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	log := logger.New(cfg.Log.Level, cfg.Log.Format)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Info("shutdown signal received")
		cancel()
	}()

	dbPool, err := db.New(ctx, cfg.DB)
	if err != nil {
		return fmt.Errorf("init db: %w", err)
	}
	defer dbPool.Close()

	rc, err := redis.New(ctx, cfg.Redis)
	if err != nil {
		return fmt.Errorf("init redis: %w", err)
	}
	_ = rc

	cipher, err := security.NewFieldCipher(cfg.Security.EncryptionKey)
	if err != nil {
		return fmt.Errorf("init field cipher: %w", err)
	}

	// 仓储与依赖服务。
	pipeRepo := pipelinerepo.New(dbPool.Pool)
	clusterRepo := clusterrepo.New(dbPool.Pool)
	buildRepo := buildrepo.New(dbPool.Pool)
	releaseRepo := releaserepo.New(dbPool.Pool)
	appRepo := applicationrepo.New(dbPool.Pool)
	wsRepo := workspacerepo.New(dbPool.Pool)
	configRepo := configrepo.New(dbPool.Pool)
	systemRepo := systemrepo.New(dbPool.Pool)
	clusterPool := k8s.NewClientPool()
	clusterSvc := clusterapp.New(clusterRepo, cipher, clusterPool)
	k8sSvc := k8sapp.New(clusterSvc)
	connector := buildinfra.NewConnector(clusterRepo, cipher)
	systemSvc := systemapp.New(systemRepo)
	buildSvc := buildapp.New(buildRepo, clusterRepo, nil, systemSvc, appRepo)
	appSvc := applicationapp.New(appRepo, wsRepo, k8sSvc)
	configSvc := configapp.New(configRepo)
	releaseSvc := releaseapp.New(releaseRepo, appSvc, buildSvc, configSvc, clusterRepo, clusterSvc, clusterSvc, clusterSvc, buildRepo).
		WithDynamicClientProvider(clusterSvc)
	dnsRepo := dnsrepo.New(dbPool.Pool)
	releaseSvc.WithDNSMapper(dnsapp.New(dnsRepo, clusterSvc))

	// 执行引擎注册阶段执行器。
	engine := executor.NewEngine()
	engine.Register(executor.NewBuildExecutor(buildSvc, connector.JenkinsClient))
	engine.Register(executor.NewScanExecutor(buildinfra.NewImageScanReader(buildRepo)))
	engine.Register(executor.NewDeployExecutor(releaseSvc))
	engine.Register(executor.NewVerifyExecutor(releaseSvc))
	engine.Register(executor.NewPromoteExecutor(releaseSvc))

	var producer *kafka.Producer
	if cfg.Kafka.Enabled {
		producer = kafka.NewProducer(cfg.Kafka.Brokers, map[string]string{
			"pipeline": cfg.Kafka.TopicPipeline,
		})
		defer producer.Close()
	}

	worker := pipelineworker.New(pipeRepo, engine, producer, cfg.Kafka.Brokers, "pipeline", cfg.Kafka.TopicPipeline, log)
	log.Info("pipeline-worker starting", "brokers", cfg.Kafka.Brokers)
	worker.Run(ctx)
	if err := ctx.Err(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}
