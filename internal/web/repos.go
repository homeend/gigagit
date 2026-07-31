package web

import (
	"net/http"
	"path"
	"runtime"
	"strings"

	"github.com/homeend/gigagit/internal/repos"
)

// handleRepos lists the machine's MRU registry (previously-opened repos) —
// the allowlist source a re-root picker chooses from. Each row says whether
// it IS the repo being served, because the two sides of that question are
// spelled differently (see sameRepoPath) and only this side can tell.
func (s *Server) handleRepos(w http.ResponseWriter, r *http.Request) {
	served, _ := s.service().TopLevel(r.Context()) // "" on error: nothing is marked current
	entries := repos.Load(s.reposStatePath())
	rows := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		rows = append(rows, map[string]any{
			"path":    e.Path,
			"name":    repos.Name(e),
			"current": served != "" && sameRepoPath(e.Path, served),
		})
	}
	writeJSON(w, map[string]any{"repos": rows})
}

// pathGOOS is runtime.GOOS behind a seam, so the Windows comparison rules can
// be exercised from a Linux test run.
var pathGOOS = runtime.GOOS

// sameRepoPath reports whether two recorded paths name the same repository
// root.
//
// It exists because the two values being compared are normalized
// differently. /api/repo reports git's own `rev-parse --show-toplevel`, which
// uses FORWARD slashes even on Windows (`T:/others/repo`), while the MRU
// registry stores filepath.Clean'd paths, which on Windows use BACKslashes
// (`T:\others\repo`). Plain string equality therefore never matches on
// Windows — which is how the repo being served kept appearing in the
// switch-repo picker, and picking it re-rooted onto the repo already open.
//
// Comparing here rather than in the browser is deliberate: the client cannot
// know which separator or case rules apply to the server's filesystem.
func sameRepoPath(a, b string) bool {
	na, nb := normalizeRepoPath(a), normalizeRepoPath(b)
	if pathGOOS == "windows" {
		// Windows paths are case-insensitive, and the two sources can differ
		// in case (a drive letter, or a directory typed with other casing).
		return strings.EqualFold(na, nb)
	}
	return na == nb
}

// normalizeRepoPath puts a path into one comparable notation: forward
// slashes, cleaned, no trailing separator. It uses path (not filepath) on
// purpose — the comparison must behave the same whichever OS is running it,
// so a test can exercise the Windows rules anywhere.
func normalizeRepoPath(p string) string {
	p = strings.ReplaceAll(p, `\`, "/")
	p = path.Clean(p)
	if len(p) > 1 {
		p = strings.TrimSuffix(p, "/")
	}
	return p
}
