package web

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

// dirtyRepo: one committed file edited in the working tree, a second
// committed file edited too, and one untracked file — three stash candidates
// of two different kinds.
func dirtyRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	write(t, dir, "one.txt", "committed one\n")
	write(t, dir, "two.txt", "committed two\n")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "base")
	write(t, dir, "one.txt", "edited one\n")
	write(t, dir, "two.txt", "edited two\n")
	write(t, dir, "new.txt", "untracked\n")
	return dir
}

func fileBody(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The headline: stashing a SUBSET takes exactly those files and leaves the
// rest dirty. That is the whole reason the checklist exists.
func TestOpStashPathsStashesTheSubsetOnly(t *testing.T) {
	t.Parallel()
	dir := dirtyRepo(t)
	ts := serve(t, New(domain.Open(dir)))

	body := `{"op":"stash-paths","paths":["one.txt"],"message":"just the one"}`
	events := readSSE(t, ts, startOpBody(t, ts, body), 30*time.Second)
	if done := events[len(events)-1]; done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	if got := fileBody(t, dir, "one.txt"); got != "committed one\n" {
		t.Errorf("one.txt = %q, want it reverted by the stash", got)
	}
	if got := fileBody(t, dir, "two.txt"); got != "edited two\n" {
		t.Errorf("two.txt = %q, want its edit left in the working tree", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "new.txt")); err != nil {
		t.Errorf("untracked new.txt was swept into the stash: %v", err)
	}
	if list := gitRun(t, dir, "stash", "list"); !strings.Contains(list, "just the one") {
		t.Errorf("stash list = %q, want the message that was sent", list)
	}
}

// An untracked file needs -u, which rides on the selection rather than a
// second question: picking one is asking for it.
func TestOpStashPathsIncludesUntrackedWhenPicked(t *testing.T) {
	t.Parallel()
	dir := dirtyRepo(t)
	ts := serve(t, New(domain.Open(dir)))

	body := `{"op":"stash-paths","paths":["new.txt"],"message":"the new file"}`
	events := readSSE(t, ts, startOpBody(t, ts, body), 30*time.Second)
	if done := events[len(events)-1]; done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	if _, err := os.Stat(filepath.Join(dir, "new.txt")); !os.IsNotExist(err) {
		t.Errorf("new.txt is still on disk (stat err %v) — -u was not applied", err)
	}
	if got := fileBody(t, dir, "one.txt"); got != "edited one\n" {
		t.Errorf("one.txt = %q, want it untouched", got)
	}
}

// An empty message falls back to git's own phrasing rather than stashing
// under a blank name.
func TestOpStashPathsDefaultsTheMessage(t *testing.T) {
	t.Parallel()
	dir := dirtyRepo(t)
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"stash-paths","paths":["one.txt"],"message":"   "}`), 30*time.Second)
	if done := events[len(events)-1]; done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	if list := gitRun(t, dir, "stash", "list"); !strings.Contains(list, "WIP on main") {
		t.Errorf("stash list = %q, want the WIP on <branch> default", list)
	}
}

// A path with nothing unstaged is refused for the WHOLE operation. Dropping
// it quietly would stash something other than what the list showed.
func TestOpStashPathsRefusesANonCandidate(t *testing.T) {
	t.Parallel()
	dir := dirtyRepo(t)
	gitRun(t, dir, "add", "two.txt") // fully staged: nothing left in the working tree
	ts := serve(t, New(domain.Open(dir)))

	code, body := postJSONRaw(t, ts, "/api/op", `{"op":"stash-paths","paths":["one.txt","two.txt"]}`)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", code)
	}
	if !strings.Contains(body["error"], "two.txt") {
		t.Errorf("error = %q, want it to name the offending path", body["error"])
	}
	if got := fileBody(t, dir, "one.txt"); got != "edited one\n" {
		t.Errorf("one.txt = %q — the refused batch stashed something anyway", got)
	}
}

// A conflicted path is never stashable: git refuses it, and stashing half a
// conflict is never what was meant.
func TestOpStashPathsRefusesConflictedPath(t *testing.T) {
	t.Parallel()
	dir := conflictRepo(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	code, body := postJSONRaw(t, ts, "/api/op", `{"op":"stash-paths","paths":["c1.txt"]}`)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", code)
	}
	if !strings.Contains(body["error"], "c1.txt") {
		t.Errorf("error = %q", body["error"])
	}
}

// No selection is a client bug, answered as one.
func TestOpStashPathsRequiresASelection(t *testing.T) {
	t.Parallel()
	dir := dirtyRepo(t)
	ts := serve(t, New(domain.Open(dir)))
	code, _ := postJSONRaw(t, ts, "/api/op", `{"op":"stash-paths","paths":[]}`)
	if code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", code)
	}
}
