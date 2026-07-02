package tui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

// The notification center: cheap health checks run on every repo load
// (domain.RepoHealth); unresolved findings become notices — a blinking red
// status segment plus the ! dialog whose actions fix the finding through
// real engine ops. Notice lifecycle: unread → read (opening the dialog) →
// dismissed for the session ("Not now"; re-evaluated next load) or never for
// this repo (persisted via promptstate.DismissNotice, keyed by common dir).

// notice is one surfaced health recommendation.
type notice struct {
	id      string   // stable dismissal key (persisted on "never")
	repoKey string   // git common dir — the promptstate dismissal scope
	title   string   // one-line list entry
	detail  []string // body lines shown above the actions
	actions []noticeAction
}

// noticeAction is one dialog choice. run nil = close-only ("Not now");
// never additionally persists the per-repo dismissal.
type noticeAction struct {
	label string
	run   func(Model) (Model, tea.Cmd)
	never bool
}

// noticeCommitGraph is the commit-graph recommendation's stable id.
const noticeCommitGraph = "commit_graph_recommend"

// bigRepoPackBytes is the pack-size floor for "big repo": below it the
// commit-graph win doesn't matter enough to nag about.
const bigRepoPackBytes = 100 << 20

// Blink = style alternation between these two on a dedicated tick;
// terminal-native blink escapes are unreliable.
var (
	noticeHotStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	noticeDimStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("124"))
)

// repoHealthMsg carries one background health read; gen guards repo switches.
type repoHealthMsg struct {
	gen    int
	health model.RepoHealth
	err    error
}

// repoHealthCmd reads repo health off the UI thread (startup, reRoot, and
// whenever Settings opens so its Commit-graph row shows fresh state).
func (m Model) repoHealthCmd(gen int) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		h, err := svc.RepoHealth(context.Background())
		return repoHealthMsg{gen: gen, health: h, err: err}
	}
}

// noticeBlinkMsg flips the blink phase while unread notices exist. gen ties
// the tick to the arm that scheduled it — a stale gen (superseded by a later
// arm, e.g. across a reRoot or an unread→read→new-notice flip within one
// 800ms window) is dropped instead of re-arming a second, parallel lane.
type noticeBlinkMsg struct{ gen int }

// noticeBlinkCmd schedules the next blink flip (~800ms; only re-armed while
// unread notices exist, so the tick self-stops).
func noticeBlinkCmd(gen int) tea.Cmd {
	return tea.Tick(800*time.Millisecond, func(time.Time) tea.Msg { return noticeBlinkMsg{gen: gen} })
}

// applyRepoHealth is the repoHealthMsg Update case: store the snapshot,
// rebuild the notice list (filtered by persisted + session dismissals), and
// start blinking only when a genuinely NEW notice id appeared — a mid-session
// re-read carrying the same ids must not re-blink.
func (m Model) applyRepoHealth(msg repoHealthMsg) (Model, tea.Cmd) {
	if msg.gen != m.noticeGen {
		return m, nil // stale: a repo switch superseded this read
	}
	if msg.err != nil {
		return m, nil // best-effort: health never surfaces errors in the UI
	}
	m.repoHealth = msg.health
	m.repoHealthKnown = true

	var dismissed map[string]bool
	if m.promptStore != nil {
		dismissed = m.promptStore.DismissedNotices(msg.health.GitCommonDir)
	}
	prev := make(map[string]bool, len(m.notices))
	for _, n := range m.notices {
		prev[n.id] = true
	}
	var next []notice
	if n := commitGraphNotice(msg.health); n != nil && !dismissed[n.id] && !m.noticeSessionDismissed[n.id] {
		next = append(next, *n)
	}
	m.notices = next
	var cmd tea.Cmd
	for _, n := range m.notices {
		if !prev[n.id] {
			if !m.noticesUnread {
				m.blinkGen++
				cmd = noticeBlinkCmd(m.blinkGen)
			}
			m.noticesUnread = true
			m.blinkOn = true
			break
		}
	}
	return m, cmd
}

// commitGraphNotice fires when the repo is big (pack ≥ bigRepoPackBytes), has
// no commit-graph file/chain, and fetch.writeCommitGraph is unset — the case
// where one keystroke makes commit browsing ~10× faster.
func commitGraphNotice(h model.RepoHealth) *notice {
	if h.PackBytes < bigRepoPackBytes || h.HasCommitGraph || h.WriteCommitGraphSet {
		return nil
	}
	return &notice{
		id:      noticeCommitGraph,
		repoKey: h.GitCommonDir,
		title:   "Commit browsing can be ~10× faster in this repo",
		detail: []string{
			fmt.Sprintf("This repo is big (%.0f MB of packs) and has no commit-graph file,", float64(h.PackBytes)/(1<<20)),
			"so ordered commit walks (the Commits panel's paging) re-walk history",
			"every time. Writing one takes a moment and git keeps it fresh when",
			"fetch.writeCommitGraph is on.",
		},
		actions: []noticeAction{
			{label: "Write commit-graph now + keep it fresh (fetch.writeCommitGraph=true)",
				run: Model.startCommitGraphWriteAndEnable},
			{label: "Enable auto-refresh only (graph appears on next fetch/gc)",
				run: func(m Model) (Model, tea.Cmd) {
					m.refreshHealthAfterOp = true
					return m.startOp(engine.SetGitConfig{Key: "fetch.writeCommitGraph", Value: "true"})
				}},
			{label: "Not now (ask again next load)"},
			{label: "Never for this repo", never: true},
		},
	}
}

// startCommitGraphWriteAndEnable is THE write+enable code path, shared by the
// notice's first action and the Settings "Commit-graph" row: write the graph
// now, then (chained on success in opFinishedMsg) enable auto-refresh.
func (m Model) startCommitGraphWriteAndEnable() (Model, tea.Cmd) {
	m.pendingNoticeConfig = &engine.SetGitConfig{Key: "fetch.writeCommitGraph", Value: "true"}
	m.refreshHealthAfterOp = true
	return m.startOp(engine.WriteCommitGraph{})
}

// removeNotice drops one notice from the session list.
func (m Model) removeNotice(id string) Model {
	var next []notice
	for _, n := range m.notices {
		if n.id != id {
			next = append(next, n)
		}
	}
	m.notices = next
	return m
}

// noticeSegment renders the status-bar segment: red + phase-alternating while
// unread, calm plain text once read, "" when there is nothing to say (or the
// conflict process owns the screen).
func (m Model) noticeSegment() string {
	n := len(m.notices)
	if n == 0 || m.proc != nil {
		return ""
	}
	seg := fmt.Sprintf("! %d notice", n)
	if n != 1 {
		seg += "s"
	}
	seg += " — press [!]"
	if !m.noticesUnread {
		return seg
	}
	if m.blinkOn {
		return noticeHotStyle.Render(seg)
	}
	return noticeDimStyle.Render(seg)
}
