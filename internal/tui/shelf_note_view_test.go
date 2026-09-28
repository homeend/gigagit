package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

func shelfNoteModel() Model {
	m := shelfPopModel(shEntry("a", "x.go"), shEntry("b", "y.go"))
	m.noteCounts = domain.NoteCounts{ByShelf: map[string]int{"b": 1}}
	return m
}

func TestShelfRowShowsNoteBadge(t *testing.T) {
	t.Parallel()
	m := shelfNoteModel()
	out := m.renderShelfPopupBox(m.shelfSwitcher())
	var withBadge []string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "◆1") {
			withBadge = append(withBadge, l)
		}
	}
	if len(withBadge) != 1 || !strings.Contains(withBadge[0], "y.go") {
		t.Fatalf("only the noted entry's row carries ◆1, got %q:\n%s", withBadge, out)
	}
}

func TestShelfNoteHintOnlyOnANotedRow(t *testing.T) {
	t.Parallel()
	m := shelfNoteModel()
	if out := m.renderShelfPopupBox(m.shelfSwitcher()); strings.Contains(out, "[n] note") {
		t.Fatalf("an entry without notes must not advertise [n]:\n%s", out)
	}
	m.shelfSwitcher().moveSel(1)
	if out := m.renderShelfPopupBox(m.shelfSwitcher()); !strings.Contains(out, "[n] note") {
		t.Fatalf("a noted entry advertises [n] note:\n%s", out)
	}
}

func TestShelfNKeyOpensReadOnlyNoteView(t *testing.T) {
	t.Parallel()
	m := shelfNoteModel()
	m.shelfSwitcher().moveSel(1)
	mm, cmd := m.Update(keyMsg("n"))
	m = mm.(Model)
	if cmd == nil {
		t.Fatal("n on a noted entry must load its notes")
	}
	note := model.Note{ID: "n1", Source: model.NoteSourceAgent, Author: "gg",
		Summary: "Recycled from /x (main)", Rationale: "Deleted (not in this set):\n  gone.go",
		Created: time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)}
	mm, _ = m.Update(shelfNotesMsg{id: "b", label: "y.go", notes: []domain.ResolvedNote{{Note: note, Status: model.NoteActive}}})
	m = mm.(Model)
	cp, ok := m.topLayer().(*contentPopup)
	if !ok {
		t.Fatalf("want the read-only content popup on top, got %T", m.topLayer())
	}
	var text []string
	for _, l := range cp.lines {
		text = append(text, l.text)
	}
	all := strings.Join(text, "\n")
	for _, want := range []string{"Recycled from /x (main)", "gg · 2026-09-28", "  gone.go"} {
		if !strings.Contains(all, want) {
			t.Errorf("viewer missing %q:\n%s", want, all)
		}
	}
	mm, _ = m.Update(keyMsg("esc"))
	if mm.(Model).shelfSwitcher() == nil || mm.(Model).topLayer() != mm.(Model).shelfSwitcher() {
		t.Fatal("esc must return to the shelf switcher")
	}
}

func TestShelfNKeyInertWithoutNotes(t *testing.T) {
	t.Parallel()
	m := shelfNoteModel()
	mm, cmd := m.Update(keyMsg("n"))
	if cmd != nil || mm.(Model).topLayer() != mm.(Model).shelfSwitcher() {
		t.Fatal("n on an entry without notes does nothing")
	}
}

// A long summary names a worktree path: it loses its MIDDLE, never its end
// (the branch in parentheses and the directory name are what the reader needs).
func TestShelfNoteViewElidesPathsInTheMiddle(t *testing.T) {
	t.Parallel()
	m := shelfNoteModel()
	m.width, m.height = 80, 30
	long := "/very/long/prefix/that/does/not/fit/in/the/box/at/all/and/keeps/going/wt-a"
	note := model.Note{ID: "n1", Author: "gg", Summary: "Recycled from " + long + " (feat)",
		Rationale: "Deleted (not in this set):\n  " + strings.Repeat("deep/", 20) + "gone.go",
		Created:   time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)}
	mm, _ := m.Update(shelfNotesMsg{id: "b", label: "y.go", notes: []domain.ResolvedNote{{Note: note, Status: model.NoteActive}}})
	m = mm.(Model)
	out := m.topLayer().(*contentPopup).box(m)
	var summary, file string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "Recycled from") {
			summary = l
		}
		if strings.Contains(l, "gone.go") {
			file = l
		}
	}
	if !strings.Contains(summary, "…") || !strings.Contains(summary, "wt-a (feat)") {
		t.Fatalf("summary row must keep its end and lose its middle, got %q\n%s", summary, out)
	}
	if !strings.Contains(file, "…") {
		t.Fatalf("a long deleted path must be middle-elided and keep its file name, got %q\n%s", file, out)
	}
}
