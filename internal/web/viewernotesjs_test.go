package web

import "testing"

const voPureStart = "// --- overview model (pure; guarded against Go) ---"
const voPureEnd = "// --- end overview model ---"

func TestViewerOverviewModelJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "viewer.js", voPureStart, voPureEnd, `
const as = [{dest: "a"}, {dest: "b"}, {dest: "c"}];
console.log([
  stepAnchor(as, -1, 1), stepAnchor(as, -1, -1), stepAnchor(as, 2, 1), stepAnchor(as, 0, -1), stepAnchor(as, 1, 1), stepAnchor([], -1, 1),
  anchorStatus({dest: "nope.txt", path: "nope.txt", missing: true}),
  anchorStatus({dest: "note:t9", note: "t9", missing: true}),
  anchorStatus({dest: "a.go", path: "a.go", missing: false}) === "",
  JSON.stringify(anchorTarget({dest: "a.go:2-3", path: "a.go", start: 2, end: 3})),
  JSON.stringify(anchorTarget({dest: "a.go", path: "a.go"})),
  JSON.stringify(anchorTarget({dest: "note:t9", note: "t9", path: "b.go", start: 4, end: 6})),
  JSON.stringify(anchorTarget({dest: "note:t9", note: "t9"})),
  keepAnchor([{dest: "x"}, {dest: "b"}, {dest: "b"}], [{dest: "a"}, {dest: "b"}], 1),
  keepAnchor([{dest: "x"}], [{dest: "a"}, {dest: "b"}], 1),
  keepAnchor([{dest: "x"}], [{dest: "a"}], -1),
].join("|"));
`)
	want := `0|2|0|2|2|-1|no file nope.txt|note t9 is gone|true|{"path":"a.go","line":2,"end":3}|{"path":"a.go","line":0,"end":0}|{"path":"b.go","line":4,"end":6,"note":"t9"}|{"note":"t9"}|1|-1|-1`
	if out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

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
