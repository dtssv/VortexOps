// Package kafka 提供 Kafka 消费者封装（consumer group）。
package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/segmentio/kafka-go"
)

// MessageHandler 消费消息处理函数。
type MessageHandler func(ctx context.Context, msg kafka.Message) error

// Consumer Kafka consumer group 消费者。
type Consumer struct {
	readers  []*kafka.Reader
	handlers map[string]MessageHandler
	enabled  bool
	wg       sync.WaitGroup
}

// ConsumerConfig 消费者配置。
type ConsumerConfig struct {
	Brokers  []string
	GroupID  string
	Topics   []string
	MinBytes int
	MaxBytes int
	MaxWait  time.Duration
}

// NewConsumer 创建消费者。brokers 为空时为 no-op。
func NewConsumer(cfg ConsumerConfig, handlers map[string]MessageHandler) *Consumer {
	if len(cfg.Brokers) == 0 || len(cfg.Topics) == 0 {
		return &Consumer{enabled: false}
	}
	if cfg.GroupID == "" {
		cfg.GroupID = "vortexops"
	}
	if cfg.MinBytes == 0 {
		cfg.MinBytes = 1
	}
	if cfg.MaxBytes == 0 {
		cfg.MaxBytes = 10e6
	}
	if cfg.MaxWait == 0 {
		cfg.MaxWait = 500 * time.Millisecond
	}
	c := &Consumer{handlers: handlers, enabled: true}
	for _, topic := range cfg.Topics {
		c.readers = append(c.readers, kafka.NewReader(kafka.ReaderConfig{
			Brokers:        cfg.Brokers,
			GroupID:        cfg.GroupID,
			Topic:          topic,
			MinBytes:       cfg.MinBytes,
			MaxBytes:       cfg.MaxBytes,
			MaxWait:        cfg.MaxWait,
			CommitInterval: time.Second,
			StartOffset:    kafka.LastOffset,
		}))
	}
	return c
}

// Enabled 是否启用 Kafka 消费。
func (c *Consumer) Enabled() bool { return c.enabled }

// Run 启动所有 topic 消费循环，阻塞直到 ctx 取消。
func (c *Consumer) Run(ctx context.Context) {
	if !c.enabled {
		<-ctx.Done()
		return
	}
	for _, r := range c.readers {
		reader := r
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			c.consumeLoop(ctx, reader)
		}()
	}
	<-ctx.Done()
	c.wg.Wait()
}

func (c *Consumer) consumeLoop(ctx context.Context, reader *kafka.Reader) {
	topic := reader.Config().Topic
	handler := c.handlers[topic]
	for {
		msg, err := reader.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil || err == io.EOF {
				return
			}
			continue
		}
		if handler != nil {
			if err := handler(ctx, msg); err != nil {
				continue
			}
		}
		_ = reader.CommitMessages(ctx, msg)
	}
}

// Close 关闭所有 reader。
func (c *Consumer) Close() error {
	if !c.enabled {
		return nil
	}
	var firstErr error
	for _, r := range c.readers {
		if err := r.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// DecodeJSON 解码 JSON 消息体。
func DecodeJSON(msg kafka.Message, dest any) error {
	if err := json.Unmarshal(msg.Value, dest); err != nil {
		return fmt.Errorf("decode kafka message: %w", err)
	}
	return nil
}

// AuditAsyncPayload 审计异步落库事件载荷。
type AuditAsyncPayload struct {
	UserID       int64          `json:"user_id"`
	WorkspaceID  int64          `json:"workspace_id"`
	ResourceType string         `json:"resource_type"`
	Action       string         `json:"action"`
	Operation    string         `json:"operation"`
	RequestBody  map[string]any `json:"request_body,omitempty"`
}

// InferenceUsagePayload Token 计量事件载荷。
type InferenceUsagePayload struct {
	WorkspaceID   int64  `json:"workspace_id"`
	ApplicationID int64  `json:"application_id"`
	ModelID       int64  `json:"model_id"`
	InputTokens   int64  `json:"input_tokens"`
	OutputTokens  int64  `json:"output_tokens"`
	RequestID     string `json:"request_id"`
}
