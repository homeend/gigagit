package web

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/observ"
)

// Staging speed is git PROCESS count: on WSL's /mnt drives every git call
// costs 50–100 ms, whatever it does. These tests pin the counts.

type subCounter struct {
	gitexec.Runner
	mu sync.Mutex
	n  map[string]int
}

func (c *subCounter) note(argv []string) {
	sub := ""
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		if a == "-c" || a == "-C" {
			i++
			continue
		}
		if !strings.HasPrefix(a, "-") {
			sub = a
			break
		}
	}
	c.mu.Lock()
	c.n[sub]++
	c.mu.Unlock()
}

func (c *subCounter) Run(ctx context.Context, name string, argv []string) (gitexec.Result, error) {
	c.note(argv)
	return c.Runner.Run(ctx, name, argv)
}

func (c *subCounter) RunEnv(ctx context.Context, name string, argv, env []string) (gitexec.Result, error) {
	c.note(argv)
	return c.Runner.RunEnv(ctx, name, argv, env)
}

func (c *subCounter) Stream(ctx context.Context, name string, argv []string, onLine func(string)) (gitexec.Result, error) {
	c.note(argv)
	return c.Runner.Stream(ctx, name, argv, onLine)
}

func (c *subCounter) take() map[string]int {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := c.n
	c.n = map[string]int{}
	return out
}

func twoFileRepo(t *testing.T) (string, *subCounter, string) {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q", "-b", "main")
	gitRun(t, dir, "config", "user.email", "t@t")
	gitRun(t, dir, "config", "user.name", "t")
	lines := func(f string, edit bool) string {
		var b strings.Builder
		for i := 1; i <= 12; i++ {
			l := f + " line " + string(rune('a'+i))
			if edit && (i == 2 || i == 10) {
				l = "EDIT " + l
			}
			b.WriteString(l + "\n")
		}
		return b.String()
	}
	for _, f := range []string{"a", "b"} {
		if err := os.WriteFile(filepath.Join(dir, f+".txt"), []byte(lines(f, false)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-qm", "base")
	for _, f := range []string{"a", "b"} {
		if err := os.WriteFile(filepath.Join(dir, f+".txt"), []byte(lines(f, true)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c := &subCounter{Runner: gitexec.NewExecRunner("git", dir, observ.NewRing(50)), n: map[string]int{}}
	return dir, c, lines("", true)
}

type diffWithHunks struct {
	Rows  []json.RawMessage `json:"rows"`
	Hunks *struct {
		Hash string `json:"hash"`
	} `json:"hunks"`
}

// One /api/diff read of a working-tree file reads each side ONCE, for the
// alignment and the staging doc alike.
func TestWorktreeDiffReadsTheIndexOnce(t *testing.T) {
	t.Parallel()
	_, c, _ := twoFileRepo(t)
	ts := serve(t, New(domain.New(&git.Repo{Runner: c})))
	c.take()
	var d diffWithHunks
	if code := getJSON(t, ts, "/api/diff?wt=unstaged&path=a.txt", &d); code != http.StatusOK || d.Hunks == nil {
		t.Fatalf("diff: code %d, hunks %v", code, d.Hunks)
	}
	if n := c.take()["show"]; n != 1 {
		t.Fatalf("one diff read ran git show %d times, want 1", n)
	}
}

// A selection spanning files is staged in ONE request: one index write, and
// the answer carries each file's fresh diff — identical to what a re-read
// would return — so the rows are live again without a second round trip.
// The batch answer carries NO status: on a slow filesystem git status is the
// costliest call, and the client reads it in the background.
func TestStageHunksBatchStagesEveryFileAndAnswersTheirDiffs(t *testing.T) {
	t.Parallel()
	dir, c, _ := twoFileRepo(t)
	ts := serve(t, New(domain.New(&git.Repo{Runner: c})))
	var a, b diffWithHunks
	getJSON(t, ts, "/api/diff?wt=unstaged&path=a.txt", &a)
	getJSON(t, ts, "/api/diff?wt=unstaged&path=b.txt", &b)
	if a.Hunks == nil || b.Hunks == nil {
		t.Fatal("fixture: both files must be tagged")
	}

	stale := `{"files":[{"path":"a.txt","hash":"` + a.Hunks.Hash + `","blocks":[{"block":0,"whole":true}]},` +
		`{"path":"b.txt","hash":"nope","blocks":[{"block":1,"rows":[0]}]}]}`
	if code := postJSON(t, ts, "/api/stage-hunks", stale, "application/json", "", nil); code != http.StatusConflict {
		t.Fatalf("a stale file in the batch: code %d, want 409", code)
	}
	if strings.Contains(gitRun(t, dir, "show", ":a.txt"), "EDIT") {
		t.Fatal("a refused batch must stage nothing")
	}

	c.take()
	body := `{"files":[{"path":"a.txt","hash":"` + a.Hunks.Hash + `","blocks":[{"block":0,"whole":true}]},` +
		`{"path":"b.txt","hash":"` + b.Hunks.Hash + `","blocks":[{"block":1,"rows":[0]}]}]}`
	var resp struct {
		Diffs []struct {
			Path string        `json:"path"`
			Lane string        `json:"lane"`
			Diff diffWithHunks `json:"diff"`
		} `json:"diffs"`
	}
	if code := postJSON(t, ts, "/api/stage-hunks", body, "application/json", "", &resp); code != http.StatusOK {
		t.Fatalf("batch stage: code %d", code)
	}
	calls := c.take()
	ia, ib := gitRun(t, dir, "show", ":a.txt"), gitRun(t, dir, "show", ":b.txt")
	if !strings.Contains(ia, "EDIT a line c") || strings.Contains(ia, "EDIT a line k") {
		t.Fatalf("a.txt index = %q: want its first hunk only", ia)
	}
	if strings.Contains(ib, "EDIT b line c") || !strings.Contains(ib, "EDIT b line k") {
		t.Fatalf("b.txt index = %q: want its second hunk only", ib)
	}
	if calls["update-index"] != 1 || calls["ls-files"] != 1 || calls["status"] != 0 {
		t.Fatalf("git calls = %v: want ONE ls-files and update-index, and no status, for the batch", calls)
	}
	if calls["for-each-ref"] != 0 {
		t.Fatalf("git calls = %v: staging must not probe branch versions", calls)
	}
	if len(resp.Diffs) != 2 {
		t.Fatalf("the answer carries %d diffs, want 2", len(resp.Diffs))
	}
	for _, got := range resp.Diffs {
		var fresh diffWithHunks
		getJSON(t, ts, "/api/diff?wt=unstaged&path="+got.Path, &fresh)
		if got.Lane != "unstaged" || got.Diff.Hunks == nil || fresh.Hunks == nil || got.Diff.Hunks.Hash != fresh.Hunks.Hash {
			t.Fatalf("%s: answered diff (lane %q, hunks %v) is not what a re-read gives (%v)", got.Path, got.Lane, got.Diff.Hunks, fresh.Hunks)
		}
		gr, _ := json.Marshal(got.Diff.Rows)
		fr, _ := json.Marshal(fresh.Rows)
		if string(gr) != string(fr) {
			t.Fatalf("%s: answered rows differ from a re-read", got.Path)
		}
	}
}
