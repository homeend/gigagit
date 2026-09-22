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

// noteboxInline is notebox.js with its `export`s stripped, so the functions
// files.js imports are in scope for a painter extracted with jsFunc.
func noteboxInline(t *testing.T) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("static", "notebox.js"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(src), "export function", "function") + "\n"
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
		"\n" + jsFunc(t, "files.js", "notesArmed") + "\n" + jsFunc(t, "files.js", "globalNoteCtx")

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
		"const state = { diffCtx: {path:'f'}, notes: JSON.parse(process.argv[1]), notesAgentOff: false, noteCollapsed: new Set(['n4']) };\n" + noteboxInline(t) + fns +
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
	// n4 is in the collapse set: its ROW carries the class the CSS folds on,
	// and only its row.
	if strings.Count(got.On, " collapsed\"") != 1 || !strings.Contains(got.On, `stale agent collapsed" data-note="n4"`) {
		t.Fatalf("a collapsed thread must carry the class on its row: %s", got.On)
	}
	if strings.Count(got.On, `data-collapse="`) != 2 {
		t.Fatalf("every box title is a fold handle: %s", got.On)
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
		"\n" + jsFunc(t, "files.js", "noteRowsHTML") + "\n" + jsFunc(t, "files.js", "noteBoxHTML") +
		"\n" + jsFunc(t, "files.js", "globalNoteCtx")

	script := "const esc = (x) => String(x);\n" +
		"const URLSearchParams = globalThis.URLSearchParams;\n" +
		"const state = { diffCtx: null, notesAgentOff: false, noteCollapsed: new Set(),\n" +
		"  notes: [{id:'n1', side:'new', line:3, source:'user', status:'active', summary:'s'}] };\n" + noteboxInline(t) + fns +
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
	// A PREVIEW is a comparison that IS note-addressable (its new side is the
	// source tip), so the flag re-arms notes for it: `!cmp || !!prev`. Pinning
	// the bare "notes: !cmp" would pass with the preview half dropped, which
	// would silently make every preview inert.
	if !strings.Contains(string(src), "notes: !cmp || !!prev") {
		t.Fatal("openFile must mark a comparison inert but RE-ARM a preview (notes: !cmp || !!prev)")
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
		if !hasCodeLineWith(src, want) {
			t.Fatalf("files.js must contain %q in CODE", want)
		}
	}
}

// hasCodeLineWith reports whether some line of src carries tok OUTSIDE a
// comment. files.js explains these very rules in prose, so every token below
// also appears in a comment: a file-wide strings.Contains would still pass
// with the code deleted. Same line-scoping TestNotesEventReloadsPreviews does,
// generalised over a token list.
func hasCodeLineWith(src, tok string) bool {
	for _, line := range strings.Split(src, "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		if strings.Contains(line, tok) {
			return true
		}
	}
	return false
}

// TestPreviewAddNoteFallsForwardToTheNewSide: with nothing clicked, `c` in a
// preview must not be refused just because firstChangedRow landed on a pure
// DELETION row — the file still has an addressable new side. The fall-forward
// is only for the unclicked case: an explicit old-side click is still refused,
// because there the user named the line.
func TestPreviewAddNoteFallsForwardToTheNewSide(t *testing.T) {
	t.Parallel()
	add := jsFunc(t, "files.js", "addNotePrompt")
	// The marked row is read through activeDiff() (a stack has one per FILE),
	// so the "nothing was clicked" test is !ad.row.
	if !strings.Contains(add, "!ad.row") {
		t.Fatal("addNotePrompt must fall forward only when the row was NOT clicked (!ad.row)")
	}
	if !strings.Contains(add, "firstNewSideRow(scope)") {
		t.Fatal("addNotePrompt must fall forward to the first new-side row, inside the active file's scope")
	}
	fwd := jsFunc(t, "files.js", "firstNewSideRow")
	if !strings.Contains(fwd, `tr[data-no][data-side="new"]`) {
		t.Fatal("firstNewSideRow must look for a new-side diff row")
	}
	// It must WALK each change block, not just test its head: in the unified
	// layout a modification block's head is the del/old row, so a head-only
	// test finds nothing and falls through to the whole-table fallback — which
	// lands on a context line. See TestPreviewFirstNewSideRowWalksTheBlockJS.
	if !strings.Contains(fwd, "nextElementSibling") {
		t.Fatal("firstNewSideRow must walk a block's rows, not just its head")
	}
}

// TestPreviewFirstNewSideRowWalksTheBlockJS runs the SHIPPED firstNewSideRow
// (and the real diffChangeBlocks it calls) under node over a stub table, so
// the walk is exercised rather than merely grepped for.
//
// The regression this pins, seen in a browser probe: a modified file renders
// same/same/same, then del(old 4), add(new 4) — the block's HEAD is the del
// row, so testing heads alone found no new side and the whole-table fallback
// answered "new line 1", a context line, instead of the modified line 4.
func TestPreviewFirstNewSideRowWalksTheBlockJS(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	fns := jsFunc(t, "files.js", "firstNewSideRow") + "\n" + jsFunc(t, "files.js", "diffChangeBlocks")

	// A minimal row/table stub: classList.contains, dataset and the sibling
	// chain are the only DOM the two functions touch, and the two selectors
	// they pass are honoured by meaning ("not a note row", "a new-side row").
	stub := `
let ROWS = [];
const mkRow = (cls, side, no) => ({
  classList: { contains: (c) => cls.split(" ").includes(c) },
  dataset: side ? { side, no: String(no) } : {},
  nextElementSibling: null,
});
const setTable = (specs) => {
  ROWS = specs.map(([cls, side, no]) => mkRow(cls, side, no));
  ROWS.forEach((r, i) => (r.nextElementSibling = ROWS[i + 1] || null));
};
const $ = () => ({
  querySelectorAll: () => ROWS.filter((r) => !r.classList.contains("note")),
  querySelector: () => ROWS.find((r) => r.dataset.side === "new" && Number(r.dataset.no)) || null,
});
`
	script := stub + fns + `
const at = (specs) => { setTable(specs); return firstNewSideRow(); };
console.log(JSON.stringify({
  // The probe's own file: the modification block's head is the del row.
  unified: at([["same","new",1],["same","new",2],["same","new",3],
               ["del","old",4],["add","new",4],["same","new",5]]),
  // A ◆ note row rides inside the block and must not end the walk.
  withNote: at([["same","new",1],["del","old",4],["note",null,0],["add","new",4]]),
  // Deletion-only change, but the file has context: the fallback answers.
  delOnly: at([["same","new",1],["del","old",2],["same","new",2]]),
  // No new side anywhere: null, so the caller keeps the refusal.
  noNewSide: at([["del","old",1],["del","old",2]]),
  // Side-by-side layout: the changed row already carries its new side.
  split: at([["same","new",1],["change","new",2],["same","new",3]]),
}));
`
	out, err := exec.Command(node, "-e", script).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	type row struct {
		Side string
		No   int
	}
	var got struct{ Unified, WithNote, DelOnly, NoNewSide, Split *row }
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("decode %q: %v", out, err)
	}
	if got.Unified == nil || got.Unified.Side != "new" || got.Unified.No != 4 {
		t.Fatalf("the unified del/add pair must anchor on new line 4, got %+v", got.Unified)
	}
	if got.WithNote == nil || got.WithNote.No != 4 {
		t.Fatalf("a note row inside the block must not end the walk, got %+v", got.WithNote)
	}
	if got.DelOnly == nil || got.DelOnly.No != 1 {
		t.Fatalf("a deletion-only change falls back to the file's first new-side row, got %+v", got.DelOnly)
	}
	if got.NoNewSide != nil {
		t.Fatalf("a file with no new side must answer null (the caller refuses), got %+v", got.NoNewSide)
	}
	if got.Split == nil || got.Split.No != 2 {
		t.Fatalf("a side-by-side changed row anchors on its own new side, got %+v", got.Split)
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
// same way a stale one is, or the class is invisible. Pinned on the RULE, not
// on the bare class name: ".outdated" appears in the comment above it, and a
// rule that dims with `opacity` would fade the whole box (border included)
// instead of recolouring it the way .stale does.
func TestOutdatedClassIsStyled(t *testing.T) {
	t.Parallel()
	css := readStaticSrc(t, "style.css")
	i := strings.Index(css, ".notebox.outdated {")
	if i < 0 {
		t.Fatal("style.css must carry a .notebox.outdated rule")
	}
	rule := css[i : i+strings.Index(css[i:], "}")]
	if !strings.Contains(rule, "var(--dim)") {
		t.Fatalf(".notebox.outdated must dim through var(--dim), got %q", rule)
	}
	if strings.Contains(rule, "opacity") {
		t.Fatalf(".notebox.outdated must recolour, not fade the whole box, got %q", rule)
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

// TestForgeNoteBoxesJS: a pull request's review threads ride the same painter.
// They are read-only boxes of their own class, the agent-layer switch never
// hides one (whatever its source says), a whole-file thread spans the row and
// leads the table, and the read is keyed on the PR NUMBER alone.
func TestForgeNoteBoxesJS(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	fns := jsFunc(t, "files.js", "notesArmed") + "\n" + jsFunc(t, "files.js", "noteQuery") + "\n" +
		jsFunc(t, "files.js", "noteRowsHTML") + "\n" + jsFunc(t, "files.js", "noteBoxHTML") + "\n" +
		jsFunc(t, "files.js", "fileNoteRowsHTML") + "\n" + jsFunc(t, "files.js", "globalNoteCtx")
	script := "const esc = (x) => String(x);\n" +
		"const state = { notesAgentOff: true, noteCollapsed: new Set(['forge:C3']),\n" +
		"  diffCtx: {path:'a.go', rev:'beef', state:'commit', preview:{source:'alice:feat', target:'main', pr:7}},\n" +
		"  notes: [\n" +
		"   {id:'forge:C1', side:'new', line:3, source:'forge', author:'carol', status:'active', read_only:true, summary:'why'},\n" +
		"   {id:'forge:C3', side:'new', line:3, source:'forge', author:'dave', status:'active', read_only:true, resolved:true, summary:'done'},\n" +
		"   {id:'forge:C4', side:'new', line:0, source:'forge', author:'erin', status:'active', read_only:true, file_level:true, summary:'whole file'},\n" +
		"   {id:'n9', side:'new', line:3, source:'agent', status:'active', summary:'bot'}] };\n" +
		noteboxInline(t) + fns +
		"\nconsole.log(JSON.stringify({ line: noteRowsHTML('new', 3, 4), file: fileNoteRowsHTML(4), query: noteQuery().toString() }));\n"
	out, err := exec.Command(node, "-e", script).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var got struct{ Line, File, Query string }
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("decode %q: %v", out, err)
	}
	if n := strings.Count(got.Line, "<tr "); n != 2 || strings.Contains(got.Line, "bot") {
		t.Fatalf("agent layer OFF must keep both forge threads and drop the agent note: %s", got.Line)
	}
	if !strings.Contains(got.Line, `class="notebox forge"`) || !strings.Contains(got.Line, "review · carol") {
		t.Fatalf("a forge thread is its own kind of box: %s", got.Line)
	}
	if !strings.Contains(got.Line, `forge collapsed" data-note="forge:C3"`) || !strings.Contains(got.Line, "· resolved") {
		t.Fatalf("the resolved thread renders folded and says so: %s", got.Line)
	}
	if strings.Contains(got.Line, "whole file") {
		t.Fatalf("a whole-file thread hangs off no line: %s", got.Line)
	}
	if !strings.Contains(got.File, `<td class="note" colspan="4">`) || strings.Contains(got.File, "note-gap") ||
		!strings.Contains(got.File, "a.go (file)") || !strings.Contains(got.File, " filenote") {
		t.Fatalf("a whole-file thread spans the row: %s", got.File)
	}
	if got.Query != "path=a.go&n=7&rev=beef&state=commit" {
		t.Fatalf("a PR's notes are asked for by NUMBER: %q", got.Query)
	}
}

// markdownInline is markdown.js with its exports stripped, for the node
// harnesses that inline files.js functions.
func markdownInline(t *testing.T) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("static", "markdown.js"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(src), "export function", "function") + "\n"
}

// A forge note is markdown and arrives parsed: its trees are painted. A note
// written HERE carries no tree and is shown exactly as typed — markdown
// markers, angle brackets and all — so the feature can never restyle (or
// un-escape) the user's own text.
func TestForgeNoteBoxesRenderMarkdownJS(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	fns := jsFunc(t, "files.js", "notesArmed") + "\n" + jsFunc(t, "files.js", "noteRowsHTML") + "\n" + jsFunc(t, "files.js", "noteBoxHTML") + "\n" + jsFunc(t, "files.js", "globalNoteCtx")
	script := `const esc = (s) => String(s).replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));
const list = { blocks: [{ k: "list", items: [{ blocks: [{ k: "p", in: [{ k: "text", t: "shorter" }] }] }] }] };
const state = { notesAgentOff: false, noteCollapsed: new Set(),
  diffCtx: { path: "a.go", rev: "beef", state: "commit", preview: { source: "alice:feat", target: "main", pr: 7 } },
  notes: [
   { id: "forge:C1", side: "new", line: 3, source: "forge", author: "carol", status: "active", read_only: true,
     summary: "Rename x", summary_md: [{ k: "text", t: "Rename " }, { k: "code", t: "x" }], rationale: "- shorter", md: list,
     replies: [{ id: "forge:C2", source: "forge", author: "dave", status: "active", read_only: true,
       summary: "ok", summary_md: [{ k: "strong", in: [{ k: "text", t: "ok" }] }] }] },
   { id: "forge:C5", side: "new", line: 3, source: "forge", author: "erin", status: "active", read_only: true,
     summary: "<old> wire", rationale: "no **tree** sent" },
   { id: "n1", side: "new", line: 3, source: "user", status: "active", summary: "**mine** <b>", rationale: "- typed\n- as is",
     summary_md: [], md: null }] };
` + noteboxInline(t) + markdownInline(t) + fns + `
console.log(JSON.stringify({ html: noteRowsHTML("new", 3, 4) }));
`
	out, err := exec.Command(node, "-e", script).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var got struct{ HTML string }
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("decode %q: %v", out, err)
	}
	for _, want := range []string{
		`<div class="notesum">Rename <code class="md-ic">x</code></div>`,
		`<div class="notetext md"><ul class="md-list"><li><p>shorter</p></li></ul></div>`,
		`<div class="notesum">↳ dave: <strong>ok</strong></div>`,
		// A forge note from a server that sent no tree falls back to the text.
		`<div class="notesum">&lt;old&gt; wire</div>`, `<div class="notetext">no **tree** sent</div>`,
		// The user's own note: never parsed, never unescaped.
		`<div class="notesum">**mine** &lt;b&gt;</div>`, "<div class=\"notetext\">- typed\n- as is</div>",
	} {
		if !strings.Contains(got.HTML, want) {
			t.Errorf("missing %s\n%s", want, got.HTML)
		}
	}
	if n := strings.Count(got.HTML, "notetext md"); n != 1 {
		t.Errorf("%d rendered rationales, want only the forge note that carried a tree\n%s", n, got.HTML)
	}
}
