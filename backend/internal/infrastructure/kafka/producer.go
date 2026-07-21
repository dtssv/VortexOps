// Package kafka 提供 Kafka 生产者封装（segmentio/kafka-go）。
// 当 Kafka 未配置时 Producer 为 no-op，确保开发环境与单元测试不依赖 Kafka。
package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/segmentio/kafka-go"
)

// Producer Kafka 生产者。Enabled=false 时所有写操作均为 no-op。
type Producer struct {
	writers map[string]*kafka.Writer
	mu      sync.Mutex
	enabled bool
}

// NewProducer 创建生产者。topicMap 预创建常用 topic 的 writer。
func NewProducer(brokers []string, topicMap map[string]string) *Producer {
	if len(brokers) == 0 {
		return &Producer{enabled: false}
	}
	p := &Producer{writers: map[string]*kafka.Writer{}, enabled: true}
	for key, topic := range topicMap {
		p.writers[key] = newWriter(brokers, topic)
	}
	return p
}

func newWriter(brokers []string, topic string) *kafka.Writer {
	return &kafka.Writer{
		Addr:         kafka.TCP(brokers...),
		Topic:        topic,
		Balancer:     &kafka.LeastBytes{},
		BatchTimeout: 10 * time.Millisecond,
		RequiredAcks: kafka.RequireAll,
		Async:        false,
	}
}

// Enabled 是否启用 Kafka。
func (p *Producer) Enabled() bool { return p.enabled }

// writer 返回指定 key 的 writer；不存在则懒创建（需 brokers，已禁用时返回 nil）。
func (p *Producer) writer(brokers []string, key, topic string) *kafka.Writer {
	if !p.enabled {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	w, ok := p.writers[key]
	if !ok {
		w = newWriter(brokers, topic)
		p.writers[key] = w
	}
	return w
}

// Publish 向指定 topic key 发送一条 JSON 事件。未启用时直接返回 nil。
func (p *Producer) Publish(ctx context.Context, brokers []string, topicKey, topic, key string, payload any) error {
	if !p.enabled {
		return nil
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	w := p.writer(brokers, topicKey, topic)
	if w == nil {
		return nil
	}
	if err := w.WriteMessages(ctx, kafka.Message{
		Key:   []byte(key),
		Value: data,
		Time:  time.Now(),
	}); err != nil {
		return fmt.Errorf("write kafka message: %w", err)
	}
	return nil
}

// Close 关闭所有 writer。
func (p *Producer) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	var firstErr error
	for _, w := range p.writers {
		if err := w.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Event 通用事件信封。
type Event struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Source    string    `json:"source,omitempty"`
	Subject   string    `json:"subject,omitempty"`
	Payload   any       `json:"payload"`
}

// NewEvent 构造事件。
func NewEvent(eventType, source string, payload any) *Event {
	return &Event{Type: eventType, Timestamp: time.Now(), Source: source, Payload: payload}
}
