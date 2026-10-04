package tui

// What an agent session is doing — working, idle, needs input, stalled, or
// what it reported (reported / done) —
// on the Worktrees/Branches sub-rows, the ctrl+\ popup rows and the console
// title, and the status-line notices when a session wants the user
// (domain.SessionStates). The TUI keeps its own subscription and notice
// cursor: the page it hosts reads the same watcher through its own.

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
)

// activityWatch is the TUI's subscription to the state watcher, on a
// pointer field so the value-receiver Model shares it (the sessionWatch
// precedent).
type activityWatch struct {
	ch     <-chan struct{}
	cancel func()
}

type sessionActivityMsg struct{}

func seqPtr(n uint64) *uint64 { return &n }

// activityText is the human label: "working 7m" · "idle 3m" · "needs
// input" · "stalled · …"; "" when gg cannot tell. Ages tick from Since.
func activityText(a domain.SessionActivity, now time.Time) string {
	label := ""
	switch a.State {
	case domain.ActivityWorking:
		label = i18n.T("working %s", formatElapsed(now.Sub(a.Since)))
	case domain.ActivityIdle:
		label = i18n.T("idle %s", formatElapsed(now.Sub(a.Since)))
	case domain.ActivityQuestion:
		label = i18n.T("needs input")
	}
	if a.Stalled {
		if label == "" {
			label = i18n.T("no output")
		}
		return i18n.T("stalled · %s", label)
	}
	return label
}

// activityAttn: the session wants the user — a question, or a stall.
func activityAttn(a domain.SessionActivity) bool {
	return a.State == domain.ActivityQuestion || a.Stalled
}

// reportText is the badge of an unanswered agent_report: "reported 2m", or
// "done 2m" when the agent called it final (ages from the report).
func reportText(r domain.AgentReport, now time.Time) string {
	if r.Final {
		return i18n.T("done %s", formatElapsed(now.Sub(r.At)))
	}
	return i18n.T("reported %s", formatElapsed(now.Sub(r.At)))
}

// sessionBadge is a session row's word and whether it wears the attention
// colour: a question first (the user must act), then an unanswered report,
// then the activity.
func sessionBadge(id domain.SessionID, now time.Time) (string, bool) {
	a, aok := domain.SessionActivityOf(id)
	if aok && a.State == domain.ActivityQuestion {
		return activityText(a, now), true
	}
	if rep, ok := domain.SessionReportOf(id); ok {
		return reportText(rep, now), true
	}
	if aok {
		return activityText(a, now), activityAttn(a)
	}
	return "", false
}

// sessionActivityText is the label for a session row, "" when unknown.
func sessionActivityText(id domain.SessionID) string {
	text, _ := sessionBadge(id, time.Now())
	return text
}

// sessionReportLine is the report's first line for the wide popup row; ""
// once someone answered it, and while a question has the row.
func sessionReportLine(id domain.SessionID) string {
	if a, ok := domain.SessionActivityOf(id); ok && a.State == domain.ActivityQuestion {
		return ""
	}
	if rep, ok := domain.SessionReportOf(id); ok {
		return domain.ReportFirstLine(rep.Text)
	}
	return ""
}

// activityNoticeText is the status-line sentence for one notice.
func activityNoticeText(n domain.ActivityNotice) string {
	wt := shortWorktreeName(n.Dir)
	switch n.Kind {
	case "question":
		return i18n.T("%s in %s needs your input", n.Label, wt)
	case "idle":
		return i18n.T("%s in %s finished its turn — idle", n.Label, wt)
	case "report":
		return i18n.T("%s in %s reports: %s", n.Label, wt, n.Text)
	default:
		if n.Spinning {
			return i18n.T("%s in %s has shown only its spinner for %s — stalled?", n.Label, wt, formatElapsed(n.Quiet))
		}
		return i18n.T("%s in %s has printed nothing for %s — stalled?", n.Label, wt, formatElapsed(n.Quiet))
	}
}

// screenRulesWarning: a command's screen_* lists do not compile; the
// built-in rules apply.
func screenRulesWarning(name string) string {
	return i18n.T("screen rules of %s are invalid — the built-in rules apply", name)
}

// startNote is a start's status line: the worktree's note (place) and the
// command's invalid screen rules.
func startNote(place string, tc config.ToolCommand) string {
	if domain.SessionRulesWarning(tc) == "" {
		return place
	}
	if place == "" {
		return screenRulesWarning(tc.Name)
	}
	return place + " · " + screenRulesWarning(tc.Name)
}

// waitActivityCmd blocks until the watcher signals a change or a notice.
// nil in quiet mode (a never-ending command) and for a Model literal.
func (m Model) waitActivityCmd() tea.Cmd {
	if m.quiet || m.actWatch == nil {
		return nil
	}
	if m.actWatch.ch == nil {
		m.actWatch.ch, m.actWatch.cancel = domain.SessionStates().Subscribe()
	}
	ch := m.actWatch.ch
	return func() tea.Msg {
		<-ch
		return sessionActivityMsg{}
	}
}

// onSessionActivity: the rows re-derive on render; the notices since the
// cursor land on the status line (the newest wins), except one about the
// session the focused console shows — the user is looking at it.
func (m Model) onSessionActivity() (Model, tea.Cmd) {
	if m.actSeq == nil || m.quiet {
		return m, m.waitActivityCmd()
	}
	m, check := m.fileReportTours() // a new report becomes (or replaces) its tour
	for _, n := range domain.SessionStates().Notices(*m.actSeq) {
		*m.actSeq = n.Seq
		if m.console != nil && m.console.focused && m.console.id == n.ID {
			continue
		}
		m.statusMsg = activityNoticeText(n)
	}
	return m, tea.Batch(m.waitActivityCmd(), check)
}

// sessionDecorators colours the Worktrees/Branches sub-rows whose session
// wants the user. idx are the display indices renderPanel is given.
func (m Model) sessionDecorators(p panel, idx []int) []rowDecorator {
	var sessAt func(i int) (domain.SessionID, bool)
	switch p {
	case panelWorktrees:
		ents := m.worktreeEntries()
		sessAt = func(i int) (domain.SessionID, bool) {
			if i < 0 || i >= len(ents) || ents[i].sess == "" {
				return "", false
			}
			return ents[i].sess, true
		}
	case panelBranches:
		ents := m.branchEntries()
		sessAt = func(i int) (domain.SessionID, bool) {
			if i < 0 || i >= len(ents) || ents[i].sess == "" {
				return "", false
			}
			return ents[i].sess, true
		}
	default:
		return nil
	}
	var decos []rowDecorator
	now := time.Now()
	for j, i := range idx {
		id, ok := sessAt(i)
		if !ok {
			continue
		}
		if _, attn := sessionBadge(id, now); attn {
			if decos == nil {
				decos = make([]rowDecorator, len(idx))
			}
			decos[j] = func(visible string, hscroll, visualLine int) string { return st().activityAttn.Render(visible) }
		}
	}
	return decos
}
