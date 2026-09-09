package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/syntax"
	"github.com/homeend/gigagit/internal/theme"
)

// internal/theme is a DAG leaf: it cannot import internal/syntax, so it mirrors
// the Class order in its own name table (theme.SyntaxClassNames) and the colour
// editor's syntax[<Name>] rows are built from that mirror. This is the gate that
// keeps the two in step — it pins each Class to BOTH its short String() (the
// syntax package's own identity) and the long name theme uses, so reordering
// either side, or renaming a class, fails here instead of silently repainting
// the wrong class in the editor.
func TestThemeSyntaxRoleNamesMatchSyntaxClasses(t *testing.T) {
	t.Parallel()
	want := []struct {
		c           syntax.Class
		short, long string
	}{
		{syntax.Plain, "", "Plain"},
		{syntax.Keyword, "kw", "Keyword"},
		{syntax.Type, "ty", "Type"},
		{syntax.Func, "fn", "Func"},
		{syntax.Name, "nm", "Name"},
		{syntax.String, "str", "String"},
		{syntax.Number, "num", "Number"},
		{syntax.Comment, "cmt", "Comment"},
		{syntax.Operator, "op", "Operator"},
		{syntax.Punct, "pn", "Punct"},
		{syntax.Attr, "at", "Attr"},
	}
	names := theme.SyntaxClassNames()
	if len(want) != len(names) {
		t.Fatalf("theme mirrors %d syntax classes, the gate lists %d", len(names), len(want))
	}

	// Every syntax role of the editor, in Roles() order.
	var rows []theme.RoleRef
	for _, r := range theme.Roles() {
		if r.ListKey == "syntax" {
			rows = append(rows, r)
		}
	}
	if len(rows) != len(want) {
		t.Fatalf("Roles() carries %d syntax rows, want %d", len(rows), len(want))
	}

	for i, w := range want {
		if int(w.c) != i {
			t.Fatalf("syntax.Class %q is %d, the gate expects index %d", w.long, int(w.c), i)
		}
		if got := w.c.String(); got != w.short {
			t.Fatalf("syntax.Class(%d).String() = %q, want %q — the class order changed", i, got, w.short)
		}
		if names[i] != w.long {
			t.Fatalf("theme.SyntaxClassNames()[%d] = %q, want %q", i, names[i], w.long)
		}
		if key := rows[i].Key; key != "syntax["+w.long+"]" {
			t.Fatalf("editor row %d key = %q, want syntax[%s]", i, key, w.long)
		}
		if !strings.HasPrefix(rows[i].Key, "syntax[") {
			t.Fatalf("row %d is not a syntax row: %q", i, rows[i].Key)
		}
	}
}
