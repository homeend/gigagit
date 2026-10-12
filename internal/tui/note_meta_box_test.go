package tui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notemeta"
	"github.com/homeend/gigagit/internal/syntax"
)

// declaredIssue is an agent note carrying the skills' default kind/severity/
// intent declaration and one undeclared (stored-only) key.
func declaredIssue() (model.Note, notemeta.Display) {
	d := notemeta.Display{
		"kind":     {Show: []string{"badge"}, Order: 1, Values: map[string]notemeta.Value{"issue": {Color: "red", Audience: "author"}}},
		"severity": {Show: []string{"badge", "frame"}, Order: 2, Style: notemeta.StyleText, Values: map[string]notemeta.Value{"high": {Color: "red"}}},
		"intent":   {Show: []string{"line"}, Order: 4},
	}
	n := model.Note{ID: "n1", Source: model.NoteSourceAgent, Author: "Claude Code", Side: model.NoteSideNew, Summary: "cached forever",
		Meta:    []notemeta.KV{{Key: "kind", Value: "issue"}, {Key: "severity", Value: "high"}, {Key: "intent", Value: "checked the cache key"}, {Key: "model", Value: "opus"}},
		Display: d}
	return n, d
}

func kinds(rows []noteLine) []noteRowKind {
	out := make([]noteRowKind, len(rows))
	for i, r := range rows {
		out[i] = r.kind
	}
	return out
}

func TestNoteBoxMetaRows(t *testing.T) {
	t.Parallel()
	n, _ := declaredIssue()
	v := &diffView{noteAddr: model.FileAddress{Path: "src/config/configuration.ts"}}
	rows := v.noteBoxLines(domain.ResolvedNote{Note: n, Range: [2]int{12, 14}}, 70, v)
	top := rows[0]
	if top.badges != " issue  high  "+i18n.T("→ author") {
		t.Fatalf("badges = %q", top.badges)
	}
	if strings.Contains(top.text, "kind:") || strings.Contains(top.text, "issue") {
		t.Fatalf("meta leaked into the title text: %q", top.text)
	}
	if top.path != "src/config/configuration.ts" || top.metaFrame != "red" {
		t.Fatalf("path %q frame %q", top.path, top.metaFrame)
	}
	var meta []string
	for _, r := range rows {
		if r.kind == noteRowMeta {
			meta = append(meta, r.text)
		}
		if r.metaFrame != "red" {
			t.Fatalf("row %v lacks the frame colour", r.kind)
		}
	}
	want := []string{"intent: checked the cache key", i18n.T("meta: %s", "model: opus")}
	if !reflect.DeepEqual(meta, want) {
		t.Fatalf("meta rows = %q", meta)
	}
	// Order: top, blank, the line row, the summary … the details row, blank, bottom.
	if rows[2].kind != noteRowMeta || rows[3].kind != noteRowSummary || rows[len(rows)-3].kind != noteRowMeta {
		t.Fatalf("order = %v", kinds(rows))
	}
}

func TestNoteBoxUndeclaredMetaIsLines(t *testing.T) {
	t.Parallel()
	n := model.Note{ID: "review:r1:0", Source: model.NoteSourceAgent, Author: "Claude Code", Summary: "name it",
		Meta: []notemeta.KV{{Key: "severity", Value: "low"}}}
	v := &diffView{noteAddr: model.FileAddress{Path: "big.txt"}}
	rows := v.noteBoxLines(domain.ResolvedNote{Note: n, Range: [2]int{5, 5}}, 70, v)
	if rows[0].badges != "" || rows[0].metaFrame != "" || strings.Contains(rows[0].text, "severity") {
		t.Fatalf("top = %+v", rows[0])
	}
	if rows[2].kind != noteRowMeta || rows[2].text != "severity: low" {
		t.Fatalf("line row = %+v", rows[2])
	}
}

func TestNoteBoxNoMetaUnchanged(t *testing.T) {
	t.Parallel()
	n := model.Note{ID: "n2", Source: model.NoteSourceUser, Author: "MR", Summary: "why?"}
	v := &diffView{noteAddr: model.FileAddress{Path: "a.txt"}}
	rows := v.noteBoxLines(domain.ResolvedNote{Note: n, Range: [2]int{1, 1}}, 70, v)
	for _, r := range rows {
		if r.kind == noteRowMeta || r.badges != "" || r.metaFrame != "" {
			t.Fatalf("a note without meta grew meta rows: %+v", r)
		}
	}
	// The painted top row is exactly today's: one Render of the whole rule.
	f := st().noteFrameUser
	title := "note · MR · a.txt R1"
	want := f.Render("╭") + f.Render("─ "+title+" "+strings.Repeat("─", 40-4-lipgloss.Width(title)-1)+"╮")
	if got := noteBoxCell(rows[0], 40); got != want {
		t.Fatalf("top cell = %q, want %q", got, want)
	}
}

func TestNoteBoxReplyUsesRootDisplay(t *testing.T) {
	t.Parallel()
	root, _ := declaredIssue()
	rep := model.Note{ID: "n1r", ParentID: "n1", Source: model.NoteSourceAgent, Author: "Codex", Summary: "agreed", Meta: []notemeta.KV{{Key: "kind", Value: "issue"}}}
	v := &diffView{noteAddr: model.FileAddress{Path: "a.txt"}}
	rows := v.noteBoxLines(domain.ResolvedNote{Note: root, Range: [2]int{1, 1}, Replies: []domain.ResolvedNote{{Note: rep}}}, 70, v)
	var badge *noteLine
	for i := range rows {
		if rows[i].id == "n1r" && rows[i].kind == noteRowMeta && rows[i].cls != nil {
			badge = &rows[i]
		}
	}
	if badge == nil {
		t.Fatalf("no reply badge row: %v", kinds(rows))
	}
	at := len([]rune(badge.text[:strings.Index(badge.text, "issue")]))
	if badge.cls[at] != metaClass("red", notemeta.StyleFilled) {
		t.Fatalf("reply badge = %q %v", badge.text, badge.cls)
	}
	if strings.Contains(rows[0].badges, "Codex") {
		t.Fatal("a reply's meta must not reach the root's title")
	}
}

func TestFitNoteTitle(t *testing.T) {
	t.Parallel()
	b, cls := " issue ", []syntax.Class{1, 1, 1, 1, 1, 1, 1}
	text, path := "review · Claude Code · packages/babel-core/src/config/files/configuration.ts R12-14", "packages/babel-core/src/config/files/configuration.ts"
	if gb, _, gt := fitNoteTitle(b, cls, text, path, 200); gb != b || gt != text {
		t.Fatal("a fitting title changed")
	}
	// Too wide: the path loses its middle first, the file name stays.
	gb, _, gt := fitNoteTitle(b, cls, text, path, 60)
	if gb != b || !strings.Contains(gt, "configuration.ts R12-14") || !strings.HasPrefix(gt, "review · Claude Code · ") || lipgloss.Width(gb)+3+lipgloss.Width(gt) > 60 {
		t.Fatalf("elided = %q %q", gb, gt)
	}
	// Narrower: today's parts are cut, the badges kept.
	gb, _, gt = fitNoteTitle(b, cls, text, path, 20)
	if gb != b || gt == "" || lipgloss.Width(gb)+3+lipgloss.Width(gt) > 20 {
		t.Fatalf("cut = %q %q", gb, gt)
	}
	// No badges: the path still goes first.
	_, _, gt = fitNoteTitle("", nil, text, path, 50)
	if !strings.Contains(gt, "configuration.ts R12-14") || lipgloss.Width(gt) > 50 {
		t.Fatalf("no badges = %q", gt)
	}
}

func TestFitNoteTitleNarrow(t *testing.T) {
	t.Parallel()
	b, cls := " issue  high ", make([]syntax.Class, 13)
	for _, room := range []int{-3, 0, 1, 4, 7, 13, 14} {
		gb, gc, gt := fitNoteTitle(b, cls, "review · a.txt R1", "a.txt", room)
		w := lipgloss.Width(gb) + lipgloss.Width(gt)
		if gb != "" && gt != "" {
			w += 3
		}
		if w > max(room, 0) || len(gc) != len([]rune(gb)) {
			t.Fatalf("room %d: %q %q (mask %d)", room, gb, gt, len(gc))
		}
		if room > 0 && room <= 14 && gt != "" {
			t.Fatalf("room %d: today's parts must go before the badges: %q", room, gt)
		}
	}
	for _, w := range []int{0, 3, 4, 10} {
		cell := noteBoxCell(noteLine{kind: noteRowTop, text: "review · a.txt R1", badges: b, badgeCls: cls, path: "a.txt"}, w)
		if lipgloss.Width(cell) > max(w, 4) {
			t.Fatalf("pane %d: cell %d wide: %q", w, lipgloss.Width(cell), cell)
		}
	}
}

func TestNoteBoxCellMetaFrameAndStale(t *testing.T) {
	t.Parallel()
	nl := noteLine{kind: noteRowBlank, metaFrame: "red", agent: true}
	red := st().metaStyle(lipgloss.NewStyle(), metaClass("red", notemeta.StyleText)).Render("│")
	if !strings.Contains(noteBoxCell(nl, 20), red) {
		t.Fatal("the declared frame colour is not painted")
	}
	nl.stale = true
	if noteBoxCell(nl, 20) != noteBoxCell(noteLine{kind: noteRowBlank, stale: true, agent: true}, 20) {
		t.Fatal("a stale box must stay dim (dim wins)")
	}
}

func TestCollapsedRowCarriesBadges(t *testing.T) {
	t.Parallel()
	n, _ := declaredIssue()
	v := &diffView{collapsed: map[string]bool{"n1": true}}
	nl := v.collapsedNoteLine(domain.ResolvedNote{Note: n})
	if !strings.HasPrefix(nl.badges, " issue ") || strings.Contains(nl.text, "issue") || nl.metaFrame != "red" {
		t.Fatalf("collapsed = %+v", nl)
	}
	if !strings.Contains(ansiStrip(noteBoxCell(nl, 60)), "issue") {
		t.Fatal("the collapsed row does not paint its badges")
	}
}

func TestOpenFileNoteBoxMeta(t *testing.T) {
	t.Parallel()
	_, d := declaredIssue()
	n := agentdocs.Note{ID: "t1", Author: "Claude Code", Start: 3, End: 3, Summary: "cached forever",
		Meta: []notemeta.KV{{Key: "kind", Value: "issue"}, {Key: "intent", Value: "checked the cache key"}}, Display: d,
		Added: []notemeta.Added{{Key: notemeta.StatusKey, Value: "disagreed", By: "MR"}}}
	rows := noteBoxLines(n, 60, 0)
	if !strings.Contains(rows[0].badges, "issue") || !strings.Contains(rows[0].badges, i18n.T("disagreed")) {
		t.Fatalf("badges = %q", rows[0].badges)
	}
	var texts []string
	for _, r := range rows {
		if r.kind == noteRowMeta {
			texts = append(texts, r.text)
		}
	}
	want := []string{"intent: checked the cache key", i18n.T("added: %s", "status: disagreed (MR)")}
	if !reflect.DeepEqual(texts, want) {
		t.Fatalf("meta rows = %q", texts)
	}
}

func TestRemarkBoxesKeepMeta(t *testing.T) {
	t.Parallel()
	n, d := declaredIssue()
	n.Added = []notemeta.Added{{Key: "status", Value: "agreed", By: "MR"}}
	got := remarkBoxes([]domain.ResolvedNote{{Note: n, Range: [2]int{1, 1}}})
	if len(got[0].Meta) != len(n.Meta) || len(got[0].Display) != len(d) || len(got[0].Added) != 1 {
		t.Fatalf("remark box = %+v", got[0])
	}
}

// A 120-column terminal side by side (paneW 59) with three badges and the
// marker: the file name and the line survive; the kind/author words go first.
func TestFitNoteTitleKeepsFileAndLine(t *testing.T) {
	t.Parallel()
	b := " issue  high  disagreed  → author"
	cls := make([]syntax.Class, len([]rune(b)))
	path := "packages/babel-core/src/config/files/configuration.ts"
	text := "review · Claude Code · " + path + " R12"
	for _, pane := range []int{59, 70} {
		gb, _, gt := fitNoteTitle(b, cls, text, path, pane-6)
		if gb != b || !strings.HasSuffix(gt, ".ts R12") || lipgloss.Width(gb)+3+lipgloss.Width(gt) > pane-6 {
			t.Fatalf("pane %d: %q %q", pane, gb, gt)
		}
		cell := noteBoxCell(noteLine{kind: noteRowTop, text: text, badges: b, badgeCls: cls, path: path}, pane)
		if lipgloss.Width(cell) != pane {
			t.Fatalf("pane %d: cell %d wide", pane, lipgloss.Width(cell))
		}
	}
}
