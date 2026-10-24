package web

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

type sidesResp struct {
	Left  struct{ Label, Spec, Hash string } `json:"left"`
	Right struct{ Label, Spec, Hash string } `json:"right"`
	Files []struct {
		Path   string `json:"path"`
		Status string `json:"status"`
	} `json:"files"`
	Specs bool `json:"specs"`
}

// A commit against the working tree or the index: the whole-tree file list
// between the two (the TUI's Compare against working tree / staged and a
// marked commit against the ◇ Working tree row), addressed by spec so each
// file diffs through /api/entry-diff.
func TestCompareSides(t *testing.T) {
	t.Parallel()
	dir := editRepo(t, 2) // c1 a.txt, c2 b.txt
	c1 := shaOf(t, dir, "c1")
	writeFile(t, dir, "b.txt", "changed\n") // unstaged
	writeFile(t, dir, "s.txt", "staged\n")
	gitRun(t, dir, "add", "s.txt")
	ts := serve(t, New(domain.Open(dir)))
	paths := func(r sidesResp) string {
		var p []string
		for _, f := range r.Files {
			p = append(p, f.Status+":"+f.Path)
		}
		return strings.Join(p, ",")
	}
	var wt sidesResp
	if code := getJSON(t, ts, "/api/compare-sides?left=commit:"+c1+"&right=worktree", &wt); code != http.StatusOK {
		t.Fatalf("commit vs worktree = %d", code)
	}
	if got := paths(wt); got != "A:b.txt,A:s.txt" {
		t.Errorf("c1 ↔ working tree = %q, want A:b.txt,A:s.txt (b.txt was added after c1, s.txt is new)", got)
	}
	if !wt.Specs || wt.Left.Spec != "commit:"+c1 || wt.Right.Spec != "worktree" || wt.Right.Label != "working tree" {
		t.Errorf("sides = %+v / %+v (specs %v)", wt.Left, wt.Right, wt.Specs)
	}
	var st sidesResp
	if code := getJSON(t, ts, "/api/compare-sides?left=commit:"+shaOf(t, dir, "c2")+"&right=staged", &st); code != http.StatusOK {
		t.Fatalf("commit vs staged = %d", code)
	}
	if got := paths(st); got != "A:s.txt" {
		t.Errorf("c2 ↔ staged = %q, want A:s.txt (the unstaged b.txt edit is not in the index)", got)
	}
	for _, bad := range []string{"left=main&right=worktree", "left=commit:main&right=worktree", "left=worktree&right=shelf:x", "left=&right=worktree"} {
		if code := getJSON(t, ts, "/api/compare-sides?"+bad, nil); code != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400", bad, code)
		}
	}
}

// Copy to working dir: a commit's (or the index's) version of a file is
// written into the working tree as an unstaged change; a differing working
// file asks first (engine.WriteFile's overwrite fork).
func TestCopyToWorkingDir(t *testing.T) {
	t.Parallel()
	dir := editRepo(t, 2)
	c1 := shaOf(t, dir, "c1")
	writeFile(t, dir, "a.txt", "local edit\n")
	srv := New(domain.Open(dir))
	ts := serve(t, srv)
	opID := startOpBody(t, ts, `{"op":"copy-to-working","path":"a.txt","sha":"`+c1+`"}`)
	run := srv.opByID(opID)
	if run == nil {
		t.Fatal("run not found")
	}
	waitDecision(t, run)
	if code := postJSON(t, ts, "/api/op/"+opID+"/decide", `{"option":"overwrite"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("decide = %d", code)
	}
	events := readSSE(t, ts, opID, 30*time.Second)
	if done := events[len(events)-1]; done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "a.txt\n" {
		t.Errorf("a.txt = %q, want c1's version", b)
	}
	writeFile(t, dir, "s.txt", "staged copy\n")
	gitRun(t, dir, "add", "s.txt")
	if err := os.Remove(filepath.Join(dir, "s.txt")); err != nil {
		t.Fatal(err)
	}
	opID = startOpBody(t, ts, `{"op":"copy-to-working","path":"s.txt","ref":"staged"}`)
	events = readSSE(t, ts, opID, 30*time.Second)
	if done := events[len(events)-1]; done["ok"] != true {
		t.Fatalf("staged copy done = %v", done)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "s.txt")); string(b) != "staged copy\n" {
		t.Errorf("s.txt = %q, want the index's version", b)
	}
	for _, bad := range []string{
		`{"op":"copy-to-working","path":"a.txt","sha":"main"}`,
		`{"op":"copy-to-working","path":"../x","sha":"` + c1 + `"}`,
		`{"op":"copy-to-working","path":"a.txt"}`,
	} {
		if code := postJSON(t, ts, "/api/op", bad, "application/json", "", nil); code != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400", bad, code)
		}
	}
}

// The page side: the commit menu, the ◇ Working tree row as a mark, the file
// menu pair, and a spec-addressed compare screen.
func TestWipCompareClientWired(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ file, want, why string }{
		{"commits.js", `{ label: "compare against working tree", act: () => openSidesCompare("commit:" + c.hash, "worktree") }`, "a commit against the working copy"},
		{"commits.js", `{ label: "compare against staged", act: () => openSidesCompare("commit:" + c.hash, "staged") }`, "…and against the index"},
		{"commits.js", `if (state.wt && i === 0) toggleWorktreeMark();`, "ctrl+click marks the working-tree row"},
		{"commits.js", `if (ends.tip === WT_MARK) return openSidesCompare("commit:" + ends.base, "worktree");`, "two marks with the working tree compare against it"},
		{"commits.js", `if (commitsOnly(m, "squash")) return;`, "squash refuses the working-tree mark"},
		{"commits.js", `if (commitsOnly(m, "drop")) return;`, "drop refuses it too"},
		{"status.js", `state.cmarks.has("@worktree")`, "the working-tree row shows its mark"},
		{"files.js", `frozen: !!body.frozen || !!body.specs,`, "a live side is addressed by spec"},
		{"conflicts.js", `label: "compare against working dir"`, "a file against its working-tree copy"},
		{"conflicts.js", `{ op: "copy-to-working", path: f.path, sha: f.sha }`, "copy to working dir"},
		// review fixes: a renamed file diffs from its old path; the list
		// follows the working tree; the menu knows a deleted file; a slow
		// answer never takes over a newer screen
		{"files.js", `oldPath: f.old_path,`, "the spec-addressed open reads a renamed file's old path"},
		{"files.js", `if (f.old_path) q.set("old_path", f.old_path);`, "…and so does the shared diff URL (stacked view)"},
		{"commits.js", "state.compare.sides = { left, right };", "a sides compare remembers its specs"},
		{"commits.js", "async function refreshSidesCompare()", "…and re-reads its list"},
		{"live.js", "await refreshSidesCompare();", "a status or feed change refreshes it"},
		{"files.js", `section: "commit",
          status: f.status,`, "a commit row's menu knows the file's status"},
		{"files.js", "section: f.section, status: f.status }", "…and so does a status row's"},
		{"commits.js", "if (gen !== state.detailGen) return; // something newer opened", "a slow compare answer is dropped"},
	} {
		if !strings.Contains(readStatic(t, c.file), c.want) {
			t.Errorf("%s lacks %q: %s", c.file, c.want, c.why)
		}
	}
}

// A file renamed since the commit: the list says R with the old path, and its
// diff reads the old path at the commit (/api/entry-diff's old_path).
func TestCompareSidesRename(t *testing.T) {
	t.Parallel()
	dir := editRepo(t, 1) // c1 a.txt
	c1 := shaOf(t, dir, "c1")
	gitRun(t, dir, "mv", "a.txt", "z.txt")
	ts := serve(t, New(domain.Open(dir)))
	var got struct {
		Files []struct {
			Path    string `json:"path"`
			Status  string `json:"status"`
			OldPath string `json:"old_path"`
		} `json:"files"`
	}
	if code := getJSON(t, ts, "/api/compare-sides?left=commit:"+c1+"&right=worktree", &got); code != http.StatusOK {
		t.Fatalf("compare = %d", code)
	}
	if len(got.Files) != 1 || got.Files[0].Status != "R" || got.Files[0].OldPath != "a.txt" {
		t.Fatalf("files = %+v, want R z.txt from a.txt", got.Files)
	}
	if code := getJSON(t, ts, "/api/entry-diff?left=commit:"+c1+"&right=worktree&path=z.txt&old_path=a.txt&status=R", nil); code != http.StatusOK {
		t.Fatalf("the renamed file's diff = %d, want 200", code)
	}
}
