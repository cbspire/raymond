package linux

import "testing"

func TestZoneOwnership(t *testing.T) {
	for _, tc := range []struct {
		output   string
		conflict bool
	}{{"Global: ~.\nLink 2 (eth0): example.org", false}, {"Link 5 (raymond0): ~test ~internal", false}, {"Link 3 (vpn0): ~test", true}, {"Global: INTERNAL.", true}, {"Link 3 (vpn0): ~private.internal", true}, {"Link 3 (vpn0): ~eviltest", false}, {"Link 3 (vpn0): ~(none)", false}} {
		if e := CheckZones([]string{"test", "internal"}, tc.output); (e != nil) != tc.conflict {
			t.Errorf("%q conflict=%v, err=%v", tc.output, tc.conflict, e)
		}
	}
}
