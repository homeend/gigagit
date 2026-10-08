package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// noteMark (notebox.js) is a box's sync mark (spec 2026-10-07 §1.1): every
// state in a PR's own diff, only sending / failed elsewhere (plan 3's T3).
func TestNoteMarkJS(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	src, err := os.ReadFile(filepath.Join("static", "notebox.js"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notebox.mjs"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	const runner = `
import { noteMark } from "./notebox.mjs";
const marks = [
  noteMark({ sync: "local" }, true),
  noteMark({ sync: "sending" }, true),
  noteMark({ sync: "failed", send_error: "HTTP 403" }, true),
  noteMark({ source: "forge", read_only: true, sync: "github" }, true),
  noteMark({ sync: "local" }, false),
  noteMark({ sync: "failed", send_error: "x" }, false),
  noteMark({ sync: "github", source: "forge", read_only: true }, false),
  noteMark({ source: "forge", read_only: true }, true),
];
console.log(JSON.stringify({ marks }));
`
	if err := os.WriteFile(filepath.Join(dir, "run.mjs"), []byte(runner), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, filepath.Join(dir, "run.mjs")).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var got struct {
		Marks []json.RawMessage `json:"marks"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("decode %s: %v", out, err)
	}
	want := []string{`{"glyph":"○","cls":"mark-local"`, `{"glyph":"◌","cls":"mark-sending"`,
		`{"glyph":"○!","cls":"mark-failed","tip":"HTTP 403"}`, `{"glyph":"●","cls":"mark-github"`,
		`null`, `{"glyph":"○!","cls":"mark-failed"`, `null`, `{"glyph":"●","cls":"mark-github"`}
	if len(got.Marks) != len(want) {
		t.Fatalf("marks = %s", out)
	}
	for i, w := range want {
		if !strings.HasPrefix(string(got.Marks[i]), w) {
			t.Errorf("mark %d = %s, want prefix %s", i, got.Marks[i], w)
		}
	}
}

// The box draws its mark and its group border only through noteMark /
// group_slot, and the border only inside a PR's own diff (plan 3's T3).
func TestNoteBoxDrawsMarksInPRDiffOnly(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("static", "files.js"))
	if err != nil {
		t.Fatal(err)
	}
	js := string(b)
	for _, want := range []string{
		"const inPR = !!(nc.ctx && nc.ctx.preview && nc.ctx.preview.pr);",
		"const mark = noteMark(n, inPR);",
		`const slot = inPR && n.group_slot ? " g" + n.group_slot : "";`,
		`class="notesenderr"`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("files.js lacks %q", want)
		}
	}
}
