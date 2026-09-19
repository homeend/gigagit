package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// notebox.js is import-free so it can run under node: a note box's title and
// the collapse set are pinned against the wire fields the server really sends
// (read_only / resolved / file_level / created).
func TestNoteboxJS(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	src, err := os.ReadFile(filepath.Join("static", "notebox.js"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notebox.mjs"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	const runner = `
import { noteTitle, noteAge, seedCollapsed, toggleCollapsed, setAllCollapsed } from "./notebox.mjs";
const now = Date.parse("2026-09-20T12:00:00Z");
const forge = { id: "forge:C1", source: "forge", author: "carol", side: "new", line: 3, status: "active",
  read_only: true, created: "2026-09-17T12:00:00Z" };
const titles = [
  noteTitle(forge, "a.go", true, now),
  noteTitle({ ...forge, resolved: true, side: "old" }, "a.go", true, now),
  noteTitle({ ...forge, file_level: true, line: 0, author: "" }, "b.txt", true, now),
  noteTitle({ id: "n1", source: "agent", author: "bot", side: "new", line: 9, status: "stale" }, "a.go", false, now),
  noteTitle({ id: "n2", source: "user", side: "old", line: 4, status: "outdated" }, "a.go", true, now),
  noteTitle({ id: "n3", source: "user", side: "new", line: 4, status: "active" }, "a.go", false, now),
];
const ages = ["", "garbage", "2026-09-20T11:59:50Z", "2026-09-20T11:15:00Z", "2026-09-20T02:00:00Z", "2026-09-01T12:00:00Z", "2025-01-01T00:00:00Z"]
  .map((c) => noteAge(c, now));
const notes = [{ id: "a", resolved: true, replies: [{ id: "a1", resolved: true }] }, { id: "b" }, { id: "c", resolved: true }];
const seed = seedCollapsed(notes);
const set = new Set();
const t1 = toggleCollapsed(set, "b"), after1 = [...set], t2 = toggleCollapsed(set, "b"), after2 = [...set];
const allOn = [...setAllCollapsed(new Set(), notes, true)].sort();
const allOff = [...setAllCollapsed(new Set(["a", "zz"]), notes, false)];
console.log(JSON.stringify({ titles, ages, seed: [...seed].sort(), t1, after1, t2, after2, allOn, allOff }));
`
	if err := os.WriteFile(filepath.Join(dir, "run.mjs"), []byte(runner), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, filepath.Join(dir, "run.mjs")).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var got struct {
		Titles, Ages, Seed, After1, After2, AllOn, AllOff []string
		T1, T2                                          bool
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("decode %s: %v", out, err)
	}
	wantTitles := []string{
		"review · carol · 3d · a.go R3",
		"review · carol · 3d · a.go L3 · resolved",
		"review · 3d · b.txt (file)",
		"agent note · bot · a.go R9 (stale)",
		"note · a.go L4 (outdated)",
		"note · a.go R4",
	}
	eq := func(what string, got, want []string) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("%s = %q, want %q", what, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%s[%d] = %q, want %q", what, i, got[i], want[i])
			}
		}
	}
	eq("titles", got.Titles, wantTitles)
	eq("ages", got.Ages, []string{"", "", "now", "45m", "10h", "19d", "1y"})
	eq("seed", got.Seed, []string{"a", "c"}) // roots only: a reply has no box of its own
	if !got.T1 || got.T2 {
		t.Errorf("toggle returned %v then %v, want true then false", got.T1, got.T2)
	}
	eq("after first toggle", got.After1, []string{"b"})
	eq("after second toggle", got.After2, []string{})
	eq("collapse all", got.AllOn, []string{"a", "b", "c"})
	eq("expand all", got.AllOff, []string{"zz"}) // only THIS diff's roots are touched
}
