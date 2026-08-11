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

func TestRequiresUnderlaySecondary(t *testing.T) {
	if !(&ProfileConfig{Profile: ProfileDevSingle}).RequiresUnderlaySecondary() {
		t.Fatal("dev-single should require underlay secondary")
	}
	if !(&ProfileConfig{Profile: ProfileMediumOverlay}).RequiresUnderlaySecondary() {
		t.Fatal("medium-overlay should require underlay secondary")
	}
	if (&ProfileConfig{Profile: ProfileLargeUnderlay}).RequiresUnderlaySecondary() {
		t.Fatal("large-underlay should not require secondary")
	}
	if !((*ProfileConfig)(nil)).RequiresUnderlaySecondary() {
		t.Fatal("nil profile should require secondary")
	}
}

func TestSupportsStaticIP(t *testing.T) {
	if !(&ProfileConfig{CNI: CNICalico}).SupportsStaticIP() {
		t.Fatal("calico should support static IP")
	}
	if (&ProfileConfig{CNI: CNIFlannel}).SupportsStaticIP() {
		t.Fatal("flannel should not support static IP")
	}
}

func TestValidateDirectAccessCapability(t *testing.T) {
	err := ValidateDirectAccessCapability(&ProfileConfig{Profile: ProfileDevSingle, CNI: CNIFlannel}, []string{"whereabouts"})
	if err == nil {
		t.Fatal("dev-single without multus should fail")
	}
	err = ValidateDirectAccessCapability(&ProfileConfig{
		Profile: ProfileDevSingle, CNI: CNIFlannel, MultusEnabled: true,
	}, []string{"macvlan"})
	if err != nil {
		t.Fatalf("dev-single + multus + macvlan pool: %v", err)
	}
	err = ValidateDirectAccessCapability(&ProfileConfig{Profile: ProfileLargeUnderlay, CNI: CNIMacvlan}, nil)
	if err == nil {
		t.Fatal("underlay without pool should fail")
	}
	err = ValidateDirectAccessCapability(&ProfileConfig{
		Profile: ProfileXLargeBGP, CNI: CNICalico,
	}, []string{"calico-ipam"})
	if err != nil {
		t.Fatalf("bgp + calico-ipam: %v", err)
	}
}
