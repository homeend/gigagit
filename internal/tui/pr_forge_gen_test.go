package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// Item 1 / Review Focus 1: a PR-list read that starts while the open PR's
// refresh is in flight bumps prsGen; the refresh answer must still land.
func TestAListReadDoesNotDropAPRRefresh(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m = revalidated(m, 7, 1, false) // the open's first read
	m.prRevalidateInflight, m.prCommentsInflight, m.prRefreshing = true, true, true
	m.prReadSeq = 2
	m.prsGen++ // r / the heartbeat started a list read
	nm, _ := m.Update(prRevalidatedMsg{n: 7, gen: m.forgeGen, seq: 2, pr: model.PullRequest{Number: 7}, commentsChanged: true})
	mm := nm.(Model)
	if mm.prUpdated != 7 || mm.prRevalidateInflight || mm.prCommentsInflight || mm.prRefreshing {
		t.Fatalf("updated=%d inflight=%v/%v refreshing=%v", mm.prUpdated, mm.prRevalidateInflight, mm.prCommentsInflight, mm.prRefreshing)
	}
}

// Item 1: the old repo's answer landing after R must not free the new repo's
// read ("refreshing…" vanishing early, a second read starting).
func TestAnOldRepoAnswerLeavesTheNewReadAlone(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	old := m.forgeGen
	m.forgeGen++                                                                    // R
	m.prRevalidateInflight, m.prCommentsInflight, m.prRefreshing = true, true, true // the new repo's read
	nm, _ := m.Update(prRevalidatedMsg{n: 7, gen: old, pr: model.PullRequest{Number: 7}})
	mm := nm.(Model)
	if !mm.prRevalidateInflight || !mm.prCommentsInflight || !mm.prRefreshing {
		t.Fatalf("inflight=%v/%v refreshing=%v", mm.prRevalidateInflight, mm.prCommentsInflight, mm.prRefreshing)
	}
}

// Item 1: R frees the read slot and forgets the reland and the revalidate
// skip — a same-numbered PR in the next repo must not consume them.
func TestReRootFreesThePRReadSlot(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m.prRevalidateInflight, m.prCommentsInflight = true, true
	m.prReland, m.prRevalidateSkip = &prReland{n: 7, path: "a.go"}, 7
	nm, _ := m.reRoot(t.TempDir())
	mm := nm.(Model)
	if mm.prRevalidateInflight || mm.prCommentsInflight || mm.prReland != nil || mm.prRevalidateSkip != 0 {
		t.Fatalf("inflight=%v/%v reland=%v skip=%d", mm.prRevalidateInflight, mm.prCommentsInflight, mm.prReland, mm.prRevalidateSkip)
	}
}

// F-e: the queued post-send read is consumed by the read that lands, even
// when its PR is no longer on screen.
func TestARefreshAgainDiesWithAClosedView(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m = revalidated(m, 7, 1, false)
	m.prRefreshAgain = 7
	m.previewOpen = nil // the PR view closed while the read ran
	nm, _ := m.Update(prRevalidatedMsg{n: 7, gen: m.forgeGen, pr: model.PullRequest{Number: 7}})
	if mm := nm.(Model); mm.prRefreshAgain != 0 {
		t.Fatalf("prRefreshAgain = %d", mm.prRefreshAgain)
	}
}

// F-g: a Reply & send whose draft was written before R does not send in the
// new repository.
func TestReplyAndSendAfterRDoesNotSend(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	old := m.forgeGen
	m.forgeGen++ // R
	nm, _ := m.Update(noteMutatedMsg{sendPR: 7, sendID: "d1", sendGen: old})
	if mm := nm.(Model); strings.Contains(mm.statusMsg, "preparing the send") {
		t.Fatalf("status = %q", mm.statusMsg)
	}
}

// B4: the read a post-send re-read waited for FAILED: the re-read is still
// issued (once — the flag is cleared first).
func TestAFailedReadStillIssuesTheQueuedReRead(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m = revalidated(m, 7, 1, false)
	m.prRefreshAgain = 7
	nm, cmd := m.Update(prRevalidatedMsg{n: 7, gen: m.forgeGen, pr: model.PullRequest{Number: 7}, err: errors.New("offline")})
	mm := nm.(Model)
	if mm.prRefreshAgain != 0 || !mm.prRevalidateInflight || cmd == nil {
		t.Fatalf("again=%d inflight=%v cmd=%v", mm.prRefreshAgain, mm.prRevalidateInflight, cmd != nil)
	}
}
