package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// typeInto feeds s to the popup one key at a time, the way a user types it.
func typeInto(t *testing.T, p *noteRemoveAllPopup, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		key := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
		if r == ' ' {
			key = tea.KeyMsg{Type: tea.KeySpace}
		}
		m, _ = p.update(m, key)
	}
	return m
}

func TestNoteRemoveAllRowSharesTheWholeDiffGate(t *testing.T) {
	t.Parallel()
	m := notedModel(t)
	m.diffLayer().setCursorLine(15, m.diffBodyRows())
	if !hasActionRow(m, "note-remove-all") {
		t.Fatal("Remove all notes must be offered anywhere in a diff that carries notes")
	}
	m.diffLayer().notes = nil
	if hasActionRow(m, "note-remove-all") {
		t.Fatal("Remove all notes must not be offered on a diff without notes")
	}
}

func TestNoteRemoveAllPopupCountsRepliesAndNamesThePath(t *testing.T) {
	t.Parallel()
	m := notedModel(t)
	m.diffLayer().notes[0].Replies = []domain.ResolvedNote{{
		Note: model.Note{ID: "r1", ParentID: "n1", Summary: "agreed"}, Status: model.NoteActive,
	}}
	m = runActionRow(t, m, "note-remove-all")
	p := layerOf[*noteRemoveAllPopup](m)
	if p == nil {
		t.Fatal("Remove all notes must push a noteRemoveAllPopup")
	}
	box := p.box(m)
	if !strings.Contains(box, i18n.T("This deletes %d notes and their replies from %s.", 2, "a/b.go")) {
		t.Fatalf("the popup must quote the count, the replies and the path:\n%s", box)
	}
	if !strings.Contains(box, noteRemoveAllToken) {
		t.Fatalf("the popup must name the confirmation token:\n%s", box)
	}

	// Without replies the prose must not claim any.
	m2 := notedModel(t)
	m2 = runActionRow(t, m2, "note-remove-all")
	if box := layerOf[*noteRemoveAllPopup](m2).box(m2); !strings.Contains(box, i18n.T("This deletes %d notes from %s.", 2, "a/b.go")) {
		t.Fatalf("a reply-less file must not mention replies:\n%s", box)
	}
}

func TestNoteRemoveAllPopupRefusesTheWrongText(t *testing.T) {
	t.Parallel()
	m := notedModel(t)
	m = runActionRow(t, m, "note-remove-all")
	p := layerOf[*noteRemoveAllPopup](m)
	m = typeInto(t, p, m, "remove")
	m, cmd := p.update(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("a half-typed token must not run the removal")
	}
	if layerOf[*noteRemoveAllPopup](m) == nil {
		t.Fatal("a refused confirmation must keep the popup open")
	}
	if box := p.box(m); !strings.Contains(box, i18n.T("type exactly: %s", noteRemoveAllToken)) {
		t.Fatalf("a refused confirmation must show the hint:\n%s", box)
	}
	// esc always gets out.
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyEsc})
	if layerOf[*noteRemoveAllPopup](m) != nil {
		t.Fatal("esc must close the popup")
	}
}

func TestNoteRemoveAllPopupClearsTheAddressOnConfirm(t *testing.T) {
	t.Parallel()
	f := gitexec.NewFakeRunner()
	f.SetResponse("git rev-parse (toplevel)", gitexec.Result{Stdout: "/wt\n"})
	svc := domain.New(&git.Repo{Runner: f})
	svc.UseNotesDir(t.TempDir()) // opts this Service back in past NotesDisabled
	ctx := context.Background()

	addr := model.FileAddress{State: model.StateUnstaged, Worktree: "/wt", Path: "a/b.go"}
	root, err := svc.NoteAdd(ctx, model.Note{
		Address: addr, Side: model.NoteSideNew, Range: [2]int{5, 5},
		Summary: "first", ContextHash: model.NoteContextHash([]string{"r"}),
	})
	if err != nil {
		t.Fatalf("NoteAdd: %v", err)
	}
	if _, err := svc.NoteReply(ctx, root.ID, model.Note{Summary: "agreed"}); err != nil {
		t.Fatalf("NoteReply: %v", err)
	}
	// A note on another file must survive the clear.
	other := model.FileAddress{State: model.StateUnstaged, Worktree: "/wt", Path: "c/d.go"}
	if _, err := svc.NoteAdd(ctx, model.Note{
		Address: other, Side: model.NoteSideNew, Range: [2]int{1, 1},
		Summary: "elsewhere", ContextHash: model.NoteContextHash([]string{"r"}),
	}); err != nil {
		t.Fatalf("NoteAdd(other): %v", err)
	}

	m := notedModel(t)
	m.svc = svc
	m = runActionRow(t, m, "note-remove-all")
	p := layerOf[*noteRemoveAllPopup](m)
	// Typed with stray spacing and odd case: the guard trims and folds case.
	m = typeInto(t, p, m, "  Remove All ")
	m, cmd := p.update(m, tea.KeyMsg{Type: tea.KeyEnter})
	if layerOf[*noteRemoveAllPopup](m) != nil {
		t.Fatal("a confirmed removal must close the popup")
	}
	if cmd == nil {
		t.Fatal("a confirmed removal must return the clear command")
	}
	msg, ok := cmd().(notesClearedMsg)
	if !ok {
		t.Fatalf("the clear command returned %T, want notesClearedMsg", cmd())
	}
	if msg.err != nil || msg.n != 2 {
		t.Fatalf("cleared %d notes (%v), want 2 (the root and its reply)", msg.n, msg.err)
	}
	// Read the STORE, not a resolved view: the fake runner serves no file
	// content, so every note would resolve as orphaned and NotesAt would report
	// "none" whether or not the clear actually landed.
	c, err := svc.NoteCounts(ctx)
	if err != nil {
		t.Fatalf("NoteCounts: %v", err)
	}
	if c.ByPath["a/b.go"] != 0 {
		t.Fatalf("a/b.go still holds %d notes after the clear", c.ByPath["a/b.go"])
	}
	if c.ByPath[other.Path] != 1 {
		t.Fatalf("c/d.go holds %d notes, want the untouched one", c.ByPath[other.Path])
	}

	// The Model reports it and re-reads the note sources (the ◆N badges).
	tm, reload := m.Update(msg)
	m = tm.(Model)
	want := i18n.T("Removed %d notes", 2)
	if m.statusMsg != want {
		t.Fatalf("statusMsg = %q, want %q", m.statusMsg, want)
	}
	// The diff view owns the whole screen and draws no status line, so the
	// outcome has to reach its OWN bottom-left notice box or the user sees
	// nothing but the boxes vanishing.
	if !strings.Contains(m.diffNotice, want) {
		t.Fatalf("diffNotice = %q, want it to carry %q", m.diffNotice, want)
	}
	if screen := m.withDiffFileNotice(m.renderDiffView()); !strings.Contains(screen, want) {
		t.Fatalf("the diff view must draw the notice:\n%s", screen)
	}
	if reload == nil {
		t.Fatal("a clear must refresh the note sources so the ◆N badges follow")
	}
}
