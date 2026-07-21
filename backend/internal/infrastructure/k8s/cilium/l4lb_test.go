package cilium

import (
	"testing"

	"github.com/vortexops/vortexops/internal/domain/application"
)

func TestRenderL4LoadBalancer(t *testing.T) {
	g := &application.Group{ID: 3, DeploymentName: "svc", Namespace: "ns"}
	obj := RenderL4LoadBalancer(L4LBInput{Group: g, BackendIPs: []string{"10.42.0.1", "10.42.0.2"}})
	if obj == nil {
		t.Fatal("expected L4 LB resource")
	}
	if obj.GetName() != "vortexops-lb-g3" {
		t.Fatalf("name=%s", obj.GetName())
	}
}

func TestBuildStaticIPAnnotations(t *testing.T) {
	ann := BuildStaticIPAnnotations("10.42.0.5", "")
	if ann[AnnotationIPv4PodIP] != "10.42.0.5" {
		t.Fatalf("ann=%v", ann)
	}
}
