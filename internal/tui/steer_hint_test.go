package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/gittest"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

// --- Task 6: a gg:// link's ?bookmark=/?shelf= hint is honoured on
// navigate (spec §3.3). SERIAL: hintNavModel sets domain.BookmarkStatePath/
// ShelfStatePath, process-global test seams, so every test built on it must
// NOT call t.Parallel() — Go pauses every parallel test in this package
// until the serial ones finish, so this is safe beside them.

// hintNavModel is refPairModel (steer_nav_test.go) plus a hermetic,
// per-test bookmark/shelf store — never the real user's.
func hintNavModel(t *testing.T, dir string) Model {
	t.Helper()
	oldB, oldS := domain.BookmarkStatePath, domain.ShelfStatePath
	domain.BookmarkStatePath, domain.ShelfStatePath = t.TempDir(), t.TempDir()
	t.Cleanup(func() { domain.BookmarkStatePath, domain.ShelfStatePath = oldB, oldS })
	return refPairModel(t, dir)
}

// TestSteerCommandForLinkHintOnly pins ruling S13 at steerCommandForLink,
// the `--at` startup path's own command builder (the brief singles this out
// as ruling S2's lesson applied a second time: several early returns, the
// hint must be set once at the top).
func TestSteerCommandForLinkHintOnly(t *testing.T) {
	t.Parallel()
	l := model.Link{
		Repo: model.LinkRepo{Abs: "/tmp/repo"},
		Hint: model.LinkHint{Kind: "bookmark", ID: "b1"},
	}
	c, ok := steerCommandForLink(l)
	if !ok {
		t.Fatal("a hint-only link must build a command (S13), not refuse")
	}
	if c.Cmd != "navigate" || c.File != "" || c.Commit != "" || c.Target != nil {
		t.Fatalf("command = %+v, want a hint-only navigate (no File/Commit/Target)", c)
	}
	if c.HintKind != "bookmark" || c.HintID != "b1" {
		t.Errorf("hint = %s/%s, want bookmark/b1", c.HintKind, c.HintID)
	}
}

// TestSteerCommandForLinkCarriesHintAlongsideAFile pins the with-address row:
// the hint must not change WHERE the link lands.
func TestSteerCommandForLinkCarriesHintAlongsideAFile(t *testing.T) {
	t.Parallel()
	l := model.Link{
		Repo:   model.LinkRepo{Abs: "/tmp/repo"},
		Path:   "a.txt",
		Target: model.LinkTarget{State: model.StateUnstaged},
		Hint:   model.LinkHint{Kind: "shelf", ID: "s1"},
	}
	c, ok := steerCommandForLink(l)
	if !ok {
		t.Fatal("steerCommandForLink refused an ordinary file link")
	}
	if c.File != "a.txt" {
		t.Errorf("File = %q, want a.txt (the hint must not change WHERE it lands)", c.File)
	}
	if c.HintKind != "shelf" || c.HintID != "s1" {
		t.Errorf("hint = %s/%s, want shelf/s1", c.HintKind, c.HintID)
	}
}

// TestNavigateLandedStagesPendingHintForBookmark pins navigateLanded's
// bookmark arm: the reply answers the landing at once (never blocked on the
// reveal), and a pendingHint is staged that does NOT require a second
// answer — the with-address shape already answered.
func TestNavigateLandedStagesPendingHintForBookmark(t *testing.T) {
	dir := gittest.BasicRepo(t, "hi\n")
	m := hintNavModel(t, dir)
	c := steer.Command{ID: "x", Cmd: "navigate", File: "README.md", HintKind: "bookmark", HintID: "b1", Wait: true}
	nm, cmd := m.navigateLanded(c, "opened README.md")
	if nm.pendingHint == nil || nm.pendingHint.cmd.HintKind != "bookmark" || nm.pendingHint.cmd.HintID != "b1" {
		t.Fatalf("pendingHint = %+v, want staged for bookmark/b1", nm.pendingHint)
	}
	if nm.pendingHint.mustAnswer {
		t.Error("the with-address shape must not require a second answer (already answered)")
	}
	if nm.pendingHint.tag == 0 {
		t.Error("fix F3: tag must be a fresh, nonzero m.hintGen value — 0 collides with an ordinary load's default gen")
	}
	if nm.pendingHint.tag != nm.hintGen {
		t.Errorf("tag = %d, want the model's own hintGen %d", nm.pendingHint.tag, nm.hintGen)
	}
	if cmd == nil {
		t.Fatal("navigateLanded must still return the landing's own reply command")
	}
}

// TestNavigateLandedDegradesAnUnrevealableHintKind pins S9's explicit
// default for the with-address shape: "stash" (no producer, spec §3.4) must
// land normally and set a degrade notice, never silently drop the hint or
// touch the reply.
func TestNavigateLandedDegradesAnUnrevealableHintKind(t *testing.T) {
	dir := gittest.BasicRepo(t, "hi\n")
	m := hintNavModel(t, dir)
	c := steer.Command{ID: "x", Cmd: "navigate", File: "README.md", HintKind: "stash", HintID: "3"}
	nm, cmd := m.navigateLanded(c, "opened README.md")
	if nm.pendingHint != nil {
		t.Error("a kind this build cannot reveal must not stage a pendingHint")
	}
	if nm.statusMsg == "" {
		t.Error("must set a degrade notice (S9's explicit default)")
	}
	if cmd == nil && c.ID == "" {
		// c.ID == "" and OK==true means answerSteer returns nil — that is
		// still correct (nobody is waiting), just noted here so a future
		// edit that expects a non-nil reply does not misread this line.
		t.Log("nil reply is expected for an id-less OK command")
	}
}

// TestBookmarksLoadedMsgRevealsThePendingHintRow is the PRESENT half of the
// with-address consumer test: the reveal must select the RIGHT row, not
// just open a popup. Fix F8: this fixture used to bookmark a FABRICATED
// commit ("deadbeef…") that does not exist in the repo, and BookmarkAdd
// does a real `git rev-parse <commit>:<path>` to record the blob — the
// fixture could never stand up, so the newest-first ordering assumption
// below (taken from BookmarkList's doc comment) had never actually
// executed. Both bookmarks now use the fixture's REAL HEAD commit, with
// explicit, strictly-ordered Created timestamps so the ordering is
// deterministic rather than racing on time.Now()'s clock resolution.
func TestBookmarksLoadedMsgRevealsThePendingHintRow(t *testing.T) {
	dir := gittest.BasicRepo(t, "hi\n")
	m := hintNavModel(t, dir)
	ctx := context.Background()
	head, _, err := m.svc.ResolveRev(ctx, "HEAD")
	if err != nil {
		t.Fatalf("ResolveRev: %v", err)
	}
	head = strings.TrimSpace(head)
	older, err := m.svc.BookmarkAdd(ctx, model.Bookmark{
		State: model.StateUnstaged, Worktree: dir, Path: "README.md",
		Created: time.Now().Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("BookmarkAdd (older): %v", err)
	}
	newer, err := m.svc.BookmarkAdd(ctx, model.Bookmark{
		State: model.StateCommitted, Commit: head, Path: "README.md",
		Created: time.Now(),
	})
	if err != nil {
		t.Fatalf("BookmarkAdd (newer): %v", err)
	}
	items, err := m.svc.BookmarkList(ctx, 0, 0)
	if err != nil {
		t.Fatalf("BookmarkList: %v", err)
	}
	// Confirmed empirically now, not restated from the doc comment: newest
	// first means the LATER Created timestamp sorts to index 0.
	if len(items) != 2 || items[0].ID != newer.ID || items[1].ID != older.ID {
		t.Fatalf("fixture: items = %+v, want newer at 0 and older (the hinted one) at 1", items)
	}
	m.hintGen++
	m.pendingHint = &pendingHint{cmd: steer.Command{ID: "n1", HintKind: "bookmark", HintID: older.ID}, tag: m.hintGen}
	tm, _ := m.Update(bookmarksLoadedMsg{items: items, gen: m.hintGen})
	nm := tm.(Model)
	p := nm.bookmarkSwitcher()
	if p == nil {
		t.Fatal("a present hint must open the bookmark popup")
	}
	if p.sel != 1 {
		t.Errorf("sel = %d, want 1 (the hinted bookmark's real index)", p.sel)
	}
	got, ok := p.selected()
	if !ok || got.ID != older.ID {
		t.Fatalf("selected = %+v ok=%v, want %s", got, ok, older.ID)
	}
	if nm.pendingHint != nil {
		t.Error("pendingHint must be cleared once consumed")
	}
}

// TestBookmarksLoadedMsgIgnoresAnUnrelatedLoadInFlight pins fix F3's race:
// the user presses `g` (an ordinary load, gen 0, already in flight) just
// before a hinted navigate lands (stages pendingHint, tag N, fires its OWN
// load). The ordinary load's message must arrive WITHOUT consuming the
// hint or resetting anything — only the message carrying the matching gen
// may.
func TestBookmarksLoadedMsgIgnoresAnUnrelatedLoadInFlight(t *testing.T) {
	dir := gittest.BasicRepo(t, "hi\n")
	m := hintNavModel(t, dir)
	ctx := context.Background()
	b, err := m.svc.BookmarkAdd(ctx, model.Bookmark{State: model.StateUnstaged, Worktree: dir, Path: "README.md"})
	if err != nil {
		t.Fatalf("BookmarkAdd: %v", err)
	}
	items, err := m.svc.BookmarkList(ctx, 0, 0)
	if err != nil {
		t.Fatalf("BookmarkList: %v", err)
	}
	m.hintGen++
	tag := m.hintGen
	m.pendingHint = &pendingHint{cmd: steer.Command{ID: "n1", HintKind: "bookmark", HintID: b.ID}, tag: tag}

	// The unrelated load's message: gen 0 (an ordinary loadBookmarksCmd
	// never sets it), arriving BEFORE the hint's own.
	tm, _ := m.Update(bookmarksLoadedMsg{items: items, gen: 0})
	nm := tm.(Model)
	if nm.pendingHint == nil {
		t.Fatal("an unrelated load (gen 0) must NOT consume the pendingHint")
	}
	if nm.pendingHint.tag != tag {
		t.Fatalf("pendingHint = %+v, want it untouched (tag %d)", nm.pendingHint, tag)
	}
	// It still opens the popup normally (the ordinary branch) — just
	// without disturbing the parked hint.
	if p := nm.bookmarkSwitcher(); p == nil {
		t.Error("the unrelated load must still open the switcher through the ordinary branch")
	}

	// NOW the hint's own load arrives (gen == tag): it must still reveal
	// correctly, proving the hint survived the earlier, unrelated message.
	tm2, _ := nm.Update(bookmarksLoadedMsg{items: items, gen: tag})
	nm2 := tm2.(Model)
	if nm2.pendingHint != nil {
		t.Error("the matching-gen message must consume the pendingHint")
	}
	p := nm2.bookmarkSwitcher()
	if p == nil {
		t.Fatal("the hint's own load must reveal the bookmark popup")
	}
	got, ok := p.selected()
	if !ok || got.ID != b.ID {
		t.Fatalf("selected = %+v ok=%v, want %s", got, ok, b.ID)
	}
}

// TestBookmarksLoadedMsgAbsentHintNoticesWithoutOpeningAPopup is the ABSENT
// half: no popup, a notice, the navigate itself is untouched (it already
// answered elsewhere — mustAnswer is false here).
func TestBookmarksLoadedMsgAbsentHintNoticesWithoutOpeningAPopup(t *testing.T) {
	dir := gittest.BasicRepo(t, "hi\n")
	m := hintNavModel(t, dir)
	m.hintGen++
	m.pendingHint = &pendingHint{cmd: steer.Command{ID: "n1", HintKind: "bookmark", HintID: "nope"}, tag: m.hintGen}
	tm, cmd := m.Update(bookmarksLoadedMsg{items: nil, gen: m.hintGen})
	nm := tm.(Model)
	if p := nm.bookmarkSwitcher(); p != nil {
		t.Error("an absent hint must NOT open the popup")
	}
	// Fix F2: the with-address absent case DID land (elsewhere) — the
	// wording must say so, distinct from the mustAnswer wording below.
	if want := i18n.T("that link's bookmark is gone; it still landed"); nm.statusMsg != want {
		t.Errorf("statusMsg = %q, want %q", nm.statusMsg, want)
	}
	if nm.pendingHint != nil {
		t.Error("pendingHint must be cleared")
	}
	if cmd != nil {
		t.Error("the with-address absent case must not post a second reply (mustAnswer is false)")
	}
}

// TestBookmarksLoadedMsgAbsentMustAnswerHintUsesTheNoLandingWording pins fix
// F2 directly at the message-handler level (the end-to-end twin,
// TestSteerNavigateHintOnlyAbsentAnswersFail, only checks the steer reply):
// for the hint-only shape nothing landed at all, so the status bar must NOT
// claim "it still landed" — that would contradict the steerFail the agent
// receives on the very same command.
func TestBookmarksLoadedMsgAbsentMustAnswerHintUsesTheNoLandingWording(t *testing.T) {
	dir := gittest.BasicRepo(t, "hi\n")
	m := hintNavModel(t, dir)
	m.hintGen++
	// Wait: true is load-bearing, not decoration: answerSteer returns a nil
	// Cmd for a command nobody is waiting on (`!c.Wait || m.steerDir == ""`,
	// steer.go:248), so without it the reply assertion below can never hold
	// however right the handler is.
	m.pendingHint = &pendingHint{cmd: steer.Command{ID: "n1", HintKind: "bookmark", HintID: "nope", Wait: true}, mustAnswer: true, tag: m.hintGen}
	tm, cmd := m.Update(bookmarksLoadedMsg{items: nil, gen: m.hintGen})
	nm := tm.(Model)
	if want := i18n.T("that link's bookmark could not be found"); nm.statusMsg != want {
		t.Errorf("statusMsg = %q, want %q (never \"it still landed\" — nothing landed)", nm.statusMsg, want)
	}
	if strings.Contains(nm.statusMsg, "still landed") {
		t.Errorf("statusMsg = %q must not claim anything landed", nm.statusMsg)
	}
	if cmd == nil {
		t.Fatal("the hint-only shape must still answer (steerFail) — this is its only reply")
	}
}

// TestShelfLoadedMsgRevealsThePendingHintRow is the shelf twin of the
// bookmark reveal test above.
func TestShelfLoadedMsgRevealsThePendingHintRow(t *testing.T) {
	dir := gittest.BasicRepo(t, "hi\n")
	m := hintNavModel(t, dir)
	ctx := context.Background()
	e, err := m.svc.ShelfAdd(ctx, model.FileAddress{State: model.StateUnstaged, Worktree: dir, Path: "README.md"}, "")
	if err != nil {
		t.Fatalf("ShelfAdd: %v", err)
	}
	items, err := m.svc.ShelfList(ctx, "", 0, 0)
	if err != nil {
		t.Fatalf("ShelfList: %v", err)
	}
	m.hintGen++
	m.pendingHint = &pendingHint{cmd: steer.Command{ID: "n2", HintKind: "shelf", HintID: e.ID}, tag: m.hintGen}
	tm, _ := m.Update(shelfLoadedMsg{entries: items, open: true, gen: m.hintGen})
	nm := tm.(Model)
	p := nm.shelfSwitcher()
	if p == nil {
		t.Fatal("a present hint must open the shelf popup")
	}
	got, ok := p.selected()
	if !ok || got.ID != e.ID {
		t.Fatalf("selected = %+v ok=%v, want %s", got, ok, e.ID)
	}
	if nm.pendingHint != nil {
		t.Error("pendingHint must be cleared once consumed")
	}
}

// TestShelfLoadedMsgAbsentHintNoticesWithoutOpeningAPopup is the shelf twin
// of the bookmark absence test.
func TestShelfLoadedMsgAbsentHintNoticesWithoutOpeningAPopup(t *testing.T) {
	dir := gittest.BasicRepo(t, "hi\n")
	m := hintNavModel(t, dir)
	m.hintGen++
	m.pendingHint = &pendingHint{cmd: steer.Command{ID: "n2", HintKind: "shelf", HintID: "nope"}, tag: m.hintGen}
	tm, _ := m.Update(shelfLoadedMsg{entries: nil, open: true, gen: m.hintGen})
	nm := tm.(Model)
	if p := nm.shelfSwitcher(); p != nil {
		t.Error("an absent hint must NOT open the popup")
	}
	if want := i18n.T("that link's shelf entry is gone; it still landed"); nm.statusMsg != want {
		t.Errorf("statusMsg = %q, want %q", nm.statusMsg, want)
	}
	if nm.pendingHint != nil {
		t.Error("pendingHint must be cleared")
	}
}

// TestShelfLoadedMsgHintInANonDefaultBucketReveals pins fix F1: domain's
// ShelfFind scans EVERY bucket, but the ordinary ShelfList("", …) any plain
// `G` open uses lists the DEFAULT bucket only. An entry `gg shelf add
// --bucket wip` put anywhere else must still reveal — the exact repro the
// controller ran end to end (`gg open --web` reporting a present entry
// "gone"). This is the S5 pair the brief asked for: the SAME id must not be
// "present" to domain (ShelfFind, checked first) and "gone" to the
// consumer (checked second, on the SAME store).
func TestShelfLoadedMsgHintInANonDefaultBucketReveals(t *testing.T) {
	dir := gittest.BasicRepo(t, "hi\n")
	m := hintNavModel(t, dir)
	ctx := context.Background()
	e, err := m.svc.ShelfAdd(ctx, model.FileAddress{State: model.StateUnstaged, Worktree: dir, Path: "README.md"}, "wip")
	if err != nil {
		t.Fatalf("ShelfAdd(bucket=wip): %v", err)
	}
	if e.Bucket != "wip" {
		t.Fatalf("fixture: entry bucket = %q, want wip", e.Bucket)
	}
	// Domain's own presence check: this is what ResolveLink's address-less
	// hint check (and evallink.go) both call, and it must agree the entry
	// is present.
	found, err := m.svc.ShelfFind(ctx, e.ID)
	if err != nil || found.ID != e.ID {
		t.Fatalf("ShelfFind: %v (found=%+v) — domain must consider it present", err, found)
	}
	// The plain default-bucket list must NOT contain it — this is the bug's
	// precondition, not the fix: without loadShelfForHintCmd's bucket-aware
	// lookup, the consumer and domain disagree.
	def, err := m.svc.ShelfList(ctx, "", 0, 0)
	if err != nil {
		t.Fatalf("ShelfList(default): %v", err)
	}
	if shelfIndexByID(def, e.ID) >= 0 {
		t.Fatalf("fixture: entry must NOT be in the default bucket's list")
	}

	// The fix under test: loadShelfForHintCmd resolves the entry's OWN
	// bucket first (ShelfFind), then lists THAT bucket.
	m.hintGen++
	tag := m.hintGen
	sdir := m.steerDir
	m.pendingHint = &pendingHint{cmd: steer.Command{ID: "n3", HintKind: "shelf", HintID: e.ID, Wait: true}, mustAnswer: true, tag: tag}
	msg := m.loadShelfForHintCmd(e.ID, tag)().(shelfLoadedMsg)
	if msg.err != nil {
		t.Fatalf("loadShelfForHintCmd: %v", msg.err)
	}
	if shelfIndexByID(msg.entries, e.ID) < 0 {
		t.Fatalf("loadShelfForHintCmd entries = %+v, want the wip-bucket entry %s present", msg.entries, e.ID)
	}
	tm, cmd := m.Update(msg)
	nm := tm.(Model)
	p := nm.shelfSwitcher()
	if p == nil {
		t.Fatal("an entry in a non-default bucket must still reveal (fix F1) — domain and the consumer must agree")
	}
	got, ok := p.selected()
	if !ok || got.ID != e.ID {
		t.Fatalf("selected = %+v ok=%v, want %s", got, ok, e.ID)
	}
	if cmd == nil {
		t.Fatal("the hint-only shape (Wait: true) must post a reply")
	}
	cmd()
	r, ok := steer.AwaitReply(sdir, "n3", 2*time.Second)
	if !ok || !r.OK {
		t.Fatalf("reply = %+v ok=%v, want ok:true — fix F1: a present (bucketed) entry must not steerFail (spec §3.3 rule 3 inverted was the bug)", r, ok)
	}
	// m.shelfEntries (the general default-bucket cache) must be untouched
	// by a bucket-scoped hint load.
	if idx := shelfIndexByID(nm.shelfEntries, e.ID); idx >= 0 {
		t.Error("a hint's bucket-scoped load must never leak into the general m.shelfEntries cache")
	}
}

// TestSteerNavigateHintOnlyPresentRevealsAndAnswersOK is the end-to-end
// hint-only landing (S13): the reveal IS the landing, so the async load's
// own finding is what answers the steer command.
func TestSteerNavigateHintOnlyPresentRevealsAndAnswersOK(t *testing.T) {
	dir := gittest.BasicRepo(t, "hi\n")
	m := hintNavModel(t, dir)
	sdir := m.steerDir
	ctx := context.Background()
	b, err := m.svc.BookmarkAdd(ctx, model.Bookmark{State: model.StateUnstaged, Worktree: dir, Path: "README.md"})
	if err != nil {
		t.Fatalf("BookmarkAdd: %v", err)
	}
	c := steer.Command{ID: "h1", Cmd: "navigate", HintKind: "bookmark", HintID: b.ID, Wait: true}
	m, cmd := m.applySteer(c)
	if m.pendingHint == nil || !m.pendingHint.mustAnswer {
		t.Fatal("a hint-only navigate must stage a MUST-ANSWER pendingHint")
	}
	m = pumpDiff(t, m, cmd)
	p := m.bookmarkSwitcher()
	if p == nil {
		t.Fatal("the reveal IS the landing — the popup must open")
	}
	got, ok := p.selected()
	if !ok || got.ID != b.ID {
		t.Fatalf("selected = %+v ok=%v, want %s", got, ok, b.ID)
	}
	r, ok := steer.AwaitReply(sdir, "h1", 2*time.Second)
	if !ok || !r.OK {
		t.Fatalf("reply = %+v ok=%v, want ok:true", r, ok)
	}
}

// TestSteerNavigateHintOnlyAbsentAnswersFail: domain hard-errors an absent
// address-less hint at RESOLVE time (ruling S11), so this shape should never
// reach a live TUI through gg's own pipeline — but if it somehow does (a
// stale bookmark deleted between resolve and reveal), the navigate must
// answer FAIL, never silently succeed or hang the caller's --wait.
func TestSteerNavigateHintOnlyAbsentAnswersFail(t *testing.T) {
	dir := gittest.BasicRepo(t, "hi\n")
	m := hintNavModel(t, dir)
	sdir := m.steerDir
	c := steer.Command{ID: "h2", Cmd: "navigate", HintKind: "bookmark", HintID: "nope", Wait: true}
	m, cmd := m.applySteer(c)
	m = pumpDiff(t, m, cmd)
	if p := m.bookmarkSwitcher(); p != nil {
		t.Error("an absent hint-only navigate must not open a popup")
	}
	r, ok := steer.AwaitReply(sdir, "h2", 2*time.Second)
	if !ok || r.OK {
		t.Fatalf("reply = %+v ok=%v, want ok:false", r, ok)
	}
}

// TestSteerNavigateHintOnlyUnknownKindIsRefused pins S9's defensive default
// on the hint-only path: unreachable through gg's own pipeline (domain
// hard-errors an address-less "stash" hint before a Command can even be
// built — S11), but a hand-crafted command must still get an EXPLICIT
// refusal, never a silent no-op.
func TestSteerNavigateHintOnlyUnknownKindIsRefused(t *testing.T) {
	dir := gittest.BasicRepo(t, "hi\n")
	m := refPairModel(t, dir)
	sdir := m.steerDir
	c := steer.Command{ID: "h3", Cmd: "navigate", HintKind: "stash", HintID: "3", Wait: true}
	_, cmd := m.applySteer(c)
	if cmd != nil {
		cmd()
	}
	r, ok := steer.AwaitReply(sdir, "h3", 2*time.Second)
	if !ok || r.OK {
		t.Fatalf("reply = %+v ok=%v, want ok:false (S9: an explicit refusal, never a silent no-op)", r, ok)
	}
}

// TestSteerEnumRefusalRejectsAnUnknownHintKind pins S13 point 2's TUI-side
// twin of the web wire validator: the closed set is bookmark/shelf/stash
// (model.LinkHint's grammar), and anything else is refused before it ever
// reaches steerNavigate.
func TestSteerEnumRefusalRejectsAnUnknownHintKind(t *testing.T) {
	t.Parallel()
	if got := steerEnumRefusal(steer.Command{HintKind: "evil", HintID: "1"}); got == "" {
		t.Error("an unknown hint kind must be refused")
	}
	if got := steerEnumRefusal(steer.Command{HintKind: "bookmark", HintID: ""}); got == "" {
		t.Error("a hint kind with no id must be refused")
	}
	for _, kind := range []string{"bookmark", "shelf", "stash", "preview"} {
		if got := steerEnumRefusal(steer.Command{HintKind: kind, HintID: "1"}); got != "" {
			t.Errorf("kind %q must be accepted by the enum gate, got refusal %q", kind, got)
		}
	}
}

// TestAStaleHintLoadDoesNotUnrevealTheBookmarkRow closes the asymmetry the
// scoped re-review found in fix F3: the shelf arm returns early on
// msg.hintBucket, so a hint-minted load that no longer matches a pending
// hint can never fall through to the ordinary switcher-open branch. The
// bookmark arm had no twin, and bookmarksLoadedMsg carries the same
// information in `gen` (0 = an ordinary `g`, non-zero = a hint's OWN load).
//
// The race is two hinted navigates landing close together with their loads
// arriving reversed: the newer load reveals and clears pendingHint, then the
// older one arrives to find nothing pending, takes the plain-open branch, and
// `*existing = *p` resets sel to 0 — silently un-revealing a row the user was
// just shown. Same shape as the shelf arm, one arm over: ruling S5's family.
func TestAStaleHintLoadDoesNotUnrevealTheBookmarkRow(t *testing.T) {
	dir := gittest.BasicRepo(t, "hi\n")
	m := hintNavModel(t, dir)
	ctx := context.Background()
	older, err := m.svc.BookmarkAdd(ctx, model.Bookmark{State: model.StateUnstaged, Worktree: dir, Path: "README.md"})
	if err != nil {
		t.Fatalf("BookmarkAdd: %v", err)
	}
	head, _, err := m.svc.ResolveRev(ctx, "HEAD")
	if err != nil {
		t.Fatalf("ResolveRev: %v", err)
	}
	if _, err := m.svc.BookmarkAdd(ctx, model.Bookmark{State: model.StateCommitted, Commit: strings.TrimSpace(head), Path: "README.md"}); err != nil {
		t.Fatalf("BookmarkAdd: %v", err)
	}
	items, err := m.svc.BookmarkList(ctx, 0, 0)
	if err != nil {
		t.Fatalf("BookmarkList: %v", err)
	}
	// The newer hinted navigate (gen 2) lands and reveals the older bookmark.
	m.hintGen = 2
	m.pendingHint = &pendingHint{cmd: steer.Command{ID: "n2", HintKind: "bookmark", HintID: older.ID}, tag: 2}
	tm, _ := m.Update(bookmarksLoadedMsg{items: items, gen: 2})
	m = tm.(Model)
	p := m.bookmarkSwitcher()
	if p == nil {
		t.Fatal("the matching hint load must reveal")
	}
	revealed := p.sel
	if got, ok := p.selected(); !ok || got.ID != older.ID {
		t.Fatalf("selected = %+v ok=%v, want %s", got, ok, older.ID)
	}
	// Now the EARLIER navigate's load (gen 1) finally arrives. Nothing is
	// pending any more, and it must not touch the revealed popup.
	tm, _ = m.Update(bookmarksLoadedMsg{items: items, gen: 1})
	m = tm.(Model)
	p = m.bookmarkSwitcher()
	if p == nil {
		t.Fatal("a stale hint load must not close the popup")
	}
	if p.sel != revealed {
		t.Errorf("sel = %d after a stale hint load, want %d — the reveal was silently undone", p.sel, revealed)
	}
	if got, ok := p.selected(); !ok || got.ID != older.ID {
		t.Errorf("selected = %+v ok=%v, want %s still revealed", got, ok, older.ID)
	}
}

// TestAStrayErroringLoadDoesNotFailAPendingHint closes the scoped re-review's
// finding 3, in both near-duplicate arms: the err paths consumed
// m.pendingHint without checking gen, so an unrelated load that happened to
// error would abandon (and, for a --wait command, FAIL) a reveal whose own
// load was still in flight and might well succeed. Only the hint's own load
// may fail it — the same gen==tag rule the success paths already applied.
func TestAStrayErroringLoadDoesNotFailAPendingHint(t *testing.T) {
	dir := gittest.BasicRepo(t, "hi\n")

	t.Run("bookmark", func(t *testing.T) {
		m := hintNavModel(t, dir)
		m.hintGen = 2
		ph := &pendingHint{cmd: steer.Command{ID: "n1", HintKind: "bookmark", HintID: "b1", Wait: true}, mustAnswer: true, tag: 2}
		m.pendingHint = ph
		tm, cmd := m.Update(bookmarksLoadedMsg{err: errStrayLoad, gen: 1})
		nm := tm.(Model)
		if nm.pendingHint == nil {
			t.Error("a stray erroring load must not consume this hint's pending reveal")
		}
		if cmd != nil {
			t.Error("a stray erroring load must not answer (and so fail) the hint's command")
		}
		// The hint's OWN erroring load still fails it.
		tm, cmd = nm.Update(bookmarksLoadedMsg{err: errStrayLoad, gen: 2})
		if tm.(Model).pendingHint != nil {
			t.Error("the hint's own erroring load must consume the pending")
		}
		if cmd == nil {
			t.Error("the hint's own erroring load must answer a Wait command")
		}
	})

	t.Run("shelf", func(t *testing.T) {
		m := hintNavModel(t, dir)
		m.hintGen = 2
		m.pendingHint = &pendingHint{cmd: steer.Command{ID: "n2", HintKind: "shelf", HintID: "s1", Wait: true}, mustAnswer: true, tag: 2}
		tm, cmd := m.Update(shelfLoadedMsg{err: errStrayLoad, gen: 1})
		nm := tm.(Model)
		if nm.pendingHint == nil {
			t.Error("a stray erroring load must not consume this hint's pending reveal")
		}
		if cmd != nil {
			t.Error("a stray erroring load must not answer (and so fail) the hint's command")
		}
		tm, cmd = nm.Update(shelfLoadedMsg{err: errStrayLoad, gen: 2})
		if tm.(Model).pendingHint != nil {
			t.Error("the hint's own erroring load must consume the pending")
		}
		if cmd == nil {
			t.Error("the hint's own erroring load must answer a Wait command")
		}
	})
}

var errStrayLoad = errors.New("stray load failed")
