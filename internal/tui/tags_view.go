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

// tagRows renders one display row per tag: "<kind> <name>  <short-target>  <subject>".
func (m Model) tagRows() []string {
	rows := make([]string, len(m.tags))
	for i, t := range m.tags {
		row := tagKindMark(t) + " " + t.Name + "  " + shortHash(t.Target)
		if t.Subject != "" {
			row += "  " + t.Subject
		}
		rows[i] = row
	}
	return rows
}
