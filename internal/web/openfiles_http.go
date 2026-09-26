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
}

type ofAnswer struct {
	File    *steer.OpenFile  `json:"file,omitempty"`
	Evicted string           `json:"evicted,omitempty"`
	Files   []steer.OpenFile `json:"files"`
}

func (s *Server) handleOpenFilesGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"files": s.ofs.list(s.service().Root())})
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
		ans.File, ans.Evicted = &f, ev
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
		ok = s.ofs.close(wt, q.ID, q.Tab, q.Everywhere)
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
	ans.Files = s.ofs.list(wt)
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
		h.fanOut(liveMsg{Changed: []string{}, Reason: "open_files", Files: s.ofs.list(wt), Evicted: evicted, Opened: opened})
	}
}

func (s *Server) broadcastFileChanged(id string) {
	if h := s.liveHubRef(); h != nil {
		h.fanOut(liveMsg{Changed: []string{}, Reason: "file_changed", FileID: id})
	}
}
