package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestOpenCompareFilesPopulatesView(t *testing.T) {
	m := loadedModel(t)
	if len(m.commits) == 0 {
		t.Skip("no commits loaded")
	}
	left := model.Endpoint{Kind: model.EndpointCommit, Hash: m.commits[0].Hash}
	right := model.Endpoint{Kind: model.EndpointWorkTree}

	m2, cmd := m.openCompareFiles(left, right)
	if !m2.inCompareMode() || m2.filesView == nil {
		t.Fatal("compare mode + filesView must be set")
	}
	if m2.filesTitle != left.Display()+" ↔ "+right.Display() {
		t.Errorf("title = %q", m2.filesTitle)
	}
	if cmd == nil {
		t.Fatal("expected a load command")
	}
	// drive the async load
	msg := cmd()
	cm, ok := msg.(compareFilesMsg)
	if !ok {
		t.Fatalf("expected compareFilesMsg, got %T", msg)
	}
	if cm.err != nil {
		t.Fatalf("load err: %v", cm.err)
	}
	m3, _ := m2.Update(cm)
	mm := m3.(Model)
	if len(mm.filesView.lines) == 0 || mm.filesView.lines[0].text == "(loading…)" {
		t.Errorf("file list not applied: %+v", mm.filesView.lines)
	}
}

func TestCompareFilesMsgStaleDropped(t *testing.T) {
	m := loadedModel(t)
	left := model.Endpoint{Kind: model.EndpointCommit, Hash: "abc"}
	m2, _ := m.openCompareFiles(left, model.Endpoint{Kind: model.EndpointWorkTree})
	before := len(m2.filesView.lines)
	m3, _ := m2.Update(compareFilesMsg{tag: "stale", files: nil})
	if got := len(m3.(Model).filesView.lines); got != before {
		t.Errorf("stale msg must not mutate the view (%d → %d)", before, got)
	}
}
