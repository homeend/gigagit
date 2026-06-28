package tui

import (
	"github.com/homeend/gigagit/internal/model"
)

// tagKindMark is the row prefix glyph: ● annotated, ○ lightweight.
func tagKindMark(t model.Tag) string {
	if t.Annotated {
		return "●"
	}
	return "○"
}

// tagRows renders one display row per tag: "<kind> <name>  <short-target>  <subject>  [▲]".
// ▲ is appended when the tag is known to exist on the default remote (m.remoteTagNames).
func (m Model) tagRows() []string {
	rows := make([]string, len(m.tags))
	for i, t := range m.tags {
		row := tagKindMark(t) + " " + t.Name + "  " + shortHash(t.Target)
		if t.Subject != "" {
			row += "  " + t.Subject
		}
		if m.remoteTagNames[t.Name] {
			row += "  ▲"
		}
		rows[i] = row
	}
	return rows
}
