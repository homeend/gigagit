package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

func writeRepoConfig(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, ".gg.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func previewWt(t *testing.T, ts *httptest.Server, body string) (int, map[string]any) {
	t.Helper()
	return postJSONAny(t, ts, "/api/worktree/preview", body)
}

// The preview is the TUI popup's recompute over the effective config: the
// branch entrance takes the path template's fields, the server fixes seed and
// now on the first call and the page echoes them back.
func TestWorktreePreviewEndpoint(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	writeRepoConfig(t, dir, "[worktree]\npath_template = \"../<repo>.wt/<branch>-<user:ticket>-<random-alpha:4>\"\n")
	ts := serve(t, New(domain.Open(dir)))

	code, a := previewWt(t, ts, `{"from":"branch","start":"main","inputs":{"ticket":"T7"}}`)
	if code != http.StatusOK {
		t.Fatalf("code %d %v", code, a)
	}
	if fmt.Sprint(a["labels"]) != "[ticket]" || a["branch"] != "main" || a["existing"] != true || a["hook"] == true {
		t.Errorf("preview = %v", a)
	}
	want := "../" + filepath.Base(dir) + ".wt/main-T7-"
	if p, _ := a["path"].(string); !strings.HasPrefix(p, want) || len(p) != len(want)+4 {
		t.Errorf("path = %q, want %s????", p, want)
	}
	seed, _ := a["seed"].(string)
	now, _ := a["now"].(string)
	if seed == "" || now == "" {
		t.Fatalf("seed/now not fixed: %v", a)
	}
	_, b := previewWt(t, ts, fmt.Sprintf(`{"from":"branch","start":"main","inputs":{"ticket":"T7"},"seed":%q,"now":%q}`, seed, now))
	if b["path"] != a["path"] || b["seed"] != seed {
		t.Errorf("echoed seed previews %v, want %v", b["path"], a["path"])
	}
	_, c := previewWt(t, ts, fmt.Sprintf(`{"from":"branch","start":"main","branch":"side","path":"/x/y","seed":%q,"now":%q}`, seed, now))
	if c["branch"] != "side" || c["path"] != "/x/y" {
		t.Errorf("fixed names = %v", c)
	}
	_, e := previewWt(t, ts, `{"from":"branch","start":"main"}`)
	if e["error"] == nil || e["error"] == "" {
		t.Errorf("an unfilled field should report: %v", e)
	}
	_, r := previewWt(t, ts, `{"from":"tag","start":"v1"}`)
	if r["existing"] == true {
		t.Errorf("a tag entrance is not existing: %v", r)
	}
	if code, _ := previewWt(t, ts, `{"from":"bogus","start":"main"}`); code != http.StatusBadRequest {
		t.Errorf("bad from = %d, want 400", code)
	}
}

// On a branch start an edited name cuts a NEW branch from it (the TUI's e);
// the done event names the created worktree (create & switch re-roots there).
func TestCreateWorktreeNewBranchFromBranch(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))
	path := filepath.Join(t.TempDir(), "side")
	events := readSSE(t, ts, startOpBody(t, ts, fmt.Sprintf(`{"op":"create-worktree","branch":"main","name":"side","path":%q}`, path)), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	if got := gitRun(t, dir, "rev-parse", "side"); got != gitRun(t, dir, "rev-parse", "main") {
		t.Errorf("side at %s, want main's tip", got)
	}
	if p, _ := done["path"].(string); p == "" || filepath.Clean(p) != filepath.Clean(path) {
		t.Errorf("done path = %v, want %s", done["path"], path)
	}
}

// A remote start is checked against a fresh remote-branch read.
func TestCreateWorktreeRemoteStart(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	gitRun(t, dir, "remote", "add", "origin", dir)
	gitRun(t, dir, "fetch", "-q", "origin")
	ts := serve(t, New(domain.Open(dir)))
	path := filepath.Join(t.TempDir(), "fr")
	events := readSSE(t, ts, startOpBody(t, ts, fmt.Sprintf(`{"op":"create-worktree","ref":"origin/main","name":"fr","path":%q}`, path)), 30*time.Second)
	if done := events[len(events)-1]; done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	if code := postJSON(t, ts, "/api/op", fmt.Sprintf(`{"op":"create-worktree","ref":"origin/nope","name":"x","path":%q}`, path+"2"), "application/json", "", nil); code != http.StatusBadRequest {
		t.Errorf("unknown remote = %d, want 400", code)
	}
}

// path_seq: the path template's counters bump after a successful create
// and only then (the TUI's pendingSeqBump).
func TestCreateWorktreePathSeqBumps(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	writeRepoConfig(t, dir, "[worktree]\npath_template = \"../w-<seq:n:2>/<branch>\"\n")
	ts := serve(t, New(domain.Open(dir)))
	sha := gitRun(t, dir, "rev-parse", "HEAD")
	peek := func() string {
		_, p := previewWt(t, ts, `{"from":"commit","start":"`+sha+`","branch":"x"}`)
		return fmt.Sprint(p["path"])
	}
	if got := peek(); got != "../w-01/x" {
		t.Fatalf("first preview = %s", got)
	}
	// a failed create (the branch exists) must not bump
	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"create-worktree","sha":"`+sha+`","name":"main","path":"../w-01/main","path_seq":true}`), 30*time.Second)
	if done := events[len(events)-1]; done["ok"] == true {
		t.Fatalf("duplicate branch create succeeded: %v", done)
	}
	if got := peek(); got != "../w-01/x" {
		t.Fatalf("after a failure = %s, want the counter untouched", got)
	}
	events = readSSE(t, ts, startOpBody(t, ts, `{"op":"create-worktree","sha":"`+sha+`","name":"x","path":"../w-01/x","path_seq":true}`), 30*time.Second)
	if done := events[len(events)-1]; done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	if got := peek(); got != "../w-02/x" {
		t.Errorf("after a create = %s, want the counter bumped", got)
	}
}

// no_hook is the TUI's h: the configured hook is skipped, nothing asks.
func TestCreateWorktreeNoHook(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	writeRepoConfig(t, dir, "[worktree]\npost_create_hook = \"touch hook-ran\"\n")
	ts := serve(t, New(domain.Open(dir)))
	_, p := previewWt(t, ts, `{"from":"branch","start":"main"}`)
	if p["hook"] != true {
		t.Errorf("preview hook = %v, want true", p["hook"])
	}
	gitRun(t, dir, "branch", "hk")
	path := filepath.Join(t.TempDir(), "hk")
	events := readSSE(t, ts, startOpBody(t, ts, fmt.Sprintf(`{"op":"create-worktree","branch":"hk","name":"hk","path":%q,"no_hook":true}`, path)), 30*time.Second)
	for _, ev := range events {
		if ev["type"] == "decision" {
			t.Fatalf("no_hook still asked: %v", ev)
		}
	}
	if done := events[len(events)-1]; done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	if _, err := os.Stat(filepath.Join(path, "hook-ran")); !os.IsNotExist(err) {
		t.Errorf("the hook ran (stat err %v)", err)
	}
}

// mode is the commit entrance's mode line: at parent, the change staged.
func TestCreateWorktreeModeStaged(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 2)
	sha := gitRun(t, dir, "rev-parse", "HEAD")
	ts := serve(t, New(domain.Open(dir)))
	path := filepath.Join(t.TempDir(), "keep")
	events := readSSE(t, ts, startOpBody(t, ts, fmt.Sprintf(`{"op":"create-worktree","sha":%q,"name":"keep","path":%q,"mode":"staged"}`, sha, path)), 30*time.Second)
	if done := events[len(events)-1]; done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	if got := gitRun(t, path, "diff", "--cached", "--name-only"); got != "f.txt" {
		t.Errorf("staged = %q, want f.txt", got)
	}
	if code := postJSON(t, ts, "/api/op", fmt.Sprintf(`{"op":"create-worktree","sha":%q,"name":"k2","path":%q,"mode":"sideways"}`, sha, path+"2"), "application/json", "", nil); code != http.StatusBadRequest {
		t.Errorf("unknown mode = %d, want 400", code)
	}
}

// A picked prefix's counters bump after a successful create.
func TestCreateWorktreeBumpsPrefixSeq(t *testing.T) {
	isolatePrefixes(t)
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))
	if code := postJSON(t, ts, "/api/prefixes", `{"value":"p-<seq:pc:2>-","scope":"repo"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatal("add failed")
	}
	id := getPrefixes(t, ts).Prefixes[0].ID
	path := filepath.Join(t.TempDir(), "p")
	events := readSSE(t, ts, startOpBody(t, ts, fmt.Sprintf(`{"op":"create-worktree","branch":"main","name":"p-01-x","path":%q,"prefix_id":%q,"prefix_scope":"repo"}`, path, id)), 30*time.Second)
	if done := events[len(events)-1]; done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	if _, out := resolvePrefix(t, ts, `{"scope":"repo","id":"`+id+`"}`); out["resolved"] != "p-02-" {
		t.Errorf("after create = %v, want p-02-", out)
	}
}

// The prefix resolver takes the dialog's start point as <parent-branch>.
func TestPrefixResolveParent(t *testing.T) {
	isolatePrefixes(t)
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))
	if code := postJSON(t, ts, "/api/prefixes", `{"value":"<parent-branch>/","scope":"repo"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatal("add failed")
	}
	id := getPrefixes(t, ts).Prefixes[0].ID
	if _, out := resolvePrefix(t, ts, `{"scope":"repo","id":"`+id+`","parent":"v1"}`); out["resolved"] != "v1/" {
		t.Errorf("resolve with parent = %v, want v1/", out)
	}
	if _, out := resolvePrefix(t, ts, `{"scope":"repo","id":"`+id+`"}`); out["resolved"] != "main/" {
		t.Errorf("resolve without parent = %v, want main/", out)
	}
}

// A branch start that gets a new name is a StartPoint: it must be a local
// branch from a fresh read, never any revision git resolves.
func TestCreateWorktreeBranchStartIsABranch(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 2)
	ts := serve(t, New(domain.Open(dir)))
	for _, start := range []string{"main~1", "HEAD", "nope"} {
		body := fmt.Sprintf(`{"op":"create-worktree","branch":%q,"name":"x","path":%q}`, start, filepath.Join(t.TempDir(), "x"))
		if code := postJSON(t, ts, "/api/op", body, "application/json", "", nil); code != http.StatusBadRequest {
			t.Errorf("branch %q = %d, want 400", start, code)
		}
	}
}
