package cli

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

// --- Task 6: a gg:// link's ?bookmark=/?shelf= hint is honoured on
// navigate. SERIAL throughout: every test here sets XDG_STATE_HOME (t.Setenv
// panics under t.Parallel) so the bookmark/shelf store it seeds or checks is
// hermetic — never the real user's.

// TestOpenCallsTheLauncherWithAHintCarryingLink pins that openAtLink (via
// linknav.AtLink) carries a hint through alongside an ORDINARY address —
// ruling S10's second producer, and the with-address row of the truth
// table: the hint must not change WHERE the link lands.
func TestOpenCallsTheLauncherWithAHintCarryingLink(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	dir := newCLIRepo(t)
	svc := openCLIService(t, dir)

	code, out, errb := runCLI(t, dir, "bookmark", "add", "README.md")
	if code != 0 {
		t.Fatalf("bookmark add exit %d: %s", code, errb)
	}
	id := strings.TrimSpace(out)

	var gotLink model.Link
	LaunchTUI = func(checkout string, at model.Link) int {
		gotLink = at
		return 0
	}
	t.Cleanup(func() { LaunchTUI = nil })
	var stdout, stderr bytes.Buffer
	link := "gg://" + filepath.ToSlash(dir) + "/README.md?bookmark=" + id
	if code := cmdOpen(svc, []string{link}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d: %s", code, stderr.String())
	}
	if gotLink.Hint.Kind != "bookmark" || gotLink.Hint.ID != id {
		t.Errorf("launched link hint = %+v, want bookmark/%s", gotLink.Hint, id)
	}
	if gotLink.Path != "README.md" {
		t.Errorf("launched link path = %q, want README.md (the hint must not change WHERE it lands)", gotLink.Path)
	}
}

// TestOpenAddressLessHintLinkIsNotRepoOnly pins ruling S10: once RepoOnly
// returns false for a hint-carrying address-less link, `gg open` must reach
// the ordinary launch path (never the bare-repository one, which launches
// with an EMPTY link) — and that link genuinely carries the hint, which is
// what makes fixing the predicate alone insufficient without also fixing
// AtLink (already pinned in the linknav package; this is the CLI-level
// wiring check for the same producer).
func TestOpenAddressLessHintLinkIsNotRepoOnly(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	dir := newCLIRepo(t)
	svc := openCLIService(t, dir)

	code, out, errb := runCLI(t, dir, "bookmark", "add", "README.md")
	if code != 0 {
		t.Fatalf("bookmark add exit %d: %s", code, errb)
	}
	id := strings.TrimSpace(out)

	var calls int
	var gotLink model.Link
	LaunchTUI = func(checkout string, at model.Link) int {
		calls++
		gotLink = at
		return 0
	}
	t.Cleanup(func() { LaunchTUI = nil })
	var stdout, stderr bytes.Buffer
	link := "gg://" + filepath.ToSlash(dir) + "?bookmark=" + id
	if code := cmdOpen(svc, []string{link}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d: %s", code, stderr.String())
	}
	if calls != 1 {
		t.Fatalf("launcher calls = %d, want 1", calls)
	}
	if gotLink == (model.Link{}) {
		t.Fatal("an address-less link WITH a hint must not launch with an EMPTY link — that is the truly-bare path")
	}
	if gotLink.Hint.Kind != "bookmark" || gotLink.Hint.ID != id {
		t.Errorf("launched link hint = %+v, want bookmark/%s", gotLink.Hint, id)
	}
	if gotLink.Path != "" || gotLink.Target.Commit != "" {
		t.Errorf("must not adopt the bookmark's own stored address (ruling S12): %+v", gotLink)
	}
}

// TestOpenSteersALiveSessionWithAHintOnlyLink pins S13 point 1 (a hint-only
// navigate — no File, no Commit, no Target) through `gg open`'s live-steer
// path.
func TestOpenSteersALiveSessionWithAHintOnlyLink(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	dir := newCLIRepo(t)
	svc := openCLIService(t, dir)

	code, out, errb := runCLI(t, dir, "shelf", "add", "README.md")
	if code != 0 {
		t.Fatalf("shelf add exit %d: %s", code, errb)
	}
	id := strings.TrimSpace(out)

	inbox := steerDirFor(svc)
	livePresence(t, inbox)
	t.Cleanup(func() { steer.Discard(inbox) })
	var stdout, stderr bytes.Buffer
	link := "gg://" + filepath.ToSlash(dir) + "?shelf=" + id
	if code := cmdOpen(svc, []string{link, "--no-wait"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d: %s", code, stderr.String())
	}
	got := steer.Drain(inbox)
	if len(got) != 1 {
		t.Fatalf("posted %d commands, want 1: %+v", len(got), got)
	}
	c := got[0]
	if c.Cmd != "navigate" || c.File != "" || c.Commit != "" || c.Target != nil {
		t.Fatalf("posted = %+v, want a hint-only navigate (S13)", c)
	}
	if c.HintKind != "shelf" || c.HintID != id {
		t.Errorf("hint = %s/%s, want shelf/%s", c.HintKind, c.HintID, id)
	}
}

// TestSessionNavigateAcceptsAHintOnlyLink pins S13 point 3: `gg session
// navigate` must accept a hint-only link rather than the shipped "a live
// session has nowhere to go" ErrRepoOnly refusal, which is correct only for
// a TRULY bare link.
func TestSessionNavigateAcceptsAHintOnlyLink(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	dir := newCLIRepo(t)
	svc := domain.Open(dir)

	code, out, errb := runCLI(t, dir, "bookmark", "add", "README.md")
	if code != 0 {
		t.Fatalf("bookmark add exit %d: %s", code, errb)
	}
	id := strings.TrimSpace(out)

	inbox := t.TempDir()
	livePresence(t, inbox)
	link := "gg://" + filepath.ToSlash(dir) + "?bookmark=" + id
	var sout, serrb bytes.Buffer
	if code := runSession(inbox, svc, []string{"navigate", link, "--no-wait"}, &sout, &serrb); code != 0 {
		t.Fatalf("exit = %d (stderr %q), want 0 — a hint-only link must not hit ErrRepoOnly", code, serrb.String())
	}
	got := steer.Drain(inbox)
	if len(got) != 1 {
		t.Fatalf("posted %d commands, want 1: %+v", len(got), got)
	}
	c := got[0]
	if c.Cmd != "navigate" || c.File != "" || c.Commit != "" || c.Target != nil {
		t.Fatalf("posted = %+v, want a hint-only navigate", c)
	}
	if c.HintKind != "bookmark" || c.HintID != id {
		t.Errorf("hint = %s/%s, want bookmark/%s", c.HintKind, c.HintID, id)
	}
}

// TestLinkResolveHintExitCodesDifferByAddressPresence is the S5 pair Task 6
// owes: ONE fixture, ONE absent shelf id ("nope"), and the two shapes must
// land in DIFFERENT places — an address-carrying link never fails on an
// absent hint (domain copies it blind), but an address-less link has
// nothing else to resolve to (spec §6) and is a hard error, mapped by
// cli.linkExit's existing ErrLink → exit 2 rule (ruling S11: "pin that with
// a CLI test rather than adding a new exit path").
func TestLinkResolveHintExitCodesDifferByAddressPresence(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	dir := newCLIRepo(t)
	svc := domain.Open(dir)
	head, _, err := svc.ResolveRev(context.Background(), "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	head = strings.TrimSpace(head)
	statePath := linkState(t)

	var out, errb bytes.Buffer
	withAddr := "gg://" + filepath.ToSlash(dir) + "/README.md@" + head + "?shelf=nope"
	if code := runLink(statePath, svc, dir, []string{"resolve", withAddr}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d (stderr %q), want 0 — an address-carrying link never fails on an absent hint", code, errb.String())
	}

	out.Reset()
	errb.Reset()
	addressLess := "gg://" + filepath.ToSlash(dir) + "?shelf=nope"
	if code := runLink(statePath, svc, dir, []string{"resolve", addressLess}, &out, &errb); code != 2 {
		t.Fatalf("exit = %d (stderr %q), want 2 — the hint was the link's only content and it is gone", code, errb.String())
	}
}

// TestLinkResolveDistinguishesRefFromPair pins what `gg link resolve` used to
// lose. Before this, a @ref: link and a @a..b link resolved to IDENTICAL
// output — both just `state: commit` plus B — so the single field that makes
// the two shapes different was the one a caller could not see. They must
// disagree on one fixture (ruling S5), in --json and in the terse line.
func TestLinkResolveDistinguishesRefFromPair(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	head := strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD"))
	if out, err := exec.Command("git", "-C", dir, "commit", "--allow-empty", "-m", "second").CombinedOutput(); err != nil {
		t.Fatalf("commit: %v\n%s", err, out)
	}
	head2 := strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD"))

	refCode, refOut, _ := runLinkCLI(t, dir, "resolve", "--json", "gg://"+dir+"@ref:main")
	pairCode, pairOut, _ := runLinkCLI(t, dir, "resolve", "--json", "gg://"+dir+"@"+head+".."+head2)
	if refCode != 0 || pairCode != 0 {
		t.Fatalf("exits = %d / %d, want 0 (both shapes resolve since Task 2)", refCode, pairCode)
	}
	if refOut == pairOut {
		t.Fatalf("a ref link and a pair link resolved IDENTICALLY:\n%s", refOut)
	}
	if !strings.Contains(refOut, `"ref":"main"`) {
		t.Errorf("ref JSON = %s, want the NAME (it is what travels, ruling R2)", refOut)
	}
	if strings.Contains(refOut, `"pair_a"`) {
		t.Errorf("ref JSON = %s, must not report a pair", refOut)
	}
	if !strings.Contains(pairOut, `"pair_a":"`+head+`"`) || !strings.Contains(pairOut, `"pair_b":"`+head2+`"`) {
		t.Errorf("pair JSON = %s, want both ends as full shas", pairOut)
	}
	if strings.Contains(pairOut, `"ref"`) {
		t.Errorf("pair JSON = %s, must not report a ref", pairOut)
	}

	// The terse line must distinguish them too: an agent reading stdout
	// rather than JSON needs the same answer.
	_, refTerse, _ := runLinkCLI(t, dir, "resolve", "gg://"+dir+"@ref:main")
	_, pairTerse, _ := runLinkCLI(t, dir, "resolve", "gg://"+dir+"@"+head+".."+head2)
	if !strings.Contains(refTerse, "ref main") {
		t.Errorf("ref terse = %q, want it to name the ref", refTerse)
	}
	if !strings.Contains(pairTerse, "pair "+head+".."+head2) {
		t.Errorf("pair terse = %q, want it to name both ends", pairTerse)
	}
}
