package web

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The ☰ menu's layout is the user's: rows another module contributes (compare
// with link, the merge previews) close the first group rather than trailing
// the last, and settings opens the Config group.
func TestMainMenuGroups(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	mod := `
let rows = null;
const state = { wt: false, conflict: false, rows: [] };
const $ = () => ({ getBoundingClientRect: () => ({ left: 0, bottom: 0 }) });
const featureDisabled = () => false;
const hostedOnly = (r) => [r];
const extraRows = (menu) => menu === "menu" ? [{ label: "contributed" }] : [];
const showCtxMenu = (r) => { rows = r; };
` + jsFunc(t, "palette.js", "openGlobalMenu") + `
openGlobalMenu();
const groups = {};
let cur = "";
for (const r of rows) {
  if (r.header) { cur = r.header; groups[cur] = []; }
  else if (r.sep) cur = "after";
  else (groups[cur] = groups[cur] || []).push(r.label);
}
console.log(JSON.stringify({
  actionsEnd: groups.Actions.at(-1),
  ui: groups.UI.includes("settings…"),
  config: groups.Config,
  after: groups.after,
}));
`
	p := filepath.Join(t.TempDir(), "m.mjs")
	if err := os.WriteFile(p, []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, p).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	want := `{"actionsEnd":"contributed","ui":false,` +
		`"config":["settings…","identity & profiles…","agent setup…","external tools…","git config…","Switch to TUI"],` +
		`"after":["help","quit gg…"]}`
	if got := strings.TrimSpace(string(out)); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}
