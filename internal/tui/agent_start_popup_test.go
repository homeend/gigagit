package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/exttool"
)

// withAgentSeams points detection and the global config at test values.
// Serial: package-level seams.
func withAgentSeams(t *testing.T, detect func() []exttool.Detection) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	oldD, oldP := agentDetect, agentGlobalConfigPath
	agentDetect, agentGlobalConfigPath = detect, func() string { return path }
	t.Cleanup(func() { agentDetect, agentGlobalConfigPath = oldD, oldP })
	return path
}

func detectOnly(ids ...string) func() []exttool.Detection {
	return func() []exttool.Detection {
		var out []exttool.Detection
		for _, tl := range exttool.Builtins() {
			for _, id := range ids {
				if tl.ID == id {
					out = append(out, exttool.Detection{Tool: tl, Bin: id})
				}
			}
		}
		return out
	}
}

func TestStartAgentFirstRunDetectsAndWrites(t *testing.T) {
	m := loadedModel(t)
	path := withAgentSeams(t, detectOnly("claude", "codex"))
	m, cmd := m.startAgentFor(m.currentWorktree)
	p, ok := m.topLayer().(*agentStartPopup)
	if !ok || p.stage != stageDetecting || cmd == nil {
		t.Fatalf("first run must show the detecting stage with a cmd; top=%T", m.topLayer())
	}
	if !strings.Contains(m.View(), "Detecting installed agents") {
		t.Fatal("the busy notice is not on screen")
	}
	mm, _ := m.Update(cmd())
	m = mm.(Model)
	p, _ = m.topLayer().(*agentStartPopup)
	if p == nil || p.stage != stageChoose || len(p.cmds) != 2 {
		t.Fatalf("after detection: %+v", p)
	}
	if !strings.Contains(m.statusMsg, "Added Claude, Codex") {
		t.Fatalf("status = %q", m.statusMsg)
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), `category = "session"`) {
		t.Fatalf("config not written:\n%s", body)
	}
}

func TestStartAgentNothingDetected(t *testing.T) {
	m := loadedModel(t)
	withAgentSeams(t, detectOnly())
	m, cmd := m.startAgentFor(m.currentWorktree)
	mm, _ := m.Update(cmd())
	m = mm.(Model)
	if m.topLayer() != nil {
		t.Fatal("the popup must close when nothing is detected")
	}
	if !strings.Contains(m.statusMsg, `category = "session"`) {
		t.Fatalf("status = %q", m.statusMsg)
	}
}

func TestStartAgentApproveThenStartsConsole(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	startTestSession(t, m, `sleep 5`) // installs a fresh process-global manager
	m.cfg.Tools.Command = []config.ToolCommand{
		{Category: "session", Name: "A", Mode: "session", Command: "sleep 5"},
		{Category: "session", Name: "B", Mode: "session", Command: "sleep 6"},
	}
	m, _ = m.startAgentFor(m.currentWorktree)
	mm, _ := m.Update(keyMsg("2"))
	m = mm.(Model)
	p, _ := m.topLayer().(*agentStartPopup)
	if p == nil || p.stage != stageApprove || p.pick.Name != "B" {
		t.Fatalf("an unapproved pick must ask first: %+v", p)
	}
	mm, _ = m.Update(keyMsg("esc"))
	m = mm.(Model)
	if p, _ := m.topLayer().(*agentStartPopup); p == nil || p.stage != stageChoose {
		t.Fatal("esc on the approval returns to the chooser")
	}
	mm, _ = m.Update(keyMsg("2"))
	m = mm.(Model)
	mm, cmd := m.Update(keyMsg("enter"))
	m = mm.(Model)
	if p, _ := m.topLayer().(*agentStartPopup); p == nil || p.stage != stageName || cmd != nil {
		t.Fatal("approving must move to the name step, starting nothing yet")
	}
	m = typeText(t, m, "viewer")
	mm, cmd = m.Update(keyMsg("enter"))
	m = mm.(Model)
	if m.topLayer() != nil || cmd == nil {
		t.Fatal("enter on the name must close the popup and start the session")
	}
	m = landStart(t, m, cmd)
	if m.console == nil || !m.console.focused {
		t.Fatalf("the new session's console must open focused: %+v", m.console)
	}
	s, _ := m.consoleSession()
	if in := s.Info(); in.Label != "B" || in.Name != "viewer" {
		t.Fatalf("started %+v, want B named viewer", in)
	}
}

// landStart runs a start's cmd tree once (the ring's persist and the start
// itself) and lands only the started message — the console it opens
// subscribes to the session, a cmd that never ends.
func landStart(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for _, msg := range flattenCmd(t, cmd) {
		if started, ok := msg.(agentStartedMsg); ok {
			mm, _ := m.Update(started)
			m = mm.(Model)
		}
	}
	return m
}

// twoCmdModel: a model with two approved session commands.
func twoCmdModel(t *testing.T) Model {
	t.Helper()
	m := loadedModel(t)
	m.width, m.height = 120, 40
	startTestSession(t, m, `sleep 5`) // installs a fresh process-global manager
	m.cfg.Tools.Command = []config.ToolCommand{
		{Category: "session", Name: "A", Mode: "session", Command: "sleep 5"},
		{Category: "session", Name: "B", Mode: "session", Command: "sleep 6"},
	}
	m.rememberToolApproval("sleep 5")
	m.rememberToolApproval("sleep 6")
	return m
}

func TestStartAgentNameStepEmptyEnterStartsUnnamed(t *testing.T) {
	m := twoCmdModel(t)
	m, _ = m.startAgentFor(m.currentWorktree)
	mm, _ := m.Update(keyMsg("1"))
	m = mm.(Model)
	p, _ := m.topLayer().(*agentStartPopup)
	if p == nil || p.stage != stageName || p.pick.Name != "A" {
		t.Fatalf("an approved pick goes to the name step: %+v", p)
	}
	if v := m.View(); !strings.Contains(v, "Name this agent (optional)") || !strings.Contains(v, "[alt+↓] recent names") {
		t.Fatalf("name step not on screen:\n%s", v)
	}
	mm, cmd := m.Update(keyMsg("enter"))
	m = landStart(t, mm.(Model), cmd)
	s, _ := m.consoleSession()
	if in := s.Info(); in.Label != "A" || in.Name != "" {
		t.Fatalf("started %+v, want A unnamed", in)
	}
	if len(m.searchHist[scopeAgentName]) != 0 {
		t.Fatal("an empty name is not remembered")
	}
}

func TestStartAgentNameStepEsc(t *testing.T) {
	m := twoCmdModel(t)
	m, _ = m.startAgentFor(m.currentWorktree)
	mm, _ := m.Update(keyMsg("2"))
	m = mm.(Model)
	mm, _ = m.Update(keyMsg("esc"))
	m = mm.(Model)
	if p, _ := m.topLayer().(*agentStartPopup); p == nil || p.stage != stageChoose {
		t.Fatal("esc on the name step returns to the chooser when there are several commands")
	}
	// One command (unapproved): approve → name; esc closes, nothing starts.
	m = m.popLayer()
	m.cfg.Tools.Command = []config.ToolCommand{{Category: "session", Name: "Solo", Mode: "session", Command: "sleep 7"}}
	m, _ = m.startAgentFor(m.currentWorktree)
	if p, _ := m.topLayer().(*agentStartPopup); p == nil || p.stage != stageApprove {
		t.Fatalf("a single unapproved command asks approval first: %+v", p)
	}
	mm, _ = m.Update(keyMsg("enter"))
	m = mm.(Model)
	if p, _ := m.topLayer().(*agentStartPopup); p == nil || p.stage != stageName {
		t.Fatal("approval leads to the name step")
	}
	before := len(domain.Sessions().List())
	mm, cmd := m.Update(keyMsg("esc"))
	m = mm.(Model)
	if m.topLayer() != nil || cmd != nil || len(domain.Sessions().List()) != before {
		t.Fatal("esc on a single command's name step closes the popup and starts nothing")
	}
}

func TestStartAgentNameRecallAndCleaning(t *testing.T) {
	m := twoCmdModel(t)
	m.searchHist = map[string][]string{scopeAgentName: {"worker", "viewer"}}
	m, _ = m.startAgentFor(m.currentWorktree)
	mm, _ := m.Update(keyMsg("1"))
	m = mm.(Model)
	mm, _ = m.Update(keyMsg("alt+down"))
	m = mm.(Model)
	mm, _ = m.Update(keyMsg("alt+down"))
	m = mm.(Model)
	if !m.recallOpen || !strings.Contains(m.View(), "viewer") {
		t.Fatal("alt+↓ opens the recent names")
	}
	mm, _ = m.Update(keyMsg("enter")) // fills the field
	m = mm.(Model)
	p, _ := m.topLayer().(*agentStartPopup)
	if p == nil || p.name.Value() != "viewer" || m.recallOpen {
		t.Fatalf("enter on a recalled name fills the field: %+v", p)
	}
	mm, cmd := m.Update(keyMsg("enter"))
	m = landStart(t, mm.(Model), cmd)
	if m.recallOpen {
		t.Fatal("the recall dropdown must not outlive the popup")
	}
	s, _ := m.consoleSession()
	if s.Info().Name != "viewer" || m.searchHist[scopeAgentName][0] != "viewer" {
		t.Fatalf("named %q, ring %v", s.Info().Name, m.searchHist[scopeAgentName])
	}
}

func TestStartAgentNamePasteIsOneLine(t *testing.T) {
	m := twoCmdModel(t)
	m, _ = m.startAgentFor(m.currentWorktree)
	mm, _ := m.Update(keyMsg("1"))
	m = mm.(Model)
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("rev\niewer\t"), Paste: true})
	m = mm.(Model)
	mm, cmd := m.Update(keyMsg("enter"))
	m = landStart(t, mm.(Model), cmd)
	s, _ := m.consoleSession()
	if got := s.Info().Name; got != "reviewer" {
		t.Fatalf("name %q, want reviewer", got)
	}
}

func TestSessionMenuRows(t *testing.T) {
	m := loadedModel(t)
	s := startTestSession(t, m, `sleep 5`)
	m.focus = panelWorktrees
	ids := func() string {
		var out []string
		for _, r := range m.sessionMenuRows() {
			out = append(out, r.id)
		}
		return strings.Join(out, ",")
	}
	m.sel[panelWorktrees] = 0
	// worktree-reserve: the Worktrees row's agent reserve toggle (no claim here).
	if got := ids(); got != "start-agent,open-terminal,worktree-reserve" {
		t.Fatalf("worktree row: %s", got)
	}
	m.sel[panelWorktrees] = 1
	if got := ids(); got != "session-open,session-kill,session-kill-remove" {
		t.Fatalf("running session row: %s", got)
	}
	for _, r := range m.sessionMenuRows() {
		if r.id == "session-kill" {
			mm, _ := r.run(m)
			if st := mm.(Model).statusMsg; !strings.Contains(st, "killing") || !strings.Contains(st, s.Info().Label) {
				t.Fatalf("Kill session must say so at once, status = %q", st)
			}
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for s.Info().State == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if got := ids(); got != "session-open,session-remove" {
		t.Fatalf("exited session row: %s", got)
	}
	found := false
	for _, r := range availableActions(m) {
		if r.id == "session-remove" {
			found = true
		}
	}
	if !found {
		t.Fatal("the . menu must list the session rows")
	}
	_ = tea.KeyMsg{}
}
