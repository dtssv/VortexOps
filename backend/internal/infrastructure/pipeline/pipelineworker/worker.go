// Package pipelineworker 是流水线执行 worker：轮询 active runs，按 stage 顺序驱动 executor.Engine，
// 更新 run/stage_run 状态，发布 Kafka 进度事件。支持多实例（apiserver 内嵌或独立 cmd/pipeline-worker）。
package pipelineworker

import (
	"context"
	"fmt"
	"time"

	"github.com/vortexops/vortexops/internal/domain/pipeline"
	"github.com/vortexops/vortexops/internal/infrastructure/kafka"
	"github.com/vortexops/vortexops/internal/infrastructure/pipeline/executor"
	"github.com/vortexops/vortexops/internal/platform/logger"
)

// Repository worker 所需仓储（pipeline.Repository 子集）。
type Repository interface {
	ListActiveRuns(ctx context.Context) ([]*pipeline.Run, error)
	GetRunByID(ctx context.Context, id int64) (*pipeline.Run, error)
	UpdateRun(ctx context.Context, r *pipeline.Run) error
	GetPipelineByID(ctx context.Context, id int64) (*pipeline.Pipeline, error)
	ListStages(ctx context.Context, pipelineID int64) ([]*pipeline.Stage, error)
	ListStageRuns(ctx context.Context, runID int64) ([]*pipeline.StageRun, int64, error)
	CreateStageRun(ctx context.Context, sr *pipeline.StageRun) error
	UpdateStageRun(ctx context.Context, sr *pipeline.StageRun) error
}

// Worker 流水线执行 worker。
type Worker struct {
	repo     Repository
	engine   *executor.Engine
	producer *kafka.Producer
	brokers  []string
	topicKey string
	topic    string
	log      *logger.Logger
	interval time.Duration
}

// New 创建 worker。
func New(repo Repository, engine *executor.Engine, producer *kafka.Producer, brokers []string, topicKey, topic string, log *logger.Logger) *Worker {
	return &Worker{repo: repo, engine: engine, producer: producer, brokers: brokers, topicKey: topicKey,
		topic: topic, log: log, interval: 5 * time.Second}
}

// Run 启动轮询循环，直到 ctx 取消。
func (w *Worker) Run(ctx context.Context) {
	w.log.Info("pipeline worker started")
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			w.log.Info("pipeline worker stopped")
			return
		case <-ticker.C:
			w.tick(ctx)
		}
	}
}

func (w *Worker) tick(ctx context.Context) {
	runs, err := w.repo.ListActiveRuns(ctx)
	if err != nil {
		w.log.Error("list active runs failed", "error", err)
		return
	}
	for _, run := range runs {
		if ctx.Err() != nil {
			return
		}
		w.processRun(ctx, run)
	}
}

func (w *Worker) processRun(ctx context.Context, run *pipeline.Run) {
	p, err := w.repo.GetPipelineByID(ctx, run.PipelineID)
	if err != nil {
		w.log.Error("get pipeline for run failed", "run_id", run.ID, "error", err)
		return
	}
	stages, err := w.repo.ListStages(ctx, p.ID)
	if err != nil {
		w.log.Error("list stages failed", "run_id", run.ID, "error", err)
		return
	}
	// 标记 running。
	if run.Status == pipeline.RunPending {
		run.Status = pipeline.RunRunning
		_ = w.repo.UpdateRun(ctx, run)
	}
	artifacts := map[string]any{}
	// 从已完成的 stage run 恢复 artifacts（重入安全）。
	existingRuns, _, _ := w.repo.ListStageRuns(ctx, run.ID)
	for _, sr := range existingRuns {
		if sr.Status == pipeline.StageRunSucceeded && sr.GateResult != nil {
			if steps, ok := sr.GateResult["steps"].([]any); ok {
				for _, raw := range steps {
					if step, ok := raw.(*executor.StepResult); ok && step.Outputs != nil {
						for k, v := range step.Outputs {
							artifacts[k] = v
						}
					}
				}
			}
		}
	}
	for _, stage := range stages {
		if ctx.Err() != nil {
			return
		}
		// 找到或创建 stage run。
		sr := w.findOrCreateStageRun(ctx, run.ID, stage)
		if sr == nil {
			continue
		}
		if sr.Status == pipeline.StageRunSucceeded {
			continue
		}
		if sr.Status == pipeline.StageRunFailed && stage.OnFailure == pipeline.FailureAbort {
			w.finalizeRun(ctx, run, pipeline.RunFailed, "stage "+stage.Name+" failed")
			return
		}
		now := time.Now()
		sr.Status = pipeline.StageRunRunning
		sr.StartedAt = &now
		_ = w.repo.UpdateStageRun(ctx, sr)
		stageCtx := &executor.StageContext{Pipeline: p, Run: run, Stage: stage, StageRun: sr, Artifacts: artifacts}
		gateResult, status, _ := w.engine.ExecuteStage(ctx, stageCtx)
		finished := time.Now()
		sr.GateResult = gateResult
		sr.Status = pipeline.StageRunStatus(status)
		sr.FinishedAt = &finished
		sr.Message = "completed"
		sr.Audit.UpdatedBy = run.TriggerBy
		_ = w.repo.UpdateStageRun(ctx, sr)
		// 推送进度事件。
		w.publish(ctx, "pipeline.stage.completed", map[string]any{
			"run_id": run.ID, "stage_id": stage.ID, "status": status,
		})
		if sr.Status == pipeline.StageRunFailed {
			if stage.OnFailure == pipeline.FailureAbort {
				w.finalizeRun(ctx, run, pipeline.RunFailed, "stage "+stage.Name+" failed")
				return
			}
			// manual_retry: 暂停运行，等待人工重试。
			if stage.OnFailure == pipeline.FailureManualRetry {
				run.Status = pipeline.RunPaused
				run.CurrentStageSeq = stage.Seq
				_ = w.repo.UpdateRun(ctx, run)
				return
			}
			// continue: 跳过该阶段继续。
		}
		run.CurrentStageSeq = stage.Seq
	}
	w.finalizeRun(ctx, run, pipeline.RunSucceeded, "all stages succeeded")
}

func (w *Worker) findOrCreateStageRun(ctx context.Context, runID int64, stage *pipeline.Stage) *pipeline.StageRun {
	existing, _, err := w.repo.ListStageRuns(ctx, runID)
	if err == nil {
		for _, sr := range existing {
			if sr.StageID == stage.ID {
				return sr
			}
		}
	}
	sr := &pipeline.StageRun{
		PipelineRunID: runID, StageID: stage.ID, Seq: stage.Seq, Status: pipeline.StageRunPending,
	}
	sr.Audit.CreatedBy = 0
	if err := w.repo.CreateStageRun(ctx, sr); err != nil {
		w.log.Error("create stage run failed", "run_id", runID, "stage_id", stage.ID, "error", err)
		return nil
	}
	return sr
}

func (w *Worker) finalizeRun(ctx context.Context, run *pipeline.Run, status pipeline.RunStatus, msg string) {
	now := time.Now()
	run.Status = status
	run.FinishedAt = &now
	run.DurationMs = now.Sub(run.StartedAt).Milliseconds()
	_ = w.repo.UpdateRun(ctx, run)
	w.publish(ctx, "pipeline.run.completed", map[string]any{
		"run_id": run.ID, "pipeline_id": run.PipelineID, "status": string(status), "message": msg,
	})
}

func (w *Worker) publish(ctx context.Context, eventType string, payload any) {
	if w.producer == nil || !w.producer.Enabled() {
		return
	}
	_ = w.producer.Publish(ctx, w.brokers, w.topicKey, w.topic, fmt.Sprintf("evt-%d", time.Now().UnixNano()),
		kafka.NewEvent(eventType, "pipeline-worker", payload))
}
