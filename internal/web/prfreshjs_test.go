package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// prfresh.js: the open PR's freshness word (spec §2.4, the TUI's
// prSeen/prUpdated rules) — never "updated" on a PR's first read, nor for
// the change your own send made.
func TestPRFreshJS(t *testing.T) {
	t.Parallel()
	got := runFreshJS(t, `
step({ n: 7, kind: "start" });
step({ n: 7, kind: "ok", changed: true });
step({ n: 7, kind: "ok", changed: true });
step({ n: 7, kind: "ok", changed: false });
step({ n: 7, kind: "sent" });
step({ n: 7, kind: "ok", changed: true });
step({ n: 7, kind: "ok", changed: true });
step({ n: 7, kind: "fail", age: "3h" });
step({ n: 8, kind: "ok", changed: true });
step({ n: 8, kind: "start" });
`)
	want := []string{"refreshing…", "", "updated", "", "", "", "updated", "offline · read 3h", "", "refreshing…"}
	if !slices.Equal(got, want) {
		t.Fatalf("steps = %q\nwant    %q", got, want)
	}
}

// Item 1 (F2): the read that absorbs my own send is the first one that
// STARTED after it; a read in flight when the send ended does not absorb it
// (a change it reports still shows), and a post-send read with nothing new
// disarms it.
func TestPRFreshJSReadOrder(t *testing.T) {
	t.Parallel()
	got := runFreshJS(t, `
step({ n: 7, kind: "ok", changed: false, seq: 1 });
step({ n: 7, kind: "sent", seq: 2 });
step({ n: 7, kind: "ok", changed: false, seq: 2 });
step({ n: 7, kind: "ok", changed: true, seq: 3 });
step({ n: 7, kind: "ok", changed: true, seq: 4 });
step({ n: 7, kind: "sent", seq: 4 });
step({ n: 7, kind: "ok", changed: false, seq: 5 });
step({ n: 7, kind: "ok", changed: true, seq: 6 });
`)
	want := []string{"", "", "", "", "updated", "", "", "updated"}
	if !slices.Equal(got, want) {
		t.Fatalf("steps = %q\nwant    %q", got, want)
	}
}

// runFreshJS runs steps against prfresh.js under node and returns the text
// after each step.
func runFreshJS(t *testing.T, steps string) []string {
	t.Helper()
	out := runFreshModuleJS(t, `
import { nextFresh } from "./prfresh.mjs";
let st = { seen: 0, updated: 0, ownSend: null };
const steps = [];
const step = (ev) => { st = nextFresh(st, ev); steps.push(st.text); };
`+steps+`
console.log(JSON.stringify(steps));
`)
	var got []string
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	return got
}

// runFreshModuleJS runs script (an ES module that imports ./prfresh.mjs)
// under node and returns its stdout.
func runFreshModuleJS(t *testing.T, script string) []byte {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	src, err := os.ReadFile(filepath.Join("static", "prfresh.js"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "prfresh.mjs"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run.mjs"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, filepath.Join(dir, "run.mjs")).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	return out
}

// C9: one read at a time; a read that cannot start waits for the running one
// (one per key, the newest wins) with no give-up timer. F-i: only a send
// that changed GitHub arms the own-send absorb.
func TestPRFreshJSSerialReads(t *testing.T) {
	t.Parallel()
	out := runFreshModuleJS(t, `
import { serialReads, sentEvent } from "./prfresh.mjs";
let slot = null; // the one running read's "end" switch
const gate = (fn) => {
  if (slot) return null;
  fn();
  return new Promise((r) => (slot = r)).then(() => { slot = null; });
};
const end = async () => { slot(); await new Promise((r) => setTimeout(r, 0)); };
const log = [];
const mark = (name) => () => log.push(name);
const reads = serialReads(gate);
reads.run(mark("a"));
const busyRun = reads.run(mark("x")) === null;
reads.soon("k", mark("b"));
reads.soon("k", mark("c")); // the newest of key k wins
await end(); // a ends: c runs, b never
await end(); // c ends
reads.soon("j", mark("d")); // nothing running: at once
await end();
console.log(JSON.stringify({ order: log, busyRun,
  sent: [sentEvent({ ok: true, changed: true }, 4), sentEvent({ ok: true, changed: false }, 4), sentEvent({ ok: false }, 4)] }));
`)
	var got struct {
		Order   []string         `json:"order"`
		BusyRun bool             `json:"busyRun"`
		Sent    []map[string]any `json:"sent"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if !slices.Equal(got.Order, []string{"a", "c", "d"}) || !got.BusyRun {
		t.Fatalf("order=%v busyRun=%v", got.Order, got.BusyRun)
	}
	want := []map[string]any{{"kind": "sent", "seq": float64(4)}, nil, nil}
	if !reflect.DeepEqual(got.Sent, want) {
		t.Fatalf("sent = %v, want %v", got.Sent, want)
	}
}

// prs.js routes every freshness change through nextFresh and shows the
// interrupted bar from both refresh answers.
func TestPRsFreshnessAndInterruptedAreWired(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("static", "prs.js"))
	if err != nil {
		t.Fatal(err)
	}
	js := string(b)
	for _, want := range []string{
		"fresh = nextFresh(fresh, { n, ...ev });",
		"showInterrupted(n, rv.interrupted);",
		"showInterrupted(n, r.interrupted);",
		`const sent = sentEvent(ev, readSeq);`, // the rule: TestPRFreshJSSerialReads
		`if (sent) freshEvent(n, sent);`,
		`refreshAfterSend(n);`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("prs.js lacks %q", want)
		}
	}
	if strings.Contains(js, "read-only — gg never") {
		t.Error("prs.js's help still says gg never writes to the forge")
	}
}
