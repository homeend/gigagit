//go:build linux

package gitwatch

import "syscall"

// V9fsMagic is the Linux superblock magic for the 9P (Plan 9) filesystem, which
// WSL2 uses for drvfs mounts under /mnt. inotify does not reliably deliver
// events on such mounts, so file-watching is disabled there.
const V9fsMagic = 0x01021997

// Supported reports whether file-watching is viable for the repository whose git
// common dir is commonDir. It returns false for 9P/v9fs mounts (WSL2 drvfs) and
// true otherwise; a statfs error fails open (returns true) so a normal repo is
// never wrongly disabled.
func Supported(commonDir string) bool {
	var st syscall.Statfs_t
	if err := syscall.Statfs(commonDir, &st); err != nil {
		return true
	}
	return int64(st.Type) != int64(V9fsMagic)
}
