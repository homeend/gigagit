package tui

import (
	"path"
	"strings"
)

// restoredPath inserts a _RESTORED marker into a repo-relative path for the
// paste/restore destination prefill:
//   - dotfile (basename starts "."): append          .gitignore -> .gitignore_RESTORED
//   - has an extension (last "." > 0): insert before config.go -> config_RESTORED.go
//   - no extension:                    append        Makefile  -> Makefile_RESTORED
func restoredPath(p string) string {
	const marker = "_RESTORED"
	dir, base := path.Split(p) // dir keeps its trailing "/"
	if base == "" || strings.HasPrefix(base, ".") {
		return p + marker
	}
	if i := strings.LastIndex(base, "."); i > 0 {
		return dir + base[:i] + marker + base[i:]
	}
	return p + marker
}
