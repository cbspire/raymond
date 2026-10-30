package diagnostics

import "testing"

func TestWildcardFieldOutput(t *testing.T) {
	for _, tc := range []struct {
		data string
		want bool
	}{{"p1\nf4\nn*:80\n", true}, {"p1\nn0.0.0.0:80\n", true}, {"p1\nn[::]:53\n", true}, {"p1\nn127.0.0.1:80\n", false}, {"p1\nn127.0.0.42:80\n", false}, {"p1\nn[::1]:80\n", false}, {"", false}} {
		if got := wildcardOutput([]byte(tc.data)); got != tc.want {
			t.Errorf("%q got %v", tc.data, got)
		}
	}
}
