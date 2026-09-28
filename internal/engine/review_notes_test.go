package engine

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// Every review context document asks for the structured review document on
// $GG_MESSAGE_FILE, and no separate notes file exists any more.
func TestReviewContextAlwaysAsksForTheDocument(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip()
	}
	if runtime.GOOS == "windows" {
		t.Skip("uses sh/cp")
	}
	dir, repo := newRepo(t)
	stageAndCommit(t, dir, repo, "a.txt", "one\n", "c1")
	cmd := `cp "$GG_CONTEXT_FILE" "` + dir + `/seen.ctx"; printf '%s' "${GG_NOTES_FILE:-unset}" > "` + dir + `/seen.env"; printf x > "$GG_MESSAGE_FILE"`
	if _, err := (ReviewChanges{Command: cmd, Dir: dir, Diff: model.DiffSpec{Rev: "HEAD"}, RangeLabel: "HEAD"}).
		Run(context.Background(), OpDeps{Repo: repo, CaptureRunner: ShellCaptureRunner{}}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := strings.TrimSpace(readFile(t, dir+"/seen.env")); got != "unset" {
		t.Fatalf("GG_NOTES_FILE must not be set, got %q", got)
	}
	ctxSeen := readFile(t, dir+"/seen.ctx")
	for _, want := range []string{"## Review output", `"version": 1`, `"summary"`, `"annotations"`, `"newRange"`, `"oldRange"`, `"meta"`, "$GG_MESSAGE_FILE", "ONLY"} {
		if !strings.Contains(ctxSeen, want) {
			t.Errorf("context lacks %q:\n%s", want, ctxSeen)
		}
	}
	if strings.Contains(ctxSeen, "Inline notes") {
		t.Error("the old notes-file paragraph must be gone")
	}
}
