package agentdocs

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/notemeta"
)

var updateExamples = flag.Bool("update-examples", false, "rewrite docs/specs/gg-session-docs/examples/ from exampleDocs() (read the diff before committing)")

// The example tree the documents anchor (examples/tree/).
var exampleTree = map[string]string{
	"internal/tui/scroll.go": "package tui\n\nimport \"sync\"\n\n// scroller coalesces wheel ticks.\ntype scroller struct{ mu sync.Mutex; delta int }\n\nfunc (s *scroller) push(d int) {\n\ts.mu.Lock()\n\ts.delta += d\n\ts.mu.Unlock()\n}\n\nfunc (s *scroller) flush() {\n\ts.mu.Lock()\n\tdefer s.mu.Unlock()\n\tif s.delta != 0 {\n\t\tapply(s.delta)\n\t\ts.delta = 0\n\t}\n}\n\nfunc apply(int) {}\n",
	"README.md":              "# demo\r\n\r\nA tree with CRLF endings.\r\nSecond paragraph.\r\n",
}

type exampleDoc struct {
	id, kind string
	over     *OverviewFile
	note     *NoteFile
	about    string // for ids.json
}

func exampleDocs() []exampleDoc {
	at := func(s string) time.Time { t, _ := time.Parse(time.RFC3339, s); return t.UTC() }
	return []exampleDoc{
		{id: "20261018T143201-9f3a", kind: KindOverview, about: "an overview with a file anchor, a range anchor, a note anchor and one UNRESOLVED anchor (missing.go)",
			over: &OverviewFile{Title: "How scroll coalescing works", Author: "claude", Created: at("2026-10-18T14:32:01Z"),
				Text: "Scroll events are coalesced per frame so a burst of wheel ticks costs one layout.\n\n## How it works\n1. [The wheel handler](internal/tui/scroll.go:8-12) adds the tick's delta under the lock.\n2. [flush](internal/tui/scroll.go:14-21) runs once per tick and applies the sum.\n3. [Why one lock](note:20261018T143205-c0d1) — the handler and the tick both reach the field.\n\n## Not covered\n- [the renderer](internal/tui/missing.go) is not in this tree: an unresolved anchor.\n- [the readme](README.md) has CRLF endings.\n"}},
		{id: "20261018T143301-0001", kind: KindOverview, about: "a minimal overview: title only, no author (reads as agent), no created (readers use the file's mtime)",
			over: &OverviewFile{Title: "Minimal", Text: "Just text, [one anchor](README.md:1).\n"}},
		{id: "20261018T143205-c0d1", kind: KindNote, about: "a note with meta and a display declaration; its lines match the tree exactly",
			note: &NoteFile{Path: "internal/tui/scroll.go", Start: 15, End: 16, Summary: "One lock around flush: the wheel handler and the tick both reach delta.", Rationale: "Without it a tick between two pushes could apply a partial sum.", Author: "claude", Created: at("2026-10-18T14:32:05Z"),
				Lines: []string{"\ts.mu.Lock()", "\tdefer s.mu.Unlock()"},
				Meta:  []notemeta.KV{{Key: "kind", Value: "design"}}, Display: notemeta.Display{"kind": {Show: []string{"badge"}, Values: map[string]notemeta.Value{"design": {Color: "blue"}}}}}},
		{id: "20261018T143210-77aa", kind: KindNote, about: "a note on CRLF content: lines carry no \\r; readers place it at 3-4",
			note: &NoteFile{Path: "README.md", Start: 3, End: 4, Summary: "Two lines of a CRLF file.", Author: "claude", Created: at("2026-10-18T14:32:10Z"), Lines: []string{"A tree with CRLF endings.", "Second paragraph."}}},
		{id: "20261018T143220-beef", kind: KindNote, about: "an OUTDATED note: its lines are not in the tree (the writer's file changed); readers keep 6-7 and mark it outdated",
			note: &NoteFile{Path: "internal/tui/scroll.go", Start: 6, End: 7, Summary: "Stale: the handler used to clamp here.", Author: "claude", Created: at("2026-10-18T14:32:20Z"), Lines: []string{"func (s *scroller) push(d int) {", "\tif d > 100 { d = 100 }"}}},
	}
}

func examplesDir() string { return specPath("examples") }

func TestExamplesAreWhatGGWrites(t *testing.T) {
	dir := examplesDir()
	if *updateExamples {
		if err := os.RemoveAll(dir); err != nil {
			t.Fatal(err)
		}
		for p, text := range exampleTree {
			full := filepath.Join(dir, "tree", filepath.FromSlash(p))
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		ids := map[string]string{}
		for _, d := range exampleDocs() {
			var data []byte
			var err error
			if d.kind == KindOverview {
				data, err = EncodeOverviewFile(*d.over)
			} else {
				data, err = EncodeNoteFile(*d.note)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := WriteDoc(dir, d.id, d.kind, data, false); err != nil {
				t.Fatal(err)
			}
			ids[FileName(d.id, d.kind)] = d.about
		}
		raw, _ := json.MarshalIndent(ids, "", "  ")
		if err := os.WriteFile(filepath.Join(dir, "ids.json"), append(raw, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte(examplesReadme), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Every example re-encodes to the bytes on disk (the spelling is gg's).
	for _, d := range exampleDocs() {
		want, err := os.ReadFile(filepath.Join(dir, FileName(d.id, d.kind)))
		if err != nil {
			t.Fatalf("%s: %v (run with -update-examples)", d.id, err)
		}
		var got []byte
		if d.kind == KindOverview {
			got, _ = EncodeOverviewFile(*d.over)
		} else {
			got, _ = EncodeNoteFile(*d.note)
		}
		if string(got) != string(want) {
			t.Errorf("%s differs from the fixture; run -update-examples and read the diff", d.id)
		}
	}
	// A store attached to the examples reads them back: placements as ids.json says.
	wt := t.TempDir()
	for p, text := range exampleTree {
		full := filepath.Join(wt, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s := New()
	s.noWatch = true
	if err := s.Attach(root, wt, dir); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if len(s.Overviews(root)) != 2 || len(s.Problems(root)) != 3 { // README.md, ids.json, tree/ are not documents
		t.Fatalf("overviews %d problems %+v", len(s.Overviews(root)), s.Problems(root))
	}
	data, _ := os.ReadFile(filepath.Join(wt, "internal", "tui", "scroll.go"))
	notes := s.AlignedNotes(root, "internal/tui/scroll.go", Lines(data))
	if len(notes) != 2 || notes[0].ID != "20261018T143220-beef" || !notes[0].Outdated || notes[0].Start != 6 || notes[0].End != 7 ||
		notes[1].ID != "20261018T143205-c0d1" || notes[1].Outdated || notes[1].Start != 15 || notes[1].End != 16 {
		t.Fatalf("scroll.go notes = %+v", notes)
	}
	data, _ = os.ReadFile(filepath.Join(wt, "README.md"))
	notes = s.AlignedNotes(root, "README.md", Lines(data))
	if len(notes) != 1 || notes[0].Outdated || notes[0].Start != 3 || notes[0].End != 4 {
		t.Fatalf("README notes = %+v", notes)
	}
	o, _ := s.Overview("20261018T143201-9f3a")
	s.CheckAnchors(o.ID)
	o, _ = s.Overview(o.ID)
	var missing []string
	for _, a := range o.Anchors {
		if a.Missing {
			missing = append(missing, a.Dest)
		}
	}
	if strings.Join(missing, ",") != "internal/tui/missing.go" {
		t.Fatalf("unresolved = %v", missing)
	}
}

const examplesReadme = "# gg session docs — examples\n\n" +
	"Written by gigagit itself (`internal/agentdocs` `TestExamplesAreWhatGGWrites -update-examples`):\n" +
	"the spelling here is what gg writes, so a reader that reads these reads gg.\n\n" +
	"- `tree/` — the worktree the documents anchor (two files; README.md has CRLF endings).\n" +
	"- `*.md` — overviews, `*.toml` — notes. `ids.json` says what each one shows.\n" +
	"- Expected placements against `tree/`: see `ids.json` (one note is outdated on purpose,\n" +
	"  one overview has one unresolved anchor on purpose).\n"
