// Package server 组装 HTTP 路由、中间件与服务依赖。
package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/vortexops/vortexops/internal/application/alertapp"
	"github.com/vortexops/vortexops/internal/application/applicationapp"
	"github.com/vortexops/vortexops/internal/application/auditapp"
	"github.com/vortexops/vortexops/internal/application/buildapp"
	"github.com/vortexops/vortexops/internal/application/clusterapp"
	"github.com/vortexops/vortexops/internal/application/clusteropsapp"
	"github.com/vortexops/vortexops/internal/application/collabapp"
	"github.com/vortexops/vortexops/internal/application/configapp"
	"github.com/vortexops/vortexops/internal/application/extapiapp"
	"github.com/vortexops/vortexops/internal/application/identityapp"
	"github.com/vortexops/vortexops/internal/application/inferenceapp"
	"github.com/vortexops/vortexops/internal/application/k8sapp"
	"github.com/vortexops/vortexops/internal/application/logapp"
	"github.com/vortexops/vortexops/internal/application/monitoringapp"
	"github.com/vortexops/vortexops/internal/application/nodepoolapp"
	"github.com/vortexops/vortexops/internal/application/opsapp"
	"github.com/vortexops/vortexops/internal/application/pipelineapp"
	"github.com/vortexops/vortexops/internal/application/rbacapp"
	"github.com/vortexops/vortexops/internal/application/releaseapp"
	"github.com/vortexops/vortexops/internal/application/approvalapp"
	"github.com/vortexops/vortexops/internal/application/bastionapp"
	"github.com/vortexops/vortexops/internal/application/chatapp"
	"github.com/vortexops/vortexops/internal/application/diagnosisapp"
	"github.com/vortexops/vortexops/internal/application/dnsapp"
	"github.com/vortexops/vortexops/internal/application/kbapp"
	"github.com/vortexops/vortexops/internal/application/systemapp"
	"github.com/vortexops/vortexops/internal/application/userprofileapp"
	"github.com/vortexops/vortexops/internal/application/workspaceapp"
	"github.com/vortexops/vortexops/internal/config"
	"github.com/vortexops/vortexops/internal/interfaces/http/alerthttp"
	"github.com/vortexops/vortexops/internal/interfaces/http/applicationhttp"
	"github.com/vortexops/vortexops/internal/interfaces/http/audithttp"
	"github.com/vortexops/vortexops/internal/interfaces/http/authhttp"
	"github.com/vortexops/vortexops/internal/interfaces/http/buildhttp"
	"github.com/vortexops/vortexops/internal/interfaces/http/clusterhttp"
	"github.com/vortexops/vortexops/internal/interfaces/http/clusteropshttp"
	"github.com/vortexops/vortexops/internal/interfaces/http/collabhttp"
	"github.com/vortexops/vortexops/internal/interfaces/http/confighttp"
	"github.com/vortexops/vortexops/internal/interfaces/http/extapi"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpauth"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpx/auditmw"
	"github.com/vortexops/vortexops/internal/interfaces/http/inferencehttp"
	"github.com/vortexops/vortexops/internal/interfaces/http/k8shttp"
	"github.com/vortexops/vortexops/internal/interfaces/http/loghttp"
	"github.com/vortexops/vortexops/internal/interfaces/http/monitoringhttp"
	"github.com/vortexops/vortexops/internal/interfaces/http/opshttp"
	"github.com/vortexops/vortexops/internal/interfaces/http/pipelinehttp"
	"github.com/vortexops/vortexops/internal/interfaces/http/rbachttp"
	"github.com/vortexops/vortexops/internal/interfaces/http/releasehttp"
	"github.com/vortexops/vortexops/internal/interfaces/http/approvalhttp"
	"github.com/vortexops/vortexops/internal/interfaces/http/bastionhttp"
	"github.com/vortexops/vortexops/internal/interfaces/http/chathttp"
	"github.com/vortexops/vortexops/internal/interfaces/http/diagnosishttp"
	"github.com/vortexops/vortexops/internal/interfaces/http/dnshttp"
	"github.com/vortexops/vortexops/internal/interfaces/http/kbhttp"
	"github.com/vortexops/vortexops/internal/interfaces/http/systemhttp"
	"github.com/vortexops/vortexops/internal/interfaces/http/userprofilehttp"
	"github.com/vortexops/vortexops/internal/interfaces/http/workspacehttp"
	"github.com/vortexops/vortexops/internal/platform/db"
	"github.com/vortexops/vortexops/internal/platform/logger"
	"github.com/vortexops/vortexops/internal/platform/metrics"
	"github.com/vortexops/vortexops/internal/platform/redis"
	"github.com/vortexops/vortexops/internal/platform/security"
	applicationrepo "github.com/vortexops/vortexops/internal/infrastructure/postgres/applicationrepo"
	alertrepo "github.com/vortexops/vortexops/internal/infrastructure/postgres/alertrepo"
	auditrepo "github.com/vortexops/vortexops/internal/infrastructure/postgres/auditrepo"
	bastionrepo "github.com/vortexops/vortexops/internal/infrastructure/postgres/bastionrepo"
	behaviorauditrepo "github.com/vortexops/vortexops/internal/infrastructure/postgres/behaviorauditrepo"
	buildrepo "github.com/vortexops/vortexops/internal/infrastructure/postgres/buildrepo"
	chatrepo "github.com/vortexops/vortexops/internal/infrastructure/postgres/chatrepo"
	clusterrepo "github.com/vortexops/vortexops/internal/infrastructure/postgres/clusterrepo"
	clusteropsrepo "github.com/vortexops/vortexops/internal/infrastructure/postgres/clusteropsrepo"
	collabrepo "github.com/vortexops/vortexops/internal/infrastructure/postgres/collabrepo"
	configrepo "github.com/vortexops/vortexops/internal/infrastructure/postgres/configrepo"
	identityrepo "github.com/vortexops/vortexops/internal/infrastructure/postgres/identityrepo"
	inferencerepo "github.com/vortexops/vortexops/internal/infrastructure/postgres/inferencerepo"
	kbrepo "github.com/vortexops/vortexops/internal/infrastructure/postgres/kbrepo"
	opssessionrepo "github.com/vortexops/vortexops/internal/infrastructure/postgres/opssessionrepo"
	pipelinerepo "github.com/vortexops/vortexops/internal/infrastructure/postgres/pipelinerepo"
	rbacrepo "github.com/vortexops/vortexops/internal/infrastructure/postgres/rbacrepo"
	releaserepo "github.com/vortexops/vortexops/internal/infrastructure/postgres/releaserepo"
	approvalrepo "github.com/vortexops/vortexops/internal/infrastructure/postgres/approvalrepo"
	systemrepo "github.com/vortexops/vortexops/internal/infrastructure/postgres/systemrepo"
	extapirepo "github.com/vortexops/vortexops/internal/infrastructure/postgres/extapirepo"
	dnsrepo "github.com/vortexops/vortexops/internal/infrastructure/postgres/dnsrepo"
	userprofilerepo "github.com/vortexops/vortexops/internal/infrastructure/postgres/userprofilerepo"
	workspacerepo "github.com/vortexops/vortexops/internal/infrastructure/postgres/workspacerepo"
	"github.com/vortexops/vortexops/internal/infrastructure/buildinfra"
	"github.com/vortexops/vortexops/internal/infrastructure/elasticsearch"
	"github.com/vortexops/vortexops/internal/infrastructure/k8s"
	k8sexec "github.com/vortexops/vortexops/internal/infrastructure/k8s/exec"
	"github.com/vortexops/vortexops/internal/infrastructure/kafka"
	"github.com/vortexops/vortexops/internal/infrastructure/redis/permission"
	extapiredis "github.com/vortexops/vortexops/internal/infrastructure/redis/extapi"
	"github.com/vortexops/vortexops/internal/infrastructure/redis/runtime"
	"github.com/vortexops/vortexops/internal/infrastructure/s3"
	"github.com/vortexops/vortexops/internal/infrastructure/tekton"
	"github.com/vortexops/vortexops/internal/infrastructure/pipeline/executor"
	"github.com/vortexops/vortexops/internal/infrastructure/pipeline/pipelineworker"
	"github.com/vortexops/vortexops/internal/domain/build"
)

// Deps 聚装 server 所需依赖。
type Deps struct {
	Config *config.Config
	Logger *logger.Logger
	DBPool *db.Pool
	Redis  *redis.Client
	Hasher *security.PasswordHasher
	JWT    *security.JWTIssuer
	Cipher *security.FieldCipher
	// LogStore 构建日志归档存储（MinIO/S3）；若为 nil 则不启用日志归档。
	LogStore *s3.LogStore
	// KafkaProducer 异步事件生产者；若为 nil 则不启用 Kafka 事件。
	KafkaProducer *kafka.Producer
}

// Server 持有 HTTP server 与依赖。
type Server struct {
	cfg    *config.Config
	log    *logger.Logger
	http   *http.Server
	dbPool *db.Pool
	redis  *redis.Client
}

// New 组装依赖并构建路由。
func New(deps Deps) (*Server, error) {
	userRepo := identityrepo.NewUserRepository(deps.DBPool.Pool)
	tokenRepo := identityrepo.NewRefreshTokenRepository(deps.DBPool.Pool)
	auditRepo := auditrepo.New(deps.DBPool.Pool)
	auditSvc := auditapp.New(auditRepo)
	authSvc := identityapp.New(userRepo, tokenRepo, deps.Hasher, deps.JWT, deps.Cipher, deps.Config.Security)
	// 登录方式注册表：默认注册 local provider。OIDC/LDAP 等外部 provider 可在此扩展注册。
	// local provider 的展示名可通过系统设置 auth.local_display_name 覆盖为"自定义"命名。
	authProviders := identityapp.NewProviderRegistry()
	_ = authProviders.Register(identityapp.NewLocalProvider("local", "默认账号密码"))
	authSvc.SetProviders(authProviders)
	authHandler := authhttp.NewHandler(authSvc, auditSvc)

	wsRepo := workspacerepo.New(deps.DBPool.Pool)
	appRepo := applicationrepo.New(deps.DBPool.Pool)
	clusterRepo := clusterrepo.New(deps.DBPool.Pool)
	buildRepo := buildrepo.New(deps.DBPool.Pool)
	releaseRepo := releaserepo.New(deps.DBPool.Pool)
	configRepo := configrepo.New(deps.DBPool.Pool)
	rbacRepo := rbacrepo.New(deps.DBPool.Pool)
	collabRepo := collabrepo.New(deps.DBPool.Pool)
	systemRepo := systemrepo.New(deps.DBPool.Pool)
	alertRepo := alertrepo.New(deps.DBPool.Pool)
	wsSvc := workspaceapp.New(wsRepo)
	clusterPool := k8s.NewClientPool()
	clusterSvc := clusterapp.New(clusterRepo, deps.Cipher, clusterPool)
	k8sSvc := k8sapp.New(clusterSvc)
	appSvc := applicationapp.New(appRepo, wsRepo, k8sSvc)
	// 注入集群网络方案解析器：分组创建/更新时校验 network_mode=underlay 需集群支持。
	// clusterSvc 实现 applicationapp.NetworkProfileResolver（SupportsUnderlay）。
	appSvc.WithNetworkProfileResolver(clusterSvc)
	// 注入 group IP 释放器：DeleteGroup 时释放稳定 IP，避免泄漏。
	// clusterSvc 实现 applicationapp.GroupIPReleaser（ReleaseForGroup）。
	appSvc.WithGroupIPReleaser(clusterSvc)
	nodePoolSvc := nodepoolapp.New()
	systemSvc := systemapp.New(systemRepo)
	alertSvc := alertapp.New(alertRepo)
	monitoringSvc := monitoringapp.New(alertSvc, systemSvc)
	connector := buildinfra.NewConnector(clusterRepo, deps.Cipher)
	buildSvc := buildapp.New(buildRepo, clusterRepo, deps.LogStore, systemSvc, appRepo)
	engineFactory := &buildapp.BuildEngineFactory{
		Jenkins: connector.JenkinsClient,
		Tekton: func(ctx context.Context) (build.BuildEngineClient, error) {
			kubeconfig, _ := systemSvc.GetTektonKubeconfig(ctx)
			namespace, _ := systemSvc.GetTektonNamespace(ctx)
			return tekton.NewFromConfig(kubeconfig, namespace)
		},
	}
	buildSvc.StartPoller(context.Background(), connector.JenkinsClient, connector.RegistryAdapter, engineFactory)

	// AI 助手：知识库 RAG + 用户画像 + 对话会话。
	// 共享 LLM 客户端：对话与画像学习使用同一配置（ai.diagnosis.*）。
	kbRepo := kbrepo.New(deps.DBPool.Pool)
	profileRepo := userprofilerepo.New(deps.DBPool.Pool)
	chatRepo := chatrepo.New(deps.DBPool.Pool)
	chatFactory := newLLMChatFactory(systemSvc)
	lazyChat := newLazyChatClient(chatFactory)
	kbSvc := kbapp.New(kbRepo, systemSvc)
	profileSvc := userprofileapp.New(profileRepo, lazyChat)
	chatSvc := chatapp.New(chatRepo, lazyChat)
	kbHandler := kbhttp.NewHandler(kbSvc)
	profileHandler := userprofilehttp.NewHandler(profileSvc)
	chatHandler := chathttp.NewHandler(chatSvc)

	diagnosisSvc := diagnosisapp.New(clusterSvc, systemSvc)
	// 注入工具提供者：启用 AI 助手的意图识别 + 工具调用能力
	// （获取构建日志、Pod 日志、事件等）。jenkinsFactory 与 buildHandler 共用。
	diagnosisSvc = diagnosisSvc.WithTools(newToolProvider(buildSvc, appSvc, clusterSvc, connector.JenkinsClient))
	// 注入知识库 / 用户画像 / 对话会话：启用 RAG 检索、个性化回答、多轮上下文持久化。
	diagnosisSvc = diagnosisSvc.
		WithKB(newKBSearcherAdapter(kbSvc)).
		WithProfiler(newProfilerAdapter(profileSvc)).
		WithSessionManager(newSessionManagerAdapter(chatSvc))
	diagnosisHandler := diagnosishttp.NewHandler(diagnosisSvc)
	monitoringSvc.StartAlertEvaluator(context.Background(), 2*time.Minute)
	configSvc := configapp.New(configRepo)
	releaseSvc := releaseapp.New(releaseRepo, appSvc, buildSvc, configSvc, clusterRepo, clusterSvc, clusterSvc, clusterSvc, buildRepo).
		WithOrchestrationRepo(releaseRepo).
		WithWindowChecker(releaseapp.NewWindowChecker(releaseRepo)).
		WithAppProbeResolver(appSvc).
		WithDynamicClientProvider(clusterSvc)
	dnsRepo := dnsrepo.New(deps.DBPool.Pool)
	dnsSvc := dnsapp.New(dnsRepo, clusterSvc)
	releaseSvc.WithDNSMapper(dnsSvc)
	dnsHandler := dnshttp.NewHandler(dnsSvc)
	// 审批服务 + 桥接器：发布审批通过后继续执行挂起的发布。
	approvalRepo := approvalrepo.New(deps.DBPool.Pool)
	approvalSvc := approvalapp.New(approvalRepo)
	approvalBridge := releaseapp.NewReleaseApprovalBridge(appSvc, appSvc, approvalSvc)
	releaseapp.SetExecutePendingReleaseFn(func(ctx context.Context, releaseID, approverID int64) error {
		return releaseSvc.ExecutePendingRelease(ctx, releaseID, approverID)
	})
	releaseSvc.WithApprovalChecker(approvalBridge)
	permCache := permission.New(deps.Redis.Universal)
	rbacSvc := rbacapp.New(rbacRepo, permCache)
	collabSvc := collabapp.New(collabRepo)
	clusterOpsRepo := clusteropsrepo.New(deps.DBPool.Pool)
	clusterOpsSvc := clusteropsapp.New(clusterOpsRepo, clusterSvc, k8sSvc, collabSvc, appRepo)
	// clusterOpsRepo 同时实现 clusterops.Repository 与 clusterops.MetricsRepository；
	// 注入后者以启用节点/Pod 指标采样能力。
	clusterOpsSvc.SetMetricsRepo(clusterOpsRepo)
	clusterOpsScheduler := clusteropsapp.NewScheduler(clusterOpsSvc, clusterOpsRepo, 1*time.Minute)
	go clusterOpsScheduler.Run(context.Background())
	wsHandler := workspacehttp.NewHandler(wsSvc)
	// execSessions/opsSvc 提前创建：appHandler 需注入 opsSvc（Pod 文件浏览器/网络命令）。
	execSessions := k8sexec.NewSessionManager()
	opsSvc := opsapp.New(clusterRepo, deps.Cipher, clusterPool, execSessions, auditSvc).
		WithSessionRepo(opssessionrepo.New(deps.DBPool.Pool)).
		WithLogStore(deps.LogStore).
		WithBehaviorRepo(behaviorauditrepo.New(deps.DBPool.Pool))
	appHandler := applicationhttp.NewHandler(appSvc, clusterSvc, opsSvc)
	clusterHandler := clusterhttp.NewHandler(clusterSvc)
	buildHandler := buildhttp.NewHandler(buildSvc, connector.JenkinsClient, connector.RegistryAdapter)
	releaseHandler := releasehttp.NewHandler(releaseSvc)
	approvalHandler := approvalhttp.NewHandler(approvalSvc)
	// 堡垒机（JumpServer 集成）：通过 provider 按需从系统设置读取配置，
	// 配置变更后下次调用即生效，无需重启 apiserver。
	jmsProvider := bastionapp.NewJMSClientProvider(systemSvc)
	bastionRepo := bastionrepo.New(deps.DBPool.Pool)
	bastionSvc := bastionapp.New(bastionRepo, jmsProvider, &bastionAuditRecorder{auditSvc: auditSvc})
	bastionHandler := bastionhttp.NewHandler(bastionSvc)
	configHandler := confighttp.NewHandler(configSvc)
	rbacHandler := rbachttp.NewHandler(rbacSvc)
	auditHandler := audithttp.NewHandler(auditSvc)
	k8sHandler := k8shttp.NewHandler(k8sSvc, nodePoolSvc, clusterSvc)
	monitoringHandler := monitoringhttp.NewHandler(monitoringSvc)
	collabHandler := collabhttp.NewHandler(collabSvc)
	clusterOpsHandler := clusteropshttp.NewHandler(clusterOpsSvc)
	systemHandler := systemhttp.NewHandler(systemSvc)
	pipeRepo := pipelinerepo.New(deps.DBPool.Pool)
	pipeSvc := pipelineapp.New(pipeRepo, deps.KafkaProducer, deps.Config.Kafka.Brokers, "pipeline", deps.Config.Kafka.TopicPipeline)
	pipeHandler := pipelinehttp.NewHandler(pipeSvc)
	infRepo := inferencerepo.New(deps.DBPool.Pool)
	infSvc := inferenceapp.New(infRepo, clusterPool, clusterSvc, deps.KafkaProducer, deps.Config.Kafka.Brokers, "inference", deps.Config.Kafka.TopicInference)
	infProxy := inferenceapp.NewProxy(infSvc, deps.Redis)
	infHandler := inferencehttp.NewHandler(infSvc, infProxy)
	// 注册阶段执行器并启动 pipeline worker（apiserver 内嵌；大规模可拆 cmd/pipeline-worker）。
	pipeEngine := executor.NewEngine()
	pipeEngine.Register(executor.NewBuildExecutor(buildSvc, connector.JenkinsClient))
	pipeEngine.Register(executor.NewScanExecutor(buildinfra.NewImageScanReader(buildRepo)))
	pipeEngine.Register(executor.NewDeployExecutor(releaseSvc))
	pipeEngine.Register(executor.NewVerifyExecutor(releaseSvc))
	pipeEngine.Register(executor.NewPromoteExecutor(releaseSvc))
	pipeWorker := pipelineworker.New(pipeRepo, pipeEngine, deps.KafkaProducer, deps.Config.Kafka.Brokers, "pipeline", deps.Config.Kafka.TopicPipeline, deps.Logger)
	go pipeWorker.Run(context.Background())

	alertHandler := alerthttp.NewHandler(alertSvc)
	esClient := elasticsearch.New(deps.Config.ES)
	logSvc := logapp.New(esClient)
	logHandler := loghttp.NewHandler(logSvc)
	opsHandler := opshttp.NewHandler(opsSvc)

	extPGRepo := extapirepo.New(deps.DBPool.Pool)
	extIdemStore := extapiredis.NewIdempotencyStore(deps.Redis.Universal)
	extRepo := extapirepo.NewComposite(extPGRepo, extIdemStore)
	extRateLimit := extapiredis.NewRateLimiter(deps.Redis.Universal)
	rtCache := runtime.New(deps.Redis.Universal)
	extSvc := extapiapp.New(
		extRepo, extPGRepo, extRateLimit,
		releaseSvc, releaseRepo, buildSvc, connector.JenkinsClient, buildRepo,
		pipeSvc, infSvc, appSvc, wsSvc, rbacSvc, rtCache, configSvc,
		&podLogAdapter{clusterSvc: clusterSvc},
	)
	extHandler := extapi.NewHandler(extSvc)

	r := chi.NewRouter()
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(recoverMiddleware(deps.Logger))
	r.Use(metrics.Middleware())
	r.Use(requestLogger(deps.Logger))
	// NOTE: chimw.Timeout wraps http.ResponseWriter, breaking WebSocket hijack.
	// Use context-only timeout instead (no response buffering).
	// WebSocket 升级路径（/ops/exec/ws）不设置 120s 超时：长连接会话会被中途取消，
	// 导致交互式终端在 120s 后断开。
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// WebSocket 升级请求跳过请求级超时（连接生命周期由 exec session 自行管理）。
			if isWebSocketUpgrade(r) {
				next.ServeHTTP(w, r)
				return
			}
			// 流式响应请求跳过请求级超时：netcmd 等长命令可能持续数分钟，
			// ctx 超时会在命令执行中途中断 exec 流，导致前端收到截断的输出。
			if isStreamingRequest(r) {
				next.ServeHTTP(w, r)
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type", "X-Request-ID", "Idempotency-Key", "X-Stream"},
		ExposedHeaders:   []string{"X-Request-ID"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	r.Get("/metrics", metrics.Handler().ServeHTTP)

	r.Route("/api/v1", func(r chi.Router) {
		// 健康检查（公开）。
		r.Get("/healthz", healthz)
		r.Get("/readyz", readyzFn(deps.DBPool, deps.Redis))

		// 认证（公开）。
		r.Route("/auth", func(r chi.Router) {
			r.Get("/providers", authHandler.ListLoginProviders)
			r.Post("/register", authHandler.Register)
			r.Post("/login", authHandler.Login)
			r.Post("/login/mfa", authHandler.LoginWithMFA)
			r.Post("/refresh", authHandler.Refresh)
			r.Post("/logout", authHandler.Logout)
		})

		// OpenAI 兼容推理代理（API Key 鉴权，非 JWT）。
		r.Handle("/inference-services/{id}/v1/*", http.HandlerFunc(infHandler.ProxyOpenAI))

		// 需鉴权的路由。
		r.Group(func(r chi.Router) {
			r.Use(httpauth.Middleware(deps.JWT))
			r.Use(auditmw.Middleware(auditSvc))

			// 平台级 WorkspaceResolver：从 chi URL 参数解析 workspace ID（用于 workspace scope 权限校验）。
			// 路径不含 wsId 时返回 0（平台级操作）。
			wsResolver := func(r *http.Request) int64 {
				if v := chi.URLParam(r, "wsId"); v != "" {
					if id, err := strconv.ParseInt(v, 10, 64); err == nil && id > 0 {
						return id
					}
				}
				if v := chi.URLParam(r, "id"); v != "" {
					// /workspaces/{id} 路径：id 即 workspace ID。
					if id, err := strconv.ParseInt(v, 10, 64); err == nil && id > 0 {
						return id
					}
				}
				return 0
			}
			_ = wsResolver // 用于 workspace scope 路由组

			r.Post("/auth/logout-all", authHandler.LogoutAll)
			r.Get("/users/me", authHandler.GetMe)
			r.Post("/users/me/password", authHandler.ChangePassword)

			// MFA (TOTP) 两步验证。
			r.Post("/users/me/mfa/generate", authHandler.GenerateMFA)
			r.Post("/users/me/mfa/enable", authHandler.EnableMFA)
			r.Post("/users/me/mfa/disable", authHandler.DisableMFA)

			// 用户管理（平台级，需 user:manage）。
			r.Group(func(r chi.Router) {
				r.Use(httpauth.RequirePermission(rbacSvc, nil, "user:manage"))
				r.Get("/users", authHandler.ListUsers)
				r.Post("/users", authHandler.CreateUser)
				r.Put("/users/{id}", authHandler.UpdateUser)
				r.Put("/users/{id}/status", authHandler.UpdateUserStatus)
				r.Put("/users/{id}/password", authHandler.ResetUserPassword)
				r.Delete("/users/{id}", authHandler.DeleteUser)
			})

			// 空间（Workspace）。
			r.Get("/workspaces", wsHandler.List)
			r.Get("/workspaces/{id}", wsHandler.Get)
			r.Post("/workspaces", wsHandler.Create)
			r.Group(func(r chi.Router) {
				r.Use(httpauth.RequirePermission(rbacSvc, wsResolver, "workspace:manage"))
				r.Put("/workspaces/{id}", wsHandler.Update)
				r.Delete("/workspaces/{id}", wsHandler.Delete)
				r.Put("/workspaces/{id}/quota", wsHandler.UpdateQuota)
				r.Post("/workspaces/{id}/members", wsHandler.AddMember)
				r.Put("/workspaces/{id}/members/{userId}", wsHandler.UpdateMemberRole)
				r.Delete("/workspaces/{id}/members/{userId}", wsHandler.RemoveMember)
				r.Post("/workspaces/{id}/clusters", wsHandler.AddClusterBinding)
				r.Delete("/workspaces/{id}/clusters/{clusterId}", wsHandler.RemoveClusterBinding)
			})
			r.Get("/workspaces/{id}/quota", wsHandler.GetQuota)
			r.Get("/workspaces/{id}/members", wsHandler.ListMembers)
			r.Get("/workspaces/{id}/clusters", wsHandler.ListClusterBindings)

			// 应用（Application）。
			r.Group(func(r chi.Router) {
				r.Use(httpauth.RequirePermission(rbacSvc, wsResolver, "application:manage"))
				r.Post("/workspaces/{wsId}/applications", appHandler.CreateApplication)
				r.Put("/applications/{appId}", appHandler.UpdateApplication)
				r.Delete("/applications/{appId}", appHandler.DeleteApplication)
				r.Post("/applications/{appId}/members", appHandler.AddAppMember)
				r.Put("/applications/{appId}/members/{userId}", appHandler.UpdateAppMemberRole)
				r.Delete("/applications/{appId}/members/{userId}", appHandler.RemoveAppMember)
			// 分组（Group）管理。
			r.Post("/applications/{appId}/groups", appHandler.CreateGroup)
			r.Put("/groups/{groupId}", appHandler.UpdateGroup)
			r.Post("/groups/{groupId}/scale", appHandler.ScaleGroup)
			r.Post("/groups/{groupId}:restart", appHandler.RestartGroup)
			r.Post("/groups/{groupId}:shutdown", appHandler.ShutdownGroup)
			r.Post("/groups/{groupId}:startup", appHandler.StartupGroup)
			r.Delete("/groups/{groupId}", appHandler.DeleteGroup)
			})
		r.Get("/workspaces/{wsId}/applications", appHandler.ListApplications)
		r.Get("/applications", appHandler.ListAllApplications)
		r.Get("/applications/{appId}", appHandler.GetApplication)
			r.Get("/applications/{appId}/members", appHandler.ListAppMembers)
			r.Get("/applications/{appId}/groups", appHandler.ListGroups)
			r.Get("/groups/{groupId}", appHandler.GetGroup)

			// 分组运维（K8s）：Pod / 日志 / 事件 / YAML（只读，workspace scope）。
			r.Get("/groups/{groupId}/pods", appHandler.ListGroupPods)
			r.Get("/groups/{groupId}/pods/{pod}/logs", appHandler.GetGroupPodLogs)
			r.Post("/groups/{groupId}/pods/{pod}:restart", appHandler.RestartPod)
			r.Get("/groups/{groupId}/pods/{pod}/files", appHandler.ListPodFiles)
			r.Get("/groups/{groupId}/pods/{pod}/files/read", appHandler.ReadPodFile)
			r.Get("/groups/{groupId}/pods/{pod}/files/search-logs", appHandler.SearchPodLogPaths)
			r.Get("/groups/{groupId}/pods/{pod}/files/download", appHandler.DownloadPodFile)
			r.Post("/groups/{groupId}/pods/{pod}/files/upload", appHandler.UploadPodFile)
			r.Delete("/groups/{groupId}/pods/{pod}/files", appHandler.DeletePodFile)
			r.Post("/groups/{groupId}/pods/{pod}/files/cleanup", appHandler.CleanupPodFiles)
			r.Post("/groups/{groupId}/pods/{pod}/netcmd", appHandler.PodNetCmd)
			r.Get("/groups/{groupId}/events", appHandler.ListGroupEvents)
			r.Get("/groups/{groupId}/yaml", appHandler.GetGroupYAML)
			r.Get("/groups/{groupId}/dns", dnsHandler.GetByGroup)
			r.Post("/groups/{groupId}/dns/reconcile", dnsHandler.Reconcile)

			// 集群（Cluster）与凭证（Credential）与 IP 池（平台级）。
			r.Get("/clusters", clusterHandler.List)
			r.Get("/clusters/{id}", clusterHandler.Get)
			r.Get("/clusters/{id}/capacity", clusterHandler.GetCapacity)
			r.Get("/clusters/{id}/ip-pools", clusterHandler.ListIPPools)
			r.Get("/credentials", clusterHandler.ListCredentials)
			r.Group(func(r chi.Router) {
				r.Use(httpauth.RequirePermission(rbacSvc, nil, "cluster:manage"))
				r.Post("/clusters", clusterHandler.Create)
				r.Put("/clusters/{id}", clusterHandler.Update)
				r.Delete("/clusters/{id}", clusterHandler.Delete)
				r.Post("/credentials", clusterHandler.CreateCredential)
				r.Post("/credentials/{id}/rotate", clusterHandler.RotateCredential)
				r.Delete("/credentials/{id}", clusterHandler.DeleteCredential)
				r.Post("/clusters/{id}/ip-pools", clusterHandler.CreateIPPool)
				r.Delete("/ip-pools/{id}", clusterHandler.DeleteIPPool)
			})
		r.Group(func(r chi.Router) {
			r.Use(httpauth.RequirePermission(rbacSvc, nil, "cluster:probe"))
			r.Post("/clusters/{id}/probe", clusterHandler.Probe)
		})

		// 集群监控/运维/通知：节点状态查询/同步、异常资源查询、受影响应用预览、
		// 计划运维任务 CRUD、通知分发。
		r.Group(func(r chi.Router) {
			r.Use(httpauth.RequirePermission(rbacSvc, nil, "k8s:workload:view"))
			r.Get("/clusters/{id}/node-statuses", clusterOpsHandler.ListNodeStatuses)
			r.Get("/clusters/{id}/abnormal-pods", clusterOpsHandler.ListAbnormalPods)
			r.Get("/clusters/{id}/abnormal-nodes", clusterOpsHandler.ListAbnormalNodes)
			r.Get("/clusters/{id}/operations", clusterOpsHandler.ListOperations)
			// 节点/Pod 指标：最新值与时序查询（趋势图）
			r.Get("/clusters/{id}/node-metrics/latest", clusterOpsHandler.ListNodeLatestMetrics)
			r.Get("/clusters/{id}/node-metrics/series", clusterOpsHandler.ListNodeMetricSeries)
			r.Get("/clusters/{id}/pod-metrics/latest", clusterOpsHandler.ListPodLatestMetrics)
			r.Get("/clusters/{id}/pod-metrics/series", clusterOpsHandler.ListPodMetricSeries)
		})
		r.Group(func(r chi.Router) {
			r.Use(httpauth.RequirePermission(rbacSvc, nil, "cluster:probe"))
			r.Post("/clusters/{id}/node-statuses/sync", clusterOpsHandler.SyncNodeStatuses)
			// 手动触发一次指标采集
			r.Post("/clusters/{id}/node-metrics/collect", clusterOpsHandler.CollectNodeMetrics)
		})
		r.Group(func(r chi.Router) {
			r.Use(httpauth.RequirePermission(rbacSvc, nil, "cluster:manage"))
			r.Post("/clusters/{id}/affected-preview", clusterOpsHandler.PreviewAffected)
			r.Post("/clusters/{id}/notify-affected", clusterOpsHandler.NotifyAffected)
			r.Post("/clusters/{id}/operations", clusterOpsHandler.CreateOperation)
			r.Delete("/clusters/{id}/operations/{opId}", clusterOpsHandler.CancelOperation)
		})

			// K8s 运维控制台：节点/工作负载/存储/网络/配置/事件。
			// 只读查询需 k8s:workload:view / k8s:storage:view / k8s:network:view。
			r.Route("/k8s/clusters/{clusterId}", func(r chi.Router) {
				// 节点
				r.Group(func(r chi.Router) {
					r.Use(httpauth.RequirePermission(rbacSvc, nil, "k8s:workload:view"))
					r.Get("/nodes", k8sHandler.ListNodes)
				})
				r.Group(func(r chi.Router) {
					r.Use(httpauth.RequirePermission(rbacSvc, nil, "k8s:node:manage"))
					r.Post("/nodes/{nodeName}/cordon", k8sHandler.CordonNode)
					r.Post("/nodes/{nodeName}/uncordon", k8sHandler.UncordonNode)
					r.Post("/nodes/{nodeName}/drain", k8sHandler.DrainNode)
				})
				// 工作负载
				r.Group(func(r chi.Router) {
					r.Use(httpauth.RequirePermission(rbacSvc, nil, "k8s:workload:view"))
					r.Get("/deployments", k8sHandler.ListDeployments)
					r.Get("/statefulsets", k8sHandler.ListStatefulSets)
					r.Get("/daemonsets", k8sHandler.ListDaemonSets)
					r.Get("/pods", k8sHandler.ListPods)
				})
				r.Group(func(r chi.Router) {
					r.Use(httpauth.RequirePermission(rbacSvc, nil, "k8s:workload:scale"))
					r.Post("/namespaces/{namespace}/deployments/{name}/scale", k8sHandler.ScaleDeployment)
					r.Post("/namespaces/{namespace}/statefulsets/{name}/scale", k8sHandler.ScaleStatefulSet)
				})
				r.Group(func(r chi.Router) {
					r.Use(httpauth.RequirePermission(rbacSvc, nil, "k8s:workload:delete"))
					r.Delete("/namespaces/{namespace}/pods/{name}", k8sHandler.DeletePod)
				})
				// 存储
				r.Group(func(r chi.Router) {
					r.Use(httpauth.RequirePermission(rbacSvc, nil, "k8s:storage:view"))
					r.Get("/persistentvolumes", k8sHandler.ListPersistentVolumes)
					r.Get("/persistentvolumeclaims", k8sHandler.ListPersistentVolumeClaims)
					r.Get("/storageclasses", k8sHandler.ListStorageClasses)
				})
				// 网络
				r.Group(func(r chi.Router) {
					r.Use(httpauth.RequirePermission(rbacSvc, nil, "k8s:network:view"))
					r.Get("/services", k8sHandler.ListServices)
					r.Get("/ingresses", k8sHandler.ListIngresses)
					r.Get("/networkpolicies", k8sHandler.ListNetworkPolicies)
				})
				// 配置（ConfigMap/Secret）
				r.Group(func(r chi.Router) {
					r.Use(httpauth.RequirePermission(rbacSvc, nil, "k8s:configmap:manage"))
					r.Get("/configmaps", k8sHandler.ListConfigMaps)
					r.Get("/secrets", k8sHandler.ListSecrets)
				})
				// 事件
				r.Group(func(r chi.Router) {
					r.Use(httpauth.RequirePermission(rbacSvc, nil, "k8s:workload:view"))
					r.Get("/events", k8sHandler.ListEvents)
				})
				// 云节点池扩缩容
				r.Group(func(r chi.Router) {
					r.Use(httpauth.RequirePermission(rbacSvc, nil, "cluster:nodepool:scale"))
					r.Get("/node-pools/{nodePoolId}", k8sHandler.GetNodePool)
					r.Post("/node-pools/{nodePoolId}/scale", k8sHandler.ScaleNodePool)
				})
			})

			// 构建与镜像：Git 源、构建、日志、仓库、Jenkins、基础镜像、模板、制品。
			r.Get("/applications/{appId}/git-sources", buildHandler.ListGitSources)
			// Git 远程操作：基于应用 git_url 列分支、获取 commit。
			r.Get("/applications/{appId}/git/refs", buildHandler.ListGitRefs)
			r.Get("/applications/{appId}/git/commit", buildHandler.GetGitCommit)
			r.Get("/applications/{appId}/builds", buildHandler.ListBuilds)
			r.Get("/builds/{id}", buildHandler.GetBuild)
			r.Get("/builds/{id}/steps", buildHandler.ListBuildSteps)
			r.Get("/builds/{id}/logs", buildHandler.GetBuildLogs)
			r.Get("/registries", buildHandler.ListRegistries)
			r.Get("/registries/{id}", buildHandler.GetRegistry)
			r.Get("/jenkins-instances", buildHandler.ListJenkins)
			r.Get("/jenkins-instances/{id}", buildHandler.GetJenkins)
			// 构建集成（系统变量化）：应用详情页读取默认 Jenkins/Registry 配置。
			r.Get("/system-settings/build-integration", buildHandler.GetBuildIntegration)
			r.Get("/base-images", buildHandler.ListBaseImages)
			r.Get("/base-images/{id}", buildHandler.GetBaseImage)
			r.Get("/build-tools", buildHandler.ListBuildTools)
			r.Get("/build-tools/{id}", buildHandler.GetBuildTool)
			r.Get("/build-templates", buildHandler.ListTemplates)
			r.Get("/applications/{appId}/images", buildHandler.ListImages)
			r.Get("/images/{id}", buildHandler.GetImage)
			r.Get("/applications/{appId}/image-tags", buildHandler.ListImageTags)
			r.Group(func(r chi.Router) {
				r.Use(httpauth.RequirePermission(rbacSvc, wsResolver, "build:trigger"))
				r.Post("/applications/{appId}/git-sources", buildHandler.CreateGitSource)
				r.Post("/applications/{appId}/builds", buildHandler.TriggerBuild)
				r.Post("/builds/{id}/rebuild", buildHandler.RebuildBuild)
				r.Post("/applications/{appId}/image-tags", buildHandler.CreateImageTag)
			})
			r.Group(func(r chi.Router) {
				r.Use(httpauth.RequirePermission(rbacSvc, wsResolver, "build:cancel"))
				r.Delete("/git-sources/{id}", buildHandler.DeleteGitSource)
				r.Post("/builds/{id}/cancel", buildHandler.CancelBuild)
				r.Put("/builds/{id}", buildHandler.UpdateBuild)
				r.Delete("/builds/{id}", buildHandler.DeleteBuild)
				r.Post("/images/{id}/retire", buildHandler.RetireImage)
				r.Put("/image-tags/{id}", buildHandler.UpdateImageTag)
				r.Delete("/image-tags/{id}", buildHandler.DeleteImageTag)
			})
			r.Group(func(r chi.Router) {
				r.Use(httpauth.RequirePermission(rbacSvc, nil, "system:settings:write"))
				r.Post("/registries", buildHandler.CreateRegistry)
				r.Put("/registries/{id}", buildHandler.UpdateRegistry)
				r.Delete("/registries/{id}", buildHandler.DeleteRegistry)
				r.Post("/registries/test", buildHandler.TestRegistryConnection)
				r.Post("/jenkins-instances", buildHandler.CreateJenkins)
				r.Put("/jenkins-instances/{id}", buildHandler.UpdateJenkins)
				r.Delete("/jenkins-instances/{id}", buildHandler.DeleteJenkins)
				r.Post("/jenkins-instances/test", buildHandler.TestJenkinsConnection)
			r.Post("/base-images", buildHandler.CreateBaseImage)
			r.Put("/base-images/{id}", buildHandler.UpdateBaseImage)
			r.Delete("/base-images/{id}", buildHandler.DeleteBaseImage)
			r.Post("/build-tools", buildHandler.CreateBuildTool)
			r.Put("/build-tools/{id}", buildHandler.UpdateBuildTool)
			r.Delete("/build-tools/{id}", buildHandler.DeleteBuildTool)
			r.Post("/build-templates", buildHandler.CreateTemplate)
			})

			// 系统设置（公开项所有登录用户可读；全部项与写入仅管理员）。
			r.Get("/system-settings", systemHandler.ListPublic)
			r.Get("/system-settings/{key}", systemHandler.Get)
			r.Group(func(r chi.Router) {
				r.Use(httpauth.RequirePermission(rbacSvc, nil, "system:settings:write"))
				r.Get("/system-settings/all", systemHandler.ListAll)
				r.Put("/system-settings/{key}", systemHandler.Update)
			})

			// 发布与配置：发布、回滚、事件、批次、预设、窗口；配置版本、ConfigSet、绑定、diff。
			r.Get("/groups/{groupId}/releases", releaseHandler.ListReleases)
			r.Get("/releases/{id}", releaseHandler.GetRelease)
			r.Get("/releases/{id}/events", releaseHandler.ListReleaseEvents)
			r.Get("/releases/{id}/batches", releaseHandler.ListBatchRecords)
			r.Get("/release-presets", releaseHandler.ListPresets)
			r.Get("/applications/{appId}/release-windows", releaseHandler.ListWindows)
			r.Get("/applications/{appId}/orchestrations", releaseHandler.ListOrchestrations)
			r.Get("/orchestrations/{id}", releaseHandler.GetOrchestration)
			r.Get("/configs", configHandler.ListConfigs)
			r.Get("/configs/{id}", configHandler.GetConfig)
			r.Get("/configs/diff", configHandler.DiffConfigs)
			r.Get("/configs/diff-cross-group", configHandler.DiffCrossGroup)
			r.Get("/workspaces/{wsId}/config-sets", configHandler.ListConfigSets)
			r.Get("/applications/{appId}/config-sets", configHandler.ListConfigSetsByApplication)
			r.Get("/config-sets/{id}", configHandler.GetConfigSet)
			r.Get("/config-sets/{id}/snapshots", configHandler.ListConfigSetSnapshots)
			r.Get("/config-snapshots/{id}/diff", configHandler.DiffConfigFile)
			r.Get("/groups/{groupId}/config-bindings", configHandler.ListBindings)
			r.Get("/groups/{groupId}/local-config", configHandler.GetLocalConfig)
			r.Get("/groups/{groupId}/local-config/snapshots", configHandler.ListLocalConfigSnapshots)
			r.Get("/groups/{groupId}/config-bind-snapshots", configHandler.ListGroupBindSnapshots)
			r.Get("/groups/{groupId}/config/files", configHandler.ListGroupConfigFiles)
			r.Group(func(r chi.Router) {
				r.Use(httpauth.RequirePermission(rbacSvc, wsResolver, "release:trigger"))
				r.Post("/groups/{groupId}/releases", releaseHandler.TriggerRelease)
				r.Post("/release-presets", releaseHandler.CreatePreset)
				r.Post("/applications/{appId}/release-windows", releaseHandler.CreateWindow)
			})
			r.Group(func(r chi.Router) {
				r.Use(httpauth.RequirePermission(rbacSvc, wsResolver, "release:orch:create"))
				r.Post("/applications/{appId}/multi-release", releaseHandler.TriggerOrchestration)
			})
			r.Group(func(r chi.Router) {
				r.Use(httpauth.RequirePermission(rbacSvc, wsResolver, "release:orch:abort"))
				r.Post("/orchestrations/{id}/abort", releaseHandler.AbortOrchestration)
			})
			r.Group(func(r chi.Router) {
				r.Use(httpauth.RequirePermission(rbacSvc, wsResolver, "release:rollback"))
				r.Post("/groups/{groupId}/rollback", releaseHandler.Rollback)
			})
			r.Group(func(r chi.Router) {
				r.Use(httpauth.RequirePermission(rbacSvc, wsResolver, "release:abort"))
				r.Post("/releases/{id}/abort", releaseHandler.AbortRelease)
			})
			r.Group(func(r chi.Router) {
				r.Use(httpauth.RequirePermission(rbacSvc, wsResolver, "release:pause"))
				r.Post("/releases/{id}/pause", releaseHandler.PauseRelease)
				r.Post("/releases/{id}/resume", releaseHandler.ResumeRelease)
			})
			// 审批：列表/详情只读，批准/拒绝需 release:approve。
			r.Get("/approvals", approvalHandler.ListApprovals)
			r.Get("/approvals/{id}", approvalHandler.GetApproval)
			r.Group(func(r chi.Router) {
				r.Use(httpauth.RequirePermission(rbacSvc, wsResolver, "release:approve"))
				r.Post("/approvals/{id}/approve", approvalHandler.Approve)
				r.Post("/approvals/{id}/reject", approvalHandler.Reject)
			})
			// 堡垒机（JumpServer 集成）已下线：WebSSH 改用自研 exec WS（含录像+审计）。
			// 路由不再注册；相关 service/handler/repo 代码保留但不挂载，避免大范围删除破坏编译。
			_ = bastionHandler
			// r.Get("/bastion/assets", bastionHandler.ListAssets)
			// r.Get("/bastion/assets/{id}", bastionHandler.GetAsset)
			// r.Get("/bastion/sessions", bastionHandler.ListSessions)
			// r.Get("/bastion/sessions/{id}/replay", bastionHandler.GetReplay)
			// r.Group(func(r chi.Router) {
			// 	r.Use(httpauth.RequirePermission(rbacSvc, wsResolver, "bastion:asset:manage"))
			// 	r.Post("/bastion/assets", bastionHandler.CreateAsset)
			// 	r.Put("/bastion/assets/{id}", bastionHandler.UpdateAsset)
			// 	r.Delete("/bastion/assets/{id}", bastionHandler.DeleteAsset)
			// })
			// r.Group(func(r chi.Router) {
			// 	r.Use(httpauth.RequirePermission(rbacSvc, wsResolver, "bastion:asset:connect"))
			// 	r.Post("/bastion/assets/{id}/connect", bastionHandler.Connect)
			// })
			// r.Group(func(r chi.Router) {
			// 	r.Use(httpauth.RequirePermission(rbacSvc, wsResolver, "bastion:sync"))
			// 	r.Post("/bastion/sync", bastionHandler.SyncAssets)
			// })
			r.Group(func(r chi.Router) {
				r.Use(httpauth.RequirePermission(rbacSvc, wsResolver, "release:approve"))
				r.Delete("/release-presets/{id}", releaseHandler.DeletePreset)
				r.Delete("/release-windows/{id}", releaseHandler.DeleteWindow)
			})
			r.Group(func(r chi.Router) {
				r.Use(httpauth.RequirePermission(rbacSvc, wsResolver, "config:manage"))
				r.Post("/configs", configHandler.CreateConfig)
				r.Post("/configs/{id}/archive", configHandler.ArchiveConfig)
				r.Post("/workspaces/{wsId}/config-sets", configHandler.CreateConfigSet)
				r.Post("/applications/{appId}/config-sets", configHandler.CreateConfigSetByApplication)
				r.Put("/config-sets/{id}", configHandler.UpdateConfigSet)
				r.Delete("/config-sets/{id}", configHandler.DeleteConfigSet)
			r.Post("/groups/{groupId}/config-bindings", configHandler.CreateBinding)
			r.Delete("/config-bindings/{id}", configHandler.DeleteBinding)
			r.Put("/groups/{groupId}/local-config", configHandler.UpsertLocalConfig)
			r.Post("/groups/{groupId}/local-config/clone-from", configHandler.CloneLocalConfigFromGroup)
			r.Delete("/groups/{groupId}/local-config", configHandler.DeleteLocalConfig)
			})

			// 权限/菜单/角色：CRUD + 用户可见菜单树 + 角色绑定 + workspace 成员。
			r.Get("/permissions", rbacHandler.ListPermissions)
			r.Get("/menus", rbacHandler.ListMenus)
			r.Get("/me/menus", rbacHandler.GetMyMenuTree)
			r.Get("/roles", rbacHandler.ListRoles)
			r.Get("/roles/{id}/permissions", rbacHandler.ListPermissionsByRole)
			r.Get("/roles/{id}/menus", rbacHandler.ListMenusByRole)
			r.Get("/users/{userId}/platform-roles", rbacHandler.ListPlatformRolesByUser)
			r.Get("/workspaces/{wsId}/members", rbacHandler.ListWorkspaceMembers)
			r.Group(func(r chi.Router) {
				r.Use(httpauth.RequirePermission(rbacSvc, nil, "rbac:manage"))
				r.Post("/permissions", rbacHandler.CreatePermission)
				r.Delete("/permissions/{id}", rbacHandler.DeletePermission)
				r.Post("/menus", rbacHandler.CreateMenu)
				r.Delete("/menus/{id}", rbacHandler.DeleteMenu)
				r.Post("/roles", rbacHandler.CreateRole)
				r.Delete("/roles/{id}", rbacHandler.DeleteRole)
				r.Post("/roles/{id}/permissions", rbacHandler.GrantPermissions)
				r.Post("/roles/{id}/menus", rbacHandler.BindRoleMenus)
				r.Post("/users/{userId}/platform-roles", rbacHandler.BindPlatformRole)
				r.Post("/workspaces/{wsId}/members", rbacHandler.AddWorkspaceMember)
				r.Delete("/workspace-members/{id}", rbacHandler.RemoveWorkspaceMember)
			})

			// 审计日志与通知。
			r.Get("/audit-logs", auditHandler.ListAuditLogs)
			r.Get("/audit-logs/{id}", auditHandler.GetAuditLog)
			r.Get("/notifications", collabHandler.ListNotifications)
			r.Get("/notifications/unread-count", collabHandler.CountUnread)
			r.Post("/notifications/read-all", collabHandler.MarkAllRead)

			// 流水线：定义 CRUD、运行触发/取消/查询、晋升、制品签名。
			r.Get("/pipelines", pipeHandler.ListPipelines)
			r.Get("/pipelines/{id}", pipeHandler.GetPipeline)
			r.Get("/pipeline-runs", pipeHandler.ListRuns)
			r.Get("/pipeline-runs/{id}", pipeHandler.GetRun)
			r.Get("/promotions", pipeHandler.ListPromotions)
			r.Get("/images/{id}/signature", pipeHandler.GetSignature)
			r.Group(func(r chi.Router) {
				r.Use(httpauth.RequirePermission(rbacSvc, wsResolver, "pipeline:manage"))
				r.Post("/pipelines", pipeHandler.CreatePipeline)
				r.Delete("/pipelines/{id}", pipeHandler.DeletePipeline)
				r.Post("/pipelines/{id}/runs", pipeHandler.TriggerRun)
				r.Post("/promotions", pipeHandler.CreatePromotion)
				r.Post("/artifacts/signatures", pipeHandler.RecordSignature)
			})
			r.Group(func(r chi.Router) {
				r.Use(httpauth.RequirePermission(rbacSvc, wsResolver, "pipeline:cancel"))
				r.Post("/pipeline-runs/{id}/cancel", pipeHandler.CancelRun)
			})

			// 可观测与运维：告警规则/事件、Pod exec/端口转发、日志搜索、监控查询。
			r.Get("/alert-rules", alertHandler.ListRules)
			r.Get("/alert-rules/{id}", alertHandler.GetRule)
			r.Get("/alert-events", alertHandler.ListEvents)
			r.Get("/alert-events/{id}", alertHandler.GetEvent)

			// 监控查询（Prometheus）：所有登录用户可查询，规则评估需管理员。
			r.Post("/monitoring/query", monitoringHandler.Query)
			r.Post("/monitoring/query-range", monitoringHandler.QueryRange)
			r.Get("/ops/sessions", opsHandler.ListSessions)
			r.Get("/logs/search", logHandler.Search)
			r.Get("/logs/audit-search", logHandler.SearchAudit)
			r.Get("/logs/stream", logHandler.Stream)
			r.Group(func(r chi.Router) {
				r.Use(httpauth.RequirePermission(rbacSvc, nil, "system:settings:write"))
				r.Post("/alert-rules", alertHandler.CreateRule)
				r.Put("/alert-rules/{id}", alertHandler.UpdateRule)
				r.Delete("/alert-rules/{id}", alertHandler.DeleteRule)
				r.Post("/monitoring/evaluate-rules", monitoringHandler.EvaluateRules)
			})
		r.Group(func(r chi.Router) {
			r.Use(httpauth.RequirePermission(rbacSvc, wsResolver, "ops:exec"))
			r.Post("/ops/exec", opsHandler.Exec)
			// WebSSH 交互式终端（WebSocket）：通过 ?token=<jwt> 鉴权。
			r.Get("/ops/exec/ws", opsHandler.ExecWS)
		})
		r.Group(func(r chi.Router) {
			r.Use(httpauth.RequirePermission(rbacSvc, wsResolver, "ops:portforward"))
			r.Post("/ops/port-forward", opsHandler.PortForward)
			r.Delete("/ops/sessions/{id}", opsHandler.CloseSession)
		})
		// 运维会话历史与录像回放。
		r.Get("/ops/sessions/history", opsHandler.ListOpsSessions)
		r.Get("/ops/sessions/history/{id}", opsHandler.GetOpsSession)
		r.Group(func(r chi.Router) {
			r.Use(httpauth.RequirePermission(rbacSvc, wsResolver, "ops:session:view"))
			r.Get("/ops/sessions/history/{id}/replay", opsHandler.ReplaySession)
			// 会话录像 cast 文件流式下载（asciinema-player 在线播放源）。
			r.Get("/ops/sessions/history/{id}/cast", opsHandler.CastDownload)
		})
		// 行为审计（WebSSH 命令捕获）。
		r.Get("/audit/behavior", opsHandler.ListBehaviorAudit)

		// AI 诊断：收集 K8s 上下文 → 调用可配置 LLM → 根因分析。
		// 上下文诊断（原 Analyze）与日志诊断（构建/启动失败时调用）走同一权限。
		// Chat/FAQ 对所有已登录用户开放（无需 menu:diagnosis:view），便于全局 AI 助手浮窗使用。
		r.Group(func(r chi.Router) {
			r.Use(httpauth.RequirePermission(rbacSvc, nil, "menu:diagnosis:view"))
			r.Post("/diagnosis/analyze", diagnosisHandler.Analyze)
			r.Post("/diagnosis/analyze-logs", diagnosisHandler.AnalyzeLogs)
			r.Post("/diagnosis/analyze-logs/stream", diagnosisHandler.AnalyzeLogsStream)
		})
		r.Post("/diagnosis/chat", diagnosisHandler.Chat)
		r.Post("/diagnosis/chat/stream", diagnosisHandler.ChatStream)
		r.Get("/diagnosis/faq", diagnosisHandler.ListFAQ)

		// AI 助手 - 对话会话：所有已登录用户可管理自己的会话。
		r.Get("/chat/sessions", chatHandler.ListSessions)
		r.Post("/chat/sessions", chatHandler.CreateSession)
		r.Get("/chat/sessions/{id}", chatHandler.GetSession)
		r.Delete("/chat/sessions/{id}", chatHandler.DeleteSession)
		r.Get("/chat/sessions/{id}/messages", chatHandler.ListMessages)

		// AI 助手 - 用户画像：所有已登录用户可查看/更新自己的画像。
		r.Get("/user-profile", profileHandler.GetProfile)
		r.Put("/user-profile", profileHandler.UpdateProfile)

		// AI 助手 - 知识库：查看对所有登录用户开放，管理需 kb:manage 权限。
		r.Get("/kb/categories", kbHandler.ListCategories)
		r.Get("/kb/documents", kbHandler.ListDocuments)
		r.Get("/kb/documents/{id}", kbHandler.GetDocument)
		r.Post("/kb/search", kbHandler.Search)
		r.Group(func(r chi.Router) {
			r.Use(httpauth.RequirePermission(rbacSvc, nil, "kb:manage"))
			r.Post("/kb/documents", kbHandler.CreateDocument)
			r.Put("/kb/documents/{id}", kbHandler.UpdateDocument)
			r.Delete("/kb/documents/{id}", kbHandler.DeleteDocument)
			r.Post("/kb/documents/{id}/reindex", kbHandler.ReindexDocument)
		})

			// 大模型推理：模型仓库/版本/适配器、推理服务、发布、API Key、路由、计量。
			r.Get("/model-registries", infHandler.ListRegistries)
			r.Get("/model-registries/{id}", infHandler.GetRegistry)
			r.Get("/models", infHandler.ListModels)
			r.Get("/models/{id}", infHandler.GetModel)
			r.Get("/models/{modelId}/versions", infHandler.ListModelVersions)
			r.Get("/model-versions/{id}", infHandler.GetModelVersion)
			r.Get("/model-adapters", infHandler.ListAdapters)
			r.Get("/model-adapters/{id}", infHandler.GetAdapter)
			r.Get("/inference-services", infHandler.ListServices)
			r.Get("/inference-services/{id}", infHandler.GetService)
			r.Get("/inference-releases", infHandler.ListReleases)
			r.Get("/inference-releases/{id}", infHandler.GetRelease)
			r.Get("/inference-services/{id}/api-keys", infHandler.ListAPIKeys)
			r.Get("/inference-routes", infHandler.ListRoutes)
			r.Get("/inference-routes/{id}", infHandler.GetRoute)
			r.Get("/inference-usage", infHandler.ListUsage)
			r.Get("/inference-usage/summary", infHandler.SummarizeUsage)
			r.Group(func(r chi.Router) {
				r.Use(httpauth.RequirePermission(rbacSvc, wsResolver, "inference:manage"))
				r.Post("/model-registries", infHandler.CreateRegistry)
				r.Delete("/model-registries/{id}", infHandler.DeleteRegistry)
				r.Post("/models", infHandler.CreateModel)
				r.Delete("/models/{id}", infHandler.DeleteModel)
			r.Post("/models/{modelId}/versions", infHandler.CreateModelVersion)
			r.Delete("/model-versions/{id}", infHandler.DeleteModelVersion)
			r.Post("/model-versions/{id}/download", infHandler.DownloadModelVersion)
				r.Post("/model-adapters", infHandler.CreateAdapter)
				r.Delete("/model-adapters/{id}", infHandler.DeleteAdapter)
				r.Post("/inference-services", infHandler.CreateService)
				r.Delete("/inference-services/{id}", infHandler.DeleteService)
				r.Post("/inference-services/{id}/api-keys", infHandler.CreateAPIKey)
				r.Post("/inference-api-keys/{keyId}/revoke", infHandler.RevokeAPIKey)
				r.Post("/inference-routes", infHandler.CreateRoute)
				r.Put("/inference-routes/{id}", infHandler.UpdateRoute)
				r.Delete("/inference-routes/{id}", infHandler.DeleteRoute)
			})
			r.Group(func(r chi.Router) {
				r.Use(httpauth.RequirePermission(rbacSvc, wsResolver, "inference:deploy"))
				r.Post("/inference-services/{id}/deploy", infHandler.DeployService)
				r.Post("/inference-services/{id}/scale", infHandler.ScaleService)
				r.Post("/inference-services/{id}/rollback", infHandler.RollbackService)
			})
		})
	})

	// 对外 API（独立鉴权：Bearer voe_ Token，不走 JWT）。
	r.Route("/api/v1/ext", func(r chi.Router) {
		r.Use(extapi.AuthMiddleware(extSvc))
		r.Use(extapi.AuditMiddleware(extSvc))
		r.Use(extapi.IdempotencyMiddleware(extSvc))

		r.With(extapi.WithOperation("workspace.create")).Post("/workspaces", extHandler.SelfCreateWorkspace)

		r.Route("/workspaces/{wsUuid}", func(r chi.Router) {
			r.With(extapi.WithOperation("deploy"), extapi.RequireScope(extapi.ScopeDeploy)).
				Post("/groups/{groupUuid}:deploy", extHandler.Deploy)
			r.With(extapi.WithOperation("scale"), extapi.RequireScope(extapi.ScopeScale)).
				Post("/groups/{groupUuid}:scale", extHandler.ScaleGroup)
			r.With(extapi.WithOperation("rollback"), extapi.RequireScope(extapi.ScopeRollback)).
				Post("/groups/{groupUuid}:rollback", extHandler.Rollback)
			r.With(extapi.WithOperation("release.current"), extapi.RequireScope(extapi.ScopeStatus)).
				Get("/groups/{groupUuid}/releases/current", extHandler.GetCurrentRelease)
			r.With(extapi.WithOperation("group.status"), extapi.RequireScope(extapi.ScopeStatus)).
				Get("/groups/{groupUuid}", extHandler.GetGroupStatus)
			r.With(extapi.WithOperation("group.pods"), extapi.RequireScope(extapi.ScopeStatus)).
				Get("/groups/{groupUuid}/pods", extHandler.ListGroupPods)

			r.With(extapi.WithOperation("build.trigger"), extapi.RequireScope(extapi.ScopeBuild)).
				Post("/applications/{appUuid}:build", extHandler.TriggerBuild)
			r.With(extapi.WithOperation("build.get"), extapi.RequireScope(extapi.ScopeBuild)).
				Get("/builds/{buildUuid}", extHandler.GetBuild)

			r.With(extapi.WithOperation("pipeline.trigger"), extapi.RequireScope(extapi.ScopePipeline)).
				Post("/pipelines/{pipelineUuid}:trigger", extHandler.TriggerPipeline)
			r.With(extapi.WithOperation("pipeline.run"), extapi.RequireScope(extapi.ScopePipeline)).
				Get("/pipeline-runs/{runUuid}", extHandler.GetPipelineRun)

			r.With(extapi.WithOperation("inference.deploy"), extapi.RequireScope(extapi.ScopeInference)).
				Post("/inference-services", extHandler.DeployInference)
			r.With(extapi.WithOperation("inference.scale"), extapi.RequireScope(extapi.ScopeInference)).
				Post("/inference-services/{svcUuid}:scale", extHandler.ScaleInference)
			r.With(extapi.WithOperation("inference.get"), extapi.RequireScope(extapi.ScopeInference)).
				Get("/inference-services/{svcUuid}", extHandler.GetInferenceService)

			r.With(extapi.WithOperation("middleware.deploy"), extapi.RequireScope(extapi.ScopeMiddleware)).
				Post("/middleware-deployments", extHandler.DeployMiddleware)
			r.With(extapi.WithOperation("middleware.update"), extapi.RequireScope(extapi.ScopeMiddleware)).
				Patch("/middleware-deployments/{appUuid}", extHandler.UpdateMiddleware)
			r.With(extapi.WithOperation("middleware.scale"), extapi.RequireScope(extapi.ScopeMiddleware)).
				Post("/middleware-deployments/{appUuid}:scale", extHandler.ScaleMiddleware)
			r.With(extapi.WithOperation("middleware.stop"), extapi.RequireScope(extapi.ScopeMiddleware)).
				Post("/middleware-deployments/{appUuid}:stop", extHandler.StopMiddleware)
			r.With(extapi.WithOperation("middleware.start"), extapi.RequireScope(extapi.ScopeMiddleware)).
				Post("/middleware-deployments/{appUuid}:start", extHandler.StartMiddleware)
			r.With(extapi.WithOperation("middleware.rollback"), extapi.RequireScope(extapi.ScopeMiddleware)).
				Post("/middleware-deployments/{appUuid}:rollback", extHandler.RollbackMiddleware)
			r.With(extapi.WithOperation("middleware.delete"), extapi.RequireScope(extapi.ScopeMiddleware)).
				Delete("/middleware-deployments/{appUuid}", extHandler.DeleteMiddleware)

			r.With(extapi.WithOperation("middleware.status"), extapi.RequireScope(extapi.ScopeMiddleware)).
				Get("/middleware-deployments/{appUuid}/status", extHandler.GetMiddlewareStatus)
			r.With(extapi.WithOperation("middleware.pods"), extapi.RequireScope(extapi.ScopeMiddleware)).
				Get("/middleware-deployments/{appUuid}/pods", extHandler.ListMiddlewarePods)
			r.With(extapi.WithOperation("middleware.pod.logs"), extapi.RequireScope(extapi.ScopeMiddleware)).
				Get("/middleware-deployments/{appUuid}/pods/{pod}/logs", extHandler.GetMiddlewarePodLogs)
			r.With(extapi.WithOperation("middleware.releases"), extapi.RequireScope(extapi.ScopeMiddleware)).
				Get("/middleware-deployments/{appUuid}/releases", extHandler.ListMiddlewareReleases)
			r.With(extapi.WithOperation("middleware.release.current"), extapi.RequireScope(extapi.ScopeMiddleware)).
				Get("/middleware-deployments/{appUuid}/releases/current", extHandler.GetCurrentMiddlewareRelease)

			r.With(extapi.WithOperation("middleware.members.list"), extapi.RequireScope(extapi.ScopeMiddleware)).
				Get("/middleware-deployments/{appUuid}/members", extHandler.ListMiddlewareMembers)
			r.With(extapi.WithOperation("middleware.members.add"), extapi.RequireScope(extapi.ScopeMiddleware)).
				Post("/middleware-deployments/{appUuid}/members", extHandler.AddMiddlewareMember)
			r.With(extapi.WithOperation("middleware.members.update"), extapi.RequireScope(extapi.ScopeMiddleware)).
				Put("/middleware-deployments/{appUuid}/members/{userId}", extHandler.UpdateMiddlewareMemberRole)
			r.With(extapi.WithOperation("middleware.members.remove"), extapi.RequireScope(extapi.ScopeMiddleware)).
				Delete("/middleware-deployments/{appUuid}/members/{userId}", extHandler.RemoveMiddlewareMember)

			r.With(extapi.WithOperation("middleware.images.list"), extapi.RequireScope(extapi.ScopeMiddleware)).
				Get("/middleware-deployments/{appUuid}/images", extHandler.ListMiddlewareImages)
			r.With(extapi.WithOperation("middleware.images.retire"), extapi.RequireScope(extapi.ScopeMiddleware)).
				Delete("/middleware-deployments/{appUuid}/images/{imageId}", extHandler.RetireMiddlewareImage)
		})
	})

	srv := &http.Server{
		Addr:              deps.Config.Server.Addr,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       deps.Config.Server.ReadTimeout,
		WriteTimeout:      deps.Config.Server.WriteTimeout,
		IdleTimeout:       deps.Config.Server.IdleTimeout,
		MaxHeaderBytes:    deps.Config.Server.MaxHeaderBytes,
	}

	return &Server{cfg: deps.Config, log: deps.Logger, http: srv, dbPool: deps.DBPool, redis: deps.Redis}, nil
}

// Start 启动 HTTP server，阻塞直到 ListenAndServe 返回。
func (s *Server) Start() error {
	s.log.Info("http server starting", "addr", s.cfg.Server.Addr)
	if err := s.http.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Shutdown 优雅关停：拒绝新连接，等待 in-flight 请求，关闭 DB/Redis。
func (s *Server) Shutdown(ctx context.Context) error {
	s.log.Info("http server shutting down")
	shutdownCtx, cancel := context.WithTimeout(ctx, s.cfg.Server.ShutdownTimeout)
	defer cancel()
	if err := s.http.Shutdown(shutdownCtx); err != nil {
		s.log.Error("http server shutdown error", "err", err)
	}
	if s.dbPool != nil {
		s.dbPool.Close()
	}
	if s.redis != nil {
		if err := s.redis.Close(); err != nil {
			s.log.Error("redis close error", "err", err)
		}
	}
	s.log.Info("shutdown complete")
	return nil
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func readyzFn(pool *db.Pool, rc *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if err := pool.HealthCheck(ctx); err != nil {
			writeUnready(w, "db: "+err.Error())
			return
		}
		if err := rc.HealthCheck(ctx); err != nil {
			writeUnready(w, "redis: "+err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	}
}

func writeUnready(w http.ResponseWriter, reason string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte(`{"status":"not_ready","reason":"` + reason + `"}`))
}

// isWebSocketUpgrade 判断请求是否为 WebSocket 升级握手。
// 用于在请求级超时中间件中跳过 WS 路径，避免长连接被中途取消。
func isWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") &&
		strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}

// isStreamingRequest 判断是否为流式响应请求（前端显式声明 Accept: text/event-stream
// 或自定义 X-Stream: true，或命中已知流式路由）。
// 这类请求的响应会持续写入，不能套用 120s 请求级超时。
func isStreamingRequest(r *http.Request) bool {
	if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		return true
	}
	if r.Header.Get("X-Stream") == "true" {
		return true
	}
	// netcmd 流式端点。
	if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/netcmd") {
		return true
	}
	return false
}

// podLogAdapter 将 clusterapp.Service 适配为 extapiapp.PodLogStreamer。
type podLogAdapter struct {
	clusterSvc *clusterapp.Service
}

func (a *podLogAdapter) StreamPodLogs(ctx context.Context, in extapiapp.PodLogsInput, out io.Writer) error {
	return a.clusterSvc.StreamPodLogs(ctx, clusterapp.PodLogsInput{
		ClusterID: in.ClusterID, Namespace: in.Namespace, Pod: in.Pod,
		Container: in.Container, TailLines: in.TailLines, Follow: in.Follow,
	}, out)
}
