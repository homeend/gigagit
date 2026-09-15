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
	fns := jsFunc(t, "files.js", "noteRowsHTML") + "\n" + jsFunc(t, "files.js", "noteBoxHTML") +
		"\n" + jsFunc(t, "files.js", "notesArmed")

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
	// Split layout: the box sits in the new side's pane (two columns) with a
	// blank two-column gap on the old side.
	if !strings.Contains(got.On, `<td class="note-gap" colspan="2"></td><td class="note" colspan="2">`) {
		t.Fatalf("a new-side note must occupy the right pane only: %s", got.On)
	}
	if !strings.Contains(got.On, "note · ") || !strings.Contains(got.On, " R3") {
		t.Fatalf("the box title must name the file line: %s", got.On)
	}
	// One box per thread, every note keyed by its id: the user root's box
	// holds its two replies; the agent root is its own box.
	if n := strings.Count(got.On, "<tr "); n != 2 {
		t.Fatalf("agent layer ON: %d boxes, want 2 (the user thread + the agent root): %s", n, got.On)
	}
	if n := strings.Count(got.On, "data-note="); n != 4 {
		t.Fatalf("agent layer ON: %d note ids, want 4 (root + 2 replies + the agent root): %s", n, got.On)
	}
	if !strings.Contains(got.On, "note stale") {
		t.Fatalf("a stale note must carry its class: %s", got.On)
	}
	if !strings.Contains(got.On, `class="notereply" data-note="n3"`) {
		t.Fatalf("a reply must be its own block inside the box: %s", got.On)
	}
	// Agent layer off: the agent root AND the agent reply go, the user reply
	// under the visible root stays.
	if n := strings.Count(got.Off, "<tr "); n != 1 {
		t.Fatalf("agent layer OFF: %d boxes, want 1 (the user thread; the agent root goes): %s", n, got.Off)
	}
	if n := strings.Count(got.Off, "data-note="); n != 2 {
		t.Fatalf("agent layer OFF: %d note ids, want 2 (user root + user reply): %s", n, got.Off)
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

// TestNotesInertOnAComparisonJS pins I2. A comparison's table shows aHash ↔
// bHash, but a stored commit note resolves its OLD side against bHash^, so a
// note on a line the comparison removed would be filed against a base it was
// never taken on and deleted by the next sweep. openFile therefore marks the
// compare context `notes: false`, and the three surfaces that could expose a
// note — the query, the ◆ rows and the key gate — all read the same predicate.
func TestNotesInertOnAComparisonJS(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	fns := jsFunc(t, "files.js", "notesArmed") + "\n" + jsFunc(t, "files.js", "noteQuery") +
		"\n" + jsFunc(t, "files.js", "noteRowsHTML") + "\n" + jsFunc(t, "files.js", "noteBoxHTML")

	script := "const esc = (x) => String(x);\n" +
		"const URLSearchParams = globalThis.URLSearchParams;\n" +
		"const state = { diffCtx: null, notesAgentOff: false,\n" +
		"  notes: [{id:'n1', side:'new', line:3, source:'user', status:'active', summary:'s'}] };\n" + fns +
		"\nconst q = (ctx) => { state.diffCtx = ctx; const r = noteQuery(); return r === null ? null : r.toString(); };\n" +
		"const rows = (ctx) => { state.diffCtx = ctx; return noteRowsHTML('new', 3, 4); };\n" +
		"const commitCtx = {path:'a.go', rev:'beef', state:'commit'};\n" +
		"const compareCtx = {path:'a.go', rev:'beef', state:'commit', notes:false};\n" +
		"console.log(JSON.stringify({\n" +
		"  commit: q(commitCtx), compare: q(compareCtx), none: q(null),\n" +
		"  armedCommit: (state.diffCtx = commitCtx, notesArmed()),\n" +
		"  armedCompare: (state.diffCtx = compareCtx, notesArmed()),\n" +
		"  rowsCommit: rows(commitCtx), rowsCompare: rows(compareCtx),\n" +
		"}));\n"
	out, err := exec.Command(node, "-e", script).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var got struct {
		Commit, Compare, None     *string
		ArmedCommit, ArmedCompare bool
		RowsCommit, RowsCompare   string
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("decode %q: %v", out, err)
	}
	if got.Commit == nil || !strings.Contains(*got.Commit, "rev=beef") {
		t.Fatalf("a plain commit diff must still query notes, got %v", got.Commit)
	}
	if got.Compare != nil {
		t.Fatalf("a comparison must not query notes, got %q", *got.Compare)
	}
	if got.None != nil {
		t.Fatalf("no open diff must not query notes, got %q", *got.None)
	}
	if !got.ArmedCommit || got.ArmedCompare {
		t.Fatalf("notesArmed: commit %v compare %v, want true/false", got.ArmedCommit, got.ArmedCompare)
	}
	if got.RowsCommit == "" {
		t.Fatal("fixture broken: a commit diff must render its ◆ rows")
	}
	if got.RowsCompare != "" {
		t.Fatalf("a comparison must render no ◆ rows, got %q", got.RowsCompare)
	}
	// The predicate is only worth as much as the flag that feeds it: openFile
	// must mark a comparison's context inert. Pinned on the source rather than
	// under node because openFile is async over the DOM and the network.
	src, rerr := os.ReadFile(filepath.Join("static", "files.js"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.Contains(string(src), "notes: !cmp") {
		t.Fatal("openFile must mark a comparison's diffCtx inert for notes (notes: !cmp)")
	}
}

// readStaticSrc reads one shipped static file for a source assertion.
func readStaticSrc(t *testing.T, file string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("static", file))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestPreviewDiffArmsNotesInJS pins the preview lane in files.js: a preview's
// compare IS note-addressable (its new side is the source tip), it queries the
// gathered set through /api/preview/notes, it refuses an old-side anchor, and
// it paints a stale note under the preview's own word for it.
func TestPreviewDiffArmsNotesInJS(t *testing.T) {
	t.Parallel()
	src := readStaticSrc(t, "files.js")
	for _, want := range []string{
		"/api/preview/notes", // the preview note query exists
		"state.diffCtx.preview",
		"notes in a preview anchor on the new side", // the old-side refusal
		"outdated", // the stale class/word a preview uses
		"state.previewCounts",
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("files.js must contain %q", want)
		}
	}
}

// TestPreviewRowShowsTheNoteBadgeInJS: the sidebar row carries the preview's
// note total, the way a file row carries its ◆N.
func TestPreviewRowShowsTheNoteBadgeInJS(t *testing.T) {
	t.Parallel()
	src := readStaticSrc(t, "previews.js")
	if !strings.Contains(src, "e.notes") {
		t.Fatal("previews.js must paint the row's note total")
	}
	if !strings.Contains(src, "tip:") {
		t.Fatal("armPreview must record the source tip a preview note is written against")
	}
}

// TestOutdatedClassIsStyled: the preview's outdated note must be dimmed the
// same way a stale one is, or the class is invisible.
func TestOutdatedClassIsStyled(t *testing.T) {
	t.Parallel()
	css := readStaticSrc(t, "style.css")
	if !strings.Contains(css, ".outdated") {
		t.Fatal("style.css must style the outdated note class")
	}
}

// TestNotesEventReloadsPreviews: a note write changes a preview row's total,
// so the notes SSE source has to pull the previews list too.
func TestNotesEventReloadsPreviews(t *testing.T) {
	t.Parallel()
	src := readStaticSrc(t, "live.js")
	i := strings.Index(src, "fetchPreviews()")
	if i < 0 {
		t.Fatal("live.js must fetch previews")
	}
	line := src[strings.LastIndex(src[:i], "\n")+1 : i]
	if !strings.Contains(line, `want.has("notes")`) {
		t.Fatalf("the previews refetch must also fire on the notes source, got %q", line)
	}
}
