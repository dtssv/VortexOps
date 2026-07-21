package podnet

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestDisplayIP_PrefersStableAnnotation(t *testing.T) {
	p := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{AnnotationStableIP0: "192.168.1.201"},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			PodIP: "10.42.0.100",
			PodIPs: []corev1.PodIP{{IP: "10.42.0.100"}},
		},
	}
	if got := DisplayIP(p); got != "192.168.1.201" {
		t.Fatalf("got %q", got)
	}
}

func TestInUseIPs_IncludesStableIP(t *testing.T) {
	p := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{AnnotationStableIP0: "192.168.1.201"},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			PodIP: "10.42.0.100",
		},
	}
	ips := InUseIPs(p)
	if len(ips) < 2 {
		t.Fatalf("expected overlay + stable, got %v", ips)
	}
}
