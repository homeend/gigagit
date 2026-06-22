package tui

import (
	"testing"
)

func TestCommitPopupCursorEdit(t *testing.T) {
	p := &commitPopup{}
	// type "abc" into the title, move left twice, insert "X" -> "aXbc"
	p.applyEditKey(keyMsg("abc"))
	p.applyEditKey(keyMsg("left"))
	p.applyEditKey(keyMsg("left"))
	p.applyEditKey(keyMsg("X"))
	if got := p.title.Value(); got != "aXbc" {
		t.Fatalf("title = %q, want aXbc", got)
	}
}

func TestCommitPopupEnterAdvancesThenNewline(t *testing.T) {
	p := &commitPopup{}
	p.applyEditKey(keyMsg("subj"))
	p.applyEditKey(keyMsg("enter")) // title -> desc
	if p.field != 1 {
		t.Fatalf("field = %d, want 1 after Enter on title", p.field)
	}
	p.applyEditKey(keyMsg("line1"))
	p.applyEditKey(keyMsg("enter")) // newline in desc
	p.applyEditKey(keyMsg("line2"))
	if got := p.desc.Value(); got != "line1\nline2" {
		t.Fatalf("desc = %q, want 'line1\\nline2'", got)
	}
}

func TestCommitPopupSubmitCancel(t *testing.T) {
	p := &commitPopup{}
	if s, c := p.applyEditKey(keyMsg("ctrl+s")); !s || c {
		t.Fatalf("ctrl+s = (%v,%v), want submit", s, c)
	}
	if s, c := p.applyEditKey(keyMsg("esc")); s || !c {
		t.Fatalf("esc = (%v,%v), want cancel", s, c)
	}
}

func TestCommitMessageAndSplit(t *testing.T) {
	p := &commitPopup{title: newTextField("subj"), desc: newTextField("body")}
	if got := p.message(); got != "subj\n\nbody" {
		t.Fatalf("message = %q", got)
	}
}
