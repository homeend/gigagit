package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The browser's single-flight task gate (core.js runOnce) is what keeps a
// mashed `r` from starting four overlapping reloads — four fans of the same
// ten requests at a server that answers them one at a time under the repo
// gate. Nothing else guards it: the failure it prevents is a slow, churning
// UI rather than a crash, so a regression would ship quietly.
//
// Only the PURE section of core.js is evaluated — the rest of the module
// reaches for `document`/`localStorage` and cannot run under node.
const runOnceStart = "// --- single-flight task gate (pure; guarded against node) ---"
const runOnceEnd = "// --- end single-flight task gate ---"

// runOnceHarness drives the extracted gate with a FAKE clock, so the timeout
// backstop is testable without waiting a minute for it.
const runOnceHarness = `
const out = {};
let clock = 1000;
const now = () => clock;

// a task that never settles by itself
let releaseA = null;
const hang = () => new Promise((r) => { releaseA = r; });

// 1. the second start of a live type is dropped, and only ONE task ran
let ran = 0;
const first = runOnce("refresh", () => { ran++; return hang(); }, {now, timeout: 5000});
const second = runOnce("refresh", () => { ran++; return hang(); }, {now, timeout: 5000});
out.firstStarted = first !== null;
out.secondDropped = second === null;
out.ranWhileLive = ran;

// 2. a DIFFERENT type is never blocked by the live one
let otherRan = 0;
const other = runOnce("health", () => { otherRan++; return Promise.resolve("ok"); }, {now, timeout: 5000});
out.otherTypeStarted = other !== null && otherRan === 1;

// 3. settling frees the type, before the timeout has anything to say
releaseA("done");
await first;
out.freedBySettling = runOnce("refresh", () => Promise.resolve(1), {now, timeout: 5000}) !== null;
await new Promise((r) => setTimeout(r, 0));

// 4. a task whose promise NEVER settles wedges its type until the timeout,
//    and not one tick longer
let stuckRelease = null;
runOnce("stuck", () => new Promise((r) => { stuckRelease = r; }), {now, timeout: 5000});
clock += 4999;
out.blockedBeforeTimeout = runOnce("stuck", () => Promise.resolve(1), {now, timeout: 5000}) === null;
clock += 2;
const afterTimeout = runOnce("stuck", () => new Promise(() => {}), {now, timeout: 5000});
out.allowedAfterTimeout = afterTimeout !== null;

// 5. the timed-out straggler settling must NOT free the newer task's slot
stuckRelease("late");
await new Promise((r) => setTimeout(r, 0));
out.stragglerDidNotFreeNewer = runOnce("stuck", () => Promise.resolve(1), {now, timeout: 5000}) === null;

// 6. a task that throws — synchronously or as a rejection — still frees it
try { runOnce("boom", () => { throw new Error("sync"); }, {now, timeout: 5000}); } catch {}
out.freedAfterSyncThrow = runOnce("boom", () => Promise.resolve(1), {now, timeout: 5000}) !== null;
await new Promise((r) => setTimeout(r, 0));

const rejected = runOnce("nope", () => Promise.reject(new Error("async")), {now, timeout: 5000});
try { await rejected; } catch {}
out.freedAfterRejection = runOnce("nope", () => Promise.resolve(1), {now, timeout: 5000}) !== null;

console.log(JSON.stringify(out));
`

func TestRunOnceGateJS(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS gate guard needs it")
	}
	src, err := os.ReadFile(filepath.Join("static", "core.js"))
	if err != nil {
		t.Fatal(err)
	}
	i := strings.Index(string(src), runOnceStart)
	j := strings.Index(string(src), runOnceEnd)
	if i < 0 || j < i {
		t.Fatalf("core.js: the guarded section markers are gone (%q / %q)", runOnceStart, runOnceEnd)
	}
	pure := string(src)[i:j]

	dir := t.TempDir()
	script := filepath.Join(dir, "gate.mjs")
	if err := os.WriteFile(script, []byte(pure+"\n"+runOnceHarness), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, script).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(out))), &got); err != nil {
		t.Fatalf("node output is not the harness JSON: %v\n%s", err, out)
	}

	// ranWhileLive is a count; every other assertion is a plain must-be-true.
	if n, ok := got["ranWhileLive"].(float64); !ok || n != 1 {
		t.Errorf("a dropped start still ran its task: ranWhileLive = %v, want 1", got["ranWhileLive"])
	}
	for _, k := range []string{
		"firstStarted",
		"secondDropped",
		"otherTypeStarted",
		"freedBySettling",
		"blockedBeforeTimeout",
		"allowedAfterTimeout",
		"stragglerDidNotFreeNewer",
		"freedAfterSyncThrow",
		"freedAfterRejection",
	} {
		if got[k] != true {
			t.Errorf("runOnce gate: %s = %v, want true", k, got[k])
		}
	}
}

// The gate is only worth anything if the reload actually goes through it —
// a refactor that calls refreshAfterOp directly from manualRefresh would
// pass the gate test above and still ship the bug.
func TestManualRefreshUsesTheGate(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("static", "ops.js"))
	if err != nil {
		t.Fatal(err)
	}
	i := strings.Index(string(src), "async function manualRefresh()")
	if i < 0 {
		t.Fatal("ops.js: manualRefresh is gone")
	}
	j := strings.Index(string(src)[i:], "\n}\n")
	if j < 0 {
		t.Fatal("ops.js: cannot find the end of manualRefresh")
	}
	body := string(src)[i : i+j]
	if !strings.Contains(body, `runOnce("refresh"`) {
		t.Errorf("manualRefresh no longer runs under the single-flight gate:\n%s", body)
	}
}
