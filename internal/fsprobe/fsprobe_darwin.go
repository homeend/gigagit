package fsprobe

import "syscall"

// Foreign reports whether path lives on a network filesystem. Darwin's statfs
// exposes the type by name; the set covers the common network mounts.
func Foreign(path string) bool {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return false
	}
	name := make([]byte, 0, len(st.Fstypename))
	for _, c := range st.Fstypename {
		if c == 0 {
			break
		}
		name = append(name, byte(c))
	}
	switch string(name) {
	case "smbfs", "nfs", "afpfs", "webdav":
		return true
	}
	return false
}
