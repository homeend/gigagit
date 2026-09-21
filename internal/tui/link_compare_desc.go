package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// The description row: a link field holds forty hex digits, and the history
// row it was picked from said "pair: Fix tests…". The words follow the link
// into the dialog — under its field — and a typed or pasted link gets the same
// ones: domain.DescribeLink, the ONE describer (the copied-link history, the
// compare view's title and gg web's dialog all read it).

// linkDescribedMsg answers "what is this link". Like baseSuggestedMsg it
// names the TEXT, not the side: the sides may swap while it is in flight.
type linkDescribedMsg struct{ text, desc string }

func (m Model) describeLinkCmd(text string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		return linkDescribedMsg{text: text, desc: describeLinkText(context.Background(), svc, text)}
	}
}

// refreshDesc asks for the field's description when it holds a link it has
// not been told about. Text that does not parse has none (the user is typing).
func (s *linkCompareSide) refreshDesc(m Model) tea.Cmd {
	text := s.linkText()
	if text == s.descFor || text == s.descAsked {
		return nil
	}
	s.desc, s.descFor, s.descAsked = "", "", ""
	if _, err := model.ParseLink(text); err != nil || m.svc == nil {
		return nil
	}
	s.descAsked = text
	return m.describeLinkCmd(text)
}

// described lands an answer on every side still holding the text it is for.
func (p *linkComparePopup) described(msg linkDescribedMsg) {
	for i := range p.side {
		s := &p.side[i]
		if s.descAsked != msg.text || s.linkText() != msg.text {
			continue
		}
		s.desc, s.descFor, s.descAsked = msg.desc, msg.text, ""
	}
}

// descLine is the row under a link field: the description, and where the link
// was copied from when it carries a landing hint. "" while there is none, or
// once the field no longer holds the text it was written for.
func (s *linkCompareSide) descLine() string {
	if s.desc == "" || s.descFor != s.linkText() {
		return ""
	}
	if o := linkOriginText(s.descFor); o != "" {
		return s.desc + " · " + o
	}
	return s.desc
}

// linkOriginText names the saved surface a hinted link was copied from. One
// literal key per kind: prose never rides a format arg.
func linkOriginText(text string) string {
	l, err := model.ParseLink(text)
	if err != nil {
		return ""
	}
	switch l.Hint.Kind {
	case "preview":
		return i18n.T("copied from a saved preview")
	case "bookmark":
		return i18n.T("copied from a saved bookmark")
	case "shelf":
		return i18n.T("copied from a saved shelf")
	case "stash":
		return i18n.T("copied from a saved stash")
	}
	return ""
}
