package web

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

// A BINARY conflict is the case the block picker cannot serve at all
// (loadConflictDoc 422s it). The whole-file actions are exactly what is left,
// so they must work on one — they never look inside the file.
func TestOpResolveConflictBinaryFile(t *testing.T) {
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "b.bin"), []byte{0, 1, 2, 0, 3}, 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "base")
	gitRun(t, dir, "checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(dir, "b.bin"), []byte{0, 9, 9, 0, 9}, 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "commit", "-am", "theirs")
	gitRun(t, dir, "checkout", "main")
	if err := os.WriteFile(filepath.Join(dir, "b.bin"), []byte{0, 7, 7, 0, 7}, 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "commit", "-am", "ours")
	conflictedMergeState(t, dir)
	ts := serve(t, New(domain.Open(dir)))

	// The picker refuses it, and says so.
	if code := getJSON(t, ts, "/api/conflict-hunks?path=b.bin", nil); code != 422 {
		t.Errorf("conflict-hunks on a binary file: status = %d, want 422", code)
	}
	// The whole-file answer works.
	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"resolve-conflict","path":"b.bin","mode":"theirs"}`), 30*time.Second)
	if done := events[len(events)-1]; done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	got, err := os.ReadFile(filepath.Join(dir, "b.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string([]byte{0, 9, 9, 0, 9}) {
		t.Errorf("content = %v, want theirs", got)
	}
	if len(unmergedPaths(statusOf(t, dir))) != 0 {
		t.Error("conflict did not clear")
	}
}
