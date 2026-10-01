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
  backAnchor([{dest: "new"}, {dest: "a"}, {dest: "lock"}], {sel: 1, dest: "lock"}),
  backAnchor([{dest: "a"}, {dest: "b"}], {sel: 1, dest: "gone"}),
  backAnchor([{dest: "a"}], {sel: 3, dest: "gone"}),
].join("|"));
`)
	want := `0|2|0|2|2|-1|no file nope.txt|note t9 is gone|true|{"path":"a.go","line":2,"end":3}|{"path":"a.go","line":0,"end":0}|{"path":"b.go","line":4,"end":6,"note":"t9"}|{"note":"t9"}|1|-1|-1|2|1|-1`
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

// The page-side minors: how esc / the backdrop leave a document, how a
// document is named in a status line, what backspace's list fetch means,
// and when an agentdocs change re-fetches the overview on screen.
func TestViewerDocumentHelpersJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "viewer.js", voPureStart, voPureEnd, `
console.log([
  escHow(true, 0), escHow(false, 2), escHow(false, 0),
  docName({ov: {title: "The \"tour\""}, id: "f5", path: "overview-5.md"}), docName({ov: null, id: "f2", path: "a/b.go"}),
  backOutcome(null, "f5"), backOutcome([{id: "f5"}], "f5"), backOutcome([{id: "f2"}], "f5"),
  overviewStale({f5: "a"}, "f5", "a"), overviewStale({f5: "b"}, "f5", "a"), overviewStale(undefined, "f5", "a"), overviewStale({}, "f5", "a"),
].join("|"));
`)
	want := `background|background|close|overview f5 "The \"tour\""|a/b.go|error|ok|closed|false|true|true|true`
	if out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

// An overview refresh's answer applies unless a newer one already did: a
// newer refresh merely started (it may fail) does not drop it.
func TestOvAnswerAppliesJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "viewer.js", voPureStart, voPureEnd, `console.log([ovAnswerApplies(1, 0), ovAnswerApplies(1, 2), ovAnswerApplies(2, 2), ovAnswerApplies(3, 2)].join("|"));`)
	if want := "true|false|false|true"; out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

// An eviction line carries the cap the server sent, not a number of its own.
func TestEvictedTextJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "viewer.js", voPureStart, voPureEnd, `console.log([evictedText("a.go", 20), evictedText("b.go", 7)].join("|"));`)
	if want := "closed a.go (20 files open)|closed b.go (7 files open)"; out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}
