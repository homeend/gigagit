package web

import (
	"context"
	"net/http"
	"os/exec"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// /api/notes/counts names the pull requests holding a local review
// (pr_reviewed) — the Pull requests list's ✎ — and sends an empty array, not
// null, when there is none.
func TestNotesCountsNamePRsWithALocalReview(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 2)
	svc := domain.Open(dir)
	svc.UseNotesDir(t.TempDir())
	ts := serve(t, New(svc))
	var counts struct {
		PRReviewed []int `json:"pr_reviewed"`
	}
	raw := map[string]any{}
	if code := getJSON(t, ts, "/api/notes/counts", &raw); code != http.StatusOK {
		t.Fatalf("counts = %d", code)
	}
	if v, ok := raw["pr_reviewed"].([]any); !ok || len(v) != 0 {
		t.Fatalf("no review: pr_reviewed = %#v, want []", raw["pr_reviewed"])
	}
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	head := strings.TrimSpace(string(out))
	if _, err := svc.NoteAdd(context.Background(), model.Note{
		Address: model.FileAddress{State: model.StateCommitted, Commit: head, Path: "f.txt"}, Side: model.NoteSideNew, Range: [2]int{1, 1},
		Summary: "why?", Preview: "main...refs/gg/pr/7",
	}); err != nil {
		t.Fatal(err)
	}
	if code := getJSON(t, ts, "/api/notes/counts", &counts); code != http.StatusOK {
		t.Fatalf("counts = %d", code)
	}
	if len(counts.PRReviewed) != 1 || counts.PRReviewed[0] != 7 {
		t.Fatalf("pr_reviewed = %v, want [7]", counts.PRReviewed)
	}
}
