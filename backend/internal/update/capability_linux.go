//go:build linux

package update

import (
	"errors"

	"golang.org/x/sys/unix"
)

// copyCapability carries the file-capabilities xattr (setcap cap_net_raw+ep)
// from the running binary onto its replacement. No xattr on the source is the
// normal case; a failure to set one is fatal so a swapped binary can never
// silently lose raw-socket access.
func copyCapability(src, dst string) error {
	buf := make([]byte, 256)
	n, err := unix.Lgetxattr(src, capabilityXattr, buf)
	if errors.Is(err, unix.ENODATA) || errors.Is(err, unix.ENOTSUP) {
		return nil
	}
	if errors.Is(err, unix.ERANGE) {
		return errors.New("file capabilities xattr larger than expected")
	}
	if err != nil {
		return err
	}
	if err := unix.Lsetxattr(dst, capabilityXattr, buf[:n], 0); err != nil {
		return err
	}
	return nil
}
