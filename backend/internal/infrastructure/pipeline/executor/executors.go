// Package executor 的步骤执行器实现：build/scan/deploy/verify/promote/image。
// 适配 buildapp/releaseapp 等现有应用服务。
package executor

import (
	"context"
	"fmt"
	"time"

	"github.com/vortexops/vortexops/internal/application/buildapp"
	"github.com/vortexops/vortexops/internal/application/releaseapp"
	"github.com/vortexops/vortexops/internal/domain/build"
	"github.com/vortexops/vortexops/internal/domain/release"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// --- build ---

// BuildExecutor 触发构建（buildapp.Service.TriggerBuild）。
type BuildExecutor struct {
	buildSvc    BuildTrigger
	jenkinsFact buildapp.JenkinsClientFactory
}

// BuildTrigger 触发构建的最小接口。
type BuildTrigger interface {
	TriggerBuild(ctx context.Context, in buildapp.TriggerBuildInput, jenkinsFactory buildapp.JenkinsClientFactory) (*build.Build, error)
}

// BuildWaiter 可选接口：等待构建到达终态。BuildExecutor 在 wait=true 时使用。
type BuildWaiter interface {
	WaitBuildTerminal(ctx context.Context, buildID int64) (*build.Build, error)
}

// NewBuildExecutor 创建构建执行器。jenkinsFact 可为 nil（仅记录 build，不触发 Jenkins）。
func NewBuildExecutor(svc BuildTrigger, jf buildapp.JenkinsClientFactory) *BuildExecutor {
	return &BuildExecutor{buildSvc: svc, jenkinsFact: jf}
}

// Kind 步骤类型。
func (e *BuildExecutor) Kind() StepKind { return StepBuild }

// Execute 执行构建步骤。
// 若步骤参数 wait=true，则轮询构建状态直到终态再返回，使流水线后续阶段可拿到 output_image_id。
func (e *BuildExecutor) Execute(ctx context.Context, stageCtx *StageContext, step map[string]any) (*StepResult, error) {
	appID, _ := step["application_id"].(float64)
	gitSourceID, _ := step["git_source_id"].(float64)
	templateID, _ := step["build_template_id"].(float64)
	if appID == 0 || gitSourceID == 0 {
		return failed(step, "application_id and git_source_id required"), nil
	}
	refType := build.RefBranch
	if rt, ok := step["ref_type"].(string); ok && rt != "" {
		refType = build.RefType(rt)
	}
	refValue, _ := step["ref_value"].(string)
	res, err := e.buildSvc.TriggerBuild(ctx, buildapp.TriggerBuildInput{
		ApplicationID:   int64(appID),
		GitSourceID:     int64(gitSourceID),
		RefType:         refType,
		RefValue:        refValue,
		BuildTemplateID: int64(templateID),
	}, e.jenkinsFact)
	if err != nil {
		if ae, ok := apperr.As(err); ok {
			return failed(step, "trigger build: "+ae.Message), nil
		}
		return failed(step, "trigger build: "+err.Error()), nil
	}
	// 可选等待构建终态（wait=true 时阻塞直到 success/failed/canceled/timeout）。
	if wait, _ := step["wait"].(bool); wait {
		if waiter, ok := e.buildSvc.(BuildWaiter); ok {
			final, werr := waiter.WaitBuildTerminal(ctx, res.ID)
			if werr != nil {
				return failed(step, "wait build: "+werr.Error()), nil
			}
			status := "succeeded"
			msg := fmt.Sprintf("build %d %s", res.ID, final.Status)
			if final.Status != build.BuildSuccess {
				status = "failed"
			}
			outputs := map[string]any{"build_id": res.ID}
			if final.OutputImageID != 0 {
				outputs["image_id"] = float64(final.OutputImageID)
			}
			return &StepResult{
				Kind: StepBuild, Name: nameOf(step), Status: status,
				Outputs: outputs, Message: msg,
			}, nil
		}
	}
	return &StepResult{
		Kind: StepBuild, Name: nameOf(step), Status: "succeeded",
		Outputs: map[string]any{"build_id": res.ID},
		Message: fmt.Sprintf("build %d triggered", res.ID),
	}, nil
}

// --- scan ---

// ScanExecutor 调用 Harbor CVE 扫描门禁（实际扫描由 build 流程触发；这里读取扫描结果并按阈值判定）。
type ScanExecutor struct {
	harbor ScanReader
}

// ScanReader 读取镜像 CVE 扫描结果。
type ScanReader interface {
	GetImageScanResult(ctx context.Context, imageID int64) (ScanSummary, error)
}

// ScanSummary 扫描摘要。
type ScanSummary struct {
	Critical int `json:"critical"`
	High     int `json:"high"`
	Medium   int `json:"medium"`
	Low      int `json:"low"`
}

// NewScanExecutor 创建扫描执行器。harbor 可为 nil（无扫描数据时跳过）。
func NewScanExecutor(h ScanReader) *ScanExecutor { return &ScanExecutor{harbor: h} }

// Kind 步骤类型。
func (e *ScanExecutor) Kind() StepKind { return StepScan }

// Execute 检查 CVE 阈值。
func (e *ScanExecutor) Execute(ctx context.Context, stageCtx *StageContext, step map[string]any) (*StepResult, error) {
	if e.harbor == nil {
		return &StepResult{Kind: StepScan, Name: nameOf(step), Status: "skipped", Message: "no scan reader configured"}, nil
	}
	imageID := pickImageID(stageCtx, step)
	if imageID == 0 {
		return failed(step, "no image to scan"), nil
	}
	summary, err := e.harbor.GetImageScanResult(ctx, imageID)
	if err != nil {
		return failed(step, "read scan result: "+err.Error()), nil
	}
	maxCritical, _ := step["max_critical"].(float64)
	maxHigh, _ := step["max_high"].(float64)
	if int(summary.Critical) > int(maxCritical) {
		return failed(step, fmt.Sprintf("critical CVEs %d exceed threshold %d", summary.Critical, int(maxCritical))), nil
	}
	if int(summary.High) > int(maxHigh) {
		return failed(step, fmt.Sprintf("high CVEs %d exceed threshold %d", summary.High, int(maxHigh))), nil
	}
	return &StepResult{
		Kind: StepScan, Name: nameOf(step), Status: "succeeded",
		Outputs: map[string]any{"scan": summary, "image_id": imageID},
		Message: "CVE scan passed",
	}, nil
}

// --- deploy ---

// DeployExecutor 触发发布到目标 group（releaseapp.Service.TriggerRelease）。
type DeployExecutor struct {
	releaseSvc ReleaseTrigger
}

// ReleaseTrigger 发布触发接口。
type ReleaseTrigger interface {
	TriggerRelease(ctx context.Context, in releaseapp.TriggerReleaseInput) (*release.Release, error)
}

// NewDeployExecutor 创建部署执行器。
func NewDeployExecutor(svc ReleaseTrigger) *DeployExecutor { return &DeployExecutor{releaseSvc: svc} }

// Kind 步骤类型。
func (e *DeployExecutor) Kind() StepKind { return StepDeploy }

// Execute 触发发布。
func (e *DeployExecutor) Execute(ctx context.Context, stageCtx *StageContext, step map[string]any) (*StepResult, error) {
	groupID, _ := step["group_id"].(float64)
	imageID := pickImageID(stageCtx, step)
	if groupID == 0 {
		return failed(step, "group_id required"), nil
	}
	relType := release.ReleaseRolling
	if rt, ok := step["release_type"].(string); ok && rt != "" {
		relType = release.ReleaseType(rt)
	}
	res, err := e.releaseSvc.TriggerRelease(ctx, releaseapp.TriggerReleaseInput{
		GroupID:     int64(groupID),
		ImageID:     imageID,
		ReleaseType: relType,
		TriggeredBy: stageCtx.Run.TriggerBy,
	})
	if err != nil {
		if _, ok := apperr.As(err); ok {
			return failed(step, err.Error()), nil
		}
		return nil, err
	}
	return &StepResult{
		Kind: StepDeploy, Name: nameOf(step), Status: "succeeded",
		Outputs: map[string]any{"release_id": res.ID, "image_id": imageID},
		Message: fmt.Sprintf("release %d triggered", res.ID),
	}, nil
}

// --- verify ---

// VerifyExecutor 部署后健康验证（检查 release 状态 + 简单冒烟）。
type VerifyExecutor struct {
	releaseSvc ReleaseReader
}

// ReleaseReader 发布读取接口。
type ReleaseReader interface {
	GetRelease(ctx context.Context, id int64) (*release.Release, error)
}

// NewVerifyExecutor 创建验证执行器。
func NewVerifyExecutor(svc ReleaseReader) *VerifyExecutor { return &VerifyExecutor{releaseSvc: svc} }

// Kind 步骤类型。
func (e *VerifyExecutor) Kind() StepKind { return StepVerify }

// Execute 轮询 release 状态直到成功或超时。
func (e *VerifyExecutor) Execute(ctx context.Context, stageCtx *StageContext, step map[string]any) (*StepResult, error) {
	relID := pickReleaseID(stageCtx)
	if relID == 0 {
		return failed(step, "no release to verify"), nil
	}
	timeoutSec := 300
	if t, ok := step["timeout_sec"].(float64); ok && t > 0 {
		timeoutSec = int(t)
	}
	deadline := time.Now().Add(time.Duration(timeoutSec) * time.Second)
	for time.Now().Before(deadline) {
		rel, err := e.releaseSvc.GetRelease(ctx, relID)
		if err != nil {
			return failed(step, "get release: "+err.Error()), nil
		}
		if isReleaseTerminal(rel.Status) {
			if rel.Status == release.StatusSucceeded {
				return &StepResult{Kind: StepVerify, Name: nameOf(step), Status: "succeeded",
					Outputs: map[string]any{"release_id": relID}, Message: "release verified"}, nil
			}
			return failed(step, "release "+string(rel.Status)+" during verify"), nil
		}
		select {
		case <-ctx.Done():
			return failed(step, "verify canceled"), nil
		case <-time.After(5 * time.Second):
		}
	}
	return failed(step, "verify timeout"), nil
}

// --- promote ---

// PromoteExecutor 将镜像晋升到目标环境：对 target_env 指定的每个 group_id 触发 releaseapp.TriggerRelease。
// 实现跨环境实际部署，而非仅记录晋升标记。
type PromoteExecutor struct {
	releaseSvc ReleaseTrigger
}

// NewPromoteExecutor 创建晋升执行器。releaseSvc 为 nil 时降级为仅记录（向后兼容）。
func NewPromoteExecutor(svc ReleaseTrigger) *PromoteExecutor {
	return &PromoteExecutor{releaseSvc: svc}
}

// Kind 步骤类型。
func (e *PromoteExecutor) Kind() StepKind { return StepPromote }

// Execute 对目标环境 group 触发实际部署。
// step 参数：target_env（string）、group_ids（[]number，目标环境分组）、image_id（可选，默认取上游产物）。
func (e *PromoteExecutor) Execute(ctx context.Context, stageCtx *StageContext, step map[string]any) (*StepResult, error) {
	imageID := pickImageID(stageCtx, step)
	targetEnv, _ := step["target_env"].(string)
	if imageID == 0 || targetEnv == "" {
		return failed(step, "image_id and target_env required"), nil
	}
	// 解析目标 group_ids（数组或单个 number）。
	groupIDs := pickGroupIDs(step)
	if len(groupIDs) == 0 {
		return failed(step, "group_ids required for promote"), nil
	}
	if e.releaseSvc == nil {
		return &StepResult{
			Kind: StepPromote, Name: nameOf(step), Status: "succeeded",
			Outputs: map[string]any{"image_id": imageID, "target_env": targetEnv},
			Message: fmt.Sprintf("artifact promoted to %s (no release service, dry-run)", targetEnv),
		}, nil
	}
	relType := release.ReleaseRolling
	if rt, ok := step["release_type"].(string); ok && rt != "" {
		relType = release.ReleaseType(rt)
	}
	releaseIDs := make([]any, 0, len(groupIDs))
	for _, gid := range groupIDs {
		rel, err := e.releaseSvc.TriggerRelease(ctx, releaseapp.TriggerReleaseInput{
			GroupID:     gid,
			ImageID:     imageID,
			ReleaseType: relType,
			TriggeredBy: stageCtx.Run.TriggerBy,
		})
		if err != nil {
			if ae, ok := apperr.As(err); ok {
				return failed(step, fmt.Sprintf("promote to group %d: %s", gid, ae.Message)), nil
			}
			return failed(step, fmt.Sprintf("promote to group %d: %v", gid, err)), nil
		}
		releaseIDs = append(releaseIDs, float64(rel.ID))
	}
	return &StepResult{
		Kind: StepPromote, Name: nameOf(step), Status: "succeeded",
		Outputs: map[string]any{"image_id": imageID, "target_env": targetEnv, "release_ids": releaseIDs},
		Message: fmt.Sprintf("artifact promoted to %s (%d groups)", targetEnv, len(groupIDs)),
	}, nil
}

// pickGroupIDs 从 step 参数解析 group_ids（支持数组或单值）。
func pickGroupIDs(step map[string]any) []int64 {
	if arr, ok := step["group_ids"].([]any); ok {
		ids := make([]int64, 0, len(arr))
		for _, v := range arr {
			if f, ok := v.(float64); ok && f > 0 {
				ids = append(ids, int64(f))
			}
		}
		return ids
	}
	if f, ok := step["group_id"].(float64); ok && f > 0 {
		return []int64{int64(f)}
	}
	return nil
}

// --- helpers ---

func nameOf(step map[string]any) string {
	n, _ := step["name"].(string)
	if n == "" {
		return "step"
	}
	return n
}

func failed(step map[string]any, msg string) *StepResult {
	return &StepResult{Kind: StepKind(str(step["kind"])), Name: nameOf(step), Status: "failed", Message: msg,
		StartedAt: time.Now(), FinishedAt: time.Now()}
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// pickImageID 从累积产物或步骤参数取 image_id。
func pickImageID(stageCtx *StageContext, step map[string]any) int64 {
	if v, ok := step["image_id"].(float64); ok && v > 0 {
		return int64(v)
	}
	if stageCtx.Artifacts != nil {
		if v, ok := stageCtx.Artifacts["image_id"].(float64); ok {
			return int64(v)
		}
	}
	return 0
}

// pickReleaseID 从累积产物取 release_id。
func pickReleaseID(stageCtx *StageContext) int64 {
	if stageCtx.Artifacts != nil {
		if v, ok := stageCtx.Artifacts["release_id"].(float64); ok {
			return int64(v)
		}
	}
	return 0
}

// isReleaseTerminal 判断 release 状态是否终态。
func isReleaseTerminal(s release.Status) bool {
	switch s {
	case release.StatusSucceeded, release.StatusFailed, release.StatusAborted, release.StatusRolledBack:
		return true
	}
	return false
}
