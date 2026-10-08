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

// B2: a second moved-head follow of the same PR while one runs is skipped;
// another PR's, or the same PR's after the first ended, runs.
func TestPRFreshJSOncePerKey(t *testing.T) {
	t.Parallel()
	out := runFreshModuleJS(t, `
import { oncePerKey } from "./prfresh.mjs";
const ends = [];
const runs = [];
const follow = oncePerKey((n) => { runs.push(n); return new Promise((r) => ends.push(r)); });
const first = follow(7);
await new Promise((r) => setTimeout(r, 0));
const dup = follow(7) === null;
follow(8);
await new Promise((r) => setTimeout(r, 0));
ends[0](); await first; await new Promise((r) => setTimeout(r, 0));
follow(7);
await new Promise((r) => setTimeout(r, 0));
console.log(JSON.stringify({ runs, dup }));
`)
	var got struct {
		Runs []int `json:"runs"`
		Dup  bool  `json:"dup"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if !slices.Equal(got.Runs, []int{7, 8, 7}) || !got.Dup {
		t.Fatalf("runs=%v dup=%v", got.Runs, got.Dup)
	}
}

// B2: prs.js follows a moved head through oncePerKey, and the reopen's
// comments read waits for a running read instead of being dropped.
func TestPRsJSFollowsOnceAndQueuesTheReopenRead(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("static", "prs.js"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, want := range []string{
		"const followMovedHead = oncePerKey(",
		`soonComments("comments:" + n, moved, (mv) => commentsRead(n, mv))`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("prs.js lacks %q", want)
		}
	}
}

// B7: a moved-head comments read replaced by a plain one while it waits
// keeps its flag ("updated"); the flag clears once a read starts.
func TestPRFreshJSStickyFlag(t *testing.T) {
	t.Parallel()
	out := runFreshModuleJS(t, `
import { serialReads, stickyFlag } from "./prfresh.mjs";
let slot = null;
const gate = (fn) => {
  if (slot) return null;
  fn();
  return new Promise((r) => (slot = r)).then(() => { slot = null; });
};
const end = async () => { slot(); await new Promise((r) => setTimeout(r, 0)); };
const reads = serialReads(gate);
const soon = stickyFlag(reads);
const seen = [];
const read = (f) => () => seen.push(f);
reads.run(() => {});            // a read in flight
soon("comments:7", true, read);  // moved: waits
soon("comments:7", false, read); // replaces it while it waits
await end();                     // the waiting read runs
await end();
soon("comments:7", false, read); // a later plain read
await end();
console.log(JSON.stringify(seen));
`)
	var got []bool
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if !slices.Equal(got, []bool{true, false}) {
		t.Fatalf("flags = %v, want [true false]", got)
	}
}

// B2: coveredReads — a call made while a read runs settles only after the
// follow-up read it queued, never on the stale answer in flight.
func TestPRFreshJSCoveredReads(t *testing.T) {
	t.Parallel()
	out := runFreshModuleJS(t, `
import { coveredReads } from "./prfresh.mjs";
let busy = false, n = 0;
const gate = (fn) => (busy ? null : (busy = true, fn().finally(() => (busy = false))));
const rel = [];
const read = () => new Promise((r) => rel.push(() => { n++; r(); }));
const tick = () => new Promise((r) => setTimeout(r, 0));
const call = coveredReads(gate, read);
const log = [];
const a = call().then(() => log.push("a:" + n));
const b = call().then(() => log.push("b:" + n)); // mid-flight
rel.shift()();
await tick();
const early = log.length; // nothing may settle on the first read alone
rel.shift()();
await Promise.all([a, b]);
const idle = await Promise.race([coveredReads(() => null, read)().then(() => "now"), tick().then(() => "late")]);
console.log(JSON.stringify({ log, early, reads: n, idle }));
`)
	var got struct {
		Log   []string `json:"log"`
		Early int      `json:"early"`
		Reads int      `json:"reads"`
		Idle  string   `json:"idle"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if got.Early != 0 || got.Reads != 2 || !slices.Equal(got.Log, []string{"a:2", "b:2"}) || got.Idle != "now" {
		t.Fatalf("got %+v", got)
	}
}

// B2: readyLatch — wait(ms) is true once open() ran, false on the timeout.
func TestPRFreshJSReadyLatch(t *testing.T) {
	t.Parallel()
	out := runFreshModuleJS(t, `
import { readyLatch } from "./prfresh.mjs";
const l = readyLatch();
const w = l.wait(1000);
l.open();
console.log(JSON.stringify([await w, await l.wait(5), await readyLatch().wait(5)]));
`)
	var got []bool
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if !slices.Equal(got, []bool{true, true, false}) {
		t.Fatalf("got %v", got)
	}
}

// B2: prs.js reads the list through coveredReads and the landing waits for
// the server's first listing.
func TestPRsJSLandingWaitsForTheList(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("static", "prs.js"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, want := range []string{
		"coveredReads(",
		"if (liveListing(body)) listLoaded.open();",
		"waitList: () => listLoaded.wait(LANDING_LIST_MS)",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("prs.js lacks %q", want)
		}
	}
}

// B1: exclusive — one task at a time; idle() waits for the running one, and
// the landing's loop (busy → wait → retry) gets its turn once it ends.
func TestPRFreshJSExclusive(t *testing.T) {
	t.Parallel()
	out := runFreshModuleJS(t, `
import { exclusive } from "./prfresh.mjs";
const tick = () => new Promise((r) => setTimeout(r, 0));
const ex = exclusive();
let rel;
const p = ex.try(() => new Promise((r) => (rel = r)));
const busy = ex.try(async () => 1) === null;
const log = [];
const w = ex.idle().then(() => log.push("idle"));
await tick();
log.push("before");
rel();
await p;
await w;
const now = await Promise.race([ex.idle().then(() => "now"), tick().then(() => "late")]);
let rel2;
ex.try(() => new Promise((r) => (rel2 = r)));
let p2;
const got = (async () => { while (!(p2 = ex.try(async () => "mine"))) await ex.idle(); return p2; })();
await tick();
rel2();
console.log(JSON.stringify({ busy, log, now, mine: await got }));
`)
	var got struct {
		Busy bool     `json:"busy"`
		Log  []string `json:"log"`
		Now  string   `json:"now"`
		Mine string   `json:"mine"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if !got.Busy || !slices.Equal(got.Log, []string{"before", "idle"}) || got.Now != "now" || got.Mine != "mine" {
		t.Fatalf("got %+v", got)
	}
}

// B1: a PR link landing never calls openPR (whose false means busy AND
// failed): it waits for the open in flight and retries through opens.try.
func TestPRsJSLandingWaitsForAnOpenInFlight(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("static", "prs.js"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	i := strings.Index(src, "export async function openPRLanding(")
	if i < 0 {
		t.Fatal("openPRLanding is gone")
	}
	body := src[i:]
	if j := strings.Index(body, "\n}\n"); j > 0 {
		body = body[:j]
	}
	for _, want := range []string{"landPR(", "idle: () => opens.idle()", "tryOpen: (pr) => opens.try(() => openPRNow(pr))"} {
		if !strings.Contains(body, want) {
			t.Errorf("openPRLanding lacks %q", want)
		}
	}
	if strings.Contains(body, "openPR(pr)") {
		t.Error("openPRLanding still calls openPR")
	}
}

// Final review #2: a CACHED listing answers loaded:true before the live one
// lands — the cold-page landing must keep waiting for the live rows.
func TestPRFreshJSLiveListing(t *testing.T) {
	t.Parallel()
	out := runFreshModuleJS(t, `
import { liveListing } from "./prfresh.mjs";
console.log(JSON.stringify([
  liveListing({ loaded: true, cached: true }),
  liveListing({ loaded: true, cached: false }),
  liveListing({ loaded: false }),
  liveListing({ loaded: true, available: false }),
]));
`)
	var got []bool
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if !slices.Equal(got, []bool{false, true, false, true}) {
		t.Fatalf("got %v", got)
	}
}

// Final review #1: previewOpen outlives the PR view (a commit opened after
// it keeps it set), so the landing never skips the open on it — it would
// land in the commit's file list. And the latch opens on the LIVE listing.
func TestPRsJSLandingTrustsNoStalePreviewOpen(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("static", "prs.js"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	i := strings.Index(src, "export async function openPRLanding(")
	if i < 0 {
		t.Fatal("openPRLanding is gone")
	}
	body := src[i:]
	if j := strings.Index(body, "\n}\n"); j > 0 {
		body = body[:j]
	}
	if strings.Contains(body, "previewOpen") {
		t.Error("openPRLanding trusts state.previewOpen")
	}
	if !strings.Contains(src, "if (liveListing(body)) listLoaded.open();") {
		t.Error("the list latch does not wait for the live listing")
	}
}

// A failed open rejects only the promise its caller holds: exclusive's own
// bookkeeping must not leave a second, unhandled rejection behind.
func TestPRFreshJSExclusiveRejects(t *testing.T) {
	t.Parallel()
	out := runFreshModuleJS(t, `
import { exclusive } from "./prfresh.mjs";
let unhandled = 0;
process.on("unhandledRejection", () => { unhandled++; });
const ex = exclusive();
ex.try(() => Promise.reject(new Error("x"))).catch(() => {});
await new Promise((r) => setTimeout(r, 20));
const idle = await Promise.race([ex.idle().then(() => "now"), new Promise((r) => setTimeout(() => r("late"), 20))]);
console.log(JSON.stringify({ unhandled, idle }));
`)
	var got struct {
		Unhandled int    `json:"unhandled"`
		Idle      string `json:"idle"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if got.Unhandled != 0 || got.Idle != "now" {
		t.Fatalf("got %+v", got)
	}
}

// The PR link landing's order (prs.js openPRLanding → landPR): a running
// open first, then the list (one newer read, then the first live listing),
// then the open — waiting while another runs; a failed open is false.
func TestPRFreshJSLandPR(t *testing.T) {
	t.Parallel()
	out := runFreshModuleJS(t, `
import { landPR } from "./prfresh.mjs";
async function run(knownAfter, opens) {
  const log = [];
  let stage = 0; // 0 = before fetchList, 1 = after it, 2 = after waitList
  const d = {
    idle: async () => log.push("idle"),
    known: (n) => (knownAfter !== null && stage >= knownAfter ? { number: n } : null),
    fetchList: async () => { log.push("fetchList"); stage = 1; },
    waitList: async () => { log.push("waitList"); stage = 2; },
    tryOpen: () => { log.push("tryOpen"); const o = opens.shift(); return o === null ? null : Promise.resolve(o); },
  };
  const got = await landPR(d, 7);
  return { log, got };
}
console.log(JSON.stringify([
  await run(0, [true]),
  await run(1, [true]),
  await run(2, [true]),
  await run(null, []),
  await run(0, [null, false]),
]));
`)
	type res struct {
		Log []string `json:"log"`
		Got *bool    `json:"got"`
	}
	var got []res
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	tr, fa := true, false
	want := []res{
		{[]string{"idle", "tryOpen"}, &tr},
		{[]string{"idle", "fetchList", "tryOpen"}, &tr},
		{[]string{"idle", "fetchList", "waitList", "tryOpen"}, &tr},
		{[]string{"idle", "fetchList", "waitList"}, nil},
		{[]string{"idle", "tryOpen", "idle", "tryOpen"}, &fa},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

// Follow-ups 5, item 6: a PR fetch's done line replaces the "⟳ fetching…"
// line — it never stays on screen after the head arrived.
func TestPRFreshJSFetchDoneLine(t *testing.T) {
	t.Parallel()
	out := runFreshModuleJS(t, `
import { prFetchDoneLine } from "./prfresh.mjs";
console.log(JSON.stringify([
  prFetchDoneLine({ ok: true, summary: "fetched refs/gg/pr/7" }, 7),
  prFetchDoneLine({ ok: true }, 7),
  prFetchDoneLine({ ok: false, error: "no network" }, 7),
  prFetchDoneLine({ ok: false }, 7),
]));
`)
	var got [][2]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	want := [][2]any{{"fetched refs/gg/pr/7", false}, {"fetched pull request #7", false}, {"error: no network", true}, {"error: operation failed", true}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
}
