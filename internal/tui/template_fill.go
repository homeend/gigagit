package tui

import (
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
}

func newTemplateFill(value string) templateFill {
	labels := template.UserLabels(value)
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
