package web

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

type lockList struct {
	Locks []lockRow `json:"locks"`
}

func touchLock(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, ".git", name)
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func removeLocksBody(paths ...string) string {
	quoted := make([]string, 0, len(paths))
	for _, p := range paths {
		quoted = append(quoted, fmt.Sprintf("%q", p))
	}
	return `{"op":"remove-locks","paths":[` + strings.Join(quoted, ",") + `]}`
}

// A clean repo has nothing to report — the notice must not appear on an
// empty list, so the empty list has to be an empty list and not null.
func TestLocksEmptyOnACleanRepo(t *testing.T) {
	t.Parallel()
	ts := serve(t, New(domain.Open(newRepoDir(t, 1))))
	var out lockList
	if code := getJSON(t, ts, "/api/locks", &out); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	if len(out.Locks) != 0 {
		t.Fatalf("locks = %v, want none", out.Locks)
	}
}

// The whole point of the surface: a stranded index.lock is listed, with the
// age that is the only staleness hint gg can honestly offer.
func TestLocksListsAStrandedLock(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	p := touchLock(t, dir, "index.lock")
	ts := serve(t, New(domain.Open(dir)))

	var out lockList
	if code := getJSON(t, ts, "/api/locks", &out); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	if len(out.Locks) != 1 {
		t.Fatalf("locks = %v, want exactly one", out.Locks)
	}
	got := out.Locks[0]
	if got.Name != "index.lock" {
		t.Errorf("name = %q", got.Name)
	}
	if !samePathish(got.Path, p) {
		t.Errorf("path = %q, want %q", got.Path, p)
	}
	if got.AgeSeconds < 0 {
		t.Errorf("age = %d, want a non-negative age", got.AgeSeconds)
	}
	if _, err := time.Parse(time.RFC3339, got.ModTime); err != nil {
		t.Errorf("mtime %q: %v", got.ModTime, err)
	}
}

// Clearing removes the file and empties the list — the client re-reads after
// the op and hides the notice on an empty answer.
func TestLocksClearRemovesThemAndEmptiesTheList(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	index := touchLock(t, dir, "index.lock")
	head := touchLock(t, dir, "HEAD.lock")
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, removeLocksBody(index, head)), 30*time.Second)
	if done := events[len(events)-1]; done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	for _, p := range []string{index, head} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s still exists (stat err %v)", filepath.Base(p), err)
		}
	}
	var out lockList
	getJSON(t, ts, "/api/locks", &out)
	if len(out.Locks) != 0 {
		t.Errorf("locks after clearing = %v, want none", out.Locks)
	}
}

// The wire cannot aim the delete somewhere else. The guard lives in the
// engine (one home for the rule), so the refusal arrives as a failed op —
// and nothing outside the git dir is touched.
func TestLocksRefusesAPathOutsideTheGitDir(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	outside := filepath.Join(t.TempDir(), "index.lock")
	if err := os.WriteFile(outside, []byte("not ours\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, removeLocksBody(outside)), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] == true {
		t.Fatalf("removing a lock outside the git dir succeeded: %v", done)
	}
	if msg, _ := done["error"].(string); !strings.Contains(msg, "outside this repository") {
		t.Errorf("error = %q", msg)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Errorf("the file outside the repo was removed: %v", err)
	}
}

// A file that is inside the git dir but is not a lockfile is refused too:
// the name is half the guard.
func TestLocksRefusesANonLockFileInTheGitDir(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	cfg := filepath.Join(dir, ".git", "config")
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, removeLocksBody(cfg)), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] == true {
		t.Fatalf("removing .git/config succeeded: %v", done)
	}
	if _, err := os.Stat(cfg); err != nil {
		t.Errorf(".git/config was removed: %v", err)
	}
}

// samePathish compares two absolute paths after cleaning. Paths cross this
// boundary from git, from filepath, and from a temp dir that may itself be a
// symlink on macOS — a raw string compare is the bug that has shipped here
// before.
func samePathish(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ra, erra := filepath.EvalSymlinks(a)
	rb, errb := filepath.EvalSymlinks(b)
	return erra == nil && errb == nil && filepath.Clean(ra) == filepath.Clean(rb)
}
