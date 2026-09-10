package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// linksPureStart/End bracket the section of links.js with no imports and no
// DOM access — the same guard convention as files.js's commit-meta-line
// section (see commitmetajs_test.go): only this slice can run under node.
const linksPureStart = "// --- link producer (pure; guarded against Go) ---"
const linksPureEnd = "// --- end link producer ---"

func TestLinksJSIsWiredEverywhere(t *testing.T) {
	t.Parallel()
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join("static", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	checks := []struct{ file, want, why string }{
		{"app.js", "./links.js", "the module must be imported at boot"},
		{"links.js", "export { linkFor }", "linkFor is the shared producer"},
		{"links.js", `registerRows("file"`, "file rows must contribute a copy-link row"},
		{"links.js", `registerRows("commit"`, "commit rows must contribute a copy-link row"},
		{"links.js", "copyText(", "the row copies through the shared clipboard helper"},
		{"files.js", "linkFor(", "the diff-row menu must build a link"},
		// This substring exists only after the diff-line copy-link branch was
		// added — unlike notesArmed() (already present, unchanged, in the
		// pre-feature file), it actually pins the new code (ruling P12).
		{"files.js", "copy gg link to this line", "the diff-row copy-link row must be wired into the contextmenu handler"},
	}
	for _, c := range checks {
		if !strings.Contains(read(c.file), c.want) {
			t.Errorf("%s: missing %q — %s", c.file, c.want, c.why)
		}
	}
	// The menu dispatcher calls .act() unconditionally: a row written with
	// run: throws on click. Pin it for every row this feature adds.
	src := read("links.js")
	if strings.Contains(src, "run:") {
		t.Error("links.js: a ctx-menu row uses run: — showCtxMenu dispatches act()")
	}
	if !strings.Contains(src, "act:") {
		t.Error("links.js: no act: row found")
	}
	filesSrc := read("files.js")
	if k := strings.Index(filesSrc, `$("diff-body").addEventListener("contextmenu"`); k < 0 {
		t.Error(`files.js: the diff-body contextmenu handler is gone`)
	} else if strings.Contains(filesSrc[k:], "run:") {
		t.Error("files.js: the diff-body contextmenu handler uses run: — showCtxMenu dispatches act()")
	}
}

// wantLink mirrors links.js's linkFor pure section using model.Link.String()
// itself as the formatter, so a mismatch here means the JS producer drifted
// from the Go grammar's canonical renderer — not that two hand-written
// stringifications happen to disagree.
func wantLink(repoName, worktree, path, rev, st, side string, no int) string {
	if repoName == "" && worktree == "" {
		return ""
	}
	if path != "" && !model.LinkPathOK(path) {
		return ""
	}
	var l model.Link
	if repoName != "" {
		l.Repo = model.LinkRepo{Name: repoName}
	} else {
		l.Repo = model.LinkRepo{Abs: worktree}
	}
	l.Path = path
	switch st {
	case "staged":
		l.Target = model.LinkTarget{State: model.StateStaged}
	case "commit":
		if len(rev) < 40 {
			return ""
		}
		l.Target = model.LinkTarget{State: model.StateCommitted, Commit: rev}
	default: // "unstaged", "untracked"
		l.Target = model.LinkTarget{State: model.StateUnstaged}
	}
	l.Side = model.NoteSideNew
	if no > 0 {
		if path == "" {
			return ""
		}
		if side == "old" {
			l.Side = model.NoteSideOld
		}
		l.Line = no
	}
	return l.String()
}

func TestLinkForJSMatchesGo(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS port guard needs it")
	}
	src, err := os.ReadFile(filepath.Join("static", "links.js"))
	if err != nil {
		t.Fatal(err)
	}
	i := strings.Index(string(src), linksPureStart)
	j := strings.Index(string(src), linksPureEnd)
	if i < 0 || j < i {
		t.Fatalf("links.js: the guarded section markers are gone (%q / %q)", linksPureStart, linksPureEnd)
	}
	pure := string(src)[i:j]

	fullSha := strings.Repeat("a", 40)
	shortSha := strings.Repeat("a", 39)

	type tcase struct {
		Name     string `json:"name"`
		Repo     string `json:"repo"`     // link_repo; "" = local form
		Worktree string `json:"worktree"` // used only when Repo == ""
		Path     string `json:"path"`
		Rev      string `json:"rev"`
		State    string `json:"state"`
		Side     string `json:"side"`
		No       int    `json:"no"`
	}
	cases := []tcase{
		{Name: "remote unstaged file, no line", Repo: "gigagit", Path: "internal/web/files.js", State: "unstaged"},
		{Name: "local unstaged file, no line", Worktree: "/mnt/t/repo", Path: "internal/web/files.js", State: "unstaged"},
		{Name: "remote staged file", Repo: "gigagit", Path: "a/b.go", State: "staged"},
		{Name: "local staged file", Worktree: "/mnt/t/repo", Path: "a/b.go", State: "staged"},
		{Name: "remote untracked file (same as unstaged)", Repo: "gigagit", Path: "new.txt", State: "untracked"},
		{Name: "local untracked file (same as unstaged)", Worktree: "/mnt/t/repo", Path: "new.txt", State: "untracked"},
		{Name: "remote commit, full sha", Repo: "gigagit", Path: "a/b.go", Rev: fullSha, State: "commit"},
		{Name: "local commit, full sha", Worktree: "/mnt/t/repo", Path: "a/b.go", Rev: fullSha, State: "commit"},
		{Name: "remote commit, no path (commit-menu row)", Repo: "gigagit", Rev: fullSha, State: "commit"},
		{Name: "local commit, no path", Worktree: "/mnt/t/repo", Rev: fullSha, State: "commit"},
		{Name: "remote commit, short sha refuses (P9)", Repo: "gigagit", Path: "a/b.go", Rev: shortSha, State: "commit"},
		{Name: "local commit, short sha refuses (P9)", Worktree: "/mnt/t/repo", Path: "a/b.go", Rev: shortSha, State: "commit"},
		{Name: "remote commit, empty rev refuses", Repo: "gigagit", Path: "a/b.go", State: "commit"},
		{Name: "line on the new side", Repo: "gigagit", Path: "a/b.go", State: "unstaged", Side: "new", No: 42},
		{Name: "line on the old side", Repo: "gigagit", Path: "a/b.go", State: "unstaged", Side: "old", No: 17},
		{Name: "local, line on the old side", Worktree: "/mnt/t/repo", Path: "a/b.go", State: "unstaged", Side: "old", No: 3},
		{Name: "line with no path refuses", Repo: "gigagit", State: "unstaged", Side: "new", No: 5},
		{Name: "remote path with @ refuses", Repo: "gigagit", Path: "a@b.go", State: "unstaged"},
		{Name: "local path with @ refuses", Worktree: "/mnt/t/repo", Path: "a@b.go", State: "unstaged"},
		{Name: "remote path with : refuses", Repo: "gigagit", Path: "a:b.go", State: "unstaged"},
		{Name: "local path with : refuses", Worktree: "/mnt/t/repo", Path: "a:b.go", State: "unstaged"},
		{Name: "remote path with # refuses", Repo: "gigagit", Path: "a#b.go", State: "unstaged"},
		{Name: "local path with # refuses", Worktree: "/mnt/t/repo", Path: "a#b.go", State: "unstaged"},
	}

	want := make([]string, len(cases))
	for n, c := range cases {
		want[n] = wantLink(c.Repo, c.Worktree, c.Path, c.Rev, c.State, c.Side, c.No)
	}

	dir := t.TempDir()
	casesPath := filepath.Join(dir, "cases.json")
	blob, _ := json.Marshal(cases)
	if err := os.WriteFile(casesPath, blob, 0o644); err != nil {
		t.Fatal(err)
	}
	script := `
import { readFileSync } from "node:fs";
const pure = readFileSync(process.argv[2], "utf8");
const cases = JSON.parse(readFileSync(process.argv[3], "utf8"));
const linkFor = new Function(pure + "; return linkFor;")();
const out = cases.map((c) => {
  const repo = c.repo ? { link_repo: c.repo } : null;
  const ctx = { path: c.path, rev: c.rev, state: c.state };
  return linkFor(repo, c.worktree, ctx, c.side, c.no);
});
console.log(JSON.stringify(out));
`
	purePath := filepath.Join(dir, "pure.js")
	scriptPath := filepath.Join(dir, "check.mjs")
	if err := os.WriteFile(purePath, []byte(pure), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scriptPath, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, scriptPath, purePath, casesPath).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var got []string
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("node output %q: %v", out, err)
	}
	if len(got) != len(want) {
		t.Fatalf("node returned %d links, want %d", len(got), len(want))
	}
	for n := range want {
		if got[n] != want[n] {
			t.Errorf("case %q: links.js = %q, model.Link.String() = %q", cases[n].Name, got[n], want[n])
		}
	}
}
