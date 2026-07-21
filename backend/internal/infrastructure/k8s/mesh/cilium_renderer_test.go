package mesh

import (
	"testing"

	"github.com/vortexops/vortexops/internal/domain/application"
)

func TestRenderAll_MeshEnabled(t *testing.T) {
	g := &application.Group{ID: 5, DeploymentName: "app", Namespace: "default", MeshEnabled: true}
	objs := RenderAll(RenderInput{Group: g, StableIPs: []string{"10.0.0.1"}})
	if len(objs) != 2 {
		t.Fatalf("expected 2 mesh CRDs, got %d", len(objs))
	}
}

func TestRenderAll_MeshDisabled(t *testing.T) {
	g := &application.Group{ID: 5, MeshEnabled: false}
	if len(RenderAll(RenderInput{Group: g})) != 0 {
		t.Fatal("expected no resources when mesh disabled")
	}
}
