package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// noticeModelWithNotice returns a loaded model (real repo) carrying the
// commit-graph notice, as if a health read just landed.
func noticeModelWithNotice(t *testing.T) (Model, string) {
	t.Helper()
	m, st := noticeTestModel(t)
	_ = st
	dir := m.currentWorktree
	h := bigRepoHealth()
	h.GitCommonDir = filepath.Join(dir, ".git")
	nm, _ := m.Update(repoHealthMsg{gen: m.noticeGen, health: h})
	return nm.(Model), dir
}

func TestBangOpensDialogAndMarksRead(t *testing.T) {
	t.Parallel()
	m, _ := noticeModelWithNotice(t)
	if !m.noticesUnread {
		t.Fatal("precondition: unread")
	}
	nm, _ := m.Update(keyMsg("!"))
	m = nm.(Model)
	if layerOf[*noticePopup](m) == nil {
		t.Fatal("! must open the notification dialog")
	}
	if m.noticesUnread {
		t.Fatal("opening the dialog must mark notices read")
	}
	if out := m.View(); !strings.Contains(out, "faster in this repo") {
		t.Fatalf("dialog must list the notice title, view:\n%s", out)
	}
}

func TestBangInertWhileFilterTyping(t *testing.T) {
	t.Parallel()
	m, _ := noticeModelWithNotice(t)
	m.filterTyping = true
	m.filterPanel = m.focus
	nm, _ := m.Update(keyMsg("!"))
	if layerOf[*noticePopup](nm.(Model)) != nil {
		t.Fatal("! must be inert while a filter is capturing input")
	}
}

func TestDialogWithZeroNoticesSaysSo(t *testing.T) {
	t.Parallel()
	m, _ := noticeTestModel(t)
	nm, _ := m.Update(keyMsg("!"))
	m = nm.(Model)
	if layerOf[*noticePopup](m) == nil {
		t.Fatal("! must open even with no notices (esc closes)")
	}
	if out := m.View(); !strings.Contains(out, "no notices") {
		t.Fatalf("empty dialog must say 'no notices', view:\n%s", out)
	}
}

func TestEscClosesActionsThenList(t *testing.T) {
	t.Parallel()
	m, _ := noticeModelWithNotice(t)
	nm, _ := m.Update(keyMsg("!"))
	m = nm.(Model)
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // list → actions
	m = nm.(Model)
	p := layerOf[*noticePopup](m)
	if p == nil || !p.showActions {
		t.Fatal("enter on a notice must show its actions")
	}
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc}) // actions → list
	m = nm.(Model)
	if p := layerOf[*noticePopup](m); p == nil || p.showActions {
		t.Fatal("esc must return from actions to the list, not close")
	}
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc}) // list → closed
	if layerOf[*noticePopup](nm.(Model)) != nil {
		t.Fatal("esc on the list must close the dialog")
	}
	if len(nm.(Model).notices) != 1 {
		t.Fatal("closing without acting must keep the notice (read, not dismissed)")
	}
}

func TestNotNowDismissesForSession(t *testing.T) {
	t.Parallel()
	m, _ := noticeModelWithNotice(t)
	nm, _ := m.Update(keyMsg("!"))
	m = nm.(Model)
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // actions
	m = nm.(Model)
	p := layerOf[*noticePopup](m)
	p.actSel = 2 // Not now
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(Model)
	if layerOf[*noticePopup](m) != nil {
		t.Fatal("acting must close the dialog")
	}
	if len(m.notices) != 0 {
		t.Fatal("Not now must remove the notice from the session list")
	}
	if !m.noticeSessionDismissed[noticeCommitGraph] {
		t.Fatal("Not now must record the session dismissal")
	}
}

func TestNeverPersistsDismissal(t *testing.T) {
	t.Parallel()
	m, _ := noticeModelWithNotice(t)
	repoKey := m.notices[0].repoKey
	nm, _ := m.Update(keyMsg("!"))
	m = nm.(Model)
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(Model)
	p := layerOf[*noticePopup](m)
	p.actSel = 3 // Never for this repo
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(Model)
	if len(m.notices) != 0 {
		t.Fatal("Never must remove the notice")
	}
	st := m.promptStore
	if st == nil || !st.DismissedNotices(repoKey)[noticeCommitGraph] {
		t.Fatal("Never must persist the per-repo dismissal in the prompt store")
	}
}

func TestWriteAndEnableRunsBothOpsForReal(t *testing.T) {
	t.Parallel()
	m, dir := noticeModelWithNotice(t)
	nm, _ := m.Update(keyMsg("!"))
	m = nm.(Model)
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // actions; actSel=0 = write+enable
	m = nm.(Model)
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(Model)
	if !m.running {
		t.Fatal("write+enable must start the WriteCommitGraph op")
	}
	m = driveOp(t, m, cmd) // drives the op AND its chained SetGitConfig

	cg := filepath.Join(dir, ".git", "objects", "info", "commit-graph")
	if _, err := os.Stat(cg); err != nil {
		t.Fatalf("commit-graph file not written at %s: %v", cg, err)
	}
	out, err := exec.Command("git", "-C", dir, "config", "--local", "fetch.writeCommitGraph").Output()
	if err != nil || strings.TrimSpace(string(out)) != "true" {
		t.Fatalf("chained config set missing: %q, %v", out, err)
	}
	if len(m.notices) != 0 {
		t.Fatal("acting must remove the notice")
	}
}

func TestDialogSwallowsGlobalKeys(t *testing.T) {
	t.Parallel()
	m, _ := noticeModelWithNotice(t)
	nm, _ := m.Update(keyMsg("!"))
	m = nm.(Model)
	before := len(m.layers.entries)
	for _, k := range []string{"p", "g", "G", ",", "!", "r"} {
		nm, _ := m.Update(keyMsg(k))
		m = nm.(Model)
	}
	if len(m.layers.entries) != before || layerOf[*noticePopup](m) == nil {
		t.Fatal("dialog must swallow global keys")
	}
}

func TestNoticePopupMaximizeWidensAndLiftsRowCap(t *testing.T) {
	t.Parallel()
	m := Model{}
	m.width, m.height = 200, 50
	for i := 0; i < 20; i++ { // more than the fixed cap of 12
		m.notices = append(m.notices, notice{id: fmt.Sprintf("n%d", i), title: fmt.Sprintf("notice %d", i)})
	}
	p := &noticePopup{}

	normal := p.box(m)
	p.maximized = true
	maxed := p.box(m)

	if lipgloss.Width(maxed) <= lipgloss.Width(normal) {
		t.Fatalf("maximized width %d must exceed normal %d", lipgloss.Width(maxed), lipgloss.Width(normal))
	}
	if lipgloss.Height(maxed) <= lipgloss.Height(normal) {
		t.Fatalf("maximized must show more rows: height %d vs %d", lipgloss.Height(maxed), lipgloss.Height(normal))
	}
}
