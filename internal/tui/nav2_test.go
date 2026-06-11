package tui

import (
	"testing"

	"github.com/gigagit/gg/internal/model"
)

// Exercises positive selection movement (the vacuous single-item clamp test
// never advances), then the clamp at the last row, then moving back up.
func TestDownAdvancesThenClampsWithMultipleItems(t *testing.T) {
	m := New(nil)
	m.commits = []model.Commit{{Hash: "a"}, {Hash: "b"}, {Hash: "c"}}
	m.focus = panelCommits

	step := func(key string) {
		t.Helper()
		updated, _ := m.Update(keyMsg(key))
		m = updated.(Model)
	}

	step("down")
	if m.sel[panelCommits] != 1 {
		t.Fatalf("after 1 down = %d, want 1", m.sel[panelCommits])
	}
	step("down")
	if m.sel[panelCommits] != 2 {
		t.Fatalf("after 2 downs = %d, want 2", m.sel[panelCommits])
	}
	step("down") // clamp at len-1 = 2
	if m.sel[panelCommits] != 2 {
		t.Fatalf("clamp = %d, want 2 (last index)", m.sel[panelCommits])
	}
	step("up")
	if m.sel[panelCommits] != 1 {
		t.Fatalf("after up = %d, want 1", m.sel[panelCommits])
	}
}
