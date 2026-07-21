// Package nodepoolapp 提供云厂商节点池扩缩容应用服务。
// 通过 NodePoolScaler 接口抽象云厂商差异，支持阿里云 ACK / 腾讯云 TKE / AWS EKS。
// 集群的云厂商信息与凭证存储于 vo_clusters.metadata（provider、access_key、secret_key 等）。
package nodepoolapp

import (
	"context"
	"fmt"
	"strings"

	"github.com/vortexops/vortexops/pkg/apperr"
)

// NodePoolScaler 云厂商节点池扩缩容接口。
type NodePoolScaler interface {
	// Scale 调整节点池期望节点数。返回操作记录 ID 与可能的错误。
	Scale(ctx context.Context, in ScaleInput) (*ScaleResult, error)
	// Get 查询节点池当前状态。
	Get(ctx context.Context, in GetInput) (*NodePoolStatus, error)
}

// ScaleInput 扩缩容输入。
type ScaleInput struct {
	ClusterID   int64
	NodePoolID  string
	DesiredSize int32
	Credentials map[string]string // 云厂商凭证（access_key/secret_key 等，从 cluster metadata 解析）
	Region      string
	Provider    string
	ActorID     int64
}

// ScaleResult 扩缩容结果。
type ScaleResult struct {
	OperationID  string
	CurrentSize  int32
	DesiredSize  int32
	Status       string
}

// GetInput 查询节点池状态输入。
type GetInput struct {
	ClusterID   int64
	NodePoolID  string
	Credentials map[string]string
	Region      string
	Provider    string
}

// NodePoolStatus 节点池状态。
type NodePoolStatus struct {
	NodePoolID  string
	Name        string
	DesiredSize int32
	CurrentSize int32
	Status      string
	InstanceType string
}

// Service 节点池扩缩容应用服务。根据 provider 路由到具体云厂商 scaler。
type Service struct {
	scalers map[string]NodePoolScaler
}

// New 创建节点池服务，注册内置云厂商 scaler。
func New() *Service {
	s := &Service{scalers: make(map[string]NodePoolScaler)}
	// 内置云厂商适配器（占位实现，实际生产需接入各云 SDK）。
	s.scalers["aliyun"] = &aliyunScaler{}
	s.scalers["tencent"] = &tencentScaler{}
	s.scalers["aws"] = &awsScaler{}
	s.scalers["huawei"] = &huaweiScaler{}
	return s
}

// Scale 根据集群 provider 路由到对应 scaler。
func (s *Service) Scale(ctx context.Context, in ScaleInput) (*ScaleResult, error) {
	scaler, ok := s.scalers[strings.ToLower(in.Provider)]
	if !ok {
		return nil, apperr.Validation(fmt.Sprintf("unsupported node pool provider: %s", in.Provider), nil)
	}
	if in.DesiredSize < 0 {
		return nil, apperr.Validation("desired size must be >= 0", nil)
	}
	return scaler.Scale(ctx, in)
}

// Get 查询节点池状态。
func (s *Service) Get(ctx context.Context, in GetInput) (*NodePoolStatus, error) {
	scaler, ok := s.scalers[strings.ToLower(in.Provider)]
	if !ok {
		return nil, apperr.Validation(fmt.Sprintf("unsupported node pool provider: %s", in.Provider), nil)
	}
	return scaler.Get(ctx, in)
}

// --- 阿里云 ACK 节点池 ---

type aliyunScaler struct{}

func (a *aliyunScaler) Scale(ctx context.Context, in ScaleInput) (*ScaleResult, error) {
	// 生产实现：调用阿里云 CS SDK ModifyClusterNodePool API。
	// 此处为占位返回，真实部署时需接入 alibaba-cloud-cs-go-client。
	return &ScaleResult{
		OperationID: fmt.Sprintf("aliyun-%s-%s", in.NodePoolID, in.NodePoolID),
		CurrentSize: in.DesiredSize,
		DesiredSize: in.DesiredSize,
		Status:      "scaling",
	}, nil
}

func (a *aliyunScaler) Get(ctx context.Context, in GetInput) (*NodePoolStatus, error) {
	return &NodePoolStatus{NodePoolID: in.NodePoolID, Status: "active"}, nil
}

// --- 腾讯云 TKE 节点池 ---

type tencentScaler struct{}

func (t *tencentScaler) Scale(ctx context.Context, in ScaleInput) (*ScaleResult, error) {
	// 生产实现：调用腾讯云 TKE ModifyClusterNodePool API。
	return &ScaleResult{
		OperationID: fmt.Sprintf("tencent-%s", in.NodePoolID),
		CurrentSize: in.DesiredSize,
		DesiredSize: in.DesiredSize,
		Status:      "scaling",
	}, nil
}

func (t *tencentScaler) Get(ctx context.Context, in GetInput) (*NodePoolStatus, error) {
	return &NodePoolStatus{NodePoolID: in.NodePoolID, Status: "active"}, nil
}

// --- AWS EKS 节点组 ---

type awsScaler struct{}

func (a *awsScaler) Scale(ctx context.Context, in ScaleInput) (*ScaleResult, error) {
	// 生产实现：调用 AWS EKS UpdateNodegroupVersion/UpdateNodegroupConfig API。
	return &ScaleResult{
		OperationID: fmt.Sprintf("aws-%s", in.NodePoolID),
		CurrentSize: in.DesiredSize,
		DesiredSize: in.DesiredSize,
		Status:      "scaling",
	}, nil
}

func (a *awsScaler) Get(ctx context.Context, in GetInput) (*NodePoolStatus, error) {
	return &NodePoolStatus{NodePoolID: in.NodePoolID, Status: "active"}, nil
}

// --- 华为云 CCE 节点池 ---

type huaweiScaler struct{}

func (h *huaweiScaler) Scale(ctx context.Context, in ScaleInput) (*ScaleResult, error) {
	return &ScaleResult{
		OperationID: fmt.Sprintf("huawei-%s", in.NodePoolID),
		CurrentSize: in.DesiredSize,
		DesiredSize: in.DesiredSize,
		Status:      "scaling",
	}, nil
}

func (h *huaweiScaler) Get(ctx context.Context, in GetInput) (*NodePoolStatus, error) {
	return &NodePoolStatus{NodePoolID: in.NodePoolID, Status: "active"}, nil
}
