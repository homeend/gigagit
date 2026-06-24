package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
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

var cfLabels = [cfFieldCount]string{"Path:    ", "Author:  ", "Message: ", "Since:   ", "Until:   "}

// commitFilterPopup collects the non-branch feed filter. Opened with `\` on the
// Commits panel; Enter applies (sets m.commitFilter + reloads), Esc cancels.
type commitFilterPopup struct {
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
	inner := popupInnerWidth(w)
	cw := popupContentWidth(w)
	var b strings.Builder
	b.WriteString("Filter commits\n\n")
	for i := cfField(0); i < cfFieldCount; i++ {
		b.WriteString(viewField(cfLabels[i], p.fields[i], i == p.focus, cw))
		b.WriteString("\n")
	}
	b.WriteString("\n[enter] apply  [tab] next  [esc] cancel")
	box := modalStyle.Width(inner).Render(b.String()) + "\n"
	return overlayCenter(clipToHeight(below, h), box, w, h)
}

// Ensure commitFilterPopup satisfies the layer interface at compile time.
var _ layer = (*commitFilterPopup)(nil)
