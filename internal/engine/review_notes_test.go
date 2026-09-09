package engine

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// With NotesFile set the tool sees $GG_NOTES_FILE and the context doc gains the
// "## Inline notes (optional)" paragraph naming that path.
func TestReviewChangesNotesFileEnvAndContext(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip()
	}
	if runtime.GOOS == "windows" {
		t.Skip("uses sh/cp")
	}
	dir, repo := newRepo(t)
	stageAndCommit(t, dir, repo, "a.txt", "one\n", "c1")
	stageAndCommit(t, dir, repo, "a.txt", "one\ntwo\n", "c2")

	notesPath := dir + "/notes.json"
	cmd := `cp "$GG_CONTEXT_FILE" "` + dir + `/seen.ctx"; printf '%s' "$GG_NOTES_FILE" > "` + dir + `/seen.env"; printf x > "$GG_MESSAGE_FILE"`
	_, err := ReviewChanges{
		Command: cmd, Dir: dir, Diff: model.DiffSpec{Rev: "HEAD~1..HEAD"},
		RangeLabel: "HEAD~1..HEAD", NotesFile: notesPath,
	}.Run(context.Background(), OpDeps{Repo: repo, CaptureRunner: ShellCaptureRunner{}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := strings.TrimSpace(readFile(t, dir+"/seen.env")); got != notesPath {
		t.Fatalf("GG_NOTES_FILE = %q, want %q", got, notesPath)
	}
	ctxSeen := readFile(t, dir+"/seen.ctx")
	for _, want := range []string{
		"## Inline notes (optional)",
		"hunk agent-context JSON (version 1)",
		notesPath,
		`{"version":1,"files":[{"path":"…","annotations":`,
		"1-based in the NEW version of each file",
		"do not annotate every hunk",
	} {
		if !strings.Contains(ctxSeen, want) {
			t.Errorf("context doc missing %q:\n%s", want, ctxSeen)
		}
	}
}

// Without NotesFile nothing changes: no env var, no extra paragraph.
func TestReviewChangesWithoutNotesFileIsUnchanged(t *testing.T) {
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
		t.Fatalf("GG_NOTES_FILE must not be set without NotesFile, got %q", got)
	}
	if strings.Contains(readFile(t, dir+"/seen.ctx"), "Inline notes") {
		t.Fatal("the context doc must not mention notes when NotesFile is empty")
	}
}
