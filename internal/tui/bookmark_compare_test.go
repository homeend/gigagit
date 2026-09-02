package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestOpenCompareFocusedVsBookmark(t *testing.T) {
	t.Parallel()
	m := footerModel()
	ref := model.FileRef{Source: model.SourceCommit, Locator: "aaaa1111", Path: "a.go"}
	bm := model.Bookmark{ID: "bm9", State: model.StateCommitted, Commit: "bbbb2222", SHA: "blob22", Path: "b.go"}
	u, cmd := m.openCompareFocusedVsBookmark(ref, "commit a.go", bm)
	if u.diffLayer() == nil {
		t.Fatal("openCompareFocusedVsBookmark must open a diff view")
	}
	if u.diffLayer().title != "a.go ↔ b.go" {
		t.Errorf("diff title = %q, want \"a.go ↔ b.go\"", u.diffLayer().title)
	}
	if u.diffTag != "cmpbm:a.go:bm9" {
		t.Errorf("diffTag = %q, want cmpbm:a.go:bm9", u.diffTag)
	}
	if cmd == nil {
		t.Error("expected a load command")
	}
}

func twoBookmarkItems() []model.Bookmark {
	return []model.Bookmark{
		{ID: "b1", State: model.StateCommitted, Commit: "c1", SHA: "s1", Path: "a.go"},
		{ID: "b2", State: model.StateCommitted, Commit: "c2", SHA: "s2", Path: "b.go"},
	}
}

func TestPendingCompareSurvivesLoad(t *testing.T) {
	t.Parallel()
	m := footerModel()
	m.pendingCompare = &pendingCompare{ref: model.FileRef{Source: model.SourceCommit, Locator: "x", Path: "a.go"}, label: "commit a.go"}
	u, _ := m.Update(bookmarksLoadedMsg{items: twoBookmarkItems()})
	mm := u.(Model)
	if mm.bookmarkSwitcher() == nil || mm.bookmarkSwitcher().compareRef == nil {
		t.Fatal("popup must open in compare mode")
	}
	if mm.pendingCompare != nil {
		t.Error("pendingCompare must be cleared once consumed")
	}
}

func TestCompareModeEnterRunsCompare(t *testing.T) {
	t.Parallel()
	m := footerModel()
	m = m.pushLayer(newBookmarkPopup(twoBookmarkItems()))
	ref := model.FileRef{Source: model.SourceCommit, Locator: "x", Path: "a.go"}
	m.bookmarkSwitcher().compareRef = &ref
	m.bookmarkSwitcher().compareLabel = "commit a.go"
	u, _ := m.Update(keyMsg("enter"))
	mm := u.(Model)
	if mm.diffLayer() == nil {
		t.Fatal("enter in compare mode must open the comparison diff")
	}
	// The diff is pushed over the switcher; the switcher sits beneath the diff
	// on the stack (esc from the diff returns to it). The switcher is still live.
	if mm.bookmarkSwitcher() == nil {
		t.Error("the bookmark switcher must remain on the stack beneath the diff")
	}
}

func TestCompareModeMutatorsInert(t *testing.T) {
	t.Parallel()
	m := footerModel()
	m = m.pushLayer(newBookmarkPopup(twoBookmarkItems()))
	ref := model.FileRef{Source: model.SourceCommit, Locator: "x", Path: "a.go"}
	m.bookmarkSwitcher().compareRef = &ref
	for _, k := range []string{"x", "p", "m"} {
		u, _ := m.Update(keyMsg(k))
		mm := u.(Model)
		if mm.bookmarkSwitcher() == nil || mm.diffLayer() != nil || mm.modal != nil || bookmarkPasteOf(mm) != nil {
			t.Errorf("%q must be inert in compare mode", k)
		}
		if mm.bookmarkSwitcher() != nil && mm.bookmarkSwitcher().markID != "" {
			t.Errorf("%q must not set a compare mark in compare mode", k)
		}
	}
}

func TestCompareRowRunSetsPendingAndLoads(t *testing.T) {
	t.Parallel()
	m := footerModel()
	m = m.pushLayer(&diffView{title: "a.go", rev: "cafe9999"}) // a resolvable focused file
	row, ok := m.compareAgainstBookmarkRow()
	if !ok {
		t.Fatal("compare row must be present when a file is focused")
	}
	u, cmd := row.run(m)
	mm := u.(Model)
	if mm.pendingCompare == nil || mm.pendingCompare.ref.Path != "a.go" {
		t.Fatalf("run must set pendingCompare for the focused file, got %+v", mm.pendingCompare)
	}
	if cmd == nil {
		t.Error("run must kick off the bookmark load")
	}
}

// Launched from a history/blame surface, the compare diff is pushed on top
// of the layer stack (history + popup + diff). The diff is the topmost layer
// and must be visible; esc returns to the picker, esc again returns to history.
func TestCompareDiffVisibleOverHistorySurface(t *testing.T) {
	t.Parallel()
	m := footerModel()
	m = m.pushLayer(newHistoryView(navContext{path: "a.go", rev: "r"}))
	m = m.pushLayer(newBookmarkPopup(twoBookmarkItems()))
	ref := model.FileRef{Source: model.SourceCommit, Locator: "x", Path: "a.go"}
	m.bookmarkSwitcher().compareRef = &ref
	u, _ := m.Update(keyMsg("enter"))
	mm := u.(Model)
	if mm.diffLayer() == nil {
		t.Error("the diff must be on the stack so it owns the screen")
	}
	if !strings.Contains(mm.render(), "↔") {
		t.Fatal("compare diff must be visible over a history/blame surface")
	}
}

func TestBookmarksLoadErrorClearsPendingCompare(t *testing.T) {
	t.Parallel()
	m := footerModel()
	m.pendingCompare = &pendingCompare{ref: model.FileRef{Path: "a.go"}, label: "x"}
	u, _ := m.Update(bookmarksLoadedMsg{err: errors.New("boom")})
	if u.(Model).pendingCompare != nil {
		t.Error("a failed bookmark load must clear pendingCompare")
	}
}

func TestCompareRowAccompaniesAddRow(t *testing.T) {
	t.Parallel()
	// Wherever "Bookmark this file" appears, so must "Compare against bookmark".
	m := footerModel()
	m = m.pushLayer(newBlameView(navContext{path: "a.go", rev: "abc123"}))
	got := ids(availableActions(m))
	if !got["bookmark-add"] {
		t.Fatal("precondition: bookmark-add expected in blame view")
	}
	if !got["bookmark-compare"] {
		t.Error("bookmark-compare must accompany bookmark-add")
	}
}

func TestCompareAgainstWorkingDirRowOpensDiff(t *testing.T) {
	t.Parallel()
	m := footerModel()
	// A focused commit file: the diff view with a rev makes focusedBookmark
	// yield a committed ref (Path = the diff title).
	m = m.pushLayer(&diffView{title: "a.go", rev: "abc1234"})
	r, ok := m.compareAgainstWorkingDirRow()
	if !ok {
		t.Fatal("row should be present for a focused commit file")
	}
	if r.label != "Compare against working dir" {
		t.Fatalf("label = %q", r.label)
	}
	u, cmd := r.run(m)
	mm := u.(Model)
	if mm.diffLayer() == nil || mm.diffLayer().title != "a.go ↔ working" {
		t.Fatalf("diff view = %+v", mm.diffLayer())
	}
	if mm.diffTag != "cmpwd:a.go" {
		t.Fatalf("diffTag = %q, want cmpwd:a.go", mm.diffTag)
	}
	if cmd == nil {
		t.Fatal("expected a load command")
	}
}

func TestCompareAgainstWorkingDirRowAbsentForWorkingFile(t *testing.T) {
	t.Parallel()
	m := footerModel()
	// A working-tree file is focused (diff view with no rev) → comparing it
	// against the working tree is itself-vs-itself, so the row is gated off.
	m = m.pushLayer(&diffView{title: "a.go"}) // rev "" → unstaged source
	if _, ok := m.compareAgainstWorkingDirRow(); ok {
		t.Fatal("row should be absent for a working-tree file")
	}
}

// A two-sided compare diff (title "a ↔ b", no rev) addresses no single file:
// its title is a composite, not a path, so bookmarking / shelving / comparing
// the "focused file" there would mint an address to a path that does not
// exist. Every opener of a compare diff must leave focusedBookmark empty,
// and the identity must survive the async load (diffMsg swaps the layer's
// view wholesale) — the shared loader builds its view without knowing which
// opener asked.
func TestFocusedBookmarkAbsentOnCompareDiff(t *testing.T) {
	t.Parallel()
	ref := model.FileRef{Source: model.SourceCommit, Locator: "abc1234", Path: "a.go"}
	openers := []struct {
		name string
		open func(Model) Model
	}{
		{"compare against working dir", func(m Model) Model {
			m = m.pushLayer(&diffView{title: "a.go", rev: "abc1234"})
			r, ok := m.compareAgainstWorkingDirRow()
			if !ok {
				t.Fatal("precondition: the compare-against-working-dir row must be present")
			}
			u, _ := r.run(m)
			return u.(Model)
		}},
		{"focused vs bookmark", func(m Model) Model {
			m, _ = m.openCompareFocusedVsBookmark(ref, "commit a.go", model.Bookmark{ID: "bm1", State: model.StateCommitted, Commit: "def5678", Path: "b.go"})
			return m
		}},
		{"focused vs shelf", func(m Model) Model {
			m, _ = m.openCompareFocusedVsShelf(ref, "commit a.go", model.ShelfEntry{ID: "sh1", Origin: model.FileAddress{Path: "b.go"}})
			return m
		}},
		{"bookmark vs bookmark", func(m Model) Model {
			a := model.Bookmark{ID: "bm1", State: model.StateCommitted, Commit: "abc1234", Path: "a.go"}
			b := model.Bookmark{ID: "bm2", State: model.StateCommitted, Commit: "def5678", Path: "b.go"}
			m = m.pushLayer(newBookmarkPopup([]model.Bookmark{a, b}))
			m, _ = m.openBookmarkCompareTwo("bm1", "bm2")
			return m
		}},
	}
	for _, o := range openers {
		m := o.open(footerModel())
		dv := m.diffLayer()
		if dv == nil || !strings.Contains(dv.title, "↔") {
			t.Fatalf("%s: precondition: a compare diff must be open, got %+v", o.name, dv)
		}
		if b, ok := m.focusedBookmark(); ok {
			t.Errorf("%s: compare diff must not yield a bookmark, got %+v", o.name, b)
		}
		got := ids(availableActions(m))
		for _, id := range []string{"bookmark-add", "shelf-add", "bookmark-compare", "shelf-compare-against", "compare-working-dir", "copy-working-dir"} {
			if got[id] {
				t.Errorf("%s: %s must be absent on a compare diff", o.name, id)
			}
		}
		// The loaded view lands: a plain diffView carrying only content.
		u, _ := m.Update(diffMsg{tag: m.diffTag, view: &diffView{title: dv.title, context: dv.context}})
		m = u.(Model)
		if b, ok := m.focusedBookmark(); ok {
			t.Errorf("%s: compare identity must survive the async load, got %+v", o.name, b)
		}
	}
}
