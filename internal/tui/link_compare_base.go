package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/linknav"
	"github.com/homeend/gigagit/internal/model"
)

// The base rows of the compare dialog (spec §5.1). A side whose link is an
// UNBOUNDED POINT — a branch or tag tip, one commit's whole tree — gets a row
// offering the base that would bound it. The row is an OFFER: nothing about
// the link changes until the user presses enter ON the row, and what enter
// does is rewrite the link field into a link `gg compare` could be handed
// (model.Link.WithBase). The dialog holds no state a link cannot spell.

// baseSuggestedMsg answers one side's "what would bound this link". It names
// the TEXT it was asked about, not the side: the sides may have been swapped,
// or the field edited, while it was in flight.
type baseSuggestedMsg struct {
	text string
	sug  domain.BaseSuggestion
	err  error
}

func (m Model) suggestBaseCmd(text string) tea.Cmd {
	svc, statePath := m.svc, m.statePath
	return func() tea.Msg {
		l, err := model.ParseLink(text)
		if err != nil {
			return baseSuggestedMsg{text: text, err: err}
		}
		sug, err := svc.SuggestBase(context.Background(), l, linknav.Opts(statePath, svc))
		return baseSuggestedMsg{text: text, sug: sug, err: err}
	}
}

// linkText is the side's link as it would be submitted.
func (s *linkCompareSide) linkText() string { return strings.TrimSpace(s.input.Value()) }

// hasBase reports whether the base row exists: domain has answered for
// EXACTLY the text in the field, and said a base could bound it.
func (s *linkCompareSide) hasBase() bool {
	return s.sugFor != "" && s.sugFor == s.linkText() && s.sug.Kind != model.LinkBoundNone
}

// refresh re-derives a side's base row after its link text changed. The pure
// kind is the cheap gate (None is certain, so it costs no git); anything else
// is domain's to confirm — model.Link.BoundKind cannot tell a local-form file
// link from a whole tree.
func (s *linkCompareSide) refresh(m Model) tea.Cmd {
	text := s.linkText()
	if text == s.sugFor || text == s.asked {
		return nil
	}
	s.sug, s.sugFor, s.asked, s.baseDirty = domain.BaseSuggestion{}, "", "", false
	l, err := model.ParseLink(text)
	if err != nil || l.BoundKind() == model.LinkBoundNone {
		return nil
	}
	s.asked = text
	return m.suggestBaseCmd(text)
}

// suggested lands an answer on every side still holding the text it is for.
func (p *linkComparePopup) suggested(msg baseSuggestedMsg) {
	for i := range p.side {
		s := &p.side[i]
		if s.asked != msg.text || s.linkText() != msg.text {
			continue
		}
		s.asked = ""
		if msg.err != nil {
			continue // no row; the comparison itself will say what is wrong
		}
		s.sug, s.sugFor = msg.sug, msg.text
		s.base, s.baseDirty = newTextField(msg.sug.Base), false
	}
	p.normalizeFocus()
}

// normalizeFocus moves the focus off a base row that no longer exists, onto
// its own side's link field.
func (p *linkComparePopup) normalizeFocus() {
	for _, r := range p.rows() {
		if r == p.focus {
			return
		}
	}
	if p.focus.side() == 1 {
		p.focus = lcLink2
	} else {
		p.focus = lcLink1
	}
}

// baseLabel names the row by where its suggestion came from.
func (s *linkCompareSide) baseLabel() string {
	switch s.sug.Why {
	case "upstream":
		return i18n.T("base (upstream): ")
	case "trunk":
		return i18n.T("base (trunk): ")
	case "parent":
		return i18n.T("base (parent): ")
	}
	return i18n.T("base: ")
}

// completeBase is tab on a base row the user has typed into: a ref's base
// completes to the best-matching branch name. It reports whether it changed
// the field — tab then stays on the row instead of moving on.
func (s *linkCompareSide) completeBase(m Model) bool {
	if !s.baseDirty || s.sug.Kind != model.LinkBoundRef || m.namesABranch(s.base.Value()) {
		return false
	}
	ms := m.branchSuggestions(s.base.Value())
	if len(ms) == 0 {
		return false
	}
	s.base, s.baseDirty = newTextField(ms[0]), false
	return true
}

// applyBase is enter on the base row — the ONLY thing that rewrites a link
// field. `@ref:A` with base B becomes `@B...A` (target first); a commit with
// its parent becomes `@<parent>..<full sha>`. The field is then bounded, so
// the row goes away; editing it back to a point brings the row back.
func (s *linkCompareSide) applyBase() bool {
	l, err := model.ParseLink(s.linkText())
	if err != nil {
		return false
	}
	bounded, ok := l.WithBase(strings.TrimSpace(s.base.Value()), s.sug.Self)
	if !ok {
		s.err = i18n.T("that base cannot bound this link")
		return false
	}
	s.input = newTextField(bounded.String())
	s.sug, s.sugFor, s.asked, s.baseDirty, s.err = domain.BaseSuggestion{}, "", "", false, ""
	return true
}
