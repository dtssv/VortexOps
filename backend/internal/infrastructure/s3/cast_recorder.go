package s3

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/minio/minio-go/v7"
)

// CastHeader asciinema cast v2 文件首行 header。
type CastHeader struct {
	Version   int               `json:"version"`
	Width     int               `json:"width"`
	Height    int               `json:"height"`
	Timestamp int64             `json:"timestamp"`
	Env       map[string]string `json:"env,omitempty"`
}

// CastRecorder 流式累积 asciinema cast 事件，Close 时上传到 MinIO。
// 用于 WebSSH / 堡垒机会话录像。
type CastRecorder struct {
	store   *LogStore
	ctx     context.Context
	bucket  string
	key     string
	mu      sync.Mutex
	buf     bytes.Buffer
	start   time.Time
	closed  bool
}

// NewCastRecorder 创建一个录像器，对象 key 形如 sessions/pod/{date}/{sessionID}.cast。
func NewCastRecorder(ctx context.Context, store *LogStore, key string) *CastRecorder {
	return &CastRecorder{
		store: store, ctx: ctx, bucket: store.bucket, key: key, start: time.Now(),
	}
}

// AppendHeader 写入 cast v2 header（必须最先调用）。
func (c *CastRecorder) AppendHeader(width, height uint16) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	hdr := CastHeader{
		Version: 2, Width: int(width), Height: int(height),
		Timestamp: c.start.Unix(),
		Env:       map[string]string{"SHELL": "/bin/sh", "TERM": "xterm-256color"},
	}
	line, err := json.Marshal(hdr)
	if err != nil {
		return err
	}
	c.buf.Write(line)
	c.buf.WriteByte('\n')
	return nil
}

// AppendEvent 追加一条 [elapsed, type, data] 事件。evType: "o"=output, "i"=input。
func (c *CastRecorder) AppendEvent(evType string, data string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	elapsed := time.Since(c.start).Seconds()
	ev := []any{elapsed, evType, data}
	line, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	c.buf.Write(line)
	c.buf.WriteByte('\n')
	return nil
}

// Close 落盘到 MinIO 并返回对象 key。
func (c *CastRecorder) Close() (string, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return c.key, nil
	}
	c.closed = true
	data := c.buf.Bytes()
	c.mu.Unlock()

	if c.store == nil {
		return c.key, nil
	}
	_, err := c.store.client.PutObject(c.ctx, c.bucket, c.key, bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: "application/json"})
	if err != nil {
		return "", fmt.Errorf("upload cast: %w", err)
	}
	return c.key, nil
}
