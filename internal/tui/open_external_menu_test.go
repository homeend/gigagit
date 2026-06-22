package tui

import "testing"

// menuIDs collects the action ids the `.` menu would offer for m.
func menuIDs(m Model) map[string]bool {
	got := map[string]bool{}
	for _, r := range availableActions(m) {
		got[r.id] = true
	}
	return got
}

// Regression: opening blame FROM the files-view commit side left the files view
// live underneath, so the whole Commits action set (cherry-pick / revert /
// reset / graph controls) leaked into the blame `.` menu — acting on a hidden
// surface. The blame surface on top must own its menu: copy rows + its own
// "Open in external editor" only.
func TestBlameMenuExcludesLeakedCommitOps(t *testing.T) {
	base := filesViewGraphModel(false) // commit side: commit ops WOULD show here
	if !menuIDs(base)["commit-cherry-pick"] {
		t.Fatal("precondition: the files-view commit side should offer commit ops")
	}
	m := base.pushLayer(&blameView{ctx: navContext{path: "a.go", rev: "h0"}})
	got := menuIDs(m)
	for _, id := range []string{"commit-cherry-pick", "commit-revert", "commit-reset", "graph-widen"} {
		if got[id] {
			t.Errorf("blame menu leaked commit op %q from the files view underneath", id)
		}
	}
	if !got["open-external"] {
		t.Errorf("blame menu should offer Open in external editor, got %v", got)
	}
}

// The diff view ranks above the files view as the front surface (it's a Model
// field, so topLayer() is nil for it). Opening a diff FROM the files-view commit
// side leaves the files view live underneath, so the same commit ops + the
// files-view View file / Open in external editor rows must not leak into the
// diff `.` menu.
func TestDiffMenuExcludesLeakedFilesViewRows(t *testing.T) {
	base := filesViewGraphModel(false) // commit side, no diff yet: commit ops show
	if !menuIDs(base)["commit-cherry-pick"] {
		t.Fatal("precondition: the files-view commit side should offer commit ops")
	}
	m := base
	m = m.pushLayer(&diffView{title: "a.go", rev: "h0"}) // a diff opened over the files view
	got := menuIDs(m)
	for _, id := range []string{"commit-cherry-pick", "commit-revert", "graph-widen", "view-file", "open-external"} {
		if got[id] {
			t.Errorf("diff menu leaked %q from the files view underneath", id)
		}
	}
}

// Same leak for the history surface (the user's first report: "it looks like the
// commit window menu").
func TestHistoryMenuExcludesLeakedCommitOps(t *testing.T) {
	base := filesViewGraphModel(false)
	if !menuIDs(base)["commit-cherry-pick"] {
		t.Fatal("precondition: the files-view commit side should offer commit ops")
	}
	m := base.pushLayer(histFixture())
	got := menuIDs(m)
	for _, id := range []string{"commit-cherry-pick", "commit-revert", "commit-reset", "graph-widen"} {
		if got[id] {
			t.Errorf("history menu leaked commit op %q from the files view underneath", id)
		}
	}
	if !got["open-external"] {
		t.Errorf("history menu should offer Open in external editor, got %v", got)
	}
}
