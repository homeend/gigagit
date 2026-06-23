package tui

import (
	"testing"
)

func TestFileFinderOpensAndLoads(t *testing.T) {
	m := loadedModel(t)
	m, _ = m.openFileFinder()
	p := layerOf[*fileFinderPopup](m)
	if p == nil || !p.loading {
		t.Fatal("F should push a loading file finder")
	}
	nm, _ := m.Update(lsFilesMsg{paths: []string{"a/b.go", "c.txt"}})
	m = nm.(Model)
	p = layerOf[*fileFinderPopup](m)
	if p == nil || p.loading || len(p.all) != 2 {
		t.Fatalf("lsFilesMsg should fill the list; %+v", p)
	}
}

func TestFileFinderFiltersAndClamps(t *testing.T) {
	m := loadedModel(t)
	m, _ = m.openFileFinder()
	nm, _ := m.Update(lsFilesMsg{paths: []string{"a/b.go", "c.txt", "files_view.go"}})
	m = nm.(Model)
	// Navigation-first: plain keys do NOT filter; press / to enter filter mode.
	nm, _ = m.Update(keyMsg("/"))
	m = nm.(Model)
	if p := layerOf[*fileFinderPopup](m); !p.filtering {
		t.Fatal("/ should enter filter mode")
	}
	// type "fv" -> matches narrow; sel clamps within matches
	for _, r := range "fv" {
		nm, _ = m.Update(keyMsg(string(r)))
		m = nm.(Model)
	}
	p := layerOf[*fileFinderPopup](m)
	if len(p.matches) == 0 || p.matches[0].S != "files_view.go" {
		t.Fatalf("fv should match files_view.go first; %+v", p.matches)
	}
	if p.sel < 0 || p.sel >= len(p.matches) {
		t.Fatalf("sel out of range: %d", p.sel)
	}
}

// TestFileFinderPlainKeysDoNotFilter pins the navigation-first contract: typing
// a letter without pressing / first must NOT start a query (the regression that
// made `z` cycle the display mode instead of filtering for "zdata").
func TestFileFinderPlainKeysDoNotFilter(t *testing.T) {
	m := loadedModel(t)
	m, _ = m.openFileFinder()
	nm, _ := m.Update(lsFilesMsg{paths: []string{"fs/erofs/zdata.c", "a/b.go"}})
	m = nm.(Model)
	nm, _ = m.Update(keyMsg("z")) // in nav mode this cycles display mode, not a query
	m = nm.(Model)
	p := layerOf[*fileFinderPopup](m)
	if p.query != "" || p.filtering {
		t.Fatalf("plain z must not type a query; query=%q filtering=%v", p.query, p.filtering)
	}
}

// TestFileFinderSlashThenZQuery is the exact user sequence that was broken:
// F, /, type "zdata" -> the z-containing query must reach the matches and find
// the file (proving z is a literal query char in filter mode, not a mode-cycle).
func TestFileFinderSlashThenZQuery(t *testing.T) {
	m := loadedModel(t)
	m, _ = m.openFileFinder()
	nm, _ := m.Update(lsFilesMsg{paths: []string{"a/b.go", "fs/erofs/zdata.c", "c.txt"}})
	m = nm.(Model)
	nm, _ = m.Update(keyMsg("/"))
	m = nm.(Model)
	for _, r := range "zdata" {
		nm, _ = m.Update(keyMsg(string(r)))
		m = nm.(Model)
	}
	p := layerOf[*fileFinderPopup](m)
	if p.query != "zdata" {
		t.Fatalf("query should be %q, got %q", "zdata", p.query)
	}
	found := false
	for _, mt := range p.matches {
		if mt.S == "fs/erofs/zdata.c" {
			found = true
		}
	}
	if !found {
		t.Fatalf("/zdata should match fs/erofs/zdata.c; matches=%+v", p.matches)
	}
}

// TestFileFinderEscClearsFilter checks the two-stage esc: esc in filter mode
// clears the query (back to the full list) and stays open; a second esc closes.
func TestFileFinderEscClearsFilter(t *testing.T) {
	m := loadedModel(t)
	m, _ = m.openFileFinder()
	nm, _ := m.Update(lsFilesMsg{paths: []string{"foo.go", "bar.go", "baz.go"}})
	m = nm.(Model)
	nm, _ = m.Update(keyMsg("/"))
	m = nm.(Model)
	nm, _ = m.Update(keyMsg("f"))
	m = nm.(Model)
	// esc clears the filter but keeps the finder open with the full list back.
	nm, _ = m.Update(keyMsg("esc"))
	m = nm.(Model)
	p := layerOf[*fileFinderPopup](m)
	if p == nil {
		t.Fatal("first esc should clear the filter, not close the finder")
	}
	if p.filtering || p.query != "" || len(p.matches) != len(p.all) {
		t.Fatalf("esc should restore the full list; filtering=%v query=%q matches=%d all=%d", p.filtering, p.query, len(p.matches), len(p.all))
	}
	// second esc closes the finder.
	nm, _ = m.Update(keyMsg("esc"))
	m = nm.(Model)
	if layerOf[*fileFinderPopup](m) != nil {
		t.Fatal("second esc should close the finder")
	}
}

// TestFileFinderRecallReranks checks the search-history dropdown in the finder's
// filter mode: alt+down previews a recalled phrase AND reranks the (cached) match
// list to it — the rerank-after-recall coupling the finder needs but bookmarkPopup
// (live visibleIdx) does not. alt+up above the newest restores the draft.
func TestFileFinderRecallReranks(t *testing.T) {
	m := loadedModel(t)
	m.searchHist = map[string][]string{scopeFiletree: {"erofs"}} // newest-first
	m, _ = m.openFileFinder()
	nm, _ := m.Update(lsFilesMsg{paths: []string{"a/b.go", "fs/erofs/zdata.c", "c.txt"}})
	m = nm.(Model)
	nm, _ = m.Update(keyMsg("/"))
	m = nm.(Model)
	// alt+down opens the recall dropdown on the newest phrase.
	nm, _ = m.Update(keyMsg("alt+down"))
	m = nm.(Model)
	p := layerOf[*fileFinderPopup](m)
	if p.query != "erofs" {
		t.Fatalf("alt+down should preview the recalled phrase; query=%q", p.query)
	}
	found := false
	for _, mt := range p.matches {
		if mt.S == "fs/erofs/zdata.c" {
			found = true
		}
	}
	if !found {
		t.Fatalf("recall must rerank: /erofs should match fs/erofs/zdata.c; matches=%+v", p.matches)
	}
	// alt+up above the newest restores the (empty) draft and reranks back.
	nm, _ = m.Update(keyMsg("alt+up"))
	m = nm.(Model)
	p = layerOf[*fileFinderPopup](m)
	if p.query != "" || len(p.matches) != len(p.all) {
		t.Fatalf("alt+up should restore draft + rerank to full; query=%q matches=%d all=%d", p.query, len(p.matches), len(p.all))
	}
}

// TestFileFinderPageKeys checks pgup/pgdn move the selection by a page, clamped.
func TestFileFinderPageKeys(t *testing.T) {
	m := loadedModel(t)
	m, _ = m.openFileFinder()
	paths := make([]string, 0, 40)
	for i := 0; i < 40; i++ {
		paths = append(paths, string(rune('a'+i%26))+"file.go")
	}
	nm, _ := m.Update(lsFilesMsg{paths: paths})
	m = nm.(Model)
	nm, _ = m.Update(keyMsg("pgdown"))
	m = nm.(Model)
	p := layerOf[*fileFinderPopup](m)
	if p.sel != fileFinderPage {
		t.Fatalf("pgdown should move sel by a page; sel=%d want=%d", p.sel, fileFinderPage)
	}
	nm, _ = m.Update(keyMsg("pgup"))
	m = nm.(Model)
	p = layerOf[*fileFinderPopup](m)
	if p.sel != 0 {
		t.Fatalf("pgup should move back to top; sel=%d", p.sel)
	}
}

func TestFileFinderEscPopsLayer(t *testing.T) {
	m := loadedModel(t)
	m, _ = m.openFileFinder()
	if layerOf[*fileFinderPopup](m) == nil {
		t.Fatal("expected file finder on stack")
	}
	nm, _ := m.Update(keyMsg("esc"))
	m = nm.(Model)
	if layerOf[*fileFinderPopup](m) != nil {
		t.Fatal("esc should pop the file finder")
	}
}

func TestFileFinderLsFilesMsgIgnoredWhenClosed(t *testing.T) {
	// If the user closes before LsFiles returns, no panic.
	m := loadedModel(t)
	m, _ = m.openFileFinder()
	// pop immediately (user pressed esc)
	m = m.popLayer()
	// now the async msg arrives — must not panic
	nm, _ := m.Update(lsFilesMsg{paths: []string{"a.go"}})
	m = nm.(Model)
	if layerOf[*fileFinderPopup](m) != nil {
		t.Fatal("no popup should be open after early close")
	}
}

func TestFileFinderBackspaceReranks(t *testing.T) {
	m := loadedModel(t)
	m, _ = m.openFileFinder()
	nm, _ := m.Update(lsFilesMsg{paths: []string{"foo.go", "bar.go"}})
	m = nm.(Model)
	// enter filter mode, then type "f"
	nm, _ = m.Update(keyMsg("/"))
	m = nm.(Model)
	nm, _ = m.Update(keyMsg("f"))
	m = nm.(Model)
	p := layerOf[*fileFinderPopup](m)
	narrowed := len(p.matches)
	// backspace removes query; all items should come back
	nm, _ = m.Update(keyMsg("backspace"))
	m = nm.(Model)
	p = layerOf[*fileFinderPopup](m)
	if len(p.matches) <= narrowed && len(p.all) > narrowed {
		t.Fatalf("backspace should re-rank with empty query; got %d matches", len(p.matches))
	}
}

func TestFileFinderMouseSwallowed(t *testing.T) {
	// After open, the layer stack catches mouse via the generic topLayer() guard.
	// We just verify the popup is on the layer stack (mouse routing covered by
	// the existing TestClickIgnoredUnderOverlays mechanism).
	m := loadedModel(t)
	m, _ = m.openFileFinder()
	if m.topLayer() == nil {
		t.Fatal("file finder should be on the layer stack")
	}
}
