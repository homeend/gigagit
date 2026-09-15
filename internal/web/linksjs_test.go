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
		// Fix round 1: a compare-mode file row's rev is bHash, but the table on
		// screen is aHash -> bHash, not bHash^ -> bHash — the file contributor
		// must be told to refuse rather than emit a misdescribed link.
		{"files.js", `compare: state.filesMode === "compare"`, "the compare-mode file-menu call site must signal the file contributor to refuse"},
		{"links.js", "ctx.compare", "linkFor must refuse when the ctx it was given says compare"},
		// Final fix wave: the diff-LINE path reads state.diffCtx, which used
		// to carry no compare field at all — so links.js's documented
		// ctx.compare guard never fired there (B5).
		{"files.js", "compare: cmp", "state.diffCtx must carry the compare flag the link producer documents"},
		{"links.js", "linkAbsOK", "the checkout path needs the same expressibility rule as the file path (A3)"},
		// Web preview links (2026-09-16): a merge preview is the one compare
		// with an address, so the refusal must be lifted for it alone.
		{"links.js", "ctx.preview", "linkFor must render the pair when the ctx names an open preview"},
		{"links.js", "linkRefOK", "branch names need model.LinkRefOK's expressibility rule"},
		{"links.js", `registerRows("preview"`, "the Previews group row must contribute the pair's own link"},
		{"links.js", "preview: ctx.preview", "the file contributor must forward the pair it was given"},
		{"files.js", "preview: po ?", "the file-row call site must hand the open preview's pair to the file contributor"},
		{"files.js", "row.dataset.rno", "the diff-line path must prefer a context row's new-side number over dropping the line"},
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
	k := strings.Index(filesSrc, `$("diff-body").addEventListener("contextmenu"`)
	if k < 0 {
		t.Fatal(`files.js: the diff-body contextmenu handler is gone`)
	}
	// Bounded to the handler's own body (its first top-level "});" close),
	// not the rest of the file — a distant, unrelated run: elsewhere in
	// files.js must not fail this check.
	end := strings.Index(filesSrc[k:], "\n});")
	if end < 0 {
		t.Fatal("files.js: could not find the end of the diff-body contextmenu handler")
	}
	if strings.Contains(filesSrc[k:k+end], "run:") {
		t.Error("files.js: the diff-body contextmenu handler uses run: — showCtxMenu dispatches act()")
	}
}

// wantLink mirrors links.js's linkFor pure section using model.Link.String()
// itself as the formatter, so a mismatch here means the JS producer drifted
// from the Go grammar's canonical renderer — not that two hand-written
// stringifications happen to disagree.
func wantLink(repoName, worktree, path, rev, st, side string, no int, compare bool) string {
	return wantLinkPreview(repoName, worktree, path, rev, st, side, no, compare, "", "")
}

// wantLinkPreview is wantLink with the merge-preview pair: a compare ctx
// refuses UNLESS it is a preview's (the one compare whose new side is a real
// commit's content), the pair rides as @<target>...<source> and — the user's
// ruling (2026-09-16) — an old-side line degrades to the file form, because a
// preview has no old side. That drop is applied HERE, explicitly: String()
// would force the side and still render ":N", which is not the ruling.
func wantLinkPreview(repoName, worktree, path, rev, st, side string, no int, compare bool, source, target string) string {
	if compare && source == "" {
		return ""
	}
	if source != "" && (!model.LinkRefOK(source) || !model.LinkRefOK(target)) {
		return ""
	}
	if repoName == "" && worktree == "" {
		return ""
	}
	if repoName == "" && !model.LinkAbsOK(worktree) {
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
	if source != "" {
		l.Target = model.LinkTarget{State: model.StateCommitted, Preview: &model.LinkPreview{Source: source, Target: target}}
		l.Side = model.NoteSideNew
		if no > 0 && side != "old" {
			if path == "" {
				return ""
			}
			l.Line = no
		}
		return l.String()
	}
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
		// Accepted (final review): a machine with no node loses this gate
		// rather than the suite — TestLinksJSIsWiredEverywhere still runs.
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
		Compare  bool   `json:"compare"`
		Source   string `json:"source"` // both set = a merge preview's pair (ctx.preview)
		Target   string `json:"target"`
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
		// Fix round 1 (controller ruling): a compare-mode ctx refuses outright,
		// even though every other field looks like a perfectly good link —
		// the compare flag alone must be decisive.
		{Name: "compare ctx refuses even with a full sha", Repo: "gigagit", Path: "a/b.go", Rev: fullSha, State: "commit", Compare: true},
		{Name: "local compare ctx refuses", Worktree: "/mnt/t/repo", Path: "a/b.go", State: "unstaged", Compare: true},
		// Final fix wave (A3): the CHECKOUT path has the same expressibility
		// rule as the file path — '@' and '#' are the grammar's separators, a
		// ':' is not (drive prefix, or a POSIX directory holding one).
		{Name: "local worktree with @ refuses", Worktree: "/home/user@corp/repo", Path: "a/b.go", State: "unstaged"},
		{Name: "local worktree with # refuses", Worktree: "/mnt/backup#1/repo", Path: "a/b.go", State: "unstaged"},
		{Name: "local worktree with a colon is fine", Worktree: "/mnt/odd:name/repo", Path: "a/b.go", State: "unstaged"},
		{Name: "windows drive worktree is fine", Worktree: "C:/src/repo", Path: "a/b.go", State: "unstaged"},
		{Name: "windows drive worktree with @ refuses", Worktree: "C:/src@work/repo", Path: "a/b.go", State: "unstaged"},
		// Web preview links (2026-09-16): a merge preview is the ONE compare
		// that has an address — the branch pair, git's three-dot spelling.
		// Every preview ctx below also carries compare:true, exactly as
		// state.diffCtx and the file-row call site hand it over.
		{Name: "preview pair, no path (Previews row)", Repo: "gigagit", State: "commit", Rev: fullSha, Compare: true, Source: "feat/x", Target: "main"},
		{Name: "local preview pair, no path", Worktree: "/mnt/t/repo", State: "commit", Rev: fullSha, Compare: true, Source: "feat/x", Target: "main"},
		{Name: "preview file (file row)", Repo: "gigagit", Path: "a/b.go", State: "commit", Rev: fullSha, Compare: true, Source: "feat/x", Target: "main"},
		{Name: "preview line, new side", Repo: "gigagit", Path: "a/b.go", State: "commit", Rev: fullSha, Compare: true, Source: "feat/x", Target: "main", Side: "new", No: 42},
		{Name: "preview line, old side degrades to the file form", Repo: "gigagit", Path: "a/b.go", State: "commit", Rev: fullSha, Compare: true, Source: "feat/x", Target: "main", Side: "old", No: 17},
		{Name: "preview line with no path refuses", Repo: "gigagit", State: "commit", Rev: fullSha, Compare: true, Source: "feat/x", Target: "main", Side: "new", No: 5},
		{Name: "preview ignores a short rev (the pair is the address)", Repo: "gigagit", Path: "a/b.go", State: "commit", Rev: shortSha, Compare: true, Source: "feat/x", Target: "main"},
		{Name: "preview without the compare flag still renders the pair", Repo: "gigagit", Path: "a/b.go", State: "commit", Rev: fullSha, Source: "feat/x", Target: "main"},
		{Name: "preview source with ... refuses", Repo: "gigagit", Path: "a/b.go", State: "commit", Compare: true, Source: "a...b", Target: "main"},
		{Name: "preview target with a space refuses", Repo: "gigagit", Path: "a/b.go", State: "commit", Compare: true, Source: "feat/x", Target: "ma in"},
		{Name: "preview source with @ refuses", Repo: "gigagit", State: "commit", Compare: true, Source: "feat@x", Target: "main"},
		{Name: "preview target with # refuses", Repo: "gigagit", State: "commit", Compare: true, Source: "feat/x", Target: "v#1"},
		{Name: "preview target with : refuses", Repo: "gigagit", State: "commit", Compare: true, Source: "feat/x", Target: "a:b"},
		{Name: "preview path with @ refuses", Repo: "gigagit", Path: "a@b.go", State: "commit", Compare: true, Source: "feat/x", Target: "main"},
		// Review F3: the JS rule is " \t" literally, as Go's — a \s would also
		// refuse an NBSP-bearing name the TUI emits a link for.
		{Name: "preview target with a tab refuses", Repo: "gigagit", State: "commit", Compare: true, Source: "feat/x", Target: "ma\tin"},
		{Name: "preview source with a no-break space is fine", Repo: "gigagit", State: "commit", Compare: true, Source: "feat/x y", Target: "main"},
	}

	want := make([]string, len(cases))
	for n, c := range cases {
		want[n] = wantLinkPreview(c.Repo, c.Worktree, c.Path, c.Rev, c.State, c.Side, c.No, c.Compare, c.Source, c.Target)
	}

	// Both halves of this test implement the preview rules by hand, so pin
	// the literal strings the user approved for the two rows that matter —
	// otherwise a shared mistake (say, rendering the old side) would agree
	// with itself and pass.
	pinned := map[string]string{
		"preview pair, no path (Previews row)":                    "gg://gigagit@main...feat/x",
		"preview line, new side":                                  "gg://gigagit/a/b.go@main...feat/x:42",
		"preview line, old side degrades to the file form":        "gg://gigagit/a/b.go@main...feat/x",
		"compare ctx refuses even with a full sha":                "",
		"preview without the compare flag still renders the pair": "gg://gigagit/a/b.go@main...feat/x",
	}
	for n, c := range cases {
		if p, ok := pinned[c.Name]; ok && want[n] != p {
			t.Errorf("case %q: wantLinkPreview = %q, pinned %q", c.Name, want[n], p)
		}
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
  const ctx = { path: c.path, rev: c.rev, state: c.state, compare: c.compare, preview: c.source ? { source: c.source, target: c.target } : null };
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
