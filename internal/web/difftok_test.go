package web

import (
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// bigGo is a Go source over domain.InlineSyntaxBytes: a deferred diff
// answers it plain and the colours by a second request.
func bigGo(extra string) string {
	return "package p\n\n" + extra + strings.Repeat("func f() int { return 1 }\n", 3000)
}

// tokRepo commits big.go then edits it (the commit form's pair), and
// returns the server and the commit's sha.
func tokRepo(t *testing.T) (dir string, ts *httptest.Server, sha string) {
	t.Helper()
	dir = t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	write(t, dir, "big.go", bigGo(""))
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "one")
	write(t, dir, "big.go", bigGo("var x = 2\n"))
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "two")
	sha = gitRun(t, dir, "rev-parse", "HEAD")
	return dir, serve(t, New(domain.Open(dir))), sha
}

type tokWire struct {
	OldSum string  `json:"old_sum"`
	NewSum string  `json:"new_sum"`
	Old    [][]any `json:"old"`
	New    [][]any `json:"new"`
}

type diffWire struct {
	TokPending bool             `json:"tok_pending"`
	OldSum     string           `json:"old_sum"`
	NewSum     string           `json:"new_sum"`
	Rows       []map[string]any `json:"rows"`
}

func noRowRuns(t *testing.T, d diffWire) {
	t.Helper()
	for i, r := range d.Rows {
		if r["left_tok"] != nil || r["right_tok"] != nil {
			t.Fatalf("row %d carries runs in a deferred answer: %v", i, r)
		}
	}
}

// checkTwoHalves asks base (a /api/diff or /api/entry-diff URL) deferred,
// then for the colours, and checks they agree with the one-shot answer.
func checkTwoHalves(t *testing.T, ts *httptest.Server, base string) {
	t.Helper()
	var def diffWire
	if code := getJSON(t, ts, base+"&defer_tok=1", &def); code != 200 {
		t.Fatalf("%s defer: code %d", base, code)
	}
	if !def.TokPending || len(def.Rows) == 0 || def.OldSum == "" || def.NewSum == "" {
		t.Fatalf("%s defer: pending=%v rows=%d sums %q/%q", base, def.TokPending, len(def.Rows), def.OldSum, def.NewSum)
	}
	noRowRuns(t, def)
	var tk tokWire
	if code := getJSON(t, ts, base+"&tok=1", &tk); code != 200 {
		t.Fatalf("%s tok: code %d", base, code)
	}
	if tk.OldSum != def.OldSum || tk.NewSum != def.NewSum {
		t.Fatalf("%s: tok sums %q/%q != diff sums %q/%q", base, tk.OldSum, tk.NewSum, def.OldSum, def.NewSum)
	}
	var full diffWire
	if code := getJSON(t, ts, base, &full); code != 200 {
		t.Fatalf("%s: code %d", base, code)
	}
	if full.TokPending {
		t.Fatalf("%s: the flagless answer must never be pending", base)
	}
	colored := 0
	for _, r := range full.Rows {
		if no, ok := r["right_no"].(float64); ok && no > 0 {
			var want any = r["right_tok"]
			got := any(nil)
			if line := tk.New[int(no)-1]; line != nil {
				got = any(line)
			}
			if !reflect.DeepEqual(normalize(got), normalize(want)) {
				t.Fatalf("%s: line %v runs %v != inline %v", base, no, got, want)
			}
			if want != nil {
				colored++
			}
		}
	}
	if colored == 0 {
		t.Fatalf("%s: no coloured line at all — the comparison proved nothing", base)
	}
}

// normalize makes []any and [][]any comparable after a JSON round trip.
func normalize(v any) any {
	if v == nil {
		return nil
	}
	switch x := v.(type) {
	case []any:
		if len(x) == 0 {
			return nil
		}
		out := make([]any, len(x))
		for i := range x {
			out[i] = normalize(x[i])
		}
		return out
	}
	return v
}

func TestDiffDeferTokCommitForm(t *testing.T) {
	t.Parallel()
	_, ts, sha := tokRepo(t)
	checkTwoHalves(t, ts, "/api/diff?sha="+sha+"&path=big.go&status=M")
}

func TestDiffTokOnlyRenameForm(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	write(t, dir, "a.go", bigGo(""))
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "one")
	gitRun(t, dir, "mv", "a.go", "a.py")
	gitRun(t, dir, "commit", "-m", "rename")
	sha := gitRun(t, dir, "rev-parse", "HEAD")
	ts := serve(t, New(domain.Open(dir)))
	var tk tokWire
	if code := getJSON(t, ts, "/api/diff?sha="+sha+"&path=a.py&old=a.go&status=R&tok=1", &tk); code != 200 {
		t.Fatalf("code %d", code)
	}
	if len(tk.Old) == 0 || tk.Old[0] == nil {
		t.Fatalf("the old side (a.go) must be lexed with Go's grammar: %v", tk.Old)
	}
}

func TestEntryDiffTokOnlyRenameLexesOldName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	write(t, dir, "a.go", bigGo(""))
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "one")
	gitRun(t, dir, "mv", "a.go", "a.py")
	gitRun(t, dir, "commit", "-m", "rename")
	sha := gitRun(t, dir, "rev-parse", "HEAD")
	parent := gitRun(t, dir, "rev-parse", "HEAD^")
	ts := serve(t, New(domain.Open(dir)))
	var tk tokWire
	q := "/api/entry-diff?left=commit:" + parent + "&right=commit:" + sha + "&path=a.py&old_path=a.go&status=R&tok=1"
	if code := getJSON(t, ts, q, &tk); code != 200 {
		t.Fatalf("code %d", code)
	}
	if len(tk.Old) == 0 || len(tk.Old[0]) == 0 || tk.Old[0][0].([]any)[2] != "kw" {
		t.Fatal("the old side (a.go) must be lexed with Go's grammar: its `package` is no keyword")
	}
}

// Each form on its own server: the colours are stored by content, so a
// second form over the same pair would (rightly) answer them inline.
func TestDiffDeferTokRevAndEntryForms(t *testing.T) {
	t.Parallel()
	dir, ts, sha := tokRepo(t)
	parent := gitRun(t, dir, "rev-parse", "HEAD^")
	checkTwoHalves(t, ts, "/api/diff?left="+parent+"&right="+sha+"&path=big.go&status=M")
	dir, ts, sha = tokRepo(t)
	parent = gitRun(t, dir, "rev-parse", "HEAD^")
	checkTwoHalves(t, ts, "/api/entry-diff?left=commit:"+parent+"&right=commit:"+sha+"&path=big.go&status=M")
}

func TestDiffTokOnlyWorktreeSumsMove(t *testing.T) {
	t.Parallel()
	dir, ts, _ := tokRepo(t)
	write(t, dir, "big.go", bigGo("var y = 3\n"))
	var def diffWire
	if code := getJSON(t, ts, "/api/diff?wt=unstaged&path=big.go&defer_tok=1", &def); code != 200 {
		t.Fatalf("code %d", code)
	}
	if !def.TokPending {
		t.Fatal("a large working-tree diff should be pending")
	}
	write(t, dir, "big.go", bigGo("var y = 4\n"))
	var tk tokWire
	if code := getJSON(t, ts, "/api/diff?wt=unstaged&path=big.go&tok=1", &tk); code != 200 {
		t.Fatalf("code %d", code)
	}
	if tk.NewSum == def.NewSum || tk.OldSum != def.OldSum {
		t.Fatalf("sums new %q→%q old %q→%q: the edited side's sum must move, the index side's not", def.NewSum, tk.NewSum, def.OldSum, tk.OldSum)
	}
}

func TestDiffNoFlagUnchanged(t *testing.T) {
	t.Parallel()
	_, ts, sha := tokRepo(t)
	var full diffWire
	if code := getJSON(t, ts, "/api/diff?sha="+sha+"&path=big.go&status=M", &full); code != 200 {
		t.Fatalf("code %d", code)
	}
	if full.TokPending {
		t.Fatal("no flag: never pending")
	}
	for _, r := range full.Rows {
		if r["right_tok"] != nil {
			return
		}
	}
	t.Fatal("no flag: the runs ride inline, as before")
}
