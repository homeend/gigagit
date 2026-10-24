package web

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The TUI's Worktrees mark column, bottom-bar text and . menu rows
// (worktree_marks.go), as wtmarks.js computes them from a /api/worktrees row.
func TestWorktreeMarksJS(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	var mod strings.Builder
	for _, fn := range []string{"sinceLabel", "markPrefix", "claimHint", "markRows"} {
		mod.WriteString(jsFunc(t, "wtmarks.js", fn) + "\n")
	}
	mod.WriteString("export { markPrefix, claimHint, markRows };\n")
	harness := `
import { markPrefix, claimHint, markRows } from "./h.mjs";
const both = { path: "/r/wt2", reserved: true, claim: { agent: "bob", since: "2026-10-24T13:05:00Z", note: "fixing the parser" } };
const anon = { path: "/r/wt3", claim: { agent: "", since: "2026-10-24T13:05:00Z", note: "" } };
const res = { path: "/r/wt4", reserved: true };
const none = { path: "/r" };
console.log(JSON.stringify([
  [both, anon, res, none].map(markPrefix),
  [both, anon, res, none].map(claimHint),
  [both, anon, res, none].map(markRows),
]));
`
	dir := t.TempDir()
	for name, body := range map[string]string{"h.mjs": mod.String(), "run.mjs": harness} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(node, filepath.Join(dir, "run.mjs"))
	cmd.Env = append(os.Environ(), "TZ=UTC")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	want := `[["⚑ bob  ⊘  ","⚑ agent  ","⊘  ",""],` +
		`["⚑ claimed by bob since 2026-10-24 13:05 · fixing the parser","⚑ claimed by agent since 2026-10-24 13:05","⊘ reserved: agents never take this worktree",""],` +
		`[[{"id":"release","label":"Release claim (bob)","agent":"bob"},{"id":"unreserve","label":"Unreserve"}],` +
		`[{"id":"release","label":"Release claim (agent)","agent":"agent"},{"id":"reserve","label":"Reserve (no agents)"}],` +
		`[{"id":"unreserve","label":"Unreserve"}],` +
		`[{"id":"reserve","label":"Reserve (no agents)"}]]]`
	if got := strings.TrimSpace(string(out)); got != want {
		t.Fatalf("marks =\n%s\nwant\n%s", got, want)
	}
}
