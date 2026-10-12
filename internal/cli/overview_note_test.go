package cli

import (
	"strings"
	"testing"
)

func TestNoteListSkipsOverviews(t *testing.T) {
	t.Parallel()
	dir := overviewCLIRepo(t) // Task 7 helper: a.go (5 lines) committed on main
	code, out, errb := runCLIStdin(t, dir, `{"version":1,"title":"T","overview":"[a](a.go:1)"}`,
		"overview", "save", "--agent", "c", "--stdin", "--json")
	if code != 0 {
		t.Fatalf("save = %d %q", code, errb)
	}
	id := jsonField(t, out, "id")
	code, out, _ = runCLI(t, dir, "note", "list")
	if code != 0 || strings.Contains(out, id) {
		t.Fatalf("note list = %d %q, must not list overview %s", code, out, id)
	}
	code, _, errb = runCLI(t, dir, "note", "show", id)
	if code == 0 || !strings.Contains(errb, "gg overview show "+id) {
		t.Fatalf("note show = %d %q", code, errb)
	}
}
