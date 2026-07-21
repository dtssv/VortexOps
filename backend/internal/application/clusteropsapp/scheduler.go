// clusteropsapp 调度器：周期扫描到期运维任务并执行。
// 遵循 Pattern A（in-process time.NewTicker goroutine），与 buildapp/poller.go、
// monitoringapp.StartAlertEvaluator 保持一致；每次迭代 recover 防止单次 panic 拖垮 goroutine。
package clusteropsapp

import (
	"context"
	"log"
	"time"

	"github.com/vortexops/vortexops/internal/domain/clusterops"
)

// Scheduler 运维任务调度器。
type Scheduler struct {
	svc     *Service
	repo    clusterops.Repository
	now     func() time.Time
	tickDur time.Duration
}

// NewScheduler 创建调度器。tickDur <= 0 时不启动（便于测试关闭）。
func NewScheduler(svc *Service, repo clusterops.Repository, tickDur time.Duration) *Scheduler {
	return &Scheduler{svc: svc, repo: repo, now: time.Now, tickDur: tickDur}
}

// Run 启动调度循环，阻塞至 ctx 取消。每 tickDur 周期扫描一次到期 pending 任务。
func (s *Scheduler) Run(ctx context.Context) {
	if s.tickDur <= 0 {
		log.Printf("[clusterops-scheduler] tick duration <= 0, scheduler exiting")
		return
	}
	log.Printf("[clusterops-scheduler] starting with tick=%s", s.tickDur)
	ticker := time.NewTicker(s.tickDur)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Printf("[clusterops-scheduler] context cancelled, exiting")
			return
		case <-ticker.C:
			s.runOnceSafe(ctx)
		}
	}
}

// runOnceSafe 单次扫描，panic 不外泄。
func (s *Scheduler) runOnceSafe(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[clusterops-scheduler] panic in runOnce: %v", r)
		}
	}()
	ops, err := s.repo.ListDueOperations(ctx, s.now(), 50)
	if err != nil {
		log.Printf("[clusterops-scheduler] list due operations failed: %v", err)
		return
	}
	if len(ops) == 0 {
		return
	}
	log.Printf("[clusterops-scheduler] processing %d due operation(s)", len(ops))
	for _, op := range ops {
		// 每个任务独立 recover，避免一个失败影响其他。
		func(o *clusterops.Operation) {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[clusterops-scheduler] panic executing op %d: %v", o.ID, r)
				}
			}()
			if err := s.svc.ExecuteOperation(ctx, o); err != nil {
				log.Printf("[clusterops-scheduler] execute op %d failed: %v", o.ID, err)
			}
		}(op)
	}
}
