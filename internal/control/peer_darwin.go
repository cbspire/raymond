package control

import "golang.org/x/sys/unix"

const SocketDir = "/var/run/raymond-control"

func peerUID(fd uintptr) (int, error) {
	c, e := unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
	if e != nil {
		return -1, e
	}
	return int(c.Uid), nil
}
