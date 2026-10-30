package install

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"syscall"
)

func installerUID() (int, error) {
	return sourceUID(os.Geteuid(), os.Getenv("SUDO_UID"))
}

func sourceUID(euid int, s string) (int, error) {
	if euid != 0 || s == "" {
		return euid, nil
	}
	uid, e := strconv.Atoi(s)
	if e != nil || uid < 0 || strconv.Itoa(uid) != s {
		return 0, fmt.Errorf("invalid SUDO_UID: cannot determine executable owner")
	}
	return uid, nil
}

// Explicit sudo installation can read the invoking user's reviewed executable.
// Apply publishes these bytes to a separate, fixed, root-owned service path;
// service startup never refers to the user's checkout or downloaded binary.
func readInstallExecutable(path string, owner int) ([]byte, error) {
	if e := trustedFor(path, owner); e != nil {
		return nil, fmt.Errorf("unsafe install source: %w", e)
	}
	f, e := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil {
		return nil, e
	}
	if !st.Mode().IsRegular() || st.Size() == 0 || st.Mode().Perm()&0111 == 0 || st.Mode().Perm()&0022 != 0 {
		return nil, fmt.Errorf("install source must be a non-empty regular executable, not group/world writable")
	}
	identity, ok := st.Sys().(*syscall.Stat_t)
	if !ok || (identity.Uid != 0 && int(identity.Uid) != owner) {
		return nil, fmt.Errorf("install source must belong to root or the invoking user")
	}
	const maxExecutableSize = 256 << 20
	b, e := io.ReadAll(io.LimitReader(f, maxExecutableSize+1))
	if e != nil {
		return nil, e
	}
	if len(b) > maxExecutableSize {
		return nil, fmt.Errorf("install executable exceeds 256 MiB")
	}
	after, e := f.Stat()
	if e != nil {
		return nil, e
	}
	if after.Size() != st.Size() || !after.ModTime().Equal(st.ModTime()) || int64(len(b)) != st.Size() {
		return nil, fmt.Errorf("install executable changed while reading; retry installation after the build finishes")
	}
	return b, nil
}
