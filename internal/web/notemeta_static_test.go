package web

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// Spec 2026-10-12-note-meta §7: the diff note box draws the resolved
// display — chips before the title, the frame class, line rows under the
// title, details rows after the body, and each reply its own run.
func TestDiffNoteBoxDrawsMeta(t *testing.T) {
	t.Parallel()
	f := readStatic(t, "files.js")
	for _, want := range []string{
		`from "./notemeta.js"`,
		`titleMetaHTML(n.display, esc)`,
		`frameClass(n.display)`,
		`metaLinesHTML(n.display, esc)`,
		`metaDetailsHTML(n.display, esc)`,
		`fitNoteTitle(badgeRunCols(n.display)`,
		"`<div class=\"mrun\">${badgeRunHTML(r.display, esc)}</div>`",
		`metaLinesHTML(r.display, esc)`,
		`metaDetailsHTML(r.display, esc)`,
	} {
		if !strings.Contains(f, want) {
			t.Errorf("files.js lacks %s", want)
		}
	}
}

// The painted box (spec §6.1/§7, under node): the chips lead the title,
// the frame is a class, the line rows sit between the title and the
// summary, the details after the body, a reply carries its own run — and
// agent data is escaped.
func TestDiffNoteBoxMetaJS(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	fns := jsFunc(t, "files.js", "noteRowsHTML") + "\n" + jsFunc(t, "files.js", "noteBoxHTML") +
		"\n" + jsFunc(t, "files.js", "notesArmed") + "\n" + jsFunc(t, "files.js", "globalNoteCtx")
	disp := map[string]any{
		"badges":   []map[string]any{{"key": "kind", "value": "issue", "color": "red", "style": "filled"}, {"key": "x", "value": "<b>", "style": "text"}},
		"audience": "author", "frame": "red",
		"lines":    []map[string]any{{"label": "confidence", "value": "high"}},
		"problems": []string{"bad colour"},
	}
	notes := []map[string]any{
		{"id": "n1", "side": "new", "line": 3, "source": "agent", "status": "active", "summary": "the race", "display": disp,
			"replies": []map[string]any{{"id": "n2", "side": "new", "line": 3, "source": "user", "status": "active", "summary": "fixing",
				"display": map[string]any{"badges": []map[string]any{{"key": "status", "value": "agreed", "color": "green", "style": "text"}}}}}},
		{"id": "n3", "side": "new", "line": 3, "source": "user", "status": "stale", "summary": "old", "display": map[string]any{"frame": "red"}},
		{"id": "n4", "side": "new", "line": 3, "source": "user", "status": "active", "summary": "plain"},
	}
	blob, _ := json.Marshal(notes)
	script := "const esc = (x) => String(x).replace(/[&<>\"]/g, (c) => ({'&':'&amp;','<':'&lt;','>':'&gt;','\"':'&quot;'}[c]));\n" +
		"const state = { diffCtx: {path:'f.go'}, notes: JSON.parse(process.argv[1]), notesAgentOff: false, noteCollapsed: new Set() };\n" +
		noteboxInline(t) + fns + "\nconsole.log(JSON.stringify(noteRowsHTML('new', 3, 4)));\n"
	out, err := exec.Command(node, "-e", script, string(blob)).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var html string
	if err := json.Unmarshal(out, &html); err != nil {
		t.Fatalf("decode %q: %v", out, err)
	}
	box := func(id string) string {
		i := strings.Index(html, `data-note="`+id+`"><td`)
		if i < 0 {
			t.Fatalf("no box %s: %s", id, html)
		}
		j := strings.Index(html[i:], "</tr>")
		return html[i : i+j]
	}
	b1 := box("n1")
	for _, want := range []string{`notebox agent mf-red"`, `<span class="mchip mc-red ms-filled"> issue </span>`, `&lt;b&gt;`,
		`<span class="maud">→ author</span><span class="msep"> · </span>agent note · f.go R3`,
		`<div class="mrun"><span class="mchip mc-green ms-text">agreed</span></div>`, `display problem: bad colour`} {
		if !strings.Contains(b1, want) {
			t.Errorf("box n1 lacks %s:\n%s", want, b1)
		}
	}
	if strings.Contains(b1, "<b>") {
		t.Errorf("agent meta must be escaped: %s", b1)
	}
	title, line, sum, det := strings.Index(b1, `class="notetitle"`), strings.Index(b1, `class="mline">confidence: high`),
		strings.Index(b1, `class="notesum"`), strings.Index(b1, `class="mdetail"`)
	if !(title < line && line < sum && sum < det) {
		t.Errorf("row order title %d < line %d < summary %d < details %d broken:\n%s", title, line, sum, det, b1)
	}
	if b3 := box("n3"); !strings.Contains(b3, `notebox user stale mf-red"`) {
		t.Errorf("a stale framed box keeps both classes (the CSS dims it): %s", b3)
	}
	if b4 := box("n4"); strings.Contains(b4, "mchip") || strings.Contains(b4, " mf-") || strings.Contains(b4, "mline") {
		t.Errorf("a note without meta draws none: %s", b4)
	}
}
