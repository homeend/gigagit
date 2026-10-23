package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

// postShas posts {"shas":[…]} plus any extra JSON fields to path and returns
// the status and the decoded body (op_id on success, error fields on refusal).
func postShas(t *testing.T, ts *httptest.Server, path, extra string, shas ...string) (int, map[string]any) {
	t.Helper()
	quoted := make([]string, len(shas))
	for i, s := range shas {
		quoted[i] = `"` + s + `"`
	}
	body := `{"shas":[` + strings.Join(quoted, ",") + `]` + extra + `}`
	return postJSONAny(t, ts, path, body)
}

func waitOK(t *testing.T, ts *httptest.Server, body map[string]any) {
	t.Helper()
	id, _ := body["op_id"].(string)
	if id == "" {
		t.Fatalf("no op_id in %v", body)
	}
	events := readSSE(t, ts, id, 60*time.Second)
	if done := events[len(events)-1]; done["ok"] != true {
		t.Fatalf("op did not succeed: done = %v", done)
	}
}

// Drop N marked commits: one interactive rebase, no adjacency needed — the
// TUI's commitDropSelectionRow.
func TestDropMarkedCommits(t *testing.T) {
	t.Parallel()
	useGGSequenceEditor(t)
	dir := editRepo(t, 4)
	ts := serve(t, New(domain.Open(dir)))
	code, body := postShas(t, ts, "/api/commit-drop", "", shaOf(t, dir, "c4"), shaOf(t, dir, "c2"))
	if code != http.StatusAccepted {
		t.Fatalf("drop status = %d (%v), want 202", code, body)
	}
	waitOK(t, ts, body)
	if got := subjects(t, dir); strings.Join(got, " ") != "c3 c1" {
		t.Fatalf("subjects = %v, want [c3 c1]", got)
	}
	for _, gone := range []string{"b.txt", "d.txt"} {
		if _, err := os.Stat(filepath.Join(dir, gone)); err == nil {
			t.Errorf("%s survived its commit's drop", gone)
		}
	}
}

func TestDropRefusals(t *testing.T) {
	t.Parallel()
	useGGSequenceEditor(t)
	dir := editRepo(t, 4)
	ts := serve(t, New(domain.Open(dir)))
	gitRun(t, dir, "checkout", "-q", "-b", "other", "HEAD~3")
	writeFile(t, dir, "z.txt", "z\n")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-q", "-m", "off")
	offBranch := gitRun(t, dir, "rev-parse", "HEAD")
	gitRun(t, dir, "checkout", "-q", "main")
	for _, tc := range []struct {
		name string
		shas []string
		want int
	}{
		{"one commit is the single-commit row's job", []string{shaOf(t, dir, "c3")}, http.StatusUnprocessableEntity},
		{"a duplicate does not make two", []string{shaOf(t, dir, "c3"), shaOf(t, dir, "c3")}, http.StatusUnprocessableEntity},
		{"not a commit id", []string{shaOf(t, dir, "c3"), "main"}, http.StatusBadRequest},
		{"not on this branch", []string{shaOf(t, dir, "c3"), offBranch}, http.StatusUnprocessableEntity},
		{"the root has no parent to rebase onto", []string{shaOf(t, dir, "c2"), shaOf(t, dir, "c1")}, http.StatusUnprocessableEntity},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if code, body := postShas(t, ts, "/api/commit-drop", "", tc.shas...); code != tc.want {
				t.Fatalf("status = %d (%v), want %d", code, body, tc.want)
			}
			if s := subjects(t, dir); len(s) != 4 {
				t.Fatalf("history changed on a refusal: %v", s)
			}
		})
	}
}

// A non-adjacent squash is refused with a flag the page asks about, and the
// same marks with reorder:true squash them — the skipped commit moves to just
// after the squash (rebaseplan.BuildSquashReorder).
func TestSquashReorder(t *testing.T) {
	t.Parallel()
	useGGSequenceEditor(t)
	dir := editRepo(t, 4)
	ts := serve(t, New(domain.Open(dir)))
	c4, c2 := shaOf(t, dir, "c4"), shaOf(t, dir, "c2")
	code, body := postShas(t, ts, "/api/commit-squash", "", c4, c2)
	if code != http.StatusUnprocessableEntity || body["not_adjacent"] != true {
		t.Fatalf("non-adjacent squash = %d %v, want 422 with not_adjacent", code, body)
	}
	if s := subjects(t, dir); len(s) != 4 {
		t.Fatalf("history changed on the refusal: %v", s)
	}
	code, body = postShas(t, ts, "/api/commit-squash", `,"reorder":true`, c4, c2)
	if code != http.StatusAccepted {
		t.Fatalf("reorder squash status = %d (%v), want 202", code, body)
	}
	waitOK(t, ts, body)
	got := subjects(t, dir)
	if len(got) != 3 || got[2] != "c1" {
		t.Fatalf("subjects = %v, want 3 commits over c1", got)
	}
	msgs := gitRun(t, dir, "log", "--format=%B")
	if !strings.Contains(msgs, "c2") || !strings.Contains(msgs, "c4") || !strings.Contains(msgs, "c3") {
		t.Fatalf("a message went missing: %q", msgs)
	}
	for _, name := range []string{"a.txt", "b.txt", "c.txt", "d.txt"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s missing after the reorder squash: %v", name, err)
		}
	}
}

// pickRepo: main has c1; topic adds p1, p2, p3 on top of c1.
func pickRepo(t *testing.T) (dir string, p1, p2, p3 string) {
	t.Helper()
	dir = editRepo(t, 1)
	gitRun(t, dir, "checkout", "-q", "-b", "topic")
	for _, n := range []string{"p1", "p2", "p3"} {
		writeFile(t, dir, n+".txt", n+"\n")
		gitRun(t, dir, "add", "-A")
		gitRun(t, dir, "commit", "-q", "-m", n)
	}
	gitRun(t, dir, "checkout", "-q", "main")
	rev := func(r string) string { return strings.TrimSpace(gitRun(t, dir, "rev-parse", r)) }
	return dir, rev("topic~2"), rev("topic~1"), rev("topic")
}

// Cherry-pick the marked commits: one sequencer run, oldest first — the
// marks arrive newest first, the feed's order.
func TestCherryPickMarked(t *testing.T) {
	t.Parallel()
	dir, p1, _, p3 := pickRepo(t)
	ts := serve(t, New(domain.Open(dir)))
	code, body := postShas(t, ts, "/api/commit-cherry-pick", "", p3, p1)
	if code != http.StatusAccepted {
		t.Fatalf("cherry-pick status = %d (%v), want 202", code, body)
	}
	waitOK(t, ts, body)
	if got := subjects(t, dir); strings.Join(got, " ") != "p3 p1 c1" {
		t.Fatalf("subjects = %v, want [p3 p1 c1] (oldest picked first)", got)
	}
}

func TestCherryPickMarkedRefusals(t *testing.T) {
	t.Parallel()
	dir, p1, p2, _ := pickRepo(t)
	gitRun(t, dir, "merge", "-q", "--no-ff", "-m", "merge topic", "topic")
	merge := gitRun(t, dir, "rev-parse", "HEAD")
	gitRun(t, dir, "reset", "-q", "--hard", "HEAD~1")
	ts := serve(t, New(domain.Open(dir)))
	if code, _ := postShas(t, ts, "/api/commit-cherry-pick", "", p2, "main"); code != http.StatusBadRequest {
		t.Fatalf("non-hex status = %d, want 400", code)
	}
	if code, _ := postShas(t, ts, "/api/commit-cherry-pick", ""); code != http.StatusUnprocessableEntity {
		t.Fatalf("empty status = %d, want 422", code)
	}
	code, body := postShas(t, ts, "/api/commit-cherry-pick", "", merge, p1)
	if code != http.StatusUnprocessableEntity || !strings.Contains(body["error"].(string), merge[:7]) {
		t.Fatalf("merge pick = %d %v, want 422 naming %s", code, body, merge[:7])
	}
	if s := subjects(t, dir); strings.Join(s, " ") != "c1" {
		t.Fatalf("history changed on a refusal: %v", s)
	}
}

// Every new mutation sits behind the write guard.
func TestCommitMarksWriteGuard(t *testing.T) {
	t.Parallel()
	dir := editRepo(t, 3)
	ts := serve(t, New(domain.Open(dir)))
	body := `{"shas":["` + shaOf(t, dir, "c3") + `","` + shaOf(t, dir, "c2") + `"]}`
	for _, path := range []string{"/api/commit-drop", "/api/commit-cherry-pick"} {
		if code := postJSON(t, ts, path, body, "application/json", "http://evil.example", nil); code != http.StatusForbidden {
			t.Fatalf("%s cross-origin status = %d, want 403", path, code)
		}
		if code := postJSON(t, ts, path, body, "text/plain", "", nil); code != http.StatusUnsupportedMediaType {
			t.Fatalf("%s bad content-type status = %d, want 415", path, code)
		}
	}
}

// Review marked range: exactly what Compare selection shows — older..newer for
// two marks, oldest^..newest for three or more — labelled from the marks.
func TestReviewMarkedRange(t *testing.T) {
	t.Parallel()
	dir := editRepo(t, 4)
	ts := serve(t, New(domain.Open(dir)))
	c1, c2, c3, c4 := shaOf(t, dir, "c1"), shaOf(t, dir, "c2"), shaOf(t, dir, "c3"), shaOf(t, dir, "c4")
	two := reviewTools(t, ts, "?target=range&shas="+c4+","+c2)
	if two.Range != c2+".."+c4 {
		t.Errorf("2-mark range = %q, want %s..%s", two.Range, c2, c4)
	}
	if want := c2[:7] + ".." + c4[:7] + " — c4"; two.Label != want {
		t.Errorf("2-mark label = %q, want %q", two.Label, want)
	}
	three := reviewTools(t, ts, "?target=range&shas="+c4+","+c3+","+c2)
	if three.Range != c1+".."+c4 {
		t.Errorf("3-mark range = %q, want %s..%s (oldest's parent)", three.Range, c1, c4)
	}
	for _, bad := range []string{
		"?target=range&shas=" + c2,                       // one mark is not a range
		"?target=range&shas=" + c3 + ",main",             // hex only
		"?target=range&shas=" + c3 + "," + c2 + "," + c1, // 3+ from the root
	} {
		var body map[string]any
		if code := getJSON(t, ts, "/api/review/tools"+bad, &body); code != http.StatusBadRequest {
			t.Errorf("GET %s = %d, want 400", bad, code)
		}
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
