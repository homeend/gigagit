package tui

import (
	"errors"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
)

// stickyNoticeFor is how long a sticky status notice survives. An ordinary
// status message dies on the next keypress — fine for "staged 3 files", useless
// for "what you just opened no longer exists", which the user reads AFTER the
// key that would have wiped it. Ten seconds matches the web's toast.
const stickyNoticeFor = 10 * time.Second

// stickyExpiredMsg ends a sticky notice; gen-guarded so an old tick never
// clears a newer notice.
type stickyExpiredMsg struct{ gen int }

// stickyNotice puts text on the status line and keeps it there across
// keypresses until its tick fires (or another message replaces it — a fresh
// status always wins, the sticky one only refuses to be wiped by navigation).
func (m Model) stickyNotice(text string) (Model, tea.Cmd) {
	m.statusMsg = text
	m.stickyMsg = text
	m.stickyGen++
	gen := m.stickyGen
	return m, tea.Tick(stickyNoticeFor, func(time.Time) tea.Msg { return stickyExpiredMsg{gen: gen} })
}

// expireSticky handles stickyExpiredMsg.
func (m Model) expireSticky(msg stickyExpiredMsg) Model {
	if msg.gen != m.stickyGen {
		return m
	}
	if m.statusMsg == m.stickyMsg {
		m.statusMsg = ""
	}
	m.stickyMsg = ""
	return m
}

// entryGoneMsg reports that the stored entry a picker diff was opened for
// points at nothing any more (domain.BookmarkProbe said so). tag is the
// diffTag the loading view was opened under.
type entryGoneMsg struct {
	tag string
	err error
}

// entryGoneText is the one sentence for a dead entry — the words the web's
// toast shows too. ok is false for any other error.
func entryGoneText(err error) (string, bool) {
	// The commit case FIRST: a dead commit bookmark's EntryGoneError wraps a
	// CommitGoneError, and "commit" is prose — it belongs in the translated
	// format, never inside a %s argument.
	var cg *domain.CommitGoneError
	if errors.As(err, &cg) {
		sha := cg.SHA
		if len(sha) > 7 {
			sha = sha[:7]
		}
		return i18n.T("commit %s is no longer available", sha), true
	}
	var gone *domain.EntryGoneError
	if errors.As(err, &gone) {
		return i18n.T("%s is no longer available", gone.What), true
	}
	return "", false
}

// entryGone closes the loading diff the dead entry was opened in — it has
// nothing to show — which lands the user back on the switcher beneath it, and
// says why with a sticky notice.
func (m Model) entryGone(msg entryGoneMsg) (Model, tea.Cmd) {
	dv := m.diffLayer()
	if dv == nil || msg.tag != m.diffTag {
		return m, nil // closed, or a stale result
	}
	m = m.popLayer()
	text, ok := entryGoneText(msg.err)
	if !ok {
		text = msg.err.Error()
	}
	return m.stickyNotice(text)
}
