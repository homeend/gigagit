package tui

import (
	"github.com/homeend/gigagit/internal/hunkpick"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/textdiff"
)

// The per-file conflict-resolve pipeline, shared by every entry point that
// hands a conflicted file to the region picker (the x process's enter, the
// Files-panel enter, …). Each step is a plain function so a new entry point
// composes them instead of driving another surface:
//
//	conflictPickable(f)          — may this row go to the picker at all?
//	loadConflictFileCmd(path)    — fetch the regenerated conflict off-thread (op.go)
//	parseConflictDoc(content)    — turn the bytes into picker regions
//	newConflictPicker(path, doc) — the surface (conflict_picker.go)
//	engine.ResolveConflictHunks  — apply: write + stage the assembled file

// conflictPickable reports whether f's conflict can be resolved by the region
// picker. The empty string means yes; otherwise it is the status line to show
// (a modify/delete conflict has no regions — the x listing's keep/delete keys
// handle it).
func conflictPickable(f model.FileStatus) string {
	if f.Kind != model.KindUnmerged {
		return i18n.T("not a conflicted file")
	}
	if f.ConflictClass() != model.ConflictBothSides {
		return i18n.T("line editor: only for files modified on both sides")
	}
	return ""
}

// parseConflictDoc turns a regenerated conflict file into the picker's
// document. A nil doc comes with the status line explaining why (binary,
// malformed markers, no regions); markerSize 0 means git's default width.
func parseConflictDoc(content []byte, markerSize int) (*hunkpick.Doc, string) {
	if textdiff.IsBinary(content) {
		return nil, i18n.T("hunk picker: binary file")
	}
	doc, err := hunkpick.ParseConflictSized(content, markerSize)
	if err != nil {
		return nil, i18n.T("hunk picker: %s", err.Error())
	}
	if len(doc.Blocks()) == 0 {
		return nil, i18n.T("hunk picker: no conflict regions found")
	}
	return doc, ""
}
