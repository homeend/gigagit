package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/i18n"
)

// agentStage is where the Start agent… popup is.
type agentStage int

const (
	stageDetecting agentStage = iota // first run: detecting + writing the session commands
	stageChoose                      // numbered list of session commands
	stageApprove                     // first-run approval of the picked command
)

// agentStartPopup starts an agent session in a worktree: on a first run it
// detects the installed agents (busy notice), then offers the configured
// session commands, gates an unapproved one behind the shared approval box,
// and opens the new session's console.
type agentStartPopup struct {
	stage    agentStage
	worktree string
	cmds     []config.ToolCommand
	sel      int
	pick     config.ToolCommand
}

// agentEnsureMsg carries the first-run detect+write result.
type agentEnsureMsg struct {
	added    []string
	cfg      config.Config
	path     string
	err      error
	worktree string
}

// agentStartedMsg carries a started (or failed) session.
type agentStartedMsg struct {
	id   domain.SessionID
	name string
	err  error
}

// Test seams: the machine's agents and the global config file.
var (
	agentDetect = func() []exttool.Detection {
		home, _ := os.UserHomeDir()
		return exttool.Detect(exec.LookPath, os.Stat, home)
	}
	agentGlobalConfigPath = config.DefaultGlobalPath
)

// startAgentFor opens the Start agent… flow for worktree.
func (m Model) startAgentFor(worktree string) (Model, tea.Cmd) {
	cmds := domain.SessionCommands(m.cfg, "tui")
	if len(cmds) == 0 {
		m = m.pushLayer(&agentStartPopup{stage: stageDetecting, worktree: worktree})
		return m, m.ensureAgentsCmd(worktree)
	}
	p := &agentStartPopup{stage: stageChoose, worktree: worktree, cmds: cmds}
	m = m.pushLayer(p)
	if len(cmds) == 1 {
		nm, cmd := p.choose(m, cmds[0])
		return nm.(Model), cmd
	}
	return m, nil
}

// ensureAgentsCmd runs the first-run auto-configure off the UI thread and
// reloads the effective config it wrote to.
func (m Model) ensureAgentsCmd(worktree string) tea.Cmd {
	cfg, repoPath := m.cfg, m.repoConfigPath
	return func() tea.Msg {
		path := agentGlobalConfigPath()
		added, err := domain.EnsureSessionCommands(cfg, path, agentDetect)
		msg := agentEnsureMsg{added: added, path: path, err: err, worktree: worktree}
		if err == nil && added != nil {
			if nc, lerr := config.Load(path, repoPath); lerr == nil {
				msg.cfg = nc
			}
		}
		return msg
	}
}

// applyAgentEnsure lands the first-run result in the popup.
func (m Model) applyAgentEnsure(msg agentEnsureMsg) (tea.Model, tea.Cmd) {
	p := layerOf[*agentStartPopup](m)
	if p == nil || p.stage != stageDetecting {
		return m, nil // the user closed it meanwhile
	}
	if msg.err != nil {
		m = m.popLayer()
		m.statusMsg = i18n.T("could not write the agent commands: %s", msg.err.Error())
		return m, nil
	}
	if msg.added != nil {
		m.cfg = msg.cfg
		m.statusMsg = i18n.T("Added %s to %s — edit there or in Settings → External tools", strings.Join(msg.added, ", "), msg.path)
	}
	cmds := domain.SessionCommands(m.cfg, "tui")
	if len(cmds) == 0 {
		m = m.popLayer()
		m.statusMsg = i18n.T("no agent found — add a [[tools.command]] block with category = \"session\" and mode = \"session\"")
		return m, nil
	}
	p.cmds, p.stage, p.sel = cmds, stageChoose, 0
	if len(cmds) == 1 {
		return p.choose(m, cmds[0])
	}
	return m, nil
}

// choose moves past the chooser: straight to start when approved, else to
// the approval box.
func (p *agentStartPopup) choose(m Model, tc config.ToolCommand) (tea.Model, tea.Cmd) {
	p.pick = tc
	if m.toolCommandApproved(tc.Command) {
		return p.start(m)
	}
	p.stage = stageApprove
	return m, nil
}

// start closes the popup and starts the session off the UI thread, sized for
// the docked console it opens in.
func (p *agentStartPopup) start(m Model) (tea.Model, tea.Cmd) {
	m = m.popLayer()
	g := m.layout()
	cols, rows := consoleInner(g.rightW, g.boxH[panelCommits])
	svc, tc, dir := m.svc, p.pick, p.worktree
	m.statusMsg = i18n.T("starting %s…", tc.Name)
	return m, func() tea.Msg {
		s, err := svc.StartSession(context.Background(), tc, dir, cols, rows, nil)
		if err != nil {
			return agentStartedMsg{name: tc.Name, err: err}
		}
		return agentStartedMsg{id: s.Info().ID, name: tc.Name}
	}
}

// applyAgentStarted opens the new session's console.
func (m Model) applyAgentStarted(msg agentStartedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.statusMsg = i18n.T("could not start %s: %s", msg.name, msg.err.Error())
		return m, nil
	}
	m.statusMsg = ""
	return m.openConsole(msg.id)
}

func (p *agentStartPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		return m, tea.Quit
	}
	switch p.stage {
	case stageDetecting:
		if key == "esc" {
			m = m.popLayer()
		}
		return m, nil
	case stageApprove:
		switch key {
		case "enter":
			m.rememberToolApproval(p.pick.Command)
			nm, cmd := p.start(m)
			return nm.(Model), cmd
		case "esc":
			if len(p.cmds) > 1 {
				p.stage = stageChoose
				return m, nil
			}
			return m.popLayer(), nil
		}
		return m, nil
	}
	switch key {
	case "esc":
		return m.popLayer(), nil
	case "up", "k":
		if p.sel > 0 {
			p.sel--
		}
	case "down", "j":
		if p.sel < len(p.cmds)-1 {
			p.sel++
		}
	case "enter":
		if p.sel < len(p.cmds) {
			nm, cmd := p.choose(m, p.cmds[p.sel])
			return nm.(Model), cmd
		}
	default:
		if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
			if i := int(key[0] - '1'); i < len(p.cmds) {
				nm, cmd := p.choose(m, p.cmds[i])
				return nm.(Model), cmd
			}
		}
	}
	return m, nil
}

func (p *agentStartPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	inner := popupInnerWidth(w)
	textW := popupTextWidth(inner)
	var lines []string
	switch p.stage {
	case stageDetecting:
		lines = []string{i18n.T("Start agent"), "", "⏳ " + i18n.T("Detecting installed agents…"), "", i18n.T("[esc] cancel")}
	case stageApprove:
		lines = []string{i18n.T("Start this agent?  (%s)", p.pick.Name), "", approvalBoxView(p.pick.Command, textW)}
	default:
		lines = []string{i18n.T("Start agent in %s", shortWorktreeName(p.worktree)), ""}
		s := st()
		for i, tc := range p.cmds {
			row := fmt.Sprintf("%d  %s", i+1, tc.Name)
			if i == p.sel {
				lines = append(lines, s.selectedRow.Render(padRight("> "+row, textW)))
			} else {
				lines = append(lines, lipgloss.NewStyle().Render("  "+row))
			}
		}
		lines = append(lines, "", i18n.T("[enter/1-9] start  [esc] cancel"))
	}
	box := popupBox(inner, strings.Join(lines, "\n"))
	return overlayCenter(clipToHeight(below, h), box, w, h)
}

// sessionMenuRows are the Worktrees `.` menu's agent rows: Start agent… on a
// worktree row; Open / Kill / Remove on a session sub-row.
func (m Model) sessionMenuRows() []actionRow {
	if m.focus != panelWorktrees {
		return nil
	}
	if info, ok := m.selectedSession(); ok {
		id := info.ID
		rows := []actionRow{{id: "session-open", label: i18n.T("Open session"), run: func(m Model) (tea.Model, tea.Cmd) {
			return m.openConsole(id)
		}}}
		if info.State == domain.SessionRunning {
			rows = append(rows, actionRow{id: "session-kill", label: i18n.T("Kill session"), run: func(m Model) (tea.Model, tea.Cmd) {
				return m.killSession(id), nil
			}})
		} else {
			rows = append(rows, actionRow{id: "session-remove", label: i18n.T("Remove session"), run: func(m Model) (tea.Model, tea.Cmd) {
				_ = domain.Sessions().Remove(id)
				return m, nil
			}})
		}
		return rows
	}
	if wt, ok := m.selectedWorktree(); ok && wt.Path != "" {
		path := wt.Path
		return []actionRow{{id: "start-agent", label: i18n.T("Start agent…"), run: func(m Model) (tea.Model, tea.Cmd) {
			return m.startAgentFor(path)
		}}}
	}
	return nil
}
