package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// pureJS runs one guarded pure section of a static file under node and
// returns what expr (evaluated after the section) prints as JSON.
func pureJS(t *testing.T, file, start, end, expr string, out any) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	src, err := os.ReadFile(filepath.Join("static", file))
	if err != nil {
		t.Fatal(err)
	}
	i, j := strings.Index(string(src), start), strings.Index(string(src), end)
	if i < 0 || j < i {
		t.Fatalf("%s: the guarded section markers are gone (%q / %q)", file, start, end)
	}
	dir := t.TempDir()
	purePath := filepath.Join(dir, "pure.js")
	scriptPath := filepath.Join(dir, "check.mjs")
	script := `
import { readFileSync } from "node:fs";
const pure = readFileSync(process.argv[2], "utf8");
console.log(JSON.stringify(new Function(pure + "; return (" + process.argv[3] + ");")()));
`
	if err := os.WriteFile(purePath, src[i:j], 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scriptPath, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err := exec.Command(node, scriptPath, purePath, expr).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, raw)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("node output %q: %v", raw, err)
	}
}

// The chunked painter covers every row exactly once, in order: the first
// (synchronous) range, then fixed-size ranges to the end.
func TestChunkRangesJS(t *testing.T) {
	t.Parallel()
	var got [][][2]int
	pureJS(t, "diffpaint.js", "// --- chunk ranges (pure; guarded against Go) ---", "// --- end chunk ranges ---",
		`[chunkRanges(0, 50, 400), chunkRanges(30, 50, 400), chunkRanges(1000, 50, 400), chunkRanges(450, 50, 400)]`, &got)
	want := [][][2]int{
		{},
		{{0, 30}},
		{{0, 50}, {50, 450}, {450, 850}, {850, 1000}},
		{{0, 50}, {50, 450}},
	}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for n := range want {
		if len(got[n]) != len(want[n]) {
			t.Fatalf("case %d: got %v want %v", n, got[n], want[n])
		}
		for k := range want[n] {
			if got[n][k] != want[n][k] {
				t.Fatalf("case %d: got %v want %v", n, got[n], want[n])
			}
		}
	}
}

// Every reader of a painted diff's rows must go through the accessor that
// finishes the paint first (diffBody / histDiff), and only code that never
// reads rows may take the raw element (diffBodyEl / histDiffEl). Naming the
// element any other way bypasses that choice, so it is allowed only in the
// accessors themselves.
func TestDiffBodyReadsGoThroughAccessor(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob(filepath.Join("static", "*.js"))
	if err != nil {
		t.Fatal(err)
	}
	bad := regexp.MustCompile(`\$\("(diff-body|history-diff)"\)|getElementById\("(diff-body|history-diff)"\)|["'` + "`" + `]#(diff-body|history-diff)\b`)
	allowed := regexp.MustCompile(`^\s*(const|function) (diffBodyEl|histDiffEl)\b|^\s*return \$\("(diff-body|history-diff)"\); // the accessor$`)
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for n, line := range strings.Split(string(src), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") || !bad.MatchString(line) || allowed.MatchString(line) {
				continue
			}
			t.Errorf("%s:%d names the diff host directly — use diffBodyEl()/diffBody() (histDiffEl()/histDiff()): %s", filepath.Base(f), n+1, strings.TrimSpace(line))
		}
	}
}
