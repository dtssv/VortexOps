package clusterapp

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/vortexops/vortexops/internal/domain/application"
	"github.com/vortexops/vortexops/internal/infrastructure/k8s"
	k8sexec "github.com/vortexops/vortexops/internal/infrastructure/k8s/exec"
)

const (
	// 列表主动拨测：单次短超时，避免 Pod 列表 5s 轮询被拖慢。
	appProbeTimeout       = 2 * time.Second
	appProbeMaxConcurrent = 8
)

// ProbePodsAppReady 按应用探活配置对 Pod 列表做主动拨测，写入 AppReady / AppReadyDetail。
// probe 为 nil 或未启用时不修改（前端按「容器起来即就绪」展示）。
func (s *Service) ProbePodsAppReady(ctx context.Context, clusterID int64, pods []PodInfo, probe *application.ProbeConfig) []PodInfo {
	if probe == nil || !probe.Enabled || len(pods) == 0 {
		return pods
	}

	entry, err := s.getClientEntry(ctx, clusterID)
	if err != nil {
		markAllNotReady(pods, "无法连接集群: "+err.Error())
		return pods
	}

	sessions := k8sexec.NewSessionManager()
	sem := make(chan struct{}, appProbeMaxConcurrent)
	var wg sync.WaitGroup

	for i := range pods {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			ready, detail := probeOnePod(ctx, sessions, entry, clusterID, &pods[idx], probe)
			// 拷贝到堆，避免多个 Pod 指针别名同一局部变量。
			readyCopy := ready
			pods[idx].AppReady = &readyCopy
			pods[idx].AppReadyDetail = detail
		}(i)
	}
	wg.Wait()
	return pods
}

func markAllNotReady(pods []PodInfo, detail string) {
	for i := range pods {
		falseVal := false
		pods[i].AppReady = &falseVal
		pods[i].AppReadyDetail = detail
	}
}

func probeOnePod(
	ctx context.Context,
	sessions *k8sexec.SessionManager,
	entry *k8s.ClientEntry,
	clusterID int64,
	pod *PodInfo,
	probe *application.ProbeConfig,
) (bool, string) {
	if pod.Phase != "Running" {
		return false, "Pod 未运行 (" + pod.Phase + ")"
	}
	container := ""
	if len(pod.Containers) > 0 {
		container = pod.Containers[0].Name
	}

	probeCtx, cancel := context.WithTimeout(ctx, appProbeTimeout)
	defer cancel()

	run := func(cmd []string) error {
		return sessions.Exec(probeCtx, entry.RestConfig, entry.Clientset, k8sexec.ExecOptions{
			ClusterID: clusterID,
			Namespace: pod.Namespace,
			Pod:       pod.Name,
			Container: container,
			Command:   cmd,
			Stdout:    &bytes.Buffer{},
			Stderr:    &bytes.Buffer{},
		})
	}

	switch probe.Method {
	case application.ProbeMethodTCP:
		if err := run(tcpProbeCmd(probe.Port)); err != nil {
			return false, fmt.Sprintf("端口 %d 未就绪", probe.Port)
		}
		return true, ""
	case application.ProbeMethodProcess:
		if err := run(processProbeCmd(probe.ProcessKeyword)); err != nil {
			return false, fmt.Sprintf("未找到进程 %q", probe.ProcessKeyword)
		}
		return true, ""
	case application.ProbeMethodBoth:
		if err := run(tcpProbeCmd(probe.Port)); err != nil {
			return false, fmt.Sprintf("端口 %d 未就绪", probe.Port)
		}
		if err := run(processProbeCmd(probe.ProcessKeyword)); err != nil {
			return false, fmt.Sprintf("未找到进程 %q", probe.ProcessKeyword)
		}
		return true, ""
	default:
		return false, "未知探活方式"
	}
}

func tcpProbeCmd(port int) []string {
	// 优先 /dev/tcp（bash）；失败时回退到 nc -z。
	return []string{"sh", "-c", fmt.Sprintf(
		`(exec 3<>/dev/tcp/127.0.0.1/%d) 2>/dev/null || nc -z -w 1 127.0.0.1 %d`,
		port, port,
	)}
}

func processProbeCmd(keyword string) []string {
	return []string{"sh", "-c", fmt.Sprintf("pgrep -f %q >/dev/null 2>&1", keyword)}
}
