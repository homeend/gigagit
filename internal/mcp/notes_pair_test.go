package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// The preview arg takes a COMMIT PAIR too — typed as <a>..<b> or named by a
// saved pair — and the note is an ordinary committed note on b, new side.
func TestNoteAddWithACommitPairStoresOnB(t *testing.T) {
	e := newTestEnv(t)
	tip, _ := seedPreviewBranch(t, e)

	out := e.call(t, "gg_note_add", map[string]any{
		"preview": "main..feat", "file": "a.txt", "new_line": 2, "summary": "on b",
	})
	note, ok := out["note"].(map[string]any)
	if !ok {
		t.Fatalf("gg_note_add reply = %v", out)
	}
	stored, err := e.svc.NoteGet(context.Background(), note["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	want := model.FileAddress{State: model.StateCommitted, Commit: tip, Path: "a.txt"}
	if stored.Address != want || stored.Side != model.NoteSideNew {
		t.Fatalf("stored %+v side %q, want %+v new", stored.Address, stored.Side, want)
	}

	p, err := e.svc.PairAdd(context.Background(), "main", "feat", "attempt")
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range []string{p.ID, "attempt", "main..feat"} {
		got := e.call(t, "gg_notes_list", map[string]any{"preview": spec})
		raw, _ := json.Marshal(got)
		if !strings.Contains(string(raw), "on b") {
			t.Fatalf("gg_notes_list preview=%s: %v", spec, got)
		}
	}
}
