package cli

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

func TestRenderNoteLineSyncTokens(t *testing.T) {
	t.Parallel()
	base := domain.ResolvedNote{Note: model.Note{ID: "n1", Source: model.NoteSourceUser, Summary: "s",
		Address: model.FileAddress{State: model.StateCommitted, Commit: strings.Repeat("a", 40), Path: "a.go"},
		Side:    model.NoteSideNew}, Range: [2]int{3, 3}, Status: model.NoteActive, Sync: model.SyncLocal}
	line := func(r domain.ResolvedNote) string {
		var b strings.Builder
		renderNoteLine(&b, r, false, "active")
		return b.String()
	}
	if got := line(base); got != "n1 [user] aaaaaaa:a.go new:3-3 active  s\n" {
		t.Fatalf("a local note's line changed: %q", got)
	}
	failed := base
	failed.Sync, failed.SendErr = model.SyncFailed, "HTTP 403"
	if got := line(failed); !strings.HasPrefix(got, "n1 [user] [failed: HTTP 403] aaaaaaa:a.go") {
		t.Errorf("failed = %q", got)
	}
	sending := base
	sending.Sync = model.SyncSending
	if got := line(sending); !strings.HasPrefix(got, "n1 [user] [sending] ") {
		t.Errorf("sending = %q", got)
	}
}
