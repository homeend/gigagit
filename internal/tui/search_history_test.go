package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/searchhist"
)

// upd drives one key/message through Update and returns the new Model.
func upd(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	u, cmd := m.Update(msg)
	return u.(Model), cmd
}

// batchCmds flattens a command into its sub-commands: if cmd produces a
// tea.BatchMsg it returns the batch's commands; otherwise it returns a single
// command that replays the captured message. Lets a test drive a batched
// (persist + reload) result the way the runtime would.
func batchCmds(cmd tea.Cmd) []tea.Cmd {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		return batch
	}
	return []tea.Cmd{func() tea.Msg { return msg }}
}

// withSearchStore wires a temp-dir store into a loaded model's service.
func withSearchStore(t *testing.T, m Model) Model {
	t.Helper()
	m.svc.SetSearchStore(searchhist.NewFileStore(t.TempDir()))
	return m
}

func TestPanelFilterEnterRecords(t *testing.T) {
	m := withSearchStore(t, loadedModel(t))
	m.focus = panelBranches
	m, _ = upd(t, m, keyMsg("/")) // start filter
	m = typeRunes(t, m, "feat")
	m, _ = upd(t, m, keyMsg("enter")) // commit -> record (in-memory ring updated)
	if got := m.searchHist[scopePanel]; len(got) == 0 || got[0] != "feat" {
		t.Fatalf("panel ring = %v, want newest 'feat'", got)
	}
}

func TestFilterEscDoesNotRecord(t *testing.T) {
	m := withSearchStore(t, loadedModel(t))
	m.focus = panelBranches
	m, _ = upd(t, m, keyMsg("/"))
	m = typeRunes(t, m, "junk")
	m, _ = upd(t, m, keyMsg("esc")) // cancel -> no record
	if got := m.searchHist[scopePanel]; len(got) != 0 {
		t.Fatalf("esc must not record, got %v", got)
	}
}

func TestHighlightSharesPanelRing(t *testing.T) {
	m := withSearchStore(t, loadedModel(t))
	m.focus = panelCommits
	m, _ = upd(t, m, keyMsg("@")) // start highlight
	m = typeRunes(t, m, "bugfix")
	m, _ = upd(t, m, keyMsg("enter"))
	// @ records into the SAME ring as /.
	if got := m.searchHist[scopePanel]; len(got) == 0 || got[0] != "bugfix" {
		t.Fatalf("@ must share the panel ring, got %v", got)
	}
}

func TestStartupLoadPopulatesRings(t *testing.T) {
	store := searchhist.NewFileStore(t.TempDir())
	_ = store.Record(scopePanel, "preexisting", 20)
	m := loadedModel(t)
	m.svc.SetSearchStore(store)
	m, _ = upd(t, m, loadSearchHistCmd(m.svc)())
	if got := m.searchHist[scopePanel]; len(got) != 1 || got[0] != "preexisting" {
		t.Fatalf("startup load ring = %v, want [preexisting]", got)
	}
}

func TestRecordPersistsToStore(t *testing.T) {
	store := searchhist.NewFileStore(t.TempDir())
	m := loadedModel(t)
	m.svc.SetSearchStore(store)
	m.focus = panelBranches
	m, _ = upd(t, m, keyMsg("/"))
	m = typeRunes(t, m, "persisted")
	m, cmd := upd(t, m, keyMsg("enter"))
	if cmd == nil {
		t.Fatal("Enter on a non-empty query should return a persist command")
	}
	cmd() // run the fire-and-forget persist
	if got := store.All()[scopePanel]; len(got) != 1 || got[0] != "persisted" {
		t.Fatalf("store ring = %v, want [persisted]", got)
	}
}
