// sessions.js — starting sessions from the page (web attach, plan 2): the
// Start agent dialog (first-run detect, list, approval), Open terminal, and
// the menu rows that reach them. Kill and remove live in console.js.
import { $, esc, getJSON, postJSON } from "./core.js";
import { closeLayer, mountOverlay, popFoot, pushFoot, pushLayer } from "./layers.js";
import { registerHelp, registerRows } from "./menus.js";
import { toast } from "./toast.js";
import { killSession, openConsole, removeSession } from "./console.js";
import { worktreePathForBranch } from "./sidebar.js";

// --- sessions model (pure; guarded against Go) ---
function commandRowState(c) {
  return !c.found ? "not found" : c.approved ? "approved" : "approve on start";
}

function wtLeaf(path) {
  return String(path).split(/[\\/]/).filter(Boolean).pop() || String(path);
}

// dialogStep: what a key does to the dialog — a patch ({sel}, {phase}), a
// {start: i, approve} order, {close: true}, or {} for nothing. Pure: the
// dialog applies it. Detecting only closes; a start in flight takes no key
// at all (a held enter must not start twice, esc must not orphan it). A
// REPEATED enter (the key held down) does nothing anywhere: it would pick a
// row and then approve its command before anyone could read it.
function dialogStep(d, key, repeat) {
  if (d.phase === "starting" || (repeat && key === "Enter")) return {};
  if (d.phase === "detecting") return key === "Escape" ? { close: true } : {};
  if (d.phase === "approve") {
    if (key === "Enter") return { start: d.sel, approve: true };
    if (key === "Escape") return d.cmds.length > 1 ? { phase: "choose" } : { close: true };
    return {};
  }
  const pick = (i) => (d.cmds[i].approved ? { start: i, approve: false } : { sel: i, phase: "approve" });
  if (key === "Escape") return { close: true };
  if (key === "ArrowDown" || key === "j") return { sel: Math.min(d.cmds.length - 1, d.sel + 1) };
  if (key === "ArrowUp" || key === "k") return { sel: Math.max(0, d.sel - 1) };
  if (key === "Enter") return d.cmds.length ? pick(d.sel) : {};
  if (key.length === 1 && key >= "1" && key <= "9") return Number(key) <= d.cmds.length ? pick(Number(key) - 1) : {};
  return {};
}

// startRows / sessionMenuRows: the menu rows as {id, label}; the impure side
// attaches the actions by id.
function startRows(path) {
  const n = wtLeaf(path);
  return [{ id: "agent", label: "Start agent in " + n }, { id: "terminal", label: "Open terminal in " + n }];
}

function sessionMenuRows(s) {
  return s.state === "exited"
    ? [{ id: "remove", label: "Remove session" }]
    : [{ id: "kill", label: "Kill session" }, { id: "killrm", label: "Kill and remove session" }];
}
// --- end sessions model ---

let dlg = null; // { phase, path, cmds, sel }
const root = mountOverlay("sessstart");
root.innerHTML = `<div id="sessstart-box"><div id="sessstart-title"></div><div id="sessstart-body"></div></div>`;
root.addEventListener("click", (e) => {
  if (!dlg) return;
  if (e.target.id === "sessstart") {
    if (dlg.phase !== "starting") closeDialog();
    return;
  }
  const li = e.target.closest("li[data-i]");
  if (li && dlg.phase === "choose") {
    dlg.sel = Number(li.dataset.i);
    return apply(dialogStep(dlg, "Enter"));
  }
  if (e.target.closest("button[data-run]")) apply(dialogStep(dlg, "Enter"));
});

const FOOT = {
  detecting: `<span>esc cancel</span>`,
  choose: `<span>↑↓ move · 1–9 / enter start · esc cancel</span>`,
  approve: `<span>enter run · esc back</span>`,
  starting: `<span>starting…</span>`,
};

function render() {
  if (!dlg) return;
  const cur = dlg.cmds[dlg.sel];
  $("sessstart-title").textContent = dlg.phase === "approve" ? "Start this agent?  (" + cur.name + ")" : "Start agent in " + wtLeaf(dlg.path);
  const body = $("sessstart-body");
  if (dlg.phase === "detecting") body.innerHTML = `<div class="rnote">⏳ Detecting installed agents…</div>`;
  else if (dlg.phase === "starting") body.innerHTML = `<div class="rnote">starting ${esc(cur.name)}…</div>`;
  else if (dlg.phase === "approve")
    body.innerHTML =
      `<div class="rcmd">${esc(cur.command)}</div>` +
      `<div class="rnote">This runs on your machine with your permissions. Approval is remembered for this repo until the command text changes.</div>` +
      `<button data-run="1">run</button>`;
  else
    body.innerHTML =
      "<ul>" +
      dlg.cmds.map((c, i) => `<li data-i="${i}"${i === dlg.sel ? ' class="sel"' : ""}>${i + 1}  ${esc(c.name)}<span class="detail">${commandRowState(c)}</span></li>`).join("") +
      "</ul>";
  pushFoot("sessstart", FOOT[dlg.phase]);
}

function closeDialog() {
  dlg = null;
  closeLayer("sessstart");
  popFoot("sessstart");
}

function apply(step) {
  if (!dlg) return;
  if (step.close) return closeDialog();
  if (step.start !== undefined) return run(step.start, step.approve);
  Object.assign(dlg, step);
  render();
}

// measure: the grid the console layer will have, so the session starts at
// the size its first viewer shows (no default-then-resize flash). The font
// is the console grid's (style.css #console-grid).
function measure() {
  const probe = document.createElement("span");
  probe.textContent = "M".repeat(20);
  probe.style.cssText = "visibility:hidden;position:absolute;white-space:pre;font:12px/16px ui-monospace, Menlo, Consolas, monospace";
  document.body.append(probe);
  const r = probe.getBoundingClientRect();
  probe.remove();
  const panes = $("panes").getBoundingClientRect();
  const side = $("branches-pane");
  const left = side && side.offsetWidth ? side.getBoundingClientRect().right + 5 : panes.left;
  const h = $("foot").getBoundingClientRect().top - panes.top;
  const cw = r.width / 20 || 7.2;
  // 16 = a console row's height (style.css .conrow) — NOT the probe's own
  // height, which is the font's content box and asks for too many rows.
  return { cols: Math.max(20, Math.floor((window.innerWidth - left - 32) / cw)), rows: Math.max(5, Math.floor((h - 50) / 16)) };
}

async function post(body) {
  const resp = await postJSON("/api/session-start", Object.assign(body, measure()));
  if (resp.warning) toast(resp.warning, { err: true }); // bad screen_* rules: the built-ins apply
  openConsole(resp.session.id);
}

async function run(i, approve) {
  const d = dlg;
  d.sel = i;
  d.phase = "starting";
  render();
  try {
    await post({ worktree: d.path, tool: d.cmds[i].name, approve });
    if (dlg === d) closeDialog();
  } catch (e) {
    if (dlg !== d) return;
    if (e.data && e.data.needs_approval) {
      // The server's word wins over the list's: approve what will RUN.
      d.cmds[i] = { ...d.cmds[i], approved: false, command: e.data.command || d.cmds[i].command };
      d.phase = "approve";
      return render();
    }
    closeDialog();
    toast("could not start " + d.cmds[i].name + ": " + (e.message || e), { err: true });
  }
}

async function startAgent(path) {
  if (dlg) return;
  const d = (dlg = { phase: "detecting", path, cmds: [], sel: 0 });
  pushLayer("sessstart", root, {
    onKey: (e) => {
      if (dlg) apply(dialogStep(dlg, e.key, e.repeat));
      e.preventDefault();
      return true; // the dialog owns the keyboard
    },
  });
  render();
  let body;
  try {
    body = await getJSON("/api/session-commands?worktree=" + encodeURIComponent(path));
  } catch (e) {
    if (dlg === d) closeDialog();
    return toast("start agent: " + (e.message || e), { err: true });
  }
  if (dlg !== d) return; // closed while detecting
  if (body.added && body.added.length) toast("Added " + body.added.join(", ") + " to " + body.config_path + " — edit there or in Settings → External tools");
  d.cmds = body.commands || [];
  if (!d.cmds.length) {
    closeDialog();
    return toast('no agent found — add a [[tools.command]] block with category = "session" and mode = "session"', { err: true });
  }
  d.phase = "choose";
  if (d.cmds.length === 1) return apply(dialogStep(d, "Enter"));
  render();
}

async function openTerminal(path) {
  try {
    await post({ worktree: path, terminal: true });
  } catch (e) {
    toast("could not start a terminal: " + (e.message || e), { err: true });
  }
}

const startActs = (path) => startRows(path).map((r) => ({ label: r.label, act: () => (r.id === "agent" ? startAgent(path) : openTerminal(path)) }));

registerRows("worktree", (w) => (w && w.path && !w.bare ? startActs(w.path) : []));
registerRows("branch", (b) => {
  const path = b && worktreePathForBranch(b.name);
  return path ? startActs(path) : [];
});
registerRows("session", (s) =>
  sessionMenuRows(s).map((r) => ({
    label: r.label,
    danger: r.id !== "remove",
    act: () => (r.id === "remove" ? removeSession(s) : killSession(s, r.id === "killrm")),
  })));

registerHelp({
  key: "starting agents",
  html:
    "a worktree's menu (and a branch's, when it is checked out in a worktree) offers <b>Start agent in …</b> and <b>Open terminal in …</b>; the new session opens as a focused console. " +
    "A command runs only after you approve it once per repository. A session's own row (right-click) offers <b>Kill</b>, <b>Kill and remove</b> and, once exited, <b>Remove</b>.",
});

export { openTerminal, startAgent };
