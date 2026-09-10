package steer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPostDrainRoundTripAndOrder(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "inbox") // Post must create the dir
	ids := make([]string, 0, 3)
	for i, cmd := range []string{"navigate", "reload", "focus"} {
		id, err := Post(dir, Command{ID: "100000000000000000" + string(rune('0'+i)) + "-7", Cmd: cmd, Wait: true})
		if err != nil {
			t.Fatalf("Post %s: %v", cmd, err)
		}
		ids = append(ids, id)
	}
	got := Drain(dir)
	if len(got) != 3 {
		t.Fatalf("Drain = %d commands, want 3", len(got))
	}
	for i, want := range []string{"navigate", "reload", "focus"} {
		if got[i].Cmd != want || got[i].ID != ids[i] {
			t.Errorf("command %d = %+v, want cmd %q id %q (name order = post order)", i, got[i], want, ids[i])
		}
		if !got[i].Wait {
			t.Errorf("command %d lost Wait", i)
		}
	}
	if again := Drain(dir); len(again) != 0 {
		t.Errorf("second Drain = %+v, want empty — Drain unlinks what it returns", again)
	}
}

func TestPostFillsAnEmptyID(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	id, err := Post(dir, Command{Cmd: "reload", Sources: []string{"notes"}})
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	if !strings.Contains(id, "-") {
		t.Fatalf("generated id = %q, want <unixnano>-<pid>", id)
	}
	got := Drain(dir)
	if len(got) != 1 || got[0].ID != id {
		t.Fatalf("Drain = %+v, want the generated id %q", got, id)
	}
	if len(got[0].Sources) != 1 || got[0].Sources[0] != "notes" {
		t.Errorf("Sources = %+v, want [notes]", got[0].Sources)
	}
}

func TestDrainNeverSeesAHalfWrittenFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// A temp file mid-write must not match the cmd-*.json glob.
	if err := os.WriteFile(filepath.Join(dir, "steer-halfway.tmp"), []byte(`{"id":"x","cmd":"nav`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Drain(dir); len(got) != 0 {
		t.Fatalf("Drain = %+v, want empty — a *.tmp is invisible to the glob", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "steer-halfway.tmp")); err != nil {
		t.Errorf("Drain removed a temp file it does not own: %v", err)
	}
}

func TestDrainDropsOversizeUnparsableAndIDLess(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	big := `{"id":"big-1","cmd":"navigate","file":"` + strings.Repeat("x", MaxCommandBytes) + `"}`
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("cmd-1-1.json", big)
	write("cmd-2-1.json", "{not json")
	write("cmd-3-1.json", `{"cmd":"navigate"}`) // no id: nothing could be replied to
	write("cmd-4-1.json", `{"id":"4-1","cmd":"navigate","file":"a.go"}`)
	got := Drain(dir)
	if len(got) != 1 || got[0].ID != "4-1" {
		t.Fatalf("Drain = %+v, want only the well-formed 4-1", got)
	}
	left, _ := os.ReadDir(dir)
	if len(left) != 0 {
		t.Errorf("Drain left %d files behind; every cmd-*.json it looked at must be unlinked", len(left))
	}
}

func TestReplyRoundTripAndAwaitTimeout(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := PostReply(dir, Reply{ID: "abc-1", OK: true, Detail: "opened src/x.go:18"}); err != nil {
		t.Fatalf("PostReply: %v", err)
	}
	r, ok := AwaitReply(dir, "abc-1", time.Second)
	if !ok || !r.OK || r.Detail != "opened src/x.go:18" {
		t.Fatalf("AwaitReply = %+v ok=%v, want the posted reply", r, ok)
	}
	if _, err := os.Stat(filepath.Join(dir, "reply-abc-1.json")); !os.IsNotExist(err) {
		t.Errorf("AwaitReply must delete the reply it read (err = %v)", err)
	}
	start := time.Now()
	if _, ok := AwaitReply(dir, "missing-1", 120*time.Millisecond); ok {
		t.Error("AwaitReply on a missing reply must report ok=false")
	}
	if d := time.Since(start); d < 100*time.Millisecond {
		t.Errorf("AwaitReply returned after %v, want it to poll until the timeout", d)
	}
}

func TestDiscardSweepsCommandsAndRepliesOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, n := range []string{"cmd-1-1.json", "cmd-2-1.json", "reply-1-1.json"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(`{"id":"x"}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, TUIPresence), []byte(`{"pid":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	Discard(dir)
	left, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 1 || left[0].Name() != TUIPresence {
		names := make([]string, len(left))
		for i, e := range left {
			names[i] = e.Name()
		}
		t.Fatalf("after Discard: %v, want only %s — presence files are not leftovers", names, TUIPresence)
	}
}

func TestCommandJSONOmitsEmptyFields(t *testing.T) {
	t.Parallel()
	data, err := json.Marshal(Command{ID: "1-1", Cmd: "reload", Sources: []string{"notes"}})
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	want := `{"id":"1-1","cmd":"reload","sources":["notes"]}`
	if got != want {
		t.Errorf("Command JSON = %s, want %s (an agent reads these files)", got, want)
	}
}
