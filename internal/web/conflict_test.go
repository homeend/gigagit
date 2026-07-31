package web

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// conflictStatusResp decodes the parts of /api/status these tests assert on.
type conflictStatusResp struct {
	Counts   map[string]int `json:"counts"`
	Conflict *struct {
		Op         string `json:"op"`
		Source     string `json:"source"`
		Target     string `json:"target"`
		Desc       string `json:"desc"`
		Conflicted int    `json:"conflicted"`
	} `json:"conflict"`
}

// conflictedMergeState runs `git merge feature`, expecting the conflict to
// leave a paused merge in the tree. gitRun can't be used — it fails the test
// on any non-zero exit, and a conflicted merge exits 1 by design.
func conflictedMergeState(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("git", "-c", "commit.gpgsign=false", "merge", "feature")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("merge feature unexpectedly succeeded:\n%s", out)
	}
	if gitRun(t, dir, "ls-files", "-u") == "" {
		t.Fatal("no unmerged entries after conflicted merge")
	}
}

func TestStatusConflictObject(t *testing.T) {
	dir := conflictingRepo(t)
	conflictedMergeState(t, dir)
	ts := serve(t, New(domain.Open(dir)))

	var st conflictStatusResp
	if code := getJSON(t, ts, "/api/status", &st); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	c := st.Conflict
	if c == nil {
		t.Fatal("no conflict object on a paused merge")
	}
	if c.Op != "merge" || c.Source != "feature" || c.Target != "main" {
		t.Errorf("conflict = %+v, want merge feature→main", c)
	}
	if c.Desc != "merging feature into main" {
		t.Errorf("desc = %q", c.Desc)
	}
	if c.Conflicted != 1 {
		t.Errorf("conflicted = %d, want 1", c.Conflicted)
	}
}

func TestStatusConflictAbsentWhenClean(t *testing.T) {
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	var st conflictStatusResp
	if code := getJSON(t, ts, "/api/status", &st); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if st.Conflict != nil {
		t.Errorf("conflict = %+v, want absent", st.Conflict)
	}
}

// A paused op whose conflicts were all resolved by hand (file written + staged
// outside gg, merge never continued) must STILL report — that is the
// resume-paused-op parity the banner's Continue depends on.
func TestStatusConflictPausedZeroConflicts(t *testing.T) {
	dir := conflictingRepo(t)
	conflictedMergeState(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("resolved\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "f.txt")
	ts := serve(t, New(domain.Open(dir)))

	var st conflictStatusResp
	if code := getJSON(t, ts, "/api/status", &st); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if st.Conflict == nil || st.Conflict.Op != "merge" {
		t.Fatalf("conflict = %+v, want paused merge reported", st.Conflict)
	}
	if st.Conflict.Conflicted != 0 {
		t.Errorf("conflicted = %d, want 0", st.Conflict.Conflicted)
	}
}

type conflictHunksResp struct {
	Count int    `json:"count"`
	Hash  string `json:"hash"`
	Items []struct {
		Kind   string   `json:"kind"`
		Lines  []string `json:"lines"`
		Index  int      `json:"index"`
		Ours   []string `json:"ours"`
		Theirs []string `json:"theirs"`
	} `json:"items"`
}

func TestConflictHunks(t *testing.T) {
	dir := conflictingRepo(t)
	conflictedMergeState(t, dir)
	ts := serve(t, New(domain.Open(dir)))

	var d conflictHunksResp
	if code := getJSON(t, ts, "/api/conflict-hunks?path=f.txt", &d); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	if d.Count != 1 || len(d.Hash) != 64 {
		t.Fatalf("count = %d hash = %q", d.Count, d.Hash)
	}
	var blocks int
	for _, it := range d.Items {
		if it.Kind != "block" {
			continue
		}
		blocks++
		if it.Index != 0 || strings.Join(it.Ours, "\n") != "main" || strings.Join(it.Theirs, "\n") != "feature" {
			t.Errorf("block = %+v, want ours=[main] theirs=[feature]", it)
		}
	}
	if blocks != 1 {
		t.Errorf("blocks = %d, want 1", blocks)
	}
}

func TestConflictHunksEligibility(t *testing.T) {
	dir := conflictingRepo(t)
	conflictedMergeState(t, dir)
	ts := serve(t, New(domain.Open(dir)))

	if code := getJSON(t, ts, "/api/conflict-hunks?path=nope.txt", nil); code != http.StatusNotFound {
		t.Errorf("unknown path code = %d, want 404", code)
	}
	// a tracked-but-clean file is known yet not conflicted → 422
	cleanDir := newRepoDir(t, 1)
	gitRun(t, cleanDir, "checkout", "-b", "x") // any repo state; f.txt is clean
	tsClean := serve(t, New(domain.Open(cleanDir)))
	if code := getJSON(t, tsClean, "/api/conflict-hunks?path=f.txt", nil); code != http.StatusNotFound {
		t.Errorf("clean-repo code = %d, want 404 (clean file is not even in status)", code)
	}
}

// The user removed the markers by hand (or the file is binary): the picker
// has nothing to pick — typed 422, the client falls back to mark-resolved.
func TestConflictHunksMalformed(t *testing.T) {
	dir := conflictingRepo(t)
	conflictedMergeState(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("hand-resolved, no markers\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := serve(t, New(domain.Open(dir)))

	if code := getJSON(t, ts, "/api/conflict-hunks?path=f.txt", nil); code != http.StatusUnprocessableEntity {
		t.Errorf("code = %d, want 422", code)
	}
}

func TestResolveHunks(t *testing.T) {
	dir := conflictingRepo(t)
	conflictedMergeState(t, dir)
	ts := serve(t, New(domain.Open(dir)))

	var d conflictHunksResp
	if code := getJSON(t, ts, "/api/conflict-hunks?path=f.txt", &d); code != http.StatusOK {
		t.Fatalf("hunks code = %d", code)
	}
	var st conflictStatusResp
	code := postJSON(t, ts, "/api/resolve-hunks",
		`{"path":"f.txt","picks":["theirs"],"hash":"`+d.Hash+`"}`,
		"application/json", "", &st)
	if code != http.StatusOK {
		t.Fatalf("resolve code = %d", code)
	}
	// file resolved to the incoming side and staged
	b, err := os.ReadFile(filepath.Join(dir, "f.txt"))
	if err != nil || string(b) != "feature\n" {
		t.Errorf("f.txt = %q, %v; want feature", b, err)
	}
	if out := gitRun(t, dir, "ls-files", "-u"); out != "" {
		t.Errorf("still unmerged:\n%s", out)
	}
	// the merge is still paused with zero conflicts — Continue's moment
	if st.Conflict == nil || st.Conflict.Op != "merge" || st.Conflict.Conflicted != 0 {
		t.Errorf("response conflict = %+v, want paused merge with 0 conflicted", st.Conflict)
	}
}

// Two regions, opposite picks — the positional contract end-to-end. The
// working-tree content is ours to write (the index, not the file, is what
// keeps the path unmerged), so a synthetic two-block marker file works.
func TestResolveHunksMixedPicks(t *testing.T) {
	dir := conflictingRepo(t)
	conflictedMergeState(t, dir)
	two := "keep\n<<<<<<< HEAD\nm1\n=======\nf1\n>>>>>>> feature\nmid\n<<<<<<< HEAD\nm2\n=======\nf2\n>>>>>>> feature\nend\n"
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(two), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := serve(t, New(domain.Open(dir)))

	var d conflictHunksResp
	if code := getJSON(t, ts, "/api/conflict-hunks?path=f.txt", &d); code != http.StatusOK || d.Count != 2 {
		t.Fatalf("hunks code = %d count = %d, want 200/2", code, d.Count)
	}
	if code := postJSON(t, ts, "/api/resolve-hunks",
		`{"path":"f.txt","picks":["ours","theirs"],"hash":"`+d.Hash+`"}`,
		"application/json", "", nil); code != http.StatusOK {
		t.Fatalf("resolve code = %d", code)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "f.txt"))
	if string(b) != "keep\nm1\nmid\nf2\nend\n" {
		t.Errorf("f.txt = %q", b)
	}
}

func TestResolveHunksHashDrift(t *testing.T) {
	dir := conflictingRepo(t)
	conflictedMergeState(t, dir)
	ts := serve(t, New(domain.Open(dir)))

	var d conflictHunksResp
	if code := getJSON(t, ts, "/api/conflict-hunks?path=f.txt", &d); code != http.StatusOK {
		t.Fatalf("hunks code = %d", code)
	}
	// the file moves under the client's feet
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("<<<<<<< HEAD\nx\n=======\ny\n>>>>>>> feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := postJSON(t, ts, "/api/resolve-hunks",
		`{"path":"f.txt","picks":["theirs"],"hash":"`+d.Hash+`"}`,
		"application/json", "", nil); code != http.StatusConflict {
		t.Errorf("code = %d, want 409", code)
	}
}

func TestResolveHunksPickCount(t *testing.T) {
	dir := conflictingRepo(t)
	conflictedMergeState(t, dir)
	ts := serve(t, New(domain.Open(dir)))

	var d conflictHunksResp
	getJSON(t, ts, "/api/conflict-hunks?path=f.txt", &d)
	for _, bad := range []string{`[]`, `["theirs","ours"]`, `["sideways"]`} {
		if code := postJSON(t, ts, "/api/resolve-hunks",
			`{"path":"f.txt","picks":`+bad+`,"hash":"`+d.Hash+`"}`,
			"application/json", "", nil); code != http.StatusBadRequest {
			t.Errorf("picks %s: code = %d, want 400", bad, code)
		}
	}
}
