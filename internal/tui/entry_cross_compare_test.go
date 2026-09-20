package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/model"
)

// crossFixture: refPairRepo (c1 → c2 adds b.txt → c3 adds c.txt) behind a
// model, with the entries the cross flow picks between. The commits are LIVE,
// so a link's ?bookmark= / ?shelf= hint never changes the answer here; the
// frozen-shelf exception is domain's and is tested there.
type crossFixture struct {
	m          Model
	dir        string
	bmCommit   model.Bookmark   // commit bookmark at c1
	bmFile     model.Bookmark   // file bookmark: a.txt at c1
	shCommit   model.ShelfEntry // shelved commit c3
	shFile     model.ShelfEntry // shelved file: c.txt at c3
	c1, c3     string
	bmLink     string
	bmFileLink string
	shLink     string
	shFileLink string
}

func newCrossFixture(t *testing.T) crossFixture {
	t.Helper()
	dir, c1, _, c3 := refPairRepo(t)
	f := crossFixture{m: refPairModel(t, dir), dir: dir, c1: c1, c3: c3}
	f.bmCommit = model.Bookmark{ID: "bm1", Commit: c1, State: model.StateCommitted}
	f.bmFile = model.Bookmark{ID: "bm2", Commit: c1, Path: "a.txt", State: model.StateCommitted}
	f.shCommit = model.ShelfEntry{ID: "sh1", Kind: model.ShelfKindCommit, Origin: model.FileAddress{State: model.StateCommitted, Commit: c3}}
	f.shFile = model.ShelfEntry{ID: "sh2", Origin: model.FileAddress{State: model.StateCommitted, Commit: c3, Path: "c.txt"}}
	var ok [4]bool
	f.bmLink, ok[0] = f.m.bookmarkLink(f.bmCommit)
	f.bmFileLink, ok[1] = f.m.bookmarkLink(f.bmFile)
	f.shLink, ok[2] = f.m.shelfEntryLink(f.shCommit)
	f.shFileLink, ok[3] = f.m.shelfEntryLink(f.shFile)
	for i, o := range ok {
		if !o {
			t.Fatalf("fixture: entry %d has no link form", i)
		}
	}
	if !f.bmCommit.IsCommit() || f.bmFile.IsCommit() || !f.shCommit.IsCommit() || f.shFile.IsCommit() {
		t.Fatal("fixture: the entries are not the kinds the tests assume")
	}
	return f
}

// shelfPick opens the shelf switcher as the SECOND picker, over entry e.
func (f crossFixture) shelfPick(first *entrySide, ref *model.FileRef, link string, e model.ShelfEntry) Model {
	p := newShelfPopup([]model.ShelfEntry{e})
	p.compareEntry, p.compareRef, p.compareLink, p.compareLabel = first, ref, link, "first"
	return f.m.pushLayer(p)
}

func (f crossFixture) bookmarkPick(first *entrySide, ref *model.FileRef, link string, b model.Bookmark) Model {
	p := newBookmarkPopup([]model.Bookmark{b})
	p.compareEntry, p.compareRef, p.compareLink, p.compareLabel = first, ref, link, "first"
	return f.m.pushLayer(p)
}

func enterAndPump(t *testing.T, m Model) Model {
	t.Helper()
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return pumpAll(t, updated.(Model), cmd)
}

// The two arms DISAGREE on one fixture: with the first pick's link the second
// pick goes through the door (the set view); without it, the endpoint compare.
// Both must list the same files — it is one comparison, asked two ways.
func TestCrossCommitCommitGoesThroughTheDoor(t *testing.T) {
	t.Parallel()
	f := newCrossFixture(t)
	first := bookmarkEntrySide(f.bmCommit)

	door := enterAndPump(t, f.shelfPick(&first, nil, f.bmLink, f.shCommit))
	if door.filesSets == nil {
		t.Fatalf("the cross flow must open the set view (status %q)", door.statusMsg)
	}
	old := enterAndPump(t, f.shelfPick(&first, nil, "", f.shCommit))
	if old.filesView == nil || old.filesSets != nil {
		t.Fatalf("with no link the endpoint compare must still open: view=%v sets=%v", old.filesView != nil, old.filesSets != nil)
	}
	if a, b := strings.Join(rowTexts(door), "|"), strings.Join(rowTexts(old), "|"); a != b || a != "A b.txt|A c.txt" {
		t.Errorf("door rows %q, endpoint rows %q — want both A b.txt|A c.txt", a, b)
	}
	// The switcher is parked, not destroyed: closing the view returns to it.
	back, _ := door.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if _, ok := back.(Model).topLayer().(*shelfPopup); !ok {
		t.Errorf("top layer after esc = %T, want the shelf switcher back", back.(Model).topLayer())
	}
}

// commit ↔ file was refused; on links it is a whole tree against one member.
func TestCrossCommitAgainstAFileIsLegal(t *testing.T) {
	t.Parallel()
	f := newCrossFixture(t)

	// commit first (a bookmark), a shelved FILE second.
	first := bookmarkEntrySide(f.bmCommit)
	m := enterAndPump(t, f.shelfPick(&first, nil, f.bmLink, f.shFile))
	if got := strings.Join(rowTexts(m), "|"); got != "A c.txt" {
		t.Fatalf("commit→file rows = %q (status %q), want exactly the one member", got, m.statusMsg)
	}
	// file first (a shelf entry), a commit BOOKMARK second.
	ref := model.FileRef{Source: model.SourceShelf, Locator: f.shFile.ID, Path: "c.txt"}
	m = enterAndPump(t, f.bookmarkPick(nil, &ref, f.shFileLink, f.bmCommit))
	if got := strings.Join(rowTexts(m), "|"); got != "D c.txt" {
		t.Fatalf("file→commit rows = %q (status %q)", got, m.statusMsg)
	}
}

// A focused file picked from a . menu has no link: it is not the cross flow,
// and it keeps the refusal.
func TestAFocusedFileAgainstACommitIsStillRefused(t *testing.T) {
	t.Parallel()
	f := newCrossFixture(t)
	ref := model.FileRef{Source: model.SourceUnstaged, Path: "a.txt"}
	updated, cmd := f.bookmarkPick(nil, &ref, "", f.bmCommit).Update(tea.KeyMsg{Type: tea.KeyEnter})
	m := updated.(Model)
	if cmd != nil || !strings.Contains(m.statusMsg, "cannot compare a file against a commit bookmark") {
		t.Errorf("cmd=%v status=%q, want the refusal", cmd != nil, m.statusMsg)
	}
	first := bookmarkEntrySide(f.bmCommit)
	updated, cmd = f.shelfPick(&first, nil, "", f.shFile).Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m = updated.(Model); cmd != nil || !strings.Contains(m.statusMsg, "cannot compare a commit against a file") {
		t.Errorf("no-link commit→file: cmd=%v status=%q, want the refusal", cmd != nil, m.statusMsg)
	}
}

// file ↔ file keeps its two-ref diff even in the cross flow: the paths differ,
// and on links a.txt vs c.txt is one D and one A, never a diff.
func TestCrossFileFileKeepsItsTwoRefDiff(t *testing.T) {
	t.Parallel()
	f := newCrossFixture(t)
	ref := model.FileRef{Source: model.SourceShelf, Locator: f.shFile.ID, Path: "c.txt"}
	updated, _ := f.bookmarkPick(nil, &ref, f.shFileLink, f.bmFile).Update(tea.KeyMsg{Type: tea.KeyEnter})
	m := updated.(Model)
	dv := m.diffLayer()
	if dv == nil || m.filesView != nil || m.linkCompareWant != "" {
		t.Fatalf("diff=%v files=%v want=%q — file↔file must open the two-ref diff, not the door", dv != nil, m.filesView != nil, m.linkCompareWant)
	}
	if !strings.Contains(dv.title, "c.txt") || !strings.Contains(dv.title, "a.txt") {
		t.Errorf("title = %q, want both paths", dv.title)
	}
}

func TestCrossCompareOfTheSameCommitIsANotice(t *testing.T) {
	t.Parallel()
	f := newCrossFixture(t)
	first := bookmarkEntrySide(f.bmCommit)
	same := model.ShelfEntry{ID: "sh9", Kind: model.ShelfKindCommit, Origin: model.FileAddress{State: model.StateCommitted, Commit: f.c1}}
	updated, cmd := f.shelfPick(&first, nil, f.bmLink, same).Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m := updated.(Model); cmd != nil || !strings.Contains(m.statusMsg, "select a different commit") {
		t.Errorf("cmd=%v status=%q", cmd != nil, m.statusMsg)
	}
}

// A bookmark whose commit is gone still gets gg's own words, not the door's
// "unknown revision".
func TestACrossCompareOfADeadBookmarkSaysTheEntryIsGone(t *testing.T) {
	t.Parallel()
	f := newCrossFixture(t)
	dead := model.Bookmark{ID: "bm9", Commit: "dead000000000000000000000000000000000000", State: model.StateCommitted}
	link, ok := f.m.bookmarkLink(dead)
	if !ok {
		t.Fatal("fixture: no link for the dead bookmark")
	}
	first := bookmarkEntrySide(dead)
	// ONE round only: the sticky notice arms a 10 s expiry tick, and pumping
	// that would wait it out and then clear the very text under test.
	updated, cmd := f.shelfPick(&first, nil, link, f.shCommit).Update(tea.KeyMsg{Type: tea.KeyEnter})
	m := updated.(Model)
	for _, msg := range flattenCmd(t, cmd) {
		next, _ := m.Update(msg)
		m = next.(Model)
	}
	if m.filesView != nil || !strings.Contains(m.statusMsg, "dead000 is no longer available") {
		t.Errorf("view=%v status=%q, want the sticky entry-gone notice", m.filesView != nil, m.statusMsg)
	}
}

// The first pick's link travels with it into the other switcher — and the
// shelf link keeps its hint, which is what reads a frozen copy when the sha is
// gone (domain tests the read; this pins that the TUI sends the hint).
func TestTheFirstPicksLinkTravelsToTheOtherSwitcher(t *testing.T) {
	t.Parallel()
	f := newCrossFixture(t)
	if !strings.HasSuffix(f.shLink, "@"+f.c3+"?shelf=sh1") || !strings.HasSuffix(f.bmLink, "@"+f.c1+"?bookmark=bm1") {
		t.Fatalf("links = %q | %q, want each entry's address plus its hint", f.shLink, f.bmLink)
	}
	m := f.m.pushLayer(newBookmarkPopup([]model.Bookmark{f.bmCommit}))
	updated, _ := m.Update(keyMsg("c"))
	if pc := updated.(Model).pendingCompare; pc == nil || pc.link != f.bmLink || pc.entry == nil {
		t.Errorf("pendingCompare = %+v, want the bookmark's link carried", pc)
	}
	m = f.m.pushLayer(newShelfPopup([]model.ShelfEntry{f.shFile}))
	m2, _ := m.shelfCompareAgainstBookmark()
	if pc := m2.pendingCompare; pc == nil || pc.link != f.shFileLink || pc.entry != nil {
		t.Errorf("pendingCompare = %+v, want the shelf file's link carried", pc)
	}
}

// The hop in the middle: the first pick waits on the Model while the OTHER
// switcher's list loads, and its link must arrive in that switcher — both
// ways round. Without it the second pick silently keeps the endpoint flow,
// and commit↔file is refused again.
func TestThePendingLinkArrivesInTheSecondSwitcher(t *testing.T) {
	t.Parallel()
	f := newCrossFixture(t)
	first := bookmarkEntrySide(f.bmCommit)

	m := f.m
	m.pendingCompare = &pendingCompare{entry: &first, link: f.bmLink, label: "bm", target: compareShelf}
	updated, _ := m.Update(shelfLoadedMsg{entries: []model.ShelfEntry{f.shFile}, open: true})
	sp, ok := updated.(Model).topLayer().(*shelfPopup)
	if !ok || sp.compareLink != f.bmLink || sp.compareEntry == nil {
		t.Fatalf("shelf switcher = %+v, want it opened in compare mode carrying %q", sp, f.bmLink)
	}

	m = f.m
	ref := model.FileRef{Source: model.SourceShelf, Locator: f.shFile.ID, Path: "c.txt"}
	m.pendingCompare = &pendingCompare{ref: ref, link: f.shFileLink, label: "sh", target: compareBookmark}
	updated, _ = m.Update(bookmarksLoadedMsg{items: []model.Bookmark{f.bmCommit}})
	bp, ok := updated.(Model).topLayer().(*bookmarkPopup)
	if !ok || bp.compareLink != f.shFileLink || bp.compareRef == nil {
		t.Fatalf("bookmark switcher = %+v, want it opened in compare mode carrying %q", bp, f.shFileLink)
	}
}
