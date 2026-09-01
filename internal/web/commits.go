package web

import (
	"errors"
	"net/http"

	"github.com/homeend/gigagit/internal/commitgraph"
	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

type commitRow struct {
	Hash    string    `json:"hash"`
	Short   string    `json:"short"`
	Subject string    `json:"subject"`
	Author  string    `json:"author"`
	Time    int64     `json:"time"`
	Refs    []refInfo `json:"refs,omitempty"`
	Cells   string    `json:"cells"`
	Lane    int       `json:"lane"`
	// Parents is the parent COUNT: what the client needs to keep the
	// history-edit rows off a merge (2+) or the root (0), the same gate the
	// TUI's commitEditRow applies.
	Parents int `json:"parents"`
	// ParentIDs are those parents' hashes. The fast-forward row is offered
	// only when the selected commit is a DESCENDANT of the current branch tip,
	// and the TUI decides that by walking parent links across the loaded feed
	// (feedDescendant). The browser cannot walk what it cannot see.
	ParentIDs []string `json:"parent_ids,omitempty"`
	// Seg is the commit's development-line segment in a SOLOED feed (nil
	// otherwise): the dot color index, changing below another branch's fork
	// point or tip and on merged-in lines, so the soloed branch's own commits
	// stand out from inherited history even when the graph is one lane.
	Seg *int `json:"seg,omitempty"`
}

type refInfo struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	Head bool   `json:"head,omitempty"`
}

// feedFor lazily builds the single server-side feed, applying the repo's
// commit-sort config the way the TUI does at load. The probe reads the
// ACTIVE per-repo config file (the machine-local private file when one
// exists, else the committed .gg.toml — the same resolution effectiveConfig
// and handleUIConfig use), so a private-config repo's commit_sort takes
// effect here too.
//
// The scope a fresh feed starts under is the solo selection PLUS the filter
// this request carries: a rebuild (an op dropped the feed, the sort changed,
// the repo was re-rooted) must not quietly widen a narrowed list.
func (s *Server) feedFor(r *http.Request, filter feedFilter) *domain.CommitFeed {
	svc := s.service()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.feed == nil {
		f := svc.CommitFeed()
		mode := "date-order"
		if active, err := s.activeRepoConfigPath(readCtx(r), svc); err == nil {
			if cfg, err := config.Load(config.DefaultGlobalPath(), active); err == nil {
				mode = cfg.UI.CommitSort
			}
		}
		f.SetSortMode(mode)
		// Re-derive the scope on every build. SetScope only records the
		// refspec — the walk the caller is about to make does the work, so a
		// rebuild costs nothing extra. This is what makes solo survive
		// resetFeed after a state-changing op.
		scope := scopeFor(s.solo, filter)
		f.SetScope(scope)
		// A fresh feed walks the scope it was just given; recording that is
		// load-bearing, since a stale signature would make the next filtered
		// request look already-applied and answer with unfiltered rows.
		s.scopeApplied(scope)
		if s.pageInitial > 0 {
			f.SetPageSizes(s.pageInitial, s.pageBatch)
		}
		s.feed = f
	}
	return s.feed
}

// feedScope is the scope /api/commits should walk: the stored solo selection
// plus this request's filter.
func (s *Server) feedScope(filter feedFilter) domain.LogScope {
	s.mu.Lock()
	defer s.mu.Unlock()
	return scopeFor(s.solo, filter)
}

func (s *Server) handleCommits(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter, ferr := parseFeedFilter(q)
	if ferr != nil {
		writeErr(w, http.StatusBadRequest, ferr)
		return
	}
	if q.Get("reset") == "1" {
		// A MANUAL refresh starts the list clean, exactly as the TUI's `r`
		// does (reloadOpts{hardFeed: true}). That is its point: reconciling
		// keeps the pages already scrolled in, which is right after an op and
		// wrong when the deep tail has gone stale — someone rewrote history,
		// and the only way back is to walk it again from the top.
		s.resetFeed()
	}
	feed := s.feedFor(r, filter)
	scope := s.feedScope(filter)
	var st domain.FeedState
	var err error
	switch {
	case s.scopeNeedsApply(scope):
		// The filter changed under a live feed (or a second tab is asking for
		// a different one): re-walk page 0 under the new scope. ApplyScope
		// stashes the accumulation it leaves behind, so CLEARING a filter
		// restores every page that was already walked with no git call at all
		// — which is what makes a filter cheap to try.
		st, err = feed.ApplyScope(readCtx(r), scope)
		s.scopeApplied(scope)
	case q.Get("more") == "1":
		st, _, err = feed.LoadMore(readCtx(r))
	default:
		// A plain reload RECONCILES: it walks the same single page 0 and merges
		// it into whatever this feed already holds, so the pages the browser
		// scrolled in survive an op or an F5. New commits prepend, a vanished
		// tip is trimmed, and a rewrite that can't be aligned degrades to the
		// hard reset LoadInitial always did. A freshly built feed (solo change,
		// sort change, re-root, the reset above — each drops s.feed) holds
		// nothing, so this degenerates to the plain page-0 walk there.
		st, err = feed.Refresh(readCtx(r))
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	// The scope rides along on the response whose content it scopes, so a
	// reload or a second tab always learns it without a second call.
	soloKind, soloRef := s.soloRef()
	// Territory segments ride only a soloed, unfiltered feed: a filtered
	// subset has no honest first-parent chain to walk, and an unsoloed feed
	// already tells branches apart by lane.
	var segs []int
	if soloRef != "" && !filter.active() {
		segs = s.soloSegments(r, soloRef, st.Commits)
	}
	writeJSON(w, map[string]any{
		// A filtered feed is a non-contiguous SUBSET of history, so its lanes
		// would connect commits that are not parent and child. The TUI drops
		// the graph for exactly that reason; here it also drops the cost,
		// which on a big repo is the larger half of the answer.
		"rows":          buildRows(st.Commits, !filter.active(), segs),
		"can_load_more": !st.Exhausted,
		"solo":          soloRef,
		"solo_kind":     soloKind,
		"filtered":      filter.active(),
	})
}

// soloSegments assigns each commit of a soloed feed its territory segment
// (commitgraph.SegmentLayer over the whole page set — the web feed is
// re-rendered in full per response, so no incremental state is kept). The
// boundary rule is the TUI's: a merge-base fork point against any other
// local branch (domain.ScopeBoundaries — the only marker left once that
// branch moved on past the fork), another local branch's tip, or a remote tip
// that is not the soloed branch's own upstream. Best-effort: a failed branch
// or merge-base read simply yields decoration-only boundaries.
func (s *Server) soloSegments(r *http.Request, ref string, commits []model.Commit) []int {
	svc := s.service()
	ctx := readCtx(r)
	ownUpstream := ""
	var others []string
	if branches, err := svc.Branches(ctx); err == nil {
		for _, b := range branches {
			if b.Name == ref {
				ownUpstream = b.Upstream
			} else {
				others = append(others, b.Name)
			}
		}
	}
	forks := map[string]bool{}
	if len(others) > 0 {
		if hs, err := svc.ScopeBoundaries(ctx, []string{ref}, others); err == nil {
			for _, h := range hs {
				forks[h] = true
			}
		}
	}
	boundary := func(i int) bool {
		c := commits[i]
		if forks[c.Hash] {
			return true
		}
		for _, rf := range c.Refs {
			switch rf.Kind {
			case model.RefLocal:
				if rf.Name != ref {
					return true
				}
			case model.RefRemote:
				if rf.Name != ownUpstream {
					return true
				}
			}
		}
		return false
	}
	cs := make([]commitgraph.Commit, len(commits))
	for i, c := range commits {
		cs[i] = commitgraph.Commit{Hash: c.Hash, Parents: c.Parents}
	}
	return (&commitgraph.SegmentLayer{}).Append(cs, boundary)
}

// buildRows renders the feed page for the wire. lanes draws the commit graph;
// callers pass false when the page is a filtered subset (see handleCommits).
// segs, when parallel to commits, sets each row's territory segment.
func buildRows(commits []model.Commit, lanes bool, segs []int) []commitRow {
	var graphRows []commitgraph.Row
	if lanes {
		cs := make([]commitgraph.Commit, len(commits))
		for i, c := range commits {
			cs[i] = commitgraph.Commit{Hash: c.Hash, Parents: c.Parents}
		}
		graphRows, _ = commitgraph.Lay(cs)
	}
	rows := make([]commitRow, len(commits))
	for i, c := range commits {
		short := c.Hash
		if len(short) > 8 {
			short = short[:8]
		}
		row := commitRow{Hash: c.Hash, Short: short, Subject: c.Subject, Author: c.Author, Time: c.UnixTime, Parents: len(c.Parents), ParentIDs: c.Parents}
		for _, ref := range c.Refs {
			row.Refs = append(row.Refs, refInfo{Name: ref.Name, Kind: refKindString(ref.Kind), Head: ref.Head})
		}
		if i < len(graphRows) {
			row.Cells = graphRows[i].Cells
			row.Lane = graphRows[i].Lane
		}
		if len(segs) == len(commits) {
			seg := segs[i]
			row.Seg = &seg
		}
		rows[i] = row
	}
	return rows
}

func refKindString(k model.RefKind) string {
	switch k {
	case model.RefLocal:
		return "local"
	case model.RefRemote:
		return "remote"
	case model.RefTag:
		return "tag"
	case model.RefHead:
		return "head"
	}
	return "other"
}

func (s *Server) handleCommitFiles(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	sha := r.PathValue("sha")
	if !isGitArgSafe(sha) {
		writeErr(w, http.StatusBadRequest, errors.New("invalid sha"))
		return
	}
	files, err := svc.CommitFiles(r.Context(), sha)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	type fileInfo struct {
		Path    string `json:"path"`
		Status  string `json:"status"`
		OldPath string `json:"old_path,omitempty"`
	}
	out := make([]fileInfo, len(files))
	for i, f := range files {
		out[i] = fileInfo{Path: f.Path, Status: f.Status, OldPath: f.OldPath}
	}
	// The file-list stage draws the commit's date under its title. The feed row
	// carries a time already, but the by-hash open (a sidebar tag) has no row —
	// serving it here gives both paths one source, and one that agrees with the
	// feed by construction (CommitMeta reads %at, the same field the walk does).
	// Best-effort: an unresolvable date leaves the fields zero and the browser
	// simply draws no line. The files are the payload; the date is decoration.
	body := map[string]any{"sha": sha, "files": out}
	if meta, merr := svc.CommitMeta(r.Context(), sha); merr == nil {
		body["time"] = meta.UnixTime
		body["author"] = meta.Author
	}
	writeJSON(w, body)
}

// handleCommitMessage serves one commit's FULL message (subject + body). The
// reword prompt prefills with it — retyping a message to change one word is
// how bodies get lost. Hex-only: a commit id is content-addressed, so unlike
// a rev expression there is nothing to resolve or mis-resolve.
func (s *Server) handleCommitMessage(w http.ResponseWriter, r *http.Request) {
	rev := r.URL.Query().Get("rev")
	if !isHexSha(rev) {
		writeErr(w, http.StatusBadRequest, errors.New("invalid commit"))
		return
	}
	msg, err := s.service().CommitMessage(readCtx(r), rev)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]any{"message": msg})
}
