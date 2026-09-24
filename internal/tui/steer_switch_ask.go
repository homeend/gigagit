package tui

import (
	"fmt"
	"path/filepath"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/steer"
)

// steerSwitchAsk is a worktree-bound command from a worktree gg is not
// showing (spec ruling 9: interactive, never intrusive). gg does not apply it
// to its own checkout and does not switch on its own: a notice asks. ONE
// slot — the latest request wins.
type steerSwitchAsk struct {
	cmd  steer.Command
	from string // who asks: the session's label, or "an agent"
}

// steerWorktreeBound reports whether c means a place in ONE checkout: a
// navigate to a file or diff, or a highlight. A commit reveal, a note step,
// focus and reload read the same in every worktree of the repo.
func steerWorktreeBound(c steer.Command) bool {
	switch c.Cmd {
	case "navigate":
		return c.File != "" || c.Target != nil
	case "highlight":
		return true
	}
	return false
}

// askSteerSwitch parks c behind the switch notice and answers the agent at
// once — it cannot wait for a person — with English protocol prose.
func (m Model) askSteerSwitch(c steer.Command) (Model, tea.Cmd) {
	m.steerAsk = &steerSwitchAsk{cmd: c, from: m.steerSender(c)}
	m = m.rebuildNotices()
	var blink tea.Cmd
	if !m.noticesUnread {
		m.blinkGen++
		blink = noticeBlinkCmd(m.blinkGen)
	}
	m.noticesUnread = true
	m.blinkOn = true
	reply := m.answerSteer(c, steerFail(c, fmt.Sprintf("gg is showing worktree %s; asked the user to switch to %s", m.snapshotWorktree, c.Worktree)))
	return m, tea.Batch(blink, reply)
}

// steerSender names who asks: the running session gg handed c's inbox and
// that runs in c's worktree, else a generic "an agent".
func (m Model) steerSender(c steer.Command) string {
	for _, info := range domain.Sessions().List() {
		if m.childInbox[info.ID] == c.From && samePathTUI(info.Dir, c.Worktree) {
			return info.Label
		}
	}
	return i18n.T("an agent")
}

// steerAskNotice renders the ask for the notice centre (nil = none).
func steerAskNotice(a *steerSwitchAsk, repoKey string) *notice {
	if a == nil {
		return nil
	}
	where := a.cmd.File
	if where == "" {
		where = a.cmd.Commit
	}
	if a.cmd.Line != nil {
		where += ":" + strconv.Itoa(a.cmd.Line.No)
	}
	base := filepath.Base(a.cmd.Worktree)
	c := a.cmd
	return &notice{
		id:      "steer_switch",
		repoKey: repoKey,
		title:   i18n.T("%s wants to show %s in %s", a.from, where, base),
		detail:  []string{i18n.T("gg is showing another worktree; nothing moves until you choose.")},
		actions: []noticeAction{
			{label: i18n.T("Switch to %s and show", base), run: func(m Model) (Model, tea.Cmd) {
				m.steerAsk = nil
				// Checked first, as switchToLink does: an unreachable
				// checkout is refused in place (guardedReRoot's message).
				if verdict, _ := checkSwitchTarget(guardStat, guardGOOS, c.Worktree); verdict != switchOK {
					nm, cmd := m.guardedReRoot(c.Worktree, false)
					return nm.(Model), cmd
				}
				nm, cmd := m.reRoot(c.Worktree)
				m = nm.(Model)
				if c.Cmd == "navigate" {
					replay := c
					replay.Wait, replay.Worktree, replay.From = false, "", ""
					m.startAtCmd = &replay
					m.startAtPending, m.startAtPreviewsSeen = true, true
				}
				return m, cmd
			}},
			{label: i18n.T("Ignore"), run: func(m Model) (Model, tea.Cmd) {
				m.steerAsk = nil
				return m, nil
			}},
		},
	}
}
