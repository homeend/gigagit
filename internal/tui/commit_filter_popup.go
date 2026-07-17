package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
)

type cfField int

const (
	cfPath cfField = iota
	cfAuthor
	cfGrep
	cfSince
	cfUntil
	cfFieldCount
)

// cfLabel translates a field's aligned label at render time (not a package
// var — a var initializer would freeze the English text at package init,
// before any language loads; see cfg language switching in Settings).
func cfLabel(f cfField) string {
	switch f {
	case cfPath:
		return i18n.T("Path:    ")
	case cfAuthor:
		return i18n.T("Author:  ")
	case cfGrep:
		return i18n.T("Message: ")
	case cfSince:
		return i18n.T("Since:   ")
	case cfUntil:
		return i18n.T("Until:   ")
	}
	return ""
}

// commitFilterPopup collects the non-branch feed filter. Opened with `\` on the
// Commits panel; Enter applies (sets m.commitFilter + reloads), Esc cancels.
type commitFilterPopup struct {
	popupMax
	fields [cfFieldCount]textfield
	focus  cfField
}

// newCommitFilterPopup prefills the popup from the active filter so re-opening
// edits the current filter rather than starting blank.
func newCommitFilterPopup(cur commitFilterFields) *commitFilterPopup {
	p := &commitFilterPopup{}
	var path string
	if len(cur.Paths) > 0 {
		path = cur.Paths[0]
	}
	p.fields[cfPath] = newTextField(path)
	p.fields[cfAuthor] = newTextField(cur.Author)
	p.fields[cfGrep] = newTextField(cur.Grep)
	p.fields[cfSince] = newTextField(cur.Since)
	p.fields[cfUntil] = newTextField(cur.Until)
	return p
}

func (p *commitFilterPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		return m.popLayer(), nil
	case "enter":
		m = m.popLayer()
		m.commitFilter = p.collect()
		return m.startFeedReload()
	case "ctrl+r":
		// Clear every field — remove the commit filter being edited — and
		// restore the full feed.
		m = m.popLayer()
		reload := m.commitFilter.filtered()
		m.commitFilter = commitFilterFields{}
		if reload {
			return m.startFeedReload()
		}
		return m, nil
	case "tab", "down":
		p.focus = (p.focus + 1) % cfFieldCount
		return m, nil
	case "shift+tab", "up":
		p.focus = (p.focus + cfFieldCount - 1) % cfFieldCount
		return m, nil
	default:
		// Route the key to the focused field. HandleEditKey returns false for
		// keys it ignores; we swallow either way so nothing leaks to globals.
		p.fields[p.focus].HandleEditKey(msg)
		return m, nil
	}
}

// collect builds the filter from the field values (empty fields contribute no
// axis; an all-empty apply clears the filter).
func (p *commitFilterPopup) collect() commitFilterFields {
	f := commitFilterFields{
		Author: p.fields[cfAuthor].Value(),
		Grep:   p.fields[cfGrep].Value(),
		Since:  p.fields[cfSince].Value(),
		Until:  p.fields[cfUntil].Value(),
	}
	if path := p.fields[cfPath].Value(); path != "" {
		f.Paths = []string{path}
	}
	return f
}

func (p *commitFilterPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	inner := popupResolveWidth(w, p.maximized, popupInnerWidth(w))
	cw := popupContentWidth(w)
	var b strings.Builder
	b.WriteString(i18n.T("Filter commits") + "\n\n")
	for i := cfField(0); i < cfFieldCount; i++ {
		b.WriteString(viewField(cfLabel(i), p.fields[i], i == p.focus, cw))
		b.WriteString("\n")
	}
	b.WriteString("\n" + i18n.T("[enter] apply  [tab] next  [ctrl+r] clear all  [esc] cancel"))
	box := modalStyle.Width(inner).Render(b.String()) + "\n"
	return overlayCenter(clipToHeight(below, h), box, w, h)
}

// Ensure commitFilterPopup satisfies the layer interface at compile time.
var _ layer = (*commitFilterPopup)(nil)
