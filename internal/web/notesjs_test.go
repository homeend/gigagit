package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// jsFunc lifts one top-level function out of a static module by name. The
// closing brace must be at column 0, which is the file's own style.
func jsFunc(t *testing.T, file, name string) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("static", file))
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	i := strings.Index(s, "function "+name+"(")
	if i < 0 {
		t.Fatalf("%s: %s is gone", file, name)
	}
	j := strings.Index(s[i:], "\n}\n")
	if j < 0 {
		t.Fatalf("%s: %s has no closing brace at column 0", file, name)
	}
	return s[i : i+j+2]
}

// TestNoteRowsHTMLJS runs the shipped note-row painter under node. Notes carry
// FREE TEXT written by a person or an agent, so the escaping is a security
// property, not a cosmetic one; the agent-layer filter is per ROW (a user
// reply under a hidden agent root still renders), which is the rule the TUI
// settled on and the one easiest to get wrong here.
func TestNoteRowsHTMLJS(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	fns := jsFunc(t, "files.js", "noteRowsHTML") + "\n" + jsFunc(t, "files.js", "noteRowHTML")

	notes := []map[string]any{
		{"id": "n1", "side": "new", "line": 3, "source": "user", "status": "active",
			"summary": "<img src=x onerror=alert(1)>", "rationale": "why & how",
			"replies": []map[string]any{
				{"id": "n2", "side": "new", "line": 3, "source": "agent", "status": "active", "summary": "bot says"},
				{"id": "n3", "side": "new", "line": 3, "source": "user", "status": "active", "summary": "human says"},
			}},
		{"id": "n4", "side": "new", "line": 3, "source": "agent", "status": "stale", "summary": "moved"},
		{"id": "n5", "side": "old", "line": 3, "source": "user", "status": "active", "summary": "old side"},
	}
	blob, _ := json.Marshal(notes)

	script := "const esc = (x) => String(x).replace(/[&<>\"]/g, (c) => ({'&':'&amp;','<':'&lt;','>':'&gt;','\"':'&quot;'}[c]));\n" +
		"const state = { diffCtx: {path:'f'}, notes: JSON.parse(process.argv[1]), notesAgentOff: false };\n" + fns +
		"\nconst on = noteRowsHTML('new', 3, 4);\n" +
		"state.notesAgentOff = true;\n" +
		"const off = noteRowsHTML('new', 3, 4);\n" +
		"const otherLine = noteRowsHTML('new', 9, 4);\n" +
		"state.notes = [];\n" +
		"const none = noteRowsHTML('new', 3, 4);\n" +
		"console.log(JSON.stringify({on, off, otherLine, none}));\n"
	out, err := exec.Command(node, "-e", script, string(blob)).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var got struct{ On, Off, OtherLine, None string }
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("decode %q: %v", out, err)
	}

	if strings.Contains(got.On, "<img") || !strings.Contains(got.On, "&lt;img") {
		t.Fatalf("a note's summary must be escaped: %s", got.On)
	}
	if !strings.Contains(got.On, "why &amp; how") {
		t.Fatalf("the rationale must be escaped: %s", got.On)
	}
	if !strings.Contains(got.On, `colspan="4"`) {
		t.Fatalf("note rows must span the whole table: %s", got.On)
	}
	if n := strings.Count(got.On, "<tr "); n != 4 {
		t.Fatalf("agent layer ON: %d rows, want 4 (root + 2 replies + the agent root): %s", n, got.On)
	}
	if !strings.Contains(got.On, "note stale") {
		t.Fatalf("a stale note must carry its class: %s", got.On)
	}
	if !strings.Contains(got.On, "note reply") {
		t.Fatalf("a reply must carry its class: %s", got.On)
	}
	// Agent layer off: the agent root AND the agent reply go, the user reply
	// under the visible root stays.
	if n := strings.Count(got.Off, "<tr "); n != 2 {
		t.Fatalf("agent layer OFF: %d rows, want 2 (user root + user reply): %s", n, got.Off)
	}
	if strings.Contains(got.Off, "bot says") || strings.Contains(got.Off, "moved") {
		t.Fatalf("agent notes must be hidden with the layer off: %s", got.Off)
	}
	if !strings.Contains(got.Off, "human says") {
		t.Fatalf("a user reply must survive the agent filter: %s", got.Off)
	}
	// A row with no note of its own — and a diff with no notes at all —
	// renders byte-identically to before.
	if got.OtherLine != "" || got.None != "" {
		t.Fatalf("unanchored rows must render nothing: %q / %q", got.OtherLine, got.None)
	}
}
