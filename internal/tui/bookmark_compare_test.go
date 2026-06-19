package tui

import (
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func TestOpenCompareFocusedVsBookmark(t *testing.T) {
	m := footerModel()
	ref := model.FileRef{Source: model.SourceCommit, Locator: "aaaa1111", Path: "a.go"}
	bm := model.Bookmark{ID: "bm9", State: model.StateCommitted, Commit: "bbbb2222", SHA: "blob22", Path: "b.go"}
	u, cmd := m.openCompareFocusedVsBookmark(ref, "commit a.go", bm)
	if u.diffView == nil {
		t.Fatal("openCompareFocusedVsBookmark must open a diff view")
	}
	if u.diffView.title != "a.go ↔ b.go" {
		t.Errorf("diff title = %q, want \"a.go ↔ b.go\"", u.diffView.title)
	}
	if u.diffTag != "cmpbm:a.go:bm9" {
		t.Errorf("diffTag = %q, want cmpbm:a.go:bm9", u.diffTag)
	}
	if cmd == nil {
		t.Error("expected a load command")
	}
}
