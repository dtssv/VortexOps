// Package config 加载并校验 VortexOps 平台配置。
// 配置来源优先级：环境变量 > 默认值。敏感配置（DB 密码、JWT 密钥、KMS 密钥）
// 仅来自环境变量或 K8s Secret 注入的环境变量，不落入配置文件。
// 实现刻意保持零外部依赖，避免拉入过多传递依赖。
package config

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config 是平台运行配置的根结构。
type Config struct {
	App         AppConfig
	Server      ServerConfig
	DB          DBConfig
	Redis       RedisConfig
	JWT         JWTConfig
	Security    SecurityConfig
	Log         LogConfig
	S3          S3Config
	Kafka       KafkaConfig
	ES          ElasticsearchConfig
	Integration IntegrationConfig
}

type AppConfig struct {
	Name        string
	Environment string
	Version     string
	InstanceID  string
}

type ServerConfig struct {
	Addr            string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration
	MaxHeaderBytes  int
}

type DBConfig struct {
	Host                string
	Port                int
	Database            string
	Username            string
	Password            string
	SSLMode             string
	MaxConns            int32
	MinConns            int32
	MaxConnLifetime     time.Duration
	MaxConnIdleTime     time.Duration
	ConnectTimeout      time.Duration
	StatementTimeout    time.Duration
	HealthCheckInterval time.Duration
	ReadReplicaHost     string
	ReadReplicaPort     int
	CitusEnabled        bool
}

type RedisConfig struct {
	Addrs          []string
	Username       string
	Password       string
	SentinelMaster string
	DB             int
	PoolSize       int
	MinIdleConns   int
	DialTimeout    time.Duration
	ReadTimeout    time.Duration
	WriteTimeout   time.Duration
	MaxRetries     int
}

type JWTConfig struct {
	SigningKey    string
	Issuer        string
	Audience      string
	AccessTTL     time.Duration
	RefreshTTL    time.Duration
	RefreshRotate bool
}

type SecurityConfig struct {
	EncryptionKey        string
	PasswordMinLength    int
	BcryptCost           int
	SessionTimeout       time.Duration
	MaxLoginAttempts     int
	LoginLockoutDuration time.Duration
}

type LogConfig struct {
	Level  string
	Format string
}

// S3Config 是 MinIO/S3 兼容对象存储配置（构建日志归档、模型权重）。
type S3Config struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	Region    string
	UseSSL    bool
	Enabled   bool
}

// KafkaConfig 异步事件总线配置（segmentio/kafka-go）。
type KafkaConfig struct {
	Brokers        []string
	TopicPipeline  string
	TopicBuild     string
	TopicAudit     string
	TopicInference string
	Enabled        bool
}

// ElasticsearchConfig Elasticsearch 全文检索配置（VORTEXOPS_ES_*）。
type ElasticsearchConfig struct {
	URL        string
	Username   string
	Password   string
	IndexAudit string
	IndexLogs  string
	Timeout    time.Duration
	Enabled    bool
}

// IntegrationConfig 构建集成配置：系统变量化 Jenkins/Harbor/K8s 默认指向。
// apiserver 启动时若检测到这些值非空且 DB 中无默认实例，自动种子一条记录。
type IntegrationConfig struct {
	JenkinsURL     string // VORTEXOPS_JENKINS_URL，默认 http://jenkins:8080
	JenkinsUser    string // VORTEXOPS_JENKINS_USER，默认 admin
	JenkinsPassword string // VORTEXOPS_JENKINS_PASSWORD（开发环境用密码，生产用 API Token）
	HarborURL      string // VORTEXOPS_HARBOR_URL，默认 http://harbor-core:8080
	HarborUser     string // VORTEXOPS_HARBOR_USER，默认 admin
	HarborPassword string // VORTEXOPS_HARBOR_PASSWORD
	K8sAPIServer   string // VORTEXOPS_K8S_API_SERVER，默认 https://k8s:6443
	K8sKubeconfig  string // VORTEXOPS_K8S_KUBECONFIG，kubeconfig 文件路径
}

// envPrefix 是所有环境变量前缀。
const envPrefix = "VORTEXOPS"

// Load 从环境变量加载配置并校验。configFile 当前保留参数兼容签名（暂未使用，未来支持 yaml）。
func Load(_ string, _ string) (*Config, error) {
	cfg := &Config{
		App: AppConfig{
			Name:        envStr("APP_NAME", "vortexops"),
			Environment: envStr("APP_ENVIRONMENT", "dev"),
			Version:     envStr("APP_VERSION", "0.1.0"),
			InstanceID:  envStr("APP_INSTANCE_ID", ""),
		},
		Server: ServerConfig{
			Addr:            envStr("SERVER_ADDR", ":8080"),
			ReadTimeout:     envDur("SERVER_READ_TIMEOUT", 15*time.Second),
			WriteTimeout:    envDur("SERVER_WRITE_TIMEOUT", 30*time.Second),
			IdleTimeout:     envDur("SERVER_IDLE_TIMEOUT", 120*time.Second),
			ShutdownTimeout: envDur("SERVER_SHUTDOWN_TIMEOUT", 30*time.Second),
			MaxHeaderBytes:  envInt("SERVER_MAX_HEADER_BYTES", 1<<20),
		},
		DB: DBConfig{
			Host:                envStr("DB_HOST", "localhost"),
			Port:                envInt("DB_PORT", 5432),
			Database:            envStr("DB_DATABASE", "vortexops"),
			Username:            envStr("DB_USERNAME", ""),
			Password:            envStr("DB_PASSWORD", ""),
			SSLMode:             envStr("DB_SSL_MODE", "prefer"),
			MaxConns:            int32(envInt("DB_MAX_CONNS", 50)),
			MinConns:            int32(envInt("DB_MIN_CONNS", 5)),
			MaxConnLifetime:     envDur("DB_MAX_CONN_LIFETIME", 30*time.Minute),
			MaxConnIdleTime:     envDur("DB_MAX_CONN_IDLE_TIME", 5*time.Minute),
			ConnectTimeout:      envDur("DB_CONNECT_TIMEOUT", 10*time.Second),
			StatementTimeout:    envDur("DB_STATEMENT_TIMEOUT", 30*time.Second),
			HealthCheckInterval: envDur("DB_HEALTH_CHECK_INTERVAL", 30*time.Second),
			ReadReplicaHost:     envStr("DB_READ_REPLICA_HOST", ""),
			ReadReplicaPort:     envInt("DB_READ_REPLICA_PORT", 5432),
			CitusEnabled:        envBool("DB_CITUS_ENABLED", false),
		},
		Redis: RedisConfig{
			Addrs:          envStrSlice("REDIS_ADDRS", []string{"localhost:6379"}),
			Username:       envStr("REDIS_USERNAME", ""),
			Password:       envStr("REDIS_PASSWORD", ""),
			SentinelMaster: envStr("REDIS_SENTINEL_MASTER", ""),
			DB:             envInt("REDIS_DB", 0),
			PoolSize:       envInt("REDIS_POOL_SIZE", 50),
			MinIdleConns:   envInt("REDIS_MIN_IDLE_CONNS", 5),
			DialTimeout:    envDur("REDIS_DIAL_TIMEOUT", 5*time.Second),
			ReadTimeout:    envDur("REDIS_READ_TIMEOUT", 3*time.Second),
			WriteTimeout:   envDur("REDIS_WRITE_TIMEOUT", 3*time.Second),
			MaxRetries:     envInt("REDIS_MAX_RETRIES", 3),
		},
		JWT: JWTConfig{
			SigningKey:    envStr("JWT_SIGNING_KEY", ""),
			Issuer:        envStr("JWT_ISSUER", "vortexops"),
			Audience:      envStr("JWT_AUDIENCE", "vortexops"),
			AccessTTL:     envDur("JWT_ACCESS_TTL", 30*time.Minute),
			RefreshTTL:    envDur("JWT_REFRESH_TTL", 7*24*time.Hour),
			RefreshRotate: envBool("JWT_REFRESH_ROTATE", true),
		},
		Security: SecurityConfig{
			EncryptionKey:        envStr("SECURITY_ENCRYPTION_KEY", ""),
			PasswordMinLength:    envInt("SECURITY_PASSWORD_MIN_LENGTH", 8),
			BcryptCost:           envInt("SECURITY_BCRYPT_COST", 12),
			SessionTimeout:       envDur("SECURITY_SESSION_TIMEOUT", 60*time.Minute),
			MaxLoginAttempts:     envInt("SECURITY_MAX_LOGIN_ATTEMPTS", 5),
			LoginLockoutDuration: envDur("SECURITY_LOGIN_LOCKOUT_DURATION", 15*time.Minute),
		},
		Log: LogConfig{
			Level:  envStr("LOG_LEVEL", "info"),
			Format: envStr("LOG_FORMAT", "json"),
		},
		S3: S3Config{
			Endpoint:  envStr("S3_ENDPOINT", ""),
			AccessKey: envStr("S3_ACCESS_KEY", ""),
			SecretKey: envStr("S3_SECRET_KEY", ""),
			Bucket:    envStr("S3_BUCKET", "vortexops"),
			Region:    envStr("S3_REGION", "us-east-1"),
			UseSSL:    envBool("S3_USE_SSL", false),
			Enabled:   envStr("S3_ENDPOINT", "") != "",
		},
		Kafka: KafkaConfig{
			Brokers:        envList("KAFKA_BROKERS"),
			TopicPipeline:  envStr("KAFKA_TOPIC_PIPELINE", "vortexops.pipeline"),
			TopicBuild:     envStr("KAFKA_TOPIC_BUILD", "vortexops.build"),
			TopicAudit:     envStr("KAFKA_TOPIC_AUDIT", "vortexops.audit"),
			TopicInference: envStr("KAFKA_TOPIC_INFERENCE", "vortexops.inference"),
			Enabled:        len(envList("KAFKA_BROKERS")) > 0,
		},
		ES: ElasticsearchConfig{
			URL:        envStr("ES_URL", ""),
			Username:   envStr("ES_USERNAME", ""),
			Password:   envStr("ES_PASSWORD", ""),
			IndexAudit: envStr("ES_INDEX_AUDIT", "vortexops-audit-*"),
			IndexLogs:  envStr("ES_INDEX_LOGS", "vortexops-build-logs-*"),
			Timeout:    envDur("ES_TIMEOUT", 10*time.Second),
			Enabled:    envStr("ES_URL", "") != "",
		},
		Integration: IntegrationConfig{
			JenkinsURL:      envStr("JENKINS_URL", "http://jenkins:8080"),
			JenkinsUser:     envStr("JENKINS_USER", "admin"),
			JenkinsPassword: envStr("JENKINS_PASSWORD", ""),
			HarborURL:       envStr("HARBOR_URL", "http://harbor-core:8080"),
			HarborUser:      envStr("HARBOR_USER", "admin"),
			HarborPassword:  envStr("HARBOR_PASSWORD", ""),
			K8sAPIServer:    envStr("K8S_API_SERVER", "https://k8s:6443"),
			K8sKubeconfig:   envStr("K8S_KUBECONFIG", ""),
		},
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}
	return cfg, nil
}

func (c *Config) validate() error {
	var errs []error
	if c.DB.Database == "" {
		errs = append(errs, errors.New("DB_DATABASE is required"))
	}
	if c.DB.Username == "" {
		errs = append(errs, errors.New("DB_USERNAME is required"))
	}
	if c.DB.MaxConns < c.DB.MinConns {
		errs = append(errs, errors.New("DB_MAX_CONNS must be >= DB_MIN_CONNS"))
	}
	if c.JWT.SigningKey == "" {
		errs = append(errs, errors.New("JWT_SIGNING_KEY is required"))
	}
	if len(c.JWT.SigningKey) < 32 {
		errs = append(errs, fmt.Errorf("JWT_SIGNING_KEY must be at least 32 bytes, got %d", len(c.JWT.SigningKey)))
	}
	if c.Security.EncryptionKey == "" {
		errs = append(errs, errors.New("SECURITY_ENCRYPTION_KEY is required"))
	}
	if _, err := hex.DecodeString(c.Security.EncryptionKey); err != nil || len(c.Security.EncryptionKey) != 64 {
		errs = append(errs, errors.New("SECURITY_ENCRYPTION_KEY must be 32-byte hex (64 hex chars)"))
	}
	if c.Security.BcryptCost < 10 || c.Security.BcryptCost > 14 {
		errs = append(errs, errors.New("SECURITY_BCRYPT_COST must be between 10 and 14"))
	}
	if len(c.Redis.Addrs) == 0 {
		errs = append(errs, errors.New("REDIS_ADDRS is required"))
	}
	if c.Server.Addr == "" {
		errs = append(errs, errors.New("SERVER_ADDR is required"))
	}
	if len(errs) > 0 {
		return fmt.Errorf("config validation failed: %v", errs)
	}
	return nil
}

// DSN 返回 PostgreSQL 连接串（key=value 形式）。
func (c *DBConfig) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%d dbname=%s user=%s password=%s sslmode=%s connect_timeout=%d application_name=vortexops",
		c.Host, c.Port, c.Database, c.Username, c.Password, c.SSLMode, int(c.ConnectTimeout.Seconds()),
	)
}

// AdminDSN 返回供 golang-migrate 使用的 postgres:// URL 形式。
func (c *DBConfig) AdminDSN() string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s&connect_timeout=%d",
		c.Username, c.Password, c.Host, c.Port, c.Database, c.SSLMode, int(c.ConnectTimeout.Seconds()),
	)
}

// --- env helpers ---

func envKey(name string) string { return envPrefix + "_" + name }

func envStr(name, def string) string {
	if v, ok := os.LookupEnv(envKey(name)); ok {
		return v
	}
	return def
}

func envInt(name string, def int) int {
	if v, ok := os.LookupEnv(envKey(name)); ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envBool(name string, def bool) bool {
	if v, ok := os.LookupEnv(envKey(name)); ok {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

func envDur(name string, def time.Duration) time.Duration {
	if v, ok := os.LookupEnv(envKey(name)); ok {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

// envList 解析逗号分隔的环境变量为字符串切片。
func envList(name string) []string {
	v, ok := os.LookupEnv(envKey(name))
	if !ok || v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func envStrSlice(name string, def []string) []string {
	if v, ok := os.LookupEnv(envKey(name)); ok {
		if v == "" {
			return nil
		}
		parts := strings.Split(v, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		return parts
	}
	return def
}
