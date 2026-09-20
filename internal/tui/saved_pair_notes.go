package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

// Review notes on a commit pair.
//
// A pair's compare view becomes note-addressable the same way a merge
// preview's does — m.filesPreviewSet is armed, and every gate that reads it
// (the diff stamp, the ◆N badges, }/{, c, the old-side refusals) then works
// unchanged. What differs is HOW it is armed: by a message of its own, gated
// on the two commits the view shows, so it does not care which opener built
// the view (a saved row, a steered link landing, the # paste).

// pairNotesMsg is a commit pair's note scope, resolved off the UI thread. A
// zero set means the scope could not be built; the handler then does nothing.
type pairNotesMsg struct {
	a, b   string
	set    domain.PreviewNoteSet
	counts map[string]int
	gen    int
}

// pairNotesCmd resolves the scope for commits a..b. Build it AFTER
// openCompareFiles returns: that call runs closeFilesView, which bumps
// previewGen, and a command stamped before it would be dropped as stale.
func (m Model) pairNotesCmd(a, b string) tea.Cmd {
	svc, gen := m.svc, m.previewGen
	if svc == nil {
		return nil
	}
	return func() tea.Msg {
		ctx := context.Background()
		msg := pairNotesMsg{a: a, b: b, gen: gen}
		set, err := svc.PairNotes(ctx, a, b)
		if err != nil || !set.OK() {
			return msg
		}
		msg.set = set
		msg.counts, _, _ = svc.PreviewNoteCounts(ctx, set)
		return msg
	}
}

// showsCommitPair reports whether the files view on screen compares commit a
// with commit b. It reads the ENDPOINTS, never m.compareTag: another opener
// (a link-shaped compare) tags the same two commits differently.
func (m Model) showsCommitPair(a, b string) bool {
	if m.filesView == nil || !m.inCompareMode() {
		return false
	}
	l, r := m.filesLeft, m.filesRight
	return l.Kind() == model.EndpointCommit && r.Kind() == model.EndpointCommit &&
		l.Hash() == a && r.Hash() == b
}

// handlePairNotesMsg arms the scope on the view it was resolved for.
func (m Model) handlePairNotesMsg(msg pairNotesMsg) (Model, tea.Cmd) {
	if msg.gen != m.previewGen || !msg.set.OK() || !m.showsCommitPair(msg.set.Base, msg.set.Tip) {
		return m, nil // the view was closed or replaced while this resolved
	}
	if m.previewOpen != nil {
		// A merge preview over the same two commits owns the scope: its set
		// carries the branch names its links and steer targets are built from.
		return m, nil
	}
	set := msg.set
	m.filesPreviewSet = &set
	if msg.counts != nil { // a failed counts read keeps the last known badges
		m.filesPreviewCounts = msg.counts
	}
	// A file diff may already be open over this view — a steered landing opens
	// it the moment the file list arrives, which can beat this message. It
	// was opened unstamped, so its notes are inert: stamp it and load them.
	// The diff tag is the proof it belongs to THIS compare (an added file's
	// diff is one-sided, so v.compare cannot be the test).
	v := m.diffLayer()
	if v == nil || v.noteAddr.Path != "" {
		return m, nil
	}
	prefix := "cmp:" + m.filesLeft.CacheTag() + ":" + m.filesRight.CacheTag() + ":"
	path, found := strings.CutPrefix(m.diffTag, prefix)
	if !found || path == "" {
		return m, nil
	}
	v.previewSet = m.filesPreviewSet
	v.noteAddr = model.FileAddress{State: model.StateCommitted, Commit: set.Tip, Path: path}
	return m, m.loadNotesCmd()
}

// pairNotesRefreshCmd re-reads the open pair's scope after the notes store
// changed: the badges count that store. nil when no pair scope is armed. A
// pair is frozen, so unlike a merge preview there is nothing to re-resolve and
// nothing that can have "moved" — only the counts change.
func (m Model) pairNotesRefreshCmd() tea.Cmd {
	s := m.filesPreviewSet
	if s == nil || !s.IsPair() || m.filesView == nil {
		return nil
	}
	return m.pairNotesCmd(s.Base, s.Tip)
}

// scopeLinkFor is the link to a place inside a note scope: a merge preview's
// names its BRANCH pair (it travels, and follows the tips), a commit pair's
// names its two frozen shas. The two scopes can cover the very same commits,
// so the kind — never the commits — picks the grammar.
func (m Model) scopeLinkFor(set *domain.PreviewNoteSet, path string, line int) (string, bool) {
	if set.IsPair() {
		return m.pairFileLinkFor(set.Base, set.Tip, path, line)
	}
	return m.previewLinkFor(set.Source, set.Target, path, line)
}

// steeredPairNotesCmd is pairNotesCmd for a link-shaped landing: nil unless
// the command that opened the view was a change-set navigate and the view
// really shows its two commits. Only a PAIR navigate arms — a generic link
// compare of two far-apart points would pay a rev-list over everything between
// them for a scope nobody asked for.
func (m Model) steeredPairNotesCmd(c steer.Command) tea.Cmd {
	if c.Target == nil || c.Target.State != "pair" {
		return nil
	}
	l, r := m.filesLeft, m.filesRight
	if l.Kind() != model.EndpointCommit || r.Kind() != model.EndpointCommit {
		return nil
	}
	return m.pairNotesCmd(l.Hash(), r.Hash())
}
