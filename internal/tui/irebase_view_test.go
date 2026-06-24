package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/rebaseplan"
)

// edRows: oldest-first input [c1,c2,c3] → editor shows newest-first [c3,c2,c1].
func edRows() []model.RangeCommit {
	return []model.RangeCommit{
		{Hash: "h1", Subject: "wip1", Message: "wip1\n"},
		{Hash: "h2", Subject: "wip2", Message: "wip2\n"},
		{Hash: "h3", Subject: "wip3", Message: "wip3\n"},
	}
}

func TestIrebaseEditorNewestFirstAndPlanOrder(t *testing.T) {
	e := newIrebaseEditor("work", "main", edRows(), "/bin/gg")
	if e.rows[0].sha != "h3" {
		t.Fatalf("top row = %q, want h3 (newest-first)", e.rows[0].sha)
	}
	// default plan is all-pick, oldest-first
	plan := e.plan()
	if len(plan.Entries) != 3 || plan.Entries[0].Sha != "h1" || plan.Entries[2].Sha != "h3" {
		t.Fatalf("plan order wrong: %+v", plan.Entries)
	}
	for _, en := range plan.Entries {
		if en.Action != rebaseplan.Pick {
			t.Fatalf("default action = %q, want pick", en.Action)
		}
	}
}

func TestIrebaseEditorActionsAndReorder(t *testing.T) {
	e := newIrebaseEditor("work", "main", edRows(), "/bin/gg")
	m := Model{layers: &layerStack{entries: []layer{e}}}
	// focus top row (h3, newest), drop it
	m, _ = e.update(m, key("d"))
	if e.rows[0].action != rebaseplan.Drop {
		t.Fatalf("top action = %q, want drop", e.rows[0].action)
	}
	// move focus down to h2, squash it
	m, _ = e.update(m, key("j"))
	m, _ = e.update(m, key("s"))
	if e.rows[1].action != rebaseplan.Squash {
		t.Fatalf("row1 action = %q, want squash", e.rows[1].action)
	}
	// reorder: move focused row up
	before := e.rows[0].sha
	m, _ = e.update(m, keyType(tea.KeyCtrlUp))
	if e.rows[0].sha == before && len(e.rows) > 1 {
		// row1 should have swapped to top
		t.Fatalf("ctrl+up did not reorder")
	}
	// reset restores all-pick, original order
	m, _ = e.update(m, key("R"))
	if e.rows[0].sha != "h3" || e.rows[0].action != rebaseplan.Pick {
		t.Fatalf("reset did not restore: %+v", e.rows[0])
	}
}

func TestIrebaseEditorSquashOnOldestRefused(t *testing.T) {
	e := newIrebaseEditor("work", "main", edRows(), "/bin/gg")
	m := Model{layers: &layerStack{entries: []layer{e}}}
	// move to the bottom (oldest) row and squash → refused
	m, _ = e.update(m, key("j"))
	m, _ = e.update(m, key("j")) // now on h1 (oldest, last row)
	m, _ = e.update(m, key("s"))
	if e.rows[len(e.rows)-1].action == rebaseplan.Squash {
		t.Fatal("squash on the oldest row must be refused")
	}
}

func TestIrebaseEditorReword(t *testing.T) {
	e := newIrebaseEditor("work", "main", edRows(), "/bin/gg")
	m := Model{layers: &layerStack{entries: []layer{e}}}
	m, _ = e.update(m, key("r")) // open reword for h3
	if e.reword == nil {
		t.Fatal("r must open reword input")
	}
	m, _ = e.update(m, keyRunes("X"))
	m, _ = e.update(m, keyType(tea.KeyCtrlS)) // submit
	if e.reword != nil {
		t.Fatal("ctrl+s must close reword input")
	}
	if e.rows[0].action != rebaseplan.Reword || e.rows[0].newMsg == "" {
		t.Fatalf("reword not stored: %+v", e.rows[0])
	}
}

func TestIrebaseLoadedMsgPushesEditor(t *testing.T) {
	m := Model{width: 80, height: 24}
	updated, _ := m.Update(irebaseLoadedMsg{branch: "work", onto: "main", commits: edRows()})
	m = updated.(Model)
	if _, ok := m.topLayer().(*irebaseEditor); !ok {
		t.Fatal("irebaseLoadedMsg should push the editor surface")
	}
}

func TestIrebaseLoadedEmptyRangeNoOp(t *testing.T) {
	m := Model{width: 80, height: 24}
	updated, _ := m.Update(irebaseLoadedMsg{branch: "work", onto: "main", commits: nil})
	m = updated.(Model)
	if m.topLayer() != nil {
		t.Fatal("empty range must not push a surface")
	}
}

// TestIrebaseEditorReceivesRoutedKeys proves the live routing: a key sent
// through Model.Update reaches the editor surface on top of the stack (not just
// direct e.update calls).
func TestIrebaseEditorReceivesRoutedKeys(t *testing.T) {
	e := newIrebaseEditor("work", "main", edRows(), "/bin/gg")
	m := Model{width: 80, height: 24}
	m = m.pushLayer(e)
	updated, _ := m.Update(key("d")) // drop the focused (top) row
	_ = updated.(Model)
	if e.rows[0].action != rebaseplan.Drop {
		t.Fatalf("routed key did not reach editor: top action = %q", e.rows[0].action)
	}
}

// key/keyType/keyRunes helpers (reuse if already present in the tui test pkg).
func key(s string) tea.KeyMsg          { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }
func keyType(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }
func keyRunes(s string) tea.KeyMsg     { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }
