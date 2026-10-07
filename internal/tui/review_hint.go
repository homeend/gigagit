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
	// preview is the scope a preview review belongs to ("" otherwise).
	preview string
	found   bool
	err     error
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
		return reviewHintMsg{svc: svc, cmd: c, commit: r.Commit, preview: r.Preview, found: err == nil, err: err}
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
	back := model.Commit{Hash: msg.commit}
	if msg.preview != "" {
		back = model.Commit{} // a preview's review returns to the panels, never a commit (R5)
	}
	// A link with a file (and line) lands there once the review's files are
	// in; the landing answers the navigate, so this does not.
	var land *steer.Command
	if c.File != "" {
		l := c
		l.HintKind, l.HintID = "", ""
		land = &l
	}
	nm, open := nm.openReviewLanding(c.HintID, reviewTitle(shortHash(msg.commit)), back, nil, land)
	// An agent that just saved this review (gg review save, /gg-review) wrote
	// the store behind this process: re-read the counts — and the Previews
	// rows, where a preview review is a sub-row — so it shows on arrival.
	// The cache is dropped HERE, not by the srcNotes read: the two reads run
	// concurrently, and a Previews read that won the race would classify
	// the reviews from the stale counts (a mutex, no git).
	nm.svc.InvalidateNoteCounts()
	var counts, rows tea.Cmd
	nm, counts = nm.reloadSourcesCmd([]sourceKey{srcNotes}, reloadOpts{})
	if msg.preview != "" {
		nm, rows = nm.chainPreviewsRead()
	}
	if land != nil {
		return nm, tea.Batch(open, counts, rows)
	}
	return nm, tea.Batch(open, counts, rows, nm.answerSteer(c, steerOK(c, "opened review "+c.HintID)))
}
