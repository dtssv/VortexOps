package releaseapp

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/vortexops/vortexops/internal/domain"
	"github.com/vortexops/vortexops/internal/domain/application"
	"github.com/vortexops/vortexops/internal/domain/release"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// --- 多集群发布编排 ---

// TriggerOrchestrationInput 多集群发布编排输入。
type TriggerOrchestrationInput struct {
	WorkspaceID      int64
	ApplicationID    int64
	Name             string
	Strategy         release.OrchestrationStrategy
	ImageID          int64
	ConfigVersion    int
	Replicas         int
	MaxSurge         string
	MaxUnavailable   string
	BatchSize        int
	BatchIntervalSec int
	TriggeredBy      int64
	TriggerSource    release.TriggerSource
	Targets          []release.OrchestrationTargetInput
}

// TriggerOrchestration 触发多集群发布编排。
func (s *Service) TriggerOrchestration(ctx context.Context, in TriggerOrchestrationInput) (*release.Orchestration, error) {
	if s.orchRepo == nil {
		return nil, apperr.Internal("orchestration repository not configured", nil)
	}
	if in.ApplicationID == 0 {
		return nil, apperr.Validation("application_id is required", nil)
	}
	if len(in.Targets) == 0 {
		return nil, apperr.Validation("at least one target is required", nil)
	}
	if in.Strategy == "" {
		in.Strategy = release.OrchSequential
	}
	if in.TriggerSource == "" {
		in.TriggerSource = release.TriggerManual
	}

	o := &release.Orchestration{
		WorkspaceID:      in.WorkspaceID,
		ApplicationID:    in.ApplicationID,
		Name:             in.Name,
		Strategy:         in.Strategy,
		Status:           release.OrchStatusPending,
		ImageID:          in.ImageID,
		ConfigVersion:    in.ConfigVersion,
		Replicas:         in.Replicas,
		MaxSurge:         in.MaxSurge,
		MaxUnavailable:   in.MaxUnavailable,
		BatchSize:        in.BatchSize,
		BatchIntervalSec: in.BatchIntervalSec,
		TriggeredBy:      in.TriggeredBy,
		TriggerSource:    in.TriggerSource,
		CreatedBy:        in.TriggeredBy,
		UpdatedBy:        in.TriggeredBy,
	}
	targets := make([]release.OrchestrationTarget, 0, len(in.Targets))
	for i, ti := range in.Targets {
		seq := ti.Seq
		if in.Strategy == release.OrchSequential && seq == 0 {
			seq = i + 1
		}
		imgID := ti.ImageID
		if imgID == 0 {
			imgID = in.ImageID
		}
		targets = append(targets, release.OrchestrationTarget{
			GroupID:          ti.GroupID,
			ClusterID:        ti.ClusterID,
			ImageID:          imgID,
			ConfigVersion:    ti.ConfigVersion,
			Replicas:         ti.Replicas,
			Seq:              seq,
			BatchSize:        ti.BatchSize,
			BatchIntervalSec: ti.BatchIntervalSec,
		})
	}

	if err := s.orchRepo.CreateOrchestration(ctx, o, targets); err != nil {
		return nil, apperr.Internal("create orchestration", err)
	}

	go s.executeOrchestration(context.Background(), o, targets)
	return o, nil
}

// executeOrchestration 异步执行编排：按策略对每个 target 调用 TriggerRelease。
func (s *Service) executeOrchestration(ctx context.Context, o *release.Orchestration, targets []release.OrchestrationTarget) {
	startedAt := time.Now()
	cur, _ := s.orchRepo.UpdateOrchestrationStatus(ctx, o.ID, release.OrchStatusRunning, 0, "", o.Version)
	if cur != nil {
		o.Version = cur.Version
	}

	total := len(targets)
	succeeded := 0
	var firstErr string

	switch o.Strategy {
	case release.OrchParallel:
		var wg sync.WaitGroup
		var mu sync.Mutex
		for i := range targets {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				ok, err := s.executeOrchestrationTarget(ctx, o, &targets[idx])
				mu.Lock()
				if ok {
					succeeded++
				} else if err != "" && firstErr == "" {
					firstErr = err
				}
				mu.Unlock()
			}(i)
		}
		wg.Wait()
	default: // sequential / canary
		for i := range targets {
			ok, err := s.executeOrchestrationTarget(ctx, o, &targets[i])
			if ok {
				succeeded++
			} else if err != "" && firstErr == "" {
				firstErr = err
			}
			if !ok && o.Strategy == release.OrchSequential {
				break // 顺序策略遇失败即停止后续 target
			}
			// 推进进度
			progress := (i + 1) * 100 / total
			cur, _ := s.orchRepo.UpdateOrchestrationStatus(ctx, o.ID, release.OrchStatusRunning, progress, "", o.Version)
			if cur != nil {
				o.Version = cur.Version
			}
		}
	}

	finalStatus := release.OrchStatusSucceeded
	if succeeded == 0 {
		finalStatus = release.OrchStatusFailed
	} else if succeeded < total {
		finalStatus = release.OrchStatusFailed
	}
	cur, _ = s.orchRepo.CompleteOrchestration(ctx, o.ID, finalStatus, time.Since(startedAt).Milliseconds(), time.Now(), o.Version)
	if cur != nil {
		o.Version = cur.Version
	}
	_ = firstErr
}

// executeOrchestrationTarget 执行单个编排目标：调用 TriggerRelease 并轮询发布终态。
func (s *Service) executeOrchestrationTarget(ctx context.Context, o *release.Orchestration, t *release.OrchestrationTarget) (bool, string) {
	now := time.Now()
	t.Status = release.TargetRunning
	t.StartedAt = &now
	_ = s.orchRepo.UpdateTargetStatus(ctx, t)

	batchSize := t.BatchSize
	if batchSize == 0 {
		batchSize = o.BatchSize
	}
	batchInterval := t.BatchIntervalSec
	if batchInterval == 0 {
		batchInterval = o.BatchIntervalSec
	}
	imageID := t.ImageID
	if imageID == 0 {
		imageID = o.ImageID
	}

	// 金丝雀批次：分批发布（每批 batchSize 副本），等待就绪后继续。
	if o.Strategy == release.OrchCanary && batchSize > 0 && t.Replicas > batchSize {
		return s.executeCanaryTarget(ctx, o, t, imageID, batchSize, batchInterval)
	}

	rel, err := s.TriggerRelease(ctx, TriggerReleaseInput{
		GroupID:               t.GroupID,
		ImageID:               imageID,
		ConfigVersion:         t.ConfigVersion,
		Replicas:              t.Replicas,
		Strategy:              release.StrategyRolling,
		MaxSurge:              o.MaxSurge,
		MaxUnavailable:        o.MaxUnavailable,
		BatchSize:             batchSize,
		BatchIntervalSec:      batchInterval,
		TriggeredBy:           o.TriggeredBy,
		TriggerSource:         release.TriggerAPI,
		AutoRollbackOnFailure: false,
	})
	if err != nil {
		finished := time.Now()
		t.Status = release.TargetFailed
		t.FailureReason = err.Error()
		t.FinishedAt = &finished
		_ = s.orchRepo.UpdateTargetStatus(ctx, t)
		return false, err.Error()
	}
	t.ReleaseID = rel.ID
	_ = s.orchRepo.UpdateTargetStatus(ctx, t)

	// 轮询发布终态。
	finalRel, werr := s.waitForReleaseTerminal(ctx, rel.ID, 15*time.Minute)
	finished := time.Now()
	t.FinishedAt = &finished
	if werr != nil || finalRel == nil || finalRel.Status != release.StatusSucceeded {
		t.Status = release.TargetFailed
		if werr != nil {
			t.FailureReason = werr.Error()
		} else if finalRel != nil {
			t.FailureReason = finalRel.FailureReason
		}
		_ = s.orchRepo.UpdateTargetStatus(ctx, t)
		return false, t.FailureReason
	}
	t.Status = release.TargetSucceeded
	_ = s.orchRepo.UpdateTargetStatus(ctx, t)
	return true, ""
}

// executeCanaryTarget 金丝雀分批发布：逐批 patch 副本并等待就绪。
func (s *Service) executeCanaryTarget(ctx context.Context, o *release.Orchestration, t *release.OrchestrationTarget, imageID int64, batchSize, batchInterval int) (bool, string) {
	g, err := s.groupRepo.GetGroup(ctx, t.GroupID)
	if err != nil {
		if errors.Is(err, application.ErrGroupNotFound) {
			return false, "group not found"
		}
		return false, err.Error()
	}

	// 解析镜像（与 TriggerRelease 一致的校验）。
	img, err := s.imageRepo.GetImage(ctx, imageID)
	if err != nil {
		return false, fmt.Sprintf("get image: %v", err)
	}

	// 渲染工作负载（复用 executeRelease 内部逻辑太重，这里直接触发首批发布后逐步扩容）。
	// v1 实现：先以 batchSize 触发发布，就绪后逐批扩容至目标副本。
	total := t.Replicas
	if total == 0 {
		total = g.Replicas
	}
	if total <= 0 {
		return false, "no replicas to deploy"
	}

	// 首批发布。
	firstBatch := batchSize
	if firstBatch > total {
		firstBatch = total
	}
	rel, err := s.TriggerRelease(ctx, TriggerReleaseInput{
		GroupID:          t.GroupID,
		ImageID:          imageID,
		ConfigVersion:    t.ConfigVersion,
		Replicas:         firstBatch,
		Strategy:         release.StrategyRolling,
		MaxSurge:         o.MaxSurge,
		MaxUnavailable:   o.MaxUnavailable,
		TriggeredBy:      o.TriggeredBy,
		TriggerSource:    release.TriggerAPI,
	})
	if err != nil {
		return false, err.Error()
	}
	t.ReleaseID = rel.ID
	_ = s.orchRepo.UpdateTargetStatus(ctx, t)

	if _, werr := s.waitForReleaseTerminal(ctx, rel.ID, 15*time.Minute); werr != nil {
		finished := time.Now()
		t.Status = release.TargetFailed
		t.FailureReason = werr.Error()
		t.FinishedAt = &finished
		_ = s.orchRepo.UpdateTargetStatus(ctx, t)
		return false, werr.Error()
	}

	// 后续批次：直接 scale deployment 到下一批副本数。
	clientset, err := s.clientProvider.GetClient(ctx, t.ClusterID)
	if err != nil {
		return false, fmt.Sprintf("get k8s client: %v", err)
	}
	current := firstBatch
	for current < total {
		next := current + batchSize
		if next > total {
			next = total
		}
		if err := scaleWorkload(ctx, clientset, g, next); err != nil {
			return false, fmt.Sprintf("scale batch %d->%d: %v", current, next, err)
		}
		if err := s.waitForRollout(ctx, clientset, g, next, 0); err != nil {
			return false, fmt.Sprintf("rollout batch %d: %v", next, err)
		}
		current = next
		if current < total && batchInterval > 0 {
			select {
			case <-ctx.Done():
				return false, ctx.Err().Error()
			case <-time.After(time.Duration(batchInterval) * time.Second):
			}
		}
	}

	finished := time.Now()
	t.Status = release.TargetSucceeded
	t.FinishedAt = &finished
	_ = s.orchRepo.UpdateTargetStatus(ctx, t)
	_ = img
	return true, ""
}

// waitForReleaseTerminal 轮询发布直到进入终态。
func (s *Service) waitForReleaseTerminal(ctx context.Context, releaseID int64, timeout time.Duration) (*release.Release, error) {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			rel, err := s.repo.GetReleaseByID(ctx, releaseID)
			if err != nil {
				return nil, err
			}
			if isTerminalStatus(rel.Status) {
				return rel, nil
			}
			if time.Now().After(deadline) {
				return nil, fmt.Errorf("release %d timed out after %s", releaseID, timeout)
			}
		}
	}
}

func isTerminalStatus(s release.Status) bool {
	switch s {
	case release.StatusSucceeded, release.StatusFailed, release.StatusAborted, release.StatusRolledBack:
		return true
	}
	return false
}

// GetOrchestration 查询编排。
func (s *Service) GetOrchestration(ctx context.Context, id int64) (*release.Orchestration, error) {
	if s.orchRepo == nil {
		return nil, apperr.Internal("orchestration repository not configured", nil)
	}
	o, err := s.orchRepo.GetOrchestration(ctx, id)
	if err != nil {
		if errors.Is(err, release.ErrOrchestrationNotFound) {
			return nil, apperr.NotFound("orchestration", strconv.FormatInt(id, 10))
		}
		return nil, apperr.Internal("get orchestration", err)
	}
	return o, nil
}

// ListOrchestrationTargets 列出编排目标。
func (s *Service) ListOrchestrationTargets(ctx context.Context, orchestrationID int64) ([]*release.OrchestrationTarget, error) {
	if s.orchRepo == nil {
		return nil, apperr.Internal("orchestration repository not configured", nil)
	}
	return s.orchRepo.ListTargets(ctx, orchestrationID)
}

// ListOrchestrations 分页列出编排。
func (s *Service) ListOrchestrations(ctx context.Context, appID int64, page, size int) ([]*release.Orchestration, int64, error) {
	if s.orchRepo == nil {
		return nil, 0, apperr.Internal("orchestration repository not configured", nil)
	}
	return s.orchRepo.ListOrchestrations(ctx, appID, (page-1)*size, size)
}

// AbortOrchestration 中止编排（仅标记，运行中的 target 自行完成）。
func (s *Service) AbortOrchestration(ctx context.Context, id, operatorID int64) (*release.Orchestration, error) {
	if s.orchRepo == nil {
		return nil, apperr.Internal("orchestration repository not configured", nil)
	}
	o, err := s.orchRepo.GetOrchestration(ctx, id)
	if err != nil {
		if errors.Is(err, release.ErrOrchestrationNotFound) {
			return nil, apperr.NotFound("orchestration", strconv.FormatInt(id, 10))
		}
		return nil, apperr.Internal("get orchestration", err)
	}
	if o.Status != release.OrchStatusRunning && o.Status != release.OrchStatusPending {
		return nil, apperr.BusinessRule("orchestration cannot be aborted in current state", release.ErrOrchestrationNotCancellable)
	}
	cur, err := s.orchRepo.CompleteOrchestration(ctx, id, release.OrchStatusAborted, time.Since(getTime(o.StartedAt)).Milliseconds(), time.Now(), o.Version)
	if err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return nil, apperr.Conflict("orchestration was modified concurrently, please refresh", err)
		}
		return nil, apperr.Internal("abort orchestration", err)
	}
	return cur, nil
}

func getTime(t *time.Time) time.Time {
	if t == nil {
		return time.Now()
	}
	return *t
}

// scaleWorkload 将 group 对应的工作负载副本数 patch 到 replicas。
func scaleWorkload(ctx context.Context, clientset kubernetes.Interface, g *application.Group, replicas int) error {
	r := int32(replicas)
	switch g.Workload.Type {
	case application.WorkloadDeployment:
		d, err := clientset.AppsV1().Deployments(g.Namespace).Get(ctx, g.DeploymentName, metav1GetOpts())
		if err != nil {
			return err
		}
		d.Spec.Replicas = &r
		_, err = clientset.AppsV1().Deployments(g.Namespace).Update(ctx, d, metav1.UpdateOptions{})
		return err
	case application.WorkloadStatefulSet:
		ss, err := clientset.AppsV1().StatefulSets(g.Namespace).Get(ctx, g.DeploymentName, metav1GetOpts())
		if err != nil {
			return err
		}
		ss.Spec.Replicas = &r
		_, err = clientset.AppsV1().StatefulSets(g.Namespace).Update(ctx, ss, metav1.UpdateOptions{})
		return err
	}
	return nil
}
