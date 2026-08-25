package web

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

// conflictRepo builds a repo paused mid-merge with n conflicted files, each
// modified on BOTH sides (a UU conflict — the class the whole-file
// ours/theirs picks apply to). The branch is named "feature" so the shared
// conflictedMergeState (conflict_test.go) can drive the merge.
func conflictRepo(t *testing.T, n int) string {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	for i := 1; i <= n; i++ {
		write(t, dir, fmt.Sprintf("c%d.txt", i), "base\n")
	}
	write(t, dir, "plain.txt", "neither side touches this\n")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "base")

	gitRun(t, dir, "checkout", "-b", "feature")
	for i := 1; i <= n; i++ {
		write(t, dir, fmt.Sprintf("c%d.txt", i), "theirs\n")
	}
	gitRun(t, dir, "commit", "-am", "feature edit")

	gitRun(t, dir, "checkout", "main")
	for i := 1; i <= n; i++ {
		write(t, dir, fmt.Sprintf("c%d.txt", i), "ours\n")
	}
	gitRun(t, dir, "commit", "-am", "main edit")

	conflictedMergeState(t, dir)
	return dir
}

// modifyDeleteRepo builds a repo paused mid-merge on ONE modify/delete
// conflict: main keeps and edits md.txt, feature deletes it (a UD conflict).
func modifyDeleteRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	write(t, dir, "md.txt", "base\n")
	write(t, dir, "keep.txt", "keep\n")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "base")

	gitRun(t, dir, "checkout", "-b", "feature")
	gitRun(t, dir, "rm", "-q", "md.txt")
	gitRun(t, dir, "commit", "-m", "feature deletes md")

	gitRun(t, dir, "checkout", "main")
	write(t, dir, "md.txt", "ours\n")
	gitRun(t, dir, "commit", "-am", "main edits md")

	conflictedMergeState(t, dir)
	return dir
}

// statusOf reads the working-tree status through the same service the
// handlers use, so assertions see exactly what the server would.
func statusOf(t *testing.T, dir string) model.WorkingTreeStatus {
	t.Helper()
	st, err := domain.Open(dir).Status(t.Context())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	return st
}

func unmergedPaths(st model.WorkingTreeStatus) []string {
	var out []string
	for _, f := range st.Files {
		if f.Kind == model.KindUnmerged {
			out = append(out, f.Path)
		}
	}
	sort.Strings(out)
	return out
}

// fileStatus finds one path's row, or the zero value when the tree is clean
// for it. Used to assert what a resolve did NOT touch.
func fileStatus(st model.WorkingTreeStatus, path string) model.FileStatus {
	for _, f := range st.Files {
		if f.Path == path {
			return f
		}
	}
	return model.FileStatus{}
}

// indexBlob returns what is STAGED for path — `git show :path`. Content is
// the honest assertion for a whole-file resolve: keeping the side that
// happens to match HEAD leaves the file staged but with nothing to report in
// status, so a status-shaped assertion would silently pass on a no-op.
func indexBlob(t *testing.T, dir, path string) string {
	t.Helper()
	return gitRun(t, dir, "show", ":"+path)
}

// Mark-all is the destructive-sounding one: it stages every conflicted file
// AS IT STANDS. It must stage all of them — and nothing else.
func TestOpMarkAllResolvedStagesEveryConflictAndNothingElse(t *testing.T) {
	t.Parallel()
	dir := conflictRepo(t, 2)
	// Two bystanders the op must not touch: a tracked file edited in the
	// working tree, and an untracked one. "stage everything" would sweep both
	// into the coming merge commit.
	write(t, dir, "plain.txt", "edited after the merge\n")
	write(t, dir, "bystander.txt", "untracked\n")
	ts := serve(t, New(domain.Open(dir)))

	if got := unmergedPaths(statusOf(t, dir)); len(got) != 2 {
		t.Fatalf("fixture: unmerged = %v, want 2 files", got)
	}
	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"mark-all-resolved"}`), 30*time.Second)
	if done := events[len(events)-1]; done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	st := statusOf(t, dir)
	if got := unmergedPaths(st); len(got) != 0 {
		t.Errorf("still unmerged: %v", got)
	}
	// Staged AS THEY STAND: the working-tree bytes (markers included) are what
	// landed in the index.
	for _, p := range []string{"c1.txt", "c2.txt"} {
		want, err := os.ReadFile(filepath.Join(dir, p))
		if err != nil {
			t.Fatal(err)
		}
		if got := indexBlob(t, dir, p); got != strings.TrimSuffix(string(want), "\n") {
			t.Errorf("%s: index = %q, want the working-tree bytes %q", p, got, want)
		}
	}
	if f := fileStatus(st, "plain.txt"); f.Staged != '.' || f.Unstaged != 'M' {
		t.Errorf("plain.txt = %q%q, want an untouched unstaged edit (.M)", string(f.Staged), string(f.Unstaged))
	}
	if f := fileStatus(st, "bystander.txt"); f.Kind != model.KindUntracked {
		t.Errorf("bystander.txt kind = %v, want it left untracked", f.Kind)
	}
}

// With nothing conflicted the op is refused at build time — a 422 the client
// can show, not a "success" that staged nothing.
func TestOpMarkAllResolvedRefusesWhenClean(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))
	code, body := postJSONRaw(t, ts, "/api/op", `{"op":"mark-all-resolved"}`)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", code)
	}
	if !strings.Contains(body["error"], "conflicted") {
		t.Errorf("error = %q", body["error"])
	}
}

// Each whole-file action leaves the expected content behind AND stages it, so
// the conflict actually clears.
func TestOpResolveConflictWholeFileActions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		action string
		want   string
	}{
		{"ours", "ours\n"},
		{"theirs", "theirs\n"},
	} {
		t.Run(tc.action, func(t *testing.T) {
			dir := conflictRepo(t, 1)
			ts := serve(t, New(domain.Open(dir)))
			body := fmt.Sprintf(`{"op":"resolve-conflict","path":"c1.txt","mode":%q}`, tc.action)
			events := readSSE(t, ts, startOpBody(t, ts, body), 30*time.Second)
			if done := events[len(events)-1]; done["ok"] != true {
				t.Fatalf("done = %v", done)
			}
			got, err := os.ReadFile(filepath.Join(dir, "c1.txt"))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Errorf("working tree = %q, want %q", got, tc.want)
			}
			// …and it is STAGED, which is what actually clears the unmerged
			// index entry — the merge cannot be continued otherwise.
			if idx := indexBlob(t, dir, "c1.txt"); idx != strings.TrimSuffix(tc.want, "\n") {
				t.Errorf("index = %q, want %q", idx, tc.want)
			}
			if got := unmergedPaths(statusOf(t, dir)); len(got) != 0 {
				t.Errorf("still unmerged: %v", got)
			}
		})
	}
}

// "mark" is the both-sides "I fixed it in my editor" action: it stages the
// file untouched, markers and all.
func TestOpResolveConflictMarkStagesAsIs(t *testing.T) {
	t.Parallel()
	dir := conflictRepo(t, 1)
	write(t, dir, "c1.txt", "hand-merged\n")
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"resolve-conflict","path":"c1.txt","mode":"mark"}`), 30*time.Second)
	if done := events[len(events)-1]; done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	got, err := os.ReadFile(filepath.Join(dir, "c1.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hand-merged\n" {
		t.Errorf("content = %q, want the hand-edited bytes untouched", got)
	}
	if len(unmergedPaths(statusOf(t, dir))) != 0 {
		t.Error("file is still unmerged")
	}
}

// A modify/delete conflict offers EXACTLY its three actions — and none of the
// both-sides ones, which have no meaning when one side has no content.
func TestConflictActionsForModifyDelete(t *testing.T) {
	t.Parallel()
	dir := modifyDeleteRepo(t)
	st := statusOf(t, dir)
	var md model.FileStatus
	for _, f := range st.Files {
		if f.Path == "md.txt" {
			md = f
		}
	}
	if md.Kind != model.KindUnmerged {
		t.Fatalf("fixture: md.txt is %v, want unmerged", md.Kind)
	}
	got := conflictActionNames(md)
	want := []string{"keep", "delete", "base"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("actions = %v, want %v (code %s%s)", got, want, string(md.Staged), string(md.Unstaged))
	}
	// The side that HAS content is ours here (main modified, side deleted).
	if a, ok := conflictActionFor(md, "keep"); !ok || a != engine.KeepOurs {
		t.Errorf("keep -> %v, %v; want KeepOurs", a, ok)
	}
}

// A both-sides conflict offers exactly the other three.
func TestConflictActionsForBothSides(t *testing.T) {
	t.Parallel()
	dir := conflictRepo(t, 1)
	st := statusOf(t, dir)
	var f model.FileStatus
	for _, x := range st.Files {
		if x.Path == "c1.txt" {
			f = x
		}
	}
	got := conflictActionNames(f)
	want := []string{"ours", "theirs", "mark"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("actions = %v, want %v", got, want)
	}
}

// An action the class does not offer is refused with a 422 that says what IS
// offered — the row should never have been shown, and the server is the
// backstop that makes a stale page harmless.
func TestOpResolveConflictRefusesActionOutsideTheClass(t *testing.T) {
	t.Parallel()
	dir := modifyDeleteRepo(t)
	ts := serve(t, New(domain.Open(dir)))
	code, body := postJSONRaw(t, ts, "/api/op", `{"op":"resolve-conflict","path":"md.txt","mode":"theirs"}`)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", code)
	}
	if !strings.Contains(body["error"], "keep") {
		t.Errorf("error = %q, want it to name the actions that ARE offered", body["error"])
	}
}

// A path that is not conflicted (already resolved in another tab) is a 422;
// one git has never heard of is a 404. Neither may resolve anything.
func TestOpResolveConflictPathGuards(t *testing.T) {
	t.Parallel()
	dir := conflictRepo(t, 1)
	write(t, dir, "plain.txt", "x\n")
	ts := serve(t, New(domain.Open(dir)))

	code, _ := postJSONRaw(t, ts, "/api/op", `{"op":"resolve-conflict","path":"plain.txt","mode":"ours"}`)
	if code != http.StatusUnprocessableEntity {
		t.Errorf("untracked path: status = %d, want 422", code)
	}
	code, _ = postJSONRaw(t, ts, "/api/op", `{"op":"resolve-conflict","path":"nope.txt","mode":"ours"}`)
	if code != http.StatusNotFound {
		t.Errorf("unknown path: status = %d, want 404", code)
	}
	code, _ = postJSONRaw(t, ts, "/api/op", `{"op":"resolve-conflict","path":"--upload-pack=x","mode":"ours"}`)
	if code != http.StatusBadRequest {
		t.Errorf("option-looking path: status = %d, want 400", code)
	}
}

// The modify/delete "delete" action removes the file and clears the conflict.
func TestOpResolveConflictDelete(t *testing.T) {
	t.Parallel()
	dir := modifyDeleteRepo(t)
	ts := serve(t, New(domain.Open(dir)))
	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"resolve-conflict","path":"md.txt","mode":"delete"}`), 30*time.Second)
	if done := events[len(events)-1]; done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	if _, err := os.Stat(filepath.Join(dir, "md.txt")); !os.IsNotExist(err) {
		t.Errorf("md.txt still present (stat err %v)", err)
	}
	if len(unmergedPaths(statusOf(t, dir))) != 0 {
		t.Error("conflict did not clear")
	}
}
