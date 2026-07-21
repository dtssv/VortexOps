// Package main - pod_anomaly.go 装配 Pod 异常检测与通知链路。
// 把基础设施（informer hook）与 application 层（pod notifier）桥接起来。
//
// 组件：
//   - podAnomalyAdapter 实现 k8s.PodAnomalyHook（informer pod/event 事件），委托给
//     clusteropsapp.PodAnomalyNotifier 分发站内通知。
//
// 探活改为原生 K8s Probe（由 K8s 自身执行），syncer 不再主动拨测；异常经 Pod/Event
// informer 事件驱动推送，通知器侧 Redis 冷却去重避免风暴。
package main

import (
	"context"
	"log"

	"github.com/vortexops/vortexops/internal/application/clusteropsapp"
	"github.com/vortexops/vortexops/internal/application/collabapp"
	"github.com/vortexops/vortexops/internal/domain/application"
	"github.com/vortexops/vortexops/internal/infrastructure/k8s"
)

// podAnomalyAdapter 把 clusteropsapp.PodAnomalyNotifier 适配为 informer 钩子。
type podAnomalyAdapter struct {
	notifier *clusteropsapp.PodAnomalyNotifier
}

// OnPodAnomaly 实现 k8s.PodAnomalyHook：informer 检测到 pod 异常/恢复或 K8s 异常事件时回调。
// 恢复事件（Recovered=true）仅清除冷却；异常事件调用 Notify 分发站内通知。
func (a *podAnomalyAdapter) OnPodAnomaly(ctx context.Context, e k8s.PodAnomalyEvent) {
	if a == nil || a.notifier == nil {
		return
	}
	if e.Recovered {
		// pod 恢复正常：清除该 pod 所有原因的冷却，允许下次异常立即通知。
		a.notifier.OnPodRecovered(ctx, e.ClusterID, e.Namespace, e.PodName)
		return
	}
	// 异常事件：优先用 pod label 反查所属应用；查不到则交由 notifier 兜底按 pod 名匹配。
	resolution := a.notifier.ResolveByPodLabel(ctx, e.ClusterID, e.Namespace, e.PodLabelInstance)
	in := clusteropsapp.PodAnomalyInput{
		ClusterID: e.ClusterID,
		Namespace: e.Namespace,
		PodName:   e.PodName,
		NodeName:  e.NodeName,
		Phase:     e.Phase,
		Reason:    e.Reason,
		Message:   e.Message,
	}
	if resolution != nil {
		in.AppResolution = resolution
	}
	if err := a.notifier.Notify(ctx, in); err != nil {
		log.Printf("[syncer] pod anomaly notify failed: pod=%s/%s reason=%s err=%v",
			e.Namespace, e.PodName, e.Reason, err)
	}
}

// buildPodAnomalyAdapter 构造通知链路适配器（informer hook）。
// 返回的 adapter 满足 k8s.PodAnomalyHook，注入 InformerManager 后由 onPod/onEvent 触发。
func buildPodAnomalyAdapter(appRepo application.Repository, collabSvc *collabapp.Service, cooldown clusteropsapp.CooldownResolver) *podAnomalyAdapter {
	notifier := clusteropsapp.NewPodAnomalyNotifier(appRepo, collabSvc, cooldown)
	return &podAnomalyAdapter{notifier: notifier}
}
