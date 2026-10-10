package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// C8: a kept body is cleared only by a changing send from its own box.
func TestPRKeptJS(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	src, err := os.ReadFile(filepath.Join("static", "prkept.js"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "prkept.mjs"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	const runner = `
import { keptText, keptAfterSend } from "./prkept.mjs";
const v = { pr: 7, group: "verdict", text: "keep me" };
const ok = { ok: true, changed: true };
const left = (k) => (k ? k.text : null);
console.log(JSON.stringify({
  text: keptText(v, 7, "verdict"), otherGroup: keptText(v, 7, "mine"), otherPR: keptText(v, 8, "verdict"),
  resolve: left(keptAfterSend(v, 7, { kind: "resolve", ids: ["PRRT_1"] }, ok)),
  otherPR2: left(keptAfterSend(v, 8, { kind: "verdict", body: "x" }, ok)),
  abort: left(keptAfterSend(v, 7, { kind: "verdict", body: "keep me" }, { ok: true, changed: false })),
  failed: left(keptAfterSend(v, 7, { kind: "verdict", body: "keep me" }, { ok: false })),
  own: left(keptAfterSend(v, 7, { kind: "verdict", body: "keep me" }, ok)),
  otherBox: left(keptAfterSend(v, 7, { kind: "notes", ids: ["n1"], verdict: true }, ok)), // the panel's send is not the verdict box's
}));
`
	if err := os.WriteFile(filepath.Join(dir, "run.mjs"), []byte(runner), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, filepath.Join(dir, "run.mjs")).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var got map[string]*string
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	keep := map[string]bool{"text": true, "resolve": true, "otherPR2": true, "abort": true, "failed": true, "otherBox": true}
	for _, k := range []string{"text", "otherGroup", "otherPR", "resolve", "otherPR2", "abort", "failed", "own", "otherBox"} {
		if keep[k] != (got[k] != nil) {
			t.Errorf("%s = %v (want kept=%v)", k, got[k], keep[k])
		}
	}
}
