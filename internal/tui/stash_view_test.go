package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

func TestCapitalSOpensStashView(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	m.focus = panelFiles
	mm, cmd := m.Update(keyMsg("S"))
	got := mm.(Model)
	if got.stashView == nil {
		t.Fatal("S should open the stash view")
	}
	if cmd == nil {
		t.Error("opening the stash view should fire its load cmd")
	}
	if got.focus != panelCommits {
		t.Error("opening the stash window should move focus to the right column (panelCommits)")
	}
	if got.panelFocused(panelFiles) {
		t.Error("left panels must dim (unfocused) while the stash window is open")
	}
}

func TestStashWindowArrowFocusSwitch(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	m.focus = panelCommits
	m.stashView = &stashView{entries: []model.StashEntry{{Ref: "stash@{0}", Subject: "WIP"}}}
	mm, _ := m.updateStashViewKey(keyMsg("l"))
	m = mm.(Model)
	if m.filesView == nil || m.filesTreeFocused {
		t.Fatal("l should open the tree with focus on the stash list (filesTreeFocused=false)")
	}
	mm, _ = m.updateFilesViewKey(keyMsg("left"))
	if !mm.(Model).filesTreeFocused {
		t.Error("← should focus the file tree")
	}
	mm, _ = mm.(Model).updateFilesViewKey(keyMsg("right"))
	if mm.(Model).filesTreeFocused {
		t.Error("→ should focus the stash list")
	}
}

func TestStashWindowCloseRestoresFocus(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	m.focus = panelFiles
	mm, _ := m.Update(keyMsg("S"))
	mm, _ = mm.(Model).updateStashViewKey(keyMsg("esc"))
	got := mm.(Model)
	if got.stashView != nil {
		t.Fatal("esc should close the stash window")
	}
	if got.focus != panelFiles {
		t.Errorf("closing should restore focus to panelFiles, got %v", got.focus)
	}
}

// enter already drilled in — with the tree open, enter on the stash-list side
// is inert; the stash actions live in the "." menu.
func TestStashListEnterUnderTreeDoesNothing(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	m.stashView = &stashView{entries: []model.StashEntry{{Ref: "stash@{0}", Subject: "WIP"}}}
	m.filesView = &contentPopup{lines: []contentLine{{text: "a.go", path: "a.go"}}}
	m.filesTreeFocused = false // focused on the stash list
	mm, cmd := m.updateFilesViewKey(keyMsg("enter"))
	got := mm.(Model)
	if got.topLayer() != nil || cmd != nil {
		t.Fatal("enter on the stash-list side (tree open) must be inert")
	}
	if got.filesView == nil || got.stashView == nil {
		t.Fatal("enter must not close the files view or the stash window")
	}
}

func TestStashListAppliedToView(t *testing.T) {
	t.Parallel()
	m := Model{width: 100, height: 30, sel: map[panel]int{}}
	m.stashView = &stashView{loading: true, tag: "stash"}
	entries := []model.StashEntry{{Ref: "stash@{0}", Subject: "On main: WIP on main"}}
	mm, _ := m.Update(stashListMsg{tag: "stash", entries: entries})
	got := mm.(Model)
	if got.stashView.loading || len(got.stashView.entries) != 1 {
		t.Fatalf("entries not applied: %+v", got.stashView)
	}
}

func TestStashViewRendersInRightColumn(t *testing.T) {
	t.Parallel()
	m := Model{width: 100, height: 30, sel: map[panel]int{}, status: model.WorkingTreeStatus{Branch: "main"}}
	m.stashView = &stashView{entries: []model.StashEntry{
		{Ref: "stash@{0}", Subject: "On main: WIP on main"},
		{Ref: "stash@{1}", Subject: "On feat: sketch"},
	}}
	out := m.View()
	if !contains(out, "Stashes") {
		t.Errorf("right column should be titled Stashes:\n%s", out)
	}
	if !contains(out, "WIP on main") || !contains(out, "sketch") {
		t.Errorf("stash subjects missing:\n%s", out)
	}
}

func TestStashViewNavAndClose(t *testing.T) {
	t.Parallel()
	m := Model{width: 100, height: 30, sel: map[panel]int{}}
	m.stashView = &stashView{entries: []model.StashEntry{{Ref: "stash@{0}"}, {Ref: "stash@{1}"}}}
	mm, _ := m.updateStashViewKey(keyMsg("j"))
	if mm.(Model).stashView.sel != 1 {
		t.Fatal("j should move stash selection")
	}
	mm, _ = mm.(Model).updateStashViewKey(keyMsg("S"))
	if mm.(Model).stashView != nil {
		t.Fatal("S should close the stash view")
	}
}

func TestStashViewLLoadsFiles(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	m.stashView = &stashView{entries: []model.StashEntry{{Ref: "stash@{0}", Subject: "On main: WIP"}}}
	mm, cmd := m.updateStashViewKey(keyMsg("l"))
	got := mm.(Model)
	if got.filesView == nil {
		t.Fatal("l should open the file tree for the stash")
	}
	if got.filesTreeFocused {
		t.Error("the stash file tree should open with focus on the list (follow-live), like commits")
	}
	if got.filesStashTag != "stash@{0}" {
		t.Errorf("filesStashTag = %q", got.filesStashTag)
	}
	if cmd == nil {
		t.Error("l should fire the stash-files load cmd")
	}
}

func TestStashListRefreshesAfterOp(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	m.stashView = &stashView{entries: []model.StashEntry{{Ref: "stash@{0}"}}, tag: "stash"}
	mm, cmd := m.Update(opFinishedMsg{res: engine.Result{Changed: true}})
	got := mm.(Model)
	if cmd == nil {
		t.Fatal("op finishing with the stash window open should refresh the list")
	}
	if !got.stashView.loading {
		t.Error("the stash list should be marked loading during the refresh")
	}
}

func TestStashFollowLiveReloadsTree(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	m.stashView = &stashView{
		entries: []model.StashEntry{{Ref: "stash@{0}", Subject: "a"}, {Ref: "stash@{1}", Subject: "b"}},
		sel:     0,
	}
	m.filesView = &contentPopup{lines: []contentLine{{text: "x"}}}
	m.filesTreeFocused = false // list side
	m.filesStashTag = "stash@{0}"
	mm, cmd := m.updateFilesViewKey(keyMsg("j"))
	got := mm.(Model)
	if got.stashView.sel != 1 {
		t.Fatal("j on the list side should move the stash selection")
	}
	if got.filesStashTag != "stash@{1}" {
		t.Errorf("follow-live should retarget the tree to stash@{1}, got %q", got.filesStashTag)
	}
	if cmd == nil {
		t.Error("landing on a different stash should fire the follow-live reload")
	}
}

func TestStashLeftArrowFocusesPanelsThenRightReturns(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	m.focus = panelFiles
	mm, _ := m.Update(keyMsg("S")) // opens; focus → panelCommits, lastLeftPanel=panelFiles
	m = mm.(Model)
	// ← from the focused stash list releases focus to the left column.
	mm, _ = m.updateStashViewKey(keyMsg("left"))
	m = mm.(Model)
	if m.focus != panelFiles {
		t.Fatalf("← should focus the left column (panelFiles), got %v", m.focus)
	}
	if m.stashView == nil {
		t.Fatal("the stash window must stay open while inspecting the left panels")
	}
	if !m.panelFocused(panelFiles) {
		t.Error("the left panel should be bright/focused now")
	}
	// While focus is on a left panel, keys go to the normal dispatch (navigable).
	mm, _ = m.Update(keyMsg("right")) // → returns to the stash list
	m = mm.(Model)
	if m.focus != panelCommits {
		t.Errorf("→ should return focus to the stash list, got %v", m.focus)
	}
}

func TestStashOpenSToggleClosesFromLeftPanel(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	m.focus = panelFiles
	mm, _ := m.Update(keyMsg("S"))
	m = mm.(Model)
	mm, _ = m.updateStashViewKey(keyMsg("left")) // focus → panelFiles
	m = mm.(Model)
	mm, _ = m.Update(keyMsg("S")) // normal dispatch: toggle closed
	if mm.(Model).stashView != nil {
		t.Fatal("S from a left panel should close the open stash window")
	}
}

func TestOneLineFlattensWhitespace(t *testing.T) {
	t.Parallel()
	in := "error: changes would be overwritten\n\t8.txt\nPlease commit\nAborting"
	got := oneLine(in)
	if got != "error: changes would be overwritten 8.txt Please commit Aborting" {
		t.Errorf("oneLine = %q", got)
	}
}

func TestStatusBarRendersMultilineErrorOnOneLine(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	m.width, m.height = 200, 30
	m.statusMsg = "error: local changes to 8.txt would be overwritten\nAborting"
	out := m.View()
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "overwritten") && !strings.Contains(line, "Aborting") {
			t.Errorf("status not flattened to one line: %q", line)
		}
	}
}

func TestStashListWrapMode(t *testing.T) {
	t.Parallel()
	m := New(nil)
	m.width, m.height = 80, 24
	m.focus = panelCommits
	m.stashView = &stashView{entries: []model.StashEntry{{Ref: "stash@{0}", Subject: strings.Repeat("z", 60)}}, mode: modeWrap}
	out := m.renderStashList(20, 6)
	if strings.Count(out, "z") < 30 {
		t.Errorf("stash wrap mode did not expand the long subject:\n%s", out)
	}
}

// / opens a live filter on the stash list: each rune narrows the visible rows
// (ref + subject, case-insensitive), the cursor snaps to the nearest match, the
// title shows the query, and every consumer of the selection (l/enter, the .
// menu) targets the VISIBLE row — not the raw entries index.
func TestStashViewSlashFiltersList(t *testing.T) {
	t.Parallel()
	m := Model{width: 100, height: 30, sel: map[panel]int{}, focus: panelCommits, status: model.WorkingTreeStatus{Branch: "main"}}
	m.stashView = &stashView{entries: []model.StashEntry{
		{Ref: "stash@{0}", Subject: "On main: WIP alpha"},
		{Ref: "stash@{1}", Subject: "On feat: Fix beta"},
		{Ref: "stash@{2}", Subject: "On main: WIP gamma"},
	}}
	mm, _ := m.updateStashViewKey(keyMsg("/"))
	m = mm.(Model)
	if !m.stashView.typing {
		t.Fatal("/ should enter filter-typing mode")
	}
	if !contains(m.View(), "filter: type to search") {
		t.Error("the footer should show the filter strip while typing")
	}
	for _, r := range "fix" {
		mm, _ = m.updateStashViewKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = mm.(Model)
	}
	v := m.stashView
	if vis := v.visible(); len(vis) != 1 || vis[0] != 1 {
		t.Fatalf("filter 'fix' should leave only entry 1 visible, got %v", vis)
	}
	if e, ok := v.current(); !ok || e.Ref != "stash@{1}" {
		t.Errorf("cursor should sit on the visible match, got %+v ok=%v", e, ok)
	}
	out := m.View()
	if !contains(out, "Stashes /fix█") {
		t.Errorf("title should carry the in-progress query:\n%s", out)
	}
	if contains(out, "WIP alpha") || contains(out, "WIP gamma") {
		t.Errorf("non-matching stashes must be hidden:\n%s", out)
	}
	// The . menu acts on the visible row.
	if rows := m.stashActionRows(); len(rows) == 0 {
		t.Fatal("stash action rows should exist for the filtered selection")
	}
	// Enter keeps the filter (typing off, query kept, cursor unchanged).
	mm, _ = m.updateStashViewKey(keyMsg("enter"))
	m = mm.(Model)
	if m.stashView.typing || m.stashView.query != "fix" {
		t.Errorf("enter should keep the filter: typing=%v query=%q", m.stashView.typing, m.stashView.query)
	}
	if !contains(m.View(), "Stashes /fix") {
		t.Error("kept filter should stay in the title")
	}
	// l opens the files of the visible (filtered) stash, not entries[0].
	mm, _ = m.updateStashViewKey(keyMsg("l"))
	m = mm.(Model)
	if m.filesView == nil || m.filesStashTag != "stash@{1}" {
		t.Errorf("l should open the filtered stash's files, got tag %q", m.filesStashTag)
	}
}

// Esc while typing drops the query and puts the cursor back on the same stash
// in the full list; ctrl+r does the same for a kept filter.
func TestStashViewFilterEscAndCtrlRClear(t *testing.T) {
	t.Parallel()
	m := Model{width: 100, height: 30, sel: map[panel]int{}, focus: panelCommits}
	m.stashView = &stashView{entries: []model.StashEntry{
		{Ref: "stash@{0}", Subject: "alpha"},
		{Ref: "stash@{1}", Subject: "beta"},
		{Ref: "stash@{2}", Subject: "gamma"},
	}}
	type step struct {
		key   tea.KeyMsg
		clear tea.KeyMsg
	}
	for _, s := range []step{
		{keyMsg("/"), keyMsg("esc")},
		{keyMsg("/"), tea.KeyMsg{Type: tea.KeyCtrlR}},
	} {
		mm, _ := m.updateStashViewKey(s.key)
		mm, _ = mm.(Model).updateStashViewKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("gam")})
		got := mm.(Model)
		if e, ok := got.stashView.current(); !ok || e.Subject != "gamma" {
			t.Fatalf("typing 'gam' should select gamma, got %+v", e)
		}
		if s.clear.Type == tea.KeyCtrlR { // ctrl+r clears a KEPT filter: commit it first
			mm, _ = got.updateStashViewKey(keyMsg("enter"))
			got = mm.(Model)
		}
		mm, _ = got.updateStashViewKey(s.clear)
		got = mm.(Model)
		if got.stashView.typing || got.stashView.query != "" {
			t.Errorf("%v should clear the filter: typing=%v query=%q", s.clear, got.stashView.typing, got.stashView.query)
		}
		if len(got.stashView.visible()) != 3 || got.stashView.sel != 2 {
			t.Errorf("after clearing, the full list shows with the cursor still on gamma (sel=2), got sel=%d", got.stashView.sel)
		}
	}
}

// Backspace widens the list again and the typed query is rendered with a
// no-match placeholder when nothing passes.
func TestStashViewFilterBackspaceAndNoMatch(t *testing.T) {
	t.Parallel()
	m := Model{width: 100, height: 30, sel: map[panel]int{}, focus: panelCommits}
	m.stashView = &stashView{entries: []model.StashEntry{{Ref: "stash@{0}", Subject: "alpha"}}}
	mm, _ := m.updateStashViewKey(keyMsg("/"))
	mm, _ = mm.(Model).updateStashViewKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("zz")})
	got := mm.(Model)
	if !contains(got.View(), "(no match)") {
		t.Error("an all-hiding query should render the no-match placeholder")
	}
	if _, ok := got.stashView.current(); ok {
		t.Error("current() must report no entry when the filter hides everything")
	}
	if rows := got.stashActionRows(); rows != nil {
		t.Error("no stash actions when nothing is visible")
	}
	mm, _ = got.updateStashViewKey(tea.KeyMsg{Type: tea.KeyBackspace})
	mm, _ = mm.(Model).updateStashViewKey(tea.KeyMsg{Type: tea.KeyBackspace})
	got = mm.(Model)
	if got.stashView.query != "" || len(got.stashView.visible()) != 1 {
		t.Errorf("backspace should widen the list back, query=%q", got.stashView.query)
	}
}

// A reload while a filter is kept clamps the cursor to the FILTERED list.
func TestStashListReloadClampsToFilteredList(t *testing.T) {
	t.Parallel()
	m := Model{width: 100, height: 30, sel: map[panel]int{}}
	m.stashView = &stashView{query: "wip", sel: 1, tag: "stash", entries: []model.StashEntry{
		{Ref: "stash@{0}", Subject: "WIP a"}, {Ref: "stash@{1}", Subject: "WIP b"}, {Ref: "stash@{2}", Subject: "other"},
	}}
	mm, _ := m.Update(stashListMsg{tag: "stash", entries: []model.StashEntry{
		{Ref: "stash@{0}", Subject: "WIP a"}, {Ref: "stash@{1}", Subject: "other"},
	}})
	got := mm.(Model)
	if got.stashView.sel != 0 {
		t.Errorf("sel should clamp to the one visible WIP row, got %d", got.stashView.sel)
	}
}
