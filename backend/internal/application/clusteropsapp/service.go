// Package clusteropsapp 是集群运维领域的应用服务层。
// 编排运维任务、节点状态同步、Pod/Node→受影响应用→成员解析与通知分发。
package clusteropsapp

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/vortexops/vortexops/internal/application/clusterapp"
	"github.com/vortexops/vortexops/internal/application/collabapp"
	"github.com/vortexops/vortexops/internal/application/k8sapp"
	"github.com/vortexops/vortexops/internal/domain/application"
	"github.com/vortexops/vortexops/internal/domain/clusterops"
	"github.com/vortexops/vortexops/internal/domain/collab"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// 异常 Pod 判定阈值：重启次数超过此值视为异常。
const abnormalRestartThreshold = 5

// Service 集群运维应用服务。
type Service struct {
	repo       clusterops.Repository
	metricsRepo clusterops.MetricsRepository
	clusters   *clusterapp.Service
	k8s        *k8sapp.Service
	collab     *collabapp.Service
	appRepo    application.Repository
}

// New 创建集群运维服务。
func New(
	repo clusterops.Repository,
	clusters *clusterapp.Service,
	k8s *k8sapp.Service,
	collab *collabapp.Service,
	appRepo application.Repository,
) *Service {
	return &Service{repo: repo, clusters: clusters, k8s: k8s, collab: collab, appRepo: appRepo}
}

// SetMetricsRepository 注入指标采样仓储。
// 单独 setter 是因为具体仓储 *clusteropsrepo.Repository 同时实现 Repository 与 MetricsRepository，
// 但接口分立；server.go 在 New 后调用此方法注入同一实例。
func (s *Service) SetMetricsRepo(r clusterops.MetricsRepository) {
	s.metricsRepo = r
}

// ============================================================================
// 运维任务 CRUD
// ============================================================================

// CreateOperationInput 创建运维任务输入。
type CreateOperationInput struct {
	ClusterID      int64
	NodeName       string
	OperationType  clusterops.OperationType
	ScheduledAt    time.Time
	NotifyAffected bool
	ActorID        int64
}

// CreateOperation 创建计划运维任务。若 scheduled_at <= now 则立即执行。
func (s *Service) CreateOperation(ctx context.Context, in CreateOperationInput) (*clusterops.Operation, error) {
	if in.ClusterID == 0 {
		return nil, apperr.Validation("cluster_id is required", nil)
	}
	if in.OperationType == "" {
		return nil, apperr.Validation("operation_type is required", nil)
	}
	if in.ScheduledAt.IsZero() {
		in.ScheduledAt = time.Now()
	}
	op := &clusterops.Operation{
		ClusterID:      in.ClusterID,
		NodeName:       in.NodeName,
		OperationType:  in.OperationType,
		ScheduledAt:    in.ScheduledAt,
		Status:         clusterops.StatusPending,
		NotifyAffected: in.NotifyAffected,
	}
	op.CreatedBy = in.ActorID
	op.UpdatedBy = in.ActorID
	if err := s.repo.CreateOperation(ctx, op); err != nil {
		return nil, apperr.Internal("create cluster operation", err)
	}
	// 立即执行（调度器也会兜底扫描）。
	if !in.ScheduledAt.After(time.Now()) {
		go func(o *clusterops.Operation) {
			_ = s.ExecuteOperation(context.Background(), o)
		}(op)
	}
	return op, nil
}

// ListOperations 分页查询运维任务。
func (s *Service) ListOperations(ctx context.Context, clusterID int64, status clusterops.OperationStatus, page, size int) ([]*clusterops.Operation, int64, error) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	q := clusterops.OperationQuery{
		ClusterID: clusterID, Status: status,
		Offset: (page - 1) * size, Limit: size,
	}
	items, total, err := s.repo.ListOperations(ctx, q)
	if err != nil {
		return nil, 0, apperr.Internal("list cluster operations", err)
	}
	return items, total, nil
}

// CancelOperation 取消运维任务（仅 pending 可取消）。
func (s *Service) CancelOperation(ctx context.Context, id, actorID int64) error {
	op, err := s.repo.GetOperation(ctx, id)
	if err != nil {
		if errors.Is(err, clusterops.ErrOperationNotFound) {
			return apperr.NotFound("cluster_operation", strconv.FormatInt(id, 10))
		}
		return apperr.Internal("get cluster operation", err)
	}
	if op.Status != clusterops.StatusPending {
		return apperr.BusinessRule(fmt.Sprintf("operation in status %s cannot be cancelled", op.Status), nil)
	}
	op.Status = clusterops.StatusCancelled
	op.UpdatedBy = actorID
	if err := s.repo.UpdateOperation(ctx, op); err != nil {
		return apperr.Internal("cancel cluster operation", err)
	}
	return nil
}

// ============================================================================
// 节点状态同步与查询
// ============================================================================

// SyncNodeStatuses 强制同步集群节点状态到缓存表。返回同步后的节点列表。
func (s *Service) SyncNodeStatuses(ctx context.Context, clusterID int64) ([]*clusterops.NodeStatus, error) {
	nodes, err := s.k8s.ListNodes(ctx, clusterID)
	if err != nil {
		return nil, apperr.Internal("list k8s nodes", err)
	}
	// 一次性拉取全集群 Pod，按 nodeName 分组以避免每节点一次 List。
	allPods, err := s.k8s.ListPods(ctx, clusterID, "", "")
	if err != nil {
		return nil, apperr.Internal("list k8s pods", err)
	}
	podsByNode := make(map[string][]corev1.Pod)
	for i := range allPods {
		n := allPods[i].Spec.NodeName
		if n == "" {
			continue
		}
		podsByNode[n] = append(podsByNode[n], allPods[i])
	}

	keep := make(map[string]struct{}, len(nodes))
	for _, node := range nodes {
		keep[node.Name] = struct{}{}
		ns := buildNodeStatus(clusterID, &node, podsByNode[node.Name])
		if _, err := s.repo.UpsertNodeStatus(ctx, ns); err != nil {
			return nil, apperr.Internal("upsert node status", err)
		}
	}
	// 清理已下线节点。
	if err := s.repo.DeleteNodeStatuses(ctx, clusterID, keep); err != nil {
		return nil, apperr.Internal("delete stale node statuses", err)
	}
	return s.repo.ListNodeStatuses(ctx, clusterID)
}

// ListNodeStatuses 读缓存表返回节点状态。
func (s *Service) ListNodeStatuses(ctx context.Context, clusterID int64) ([]*clusterops.NodeStatus, error) {
	items, err := s.repo.ListNodeStatuses(ctx, clusterID)
	if err != nil {
		return nil, apperr.Internal("list node statuses", err)
	}
	return items, nil
}

func buildNodeStatus(clusterID int64, node *corev1.Node, pods []corev1.Pod) clusterops.UpsertNodeStatusInput {
	in := clusterops.UpsertNodeStatusInput{
		ClusterID:             clusterID,
		NodeName:              node.Name,
		Unschedulable:         node.Spec.Unschedulable,
		KubeletVersion:        node.Status.NodeInfo.KubeletVersion,
		AllocatableCPUM:       cpuQuantityToMilli(node.Status.Allocatable.Cpu()),
		AllocatableMemoryBytes: node.Status.Allocatable.Memory().Value(),
		AllocatableGPU:        gpuCount(node.Status.Allocatable),
		UsedCPUM:              0,
		UsedMemoryBytes:       0,
		PodCount:              len(pods),
		AbnormalPodCount:      countAbnormalPods(pods),
	}
	// 聚合已调度 Pod 的资源 requests 作为「已用」估算。
	for i := range pods {
		for j := range pods[i].Spec.Containers {
			c := pods[i].Spec.Containers[j]
			in.UsedCPUM += cpuQuantityToMilli(c.Resources.Requests.Cpu())
			in.UsedMemoryBytes += c.Resources.Requests.Memory().Value()
		}
	}
	in.Status = nodeHealth(node)
	in.Roles = nodeRoles(node)
	in.Taints = nodeTaints(node)
	in.Addresses = nodeAddresses(node)
	return in
}

func nodeHealth(node *corev1.Node) clusterops.NodeHealth {
	for _, c := range node.Status.Conditions {
		if c.Type == corev1.NodeReady {
			if c.Status == corev1.ConditionTrue {
				return clusterops.NodeReady
			}
			return clusterops.NodeNotReady
		}
	}
	return clusterops.NodeUnknown
}

func nodeRoles(node *corev1.Node) []string {
	var roles []string
	for k := range node.Labels {
		if strings.HasPrefix(k, "node-role.kubernetes.io/") {
			role := strings.TrimPrefix(k, "node-role.kubernetes.io/")
			roles = append(roles, role)
		}
	}
	if len(roles) == 0 {
		roles = []string{"worker"}
	}
	sort.Strings(roles)
	return roles
}

func nodeTaints(node *corev1.Node) []map[string]any {
	out := make([]map[string]any, 0, len(node.Spec.Taints))
	for _, t := range node.Spec.Taints {
		out = append(out, map[string]any{
			"key":    string(t.Key),
			"value":  string(t.Value),
			"effect": string(t.Effect),
		})
	}
	return out
}

func nodeAddresses(node *corev1.Node) []map[string]any {
	out := make([]map[string]any, 0, len(node.Status.Addresses))
	for _, a := range node.Status.Addresses {
		out = append(out, map[string]any{
			"type":    string(a.Type),
			"address": a.Address,
		})
	}
	return out
}

// cpuQuantityToMilli 把 corev1 资源 quantity（如 "2" 或 "500m"）转成 millicores 整数。
func cpuQuantityToMilli(q *resource.Quantity) int {
	if q == nil {
		return 0
	}
	return int(q.MilliValue())
}

func gpuCount(allocatable corev1.ResourceList) int {
	for name, qty := range allocatable {
		if strings.Contains(string(name), "nvidia") || strings.Contains(string(name), "gpu") {
			return int(qty.Value())
		}
	}
	return 0
}

func countAbnormalPods(pods []corev1.Pod) int {
	n := 0
	for i := range pods {
		p := &pods[i]
		// 排除 DaemonSet/static pod（这些通常不视为应用异常）。
		if isDaemonSetOrStatic(p) {
			continue
		}
		if p.Status.Phase != corev1.PodRunning && p.Status.Phase != corev1.PodSucceeded {
			n++
			continue
		}
		for _, cs := range p.Status.ContainerStatuses {
			if int(cs.RestartCount) >= abnormalRestartThreshold {
				n++
				break
			}
		}
	}
	return n
}

func isDaemonSetOrStatic(p *corev1.Pod) bool {
	for _, owner := range p.OwnerReferences {
		if owner.Kind == "DaemonSet" {
			return true
		}
		if owner.Kind == "Node" && owner.Controller != nil && *owner.Controller {
			return true
		}
	}
	return false
}

// ============================================================================
// 异常资源查询
// ============================================================================

// AbnormalPod 异常 Pod（含受影响应用信息）。
type AbnormalPod struct {
	Name             string `json:"name"`
	Namespace        string `json:"namespace"`
	NodeName         string `json:"node_name"`
	Phase            string `json:"phase"`
	RestartCount     int    `json:"restart_count"`
	Ready            bool   `json:"ready"`
	Reason           string `json:"reason,omitempty"`
	Message          string `json:"message,omitempty"`
	ApplicationID    int64  `json:"application_id,omitempty"`
	ApplicationName  string `json:"application_name,omitempty"`
	GroupName        string `json:"group_name,omitempty"`
}

// ListAbnormalPods 列出集群下异常 Pod，并尝试解析所属应用。
func (s *Service) ListAbnormalPods(ctx context.Context, clusterID int64) ([]AbnormalPod, error) {
	pods, err := s.k8s.ListPods(ctx, clusterID, "", "")
	if err != nil {
		return nil, apperr.Internal("list k8s pods", err)
	}
	// 预加载该集群所有 group，按 "namespace:groupName" 建索引以便批量解析。
	groupIndex, err := s.loadGroupIndex(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	out := make([]AbnormalPod, 0)
	for i := range pods {
		p := &pods[i]
		if isDaemonSetOrStatic(p) {
			continue
		}
		abnormal, reason, msg := classifyPod(p)
		if !abnormal {
			continue
		}
		ap := AbnormalPod{
			Name:         p.Name,
			Namespace:    p.Namespace,
			NodeName:     p.Spec.NodeName,
			Phase:        string(p.Status.Phase),
			RestartCount: totalRestarts(p),
			Ready:        podReady(p),
			Reason:       reason,
			Message:      msg,
		}
		if g := p.Labels["app.kubernetes.io/instance"]; g != "" {
			if gi, ok := groupIndex[p.Namespace+":"+g]; ok {
				ap.ApplicationID = gi.applicationID
				ap.ApplicationName = gi.applicationName
				ap.GroupName = gi.groupName
			}
		}
		out = append(out, ap)
	}
	return out, nil
}

// AbnormalNode 异常节点。
type AbnormalNode struct {
	NodeName         string           `json:"node_name"`
	Status           clusterops.NodeHealth `json:"status"`
	Unschedulable    bool             `json:"unschedulable"`
	AbnormalPodCount int              `json:"abnormal_pod_count"`
	PodCount         int              `json:"pod_count"`
	Addresses        []map[string]any `json:"addresses"`
	Taints           []map[string]any `json:"taints"`
	LastSyncedAt     *time.Time       `json:"last_synced_at,omitempty"`
}

// ListAbnormalNodes 列出异常节点（状态非 ready 或 unschedulable 或有异常 Pod）。
func (s *Service) ListAbnormalNodes(ctx context.Context, clusterID int64) ([]AbnormalNode, error) {
	statuses, err := s.repo.ListNodeStatuses(ctx, clusterID)
	if err != nil {
		return nil, apperr.Internal("list node statuses", err)
	}
	out := make([]AbnormalNode, 0)
	for _, ns := range statuses {
		if ns.Status == clusterops.NodeReady && !ns.Unschedulable && ns.AbnormalPodCount == 0 {
			continue
		}
		out = append(out, AbnormalNode{
			NodeName:         ns.NodeName,
			Status:           ns.Status,
			Unschedulable:    ns.Unschedulable,
			AbnormalPodCount: ns.AbnormalPodCount,
			PodCount:         ns.PodCount,
			Addresses:        ns.Addresses,
			Taints:           ns.Taints,
			LastSyncedAt:     ns.LastSyncedAt,
		})
	}
	return out, nil
}

func classifyPod(p *corev1.Pod) (abnormal bool, reason, msg string) {
	if p.Status.Phase != corev1.PodRunning && p.Status.Phase != corev1.PodSucceeded {
		// 取最近一个非正常 condition 作为原因。
		for _, c := range p.Status.Conditions {
			if c.Status != corev1.ConditionTrue && c.Reason != "" {
				return true, c.Reason, c.Message
			}
		}
		return true, string(p.Status.Phase), ""
	}
	for _, cs := range p.Status.ContainerStatuses {
		if int(cs.RestartCount) >= abnormalRestartThreshold {
			r := ""
			if cs.LastTerminationState.Terminated != nil {
				r = cs.LastTerminationState.Terminated.Reason
			}
			return true, "HighRestartCount", r
		}
		if !cs.Ready {
			r := ""
			if cs.State.Waiting != nil {
				r = cs.State.Waiting.Reason
			}
			return true, "ContainerNotReady", r
		}
	}
	return false, "", ""
}

func totalRestarts(p *corev1.Pod) int {
	n := 0
	for _, cs := range p.Status.ContainerStatuses {
		n += int(cs.RestartCount)
	}
	return n
}

func podReady(p *corev1.Pod) bool {
	if p.Status.Phase != corev1.PodRunning {
		return false
	}
	if len(p.Status.ContainerStatuses) == 0 {
		return false
	}
	for _, cs := range p.Status.ContainerStatuses {
		if !cs.Ready {
			return false
		}
	}
	return true
}

// ============================================================================
// Pod/Node → 受影响应用 → 成员解析与通知分发
// ============================================================================

type groupInfo struct {
	applicationID   int64
	applicationName string
	groupName       string
}

// loadGroupIndex 加载该集群下所有 group，按 "namespace:groupName" 索引。
func (s *Service) loadGroupIndex(ctx context.Context, clusterID int64) (map[string]groupInfo, error) {
	groups, _, err := s.appRepo.ListGroups(ctx, application.GroupQuery{ClusterID: clusterID, Limit: 10000})
	if err != nil {
		return nil, apperr.Internal("list groups by cluster", err)
	}
	// 预加载 application name：批量取 applicationID 集合后逐个查（简化实现）。
	appCache := make(map[int64]string)
	index := make(map[string]groupInfo, len(groups))
	for _, g := range groups {
		appName, ok := appCache[g.ApplicationID]
		if !ok {
			if app, err := s.appRepo.GetApplicationByID(ctx, g.ApplicationID); err == nil {
				appName = app.Name
			}
			appCache[g.ApplicationID] = appName
		}
		index[g.Namespace+":"+g.Name] = groupInfo{
			applicationID:   g.ApplicationID,
			applicationName: appName,
			groupName:       g.Name,
		}
	}
	return index, nil
}

// ResolveAffectedAppsByNode 解析某节点上所有 Pod 所属的应用。
func (s *Service) ResolveAffectedAppsByNode(ctx context.Context, clusterID int64, nodeName string) ([]AffectedApp, error) {
	pods, err := s.k8s.ListPods(ctx, clusterID, "", "spec.nodeName="+nodeName)
	if err != nil {
		return nil, apperr.Internal("list pods by node", err)
	}
	groupIndex, err := s.loadGroupIndex(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	return resolveAppsFromPods(pods, groupIndex), nil
}

// ResolveAffectedAppsByPod 解析单个 Pod 所属应用。
func (s *Service) ResolveAffectedAppsByPod(ctx context.Context, clusterID int64, namespace, podName string) ([]AffectedApp, error) {
	pods, err := s.k8s.ListPods(ctx, clusterID, namespace, "metadata.name="+podName)
	if err != nil {
		return nil, apperr.Internal("list pod", err)
	}
	groupIndex, err := s.loadGroupIndex(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	return resolveAppsFromPods(pods, groupIndex), nil
}

func resolveAppsFromPods(pods []corev1.Pod, index map[string]groupInfo) []AffectedApp {
	seen := make(map[int64]AffectedApp)
	for i := range pods {
		p := &pods[i]
		if isDaemonSetOrStatic(p) {
			continue
		}
		g := p.Labels["app.kubernetes.io/instance"]
		if g == "" {
			continue
		}
		gi, ok := index[p.Namespace+":"+g]
		if !ok {
			continue
		}
		if existing, found := seen[gi.applicationID]; found {
			existing.GroupNames = appendUnique(existing.GroupNames, gi.groupName)
			seen[gi.applicationID] = existing
		} else {
			seen[gi.applicationID] = AffectedApp{
				ApplicationID:   gi.applicationID,
				ApplicationName: gi.applicationName,
				GroupNames:      []string{gi.groupName},
			}
		}
	}
	out := make([]AffectedApp, 0, len(seen))
	for _, v := range seen {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ApplicationID < out[j].ApplicationID })
	return out
}

func appendUnique(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}

// AffectedApp 受影响应用（含其成员列表，供前端预览）。
type AffectedApp struct {
	ApplicationID   int64     `json:"application_id"`
	ApplicationName string    `json:"application_name"`
	GroupNames      []string  `json:"group_names"`
	Members         []Member  `json:"members"`
}

// Member 通知接收人。
type Member struct {
	UserID      int64  `json:"user_id"`
	UserName    string `json:"user_name"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	RoleName    string `json:"role_name"`
}

// PreviewAffected 预览受影响应用与成员（发送前确认用）。
func (s *Service) PreviewAffected(ctx context.Context, clusterID int64, scope, nodeName, podNamespace, podName string) ([]AffectedApp, error) {
	var apps []AffectedApp
	var err error
	switch scope {
	case "node":
		apps, err = s.ResolveAffectedAppsByNode(ctx, clusterID, nodeName)
	case "pod":
		apps, err = s.ResolveAffectedAppsByPod(ctx, clusterID, podNamespace, podName)
	case "cluster":
		apps, err = s.resolveAllAppsOnCluster(ctx, clusterID)
	default:
		return nil, apperr.Validation("scope must be pod|node|cluster", nil)
	}
	if err != nil {
		return nil, err
	}
	// 填充成员。
	for i := range apps {
		members, _, mErr := s.appRepo.ListAppMembers(ctx, apps[i].ApplicationID, 0, 1000)
		if mErr != nil {
			continue
		}
		for _, m := range members {
			if m.Status != application.MemberStatusActive {
				continue
			}
			apps[i].Members = append(apps[i].Members, Member{
				UserID:      m.UserID,
				UserName:    m.UserName,
				DisplayName: m.DisplayName,
				Email:       m.Email,
				RoleName:    m.RoleName,
			})
		}
	}
	return apps, nil
}

func (s *Service) resolveAllAppsOnCluster(ctx context.Context, clusterID int64) ([]AffectedApp, error) {
	groups, _, err := s.appRepo.ListGroups(ctx, application.GroupQuery{ClusterID: clusterID, Limit: 10000})
	if err != nil {
		return nil, apperr.Internal("list groups by cluster", err)
	}
	seen := make(map[int64]AffectedApp)
	for _, g := range groups {
		appName := ""
		if app, err := s.appRepo.GetApplicationByID(ctx, g.ApplicationID); err == nil {
			appName = app.Name
		}
		if existing, found := seen[g.ApplicationID]; found {
			existing.GroupNames = appendUnique(existing.GroupNames, g.Name)
			seen[g.ApplicationID] = existing
		} else {
			seen[g.ApplicationID] = AffectedApp{
				ApplicationID:   g.ApplicationID,
				ApplicationName: appName,
				GroupNames:      []string{g.Name},
			}
		}
	}
	out := make([]AffectedApp, 0, len(seen))
	for _, v := range seen {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ApplicationID < out[j].ApplicationID })
	return out, nil
}

// NotifyInput 通知分发输入。
type NotifyInput struct {
	Scope        string // pod|node|cluster
	NodeName     string
	PodNamespace string
	PodName      string
	Subject      string
	Body         string
	ActorID      int64
}

// NotifyResult 通知分发结果。
type NotifyResult struct {
	AffectedApps   []AffectedApp `json:"affected_apps"`
	NotifiedUserIDs []int64      `json:"notified_user_ids"`
	TotalNotified  int           `json:"total_notified"`
}

// NotifyAffected 解析受影响应用并分发站内通知给所有 active 成员。
func (s *Service) NotifyAffected(ctx context.Context, clusterID int64, in NotifyInput) (*NotifyResult, error) {
	apps, err := s.PreviewAffected(ctx, clusterID, in.Scope, in.NodeName, in.PodNamespace, in.PodName)
	if err != nil {
		return nil, err
	}
	seenUsers := make(map[int64]struct{})
	total := 0
	for _, app := range apps {
		payload := map[string]any{
			"app_id":            app.ApplicationID,
			"app_name":          app.ApplicationName,
			"group_names":       app.GroupNames,
			"scope":             in.Scope,
			"node_name":         in.NodeName,
			"pod_namespace":     in.PodNamespace,
			"pod_name":          in.PodName,
			"triggered_by":      in.ActorID,
		}
		for _, m := range app.Members {
			if _, ok := seenUsers[m.UserID]; ok {
				continue
			}
			seenUsers[m.UserID] = struct{}{}
			_, nErr := s.collab.CreateNotification(ctx, collabapp.CreateNotificationInput{
				UserID:    m.UserID,
				Channel:   collab.ChannelInApp,
				Subject:   in.Subject,
				Body:      in.Body,
				Payload:   payload,
			})
			if nErr == nil {
				total++
			}
		}
	}
	ids := make([]int64, 0, len(seenUsers))
	for id := range seenUsers {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return &NotifyResult{AffectedApps: apps, NotifiedUserIDs: ids, TotalNotified: total}, nil
}

// ============================================================================
// 运维任务执行（由调度器或立即执行调用）
// ============================================================================

// ExecuteOperation 执行一条运维任务。幂等：仅 pending 转 running 后执行。
func (s *Service) ExecuteOperation(ctx context.Context, op *clusterops.Operation) error {
	// 重新读取以确保最新状态（避免重复执行）。
	latest, err := s.repo.GetOperation(ctx, op.ID)
	if err != nil {
		return err
	}
	if latest.Status != clusterops.StatusPending {
		return nil
	}
	now := time.Now()
	latest.Status = clusterops.StatusRunning
	latest.ExecutedAt = &now
	latest.UpdatedBy = op.UpdatedBy
	if err := s.repo.UpdateOperation(ctx, latest); err != nil {
		return err
	}

	execErr := s.executeOpType(ctx, latest)
	completedAt := time.Now()
	if execErr != nil {
		latest.Status = clusterops.StatusFailed
		latest.ErrorMessage = truncateErr(execErr.Error())
	} else {
		latest.Status = clusterops.StatusCompleted
	}
	latest.CompletedAt = &completedAt
	_ = s.repo.UpdateOperation(ctx, latest)

	// 通知受影响应用参与人（无论成功失败都通知运维结果）。
	if latest.NotifyAffected {
		s.notifyOperationResult(ctx, latest, execErr)
	}
	return execErr
}

func (s *Service) executeOpType(ctx context.Context, op *clusterops.Operation) error {
	switch op.OperationType {
	case clusterops.OpCordon:
		return s.k8s.CordonNode(ctx, op.ClusterID, op.NodeName)
	case clusterops.OpUncordon:
		return s.k8s.UncordonNode(ctx, op.ClusterID, op.NodeName)
	case clusterops.OpDrain:
		return s.k8s.DrainNode(ctx, op.ClusterID, op.NodeName)
	case clusterops.OpRestart:
		// 计划重启：先 cordon + drain（驱逐所有 Pod），实际节点重启由云厂商或人工触发，
		// 完成后 uncordon 恢复调度。此处执行 drain + uncordon 序列。
		if err := s.k8s.CordonNode(ctx, op.ClusterID, op.NodeName); err != nil {
			return fmt.Errorf("cordon: %w", err)
		}
		if err := s.k8s.DrainNode(ctx, op.ClusterID, op.NodeName); err != nil {
			return fmt.Errorf("drain: %w", err)
		}
		// 注意：此处不自动 uncordon，等待人工确认节点已重启后手动 uncordon。
		return nil
	case clusterops.OpSyncStatus:
		_, err := s.SyncNodeStatuses(ctx, op.ClusterID)
		return err
	default:
		return fmt.Errorf("unknown operation_type: %s", op.OperationType)
	}
}

// notifyOperationResult 运维任务执行后通知受影响应用参与人。
func (s *Service) notifyOperationResult(ctx context.Context, op *clusterops.Operation, execErr error) {
	scope := "node"
	if op.NodeName == "" {
		scope = "cluster"
	}
	statusText := "成功"
	if execErr != nil {
		statusText = "失败：" + truncateErr(execErr.Error())
	}
	subject := fmt.Sprintf("【集群运维】%s 任务%s - 集群%d", op.OperationType, statusText, op.ClusterID)
	body := fmt.Sprintf("运维任务类型: %s\n集群ID: %d\n节点: %s\n计划时间: %s\n执行结果: %s",
		op.OperationType, op.ClusterID, op.NodeName, op.ScheduledAt.Format(time.RFC3339), statusText)
	_, _ = s.NotifyAffected(ctx, op.ClusterID, NotifyInput{
		Scope:    scope,
		NodeName: op.NodeName,
		Subject:  subject,
		Body:     body,
		ActorID:  op.UpdatedBy,
	})
}

// ============================================================================
// 辅助
// ============================================================================

func truncateErr(s string) string {
	if len(s) > 500 {
		return s[:500] + "..."
	}
	return s
}
