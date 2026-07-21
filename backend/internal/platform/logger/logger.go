// Package logger 提供基于 log/slog 的结构化日志。
// 级别可通过 SIGHUP 或 admin API 热调整（运行时切换 atomic level）。
package logger

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
)

// LevelHolder 持有可运行时修改的日志级别。
type LevelHolder struct {
	level atomic.Int32 // slog.Level
}

// NewLevelHolder 创建初始级别 holder。
func NewLevelHolder(initial slog.Level) *LevelHolder {
	h := &LevelHolder{}
	h.level.Store(int32(initial))
	return h
}

// Level 返回当前级别。
func (h *LevelHolder) Level() slog.Level {
	return slog.Level(h.level.Load())
}

// SetLevel 修改运行时级别。
func (h *LevelHolder) SetLevel(l slog.Level) {
	h.level.Store(int32(l))
}

// dynamicHandler 包装 slog.LevelHandler，使级别可运行时切换。
type dynamicHandler struct {
	holder *LevelHolder
	inner  slog.Handler
}

func (d *dynamicHandler) Enabled(_ context.Context, l slog.Level) bool {
	return l >= d.holder.Level()
}

func (d *dynamicHandler) Handle(ctx context.Context, r slog.Record) error {
	return d.inner.Handle(ctx, r)
}

func (d *dynamicHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &dynamicHandler{holder: d.holder, inner: d.inner.WithAttrs(attrs)}
}

func (d *dynamicHandler) WithGroup(name string) slog.Handler {
	return &dynamicHandler{holder: d.holder, inner: d.inner.WithGroup(name)}
}

// Logger 封装 slog.Logger 与可变级别。
type Logger struct {
	*slog.Logger
	holder *LevelHolder
}

// SetLevel 运行时调整级别。
func (l *Logger) SetLevel(level slog.Level) {
	l.holder.SetLevel(level)
}

// Level 返回当前级别。
func (l *Logger) Level() slog.Level {
	return l.holder.Level()
}

// New 根据级别与格式创建 Logger。
// format: "json" 或 "text"。
func New(level, format string) *Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	holder := NewLevelHolder(lvl)
	var inner slog.Handler
	// holder 实现 slog.Leveler 接口（Level 方法），使级别可运行时切换。
	opts := &slog.HandlerOptions{Level: holder}
	if strings.ToLower(format) == "text" {
		inner = slog.NewTextHandler(os.Stdout, opts)
	} else {
		inner = slog.NewJSONHandler(os.Stdout, opts)
	}
	h := &dynamicHandler{holder: holder, inner: inner}
	return &Logger{Logger: slog.New(h), holder: holder}
}

// Default 返回一个 info 级别 JSON logger，用于未显式注入时的兜底。
func Default() *Logger {
	return New("info", "json")
}
