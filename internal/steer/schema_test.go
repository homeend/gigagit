package steer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

func loadSchema(t *testing.T, name string) *jsonschema.Resolved {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "specs", "gg-steer", name))
	if err != nil {
		t.Fatal(err)
	}
	var s jsonschema.Schema
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	rs, err := s.Resolve(nil)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return rs
}

func validate(t *testing.T, rs *jsonschema.Resolved, v any) error {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var inst any
	if err := json.Unmarshal(raw, &inst); err != nil {
		t.Fatal(err)
	}
	return rs.Validate(inst)
}

// Every wire shape gg writes validates against the published schemas
// (spec 2026-10-15-gg-steer-ide-consumer §7).
func TestWireShapesMatchSchemas(t *testing.T) {
	t.Parallel()
	cmd, rep, pres := loadSchema(t, "command.schema.json"), loadSchema(t, "reply.schema.json"), loadSchema(t, "presence.schema.json")
	cmds := []Command{
		{ID: "1-1", Cmd: "navigate", File: "a.go", Line: &Line{Side: "new", No: 40, End: 60}, Worktree: "/w", Wait: true},
		{ID: "1-2", Cmd: "navigate", Commit: "d5efa035df1f3c4c949040bebb924a35d0ea61a0"},
		{ID: "1-3", Cmd: "navigate", File: "a.go", Target: &Target{State: "commit", Commit: "d5efa035df1f3c4c949040bebb924a35d0ea61a0"}, Background: true},
		{ID: "1-4", Cmd: "highlight", File: "a.go", Side: "new", Start: 3, End: 4, Tone: "warn"},
		{ID: "1-5", Cmd: "file_focus", FileID: "f3", Line: &Line{Side: "new", No: 7}},
		{ID: "1-6", Cmd: "note_add", File: "a.go", Start: 3, End: 4, Summary: "s", Rationale: "r", Author: "ada", Meta: map[string]string{"kind": "question"}, Display: json.RawMessage(`{"kind":{"show":["badge"]}}`)},
		{ID: "1-7", Cmd: "overview_add", Title: "Tour", Text: "# t\n[a](a.go:3)", Background: true},
		{ID: "1-8", Cmd: "overview_show", FileID: "f9"},
		{ID: "1-9", Cmd: "reload", Sources: []string{"notes"}, Dir: "/w"},
	}
	for _, c := range cmds {
		if err := validate(t, cmd, c); err != nil {
			t.Errorf("command %s: %v", c.ID, err)
		}
	}
	reps := []Reply{
		{ID: "1-1", OK: true, Detail: "opened a.go:40-60 at line 40"},
		{ID: "1-2", OK: false, Error: "no IDE for this worktree"},
		{ID: "1-5", OK: true, Files: []OpenFile{{ID: "f3", Path: "a.go", Source: "worktree", Line: 7, State: "shown", Notes: 1}}},
		{ID: "1-6", OK: true, Notes: []FileNote{{ID: "t1", FileID: "f3", Path: "a.go", Start: 3, End: 4, Summary: "s"}}},
		{ID: "1-7", OK: true, Overviews: []Overview{{ID: "f4", Title: "Tour", State: "background", Anchors: 1}}},
	}
	for _, r := range reps {
		if err := validate(t, rep, r); err != nil {
			t.Errorf("reply %s: %v", r.ID, err)
		}
	}
	for _, p := range []Presence{{PID: 1, Worktree: "/w"}, {PID: 2, Worktree: "/w", Started: "2026-10-15T12:00:00Z", URL: "http://127.0.0.1:1"}} {
		if err := validate(t, pres, p); err != nil {
			t.Errorf("presence %+v: %v", p, err)
		}
	}
	// Unknown keys are tolerated (the IDE adds "ide"/"plugin" to its presence).
	var inst any
	_ = json.Unmarshal([]byte(`{"pid":3,"worktree":"/w","ide":"IntelliJ IDEA 2026.2","plugin":"0.2.0"}`), &inst)
	if err := pres.Validate(inst); err != nil {
		t.Errorf("presence with IDE keys: %v", err)
	}
	// A verb, target state or side the reader does not know is NOT a schema
	// failure: the command is dispatched and refused with prose (the replay
	// corpus has those cases), so the schema must let it through.
	for _, odd := range []string{`{"id":"1-9","cmd":"dance"}`, `{"id":"1-9","cmd":"navigate","file":"a.go","target":{"state":"shelf"}}`, `{"id":"1-9","cmd":"navigate","file":"a.go","line":{"side":"left","no":1}}`} {
		var inst any
		_ = json.Unmarshal([]byte(odd), &inst)
		if err := cmd.Validate(inst); err != nil {
			t.Errorf("command schema gates dispatch on %s: %v", odd, err)
		}
	}
	// What a reader must refuse: no id / no cmd / a line without a side.
	for _, bad := range []string{`{"cmd":"navigate"}`, `{"id":"1-9"}`, `{"id":"1-9","cmd":"navigate","line":{"no":3}}`} {
		var inst any
		_ = json.Unmarshal([]byte(bad), &inst)
		if err := cmd.Validate(inst); err == nil {
			t.Errorf("command schema accepted %s", bad)
		}
	}
}
