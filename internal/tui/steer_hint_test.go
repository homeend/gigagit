package tui

import (
	"context"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/gittest"
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
// just open a popup — BookmarkList returns newest-first, so the hinted
// (older) entry sits at index 1, never 0.
func TestBookmarksLoadedMsgRevealsThePendingHintRow(t *testing.T) {
	dir := gittest.BasicRepo(t, "hi\n")
	m := hintNavModel(t, dir)
	ctx := context.Background()
	older, err := m.svc.BookmarkAdd(ctx, model.Bookmark{State: model.StateUnstaged, Worktree: dir, Path: "README.md"})
	if err != nil {
		t.Fatalf("BookmarkAdd: %v", err)
	}
	if _, err := m.svc.BookmarkAdd(ctx, model.Bookmark{State: model.StateCommitted, Commit: "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", Path: "other.txt"}); err != nil {
		t.Fatalf("BookmarkAdd: %v", err)
	}
	items, err := m.svc.BookmarkList(ctx, 0, 0)
	if err != nil {
		t.Fatalf("BookmarkList: %v", err)
	}
	if len(items) != 2 || items[1].ID != older.ID {
		t.Fatalf("fixture: items = %+v, want the older bookmark at index 1 (newest-first)", items)
	}
	m.pendingHint = &pendingHint{cmd: steer.Command{ID: "n1", HintKind: "bookmark", HintID: older.ID}}
	tm, _ := m.Update(bookmarksLoadedMsg{items: items})
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

// TestBookmarksLoadedMsgAbsentHintNoticesWithoutOpeningAPopup is the ABSENT
// half: no popup, a notice, the navigate itself is untouched (it already
// answered elsewhere — mustAnswer is false here).
func TestBookmarksLoadedMsgAbsentHintNoticesWithoutOpeningAPopup(t *testing.T) {
	dir := gittest.BasicRepo(t, "hi\n")
	m := hintNavModel(t, dir)
	m.pendingHint = &pendingHint{cmd: steer.Command{ID: "n1", HintKind: "bookmark", HintID: "nope"}}
	tm, cmd := m.Update(bookmarksLoadedMsg{items: nil})
	nm := tm.(Model)
	if p := nm.bookmarkSwitcher(); p != nil {
		t.Error("an absent hint must NOT open the popup")
	}
	if nm.statusMsg == "" {
		t.Error("an absent hint must still notice (spec §3.3 rule 3: degrades, never fails)")
	}
	if nm.pendingHint != nil {
		t.Error("pendingHint must be cleared")
	}
	if cmd != nil {
		t.Error("the with-address absent case must not post a second reply (mustAnswer is false)")
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
	m.pendingHint = &pendingHint{cmd: steer.Command{ID: "n2", HintKind: "shelf", HintID: e.ID}}
	tm, _ := m.Update(shelfLoadedMsg{entries: items, open: true})
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
	m.pendingHint = &pendingHint{cmd: steer.Command{ID: "n2", HintKind: "shelf", HintID: "nope"}}
	tm, _ := m.Update(shelfLoadedMsg{entries: nil, open: true})
	nm := tm.(Model)
	if p := nm.shelfSwitcher(); p != nil {
		t.Error("an absent hint must NOT open the popup")
	}
	if nm.statusMsg == "" {
		t.Error("an absent hint must still notice")
	}
	if nm.pendingHint != nil {
		t.Error("pendingHint must be cleared")
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
	for _, kind := range []string{"bookmark", "shelf", "stash"} {
		if got := steerEnumRefusal(steer.Command{HintKind: kind, HintID: "1"}); got != "" {
			t.Errorf("kind %q must be accepted by the enum gate, got refusal %q", kind, got)
		}
	}
}
