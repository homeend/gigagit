package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestRecycleDirtyOverviewMarkers(t *testing.T) {
	t.Parallel()
	got := recycleDirtyOverview([]model.FileStatus{
		{Path: "staged.go", Staged: 'M', Unstaged: '.'},
		{Path: "unstaged.go", Staged: '.', Unstaged: 'M'},
		{Path: "both.go", Staged: 'M', Unstaged: 'M'},
		{Path: "new.go", OrigPath: "old.go", Staged: 'R', Unstaged: '.'},
		{Path: "clash.go", Kind: model.KindUnmerged, Staged: 'U', Unstaged: 'U'},
		{Path: "bare-clash.go", Kind: model.KindUnmerged},
		{Path: "notes.txt", Kind: model.KindUntracked},
	})
	want := strings.Join([]string{
		"M. staged.go",
		".M unstaged.go",
		"MM both.go",
		"R. old.go → new.go",
		"UU clash.go",
		"UU bare-clash.go",
		"?? notes.txt",
	}, "\n")
	if got != want {
		t.Fatalf("overview =\n%s\nwant\n%s", got, want)
	}
}

func TestRecycleDirtyOverviewCapsAtTen(t *testing.T) {
	t.Parallel()
	mk := func(n int) []model.FileStatus {
		fs := make([]model.FileStatus, n)
		for i := range fs {
			fs[i] = model.FileStatus{Path: fmt.Sprintf("f%02d", i), Kind: model.KindUntracked}
		}
		return fs
	}
	if got := recycleDirtyOverview(mk(10)); strings.Count(got, "\n") != 9 || strings.Contains(got, "more") {
		t.Fatalf("ten files must all be listed, no tail:\n%s", got)
	}
	lines := strings.Split(recycleDirtyOverview(mk(13)), "\n")
	if len(lines) != 11 {
		t.Fatalf("13 files: %d lines, want 10 rows + a tail:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	if lines[9] != "?? f09" || lines[10] != "… and 3 more" {
		t.Fatalf("tail = %q, %q", lines[9], lines[10])
	}
}

// The question itself carries the overview: the user decides from it.
func TestRecycleWorktreeDirtyPromptListsFiles(t *testing.T) {
	t.Parallel()
	_, deps, wt := recycleFixture(t)
	os.WriteFile(filepath.Join(wt, "new.txt"), []byte("n\n"), 0o644)
	ch := make(chan Event, 32)
	deps.Events = ch
	deps.Decider = MapDecider{RecycleDirtyDecisionID: "abort"}
	if _, err := (RecycleWorktree{Dir: wt, Branch: "target", Now: fixedNow}).Run(context.Background(), deps); err != nil {
		t.Fatalf("Run: %v", err)
	}
	close(ch)
	for _, e := range drain(ch) {
		if d, ok := e.(DecisionNeeded); ok && d.Request.ID == RecycleDirtyDecisionID {
			if !strings.HasSuffix(d.Request.Prompt, ":\n\n?? new.txt") {
				t.Fatalf("prompt = %q", d.Request.Prompt)
			}
			if len(d.Request.PromptMsg.Args) != 3 {
				t.Fatalf("the overview must ride as a prompt arg: %v", d.Request.PromptMsg.Args)
			}
			return
		}
	}
	t.Fatal("expected the recycle.dirty decision")
}
