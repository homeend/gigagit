package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The web All notes tree is the TUI's buildAllNotesRows: groups, subs, a
// directory heading when the directory changes, file, then its notes; a
// missing commit's notes read "missing"; a query keeps the matching notes and
// their ancestors (folds ignored), no query hides a folded heading's subtree.
const allNotesHarness = `
import { anBuildRows, anVisible, anAgo } from "./an.mjs";
const note = (id, summary, author) => ({ id, summary, author, source: "user", status: "active", side: "new", range: [1, 1], replies: [] });
const ov = {
  worktree: "repo",
  unstaged: [{ path: "a/x.go", state: "unstaged", notes: [note("n1", "fix x", "me")] }],
  staged: [], untracked: [],
  commits: [
    { hash: "abcdef1234567", subject: "c2", time: 0, missing: false,
      reviews: [{ id: "r1", kind: "commit", agent: "Claude", summary: "looks fine" }],
      files: [{ path: "a/y.go", state: "commit", status: "M", notes: [note("n2", "why y", "you")] },
              { path: "a/z.go", state: "commit", status: "A", notes: [note("n3", "zed", "you")] }] },
    { hash: "0000000aaaaaa", subject: "", time: 0, missing: true,
      reviews: [], files: [{ path: "q.go", state: "commit", notes: [note("n4", "gone", "you")] }] },
  ],
  shelves: [{ id: "s1", label: "", missing: false, entry: [note("e1", "deleted 2 files", "gg"), note("e2", "renamed 1 file", "gg")],
               files: [{ path: "s.go", state: "shelf", notes: [note("n5", "shelved", "you")] }] },
            { id: "s2", label: "gone", missing: true, entry: [note("e3", "old recycle", "gg")], files: [] }],
};
const rows = anBuildRows(ov);
const shape = rows.map((r) => r.kind + ":" + r.depth + ":" + (r.kind === "note" ? r.note.id + "/" + r.status : r.kind === "review" ? r.review.id : r.text));
const spans = rows.map((r) => r.span);
const q = anVisible(rows, "why", {}).map((r) => r.kind === "note" ? r.note.id : r.text);
const folded = anVisible(rows, "", { commits: true }).map((r) => r.kind);
const reviewQ = anVisible(rows, "claude", {}).map((r) => r.kind);
const entryQ = anVisible(rows, "renamed", {}).map((r) => r.kind === "note" ? r.note.id + "@" + r.target.shelf : r.text);
console.log(JSON.stringify({ shape, spans, q, folded, reviewQ, entryQ,
  ago: [anAgo(5000), anAgo(120000), anAgo(7200000), anAgo(3 * 86400000)] }));
`

func TestAllNotesRowsMatchTheTUITree(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	mod := jsFunc(t, "allnotes.js", "anBuildRows") + "\n" + jsFunc(t, "allnotes.js", "anVisible") + "\n" +
		jsFunc(t, "allnotes.js", "anAgo") + "\nexport { anBuildRows, anVisible, anAgo };\n"
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "an.mjs"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run.mjs"), []byte(allNotesHarness), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, filepath.Join(dir, "run.mjs")).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var got struct {
		Shape   []string `json:"shape"`
		Spans   []int    `json:"spans"`
		Q       []string `json:"q"`
		Folded  []string `json:"folded"`
		ReviewQ []string `json:"reviewQ"`
		EntryQ  []string `json:"entryQ"`
		Ago     []string `json:"ago"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(out))), &got); err != nil {
		t.Fatalf("not the harness JSON: %v\n%s", err, out)
	}
	wantShape := []string{
		"group:0:Working tree  (repo)",
		"sub:1:Unstaged",
		"dir:2:a/",
		"file:3:x.go",
		"note:4:n1/active",
		"group:0:Commits",
		"sub:1:abcdef1  c2",
		"dir:2:Reviews",
		"review:3:r1",
		"dir:2:a/",
		"file:3:y.go",
		"note:4:n2/active",
		"file:3:z.go",
		"note:4:n3/active",
		"sub:1:0000000  (missing — rewritten or deleted)",
		"file:3:q.go",
		"note:4:n4/missing",
		"group:0:Other",
		"sub:1:shelf  s1",
		"note:2:e1/active",
		"note:2:e2/active",
		"file:3:s.go",
		"note:4:n5/active",
		"sub:1:shelf  gone",
		"note:2:e3/missing",
	}
	if strings.Join(got.Shape, "\n") != strings.Join(wantShape, "\n") {
		t.Fatalf("rows:\n%s\nwant:\n%s", strings.Join(got.Shape, "\n"), strings.Join(wantShape, "\n"))
	}
	// The Commits group spans to the Other group; the first commit's sub to
	// the missing commit's.
	if got.Spans[5] != 17 || got.Spans[6] != 14 || got.Spans[3] != 5 {
		t.Fatalf("spans = %v", got.Spans)
	}
	if strings.Join(got.Q, ",") != "Commits,abcdef1  c2,a/,y.go,n2" {
		t.Fatalf("query rows = %v", got.Q)
	}
	if strings.Join(got.Folded, ",") != "group,sub,dir,file,note,group,group,sub,note,note,file,note,sub,note" {
		t.Fatalf("folded rows = %v", got.Folded)
	}
	if strings.Join(got.ReviewQ, ",") != "group,sub,dir,review" {
		t.Fatalf("review query rows = %v", got.ReviewQ)
	}
	// A shelf entry's own notes head its files and are found by a query; the
	// row knows its entry (enter reads the note there).
	if strings.Join(got.EntryQ, ",") != "Other,shelf  s1,e2@s1" {
		t.Fatalf("entry query rows = %v", got.EntryQ)
	}
	if strings.Join(got.Ago, ",") != "5s,2m,2h,3d" {
		t.Fatalf("ago = %v", got.Ago)
	}
}
