// Package buildlog 提供构建日志流式输出的辅助类型与编码器。
// 供 HTTP SSE/Chunked handler 使用：把日志片段编码为 SSE 事件或纯文本块。
package buildlog

import (
	"encoding/json"
	"fmt"
)

// StreamOpts 日志流选项。
type StreamOpts struct {
	// Format 输出格式："sse"（Server-Sent Events）或 "text"（纯文本块）。
	Format string
	// Follow 是否持续跟随（运行中构建）。
	Follow bool
	// Offset 起始字节偏移。
	Offset int64
}

// Event 日志流事件。
type Event struct {
	Type    string `json:"type"`     // "log" | "status" | "done" | "error"
	Source  string `json:"source"`   // "jenkins" | "archive"
	Chunk   string `json:"chunk,omitempty"`
	Message string `json:"message,omitempty"`
}

// EncodeSSE 把事件编码为 SSE 帧。
func EncodeSSE(ev Event) ([]byte, error) {
	data, err := json.Marshal(ev)
	if err != nil {
		return nil, fmt.Errorf("encode sse event: %w", err)
	}
	return []byte(fmt.Sprintf("event: %s\ndata: %s\n\n", ev.Type, string(data))), nil
}

// EncodeText 把日志片段编码为纯文本（直接追加）。
func EncodeText(chunk []byte) []byte {
	return chunk
}
