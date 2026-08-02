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
	// Parents is the parent COUNT, not the ids: it is what the client needs
	// to keep the history-edit rows off a merge (2+) or the root (0), the
	// same gate the TUI's commitEditRow applies.
	Parents int `json:"parents"`
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
func (s *Server) feedFor(r *http.Request) *domain.CommitFeed {
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
		// Re-derive the solo scope on every build. SetScope only records the
		// refspec — the LoadInitial the caller is about to make does the walk,
		// so a rebuild costs nothing extra. This is what makes solo survive
		// resetFeed after a state-changing op.
		f.SetScope(soloScope(s.solo))
		if s.pageInitial > 0 {
			f.SetPageSizes(s.pageInitial, s.pageBatch)
		}
		s.feed = f
	}
	return s.feed
}

func (s *Server) handleCommits(w http.ResponseWriter, r *http.Request) {
	feed := s.feedFor(r)
	var st domain.FeedState
	var err error
	if r.URL.Query().Get("more") == "1" {
		st, _, err = feed.LoadMore(readCtx(r))
	} else {
		st, err = feed.LoadInitial(readCtx(r))
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	// solo rides along on the response whose content it scopes, so a reload
	// or a second tab always learns the active scope without a second call.
	writeJSON(w, map[string]any{
		"rows":          buildRows(st.Commits),
		"can_load_more": !st.Exhausted,
		"solo":          s.soloBranch(),
	})
}

func buildRows(commits []model.Commit) []commitRow {
	cs := make([]commitgraph.Commit, len(commits))
	for i, c := range commits {
		cs[i] = commitgraph.Commit{Hash: c.Hash, Parents: c.Parents}
	}
	graphRows, _ := commitgraph.Lay(cs)
	rows := make([]commitRow, len(commits))
	for i, c := range commits {
		short := c.Hash
		if len(short) > 8 {
			short = short[:8]
		}
		row := commitRow{Hash: c.Hash, Short: short, Subject: c.Subject, Author: c.Author, Time: c.UnixTime, Parents: len(c.Parents)}
		for _, ref := range c.Refs {
			row.Refs = append(row.Refs, refInfo{Name: ref.Name, Kind: refKindString(ref.Kind), Head: ref.Head})
		}
		if i < len(graphRows) {
			row.Cells = graphRows[i].Cells
			row.Lane = graphRows[i].Lane
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
	writeJSON(w, map[string]any{"sha": sha, "files": out})
}
