package server

import (
	"context"

	"github.com/vortexops/vortexops/internal/application/chatapp"
	"github.com/vortexops/vortexops/internal/application/diagnosisapp"
	"github.com/vortexops/vortexops/internal/application/kbapp"
	"github.com/vortexops/vortexops/internal/application/systemapp"
	"github.com/vortexops/vortexops/internal/application/userprofileapp"
	"github.com/vortexops/vortexops/internal/platform/llm"
)

// llmChatFactory 懒加载 LLM 对话客户端。
// 配置来自系统设置 ai.diagnosis.*，与诊断服务共用。
type llmChatFactory struct {
	settings *systemapp.Service
	client   llm.ChatClient
	cfgKey   string
}

func newLLMChatFactory(settings *systemapp.Service) *llmChatFactory {
	return &llmChatFactory{settings: settings}
}

// Client 返回 LLM 对话客户端（单例，按当前系统设置创建一次）。
// 注意：若管理员后续修改 LLM 配置，需重启 apiserver 才会生效（与诊断服务行为一致）。
func (f *llmChatFactory) Client(ctx context.Context) (llm.ChatClient, error) {
	if f.client != nil {
		return f.client, nil
	}
	provider, _ := f.settings.GetStringSetting(ctx, "ai.diagnosis.provider", "openai")
	baseURL, _ := f.settings.GetStringSetting(ctx, "ai.diagnosis.url", "")
	apiKey, _ := f.settings.GetStringSetting(ctx, "ai.diagnosis.api_key", "")
	model, _ := f.settings.GetStringSetting(ctx, "ai.diagnosis.model", "gpt-4o-mini")
	c, err := llm.NewChatClient(llm.Config{Provider: provider, BaseURL: baseURL, APIKey: apiKey, Model: model})
	if err != nil {
		return nil, err
	}
	f.client = c
	return c, nil
}

// 桥接 llm.ChatClient 到 userprofileapp/chatapp 期望的接口。
// 由于 userprofileapp.Service 与 chatapp.Service 直接依赖 llm.ChatClient，
// 这里返回一个懒加载包装器。
type lazyChatClient struct {
	factory *llmChatFactory
}

func (c *lazyChatClient) Chat(ctx context.Context, system, prompt string) (string, error) {
	cli, err := c.factory.Client(ctx)
	if err != nil {
		return "", err
	}
	return cli.Chat(ctx, system, prompt)
}

func (c *lazyChatClient) ChatMultiTurn(ctx context.Context, system string, messages []llm.Message) (string, error) {
	cli, err := c.factory.Client(ctx)
	if err != nil {
		return "", err
	}
	return cli.ChatMultiTurn(ctx, system, messages)
}

func (c *lazyChatClient) ChatMultiTurnStream(ctx context.Context, system string, messages []llm.Message, onDelta func(string)) (string, error) {
	cli, err := c.factory.Client(ctx)
	if err != nil {
		return "", err
	}
	return cli.ChatMultiTurnStream(ctx, system, messages, onDelta)
}

// 实现 userprofileapp/chatapp 期望的构造函数签名：(*userprofileapp.Service, llm.ChatClient)
// 我们通过包装器注入懒加载客户端。
// 由于 Go 不允许把 *llmChatFactory 直接当 llm.ChatClient，这里定义适配器类型。
// 注意：userprofileapp.New 与 chatapp.New 接收 llm.ChatClient，因此我们传入 lazyChatClient。

// 由于 userprofileapp.New 与 chatapp.New 的签名要求 llm.ChatClient，
// 而我们在 server.go 中直接传 llmChatFactory 会类型不匹配。
// 解决方案：在 server.go 中调用 newLLMChatFactory 后，将其包装为 lazyChatClient 传入。
// 为简化，这里提供辅助函数。
func newLazyChatClient(f *llmChatFactory) llm.ChatClient {
	return &lazyChatClient{factory: f}
}

// --- 适配器：把 kbapp/userprofileapp/chatapp 的具体类型适配为 diagnosisapp 期望的接口 ---

// kbSearcherAdapter 适配 kbapp.Service -> diagnosisapp.KBSearcher。
type kbSearcherAdapter struct {
	svc *kbapp.Service
}

func newKBSearcherAdapter(svc *kbapp.Service) *kbSearcherAdapter {
	return &kbSearcherAdapter{svc: svc}
}

func (a *kbSearcherAdapter) Search(ctx context.Context, query string, topK int, categoryCode string) ([]diagnosisapp.KBHit, error) {
	hits, err := a.svc.Search(ctx, query, topK, categoryCode)
	if err != nil {
		return nil, err
	}
	out := make([]diagnosisapp.KBHit, 0, len(hits))
	for _, h := range hits {
		out = append(out, diagnosisapp.KBHit{
			DocumentTitle: h.DocumentTitle,
			CategoryCode:  h.CategoryCode,
			Content:       h.Content,
			Score:         h.Score,
		})
	}
	return out, nil
}

// profilerAdapter 适配 userprofileapp.Service -> diagnosisapp.Profiler。
type profilerAdapter struct {
	svc *userprofileapp.Service
}

func newProfilerAdapter(svc *userprofileapp.Service) *profilerAdapter {
	return &profilerAdapter{svc: svc}
}

func (a *profilerAdapter) GetByUserID(ctx context.Context, userID int64) (*diagnosisapp.UserProfile, error) {
	p, err := a.svc.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &diagnosisapp.UserProfile{
		ExpertiseLevel:     p.ExpertiseLevel,
		Roles:              p.Roles,
		Domains:            p.Domains,
		CommunicationStyle: p.CommunicationStyle,
		Summary:            p.Summary,
	}, nil
}

func (a *profilerAdapter) LearnFromConversation(ctx context.Context, userID int64, dialog string) (*diagnosisapp.UserProfile, error) {
	p, err := a.svc.LearnFromConversation(ctx, userID, dialog)
	if err != nil {
		return nil, err
	}
	return &diagnosisapp.UserProfile{
		ExpertiseLevel:     p.ExpertiseLevel,
		Roles:              p.Roles,
		Domains:            p.Domains,
		CommunicationStyle: p.CommunicationStyle,
		Summary:            p.Summary,
	}, nil
}

// sessionManagerAdapter 适配 chatapp.Service -> diagnosisapp.SessionManager。
type sessionManagerAdapter struct {
	svc *chatapp.Service
}

func newSessionManagerAdapter(svc *chatapp.Service) *sessionManagerAdapter {
	return &sessionManagerAdapter{svc: svc}
}

func (a *sessionManagerAdapter) AppendMessage(ctx context.Context, in diagnosisapp.SessionAppendInput) error {
	_, err := a.svc.AppendMessage(ctx, chatapp.AppendMessageInput{
		SessionID: in.SessionID, UserID: in.UserID, Role: in.Role, Content: in.Content,
		Intent: in.Intent, Tools: in.Tools, References: in.References,
		LatencyMs: in.LatencyMs, ActorID: in.ActorID,
	})
	return err
}

func (a *sessionManagerAdapter) UpdateSessionMeta(ctx context.Context, sessionID int64, scene string, entities map[string]any, lastIntent string, actorID int64) error {
	return a.svc.UpdateSessionMeta(ctx, sessionID, scene, entities, lastIntent, actorID)
}

func (a *sessionManagerAdapter) SummarizeIfNeeded(ctx context.Context, sessionID int64, threshold int) error {
	return a.svc.SummarizeIfNeeded(ctx, sessionID, threshold)
}

func (a *sessionManagerAdapter) BuildContext(ctx context.Context, sessionID int64, recentN int) (string, error) {
	return a.svc.BuildContext(ctx, sessionID, recentN)
}
