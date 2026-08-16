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

// The browser decides which whole-file conflict rows to OFFER while the menu
// is opening, with no round-trip to ask — so conflicts.js carries a port of
// the conflict-class rules that live in internal/model/conflict.go. Nothing
// but this test keeps the two in step, and the failure it prevents is quiet:
// a row that should not exist (resolving a conflict a way its class cannot
// express) or one that silently stops being offered.
//
// The server validates every action against the file's real class regardless
// (buildResolveConflict), so a drift is a UI bug rather than a data-loss one
// — but a menu that lies about what is possible is still a bug.
//
// Only the PURE section of conflicts.js is evaluated: the rest of the module
// touches the DOM at import time and cannot run under node.
const jsPureStart = "// --- conflict class table (pure; guarded against Go) ---"
const jsPureEnd = "// --- end conflict class table ---"

// allConflictCodes is git's complete set of porcelain-v2 unmerged codes.
var allConflictCodes = []string{"DD", "AU", "UD", "UA", "DU", "AA", "UU"}

func TestConflictActionsJSPortMatchesGo(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS port guard needs it")
	}
	src, err := os.ReadFile(filepath.Join("static", "conflicts.js"))
	if err != nil {
		t.Fatal(err)
	}
	i := strings.Index(string(src), jsPureStart)
	j := strings.Index(string(src), jsPureEnd)
	if i < 0 || j < i {
		t.Fatalf("conflicts.js: the guarded section markers are gone (%q / %q)", jsPureStart, jsPureEnd)
	}
	pure := string(src)[i:j]

	// What Go says, per code: the action names conflictActionFor accepts.
	want := map[string][]string{}
	for _, code := range allConflictCodes {
		f := model.FileStatus{Path: "f.txt", Kind: model.KindUnmerged, Staged: code[0], Unstaged: code[1]}
		names := conflictActionNames(f)
		// "mark" is the both-sides stage-as-is action, which the browser
		// reaches through the existing "mark resolved (stage as-is)" row on
		// /api/stage rather than through this menu — so it is not part of
		// what the JS table is expected to offer.
		var filtered []string
		for _, n := range names {
			if n != "mark" {
				filtered = append(filtered, n)
			}
		}
		want[code] = filtered
	}

	dir := t.TempDir()
	codesPath := filepath.Join(dir, "codes.json")
	blob, _ := json.Marshal(allConflictCodes)
	if err := os.WriteFile(codesPath, blob, 0o644); err != nil {
		t.Fatal(err)
	}
	script := `
import { readFileSync } from "node:fs";
const pure = readFileSync(process.argv[2], "utf8");
const codes = JSON.parse(readFileSync(process.argv[3], "utf8"));
const conflictActions = new Function(pure + "; return conflictActions;")();
const out = {};
for (const code of codes) {
  const f = { section: "conflicts", staged: code[0], unstaged: code[1] };
  out[code] = conflictActions(f).map((r) => r.action);
}
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
	out, err := exec.Command(node, scriptPath, purePath, codesPath).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	got := map[string][]string{}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("node output %q: %v", out, err)
	}
	for _, code := range allConflictCodes {
		if strings.Join(got[code], ",") != strings.Join(want[code], ",") {
			t.Errorf("%s: conflicts.js offers %v, the model allows %v", code, got[code], want[code])
		}
	}
	// A non-conflicted row must offer nothing at all — these rows appear in
	// the same menu as every other file's.
	if len(conflictActions(model.FileStatus{Path: "f.txt", Kind: model.KindTracked, Staged: 'M', Unstaged: '.'})) != 0 {
		t.Error("Go offers whole-file conflict actions for a non-conflicted file")
	}
}

// conflictActions is the Go side of the comparison above for the
// not-conflicted case: an ordinary file has no whole-file conflict actions.
func conflictActions(f model.FileStatus) []string {
	if f.Kind != model.KindUnmerged {
		return nil
	}
	return conflictActionNames(f)
}
