package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

func resolvedRoot(id string, line int, summary string) domain.ResolvedNote {
	r := rootNote(id, line, summary, "", model.NoteSourceUser, model.NoteActive)
	r.Resolution = &model.ThreadResolution{Root: id, By: "B"}
	return r
}

func TestResolvedThreadsStartFoldedOnce(t *testing.T) {
	t.Parallel()
	m := notedModel(t)
	v := m.diffLayer()
	notes := []domain.ResolvedNote{rootNote("n1", 5, "open", "", model.NoteSourceUser, model.NoteActive), resolvedRoot("n2", 25, "settled")}
	v.setNotes(notes)
	if v.collapsed["n1"] || !v.collapsed["n2"] {
		t.Fatalf("only the resolved stored thread starts folded: %v", v.collapsed)
	}
	v.collapsed["n2"] = false // the user unfolded it
	v.setNotes(notes)
	if v.collapsed["n2"] {
		t.Fatal("seeding is once per view: a re-read must not refold")
	}
}

func TestResolvedThreadSaysResolved(t *testing.T) {
	t.Parallel()
	m := notedModel(t)
	v := m.diffLayer()
	r := resolvedRoot("n1", 5, "agreed")
	if got := v.collapsedNoteLine(r).text; !strings.HasSuffix(got, " · "+i18n.T("resolved")) {
		t.Fatalf("collapsed row = %q", got)
	}
	if got := v.noteBoxTitle(r); !strings.HasSuffix(got, " · "+i18n.T("resolved")) {
		t.Fatalf("title = %q", got)
	}
}

func TestReplyLinkDrawsItsOwnRow(t *testing.T) {
	t.Parallel()
	r := rootNote("n1", 5, "fix it", "", model.NoteSourceUser, model.NoteActive)
	rep := rootNote("r1", 5, "fixed", "", model.NoteSourceAgent, model.NoteActive)
	rep.Note.Link = "0123456789012345678901234567890123456789"
	rows := noteBodyLines(rep, r.Note.ID, 1, 80, false)
	if last := rows[len(rows)-1].text; !strings.Contains(last, "→ "+rep.Note.Link) {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestXResolvesTheThreadAtTheCursor(t *testing.T) {
	t.Parallel()
	m := notedModel(t)
	v := m.diffLayer()
	v.setCursorLine(4, m.diffBodyRows())
	next, cmd := v.update(m, synthKey("x"))
	if cmd == nil {
		t.Fatal("x on a stored thread must write")
	}
	// The result of the write: the thread folds in place.
	nm, _ := next.Update(threadResolvedMsg{root: "n1", resolved: true})
	if !nm.(Model).diffLayer().collapsed["n1"] {
		t.Fatal("resolving folds the thread in place")
	}
	nm, _ = nm.(Model).Update(threadResolvedMsg{root: "n1", resolved: false})
	if nm.(Model).diffLayer().collapsed["n1"] {
		t.Fatal("reopening unfolds it")
	}
}

func TestXOnAForgeThreadSaysGitHub(t *testing.T) {
	t.Parallel()
	m := notedModel(t)
	v := m.diffLayer()
	v.notes = []domain.ResolvedNote{forgeRoot("C1", 5, "rename this", false)}
	v.relayout(0)
	v.setCursorLine(4, m.diffBodyRows())
	next, cmd := v.update(m, synthKey("x"))
	if cmd != nil {
		t.Fatal("no store call for a forge thread")
	}
	if got := next.diffNotice; !strings.Contains(got, i18n.T("resolved on GitHub")) {
		t.Fatalf("notice = %q", got)
	}
}

func TestRInTheReviewViewRepliesToARemark(t *testing.T) {
	t.Parallel()
	m := notedModel(t)
	v := m.diffLayer()
	v.reviewID = "abcd1234"
	remark := rootNote("review:abcd1234:0", 5, "rename this", "", model.NoteSourceAgent, model.NoteActive)
	v.notes = []domain.ResolvedNote{remark}
	v.relayout(0)
	v.setCursorLine(4, m.diffBodyRows())
	// c (add) and E (edit) still refuse in the review view.
	for _, k := range []string{"c", "E"} {
		next, _ := v.update(m, synthKey(k))
		if layerOf[*notePopup](next) != nil {
			t.Fatalf("%s must still refuse in the review view", k)
		}
	}
	next, _ := v.update(m, synthKey("R"))
	p := layerOf[*notePopup](next)
	if p == nil || p.mode != noteReply || p.targetID != "review:abcd1234:0" {
		t.Fatalf("popup = %#v", p)
	}
	if p.fields() != 3 {
		t.Fatalf("a reply form has summary, rationale and link: %d fields", p.fields())
	}
}

func TestReplyPopupCarriesTheLink(t *testing.T) {
	t.Parallel()
	p := &notePopup{mode: noteReply, summary: newTextField("fixed"), rationale: newTextField(""), link: newTextField(" HEAD ")}
	if n := p.note("fixed", ""); n.Link != "HEAD" {
		t.Fatalf("note = %+v", n)
	}
}
