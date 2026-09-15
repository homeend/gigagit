package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

// A preview link posts a navigate carrying the PAIR, not a sha, and no commit.
func TestSessionNavigatePreviewLinkPostsThePair(t *testing.T) {
	dir := previewRepo(t)
	inbox := t.TempDir()
	livePresence(t, inbox)
	svc := domain.Open(dir)
	var out, errb strings.Builder
	code := runSession(inbox, svc, []string{"navigate", previewLinkFor(dir, "a.txt") + ":1", "--no-wait"}, &out, &errb)
	if code != 0 {
		t.Fatalf("exit = %d: %s", code, errb.String())
	}
	got := steer.Drain(inbox)
	if len(got) != 1 {
		t.Fatalf("posted %d commands, want 1", len(got))
	}
	c := got[0]
	if c.Cmd != "navigate" || c.File != "a.txt" {
		t.Fatalf("command = %+v", c)
	}
	if c.Target == nil || c.Target.State != "preview" {
		t.Fatalf("target = %+v, want state preview", c.Target)
	}
	if c.Target.Source != "feat/x" || c.Target.Target != "main" {
		t.Errorf("pair = %s...%s", c.Target.Target, c.Target.Source)
	}
	if c.Target.Commit != "" {
		t.Errorf("target.commit = %q, want empty — the consumer resolves the tip", c.Target.Commit)
	}
	if c.Line == nil || c.Line.Side != "new" || c.Line.No != 1 {
		t.Errorf("line = %+v", c.Line)
	}
}

// A preview link with NO file reveals the Previews entry: no file, no line.
func TestSessionNavigatePreviewRepoLinkReveals(t *testing.T) {
	dir := previewRepo(t)
	inbox := t.TempDir()
	livePresence(t, inbox)
	var out, errb strings.Builder
	if code := runSession(inbox, domain.Open(dir), []string{"navigate", previewLinkFor(dir, ""), "--no-wait"}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d: %s", code, errb.String())
	}
	got := steer.Drain(inbox)
	if len(got) != 1 || got[0].File != "" || got[0].Line != nil {
		t.Fatalf("command = %+v, want a file-less reveal", got)
	}
	if got[0].Target == nil || got[0].Target.State != "preview" {
		t.Fatalf("target = %+v", got[0].Target)
	}
}

// A #hunk preview link is lowered through PreviewHunkAnchor: the posted line is
// the FIRST line of the preview patch's hunk, on the new side.
func TestSessionNavigatePreviewHunkLowersThroughPreviewHunkAnchor(t *testing.T) {
	dir := previewRepo(t)
	inbox := t.TempDir()
	livePresence(t, inbox)
	var out, errb strings.Builder
	if code := runSession(inbox, domain.Open(dir), []string{"navigate", previewLinkFor(dir, "a.txt") + "#1", "--no-wait"}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d: %s", code, errb.String())
	}
	got := steer.Drain(inbox)
	if len(got) != 1 || got[0].Line == nil {
		t.Fatalf("command = %+v", got)
	}
	if got[0].Line.Side != "new" || got[0].Line.No != 1 {
		t.Errorf("line = %+v, want new:1 (a.txt is wholly added on feat/x)", got[0].Line)
	}
}

// highlight add keys the band on the TIP commit (ruling 6): the wire target is
// the ordinary commit form, so no new mark wire is needed.
func TestSessionHighlightAddPreviewLinkKeysOnTheTip(t *testing.T) {
	dir := previewRepo(t)
	inbox := t.TempDir()
	livePresence(t, inbox)
	var out, errb strings.Builder
	if code := runSession(inbox, domain.Open(dir), []string{"highlight", "add", previewLinkFor(dir, "a.txt") + ":1", "--no-wait"}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d: %s", code, errb.String())
	}
	got := steer.Drain(inbox)
	if len(got) != 1 || got[0].Target == nil {
		t.Fatalf("command = %+v", got)
	}
	if got[0].Target.State != "commit" || len(got[0].Target.Commit) != 40 {
		t.Errorf("target = %+v, want the tip commit", got[0].Target)
	}
}

// A #hunk preview link that only deletes lines has no new side to paint a
// band on: sessionHighlightAdd's link arm must go through PreviewHunkAnchor
// (T5a / controller correction), which refuses it (domain.ErrPreviewOldSide)
// at exit 2, with nothing posted to the inbox.
func TestSessionHighlightAddPreviewDeleteOnlyHunkRefuses(t *testing.T) {
	dir := deleteOnlyHunkPreviewRepo(t)
	inbox := t.TempDir()
	livePresence(t, inbox)
	var out, errb strings.Builder
	code := runSession(inbox, domain.Open(dir), []string{"highlight", "add", previewLinkFor(dir, "a.txt") + "#1", "--no-wait"}, &out, &errb)
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (%s)", code, errb.String())
	}
	if !strings.Contains(errb.String(), "notes in a preview anchor on the new side") {
		t.Errorf("stderr = %q, want the ErrPreviewOldSide message", errb.String())
	}
	if got := steer.Drain(inbox); len(got) != 0 {
		t.Fatalf("posted %d commands, want 0 (refused before posting)", len(got))
	}
}

// deleteOnlyHunkPreviewRepo is previewRepo's shape, but a.txt exists at the
// MERGE BASE and feat/x's only change DROPS it entirely — a hunk whose new
// side is empty ("+0,0"), so PreviewHunkAnchor's old-side refusal
// (domain.ErrPreviewOldSide) is reachable through a #hunk preview link (T5a /
// controller correction). A partial-line deletion still carries unchanged
// context lines on both sides of the hunk and would NOT trigger the refusal;
// only a whole-file drop leaves the new side with nothing at all.
func deleteOnlyHunkPreviewRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	dir := newCLIRepo(t)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\nthree\n"), 0o644)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "add a.txt")
	gitRun(t, dir, "checkout", "-q", "-b", "feat/x")
	gitRun(t, dir, "rm", "-q", "a.txt")
	gitRun(t, dir, "commit", "-q", "-m", "drop a.txt")
	gitRun(t, dir, "checkout", "-q", "main")
	os.WriteFile(filepath.Join(dir, "m.txt"), []byte("m\n"), 0o644)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "main moves")
	return dir
}

// A #hunk preview link that only deletes lines has nothing on the new side to
// anchor on: PreviewHunkAnchor refuses it (domain.ErrPreviewOldSide), the CLI
// maps that to exit 2 with its message — the same surface as
// `gg note add <preview link>#N` — and nothing reaches the inbox.
func TestSessionNavigatePreviewDeleteOnlyHunkRefuses(t *testing.T) {
	dir := deleteOnlyHunkPreviewRepo(t)
	inbox := t.TempDir()
	livePresence(t, inbox)
	var out, errb strings.Builder
	code := runSession(inbox, domain.Open(dir), []string{"navigate", previewLinkFor(dir, "a.txt") + "#1", "--no-wait"}, &out, &errb)
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (%s)", code, errb.String())
	}
	if !strings.Contains(errb.String(), "notes in a preview anchor on the new side") {
		t.Errorf("stderr = %q, want the ErrPreviewOldSide message", errb.String())
	}
	if got := steer.Drain(inbox); len(got) != 0 {
		t.Fatalf("posted %d commands, want 0 (refused before posting)", len(got))
	}
}
