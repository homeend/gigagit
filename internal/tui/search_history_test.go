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

func seedPanelRing(m Model, phrases ...string) Model {
	m.searchHist = map[string][]string{scopePanel: phrases} // newest-first
	return m
}

func TestRecallAltDownOpensNewest(t *testing.T) {
	m := seedPanelRing(loadedModel(t), "newest", "older", "oldest")
	m.focus = panelBranches
	m, _ = upd(t, m, keyMsg("/"))
	m = typeRunes(t, m, "dr")            // draft "dr"
	m, _ = upd(t, m, keyMsg("alt+down")) // open on newest
	if !m.recallOpen || m.filterQuery != "newest" {
		t.Fatalf("alt+down should open & preview newest; open=%v q=%q", m.recallOpen, m.filterQuery)
	}
	m, _ = upd(t, m, keyMsg("alt+down")) // -> older
	if m.filterQuery != "older" {
		t.Fatalf("second alt+down -> older, got %q", m.filterQuery)
	}
}

func TestRecallAltUpAboveNewestRestoresDraft(t *testing.T) {
	m := seedPanelRing(loadedModel(t), "newest", "older")
	m.focus = panelBranches
	m, _ = upd(t, m, keyMsg("/"))
	m = typeRunes(t, m, "dr")
	m, _ = upd(t, m, keyMsg("alt+down")) // open, q="newest"
	m, _ = upd(t, m, keyMsg("alt+up"))   // above newest -> close + restore draft
	if m.recallOpen || m.filterQuery != "dr" {
		t.Fatalf("alt+up above newest restores draft & closes; open=%v q=%q", m.recallOpen, m.filterQuery)
	}
}

func TestRecallEscRestoresDraftKeepsTyping(t *testing.T) {
	m := seedPanelRing(loadedModel(t), "newest")
	m.focus = panelBranches
	m, _ = upd(t, m, keyMsg("/"))
	m = typeRunes(t, m, "dr")
	m, _ = upd(t, m, keyMsg("alt+down")) // open
	m, _ = upd(t, m, keyMsg("esc"))      // close, restore draft, STILL typing
	if m.recallOpen || !m.filterTyping || m.filterQuery != "dr" {
		t.Fatalf("esc in dropdown: open=%v typing=%v q=%q", m.recallOpen, m.filterTyping, m.filterQuery)
	}
}

func TestRecallTypingClosesDropdown(t *testing.T) {
	m := seedPanelRing(loadedModel(t), "newest")
	m.focus = panelBranches
	m, _ = upd(t, m, keyMsg("/"))
	m, _ = upd(t, m, keyMsg("alt+down")) // open, q="newest"
	m = typeRunes(t, m, "x")             // typing closes; appends to previewed query
	if m.recallOpen {
		t.Fatalf("typing must close the dropdown")
	}
	if m.filterQuery != "newestx" {
		t.Fatalf("typing appends to the previewed query, got %q", m.filterQuery)
	}
}

func TestRecallEnterCommitsHighlighted(t *testing.T) {
	m := withSearchStore(t, seedPanelRing(loadedModel(t), "newest", "older"))
	m.focus = panelBranches
	m, _ = upd(t, m, keyMsg("/"))
	m, _ = upd(t, m, keyMsg("alt+down")) // open -> "newest"
	m, _ = upd(t, m, keyMsg("alt+down")) // -> "older"
	m, _ = upd(t, m, keyMsg("enter"))    // accept "older"
	if m.filterTyping || m.recallOpen {
		t.Fatalf("enter must commit: typing=%v open=%v", m.filterTyping, m.recallOpen)
	}
	if m.filterQuery != "older" {
		t.Fatalf("committed query = %q, want older", m.filterQuery)
	}
	// "older" moves to top of the ring (dedup-to-top on re-commit).
	if got := m.searchHist[scopePanel]; got[0] != "older" {
		t.Fatalf("ring after commit = %v, want older newest", got)
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
