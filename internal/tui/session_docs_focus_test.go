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
