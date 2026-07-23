package web

import (
	"errors"
	"net/http"
	"path/filepath"

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
}

type refInfo struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	Head bool   `json:"head,omitempty"`
}

// feedFor lazily builds the single server-side feed, applying the repo's
// commit-sort config the way the TUI does at load. The probe reads the
// committed .gg.toml only (the machine-local private repo config file is
// not consulted).
func (s *Server) feedFor(r *http.Request) *domain.CommitFeed {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.feed == nil {
		f := s.svc.CommitFeed()
		mode := "date-order"
		if top, err := s.svc.TopLevel(r.Context()); err == nil {
			if cfg, err := config.Load(config.DefaultGlobalPath(), filepath.Join(top, ".gg.toml")); err == nil {
				mode = cfg.UI.CommitSort
			}
		}
		f.SetSortMode(mode)
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
		st, _, err = feed.LoadMore(r.Context())
	} else {
		st, err = feed.LoadInitial(r.Context())
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]any{
		"rows":          buildRows(st.Commits),
		"can_load_more": !st.Exhausted,
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
		row := commitRow{Hash: c.Hash, Short: short, Subject: c.Subject, Author: c.Author, Time: c.UnixTime}
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
	sha := r.PathValue("sha")
	if !isGitArgSafe(sha) {
		writeErr(w, http.StatusBadRequest, errors.New("invalid sha"))
		return
	}
	files, err := s.svc.CommitFiles(r.Context(), sha)
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
