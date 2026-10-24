// launch.js — the AI-task launch dialog (the TUI's task_launch_popup.go) and
// what happens to a task THIS page started (the TUI's task_track.go): every
// web AI action (review, commit message, AI resolve) picks an agent (←/→) and
// a mode and variant (↑/↓) — interactive in the background, interactive in
// the foreground (its console opens), or headless — then goes to the
// process's task scheduler. Results land where the TUI puts them: a commit
// message in the commit box, a review as its review, a resolution overview in
// the report viewer; a result that would get in the way waits behind a toast
// ("… ready — ctrl+\"). Only tasks this tab submitted are applied here: a
// terminal hosting the page applies its own.
import { $, esc, getJSON, postJSON, state } from "./core.js";
import { closeLayer, layers, mountOverlay, pushLayer } from "./layers.js";
import { opLine, refreshAfterOp, showLocalConfirm } from "./ops.js";
import { consoleFocusedId, openConsole } from "./console.js";
import { measure } from "./sessions.js";
import { openReview } from "./reviews.js";
import { openReport } from "./review.js";
import { toast } from "./toast.js";

// --- launch model (pure; guarded against Go) ---
const MODE_LABELS = { background: "Interactive, background", foreground: "Interactive, foreground", headless: "Headless" };

// firstSelectable is the first row with a command.
function firstSelectable(rows) {
  const i = rows.findIndex((r) => !r.none);
  return i < 0 ? 0 : i;
}

// preselect is the TUI's: the remembered agent, then its mode and command,
// else that mode's first command, else the agent's first runnable row.
function preselect(choices, pick) {
  let agent = 0;
  if (pick && pick.identity) {
    const i = choices.findIndex((c) => c.identity === pick.identity);
    if (i >= 0) agent = i;
  }
  const rows = choices[agent].rows;
  let sel = firstSelectable(rows);
  if (pick && pick.identity === choices[agent].identity) {
    const exact = rows.findIndex((r) => !r.none && r.mode === pick.mode && r.name === pick.name);
    const mode = rows.findIndex((r) => !r.none && r.mode === pick.mode);
    if (exact >= 0) sel = exact;
    else if (mode >= 0) sel = mode;
  }
  return { agent, sel };
}

// switchAgent cycles the agent, keeping the mode when the new agent has it.
function switchAgent(choices, at, dir) {
  const n = choices.length;
  if (n < 2) return at;
  const mode = choices[at.agent].rows[at.sel].mode;
  const agent = (at.agent + dir + n) % n;
  const rows = choices[agent].rows;
  const same = rows.findIndex((r) => !r.none && r.mode === mode);
  return { agent, sel: same >= 0 ? same : firstSelectable(rows) };
}

// moveSel steps the cursor over selectable rows (no wrap).
function moveSel(rows, sel, dir) {
  for (let i = sel + dir; i >= 0 && i < rows.length; i += dir) if (!rows[i].none) return i;
  return sel;
}
// --- end launch model ---

const box = mountOverlay("launch");
box.innerHTML = `<div id="launch-box"><div id="launch-body"></div></div>`;
let dlg = null; // { req, key, choices, at, wait, stage: "choose"|"approve", command, tool, busy }
let opening = false; // the launch GET is in flight: a second click must not open a second dialog

// openLaunch opens the dialog for req ({kind, target, branch, sha, preview,
// shas}) — the one door every web AI action goes through.
async function openLaunch(req) {
  if (dlg || opening) return;
  opening = true;
  try {
    await openLaunchNow(req);
  } finally {
    opening = false;
  }
}

async function openLaunchNow(req) {
  const q = new URLSearchParams();
  for (const [k, v] of Object.entries(req)) if (v) q.set(k, Array.isArray(v) ? v.join(",") : v);
  let got;
  try {
    got = await getJSON("/api/tasks/launch?" + q);
  } catch (e) {
    opLine(kindLabel(req.kind) + ": " + (e.message || e), true);
    return;
  }
  if (got.added && got.added.length) opLine("Added " + got.added.join(", ") + " to " + got.config_path + " — edit there or in Settings → External tools");
  if (!got.choices || !got.choices.length) {
    opLine("no " + kindLabel(req.kind) + " agent configured (Settings → External tools)", true);
    return;
  }
  dlg = { req, key: got.key, choices: got.choices, at: preselect(got.choices, got.pick), wait: got.wait || "", stage: "choose" };
  pushLayer("launch", box, { onKey: launchKey });
  render();
}

function kindLabel(kind) {
  return { commit_message: "commit message", review: "review", conflict: "resolve conflict", conflict_complete: "resolve & complete" }[kind] || kind;
}

function closeLaunch() {
  dlg = null;
  closeLayer("launch");
}

function render() {
  if (!dlg) return;
  const body = $("launch-body");
  if (dlg.stage === "approve") {
    body.innerHTML =
      `<div class="ltitle">Run this command?  (${esc(dlg.tool)})</div><pre class="lcmd">${esc(dlg.command)}</pre>` +
      `<div class="lnote">Approval is remembered for this repo until the command changes.</div>` +
      `<div class="lbtns"><button data-l="run">run</button><button data-l="back">back</button></div>`;
    return;
  }
  const c = dlg.choices[dlg.at.agent];
  body.innerHTML =
    `<div class="ltitle">${esc(dlg.key)}</div>` +
    `<div class="lagent">Agent <button data-l="prev">◀</button> ${esc(c.agent)} <button data-l="next">▶</button><span class="lcount">${dlg.at.agent + 1}/${dlg.choices.length}</span></div>` +
    `<div class="lrows">` +
    c.rows
      .map((r, i) => {
        const text = esc(MODE_LABELS[r.mode]) + `<span class="lname">${r.none ? "(no command for this agent)" : esc(r.name) + (r.missing ? "  not found" : "")}</span>`;
        return `<div class="lrow${r.none ? " none" : ""}${i === dlg.at.sel ? " sel" : ""}" data-i="${i}">${text}</div>`;
      })
      .join("") +
    `</div>` +
    (dlg.wait ? `<div class="lwait">${esc(dlg.wait)}</div>` : "") +
    `<div class="lbtns"><span class="lhint">←/→ agent  ↑/↓ mode</span><button data-l="run">run</button><button data-l="cancel">cancel</button></div>`;
}

// askApproval shows a command in full before its first run (the dialog's
// approve stage, standalone): the conflict window's in-place tools use it.
// Resolves true on run, false on back / esc / a backdrop click.
function askApproval(tool, command) {
  if (dlg) return Promise.resolve(false);
  return new Promise((resolve) => {
    dlg = { stage: "approve", standalone: true, tool, command, resolve };
    pushLayer("launch", box, { onKey: launchKey });
    render();
  });
}

// endApproval closes a standalone approval with its answer.
function endApproval(ok) {
  const r = dlg && dlg.resolve;
  closeLaunch();
  if (r) r(ok);
}

box.addEventListener("click", (e) => {
  if (!dlg || dlg.busy) return; // a submit is in flight
  if (dlg.standalone) {
    const b = e.target.closest("button[data-l]");
    if (e.target === box || (b && b.dataset.l === "back")) return endApproval(false);
    if (b && b.dataset.l === "run") return endApproval(true);
    return;
  }
  if (e.target === box) return closeLaunch();
  const row = e.target.closest(".lrow[data-i]");
  if (row && !row.classList.contains("none")) {
    dlg.at.sel = Number(row.dataset.i);
    return render();
  }
  const b = e.target.closest("button[data-l]");
  if (!b) return;
  const act = b.dataset.l;
  if (act === "prev" || act === "next") {
    dlg.at = switchAgent(dlg.choices, dlg.at, act === "next" ? 1 : -1);
    render();
  } else if (act === "run") dlg.stage === "approve" ? submit(true) : pick();
  else if (act === "back") {
    dlg.stage = "choose";
    render();
  } else closeLaunch();
});

function launchKey(e) {
  if (!dlg || dlg.busy) {
    // A submit is in flight: enter again (or auto-repeat) must not start a
    // second task, and esc must not close a dialog whose task is starting.
    if (!e.ctrlKey && !e.metaKey) e.preventDefault();
    return true;
  }
  if (dlg.standalone) {
    if (e.key === "Enter" || e.key === "y") endApproval(true);
    else if (e.key === "Escape" || e.key === "n") endApproval(false);
  } else if (dlg.stage === "approve") {
    if (e.key === "Enter" || e.key === "y") submit(true);
    else if (e.key === "Escape" || e.key === "n") {
      dlg.stage = "choose";
      render();
    }
  } else {
    const rows = dlg.choices[dlg.at.agent].rows;
    if (e.key === "Escape") closeLaunch();
    else if (e.key === "ArrowLeft" || e.key === "h") dlg.at = switchAgent(dlg.choices, dlg.at, -1);
    else if (e.key === "ArrowRight" || e.key === "l") dlg.at = switchAgent(dlg.choices, dlg.at, 1);
    else if (e.key === "ArrowUp" || e.key === "k") dlg.at.sel = moveSel(rows, dlg.at.sel, -1);
    else if (e.key === "ArrowDown" || e.key === "j") dlg.at.sel = moveSel(rows, dlg.at.sel, 1);
    else if (e.key === "Enter") return pick(), e.preventDefault(), true;
    if (dlg) render();
  }
  if (!e.ctrlKey && !e.metaKey) e.preventDefault();
  return true;
}

// pick runs the selected row: a missing program is refused here; the server
// answers an unapproved command with its resolved text, shown before it runs.
function pick() {
  const r = dlg.choices[dlg.at.agent].rows[dlg.at.sel];
  if (!r || r.none) return;
  if (r.missing) return opLine(r.name + ": program not found on PATH", true);
  submit(false);
}

async function submit(approve) {
  if (!dlg || dlg.busy) return;
  const c = dlg.choices[dlg.at.agent];
  const r = c.rows[dlg.at.sel];
  const kind = dlg.req.kind;
  const body = { ...dlg.req, identity: c.identity, mode: r.mode, name: r.name, approve, ...measure() };
  dlg.busy = true;
  let got;
  try {
    got = await postJSON("/api/tasks/submit", body);
  } catch (e) {
    if (dlg) dlg.busy = false;
    if (e.data && e.data.needs_approval && dlg) {
      dlg.stage = "approve";
      dlg.command = e.data.command;
      dlg.tool = e.data.tool;
      return render();
    }
    opLine("could not start " + r.name + ": " + (e.message || e), true);
    return;
  }
  closeLaunch();
  remember(got.id, { kind, fg: r.mode === "foreground", seen: 0, ended: false });
  opLine((got.state === "queued" ? "queued " : "started ") + got.key);
  // A task can end before this reply arrives (its last live event already
  // came): catch up from the rows as they stand.
  launchTasks(state.tasks || []);
}

// --- what this page's tasks produce (the TUI's onTasksChanged) ---
// mine survives a reload of this tab (a repo switch reloads the page) in
// sessionStorage — per tab, so another tab never applies this one's results.
const MINE_KEY = "gg-launch-mine";
const mine = new Map(loadMine()); // task id → { kind, fg, seen, ended }

function loadMine() {
  try {
    return JSON.parse(sessionStorage.getItem(MINE_KEY) || "[]");
  } catch {
    return [];
  }
}

function saveMine() {
  try {
    sessionStorage.setItem(MINE_KEY, JSON.stringify([...mine].filter(([, m]) => !m.ended)));
  } catch {
    // no storage (a private window): this tab simply forgets on reload
  }
}

function remember(id, m) {
  mine.set(id, m);
  saveMine();
}

// canShow: a result may open now — the TUI's canShowResult: nothing owns
// the keyboard (no layer but an unfocused console, no focused console, no
// field being typed in) and the panes show the list, not a diff being read —
// a review replaces the files pane, which has no way back to a diff.
function canShow() {
  const a = document.activeElement;
  const typing = a && (a.tagName === "INPUT" || a.tagName === "TEXTAREA" || a.isContentEditable);
  return !layers.some((l) => l.id !== "console") && !consoleFocusedId() && !typing && state.layout !== "diff";
}

// launchTasks takes every "tasks" live event's rows (live.js).
function launchTasks(rows) {
  let changed = false;
  for (const t of rows) {
    const m = mine.get(t.id);
    if (!m) continue;
    changed = true;
    if (m.fg && t.session) {
      m.fg = false;
      openConsole(t.session);
    }
    if (t.results > m.seen) {
      m.seen = t.results;
      applyResult(t);
    }
    if (!t.live && !m.ended) {
      m.ended = true;
      taskEnded(t, m);
    }
  }
  if (changed) saveMine();
}

// taskEnded reports an end with no result of its own to show: a failure, or
// a conflict run that reported nothing (the repository is its outcome).
function taskEnded(t, m) {
  if (t.state === "failed") toast(t.key + " failed — " + (t.err || "unknown error") + "  (ctrl+\\ for details)", { err: true });
  else if (t.state === "done" && t.results === 0 && (m.kind === "conflict" || m.kind === "conflict_complete")) {
    opLine(t.key + " finished");
    refreshAfterOp();
  }
}

async function resultText(t) {
  const got = await getJSON("/api/tasks/result?id=" + encodeURIComponent(t.id)).catch(() => null);
  return got ? got.text || "" : "";
}

async function applyResult(t) {
  const ready = () => toast(t.key + " ready — ctrl+\\");
  const here = t.worktree === state.worktree;
  if (t.kind === "commit_message") {
    const box = $("commit-box"), ta = $("commit-msg");
    if (!here || box.classList.contains("hidden")) return toast("commit message from " + t.agent + " ready — ctrl+\\");
    const text = (await resultText(t)).trim();
    if (!text) return;
    if (ta.value.trim() === "") {
      ta.value = text;
      return opLine("message from " + t.agent);
    }
    // The one modal may hold an operation's decision: never take it over —
    // the message waits in the AI tasks tab (enter → apply).
    if (state.op || layers.some((l) => l.id === "modal")) return toast("commit message from " + t.agent + " ready — ctrl+\\");
    return showLocalConfirm("A commit message from " + t.agent + " is ready. Replace the one you typed?", ["replace", "keep"], (o) => {
      if (o === "replace") ta.value = text;
    });
  }
  if (t.kind === "review") {
    if (t.save_err) return toast(t.key + ": " + t.save_err + " — ctrl+\\ to retry", { err: true });
    if (!here || !canShow()) return ready();
    if (t.note_id) return openReview(t.note_id, { kind: "list" });
    return openReport("Review " + t.key.replace(/^review — /, ""), "", await resultText(t));
  }
  if (t.kind === "conflict" || t.kind === "conflict_complete") {
    if (here) refreshAfterOp();
    if (!here || !canShow()) return ready();
    return openReport("Resolution overview — " + t.agent, "", await resultText(t));
  }
}

export { askApproval, firstSelectable, launchTasks, moveSel, openLaunch, preselect, switchAgent };
