// Package clusteropsapp - pod_notifier.go 提供 Pod 异常事件驱动的通知分发。
// 复用集群重启通知的核心逻辑（解析 pod→应用→active 成员 → collabapp 站内通知），
// 但入口来自 Informer 事件而非运维任务执行结果，并带冷却去重避免通知风暴。
//
// 依赖仅 application.Repository + collabapp.Service，可在 syncer 独立装配，
// 无需构造完整 clusteropsapp.Service（避免反向引入 clusterapp/k8sapp 依赖）。
package clusteropsapp

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/vortexops/vortexops/internal/application/collabapp"
	"github.com/vortexops/vortexops/internal/domain/application"
	"github.com/vortexops/vortexops/internal/domain/collab"
)

// CooldownResolver 冷却存储抽象（由 Redis CooldownStore 实现）。
// 返回 true 表示可通知（首次或冷却已过期）；false 表示在冷却窗口内应抑制。
type CooldownResolver interface {
	Acquire(ctx context.Context, clusterID int64, ns, podName, reason string) (bool, error)
	ReleaseAll(ctx context.Context, clusterID int64, ns, podName string) error
}

// PodAnomalyInput Pod 异常事件输入（由 Informer 钩子或探活评估器填充）。
type PodAnomalyInput struct {
	ClusterID   int64
	Namespace   string
	PodName     string
	NodeName    string
	Phase       string // Pod Phase（Running/Pending/Failed/...）
	Reason      string // 异常原因（HighRestartCount/ContainerNotReady/ProbeFailed/...）
	Message     string // 异常详情
	// AppResolution 应用解析结果（可选）。若调用方已解析则直接传入，避免重复查询；
	// 为空时通知器自行通过 loadGroupIndex + pod label 解析。
	AppResolution *AppResolution
}

// AppResolution 应用解析结果（pod → 所属应用 + 分组）。
type AppResolution struct {
	ApplicationID   int64
	ApplicationName string
	GroupNames      []string
}

// PodAnomalyNotifier 处理 Pod 异常事件并通知应用参与人。
// 与 NotifyAffected 区别：
//   - NotifyAffected 是同步的"广播"接口（按 scope 重新 ListPods 解析），用于运维任务结果通知；
//   - PodAnomalyNotifier 是事件驱动的"精准通知"（接收已发生的单 Pod 异常事件），
//     通过 pod label app.kubernetes.io/instance 反查 group，再查应用成员，带冷却去重。
type PodAnomalyNotifier struct {
	appRepo  application.Repository
	collab   *collabapp.Service
	cooldown CooldownResolver
	// appNameCache 应用 ID→名称缓存（loadGroupIndex 内填充，避免重复查 DB）。
	appNameCache map[int64]string
}

// NewPodAnomalyNotifier 创建 Pod 异常通知器。
// appRepo 用于 ListGroups / GetApplicationByID / ListAppMembers；
// collab 用于分发站内通知；cooldown 可为 nil（不抑制）。
func NewPodAnomalyNotifier(appRepo application.Repository, collab *collabapp.Service, cooldown CooldownResolver) *PodAnomalyNotifier {
	return &PodAnomalyNotifier{
		appRepo:      appRepo,
		collab:       collab,
		cooldown:     cooldown,
		appNameCache: make(map[int64]string),
	}
}

// Notify 处理一条 Pod 异常事件。
// 流程：冷却判断 → 解析 pod 所属应用（若未提供 AppResolution）→ 查询 active 成员 → 分发站内通知。
// 同一 Pod 同一 Reason 在冷却窗口内只通知一次。
func (n *PodAnomalyNotifier) Notify(ctx context.Context, in PodAnomalyInput) error {
	if n == nil || n.appRepo == nil {
		return nil
	}
	// 冷却判断：避免 Informer 高频事件触发通知风暴。
	if n.cooldown != nil {
		ok, err := n.cooldown.Acquire(ctx, in.ClusterID, in.Namespace, in.PodName, in.Reason)
		if err != nil {
			log.Printf("[pod_notifier] cooldown acquire failed: cluster=%d pod=%s/%s reason=%s err=%v",
				in.ClusterID, in.Namespace, in.PodName, in.Reason, err)
		}
		if !ok {
			// 冷却中，抑制本次通知。
			return nil
		}
	}

	// 解析 pod 所属应用（若调用方未提供）。
	resolution := in.AppResolution
	if resolution == nil {
		res, err := n.resolvePodApp(ctx, in)
		if err != nil {
			return err
		}
		resolution = res
	}
	if resolution == nil || resolution.ApplicationID == 0 {
		// Pod 不属于任何已知应用（如系统 pod 或无 label），跳过通知。
		return nil
	}

	// 查询应用 active 成员。
	members, _, err := n.appRepo.ListAppMembers(ctx, resolution.ApplicationID, 0, 1000)
	if err != nil {
		return fmt.Errorf("list app members: %w", err)
	}

	subject := fmt.Sprintf("【应用异常】%s - Pod %s/%s",
		resolution.ApplicationName, in.Namespace, in.PodName)
	body := strings.Join([]string{
		fmt.Sprintf("应用: %s", resolution.ApplicationName),
		fmt.Sprintf("分组: %s", strings.Join(resolution.GroupNames, ", ")),
		fmt.Sprintf("集群ID: %d", in.ClusterID),
		fmt.Sprintf("命名空间: %s", in.Namespace),
		fmt.Sprintf("Pod: %s", in.PodName),
		fmt.Sprintf("节点: %s", in.NodeName),
		fmt.Sprintf("Pod状态: %s", in.Phase),
		fmt.Sprintf("异常原因: %s", in.Reason),
		fmt.Sprintf("详情: %s", in.Message),
		fmt.Sprintf("时间: %s", time.Now().Format(time.RFC3339)),
	}, "\n")

	payload := map[string]any{
		"app_id":         resolution.ApplicationID,
		"app_name":       resolution.ApplicationName,
		"group_names":    resolution.GroupNames,
		"scope":          "pod",
		"cluster_id":     in.ClusterID,
		"pod_namespace":  in.Namespace,
		"pod_name":       in.PodName,
		"node_name":      in.NodeName,
		"phase":          in.Phase,
		"reason":         in.Reason,
		"message":        in.Message,
		"triggered_by":   int64(0), // 系统触发，无 actor
		"event_type":     "pod_anomaly",
	}

	notified := 0
	for _, m := range members {
		if m.Status != application.MemberStatusActive {
			continue
		}
		_, nErr := n.collab.CreateNotification(ctx, collabapp.CreateNotificationInput{
			UserID:  m.UserID,
			Channel: collab.ChannelInApp,
			Subject: subject,
			Body:    body,
			Payload: payload,
		})
		if nErr == nil {
			notified++
		}
	}
	log.Printf("[pod_notifier] notified %d members for pod %s/%s reason=%s",
		notified, in.Namespace, in.PodName, in.Reason)
	return nil
}

// OnPodRecovered Pod 恢复正常时清除其所有冷却，允许下次异常立即通知。
func (n *PodAnomalyNotifier) OnPodRecovered(ctx context.Context, clusterID int64, ns, podName string) {
	if n == nil || n.cooldown == nil {
		return
	}
	if err := n.cooldown.ReleaseAll(ctx, clusterID, ns, podName); err != nil {
		log.Printf("[pod_notifier] release cooldown failed: cluster=%d pod=%s/%s err=%v",
			clusterID, ns, podName, err)
	}
}

// loadGroupIndex 加载该集群下所有 group，按 "namespace:groupName" 索引。
// 镜像 Service.loadGroupIndex 的逻辑（避免依赖完整 Service）。
func (n *PodAnomalyNotifier) loadGroupIndex(ctx context.Context, clusterID int64) (map[string]groupInfo, error) {
	groups, _, err := n.appRepo.ListGroups(ctx, application.GroupQuery{ClusterID: clusterID, Limit: 10000})
	if err != nil {
		return nil, err
	}
	index := make(map[string]groupInfo, len(groups))
	for _, g := range groups {
		appName, ok := n.appNameCache[g.ApplicationID]
		if !ok {
			if app, gerr := n.appRepo.GetApplicationByID(ctx, g.ApplicationID); gerr == nil {
				appName = app.Name
			}
			n.appNameCache[g.ApplicationID] = appName
		}
		index[g.Namespace+":"+g.Name] = groupInfo{
			applicationID:   g.ApplicationID,
			applicationName: appName,
			groupName:       g.Name,
		}
	}
	return index, nil
}

// resolvePodApp 通过 pod label app.kubernetes.io/instance + namespace:groupName 索引反查应用。
// pod 异常事件若未携带 AppResolution，则按 pod 名前缀匹配 group name（兜底）。
func (n *PodAnomalyNotifier) resolvePodApp(ctx context.Context, in PodAnomalyInput) (*AppResolution, error) {
	groupIndex, err := n.loadGroupIndex(ctx, in.ClusterID)
	if err != nil {
		return nil, err
	}
	// 兜底匹配：pod 名形如 "{groupName}-{hash}"，尝试按 namespace:groupName 精确匹配 group name。
	// Informer 钩子侧应优先传入 pod label 解析结果（AppResolution），此处仅作最终兜底。
	for nsKey, gi := range groupIndex {
		if !strings.HasPrefix(nsKey, in.Namespace+":") {
			continue
		}
		if strings.HasPrefix(in.PodName, gi.groupName+"-") || in.PodName == gi.groupName {
			return &AppResolution{
				ApplicationID:   gi.applicationID,
				ApplicationName: gi.applicationName,
				GroupNames:      []string{gi.groupName},
			}, nil
		}
	}
	return nil, nil
}

// ResolveByPodLabel 通过 pod label 直接解析所属应用（推荐路径，由 informer 钩子调用）。
// labelKey 通常是 "app.kubernetes.io/instance"，其值为 group name。
func (n *PodAnomalyNotifier) ResolveByPodLabel(ctx context.Context, clusterID int64, namespace, labelValue string) *AppResolution {
	if labelValue == "" {
		return nil
	}
	groupIndex, err := n.loadGroupIndex(ctx, clusterID)
	if err != nil {
		return nil
	}
	gi, ok := groupIndex[namespace+":"+labelValue]
	if !ok {
		return nil
	}
	return &AppResolution{
		ApplicationID:   gi.applicationID,
		ApplicationName: gi.applicationName,
		GroupNames:      []string{gi.groupName},
	}
}

// SortGroupNames 返回排序后的分组名（便于稳定输出）。
func SortGroupNames(names []string) []string {
	out := make([]string, len(names))
	copy(out, names)
	sort.Strings(out)
	return out
}
