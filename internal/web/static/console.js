// console.js — an agent session's console in the page (web attach, plan 1):
// the server's screen painted as styled runs over a per-console SSE stream,
// keys and paste posted back, the focused viewer owning the session's size.
import { $, activityAttn, activityLabel, elidePath, esc, getJSON, postJSON, state } from "./core.js";
import { closeLayer, copyText, mountOverlay, popFoot, pushFoot, pushLayer, topLayer } from "./layers.js";
import { cellAt, enqueueMouse, linesText, scrollAction, scrollMove, scrollTop, wheelPlan } from "./conscroll.js";
import { registerHelp } from "./menus.js";
import { toast } from "./toast.js";
import { doReroot, opLine, showLocalConfirm, switchingWorktree } from "./ops.js";
import { landingAfterOpen, pathKey, returnPlan, screenStops, sessionStops, switchTarget, walkAllScreens, walkLine, walkNext, walkRing, walkScreens, worktreeBranch, wtCycle } from "./walk.js";
import { land, landingGen } from "./sidebar.js";
import { closeViewer } from "./viewer.js";
import { closeHistory } from "./filehist.js";
import { openPalette } from "./palette.js";
import { textTemplatesKey } from "./texttemplates.js";

// --- console model (pure; guarded against Go) ---
const KEY_NAMES = {
  Enter: "enter", Tab: "tab", Escape: "esc", Backspace: "backspace", Delete: "delete", Insert: "insert",
  ArrowUp: "up", ArrowDown: "down", ArrowLeft: "left", ArrowRight: "right",
  Home: "home", End: "end", PageUp: "pgup", PageDown: "pgdn",
  F1: "f1", F2: "f2", F3: "f3", F4: "f4", F6: "f6", F7: "f7", F8: "f8", F9: "f9", F10: "f10",
};
// BROWSER_CTRL: ctrl combinations Chrome keeps for itself — never captured,
// never faked. ctrl+shift+<letter> and F5/F11/F12 are the browser's too.
const BROWSER_CTRL = new Set(["w", "t", "n", "Tab"]);

function modOf(e) {
  return (e.shiftKey ? 1 : 0) | (e.ctrlKey ? 2 : 0) | (e.altKey ? 4 : 0);
}

// keyToWire maps a keydown to the wire key, or null for a key the page must
// let through (the browser's own, a bare modifier, an unknown special key).
function keyToWire(e) {
  if (e.metaKey) return null;
  if (e.key === "F5" || e.key === "F11" || e.key === "F12") return null;
  if (e.ctrlKey && (BROWSER_CTRL.has(e.key) || (e.shiftKey && e.key.length === 1))) return null;
  const name = KEY_NAMES[e.key];
  if (name) return { k: name, mod: modOf(e), text: "" };
  if ([...e.key].length === 1) return { k: "char", mod: modOf(e), text: e.key };
  return null; // Shift, Control, CapsLock, Dead, Unidentified, …
}

function isReserved(e) {
  return e.ctrlKey && !e.altKey && !e.metaKey && (e.code === "BracketRight" || e.key === "]" || e.code === "Backslash" || e.key === "\\");
}

// unboundPaletteKey: ctrl+p / ctrl+k on an UNBOUND console. Docked, the
// palette opens over it (the TUI's consolePassthrough); maximised it covers
// the page, so the TUI keeps ctrl+p out — "block" only keeps the key from
// the browser's print dialog.
function unboundPaletteKey(e, max) {
  if (!(e.ctrlKey || e.metaKey) || e.altKey || (e.key !== "p" && e.key !== "k")) return "";
  return max ? "block" : "palette";
}

// altBKey is the TUI's alt+b / alt+B on a console: unbound, both bind
// ("focus"); bound, alt+b steps out to the session's Branches row ("row")
// and alt+B steps out in place ("out"). "" when e is neither.
function altBKey(e, focused) {
  if (!e.altKey || e.ctrlKey || e.metaKey || e.code !== "KeyB") return "";
  if (!focused) return "focus";
  return e.shiftKey ? "out" : "row";
}

function gridSize(w, h, cw, ch) {
  return { cols: Math.max(1, Math.floor(w / cw)), rows: Math.max(1, Math.floor(h / ch)) };
}

// escRun is core.js's esc, repeated so the guarded section stands alone.
function escRun(s) {
  return String(s).replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));
}

// BOXED: a stretch of glyphs the monospace font may not have — blocks, box
// drawing, powerline and Nerd Font icons, symbols. A browser takes them from
// a fallback font at THAT font's width, and every later column drifts; so
// the stretch sits in a box of whole cells (one per code point). Latin
// (through U+024F) and zero-width marks stay plain text.
const BOXED = /[^\u0000-\u024f\u0300-\u036f\u200b-\u200d\ufe00-\ufe0f]+/gu;

function cellBox(text, cells) {
  return `<span class="cg" style="width:calc(var(--cw) * ${cells})">${escRun(text)}</span>`;
}

// textHTML: a run's text with its boxed stretches; run.w marks a run that is
// one wide glyph (the server's cell width for it).
function textHTML(run) {
  if (run.w) return cellBox(run.t, run.w);
  let out = "", at = 0;
  for (const m of run.t.matchAll(BOXED)) {
    out += escRun(run.t.slice(at, m.index)) + cellBox(m[0], [...m[0]].length);
    at = m.index + m[0].length;
  }
  return out + escRun(run.t.slice(at));
}

function runHTML(run) {
  const st = [];
  if (run.fg) st.push("color:" + run.fg);
  if (run.bg) st.push("background:" + run.bg);
  const cls = ["b", "i", "u", "r", "d", "s"].filter((f) => run[f]).join(" ");
  return `<span${st.length ? ` style="${st.join(";")}"` : ""}${cls ? ` class="${cls}"` : ""}>${textHTML(run)}</span>`;
}

function rowHTML(runs) {
  return (runs || []).map(runHTML).join("");
}

// applyFrame folds a frame into the row array: a full frame replaces it,
// a partial one patches the rows it names.
function applyFrame(rows, frame) {
  const out = frame.full ? new Array(frame.rows).fill("") : rows.slice();
  for (const l of frame.lines || []) out[l.y] = rowHTML(l.runs);
  return out;
}

function ageText(iso, now) {
  const s = Math.max(0, Math.floor((now - Date.parse(iso)) / 1000));
  if (s < 60) return s + "s";
  if (s < 3600) return Math.floor(s / 60) + "m";
  return Math.floor(s / 3600) + "h";
}

// exitToast: an exit is announced only when it was not on screen already —
// an unfocused console (the user looked away) that was running until now.
// A console opened on an exited session, or a focused one, says nothing.
function exitToast(prevState, wasFocused) {
  return prevState !== "exited" && !wasFocused;
}

// consoleTitleParts: the title's base ("claude · wt · running 2m") and the
// activity it appends when the server knows one ("needs input").
function consoleTitleParts(s, now, elide) {
  const st = s.state === "exited" ? "exited (" + s.exit_code + ")" : "running " + ageText(s.started, now);
  return { base: s.label + " · " + elide(s.worktree) + " · " + st, act: activityLabel(s, now) };
}
function consoleTitle(s, now, elide) {
  const p = consoleTitleParts(s, now, elide);
  return p.act ? p.base + " · " + p.act : p.base;
}

function wtName(path) {
  return String(path).split(/[\\/]/).filter(Boolean).pop() || String(path);
}

function killPrompt(s, remove) {
  return "Kill " + s.label + " in " + wtName(s.worktree) + (remove ? " and remove it from the list?" : "?");
}
// --- end console model ---

const con = { id: "", info: null, rows: [], frame: null, focused: false, max: false, es: null, queue: [], inflight: false, cell: null, size: null, tick: 0, ret: "" };
const root = mountOverlay("console");
root.innerHTML =
  `<div id="console-title"><span id="console-label"></span><span id="console-size"></span></div>` +
  `<div id="console-body"><div id="console-grid"></div><div id="console-cursor" class="hidden"></div></div>`;
const grid = $("console-grid");
const cursor = $("console-cursor");

const FOOT_FOCUSED = `<button data-cact="out">alt+B / ctrl+] step out</button><button data-cact="row">alt+b Branches row</button><button data-cact="altf">alt+f maximize</button><button data-cact="sessions">ctrl+\\ sessions</button><span class="cwarn">ctrl+w · ctrl+t · ctrl+n stay with the browser</span>`;
const FOOT_UNFOCUSED = `<button data-cact="focus">enter / alt+b focus</button><button data-cact="max">m maximize</button><button data-cact="kill">k kill</button><button data-cact="killrm">X kill + remove</button><button data-cact="sessions">ctrl+\\ sessions</button><button data-cact="close">esc / ctrl+] close</button>`;
const FOOT_EXITED = `<button data-cact="remove">x remove</button><button data-cact="sessions">ctrl+\\ sessions</button><button data-cact="close">esc / ctrl+] close</button>`;

// openSwitcher is the ctrl+\ popup (openfiles.js); a document event keeps
// the two modules from importing each other.
function askSwitcher() {
  document.dispatchEvent(new CustomEvent("gg:switcher"));
}

$("foot").addEventListener("click", (e) => {
  const b = e.target.closest("button[data-cact]");
  if (!b || !con.id) return;
  ({
    out: stepOut, row: stepOutToRow, focus: focusConsole, max: maximize, altf: toggleMax, close: leaveConsole, sessions: askSwitcher,
    kill: () => killSession(con.info, false), killrm: () => killSession(con.info, true), remove: () => removeSession(con.info),
  })[b.dataset.cact]();
});

// measureCell: the cell is a ROW's height (not the font's content box, which
// is shorter — the grid then asked for more rows than fit and scrolled) and
// a twentieth of twenty M's. The width also sizes the glyph boxes (--cw).
function measureCell() {
  const probe = document.createElement("div");
  probe.className = "conrow";
  probe.style.visibility = "hidden";
  const m = document.createElement("span");
  m.textContent = "M".repeat(20);
  probe.append(m);
  grid.append(probe);
  con.cell = { w: m.getBoundingClientRect().width / 20, h: probe.getBoundingClientRect().height };
  probe.remove();
  grid.style.setProperty("--cw", con.cell.w + "px");
}

// layout puts the layer over the panes right of the sidebar (over the whole
// pane area when the sidebar is hidden or the console is maximized), above
// the foot.
function layout() {
  const foot = $("foot").getBoundingClientRect();
  const panes = $("panes").getBoundingClientRect();
  const side = con.max ? null : $("branches-pane");
  const left = side && side.offsetWidth ? side.getBoundingClientRect().right + 5 : panes.left;
  Object.assign(root.style, { top: panes.top + "px", left: left + "px", right: "0", bottom: window.innerHeight - foot.top + "px" });
}

function paint() {
  if (con.scroll) return; // the frozen view changes only on scroll keys and the wheel
  grid.innerHTML = con.rows.map((r) => `<div class="conrow">${r || " "}</div>`).join("");
  paintCursor();
}

function paintCursor() {
  const f = con.frame;
  if (con.scroll || !f || !f.cursor || !con.cell || con.info.state === "exited") return cursor.classList.add("hidden");
  cursor.classList.remove("hidden");
  cursor.classList.toggle("outline", !con.focused);
  cursor.style.left = f.cx * con.cell.w + "px";
  cursor.style.top = f.cy * con.cell.h + "px";
  cursor.style.width = con.cell.w + "px";
  cursor.style.height = con.cell.h + "px";
}

function retitle() {
  if (!con.info) return;
  const budget = Math.max(12, Math.floor(($("console-title").clientWidth - 160) / (con.cell ? con.cell.w : 7)));
  const p = consoleTitleParts(con.info, Date.now(), (q) => elidePath(q, budget));
  $("console-label").innerHTML = esc(p.base) + (p.act ? ` · <span class="act${activityAttn(con.info) ? " attn" : ""}">${esc(p.act)}</span>` : "");
  $("console-size").textContent = con.frame ? con.frame.cols + "×" + con.frame.rows : "";
  $("console-label").classList.toggle("exited", con.info.state === "exited");
}

function foot() {
  pushFoot("console", con.scroll ? FOOT_SCROLL : con.info.state === "exited" ? FOOT_EXITED : con.focused ? FOOT_FOCUSED : FOOT_UNFOCUSED);
}

// --- scroll mode (the TUI's console_scroll.go): a frozen history ---
const FOOT_SCROLL = `<span class="cwarn">scrollback: ↑/↓ pgup/pgdn g/G move · space marks a line range · enter copies · a drag copies text · esc / q leave · any other key leaves (to the agent)</span>`;

async function enterScroll() {
  if (!con.id || con.scroll) return;
  let h;
  try {
    h = await getJSON("/api/session-history?id=" + encodeURIComponent(con.id));
  } catch (e) {
    return toast("scrollback: " + (e.message || e), { err: true });
  }
  const lines = h.lines || [];
  if (!lines.length || !con.id || con.scroll) return;
  con.scroll = { lines, cur: lines.length - 1, mark: -1, top: lines.length };
  paintScroll();
  foot();
}

function scrollRows() {
  return (con.frame && con.frame.rows) || 24;
}

function paintScroll() {
  const s = con.scroll;
  const rows = scrollRows();
  s.top = scrollTop(s.top, s.cur, rows, s.lines.length);
  const lo = s.mark >= 0 ? Math.min(s.mark, s.cur) : -1;
  const hi = s.mark >= 0 ? Math.max(s.mark, s.cur) : -1;
  let html = "";
  for (let i = s.top; i < Math.min(s.lines.length, s.top + rows); i++) {
    html += `<div class="conrow${i === s.cur ? " scur" : ""}${i >= lo && i <= hi ? " ssel" : ""}">${rowHTML(s.lines[i]) || " "}</div>`;
  }
  grid.innerHTML = html;
  cursor.classList.add("hidden");
  $("console-size").textContent = "scrollback " + (s.cur + 1) + "/" + s.lines.length;
}

function leaveScroll() {
  con.scroll = null;
  paint();
  retitle();
  foot();
}

// scrollKey runs one key in scroll mode; false hands it on (the agent's).
function scrollKey(e) {
  const a = scrollAction(e.key);
  if (!a) return true;
  const s = con.scroll;
  if (a === "send") {
    leaveScroll();
    return !con.focused; // focused: the key goes on to the agent
  }
  e.preventDefault();
  if (a === "leave") return leaveScroll(), true;
  if (a === "mark") s.mark = s.mark === s.cur ? -1 : s.cur;
  else if (a === "copy") {
    const text = linesText(s.lines, s.mark >= 0 ? s.mark : s.cur, s.cur);
    const n = Math.abs((s.mark >= 0 ? s.mark : s.cur) - s.cur) + 1;
    copyText(text, n === 1 ? "line" : n + " lines");
    s.mark = -1;
  } else s.cur = scrollMove(s.cur, s.lines.length, a, scrollRows() - 1);
  paintScroll();
  return true;
}

function connect() {
  if (con.es) con.es.close();
  const es = new EventSource("/api/session-screen?id=" + encodeURIComponent(con.id));
  con.es = es;
  es.addEventListener("hello", (ev) => {
    const h = JSON.parse(ev.data);
    con.frame = h.frame;
    con.rows = applyFrame([], h.frame);
    paint();
    retitle();
  });
  es.addEventListener("frame", (ev) => {
    const f = JSON.parse(ev.data);
    con.frame = Object.assign({}, con.frame, f);
    con.rows = applyFrame(con.rows, f);
    paint();
    if (f.full) retitle();
  });
  es.addEventListener("exited", (ev) => {
    const wasFocused = con.focused;
    const prev = con.info.state;
    con.info = Object.assign({}, con.info, { state: "exited", exit_code: JSON.parse(ev.data).code });
    con.focused = false;
    retitle();
    foot();
    paintCursor();
    if (exitToast(prev, wasFocused)) toast(con.info.label + " in " + con.info.worktree.split("/").pop() + " exited (" + con.info.exit_code + ")");
  });
  es.addEventListener("gone", () => {
    toast(con.info.label + " was removed");
    leaveConsole();
  });
}

// pushSize: the focused viewer owns the session's size (ruling 4). Never
// while unfocused; a no-op when the box still fits the same grid.
async function pushSize() {
  if (!con.focused || !con.cell) return;
  const body = $("console-body").getBoundingClientRect();
  const sz = gridSize(body.width - 16, body.height - 12, con.cell.w, con.cell.h);
  if (con.size && con.size.cols === sz.cols && con.size.rows === sz.rows) return;
  con.size = sz;
  try {
    await postJSON("/api/session-size", { id: con.id, cols: sz.cols, rows: sz.rows });
  } catch (e) {
    toast("resize failed: " + (e.message || e), { err: true });
  }
}

const ro = new ResizeObserver(() => {
  layout();
  if (con.focused) pushSize();
});

// flush sends the queued keys as one batch (and any paste after them), one
// request in flight at a time; keys typed meanwhile ride the next batch.
async function flush() {
  if (con.inflight || !con.queue.length) return;
  con.inflight = true;
  const batch = con.queue.splice(0);
  const keys = batch.filter((x) => x.k);
  const paste = batch.filter((x) => x.paste).map((x) => x.paste).join("");
  try {
    if (keys.length) await postJSON("/api/session-input", { id: con.id, keys });
    if (paste) await postJSON("/api/session-input", { id: con.id, paste });
  } catch (e) {
    if (e.status !== 409) toast("input failed: " + (e.message || e), { err: true });
  } finally {
    con.inflight = false;
    if (con.queue.length) flush();
  }
}

function send(item) {
  if (!con.info || con.info.state === "exited") return;
  con.queue.push(item);
  flush();
}

document.addEventListener("paste", (e) => {
  const top = topLayer();
  if (!con.id || !con.focused || !top || top.id !== "console") return;
  const text = (e.clipboardData || window.clipboardData).getData("text");
  if (!text) return;
  e.preventDefault();
  send({ paste: text });
});

function consoleKey(e) {
  outsideClick = 0; // typing here: a later background refresh is not the user's navigation
  if (isReserved(e)) {
    e.preventDefault();
    if (e.code === "Backslash" || e.key === "\\") askSwitcher();
    else if (con.focused) stepOut();
    else leaveConsole(); // ctrl+] twice = out, then away
    return true;
  }
  if (walkKey(e)) return true; // gg's alt keys, even inside a bound console
  if (e.altKey && e.code === "KeyF" && !e.ctrlKey && !e.metaKey) {
    // the TUI's alt+f: docked ↔ maximised, bound after either way
    e.preventDefault();
    toggleMax();
    return true;
  }
  const ab = altBKey(e, con.focused);
  if (ab) {
    e.preventDefault();
    ({ row: stepOutToRow, out: stepOut, focus: focusConsole })[ab]();
    return true;
  }
  if (textTemplatesKey(e)) return true; // alt+x reaches gg past a console, bound or not (the TUI's)
  if (con.scroll && scrollKey(e)) return true;
  if (con.focused && e.key === "PageUp" && e.altKey) return enterScroll(), true;
  if (!con.focused && e.key === "PageUp") return enterScroll(), true;
  if (con.focused) {
    if (e.isComposing || e.key === "Process") return true; // an IME commit arrives as a char later
    const k = keyToWire(e);
    if (!k) return true; // the browser's own keys keep their default; nothing reaches the page
    e.preventDefault();
    send(k);
    return true;
  }
  const pk = unboundPaletteKey(e, con.max);
  if (pk) {
    e.preventDefault();
    if (pk === "palette") openPalette("cmd");
    return true;
  }
  switch (e.key) {
    case "Enter": focusConsole(); break;
    case "m": maximize(); break;
    case "k": if (con.info.state !== "exited") killSession(con.info, false); break;
    case "X": if (con.info.state !== "exited") killSession(con.info, true); break;
    case "x": if (con.info.state === "exited") removeSession(con.info); break;
    case "Escape": leaveConsole(); break;
    default: return true; // an unfocused console still owns the keyboard: nothing leaks to the page
  }
  e.preventDefault();
  return true;
}

function focusConsole() {
  if (!con.info || con.info.state === "exited") return;
  con.focused = true;
  con.size = null;
  foot();
  paintCursor();
  pushSize();
}

function stepOut() {
  con.focused = false;
  // m's maximise shrinks back; alt+f's stays full-screen (the TUI's ctrl+]).
  if (con.max && con.maxBy !== "alt") {
    con.max = false;
    layout();
  }
  foot();
  paintCursor();
}

// stepOutToRow is alt+b on a bound console (the TUI's unbindToSessionRow):
// unbound, and the Branches mark lands on the session's sub-row. A
// maximised console docks first, or it would cover the sidebar the mark
// is in. The console stays shown and keeps the keyboard — the page has no
// sidebar cursor to hand it to.
function stepOutToRow() {
  con.focused = false;
  if (con.max) {
    con.max = false;
    con.maxBy = "";
    layout();
  }
  land({ sid: con.id });
  foot();
  paintCursor();
}

function maximize() {
  if (!con.info || con.info.state === "exited") return;
  con.max = true;
  con.maxBy = "m";
  layout();
  focusConsole();
}

// toggleMax is alt+f: docked ↔ maximised, the console bound after.
function toggleMax() {
  if (!con.info || con.info.state === "exited") return;
  con.max = !con.max;
  con.maxBy = con.max ? "alt" : "";
  layout();
  focusConsole();
}

// --- the walks (walk.js): alt+a / alt+t / alt+A / alt+T, alt+q / alt+Q, alt+s, alt+w ---
// The viewed worktree is the shown console's — the page switches to it
// (openConsole), unless an operation held the page where it was — else the
// page's.
// walked is the worktree alt+q's page stop left viewed while the page stayed
// where it was (an operation held it), with the page it was left on: once the
// page switches anywhere else (alt+w, a sidebar click, alt+s) it is stale.
let walked = null; // {dir, page}
function walkedDir() {
  return walked && pathKey(walked.page) === pathKey(switchingWorktree()) ? walked.dir : "";
}

function viewedWorktree() {
  return con.id && con.info ? con.info.worktree : walkedDir() || state.worktree;
}

function wtOrder() {
  return [...document.querySelectorAll("#worktrees-list li[data-p]")].map((li) => li.dataset.p);
}

// showStop shows a walk's session — the Branches mark lands on its sub-row
// first (the TUI's selectSessionRow), so it is there however the page
// switch goes.
function showStop(id, line) {
  land({ sid: id });
  if (con.id !== id) return openConsole(id, { line });
  focusConsole();
  if (line) opLine(line);
}

function walkSessions(terminal, scoped) {
  const order = wtOrder();
  const viewed = viewedWorktree();
  const list = walkRing(state.sessions, order, terminal, scoped ? viewed : "");
  const r = walkNext(list, con.id, con.focused, order.indexOf(viewed), (p) => order.indexOf(p), terminal, scoped);
  if (r.msg) opLine(r.msg);
  if (r.i == null) return;
  showStop(list[r.i].id, walkLine(list[r.i], r.i, list.length, terminal));
}

function walkWorktreeScreens(back) {
  const dir = viewedWorktree();
  const stops = screenStops(state.sessions, wtOrder(), dir);
  const r = walkScreens(stops, con.id, back, dir);
  if (r.i != null && stops[r.i]) return showStop(stops[r.i].id, r.msg);
  opLine(r.msg);
  if (r.i == null) return;
  closeConsole();
  walked = { dir, page: switchingWorktree() }; // the TUI's plain stop keeps the walked worktree viewed
  landWorktree(dir);
}

// walkAllSessions is alt+s / alt+S (the TUI's walkSessions; the page has no
// workspace, so alt+S is the same walk): alt+q's stops, continued from
// worktree to worktree. A session stop shows as alt+q's do (openConsole
// switches the page to its worktree); another worktree's page is an
// in-place switch there, its branch marked as alt+w marks it.
function walkAllSessions() {
  if (state.op) return opLine("an operation is running — switch once it has finished");
  const order = wtOrder();
  // A console of a worktree the list does not name (another repository's)
  // is no stop: the walk steps from the page's worktree, as the TUI hides it.
  const listed = con.id && con.info && order.some((p) => pathKey(p) === pathKey(con.info.worktree));
  const viewed = listed ? con.info.worktree : walkedDir() || switchingWorktree();
  const skip = (state.worktrees || []).filter((w) => w.unreachable).map((w) => w.path);
  const stops = sessionStops(state.sessions, order);
  const r = walkAllScreens(stops, con.id, viewed, order, skip);
  if (r.i == null) return opLine(r.msg);
  const st = stops[r.i];
  if (!st.page) return showStop(st.id, r.msg);
  closeConsole(); // hidden: hiding is not leaving, no way back
  if (pathKey(st.page) === pathKey(switchingWorktree())) {
    opLine(r.msg);
    return landWorktree(st.page);
  }
  const gen = landingGen();
  doReroot(st.page, null, r.msg).then((ok) => ok && gen === landingGen() && landWorktree(st.page)); // a later walk or click wins
}

// landWorktree is selectWorktreeBranch: the mark lands on the branch
// checked out at path — none when detached or filtered out.
function landWorktree(path) {
  const b = worktreeBranch(state.worktrees, path);
  land(b ? { branch: b } : null);
}

// WINDOWS are what alt+w closes (the TUI's closeWindows): the console, the
// file viewer (to the background), file history, blame. Any other layer
// on top is a surface being filled — alt+w is ignored there.
const WINDOWS = ["console", "viewer", "history", "blame"];
function closeWindows() {
  for (let top = topLayer(); top && WINDOWS.includes(top.id); top = topLayer()) {
    if (top.id === "console") closeConsole(); // hidden: hiding is not leaving, no way back
    else if (top.id === "viewer") closeViewer("background");
    else if (top.id === "history") closeHistory();
    else closeLayer(top.id);
  }
}

// cycleWorktrees is alt+w (the TUI's cycleWorktrees): the first press
// over windows closes them, the next steps the Worktrees list's ring — an
// in-place switch, its line said when it lands; a press while one is in
// flight steps on from where it is going.
function cycleWorktrees() {
  const top = topLayer();
  if (top && !WINDOWS.includes(top.id)) return;
  if (state.op) return opLine("an operation is running — switch once it has finished");
  const skip = (state.worktrees || []).filter((w) => w.unreachable).map((w) => w.path);
  const r = wtCycle(wtOrder(), switchingWorktree(), !!top, !!con.id, skip);
  if (r.close) closeWindows();
  if (!r.path) {
    opLine(r.msg);
    return landWorktree(switchingWorktree()); // the TUI's cursor is on the viewed worktree's branch
  }
  const gen = landingGen();
  doReroot(r.path, null, r.msg).then((ok) => ok && gen === landingGen() && landWorktree(r.path)); // a later walk or click wins
}

// walkKey runs a walk key and reports whether it took it.
function walkKey(e) {
  if (!e.altKey || e.ctrlKey || e.metaKey) return false;
  if (e.code !== "KeyA" && e.code !== "KeyT" && e.code !== "KeyQ" && e.code !== "KeyS" && e.code !== "KeyW") return false;
  e.preventDefault();
  if (e.code === "KeyW") cycleWorktrees();
  else if (e.code === "KeyS") walkAllSessions();
  else if (e.code === "KeyQ") walkWorktreeScreens(e.shiftKey);
  else walkSessions(e.code === "KeyT", e.shiftKey);
  return true;
}

// openConsole shows a session's console. A session of another listed
// worktree shows IN it (the TUI's showConsoleBy): the console opens at once
// and the page switches there in place (ops.js doReroot → softSwitch, no
// reload), the walk's line said when it lands. Its way back is the
// worktree the first console of a walk was shown over (con.ret), which its
// own close brings back (leaveConsole). An operation running holds the
// page: the console shows here and says so; a refused switch leaves its
// error line up. opts: line (the walk's status line), fromBranches (opened
// from a Branches session sub-row: the landing mark goes there), editor (an
// editor session: called instead of the "gone" toast when it already exited).
async function openConsole(id, opts = {}) {
  outsideClick = 0;
  const gen = landingGen(); // a walk landing while the session list is read wins over this open's
  let body;
  try {
    body = await getJSON("/api/sessions");
  } catch (e) {
    return toast("sessions: " + (e.message || e), { err: true });
  }
  const info = (body.sessions || []).find((s) => s.id === id);
  if (!info) {
    if (opts.editor) return opts.editor(); // an editor that exited before its console opened (a GUI editor returns at once)
    return toast("that agent session is gone", { err: true });
  }
  const mark = landingAfterOpen(state.landing, id, !!opts.fromBranches);
  if (mark !== state.landing && gen === landingGen()) land(mark, false);
  const line = opts.line || "";
  const keepMax = !!(con.id && con.max); // a walk keeps the maximised layout
  const maxBy = con.maxBy;
  const page = switchingWorktree(); // a switch in flight: where the page is going (a fast walk's second press)
  const dir = switchTarget(info.worktree, page, wtOrder());
  // A console replacing a console keeps the way back; a switch starts one.
  const ret = (con.id ? con.ret : "") || (dir && !state.op ? page : "");
  if (con.id) closeConsole();
  Object.assign(con, { id, info, rows: [], frame: null, scroll: null, focused: false, max: keepMax, maxBy: keepMax ? maxBy : "", queue: [], size: null, ret });
  pushLayer("console", root, { onKey: consoleKey });
  layout();
  measureCell();
  ro.observe($("console-body"));
  ro.observe($("panes")); // a bar appearing above the panes moves them (an in-place switch's health lands late)
  connect();
  retitle();
  con.tick = setInterval(retitle, 15000);
  focusConsole();
  if (!dir) {
    if (line) opLine(line);
    return;
  }
  if (state.op) return opLine("an operation is running — switch once it has finished");
  await doReroot(dir, null, line); // false: refused or failed — its line is up, the console stays here
}

// leaveConsole is the console's OWN close (esc, ctrl+] twice, the session
// gone): the worktree it was shown over comes back (the TUI's returnView).
// closeConsole alone is a give-way — the user went on in this worktree's
// page (a sidebar click, a pane navigation), or a hide (alt+q's page
// stop, alt+w): no way back.
function leaveConsole() {
  const r = returnPlan(con.ret, state.worktree, wtOrder());
  closeConsole();
  if (r.msg) opLine(r.msg);
  if (r.path) doReroot(r.path);
}

function closeConsole() {
  if (!con.id) return;
  clearInterval(con.tick);
  ro.disconnect();
  if (con.es) con.es.close();
  con.es = null;
  con.id = "";
  con.ret = "";
  con.scroll = null;
  walked = null; // a console closed any other way brings the page's worktree back
  con.focused = con.max = false;
  closeLayer("console");
  popFoot("console");
}

// killSession asks once, then kills (remove = the TUI's X: the session leaves
// the list when its exit is recorded). The row and any console on it follow
// the live sessions event; nothing is repainted here. showLocalConfirm comes
// from ops.js, which reaches back here through sidebar.js — an import cycle
// that is safe only because it is CALLED from handlers, never at load.
function killSession(s, remove) {
  if (!s) return toast("that agent session is gone", { err: true });
  if (s.state === "exited") return remove ? removeSession(s) : toast(s.label + " has already exited");
  const yes = remove ? "kill and remove" : "kill";
  showLocalConfirm(killPrompt(s, remove), [yes, "cancel"], async (o) => {
    if (o !== yes) return; // cancel, esc — whatever the modal hands back
    try {
      await postJSON("/api/session-kill", { id: s.id, remove: !!remove });
      toast("killing " + s.label + " in " + wtName(s.worktree) + "…");
    } catch (e) {
      toast("kill: " + (e.message || e), { err: true });
    }
  });
}

async function removeSession(s) {
  if (!s) return;
  if (s.state !== "exited") return toast("only an exited session can be removed — X kills and removes a running one");
  try {
    await postJSON("/api/session-remove", { id: s.id });
  } catch (e) {
    toast("remove: " + (e.message || e), { err: true });
  }
}

function consoleSessionId() {
  return con.id;
}

// consoleFocusedId: the session the user is typing into ("" when none) —
// its notices are not toasted, the user is looking at it.
function consoleFocusedId() {
  return con.focused ? con.id : "";
}

// consoleSessions: the live list changed — retitle the shown session, close on
// its removal (the stream's gone event also does; the list may land first).
function consoleSessions(list) {
  if (!con.id) return;
  const info = (list || []).find((s) => s.id === con.id);
  if (!info) return leaveConsole();
  con.info = Object.assign({}, con.info, info);
  retitle();
}

// The console lies over the panes. When the user asks the page for something
// else it gives way (the session runs on; ctrl+\ brings it back):
//  - a click on a sidebar row navigates the panes (a session sub-row opens a
//    console instead, and openConsole replaces this one);
//  - "gg:panes" — a pane navigation (files.js setLayout), a surface opening
//    under the console, an agent showing something (live.js consoleGivesWay:
//    a navigate, a review, an overview, a file focus). A plain navigation counts only
//    shortly after a click OUTSIDE the console: background refreshes move
//    the panes too, and must never close a console the user is typing in.
const GIVE_WAY_MS = 10000;
let outsideClick = 0;

document.addEventListener("click", (e) => {
  if (!con.id) return;
  if (root.contains(e.target)) {
    outsideClick = 0;
    return;
  }
  if ($("foot").contains(e.target)) return; // the console's own foot buttons
  outsideClick = Date.now();
  const li = e.target.closest("#branches-pane li");
  if (li && !li.classList.contains("wsess")) closeConsole();
}, true);

document.addEventListener("gg:panes", (e) => {
  if (!con.id) return;
  if ((e.detail && e.detail.force) || Date.now() - outsideClick < GIVE_WAY_MS) closeConsole();
});

// --- the mouse (the TUI's console_mouse.go): to the program when it asks ---
const MOUSE_BUTTONS = ["left", "middle", "right"];
let mouseBtn = "";
let mouseCell = null;
let wheelAt = 0;

function mouseOn() {
  return con.id && !con.scroll && con.frame && con.frame.mouse && con.info && con.info.state !== "exited";
}

// The mouse goes one request at a time, in order (a release never
// overtakes its drag); pending motions merge (enqueueMouse).
let mouseQ = [];
let mouseBusy = false;
async function flushMouse() {
  if (mouseBusy) return;
  mouseBusy = true;
  try {
    while (mouseQ.length) {
      const ev = mouseQ.shift();
      await postJSON("/api/session-mouse", ev).catch(() => {});
    }
  } finally {
    mouseBusy = false;
  }
}

function gridCell(e) {
  const r = grid.getBoundingClientRect();
  const f = con.frame || {};
  return cellAt(e.clientX - r.left, e.clientY - r.top, con.cell || { w: 8, h: 16 }, f.cols, f.rows);
}

function sendMouse(kind, button, e) {
  const c = gridCell(e);
  mouseQ = enqueueMouse(mouseQ, { id: con.id, kind, button, x: c.x, y: c.y, mod: modOf(e) });
  flushMouse();
  return c;
}

grid.addEventListener("mousedown", (e) => {
  if (!con.id) return;
  if (!con.focused) focusConsole();
  if (!mouseOn()) return;
  e.preventDefault(); // the program selects; the browser does not
  mouseBtn = MOUSE_BUTTONS[e.button] || "left";
  mouseCell = sendMouse("press", mouseBtn, e);
});

window.addEventListener("mousemove", (e) => {
  if (!mouseBtn || !mouseOn()) return;
  const c = gridCell(e);
  if (mouseCell && c.x === mouseCell.x && c.y === mouseCell.y) return;
  mouseCell = sendMouse("motion", mouseBtn, e);
});

window.addEventListener("mouseup", (e) => {
  if (mouseBtn) {
    if (mouseOn()) sendMouse("release", mouseBtn, e);
    mouseBtn = "";
    return;
  }
  // scroll mode: a drag copies what it selected
  if (con.scroll && grid.contains(e.target)) {
    const sel = window.getSelection();
    const text = sel && !sel.isCollapsed && grid.contains(sel.anchorNode) ? sel.toString() : "";
    if (text) copyText(text, "selection");
  }
});

grid.addEventListener("contextmenu", (e) => {
  if (mouseOn()) e.preventDefault(); // the right button is the program's
});

// The wheel: the program's when it takes the mouse, ↑/↓ on an alternate
// screen without it, else scroll mode (it works without focusing).
grid.addEventListener("wheel", (e) => {
  if (!con.id) return;
  e.preventDefault();
  const now = Date.now();
  if (now - wheelAt < 30) return; // a trackpad's burst is one notch per 30 ms
  wheelAt = now;
  const up = e.deltaY < 0;
  if (con.scroll) {
    for (let k = 0; k < 3; k++) con.scroll.cur = scrollMove(con.scroll.cur, con.scroll.lines.length, up ? "up" : "down", 1);
    paintScroll();
    return;
  }
  const plan = wheelPlan(con.frame);
  if (plan === "scroll") {
    if (up) enterScroll();
    return;
  }
  if (con.info && con.info.state !== "exited") sendMouse("wheel", up ? "wheelup" : "wheeldown", e);
}, { passive: false });

registerHelp({
  key: "agent consoles",
  html:
    "<b>ctrl+\\</b> lists the agent sessions of this gg (Agents tab); <b>enter</b> opens one as a live console over the panes. " +
    "A focused console sends every key to the agent except <b>ctrl+]</b> (step out) and <b>ctrl+\\</b>; ctrl+w, ctrl+t and ctrl+n stay with the browser. " +
    "Unfocused: <b>enter</b> focus, <b>m</b> maximize, <b>k</b> kill, <b>X</b> kill and remove, <b>esc</b> or <b>ctrl+]</b> again close (the session keeps running); an exited one: <b>x</b> removes it. The viewer that has the console focused sets its size. " +
    "<b>alt+a</b> / <b>alt+t</b> walk this repository's running agents / terminals in the Worktrees list's order (the viewed worktree's own first; from a focused one, the next), <b>alt+A</b> / <b>alt+T</b> only the viewed worktree's, <b>alt+q</b> / <b>alt+Q</b> its screens (the page, then its agents and terminals in their branches rows' order), <b>alt+s</b> / <b>alt+S</b> the same screens of every worktree that runs an agent or a terminal, in the Worktrees list's order (past a worktree's last session, the next one's page; around again past the last) — even inside a focused console. " +
    "A console of another worktree shows in it: the page switches there, and the console's own close (esc, ctrl+] twice, the session gone) switches back. " +
    "<b>alt+w</b> closes the console and the file windows over the page, then steps the Worktrees list's worktrees. " +
    "Each walk marks the row it landed on in the sidebar's branches (the TUI's Branches cursor): the shown session's └ ● row, or the worktree's branch for alt+w and alt+q's page stop — so with two agents open you see which one this is. A click on another branches row clears the mark. " +
    "<b>alt+f</b> maximises or docks the console (focused after either way), <b>alt+B</b> moves the keys between the console and the page; <b>alt+b</b> on a focused console steps out and marks the session's row in the sidebar's branches (a maximised console docks so the row shows). " +
    "The wheel over a program that does not take the mouse (or <b>alt+pgup</b> focused, <b>pgup</b> unfocused) opens its scrollback: ↑/↓ pgup/pgdn g/G move, space marks a line range and enter copies it, a drag copies text, esc or q leave, any other key leaves and goes to the agent. " +
    "A program that asks for the mouse (Claude Code's fullscreen mode, mc) gets clicks, drags and the wheel; a full-screen one without mouse support gets ↑/↓ for the wheel.",
});

export { closeConsole, walkKey, consoleFocusedId, consoleSessionId, consoleSessions, killSession, openConsole, removeSession };
