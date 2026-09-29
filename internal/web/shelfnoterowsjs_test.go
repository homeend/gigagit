package web

import (
	"regexp"
	"strings"
	"testing"
)

// TestElideNoteSummaryJS pins the web port of the TUI's elideNoteSummary to
// the TUI's own outputs (captured from internal/tui/shelf_note_view.go): the
// "(branch)" group stays whole, the words before the path stay whole while the
// path keeps a few columns, and the path is cut in its MIDDLE.
func TestElideNoteSummaryJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "core.js", "function runes(s) {", "// --- end path elision ---", `
const s = "Recycled from /very/long/prefix/that/does/not/fit/at/all/wt-a (feature/x)";
const r = [];
for (const n of [10, 16, 20, 30, 40, 50, 80]) r.push(n + "=" + elideNoteSummary(s, n));
r.push(elideNoteSummary("short (x)", 30));
r.push(elideNoteSummary("no/group/here/at/all/file.go", 12));
console.log(r.join("\n"));
`)
	want := strings.Join([]string{
		"10=…/all/wt-a",
		"16=wt-a (feature/x)",
		"20=…/wt-a (feature/x)",
		"30=…/fit/at/all/wt-a (feature/x)",
		"40=Recycled from …/at/all/wt-a (feature/x)",
		"50=Recycled from /very/long/…/at/all/wt-a (feature/x)",
		"80=Recycled from /very/long/prefix/that/does/not/fit/at/all/wt-a (feature/x)",
		"short (x)",
		"no/…/file.go",
	}, "\n")
	if out != want {
		t.Errorf("elideNoteSummary drifted from the TUI:\n got:\n%s\nwant:\n%s", out, want)
	}
}

// TestShelfNoteRowsWiring: a shelved entry with notes carries them into its
// frozen compare, the file list paints them as clickable rows above the files
// (outside the file cursor's index space), a row opens that ONE note, and the
// stacked view leads with a prose block per note.
func TestShelfNoteRowsWiring(t *testing.T) {
	t.Parallel()
	sidebar := readStatic(t, "sidebar.js")
	files := readStatic(t, "files.js")
	stack := readStatic(t, "stackview.js")
	notes := readStatic(t, "shelfnotes.js")
	core := readStatic(t, "core.js")
	checks := []struct {
		name, src, pattern string
	}{
		{"core exports elideNoteSummary", core, `export \{[^}]*\belideNoteSummary\b`},
		{"openShelfEntry fetches the entry's notes", sidebar, `(?s)async function openShelfEntry.*?/api/shelf/notes\?id=`},
		{"the compare carries them", files, `shelfNotes:\s*body\.shelf_notes`},
		{"note rows are data-note, not data-i", files, `data-note="`},
		{"the Notes heading", files, `class="sect[^"]*">Notes<`},
		{"note rows cut the summary with elideNoteSummary", files, `elideNoteSummary\(`},
		{"files.js imports elideNoteSummary", files, `import \{[^}]*\belideNoteSummary\b[^}]*\} from "\./core\.js"`},
		{"a click opens that one note", files, `openShelfNotes\([^)]*dataset\.note`},
		{"files.js imports openShelfNotes", files, `import \{[^}]*\bopenShelfNotes\b[^}]*\} from "\./shelfnotes\.js"`},
		{"openShelfNotes takes a note id", notes, `async function openShelfNotes\(e, label, noteId\)`},
		{"shelfnotes exports noteHTML", notes, `export \{[^}]*\bnoteHTML\b`},
		{"the stack leads with the notes", stack, `stk-notes`},
		{"the first file lands on the top, notes in sight", stack, `k === 0 && document\.querySelector\("#diff-body \.stk-notes"\)\) pane\.scrollTop = 0`},
		{"stackview imports noteHTML", stack, `import \{[^}]*\bnoteHTML\b[^}]*\} from "\./shelfnotes\.js"`},
		// A frozen member has no status of its own: "-" fills the status
		// column, so the files do not look indented against the Notes rows.
		{"frozen members show - as their status", sidebar, `(?s)async function openShelfEntry.*?status: "-"`},
		// The popup fits its content (up to the window) and WRAPS the note
		// text: an overlay scrollbar (Firefox on Windows 11) paints over the
		// last line of a sideways-scrolling <pre>.
		{"the popup sizes to its content", notes, `#gg-shelf-notes \.box \{[^}]*width: max-content;[^}]*max-width: 92vw;`},
		{"the popup wraps its note text", notes, `#gg-shelf-notes pre \{[^}]*white-space: pre-wrap;`},
		{"the notes never shrink under the height cap", notes, `#gg-shelf-notes \.notes > \* \{ flex-shrink: 0; \}`},
	}
	for _, c := range checks {
		if !regexp.MustCompile(c.pattern).MatchString(c.src) {
			t.Errorf("%s: /%s/ not found", c.name, c.pattern)
		}
	}
}
