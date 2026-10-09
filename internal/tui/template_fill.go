package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/template"
)

// templateFill collects values for a prefix's interactive <user:LABEL> tokens
// (in first-appearance order) so the call site can template.Resolve the prefix.
// Pure: it never touches the Model.
type templateFill struct {
	labels []string
	fields []textfield
	idx    int
	// scrolls is each field's first shown display line under viewWindow
	// (sized lazily; view ignores it).
	scrolls []int
}

func newTemplateFill(value string) templateFill {
	return newTemplateFillLabels(template.UserLabels(value))
}

// newTemplateFillLabels is newTemplateFill for a caller that already knows
// the labels (a text template scans with its own token rules).
func newTemplateFillLabels(labels []string) templateFill {
	f := templateFill{labels: labels, fields: make([]textfield, len(labels))}
	for i := range f.fields {
		f.fields[i] = newTextField("")
	}
	return f
}

func (f *templateFill) needsInput() bool { return len(f.labels) > 0 }

func (f *templateFill) inputs() map[string]string {
	out := make(map[string]string, len(f.labels))
	for i, l := range f.labels {
		out[l] = f.fields[i].Value()
	}
	return out
}

// handleKey routes one key. tab/enter advance; enter on the last field returns
// done=true; esc returns cancel=true. Other keys edit the focused field.
func (f *templateFill) handleKey(msg tea.KeyMsg) (done, cancel bool) {
	switch msg.Type {
	case tea.KeyEsc:
		return false, true
	case tea.KeyTab, tea.KeyEnter:
		if f.idx >= len(f.fields)-1 {
			return true, false
		}
		f.idx++
		return false, false
	default:
		if f.idx >= 0 && f.idx < len(f.fields) {
			f.fields[f.idx].HandleEditKey(msg)
		}
		return false, false
	}
}

func (f *templateFill) view(contentWidth int) []string {
	lines := make([]string, len(f.labels))
	for i, l := range f.labels {
		cursor := "  "
		if i == f.idx {
			cursor = "> "
		}
		lines[i] = viewField(cursor+l+": ", f.fields[i], i == f.idx, contentWidth)
	}
	return lines
}

// viewWindow is view for a window with room rows for the fields. A value
// that runs over many lines (a pasted stack trace) no longer draws them all:
// the focused field shows a window that follows its cursor in the rows the
// others leave, the other fields one line and a scroll marker, and on a
// terminal too short for all that the rows shown follow the focused field.
func (f *templateFill) viewWindow(contentWidth, room int) []string {
	room = max(1, room)
	if len(f.scrolls) != len(f.fields) {
		f.scrolls = make([]int, len(f.fields))
	}
	var lines []string
	focusTop, focusLen := 0, 0
	for i, l := range f.labels {
		cursor, maxLines := "  ", 2
		if i != f.idx {
			f.scrolls[i] = 0 // an unfocused value shows its first line
		} else {
			// The other fields keep their (at most two) rows on screen.
			cursor, maxLines = "> ", max(2, room-2*(len(f.fields)-1))
		}
		block := strings.Split(viewFieldWindow(cursor+l+": ", f.fields[i], i == f.idx, contentWidth, maxLines, &f.scrolls[i]), "\n")
		if i == f.idx {
			focusTop, focusLen = len(lines), len(block)
		}
		lines = append(lines, block...)
	}
	if len(lines) <= room {
		return lines
	}
	top := max(0, min(focusTop-max(0, room-focusLen)/2, len(lines)-room))
	return lines[top : top+room]
}

// multiLine reports whether the focused field holds more than one line.
func (f *templateFill) multiLine() bool {
	return f.idx >= 0 && f.idx < len(f.fields) && strings.ContainsRune(f.fields[f.idx].Value(), '\n')
}

// moveLines moves the focused field's cursor n lines down (up when n < 0).
func (f *templateFill) moveLines(n int) {
	if f.idx < 0 || f.idx >= len(f.fields) {
		return
	}
	for ; n > 0; n-- {
		f.fields[f.idx].Down()
	}
	for ; n < 0; n++ {
		f.fields[f.idx].Up()
	}
}
