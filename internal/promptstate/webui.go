package promptstate

// WebUI is the browser frontend's remembered layout. It lives here, in the
// machine-local state file, rather than in the browser: `gg web` binds a
// RANDOM loopback port, so every session is a different origin and anything
// kept in localStorage is unreachable the next time gg starts.
//
// It is deliberately global (not per repo): which sections you keep folded and
// how wide you drag the sidebar is a habit, not a property of a repository.
type WebUI struct {
	// Sections are the sidebar sections folded shut (by list name).
	Sections []string `toml:"sections"`
	// SidebarHidden is the whole-sidebar toggle (the b key).
	SidebarHidden bool `toml:"sidebar_hidden"`
	// SidebarWidth / FilesWidth are the dragged pane widths in CSS pixels;
	// 0 means "never dragged, use the default".
	SidebarWidth int `toml:"sidebar_width"`
	FilesWidth   int `toml:"files_width"`
	// Graph is the commit-graph render mode ("svg" or "off").
	Graph string `toml:"graph"`
	// Sorts is each list's display order (list name -> sort mode), the web
	// twin of the TUI's per-panel `o` cycle. Lists left in their default
	// order are absent rather than stored as "default".
	Sorts map[string]string `toml:"sorts,omitempty"`
}

// WebUIState returns the stored layout and whether anything was ever stored.
// The flag matters: a saved EMPTY section list ("I unfolded them all") must
// not be mistaken for a first run, which starts some sections folded.
func (fs *FileStore) WebUIState() (WebUI, bool) {
	r := fs.read()
	if r.WebUI == nil {
		return WebUI{}, false
	}
	w := *r.WebUI
	if w.Sections == nil {
		w.Sections = []string{}
	}
	return w, true
}

// SetWebUIState replaces the stored layout (read-merge, then atomic rewrite,
// like every other record here).
func (fs *FileStore) SetWebUIState(w WebUI) error {
	if w.Sections == nil {
		w.Sections = []string{}
	}
	r := fs.read()
	r.WebUI = &w
	return fs.write(r)
}
