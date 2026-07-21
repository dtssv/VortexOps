package releaseapp

import "testing"

func TestStableIPRollingOverrides(t *testing.T) {
	cases := []struct {
		name       string
		stableIPs  []string
		replicas   int
		inSurge    string
		inUnavail  string
		wantSurge  string
		wantUnavail string
	}{
		{"no stable ip", nil, 3, "", "", "", ""},
		{"3 replicas default", []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"}, 3, "", "", "0%", "34%"},
		{"1 replica", []string{"10.0.0.1"}, 1, "", "", "0%", "100%"},
		{"explicit override", []string{"10.0.0.1"}, 3, "10%", "25%", "10%", "25%"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			surge, unavail := stableIPRollingOverrides(tc.stableIPs, tc.replicas, tc.inSurge, tc.inUnavail)
			if surge != tc.wantSurge || unavail != tc.wantUnavail {
				t.Fatalf("got maxSurge=%q maxUnavailable=%q, want %q %q", surge, unavail, tc.wantSurge, tc.wantUnavail)
			}
		})
	}
}
