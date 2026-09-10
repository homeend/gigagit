package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/hunkpick"
	"github.com/homeend/gigagit/internal/syntax"
)

// pickerSyntaxDoc is a two-block Go document whose sides differ in LENGTH, so
// a mapping that advanced one full-file line cursor for both sides would land
// the second block's incoming line on the wrong line number.
//
//	current side (6 lines)      incoming side (4 lines)
//	0 package main              0 package main
//	1 var a int                 1 var x int
//	2 var b int                 2 const K = 1
//	3 var c int                 3 // tail
//	4 const K = 1
//	5 var d int
func pickerSyntaxDoc() *hunkpick.Doc {
	return &hunkpick.Doc{FinalNewline: true, Items: []hunkpick.Item{
		{Literal: []string{"package main"}},
		{Block: &hunkpick.Block{
			Current:  []string{"var a int", "var b int", "var c int"},
			Incoming: []string{"var x int"},
		}},
		{Literal: []string{"const K = 1"}},
		{Block: &hunkpick.Block{
			Current:  []string{"var d int"},
			Incoming: []string{"// tail"},
		}},
	}}
}

func TestPickerLexesBothSidesWithIndependentLineNumbers(t *testing.T) {
	t.Parallel()
	e := newConflictPicker("f.go", pickerSyntaxDoc()).withSyntax(true)
	if len(e.curTok) != 6 {
		t.Fatalf("current side lexed %d lines, want 6", len(e.curTok))
	}
	if len(e.incTok) != 4 {
		t.Fatalf("incoming side lexed %d lines, want 4", len(e.incTok))
	}
	// Block 2's incoming line is incoming line 4 (1-based) — a comment. Had the
	// mapping used the CURRENT side's cursor it would read line 6, past the end.
	if toks := tokAt(e.incTok, 4); len(toks) == 0 || toks[0].Class != syntax.Comment {
		t.Errorf("incoming line 4 (`// tail`) runs = %v, want a leading Comment", toks)
	}
	// Block 2's current line is current line 6 — `var` is a keyword.
	if toks := tokAt(e.curTok, 6); len(toks) == 0 || toks[0].Class != syntax.Keyword {
		t.Errorf("current line 6 (`var d int`) runs = %v, want a leading Keyword", toks)
	}
}

func TestPickerSyntaxOffLeavesNoRuns(t *testing.T) {
	t.Parallel()
	e := newConflictPicker("f.go", pickerSyntaxDoc()).withSyntax(false)
	if e.curTok != nil || e.incTok != nil {
		t.Fatalf("syntax off must leave both sides unlexed: cur=%d inc=%d", len(e.curTok), len(e.incTok))
	}
}

func TestPickerUnknownLanguageLeavesNoRuns(t *testing.T) {
	t.Parallel()
	e := newConflictPicker("f.unknownext", pickerSyntaxDoc()).withSyntax(true)
	if e.curTok != nil || e.incTok != nil {
		t.Fatalf("a path with no lexer must leave both sides unlexed: cur=%d inc=%d", len(e.curTok), len(e.incTok))
	}
}

// A side holding a BARE \r is refused, exactly as lexBlame/lexPreview refuse one.
func TestPickerBareCRSideIsNotLexed(t *testing.T) {
	t.Parallel()
	doc := &hunkpick.Doc{Items: []hunkpick.Item{
		{Literal: []string{"package main"}},
		{Block: &hunkpick.Block{Current: []string{"var a\rint"}, Incoming: []string{"var b int"}}},
	}}
	e := newConflictPicker("f.go", doc).withSyntax(true)
	if e.curTok != nil {
		t.Errorf("a bare-\\r current side must not be lexed: %d lines", len(e.curTok))
	}
	if e.incTok == nil {
		t.Errorf("the clean incoming side should still be lexed")
	}
}

// Every constructor records its path, so withSyntax needs no second argument.
func TestPickerConstructorsRecordPath(t *testing.T) {
	t.Parallel()
	d := pickerSyntaxDoc()
	for name, e := range map[string]*hunkPicker{
		"conflict": newConflictPicker("a.go", d),
		"process":  newProcessConflictPicker("b.go", d),
		"stage":    newStagePicker("c.go", d),
		"unstage":  newUnstagePicker("d.go", d),
	} {
		if e.path == "" {
			t.Errorf("%s picker did not record its path", name)
		}
	}
}
