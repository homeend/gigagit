package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/exttool"
)

// A finished review saves a review NOTE: the Commits review marker and the
// Branches ◆N read noteCounts.Reviews, so its arrival must reload srcNotes —
// whether or not its viewer can open now (another checkout, a modal up).
func TestReviewResultReloadsNoteCounts(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		info   domain.TaskInfo
		reload bool
	}{
		{"saved, shown elsewhere", domain.TaskInfo{Kind: exttool.CatReview, Key: "review — main", NoteID: "n1", Worktree: "/elsewhere"}, true},
		{"working changes: no note", domain.TaskInfo{Kind: exttool.CatReview, Key: "review — working changes", Worktree: "/elsewhere"}, false},
		{"save failed: no note", domain.TaskInfo{Kind: exttool.CatReview, Key: "review — main", NoteID: "n1", SaveErr: "disk full"}, false},
	} {
		m := diffModel()
		before := m.srcGen[srcNotes]
		m, _ = m.applyReviewResult(tc.info)
		if got := m.srcGen[srcNotes] > before; got != tc.reload {
			t.Errorf("%s: srcNotes reloaded = %v, want %v", tc.name, got, tc.reload)
		}
	}
}
