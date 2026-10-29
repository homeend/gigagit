package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"regexp"

	"github.com/homeend/gigagit/internal/steer"
)

// The open-files endpoints (plan 5b): GET the current worktree's list, POST
// one op on it. Every mutation is broadcast as "open_files" on /api/events.

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/open-files", s.handleOpenFilesGet)
		mux.HandleFunc("POST /api/open-files", writeGuard(s.handleOpenFilesPost))
	})
}

var tabRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// validTab reports whether s names a tab (ruling L1).
func validTab(s string) bool { return tabRE.MatchString(s) }

type ofReq struct {
	Op         string `json:"op"`
	ID         string `json:"id"`
	Src        string `json:"src"`
	Rev        string `json:"rev"`
	Path       string `json:"path"`
	Line       int    `json:"line"`
	Tab        string `json:"tab"`
	Everywhere bool   `json:"everywhere"`
	Kind       string `json:"kind"` // op "stored": "review" | "overview"
}

type ofAnswer struct {
	File    *steer.OpenFile `json:"file,omitempty"`
	Evicted string          `json:"evicted,omitempty"`
	Cap     int             `json:"cap,omitempty"` // the list's cap, beside Evicted
	Files   []ofRow         `json:"files"`
}

// capIf is the list's cap when an answer names an eviction (0 = omitted):
// the page words "(<cap> files open)" from it rather than a number its own.
func capIf(evicted bool) int {
	if evicted {
		return maxOpenFiles
	}
	return 0
}

func (s *Server) handleOpenFilesGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"files": s.pageList(s.service().Root())})
}

// ofAbs is where a working-tree path lives on disk.
func (s *Server) ofAbs(wt, path string) string { return filepath.Join(wt, filepath.FromSlash(path)) }

// ofKeyOf validates an open's version: a known src, safe values, and a
// working-tree path that stays inside the tree (the poller stats it).
func ofKeyOf(q ofReq) (ofKey, error) {
	k := ofKey{Src: q.Src, Rev: q.Rev, Path: q.Path}
	if k.Src == "" {
		k.Src = "worktree"
	}
	if !isGitArgSafe(k.Path) || !filepath.IsLocal(filepath.FromSlash(k.Path)) || (k.Rev != "" && !isGitArgSafe(k.Rev)) {
		return k, errors.New("invalid path/rev")
	}
	switch k.Src {
	case "worktree":
		k.Rev = ""
	case "commit", "shelf":
		if k.Rev == "" {
			return k, errors.New("a " + k.Src + " version needs rev")
		}
	case "overview":
		return k, errors.New("an overview opens by id")
	default:
		return k, errors.New("unknown src " + k.Src)
	}
	return k, nil
}

func (s *Server) handleOpenFilesPost(w http.ResponseWriter, r *http.Request) {
	var q ofReq
	if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if q.Tab != "" && !validTab(q.Tab) {
		writeErr(w, http.StatusBadRequest, errors.New("invalid tab"))
		return
	}
	wt := s.service().Root()
	var ans ofAnswer
	ok, broadcast := true, true
	switch q.Op {
	case "open":
		k, err := ofKeyOf(q)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		f, ev := s.ofs.open(wt, k, q.Tab, q.Line)
		s.baseline(wt, f.ID, k)
		ans.File, ans.Evicted, ans.Cap = &f, ev, capIf(ev != "")
	case "stored":
		f, code, err := s.openStoredOverview(readCtx(r), wt, q)
		if err != nil {
			writeErr(w, code, err)
			return
		}
		ans.File = &f
	case "focus":
		var f steer.OpenFile
		if f, ok = s.ofs.focus(wt, q.ID, q.Tab); ok {
			if k, found := s.ofs.entryKey(wt, f.ID); found {
				s.baseline(wt, f.ID, k)
			}
			ans.File = &f
		}
	case "background":
		ok = s.ofs.background(wt, q.ID, q.Tab, q.Line)
	case "close":
		// Close is LOCAL (spec 2026-10-18-gg-session-docs §4.4): the list
		// never touches a document. The server decides, not the page: a note
		// may have landed after the page read the file. A pinned entry — an
		// agent's notes are on it, or it is an overview — backgrounds on esc
		// and on x alike: its document holds it in the list (the follow pass
		// would relist it); the page's delete (session_docs_http.go) is the
		// one way a document goes.
		_, pinned, found := s.ofs.pinnedEntry(wt, q.ID)
		switch {
		case !found:
			ok = false
		case pinned:
			ok = s.ofs.background(wt, q.ID, q.Tab, q.Line)
		default:
			ok = s.ofs.close(wt, q.ID, q.Tab, q.Everywhere)
		}
	case "cursor":
		ok, broadcast = s.ofs.cursor(wt, q.ID, q.Line), false
	case "shown":
		ok = s.ofs.setShown(wt, q.Tab, q.ID)
	default:
		writeErr(w, http.StatusBadRequest, errors.New("unknown op "+q.Op))
		return
	}
	if !ok {
		writeErr(w, http.StatusNotFound, errors.New("no open file "+q.ID))
		return
	}
	if broadcast {
		s.broadcastOpenFiles(wt, ans.Evicted)
	}
	ans.Files = s.pageList(wt)
	writeJSON(w, ans)
}

// baseline stats a working-tree entry BEFORE the page reads its content
// (ruling L5): an edit racing that read shows on the next poll.
func (s *Server) baseline(wt, id string, k ofKey) {
	if k.Src == "worktree" {
		s.ofs.setBaseline(wt, id, statDisk(s.ofAbs(wt, k.Path)))
	}
}

// broadcastOpenFiles sends wt's list to every tab — never dropped by the op
// gate (ruling L6): it answers something a tab or agent just did.
func (s *Server) broadcastOpenFiles(wt, evicted string) { s.broadcastOpened(wt, evicted, "") }

// broadcastOpened is broadcastOpenFiles naming the file an agent's background
// open just added, so every tab can say so.
func (s *Server) broadcastOpened(wt, evicted, opened string) {
	if h := s.liveHubRef(); h != nil {
		h.fanOut(liveMsg{Changed: []string{}, Reason: "open_files", Files: s.pageList(wt), Evicted: evicted, Cap: capIf(evicted != ""), Opened: opened})
	}
}

func (s *Server) broadcastFileChanged(id string) {
	if h := s.liveHubRef(); h != nil {
		h.fanOut(liveMsg{Changed: []string{}, Reason: "file_changed", FileID: id})
	}
}
