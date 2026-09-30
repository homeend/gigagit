package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/promptstate"
)

// sampleToolStatus is the trigger case: an unstamped, web-only Claude
// headless complete block, offered the retagged template.
func sampleToolStatus(t *testing.T) domain.ToolTemplateStatus {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	old := config.ToolCommand{Category: "conflict_complete", Name: "Claude — resolve & complete (yolo, headless)", Mode: "capture", Frontends: []string{"web"}, Command: "claude -p x"}
	if err := config.AppendToolCommands(path, []config.ToolCommand{old}); err != nil {
		t.Fatal(err)
	}
	nw := old
	nw.Frontends, nw.TemplateVersion = []string{"tui", "web"}, 2
	return domain.ToolTemplateStatus{Path: path, Block: old, New: nw, Kind: domain.ToolUpdateAvailable, FromVersion: 0, ToVersion: 2, Edited: true, ToolLabel: "Claude Code"}
}

func sendKey(m Model, k tea.KeyMsg) Model {
	nm, _ := m.Update(k)
	return nm.(Model)
}

func TestToolUpdatePopupShowsReasonAndNewText(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.width, m.height = 140, 50
	st := sampleToolStatus(t)
	out := (&toolUpdatePopup{st: st}).box(m)
	for _, want := range []string{`frontends = ["tui", "web"]`, "claude -p x", "template_version = 2", "[t]", "[k]", "v2"} {
		if !strings.Contains(out, want) {
			t.Errorf("popup lacks %q:\n%s", want, out)
		}
	}
}

func TestToolUpdatePopupTakeNewWrites(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	st := sampleToolStatus(t)
	m = m.pushLayer(&toolUpdatePopup{st: st})
	m = sendKey(m, keyRunes("t"))
	got, _ := config.ToolCommandsIn(st.Path)
	if len(got) != 1 || len(got[0].Frontends) != 2 || got[0].TemplateVersion != 2 {
		t.Fatalf("take new did not write: %+v", got)
	}
	if layerOf[*toolUpdatePopup](m) != nil {
		t.Fatal("popup must close after take new")
	}
}

func TestToolUpdatePopupKeepMineRemembers(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	store := promptstate.NewFileStore(filepath.Join(t.TempDir(), "prompts.toml"))
	m.promptStore = store
	st := sampleToolStatus(t)
	m.toolStatuses = []domain.ToolTemplateStatus{st}
	m = m.rebuildNotices()
	before, _ := os.ReadFile(st.Path)
	m = m.pushLayer(&toolUpdatePopup{st: st})
	m = sendKey(m, keyRunes("k"))
	if findNotice(m, noticeToolTemplateUpdate) != nil {
		t.Fatal("the update notice must clear after keep mine")
	}
	after, _ := os.ReadFile(st.Path)
	if string(before) != string(after) {
		t.Fatal("keep mine must not write the config")
	}
	if !store.DeclinedToolUpdates()[promptstate.ToolUpdateID(st.OfferKey())] {
		t.Fatal("keep mine must remember the offer")
	}
	if layerOf[*toolUpdatePopup](m) != nil {
		t.Fatal("popup must close after keep mine")
	}
}

func TestToolUpdatePopupEscLeavesEverything(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	st := sampleToolStatus(t)
	before, _ := os.ReadFile(st.Path)
	m = m.pushLayer(&toolUpdatePopup{st: st})
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyEsc})
	after, _ := os.ReadFile(st.Path)
	if string(before) != string(after) || layerOf[*toolUpdatePopup](m) != nil {
		t.Fatal("esc must close without writing")
	}
}

func TestToolsWizardUOpensReviewForUpdateRow(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	st := sampleToolStatus(t)
	p := &settingsPopup{toolsView: true}
	p.toolRows = []toolWizardRow{{tmpl: exttoolTemplateFor(st), existing: true, status: &st}}
	p.toolChecked = []bool{true}
	m = m.pushLayer(p)
	m = sendKey(m, keyRunes("u"))
	if layerOf[*toolUpdatePopup](m) == nil {
		t.Fatal("u on an update-available row must open the review")
	}
}

func exttoolTemplateFor(st domain.ToolTemplateStatus) exttool.CommandTemplate {
	return exttool.CommandTemplate{Category: exttool.Category(st.Block.Category), Name: st.Block.Name}
}

// A long template never pushes the answer keys off a short terminal: the
// template text scrolls inside the box instead.
func TestToolUpdatePopupFitsShortTerminal(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.width, m.height = 80, 24
	st := sampleToolStatus(t)
	st.New.Command = strings.Repeat("claude --a-rather-long-flag value \\\n", 40)
	p := &toolUpdatePopup{st: st}
	box := p.box(m)
	if n := strings.Count(strings.TrimRight(box, "\n"), "\n") + 1; n > m.height {
		t.Fatalf("box is %d rows on a %d-row terminal", n, m.height)
	}
	if !strings.Contains(box, "[t]") {
		t.Fatal("answer keys must stay visible")
	}
	p.update(m, tea.KeyMsg{Type: tea.KeyDown})
	if p.top != 1 {
		t.Fatalf("down must scroll the template text, top=%d", p.top)
	}
}

// Take new marks the offer settled at once — the Settings row and the notice
// must not keep offering it while the background re-read runs.
func TestToolUpdatePopupTakeNewSettlesStatusImmediately(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	st := sampleToolStatus(t)
	m.toolStatuses = []domain.ToolTemplateStatus{st}
	m = m.rebuildNotices()
	m = m.pushLayer(&toolUpdatePopup{st: st})
	m = sendKey(m, keyRunes("t"))
	if len(m.toolStatuses) != 1 || m.toolStatuses[0].Kind != domain.ToolCurrent {
		t.Fatalf("status after take new: %+v", m.toolStatuses)
	}
	if findNotice(m, noticeToolTemplateUpdate) != nil {
		t.Fatal("the update notice must clear after take new")
	}
}
