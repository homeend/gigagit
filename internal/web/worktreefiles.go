package web

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"sync"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/fuzzy"
)

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/worktree-files", s.handleWorktreeFiles)
	})
}

// F on the web (plan 5d): the files pane lists every file in the working
// tree. The list can be huge (gg's target repos hold 100k+ paths), so the
// page never holds it: this endpoint ranks a query here (the best
// wtFilesPage) or hands out the sorted list a page at a time.

// wtFilesPage is the most rows one answer carries: the ranked cap (the TUI's
// fileFinderLimit) and the page an unfiltered list scrolls by.
const wtFilesPage = 200

type wtFileRow struct {
	Path      string `json:"path"`
	Untracked bool   `json:"untracked,omitempty"`
}

// wtFilesBody is one answer: the rows, the whole list's size, the offset of
// the next unfiltered page (0 = the last), whether a query hit the cap, and
// the read the rows came from — a page whose gen differs from its first
// page's was cut from a newer read, and the page starts over.
type wtFilesBody struct {
	Files   []wtFileRow `json:"files"`
	Total   int         `json:"total"`
	Next    int         `json:"next,omitempty"`
	Limited bool        `json:"limited,omitempty"`
	Gen     int         `json:"gen"`
}

// wtFilesList is one read of the working tree's files, for one checkout.
type wtFilesList struct {
	root      string
	gen       int
	paths     []string
	untracked map[string]bool
}

// The cache sits beside the Server (searchState's pattern): one read per
// Server, replaced by every fresh read or a re-root. Losing it costs a
// re-read, never a wrong answer.
var (
	wtFilesMu  sync.Mutex
	wtFilesOf  = map[*Server]*wtFilesList{}
	wtFilesGen int
)

func (s *Server) handleWorktreeFiles(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	offset := 0
	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeErr(w, http.StatusBadRequest, errors.New("invalid offset"))
			return
		}
		offset = n
	}
	l, err := s.worktreeFiles(readCtx(r), q.Get("fresh") == "1")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	body := wtFilesBody{Files: []wtFileRow{}, Total: len(l.paths), Gen: l.gen}
	if query := q.Get("q"); query != "" {
		for _, m := range fuzzy.Rank(query, l.paths, wtFilesPage) {
			body.Files = append(body.Files, wtFileRow{Path: m.S, Untracked: l.untracked[m.S]})
		}
		body.Limited = len(body.Files) == wtFilesPage
	} else {
		from := min(offset, len(l.paths))
		to := min(from+wtFilesPage, len(l.paths))
		for _, p := range l.paths[from:to] {
			body.Files = append(body.Files, wtFileRow{Path: p, Untracked: l.untracked[p]})
		}
		if to < len(l.paths) {
			body.Next = to
		}
	}
	writeJSON(w, body)
}

// worktreeFiles returns the cached read, or reads the list anew (fresh, no
// read yet, or the served checkout changed): ls-files and the status, the
// TUI's rule (domain.WorktreeFileList).
func (s *Server) worktreeFiles(ctx context.Context, fresh bool) (*wtFilesList, error) {
	svc := s.service()
	root := svc.Root()
	wtFilesMu.Lock()
	l := wtFilesOf[s]
	wtFilesMu.Unlock()
	if !fresh && l != nil && l.root == root {
		return l, nil
	}
	tracked, err := svc.LsFiles(ctx)
	if err != nil {
		return nil, err
	}
	st, err := svc.Status(ctx)
	if err != nil {
		return nil, err
	}
	paths, untracked := domain.WorktreeFileList(tracked, st)
	wtFilesMu.Lock()
	defer wtFilesMu.Unlock()
	wtFilesGen++
	l = &wtFilesList{root: root, gen: wtFilesGen, paths: paths, untracked: untracked}
	wtFilesOf[s] = l
	return l, nil
}
