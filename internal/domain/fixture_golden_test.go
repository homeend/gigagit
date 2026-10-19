package domain

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notebatch"
	"github.com/homeend/gigagit/internal/notemeta"
	"github.com/homeend/gigagit/internal/notes"
)

var updateFixture = flag.Bool("update-fixture", false, "rewrite docs/specs/gg-notes-store/fixture/ from fixtureNotes() (read the diff before committing)")

// The spec fixture's constants (docs/specs/gg-notes-store/fixture/README.md).
const (
	fixtureWorktree = "/home/dev/src/gate"
	fixtureCommit   = "d5efa035df1f3c4c949040bebb924a35d0ea61a0"
	fixturePRHead   = "e4a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3"
	fixturePRBase   = "0a0b0c0d0e0f1a1b2c3d4e5f6a7b8c9d0e1f2a3b"
)

func fixtureDir() string {
	return filepath.Join("..", "..", "docs", "specs", "gg-notes-store", "fixture")
}

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		panic(err)
	}
	return t.UTC()
}

func canonOverview(t *testing.T, in string) string {
	t.Helper()
	d, err := notebatch.ParseOverview([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	return string(d.Canonical())
}

func canonReview(t *testing.T, in string) string {
	t.Helper()
	d, err := notebatch.ParseReview([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	return string(d.Canonical())
}

// fixtureGateLines are internal/x/gate.go's lines 1–7 at fixtureCommit —
// what the line notes' context_hash values fingerprint (fixture README).
var fixtureGateLines = []string{"func gate(closed bool) bool {", "\topen := !closed", "\tif closed {", "\t\treturn false", "\t}", "\treturn open", "}"}

// fixtureNotes is every record of the spec's fixture store, roots before
// their replies (Put files a reply in its root's part). Part 1's records
// come first, unchanged.
func fixtureNotes(t *testing.T) ([]model.Note, []model.ThreadResolution) {
	t.Helper()
	committed := func(path string) model.FileAddress {
		return model.FileAddress{State: model.StateCommitted, Commit: fixtureCommit, Path: path}
	}
	live := func(path string) model.FileAddress {
		return model.FileAddress{State: model.StateUnstaged, Worktree: fixtureWorktree, Path: path}
	}
	overview := canonOverview(t, `{"version":1,"title":"How the gate works","overview":"The gate is one function.\n\n## 1. The check\n\n- [`+"`gate`"+`](internal/x/gate.go:3-4) refuses a closed door.\n- [this line](internal/x/gate.go:2) opens it.\n\n## 2. The caller\n\n- [the call](internal/y/call.go:1) runs the gate first. See [the config](config.toml).\n\nSkipped: nothing. Next: none.","files":[{"path":"internal/x/gate.go","annotations":[{"newRange":[3,4],"summary":"The refusal.","rationale":"A closed door must not pass."}]},{"path":"internal/y/call.go","annotations":[{"newRange":[1,1],"summary":"The only caller."}]}]}`)
	prOverview := canonOverview(t, `{"version":1,"title":"PR 7: the gate, reviewed","overview":"The pull request adds [the gate](internal/x/gate.go:1-4)."}`)
	commitReview := canonReview(t, `{"version":1,"summary":"## Summary\nThe gate refuses a closed door.\n\n## Findings\n- one high finding","overview":"Start at [the refusal](internal/x/gate.go:3-4).","meta":{"verdict":"approve"},"display":{"severity":{"show":["badge","count"],"label":"Severity","order":1,"style":"filled","as":"level","values":{"high":{"color":"red","glyph":"!","level":"high"},"low":{"color":"yellow","level":"low"}},"other":{"color":"grey"}}},"files":[{"path":"internal/x/gate.go","summary":"the gate itself","annotations":[{"newRange":[3,4],"summary":"The refusal returns false.","rationale":"A closed door must not pass.","meta":{"severity":"high"}},{"oldRange":[2,2],"summary":"The old open flag is gone.","meta":{"severity":"low"}}]}]}`)
	prReview := canonReview(t, `{"version":1,"summary":"## Summary\nPR 7 adds the gate. Fine.","files":[{"path":"internal/x/gate.go","annotations":[{"newRange":[1,1],"summary":"Name the parameter.","meta":{"severity":"low"}}]}]}`)
	workingReview := canonReview(t, `{"version":1,"summary":"## Summary\nThe uncommitted change renames open.","files":[{"path":"internal/x/gate.go","annotations":[{"newRange":[2,2],"summary":"Rename it back.","meta":{"severity":"medium"}}]}]}`)
	ns := []model.Note{
		// ---- part 1 (unchanged) ----
		{ID: "7cf0812f", Source: model.NoteSourceAgent, Author: "Claude Opus 5.5", Side: model.NoteSideNew, Tags: []string{"overview"},
			Address: model.FileAddress{State: model.StateCommitted, Commit: fixtureCommit, Branch: "main"},
			Summary: "How the gate works", Rationale: overview, Created: at("2026-10-13T23:53:56.742993129Z"), Updated: at("2026-10-13T23:53:56.742993129Z")},
		{ID: "3a9e1c20", Source: model.NoteSourceAgent, Author: "ada", Side: model.NoteSideNew, Tags: []string{"overview"}, Preview: "main...refs/gg/pr/7",
			Address: model.FileAddress{State: model.StateCommitted, Commit: fixturePRHead},
			Summary: "PR 7: the gate, reviewed", Rationale: prOverview, Created: at("2026-10-14T08:00:00Z"), Updated: at("2026-10-14T08:00:00Z")},
		{ID: "91f0aa07", Source: model.NoteSourceAgent, Author: "ada", Side: model.NoteSideNew, Range: [2]int{3, 4}, Tags: []string{"review"}, Scope: "d5efa035..e4a1b2c3",
			Address: model.FileAddress{State: model.StateCommitted, Commit: fixturePRHead, Path: "internal/x/gate.go"},
			Summary: "A review remark, not an overview", Rationale: "A reader of overview records skips this record: it has a Path and no overview tag.",
			Created: at("2026-10-14T08:05:00Z"), Updated: at("2026-10-14T08:05:00Z")},
		{ID: "b2c3d4e5", Source: model.NoteSourceUser, Side: model.NoteSideNew, Range: [2]int{2, 2}, Tags: []string{"overview"},
			Address: committed("internal/x/gate.go"),
			Summary: "Tagged overview but a FILE note", Rationale: "A reader skips this record: it carries the overview tag but has a Path (not commit-level).",
			Created: at("2026-10-14T08:06:00Z"), Updated: at("2026-10-14T08:06:00Z")},
		{ID: "c3d4e5f6", ParentID: "7cf0812f", Source: model.NoteSourceUser, Side: model.NoteSideNew, Tags: []string{"overview"},
			Address: model.FileAddress{State: model.StateCommitted, Commit: fixtureCommit},
			Summary: "A reply under the overview", Rationale: "A reader skips this record: it has a parent_id (a reply), whatever its tags say.",
			Created: at("2026-10-14T08:07:00Z"), Updated: at("2026-10-14T08:07:00Z")},
		// ---- part 2: reviews ----
		{ID: "c0ffee01", Source: model.NoteSourceAgent, Author: "ada", Side: model.NoteSideNew, Tags: []string{"review"},
			Scope: fixtureCommit + "^.." + fixtureCommit, Address: model.FileAddress{State: model.StateCommitted, Commit: fixtureCommit},
			Summary: "Review: d5efa03 Add the gate (d5efa03^..d5efa03)", Rationale: commitReview,
			Created: at("2026-10-14T09:00:00Z"), Updated: at("2026-10-14T09:00:00Z")},
		{ID: "c0ffee02", Source: model.NoteSourceAgent, Author: "ada", Side: model.NoteSideNew, Tags: []string{"review"},
			Scope: fixturePRBase + ".." + fixturePRHead, Preview: "main...refs/gg/pr/7",
			Address: model.FileAddress{State: model.StateCommitted, Commit: fixturePRHead},
			Summary: "Review: PR #7 (0a0b0c0..e4a1b2c)", Rationale: prReview,
			Created: at("2026-10-14T09:10:00Z"), Updated: at("2026-10-14T09:10:00Z")},
		// a reply to remark 0 of the PR review (lives in the review's part: previews.toml)
		{ID: "c0ffee03", ParentID: "c0ffee02", Source: model.NoteSourceUser, Author: "me", Side: model.NoteSideNew,
			Address: model.FileAddress{State: model.StateCommitted, Commit: fixturePRHead},
			Summary: "Done in the next push.", Remark: "review:c0ffee02:0",
			RemarkFP: remarkFP("internal/x/gate.go", model.NoteSideNew, [2]int{1, 1}, "Name the parameter."), RemarkSummary: "Name the parameter.",
			Created: at("2026-10-14T09:20:00Z"), Updated: at("2026-10-14T09:20:00Z")},
		// ---- part 2: a committed line note, its reply ----
		{ID: "aa110001", Source: model.NoteSourceUser, Author: "me", Side: model.NoteSideNew, Range: [2]int{3, 4},
			ContextHash: model.NoteContextHash(fixtureGateLines[2:4]), Address: committed("internal/x/gate.go"),
			Summary: "Why not return early?", Rationale: "The open flag is computed and then ignored on this path.",
			Meta:    []notemeta.KV{{Key: "kind", Value: "question"}},
			Display: notemeta.Display{"kind": {Show: []string{"badge"}, Values: map[string]notemeta.Value{"question": {Color: "blue", Glyph: "?"}}}},
			Added:   []notemeta.Added{{Key: "status", Value: "agreed", By: "ada", At: at("2026-10-14T10:05:00Z")}},
			Created: at("2026-10-14T10:00:00Z"), Updated: at("2026-10-14T10:00:00Z")},
		{ID: "aa110002", ParentID: "aa110001", Source: model.NoteSourceAgent, Author: "ada", Side: model.NoteSideNew, Range: [2]int{3, 4},
			ContextHash: model.NoteContextHash(fixtureGateLines[2:4]), Address: committed("internal/x/gate.go"),
			Summary: "Agreed; an early return reads better.", Link: "gg://gate/internal/x/gate.go@" + fixtureCommit + ":3",
			Created: at("2026-10-14T10:04:00Z"), Updated: at("2026-10-14T10:04:00Z")},
		// ---- part 2: live notes in the worktree part ----
		{ID: "bb220001", Source: model.NoteSourceUser, Author: "me", Side: model.NoteSideNew, Range: [2]int{6, 6},
			ContextHash: model.NoteContextHash(fixtureGateLines[5:6]), Address: live("internal/x/gate.go"),
			Summary: "Still returns the flag.", Created: at("2026-10-14T11:00:00Z"), Updated: at("2026-10-14T11:00:00Z")},
		{ID: "bb220002", Source: model.NoteSourceAgent, Author: "ada", Side: model.NoteSideNew, Tags: []string{"review"},
			Address: model.FileAddress{State: model.StateUnstaged, Worktree: fixtureWorktree},
			Files:   []model.NoteFile{{Path: "internal/x/gate.go", Blob: "5d2a7c1e9b3f4a6d8c0e2f1a3b5c7d9e1f3a5b7c"}},
			Summary: "Review: working changes", Rationale: workingReview,
			Created: at("2026-10-14T11:30:00Z"), Updated: at("2026-10-14T11:30:00Z")},
	}
	res := []model.ThreadResolution{{Root: "aa110001", By: "me", At: at("2026-10-14T10:06:00Z")}}
	return ns, res
}

// writeFixture writes every record through gg's own store into dir.
func writeFixture(t *testing.T, dir string) {
	t.Helper()
	ns, res := fixtureNotes(t)
	st := notes.NewFileStore(dir)
	for _, n := range ns {
		if err := st.Put(n); err != nil {
			t.Fatalf("put %s: %v", n.ID, err)
		}
	}
	for _, r := range res {
		if err := st.Resolve(r); err != nil {
			t.Fatalf("resolve %s: %v", r.Root, err)
		}
	}
}

func fixtureWorktreeKey() string {
	return strings.TrimPrefix(string(notes.WorktreePart(fixtureWorktree)), "wt-")
}

func fixtureFiles() []string {
	return []string{"commits.toml", "previews.toml", filepath.Join("worktrees", fixtureWorktreeKey()+".toml")}
}

// The committed fixture is byte-for-byte what gg's store writes for
// fixtureNotes (spec part 2 §9). -update-fixture regenerates it.
func TestFixtureIsWhatGGWrites(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir)
	for _, name := range fixtureFiles() {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("gg wrote no %s: %v", name, err)
		}
		target := filepath.Join(fixtureDir(), name)
		if *updateFixture {
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(target, got, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(target)
		if err != nil {
			t.Fatalf("%s: %v (run with -update-fixture)", name, err)
		}
		if string(want) != string(got) {
			t.Errorf("%s differs from what gg writes (run with -update-fixture and read the diff)", name)
		}
	}
}

// Review focus 3: the worktree part's key is the hash of the path stored in
// its records — the reader's self-check (spec part 2 §5.1).
func TestFixtureWorktreeKeyMatchesRecord(t *testing.T) {
	t.Parallel()
	wkey := fixtureWorktreeKey()
	if len(wkey) != 16 || wkey != repoKey(fixtureWorktree) {
		t.Fatalf("wkey %q is not sha256(Clean(%q))[:8] = %q", wkey, fixtureWorktree, repoKey(fixtureWorktree))
	}
	data, err := os.ReadFile(filepath.Join(fixtureDir(), "worktrees", wkey+".toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Worktree = '"+fixtureWorktree+"'") {
		t.Fatalf("worktree part does not carry its path:\n%s", data)
	}
}
