package tui

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The web client carries a JS port of elidePath (internal/web/static/core.js)
// so the browser sidebar shortens paths exactly the way the TUI does. Nothing
// but this test keeps the two in step — a change here that isn't mirrored
// there would silently give the two frontends different paths.
//
// It shells out to node, which is NOT a build requirement of this repo, so it
// skips when node isn't installed. The web client is monospace, so its
// "columns" are characters and the arithmetic matches lipgloss.Width for the
// ASCII paths covered here.
func TestElidePathJSPortMatchesGo(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS port guard needs it")
	}
	core, err := filepath.Abs(filepath.Join("..", "web", "static", "core.js"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(core); err != nil {
		t.Fatalf("web client core.js: %v", err)
	}

	paths := []string{
		"/mnt/t/others/gigagit",
		"/mnt/t/others/gigagit.worktrees/web-dev",
		"/home/user/.claude/projects/-mnt-t-others-gigagit/memory",
		"/a/b/c/d/e/f/g/h",
		"relative/path/to/thing",
		"/single",
		"single",
		"/mnt/t/others/gigagit/",
		`C:\Users\user\src\gigagit`,
		"/very-long-single-segment-name-that-cannot-fit.txt",
		"/x/averyveryverylongfinalsegmentname",
		"//server/share/deep/dir/name",
		"/",
		"",
	}
	type kase struct {
		Path string `json:"path"`
		N    int    `json:"n"`
		Want string `json:"want"`
	}
	var cases []kase
	for _, p := range paths {
		for n := 0; n <= 46; n++ {
			cases = append(cases, kase{Path: p, N: n, Want: elidePath(p, n)})
		}
	}
	blob, err := json.Marshal(cases)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	casesPath := filepath.Join(dir, "cases.json")
	if err := os.WriteFile(casesPath, blob, 0o644); err != nil {
		t.Fatal(err)
	}
	// The script prints one line per mismatch; silence means the port agrees.
	script := `
import { readFileSync } from "node:fs";
const { elidePath } = await import(process.argv[2]); // argv: node, script, core.js, cases
for (const c of JSON.parse(readFileSync(process.argv[3], "utf8"))) {
  const got = elidePath(c.path, c.n);
  if (got !== c.want) console.log(JSON.stringify({ ...c, got }));
}
`
	scriptPath := filepath.Join(dir, "check.mjs")
	if err := os.WriteFile(scriptPath, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, scriptPath, core, casesPath).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	if s := strings.TrimSpace(string(out)); s != "" {
		t.Fatalf("the JS port in internal/web/static/core.js disagrees with elidePath:\n%s", s)
	}
}
