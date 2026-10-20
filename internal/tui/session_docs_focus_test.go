package tui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

// gg session overview add writes the file and posts file_focus at once:
// the focus must find the document before the store's watch has fired.
func TestFileFocusOfAJustWrittenOverviewReloadsFirst(t *testing.T) {
	t.Parallel()
	m, _, dir := attachedDocsModel(t)
	inbox := filepath.Join(t.TempDir(), "steer")
	m.steerDir = inbox
	m.loading = false
	content := "+++\ntitle = \"Fresh\"\nauthor = \"agent\"\ncreated = 2026-01-01T00:00:00Z\n+++\nSee [a](a.txt:1).\n"
	if err := os.WriteFile(filepath.Join(dir, "20260101T000000-ffff.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	c := steer.Command{ID: "c1", Cmd: "file_focus", FileID: "20260101T000000-ffff", Wait: true}
	_, cmd := m.applySteer(c)
	runSteerCmd(t, cmd)
	r, ok := steer.AwaitReply(inbox, "c1", time.Second)
	if !ok || !r.OK || r.Detail != "focused overview 20260101T000000-ffff \"Fresh\"" {
		t.Fatalf("reply = %+v (ok %v)", r, ok)
	}
}

// An overview written in a worktree gg is not showing is not in the shown
// worktree's store: the reply says where it is and when it shows, not a
// bare "no open file" an agent reads as a failed write.
func TestFileFocusFromAWorktreeNotShownSaysWhereTheDocumentIs(t *testing.T) {
	t.Parallel()
	m, _, _ := attachedDocsModel(t)
	inbox := filepath.Join(t.TempDir(), "steer")
	m.steerDir = inbox
	m.loading = false
	other := filepath.Join(t.TempDir(), "wt-orange")
	c := steer.Command{ID: "c1", Cmd: "file_focus", FileID: "20260101T000000-ffff", Worktree: other, Wait: true}
	_, cmd := m.applySteer(c)
	runSteerCmd(t, cmd)
	r, ok := steer.AwaitReply(inbox, "c1", time.Second)
	want := "gg is showing worktree " + m.currentWorktree + ", not " + other + ": the document is saved and shows when the user opens that worktree"
	if !ok || r.OK || r.Error != want {
		t.Fatalf("reply = %+v (ok %v), want error %q", r, ok, want)
	}
}

// The case behind the not-shown fix: gg runs in its home worktree and a
// look shows wt-orange; an agent there writes an overview into wt-orange's
// docs and posts file_focus stamped with wt-orange. The look attached that
// directory, so the focus finds the document and shows it.
func TestFileFocusUnderALookFindsTheViewedWorktreesDocument(t *testing.T) {
	t.Parallel()
	m := privateDocsModel(t)
	m.width, m.height = 120, 40
	home := m.currentWorktree
	m = m.seedHome(home) // the first load's seeding: a look swaps from home's slot
	homeDocs, orangeDocs := filepath.Join(t.TempDir(), "home"), filepath.Join(t.TempDir(), "orange")
	m, orange := addWorktree(t, m, "wt-orange")
	m.docsDirOf = func(w string) string {
		if model.SamePath(w, orange) {
			return orangeDocs
		}
		return homeDocs
	}
	m = m.attachShownRoots()
	t.Cleanup(m.docs.Close)
	m, ok := m.userSwitchView(orange)
	if !ok || !model.SamePath(m.currentWorktree, orange) || model.SamePath(m.currentWorktree, home) {
		t.Fatalf("setup: look refused or not on wt-orange: %q (%s)", m.statusMsg, m.currentWorktree)
	}
	content := "+++\ntitle = \"Orange\"\nauthor = \"agent\"\ncreated = 2026-01-01T00:00:00Z\n+++\nSee [a](a.txt:1).\n"
	if err := os.MkdirAll(orangeDocs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(orangeDocs, "20260101T000000-0a0a.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	inbox := filepath.Join(t.TempDir(), "steer")
	m.steerDir = inbox
	m.loading = false
	c := steer.Command{ID: "c1", Cmd: "file_focus", FileID: "20260101T000000-0a0a", Worktree: orange, Wait: true}
	_, cmd := m.applySteer(c)
	runSteerCmd(t, cmd)
	r, ok := steer.AwaitReply(inbox, "c1", time.Second)
	if !ok || !r.OK || r.Detail != "focused overview 20260101T000000-0a0a \"Orange\"" {
		t.Fatalf("reply = %+v (ok %v)", r, ok)
	}
}
