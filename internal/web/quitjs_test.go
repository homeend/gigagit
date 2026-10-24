package web

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The switcher's quit mode is the TUI sessions popup's: its question, the
// Agents and AI tasks tabs only, landing on AI tasks when no session runs.
func TestQuitModeJS(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	mod := "const TABS = [\"agents\", \"tasks\", \"files\"];\n"
	for _, fn := range []string{"quitHeader", "switcherTabs", "quitLanding"} {
		mod += jsFunc(t, "openfiles.js", fn) + "\n"
	}
	mod += "export { quitHeader, switcherTabs, quitLanding };\n"
	harness := `
import { quitHeader, switcherTabs, quitLanding } from "./h.mjs";
console.log(JSON.stringify([
  quitHeader(3),
  switcherTabs(null), switcherTabs({ live: 1 }),
  quitLanding([{ state: "running" }]), quitLanding([{ state: "exited" }]), quitLanding([]),
]));
`
	dir := t.TempDir()
	for name, body := range map[string]string{"h.mjs": mod, "run.mjs": harness} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out, err := exec.Command(node, filepath.Join(dir, "run.mjs")).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	want := `["agents and AI tasks still running: 3 — quit gg web?",["agents","tasks","files"],["agents","tasks"],"agents","tasks","tasks"]`
	if got := strings.TrimSpace(string(out)); got != want {
		t.Fatalf("quit mode =\n%s\nwant\n%s", got, want)
	}
}

// Wiring: the live "quit" reason re-reads the ask; Q confirms before any tab
// handler; esc and closing cancel; the tasks tab's row keys are off; the
// foot carries the TUI's [Q] kill all and quit  [esc] cancel.
func TestQuitModeWired(t *testing.T) {
	t.Parallel()
	live := readStaticSrc(t, "live.js")
	if !strings.Contains(live, `if (msg.reason === "quit") {`) || !strings.Contains(live, `new CustomEvent("gg:quit")`) {
		t.Error(`live.js: the "quit" reason does not reach the switcher`)
	}
	src := readStaticSrc(t, "openfiles.js")
	for _, want := range []string{
		`document.addEventListener("gg:quit", refreshQuit);`,
		`getJSON("/api/quit")`,
		`postJSON("/api/quit", { confirm: true })`,
		`postJSON("/api/quit", { confirm: false })`,
		`if (sw.quit && e.key === "Q") {`,
		`if (sw.tab === "tasks" && !sw.quit && (e.key === "k" || e.key === "x" || e.key === "s")) {`,
		`<button data-oact="quitQ">Q kill all and quit</button><button data-oact="quitesc">esc cancel</button>`,
		`opLine("ending agent sessions…")`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("openfiles.js lacks %q", want)
		}
	}
	if i, j := strings.Index(src, `if (sw.quit && e.key === "Q") {`), strings.Index(src, `if (sw.tab === "tasks" && !sw.quit`); i < 0 || j < 0 || i > j {
		t.Error("openfiles.js: Q must be caught before the tab handlers")
	}
	css := readStaticSrc(t, "style.css")
	if !strings.Contains(css, "#openfiles-quit.hidden { display: none; }") {
		t.Error("style.css: #openfiles-quit needs its own hidden rule")
	}
}

// Review fixes: a tab whose ask came down because another tab (or the
// terminal) confirmed says the work is ending; quit mode's AI tasks tab has
// no row actions at all — no keys, no foot buttons, no enter.
func TestQuitModeReviewFixes(t *testing.T) {
	t.Parallel()
	src := readStaticSrc(t, "openfiles.js")
	for _, want := range []string{
		`if (q && q.ending) opLine("ending agent sessions…");`,
		`(r && !sw.quit ? taskHint(r)`,
		`if (sw.quit && sw.tab === "tasks" && b.dataset.oact !== "quitQ" && b.dataset.oact !== "quitesc" && b.dataset.oact !== "tab" && b.dataset.oact !== "esc") return;`,
		`if (sw.quit) return; // quit mode: the AI tasks tab's row actions are off`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("openfiles.js lacks %q", want)
		}
	}
}
