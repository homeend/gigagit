package cli

import (
	"path/filepath"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
)

// repoPathspecs rebases pathspecs a user typed in dir onto the worktree root.
//
// gg runs every git invocation at the worktree top level (domain.resolveRoot),
// so that the worktree-root-relative paths gg prints everywhere — git status
// --porcelain reports them that way regardless of cwd — are the paths gg
// accepts back. A pathspec the user types in a shell, though, is relative to
// THEIR cwd, exactly as it is for git itself: `gg add .` in src/ must stage
// src/, not the whole repo. Translating here keeps both true.
//
// A pathspec that resolves outside the worktree, one that cannot be made
// relative, and git's pathspec magic (":/...", ":(exclude)...") pass through
// untouched: git then reports its own error rather than gg inventing one.
func repoPathspecs(svc *domain.Service, dir string, specs []string) []string {
	root := svc.Root()
	if root == "" || len(specs) == 0 {
		return specs
	}
	out := make([]string, len(specs))
	for i, s := range specs {
		out[i] = repoPathspec(root, dir, s)
	}
	return out
}

func repoPathspec(root, dir, spec string) string {
	if spec == "" || strings.HasPrefix(spec, ":") {
		return spec
	}
	abs := spec
	if !filepath.IsAbs(abs) {
		d, err := filepath.Abs(dir)
		if err != nil {
			return spec
		}
		abs = filepath.Join(d, spec)
	}
	// git realpaths --show-toplevel; the shell's cwd may reach the same
	// directory through a symlink, and an unresolved prefix mismatch would
	// silently turn every pathspec into a "../.." pass-through.
	rel, err := filepath.Rel(evalSymlinks(root), evalSymlinks(abs))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return spec
	}
	if rel == "." {
		// The root itself: "" is not a pathspec, "." at the root is.
		return "."
	}
	return filepath.ToSlash(rel)
}

// evalSymlinks resolves p, falling back to p itself when it cannot be
// resolved — a pathspec naming a deleted or not-yet-created file is normal.
func evalSymlinks(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	// A missing leaf still wants its existing parent resolved, so the root
	// and the pathspec share a prefix.
	dir, base := filepath.Split(p)
	if dir == "" {
		return p
	}
	if r, err := filepath.EvalSymlinks(filepath.Clean(dir)); err == nil {
		return filepath.Join(r, base)
	}
	return p
}
