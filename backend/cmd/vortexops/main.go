// Package main 是 VortexOps apiserver 进程入口。
// 职责：加载配置、初始化依赖、启动 HTTP server、监听信号优雅关停。
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/vortexops/vortexops/internal/config"
	"github.com/vortexops/vortexops/internal/interfaces/http/server"
	"github.com/vortexops/vortexops/internal/platform/db"
	"github.com/vortexops/vortexops/internal/platform/logger"
	"github.com/vortexops/vortexops/internal/platform/migrate"
	"github.com/vortexops/vortexops/internal/platform/redis"
	"github.com/vortexops/vortexops/internal/platform/security"
	kafkainfra "github.com/vortexops/vortexops/internal/infrastructure/kafka"
	"github.com/vortexops/vortexops/internal/infrastructure/s3"
	"github.com/vortexops/vortexops/internal/version"
)

const envPrefix = "VORTEXOPS"

func main() {
	root := &cobra.Command{
		Use:   "vortexops",
		Short: "VortexOps platform apiserver",
	}
	root.PersistentFlags().String("config", "", "path to config file (optional)")

	root.AddCommand(serveCmd(), migrateCmd(), versionCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func loadConfig(cmd *cobra.Command) (*config.Config, error) {
	configFile, _ := cmd.Flags().GetString("config")
	cfg, err := config.Load(envPrefix, configFile)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

func serveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the HTTP apiserver",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(cmd)
			if err != nil {
				return err
			}
		log := logger.New(cfg.Log.Level, cfg.Log.Format)
		log.Info("starting vortexops apiserver",
			"env", cfg.App.Environment,
			"version", version.Version,
			"commit", version.Commit,
		)

			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()

			// 依赖初始化。
			dbPool, err := db.New(ctx, cfg.DB)
			if err != nil {
				return fmt.Errorf("init db: %w", err)
			}
			log.Info("db pool ready", "max_conns", cfg.DB.MaxConns)

			rc, err := redis.New(ctx, cfg.Redis)
			if err != nil {
				dbPool.Close()
				return fmt.Errorf("init redis: %w", err)
			}
			log.Info("redis client ready")

			// 启动时确保 schema 已迁移就绪（生产部署通常已由 migrate 命令执行）。
			if err := migrate.WaitForReady(cfg.DB, 60*time.Second); err != nil {
				log.Warn("migration readiness check failed, ensure schema is migrated", "err", err)
			}

		hasher, err := security.NewPasswordHasher(cfg.Security.BcryptCost)
		if err != nil {
			return fmt.Errorf("init password hasher: %w", err)
		}
		jwtIssuer, err := security.NewJWTIssuer(cfg.JWT)
		if err != nil {
			return fmt.Errorf("init jwt issuer: %w", err)
		}
		cipher, err := security.NewFieldCipher(cfg.Security.EncryptionKey)
		if err != nil {
			return fmt.Errorf("init field cipher: %w", err)
		}

		// S3 日志归档（可选：配置 endpoint 后启用）。
		var logStore *s3.LogStore
		if cfg.S3.Enabled {
			logStore, err = s3.NewLogStore(ctx, s3.Config{
				Endpoint:  cfg.S3.Endpoint,
				AccessKey: cfg.S3.AccessKey,
				SecretKey: cfg.S3.SecretKey,
				Bucket:    cfg.S3.Bucket,
				Region:    cfg.S3.Region,
				UseSSL:    cfg.S3.UseSSL,
			})
			if err != nil {
				return fmt.Errorf("init s3 log store: %w", err)
			}
		}

		// Kafka 异步事件总线（可选：配置 brokers 后启用）。
		var kafkaProducer *kafkainfra.Producer
		if cfg.Kafka.Enabled {
			kafkaProducer = kafkainfra.NewProducer(cfg.Kafka.Brokers, map[string]string{
				"pipeline":  cfg.Kafka.TopicPipeline,
				"build":     cfg.Kafka.TopicBuild,
				"audit":     cfg.Kafka.TopicAudit,
				"inference": cfg.Kafka.TopicInference,
			})
		}

		srv, err := server.New(server.Deps{
			Config: cfg, Logger: log, DBPool: dbPool, Redis: rc, Hasher: hasher, JWT: jwtIssuer, Cipher: cipher,
			LogStore: logStore, KafkaProducer: kafkaProducer,
		})
			if err != nil {
				return fmt.Errorf("build server: %w", err)
			}

			// 信号监听：优雅关停。
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				sig := <-sigCh
				log.Info("signal received, shutting down", "signal", sig.String())
				shutdownCtx, sdCancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
				defer sdCancel()
				if err := srv.Shutdown(shutdownCtx); err != nil {
					log.Error("graceful shutdown failed", "err", err)
				}
				cancel()
			}()

			if err := srv.Start(); err != nil {
				return err
			}
			log.Info("server stopped")
			return nil
		},
	}
}

func migrateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate [up|down|version|force|steps]",
		Short: "Run database migrations",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(cmd)
			if err != nil {
				return err
			}
			command := args[0]
			arg := 0
			if len(args) > 1 {
				fmt.Sscanf(args[1], "%d", &arg)
			}
			return migrate.Run(cfg.DB, command, arg)
		},
	}
	return cmd
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

var _ = errors.New
