package domain

import (
	"context"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/repos"
)

var (
	// ErrLinkAmbiguous means several checkouts on this machine answer to the
	// link and nothing distinguishes them. The resolver never guesses: two
	// worktrees of one repo hold DIFFERENT working-tree files, so a wrong
	// guess would silently point at the wrong content.
	ErrLinkAmbiguous = errors.New("gg link is ambiguous")
	// ErrLinkUnknownRepo means no checkout on this machine answers to the
	// link. The registry is the only source; gg never scans the filesystem.
	ErrLinkUnknownRepo = errors.New("gg link names an unknown repository")
)

// Resolved is a link turned into a place on THIS machine.
type Resolved struct {
	Checkout string            // absolute top level of the chosen checkout
	Addr     model.FileAddress // Worktree = Checkout for the working-tree states
	Line     int
	Side     model.NoteSide
	Hunk     int
	Commit   string // the FULL sha when the link named a commit
}

// ResolveOpts carries everything the resolver may not reach for itself, so a
// parallel test can supply all of it.
type ResolveOpts struct {
	// RegistryPath is the repo-switcher registry. "" (or a missing file)
	// simply means "no registry candidates" — never an error.
	RegistryPath string
	// Cwd is the service for the directory the caller ran in, or nil. When it
	// matches the link's identity it wins outright.
	Cwd *Service
	// LiveFn reports whether a gg session is live for a checkout. nil = none
	// is. The CLI supplies the real steer-presence probe; domain must not
	// import internal/steer.
	LiveFn func(commonDir, checkout string) bool
	// OpenFn opens a service for another checkout. nil = domain.Open.
	OpenFn func(dir string) *Service
}

// linkCandidate is one checkout that answers to the link, plus the file path
// within it (a local link's path is only known after the split).
type linkCandidate struct {
	checkout string
	relPath  string
	isCwd    bool
}

// ResolveLink turns a link into a checkout on this machine.
//
// Candidate order (spec §2): the cwd's repo when its identity matches, then
// every registry entry (MRU order) whose identity matches. Several matches
// are narrowed by containment of the target commit, then by a live gg
// session; a WORKING-TREE link that still has more than one candidate is
// refused (ErrLinkAmbiguous) rather than guessed, because two checkouts hold
// different working files. A COMMIT link takes the most recently opened
// container — every container holds byte-identical content, so there is
// nothing to get wrong.
func ResolveLink(ctx context.Context, l model.Link, opts ResolveOpts) (Resolved, error) {
	if opts.OpenFn == nil {
		opts.OpenFn = Open
	}
	cands := linkCandidates(ctx, l, opts)
	if len(cands) == 0 {
		return Resolved{}, fmt.Errorf("%w: %s is not in this machine's gg history; open it once in gg", ErrLinkUnknownRepo, linkRepoLabel(l))
	}
	// The cwd is never ambiguous: the caller ran the command THERE, which is
	// the strongest statement of intent available. A commit link still goes
	// through containment below — the cwd may not hold that commit — and the
	// cwd sorts first, so it wins any tie the filters leave.
	if cands[0].isCwd && l.Target.State != model.StateCommitted {
		return finishLink(ctx, l, cands[0], opts)
	}
	if len(cands) > 1 && l.Target.State == model.StateCommitted {
		if kept := containing(ctx, cands, l.Target.Commit, opts); len(kept) > 0 {
			cands = kept
		}
	}
	if len(cands) > 1 && opts.LiveFn != nil {
		var live []linkCandidate
		for _, c := range cands {
			cd := ""
			if s := opts.OpenFn(c.checkout); s != nil {
				cd, _ = s.GitCommonDir(ctx)
			}
			if opts.LiveFn(cd, c.checkout) {
				live = append(live, c)
			}
		}
		// Narrow to the live subset whenever ANY candidate is live — even
		// when more than one is (e.g. two live gg sessions on shared
		// clones); MRU / commit-containment order then breaks any remaining
		// tie. Only an EMPTY live set leaves the candidates untouched.
		if len(live) > 0 {
			cands = live
		}
	}
	if len(cands) > 1 && l.Target.State != model.StateCommitted {
		var paths []string
		for _, c := range cands {
			paths = append(paths, c.checkout)
		}
		return Resolved{}, fmt.Errorf("%w: %s matches %s; run the command inside the one you mean", ErrLinkAmbiguous, linkRepoLabel(l), strings.Join(paths, ", "))
	}
	return finishLink(ctx, l, cands[0], opts)
}

// linkCandidates lists every checkout whose identity matches the link, cwd
// first, then the registry in MRU order, deduplicated by path.
func linkCandidates(ctx context.Context, l model.Link, opts ResolveOpts) []linkCandidate {
	var out []linkCandidate
	seen := map[string]bool{}
	add := func(c linkCandidate) {
		key := linkPathKey(c.checkout)
		if c.checkout == "" || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, c)
	}
	if opts.Cwd != nil {
		if top, err := opts.Cwd.TopLevel(ctx); err == nil && top != "" {
			if l.IsLocal() {
				if rel, ok := linkSplit(l.Repo.Abs, top); ok {
					add(linkCandidate{checkout: top, relPath: rel, isCwd: true})
				}
			} else if name, err := opts.Cwd.RepoName(ctx); err == nil && linkNameEq(name, l.Repo.Name) {
				add(linkCandidate{checkout: top, relPath: l.Path, isCwd: true})
			}
		}
	}
	entries := repos.Load(opts.RegistryPath)
	if l.IsLocal() {
		// Longest path-boundary prefix wins: a worktree nested inside another
		// checkout must not be shadowed by its parent.
		best, bestRel, bestLen := "", "", -1
		for _, e := range entries {
			if rel, ok := linkSplit(l.Repo.Abs, e.Path); ok && len(e.Path) > bestLen {
				best, bestRel, bestLen = e.Path, rel, len(e.Path)
			}
		}
		if best != "" {
			add(linkCandidate{checkout: best, relPath: bestRel})
			return out
		}
		// Moved-checkout fallback: the recorded location is gone, so match by
		// directory NAME against the link's own path segments (last occurrence
		// first, so a repo called "src" inside ".../src/src" resolves deepest).
		for _, e := range entries {
			if rel, ok := linkMovedSplit(l.Repo.Abs, filepath.Base(e.Path)); ok {
				add(linkCandidate{checkout: e.Path, relPath: rel})
			}
		}
		return out
	}
	for _, e := range entries {
		name := e.Remote
		if name == "" {
			// Lazy backfill for an entry an older gg wrote. SetRemote does not
			// bump LastOpened: resolving is not opening. Only a NON-EMPTY
			// computed name is worth writing back — SetRemote has no empty
			// guard of its own, and an empty write would just erase nothing
			// useful while still touching the file.
			if s := opts.OpenFn(e.Path); s != nil {
				if n, err := s.RepoName(ctx); err == nil && n != "" {
					name = n
					_ = repos.SetRemote(opts.RegistryPath, e.Path, n)
				}
			}
		}
		if linkNameEq(name, l.Repo.Name) {
			add(linkCandidate{checkout: e.Path, relPath: l.Path})
		}
	}
	return out
}

// containing keeps the candidates whose object database holds sha.
func containing(ctx context.Context, cands []linkCandidate, sha string, opts ResolveOpts) []linkCandidate {
	var kept []linkCandidate
	for _, c := range cands {
		s := opts.OpenFn(c.checkout)
		if s == nil {
			continue
		}
		if _, found, err := s.ResolveRev(ctx, sha); err == nil && found {
			kept = append(kept, c)
		}
	}
	return kept
}

// finishLink builds the Resolved value for the chosen candidate: the address,
// the worktree pin for the live states, and the FULL sha for a commit link.
func finishLink(ctx context.Context, l model.Link, c linkCandidate, opts ResolveOpts) (Resolved, error) {
	r := Resolved{
		Checkout: c.checkout,
		Addr:     l.Address(),
		Line:     l.Line,
		Side:     l.Side,
		Hunk:     l.Hunk,
	}
	if r.Side == "" {
		r.Side = model.NoteSideNew
	}
	r.Addr.Path = c.relPath
	switch l.Target.State {
	case model.StateCommitted:
		// ResolveRev peels to ^{commit} and is also the >=7-hex → full-sha
		// expansion: no second verb exists for that.
		full, found, err := opts.OpenFn(c.checkout).ResolveRev(ctx, l.Target.Commit)
		if err != nil {
			return Resolved{}, err
		}
		if !found {
			return Resolved{}, fmt.Errorf("%w: %s does not contain commit %s", ErrLinkUnknownRepo, c.checkout, l.Target.Commit)
		}
		full = strings.TrimSpace(full)
		r.Commit, r.Addr.Commit = full, full
	default:
		r.Addr.Worktree = c.checkout
	}
	return r, nil
}

// linkSplit reports whether abs is checkout itself or sits underneath it, and
// returns the git slash path of the remainder ("" when abs IS the checkout).
func linkSplit(abs, checkout string) (string, bool) {
	a := filepath.ToSlash(filepath.Clean(abs))
	c := filepath.ToSlash(filepath.Clean(checkout))
	if linkPathKey(a) == linkPathKey(c) {
		return "", true
	}
	pre := c
	if !strings.HasSuffix(pre, "/") {
		pre += "/"
	}
	if !strings.HasPrefix(linkPathKey(a), linkPathKey(pre)) {
		return "", false
	}
	return a[len(pre):], true
}

// linkMovedSplit finds base as a directory segment of abs (the LAST match
// wins) and returns everything after it. It is the moved-checkout fallback:
// the recorded path is gone, so only the checkout's own directory name is
// left to match on.
func linkMovedSplit(abs, base string) (string, bool) {
	segs := strings.Split(strings.Trim(filepath.ToSlash(abs), "/"), "/")
	for i := len(segs) - 1; i >= 0; i-- {
		if linkPathKey(segs[i]) == linkPathKey(base) {
			if i == len(segs)-1 {
				return "", true
			}
			return path.Join(segs[i+1:]...), true
		}
	}
	return "", false
}

// linkNameEq compares two repository names. Case matters everywhere except
// Windows, where the filesystem does not distinguish them (spec §1).
func linkNameEq(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// linkPathKey normalises a path for comparison: slash form, and case-folded on
// the platforms whose filesystems are case-insensitive.
func linkPathKey(p string) string {
	s := filepath.ToSlash(p)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.ToLower(s)
	}
	return s
}

// samePathLink reports whether two absolute paths name the same place.
func samePathLink(a, b string) bool {
	return linkPathKey(filepath.Clean(a)) == linkPathKey(filepath.Clean(b))
}

// SamePath reports whether two absolute paths name the same place on this
// machine (filepath.Clean + slash-form + case-folded where the filesystem is
// case-insensitive). Exported so Task 6's CLI can reuse the exact rule this
// resolver uses instead of duplicating the normalisation.
func SamePath(a, b string) bool { return samePathLink(a, b) }

// linkRepoLabel names the link's repository half for an error message.
func linkRepoLabel(l model.Link) string {
	if l.IsLocal() {
		return l.Repo.Abs
	}
	return l.Repo.Name
}
