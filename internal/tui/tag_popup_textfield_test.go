package tui

import (
	"testing"
)

func TestTagPopupNameNoSpaceMessageSpace(t *testing.T) {
	t.Parallel()
	p := &tagPopup{commit: "deadbeef"}
	m := Model{}
	m, _ = p.update(m, keyMsg("v1"))
	m, _ = p.update(m, keyMsg("space")) // dropped in name
	m, _ = p.update(m, keyMsg("tab"))   // -> message
	m, _ = p.update(m, keyMsg("a"))
	m, _ = p.update(m, keyMsg("space")) // kept in message
	m, _ = p.update(m, keyMsg("b"))
	_ = m
	if p.name.Value() != "v1" {
		t.Fatalf("name = %q, want v1", p.name.Value())
	}
	if p.message.Value() != "a b" {
		t.Fatalf("message = %q, want 'a b'", p.message.Value())
	}
}

func TestTagCheckoutPrefillNoSpace(t *testing.T) {
	t.Parallel()
	p := &tagCheckoutPopup{tag: "v1", name: newTextField("v1")}
	m := Model{}
	m, _ = p.update(m, keyMsg("-x"))
	m, _ = p.update(m, keyMsg("space"))
	_ = m
	if p.name.Value() != "v1-x" {
		t.Fatalf("name = %q, want v1-x", p.name.Value())
	}
}
