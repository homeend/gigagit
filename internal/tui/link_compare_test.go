package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

// stashPairRepo is refPairRepo plus ONE `-u` stash holding a tracked edit
// (a.txt) and an untracked file (scratch.txt). It returns the stash's pair.
func stashPairRepo(t *testing.T) (dir, parent, sha string) {
	t.Helper()
	dir, _, _, _ = refPairRepo(t)
	for name, body := range map[string]string{"a.txt": "a edited\n", "scratch.txt": "scratch body\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitRun(t, dir, "stash", "push", "-u", "-m", "wip")
	return dir, gitOut(t, dir, "rev-parse", "stash@{0}^1"), gitOut(t, dir, "rev-parse", "stash@{0}")
}

func rowTexts(m Model) []string {
	var out []string
	if m.filesView == nil {
		return nil
	}
	for _, l := range m.filesView.lines {
		if l.path != "" {
			out = append(out, l.status+" "+l.path)
		}
	}
	return out
}

func localLink(dir, suffix string) string {
	abs := filepath.ToSlash(dir)
	if !strings.HasPrefix(abs, "/") {
		abs = "/" + abs
	}
	return "gg://" + abs + suffix
}

// The window plan 3b-1 left open: `gg compare <stash link>` listed a -u
// stash's untracked file while the same link landed in the TUI did not,
// because the landing was a two-commit tree diff. Landing now goes through
// domain.CompareLinks, where the untracked file is a member of the set.
func TestLandingAStashPairListsItsUntrackedFile(t *testing.T) {
	t.Parallel()
	dir, parent, sha := stashPairRepo(t)
	m := refPairModel(t, dir)

	// The arms must differ, or this test could not see which one ran.
	l, _ := model.CommitEndpoint(parent)
	r, _ := model.CommitEndpoint(sha)
	tracked, err := m.svc.CompareFiles(context.Background(), l, r)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range tracked {
		if f.Path == "scratch.txt" {
			t.Fatal("fixture: the two-commit diff already lists scratch.txt — the arms do not differ")
		}
	}

	c := steer.Command{ID: "st-1", Cmd: "navigate", Target: &steer.Target{State: "pair", A: parent, B: sha}, Wait: true}
	m, cmd := m.applySteer(c)
	m = pumpAll(t, m, cmd)
	if got := strings.Join(rowTexts(m), "|"); got != "M a.txt|A scratch.txt" {
		t.Fatalf("rows = %q, want the tracked edit AND the untracked file", got)
	}
	if m.filesSets == nil {
		t.Error("a pair landing must open the set-shaped view")
	}
}

// An A row has no OLD side, but its NEW side is still read — from the member's
// own source. scratch.txt is not in the stash commit's tree at all (it lives
// in the stash's third parent), so reading it from the view's right endpoint
// fails.
func TestAnAddedUntrackedRowReadsItsOwnSource(t *testing.T) {
	t.Parallel()
	dir, parent, sha := stashPairRepo(t)
	m := refPairModel(t, dir)
	c := steer.Command{ID: "st-2", Cmd: "navigate", File: "scratch.txt",
		Target: &steer.Target{State: "pair", A: parent, B: sha}, Wait: true}
	m, cmd := m.applySteer(c)
	m = pumpAll(t, m, cmd)
	dv := m.diffLayer()
	if dv == nil {
		t.Fatal("the untracked file's diff must open")
	}
	if dv.err != nil {
		t.Fatalf("diff error = %v — the row was read from the stash commit, where the file does not exist", dv.err)
	}
	if !strings.Contains(m.View(), "scratch body") {
		t.Error("the diff must show the untracked file's own text")
	}
	r, ok := steer.AwaitReply(m.steerDir, "st-2", 3*time.Second)
	if !ok || !r.OK {
		t.Fatalf("reply = %+v ok=%v", r, ok)
	}
}

// The reply waits for the view: at dispatch the comparison has not run, and
// claiming "opened" then would be a lie whenever it goes on to fail.
func TestLandingAPairRepliesAfterTheViewOpens(t *testing.T) {
	t.Parallel()
	dir, c1, _, c3 := refPairRepo(t)
	m := refPairModel(t, dir)
	c := steer.Command{ID: "st-3", Cmd: "navigate", Target: &steer.Target{State: "pair", A: c1, B: "feat/x"}, Wait: true}
	m, cmd := m.applySteer(c)
	if m.pendingSteer == nil {
		t.Fatal("a pair landing must park until its comparison lands")
	}
	if m.filesView != nil {
		t.Fatal("the view cannot be open before the comparison has run")
	}
	if _, ok := steer.AwaitReply(m.steerDir, "st-3", 50*time.Millisecond); ok {
		t.Fatal("replied before the comparison ran")
	}
	m = pumpAll(t, m, cmd)
	r, ok := steer.AwaitReply(m.steerDir, "st-3", 3*time.Second)
	if !ok || !r.OK || !strings.Contains(r.Detail, c1[:7]+".."+c3[:7]) {
		t.Fatalf("reply = %+v ok=%v, want ok naming %s..%s", r, ok, c1[:7], c3[:7])
	}
	if m.pendingSteer != nil {
		t.Error("the pending navigate must be drained")
	}
}

// gg://r@sha and gg://r/f.go@sha resolve to the SAME endpoint. Tagged by
// endpoint, the second open is "already showing" and silently does nothing.
func TestLinkCompareTagIsTheTextsNotTheEndpoints(t *testing.T) {
	t.Parallel()
	dir, c1, _, c3 := refPairRepo(t)
	m := refPairModel(t, dir)
	right := localLink(dir, "@"+c3)

	m, cmd := m.startLinkCompare(localLink(dir, "@"+c1), right)
	m = pumpAll(t, m, cmd)
	if got := strings.Join(rowTexts(m), "|"); got != "A b.txt|A c.txt" {
		t.Fatalf("whole-tree rows = %q", got)
	}
	m, cmd = m.startLinkCompare(localLink(dir, "/c.txt@"+c1), right)
	if cmd == nil {
		t.Fatal("a file link sharing the first link's endpoint was taken for the comparison already showing")
	}
	m = pumpAll(t, m, cmd)
	if got := strings.Join(rowTexts(m), "|"); got != "A c.txt" {
		t.Fatalf("file-link rows = %q, want the second comparison to REPLACE the first", got)
	}
	if _, cmd = m.startLinkCompare(localLink(dir, "/c.txt@"+c1), right); cmd != nil {
		t.Error("re-asking for exactly the comparison on screen must be a no-op")
	}
}

// Spec §5 "one door", the TUI leg: the expectation the domain, MCP and CLI
// legs share — a local-form file link is one file, never the whole tree.
func TestLocalFormFileLinksCompareAsOneFileInTheTUI(t *testing.T) {
	t.Parallel()
	dir, c1, _, c3 := refPairRepo(t)
	m := refPairModel(t, dir)
	m, cmd := m.startLinkCompare(localLink(dir, "/b.txt@"+c1), localLink(dir, "/b.txt@"+c3))
	m = pumpAll(t, m, cmd)
	if got := strings.Join(rowTexts(m), "|"); got != "A b.txt" {
		t.Fatalf("rows = %q, want exactly b.txt", got)
	}
}

func TestAStaleLinkCompareLoadIsDropped(t *testing.T) {
	t.Parallel()
	dir, c1, c2, c3 := refPairRepo(t)
	m := refPairModel(t, dir)
	m, first := m.startLinkCompare(localLink(dir, "@"+c1), localLink(dir, "@"+c2))
	m, second := m.startLinkCompare(localLink(dir, "@"+c1), localLink(dir, "@"+c3))
	// The OLDER load lands last — the order that would clobber the newer view.
	m = pumpAll(t, m, second)
	m = pumpAll(t, m, first)
	if got := strings.Join(rowTexts(m), "|"); got != "A b.txt|A c.txt" {
		t.Fatalf("rows = %q, want the NEWER comparison (c1..c3) still on screen", got)
	}
}

func TestAFailedLinkCompareIsRetryable(t *testing.T) {
	t.Parallel()
	dir, c1, _, _ := refPairRepo(t)
	m := refPairModel(t, dir)
	left, bad := localLink(dir, "@"+c1), "gg://r@not a target"
	m, cmd := m.startLinkCompare(left, bad)
	m = pumpAll(t, m, cmd)
	if m.filesView != nil {
		t.Fatal("a failed comparison must not open a view")
	}
	if !strings.Contains(m.statusMsg, "bad gg link") {
		t.Errorf("status = %q, want the failure said", m.statusMsg)
	}
	if m.linkCompareWant != "" {
		t.Error("a failed load must clear the want, or the same pair can never be asked again")
	}
}

// A view opened any other way forgets the sets: compareSides would otherwise
// read the NEXT compare's rows from the previous comparison's sources.
func TestAnOrdinaryCompareAfterALinkCompareForgetsTheSets(t *testing.T) {
	t.Parallel()
	dir, c1, c2, c3 := refPairRepo(t)
	m := refPairModel(t, dir)
	m, cmd := m.startLinkCompare(localLink(dir, "@"+c1), localLink(dir, "@"+c3))
	m = pumpAll(t, m, cmd)
	if m.filesSets == nil {
		t.Fatal("fixture: the link compare did not open")
	}
	l, _ := model.CommitEndpoint(c1)
	r, _ := model.CommitEndpoint(c2)
	m, _ = m.openCompareFiles(l, r)
	if m.filesSets != nil {
		t.Error("an endpoint compare must not inherit a link compare's sets")
	}
}

func TestPointLinkRefusesAShortSha(t *testing.T) {
	t.Parallel()
	dir, c1, _, _ := refPairRepo(t)
	m := refPairModel(t, dir)
	if _, ok := m.pointLinkFor(c1[:7]); ok {
		t.Error("a producer never emits an abbreviated sha")
	}
	text, ok := m.pointLinkFor(c1)
	if !ok || !strings.HasSuffix(text, "@"+c1) {
		t.Errorf("pointLinkFor = %q %v", text, ok)
	}
}
