package tui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

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
