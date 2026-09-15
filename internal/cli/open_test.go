package cli

import (
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
