package workspace

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/homeend/gigagit/internal/model"
)

// NormalizeEntry is a workspace as it opens: every listed path resolved
// to its repository's main checkout (a hand-edited file may name a linked
// worktree), one member per repository (by key, then by real path), the
// cap. Every membership question — the launch dialog, --workspace, the
// popup's marks, openWorkspace — asks it of THIS shape, so a repository the
// cap cuts is no member anywhere rather than a member that re-roots away.
func NormalizeEntry(e Entry) Entry {
	return DedupeRealPaths(ResolveMemberPaths(e)).Normalized()
}

// NormalizeEntries is NormalizeEntry over a registry.
func NormalizeEntries(ws []Entry) []Entry {
	out := make([]Entry, len(ws))
	for i, e := range ws {
		out[i] = NormalizeEntry(e)
	}
	return out
}

// ResolveMemberPaths replaces a symlink among e's paths with the real path (git
// reports real paths, so a symlink spelling would be no member anywhere) and a
// linked worktree with its repository's main checkout: one `.git` file read, no
// git (a submodule's or an unreadable one is kept as written).
func ResolveMemberPaths(e Entry) Entry {
	out := Entry{Name: e.Name, SettingsRepo: e.SettingsRepo, Repos: make([]string, 0, len(e.Repos))}
	for _, p := range e.Repos {
		if real, err := filepath.EvalSymlinks(p); err == nil && real != p {
			if model.SamePath(p, out.SettingsRepo) {
				out.SettingsRepo = real
			}
			p = real
		}
		if common := LinkedWorktreeCommonDir(p); common != "" {
			if main := MainPathOfCommonDir(common); main != "" {
				if model.SamePath(p, out.SettingsRepo) {
					out.SettingsRepo = main
				}
				p = main
			}
		}
		out.Repos = append(out.Repos, p)
	}
	return out
}

// DedupeRealPaths drops members that are another member's path through a
// symlink (a hand edit; the picker resolves them): the first spelling
// stays. One Lstat chain per member, no git.
func DedupeRealPaths(e Entry) Entry {
	out := Entry{Name: e.Name, SettingsRepo: e.SettingsRepo}
	seen := map[model.CheckoutKey]bool{}
	for _, p := range e.Repos {
		real := p
		if r, err := filepath.EvalSymlinks(p); err == nil {
			real = r
		}
		k := model.KeyOf(real)
		if seen[k] {
			continue
		}
		seen[k] = true
		out.Repos = append(out.Repos, p)
	}
	if out.SettingsRepo != "" && !out.Has(out.SettingsRepo) {
		out.SettingsRepo = ""
	}
	return out
}

// LinkedWorktreeCommonDir is the git common dir a linked worktree's `.git`
// file points into (<common>/worktrees/<name>), "" for anything else.
func LinkedWorktreeCommonDir(path string) string {
	data, err := os.ReadFile(filepath.Join(path, ".git"))
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(data))
	const prefix = "gitdir:"
	if !strings.HasPrefix(line, prefix) {
		return ""
	}
	gitdir := strings.TrimSpace(strings.TrimPrefix(line, prefix))
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(path, gitdir)
	}
	gitdir = filepath.Clean(gitdir)
	wts := filepath.Dir(gitdir) // <common>/worktrees
	if filepath.Base(wts) != "worktrees" {
		return ""
	}
	return filepath.Dir(wts)
}

// MainPathOfCommonDir is the member identity behind a common git dir: its
// parent for a ".git" directory, the directory itself for a bare one
// (domain.RepoMainPath's rule, without a git call).
func MainPathOfCommonDir(common string) string {
	common = filepath.Clean(common)
	if strings.EqualFold(filepath.Base(common), ".git") {
		return filepath.Dir(common)
	}
	if BareGitDir(common) {
		return common
	}
	return "" // a submodule's git dir (under the superproject's .git/modules): the identity is its checkout — the worktree list's fallback says which
}

// GitDirLayout reports a directory shaped like a git dir (HEAD beside
// objects/, no .git entry): a bare repository or a submodule's git dir.
func GitDirLayout(path string) bool {
	if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
		return false
	}
	_, headErr := os.Stat(filepath.Join(path, "HEAD"))
	st, objErr := os.Stat(filepath.Join(path, "objects"))
	return headErr == nil && objErr == nil && st.IsDir()
}

// BareGitDir reports a git dir whose config says bare = true (one file
// read, no git).
func BareGitDir(common string) bool {
	data, err := os.ReadFile(filepath.Join(common, "config"))
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok && strings.TrimSpace(k) == "bare" && strings.TrimSpace(v) == "true" {
			return true
		}
	}
	return false
}

// HeadingLabels is each member's heading: its name, with the parent directory
// added when another member has the same name. names and paths are 1:1.
func HeadingLabels(names, paths []string) []string {
	out := make([]string, len(names))
	for i, name := range names {
		out[i] = name
		for j, o := range names {
			if j != i && o == name {
				out[i] = name + " (" + filepath.Base(filepath.Dir(filepath.Clean(paths[i]))) + ")"
				break
			}
		}
	}
	return out
}
