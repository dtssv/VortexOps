package dnsapp

import (
	"context"
	"testing"

	"github.com/vortexops/vortexops/internal/domain/application"
	dnsdomain "github.com/vortexops/vortexops/internal/domain/dns"
)

type memRepo struct {
	records  map[int64]*dnsdomain.Record
	backends map[int64][]dnsdomain.Backend
	nextID   int64
}

func (m *memRepo) UpsertRecord(ctx context.Context, rec *dnsdomain.Record) (*dnsdomain.Record, error) {
	m.nextID++
	rec.ID = m.nextID
	if m.records == nil {
		m.records = map[int64]*dnsdomain.Record{}
	}
	m.records[rec.GroupID] = rec
	return rec, nil
}

func (m *memRepo) ReplaceBackends(ctx context.Context, recordID int64, backends []dnsdomain.Backend) error {
	if m.backends == nil {
		m.backends = map[int64][]dnsdomain.Backend{}
	}
	m.backends[recordID] = backends
	return nil
}

func (m *memRepo) GetRecordByGroupID(ctx context.Context, groupID int64) (*dnsdomain.Record, error) {
	return m.records[groupID], nil
}

func (m *memRepo) ListAllActiveRecords(ctx context.Context) ([]*dnsdomain.Record, error) {
	var out []*dnsdomain.Record
	for _, r := range m.records {
		out = append(out, r)
	}
	return out, nil
}

func (m *memRepo) ListBackendsByRecordID(ctx context.Context, recordID int64) ([]dnsdomain.Backend, error) {
	return m.backends[recordID], nil
}

func (m *memRepo) MarkBackendHealth(ctx context.Context, recordID int64, podIP string, healthy bool) error {
	for i, b := range m.backends[recordID] {
		if b.PodIP == podIP {
			m.backends[recordID][i].Healthy = healthy
		}
	}
	return nil
}

func (m *memRepo) DeleteByGroupID(ctx context.Context, groupID int64) error {
	delete(m.records, groupID)
	return nil
}

func TestUpsertDomainMapping(t *testing.T) {
	repo := &memRepo{}
	svc := New(repo, nil)
	g := &application.Group{ID: 1, ClusterID: 10, DeploymentName: "app-1", Namespace: "default"}
	rec, err := svc.UpsertDomainMapping(context.Background(), g, []string{"10.42.0.100", "10.42.0.101"})
	if err != nil {
		t.Fatal(err)
	}
	want := dnsdomain.BuildFQDN("app-1", "default")
	if rec.FQDN != want {
		t.Fatalf("fqdn=%q want %q", rec.FQDN, want)
	}
	bs := repo.backends[rec.ID]
	if len(bs) != 2 {
		t.Fatalf("backends=%d want 2", len(bs))
	}
}

func TestReconcileBackends(t *testing.T) {
	repo := &memRepo{}
	svc := New(repo, nil)
	g := &application.Group{ID: 2, ClusterID: 10, DeploymentName: "app-2", Namespace: "ns"}
	rec, _ := svc.UpsertDomainMapping(context.Background(), g, []string{"10.0.0.1", "10.0.0.2"})
	if err := svc.ReconcileBackends(context.Background(), g.ID, []string{"10.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	for _, b := range repo.backends[rec.ID] {
		if b.PodIP == "10.0.0.2" && b.Healthy {
			t.Fatal("10.0.0.2 should be unhealthy")
		}
	}
}

func TestBuildFQDN(t *testing.T) {
	if dnsdomain.BuildFQDN("svc", "prod") != "svc.prod.svc.vortexops.local" {
		t.Fatal("unexpected fqdn")
	}
}
