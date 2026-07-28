package web

import (
	"encoding/json"
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

// irebaseRepo builds main plus a `work` branch with n commits on top of it,
// one file each so no two commits touch the same path.
func irebaseRepo(t *testing.T, n int) string {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "base")
	gitRun(t, dir, "checkout", "-q", "-b", "work")
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("w%d.txt", i+1)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		gitRun(t, dir, "add", "-A")
		gitRun(t, dir, "commit", "-m", fmt.Sprintf("w%d", i+1))
	}
	return dir
}

type rangeResp struct {
	Branch  string `json:"branch"`
	Onto    string `json:"onto"`
	Commits []struct {
		Sha     string `json:"sha"`
		Short   string `json:"short"`
		Subject string `json:"subject"`
		Message string `json:"message"`
	} `json:"commits"`
}

func rebaseRange(t *testing.T, ts *httptest.Server, branch, onto string) rangeResp {
	t.Helper()
	var out rangeResp
	if code := getJSON(t, ts, "/api/rebase-range?branch="+branch+"&onto="+onto, &out); code != http.StatusOK {
		t.Fatalf("rebase-range code = %d", code)
	}
	return out
}

// planJSON renders {sha, action, msg} rows as the wire's todo-order plan.
func planJSON(rows ...[3]string) string {
	parts := make([]string, 0, len(rows))
	for _, r := range rows {
		b, _ := json.Marshal(map[string]string{"sha": r[0], "action": r[1], "msg": r[2]})
		parts = append(parts, string(b))
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func TestRebaseRangeOldestFirst(t *testing.T) {
	dir := irebaseRepo(t, 3)
	ts := serve(t, New(domain.Open(dir)))

	got := rebaseRange(t, ts, "work", "main")
	if len(got.Commits) != 3 {
		t.Fatalf("commits = %d, want 3", len(got.Commits))
	}
	// git todo order: oldest first, and the base commit is NOT in the range.
	if got.Commits[0].Subject != "w1" || got.Commits[2].Subject != "w3" {
		t.Errorf("order = %s..%s, want w1..w3", got.Commits[0].Subject, got.Commits[2].Subject)
	}
	if got.Commits[0].Message == "" {
		t.Error("message is empty — reword could not edit the body")
	}
}

func TestRebaseRangeRefusesUnknownAndSelf(t *testing.T) {
	dir := irebaseRepo(t, 2)
	ts := serve(t, New(domain.Open(dir)))

	for _, c := range []struct {
		q    string
		want int
	}{
		{"branch=work&onto=nope", http.StatusNotFound},
		{"branch=nope&onto=main", http.StatusNotFound},
		{"branch=work&onto=work", http.StatusBadRequest},
		{"branch=&onto=main", http.StatusBadRequest},
		{"branch=work&onto=-x", http.StatusBadRequest},
	} {
		if code := getJSON(t, ts, "/api/rebase-range?"+c.q, nil); code != c.want {
			t.Errorf("%s: code = %d, want %d", c.q, code, c.want)
		}
	}
}

// The editor's core: reorder + squash + reword + drop in one plan.
func TestOpInteractiveRebaseAppliesPlan(t *testing.T) {
	useGGSequenceEditor(t)
	dir := irebaseRepo(t, 4) // w1 w2 w3 w4 on top of main
	ts := serve(t, New(domain.Open(dir)))

	cs := rebaseRange(t, ts, "work", "main").Commits
	sha := map[string]string{}
	for _, c := range cs {
		sha[c.Subject] = c.Sha
	}
	// todo order: w2 first (reworded), then w1, then w3 squashed into w1,
	// and w4 dropped.
	plan := planJSON(
		[3]string{sha["w2"], "reword", "w2 renamed\n\nbody\n"},
		[3]string{sha["w1"], "pick", ""},
		[3]string{sha["w3"], "squash", ""},
		[3]string{sha["w4"], "drop", ""},
	)
	body := `{"op":"interactive-rebase","branch":"work","onto":"main","plan":` + plan + `}`
	events := readSSE(t, ts, startOpBody(t, ts, body), 60*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}

	got := subjects(t, dir) // newest-first
	if strings.Join(got, ",") != "w1,w2 renamed,base" {
		t.Fatalf("log = %v, want [w1, w2 renamed, base]", got)
	}
	// w3 was squashed INTO w1, so its file rides along with w1's commit.
	if _, err := os.Stat(filepath.Join(dir, "w3.txt")); err != nil {
		t.Errorf("w3.txt missing — the squash lost its content: %v", err)
	}
	// w4 was dropped: gone from the tree entirely.
	if _, err := os.Stat(filepath.Join(dir, "w4.txt")); !os.IsNotExist(err) {
		t.Errorf("w4.txt still present after dropping w4: err=%v", err)
	}
	if !strings.Contains(gitRun(t, dir, "log", "-1", "--format=%B", "work~1"), "body") {
		t.Error("the reworded commit lost its body")
	}
}

// A plan that no longer describes the branch is refused, not applied to a
// history that moved under the open editor.
func TestOpInteractiveRebaseStalePlan(t *testing.T) {
	dir := irebaseRepo(t, 3)
	ts := serve(t, New(domain.Open(dir)))
	cs := rebaseRange(t, ts, "work", "main").Commits

	// A commit lands while the editor is open.
	if err := os.WriteFile(filepath.Join(dir, "late.txt"), []byte("late\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "late")
	before := gitRun(t, dir, "rev-parse", "work")

	plan := planJSON(
		[3]string{cs[0].Sha, "pick", ""},
		[3]string{cs[1].Sha, "pick", ""},
		[3]string{cs[2].Sha, "pick", ""},
	)
	code, out := postJSONRaw(t, ts, "/api/op", `{"op":"interactive-rebase","branch":"work","onto":"main","plan":`+plan+`}`)
	if code != http.StatusConflict {
		t.Fatalf("code = %d, want 409 (body %v)", code, out)
	}
	if after := gitRun(t, dir, "rev-parse", "work"); after != before {
		t.Errorf("work moved: %s -> %s", before, after)
	}
}

// A plan naming a commit outside the range is refused even when the COUNT
// matches — the count alone would let a swapped sha through.
func TestOpInteractiveRebaseForeignCommit(t *testing.T) {
	dir := irebaseRepo(t, 2)
	ts := serve(t, New(domain.Open(dir)))
	cs := rebaseRange(t, ts, "work", "main").Commits
	base := strings.TrimSpace(gitRun(t, dir, "rev-parse", "main"))
	before := gitRun(t, dir, "rev-parse", "work")

	plan := planJSON(
		[3]string{cs[0].Sha, "pick", ""},
		[3]string{base, "pick", ""}, // in the repo, but not in the range
	)
	code, out := postJSONRaw(t, ts, "/api/op", `{"op":"interactive-rebase","branch":"work","onto":"main","plan":`+plan+`}`)
	if code != http.StatusConflict {
		t.Fatalf("code = %d, want 409 (body %v)", code, out)
	}
	if after := gitRun(t, dir, "rev-parse", "work"); after != before {
		t.Errorf("work moved: %s -> %s", before, after)
	}
}

// The same commit twice has the right COUNT and every sha is in the range —
// only the once-each rule catches it.
func TestOpInteractiveRebaseDuplicateCommit(t *testing.T) {
	dir := irebaseRepo(t, 2)
	ts := serve(t, New(domain.Open(dir)))
	cs := rebaseRange(t, ts, "work", "main").Commits
	before := gitRun(t, dir, "rev-parse", "work")

	plan := planJSON(
		[3]string{cs[0].Sha, "pick", ""},
		[3]string{cs[0].Sha, "pick", ""},
	)
	code, out := postJSONRaw(t, ts, "/api/op", `{"op":"interactive-rebase","branch":"work","onto":"main","plan":`+plan+`}`)
	if code != http.StatusConflict {
		t.Fatalf("code = %d, want 409 (body %v)", code, out)
	}
	if after := gitRun(t, dir, "rev-parse", "work"); after != before {
		t.Errorf("work moved: %s -> %s", before, after)
	}
}

func TestOpInteractiveRebaseRejectsMalformedPlans(t *testing.T) {
	dir := irebaseRepo(t, 2)
	ts := serve(t, New(domain.Open(dir)))
	cs := rebaseRange(t, ts, "work", "main").Commits
	before := gitRun(t, dir, "rev-parse", "work")

	cases := []struct {
		name string
		body string
		want int
	}{
		{"empty plan", `{"op":"interactive-rebase","branch":"work","onto":"main","plan":[]}`, http.StatusBadRequest},
		{"unknown action", `{"op":"interactive-rebase","branch":"work","onto":"main","plan":` +
			planJSON([3]string{cs[0].Sha, "fixup", ""}, [3]string{cs[1].Sha, "pick", ""}) + `}`, http.StatusBadRequest},
		{"squash the oldest", `{"op":"interactive-rebase","branch":"work","onto":"main","plan":` +
			planJSON([3]string{cs[0].Sha, "squash", ""}, [3]string{cs[1].Sha, "pick", ""}) + `}`, http.StatusBadRequest},
		{"reword with no message", `{"op":"interactive-rebase","branch":"work","onto":"main","plan":` +
			planJSON([3]string{cs[0].Sha, "pick", ""}, [3]string{cs[1].Sha, "reword", ""}) + `}`, http.StatusBadRequest},
		{"flag-shaped branch", `{"op":"interactive-rebase","branch":"-x","onto":"main","plan":` +
			planJSON([3]string{cs[0].Sha, "pick", ""}) + `}`, http.StatusBadRequest},
		{"unknown branch", `{"op":"interactive-rebase","branch":"nope","onto":"main","plan":` +
			planJSON([3]string{cs[0].Sha, "pick", ""}) + `}`, http.StatusNotFound},
	}
	for _, c := range cases {
		code, out := postJSONRaw(t, ts, "/api/op", c.body)
		if code != c.want {
			t.Errorf("%s: code = %d, want %d (body %v)", c.name, code, c.want, out)
		}
	}
	if after := gitRun(t, dir, "rev-parse", "work"); after != before {
		t.Errorf("work moved despite every plan being refused: %s -> %s", before, after)
	}
}

// A pick's message comes from the server's own read, never the wire. The path
// where that MATTERS is a squash target: its Orig composes the combined
// message (a plain pick's Orig is never read — git keeps the object's own
// message — so asserting on one would prove nothing).
func TestOpInteractiveRebaseSquashMessageIsNotWireControlled(t *testing.T) {
	useGGSequenceEditor(t)
	dir := irebaseRepo(t, 2)
	ts := serve(t, New(domain.Open(dir)))
	cs := rebaseRange(t, ts, "work", "main").Commits

	// w1 is the squash target and carries a message the client made up; w2
	// melds into it. Only the target's Orig reaches the composed message.
	plan := planJSON(
		[3]string{cs[0].Sha, "pick", "hijacked\n"},
		[3]string{cs[1].Sha, "squash", ""},
	)
	body := `{"op":"interactive-rebase","branch":"work","onto":"main","plan":` + plan + `}`
	events := readSSE(t, ts, startOpBody(t, ts, body), 60*time.Second)
	if done := events[len(events)-1]; done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	msg := gitRun(t, dir, "log", "-1", "--format=%B", "work")
	if strings.Contains(msg, "hijacked") {
		t.Errorf("the wire rewrote a message it never showed as a reword:\n%s", msg)
	}
	if !strings.Contains(msg, "w1") || !strings.Contains(msg, "w2") {
		t.Errorf("composed message lost its real content:\n%s", msg)
	}
}
