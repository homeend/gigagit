package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/model"
)

// prMenuModel is notedModel inside PR #7's diff with a service (forgeSendCmd
// needs one to dispatch; the command itself is never run here).
func prMenuModel(t *testing.T) Model {
	t.Helper()
	m := prNotedModel(t)
	m.svc = domain.New(&git.Repo{Runner: gitexec.NewFakeRunner()})
	m.previewOpen = &previewOpenState{prNumber: 7}
	m.filesView = newContentPopup("PR #7", nil)
	return m
}

func menuIDString(rows []actionRow) string {
	var ids []string
	for _, r := range rows {
		ids = append(ids, r.id)
	}
	return strings.Join(ids, ",")
}

func TestPRNoteMenuSendsALocalNote(t *testing.T) {
	t.Parallel()
	m := prMenuModel(t)
	v := m.diffLayer()
	v.notes[0].Group = domain.GroupMine
	v.setCursorLine(4, m.diffBodyRows())
	ids := menuIDString(m.noteMenuRows())
	for _, want := range []string{"note-send", "note-edit", "note-reply", "note-delete"} {
		if !strings.Contains(ids, want) {
			t.Errorf("rows %s lack %s", ids, want)
		}
	}
	if strings.Contains(ids, "note-send-review") {
		t.Errorf("rows %s still offer the group send (R9: the note menu sends that one note only)", ids)
	}
	tg, _ := m.noteNearCursor()
	if req := noteSendRequest(7, tg); req.PR != 7 || len(req.Notes) != 1 || req.Notes[0] != "n1" {
		t.Fatalf("request %+v", req)
	}
}

func TestPRNoteMenuRetriesAFailedNote(t *testing.T) {
	t.Parallel()
	m := prMenuModel(t)
	v := m.diffLayer()
	v.notes[0].Sync, v.notes[0].SendErr = model.SyncFailed, "HTTP 502"
	v.setCursorLine(4, m.diffBodyRows())
	for _, r := range m.noteMenuRows() {
		if r.id == "note-send" && r.label != "Retry sending as GitHub comment" {
			t.Fatalf("label %q", r.label)
		}
	}
	v.notes[0].Sync = model.SyncSending
	if strings.Contains(menuIDString(m.noteMenuRows()), "note-send,") {
		t.Fatal("a note being sent offers no second send")
	}
}

func TestPRNoteMenuOnAGitHubThread(t *testing.T) {
	t.Parallel()
	m := prMenuModel(t)
	v := m.diffLayer()
	root := forgeRoot("C1", 5, "rename this", false)
	draft := rootNote("d1", 5, "on it", "", model.NoteSourceUser, model.NoteActive)
	draft.Note.ParentID = model.ForgeNoteIDPrefix + "C1"
	root.Replies = []domain.ResolvedNote{draft}
	v.notes = []domain.ResolvedNote{root}
	v.relayout(0)
	v.setCursorLine(4, m.diffBodyRows())
	ids := menuIDString(m.noteMenuRows())
	for _, want := range []string{"note-reply", "note-reply-send", "note-resolve", "note-send-drafts"} {
		if !strings.Contains(ids, want) {
			t.Errorf("rows %s lack %s", ids, want)
		}
	}
	for _, not := range []string{"note-send,", "note-edit", "note-delete"} {
		if strings.Contains(ids+",", not) {
			t.Errorf("rows %s offer %s on a GitHub thread", ids, not)
		}
	}
	tg, _ := m.noteNearCursor()
	if req := threadActionRequest(7, tg); len(req.Resolve) != 1 || req.Resolve[0] != "forge:C1" {
		t.Fatalf("resolve request %+v", req)
	}
	tg.resolved = true
	if req := threadActionRequest(7, tg); len(req.Unresolve) != 1 {
		t.Fatalf("reopen request %+v", req)
	}
}

func TestRAndXOnAGitHubThreadInsideAPR(t *testing.T) {
	t.Parallel()
	m := prMenuModel(t)
	v := m.diffLayer()
	v.notes = []domain.ResolvedNote{forgeRoot("C1", 5, "rename this", false)}
	v.relayout(0)
	v.setCursorLine(4, m.diffBodyRows())
	nm, _ := v.update(m, synthKey("R"))
	if p := layerOf[*notePopup](nm); p == nil || p.targetID != "forge:C1" {
		t.Fatal("R inside a PR answers a GitHub thread")
	}
	nm, cmd := v.update(m, synthKey("x"))
	if cmd == nil || !strings.Contains(nm.statusMsg, "#7") {
		t.Fatalf("x resolves on GitHub through a send (status %q)", nm.statusMsg)
	}
}
