package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const livenessPureStart = "// --- server liveness (pure; guarded against Go) ---"
const livenessPureEnd = "// --- end server liveness ---"

// The page decides "the server is gone" on its own, so the decision is a
// pure state machine node can drive with a fake clock and scripted probes.
// The rules it pins: one failed probe is NOT a verdict (the server ends
// every stream on a re-root, and a probe can lose a race with it), two are;
// a "shutdown" message is a verdict at once; any sign of life clears it; the
// new-port hint appears only once the server has stayed down a while.
func TestServerLivenessMonitorJS(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	src, err := os.ReadFile(filepath.Join("static", "serverdown.js"))
	if err != nil {
		t.Fatal(err)
	}
	i := strings.Index(string(src), livenessPureStart)
	j := strings.Index(string(src), livenessPureEnd)
	if i < 0 || j < i {
		t.Fatalf("serverdown.js: the guarded section markers are gone (%q / %q)", livenessPureStart, livenessPureEnd)
	}
	pure := string(src)[i:j]

	// A step is "suspect", "shutdown", "alive", or "+<ms>" (advance the fake
	// clock, running due timers and letting their probes settle). probes is
	// the scripted answer queue (1 = alive); an exhausted queue answers 0.
	// want is the onChange log: "down", "down+hint", "up".
	type tc struct {
		name   string
		probes []int
		steps  []string
		want   string
		probed int // probes consumed, -1 = don't care
	}
	cases := []tc{
		{name: "a lone stream error with a live server shows nothing", probes: []int{1}, steps: []string{"suspect", "+5000"}, want: "", probed: 1},
		{name: "one failed probe is not a verdict", probes: []int{0, 1}, steps: []string{"suspect", "+5000"}, want: "", probed: 2},
		{name: "two failed probes are", probes: []int{0, 0}, steps: []string{"suspect", "+1500"}, want: "down", probed: 2},
		{name: "errors while probing do not restart the count", probes: []int{0, 0}, steps: []string{"suspect", "suspect", "+700", "suspect", "+800"}, want: "down", probed: 2},
		{name: "errors while down start no second probe loop", probes: []int{0, 0, 0}, steps: []string{"suspect", "+1500", "suspect", "suspect", "+2000"}, want: "down", probed: 3},
		{name: "a probe that succeeds brings the page back", probes: []int{0, 0, 0, 1}, steps: []string{"suspect", "+1500", "+2000", "+2000"}, want: "down up", probed: 4},
		{name: "up again means watching again", probes: []int{0, 0, 1, 0, 0}, steps: []string{"suspect", "+1500", "+2000", "suspect", "+1500"}, want: "down up down", probed: 5},
		{name: "the port hint appears once, after ten seconds down", probes: nil, steps: []string{"suspect", "+1500", "+2000", "+2000", "+2000", "+2000", "+2000", "+2000", "+2000"}, want: "down down+hint", probed: -1},
		{name: "a shutdown message is a verdict at once", probes: nil, steps: []string{"shutdown"}, want: "down", probed: 0},
		{name: "a shutdown is not probed while the server is still exiting", probes: []int{1}, steps: []string{"shutdown", "+1000"}, want: "down", probed: 0},
		{name: "a shutdown then a restart on the same port recovers", probes: []int{0, 1}, steps: []string{"shutdown", "+2000", "+2000"}, want: "down up", probed: 2},
		{name: "a hello on the stream is a sign of life", probes: []int{0, 0}, steps: []string{"suspect", "+1500", "alive", "+10000"}, want: "down up", probed: 2},
		{name: "alive while up says nothing", probes: nil, steps: []string{"alive"}, want: "", probed: 0},
	}

	dir := t.TempDir()
	blob, _ := json.Marshal(func() []map[string]any {
		out := make([]map[string]any, len(cases))
		for n, c := range cases {
			p := c.probes
			if p == nil {
				p = []int{}
			}
			out[n] = map[string]any{"probes": p, "steps": c.steps}
		}
		return out
	}())
	script := `
import { readFileSync } from "node:fs";
const pure = readFileSync(process.argv[2], "utf8");
const cases = JSON.parse(readFileSync(process.argv[3], "utf8"));
const create = new Function(pure + "; return createLivenessMonitor;")();
const settle = async () => { for (let i = 0; i < 20; i++) await Promise.resolve(); };
const out = [];
for (const c of cases) {
  let clock = 0, seq = 0, probed = 0;
  const timers = new Map();
  const log = [];
  const mon = create({
    probe: () => { probed++; return Promise.resolve(c.probes.length ? !!c.probes.shift() : false); },
    setTimeout: (fn, ms) => { timers.set(++seq, { at: clock + ms, fn }); return seq; },
    clearTimeout: (id) => timers.delete(id),
    now: () => clock,
    onChange: (s) => log.push(s.state + (s.hint ? "+hint" : "")),
  });
  for (const step of c.steps) {
    if (step[0] === "+") {
      const end = clock + Number(step.slice(1));
      for (;;) {
        let next = null;
        for (const [id, t] of timers) if (t.at <= end && (!next || t.at < next.t.at)) next = { id, t };
        if (!next) break;
        timers.delete(next.id);
        clock = next.t.at;
        next.t.fn();
        await settle();
      }
      clock = end;
    } else {
      mon[step]();
      await settle();
    }
  }
  out.push({ log: log.join(" "), probed });
}
console.log(JSON.stringify(out));
`
	purePath := filepath.Join(dir, "pure.js")
	casesPath := filepath.Join(dir, "cases.json")
	scriptPath := filepath.Join(dir, "check.mjs")
	for p, b := range map[string][]byte{purePath: []byte(pure), casesPath: blob, scriptPath: []byte(script)} {
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := exec.Command(node, scriptPath, purePath, casesPath).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, raw)
	}
	var got []struct {
		Log    string `json:"log"`
		Probed int    `json:"probed"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("node output %q: %v", raw, err)
	}
	if len(got) != len(cases) {
		t.Fatalf("node returned %d results, want %d", len(got), len(cases))
	}
	for n, c := range cases {
		if got[n].Log != c.want {
			t.Errorf("%s: steps %v\n got %q\nwant %q", c.name, c.steps, got[n].Log, c.want)
		}
		if c.probed >= 0 && got[n].Probed != c.probed {
			t.Errorf("%s: %d probes ran, want %d", c.name, got[n].Probed, c.probed)
		}
	}
}

// What the Go side can pin of the browser half: the bar hides by ID (gg web
// has no global .hidden — a bare class="hidden" is ALWAYS visible), it sits
// above every overlay, and each trigger is wired to the one monitor.
func TestServerDownStaticWiring(t *testing.T) {
	t.Parallel()
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join("static", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	checks := []struct{ file, want, why string }{
		{"index.html", `id="server-down" class="hidden"`, "the bar ships hidden"},
		{"index.html", `id="server-down-hint" class="hidden"`, "the new-port hint ships hidden"},
		{"style.css", `#server-down.hidden { display: none; }`, "per-ID hidden rule — the bar would ALWAYS show"},
		{"style.css", `#server-down-hint.hidden { display: none; }`, "per-ID hidden rule — the hint would always show"},
		{"style.css", `#server-down { position: fixed; inset: 0; z-index: 100;`, "the veil covers the page above every overlay (modal 30, menu 40, toast 50)"},
		{"serverdown.js", `fetch("/api/ping"`, "the probe hits the gate-free endpoint, never a repo read"},
		{"serverdown.js", `window.addEventListener("keydown"`, "keys must not drive a dead page"},
		{"live.js", `es.onerror = () => suspectServerDown();`, "a dropped stream starts the probe"},
		{"live.js", `msg.reason === "shutdown"`, "the server's goodbye paints the bar at once"},
		{"live.js", `serverSeen()`, "a hello is a sign of life — it must clear the bar before the refresh it schedules"},
		{"live.js", `if (isServerDown())`, "no refresh fan against a dead port"},
		{"core.js", `suspectServerDown()`, "a failed API call starts the probe between keepalive pings"},
		{"toast.js", `isServerDown()`, "no wall of 'Failed to fetch' toasts under the bar"},
	}
	for _, c := range checks {
		if !strings.Contains(read(c.file), c.want) {
			t.Errorf("%s: missing %q — %s", c.file, c.want, c.why)
		}
	}
}
