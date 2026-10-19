package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/steer"
)

// gg session files on the TUI lists a session overview's author, created
// stamp and anchor count, as the web does.
func TestFilesReplyCarriesAnOverviewsFrontMatter(t *testing.T) {
	t.Parallel()
	m, _, dir := attachedDocsModel(t)
	writeSessionOverview(t, m, dir, "20260101T220000-ab12", "Tour of a", "2026-01-01T22:00:00Z", "See [two](a.txt:2) and [one](a.txt:1).\n")
	m, _ = m.syncAgentDocs()
	var row *steer.OpenFile
	for _, f := range m.openFilesProto() {
		if f.ID == "20260101T220000-ab12" {
			f := f
			row = &f
		}
	}
	if row == nil {
		t.Fatal("overview not listed")
	}
	if row.Source != "overview" || row.Author != "agent" || row.Created != "2026-01-01T22:00:00Z" || row.Anchors != 2 {
		t.Fatalf("row = %+v", *row)
	}
}
