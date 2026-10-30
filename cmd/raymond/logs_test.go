package main

import "testing"

func TestLogsOptions(t *testing.T) {
	for _, tc := range []struct {
		args   []string
		lines  int
		follow bool
		valid  bool
	}{
		{nil, 50, true, true},
		{[]string{"--no-follow"}, 50, false, true},
		{[]string{"--lines", "3"}, 3, true, true},
		{[]string{"-n", "0", "--no-follow"}, 0, false, true},
		{[]string{"-f", "--lines", "8"}, 8, true, true},
		{[]string{"--lines", "-1"}, 0, false, false},
		{[]string{"--lines", "10001"}, 0, false, false},
		{[]string{"--unknown"}, 0, false, false},
		{[]string{"extra"}, 0, false, false},
	} {
		opts, e := parseLogOptions(tc.args)
		if (e == nil) != tc.valid || (e == nil && (opts.Lines != tc.lines || opts.Follow != tc.follow)) {
			t.Fatalf("logs %v: %+v, %v", tc.args, opts, e)
		}
	}
}
