package web

import "testing"

const vnPureStart = "// --- note model (pure; guarded against Go) ---"
const vnPureEnd = "// --- end note model ---"

func TestViewerNoteModelJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "viewer.js", vnPureStart, vnPureEnd, `
const ns = [
  {id: "t2", start: 3, end: 4, author: "agent", outdated: false},
  {id: "t5", start: 9, end: 9, author: "claude", outdated: true},
];
console.log([
  noteBoxTitle(ns[0]), noteBoxTitle(ns[1]),
  (noteAtLine(ns, 4) || {}).id, String(noteAtLine(ns, 5)),
  boxesAfter(ns, 3).map((n) => n.id).join("+"), boxesAfter(ns, 2).length,
  nextNoteLine(ns, 1, 1), nextNoteLine(ns, 3, 1), nextNoteLine(ns, 9, 1),
  nextNoteLine(ns, 9, -1), nextNoteLine(ns, 3, -1),
  nextNotedFile([{id: "f7", notes: 1}, {id: "f2", notes: 0}, {id: "f3", notes: 2}], "f3", 1),
  nextNotedFile([{id: "f7", notes: 1}, {id: "f3", notes: 2}], "f3", -1),
  nextNotedFile([{id: "f3", notes: 2}], "f3", 1),
  nextNotedFile([{id: "f2", notes: 1}, {id: "f9", notes: 1}, {id: "f5", notes: 1}], "f5", 1),
  nextNotedFile([{id: "f2", notes: 1}, {id: "f9", notes: 1}, {id: "f5", notes: 1}], "f5", -1),
].join("|"));
`)
	want := "note t2 · agent · lines 3–4|note t5 · claude · line 9 · outdated|t2|null|t2|0|3|9|0|3|0|f7|f7||f9|f2"
	if out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}
