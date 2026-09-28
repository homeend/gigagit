// console.js — an agent session's console in the page (web attach, plan 1):
// the server's screen painted as styled runs over a per-console SSE stream,
// keys and paste posted back, the focused viewer owning the session's size.
import { $, elidePath, getJSON, postJSON } from "./core.js";
import { closeLayer, mountOverlay, popFoot, pushFoot, pushLayer, topLayer } from "./layers.js";
import { registerHelp } from "./menus.js";
import { toast } from "./toast.js";

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

function gridSize(w, h, cw, ch) {
  return { cols: Math.max(1, Math.floor(w / cw)), rows: Math.max(1, Math.floor(h / ch)) };
}

// escRun is core.js's esc, repeated so the guarded section stands alone.
function escRun(s) {
  return String(s).replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));
}

function runHTML(run) {
  const st = [];
  if (run.fg) st.push("color:" + run.fg);
  if (run.bg) st.push("background:" + run.bg);
  const cls = ["b", "i", "u", "r", "d", "s"].filter((f) => run[f]).join(" ");
  return `<span${st.length ? ` style="${st.join(";")}"` : ""}${cls ? ` class="${cls}"` : ""}>${escRun(run.t)}</span>`;
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

function consoleTitle(s, now, elide) {
  const st = s.state === "exited" ? "exited (" + s.exit_code + ")" : "running " + ageText(s.started, now);
  return s.label + " · " + elide(s.worktree) + " · " + st;
}
// --- end console model ---

const con = { id: "", info: null, rows: [], frame: null, focused: false, max: false, es: null, queue: [], inflight: false, cell: null, size: null, tick: 0 };
const root = mountOverlay("console");
root.innerHTML =
  `<div id="console-title"><span id="console-label"></span><span id="console-size"></span></div>` +
  `<div id="console-body"><div id="console-grid"></div><div id="console-cursor" class="hidden"></div></div>`;
const grid = $("console-grid");
const cursor = $("console-cursor");

const FOOT_FOCUSED = `<button data-cact="out">ctrl+] step out</button><button data-cact="sessions">ctrl+\\ sessions</button><span class="cwarn">ctrl+w · ctrl+t · ctrl+n stay with the browser</span>`;
const FOOT_UNFOCUSED = `<button data-cact="focus">enter focus</button><button data-cact="max">m maximize</button><button data-cact="sessions">ctrl+\\ sessions</button><button data-cact="close">esc close</button>`;
const FOOT_EXITED = `<button data-cact="sessions">ctrl+\\ sessions</button><button data-cact="close">esc close</button>`;

// openSwitcher is the ctrl+\ popup (openfiles.js); a document event keeps
// the two modules from importing each other.
function askSwitcher() {
  document.dispatchEvent(new CustomEvent("gg:switcher"));
}

$("foot").addEventListener("click", (e) => {
  const b = e.target.closest("button[data-cact]");
  if (!b || !con.id) return;
  ({ out: stepOut, focus: focusConsole, max: maximize, close: closeConsole, sessions: askSwitcher })[b.dataset.cact]();
});

function measureCell() {
  const probe = document.createElement("span");
  probe.textContent = "M".repeat(20);
  probe.style.visibility = "hidden";
  grid.append(probe);
  const r = probe.getBoundingClientRect();
  probe.remove();
  con.cell = { w: r.width / 20, h: r.height };
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
  grid.innerHTML = con.rows.map((r) => `<div class="crow">${r || " "}</div>`).join("");
  paintCursor();
}

function paintCursor() {
  const f = con.frame;
  if (!f || !f.cursor || !con.cell || con.info.state === "exited") return cursor.classList.add("hidden");
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
  $("console-label").textContent = consoleTitle(con.info, Date.now(), (p) => elidePath(p, budget));
  $("console-size").textContent = con.frame ? con.frame.cols + "×" + con.frame.rows : "";
  $("console-label").classList.toggle("exited", con.info.state === "exited");
}

function foot() {
  pushFoot("console", con.info.state === "exited" ? FOOT_EXITED : con.focused ? FOOT_FOCUSED : FOOT_UNFOCUSED);
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
    con.info = Object.assign({}, con.info, { state: "exited", exit_code: JSON.parse(ev.data).code });
    con.focused = false;
    retitle();
    foot();
    paintCursor();
    if (!wasFocused) toast(con.info.label + " in " + con.info.worktree.split("/").pop() + " exited (" + con.info.exit_code + ")");
  });
  es.addEventListener("gone", () => {
    toast(con.info.label + " was removed");
    closeConsole();
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
  if (isReserved(e)) {
    e.preventDefault();
    if (e.code === "Backslash" || e.key === "\\") askSwitcher();
    else if (con.focused) stepOut();
    return true;
  }
  if (con.focused) {
    if (e.isComposing || e.key === "Process") return true; // an IME commit arrives as a char later
    const k = keyToWire(e);
    if (!k) return true; // the browser's own keys keep their default; nothing reaches the page
    e.preventDefault();
    send(k);
    return true;
  }
  switch (e.key) {
    case "Enter": focusConsole(); break;
    case "m": maximize(); break;
    case "Escape": closeConsole(); break;
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
  if (con.max) {
    con.max = false;
    layout();
  }
  foot();
  paintCursor();
}

function maximize() {
  if (!con.info || con.info.state === "exited") return;
  con.max = true;
  layout();
  focusConsole();
}

async function openConsole(id) {
  let body;
  try {
    body = await getJSON("/api/sessions");
  } catch (e) {
    return toast("sessions: " + (e.message || e), { err: true });
  }
  const info = (body.sessions || []).find((s) => s.id === id);
  if (!info) return toast("that agent session is gone", { err: true });
  if (con.id) closeConsole();
  Object.assign(con, { id, info, rows: [], frame: null, focused: false, max: false, queue: [], size: null });
  pushLayer("console", root, { onKey: consoleKey });
  layout();
  measureCell();
  ro.observe($("console-body"));
  connect();
  retitle();
  con.tick = setInterval(retitle, 15000);
  focusConsole();
}

function closeConsole() {
  if (!con.id) return;
  clearInterval(con.tick);
  ro.disconnect();
  if (con.es) con.es.close();
  con.es = null;
  con.id = "";
  con.focused = con.max = false;
  closeLayer("console");
  popFoot("console");
}

function consoleSessionId() {
  return con.id;
}

// consoleSessions: the live list changed — retitle the shown session, close on
// its removal (the stream's gone event also does; the list may land first).
function consoleSessions(list) {
  if (!con.id) return;
  const info = (list || []).find((s) => s.id === con.id);
  if (!info) return closeConsole();
  con.info = Object.assign({}, con.info, info);
  retitle();
}

grid.addEventListener("mousedown", () => {
  if (con.id && !con.focused) focusConsole();
});

registerHelp({
  key: "agent consoles",
  html:
    "<b>ctrl+\\</b> lists the agent sessions of this gg (Agents tab); <b>enter</b> opens one as a live console over the panes. " +
    "A focused console sends every key to the agent except <b>ctrl+]</b> (step out) and <b>ctrl+\\</b>; ctrl+w, ctrl+t and ctrl+n stay with the browser. " +
    "Unfocused: <b>enter</b> focus, <b>m</b> maximize, <b>esc</b> close (the session keeps running). The viewer that has the console focused sets its size.",
});

export { closeConsole, consoleSessionId, consoleSessions, openConsole };
