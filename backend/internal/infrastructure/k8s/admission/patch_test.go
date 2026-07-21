package admission

import (
	"testing"

	"github.com/vortexops/vortexops/internal/domain/networkprofile"
)

func TestBuildCNIAnnotations_Cilium(t *testing.T) {
	profile := &networkprofile.ProfileConfig{CNI: networkprofile.CNICilium}
	ann := BuildCNIAnnotations("10.42.0.100", profile)
	if ann["cilium.io/ipv4-pod-ip"] != "10.42.0.100" {
		t.Fatalf("missing cilium ipv4-pod-ip: %v", ann)
	}
	if ann["ipam.cilium.io/ip-pool"] != "vortexops-stable-ip" {
		t.Fatalf("missing ip pool: %v", ann)
	}
	if ann["cni.projectcalico.org/ipAddrs"] == "" {
		t.Fatalf("expected calico compat annotation")
	}
}

func TestBuildStableIPAnnotations(t *testing.T) {
	ann := BuildStableIPAnnotations("10.42.0.101", 1, &networkprofile.ProfileConfig{CNI: networkprofile.CNICalico})
	if ann[AnnotationReplicaIndex] != "1" {
		t.Fatalf("replica index: %v", ann)
	}
	if ann[AnnotationAssignedBy] != "webhook" {
		t.Fatalf("assigned-by: %v", ann)
	}
}

func TestBuildPatch(t *testing.T) {
	existing := map[string]string{"a": "old"}
	desired := map[string]string{"a": "new", "b": "val"}
	ops := BuildPatch(existing, desired)
	if len(ops) != 2 {
		t.Fatalf("expected 2 ops, got %d", len(ops))
	}
}
