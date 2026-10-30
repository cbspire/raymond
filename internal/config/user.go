package config

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"

	"golang.org/x/sys/unix"
)

const defaultUserConfig = `# Raymond user configuration. Edit installations to point to your local apps.
version: 1
dns:
  zones: [raymond.test]
  listen: 127.0.0.43:53
proxy:
  listen: 127.0.0.42:80
installations: []

# Replace the empty installations list above with your own mappings, for example:
# installations:
#   - domain: install01.web.raymond.test
#     target: 127.0.0.1:9001
`

type configOwner struct {
	home string
	uid  int
}

// resolveUser uses sudo's invoking account, even when sudo changes HOME to root.
// An ordinary user's SUDO_UID environment variable has no authority.
func resolveUser(euid int, sudoUID, home string) (configOwner, error) {
	o := configOwner{home: home, uid: euid}
	if euid == 0 && sudoUID != "" {
		uid, e := strconv.Atoi(sudoUID)
		if e != nil || uid < 0 || strconv.Itoa(uid) != sudoUID {
			return o, fmt.Errorf("invalid SUDO_UID: cannot determine user config directory")
		}
		u, e := user.LookupId(sudoUID)
		if e != nil {
			return o, fmt.Errorf("cannot find invoking user: %w", e)
		}
		o = configOwner{home: u.HomeDir, uid: uid}
	}
	if !filepath.IsAbs(o.home) {
		return o, fmt.Errorf("user config requires an absolute home directory")
	}
	return o, nil
}

func currentConfigOwner() (configOwner, error) {
	home, e := os.UserHomeDir()
	if e != nil && (os.Geteuid() != 0 || os.Getenv("SUDO_UID") == "") {
		return configOwner{}, e
	}
	return resolveUser(os.Geteuid(), os.Getenv("SUDO_UID"), home)
}

// UserPath is the editable CLI config, separate from the privileged service copy.
func UserPath() (string, error) {
	o, e := currentConfigOwner()
	if e != nil {
		return "", e
	}
	return filepath.Join(o.home, ".raymond", "raymond.yaml"), nil
}

// InitUser atomically creates a valid starter config without replacing user edits.
// It runs as the config owner and uses directory-relative descriptors to reject
// symlinks without a check-then-open race.
func InitUser() (string, bool, error) {
	o, e := currentConfigOwner()
	if e != nil {
		return "", false, e
	}
	p := filepath.Join(o.home, ".raymond", "raymond.yaml")
	created, e := initUser(o)
	if e != nil {
		return p, false, fmt.Errorf("user config %s: %w", p, e)
	}
	return p, created, nil
}

func initUser(o configOwner) (bool, error) {
	if o.uid != os.Geteuid() {
		return false, fmt.Errorf("user config must be initialized as its owning user")
	}
	home, e := unix.Open(o.home, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if e != nil {
		return false, e
	}
	defer unix.Close(home)
	e = unix.Mkdirat(home, ".raymond", 0700)
	if e != nil && !errors.Is(e, unix.EEXIST) {
		return false, e
	}
	dir, e := unix.Openat(home, ".raymond", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if e != nil {
		return false, e
	}
	defer unix.Close(dir)
	var st unix.Stat_t
	if e = unix.Fstat(dir, &st); e != nil {
		return false, e
	}
	if int(st.Uid) != o.uid || st.Mode&0022 != 0 {
		return false, fmt.Errorf(".raymond must belong to the user and must not be group/world writable")
	}
	if exists, e := existingUserConfig(dir); e != nil || exists {
		return false, e
	}
	name := ".raymond-" + rand.Text()
	fd, e := unix.Openat(dir, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if e != nil {
		return false, e
	}
	defer unix.Unlinkat(dir, name, 0)
	f := os.NewFile(uintptr(fd), name)
	defer f.Close()
	if _, e = f.WriteString(defaultUserConfig); e != nil {
		return false, e
	}
	if e = f.Sync(); e != nil {
		return false, e
	}
	if e = unix.Linkat(dir, name, dir, "raymond.yaml", 0); errors.Is(e, unix.EEXIST) {
		_, e = existingUserConfig(dir)
		return false, e
	} else if e != nil {
		return false, e
	}
	return true, unix.Fsync(dir)
}

func existingUserConfig(dir int) (bool, error) {
	fd, e := unix.Openat(dir, "raymond.yaml", unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if errors.Is(e, unix.ENOENT) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	f := os.NewFile(uintptr(fd), "raymond.yaml")
	defer f.Close()
	st, e := f.Stat()
	if e != nil {
		return true, e
	}
	if !st.Mode().IsRegular() {
		return true, fmt.Errorf("raymond.yaml must be a regular file")
	}
	_, e = Decode(f)
	return true, e
}
