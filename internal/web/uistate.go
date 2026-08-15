package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"

	"github.com/homeend/gigagit/internal/promptstate"
)

// The browser cannot remember its own layout: `gg web` binds a RANDOM loopback
// port, so every session is a different origin and localStorage starts empty.
// The layout therefore lives in gg's machine-local state file, read and written
// through these two endpoints.

// uiSections are the sidebar lists whose folded state is remembered. Wire
// values are resolved against this allowlist, like every other web input.
var uiSections = []string{"branches", "remotes", "worktrees", "tags", "stashes", "reflog", "bookmarks", "shelf"}

// uiMaxPaneWidth bounds a stored pane width. The client clamps against the
// live window anyway; this only keeps a nonsense value out of the state file.
const uiMaxPaneWidth = 4000

type uiStateWire struct {
	// Saved reports whether a layout was ever stored. False means a first
	// run, and the client applies its own defaults (some sections folded)
	// instead of the zero values below.
	Saved         bool     `json:"saved"`
	Sections      []string `json:"sections"`
	SidebarHidden bool     `json:"sidebar_hidden"`
	SidebarWidth  int      `json:"sidebar_width"`
	FilesWidth    int      `json:"files_width"`
	Graph         string   `json:"graph"`
}

// webUIStore is the machine-local state file the layout shares with the
// prompt/notice records. nil when there is no state dir (no home) — the UI
// then simply never remembers, exactly as it behaves today.
func (s *Server) webUIStore() *promptstate.FileStore {
	path := s.reposStatePath()
	if path == "" {
		return nil
	}
	return promptstate.NewFileStore(filepath.Join(filepath.Dir(path), "prompts.toml"))
}

func (s *Server) handleUIStateGet(w http.ResponseWriter, r *http.Request) {
	store := s.webUIStore()
	if store == nil {
		writeJSON(w, uiStateWire{Sections: []string{}})
		return
	}
	st, saved := store.WebUIState()
	writeJSON(w, uiStateWire{
		Saved:         saved,
		Sections:      st.Sections,
		SidebarHidden: st.SidebarHidden,
		SidebarWidth:  st.SidebarWidth,
		FilesWidth:    st.FilesWidth,
		Graph:         st.Graph,
	})
}

func (s *Server) handleUIStateSet(w http.ResponseWriter, r *http.Request) {
	var in uiStateWire
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	store := s.webUIStore()
	if store == nil {
		writeErr(w, http.StatusServiceUnavailable, errors.New("no state directory: layout cannot be remembered"))
		return
	}
	if err := store.SetWebUIState(promptstate.WebUI{
		Sections:      allowedSections(in.Sections),
		SidebarHidden: in.SidebarHidden,
		SidebarWidth:  clampPaneWidth(in.SidebarWidth),
		FilesWidth:    clampPaneWidth(in.FilesWidth),
		Graph:         allowedGraph(in.Graph),
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.handleUIStateGet(w, r) // answer with what was actually stored
}

// allowedSections keeps only known section names, deduped and in the canonical
// order, so a stored layout can never carry junk (or grow unboundedly).
func allowedSections(in []string) []string {
	want := make(map[string]bool, len(in))
	for _, n := range in {
		want[n] = true
	}
	out := make([]string, 0, len(uiSections))
	for _, n := range uiSections {
		if want[n] {
			out = append(out, n)
		}
	}
	return out
}

func allowedGraph(mode string) string {
	if mode == "off" {
		return "off"
	}
	return "svg" // the default render mode; anything unrecognized falls back to it
}

func clampPaneWidth(px int) int {
	if px < 0 {
		return 0 // 0 = never dragged; the client uses its default
	}
	if px > uiMaxPaneWidth {
		return uiMaxPaneWidth
	}
	return px
}
