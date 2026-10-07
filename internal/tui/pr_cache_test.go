package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/clock"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// The PR tab appears from the cached listing before the forge answers; a
// live read that already landed is never overwritten by the cached rows.
func TestPRTabDrawsTheCachedListFirst(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	m.svc.SeedPRListing("github", testPRs())
	msg := m.cachedPRsCmd()()
	cached, ok := msg.(prsCachedMsg)
	if !ok {
		t.Fatalf("cachedPRsCmd = %T, want prsCachedMsg", msg)
	}
	nm, _ := m.Update(cached)
	mm := nm.(Model)
	if !mm.forgeShown || len(mm.prs) != len(testPRs()) || mm.forgeProvider != "github" {
		t.Fatalf("after the cached list: shown=%v prs=%d provider=%q", mm.forgeShown, len(mm.prs), mm.forgeProvider)
	}
	live := m
	live.prs, live.prsLoaded, live.forgeShown = testPRs()[:1], true, true
	nm, _ = live.Update(cached)
	if got := nm.(Model).prs; len(got) != 1 {
		t.Fatalf("a cached list overwrote a live read: %d rows", len(got))
	}
	stale := m
	stale.prsGen++
	nm, _ = stale.Update(cached)
	if nm.(Model).forgeShown {
		t.Fatal("a cached list from before a repo switch must be dropped")
	}
}

// One refresh drives both the comments and the freshness mark.
func TestPRRefreshDrivesCommentsAndFreshness(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t) // the open started one refresh
	if !strings.Contains(m.prFreshnessSuffix(), i18n.T("refreshing…")) {
		t.Fatalf("suffix while in flight = %q", m.prFreshnessSuffix())
	}
	if !strings.Contains(m.View(), "PR #7 · x · refresh") { // the narrow test pane cuts the tail
		t.Fatal("the freshness mark must be drawn in the files title")
	}
	nm, cmd := m.Update(prRevalidatedMsg{n: 7, gen: m.prsGen, pr: model.PullRequest{Number: 7, State: "open"}, commentsChanged: true})
	m2 := nm.(Model)
	if cmd == nil {
		t.Fatal("changed comments must schedule a notes/counts reload")
	}
	if m2.prFreshnessSuffix() != "" || m2.prCommentsInflight || m2.prRevalidateInflight {
		t.Fatalf("after a good refresh: suffix %q, comments %v, revalidate %v", m2.prFreshnessSuffix(), m2.prCommentsInflight, m2.prRevalidateInflight)
	}
	m2, _ = m2.prCommentsCmd(false)
	nm, _ = m2.Update(prRevalidatedMsg{n: 7, gen: m2.prsGen, err: errors.New("offline"), readAt: clock.Now().Add(-3 * time.Hour)})
	if got := nm.(Model).prFreshnessSuffix(); !strings.Contains(got, "3") {
		t.Fatalf("offline suffix = %q", got)
	}
}

// Review focus 5: an answer for the previous repository is dropped.
func TestPRRefreshAfterARepoSwitchIsDropped(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	gen := m.prsGen
	m.prsGen++ // what a repo switch does
	nm, cmd := m.Update(prRevalidatedMsg{n: 7, gen: gen, moved: true, commentsChanged: true, pr: model.PullRequest{Number: 7}})
	mm := nm.(Model)
	if cmd != nil || mm.prRevalidateInflight || mm.prCommentsInflight {
		t.Fatalf("a stale-generation refresh must be dropped and free the flags (cmd=%v)", cmd != nil)
	}
}

// feedCompareFiles runs cmd and feeds only its compare-file lists back (the
// forge reads it also starts cannot answer in a test).
func feedCompareFiles(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for _, msg := range flattenCmd(t, cmd) {
		if cf, ok := msg.(compareFilesMsg); ok {
			nm, _ := m.Update(cf)
			m = nm.(Model)
		}
	}
	return m
}

func diffLayerCount(m Model) int {
	n := 0
	if m.layers != nil {
		for _, l := range m.layers.entries {
			if _, ok := l.(*diffView); ok {
				n++
			}
		}
	}
	return n
}

// After a moved-head reopen the files cursor is back on the same path, and an
// open diff reopens on that file — once, not stacked on the stale one.
func TestMovedHeadKeepsTheFileAndDiff(t *testing.T) {
	t.Parallel()
	m, dir, _ := mergePreviewModel(t)
	runGit(t, dir, "checkout", "-q", "feat/x")
	for _, f := range []string{"b.txt", "c.txt"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte(f+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-q", "-m", "b and c")
	runGit(t, dir, "checkout", "-q", "main")
	runGit(t, dir, "update-ref", "refs/gg/pr/7", "feat/x")
	pr := model.PullRequest{Number: 7, Title: "x", State: model.PRStateOpen, Target: "main"}
	m.forgeShown, m.prs = true, []model.PullRequest{pr}

	nm, cmd := m.Update(m.openPRPreviewCmd(pr)())
	m = feedCompareFiles(t, nm.(Model), cmd)
	for i, l := range m.filesView.visible() {
		if l.path == "b.txt" {
			m.filesView.sel = i
		}
	}
	tm, _ := m.openDiffForFileLine(m.filesView.visible()[m.filesView.sel])
	m = tm.(Model)
	if m.diffLayer() == nil || m.previewSelectedPath() != "b.txt" {
		t.Fatal("setup: a diff of b.txt is open")
	}

	// The PR's head moves (b.txt changes again); the refresh says so.
	runGit(t, dir, "checkout", "-q", "feat/x")
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "commit", "-q", "-am", "b2")
	runGit(t, dir, "checkout", "-q", "main")
	runGit(t, dir, "update-ref", "refs/gg/pr/7", "feat/x")
	m.prRevalidateInflight, m.prCommentsInflight = false, false
	nm, _ = m.Update(prRevalidatedMsg{n: 7, gen: m.prsGen, moved: true, pr: pr})
	m = nm.(Model)
	if r := m.prReland; r == nil || r.path != "b.txt" || !r.diff {
		t.Fatalf("reland = %+v", m.prReland)
	}
	// The fetch landed: the open chain resolves the new pair.
	nm, cmd = m.Update(m.openPRPreviewCmd(pr)())
	m = feedCompareFiles(t, nm.(Model), cmd)
	if got := m.previewSelectedPath(); got != "b.txt" {
		t.Fatalf("cursor on %q after the reopen, want b.txt", got)
	}
	if m.prReland != nil {
		t.Fatal("reland must be consumed once")
	}
	if n := diffLayerCount(m); n != 1 {
		t.Fatalf("diff layers after the reopen = %d, want exactly 1", n)
	}
}

// A successful list refresh schedules a background prefetch; prefetch = 0
// and a failed list schedule nothing.
func TestPRListRefreshSchedulesPrefetch(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	ok := domain.ForgeStatus{Provider: "github"}
	has := func(cmd tea.Cmd) bool {
		for _, msg := range flattenCmd(t, cmd) {
			if _, ok := msg.(prPrefetchedMsg); ok {
				return true
			}
		}
		return false
	}
	_, cmd := m.handlePRsLoaded(prsLoadedMsg{gen: m.prsGen, bg: true, status: ok, prs: testPRs()})
	if !has(cmd) {
		t.Fatal("a successful list refresh must schedule a prefetch")
	}
	off := m
	off.cfg.Forge.Prefetch = intPtr(0)
	if _, cmd = off.handlePRsLoaded(prsLoadedMsg{gen: off.prsGen, bg: true, status: ok, prs: testPRs()}); has(cmd) {
		t.Fatal("prefetch = 0 must schedule nothing")
	}
	if _, cmd = m.handlePRsLoaded(prsLoadedMsg{gen: m.prsGen, bg: true, status: ok, err: errors.New("502")}); has(cmd) {
		t.Fatal("a failed list must not prefetch")
	}
}

// Review finding 3: the TUI owns its prefetch — a repo switch (and quit)
// cancels it.
func TestPRPrefetchIsCancelledOnRepoSwitch(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	m, cmd := m.prPrefetchCmd()
	if cmd == nil || m.prPrefetch == nil {
		t.Fatal("prefetch not started")
	}
	ctx := m.prPrefetch.ctx
	if ctx.Err() != nil {
		t.Fatal("cancelled at start")
	}
	m = m.stopPRPrefetch()
	if ctx.Err() == nil || m.prPrefetch != nil {
		t.Fatal("stopPRPrefetch must cancel the running prefetch")
	}
	m, _ = m.prPrefetchCmd()
	ctx = m.prPrefetch.ctx
	top, err := m.svc.TopLevel(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	nm, _ := m.reRoot(top)
	if ctx.Err() == nil {
		t.Fatal("a repo switch must cancel the old repo's prefetch")
	}
	_ = nm
}

// A prefetch may already have fetched the moved head into the local ref, so
// the domain says "not moved": the view still shows the OLD head and must
// follow the forge's head all the same.
func TestMovedIsJudgedAgainstTheHeadOnScreen(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m.prRevalidateInflight, m.prCommentsInflight = false, false
	shown := m.previewOpen.srcHash
	if shown == "" {
		t.Fatal("setup: the open view has no head hash")
	}
	pr := model.PullRequest{Number: 7, State: model.PRStateOpen, HeadSHA: strings.Repeat("e", 40)}
	nm, cmd := m.Update(prRevalidatedMsg{n: 7, gen: m.prsGen, moved: false, pr: pr})
	if mm := nm.(Model); cmd == nil || mm.prRevalidateSkip != 7 {
		t.Fatalf("a forge head unlike the one on screen must re-open (cmd=%v skip=%d)", cmd != nil, mm.prRevalidateSkip)
	}
	pr.HeadSHA = shown
	// A good refresh still asks about interrupted sends (plan 3): "nothing to
	// do" is no reopen, read off the reopen's own marker.
	if nm, _ := m.Update(prRevalidatedMsg{n: 7, gen: m.prsGen, moved: false, pr: pr}); nm.(Model).prRevalidateSkip != 0 {
		t.Fatal("the head on screen is the forge's: nothing to do")
	}
}
