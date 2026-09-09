package domain

// Git `@@` hunk addressing — the agent-facing hunk number behind
// `gg diff --hunks` and `gg note add --hunk N`. The parser is pure so both the
// CLI and MCP reach it through one Service method and can never disagree with
// what `gg diff` printed.

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/homeend/gigagit/internal/model"
)

// hunkHeaderRe matches a unified-diff hunk header. Anchored at the start of the
// line, so a CONTENT line (which always begins with ' ', '+', '-' or '\') can
// never be mistaken for one — a patch of a patch is a real input.
var hunkHeaderRe = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@ ?(.*)$`)

// EmptyTreeSHA1 is git's well-known hash of the empty tree object. It depends
// only on the empty tree's content, so it is the same value in every SHA-1
// git repository (a SHA-256 repository has a different one, not yet in play
// here). It stands in for "nothing" on the old side of a root commit's own
// diff, since `git diff` has no bare-rev syntax that means that: a lone
// <rev> always means "index/worktree vs rev".
const EmptyTreeSHA1 = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// HunkDiffSpec resolves cached/rev/paths into the DiffSpec `gg diff --hunks`
// numbers, and `--hunk N` resolves against. The three note target states map
// onto it exactly:
//
//	rev == "", cached == false  → index → working tree   (StateUnstaged/Untracked)
//	rev == "", cached == true   → HEAD  → index          (StateStaged)
//	rev == "<commit>"           → parent → commit        (StateCommitted)
//
// A SINGLE commit therefore means that commit's OWN change (parent → commit),
// the pair of texts a commit note anchors to — not `git diff <c>` (working
// tree vs commit), which would number a different patch than the note lands
// in. There is no single-token `git diff` syntax for "a commit against its
// own parent" that also survives a ROOT commit: `<c>^..<c>` fails outright
// (no `<c>^` to resolve), and `<c>^!` — git's "this commit and none of its
// parents" shorthand — silently degrades to plain `<c>` the instant `<c>` has
// no parent (verified against real git: `git rev-parse <root>^!` prints only
// one line, not "<root>" plus an excluded parent, so `git diff` treats it as
// the ordinary bare-rev form and returns index/worktree-vs-<root>, not
// <root>'s own change). So a bare commit is probed for a parent first: with
// one, `<c>^..<c>`; without (a root), EmptyTreeSHA1 stands in for the old
// side. An explicit A..B / A...B range passes through unchanged and is never
// probed.
func (s *Service) HunkDiffSpec(ctx context.Context, cached bool, rev string, paths []string) (model.DiffSpec, error) {
	switch {
	case rev == "":
		return model.DiffSpec{Cached: cached, Paths: paths}, nil
	case strings.Contains(rev, ".."):
		return model.DiffSpec{Rev: rev, Paths: paths}, nil
	default:
		base := rev + "^"
		_, found, err := s.ResolveRev(ctx, base)
		if err != nil {
			return model.DiffSpec{}, err
		}
		if !found {
			base = EmptyTreeSHA1
		}
		return model.DiffSpec{Rev: base + ".." + rev, Paths: paths}, nil
	}
}

// DiffHunks lists each file's `@@` hunks for spec, over the same patch
// DiffPatch prints for it.
func (s *Service) DiffHunks(ctx context.Context, spec model.DiffSpec) ([]model.FileHunks, error) {
	patch, err := s.DiffPatch(ctx, spec)
	if err != nil {
		return nil, err
	}
	return ParseDiffHunks(patch), nil
}

// HunkRange resolves hunk n of path to the side and 1-based inclusive line
// range a note anchored by `--hunk N` covers: the hunk's whole NEW-side span,
// or its old-side span when the hunk only deletes (New == [0,0]).
func (s *Service) HunkRange(ctx context.Context, spec model.DiffSpec, path string, n int) (model.NoteSide, [2]int, error) {
	files, err := s.DiffHunks(ctx, spec)
	if err != nil {
		return "", [2]int{}, err
	}
	want := toGitPath(path)
	for _, f := range files {
		if f.Path != want && f.OldPath != want {
			continue
		}
		if n < 1 || n > len(f.Hunks) {
			return "", [2]int{}, fmt.Errorf("%s has %d hunks", want, len(f.Hunks))
		}
		h := f.Hunks[n-1]
		if h.New == [2]int{0, 0} {
			return model.NoteSideOld, h.Old, nil
		}
		return model.NoteSideNew, h.New, nil
	}
	return "", [2]int{}, fmt.Errorf("%s has no changes in this diff", want)
}

// ParseDiffHunks is the pure `diff --git` / `@@` reader. It never touches git,
// so the CLI, MCP and the review importer all number hunks identically.
func ParseDiffHunks(patch string) []model.FileHunks {
	var out []model.FileHunks
	cur := -1
	for _, line := range strings.Split(patch, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			a, b := gitHeaderPaths(line)
			out = append(out, model.FileHunks{Path: b, OldPath: ""})
			cur = len(out) - 1
			if a != "" && a != b {
				out[cur].OldPath = a
			}
		case cur < 0:
			// Header noise before the first file (e.g. a commit header).
		case strings.HasPrefix(line, "rename to "):
			out[cur].Path = unquoteGitPath(strings.TrimPrefix(line, "rename to "))
		case strings.HasPrefix(line, "rename from "):
			out[cur].OldPath = unquoteGitPath(strings.TrimPrefix(line, "rename from "))
		case strings.HasPrefix(line, "+++ "):
			// The +++ line is authoritative for the new-side path; /dev/null
			// (a deletion) leaves the path taken from `diff --git`.
			if p := stripSidePrefix(strings.TrimPrefix(line, "+++ ")); p != "" {
				out[cur].Path = p
			}
		case strings.HasPrefix(line, "@@"):
			m := hunkHeaderRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			h := model.Hunk{
				N:      len(out[cur].Hunks) + 1,
				Old:    sideRange(m[1], m[2]),
				New:    sideRange(m[3], m[4]),
				Header: strings.TrimRight(m[5], " \t\r"),
			}
			out[cur].Hunks = append(out[cur].Hunks, h)
		}
	}
	return out
}

// sideRange turns a "@@" start plus optional count into a 1-based inclusive
// range. An omitted count is 1 (git's rule); a zero count is [0,0] — that side
// has no lines at all.
func sideRange(start, count string) [2]int {
	s, err := strconv.Atoi(start)
	if err != nil {
		return [2]int{0, 0}
	}
	n := 1
	if count != "" {
		n, err = strconv.Atoi(count)
		if err != nil {
			n = 1
		}
	}
	if n <= 0 {
		return [2]int{0, 0}
	}
	return [2]int{s, s + n - 1}
}

// gitHeaderPaths splits `diff --git a/<old> b/<new>` into its two paths. It is
// a FALLBACK: the +++ / rename lines override it whenever they are present,
// because a path containing " b/" makes this split ambiguous.
func gitHeaderPaths(line string) (oldPath, newPath string) {
	rest := strings.TrimPrefix(line, "diff --git ")
	i := strings.Index(rest, " b/")
	if i < 0 {
		return "", ""
	}
	return stripSidePrefix(rest[:i]), stripSidePrefix(rest[i+1:])
}

// stripSidePrefix drops git's a// b/ side prefix and any trailing timestamp,
// unquotes a C-quoted path, and maps /dev/null to "".
func stripSidePrefix(tok string) string {
	tok = strings.TrimSpace(tok)
	if tok == "/dev/null" || tok == "" {
		return ""
	}
	if i := strings.IndexAny(tok, "\t"); i >= 0 {
		tok = tok[:i]
	}
	tok = unquoteGitPath(tok)
	for _, p := range []string{"a/", "b/"} {
		if strings.HasPrefix(tok, p) {
			return tok[len(p):]
		}
	}
	return tok
}

// unquoteGitPath undoes git's C-style quoting of a path with special bytes.
// A path that does not unquote is used verbatim rather than dropped.
func unquoteGitPath(p string) string {
	p = strings.TrimSpace(p)
	if len(p) < 2 || p[0] != '"' {
		return p
	}
	if unq, err := strconv.Unquote(p); err == nil {
		return unq
	}
	return p
}

// toGitPath normalises a caller-supplied path (either notation) to the git
// slash form stored in a note address and printed in a patch. Never compare a
// raw argv path against a patch path: on Windows the two notations differ.
func toGitPath(p string) string {
	return filepath.ToSlash(filepath.Clean(filepath.FromSlash(p)))
}
