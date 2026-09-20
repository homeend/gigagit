package tui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// forgeRoot is a forge review thread's root on the new side of line (0 = a
// file-level comment).
func forgeRoot(id string, line int, summary string, resolved bool) domain.ResolvedNote {
	r := rootNote(model.ForgeNoteIDPrefix+id, line, summary, "", model.NoteSourceForge, model.NoteActive)
	r.Note.Author = "octocat"
	r.Note.Created = time.Now().Add(-49 * time.Hour)
	if line == 0 {
		r.Range, r.Note.Range = [2]int{}, [2]int{}
	}
	if resolved {
		r.Note.Tags = []string{model.NoteTagResolved}
	}
	return r
}

// forgeNotedModel is notedModel plus a forge thread on line 5 (beside the
// user's note) and a file-level one.
func forgeNotedModel(t *testing.T) Model {
	t.Helper()
	m := notedModel(t)
	v := m.diffLayer()
	v.notes = append(v.notes, forgeRoot("C1", 5, "rename this", false), forgeRoot("C9", 0, "should this file exist?", false))
	v.relayout(0)
	return m
}

func TestForgeNoteBoxTitle(t *testing.T) {
	t.Parallel()
	m := forgeNotedModel(t)
	v := m.diffLayer()
	if got := v.noteBoxTitle(forgeRoot("C1", 5, "x", false)); got != "review · octocat · 2d ago · a/b.go R5" {
		t.Fatalf("title = %q", got)
	}
	if got := v.noteBoxTitle(forgeRoot("C1", 5, "x", true)); !strings.HasSuffix(got, " · resolved") {
		t.Fatalf("resolved title = %q", got)
	}
	if got := v.noteBoxTitle(forgeRoot("C9", 0, "x", false)); got != "review · octocat · 2d ago · a/b.go (file)" {
		t.Fatalf("file-level title = %q", got)
	}
}

func TestForgeFileLevelNoteAnchorsOnTheFirstLine(t *testing.T) {
	t.Parallel()
	m := forgeNotedModel(t)
	v := m.diffLayer()
	li, visible := v.noteAnchorLine(forgeRoot("C9", 0, "x", false))
	if li != 0 || !visible {
		t.Fatalf("file-level anchor = %d visible=%v, want the first line", li, visible)
	}
	// A STORED note with a zero range is malformed, not file-level: it stays unanchored.
	bad := rootNote("n9", 0, "x", "", model.NoteSourceUser, model.NoteActive)
	bad.Range = [2]int{}
	if li, _ := v.noteAnchorLine(bad); li != -1 {
		t.Fatalf("a stored zero-range note anchored at %d", li)
	}
	byLine, _ := v.noteRowIndex()
	found := false
	for _, nl := range byLine[0] {
		if nl.rootID == "forge:C9" {
			found = true
			if nl.side != model.NoteSideNew {
				t.Fatalf("file-level rows sit in the right pane, got side %q", nl.side)
			}
		}
	}
	if !found {
		t.Fatal("the file-level box is not laid out under the first line")
	}
}

func TestForgeNotesAreReadOnlyInTheDiff(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"E", "R"} {
		m := forgeNotedModel(t)
		v := m.diffLayer()
		v.notes = []domain.ResolvedNote{forgeRoot("C1", 5, "rename this", false)}
		v.relayout(0)
		v.setCursorLine(4, m.diffBodyRows())
		nm, _ := v.update(m, synthKey(key))
		if _, open := nm.topLayer().(*notePopup); open {
			t.Fatalf("%s opened a form on a forge comment", key)
		}
		if !strings.Contains(nm.diffNotice, "read-only") {
			t.Fatalf("%s: the diff's own notice box must say it (the status bar is hidden there), got %q", key, nm.diffNotice)
		}
		if !strings.Contains(nm.statusMsg, "read-only") {
			t.Fatalf("%s: status = %q", key, nm.statusMsg)
		}
	}
	// The . menu offers no Delete for it either; with a user's note on the same
	// line the chooser still reaches the user's note.
	m := forgeNotedModel(t)
	v := m.diffLayer()
	v.notes = []domain.ResolvedNote{forgeRoot("C1", 5, "rename this", false)}
	v.relayout(0)
	v.setCursorLine(4, m.diffBodyRows())
	for _, r := range m.noteMenuRows() {
		if r.id == "note-delete" || r.id == "note-edit" || r.id == "note-reply" {
			t.Fatalf("menu offers %q on a forge-only line", r.id)
		}
	}
}

func TestRemoveAllIgnoresForgeNotes(t *testing.T) {
	t.Parallel()
	m := forgeNotedModel(t)
	v := m.diffLayer()
	f := forgeRoot("C1", 5, "x", false)
	f.Note.Address.Commit = "tip"
	v.notes = []domain.ResolvedNote{f}
	if diffHasTipNotes(v, "tip") {
		t.Fatal("a forge thread is not a removable tip note")
	}
}

func TestNotesListMarksForgeThreads(t *testing.T) {
	t.Parallel()
	es := noteListEntries([]domain.ResolvedNote{forgeRoot("C1", 5, "rename this", true), forgeRoot("C9", 0, "file", false)})
	if !es[0].forge || !es[0].resolved || es[0].anchor != "new:5" {
		t.Fatalf("entry 0 = %+v", es[0])
	}
	if es[1].anchor != "file" {
		t.Fatalf("file-level anchor label = %q", es[1].anchor)
	}
	if line := es[0].line(100); !strings.Contains(line, "review") || !strings.Contains(line, "resolved") {
		t.Fatalf("list row = %q", line)
	}
}

// A forge thread is markdown: its box rows come from the parsed tree (markers
// gone, styles on the mask, wrapped to the box). A LOCAL note with the very
// same text is shown as typed and carries no mask at all.
func TestForgeNoteBodyRendersMarkdown(t *testing.T) {
	t.Parallel()
	fence := "```"
	forge := domain.ResolvedNote{
		Note: model.Note{ID: "forge:C1", Source: model.NoteSourceForge, Side: model.NoteSideNew,
			Summary: "Rename x to y", Rationale: "- it is **shorter** and this item is long enough to wrap\n\n" + fence + "suggestion\ny := 1\n" + fence},
		SummarySrc: "Rename `x` to **y**",
	}
	const innerW = 30
	rows := noteBodyLines(forge, "forge:C1", 0, innerW, false)
	var texts []string
	for _, nl := range rows {
		texts = append(texts, nl.text)
		if len(nl.cls) != len([]rune(nl.text)) {
			t.Fatalf("mask/text mismatch on %q", nl.text)
		}
		if lipgloss.Width(nl.text) > innerW {
			t.Fatalf("row wider than the box: %q", nl.text)
		}
	}
	joined := strings.Join(texts, "\n")
	if rows[0].kind != noteRowSummary || rows[0].text != "Rename x to y" || strings.ContainsAny(joined, "*`") {
		t.Fatalf("rows:\n%s", joined)
	}
	for _, want := range []string{"• it is shorter and this item", "\n  ", "suggestion", "  y := 1"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in:\n%s", want, joined)
		}
	}
	if c := rows[0].cls[strings.Index(rows[0].text, "x")]; c != mdCode {
		t.Errorf("summary code class %d", c)
	}

	reply := forge
	reply.Note.Author = "carol"
	rr := noteBodyLines(reply, "forge:C1", 1, innerW, false)
	if !strings.HasPrefix(rr[0].text, "  ↳ carol: Rename x") {
		t.Errorf("reply head: %q", rr[0].text)
	}

	label := forge
	label.Note.Summary, label.SummarySrc = "suggestion", ""
	label.Note.Rationale = fence + "suggestion\ny := 1\n" + fence
	lr := noteBodyLines(label, "forge:C1", 0, innerW, false)
	if len(lr) != 2 || lr[0].text != "suggestion" || lr[0].kind != noteRowSummary || lr[1].text != "  y := 1" {
		t.Errorf("a label summary is plain and its block's caption is not repeated: %+v", lr)
	}

	mine := domain.ResolvedNote{Note: model.Note{ID: "n1", Source: model.NoteSourceUser, Side: model.NoteSideNew,
		Summary: "Rename `x` to **y**", Rationale: "- it is **shorter**"}}
	for _, nl := range noteBodyLines(mine, "n1", 0, innerW, false) {
		if nl.cls != nil {
			t.Fatalf("a local note is never styled: %q", nl.text)
		}
	}
	if got := noteBodyLines(mine, "n1", 0, innerW, false); got[0].text != "Rename `x` to **y**" || got[1].text != "- it is **shorter**" {
		t.Fatalf("a local note is shown as typed: %q / %q", got[0].text, got[1].text)
	}

	// Painted: the strong run is bold, and the cell is exactly the pane wide.
	const paneW = 40
	var strong noteLine
	for _, nl := range rows {
		if strings.Contains(nl.text, "shorter") {
			strong = nl
		}
	}
	cell := noteRowCells(strong, paneW)
	if w := lipgloss.Width(cell); w != 2*paneW+1 {
		t.Errorf("painted row is %d wide, want %d", w, 2*paneW+1)
	}
	if !strings.Contains(ansi.Strip(cell), "shorter") {
		t.Errorf("painted row lost its text: %q", cell)
	}
}
