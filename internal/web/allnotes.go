package web

import (
	"errors"
	"net/http"
	"path/filepath"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

// The web "View all notes…" (the TUI command palette's popup): every note
// this checkout can see, as the TUI's tree — working tree, commits, other
// (shelf entries). One read of domain.NotesOverview; the page builds the tree.

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/notes/overview", s.handleNotesOverview)
	})
}

// overviewFileWire is one file's threads. State is the wire word the page's
// openers speak ("unstaged"/"staged"/"untracked"/"commit"); Status/OldPath
// are the commit's own change to the path ("" when it has none).
type overviewFileWire struct {
	Path    string            `json:"path"`
	State   string            `json:"state"`
	Status  string            `json:"status,omitempty"`
	OldPath string            `json:"old_path,omitempty"`
	Notes   []domain.WireNote `json:"notes"`
}

type overviewReviewWire struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"` // "commit" | "branch" | "was_tip"
	Branch  string `json:"branch,omitempty"`
	Agent   string `json:"agent"`
	Summary string `json:"summary"`
	Created string `json:"created,omitempty"`
}

type overviewCommitWire struct {
	Hash    string               `json:"hash"`
	Subject string               `json:"subject"`
	Time    int64                `json:"time"`
	Missing bool                 `json:"missing"`
	Files   []overviewFileWire   `json:"files"`
	Reviews []overviewReviewWire `json:"reviews"`
}

type overviewShelfWire struct {
	ID      string             `json:"id"`
	Label   string             `json:"label"`
	Missing bool               `json:"missing"`
	Entry   []domain.WireNote  `json:"entry"` // notes on the entry itself (a recycle's), oldest first
	Files   []overviewFileWire `json:"files"`
}

type notesOverviewWire struct {
	Worktree  string               `json:"worktree"` // the checkout's directory name, for the group label
	Count     int                  `json:"count"`
	Unstaged  []overviewFileWire   `json:"unstaged"`
	Staged    []overviewFileWire   `json:"staged"`
	Untracked []overviewFileWire   `json:"untracked"`
	Commits   []overviewCommitWire `json:"commits"`
	Shelves   []overviewShelfWire  `json:"shelves"`
}

func overviewFiles(fs []domain.NoteFileNotes, state string) []overviewFileWire {
	out := make([]overviewFileWire, 0, len(fs))
	for _, f := range fs {
		w := overviewFileWire{Path: f.Addr.Path, State: state, Status: f.Status, OldPath: f.OldPath,
			Notes: make([]domain.WireNote, 0, len(f.Notes))}
		for _, n := range f.Notes {
			w.Notes = append(w.Notes, domain.ToWireNote(n))
		}
		out = append(out, w)
	}
	return out
}

func overviewReviewKind(k domain.ReviewNoteKind) string {
	switch k {
	case domain.ReviewOnBranch:
		return "branch"
	case domain.ReviewWasTip:
		return "was_tip"
	}
	return "commit"
}

func (s *Server) handleNotesOverview(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	ov, err := svc.NotesOverview(readCtx(r))
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, domain.ErrNotesDisabled) {
			status = http.StatusConflict
		}
		writeErr(w, status, err)
		return
	}
	// What was created on a branch is listed on that branch only: the page
	// says which branch it is on (checked out, or the one its commit list is
	// narrowed to). The names are only compared — they never reach git. None
	// given (a detached HEAD) lists everything.
	ov = ov.ShownOn(r.URL.Query()["on"])
	out := notesOverviewWire{
		Count:     ov.Count(),
		Unstaged:  overviewFiles(ov.Unstaged, "unstaged"),
		Staged:    overviewFiles(ov.Staged, "staged"),
		Untracked: overviewFiles(ov.Untracked, "untracked"),
		Commits:   make([]overviewCommitWire, 0, len(ov.Commits)),
		Shelves:   make([]overviewShelfWire, 0, len(ov.Shelves)),
	}
	if top, err := svc.TopLevel(readCtx(r)); err == nil && top != "" {
		out.Worktree = filepath.Base(top)
	}
	for _, c := range ov.Commits {
		cw := overviewCommitWire{Hash: c.Hash, Subject: c.Subject, Time: c.UnixTime, Missing: c.Missing,
			Files: overviewFiles(c.Files, "commit"), Reviews: make([]overviewReviewWire, 0, len(c.Reviews))}
		for _, v := range c.Reviews {
			rw := overviewReviewWire{ID: v.ID, Kind: overviewReviewKind(v.Kind), Branch: v.Branch, Agent: v.Agent, Summary: v.Summary}
			if !v.Created.IsZero() {
				rw.Created = v.Created.UTC().Format(time.RFC3339)
			}
			cw.Reviews = append(cw.Reviews, rw)
		}
		out.Commits = append(out.Commits, cw)
	}
	for _, sh := range ov.Shelves {
		sw := overviewShelfWire{ID: sh.ID, Label: sh.Label, Missing: sh.Missing,
			Entry: make([]domain.WireNote, 0, len(sh.Entry)), Files: overviewFiles(sh.Files, "shelf")}
		for _, n := range sh.Entry {
			sw.Entry = append(sw.Entry, domain.ToWireNote(n))
		}
		out.Shelves = append(out.Shelves, sw)
	}
	writeJSON(w, out)
}
