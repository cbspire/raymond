package control

import "golang.org/x/sys/unix"

const SocketDir = "/run/raymond-control"

func peerUID(fd uintptr) (int, error) {
	c, e := unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	if e != nil {
		return -1, e
	}
	return int(c.Uid), nil
}
