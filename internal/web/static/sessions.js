// sessions.js — starting sessions from the page (web attach, plan 2): the
// Start agent dialog (first-run detect, list, approval), Open terminal, and
// the menu rows that reach them. Kill and remove live in console.js.
import { $, esc, getJSON, postJSON, ssGet, ssSet } from "./core.js";
import { closeLayer, mountOverlay, popFoot, pushFoot, pushLayer } from "./layers.js";
import { registerHelp, registerRows } from "./menus.js";
import { toast } from "./toast.js";
import { killSession, openConsole, removeSession } from "./console.js";
import { worktreePathForBranch } from "./sidebar.js";
import { doReroot } from "./ops.js";
import { openViewer } from "./viewer.js";

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
// row and then approve its command before anyone could read it. A pick goes
// to the optional name step (after the approval when the command needs
// one); there every key but enter and esc is the name input's.
function dialogStep(d, key, repeat) {
  if (d.phase === "starting" || (repeat && key === "Enter")) return {};
  if (d.phase === "detecting") return key === "Escape" ? { close: true } : {};
  if (d.phase === "name") {
    if (key === "Enter") return { start: d.sel, approve: !!d.approved };
    if (key === "Escape") return d.cmds.length > 1 ? { phase: "choose" } : { close: true };
    return {};
  }
  if (d.phase === "approve") {
    if (key === "Enter") return { phase: "name", approved: true };
    if (key === "Escape") return d.cmds.length > 1 ? { phase: "choose" } : { close: true };
    return {};
  }
  const pick = (i) => (d.cmds[i].approved ? { sel: i, phase: "name" } : { sel: i, phase: "approve" });
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

// A worker's brief and its latest report open as tours (stage 4), first.
function sessionMenuRows(s) {
  const tours = [];
  if (s.has_brief) tours.push({ id: "brief", label: "Open brief" });
  if (s.has_report) tours.push({ id: "report", label: "Open report" });
  return tours.concat(s.state === "exited"
    ? [{ id: "remove", label: "Remove session" }]
    : [{ id: "kill", label: "Kill session" }, { id: "killrm", label: "Kill and remove session" }]);
}
// --- end sessions model ---

let dlg = null; // { phase, path, cmds, sel, names, name, approved }
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
  name: `<span>enter start · alt+↓ recent names · esc back</span>`,
  starting: `<span>starting…</span>`,
};

function render() {
  if (!dlg) return;
  const cur = dlg.cmds[dlg.sel];
  $("sessstart-title").textContent =
    dlg.phase === "approve" ? "Start this agent?  (" + cur.name + ")" : dlg.phase === "name" ? "Name this agent (optional)" : "Start agent in " + wtLeaf(dlg.path);
  const body = $("sessstart-body");
  if (dlg.phase === "detecting") body.innerHTML = `<div class="rnote">⏳ Detecting installed agents…</div>`;
  else if (dlg.phase === "starting") body.innerHTML = `<div class="rnote">starting ${esc(cur.name + (dlg.name.trim() ? " [" + dlg.name.trim() + "]" : ""))}…</div>`;
  else if (dlg.phase === "name") {
    body.innerHTML =
      `<div class="rnote">Start ${esc(cur.name)} in ${esc(wtLeaf(dlg.path))}</div>` +
      `<input id="sessstart-name" list="sessstart-names" maxlength="40" placeholder="optional — alt+↓ recent names" autocomplete="off">` +
      `<datalist id="sessstart-names">${(dlg.names || []).map((n) => `<option value="${esc(n)}">`).join("")}</datalist>`;
    const inp = $("sessstart-name");
    inp.value = dlg.name;
    inp.addEventListener("input", () => {
      if (dlg) dlg.name = inp.value;
    });
    inp.focus();
  }
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
  if (step.phase === "choose") Object.assign(dlg, { name: "", approved: false });
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
    await post({ worktree: d.path, tool: d.cmds[i].name, approve, name: d.name });
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
  const d = (dlg = { phase: "detecting", path, cmds: [], sel: 0, names: [], name: "", approved: false });
  pushLayer("sessstart", root, {
    onKey: (e) => {
      // The name step's typing (alt+↓ included) is the input's: no step,
      // no preventDefault — the stack still keeps it from gg's shortcuts.
      if (dlg && dlg.phase === "name" && e.key !== "Enter" && e.key !== "Escape") return false;
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
  d.names = body.names || [];
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
    danger: r.id === "kill" || r.id === "killrm",
    act: () => (r.id === "brief" || r.id === "report" ? openTour(s, r.id)
      : r.id === "remove" ? removeSession(s) : killSession(s, r.id === "killrm")),
  })));

// openTour asks the server to file the tour (again, if it was closed), then
// opens it — after switching the page (a hosted page: the terminal too)
// when the server says it lives in another worktree; the switch's reload
// leaves a one-shot key that boot reads back (openPendingTour).
async function openTour(s, kind) {
  let r;
  try {
    r = await postJSON("/api/agent-tour", { id: s.id, kind });
  } catch (e) {
    toast(String(e.message || e));
    return;
  }
  if (!r.here) {
    doReroot(r.worktree, r.overview); // the switch's reload carries the tour (ops.js)
    return;
  }
  openViewer({ id: r.overview });
}

// openPendingTour: the tour a switch was made for, once the page is up.
function openPendingTour() {
  const id = ssGet("gg-open-tour");
  if (!id) return;
  ssSet("gg-open-tour", "");
  openViewer({ id }).catch(() => {});
}

registerHelp({
  key: "starting agents",
  html:
    "a worktree's menu (and a branch's, when it is checked out in a worktree) offers <b>Start agent in …</b> and <b>Open terminal in …</b>; the new session opens as a focused console. " +
    "A command runs only after you approve it once per repository. A session's own row (right-click) offers <b>Kill</b>, <b>Kill and remove</b> and, once exited, <b>Remove</b>.",
});

export { openPendingTour, openTerminal, startAgent };
