//go:build osintegration && (darwin || linux)

package acceptance

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func assertLoopbackListeners(t *testing.T, pid int) {
	t.Helper()
	want := map[string]bool{"tcp 127.0.0.42:80": false, "tcp 127.0.0.43:53": false, "udp 127.0.0.43:53": false}
	got := map[string]bool{}
	if runtime.GOOS == "darwin" {
		b := must(t, "/usr/sbin/lsof", "-nP", "-a", "-p", strconv.Itoa(pid), "-i", "-F", "Pn")
		protocol := ""
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, "P") {
				protocol = strings.ToLower(line[1:])
			}
			if strings.HasPrefix(line, "n") && !strings.Contains(line, "->") {
				got[protocol+" "+line[1:]] = true
			}
		}
	} else {
		fds, e := os.ReadDir(fmt.Sprintf("/proc/%d/fd", pid))
		if e != nil {
			t.Fatal(e)
		}
		inodes := map[string]bool{}
		for _, fd := range fds {
			target, e := os.Readlink(filepath.Join(fmt.Sprintf("/proc/%d/fd", pid), fd.Name()))
			if e == nil && strings.HasPrefix(target, "socket:[") {
				inodes[strings.TrimSuffix(strings.TrimPrefix(target, "socket:["), "]")] = true
			}
		}
		for _, table := range []string{"tcp", "udp", "tcp6", "udp6"} {
			b, e := os.ReadFile(fmt.Sprintf("/proc/%d/net/%s", pid, table))
			if e != nil {
				t.Fatal(e)
			}
			for _, line := range strings.Split(string(b), "\n") {
				fields := strings.Fields(line)
				if len(fields) < 10 || !inodes[fields[9]] {
					continue
				}
				if strings.HasPrefix(table, "tcp") && fields[3] != "0A" {
					continue
				}
				if strings.HasSuffix(table, "6") {
					t.Fatalf("unexpected IPv6 listener: %s", line)
				}
				parts := strings.Split(fields[1], ":")
				if len(parts) != 2 {
					t.Fatal("invalid kernel socket record")
				}
				ip, e := strconv.ParseUint(parts[0], 16, 32)
				if e != nil {
					t.Fatal(e)
				}
				port, e := strconv.ParseUint(parts[1], 16, 16)
				if e != nil {
					t.Fatal(e)
				}
				address := net.IPv4(byte(ip), byte(ip>>8), byte(ip>>16), byte(ip>>24)).String()
				got[fmt.Sprintf("%s %s:%d", table, address, port)] = true
			}
		}
	}
	for address := range got {
		if _, ok := want[address]; !ok {
			t.Errorf("unexpected bound listener %s", address)
		} else {
			want[address] = true
		}
	}
	for address, found := range want {
		if !found {
			t.Errorf("expected listener missing: %s (got %v)", address, got)
		}
	}
	t.Log("kernel socket inspection confirmed exactly the dedicated loopback TCP/UDP listeners")
}
