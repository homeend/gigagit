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

// A modify/delete conflict has no markers at all: one side deletes the path,
// so UnmergedStages is missing a stage and ConflictPickerFile falls back to
// the worktree bytes it left there (git's own "ours" copy, unmarked) — the
// picker has nothing to pick. Typed 422, the client falls back to
// mark-resolved. (A hand-edited worktree no longer reaches this path at all:
// the loader regenerates from the index stages, which the edit didn't touch.)
func TestConflictHunksMalformed(t *testing.T) {
	dir := newRepoDir(t, 1)
	gitRun(t, dir, "checkout", "-b", "feature")
	gitRun(t, dir, "rm", "f.txt")
	gitRun(t, dir, "commit", "-m", "feature deletes f.txt")
	gitRun(t, dir, "checkout", "main")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("main edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "main edit")
	conflictedMergeState(t, dir)
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

// twoRegionConflictRepo builds a real merge conflict with two separate
// conflicting regions in the same file — the loader now regenerates from the
// index stages (not the worktree bytes), so the multi-block positional
// contract needs an actual multi-region conflict rather than a synthetic
// worktree overwrite.
func twoRegionConflictRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	base := "keep\nbase1\nc1\nc2\nc3\nc4\nc5\nbase2\nend\n"
	f := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(f, []byte(base), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "base")

	gitRun(t, dir, "checkout", "-b", "feature")
	feature := strings.NewReplacer("base1", "f1", "base2", "f2").Replace(base)
	if err := os.WriteFile(f, []byte(feature), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "feature edit")

	gitRun(t, dir, "checkout", "main")
	mainC := strings.NewReplacer("base1", "m1", "base2", "m2").Replace(base)
	if err := os.WriteFile(f, []byte(mainC), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "main edit")

	conflictedMergeState(t, dir)
	return dir
}

// Two regions, opposite picks — the positional contract end-to-end.
func TestResolveHunksMixedPicks(t *testing.T) {
	dir := twoRegionConflictRepo(t)
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
	want := "keep\nm1\nc1\nc2\nc3\nc4\nc5\nf2\nend\n"
	if string(b) != want {
		t.Errorf("f.txt = %q, want %q", b, want)
	}
}

// gitIndexInfo feeds "<mode> <object> <stage>\t<path>\n" lines to
// `git update-index --index-info` — the only way to rewrite a single
// unmerged stage in place without resolving the conflict. Needed because
// gitRun has no stdin; this is the drift test's own plumbing.
func gitIndexInfo(t *testing.T, dir, stdin string) {
	t.Helper()
	cmd := exec.Command("git", "-c", "commit.gpgsign=false", "update-index", "--index-info")
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(stdin)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git update-index --index-info: %v\n%s", err, out)
	}
}

// The loader now regenerates from the index stages, not the worktree bytes,
// so drift has to happen in the index: rewrite stage 3 (theirs) to a new
// blob between GET and POST. The path stays unmerged (still 3 stages), so
// the eligibility gate still passes — only the regenerated content, and
// therefore the hash, changes under the client's feet.
func TestResolveHunksHashDrift(t *testing.T) {
	dir := conflictingRepo(t)
	conflictedMergeState(t, dir)
	ts := serve(t, New(domain.Open(dir)))

	var d conflictHunksResp
	if code := getJSON(t, ts, "/api/conflict-hunks?path=f.txt", &d); code != http.StatusOK {
		t.Fatalf("hunks code = %d", code)
	}
	drifted := filepath.Join(t.TempDir(), "drifted.txt")
	if err := os.WriteFile(drifted, []byte("drifted\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	blob := gitRun(t, dir, "hash-object", "-w", drifted)
	gitIndexInfo(t, dir, "100644 "+blob+" 3\tf.txt\n")

	if code := postJSON(t, ts, "/api/resolve-hunks",
		`{"path":"f.txt","picks":["theirs"],"hash":"`+d.Hash+`"}`,
		"application/json", "", nil); code != http.StatusConflict {
		t.Errorf("code = %d, want 409", code)
	}
}

// nestedMarkerRepo builds a repo whose conflicted file's content itself
// contains literal 7-char conflict-marker lines (a conflict once committed
// unresolved), the case raw-worktree parsing cannot disambiguate. The base
// content's ghost markers are untouched by either branch — only the trailing
// "end" line diverges — so the merge produces exactly one real conflict
// region, away from the ghost lines, and both sides keep those lines intact.
func nestedMarkerRepo(t *testing.T) (dir, path string) {
	t.Helper()
	dir = t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	base := "top\n<<<<<<< HEAD\nghost\n=======\nother\n>>>>>>> x\nbottom\nend\n"
	weird := filepath.Join(dir, "weird.txt")
	if err := os.WriteFile(weird, []byte(base), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "base")

	gitRun(t, dir, "checkout", "-b", "feature")
	if err := os.WriteFile(weird, []byte(strings.Replace(base, "end\n", "END-F\n", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "feature edit")

	gitRun(t, dir, "checkout", "main")
	if err := os.WriteFile(weird, []byte(strings.Replace(base, "end\n", "END-M\n", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "main edit")

	conflictedMergeState(t, dir)
	return dir, "weird.txt"
}

func TestConflictHunksNestedMarkers(t *testing.T) {
	dir, path := nestedMarkerRepo(t)
	ts := serve(t, New(domain.Open(dir)))
	var d struct {
		Count int    `json:"count"`
		Hash  string `json:"hash"`
		Items []struct {
			Kind   string   `json:"kind"`
			Lines  []string `json:"lines"`
			Ours   []string `json:"ours"`
			Theirs []string `json:"theirs"`
		} `json:"items"`
	}
	if code := getJSON(t, ts, "/api/conflict-hunks?path="+path, &d); code != http.StatusOK {
		t.Fatalf("nested-marker file must load via regeneration, got %d", code)
	}
	if d.Count != 1 {
		t.Fatalf("count = %d, want 1 real region", d.Count)
	}
	// The literal ghost markers are passthrough text, not a block.
	found := false
	for _, it := range d.Items {
		if it.Kind == "text" && strings.Contains(strings.Join(it.Lines, "\n"), "<<<<<<< HEAD") {
			found = true
		}
	}
	if !found {
		t.Fatalf("literal marker lines must survive as passthrough text")
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
