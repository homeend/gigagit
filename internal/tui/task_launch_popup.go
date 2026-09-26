package tui

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/promptstate"
	"github.com/homeend/gigagit/internal/template"
)

// launchMode is how the dialog runs a task (spec ruling 1).
type launchMode string

const (
	launchBackground launchMode = "background" // an agent session, no console opened
	launchForeground launchMode = "foreground" // an agent session, its console opens focused
	launchHeadless   launchMode = "headless"   // queued, captured, no console
)

var launchModes = []launchMode{launchBackground, launchForeground, launchHeadless}

// taskLaunch is what a caller asks the dialog to launch.
type taskLaunch struct {
	kind      exttool.Category
	review    domain.ReviewTarget // CatReview only
	whenOp    string              // conflict kinds: the paused op (when_op filter)
	commitBox bool                // opened from the commit box
}

type launchStage int

const (
	launchPreparing launchStage = iota // key + first-run interactive commands (async)
	launchChoose
	launchApprove
)

// launchRow is one mode × command row; none = the agent has no command for
// that mode (greyed, never selectable).
type launchRow struct {
	mode    launchMode
	tc      config.ToolCommand
	none    bool
	missing bool // the command's program is not on PATH
}

// taskLaunchPopup is the AI-task launch dialog: agent (←/→) × mode and
// variant (↑/↓), a wait line from the scheduler's load, the first-run
// approval, then Submit.
type taskLaunchPopup struct {
	launch  taskLaunch
	stage   launchStage
	key     string
	choices []domain.TaskChoice
	agent   int
	rows    []launchRow
	sel     int
}

// taskLookPath is the "not found" probe (a test seam).
var taskLookPath = exec.LookPath

// taskLaunchReadyMsg carries the dialog's async preparation.
type taskLaunchReadyMsg struct {
	key   string
	cfg   *config.Config // non-nil when first-run interactive commands were added
	added []string
	path  string
	err   error
}

// taskSubmittedMsg carries a submitted (or refused) task.
type taskSubmittedMsg struct {
	id     domain.TaskID
	launch taskLaunch
	mode   launchMode
	key    string
	inbox  string
	name   string
	err    error
}

// openTaskLaunch opens the dialog for l and prepares it off the UI thread:
// the task key (its title) and, for the commit-message and review kinds, the
// first-run interactive command rows.
func (m Model) openTaskLaunch(l taskLaunch) (Model, tea.Cmd) {
	if _, _, why := sessionPlace(m.currentWorktree); why != "" {
		m.statusMsg = why
		return m, nil
	}
	m = m.pushLayer(&taskLaunchPopup{launch: l, stage: launchPreparing})
	svc, cfg, repoPath := m.svc, m.cfg, m.repoConfigPath
	return m, func() tea.Msg {
		key, err := svc.TaskKey(context.Background(), l.kind, l.review)
		if err != nil {
			return taskLaunchReadyMsg{err: err}
		}
		msg := taskLaunchReadyMsg{key: key}
		if l.kind == exttool.CatCommitMessage || l.kind == exttool.CatReview {
			path := agentGlobalConfigPath()
			added, aerr := domain.EnsureInteractiveCommands(cfg, path, agentDetect)
			if aerr == nil && len(added) > 0 {
				if nc, lerr := config.Load(path, repoPath); lerr == nil {
					msg.cfg, msg.added, msg.path = &nc, added, path
				}
			}
		}
		return msg
	}
}

// applyTaskLaunchReady fills the dialog once its preparation lands.
func (m Model) applyTaskLaunchReady(msg taskLaunchReadyMsg) (tea.Model, tea.Cmd) {
	p := layerOf[*taskLaunchPopup](m)
	if p == nil || p.stage != launchPreparing {
		return m, nil // closed meanwhile
	}
	kind := taskKindLabel(p.launch.kind)
	if msg.err != nil {
		m = m.removeLayer(p)
		m.statusMsg = i18n.T("%s: %s", kind, msg.err.Error())
		return m, nil
	}
	if msg.cfg != nil {
		m.cfg = *msg.cfg
		m.statusMsg = i18n.T("Added %s to %s — edit there or in Settings → External tools", strings.Join(msg.added, ", "), msg.path)
	}
	p.key = msg.key
	p.choices = m.launchChoices(p.launch)
	if len(p.choices) == 0 {
		m = m.removeLayer(p)
		m.statusMsg = i18n.T("no %s agent configured (Settings → External tools)", kind)
		return m, nil
	}
	p.stage = launchChoose
	p.preselect(m)
	return m, nil
}

// launchChoices is TaskChoices minus what the dialog cannot run: commands
// asking for <user:…> input, and conflict commands whose when_op does not
// match the paused op.
func (m Model) launchChoices(l taskLaunch) []domain.TaskChoice {
	keep := func(cmds []config.ToolCommand) []config.ToolCommand {
		var out []config.ToolCommand
		for _, tc := range cmds {
			if f := newTemplateFill(tc.Command); f.needsInput() {
				continue
			}
			if tc.WhenOp != "" && tc.WhenOp != l.whenOp {
				continue
			}
			out = append(out, tc)
		}
		return out
	}
	var out []domain.TaskChoice
	for _, c := range domain.TaskChoices(m.cfg, l.kind, "tui") {
		c.Headless, c.Interactive = keep(c.Headless), keep(c.Interactive)
		if len(c.Headless)+len(c.Interactive) > 0 {
			out = append(out, c)
		}
	}
	return out
}

// choiceIdentity names an agent in the remembered choice.
func choiceIdentity(c domain.TaskChoice) string {
	if c.AgentID != "" {
		return "id:" + c.AgentID
	}
	return "name:" + c.Agent
}

// preselect puts the cursor on the remembered agent, mode and command, else
// on the first agent's first runnable row.
func (p *taskLaunchPopup) preselect(m Model) {
	p.agent = 0
	var want promptstate.TaskLaunch
	var have bool
	if m.promptStore != nil {
		want, have = m.promptStore.TaskLaunchChoice(string(p.launch.kind))
	}
	if have {
		for i, c := range p.choices {
			if choiceIdentity(c) == want.Agent {
				p.agent = i
			}
		}
	}
	p.rows = buildLaunchRows(p.choices[p.agent])
	p.sel = p.firstSelectable()
	if !have {
		return
	}
	for i, r := range p.rows {
		if !r.none && string(r.mode) == want.Mode && r.tc.Name == want.Command {
			p.sel = i
			return
		}
	}
	for i, r := range p.rows {
		if !r.none && string(r.mode) == want.Mode {
			p.sel = i
			return
		}
	}
}

// buildLaunchRows lays out one agent's rows: every mode, one row per
// command (variants), a greyed row for a mode without a command.
func buildLaunchRows(c domain.TaskChoice) []launchRow {
	var rows []launchRow
	for _, mode := range launchModes {
		cmds := c.Interactive
		if mode == launchHeadless {
			cmds = c.Headless
		}
		if len(cmds) == 0 {
			rows = append(rows, launchRow{mode: mode, none: true})
			continue
		}
		for _, tc := range cmds {
			_, err := taskLookPath(commandProgram(tc.Command))
			rows = append(rows, launchRow{mode: mode, tc: tc, missing: err != nil})
		}
	}
	return rows
}

// commandProgram is a command line's first shell word, unquoted.
func commandProgram(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return ""
	}
	if q := cmd[0]; q == '"' || q == '\'' {
		if end := strings.IndexByte(cmd[1:], q); end >= 0 {
			return cmd[1 : 1+end]
		}
		return cmd[1:]
	}
	if i := strings.IndexAny(cmd, " \t"); i >= 0 {
		return cmd[:i]
	}
	return cmd
}

func (p *taskLaunchPopup) firstSelectable() int {
	for i, r := range p.rows {
		if !r.none {
			return i
		}
	}
	return 0
}

// moveSel steps the cursor over selectable rows.
func (p *taskLaunchPopup) moveSel(dir int) {
	for i := p.sel + dir; i >= 0 && i < len(p.rows); i += dir {
		if !p.rows[i].none {
			p.sel = i
			return
		}
	}
}

// switchAgent cycles the agent, keeping the mode when the new agent has it.
func (p *taskLaunchPopup) switchAgent(dir int) {
	n := len(p.choices)
	if n < 2 {
		return
	}
	mode := p.rows[p.sel].mode
	p.agent = (p.agent + dir + n) % n
	p.rows = buildLaunchRows(p.choices[p.agent])
	p.sel = p.firstSelectable()
	for i, r := range p.rows {
		if !r.none && r.mode == mode {
			p.sel = i
			return
		}
	}
}

func (p *taskLaunchPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		return m, tea.Quit
	}
	switch p.stage {
	case launchPreparing:
		if key == "esc" {
			return m.cancelTaskLaunch(p)
		}
		return m, nil
	case launchApprove:
		switch key {
		case "enter", "y":
			row := p.rows[p.sel]
			m.rememberToolApproval(row.tc.Command)
			return m.submitTask(p, row)
		case "esc", "n":
			p.stage = launchChoose
		}
		return m, nil
	}
	switch key {
	case "esc":
		return m.cancelTaskLaunch(p)
	case "left", "h":
		p.switchAgent(-1)
	case "right", "l":
		p.switchAgent(+1)
	case "up", "k":
		p.moveSel(-1)
	case "down", "j":
		p.moveSel(+1)
	case "enter":
		return m.launchPick(p)
	}
	return m, nil
}

// cancelTaskLaunch closes the dialog; a conflict kind returns to the
// conflict window it was opened from.
func (m Model) cancelTaskLaunch(p *taskLaunchPopup) (Model, tea.Cmd) {
	m = m.removeLayer(p)
	if isConflictKind(p.launch.kind) {
		return startConflictProcess(m)
	}
	return m, nil
}

// launchPick runs the selected row: refused when its program is missing,
// else the choice is remembered and the approval gate comes first.
func (m Model) launchPick(p *taskLaunchPopup) (Model, tea.Cmd) {
	if p.sel < 0 || p.sel >= len(p.rows) || p.rows[p.sel].none {
		return m, nil
	}
	row := p.rows[p.sel]
	if row.missing {
		m.statusMsg = i18n.T("%s: program not found on PATH", row.tc.Name)
		return m, nil
	}
	if m.promptStore != nil {
		_ = m.promptStore.SetTaskLaunchChoice(string(p.launch.kind), promptstate.TaskLaunch{
			Agent: choiceIdentity(p.choices[p.agent]), Mode: string(row.mode), Command: row.tc.Name,
		})
	}
	if !m.toolCommandApproved(row.tc.Command) {
		p.stage = launchApprove
		return m, nil
	}
	return m.submitTask(p, row)
}

// submitTask closes the dialog and submits the task off the UI thread (the
// kind builders read the repository).
func (m Model) submitTask(p *taskLaunchPopup, row launchRow) (Model, tea.Cmd) {
	m = m.removeLayer(p)
	l := p.launch
	if row.mode == launchForeground && l.commitBox {
		// The docked console gets keys only with no layer on top.
		if cp := m.topCommitPopup(); cp != nil {
			m = m.removeLayer(cp)
		}
	}
	g := m.layout()
	cols, rows := consoleInner(g.rightW, g.boxH[panelCommits])
	cwd, _, _ := sessionPlace(m.currentWorktree)
	env, inbox := m.childEnv(), m.childInboxDir()
	svc, tc, mode := m.svc, row.tc, row.mode
	m.statusMsg = i18n.T("starting %s…", tc.Name)
	return m, func() tea.Msg {
		ctx := context.Background()
		var spec domain.TaskSpec
		var err error
		switch l.kind {
		case exttool.CatCommitMessage:
			spec, err = svc.CommitMessageTask(ctx, tc)
		case exttool.CatReview:
			spec, err = svc.ReviewTask(ctx, tc, l.review, "")
		case exttool.CatConflict, exttool.CatConflictComplete:
			spec, err = svc.ConflictTask(ctx, tc, l.kind == exttool.CatConflictComplete)
		default:
			err = fmt.Errorf("no task kind %q", l.kind)
		}
		if err != nil {
			return taskSubmittedMsg{launch: l, mode: mode, name: tc.Name, err: err}
		}
		spec.Env, spec.Cwd, spec.Cols, spec.Rows = env, cwd, cols, rows
		id := domain.Tasks().Submit(spec)
		return taskSubmittedMsg{id: id, launch: l, mode: mode, key: spec.Key, inbox: inbox, name: tc.Name}
	}
}

// applyTaskSubmitted wires a submitted task into the TUI: its inbox, its
// foreground console, the status line.
func (m Model) applyTaskSubmitted(msg taskSubmittedMsg) (Model, tea.Cmd) {
	if msg.err != nil {
		m.statusMsg = i18n.T("could not start %s: %s", msg.name, msg.err.Error())
		return m, nil
	}
	m = m.ensureTaskTrack()
	if msg.mode != launchHeadless {
		if msg.inbox != "" {
			m.taskTrack.inbox[msg.id] = msg.inbox
		}
		if msg.mode == launchForeground {
			m.taskTrack.fg[msg.id] = true
		}
	}
	m.statusMsg = i18n.T("started %s", msg.key)
	if info, ok := domain.Tasks().Get(msg.id); ok && info.State == domain.TaskQueued {
		m.statusMsg = i18n.T("queued %s", msg.key)
	}
	if msg.mode == launchHeadless && msg.launch.commitBox {
		return m.commitBoxWaitOn(msg.id)
	}
	return m, nil
}

// launchWaitLine says why a new task would wait ("" = it starts now).
func launchWaitLine(l domain.TaskLoad) string {
	switch {
	case l.SameKey > 0:
		return i18n.T("%d task(s) with this key ahead — this one will wait", l.SameKey)
	case l.Running >= l.Max:
		return i18n.T("%d tasks running — it will start when one finishes", l.Running)
	}
	return ""
}

func modeLabel(mode launchMode) string {
	switch mode {
	case launchBackground:
		return i18n.T("Interactive, background")
	case launchForeground:
		return i18n.T("Interactive, foreground")
	}
	return i18n.T("Headless")
}

func (p *taskLaunchPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	inner := popupInnerWidth(w)
	textW := popupTextWidth(inner)
	s := st()
	var lines []string
	switch p.stage {
	case launchPreparing:
		lines = []string{truncate(taskKindLabel(p.launch.kind)+" …", textW), "", i18n.T("[esc] cancel")}
	case launchApprove:
		row := p.rows[p.sel]
		shown := row.tc.Command
		if !isConflictKind(p.launch.kind) { // conflict ops resolve after writing their context file
			if r, err := template.ResolveCommand(row.tc.Command, nil, template.CmdCtx{Repo: m.currentWorktree, Range: p.launch.review.Range}); err == nil {
				shown = r
			}
		}
		lines = []string{truncate(i18n.T("Run this command?  (%s)", row.tc.Name), textW), "", approvalBoxView(shown, textW),
			"", i18n.T("[enter] approve and run  [esc] back")}
	default:
		lines = p.chooseLines(textW, s)
	}
	box := popupBox(inner, strings.Join(lines, "\n"))
	return overlayCenter(clipToHeight(below, h), box, w, h)
}

func (p *taskLaunchPopup) chooseLines(textW int, s *styles) []string {
	agentLine := i18n.T("Agent") + "  ◀ " + p.choices[p.agent].Agent + " ▶"
	count := fmt.Sprintf("%d/%d", p.agent+1, len(p.choices))
	if gap := textW - lipgloss.Width(agentLine) - lipgloss.Width(count); gap > 0 {
		agentLine += strings.Repeat(" ", gap) + count
	}
	lines := []string{truncate(p.key, textW), truncate(agentLine, textW), ""}
	modeW := 0
	for _, mode := range launchModes {
		modeW = max(modeW, lipgloss.Width(modeLabel(mode)))
	}
	for i, r := range p.rows {
		text := padRight(modeLabel(r.mode), modeW) + "   "
		if r.none {
			text += i18n.T("(no command for this agent)")
		} else {
			text += r.tc.Name
			if r.missing {
				text += "  " + i18n.T("not found")
			}
		}
		switch {
		case r.none:
			lines = append(lines, s.dim.Render(truncate("  "+text, textW)))
		case i == p.sel:
			lines = append(lines, s.selectedRow.Render(padRight(truncate("> "+text, textW), textW)))
		default:
			lines = append(lines, truncate("  "+text, textW))
		}
	}
	if wait := launchWaitLine(domain.Tasks().Load(p.key)); wait != "" {
		lines = append(lines, "", truncate(wait, textW))
	}
	return append(lines, "", truncate(i18n.T("[←/→] agent  [↑/↓] mode  [enter] run  [esc] cancel"), textW))
}
