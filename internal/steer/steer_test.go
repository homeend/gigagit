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

// TestCommandHintFieldsRoundTripThroughJSON pins the wire shape Task 6 adds:
// hint_kind/hint_id survive Post/Drain, and stay absent (omitempty) for a
// command that carries no hint — a regression a consumer's JSON decode could
// silently drop without ever failing a build.
func TestCommandHintFieldsRoundTripThroughJSON(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "inbox")
	if _, err := Post(dir, Command{ID: "1", Cmd: "navigate", HintKind: "bookmark", HintID: "b1"}); err != nil {
		t.Fatalf("Post: %v", err)
	}
	got := Drain(dir)
	if len(got) != 1 {
		t.Fatalf("Drain = %d, want 1", len(got))
	}
	if got[0].HintKind != "bookmark" || got[0].HintID != "b1" {
		t.Errorf("hint = %q/%q, want bookmark/b1", got[0].HintKind, got[0].HintID)
	}

	// Read the raw file bytes before Drain would remove it: omitempty means a
	// hint-less command's JSON carries neither key at all.
	id2, err := Post(dir, Command{ID: "2", Cmd: "navigate", File: "a.txt"})
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "cmd-"+id2+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "hint_kind") || strings.Contains(string(raw), "hint_id") {
		t.Errorf("hint-less command JSON = %s, want no hint_kind/hint_id key", raw)
	}
	var decoded Command
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.HintKind != "" || decoded.HintID != "" {
		t.Errorf("decoded hint = %q/%q, want both empty", decoded.HintKind, decoded.HintID)
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

func TestDrainDropsPathTraversalIDs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// A crafted id containing a path separator must never survive Drain: it
	// would let PostReply/AwaitReply write reply-<id>.json outside the inbox.
	if err := os.WriteFile(filepath.Join(dir, "cmd-1-1.json"), []byte(`{"id":"../../x","cmd":"navigate","wait":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := Drain(dir)
	if len(got) != 0 {
		t.Fatalf("Drain = %+v, want none — a path-traversal id must be dropped", got)
	}
	left, _ := os.ReadDir(dir)
	if len(left) != 0 {
		t.Errorf("Drain left %d files behind for a path-traversal id", len(left))
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

// TestPostDrainRoundTripsRefAndPairTargets pins that a ref/pair Target's new
// fields survive Post/Drain exactly like the preview target's Source/Target
// already do: the NAME (or the two-halves NAME pair) is what rides the wire.
func TestPostDrainRoundTripsRefAndPairTargets(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	refID, err := Post(dir, Command{Cmd: "navigate", File: "a.go", Target: &Target{State: "ref", Ref: "main"}})
	if err != nil {
		t.Fatalf("Post ref: %v", err)
	}
	pairID, err := Post(dir, Command{Cmd: "navigate", File: "a.go", Target: &Target{State: "pair", A: "main", B: "feat/x"}})
	if err != nil {
		t.Fatalf("Post pair: %v", err)
	}
	got := Drain(dir)
	if len(got) != 2 {
		t.Fatalf("Drain = %d commands, want 2", len(got))
	}
	byID := map[string]Command{}
	for _, c := range got {
		byID[c.ID] = c
	}
	ref, ok := byID[refID]
	if !ok || ref.Target == nil || ref.Target.State != "ref" || ref.Target.Ref != "main" {
		t.Errorf("ref command = %+v, want Target.State=ref Target.Ref=main", ref)
	}
	pair, ok := byID[pairID]
	if !ok || pair.Target == nil || pair.Target.State != "pair" || pair.Target.A != "main" || pair.Target.B != "feat/x" {
		t.Errorf("pair command = %+v, want Target.State=pair A=main B=feat/x", pair)
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

func TestDrainStampsFromAndKeepsWorktree(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if _, err := Post(dir, Command{Cmd: "reload", Worktree: "/w/b"}); err != nil {
		t.Fatal(err)
	}
	got := Drain(dir)
	if len(got) != 1 || got[0].From != dir || got[0].Worktree != "/w/b" {
		t.Fatalf("drained %+v", got)
	}
	data, _ := json.Marshal(got[0])
	if strings.Contains(string(data), dir) {
		t.Fatalf("From must never reach the wire: %s", data)
	}
}
