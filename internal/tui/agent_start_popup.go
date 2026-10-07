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
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/i18n"
)

// agentStage is where the Start agent popup is.
type agentStage int

const (
	stageDetecting agentStage = iota // first run: detecting + writing the session commands
	stageChoose                      // numbered list of session commands
	stageApprove                     // first-run approval of the picked command
	stageName                        // the optional session name; alt+↓ recalls names used before
)

// agentStartPopup starts an agent session in a worktree: on a first run it
// detects the installed agents (busy notice), then offers the configured
// session commands, gates an unapproved one behind the shared approval box,
// asks for an optional name, and opens the new session's console.
type agentStartPopup struct {
	stage    agentStage
	worktree string
	cmds     []config.ToolCommand
	sel      int
	pick     config.ToolCommand
	name     textfield
}

// agentEnsureMsg carries the first-run detect+write result.
type agentEnsureMsg struct {
	added    []string
	cfg      config.Config
	loaded   bool // cfg is the re-read effective config
	path     string
	err      error
	worktree string
}

// agentStartedMsg carries a started (or failed) session.
type agentStartedMsg struct {
	id    domain.SessionID
	name  string
	inbox string // the GG_INBOX the child was given ("" = none)
	note  string // a status line to show once the console opens (sessionPlace)
	err   error
}

// Test seams: the machine's agents and the global config file.
var (
	agentDetect = func() []exttool.Detection {
		home, _ := os.UserHomeDir()
		return exttool.Detect(exec.LookPath, os.Stat, home)
	}
	agentGlobalConfigPath = config.DefaultGlobalPath
)

// startAgentFor opens the Start agent flow for worktree.
func (m Model) startAgentFor(worktree string) (Model, tea.Cmd) {
	if _, _, why := sessionPlace(worktree); why != "" {
		m.statusMsg = why
		return m, nil
	}
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
		if err == nil {
			// Always: with nothing added the file may still hold commands this
			// model never loaded (the hosted page's first run wrote them).
			if nc, lerr := config.Load(path, repoPath); lerr == nil {
				msg.cfg, msg.loaded = nc, true
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
	if msg.loaded {
		m.cfg = msg.cfg
	}
	if msg.added != nil {
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

// choose moves past the chooser: straight to the name step when approved,
// else to the approval box.
func (p *agentStartPopup) choose(m Model, tc config.ToolCommand) (tea.Model, tea.Cmd) {
	p.pick = tc
	if m.toolCommandApproved(tc.Command) {
		return p.askName(m)
	}
	p.stage = stageApprove
	return m, nil
}

// askName moves to the optional name step for the picked command.
func (p *agentStartPopup) askName(m Model) (tea.Model, tea.Cmd) {
	p.stage, p.name = stageName, newTextField("")
	return m.recallReset(), nil
}

// start closes the popup and starts the session (named name, "" = unnamed)
// off the UI thread, sized for the docked console it opens in.
func (p *agentStartPopup) start(m Model, name string) (tea.Model, tea.Cmd) {
	m = m.recallReset().popLayer()
	g := m.layout()
	cols, rows := consoleInner(g.rightW, g.boxH[panelCommits])
	svc, tc, dir, env, inbox, url := m.svc, p.pick, p.worktree, m.childEnv(), m.childInboxDir(), m.agentURL()
	cwd, note, _ := sessionPlace(dir)
	note = startNote(note, tc)
	title := domain.SessionTitle(tc.Name, name)
	m.statusMsg = i18n.T("starting %s…", title)
	return m, func() tea.Msg {
		s, _, err := svc.StartAgentSession(context.Background(), tc, dir, cwd, cols, rows, env, url, domain.SpawnRecord{Name: name}, "")
		if err != nil {
			return agentStartedMsg{name: title, err: err}
		}
		return agentStartedMsg{id: s.Info().ID, name: title, inbox: inbox, note: note}
	}
}

// applyAgentStarted opens the new session's console.
func (m Model) applyAgentStarted(msg agentStartedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.statusMsg = i18n.T("could not start %s: %s", msg.name, msg.err.Error())
		return m, nil
	}
	m.statusMsg = msg.note
	if msg.inbox != "" {
		m.childInbox[msg.id] = msg.inbox
	}
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
	case stageName:
		if nm, nq, handled, _ := m.recallUpdate(scopeAgentName, msg, p.name.Value()); handled {
			p.name = newTextField(nq) // a recalled name fills the field; enter again starts
			return nm, nil
		} else {
			m = nm
		}
		switch msg.Type {
		case tea.KeyEsc:
			m = m.recallReset()
			if len(p.cmds) > 1 {
				p.stage = stageChoose
				return m, nil
			}
			return m.popLayer(), nil
		case tea.KeyEnter:
			name := domain.CleanAgentName(p.name.Value())
			var record tea.Cmd
			if name != "" {
				m, record = m.recordSearch(scopeAgentName, name)
			}
			nm, cmd := p.start(m, name)
			return nm.(Model), tea.Batch(record, cmd)
		default:
			p.name.HandleEditKey(msg) // spaces included — do NOT swallow KeySpace
		}
		return m, nil
	case stageApprove:
		switch key {
		case "enter":
			m.rememberToolApproval(p.pick.Command)
			nm, cmd := p.askName(m)
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
	case stageName:
		hint := i18n.T("[enter] start  [alt+↓] recent names  [esc] back")
		if len(p.cmds) <= 1 {
			hint = i18n.T("[enter] start  [alt+↓] recent names  [esc] cancel")
		}
		lines = []string{
			i18n.T("Name this agent (optional)"), "",
			i18n.T("Start %s in %s", p.pick.Name, shortWorktreeName(p.worktree)), "",
			viewField("> ", p.name, true, textW), "",
			hint,
		}
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

// sessionMenuRows are the Worktrees `.` menu's agent rows: Start agent on a
// worktree row; Open / Kill / Remove on a session sub-row.
func (m Model) sessionMenuRows() []actionRow {
	if m.focus != panelWorktrees && m.focus != panelBranches {
		return nil
	}
	if info, ok := m.selectedSession(); ok {
		id := info.ID
		rows := []actionRow{{id: "session-open", label: i18n.T("Open session"), run: func(m Model) (tea.Model, tea.Cmd) {
			return m.openConsole(id)
		}}}
		rows = append(rows, m.tourMenuRows(id)...)
		if info.State == domain.SessionRunning {
			rows = append(rows, actionRow{id: "session-kill", label: i18n.T("Kill session"), run: func(m Model) (tea.Model, tea.Cmd) {
				return m.killSession(id), nil
			}}, actionRow{id: "session-kill-remove", label: i18n.T("Kill and remove session"), run: func(m Model) (tea.Model, tea.Cmd) {
				return m.killRemoveSessionRow(info), nil
			}})
		} else {
			rows = append(rows, actionRow{id: "session-remove", label: i18n.T("Remove session"), run: func(m Model) (tea.Model, tea.Cmd) {
				_ = domain.Sessions().Remove(id)
				return m, nil
			}})
		}
		return rows
	}
	if info, ok := m.selectedTask(); ok {
		id, key := info.ID, info.Key
		var rows []actionRow
		if info.State.Live() {
			rows = append(rows, actionRow{id: "task-cancel", label: i18n.T("Cancel task"), run: func(m Model) (tea.Model, tea.Cmd) {
				_ = domain.Tasks().Cancel(id)
				m.statusMsg = i18n.T("cancelled %s", key)
				return m, nil
			}})
		}
		rows = append(rows, actionRow{id: "task-result", label: i18n.T("Show result"), run: func(m Model) (tea.Model, tea.Cmd) {
			return m.openSessionsPopupOn(tabTasks, id)
		}})
		return rows
	}
	if m.focus != panelWorktrees {
		return nil // a Branches row has its own, worktree-qualified rows (branchSessionRows)
	}
	if wt, ok := m.selectedWorktree(); ok && wt.Path != "" {
		path := wt.Path
		rows := []actionRow{
			{id: "start-agent", label: i18n.T("Start agent"), run: func(m Model) (tea.Model, tea.Cmd) {
				return m.startAgentFor(path)
			}},
			{id: "open-terminal", label: i18n.T("Open terminal"), run: func(m Model) (tea.Model, tea.Cmd) {
				return m.openTerminal(path)
			}},
		}
		return append(rows, m.worktreeMarkRows(wt)...)
	}
	return nil
}

// canRemoveSessionRow gates x on the Worktrees and Branches tabs: an exited
// session sub-row is selected.
func (m Model) canRemoveSessionRow() bool {
	info, ok := m.selectedSession()
	return ok && info.State != domain.SessionRunning
}

// canKillRemoveSessionRow gates X on the Worktrees and Branches tabs: a
// running session sub-row is selected (an exited one already shows x).
func (m Model) canKillRemoveSessionRow() bool {
	info, ok := m.selectedSession()
	return ok && info.State == domain.SessionRunning
}

// removeSessionRow is x on a session sub-row: an exited session is removed
// (its console closes through onSessionsChanged); a running one is refused
// and pointed at X.
func (m Model) removeSessionRow(info domain.SessionInfo) Model {
	if info.State == domain.SessionRunning {
		m.statusMsg = i18n.T("only an exited session can be removed — X kills and removes a running one")
		return m
	}
	if err := domain.Sessions().Remove(info.ID); err != nil {
		m.statusMsg = i18n.T("only an exited session can be removed — X kills and removes a running one")
		return m
	}
	m.statusMsg = i18n.T("removed %s", info.Label)
	return m
}

// killRemoveSessionRow is X on a session sub-row (and the . menu's Kill and
// remove session): a running session is killed after a Kill/Cancel confirm
// and its row disappears once the exit is recorded (the manager removes it;
// the list change closes any console docked on it). On an exited row it is
// x. Option VALUES stay English (optionDisplayName renders them); Cancel is
// last so esc resolves to it.
func (m Model) killRemoveSessionRow(info domain.SessionInfo) Model {
	if info.State != domain.SessionRunning {
		return m.removeSessionRow(info)
	}
	id, label, dir := info.ID, info.Label, shortWorktreeName(info.Dir)
	m.modal = &decisionState{
		req: engine.DecisionRequest{
			ID:      "session-kill-remove",
			Prompt:  i18n.T("Kill %s in %s and remove it from the list?", label, dir),
			Options: []string{"Kill", "Cancel"},
		},
		sel: 1, // default highlight = Cancel
		onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
			if opt != "Kill" {
				return m, nil
			}
			if err := domain.Sessions().KillAndRemove(id); err != nil {
				return m, nil // already gone
			}
			m.statusMsg = i18n.T("killing and removing %s in %s…", label, dir)
			return m, nil
		},
	}
	return m
}

// branchSessionRows are the Branches `.` menu's agent rows: Start agent /
// Open terminal on a branch that is checked out in some worktree — the rows
// exist only where the row itself shows a worktree path (worktreePathOf, the
// marker's own lookup), because only that branch exists on disk to run
// anything in. The current worktree counts. The labels name the worktree,
// which the Branches tab does not otherwise show; the actions are the
// Worktrees tab's own.
func (m Model) branchSessionRows() []actionRow {
	if m.focus != panelBranches {
		return nil
	}
	b, ok := m.selectedBranch()
	if !ok {
		return nil
	}
	path, ok := m.worktreePathOf(b.Name)
	if !ok || path == "" {
		return nil
	}
	name := shortWorktreeName(path)
	return []actionRow{
		{id: "start-agent", label: i18n.T("Start agent in %s", name), run: func(m Model) (tea.Model, tea.Cmd) {
			return m.startAgentFor(path)
		}},
		{id: "open-terminal", label: i18n.T("Open terminal in %s", name), run: func(m Model) (tea.Model, tea.Cmd) {
			return m.openTerminal(path)
		}},
	}
}
