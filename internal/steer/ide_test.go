package steer

import (
	"os"
	"path/filepath"
	"testing"
)

// Review focus 3 + spec §2.1: the IDE's sub-inbox is invisible to the TUI's
// Drain and survives its Discard; its own Drain sees only its own files.
func TestDiscardLeavesTheIDESubInbox(t *testing.T) {
	t.Parallel()
	inbox := t.TempDir()
	ide := IDEDir(inbox)
	if ide != filepath.Join(inbox, "ide") {
		t.Fatalf("IDEDir = %q", ide)
	}
	if _, err := Post(ide, Command{ID: "1-1", Cmd: "navigate", File: "a.go"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Post(inbox, Command{ID: "2-2", Cmd: "navigate", File: "b.go"}); err != nil {
		t.Fatal(err)
	}
	if got := Drain(inbox); len(got) != 1 || got[0].ID != "2-2" {
		t.Fatalf("top-level Drain = %+v, want only the TUI's command", got)
	}
	Discard(inbox)
	if _, err := os.Stat(filepath.Join(ide, "cmd-1-1.json")); err != nil {
		t.Fatalf("Discard of the inbox removed the IDE's command: %v", err)
	}
	if got := Drain(ide); len(got) != 1 || got[0].ID != "1-1" || got[0].From != ide {
		t.Fatalf("IDE Drain = %+v", got)
	}
	// Presence names: three distinct files at the top level.
	if IDEPresence != "ide.json" || IDEPresence == TUIPresence || IDEPresence == WebPresence {
		t.Fatalf("IDEPresence = %q", IDEPresence)
	}
	if err := Touch(inbox, IDEPresence, Presence{PID: 1, Worktree: "/w"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := Live(inbox, IDEPresence); !ok {
		t.Fatal("ide.json not live right after Touch")
	}
	if _, ok := Live(inbox, TUIPresence); ok {
		t.Fatal("an ide.json made the TUI look live")
	}
}
