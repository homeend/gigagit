package cli

import (
	"strings"
	"testing"
)

// pairShas are the fixture's two commits: main's tip (a) and feat/x's (b).
func pairShas(t *testing.T, dir string) (string, string) {
	t.Helper()
	return strings.TrimSpace(gitOut(t, dir, "rev-parse", "main")),
		strings.TrimSpace(gitOut(t, dir, "rev-parse", "feat/x"))
}

// --preview takes a commit pair — typed as <a>..<b> or named by a saved
// pair's id — and the note lands on b, new side, like any committed note.
func TestNotePreviewFlagTakesACommitPair(t *testing.T) {
	dir := previewRepo(t) // t.Setenv: serial
	_, b := pairShas(t, dir)
	if code, _, errb := runCLI(t, dir, "note", "add", "--preview", "main..feat/x",
		"--file", "a.txt", "--new-line", "1", "--summary", "pair note"); code != 0 {
		t.Fatalf("note add: %d %s", code, errb)
	}
	code, out, errb := runCLI(t, dir, "note", "list", "--rev", b, "--file", "a.txt")
	if code != 0 || !strings.Contains(out, "pair note") {
		t.Fatalf("the note is an ordinary note on b: %d %q %s", code, out, errb)
	}
	code, id, errb := runCLI(t, dir, "preview", "add", "--label", "attempt", "main..feat/x")
	if code != 0 {
		t.Fatalf("preview add: %d %s", code, errb)
	}
	id = strings.TrimSpace(id)
	for _, spec := range []string{id, "attempt", "main..feat/x"} {
		code, out, errb = runCLI(t, dir, "note", "list", "--preview", spec)
		if code != 0 || !strings.Contains(out, "pair note") {
			t.Fatalf("note list --preview %s: %d %q %s", spec, code, out, errb)
		}
	}
	// Removing the entry never removes the notes: they belong to b.
	if code, _, errb := runCLI(t, dir, "preview", "rm", id); code != 0 {
		t.Fatalf("rm: %d %s", code, errb)
	}
	if _, out, _ := runCLI(t, dir, "note", "list", "--rev", b, "--file", "a.txt"); !strings.Contains(out, "pair note") {
		t.Fatalf("the note must outlive its entry: %q", out)
	}
}

func TestDiffPreviewFlagOnAPairIsThePairsDiff(t *testing.T) {
	dir := previewRepo(t)
	_, want, _ := runCLI(t, dir, "diff", "main..feat/x")
	code, got, errb := runCLI(t, dir, "diff", "--preview", "main..feat/x")
	if code != 0 || got != want || !strings.Contains(got, "m.txt") {
		// TWO-dot: main moved, so m.txt is in the pair's diff (it is NOT in
		// the three-dot preview's).
		t.Fatalf("diff --preview a..b: %d %s\n got %q\nwant %q", code, errb, got, want)
	}
	code, _, errb = runCLI(t, dir, "diff", "--preview", "main..nope")
	if code == 0 || !strings.Contains(errb, "missing commit: nope") {
		t.Fatalf("a missing half must be named: %d %s", code, errb)
	}
}

func TestPreviewDiffTakesASavedPair(t *testing.T) {
	dir := previewRepo(t)
	_, id, _ := runCLI(t, dir, "preview", "add", "main..feat/x")
	id = strings.TrimSpace(id)
	code, out, errb := runCLI(t, dir, "preview", "diff", id)
	if code != 0 || !strings.Contains(out, "a.txt") || !strings.Contains(out, "m.txt") {
		t.Fatalf("preview diff <pair id>: %d %q %s", code, out, errb)
	}
	code, out, errb = runCLI(t, dir, "preview", "diff", "--hunks", id)
	if code != 0 || !strings.Contains(out, "a.txt") {
		t.Fatalf("preview diff --hunks <pair id>: %d %q %s", code, out, errb)
	}
}

func TestLinkPreviewFlagOnAPairBuildsAChangeSetLink(t *testing.T) {
	dir := previewRepo(t)
	a, b := pairShas(t, dir)
	code, out, errb := runCLI(t, dir, "link", "--preview", "main..feat/x", "a.txt:1")
	if code != 0 || !strings.Contains(out, "/a.txt@"+a+".."+b+":1") {
		t.Fatalf("link --preview a..b: %d %q %s", code, out, errb)
	}
}
