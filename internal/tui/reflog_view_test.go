package tui

import (
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func reflogTestModel() Model {
	return Model{
		sel:       map[panel]int{},
		width:     120,
		height:    40,
		sortModes: map[panel]sortMode{},
		reflog: []model.ReflogEntry{
			{Selector: "HEAD@{0}", Hash: "1111111111111111111111111111111111111111", ShortHash: "1111111", Subject: "checkout: moving from main to feature", Rel: "1 minute ago"},
			{Selector: "HEAD@{1}", Hash: "2222222222222222222222222222222222222222", ShortHash: "2222222", Subject: "commit: second", Rel: "2 minutes ago"},
		},
	}
}

func TestReflogListLenAndRows(t *testing.T) {
	m := reflogTestModel()
	if m.panelLen(panelReflog) != 2 {
		t.Fatalf("panelLen(reflog) = %d, want 2", m.panelLen(panelReflog))
	}
	rows := m.reflogRows()
	if len(rows) != 2 {
		t.Fatalf("reflogRows = %d, want 2", len(rows))
	}
	if !contains(rows[0], "1111111") || !contains(rows[0], "checkout") {
		t.Fatalf("row 0 = %q, want short hash + subject", rows[0])
	}
}

func TestReflogEnterOpensCommitFilesView(t *testing.T) {
	m := reflogTestModel()
	m.focus = panelReflog
	m.sel[panelReflog] = 1 // anchor on the SECOND row, not the default 0
	nm, cmd := m.Update(keyMsg("enter"))
	m = nm.(Model)
	if m.filesView == nil {
		t.Fatal("enter on a reflog row must open the files view")
	}
	if m.filesHash != "2222222222222222222222222222222222222222" {
		t.Fatalf("filesHash = %q, want the SECOND entry's hash (cursor-anchored)", m.filesHash)
	}
	if cmd == nil {
		t.Fatal("expected a files-load command")
	}
}

func TestBottomTabTogglesStagedReflog(t *testing.T) {
	m := reflogTestModel()
	m.focus = panelStaged
	nm, _ := m.Update(keyMsg("ctrl+right"))
	m = nm.(Model)
	if m.focus != panelReflog || m.bottomTab() != panelReflog {
		t.Fatalf("after ctrl+right: focus=%v bottomTab=%v, want panelReflog", m.focus, m.bottomTab())
	}
	nm, _ = m.Update(keyMsg("ctrl+left"))
	m = nm.(Model)
	if m.focus != panelStaged || m.bottomTab() != panelStaged {
		t.Fatalf("after ctrl+left: focus=%v bottomTab=%v, want panelStaged", m.focus, m.bottomTab())
	}
}
