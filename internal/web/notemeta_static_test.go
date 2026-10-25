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
		`fitNoteTitle(meta ? badgeRunCols(n.display) : 0, parts.words, path,`,
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

// The viewer's boxes (an agent's temporary notes, a review's remarks) draw
// the same display.
func TestViewerNoteBoxDrawsMeta(t *testing.T) {
	t.Parallel()
	v := readStatic(t, "viewer.js")
	for _, want := range []string{
		`from "./notemeta.js"`,
		`frameClass(n.display)`,
		`<div class="vnote-title">${titleMetaHTML(n.display, esc)}${esc(noteBoxTitle(n))}</div>`,
		`metaLinesHTML(n.display, esc)`,
		`metaDetailsHTML(n.display, esc)`,
	} {
		if !strings.Contains(v, want) {
			t.Errorf("viewer.js lacks %s", want)
		}
	}
}

// The review view: the header's counts on a line of their own, each file's
// compact counts after its ◆N, the unplaced remarks' chips in Other notes.
func TestReviewViewDrawsMetaCounts(t *testing.T) {
	t.Parallel()
	idx, css := readStatic(t, "index.html"), readStatic(t, "style.css")
	f, r := readStatic(t, "files.js"), readStatic(t, "reviews.js")
	for _, c := range []struct{ src, file, want string }{
		{idx, "index.html", `<span id="files-counts" class="hidden"></span>`},
		{css, "style.css", `#files-counts.hidden { display: none; }`},
		{f, "files.js", `export function setFilesCounts(text)`},
		{f, "files.js", `setFilesCounts(""); // a review sets its counts`},
		{r, "reviews.js", `setFilesCounts(countsText(d.meta_counts))`},
		{r, "reviews.js", `countsRunHTML((d.meta_by_file || {})[path], esc)`},
		{r, "reviews.js", `badgeRunHTML(n.display, esc)`},
	} {
		if !strings.Contains(c.src, c.want) {
			t.Errorf("%s lacks %s", c.file, c.want)
		}
	}
}

// R9: the send panel keeps nothing ticked and no filter; meta shows only in an
// OPENED row, and the collapsed row's severity chip takes the badge colour.
func TestSendPanelDrawsMetaInOpenedRows(t *testing.T) {
	t.Parallel()
	p, rows := readStatic(t, "prsendpanel.js"), readStatic(t, "prsendrows.js")
	for _, want := range []string{
		`severityColour(r.display, r.severity)`,
		`<div class="pmeta">${badgeRunHTML(r.display, esc)}${metaLinesHTML(r.display, esc)}</div>`,
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prsendpanel.js lacks %s", want)
		}
	}
	if !strings.Contains(rows, `display: r.display || null`) {
		t.Errorf("prsendrows.js does not carry the row's display")
	}
	// R9/R12: what opens ticked, what a tick does and where the cursor starts
	// never read meta.
	for _, fn := range []struct{ file, name string }{{"prsendpanel.js", "tick"}, {"prsendpanel.js", "firstCandidate"}, {"prsendrows.js", "tickAllInGroup"}} {
		if body := jsFunc(t, fn.file, fn.name); strings.Contains(body, "display") {
			t.Errorf("%s %s reads meta: %s", fn.file, fn.name, body)
		}
	}
	if i := strings.Index(rows, "ticked: !r.skip && ticked.has(r.id)"); i < 0 {
		t.Errorf("prsendrows.js: a row's tick is no longer only the kept set")
	}
}

// Mark (spec §5.4, R12): a header and four rows in the diff note box's
// menu — never on a GitHub thread, hidden while note-edits is off — and a
// mark inside a review view re-reads the review's counts.
func TestDiffNoteMenuMarks(t *testing.T) {
	t.Parallel()
	f, r := readStatic(t, "files.js"), readStatic(t, "reviews.js")
	for _, want := range []string{
		`import { featureDisabled } from "./preflight.js";`,
		`{ header: "Mark" }`,
		`for (const s of ["agreed", "disagreed", "deferred"])`,
		`{ label: "clear", act: () => markNote(n, "") }`,
		`!String(rootId).startsWith("forge:") && n.source !== "forge" && !featureDisabled("note-edits")`,
		`noteWrite("mark", "/api/notes/mark", { id: n.id, status }, () => refreshReviewMeta())`,
	} {
		if !strings.Contains(f, want) {
			t.Errorf("files.js lacks %s", want)
		}
	}
	if !strings.Contains(r, `export async function refreshReviewMeta()`) {
		t.Errorf("reviews.js lacks refreshReviewMeta")
	}
	// Open + Delete only on Files-view note rows (user ruling).
	if i := strings.Index(r, "function notedRowMenu"); i >= 0 && strings.Contains(r[i:i+800], "/api/notes/mark") {
		t.Errorf("a Files-view note row must not offer Mark")
	}
}

// A mark refreshes a review's counts only while the review view is what the
// page shows: a review left for a commit keeps state.review, and its counts
// must not land under the commit's header.
func TestRefreshReviewMetaOnlyInTheReviewView(t *testing.T) {
	t.Parallel()
	body := jsFunc(t, "reviews.js", "refreshReviewMeta")
	if strings.Count(body, "reviewActive()") < 2 {
		t.Errorf("refreshReviewMeta must check reviewActive() before the read and after it:\n%s", body)
	}
}

// The note titles are fitted to the pane's width: a pane drag (not a window
// resize) must refit them.
func TestDiffNoteTitlesRefitOnPaneResize(t *testing.T) {
	t.Parallel()
	f := readStatic(t, "files.js")
	if !strings.Contains(f, `observe(diffBodyEl())`) || !strings.Contains(f, "rerenderDiffKeepingPlace(true)") {
		t.Errorf("files.js does not refit the note titles when #diff-body's width changes")
	}
}
