package diagnosisapp

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

// llmClient 抽象 LLM 调用。
type llmClient interface {
	Chat(ctx context.Context, prompt string) (string, error)
	ChatMultiTurn(ctx context.Context, messages []ChatMessage) (string, error)
	// ChatMultiTurnStream 多轮对话流式。onDelta 收到增量文本时回调；返回完整文本。
	ChatMultiTurnStream(ctx context.Context, messages []ChatMessage, onDelta func(string)) (string, error)
}

func newLLMClient(provider, baseURL, apiKey, model string) (llmClient, error) {
	switch provider {
	case "openai", "openai_compatible", "":
		return &openAICompatibleClient{baseURL: baseURL, apiKey: apiKey, model: model}, nil
	case "anthropic":
		return &anthropicClient{baseURL: baseURL, apiKey: apiKey, model: model}, nil
	case "ollama":
		return &ollamaClient{baseURL: baseURL, model: model}, nil
	}
	return nil, fmt.Errorf("%w: %s", errUnsupportedProvider, provider)
}

var httpClient = &http.Client{Timeout: 120 * time.Second}

// --- OpenAI 兼容（含 Ollama OpenAI 模式、vLLM、本地 LM Studio 等） ---

type openAICompatibleClient struct {
	baseURL string
	apiKey  string
	model   string
}

func (c *openAICompatibleClient) Chat(ctx context.Context, prompt string) (string, error) {
	msgs := []map[string]string{
		{"role": "system", "content": "你是 Kubernetes 运维诊断专家，回答简明、可执行。"},
		{"role": "user", "content": prompt},
	}
	return c.chat(ctx, msgs)
}

func (c *openAICompatibleClient) ChatMultiTurn(ctx context.Context, messages []ChatMessage) (string, error) {
	msgs := make([]map[string]string, 0, len(messages))
	for _, m := range messages {
		msgs = append(msgs, map[string]string{"role": string(m.Role), "content": m.Content})
	}
	return c.chat(ctx, msgs)
}

func (c *openAICompatibleClient) chat(ctx context.Context, msgs []map[string]string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"model":       c.model,
		"messages":    msgs,
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

// ChatMultiTurnStream OpenAI 兼容流式：通过 SSE 推送 chunk。
func (c *openAICompatibleClient) ChatMultiTurnStream(ctx context.Context, messages []ChatMessage, onDelta func(string)) (string, error) {
	msgs := make([]map[string]string, 0, len(messages))
	for _, m := range messages {
		msgs = append(msgs, map[string]string{"role": string(m.Role), "content": m.Content})
	}
	body, _ := json.Marshal(map[string]any{
		"model":       c.model,
		"messages":    msgs,
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
	var full strings.Builder
	br := bufio.NewReader(resp.Body)
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			if errors.Is(err, context.Canceled) {
				break
			}
			return full.String(), fmt.Errorf("read sse: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			break
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		if len(chunk.Choices) == 0 || chunk.Choices[0].Delta.Content == "" {
			continue
		}
		delta := chunk.Choices[0].Delta.Content
		full.WriteString(delta)
		if onDelta != nil {
			onDelta(delta)
		}
	}
	return full.String(), nil
}

// --- Anthropic Claude ---

type anthropicClient struct {
	baseURL string
	apiKey  string
	model   string
}

func (c *anthropicClient) Chat(ctx context.Context, prompt string) (string, error) {
	return c.chat(ctx, "你是 Kubernetes 运维诊断专家，回答简明、可执行。", []ChatMessage{
		{Role: ChatRoleUser, Content: prompt},
	})
}

func (c *anthropicClient) ChatMultiTurn(ctx context.Context, messages []ChatMessage) (string, error) {
	var system string
	userTurns := make([]ChatMessage, 0, len(messages))
	for _, m := range messages {
		if m.Role == ChatRoleSystem {
			if system == "" {
				system = m.Content
			}
			continue
		}
		userTurns = append(userTurns, m)
	}
	if system == "" {
		system = "你是 Kubernetes 运维诊断专家，回答简明、可执行。"
	}
	return c.chat(ctx, system, userTurns)
}

func (c *anthropicClient) chat(ctx context.Context, system string, msgs []ChatMessage) (string, error) {
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

// ChatMultiTurnStream Anthropic 流式。SSE 事件 content_block_delta 含 text 增量。
func (c *anthropicClient) ChatMultiTurnStream(ctx context.Context, messages []ChatMessage, onDelta func(string)) (string, error) {
	var system string
	userTurns := make([]ChatMessage, 0, len(messages))
	for _, m := range messages {
		if m.Role == ChatRoleSystem {
			if system == "" {
				system = m.Content
			}
			continue
		}
		userTurns = append(userTurns, m)
	}
	if system == "" {
		system = "你是 Kubernetes 运维诊断专家，回答简明、可执行。"
	}
	apiMsgs := make([]map[string]string, 0, len(userTurns))
	for _, m := range userTurns {
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
	req.Header.Set("x-api-key", c.apiKey)
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
	var full strings.Builder
	br := bufio.NewReader(resp.Body)
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			if errors.Is(err, context.Canceled) {
				break
			}
			return full.String(), fmt.Errorf("read sse: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if !strings.HasPrefix(line, "data: ") {
			continue
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
			continue
		}
		if chunk.Type != "content_block_delta" || chunk.Delta.Text == "" {
			continue
		}
		full.WriteString(chunk.Delta.Text)
		if onDelta != nil {
			onDelta(chunk.Delta.Text)
		}
	}
	return full.String(), nil
}

// --- Ollama 原生 API ---

type ollamaClient struct {
	baseURL string
	model   string
}

func (c *ollamaClient) Chat(ctx context.Context, prompt string) (string, error) {
	return c.generate(ctx, "你是 Kubernetes 运维诊断专家，回答简明、可执行。", prompt)
}

func (c *ollamaClient) ChatMultiTurn(ctx context.Context, messages []ChatMessage) (string, error) {
	// Ollama /api/chat 支持多轮；这里使用 /api/generate + 拼接上下文以保持简单。
	var system, conv strings.Builder
	for _, m := range messages {
		if m.Role == ChatRoleSystem {
			system.WriteString(m.Content)
			continue
		}
		role := "用户"
		if m.Role == ChatRoleAssistant {
			role = "助手"
		}
		fmt.Fprintf(&conv, "%s: %s\n", role, m.Content)
	}
	if system.Len() == 0 {
		system.WriteString("你是 Kubernetes 运维诊断专家，回答简明、可执行。")
	}
	return c.generate(ctx, system.String(), conv.String())
}

func (c *ollamaClient) generate(ctx context.Context, system, prompt string) (string, error) {
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

// ChatMultiTurnStream Ollama 流式。stream=true 时按行返回 NDJSON，
// 每行 {"response":"...","done":bool}。
func (c *ollamaClient) ChatMultiTurnStream(ctx context.Context, messages []ChatMessage, onDelta func(string)) (string, error) {
	var system, conv strings.Builder
	for _, m := range messages {
		if m.Role == ChatRoleSystem {
			system.WriteString(m.Content)
			continue
		}
		role := "用户"
		if m.Role == ChatRoleAssistant {
			role = "助手"
		}
		fmt.Fprintf(&conv, "%s: %s\n", role, m.Content)
	}
	if system.Len() == 0 {
		system.WriteString("你是 Kubernetes 运维诊断专家，回答简明、可执行。")
	}
	body, _ := json.Marshal(map[string]any{
		"model":  c.model,
		"prompt": conv.String(),
		"system": system.String(),
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
	var full strings.Builder
	br := bufio.NewReader(resp.Body)
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			if errors.Is(err, context.Canceled) {
				break
			}
			return full.String(), fmt.Errorf("read ollama stream: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			continue
		}
		var chunk struct {
			Response string `json:"response"`
			Done     bool   `json:"done"`
		}
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			continue
		}
		if chunk.Response == "" {
			continue
		}
		full.WriteString(chunk.Response)
		if onDelta != nil {
			onDelta(chunk.Response)
		}
	}
	return full.String(), nil
}
