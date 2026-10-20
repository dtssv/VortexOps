// Package systemapp 是系统设置领域的应用服务层。
// 提供全局配置项（vo_system_settings）的读写，以及默认 Jenkins/Registry 实例 ID 的便捷访问。
package systemapp

import (
	"context"
	"errors"
	"strconv"

	"github.com/vortexops/vortexops/internal/domain/system"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// Service 系统设置应用服务。
type Service struct {
	repo system.Repository
}

// New 创建服务。
func New(repo system.Repository) *Service {
	return &Service{repo: repo}
}

// 默认设置项 key 常量。
const (
	KeyDefaultJenkinsID  = "platform.default_jenkins_id"
	KeyDefaultRegistryID = "platform.default_registry_id"
	KeyBuildEngine       = "platform.build_engine" // jenkins | tekton
	KeyTektonNamespace   = "tekton.namespace"
	KeyTektonKubeconfig  = "tekton.kubeconfig"
	// 制品签名 / SBOM 工具二进制路径（空则使用系统 PATH 中的 cosign / syft）。
	KeyCosignPath = "platform.cosign_path"
	KeySyftPath   = "platform.syft_path"
	// GitOps manifest 仓库配置（启用后发布时把 manifest 推到 Git 仓库）。
	KeyGitOpsRepoURL    = "gitops.repo_url"
	KeyGitOpsBranch     = "gitops.branch"
	KeyGitOpsUsername   = "gitops.username"
	KeyGitOpsPassword   = "gitops.password"
	KeyGitOpsWorkDir    = "gitops.work_dir"
	// 通知渠道默认 webhook URL（外发通知在 Recipient 为空时回退使用）。
	KeyNotifDingTalkWebhook = "notif.dingtalk_webhook"
	KeyNotifFeishuWebhook   = "notif.feishu_webhook"
	KeyNotifWeComWebhook    = "notif.wecom_webhook"
	KeyNotifWebhookURL      = "notif.webhook_url"
	// SMTP 邮件配置（覆盖环境变量；为空则回退到 config.Email）。
	KeyNotifSMTPHost     = "notif.smtp_host"
	KeyNotifSMTPPort     = "notif.smtp_port"
	KeyNotifSMTPUsername = "notif.smtp_username"
	KeyNotifSMTPPassword = "notif.smtp_password"
	KeyNotifSMTPFrom     = "notif.smtp_from"
)

// SetInput 设置项写入入参。
type SetInput struct {
	Key         string
	Value       any
	Description string
	IsPublic    bool
	ActorID     int64
}

// Set 写入（upsert）一个设置项。
func (s *Service) Set(ctx context.Context, in SetInput) (*system.Setting, error) {
	if in.Key == "" {
		return nil, apperr.Validation("setting key is required", nil)
	}
	setting := &system.Setting{
		Key: in.Key, Value: in.Value, Description: in.Description, IsPublic: in.IsPublic,
	}
	setting.UpdatedBy = in.ActorID
	setting.CreatedBy = in.ActorID
	out, err := s.repo.Upsert(ctx, setting)
	if err != nil {
		return nil, apperr.Internal("upsert system setting", err)
	}
	return out, nil
}

// Get 读取一个设置项。
func (s *Service) Get(ctx context.Context, key string) (*system.Setting, error) {
	setting, err := s.repo.GetByKey(ctx, key)
	if err != nil {
		if errors.Is(err, system.ErrSettingNotFound) {
			return nil, apperr.NotFound("system setting", key)
		}
		return nil, apperr.Internal("get system setting", err)
	}
	return setting, nil
}

// List 列出设置项。publicOnly=true 时只返回 is_public=true 的项。
func (s *Service) List(ctx context.Context, publicOnly bool, search string) ([]*system.Setting, error) {
	items, err := s.repo.List(ctx, system.Query{PublicOnly: publicOnly, Search: search})
	if err != nil {
		return nil, apperr.Internal("list system settings", err)
	}
	return items, nil
}

// --- 默认 Jenkins/Registry 便捷方法 ---

// GetDefaultJenkinsID 读取默认 Jenkins 实例 ID。未配置返回 (0, nil)。
func (s *Service) GetDefaultJenkinsID(ctx context.Context) (int64, error) {
	return s.getIntSetting(ctx, KeyDefaultJenkinsID)
}

// SetDefaultJenkinsID 写入默认 Jenkins 实例 ID。
func (s *Service) SetDefaultJenkinsID(ctx context.Context, id int64, actorID int64) error {
	_, err := s.Set(ctx, SetInput{
		Key: KeyDefaultJenkinsID, Value: id, Description: "系统默认 Jenkins 实例 ID（全局唯一）",
		IsPublic: false, ActorID: actorID,
	})
	return err
}

// GetDefaultRegistryID 读取默认镜像仓库 ID。未配置返回 (0, nil)。
func (s *Service) GetDefaultRegistryID(ctx context.Context) (int64, error) {
	return s.getIntSetting(ctx, KeyDefaultRegistryID)
}

// SetDefaultRegistryID 写入默认镜像仓库 ID。
func (s *Service) SetDefaultRegistryID(ctx context.Context, id int64, actorID int64) error {
	_, err := s.Set(ctx, SetInput{
		Key: KeyDefaultRegistryID, Value: id, Description: "系统默认镜像仓库 ID（全局唯一）",
		IsPublic: false, ActorID: actorID,
	})
	return err
}

// GetBuildEngine 读取构建引擎类型（jenkins|tekton），未配置返回 "jenkins"。
func (s *Service) GetBuildEngine(ctx context.Context) (string, error) {
	return s.getStringSetting(ctx, KeyBuildEngine, "jenkins")
}

// GetTektonNamespace 读取 Tekton 运行命名空间，未配置返回 "vo-builds"。
func (s *Service) GetTektonNamespace(ctx context.Context) (string, error) {
	return s.getStringSetting(ctx, KeyTektonNamespace, "vo-builds")
}

// GetTektonKubeconfig 读取 Tekton 构建集群 kubeconfig（base64 或 PEM），未配置返回空。
func (s *Service) GetTektonKubeconfig(ctx context.Context) (string, error) {
	return s.getStringSetting(ctx, KeyTektonKubeconfig, "")
}

// GetCosignPath 读取 cosign 二进制路径，未配置返回空（使用系统 PATH）。
func (s *Service) GetCosignPath(ctx context.Context) (string, error) {
	return s.getStringSetting(ctx, KeyCosignPath, "")
}

// GetSyftPath 读取 syft 二进制路径，未配置返回空（使用系统 PATH）。
func (s *Service) GetSyftPath(ctx context.Context) (string, error) {
	return s.getStringSetting(ctx, KeySyftPath, "")
}

// GitOpsConfig GitOps manifest 仓库配置（从系统设置读取）。
type GitOpsConfig struct {
	RepoURL  string
	Branch   string
	Username string
	Password string
	WorkDir  string
	Enabled  bool
}

// GetGitOpsConfig 读取 GitOps 配置。RepoURL 为空则未启用。
func (s *Service) GetGitOpsConfig(ctx context.Context) (GitOpsConfig, error) {
	url, err := s.getStringSetting(ctx, KeyGitOpsRepoURL, "")
	if err != nil {
		return GitOpsConfig{}, err
	}
	branch, err := s.getStringSetting(ctx, KeyGitOpsBranch, "main")
	if err != nil {
		return GitOpsConfig{}, err
	}
	username, err := s.getStringSetting(ctx, KeyGitOpsUsername, "")
	if err != nil {
		return GitOpsConfig{}, err
	}
	password, err := s.getStringSetting(ctx, KeyGitOpsPassword, "")
	if err != nil {
		return GitOpsConfig{}, err
	}
	workDir, err := s.getStringSetting(ctx, KeyGitOpsWorkDir, "")
	if err != nil {
		return GitOpsConfig{}, err
	}
	return GitOpsConfig{
		RepoURL: url, Branch: branch, Username: username,
		Password: password, WorkDir: workDir, Enabled: url != "",
	}, nil
}

// NotifChannelConfig 通知渠道默认配置（从系统设置读取，供通知投递层回退使用）。
type NotifChannelConfig struct {
	DingTalkWebhook string
	FeishuWebhook   string
	WeComWebhook    string
	WebhookURL      string
	SMTPHost        string
	SMTPPort        int
	SMTPUsername    string
	SMTPPassword    string
	SMTPFrom        string
	SMTPEEnabled    bool
}

// GetNotifChannelConfig 读取通知渠道默认配置。
func (s *Service) GetNotifChannelConfig(ctx context.Context) (NotifChannelConfig, error) {
	ding, err := s.getStringSetting(ctx, KeyNotifDingTalkWebhook, "")
	if err != nil {
		return NotifChannelConfig{}, err
	}
	fei, err := s.getStringSetting(ctx, KeyNotifFeishuWebhook, "")
	if err != nil {
		return NotifChannelConfig{}, err
	}
	wecom, err := s.getStringSetting(ctx, KeyNotifWeComWebhook, "")
	if err != nil {
		return NotifChannelConfig{}, err
	}
	wh, err := s.getStringSetting(ctx, KeyNotifWebhookURL, "")
	if err != nil {
		return NotifChannelConfig{}, err
	}
	host, err := s.getStringSetting(ctx, KeyNotifSMTPHost, "")
	if err != nil {
		return NotifChannelConfig{}, err
	}
	port, err := s.getIntSetting(ctx, KeyNotifSMTPPort)
	if err != nil {
		return NotifChannelConfig{}, err
	}
	if port == 0 {
		port = 587
	}
	user, err := s.getStringSetting(ctx, KeyNotifSMTPUsername, "")
	if err != nil {
		return NotifChannelConfig{}, err
	}
	pass, err := s.getStringSetting(ctx, KeyNotifSMTPPassword, "")
	if err != nil {
		return NotifChannelConfig{}, err
	}
	from, err := s.getStringSetting(ctx, KeyNotifSMTPFrom, "")
	if err != nil {
		return NotifChannelConfig{}, err
	}
	return NotifChannelConfig{
		DingTalkWebhook: ding, FeishuWebhook: fei, WeComWebhook: wecom, WebhookURL: wh,
		SMTPHost: host, SMTPPort: int(port), SMTPUsername: user, SMTPPassword: pass, SMTPFrom: from,
		SMTPEEnabled: host != "",
	}, nil
}

// getIntSetting 读取一个 int 类型设置项；value 为 null/缺失返回 0,nil。
func (s *Service) getIntSetting(ctx context.Context, key string) (int64, error) {
	setting, err := s.repo.GetByKey(ctx, key)
	if err != nil {
		if errors.Is(err, system.ErrSettingNotFound) {
			return 0, nil
		}
		return 0, apperr.Internal("get int setting", err)
	}
	if setting.Value == nil {
		return 0, nil
	}
	switch v := setting.Value.(type) {
	case float64:
		return int64(v), nil
	case string:
		if v == "" || v == "null" {
			return 0, nil
		}
		n, perr := strconv.ParseInt(v, 10, 64)
		if perr != nil {
			return 0, apperr.Internal("parse int setting", perr)
		}
		return n, nil
	case int:
		return int64(v), nil
	case int64:
		return v, nil
	default:
		return 0, nil
	}
}

// getStringSetting 读取一个 string 类型设置项；value 为 null/缺失返回 fallback。
func (s *Service) getStringSetting(ctx context.Context, key, fallback string) (string, error) {
	setting, err := s.repo.GetByKey(ctx, key)
	if err != nil {
		if errors.Is(err, system.ErrSettingNotFound) {
			return fallback, nil
		}
		return fallback, apperr.Internal("get string setting", err)
	}
	if setting.Value == nil {
		return fallback, nil
	}
	switch v := setting.Value.(type) {
	case string:
		if v == "" || v == "null" {
			return fallback, nil
		}
		return v, nil
	default:
		return fallback, nil
	}
}

// GetStringSetting 公开方法：读取字符串类型系统设置，未配置时返回 fallback。
func (s *Service) GetStringSetting(ctx context.Context, key, fallback string) (string, error) {
	return s.getStringSetting(ctx, key, fallback)
}
