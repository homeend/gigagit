package tui

import (
	"context"
	"testing"
)

// A parked F window drops its on-disk list and rendered tree (the one
// parked item worth not keeping per worktree) but keeps the window, its
// filter and the path under the cursor.
func TestParkedFWindowDropsItsListAndKeepsItsFilter(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	home := m.viewed
	m, other := addWorktree(t, m, "wt2")
	m, _ = m.openWorktreeFiles()
	nm, _ := m.Update(lsFilesMsg{paths: []string{"a.go", "b.go", "dir/c.go"}})
	m = nm.(Model)
	m.wtSetQuery("c")
	if got := m.wtSelected(); got != "dir/c.go" {
		t.Fatalf("precondition: cursor on %q, want dir/c.go", got)
	}
	m, ok := m.switchView(other)
	if !ok {
		t.Fatalf("refused: %s", m.statusMsg)
	}
	w := m.views[home].windows
	if w.wtFiles == nil || w.filesView == nil {
		t.Fatal("the F window itself was dropped")
	}
	if w.wtFiles.all != nil || w.wtFiles.untracked != nil || w.wtFiles.letters != nil || !w.wtFiles.loading {
		t.Fatalf("the list was kept while sleeping: all=%d loading=%v", len(w.wtFiles.all), w.wtFiles.loading)
	}
	if len(w.filesView.lines) > 1 {
		t.Fatalf("the rendered tree was kept while sleeping: %d lines", len(w.filesView.lines))
	}
	if w.wtFiles.query != "c" || w.wtFiles.keepPath != "dir/c.go" {
		t.Fatalf("filter %q keepPath %q: not kept", w.wtFiles.query, w.wtFiles.keepPath)
	}
}

// A returned F window re-reads its list through its own worktree's
// service; the filter stays and the cursor lands back on the kept path.
func TestReturnedFWindowReReadsItsList(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	home := m.currentWorktree
	m, other := addWorktree(t, m, "wt2")
	real, err := m.svc.LsFiles(context.Background())
	if err != nil || len(real) == 0 {
		t.Fatalf("ls-files: %v %v", real, err)
	}
	keep := real[len(real)-1]
	m, _ = m.openWorktreeFiles()
	nm, _ := m.Update(lsFilesMsg{paths: real})
	m = nm.(Model)
	m.wtSetQuery(keep)
	if m.wtSelected() != keep {
		t.Fatalf("precondition: cursor on %q, want %q", m.wtSelected(), keep)
	}
	m, _ = m.switchView(other)
	m, ok := m.switchView(home)
	if !ok {
		t.Fatalf("refused: %s", m.statusMsg)
	}
	var landed bool
	for _, msg := range drainBatch(m.viewKickCmd()) {
		if ls, ok := msg.(lsFilesMsg); ok {
			nm, _ := m.Update(ls)
			m = nm.(Model)
			landed = true
		}
	}
	if !landed {
		t.Fatal("the kick did not re-read the F window's list")
	}
	if m.wtFiles == nil || m.wtFiles.loading || len(m.wtFiles.all) != len(real) {
		t.Fatalf("the list is not back: %+v", m.wtFiles)
	}
	if m.wtFiles.query != keep || m.wtSelected() != keep || m.wtFiles.keepPath != "" {
		t.Fatalf("filter %q cursor %q keepPath %q", m.wtFiles.query, m.wtSelected(), m.wtFiles.keepPath)
	}
}
