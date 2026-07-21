package application

import (
	"encoding/json"
	"fmt"
)

// ProbeMethod 探活方式。
type ProbeMethod string

const (
	// ProbeMethodTCP 通过 TCP 端口探活（连接成功即视为应用就绪）。
	ProbeMethodTCP ProbeMethod = "tcp"
	// ProbeMethodProcess 通过进程关键字探活（容器内 pgrep -f <keyword> 命中即视为就绪）。
	ProbeMethodProcess ProbeMethod = "process"
	// ProbeMethodBoth 同时配置 TCP 与进程探活：两者均通过才视为就绪。
	ProbeMethodBoth ProbeMethod = "both"
)

// ProbeConfig 应用探活配置（应用维度，对该应用下所有分组生效）。
// 通过监听 Pod 端口（如 8080）或进程关键字（如 java）决定应用状态。
// 存储位置：vo_applications.metadata["probe"]，不新增 schema 列。
type ProbeConfig struct {
	// Enabled 是否启用探活。false 或缺省时跳过该应用。
	Enabled bool `json:"enabled"`
	// Method 探活方式：tcp | process | both。
	Method ProbeMethod `json:"method,omitempty"`
	// Port TCP 探活端口（如 8080）。Method=tcp/both 时必填。
	Port int `json:"port,omitempty"`
	// ProcessKeyword 进程关键字（如 java / nginx / python）。
	// Method=process/both 时必填，容器内执行 pgrep -f <keyword>。
	ProcessKeyword string `json:"process_keyword,omitempty"`
	// TimeoutSeconds 单次探活超时（秒），默认 5。
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`
	// PeriodSeconds 探活周期（秒），默认 30。
	PeriodSeconds int `json:"period_seconds,omitempty"`
	// FailureThreshold 连续失败次数阈值，达到后视为应用异常并触发通知，默认 3。
	FailureThreshold int `json:"failure_threshold,omitempty"`
}

// Validate 校验探活配置合法性。
func (p *ProbeConfig) Validate() error {
	if p == nil || !p.Enabled {
		return nil
	}
	switch p.Method {
	case ProbeMethodTCP, ProbeMethodBoth:
		if p.Port <= 0 || p.Port > 65535 {
			return fmt.Errorf("probe.port must be in 1-65535 when method=tcp/both")
		}
	case ProbeMethodProcess:
		// 仅进程关键字探活，无需端口。
	default:
		return fmt.Errorf("probe.method must be one of: tcp, process, both")
	}
	if p.Method == ProbeMethodProcess || p.Method == ProbeMethodBoth {
		if p.ProcessKeyword == "" {
			return fmt.Errorf("probe.process_keyword is required when method=process/both")
		}
	}
	if p.TimeoutSeconds < 0 {
		p.TimeoutSeconds = 5
	}
	if p.TimeoutSeconds == 0 {
		p.TimeoutSeconds = 5
	}
	if p.PeriodSeconds <= 0 {
		p.PeriodSeconds = 30
	}
	if p.FailureThreshold <= 0 {
		p.FailureThreshold = 3
	}
	return nil
}

// MarshalProbe 把 ProbeConfig 序列化为 metadata["probe"] 的值（JSON any）。
// 调用方写入 metadata map 时使用：metadata["probe"] = MarshalProbe(p)。
func MarshalProbe(p *ProbeConfig) any {
	if p == nil {
		return nil
	}
	// 经 map 而非结构体传递，确保反序列化时为 map[string]any（与 metadata JSONB 兼容）。
	raw, err := json.Marshal(p)
	if err != nil {
		return nil
	}
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out
}

// UnmarshalProbe 从 metadata["probe"] 反序列化 ProbeConfig。
// 缺省或类型不符返回 nil（视为未配置探活）。
func UnmarshalProbe(metadata map[string]any) *ProbeConfig {
	if metadata == nil {
		return nil
	}
	v, ok := metadata["probe"]
	if !ok || v == nil {
		return nil
	}
	// 直接 marshal/unmarshal 兼容 map[string]any 与其他类型。
	raw, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var p ProbeConfig
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil
	}
	if !p.Enabled {
		return nil
	}
	// 容错：method 为空但配置了 port，按 tcp 处理。
	if p.Method == "" {
		if p.Port > 0 && p.ProcessKeyword == "" {
			p.Method = ProbeMethodTCP
		} else if p.Port == 0 && p.ProcessKeyword != "" {
			p.Method = ProbeMethodProcess
		} else if p.Port > 0 && p.ProcessKeyword != "" {
			p.Method = ProbeMethodBoth
		}
	}
	return &p
}

// ProbeFromApplication 从 application.Metadata 解析探活配置。
func ProbeFromApplication(a *Application) *ProbeConfig {
	if a == nil {
		return nil
	}
	return UnmarshalProbe(a.Metadata)
}
