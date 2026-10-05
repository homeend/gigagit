package tui

import (
	"context"
	"errors"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

// A review link (?review=<id>) lands on the REVIEW, not its commit: every
// entry point — the # paste, gg open's start-at, an agent's navigate — reaches
// steerNavigate, which sends it here. The review is looked up off the Update
// thread; a deleted one degrades to the plain landing of its address with a
// notice (the address still means something). A link moved onto another
// change never gets this far: domain.ResolveLink refuses it.

type reviewHintMsg struct {
	// svc is the repository the lookup ran against: a repo switch since
	// (reRoot replaces m.svc) makes the answer stale.
	svc    *domain.Service
	cmd    steer.Command
	commit string
	found  bool
	err    error
}

func (m Model) steerNavigateReview(c steer.Command) (Model, tea.Cmd) {
	svc := m.svc
	if svc == nil {
		return m, m.answerSteer(c, steerFail(c, "no repository is open"))
	}
	return m, func() tea.Msg {
		r, err := svc.Review(context.Background(), c.HintID)
		if errors.Is(err, domain.ErrReviewNotFound) {
			return reviewHintMsg{svc: svc, cmd: c}
		}
		return reviewHintMsg{svc: svc, cmd: c, commit: r.Commit, found: err == nil, err: err}
	}
}

func (m Model) onReviewHint(msg reviewHintMsg) (Model, tea.Cmd) {
	c := msg.cmd
	if msg.svc != m.svc {
		return m, m.answerSteer(c, steerFail(c, "the repository changed before the review opened"))
	}
	if !msg.found {
		plain := c
		plain.HintKind, plain.HintID = "", ""
		nm, cmd := m.steerNavigate(plain)
		if msg.err == nil {
			nm.statusMsg = i18n.T("That review no longer exists; opened its change")
		} else {
			nm.statusMsg = i18n.T("review: %s", msg.err.Error())
		}
		return nm, cmd
	}
	nm := m.steerToPanels()
	nm, open := nm.openReviewFrom(c.HintID, reviewTitle(shortHash(msg.commit)), model.Commit{Hash: msg.commit})
	return nm, tea.Batch(open, nm.answerSteer(c, steerOK(c, "opened review "+c.HintID)))
}
