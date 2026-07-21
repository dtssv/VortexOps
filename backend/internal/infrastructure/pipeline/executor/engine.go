// Package executor 是流水线阶段执行器。每个 stage 包含若干 step（build/test/scan/image/deploy/verify/promote），
// 由对应 StepExecutor 执行。执行结果写入 stage run 的 gate_result。
package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/vortexops/vortexops/internal/domain/pipeline"
)

// StepKind 阶段内步骤类型。
type StepKind string

const (
	StepBuild  StepKind = "build"
	StepTest   StepKind = "test"
	StepScan   StepKind = "scan"
	StepImage  StepKind = "image"
	StepDeploy StepKind = "deploy"
	StepVerify StepKind = "verify"
	StepPromote StepKind = "promote"
)

// StepResult 单步骤执行结果。
type StepResult struct {
	Kind     StepKind              `json:"kind"`
	Name     string                `json:"name"`
	Status   string                `json:"status"`
	Message  string                `json:"message,omitempty"`
	Outputs  map[string]any        `json:"outputs,omitempty"`
	Duration time.Duration         `json:"-"`
	StartedAt time.Time            `json:"started_at"`
	FinishedAt time.Time           `json:"finished_at"`
}

// StageContext 阶段执行上下文（pipeline + run + stage 定义 + 累积产物）。
type StageContext struct {
	Pipeline    *pipeline.Pipeline
	Run         *pipeline.Run
	Stage       *pipeline.Stage
	StageRun    *pipeline.StageRun
	// 累积产物（前序阶段输出），executor 可读取/写入。
	Artifacts   map[string]any
}

// StepExecutor 单步骤执行器接口。各领域适配器实现。
type StepExecutor interface {
	Kind() StepKind
	Execute(ctx context.Context, stageCtx *StageContext, step map[string]any) (*StepResult, error)
}

// Engine 阶段执行引擎。聚合所有 step executor。
type Engine struct {
	executors map[StepKind]StepExecutor
}

// NewEngine 创建引擎。
func NewEngine() *Engine { return &Engine{executors: map[StepKind]StepExecutor{}} }

// Register 注册步骤执行器。
func (e *Engine) Register(ex StepExecutor) {
	e.executors[ex.Kind()] = ex
}

// ExecuteStage 执行一个阶段：按 stage.Params 中的 steps 列表顺序/并行执行，汇总结果。
// 返回聚合 gate_result 与整体状态。
func (e *Engine) ExecuteStage(ctx context.Context, stageCtx *StageContext) (map[string]any, string, error) {
	steps, _ := stageCtx.Stage.Params["steps"].([]any)
	results := make([]*StepResult, 0, len(steps))
	overallStatus := string(pipeline.StageRunSucceeded)
	for _, raw := range steps {
		step, _ := raw.(map[string]any)
		kindStr, _ := step["kind"].(string)
		name, _ := step["name"].(string)
		kind := StepKind(kindStr)
		ex, ok := e.executors[kind]
		if !ok {
			overallStatus = string(pipeline.StageRunFailed)
			results = append(results, &StepResult{Kind: kind, Name: name, Status: "failed", Message: "no executor for step kind"})
			break
		}
		started := time.Now()
		res, err := ex.Execute(ctx, stageCtx, step)
		finished := time.Now()
		if err != nil {
			overallStatus = string(pipeline.StageRunFailed)
			results = append(results, &StepResult{Kind: kind, Name: name, Status: "failed", Message: err.Error(), StartedAt: started, FinishedAt: finished})
			break
		}
		if res.StartedAt.IsZero() {
			res.StartedAt = started
		}
		if res.FinishedAt.IsZero() {
			res.FinishedAt = finished
		}
		results = append(results, res)
		if res.Status != "succeeded" && res.Status != "skipped" {
			overallStatus = string(pipeline.StageRunFailed)
			break
		}
		// 合并产物。
		if res.Outputs != nil {
			if stageCtx.Artifacts == nil {
				stageCtx.Artifacts = map[string]any{}
			}
			for k, v := range res.Outputs {
				stageCtx.Artifacts[k] = v
			}
		}
	}
	gateResult := map[string]any{
		"steps":   results,
		"status":  overallStatus,
		"summary": fmt.Sprintf("%d steps executed", len(results)),
	}
	return gateResult, overallStatus, nil
}

// MarshalGateResult 序列化 gate_result 为 map（仓储以 JSONB 存储）。
func MarshalGateResult(results []*StepResult) map[string]any {
	return map[string]any{"steps": results}
}

// EncodeJSON 编码为 JSON 字节（调试/日志用）。
func EncodeJSON(v any) ([]byte, error) {
	return json.Marshal(v)
}
