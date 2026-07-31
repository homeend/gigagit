package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// shCommitEntry is a shelved-commit entry (path-less origin, tar payload).
func shCommitEntry(id string) model.ShelfEntry {
	return model.ShelfEntry{
		ID:     id,
		Kind:   model.ShelfKindCommit,
		Origin: model.FileAddress{State: model.StateCommitted, Commit: "a1b2c3d4e5"},
		SHA:    id + "0000",
	}
}

// A shelved commit has no single file behind it: the per-file switcher actions
// (p restore, e editor, c vs-bookmark) must notice-and-no-op instead of
// treating the tar payload as a file. (enter is different: it opens the shelf
// files view — TestShelfSwitcherEnterOpensCommitFilesView. m is different too,
// now that mark-two supports comparing two commit entries — see
// TestShelfPopupMarkTogglesOnLoneCommitEntry in entry_compare_test.go.)
func TestShelfPopupCommitEntryGuardsFileActions(t *testing.T) {
	for _, key := range []string{"p", "e", "c"} {
		m := shelfPopModel(shCommitEntry("ce"))
		mm, cmd := m.Update(keyMsg(key))
		m = mm.(Model)
		if m.diffLayer() != nil {
			t.Fatalf("[%s] must not open a diff for a shelved commit", key)
		}
		if shelfRestoreOf(m) != nil {
			t.Fatalf("[%s] must not open the restore popup for a shelved commit", key)
		}
		if m.pendingCompare != nil {
			t.Fatalf("[%s] must not start a compare for a shelved commit", key)
		}
		if cmd != nil {
			t.Fatalf("[%s] must not dispatch a command for a shelved commit", key)
		}
		if !strings.Contains(m.statusMsg, "shelved commit") {
			t.Fatalf("[%s] should explain why nothing happened, statusMsg=%q", key, m.statusMsg)
		}
		if m.shelfSwitcher() == nil {
			t.Fatalf("[%s] must leave the switcher open", key)
		}
		if m.shelfSwitcher().markID != "" {
			t.Fatalf("[%s] must not mark a shelved commit", key)
		}
	}
}

// In compare mode (a focused file picked first), enter on a shelved commit is
// refused the same way a commit bookmark is on the bookmark side.
func TestShelfPopupCompareModeEnterRefusesCommitEntry(t *testing.T) {
	m := shelfPopModel(shCommitEntry("ce"))
	m.shelfSwitcher().compareRef = &model.FileRef{Source: model.SourceUnstaged, Path: "focused.go"}
	m.shelfSwitcher().compareLabel = "wt / unstaged / focused.go"
	mm, _ := m.Update(keyMsg("enter"))
	m = mm.(Model)
	if m.diffLayer() != nil || m.diffTag != "" {
		t.Fatalf("compare-mode enter must not diff a file against a shelved commit, tag=%q", m.diffTag)
	}
	if !strings.Contains(m.statusMsg, "shelved commit") {
		t.Fatalf("should explain the refusal, statusMsg=%q", m.statusMsg)
	}
}

// The whole-entry actions stay available on a shelved commit: x (remove) and
// t (copy to temp dir).
func TestShelfPopupCommitEntryKeepsRemoveAndExport(t *testing.T) {
	m := shelfPopModel(shCommitEntry("ce"))
	mm, _ := m.Update(keyMsg("x"))
	if mm.(Model).modal == nil {
		t.Fatal("x must still offer remove for a shelved commit")
	}

	m = shelfPopModel(shCommitEntry("ce"))
	_, cmd := m.Update(keyMsg("t"))
	if cmd == nil {
		t.Fatal("t must still start the temp-dir export for a shelved commit")
	}
}
