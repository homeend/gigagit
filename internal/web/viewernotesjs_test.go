package web

import (
	"os"
	"strings"
	"testing"
)

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
  keepAnchor([{dest: "x"}, {dest: "b"}, {dest: "b"}], "b", 1),
  keepAnchor([{dest: "x"}], "b", 1),
  keepAnchor([{dest: "x"}], "", -1),
  keepAnchor([{dest: "a"}, {dest: "b"}], "b", -1),
  keepAnchor([{dest: "b"}, {dest: "a"}, {dest: "b"}], "b", 2),
  backAnchor([{dest: "new"}, {dest: "a"}, {dest: "lock"}], {sel: 1, dest: "lock"}),
  backAnchor([{dest: "a"}, {dest: "b"}], {sel: 1, dest: "gone"}),
  backAnchor([{dest: "a"}], {sel: 3, dest: "gone"}),
].join("|"));
`)
	want := `0|2|0|2|2|-1|no file nope.txt|note t9 is gone|true|{"path":"a.go","line":2,"end":3}|{"path":"a.go","line":0,"end":0}|{"path":"b.go","line":4,"end":6,"note":"t9"}|{"note":"t9"}|1|-1|-1|1|2|2|1|-1`
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

// A tab whose open failed after the server moved it there gives the server
// back what it really shows: the file still on screen, else nothing.
func TestReleaseAfterFailedOpenJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "viewer.js", voPureStart, voPureEnd, `console.log([
  releaseAfterFailedOpen("f2", "f1"), releaseAfterFailedOpen("f2", ""), releaseAfterFailedOpen("f2", "f2"), releaseAfterFailedOpen("", "f1"),
].map((x) => JSON.stringify(x)).join("|"));`)
	if want := `{"op":"focus","id":"f1"}|{"op":"background","id":"f2"}|null|null`; out != want {
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

func TestViewerAnchorBandsJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "viewer.js", voPureStart, voPureEnd, `
const as = [
  {dest: "a.go", path: "a.go"},
  {dest: "a.go:12", path: "a.go", start: 12, end: 12},
  {dest: "a.go:5-8", path: "a.go", start: 5, end: 8},
  {dest: "b.go:3", path: "b.go", start: 3, end: 3},
  {dest: "note:t1", note: "t1", path: "a.go", start: 20, end: 20},
  {dest: "./a.go:5-8", path: "a.go", start: 5, end: 8},
  {dest: "a.go:40", path: "a.go", start: 40, end: 40},
  {dest: "a.go:28-35", path: "a.go", start: 28, end: 35},
];
const bs = anchorBands(as, "a.go", 30);
const st = (c, l, d) => { const r = stepBand(bs, c, l, d); return r.i + (r.wrapped ? "w" : ""); };
console.log([
  JSON.stringify(bs),
  bandOf(bs, as, "a.go:12", 30), bandOf(bs, as, "./a.go:5-8", 30), bandOf(bs, as, "gone", 30), bandOf(bs, as, "", 30), bandOf(bs, as, "a.go:28-35", 30),
  st(1, 12, 1), st(2, 28, 1), st(0, 6, -1), st(-1, 1, 1), st(-1, 30, 1), st(-1, 20, -1), st(-1, 3, -1),
  stepBand([], -1, 5, 1).i,
  JSON.stringify(stepBand(bs.slice(0, 1), 0, 5, 1)),
  bandKindAt(bs, 1, 12), bandKindAt(bs, 1, 6), bandKindAt(bs, 1, 9),
  JSON.stringify(anchorBands([{dest: "x:2", path: "x", start: 2}], "x", 10)),
].join("|"));
`)
	want := `[{"start":5,"end":8,"i":2},{"start":12,"end":12,"i":1},{"start":28,"end":30,"i":7}]|1|0|-1|-1|2|2|0w|2w|0|0w|1|2w|-1|{"i":0,"wrapped":false}|cur|other||[{"start":2,"end":2,"i":0}]`
	if out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

// The viewer's wiring of the bands: n / p always the viewer's (a p there is
// never the global pull), an anchor open no longer sets the reader's range,
// and a change in the store re-reads the anchors of the overview that opened
// the file.
func TestViewerAnchorBandsWiring(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile("static/viewer.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, want := range []string{`case "n": stepViewerAnchor(1)`, `case "p": stepViewerAnchor(-1)`, `cur: t.note ? "" : a.dest`, "refreshFromAnchors(f)", "bandKindAt(bands, curB, i + 1)"} {
		if !strings.Contains(src, want) {
			t.Errorf("viewer.js lacks %q", want)
		}
	}
	// from reads t: t must be declared first (a use before it throws, and
	// enter on an anchor silently opened nothing).
	fn := src[strings.Index(src, "async function openAnchorAt("):]
	fn = fn[:strings.Index(fn, "\n}\n")]
	if ti, fi := strings.Index(fn, "const t = anchorTarget(a)"), strings.Index(fn, "const from = {"); ti < 0 || fi < 0 || fi < ti {
		t.Errorf("openAnchorAt builds from (at %d) before t (at %d)", fi, ti)
	}
	if strings.Contains(src, "view.range = t.end > t.line") {
		t.Error("an anchor open still sets the reader's range")
	}
	css, err := os.ReadFile("static/style.css")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(css), ".vline.vanchor.acur") {
		t.Error("style.css has no current-band rule")
	}
}
