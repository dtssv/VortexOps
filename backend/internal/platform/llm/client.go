// Package llm 提供 LLM 调用与向量嵌入的共享客户端。
// 由 diagnosisapp（AI 助手）与 kbapp（知识库向量化）共用。
package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Role 对话角色。
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message 一条对话消息。
type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
}

// ChatClient LLM 对话客户端。
type ChatClient interface {
	// Chat 单轮对话（system 提示内置）。
	Chat(ctx context.Context, system, prompt string) (string, error)
	// ChatMultiTurn 多轮对话。
	ChatMultiTurn(ctx context.Context, system string, messages []Message) (string, error)
	// ChatMultiTurnStream 多轮对话（流式）。onDelta 在收到增量文本时回调；
	// 返回值为最终完整文本。若 provider 不支持流式，可降级为一次性返回完整文本。
	ChatMultiTurnStream(ctx context.Context, system string, messages []Message, onDelta func(delta string)) (string, error)
}

// EmbedClient 向量嵌入客户端。
type EmbedClient interface {
	// Embed 批量生成向量。返回的向量顺序与输入一致。
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	// Dim 返回向量维度。
	Dim() int
}

// Config LLM 配置。
type Config struct {
	Provider string // openai | openai_compatible | anthropic | ollama
	BaseURL  string
	APIKey   string
	Model    string
}

// NewChatClient 按 provider 创建对话客户端。
func NewChatClient(cfg Config) (ChatClient, error) {
	switch cfg.Provider {
	case "openai", "openai_compatible", "":
		return &openAIChat{baseURL: cfg.BaseURL, apiKey: cfg.APIKey, model: cfg.Model}, nil
	case "anthropic":
		return &anthropicChat{baseURL: cfg.BaseURL, apiKey: cfg.APIKey, model: cfg.Model}, nil
	case "ollama":
		return &ollamaChat{baseURL: cfg.BaseURL, model: cfg.Model}, nil
	}
	return nil, fmt.Errorf("unsupported chat provider: %s", cfg.Provider)
}

// NewEmbedClient 创建向量嵌入客户端。
// provider 与 chat 共用配置（OpenAI 兼容服务通常同时提供 /v1/chat/completions 与 /v1/embeddings）。
// Ollama 使用 /api/embeddings 端点。
func NewEmbedClient(cfg Config, dim int) (EmbedClient, error) {
	switch cfg.Provider {
	case "ollama":
		return &ollamaEmbed{baseURL: cfg.BaseURL, model: cfg.Model, dim: dim}, nil
	case "openai", "openai_compatible", "", "anthropic":
		return &openAIEmbed{baseURL: cfg.BaseURL, apiKey: cfg.APIKey, model: cfg.Model, dim: dim}, nil
	}
	return nil, fmt.Errorf("unsupported embed provider: %s", cfg.Provider)
}

var httpClient = &http.Client{Timeout: 120 * time.Second}

// ErrUnsupportedProvider 不支持的 provider。
var ErrUnsupportedProvider = errors.New("unsupported provider")

// ==================== OpenAI 兼容 Chat ====================

type openAIChat struct {
	baseURL string
	apiKey  string
	model   string
}

func (c *openAIChat) Chat(ctx context.Context, system, prompt string) (string, error) {
	return c.chat(ctx, system, []Message{{Role: RoleUser, Content: prompt}})
}

func (c *openAIChat) ChatMultiTurn(ctx context.Context, system string, messages []Message) (string, error) {
	return c.chat(ctx, system, messages)
}

// ChatMultiTurnStream 流式多轮对话。OpenAI 兼容服务通过 SSE 推送 chunk。
func (c *openAIChat) ChatMultiTurnStream(ctx context.Context, system string, messages []Message, onDelta func(string)) (string, error) {
	apiMsgs := make([]map[string]string, 0, len(messages)+1)
	if system != "" {
		apiMsgs = append(apiMsgs, map[string]string{"role": "system", "content": system})
	}
	for _, m := range messages {
		apiMsgs = append(apiMsgs, map[string]string{"role": string(m.Role), "content": m.Content})
	}
	body, _ := json.Marshal(map[string]any{
		"model":       c.model,
		"messages":    apiMsgs,
		"temperature": 0.3,
		"max_tokens":  2048,
		"stream":      true,
	})
	url := c.baseURL + "/v1/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("openai stream: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("openai stream http %d: %s", resp.StatusCode, string(raw))
	}
	return readSSEStream(resp.Body, onDelta, parseOpenAIDelta)
}

// parseOpenAIDelta 从 OpenAI SSE data 行解析增量文本。
func parseOpenAIDelta(line string) string {
	if !strings.HasPrefix(line, "data: ") {
		return ""
	}
	payload := strings.TrimPrefix(line, "data: ")
	if payload == "[DONE]" {
		return ""
	}
	var chunk struct {
		Choices []struct {
			Delta struct {
				Content string `json:"content"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
		return ""
	}
	if len(chunk.Choices) == 0 {
		return ""
	}
	return chunk.Choices[0].Delta.Content
}

// readSSEStream 通用 SSE 读取循环：按行扫描，遇到 data: 行调用 parser 提取增量并回调 onDelta。
// 返回累计的完整文本。
func readSSEStream(body io.ReadCloser, onDelta func(string), parser func(line string) string) (string, error) {
	var full strings.Builder
	br := bufio.NewReaderSize(body, 4096)
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			if errors.Is(err, context.Canceled) {
				return full.String(), nil
			}
			return full.String(), fmt.Errorf("read sse: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			continue
		}
		delta := parser(line)
		if delta != "" {
			full.WriteString(delta)
			if onDelta != nil {
				onDelta(delta)
			}
		}
	}
	return full.String(), nil
}

func (c *openAIChat) chat(ctx context.Context, system string, msgs []Message) (string, error) {
	apiMsgs := make([]map[string]string, 0, len(msgs)+1)
	if system != "" {
		apiMsgs = append(apiMsgs, map[string]string{"role": "system", "content": system})
	}
	for _, m := range msgs {
		apiMsgs = append(apiMsgs, map[string]string{"role": string(m.Role), "content": m.Content})
	}
	body, _ := json.Marshal(map[string]any{
		"model":       c.model,
		"messages":    apiMsgs,
		"temperature": 0.3,
		"max_tokens":  2048,
	})
	url := c.baseURL + "/v1/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("openai chat: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("openai http %d: %s", resp.StatusCode, string(raw))
	}
	var out struct {
		Choices []struct {
			Message struct{ Content string `json:"content"` } `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("decode openai resp: %w", err)
	}
	if len(out.Choices) == 0 {
		return "", errors.New("openai: empty choices")
	}
	return out.Choices[0].Message.Content, nil
}

// ==================== Anthropic Chat ====================

type anthropicChat struct {
	baseURL string
	apiKey  string
	model   string
}

func (c *anthropicChat) Chat(ctx context.Context, system, prompt string) (string, error) {
	return c.chat(ctx, system, []Message{{Role: RoleUser, Content: prompt}})
}

func (c *anthropicChat) ChatMultiTurn(ctx context.Context, system string, messages []Message) (string, error) {
	return c.chat(ctx, system, messages)
}

// ChatMultiTurnStream Anthropic 流式。Anthropic SSE 事件格式：
// event: content_block_delta / data: {"delta":{"text":"..."}}
func (c *anthropicChat) ChatMultiTurnStream(ctx context.Context, system string, messages []Message, onDelta func(string)) (string, error) {
	if system == "" {
		system = "你是 VortexOps AI 助手。"
	}
	apiMsgs := make([]map[string]string, 0, len(messages))
	for _, m := range messages {
		apiMsgs = append(apiMsgs, map[string]string{"role": string(m.Role), "content": m.Content})
	}
	body, _ := json.Marshal(map[string]any{
		"model":      c.model,
		"max_tokens": 2048,
		"system":     system,
		"messages":   apiMsgs,
		"stream":     true,
	})
	url := c.baseURL + "/v1/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if c.apiKey != "" {
		req.Header.Set("x-api-key", c.apiKey)
	}
	req.Header.Set("anthropic-version", "2023-06-01")
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("anthropic stream: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("anthropic stream http %d: %s", resp.StatusCode, string(raw))
	}
	return readSSEStream(resp.Body, onDelta, parseAnthropicDelta)
}

// parseAnthropicDelta Anthropic SSE data 行解析。
// 仅 content_block_delta 事件的 data 含 text 增量。
func parseAnthropicDelta(line string) string {
	if !strings.HasPrefix(line, "data: ") {
		return ""
	}
	payload := strings.TrimPrefix(line, "data: ")
	var chunk struct {
		Type  string `json:"type"`
		Delta struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"delta"`
	}
	if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
		return ""
	}
	if chunk.Type != "content_block_delta" {
		return ""
	}
	return chunk.Delta.Text
}

func (c *anthropicChat) chat(ctx context.Context, system string, msgs []Message) (string, error) {
	if system == "" {
		system = "你是 VortexOps AI 助手。"
	}
	apiMsgs := make([]map[string]string, 0, len(msgs))
	for _, m := range msgs {
		apiMsgs = append(apiMsgs, map[string]string{"role": string(m.Role), "content": m.Content})
	}
	body, _ := json.Marshal(map[string]any{
		"model":      c.model,
		"max_tokens": 2048,
		"system":     system,
		"messages":   apiMsgs,
	})
	url := c.baseURL + "/v1/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("anthropic chat: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("anthropic http %d: %s", resp.StatusCode, string(raw))
	}
	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("decode anthropic resp: %w", err)
	}
	for _, blk := range out.Content {
		if blk.Type == "text" {
			return blk.Text, nil
		}
	}
	return "", errors.New("anthropic: empty content")
}

// ==================== Ollama Chat ====================

type ollamaChat struct {
	baseURL string
	model   string
}

func (c *ollamaChat) Chat(ctx context.Context, system, prompt string) (string, error) {
	return c.generate(ctx, system, prompt)
}

func (c *ollamaChat) ChatMultiTurn(ctx context.Context, system string, messages []Message) (string, error) {
	// Ollama /api/generate 不支持多轮 messages，拼接为单轮。
	var conv strings.Builder
	for _, m := range messages {
		role := "用户"
		if m.Role == RoleAssistant {
			role = "助手"
		}
		fmt.Fprintf(&conv, "%s: %s\n", role, m.Content)
	}
	return c.generate(ctx, system, conv.String())
}

// ChatMultiTurnStream Ollama 流式。Ollama /api/generate 在 stream=true 时
// 按行返回 NDJSON，每行 {"response":"...","done":bool}。
func (c *ollamaChat) ChatMultiTurnStream(ctx context.Context, system string, messages []Message, onDelta func(string)) (string, error) {
	var conv strings.Builder
	for _, m := range messages {
		role := "用户"
		if m.Role == RoleAssistant {
			role = "助手"
		}
		fmt.Fprintf(&conv, "%s: %s\n", role, m.Content)
	}
	body, _ := json.Marshal(map[string]any{
		"model":  c.model,
		"prompt": conv.String(),
		"system": system,
		"stream": true,
	})
	url := c.baseURL + "/api/generate"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama stream: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("ollama stream http %d: %s", resp.StatusCode, string(raw))
	}
	return readSSEStream(resp.Body, onDelta, parseOllamaDelta)
}

// parseOllamaDelta Ollama NDJSON 每行解析。
func parseOllamaDelta(line string) string {
	if line == "" || !strings.HasPrefix(line, "{") {
		return ""
	}
	var chunk struct {
		Response string `json:"response"`
		Done     bool   `json:"done"`
	}
	if err := json.Unmarshal([]byte(line), &chunk); err != nil {
		return ""
	}
	return chunk.Response
}

func (c *ollamaChat) generate(ctx context.Context, system, prompt string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"model":  c.model,
		"prompt": prompt,
		"system": system,
		"stream": false,
	})
	url := c.baseURL + "/api/generate"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama generate: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("ollama http %d: %s", resp.StatusCode, string(raw))
	}
	var out struct {
		Response string `json:"response"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("decode ollama resp: %w", err)
	}
	return out.Response, nil
}

// ==================== OpenAI 兼容 Embeddings ====================

type openAIEmbed struct {
	baseURL string
	apiKey  string
	model   string
	dim     int
}

func (c *openAIEmbed) Dim() int { return c.dim }

func (c *openAIEmbed) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	body, _ := json.Marshal(map[string]any{
		"model": c.model,
		"input": texts,
	})
	url := c.baseURL + "/v1/embeddings"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai embed: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("openai embed http %d: %s", resp.StatusCode, string(raw))
	}
	var out struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode openai embed resp: %w", err)
	}
	if len(out.Data) != len(texts) {
		return nil, fmt.Errorf("openai embed: expected %d vectors, got %d", len(texts), len(out.Data))
	}
	result := make([][]float32, len(out.Data))
	for i, d := range out.Data {
		result[i] = d.Embedding
	}
	return result, nil
}

// ==================== Ollama Embeddings ====================

type ollamaEmbed struct {
	baseURL string
	model   string
	dim     int
}

func (c *ollamaEmbed) Dim() int { return c.dim }

func (c *ollamaEmbed) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	// Ollama 一次只能嵌入一条文本，循环调用。
	result := make([][]float32, len(texts))
	for i, text := range texts {
		body, _ := json.Marshal(map[string]any{
			"model": c.model,
			"prompt": text,
		})
		url := c.baseURL + "/api/embeddings"
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("ollama embed: %w", err)
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("ollama embed http %d: %s", resp.StatusCode, string(raw))
		}
		var out struct {
			Embedding []float32 `json:"embedding"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, fmt.Errorf("decode ollama embed resp: %w", err)
		}
		result[i] = out.Embedding
	}
	return result, nil
}
