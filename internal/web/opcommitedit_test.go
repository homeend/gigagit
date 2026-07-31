package web

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

// useGGSequenceEditor points execPath at a real gg binary for the duration of
// the test: os.Executable() under `go test` is the test binary, which cannot
// serve as a rebase sequence editor (it would re-run the suite). Mirrors
// internal/engine's buildGG, but built ONCE for the whole package — this is
// already one of the slowest packages under -race, and three separate links
// were enough to push it past the 10-minute timeout on a loaded machine.
func useGGSequenceEditor(t *testing.T) {
	t.Helper()
	ggBinOnce.Do(func() {
		dir, err := os.MkdirTemp("", "gg-seq-editor")
		if err != nil {
			ggBinErr = err
			return
		}
		ggBinDir = dir
		bin := filepath.Join(dir, "gg-test-bin")
		if out, berr := exec.Command("go", "build", "-o", bin, "github.com/homeend/gigagit/cmd/gg").CombinedOutput(); berr != nil {
			ggBinErr = fmt.Errorf("build gg: %v\n%s", berr, out)
			return
		}
		ggBinPath = bin
	})
	if ggBinErr != nil {
		t.Fatal(ggBinErr)
	}
	prev := execPath
	execPath = func() (string, error) { return ggBinPath, nil }
	t.Cleanup(func() { execPath = prev })
}

var (
	ggBinOnce sync.Once
	ggBinPath string
	ggBinDir  string
	ggBinErr  error
)

// TestMain removes the shared binary after the package's tests: it outlives
// any single test's t.TempDir(), so it needs an owner of its own.
func TestMain(m *testing.M) {
	code := m.Run()
	if ggBinDir != "" {
		_ = os.RemoveAll(ggBinDir)
	}
	os.Exit(code)
}

// editRepo builds main with one commit per file (a.txt, b.txt, …) so a single
// commit can be dropped or moved without any two commits touching the same
// file — a rebase conflict would otherwise be the fixture's fault, not the
// code's.
func editRepo(t *testing.T, n int) string {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("%c.txt", 'a'+i)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		gitRun(t, dir, "add", "-A")
		gitRun(t, dir, "commit", "-m", "c"+fmt.Sprint(i+1))
	}
	return dir
}

// shaOf returns the full hash of the commit with the given subject.
func shaOf(t *testing.T, dir, subject string) string {
	t.Helper()
	out := gitRun(t, dir, "log", "--format=%H %s")
	for _, line := range strings.Split(out, "\n") {
		h, s, ok := strings.Cut(line, " ")
		if ok && s == subject {
			return h
		}
	}
	t.Fatalf("no commit with subject %q in:\n%s", subject, out)
	return ""
}

func subjects(t *testing.T, dir string) []string {
	t.Helper()
	out := gitRun(t, dir, "log", "--format=%s")
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

func TestOpCommitEditDrop(t *testing.T) {
	useGGSequenceEditor(t)
	dir := editRepo(t, 3) // c1 a.txt, c2 b.txt, c3 c.txt
	ts := serve(t, New(domain.Open(dir)))

	c2 := shaOf(t, dir, "c2")
	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"commit-edit","sha":"`+c2+`","edit":"drop"}`), 60*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if got := subjects(t, dir); strings.Join(got, ",") != "c3,c1" {
		t.Errorf("log = %v, want [c3 c1]", got)
	}
	// The dropped commit's file is gone; the later commit's survives.
	if _, err := os.Stat(filepath.Join(dir, "b.txt")); !os.IsNotExist(err) {
		t.Errorf("b.txt still present after dropping c2: err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "c.txt")); err != nil {
		t.Errorf("c.txt missing after dropping c2: %v", err)
	}
}

func TestOpCommitEditMoveUp(t *testing.T) {
	useGGSequenceEditor(t)
	dir := editRepo(t, 3)
	ts := serve(t, New(domain.Open(dir)))

	c2 := shaOf(t, dir, "c2")
	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"commit-edit","sha":"`+c2+`","edit":"move-up"}`), 60*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	// move-up = towards newer: c2 swaps with c3, so newest-first reads c2,c3,c1.
	if got := subjects(t, dir); strings.Join(got, ",") != "c2,c3,c1" {
		t.Errorf("log = %v, want [c2 c3 c1]", got)
	}
}

func TestOpCommitEditMoveDown(t *testing.T) {
	useGGSequenceEditor(t)
	dir := editRepo(t, 3)
	ts := serve(t, New(domain.Open(dir)))

	c3 := shaOf(t, dir, "c3")
	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"commit-edit","sha":"`+c3+`","edit":"move-down"}`), 60*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if got := subjects(t, dir); strings.Join(got, ",") != "c2,c3,c1" {
		t.Errorf("log = %v, want [c2 c3 c1]", got)
	}
}

// A commit that is not on the checked-out branch is refused BEFORE any rebase
// starts — the branch must be left exactly as it was.
func TestOpCommitEditNotOnBranch(t *testing.T) {
	dir := editRepo(t, 2)
	gitRun(t, dir, "checkout", "-b", "side")
	if err := os.WriteFile(filepath.Join(dir, "side.txt"), []byte("side\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "side-only")
	sideSha := shaOf(t, dir, "side-only")
	gitRun(t, dir, "checkout", "main")
	before := gitRun(t, dir, "rev-parse", "main")

	ts := serve(t, New(domain.Open(dir)))
	code, out := postJSONRaw(t, ts, "/api/op", `{"op":"commit-edit","sha":"`+sideSha+`","edit":"drop"}`)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("code = %d, want 422 (body %v)", code, out)
	}
	if after := gitRun(t, dir, "rev-parse", "main"); after != before {
		t.Errorf("main moved: %s -> %s", before, after)
	}
}

// The oldest commit in the repo has no parent to rebase onto: refused, not
// half-run.
func TestOpCommitEditRootCommit(t *testing.T) {
	dir := editRepo(t, 2)
	ts := serve(t, New(domain.Open(dir)))

	c1 := shaOf(t, dir, "c1")
	before := gitRun(t, dir, "rev-parse", "main")
	code, out := postJSONRaw(t, ts, "/api/op", `{"op":"commit-edit","sha":"`+c1+`","edit":"drop"}`)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("code = %d, want 422 (body %v)", code, out)
	}
	if after := gitRun(t, dir, "rev-parse", "main"); after != before {
		t.Errorf("main moved: %s -> %s", before, after)
	}
}

func TestOpCommitEditDetachedHEAD(t *testing.T) {
	dir := editRepo(t, 3)
	c2 := shaOf(t, dir, "c2")
	gitRun(t, dir, "checkout", "--detach", "HEAD")
	ts := serve(t, New(domain.Open(dir)))

	code, out := postJSONRaw(t, ts, "/api/op", `{"op":"commit-edit","sha":"`+c2+`","edit":"drop"}`)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("code = %d, want 422 (body %v)", code, out)
	}
	if !strings.Contains(out["error"], "branch") {
		t.Errorf("error = %v, want it to name the missing branch", out)
	}
}

// The sha reaches git argv as `<sha>~1`, so it is required to be a plain hex
// object name — not merely "does not start with a dash". A rev expression
// would otherwise let a client pick the rebase base itself.
func TestOpCommitEditRejectsNonHexAndBadEdit(t *testing.T) {
	dir := editRepo(t, 3)
	ts := serve(t, New(domain.Open(dir)))
	c2 := shaOf(t, dir, "c2")

	for _, body := range []string{
		`{"op":"commit-edit","edit":"drop"}`,
		`{"op":"commit-edit","sha":"HEAD","edit":"drop"}`,
		`{"op":"commit-edit","sha":"main~2","edit":"drop"}`,
		`{"op":"commit-edit","sha":"--exec=id","edit":"drop"}`,
		`{"op":"commit-edit","sha":"` + c2 + `","edit":""}`,
		`{"op":"commit-edit","sha":"` + c2 + `","edit":"squash"}`,
	} {
		if code := postJSON(t, ts, "/api/op", body, "application/json", "", nil); code != http.StatusBadRequest {
			t.Errorf("body %s: code = %d, want 400", body, code)
		}
	}
}

// The feed rows carry a parent count so the client can keep the history-edit
// rows off a merge commit — the TUI's own gate.
func TestCommitRowsCarryParentCount(t *testing.T) {
	dir := editRepo(t, 2)
	gitRun(t, dir, "checkout", "-b", "side", "HEAD~1")
	if err := os.WriteFile(filepath.Join(dir, "side.txt"), []byte("side\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "side")
	gitRun(t, dir, "checkout", "main")
	gitRun(t, dir, "merge", "--no-ff", "-m", "merge side", "side")

	ts := serve(t, New(domain.Open(dir)))
	var out struct {
		Rows []struct {
			Subject string `json:"subject"`
			Parents int    `json:"parents"`
		} `json:"rows"`
	}
	if code := getJSON(t, ts, "/api/commits", &out); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	want := map[string]int{"merge side": 2, "c1": 0, "c2": 1, "side": 1}
	seen := 0
	for _, row := range out.Rows {
		if n, ok := want[row.Subject]; ok {
			seen++
			if row.Parents != n {
				t.Errorf("%q parents = %d, want %d", row.Subject, row.Parents, n)
			}
		}
	}
	if seen != len(want) {
		t.Errorf("saw %d of %d expected rows in %v", seen, len(want), out.Rows)
	}
}
