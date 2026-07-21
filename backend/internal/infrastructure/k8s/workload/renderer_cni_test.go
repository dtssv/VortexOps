package workload

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"

	"github.com/vortexops/vortexops/internal/domain/application"
	"github.com/vortexops/vortexops/internal/domain/networkprofile"
)

func TestInjectCNIAnnotations_Cilium(t *testing.T) {
	ann := map[string]string{}
	injectCNIAnnotations(ann, []string{"10.42.0.100"}, &networkprofile.ProfileConfig{
		CNI:       networkprofile.CNICilium,
		DataPlane: networkprofile.DataPlaneCilium,
	})
	if ann["cilium.io/ipv4-pod-ip"] != "10.42.0.100" {
		t.Fatalf("cilium ip: %v", ann)
	}
}

func TestRender_MeshAndCiliumResources(t *testing.T) {
	g := &application.Group{
		ID: 1, ApplicationID: 1, ClusterID: 1, Namespace: "default",
		DeploymentName: "app-1", Replicas: 3, MeshEnabled: true,
		Workload: application.Workload{Type: application.WorkloadDeployment},
	}
	result, err := Render(RenderInput{
		Group: g, ImageRef: "registry/app:1",
		StableIPs: []string{"10.42.0.100", "10.42.0.101", "10.42.0.102"},
		NetworkProfile: &networkprofile.ProfileConfig{
			Profile:   networkprofile.ProfileMediumOverlay,
			CNI:       networkprofile.CNICilium,
			DataPlane: networkprofile.DataPlaneCilium,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.MeshResources) == 0 {
		t.Fatal("expected mesh resources")
	}
	if len(result.CiliumResources) == 0 {
		t.Fatal("expected cilium L4 LB resource")
	}
}

func TestRender_MeshLabelOnPodTemplate(t *testing.T) {
	g := &application.Group{
		ID: 2, ApplicationID: 1, ClusterID: 1, Namespace: "ns",
		DeploymentName: "svc", Replicas: 1, MeshEnabled: true,
		Workload: application.Workload{Type: application.WorkloadDeployment},
	}
	result, err := Render(RenderInput{Group: g, ImageRef: "img:tag"})
	if err != nil {
		t.Fatal(err)
	}
	dep, ok := result.Workload.(*appsv1.Deployment)
	if !ok {
		t.Fatalf("expected deployment, got %T", result.Workload)
	}
	if dep.Spec.Template.Labels["app.vortexops.io/mesh-enabled"] != "true" {
		t.Fatalf("mesh label missing: %v", dep.Spec.Template.Labels)
	}
}
