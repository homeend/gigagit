package cli

import (
	"strings"
	"testing"
)

func TestPairSpec(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		in, a, b string
		ok       bool
	}{
		{"abc..def", "abc", "def", true},
		{"main..feat/x", "main", "feat/x", true},
		{"main...feat/x", "", "", false}, // three dots = a merge preview
		{"abc..", "", "", false},
		{"..def", "", "", false},
		{"abcdef", "", "", false},
	} {
		a, b, ok := pairSpec(c.in)
		if ok != c.ok || (ok && (a != c.a || b != c.b)) {
			t.Errorf("pairSpec(%q) = %q, %q, %v; want %q, %q, %v", c.in, a, b, ok, c.a, c.b, c.ok)
		}
	}
}

// previewRepo uses t.Setenv, so these stay serial.
func TestPreviewAddPairListShowRenameRm(t *testing.T) {
	dir := previewRepo(t)
	a := strings.TrimSpace(gitOut(t, dir, "rev-parse", "main"))
	b := strings.TrimSpace(gitOut(t, dir, "rev-parse", "feat/x"))
	code, id, errb := runCLI(t, dir, "preview", "add", "--label", "attempt", "main..feat/x")
	if code != 0 {
		t.Fatalf("add pair: %d %s", code, errb)
	}
	id = strings.TrimSpace(id)
	// A merge preview beside it: list must show BOTH kinds, the preview first.
	if code, _, errb := runCLI(t, dir, "preview", "add", "feat/x", "main"); code != 0 {
		t.Fatalf("add preview: %d %s", code, errb)
	}
	_, out, _ := runCLI(t, dir, "preview", "list")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	// main..feat/x TWO-dot = a.txt (added) + m.txt (absent on feat/x) = 2 files.
	want := id + "\tattempt\t" + a + "\t" + b + "\tpair\t2\t0"
	if len(lines) != 2 || !strings.Contains(lines[0], "\tfeat/x\tmain\tok\t") || lines[1] != want {
		t.Fatalf("list = %q\nwant row 2 = %q", out, want)
	}
	_, out, _ = runCLI(t, dir, "preview", "show", "attempt")
	if !strings.Contains(out, "a.txt") || !strings.Contains(out, "m.txt") {
		t.Fatalf("show must be the TWO-dot list (both files): %q", out)
	}
	if _, out, _ = runCLI(t, dir, "preview", "show", "--patch", id); !strings.Contains(out, "+a") {
		t.Fatalf("patch = %q", out)
	}
	if code, _, errb := runCLI(t, dir, "preview", "add", "main..feat/x"); code != 1 || !strings.Contains(errb, id) {
		t.Fatalf("duplicate add: %d %q (must name the existing id)", code, errb)
	}
	if code, _, _ := runCLI(t, dir, "preview", "rename", id, "renamed"); code != 0 {
		t.Fatal("rename")
	}
	if code, _, _ := runCLI(t, dir, "preview", "rm", "renamed"); code != 0 {
		t.Fatal("rm by label")
	}
	if _, out, _ := runCLI(t, dir, "preview", "list"); strings.Contains(out, id) {
		t.Fatalf("list after rm still holds the pair: %q", out)
	}
}

func TestPreviewAddPairRefusals(t *testing.T) {
	dir := previewRepo(t)
	if code, _, _ := runCLI(t, dir, "preview", "add", "main..main"); code != 2 {
		t.Fatalf("same commit: exit %d, want 2", code)
	}
	if code, _, _ := runCLI(t, dir, "preview", "add", "main..nope"); code != 2 {
		t.Fatalf("unknown rev: exit %d, want 2", code)
	}
	// A lone three-dot text was a usage error before pairs existed and stays
	// one: it must never reach PairAdd.
	if code, _, errb := runCLI(t, dir, "preview", "add", "main...feat/x"); code != 2 || !strings.Contains(errb, "usage") {
		t.Fatalf("one three-dot arg: %d %q", code, errb)
	}
}
