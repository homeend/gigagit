package cli

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
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

// pairLinkTo is the local-form change-set link into dir, optionally at a path.
func pairLinkTo(t *testing.T, dir, path string) string {
	t.Helper()
	a, b := pairShas(t, dir)
	l := "gg://" + filepath.ToSlash(dir)
	if path != "" {
		l += "/" + path
	}
	return l + "@" + a + ".." + b
}

// growSourceFile adds a second commit on feat/x that APPENDS to a.txt, so the
// tip's own change (B^..B) numbers a.txt's hunk over line 2 alone while the
// pair's patch (a..b) numbers it over the whole new file.
func growSourceFile(t *testing.T, dir string) {
	t.Helper()
	gitRun(t, dir, "checkout", "-q", "feat/x")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\nsecond\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "grow a")
	gitRun(t, dir, "checkout", "-q", "main")
}

// The change-set link and --preview <a>..<b> are ONE lane: same diff, same
// hunk numbering, same note rows.
func TestPairLinkMatchesThePreviewFlag(t *testing.T) {
	dir := previewRepo(t)
	growSourceFile(t, dir)
	fileLink := pairLinkTo(t, dir, "a.txt")
	if code, _, errb := runCLI(t, dir, "note", "add", fileLink+":1", "--summary", "via the pair link"); code != 0 {
		t.Fatalf("note add <pair link>: %d %s", code, errb)
	}
	for _, c := range []struct {
		name           string
		flagArgs, link []string
	}{
		{"diff --hunks --json", []string{"diff", "--preview", "main..feat/x", "--hunks", "--json", "--", "a.txt"}, []string{"diff", fileLink, "--hunks", "--json"}},
		{"note list", []string{"note", "list", "--preview", "main..feat/x", "--file", "a.txt"}, []string{"note", "list", fileLink}},
		{"note list all", []string{"note", "list", "--preview", "main..feat/x"}, []string{"note", "list", pairLinkTo(t, dir, "")}},
	} {
		codeF, outF, errF := runCLI(t, dir, c.flagArgs...)
		codeL, outL, errL := runCLI(t, dir, c.link...)
		if codeF != 0 || codeL != 0 || outF != outL {
			t.Fatalf("%s: flag %d %q (%s)\nlink %d %q (%s)", c.name, codeF, outF, errF, codeL, outL, errL)
		}
		if strings.HasPrefix(c.name, "note list") && !strings.Contains(outL, "via the pair link") {
			t.Fatalf("%s: the note is missing: %q", c.name, outL)
		}
	}
}

// #N is numbered over the PAIR's patch, never over b's own parent→b change.
func TestNoteAddThroughAPairLinkNumbersHunksOverThePair(t *testing.T) {
	dir := previewRepo(t)
	growSourceFile(t, dir)
	_, b := pairShas(t, dir)
	if code, _, errb := runCLI(t, dir, "note", "add", pairLinkTo(t, dir, "a.txt")+"#1", "--summary", "whole file"); code != 0 {
		t.Fatalf("note add #1: %d %s", code, errb)
	}
	_, out, _ := runCLI(t, dir, "note", "list", "--rev", b, "--file", "a.txt", "--json")
	// a..b adds the whole two-line file: [1,2]. b^..b would be line 2 alone.
	if !strings.Contains(out, "whole file") || !strings.Contains(strings.ReplaceAll(out, " ", ""), `"range":[1,2]`) {
		t.Fatalf("the note must sit on b, over the pair's hunk [1,2]: %q", out)
	}
}

func TestPairLinkOldSideIsNotAddressable(t *testing.T) {
	dir := previewRepo(t)
	// m.txt exists on main only: in main..feat/x its one hunk ONLY deletes.
	code, _, errb := runCLI(t, dir, "note", "add", pairLinkTo(t, dir, "m.txt")+"#1", "--summary", "x")
	if code != 2 || !strings.Contains(errb, "new side") {
		t.Fatalf("a delete-only hunk: %d %s", code, errb)
	}
	code, _, errb = runCLI(t, dir, "note", "add", pairLinkTo(t, dir, "m.txt")+":old:1", "--summary", "x")
	if code != 2 || !strings.Contains(errb, "new side") {
		t.Fatalf(":old: on a pair link: %d %s", code, errb)
	}
	code, _, errb = runCLI(t, dir, "session", "highlight", "add", pairLinkTo(t, dir, "m.txt")+":old:1", "--no-wait")
	if code != 2 || !strings.Contains(errb, "new side") {
		t.Fatalf("highlight :old: on a pair link: %d %s", code, errb)
	}
}

func TestNoteApplyThroughAPairLinkStoresOnB(t *testing.T) {
	dir := previewRepo(t)
	_, b := pairShas(t, dir)
	batch := `{"comments":[{"filePath":"a.txt","newLine":1,"summary":"applied"},{"filePath":"m.txt","oldLine":1,"summary":"old side"}]}`
	code, out, errb := runCLIStdin(t, dir, batch, "note", "apply", pairLinkTo(t, dir, ""), "--stdin")
	if code != 0 {
		t.Fatalf("note apply: %d %s %s", code, out, errb)
	}
	_, got, _ := runCLI(t, dir, "note", "list", "--rev", b, "--file", "a.txt")
	if !strings.Contains(got, "applied") {
		t.Fatalf("the batch note must land on b: %q (apply said %q %q)", got, out, errb)
	}
	if _, all, _ := runCLI(t, dir, "note", "list", "--preview", "main..feat/x"); strings.Contains(all, "old side") {
		t.Fatalf("an old-side item must be skipped: %q", all)
	}
}

// The pair arm linkDiffSpec keeps (no rev-list for a plain `gg diff`) must be
// the very spec the note scope hands out, or `gg diff <link> --hunks` and
// `gg note add <link>#N` would number different patches.
func TestPairLinkDiffSpecEqualsItsNoteScope(t *testing.T) {
	dir := previewRepo(t)
	svc := domain.Open(dir)
	ctx := context.Background()
	res, err := resolveLinkArg(ctx, svc, pairLinkTo(t, dir, "a.txt"), linkShapes{Pair: true}, "test")
	if err != nil {
		t.Fatal(err)
	}
	spec, err := linkDiffSpec(ctx, svc, res)
	if err != nil {
		t.Fatal(err)
	}
	scope, ok, err := noteScopeFromLink(ctx, svc, res)
	if err != nil || !ok {
		t.Fatalf("scope: %v %v", ok, err)
	}
	if want := scope.withPaths([]string{"a.txt"}); !reflect.DeepEqual(spec, want) {
		t.Fatalf("linkDiffSpec %+v != scope %+v", spec, want)
	}
	if res.Preview != nil {
		t.Fatal("Resolved.Preview must stay nil for a pair link: linknav and the steer layers dispatch on it")
	}
}
