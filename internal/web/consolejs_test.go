package web

import (
	"strings"
	"testing"
)

const consolePureStart = "// --- console model (pure; guarded against Go) ---"
const consolePureEnd = "// --- end console model ---"

func TestConsoleModelJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "console.js", consolePureStart, consolePureEnd, activitySection(t)+`
const ev = (o) => Object.assign({ key: "", code: "", ctrlKey: false, altKey: false, shiftKey: false, metaKey: false }, o);
const r = [];
r.push(JSON.stringify(keyToWire(ev({ key: "a", code: "KeyA" }))));
r.push(JSON.stringify(keyToWire(ev({ key: "Enter", code: "Enter" }))));
r.push(JSON.stringify(keyToWire(ev({ key: "Tab", code: "Tab", shiftKey: true }))));
r.push(JSON.stringify(keyToWire(ev({ key: "c", code: "KeyC", ctrlKey: true }))));
r.push(JSON.stringify(keyToWire(ev({ key: "ArrowUp", code: "ArrowUp", ctrlKey: true, shiftKey: true }))));
r.push(JSON.stringify(keyToWire(ev({ key: "F5", code: "F5" }))));            // browser's: null
r.push(JSON.stringify(keyToWire(ev({ key: "w", code: "KeyW", ctrlKey: true }))));  // browser's: null
r.push(JSON.stringify(keyToWire(ev({ key: "T", code: "KeyT", ctrlKey: true, shiftKey: true }))));  // browser's: null
r.push(JSON.stringify(keyToWire(ev({ key: "Shift", code: "ShiftLeft" }))));  // a bare modifier: null
r.push(JSON.stringify(keyToWire(ev({ key: "Escape", code: "Escape" }))));
r.push(String(isReserved(ev({ key: "]", code: "BracketRight", ctrlKey: true }))), String(isReserved(ev({ key: "\\", code: "Backslash", ctrlKey: true }))), String(isReserved(ev({ key: "]", code: "BracketRight" }))));
r.push(JSON.stringify(gridSize(800, 400, 7.2, 16)), JSON.stringify(gridSize(0, 0, 7.2, 16)));
r.push(runHTML({ t: "a<b", fg: "#ff0000", b: true }), runHTML({ t: "x" }), rowHTML([]));
let rows = applyFrame([], { full: true, rows: 3, lines: [{ y: 0, runs: [{ t: "one" }] }, { y: 1, runs: [] }, { y: 2, runs: [{ t: "three" }] }] });
r.push(rows.length, rows[0], rows[1] === "", rows[2]);
rows = applyFrame(rows, { full: false, rows: 3, lines: [{ y: 1, runs: [{ t: "TWO" }] }] });
r.push(rows[0], rows[1], rows[2]);
r.push(consoleTitle({ label: "claude", worktree: "/a/b/wt", state: "running", started: new Date(Date.now() - 125000).toISOString() }, Date.now(), (p) => p));
r.push(consoleTitle({ label: "codex", worktree: "/a/b/wt", state: "exited", exit_code: 3 }, Date.now(), (p) => p));
r.push(String(exitToast("running", false)), String(exitToast("running", true)), String(exitToast("exited", false)));
r.push(runHTML({ t: "ab\u2590\u259bX\ue0b0" }), runHTML({ t: "\u4f60", w: 2 }), runHTML({ t: "e\u0301x" }));
r.push(killPrompt({ label: "claude", worktree: "/a/b/wt" }, false), killPrompt({ label: "claude", worktree: "C:\\x\\wt2" }, true));
console.log(r.join("|"));
`)
	want := `{"k":"char","mod":0,"text":"a"}|{"k":"enter","mod":0,"text":""}|{"k":"tab","mod":1,"text":""}|{"k":"char","mod":2,"text":"c"}|` +
		`{"k":"up","mod":3,"text":""}|null|null|null|null|{"k":"esc","mod":0,"text":""}|true|true|false|` +
		`{"cols":111,"rows":25}|{"cols":1,"rows":1}|` +
		`<span style="color:#ff0000" class="b">a&lt;b</span>|<span>x</span>||` +
		`3|<span>one</span>|true|<span>three</span>|<span>one</span>|<span>TWO</span>|<span>three</span>|` +
		`claude · /a/b/wt · running 2m|codex · /a/b/wt · exited (3)|true|false|false|<span>ab<span class="cg" style="width:calc(var(--cw) * 2)">▐▛</span>X<span class="cg" style="width:calc(var(--cw) * 1)"></span></span>|` +
		`<span><span class="cg" style="width:calc(var(--cw) * 2)">你</span></span>|<span>éx</span>` +
		`|Kill claude in wt?|Kill claude in wt2 and remove it from the list?`
	if out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

var consoleWiring = []struct{ file, want, why string }{
	{"app.js", "./console.js", "the module must be imported at boot"},
	{"console.js", `pushLayer("console"`, "the console rides the layer stack"},
	{"console.js", "/api/session-screen?id=", "frames come from the per-console stream"},
	{"console.js", "/api/session-input", "typing goes to the input endpoint"},
	{"console.js", "/api/session-size", "a focused console pushes its size"},
	{"console.js", `addEventListener("paste"`, "paste rides the paste event, not keys"},
	{"console.js", "stay with the browser", "the focused foot says which keys the browser keeps"},
	{"console.js", "else closeConsole();", "ctrl+] on an unfocused console closes it"},
	{"console.js", "ResizeObserver", "a focused console re-measures on resize"},
	{"console.js", "/api/session-kill", "kill goes to its endpoint"},
	{"console.js", "/api/session-remove", "remove goes to its endpoint"},
	{"console.js", `k kill`, "the unfocused foot offers kill"},
	{"console.js", `X kill + remove`, "…and kill and remove"},
	{"console.js", `x remove`, "the exited foot offers remove"},
	{"console.js", `if (o !== yes) return;`, "only the explicit kill option kills: esc and cancel never do"},
	{"core.js", `"kill", "kill and remove"`, "the kill options render as danger"},
	{"style.css", "#console.hidden", "hidden by id, never a global .hidden"},
	{"style.css", "#console-grid", "the grid has its own rules (monospace, pre)"},
	{"console.js", `addEventListener("gg:panes"`, "the console closes when the page shows something under it"},
	{"files.js", `new CustomEvent("gg:panes")`, "a pane navigation (setLayout) says so"},
	{"layers.js", `new CustomEvent("gg:panes", { detail: { force: true } })`, "…and so does a layer that opens UNDER the console"},
	{"live.js", `new CustomEvent("gg:panes", { detail: { force: true } })`, "an agent's navigate is shown, not hidden under its own console"},
	{"console.js", "Date.now() - outsideClick < GIVE_WAY_MS", "a plain pane change closes the console only after the user's own click outside it — never a background refresh"},
	{"console.js", `li && !li.classList.contains("wsess")`, "a click on a sidebar row navigates the panes: the console gives way (a session sub-row opens one instead)"},
	{"console.js", `class="conrow"`, "a console row has its OWN class: .crow is the commit row (flex, nowrap, padded) and deformed the screen"},
	{"style.css", "#console-grid .conrow { height: 16px; line-height: 16px; white-space: pre; }", "the row keeps its spaces and its height"},
	{"style.css", "#console-grid .cg { display: inline-block;", "a non-ASCII stretch sits in a box of whole cells"},
	{"console.js", `probe.className = "conrow"`, "the cell height is a ROW's height, not the font's content box (the grid overflowed the console)"},
	{"console.js", `setProperty("--cw"`, "the cell width reaches the glyph boxes"},
	{"style.css", `"Symbols Nerd Font Mono"`, "prompt icons (Nerd Font private-use glyphs) find a font"},
	{"style.css", "#sessstart-body button { background: var(--bg);", "the dialog's run button looks like the page's buttons"},
	{"sidebar.js", "sessionSubRows(path", "a branch row checked out in a worktree lists that worktree's sessions"},
	{"sidebar.js", "sessionSubRows(w.path", "…with the worktree row's own markup"},
	{"style.css", "#branches-list li.wsess", "the branch sub-rows are styled"},
}

func TestConsoleJSIsWired(t *testing.T) {
	t.Parallel()
	for _, c := range consoleWiring {
		if !strings.Contains(readStatic(t, c.file), c.want) {
			t.Errorf("%s lacks %q: %s", c.file, c.want, c.why)
		}
	}
}

func TestConsoleRowClassIsNotTheCommitRows(t *testing.T) {
	t.Parallel()
	if strings.Contains(readStatic(t, "console.js"), `class="crow"`) {
		t.Fatal(`console.js paints rows as .crow — the commit list's class`)
	}
}
