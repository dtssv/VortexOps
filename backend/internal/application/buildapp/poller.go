package buildapp

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
	"io"

	"github.com/vortexops/vortexops/internal/domain/build"
	"github.com/vortexops/vortexops/internal/domain/cluster"
	"github.com/vortexops/vortexops/internal/platform/logger"
)

// BuildPoller 后台轮询运行中的构建，从构建引擎（Jenkins/Tekton）拉取状态并推进构建状态机。
// 构建完成时：归档完整日志到 S3、写入 Image 制品记录、更新构建为终态。
type BuildPoller struct {
	repo            build.Repository
	credRepo        cluster.Repository
	jenkinsFactory  JenkinsClientFactory
	registryFactory build.RegistryAdapterFactory
	logStore        build.LogStore
	log             *logger.Logger
	interval        time.Duration
	systemSvc       SystemSettingProvider
	engineFact      *BuildEngineFactory
}

// NewBuildPoller 创建构建轮询器。
func NewBuildPoller(repo build.Repository) *BuildPoller {
	return &BuildPoller{
		repo:     repo,
		interval: 5 * time.Second,
	}
}

// Run 启动轮询循环，阻塞直到 ctx 取消。
// 每 interval 扫描所有 running 构建并推进状态。
// 至少需要 Jenkins 或 Tekton 引擎之一可用，否则直接退出。
func (p *BuildPoller) Run(ctx context.Context) {
	if p.jenkinsFactory == nil && (p.engineFact == nil || p.engineFact.Tekton == nil) {
		log.Printf("[poller] no build engine configured (jenkins/tekton both nil), poller exiting")
		return
	}
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.pollOnceSafe(ctx)
		}
	}
}

// pollOnceSafe 包裹 pollOnce 加 recover：单个构建处理 panic 不应杀死整个 poller goroutine。
func (p *BuildPoller) pollOnceSafe(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[poller] pollOnce panicked: %v", r)
		}
	}()
	p.pollOnce(ctx)
}

func (p *BuildPoller) pollOnce(ctx context.Context) {
	// 扫描所有 running 构建（跨应用）。
	// 为避免全表扫描，按状态查询；ListBuilds 支持空 ApplicationID 跨应用。
	running, _, err := p.repo.ListBuilds(ctx, build.BuildQuery{Status: build.BuildRunning, Limit: 200})
	if err != nil {
		log.Printf("[poller] list running builds: %v", err)
		return
	}
	for _, b := range running {
		p.pollBuild(ctx, b)
	}
	// 扫描 pending 构建做超时兜底（文档 4.3：每 30s 扫 pending+running 对账）。
	// 异步触发失败/进程崩溃会导致构建永久卡在 pending，此处推进或标记失败。
	pending, _, err := p.repo.ListBuilds(ctx, build.BuildQuery{Status: build.BuildPending, Limit: 200})
	if err != nil {
		return
	}
	for _, b := range pending {
		p.pollPendingBuild(ctx, b)
	}
}

// pollPendingBuild 处理卡在 pending 的构建。
// - 有 jenkins_queue_id：说明 SetJenkinsInfo 后仍在 pending（Jenkins 队列未调度），交给 pollBuild 解析队列推进 running。
// - 无 jenkins_queue_id 且超时（30min）：异步触发未完成（失败/panic/进程崩溃），标记 failed。
// - 无 jenkins_queue_id 且未超时：等待异步触发完成，跳过本轮。
func (p *BuildPoller) pollPendingBuild(ctx context.Context, b *build.Build) {
	if b.JenkinsQueueID != "" {
		// 已入队但仍在 pending：交给 pollBuild 走队列解析（pending→running）。
		p.pollBuild(ctx, b)
		return
	}
	// 无 queue_id 且超时：异步触发未完成，标记失败避免永久卡死。
	if time.Since(b.CreatedAt) > 30*time.Minute {
		_, _ = p.repo.CompleteBuild(ctx, b.ID, build.BuildFailed, 0, 0, "", "",
			"build stuck in pending: jenkins trigger did not complete within 30min", time.Now(), b.Version)
	}
}

func (p *BuildPoller) pollBuild(ctx context.Context, b *build.Build) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[poller] pollBuild build %d panicked: %v", b.ID, r)
		}
	}()
	// Tekton 模式：通过 pipeline_run_name 标识运行。
	if b.PipelineRunName != "" {
		p.pollTektonBuild(ctx, b)
		return
	}
	// Jenkins 模式。
	if b.JenkinsInstanceID == 0 {
		// 未配置 Jenkins 实例：跳过。
		return
	}
	jk, err := p.repo.GetJenkinsByID(ctx, b.JenkinsInstanceID)
	if err != nil {
		log.Printf("[poller] build %d get jenkins %d: %v", b.ID, b.JenkinsInstanceID, err)
		return
	}
	client, err := p.jenkinsFactory(ctx, jk)
	if err != nil {
		log.Printf("[poller] build %d build jenkins client: %v", b.ID, err)
		return
	}
	// 已设为 running 但 Jenkins 尚未分配构建号：通过 queue/item API 解析。
	if b.JenkinsBuildNumber == 0 {
		if b.JenkinsQueueID == "" {
			log.Printf("[poller] build %d has no jenkins queue id, cannot resolve build number", b.ID)
			return
		}
		num, ready, qerr := client.GetQueueItemBuildNumber(ctx, b.JenkinsQueueID)
		if qerr != nil {
			log.Printf("[poller] build %d get queue item %s: %v", b.ID, b.JenkinsQueueID, qerr)
			return
		}
		if !ready {
			// 仍在队列中等待调度。但 Jenkins 会在构建开始后 GC 队列项（queue/item 返回 404 → ready=false），
			// 此时永远拿不到 build number。回退：用 started_at 超过 30s 作为信号，查 job 的 lastBuild。
			if b.StartedAt != nil && time.Since(*b.StartedAt) > 30*time.Second {
				if lastNum, lerr := client.GetLastBuildNumber(ctx, b.JenkinsJobName); lerr == nil && lastNum > 0 {
					log.Printf("[poller] build %d queue item %s gone, fallback to lastBuild #%d", b.ID, b.JenkinsQueueID, lastNum)
					num = lastNum
					ready = true
				}
			}
			if !ready {
				// 仍在队列中等待调度：下次轮询继续探测。
				return
			}
		}
		if uerr := p.repo.SetJenkinsBuildNumber(ctx, b.ID, num); uerr != nil {
			log.Printf("[poller] build %d set jenkins build number %d: %v", b.ID, num, uerr)
			return
		}
		b.JenkinsBuildNumber = num
		// SetJenkinsBuildNumber 递增了 version，同步本地 Version 避免 finalize 时 CompleteBuild 因版本不匹配静默失败。
		if refreshed, rerr := p.repo.GetBuildByID(ctx, b.ID); rerr == nil {
			b.Version = refreshed.Version
		}
	}
	status, building, err := client.GetBuildStatus(ctx, b.JenkinsJobName, b.JenkinsBuildNumber)
	if err != nil {
		log.Printf("[poller] build %d jenkins status job=%s num=%d: %v", b.ID, b.JenkinsJobName, b.JenkinsBuildNumber, err)
		return
	}
	if building {
		// 仍在运行：更新进度（简化：按运行时长估算）。
		progress := estimateProgress(b.StartedAt)
		_, _ = p.repo.UpdateBuildStatus(ctx, b.ID, build.BuildRunning, progress, b.CurrentStep, b.Version)
		return
	}
	// 构建结束：归档日志 + 创建制品 + 标记终态。
	p.finalizeBuildJenkins(ctx, b, client, status)
}

// pollTektonBuild 轮询 Tekton PipelineRun 状态，并同步 TaskRun 分步信息到 vo_build_steps。
func (p *BuildPoller) pollTektonBuild(ctx context.Context, b *build.Build) {
	if p.engineFact == nil || p.engineFact.Tekton == nil {
		log.Printf("[poller] build %d tekton engine factory not configured", b.ID)
		return
	}
	client, err := p.engineFact.Tekton(ctx)
	if err != nil {
		log.Printf("[poller] build %d build tekton client: %v", b.ID, err)
		return
	}
	status, building, err := client.GetStatus(ctx, b.PipelineRunName)
	if err != nil {
		log.Printf("[poller] build %d tekton status pr=%s: %v", b.ID, b.PipelineRunName, err)
		return
	}
	// 同步分步信息（每个 TaskRun 一步）。
	p.syncTektonSteps(ctx, b, client)
	if building {
		progress := estimateProgress(b.StartedAt)
		if _, uerr := p.repo.UpdateBuildStatus(ctx, b.ID, build.BuildRunning, progress, b.CurrentStep, b.Version); uerr != nil {
			log.Printf("[poller] build %d update progress: %v", b.ID, uerr)
		}
		return
	}
	p.finalizeBuildEngine(ctx, b, client, status)
}

// syncTektonSteps 将 Tekton TaskRun 列表同步为 vo_build_steps（按 seq upsert）。
func (p *BuildPoller) syncTektonSteps(ctx context.Context, b *build.Build, client build.BuildEngineClient) {
	steps, err := client.ListSteps(ctx, b.PipelineRunName)
	if err != nil {
		return
	}
	existing, _ := p.repo.ListSteps(ctx, b.ID)
	existingByName := make(map[string]*build.BuildStep, len(existing))
	for _, s := range existing {
		existingByName[s.Name] = s
	}
	for i, st := range steps {
		if old, ok := existingByName[st.Name]; ok {
			if old.Status != st.Status {
				old.Status = st.Status
				old.StartedAt = st.StartedAt
				old.FinishedAt = st.FinishedAt
				old.Message = st.Message
				_ = p.repo.UpdateStep(ctx, old)
			}
			continue
		}
		_ = p.repo.CreateStep(ctx, &build.BuildStep{
			BuildID:   b.ID,
			Seq:       i + 1,
			Name:      st.Name,
			Status:    st.Status,
			StartedAt: st.StartedAt,
			FinishedAt: st.FinishedAt,
			Message:   st.Message,
		})
	}
}

// finalizeBuildJenkins Jenkins 构建完成归档。
func (p *BuildPoller) finalizeBuildJenkins(ctx context.Context, b *build.Build, client build.JenkinsClient, status build.BuildStatus) {
	// 刷新 version：pollBuild 路径中 SetJenkinsBuildNumber/UpdateBuildStatus 可能已递增 version，
	// 用本地 stale version 调 CompleteBuild 会因乐观锁失败（0 rows）导致构建永远卡在 running。
	if refreshed, rerr := p.repo.GetBuildByID(ctx, b.ID); rerr == nil {
		b.Version = refreshed.Version
	}
	// 1. 拉取完整日志并归档到 S3。
	var logKey, logExcerpt string
	if p.logStore != nil {
		fullLog, err := p.fetchFullLog(ctx, client, b)
		if err == nil && len(fullLog) > 0 {
			logKey = fmt.Sprintf("builds/%d/%d.log", b.ApplicationID, b.ID)
			if uerr := p.logStore.Upload(ctx, logKey, fullLog); uerr == nil {
				logExcerpt = extractExcerpt(fullLog, 500)
			}
		}
	}

	durationMs := computeDurationMs(b.StartedAt)
	failureReason := ""
	if status == build.BuildFailed {
		failureReason = "jenkins build failed"
	}

	var outputImageID int64
	if status == build.BuildSuccess {
		img, err := p.createImageRecord(ctx, b)
		if err == nil && img != nil {
			outputImageID = img.ID
		} else if err != nil {
			log.Printf("[poller] build %d createImageRecord failed: %v", b.ID, err)
		}
	}

	_, err := p.repo.CompleteBuild(ctx, b.ID, status, outputImageID, durationMs, logKey, logExcerpt, failureReason, time.Now(), b.Version)
	if err != nil {
		log.Printf("[poller] build %d finalize complete failed (version=%d): %v", b.ID, b.Version, err)
	}
}

// finalizeBuildEngine 通用引擎（Tekton）构建完成归档：拉日志、写步骤终态、创建制品、标记终态。
func (p *BuildPoller) finalizeBuildEngine(ctx context.Context, b *build.Build, client build.BuildEngineClient, status build.BuildStatus) {
	var logKey, logExcerpt string
	if p.logStore != nil {
		fullLog, err := p.fetchEngineLog(ctx, client, b)
		if err == nil && len(fullLog) > 0 {
			logKey = fmt.Sprintf("builds/%d/%d.log", b.ApplicationID, b.ID)
			if uerr := p.logStore.Upload(ctx, logKey, fullLog); uerr == nil {
				logExcerpt = extractExcerpt(fullLog, 500)
			}
		}
	}
	// 最终同步一次步骤状态。
	p.syncTektonSteps(ctx, b, client)

	durationMs := computeDurationMs(b.StartedAt)
	failureReason := ""
	if status == build.BuildFailed {
		failureReason = "tekton build failed"
	}
	var outputImageID int64
	if status == build.BuildSuccess {
		img, err := p.createImageRecord(ctx, b)
		if err == nil && img != nil {
			outputImageID = img.ID
		} else if err != nil {
			log.Printf("[poller] build %d createImageRecord failed: %v", b.ID, err)
		}
	}
	_, _ = p.repo.CompleteBuild(ctx, b.ID, status, outputImageID, durationMs, logKey, logExcerpt, failureReason, time.Now(), b.Version)
}

// fetchEngineLog 拉取通用引擎完整日志（Tekton 聚合所有 TaskRun Pod 日志）。
func (p *BuildPoller) fetchEngineLog(ctx context.Context, client build.BuildEngineClient, b *build.Build) ([]byte, error) {
	var buf strings.Builder
	var offset int64
	for {
		text, hasMore, err := client.GetLog(ctx, b.PipelineRunName, offset)
		if err != nil {
			return nil, err
		}
		if text == "" && !hasMore {
			break
		}
		buf.WriteString(text)
		offset += int64(len(text))
		if !hasMore {
			break
		}
	}
	return []byte(buf.String()), nil
}

// fetchFullLog 分页拉取 Jenkins 完整 console log。
func (p *BuildPoller) fetchFullLog(ctx context.Context, client build.JenkinsClient, b *build.Build) ([]byte, error) {
	// Jenkins consoleText 一次性返回完整日志且不标准支持 Range 增量；
	// 此前按 hasMore 循环会因每次都返回全长（hasMore 恒 true）陷入死循环，耗尽 poller goroutine。
	// 改为单次拉取完整日志：构建已结束，consoleText 即完整终态日志。
	text, _, err := client.GetConsoleLog(ctx, b.JenkinsJobName, b.JenkinsBuildNumber, 0)
	if err != nil {
		return nil, err
	}
	return []byte(text), nil
}

// createImageRecord 为成功构建创建制品版本记录。
// 若 registryFactory 可用，从镜像仓库拉取真实 digest/size，并构造完整镜像引用。
func (p *BuildPoller) createImageRecord(ctx context.Context, b *build.Build) (*build.Image, error) {
	version, err := p.repo.NextImageVersion(ctx, b.ApplicationID)
	if err != nil {
		return nil, err
	}
	// 解析默认镜像仓库实例以构造完整镜像引用 + 拉取元信息。
	reg, regErr := p.repo.GetRegistryByID(ctx, b.TargetRegistryID)
	registryHost := registryRefPlaceholder(b.TargetRegistryID)
	if regErr == nil && reg != nil {
		registryHost = registryHostFromURL(reg.URL)
	}
	fullRef := fmt.Sprintf("%s/%s:%s", registryHost, b.TargetRepository, b.TargetTag)
	img := &build.Image{
		ApplicationID: b.ApplicationID, RegistryID: b.TargetRegistryID,
		Repository: b.TargetRepository, Tag: b.TargetTag, Digest: "", FullReference: fullRef,
		VersionNumber: version, VersionLabel: b.TargetTag, Source: build.ImgSourceBuild, BuildID: b.ID,
		GitCommitSHA: b.CommitSHA, GitBranch: branchFromRef(b.RefType, b.RefValue),
		GitCommitMessage: b.CommitMessage, ScanStatus: build.ImgScanPending,
		Status: build.ImgStatusAvailable, Labels: map[string]string{},
	}
	// 拉取镜像元信息（digest/size/labels），失败不阻断制品创建。
	if p.registryFactory != nil && regErr == nil && reg != nil {
		if adapter, aerr := p.registryFactory(ctx, reg); aerr == nil {
			if meta, merr := adapter.GetImageMeta(ctx, b.TargetRepository, b.TargetTag); merr == nil {
				img.Digest = meta.Digest
				img.SizeBytes = meta.SizeBytes
				if meta.Labels != nil {
					img.Labels = meta.Labels
				}
			}
		}
	}
	if err := p.repo.CreateImage(ctx, img); err != nil {
		return nil, err
	}
	return img, nil
}

// registryHostFromURL 从 registry URL 提取 host[:port]，用于构造镜像引用。
// 例：http://harbor-core:8080 -> harbor-core:8080
func registryHostFromURL(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	rawURL = strings.TrimPrefix(rawURL, "http://")
	rawURL = strings.TrimPrefix(rawURL, "https://")
	// 去掉路径部分。
	if i := strings.Index(rawURL, "/"); i >= 0 {
		rawURL = rawURL[:i]
	}
	return rawURL
}

func registryRefPlaceholder(registryID int64) string {
	return fmt.Sprintf("registry-%d", registryID)
}

func branchFromRef(refType build.RefType, refValue string) string {
	if refType == build.RefBranch {
		return refValue
	}
	return ""
}

func computeDurationMs(startedAt *time.Time) int64 {
	if startedAt == nil {
		return 0
	}
	return time.Since(*startedAt).Milliseconds()
}

func estimateProgress(startedAt *time.Time) int {
	if startedAt == nil {
		return 10
	}
	elapsed := time.Since(*startedAt).Minutes()
	// 简化：5 分钟内线性到 90%，超过则稳定在 90%。
	progress := int(elapsed / 5.0 * 90.0)
	if progress > 90 {
		progress = 90
	}
	if progress < 10 {
		progress = 10
	}
	return progress
}

func extractExcerpt(log []byte, n int) string {
	if len(log) <= n {
		return string(log)
	}
	// 取最后 n 字节（通常包含失败原因）。
	return string(log[len(log)-n:])
}

// 确保 io import 被使用（fetchFullLog 之外的潜在用途）。
var _ io.Writer = (*strings.Builder)(nil)

// 确保 errors import 被引用。
var _ = errors.New
