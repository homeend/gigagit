package web

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The create-branch dialog through "use prefix…", as the TUI's branch popup
// does it (branch_popup.go resolvePrefix: parent := p.startPoint): the prefix
// resolves against the dialog's own start point, so <parent-branch> names the
// row the dialog was opened from, not the active worktree's branch. The start
// point also survives the picker, whichever way the picker ends.
func TestCreateBranchPromptPrefixResolvesAgainstItsStart(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	mod := `
let prompt = null, pickerOpts = "unset", pickerCb = null, started = null;
const openPrompt = (o) => { prompt = o; };
const closePrompt = () => {};
const openPrefixPicker = (cb, opts) => { pickerCb = cb; pickerOpts = opts; };
const startOp = (body) => { started = body; };
` + jsFunc(t, "ops.js", "openCreateBranchPrompt") + `
const flow = (start, label, pick) => {
  started = null;
  openCreateBranchPrompt(start, undefined, label);
  prompt.extra.run("typed");
  const parent = pickerOpts ? pickerOpts.parent : null;
  pick();
  const value = prompt.value, title = prompt.title;
  prompt.onSubmit(value);
  return { parent, value, title, started };
};
const p = { id: "bck", scope: "global" };
console.log(JSON.stringify([
  flow("zz-clicked", undefined, () => pickerCb("zz-clicked_BCK", p)),
  flow("abc123", "abc", () => pickerCb("n", p)),
  flow(undefined, undefined, () => pickerCb("n", p)),
  flow("zz-clicked", undefined, () => pickerCb(null, null)),
]));
`
	path := filepath.Join(t.TempDir(), "run.mjs")
	if err := os.WriteFile(path, []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, path).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	want := `[{"parent":"zz-clicked","value":"zz-clicked_BCK","title":"New branch, starting at zz-clicked:","started":{"op":"create-branch","name":"zz-clicked_BCK","branch":"zz-clicked","prefix_id":"bck","prefix_scope":"global"}},` +
		`{"parent":"abc123","value":"n","title":"New branch, starting at abc:","started":{"op":"create-branch","name":"n","branch":"abc123","prefix_id":"bck","prefix_scope":"global"}},` +
		`{"value":"n","title":"New branch, starting at the current HEAD:","started":{"op":"create-branch","name":"n","prefix_id":"bck","prefix_scope":"global"}},` +
		`{"parent":"zz-clicked","value":"typed","title":"New branch, starting at zz-clicked:","started":{"op":"create-branch","name":"typed","branch":"zz-clicked"}}]` + "\n"
	if string(out) != want {
		t.Fatalf("flow:\n got %s\nwant %s", out, want)
	}
}
