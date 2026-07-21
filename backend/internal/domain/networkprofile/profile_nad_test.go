package networkprofile

import "testing"

func TestMultusNADName(t *testing.T) {
	if got := MultusNADName(CNIMacvlan, 0); got != "macvlan" {
		t.Fatalf("no vlan: got %q", got)
	}
	if got := MultusNADName(CNIMacvlan, 100); got != "macvlan-100" {
		t.Fatalf("with vlan: got %q", got)
	}
}

func TestProfileConfig_NADName(t *testing.T) {
	cfg := &ProfileConfig{CNI: CNIIPVLAN, VLANID: 200}
	if got := cfg.NADName(); got != "ipvlan-200" {
		t.Fatalf("got %q", got)
	}
}
