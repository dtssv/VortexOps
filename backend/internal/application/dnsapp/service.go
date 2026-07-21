// Package dnsapp 域名→Pod IP 映射应用服务。
package dnsapp

import (
	"context"
	"fmt"

	"github.com/vortexops/vortexops/internal/domain/application"
	dnsdomain "github.com/vortexops/vortexops/internal/domain/dns"
	k8sdns "github.com/vortexops/vortexops/internal/infrastructure/k8s/dns"
	"k8s.io/client-go/kubernetes"
)

// Repository DNS 仓储接口。
type Repository interface {
	UpsertRecord(ctx context.Context, rec *dnsdomain.Record) (*dnsdomain.Record, error)
	ReplaceBackends(ctx context.Context, recordID int64, backends []dnsdomain.Backend) error
	GetRecordByGroupID(ctx context.Context, groupID int64) (*dnsdomain.Record, error)
	ListAllActiveRecords(ctx context.Context) ([]*dnsdomain.Record, error)
	ListBackendsByRecordID(ctx context.Context, recordID int64) ([]dnsdomain.Backend, error)
	MarkBackendHealth(ctx context.Context, recordID int64, podIP string, healthy bool) error
	DeleteByGroupID(ctx context.Context, groupID int64) error
}

// K8sClientProvider 按集群 ID 提供 K8s clientset。
type K8sClientProvider interface {
	GetClient(ctx context.Context, clusterID int64) (kubernetes.Interface, error)
}

// Service DNS 映射服务。
type Service struct {
	repo           Repository
	clientProvider K8sClientProvider
	syncer         *k8sdns.ConfigMapSyncer
}

// New 创建服务。clientProvider 可为 nil（仅写 DB 不同步 CoreDNS）。
func New(repo Repository, clientProvider K8sClientProvider) *Service {
	return &Service{
		repo:           repo,
		clientProvider: clientProvider,
		syncer:         k8sdns.NewConfigMapSyncer(),
	}
}

// UpsertDomainMapping 发布成功后更新域名→Pod IP 映射。
func (s *Service) UpsertDomainMapping(ctx context.Context, g *application.Group, podIPs []string) (*dnsdomain.Record, error) {
	if g == nil {
		return nil, fmt.Errorf("group is required")
	}
	fqdn := dnsdomain.BuildFQDN(g.DeploymentName, g.Namespace)
	name := dnsdomain.BuildRelativeName(g.DeploymentName, g.Namespace)
	rec, err := s.repo.UpsertRecord(ctx, &dnsdomain.Record{
		GroupID:   g.ID,
		ClusterID: g.ClusterID,
		Zone:      dnsdomain.DefaultZone,
		Name:      name,
		FQDN:      fqdn,
		Type:      dnsdomain.RecordA,
		TTL:       dnsdomain.DefaultTTL,
		Status:    dnsdomain.StatusActive,
	})
	if err != nil {
		return nil, err
	}
	backends := make([]dnsdomain.Backend, 0, len(podIPs))
	for _, ip := range podIPs {
		if ip == "" {
			continue
		}
		backends = append(backends, dnsdomain.Backend{
			PodIP:   ip,
			Healthy: true,
			Weight:  100,
		})
	}
	if err := s.repo.ReplaceBackends(ctx, rec.ID, backends); err != nil {
		return nil, err
	}
	if err := s.syncCoreDNS(ctx, g.ClusterID); err != nil {
		// 软降级：DB 已更新，CoreDNS 同步失败不阻断发布。
		return rec, nil
	}
	return rec, nil
}

// ReconcileBackends 根据健康 Pod IP 列表更新后端（syncer 健康事件驱动）。
func (s *Service) ReconcileBackends(ctx context.Context, groupID int64, healthyIPs []string) error {
	rec, err := s.repo.GetRecordByGroupID(ctx, groupID)
	if err != nil || rec == nil {
		return err
	}
	healthySet := make(map[string]bool, len(healthyIPs))
	for _, ip := range healthyIPs {
		healthySet[ip] = true
	}
	existing, err := s.repo.ListBackendsByRecordID(ctx, rec.ID)
	if err != nil {
		return err
	}
	for _, b := range existing {
		healthy := healthySet[b.PodIP]
		if b.Healthy != healthy {
			if err := s.repo.MarkBackendHealth(ctx, rec.ID, b.PodIP, healthy); err != nil {
				return err
			}
		}
	}
	return s.syncCoreDNS(ctx, rec.ClusterID)
}

// GetByGroupID 查询分组 DNS 记录及后端。
func (s *Service) GetByGroupID(ctx context.Context, groupID int64) (*dnsdomain.Record, []dnsdomain.Backend, error) {
	rec, err := s.repo.GetRecordByGroupID(ctx, groupID)
	if err != nil || rec == nil {
		return rec, nil, err
	}
	bs, err := s.repo.ListBackendsByRecordID(ctx, rec.ID)
	return rec, bs, err
}

// DeleteByGroupID 删除分组 DNS 映射。
func (s *Service) DeleteByGroupID(ctx context.Context, groupID, clusterID int64) error {
	if err := s.repo.DeleteByGroupID(ctx, groupID); err != nil {
		return err
	}
	return s.syncCoreDNS(ctx, clusterID)
}

func (s *Service) syncCoreDNS(ctx context.Context, clusterID int64) error {
	if s.clientProvider == nil || clusterID == 0 {
		return nil
	}
	client, err := s.clientProvider.GetClient(ctx, clusterID)
	if err != nil {
		return err
	}
	records, err := s.repo.ListAllActiveRecords(ctx)
	if err != nil {
		return err
	}
	backends := make(map[int64][]dnsdomain.Backend)
	for _, rec := range records {
		bs, err := s.repo.ListBackendsByRecordID(ctx, rec.ID)
		if err != nil {
			return err
		}
		backends[rec.ID] = bs
	}
	return s.syncer.Sync(ctx, client, records, backends)
}
