package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/model"
)

// A click in the . menu is positional: the row under the pointer becomes the
// highlight, and a double-click there runs THAT row. The user hit the old
// behaviour — double-click ran whatever was highlighted — by clicking "Reply
// to note" while the highlight sat on "Delete note", and got the delete modal.
func TestActionMenuClickSelectsTheRowUnderThePointer(t *testing.T) {
	t.Parallel()
	m := notedModel(t)
	m.width, m.height = 100, 40
	m.diffLayer().setCursorLine(24, m.diffBodyRows()) // next to n2, so the note rows are offered
	m = m.openActionMenu()
	if m.actionMenu == nil {
		t.Fatal("no action menu")
	}
	vis := m.actionMenu.visible()
	idx := map[string]int{}
	for i, r := range vis {
		idx[r.id] = i
	}
	reply, del := idx["note-reply"], idx["note-delete"]
	if reply == 0 || del == 0 {
		t.Fatalf("missing note rows in %d rows", len(vis))
	}
	// Highlight Delete (as the user had it), then work out Reply's terminal row.
	m.actionMenu.sel = del
	y := actionMenuRowY(m, reply)
	top := (m.height - len(strings.Split(m.renderActionMenu(), "\n"))) / 2
	if got, ok := m.actionMenuRowAt(y); !ok || got != reply {
		t.Fatalf("actionMenuRowAt(%d) = %d,%v want %d", y, got, ok, reply)
	}
	click := tea.MouseMsg{X: 20, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
	nm, _ := m.Update(click)
	m = nm.(Model)
	if m.actionMenu == nil || m.actionMenu.sel != reply {
		t.Fatalf("single click did not move the highlight to Reply (sel=%d)", m.actionMenu.sel)
	}
	nm, _ = m.Update(click) // second click within the double-click window
	m = nm.(Model)
	if m.modal != nil {
		t.Fatalf("double-click on Reply opened a modal: %+v", m.modal.req)
	}
	p, ok := m.topLayer().(*notePopup)
	if !ok || p.mode != noteReply {
		t.Fatalf("top layer = %T, want the reply popup", m.topLayer())
	}
	// A click off the rows (the header line) leaves the highlight alone and
	// never runs anything, even when doubled.
	m2 := notedModel(t)
	m2.width, m2.height = 100, 40
	m2.diffLayer().setCursorLine(24, m2.diffBodyRows())
	m2 = m2.openActionMenu()
	m2.actionMenu.sel = del
	hdr := tea.MouseMsg{X: 20, Y: top + 2, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
	nm, _ = m2.Update(hdr)
	nm, _ = nm.(Model).Update(hdr)
	m2 = nm.(Model)
	if m2.actionMenu == nil || m2.actionMenu.sel != del || m2.modal != nil {
		t.Fatalf("header double-click changed state: menu=%v modal=%v", m2.actionMenu != nil, m2.modal != nil)
	}
}

// actionMenuRowY is the terminal row the open action menu draws visible row i
// on, mirroring actionMenuRowAt's arithmetic (centred overlay, frame + header
// above the body, the window slice around the highlight).
func actionMenuRowY(m Model, i int) int {
	vis := m.actionMenu.visible()
	_, h := m.overlayDims()
	top := (h - len(strings.Split(m.renderActionMenu(), "\n"))) / 2
	lo, _ := windowRowBounds(len(vis), min(len(vis), 14), m.actionMenu.sel, m.actionMenu.mode)
	return top + actionMenuBodyTop + (i - lo)
}

// A decision modal raised from the diff view (the note chooser, the delete
// confirm) is drawn OVER the diff, not over the bare panel interface — the
// user saw the diff "close" behind the chooser.
func TestModalDrawsOverTheOpenDiff(t *testing.T) {
	t.Parallel()
	m := notedModel(t)
	m.width, m.height = 120, 40
	v := m.diffLayer()
	v.notes = append(v.notes, rootNote("n3", 25, "third", "", model.NoteSourceUser, model.NoteActive))
	v.relayout(m.width)
	v.setCursorLine(24, m.diffBodyRows())
	nm, _ := v.update(m, synthKey("E"))
	m = nm
	if m.modal == nil {
		t.Fatal("E with two notes must raise the chooser")
	}
	frame := m.render()
	if !strings.Contains(frame, "Which note?") {
		t.Fatal("the chooser is not on screen")
	}
	if !strings.Contains(frame, "diff: a/b.go") {
		t.Fatalf("the diff view header vanished behind the modal:\n%s", frame)
	}
}
