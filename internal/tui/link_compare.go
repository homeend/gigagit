package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/linknav"
	"github.com/homeend/gigagit/internal/model"
)

// A LINK comparison is SET-shaped: domain.CompareLinks hands back the two file
// sets and the changed-file list together, so this view opens with its list
// already in hand. The endpoint-shaped openCompareFiles cannot show one — it
// loads through CompareFiles, which refuses a change-set, and it knows nothing
// of a member whose bytes live somewhere other than its set's endpoint (a
// `-u` stash's untracked files, read from the stash's third parent).

// linkCompareTag identifies a link comparison by its two TEXTS, never by the
// endpoints they resolve to: gg://r@sha and gg://r/f.go@sha share an endpoint,
// so an endpoint tag would call the second "already showing" and do nothing.
func linkCompareTag(left, right string) string { return "links:" + left + "\x00" + right }

// linkCompareLoadedMsg carries a finished comparison (or its failure) back to
// the UI thread, tagged so a superseded or cancelled load is dropped.
type linkCompareLoadedMsg struct {
	tag                 string
	c                   domain.LinkComparison
	leftDesc, rightDesc string
	err                 error
}

// pointLinkFor builds the gg:// address of ONE commit's whole tree. A full sha
// only, like every producer: anything shorter did not come from a commit read.
func (m Model) pointLinkFor(sha string) (string, bool) {
	if len(sha) < 40 {
		return "", false
	}
	repo, ok := m.linkRepoFor("")
	if !ok {
		return "", false
	}
	l := model.Link{
		Repo:   repo,
		Target: model.LinkTarget{State: model.StateCommitted, Commit: sha},
		Side:   model.NoteSideNew,
	}
	return l.String(), true
}

// startLinkCompare begins comparing two link texts. A nil cmd means the view
// already shows exactly this comparison — re-running it would only blank and
// repaint identical content.
func (m Model) startLinkCompare(left, right string) (Model, tea.Cmd) {
	return m.startLinkCompareAfter(left, right, nil)
}

// startLinkCompareAfter is startLinkCompare with a check that runs first, off
// the UI thread; its error is the comparison's. The entry cross-compare uses
// it to keep saying "this bookmark's commit is gone" in gg's own words, which
// the door — knowing only links — would report as a bare unknown revision.
func (m Model) startLinkCompareAfter(left, right string, pre func(context.Context) error) (Model, tea.Cmd) {
	tag := linkCompareTag(left, right)
	if m.filesView != nil && m.inCompareMode() && m.compareTag == tag {
		return m, nil
	}
	m.linkCompareWant = tag
	svc, statePath := m.svc, m.statePath
	return m, func() tea.Msg {
		ctx := context.Background()
		if pre != nil {
			if err := pre(ctx); err != nil {
				return linkCompareLoadedMsg{tag: tag, err: err}
			}
		}
		c, err := svc.CompareLinks(ctx, left, right, linknav.Opts(statePath, svc))
		if err != nil {
			return linkCompareLoadedMsg{tag: tag, err: err}
		}
		return linkCompareLoadedMsg{tag: tag, c: c,
			leftDesc: describeLinkText(ctx, svc, c.LeftText), rightDesc: describeLinkText(ctx, svc, c.RightText)}
	}
}

// describeLinkText is a link text's one-line description for a title; text
// that does not parse (it cannot, having just compared) stands for itself.
func describeLinkText(ctx context.Context, svc *domain.Service, text string) string {
	l, err := model.ParseLink(text)
	if err != nil {
		return text
	}
	return svc.DescribeLink(ctx, l)
}

// loadedLinkCompare routes a finished load: dropped when stale, reported when
// failed, else the view opens. A parked steer navigate is answered from HERE,
// in both outcomes — the view does not exist until this message lands, so
// answering at dispatch would claim an open that might still fail.
func (m Model) loadedLinkCompare(msg linkCompareLoadedMsg) (Model, tea.Cmd) {
	if msg.tag == "" || msg.tag != m.linkCompareWant {
		return m, nil // superseded by a newer open, or cancelled
	}
	// A failed compare must be retryable: clearing the want is what lets the
	// SAME pair be asked again.
	m.linkCompareWant = ""
	m = m.closeCompareLoading(msg.tag) // the Previews marks' "comparing…" popup, if it asked
	steered := m.pendingSteer != nil && m.pendingSteer.stage == steerStageCompare && m.pendingSteer.tag == msg.tag
	if !steered {
		// The dialog that asked takes its own answer: a failure belongs under
		// one of its fields, and success parks it behind the view.
		if p, ok := m.topLayer().(*linkComparePopup); ok && p.busy {
			return p.loaded(m, msg)
		}
	}
	if msg.err != nil {
		if steered {
			return m.failPending("the compare failed: " + msg.err.Error())
		}
		// A bookmark is a pointer: its commit gone is not a compare failure
		// but "this entry is dead", said the sticky way (entryCompareMsg's rule).
		if text, ok := entryGoneText(msg.err); ok {
			return m.stickyNotice(text)
		}
		m.statusMsg = i18n.T("compare: %s", msg.err.Error())
		return m, nil
	}
	if m.topLayer() != nil {
		// Asked for from a popup (a switcher picking its second side): the
		// files view is not a layer, so the popup is parked, never drawn over
		// it — and comes back when the view closes.
		return m.handOffToFilesView(func(m Model) (Model, tea.Cmd) { return m.openLinkCompare(msg) })
	}
	return m.openLinkCompare(msg)
}

// openLinkCompare opens the files view in compare mode over a finished link
// comparison. filesLeft/filesRight are each set's OWN endpoint (history/blame
// context and the session snapshot read them); the per-file byte sources come
// from filesSets, through compareSides.
func (m Model) openLinkCompare(msg linkCompareLoadedMsg) (Model, tea.Cmd) {
	left, right := msg.c.Left.Endpoint(), msg.c.Right.Endpoint()
	if !endpointComparable(left) || !endpointComparable(right) {
		m.statusMsg = i18n.T("no commit selected to compare against")
		if ps := m.pendingSteer; ps != nil && ps.stage == steerStageCompare && ps.tag == msg.tag {
			return m.failPending("the compare's sides could not be identified")
		}
		return m, nil
	}
	m = m.beginFilesView()
	m.filesView = &contentPopup{lines: commitFileLines(msg.c.Files)}
	m.filesTitle = msg.leftDesc + " ↔ " + msg.rightDesc
	m.filesContext = m.filesTitle
	m.filesMode = filesModeCompare
	m.filesLeft, m.filesRight = left, right
	m.filesHash = compareFilesHash(left, right)
	c := msg.c
	m.filesSets = &c
	m.compareTag = msg.tag
	m.filesTreeFocused = true
	if ps := m.pendingSteer; ps != nil && ps.stage == steerStageCompare && ps.tag == msg.tag {
		// A steered change-set landing (@<a>..<b>) is a NOTE SCOPE: its review
		// notes ride along (saved_pair_notes.go). Built here, after
		// beginFilesView bumped the generation the command stamps.
		notes := m.steeredPairNotesCmd(ps.cmd)
		nm, cmd := m.drainPendingCompare()
		if notes != nil {
			cmd = tea.Batch(cmd, notes)
		}
		return nm, cmd
	}
	return m, nil
}

// compareSides is the pair of endpoints ONE row's bytes are read from. An
// endpoint compare reads every row from the view's two endpoints; a link
// compare asks each set, because a member may have its own source. The Differ
// cache key is built from what this returns, so a diff read from one source is
// never served under another's key.
func (m Model) compareSides(l contentLine) (left, right model.Endpoint) {
	if m.filesSets == nil {
		return m.filesLeft, m.filesRight
	}
	oldP := l.path
	if l.oldPath != "" {
		oldP = l.oldPath
	}
	return m.filesSets.Left.Source(oldP), m.filesSets.Right.Source(l.path)
}
