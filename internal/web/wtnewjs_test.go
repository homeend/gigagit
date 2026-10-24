package web

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The TUI create-worktree popup's model (worktree_popup.go) as wtnew.js
// computes it: what is fixed, what is previewed, the lines and hint words,
// the refusals, and the op a create sends.
func TestWorktreeDialogJS(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	fns := []string{"displayStart", "fixedBranch", "fixedPath", "previewBody", "wtTitle", "wtHint", "keepLabel", "modeLine", "hookLine", "createCheck", "createBody"}
	var mod strings.Builder
	for _, fn := range fns {
		mod.WriteString(jsFunc(t, "wtnew.js", fn) + "\n")
	}
	mod.WriteString("export { " + strings.Join(fns, ", ") + " };\n")
	harness := `
import * as m from "./h.mjs";
const sha = "abcdef1234567890abcdef1234567890abcdef12";
const b = { from: "branch", start: "main", existing: true, state: "action", edit: "", branchOverride: "", pathOverride: "", inputs: {}, seed: "1", now: "N",
  preview: { branch: "main", path: "../r.wt/main", error: "" }, hook: false, runHook: true, switchOnCreate: false, keepOffered: false, keepLocked: false, keep: 0, prefix: null };
const c = { ...b, from: "commit", start: sha, existing: false, state: "edit", edit: "", preview: { branch: "x", path: "../w/x", error: "" }, keepOffered: true };
const out = [
  [m.fixedBranch(b), m.fixedBranch({ ...b, state: "edit", edit: "" }), m.fixedBranch({ ...b, state: "edit", edit: "x" }), m.fixedBranch({ ...b, branchOverride: "side" }), m.fixedBranch(c)],
  [m.fixedPath(b), m.fixedPath({ ...b, state: "editpath", edit: "/p" }), m.fixedPath({ ...b, pathOverride: "/q" })],
  m.previewBody(b),
  [m.wtTitle(b), m.wtTitle({ ...b, preview: { ...b.preview, branch: "side" } }), m.wtTitle(c), m.displayStart("v1")],
  [m.wtHint(b), m.wtHint({ ...b, hook: true }), m.wtHint({ ...b, switchOnCreate: true }), m.wtHint({ ...b, switchOnCreate: true, hook: true }),
   m.wtHint({ ...b, state: "input" }), m.wtHint({ ...b, state: "edit" }), m.wtHint({ ...b, state: "editpath" })],
  [0, 1, 2].map(m.keepLabel),
  [m.modeLine(b), m.modeLine({ ...c, state: "action", keep: 1 }), m.modeLine(c), m.modeLine({ ...c, keepLocked: true })],
  [m.hookLine(b), m.hookLine({ ...b, hook: true }), m.hookLine({ ...b, hook: true, runHook: false }), m.hookLine({ ...b, hook: true, state: "edit" })],
  [m.createCheck(c), m.createCheck({ ...c, branchOverride: "x", preview: { ...c.preview, error: "bad" } }), m.createCheck({ ...c, branchOverride: "x" }), m.createCheck(b)],
  m.createBody(b),
  m.createBody({ ...b, branchOverride: "side", preview: { ...b.preview, branch: "side" }, pathOverride: "/q", hook: true, runHook: false }),
  m.createBody({ ...c, branchOverride: "x", keep: 1, prefix: { id: "p1", scope: "repo" } }),
  m.createBody({ ...b, from: "tag", start: "v1", existing: false, preview: { branch: "v1", path: "../w/v1", error: "" } }),
  m.createBody({ ...b, from: "remote", start: "origin/f", existing: false, preview: { branch: "f", path: "../w/f", error: "" } }),
];
console.log(JSON.stringify(out));
`
	dir := t.TempDir()
	for name, body := range map[string]string{"h.mjs": mod.String(), "run.mjs": harness} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out, err := exec.Command(node, filepath.Join(dir, "run.mjs")).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	want := `[["main","main","x","side",""],` +
		`["","/p","/q"],` +
		`{"from":"branch","start":"main","branch":"main","path":"","inputs":{},"seed":"1","now":"N"},` +
		`["Create worktree for main","Create worktree from main","Create worktree from abcdef1","v1"],` +
		`["[w] create  [W] create & switch  [e] edit name  [E] edit path  [p] use a prefix  [esc] cancel",` +
		`"[w] create  [W] create & switch  [e] edit name  [E] edit path  [p] use a prefix  [h] hook  [esc] cancel",` +
		`"[enter/W] create & switch  [w] create only  [e] edit name  [E] edit path  [p] use a prefix  [esc] cancel",` +
		`"[enter/W] create & switch  [w] create only  [e] edit name  [E] edit path  [p] use a prefix  [h] hook  [esc] cancel",` +
		`"[type] value  [tab/enter] next field  [esc] cancel","[type] edit name  [enter] done  [esc] discard","[type] edit path  [enter] done  [esc] discard"],` +
		`["at this commit","at parent, changes staged","at parent, changes unstaged"],` +
		`["","start:  at parent, changes staged  ([m] change)","start:  at this commit","start:  at this commit  (root/merge commit — at this commit only)"],` +
		`["","[x] run post-create hook  ([h] toggle)","[ ] run post-create hook  ([h] toggle)",""],` +
		`["branch name required","cannot create: bad","",""],` +
		`{"op":"create-worktree","branch":"main","name":"main","path":"../r.wt/main","path_seq":true},` +
		`{"op":"create-worktree","branch":"main","name":"side","path":"../r.wt/main","no_hook":true},` +
		`{"op":"create-worktree","sha":"abcdef1234567890abcdef1234567890abcdef12","name":"x","path":"../w/x","path_seq":true,"mode":"staged","prefix_id":"p1","prefix_scope":"repo"},` +
		`{"op":"create-worktree","tag":"v1","name":"v1","path":"../w/v1","path_seq":true},` +
		`{"op":"create-worktree","ref":"origin/f","name":"f","path":"../w/f","path_seq":true}]`
	if got := strings.TrimSpace(string(out)); got != want {
		t.Fatalf("dialog model =\n%s\nwant\n%s", got, want)
	}
}

// Every "new worktree" entrance opens the one dialog; the old chained
// prompts, the client-side path guess and the separate keep rows are gone.
func TestWorktreeDialogWired(t *testing.T) {
	t.Parallel()
	sidebar := readStaticSrc(t, "sidebar.js")
	for _, want := range []string{
		`openWorktreeDialog({ from: "branch", start: b.name })`,
		`openWorktreeDialog({ from: "branch", start: b.name, switchOnCreate: true })`,
		`"create worktree & switch…"`,
		`openWorktreeDialog({ from: "tag", start: tg.name, prefill: tg.name })`,
		`"create worktree from " + rb.name + "…"`,
		`openWorktreeDialog({ from: "remote", start: rb.name, prefill: rb.branch })`,
	} {
		if !strings.Contains(sidebar, want) {
			t.Errorf("sidebar.js lacks %s", want)
		}
	}
	commits := readStaticSrc(t, "commits.js")
	if !strings.Contains(commits, `openWorktreeDialog({ from: "commit", start: c.hash, prefill: worktreePrefill(c.hash), keepOffered: true, keepLocked: c.parents !== 1 })`) {
		t.Error("commits.js: the commit entrance must open the dialog with the mode line")
	}
	if strings.Contains(readStaticSrc(t, "locks.js"), "worktree here, keeping the change") {
		t.Error("locks.js: the keep rows are the dialog's m now")
	}
	for _, f := range []string{"core.js", "sidebar.js", "commits.js", "locks.js"} {
		if strings.Contains(readStaticSrc(t, f), "defaultWorktreePath") {
			t.Errorf("%s: the client-side path guess is the server's template now", f)
		}
	}
	if !strings.Contains(readStaticSrc(t, "wtnew.js"), `registerHelp({`) {
		t.Error("wtnew.js: the dialog needs a help entry")
	}
}

// worktreePrefill is the TUI's commit-row prefill: <branch>_<short7>, wt_<short7> detached.
func TestWorktreePrefillJS(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	src := jsFunc(t, "commits.js", "worktreePrefillFor") + "\nconsole.log(JSON.stringify([worktreePrefillFor(\"abcdef1234\", \"main\"), worktreePrefillFor(\"abcdef1234\", \"\"), worktreePrefillFor(\"abcdef1234\", \"(detached)\")]));\n"
	out, err := exec.Command(node, "-e", src).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != `["main_abcdef1","wt_abcdef1","wt_abcdef1"]` {
		t.Errorf("prefill = %s", got)
	}
}
