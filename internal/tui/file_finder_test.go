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
	// type "f" to filter
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
