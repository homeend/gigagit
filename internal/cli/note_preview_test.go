package cli

// `gg note … --preview` and `gg review --preview` (spec §1.5/§1.6): a merge
// preview is a target the note verbs accept, stored on the SOURCE TIP and
// numbered over the preview's own patch.

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// newCLIPreviewRepo mirrors internal/domain's newPreviewRepo: main with one
// commit, then feat with three, the second of which REWRITES a line the first
// added (so a note on the first commit goes outdated on the tip). Task 5's
// previewRepo cannot serve here — its feat branch is a single commit adding
// one line, so nothing in it can ever resolve as outdated.
//
// It sets XDG_STATE_HOME/XDG_CONFIG_HOME (like noteRepo), so it must NOT be
// used from a t.Parallel() test.
func newCLIPreviewRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runGit(t, dir, "init", "-b", "main")
	write("alpha\nbravo\ncharlie\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "seed")
	runGit(t, dir, "checkout", "-b", "feat")
	write("alpha\nbravo\ncharlie\nDELTA\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "c1 adds DELTA")
	write("alpha\nbravo\ncharlie\nECHO\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "c2 rewrites DELTA")
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("bee\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "c3 adds b.txt")
	runGit(t, dir, "checkout", "main")
	return dir
}

// The note is stored on the TIP, so the tip's own commit view shows it too.
func TestNoteAddPreviewStoresOnTheTip(t *testing.T) {
	dir := newCLIPreviewRepo(t)
	tip := runGit(t, dir, "rev-parse", "feat")
	code, _, errb := runCLI(t, dir, "note", "add", "--preview", "main...feat", "--file", "a.txt",
		"--new-line", "1", "--summary", "alpha stands")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errb)
	}
	code, out, errb := runCLI(t, dir, "note", "list", "--rev", tip, "--file", "a.txt")
	if code != 0 {
		t.Fatalf("list exit %d: %s", code, errb)
	}
	if !strings.Contains(out, "alpha stands") {
		t.Fatalf("the note must be stored on the tip, got %q", out)
	}
}

// --hunk under --preview numbers the PREVIEW's patch (merge-base → tip), which
// reaches ECHO on line 4 — the tip commit's own patch touches only b.txt.
func TestNoteAddPreviewHunkUsesThePreviewPatch(t *testing.T) {
	dir := newCLIPreviewRepo(t)
	tip := runGit(t, dir, "rev-parse", "feat")
	code, _, errb := runCLI(t, dir, "note", "add", "--preview", "main...feat", "--file", "a.txt",
		"--hunk", "1", "--summary", "the whole hunk")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errb)
	}
	_, out, _ := runCLI(t, dir, "note", "list", "--rev", tip, "--file", "a.txt")
	if !strings.Contains(out, "the whole hunk") || !strings.Contains(out, "new:1-4") {
		t.Fatalf("want a new-side anchor spanning the preview hunk, got %q", out)
	}
}

func TestNoteAddPreviewRefusesOldLine(t *testing.T) {
	dir := newCLIPreviewRepo(t)
	code, _, errb := runCLI(t, dir, "note", "add", "--preview", "main...feat", "--file", "a.txt",
		"--old-line", "1", "--summary", "no")
	if code != 2 {
		t.Fatalf("want exit 2, got %d (%s)", code, errb)
	}
	if !strings.Contains(errb, "new side") {
		t.Fatalf("want the new-side message, got %q", errb)
	}
}

func TestNoteAddPreviewRefusesRev(t *testing.T) {
	dir := newCLIPreviewRepo(t)
	tip := runGit(t, dir, "rev-parse", "feat")
	code, _, errb := runCLI(t, dir, "note", "add", "--preview", "main...feat", "--rev", tip,
		"--file", "a.txt", "--new-line", "1", "--summary", "no")
	if code != 2 || !strings.Contains(errb, "one target only") {
		t.Fatalf("want exit 2 + one-target message, got %d %q", code, errb)
	}
}

func TestNoteAddPreviewNeedsAFile(t *testing.T) {
	dir := newCLIPreviewRepo(t)
	code, _, errb := runCLI(t, dir, "note", "add", "--preview", "main...feat",
		"--new-line", "1", "--summary", "no")
	if code != 2 || !strings.Contains(errb, "--file") {
		t.Fatalf("want exit 2 naming --file, got %d %q", code, errb)
	}
}

// A note on an older commit's rewritten line lists as `outdated`.
func TestNoteListPreviewReportsOutdated(t *testing.T) {
	dir := newCLIPreviewRepo(t)
	c1 := runGit(t, dir, "rev-parse", "feat~2")
	if code, _, errb := runCLI(t, dir, "note", "add", "--rev", c1, "--file", "a.txt",
		"--new-line", "4", "--summary", "why DELTA"); code != 0 {
		t.Fatalf("seed exit %d: %s", code, errb)
	}
	code, out, errb := runCLI(t, dir, "note", "list", "--preview", "main...feat", "--file", "a.txt")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errb)
	}
	if !strings.Contains(out, "outdated") || !strings.Contains(out, "why DELTA") {
		t.Fatalf("want the gathered note reported outdated, got %q", out)
	}
	// Without --file the preview lists every path it covers.
	code, all, errb := runCLI(t, dir, "note", "list", "--preview", "main...feat")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errb)
	}
	if !strings.Contains(all, "why DELTA") {
		t.Fatalf("a --file-less preview list must still gather the note, got %q", all)
	}
}

func TestNoteListPreviewRefusesRev(t *testing.T) {
	dir := newCLIPreviewRepo(t)
	code, _, errb := runCLI(t, dir, "note", "list", "--preview", "main...feat", "--cached")
	if code != 2 || !strings.Contains(errb, "one target only") {
		t.Fatalf("want exit 2 + one-target message, got %d %q", code, errb)
	}
}

// `gg note apply --preview` stores on the tip, numbers hunks over the preview
// patch, and SKIPS old-side items with one warning.
func TestNoteApplyPreviewNewSideOnly(t *testing.T) {
	dir := newCLIPreviewRepo(t)
	tip := runGit(t, dir, "rev-parse", "feat")
	// The comment-apply shape: `hunk` is only expressible there.
	batch := `{"comments":[{"filePath":"a.txt","hunk":1,"summary":"kept"},` +
		`{"filePath":"a.txt","oldLine":1,"summary":"dropped"}]}`
	code, _, errb := runCLIStdin(t, dir, batch, "note", "apply", "--preview", "main...feat", "--stdin")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errb)
	}
	if !strings.Contains(errb, "new side") {
		t.Fatalf("want one old-side warning, got %q", errb)
	}
	_, out, _ := runCLI(t, dir, "note", "list", "--rev", tip, "--file", "a.txt")
	if !strings.Contains(out, "kept") || strings.Contains(out, "dropped") {
		t.Fatalf("only the new-side item may land on the tip:\n%s", out)
	}
	if !strings.Contains(out, "new:1-4") {
		t.Fatalf("the hunk must be numbered over the preview patch:\n%s", out)
	}
}

// `gg review --preview` reviews the pair's range and, with --notes, imports the
// tool's notes onto the source tip, new side only (spec §1.6).
func TestReviewPreviewImportsNotesOntoTheTip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh/printf")
	}
	dir := newCLIPreviewRepo(t)
	tip := runGit(t, dir, "rev-parse", "feat")
	writeReviewTool(t, dir, "Echo",
		`printf 'R\n'; printf '{"comments":[{"filePath":"a.txt","hunk":1,"summary":"kept"},{"filePath":"a.txt","oldLine":1,"summary":"dropped"}]}' > "$GG_NOTES_FILE"`)

	code, _, errb := runCLI(t, dir, "review", "--tool", "Echo", "--preview", "main...feat", "--notes")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errb)
	}
	if !strings.Contains(errb, "notes:") {
		t.Fatalf("stderr must list the imported ids: %q", errb)
	}
	if !strings.Contains(errb, "old-side") {
		t.Fatalf("want one old-side warning, got %q", errb)
	}
	_, out, _ := runCLI(t, dir, "note", "list", "--rev", tip, "--file", "a.txt")
	if !strings.Contains(out, "kept") || strings.Contains(out, "dropped") {
		t.Fatalf("only the new-side note may land on the tip:\n%s", out)
	}
	if !strings.Contains(out, "new:1-4") {
		t.Fatalf("the hunk must be numbered over the preview patch:\n%s", out)
	}
	_, prev, _ := runCLI(t, dir, "note", "list", "--preview", "main...feat", "--file", "a.txt")
	if !strings.Contains(prev, "kept") || !strings.Contains(prev, "active") {
		t.Fatalf("the preview must show its own imported note as active:\n%s", prev)
	}
}

func TestReviewPreviewRefusesWorking(t *testing.T) {
	dir := newCLIPreviewRepo(t)
	code, _, errb := runCLI(t, dir, "review", "--preview", "main...feat", "--working")
	if code != 2 || !strings.Contains(errb, "one target only") {
		t.Fatalf("want exit 2 + one-target message, got %d %q", code, errb)
	}
}
