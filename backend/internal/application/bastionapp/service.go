// Package bastionapp 是堡垒机领域的应用服务层。
// 编排：资产 CRUD、JumpServer 同步、SSO 连接 URL 签发、会话录像查询、审计转发。
package bastionapp

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/vortexops/vortexops/internal/domain/bastion"
	"github.com/vortexops/vortexops/internal/infrastructure/jumpserver"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// AuditRecorder 审计记录器（转发堡垒机会话事件到统一审计）。
type AuditRecorder interface {
	Record(ctx context.Context, actorID int64, action, resourceType string, resourceID string, detail map[string]any) error
}

// JMSClientProvider 按需返回 JumpServer 客户端。
// 实现应从系统设置读取 base_url/access_key/secret_key，并在配置变更后重建客户端。
// 返回 (nil, nil) 表示 JumpServer 未配置（调用方应降级为仅本地资产）。
type JMSClientProvider interface {
	GetClient(ctx context.Context) (*jumpserver.Client, error)
}

// Service 堡垒机应用服务。
type Service struct {
	repo  bastion.Repository
	jms   JMSClientProvider
	audit AuditRecorder
}

// New 创建堡垒机服务。jms 可为 nil（未配置 JumpServer 时降级为仅本地资产）。
func New(repo bastion.Repository, jms JMSClientProvider, audit AuditRecorder) *Service {
	return &Service{repo: repo, jms: jms, audit: audit}
}

// --- 资产 ---

// CreateAsset 创建资产。
func (s *Service) CreateAsset(ctx context.Context, in bastion.CreateAssetInput) (*bastion.Asset, error) {
	if in.WorkspaceID == 0 {
		return nil, apperr.Validation("workspace_id is required", nil)
	}
	if in.Name == "" || in.Host == "" {
		return nil, apperr.Validation("name and host are required", nil)
	}
	a := &bastion.Asset{
		WorkspaceID: in.WorkspaceID, Name: in.Name, Host: in.Host, Port: in.Port,
		Protocol: in.Protocol, Platform: in.Platform, Username: in.Username,
		CredentialID: in.CredentialID, Tags: in.Tags, Comment: in.Comment,
		CreatedBy: in.CreatedBy, UpdatedBy: in.CreatedBy,
	}
	if err := s.repo.CreateAsset(ctx, a); err != nil {
		return nil, apperr.Internal("create bastion asset", err)
	}
	return a, nil
}

// GetAsset 查询资产。
func (s *Service) GetAsset(ctx context.Context, id int64) (*bastion.Asset, error) {
	a, err := s.repo.GetAssetByID(ctx, id)
	if err != nil {
		if errors.Is(err, bastion.ErrAssetNotFound) {
			return nil, apperr.NotFound("bastion asset", strconv.FormatInt(id, 10))
		}
		return nil, apperr.Internal("get bastion asset", err)
	}
	return a, nil
}

// ListAssets 分页查询资产。
func (s *Service) ListAssets(ctx context.Context, q bastion.AssetQuery, page, size int) ([]*bastion.Asset, int64, error) {
	q.Offset = (page - 1) * size
	q.Limit = size
	items, total, err := s.repo.ListAssets(ctx, q)
	if err != nil {
		return nil, 0, apperr.Internal("list bastion assets", err)
	}
	return items, total, nil
}

// UpdateAssetInput 更新资产输入。
type UpdateAssetInput struct {
	Name         string
	Host         string
	Port         int
	Protocol     bastion.Protocol
	Platform     string
	Username     string
	CredentialID int64
	Tags         []string
	Comment      string
	IsActive     bool
	UpdatedBy    int64
}

// UpdateAsset 更新资产。
func (s *Service) UpdateAsset(ctx context.Context, id int64, in UpdateAssetInput) (*bastion.Asset, error) {
	a, err := s.repo.GetAssetByID(ctx, id)
	if err != nil {
		if errors.Is(err, bastion.ErrAssetNotFound) {
			return nil, apperr.NotFound("bastion asset", strconv.FormatInt(id, 10))
		}
		return nil, apperr.Internal("get bastion asset", err)
	}
	a.Name = in.Name
	a.Host = in.Host
	a.Port = in.Port
	a.Protocol = in.Protocol
	a.Platform = in.Platform
	a.Username = in.Username
	a.CredentialID = in.CredentialID
	a.Tags = in.Tags
	a.Comment = in.Comment
	a.IsActive = in.IsActive
	a.UpdatedBy = in.UpdatedBy
	if err := s.repo.UpdateAsset(ctx, a); err != nil {
		return nil, apperr.Internal("update bastion asset", err)
	}
	return a, nil
}

// DeleteAsset 软删除资产。
func (s *Service) DeleteAsset(ctx context.Context, id, actorID int64) error {
	if err := s.repo.DeleteAsset(ctx, id, actorID); err != nil {
		if errors.Is(err, bastion.ErrAssetNotFound) {
			return apperr.NotFound("bastion asset", strconv.FormatInt(id, 10))
		}
		return apperr.Internal("delete bastion asset", err)
	}
	return nil
}

// --- 连接 ---

// Connect 签发 JumpServer 连接 URL（SSO 跳转 Luna）。
func (s *Service) Connect(ctx context.Context, assetID, userID int64, username string) (string, error) {
	jms, err := s.jmsClient(ctx)
	if err != nil {
		return "", err
	}
	if jms == nil {
		return "", apperr.BusinessRule("jumpserver not configured", nil)
	}
	a, err := s.repo.GetAssetByID(ctx, assetID)
	if err != nil {
		if errors.Is(err, bastion.ErrAssetNotFound) {
			return "", apperr.NotFound("bastion asset", strconv.FormatInt(assetID, 10))
		}
		return "", apperr.Internal("get bastion asset", err)
	}
	if a.JMSAssetID == "" {
		return "", apperr.BusinessRule("asset not synced to jumpserver", nil)
	}
	// 简化：使用资产 username 作为 system_user（实际应查 JMS system user 映射）。
	token, err := jms.CreateConnectionToken(ctx, username, a.JMSAssetID, a.Username, string(a.Protocol))
	if err != nil {
		return "", apperr.Internal("create connection token", err)
	}
	// 记录连接审计。
	if s.audit != nil {
		_ = s.audit.Record(ctx, userID, "bastion_connect", "bastion_asset", strconv.FormatInt(assetID, 10), map[string]any{
			"asset": a.Name, "host": a.Host, "protocol": a.Protocol, "login_url": token.LoginURL,
		})
	}
	return token.LoginURL, nil
}

// --- 会话 ---

// ListSessions 分页查询会话。
func (s *Service) ListSessions(ctx context.Context, q bastion.SessionQuery, page, size int) ([]*bastion.Session, int64, error) {
	q.Offset = (page - 1) * size
	q.Limit = size
	items, total, err := s.repo.ListSessions(ctx, q)
	if err != nil {
		return nil, 0, apperr.Internal("list bastion sessions", err)
	}
	return items, total, nil
}

// GetReplayURL 获取会话录像回放 URL。
func (s *Service) GetReplayURL(ctx context.Context, sessionID int64) (string, error) {
	sess, err := s.repo.GetSessionByID(ctx, sessionID)
	if err != nil {
		if errors.Is(err, bastion.ErrSessionNotFound) {
			return "", apperr.NotFound("bastion session", strconv.FormatInt(sessionID, 10))
		}
		return "", apperr.Internal("get bastion session", err)
	}
	if sess.JMSSessionID != "" {
		if jms, _ := s.jmsClient(ctx); jms != nil {
			return jms.GetReplayURL(sess.JMSSessionID), nil
		}
	}
	return sess.ReplayURL, nil
}

// --- 同步 ---

// SyncAssets 从 JumpServer 同步资产到本地。
func (s *Service) SyncAssets(ctx context.Context, workspaceID, actorID int64) (int, error) {
	jms, err := s.jmsClient(ctx)
	if err != nil {
		return 0, err
	}
	if jms == nil {
		return 0, apperr.BusinessRule("jumpserver not configured", nil)
	}
	assets, err := jms.ListAssets(ctx)
	if err != nil {
		return 0, apperr.Internal("list jumpserver assets", err)
	}
	existing, _, err := s.repo.ListAssets(ctx, bastion.AssetQuery{WorkspaceID: workspaceID, Limit: 1000})
	if err != nil {
		return 0, apperr.Internal("list local assets", err)
	}
	byJMS := make(map[string]*bastion.Asset)
	for _, a := range existing {
		if a.JMSAssetID != "" {
			byJMS[a.JMSAssetID] = a
		}
	}
	synced := 0
	for _, ja := range assets {
		if !ja.IsActive {
			continue
		}
		if local, ok := byJMS[ja.ID]; ok {
			local.Name = ja.Hostname
			local.Host = ja.IP
			local.Port = ja.Port
			local.Platform = ja.Platform
			local.Protocol = bastion.Protocol(ja.Protocol)
			local.Comment = ja.Comment
			local.UpdatedBy = actorID
			_ = s.repo.UpdateAsset(ctx, local)
		} else {
			a := &bastion.Asset{
				WorkspaceID: workspaceID, Name: ja.Hostname, Host: ja.IP, Port: ja.Port,
				Protocol: bastion.Protocol(ja.Protocol), Platform: ja.Platform, Comment: ja.Comment,
				JMSAssetID: ja.ID, JMSOrgID: ja.OrgID, IsActive: true,
				CreatedBy: actorID, UpdatedBy: actorID,
			}
			_ = s.repo.CreateAsset(ctx, a)
		}
		synced++
	}
	if s.audit != nil {
		_ = s.audit.Record(ctx, actorID, "bastion_sync", "bastion_workspace", strconv.FormatInt(workspaceID, 10), map[string]any{
			"synced": synced,
		})
	}
	return synced, nil
}

// SyncSessions 从 JumpServer 同步会话（最近 7 天）。
func (s *Service) SyncSessions(ctx context.Context, workspaceID int64) (int, error) {
	jms, err := s.jmsClient(ctx)
	if err != nil {
		return 0, err
	}
	if jms == nil {
		return 0, apperr.BusinessRule("jumpserver not configured", nil)
	}
	sessions, err := jms.ListSessions(ctx, time.Now().AddDate(0, 0, -7))
	if err != nil {
		return 0, apperr.Internal("list jumpserver sessions", err)
	}
	// 资产名映射。
	assets, _, _ := s.repo.ListAssets(ctx, bastion.AssetQuery{WorkspaceID: workspaceID, Limit: 1000})
	assetByName := make(map[string]*bastion.Asset)
	for _, a := range assets {
		assetByName[a.Name] = a
	}
	synced := 0
	for _, js := range sessions {
		status := bastion.SessionActive
		if js.IsFinished {
			status = bastion.SessionClosed
		}
		startedAt, _ := time.Parse(time.RFC3339, js.DateStart)
		var endedAt *time.Time
		if js.DateEnd != nil {
			t, _ := time.Parse(time.RFC3339, *js.DateEnd)
			endedAt = &t
		}
		var assetID int64
		if a, ok := assetByName[js.Asset]; ok {
			assetID = a.ID
		}
		sess := &bastion.Session{
			WorkspaceID: workspaceID, AssetID: assetID, JMSSessionID: js.ID,
			Username: js.User, AssetName: js.Asset, Protocol: bastion.Protocol(js.Protocol),
			RemoteAddr: js.RemoteAddr, LoginFrom: js.LoginFrom, Status: status,
			StartedAt: &startedAt, EndedAt: endedAt, DurationMs: int64(js.Duration * 1000),
			CommandCount: js.CommandCnt,
		}
		_ = s.repo.CreateSession(ctx, sess)
		synced++
	}
	return synced, nil
}

// jmsClient 解析当前 JumpServer 客户端（未配置时返回 nil, nil）。
// s.jms 为 nil 时（未注入 provider）也返回 nil, nil。
func (s *Service) jmsClient(ctx context.Context) (*jumpserver.Client, error) {
	if s.jms == nil {
		return nil, nil
	}
	return s.jms.GetClient(ctx)
}

// --- JumpServer 客户端 Provider ---

// jmsSettingsProvider 从系统设置读取 JumpServer 配置并按需构造客户端。
// 配置指纹（base_url|access_key|secret_key）变化时重建客户端，否则复用缓存，
// 避免每次调用都查 DB 与重复构造。
type jmsSettingsProvider struct {
	settings SettingsReader

	mu     sync.Mutex
	client *jumpserver.Client
	// 上次构造客户端时使用的配置指纹，用于检测变更。
	fingerprint string
}

// SettingsReader 系统设置读取接口（由 systemapp.Service 实现）。
type SettingsReader interface {
	GetStringSetting(ctx context.Context, key, fallback string) (string, error)
}

// NewJMSClientProvider 创建基于系统设置的 JumpServer 客户端 provider。
func NewJMSClientProvider(settings SettingsReader) JMSClientProvider {
	return &jmsSettingsProvider{settings: settings}
}

// GetClient 返回当前配置对应的 JumpServer 客户端。
// base_url 为空时返回 (nil, nil)（未配置）。
func (p *jmsSettingsProvider) GetClient(ctx context.Context) (*jumpserver.Client, error) {
	baseURL, err := p.settings.GetStringSetting(ctx, "jumpserver.base_url", "")
	if err != nil {
		return nil, apperr.Internal("read jumpserver.base_url", err)
	}
	if baseURL == "" {
		// 未配置：清空缓存并返回 nil（降级为仅本地资产）。
		p.mu.Lock()
		p.client = nil
		p.fingerprint = ""
		p.mu.Unlock()
		return nil, nil
	}
	accessKey, err := p.settings.GetStringSetting(ctx, "jumpserver.access_key", "")
	if err != nil {
		return nil, apperr.Internal("read jumpserver.access_key", err)
	}
	secretKey, err := p.settings.GetStringSetting(ctx, "jumpserver.secret_key", "")
	if err != nil {
		return nil, apperr.Internal("read jumpserver.secret_key", err)
	}

	fp := baseURL + "|" + accessKey + "|" + secretKey
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.client != nil && p.fingerprint == fp {
		return p.client, nil
	}
	// 配置变更或首次构造：重建客户端。
	client := jumpserver.New(baseURL, accessKey, secretKey)
	p.client = client
	p.fingerprint = fp
	return client, nil
}

// ensure fmt used
var _ = fmt.Sprintf
