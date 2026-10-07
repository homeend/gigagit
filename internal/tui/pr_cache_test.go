package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/clock"
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
