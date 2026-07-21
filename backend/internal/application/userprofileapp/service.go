// Package userprofileapp 是用户画像的应用服务层。
// 在 AI 助手多轮对话中持续更新用户画像，用于个性化回答。
package userprofileapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/vortexops/vortexops/internal/domain/userprofile"
	"github.com/vortexops/vortexops/internal/platform/llm"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// Service 用户画像应用服务。
type Service struct {
	repo userprofile.Repository
	llm  llm.ChatClient
}

// New 创建服务。
func New(repo userprofile.Repository, llm llm.ChatClient) *Service {
	return &Service{repo: repo, llm: llm}
}

// GetByUserID 获取用户画像。若不存在返回默认画像。
func (s *Service) GetByUserID(ctx context.Context, userID int64) (*userprofile.Profile, error) {
	p, err := s.repo.GetByUserID(ctx, userID)
	if err != nil {
		if errors.Is(err, userprofile.ErrProfileNotFound) {
			return defaultProfile(userID), nil
		}
		return nil, apperr.Internal("get user profile", err)
	}
	return p, nil
}

// UpdateProfileInput 更新画像输入。
type UpdateProfileInput struct {
	UserID             int64
	ExpertiseLevel     *string
	Roles              *[]string
	Domains            *[]string
	CommunicationStyle *string
	PreferredLanguage  *string
	Summary            *string
	ActorID            int64
}

// UpdateProfile 手动更新画像。
func (s *Service) UpdateProfile(ctx context.Context, in UpdateProfileInput) (*userprofile.Profile, error) {
	p, err := s.repo.GetByUserID(ctx, in.UserID)
	if err != nil {
		if !errors.Is(err, userprofile.ErrProfileNotFound) {
			return nil, apperr.Internal("get user profile", err)
		}
		p = defaultProfile(in.UserID)
	}
	if in.ExpertiseLevel != nil {
		p.ExpertiseLevel = *in.ExpertiseLevel
	}
	if in.Roles != nil {
		p.Roles = *in.Roles
	}
	if in.Domains != nil {
		p.Domains = *in.Domains
	}
	if in.CommunicationStyle != nil {
		p.CommunicationStyle = *in.CommunicationStyle
	}
	if in.PreferredLanguage != nil {
		p.PreferredLanguage = *in.PreferredLanguage
	}
	if in.Summary != nil {
		p.Summary = *in.Summary
	}
	p.UpdatedBy = in.ActorID
	out, err := s.repo.Upsert(ctx, p)
	if err != nil {
		return nil, apperr.Internal("upsert user profile", err)
	}
	return out, nil
}

// LearnFromConversation 输入最近一段对话（user 消息 + assistant 回复），
// 由 LLM 推断用户特征并更新画像。
// dialog 格式示例：
//   user: 我的 Spring Boot 应用启动失败，日志报 Bean 创建异常
//   assistant: ...
func (s *Service) LearnFromConversation(ctx context.Context, userID int64, dialog string) (*userprofile.Profile, error) {
	if s.llm == nil || strings.TrimSpace(dialog) == "" {
		return s.GetByUserID(ctx, userID)
	}
	p, err := s.repo.GetByUserID(ctx, userID)
	if err != nil {
		if !errors.Is(err, userprofile.ErrProfileNotFound) {
			return nil, apperr.Internal("get user profile", err)
		}
		p = defaultProfile(userID)
	}
	prompt := buildProfilePrompt(p, dialog)
	system := "你是用户画像分析助手。根据对话内容推断用户的技术角色、擅长领域与专业水平，输出 JSON。"
	resp, err := s.llm.Chat(ctx, system, prompt)
	if err != nil {
		// 画像更新失败不应影响主对话流程，返回当前画像。
		return p, nil
	}
	updated := parseProfileFromLLM(resp, p)
	updated.UserID = userID
	updated.InteractionCount = p.InteractionCount + 1
	updated.UpdatedBy = userID
	out, err := s.repo.Upsert(ctx, updated)
	if err != nil {
		return nil, apperr.Internal("upsert user profile", err)
	}
	return out, nil
}

// ProfileToPrompt 将画像转换为 system prompt 片段，注入到对话中。
// 例：「用户画像：资深 Java 工程师，擅长 Kubernetes/Spring，回答可使用专业术语。」
func ProfileToPrompt(p *userprofile.Profile) string {
	if p == nil {
		return ""
	}
	var parts []string
	if p.ExpertiseLevel != "" && p.ExpertiseLevel != "unknown" {
		levelMap := map[string]string{
			"beginner":     "初学者", "intermediate": "中级", "advanced": "高级", "expert": "资深",
		}
		if zh, ok := levelMap[p.ExpertiseLevel]; ok {
			parts = append(parts, "专业水平："+zh)
		}
	}
	if len(p.Roles) > 0 {
		parts = append(parts, "角色："+strings.Join(p.Roles, "/"))
	}
	if len(p.Domains) > 0 {
		parts = append(parts, "擅长领域："+strings.Join(p.Domains, "/"))
	}
	if p.CommunicationStyle != "" && p.CommunicationStyle != "balanced" {
		styleMap := map[string]string{"concise": "回答简洁", "detailed": "回答详细"}
		if zh, ok := styleMap[p.CommunicationStyle]; ok {
			parts = append(parts, zh)
		}
	}
	if p.Summary != "" {
		parts = append(parts, "画像摘要："+p.Summary)
	}
	if len(parts) == 0 {
		return ""
	}
	return "用户画像：" + strings.Join(parts, "；") + "。请根据用户画像调整回答深度与术语。"
}

// buildProfilePrompt 构造画像推断 prompt。
func buildProfilePrompt(p *userprofile.Profile, dialog string) string {
	existing := "无"
	if p != nil {
		existing = fmt.Sprintf("expertise_level=%s, roles=%v, domains=%v, communication_style=%s",
			p.ExpertiseLevel, p.Roles, p.Domains, p.CommunicationStyle)
	}
	return fmt.Sprintf(`现有用户画像：%s

最近对话内容：
%s

请分析用户的技术背景，推断并输出 JSON（仅输出 JSON，不要其它内容）：
{
  "expertise_level": "beginner|intermediate|advanced|expert",
  "roles": ["可能的角色，如 java_engineer/sre/devops/frontend_engineer/dba"],
  "domains": ["擅长的技术领域，如 kubernetes/spring/redis/mysql"],
  "communication_style": "concise|detailed|balanced",
  "summary": "一句话描述用户特征，如「资深 Java 工程师，擅长分布式系统」"
}

推断规则：
- 若对话涉及深入排查 JVM/K8s/分布式问题，expertise_level 至少 advanced。
- 若对话仅为概念性问题或基础操作，expertise_level 为 beginner/intermediate。
- roles 与 domains 应从对话内容提取，不要臆测。
- summary 应简洁，融合角色与领域。`, existing, dialog)
}

// parseProfileFromLLM 解析 LLM 输出的 JSON，更新画像。
func parseProfileFromLLM(resp string, base *userprofile.Profile) *userprofile.Profile {
	out := *base // 复制基础画像
	jsonStr := extractJSON(resp)
	var parsed struct {
		ExpertiseLevel     string   `json:"expertise_level"`
		Roles              []string `json:"roles"`
		Domains            []string `json:"domains"`
		CommunicationStyle string   `json:"communication_style"`
		Summary            string   `json:"summary"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return &out
	}
	if parsed.ExpertiseLevel != "" {
		out.ExpertiseLevel = parsed.ExpertiseLevel
	}
	if len(parsed.Roles) > 0 {
		out.Roles = parsed.Roles
	}
	if len(parsed.Domains) > 0 {
		out.Domains = parsed.Domains
	}
	if parsed.CommunicationStyle != "" {
		out.CommunicationStyle = parsed.CommunicationStyle
	}
	if parsed.Summary != "" {
		out.Summary = parsed.Summary
	}
	return &out
}

// extractJSON 从可能含 markdown 代码块的文本中提取 JSON。
func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		// 去掉首行 ```json 或 ```
		if idx := strings.Index(s, "\n"); idx > 0 {
			s = s[idx+1:]
		}
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	}
	// 截取首个 { 到最后一个 }。
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start >= 0 && end > start {
		return s[start : end+1]
	}
	return s
}

func defaultProfile(userID int64) *userprofile.Profile {
	return &userprofile.Profile{
		UserID:             userID,
		ExpertiseLevel:     "unknown",
		Roles:              []string{},
		Domains:            []string{},
		CommunicationStyle: "balanced",
		PreferredLanguage:  "zh-CN",
	}
}
