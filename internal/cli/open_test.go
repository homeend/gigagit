package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

// With a live session, gg open steers it and says so.
func TestOpenSteersALiveSession(t *testing.T) {
	dir := previewRepo(t)
	svc := openCLIService(t, dir)
	inbox := steerDirFor(svc)
	livePresence(t, inbox)
	t.Cleanup(func() { steer.Discard(inbox) })
	var out, errb strings.Builder
	code := cmdOpen(svc, []string{previewLinkFor(dir, "a.txt") + ":1", "--no-wait"}, &out, &errb)
	if code != 0 {
		t.Fatalf("exit = %d: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "steered: ") {
		t.Errorf("stdout = %q, want a \"steered:\" line", out.String())
	}
	got := steer.Drain(inbox)
	if len(got) != 1 || got[0].Target == nil || got[0].Target.State != "preview" {
		t.Fatalf("posted = %+v, want one preview navigate", got)
	}
}

// With no session and no launcher installed, gg open exits 1 and says why.
func TestOpenWithoutASessionOrALauncherExitsOne(t *testing.T) {
	dir := previewRepo(t)
	svc := openCLIService(t, dir)
	LaunchTUI = nil
	var out, errb strings.Builder
	code := cmdOpen(svc, []string{previewLinkFor(dir, "a.txt") + ":1"}, &out, &errb)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errb.String(), "no live gg session in") || !strings.Contains(errb.String(), "launcher is unavailable") {
		t.Errorf("stderr = %q", errb.String())
	}
}

// With a launcher installed it is called with the checkout and a fully
// resolved link — the #hunk already lowered to a line.
func TestOpenCallsTheLauncherWithAResolvedLink(t *testing.T) {
	dir := previewRepo(t)
	svc := openCLIService(t, dir)
	var gotCheckout string
	var gotLink model.Link
	LaunchTUI = func(checkout string, at model.Link) int {
		gotCheckout, gotLink = checkout, at
		return 0
	}
	t.Cleanup(func() { LaunchTUI = nil })
	var out, errb strings.Builder
	if code := cmdOpen(svc, []string{previewLinkFor(dir, "a.txt") + "#1"}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d: %s", code, errb.String())
	}
	// SamePath, not ==: it is the same normalisation the resolver used, and it
	// survives macOS's /private symlink in front of a t.TempDir().
	if !domain.SamePath(gotCheckout, dir) {
		t.Errorf("checkout = %q, want %q", gotCheckout, dir)
	}
	if gotLink.Target.Preview == nil || gotLink.Target.Preview.Source != "feat/x" {
		t.Fatalf("link target = %+v", gotLink.Target)
	}
	if gotLink.Hunk != 0 || gotLink.Line != 1 {
		t.Errorf("link = hunk %d line %d, want the hunk lowered to line 1", gotLink.Hunk, gotLink.Line)
	}
	if gotLink.Path != "a.txt" {
		t.Errorf("link path = %q", gotLink.Path)
	}
}

func TestOpenRefusesAnUnresolvableLink(t *testing.T) {
	dir := previewRepo(t)
	svc := openCLIService(t, dir)
	LaunchTUI = nil
	var out, errb strings.Builder
	if code := cmdOpen(svc, []string{"gg://no-such-repo/a.txt:1"}, &out, &errb); code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if code := cmdOpen(svc, []string{"not-a-link"}, &out, &errb); code != 2 {
		t.Errorf("a non-link argument must exit 2, got %d", code)
	}
}

// A bare repository link (no path, no commit, no preview) names nowhere to
// steer to: gg open launches the TUI in that checkout with no landing link —
// even when a session is live there — and says so when it cannot.
func TestOpenBareRepositoryLinkLaunchesTheTUIThere(t *testing.T) {
	dir := previewRepo(t)
	svc := openCLIService(t, dir)
	inbox := steerDirFor(svc)
	livePresence(t, inbox)
	t.Cleanup(func() { steer.Discard(inbox) })
	var gotCheckout string
	var gotLink model.Link
	calls := 0
	LaunchTUI = func(checkout string, at model.Link) int {
		calls++
		gotCheckout, gotLink = checkout, at
		return 0
	}
	t.Cleanup(func() { LaunchTUI = nil })
	var out, errb strings.Builder
	bare := "gg://" + filepath.ToSlash(dir)
	if code := cmdOpen(svc, []string{bare}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d: %s", code, errb.String())
	}
	if calls != 1 || !domain.SamePath(gotCheckout, dir) {
		t.Errorf("launcher calls = %d checkout = %q, want one call for %q", calls, gotCheckout, dir)
	}
	if gotLink != (model.Link{}) {
		t.Errorf("a bare link must launch with NO landing link, got %+v", gotLink)
	}
	if got := steer.Drain(inbox); len(got) != 0 {
		t.Errorf("a bare link must not steer the live session, posted %+v", got)
	}
	LaunchTUI = nil
	out.Reset()
	errb.Reset()
	if code := cmdOpen(svc, []string{bare}, &out, &errb); code != 1 {
		t.Errorf("without a launcher exit = %d, want 1", code)
	}
	if !strings.Contains(errb.String(), "names a repository, not a place in it") {
		t.Errorf("stderr = %q", errb.String())
	}
}

// TestOpenSteersAFileLinkWithNoLine is the round trip for R1: `gg link
// <path>` emits a link with no line, and `gg open` on that very link must
// steer a navigate that names the file and leaves the cursor alone — not the
// old ErrNoLine exit-2 refusal, which made `gg open` reject the very link
// `gg link` had just printed.
func TestOpenSteersAFileLinkWithNoLine(t *testing.T) {
	dir := previewRepo(t)
	svc := openCLIService(t, dir)
	inbox := steerDirFor(svc)
	livePresence(t, inbox)
	t.Cleanup(func() { steer.Discard(inbox) })
	link := model.Link{
		Repo:   model.LinkRepo{Abs: filepath.ToSlash(dir)},
		Path:   "m.txt",
		Target: model.LinkTarget{State: model.StateUnstaged},
		Side:   model.NoteSideNew,
	}.String()
	var out, errb strings.Builder
	code := cmdOpen(svc, []string{link, "--no-wait"}, &out, &errb)
	if code != 0 {
		t.Fatalf("exit = %d: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "steered: ") {
		t.Errorf("stdout = %q, want a \"steered:\" line", out.String())
	}
	got := steer.Drain(inbox)
	if len(got) != 1 || got[0].Cmd != "navigate" || got[0].File != "m.txt" {
		t.Fatalf("posted = %+v, want one navigate naming m.txt", got)
	}
	if got[0].Line != nil {
		t.Errorf("Line = %+v, want nil (open the file, do not move the cursor)", got[0].Line)
	}

	// The COMMIT-target twin: the web UI's own "copy gg link" row prints
	// exactly this shape for a committed file — gg:///<abs>/m.txt@<sha> — and
	// it hit the identical ErrNoLine refusal `gg open` also gave the
	// working-tree form above.
	sha := runGit(t, dir, "rev-parse", "HEAD")
	commitLink := model.Link{
		Repo:   model.LinkRepo{Abs: filepath.ToSlash(dir)},
		Path:   "m.txt",
		Target: model.LinkTarget{State: model.StateCommitted, Commit: sha},
		Side:   model.NoteSideNew,
	}.String()
	out.Reset()
	errb.Reset()
	code = cmdOpen(svc, []string{commitLink, "--no-wait"}, &out, &errb)
	if code != 0 {
		t.Fatalf("commit-target: exit = %d: %s", code, errb.String())
	}
	got = steer.Drain(inbox)
	if len(got) != 1 || got[0].Cmd != "navigate" || got[0].File != "m.txt" {
		t.Fatalf("commit-target posted = %+v, want one navigate naming m.txt", got)
	}
	if got[0].Target == nil || got[0].Target.State != "commit" || got[0].Target.Commit != sha {
		t.Errorf("commit-target = %+v, want commit %s", got[0].Target, sha)
	}
	if got[0].Line != nil {
		t.Errorf("commit-target Line = %+v, want nil", got[0].Line)
	}
}
