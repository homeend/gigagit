package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// prfresh.js: the open PR's freshness word (spec §2.4, the TUI's
// prSeen/prUpdated rules) — never "updated" on a PR's first read, nor for
// the change your own send made.
func TestPRFreshJS(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	src, err := os.ReadFile(filepath.Join("static", "prfresh.js"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "prfresh.mjs"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	const runner = `
import { nextFresh } from "./prfresh.mjs";
let st = { seen: 0, updated: 0, ownSend: false };
const steps = [];
const step = (ev) => { st = nextFresh(st, ev); steps.push(st.text); };
step({ n: 7, kind: "start" });
step({ n: 7, kind: "ok", changed: true });
step({ n: 7, kind: "ok", changed: true });
step({ n: 7, kind: "ok", changed: false });
step({ n: 7, kind: "sent" });
step({ n: 7, kind: "ok", changed: true });
step({ n: 7, kind: "ok", changed: true });
step({ n: 7, kind: "fail", age: "3h" });
step({ n: 8, kind: "ok", changed: true });
step({ n: 8, kind: "start" });
console.log(JSON.stringify(steps));
`
	if err := os.WriteFile(filepath.Join(dir, "run.mjs"), []byte(runner), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, filepath.Join(dir, "run.mjs")).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var got []string
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	want := []string{"refreshing…", "", "updated", "", "", "", "updated", "offline · read 3h", "", "refreshing…"}
	if !slices.Equal(got, want) {
		t.Fatalf("steps = %q\nwant    %q", got, want)
	}
}

// prs.js routes every freshness change through nextFresh and shows the
// interrupted bar from both refresh answers.
func TestPRsFreshnessAndInterruptedAreWired(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("static", "prs.js"))
	if err != nil {
		t.Fatal(err)
	}
	js := string(b)
	for _, want := range []string{
		"fresh = nextFresh(fresh, { n, ...ev });",
		"showInterrupted(n, rv.interrupted);",
		"showInterrupted(n, r.interrupted);",
		`freshEvent(n, { kind: "sent" });`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("prs.js lacks %q", want)
		}
	}
	if strings.Contains(js, "read-only — gg never") {
		t.Error("prs.js's help still says gg never writes to the forge")
	}
}
