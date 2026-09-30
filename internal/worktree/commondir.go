package worktree

import (
	"os"
	"path/filepath"
	"strings"
)

// CommonDirAt returns the git common directory of the checkout at path — the
// one .git directory a main checkout and all of its linked worktrees share —
// or "" when path holds no checkout this environment can read. It only reads
// files (no git invocation), so it is cheap enough to run per registry entry.
// It is cross-environment aware: a worktree created on the other side of a
// disk shared by WSL and Windows records its admin dir in that side's notation
// (T:/… vs /mnt/t/…), which git itself cannot follow; the pointer is
// translated through TranslatePath instead.
func CommonDirAt(goos, path string) string {
	isDir := func(p string) (bool, bool) {
		fi, err := os.Stat(p)
		if err != nil {
			return false, false
		}
		return fi.IsDir(), true
	}
	return commonDirAt(isDir, os.ReadFile, goos, path)
}

// commonDirAt is CommonDirAt over an injected filesystem. isDir reports
// (is a directory, exists).
func commonDirAt(isDir func(string) (bool, bool), read func(string) ([]byte, error), goos, path string) string {
	// locate resolves a recorded path against base (git writes both pointers
	// relative to the file holding them) and, failing that, under the other
	// environment's notation.
	locate := func(p, base string) string {
		if !absAnyNotation(p) {
			p = filepath.Join(base, p)
		}
		if _, ok := isDir(p); ok {
			return filepath.Clean(p)
		}
		if tp, ok := TranslatePath(goos, p); ok {
			if _, ok := isDir(tp); ok {
				return filepath.Clean(tp)
			}
		}
		return ""
	}

	link := filepath.Join(path, ".git")
	dir, ok := isDir(link)
	if !ok {
		return ""
	}
	if dir {
		return filepath.Clean(link) // a main checkout
	}
	b, err := read(link)
	if err != nil {
		return ""
	}
	content := strings.TrimSpace(string(b))
	const prefix = "gitdir:"
	if !strings.HasPrefix(content, prefix) {
		return ""
	}
	admin := locate(strings.TrimSpace(content[len(prefix):]), path)
	if admin == "" {
		return ""
	}
	b, err = read(filepath.Join(admin, "commondir"))
	if err != nil {
		return admin // no commondir: a submodule's gitdir, its own repository
	}
	c := strings.TrimSpace(string(b))
	if c == "" {
		return admin
	}
	return locate(c, admin)
}

// absAnyNotation reports whether p is absolute in EITHER environment's
// notation: a Windows drive path is not filepath.IsAbs on Linux, nor a /mnt/…
// path on Windows, and joining either onto a base would mangle it.
func absAnyNotation(p string) bool {
	if filepath.IsAbs(p) || strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) {
		return true
	}
	return len(p) >= 2 && p[1] == ':'
}
