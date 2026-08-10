// Package fsprobe classifies paths that live on "foreign" filesystem mounts —
// network filesystems and OS-bridge mounts (WSL drvfs, sshfs) where per-file
// metadata is orders of magnitude slower than a native disk, making whole-tree
// git operations (status, checkout) crawl. The repo switcher uses it to warn
// before the user commits to a slow switch. Probes must never block the UI:
// callers run Foreign off-thread with a deadline (a dead network mount can
// wedge statfs indefinitely). A failed probe reports false — no warning on
// unknown, never a spurious one.
package fsprobe

import "syscall"

// Filesystem magic numbers from linux/magic.h.
const (
	v9fsMagic = 0x01021997 // 9p — WSL's drvfs mounts of Windows drives (/mnt/<drive>)
	cifsMagic = 0xFF534D42 // cifs (SMB1 era mounts)
	smb2Magic = 0xFE534D42 // smb2/smb3
	nfsMagic  = 0x6969     // nfs
	fuseMagic = 0x65735546 // fuse — sshfs, virtiofs and other userspace bridges
	afsMagic  = 0x5346414F // afs
)

// foreignType reports whether a statfs f_type belongs to the slow set.
func foreignType(t int64) bool {
	switch t {
	case v9fsMagic, cifsMagic, smb2Magic, nfsMagic, fuseMagic, afsMagic:
		return true
	}
	return false
}

// Foreign reports whether path lives on a foreign (slow-metadata) filesystem.
func Foreign(path string) bool {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return false
	}
	return foreignType(int64(st.Type))
}
