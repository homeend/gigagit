// ops.js — part of gg's web client. Split from the original app.js;
// see app.js (the entry module) for the load order.
import { $, DANGER_OPTIONS, esc, getJSON, lsSet, postJSON, state } from "./core.js";
import { closeLayer, pushLayer } from "./layers.js";
import { fetchStatus } from "./status.js";
import { fetchBranches } from "./sidebar.js";
import { closeReviewLane, unparkReview } from "./review.js";
import { closeCommitFilter, loadCommits, renderCommits } from "./commits.js";
import { reconcileStatusView, stage } from "./files.js";
import { fetchHealth } from "./bigrepo.js";

// --- op transport client ---

let opLineTimer = null;

function hideOpLine() {
  clearTimeout(opLineTimer);
  $("op-text").textContent = "";
  $("op-line").classList.add("hidden");
}


let taskRestoreTimer = null;

function opLine(text, isErr, isTask) {
  const el = $("op-line");
  $("op-text").textContent = text || "";
  el.classList.toggle("err", !!isErr);
  el.classList.remove("task"); // a new message is never the parked-run handle
  el.classList.toggle("hidden", !text);
  clearTimeout(opLineTimer);
  if (!text) return;
  // A transient notice may borrow the line from the parked-run handle (there
  // is only one line), but the handle must come BACK: it is the standing
  // indicator that something is still running (user report: the "already
  // running" guard ate the background-run line for good).
  if (!isTask) {
    clearTimeout(taskRestoreTimer);
    if (parkedRunning()) {
      taskRestoreTimer = setTimeout(() => {
        if (parkedRunning() && !$("op-line").classList.contains("task")) taskLine(parkedTaskText());
      }, 6000);
    }
  }
  // every message expires after 30s — but never while its op still runs
  // (each op event overwrites the line and re-arms the timer anyway)
  opLineTimer = setTimeout(() => {
    // NOT opBusy(): that reports, and reporting re-arms this timer.
    // The parked-run HANDLE never expires: it stays until the run finishes
    // (reviewDone repaints or hides it). Only ordinary notices time out.
    if (state.op && !parkedRunning()) return;
    if ($("op-line").classList.contains("task") && parkedRunning()) return;
    hideOpLine();
  }, 30000);
}


// parkedTaskText is the parked-run handle's one message — shared by the park
// action and the restore-after-a-transient-notice timer above.
function parkedTaskText() {
  const noun = state.task && state.task.kind === "conflict" ? "AI resolve" : "review";
  return noun + " running in the background — click here to watch or cancel it";
}


// taskLine is the parked-run status line. Unlike every other message it is a
// live HANDLE on something still running, not a notice — so it is clickable
// and exempt from dismissal (see the two listeners below).
function taskLine(text) {
  opLine(text, false, true); // clears .task first, then we claim it
  $("op-line").classList.add("task");
}


// Clicking the parked-run line reopens the review. This line is the biggest
// thing on screen mentioning that run, so it is what people click; without
// this the click did nothing and the double-click below deleted it.
$("op-line").addEventListener("click", () => {
  if ($("op-line").classList.contains("task") && parkedRunning()) unparkReview();
});


// Double-click anywhere in the strip dismisses it immediately (the error
// header advertises this) — but NOT the parked-run line, whose dismissal is
// undiscoverable (the header only renders on errors) and which would silently
// remove the most visible mention of a running review.
$("op-line").addEventListener("dblclick", () => {
  if ($("op-line").classList.contains("task")) return;
  hideOpLine();
});


// startOp is the transport client, op-agnostic: POST /api/op, then follow
// the SSE stream. state.op.kind lets done-handling react per op (a commit
// clears the message box; a switch must not eat a draft).
// opBusy is the shared "an operation is already running" guard for every op
// entry point. It REPORTS rather than returning silently: these guards used
// to sit in front of a visible modal or progress line that explained itself,
// but a backgrounded review holds the lane with nothing on screen at all —
// and a button that does nothing without saying why reads as broken.
// parkedRunning: a review is live but backgrounded, so the only thing on
// screen speaking for it is the top-bar chip.
function parkedRunning() {
  return !!(state.task && state.task.status === "running");
}


function opBusy() {
  if (!state.op) return false;
  if (parkedRunning()) {
    const noun = state.task.kind === "conflict" ? "an AI resolve run" : "a review";
    opLine(noun + " is running in the background — open the chip to watch or cancel it", true);
  } else {
    opLine("an operation is already running", true);
  }
  return true;
}


async function startOp(body, label) {
  if (opBusy()) return; // one live op; the server would 409 anyway
  let resp;
  try {
    resp = await postJSON("/api/op", body);
  } catch (e) {
    opLine("error: " + (e.message || e), true);
    return;
  }
  followOp(resp.op_id, label, body.op, null);
}


// followOp attaches the SSE client to an already-started run. Split out of
// startOp because the review lane starts its run at a different endpoint but
// wants the identical stream handling — including the lost-connection rules.
// onDone, when given, REPLACES the generic done handling for that run.
function followOp(opID, label, kind, onDone) {
  opLine("⟳ " + label + "…");
  const es = new EventSource("/api/op/" + opID + "/events");
  state.op = { id: opID, es, kind, onDone: onDone || null };
  $("pull-btn").disabled = true;
  $("push-btn").disabled = true;
  es.onmessage = (m) => handleOpEvent(JSON.parse(m.data));
  // EventSource auto-retries transient drops (readyState CONNECTING) and
  // the server replays full history on reconnect. A permanent failure
  // (readyState CLOSED — e.g. the server restarted and the op id is gone)
  // or 5 straight failed retries declares the op lost: unlock the UI and
  // refresh so panels show whatever the op actually did.
  let errCount = 0;
  es.onopen = () => { errCount = 0; };
  es.onerror = () => {
    if (!state.op || state.op.es !== es) return; // stale source after done
    errCount++;
    if (es.readyState === EventSource.CLOSED || errCount >= 5) {
      es.close();
      state.op = null;
      $("pull-btn").disabled = false;
      $("push-btn").disabled = false;
      hideModal();
      closeReviewLane(); // a review's own overlay would otherwise spin forever
      opLine("error: lost connection to operation — repo state refreshed", true);
      refreshAfterOp();
    } else {
      opLine("⟳ reconnecting…");
    }
  };
}


function startSwitch(branch) {
  startOp({ op: "switch", branch }, "switching " + branch);
}


function doCommit() {
  const message = $("commit-msg").value;
  // report instead of a dead click — a silent return here read as
  // "commit is broken" the first time someone hit it mid-merge
  if (!message.trim()) { opLine("write a commit message first", true); return; }
  startOp({ op: "commit", message }, "committing");
}


function doPull() {
  if (opBusy()) return;
  // TUI parity: pull is confirmed up front (it may rewrite the working
  // tree); esc maps to abort via the modal's existing rule.
  const branch = $("repo-branch").textContent || "current branch";
  showLocalConfirm("Pull " + branch + "? This may rewrite the working tree.", ["pull", "abort"], (o) => {
    if (o === "pull") startOp({ op: "pull" }, "pulling");
  });
}


// Pulling a branch you are NOT standing on updates its ref without checking
// it out, so there is nothing to rewrite and nothing to confirm — unlike
// doPull above. The server sends the current branch down the ordinary
// pull-and-stay lane if the two happen to coincide.
function doPullBranch(name) {
  if (opBusy()) return;
  startOp({ op: "pull", branch: name }, "pulling " + name);
}


// Force push. The wire flag does NOT force anything: engine.Push{Force} asks
// the push-force decision (force-with-lease / force / abort) and pushes what
// comes back, so this row's only effect is reaching that modal without
// waiting for a rejection. Hence no client confirm — the modal IS the
// confirm, and its two force options render red via DANGER_OPTIONS.
function doForcePush(name) {
  startOp({ op: "push", branch: name, force: true }, "force-pushing " + name);
}


function doPushBranch(name) {
  if (opBusy()) return;
  startOp({ op: "push", branch: name }, "pushing " + name);
}


function doFetch() {
  if (opBusy()) return;
  startOp({ op: "fetch" }, "fetching");
}


function doPush() {
  if (opBusy()) return;
  startOp({ op: "push" }, "pushing");
}


// doReroot points the server at another root. The whole client state is
// repo-scoped, so a clean reload is the honest reset on success
// (localStorage prefs survive); errors land on the status strip.
async function doReroot(path) {
  closeCommitFilter();
  state.gotoGen++;
  if (opBusy()) return;
  try {
    await postJSON("/api/reroot", { path });
    location.reload();
  } catch (e) {
    opLine("error: " + (e.message || e), true);
  }
}


// toggleSidebar and stageFocused are shared by their keys (b, s/u) and the
// clickable footer chips.
function toggleSidebar() {
  if (state.layout === "diff") return; // no sidebar on the diff screen to toggle
  state.sidebar = !state.sidebar;
  lsSet("gg.sidebar.hidden", state.sidebar ? "0" : "1");
  $("panes").classList.toggle("nosb", !state.sidebar);
  renderCommits(); // list width changed
}


function stageFocused(unstage) {
  if (state.pane !== "files" || state.filesMode !== "status") return;
  const f = state.statusEntries[state.fileCursor];
  if (!f || f.section === "conflicts") return;
  if (!unstage && f.section !== "staged") stage({ paths: [f.path] });
  else if (unstage && f.section === "staged") stage({ paths: [f.path], unstage: true });
}


function doStash() {
  if (state.op || !state.wt) return;
  const message = $("commit-msg").value.trim();
  showLocalConfirm("Stash all working-tree changes?", ["stash", "abort"], (o) => {
    if (o === "stash") startOp({ op: "stash", message }, "stashing");
  });
}


function handleOpEvent(ev) {
  if (ev.type === "progress") {
    opLine("⟳ " + ev.step + (ev.detail ? " " + ev.detail : "") + "…");
  } else if (ev.type === "decision") {
    showModal(ev);
  } else if (ev.type === "resolved") {
    hideModal(); // this decision was answered (another tab, or a replay)
  } else if (ev.type === "done") {
    const op = state.op;
    const kind = op && op.kind;
    // done is terminal: close the source (EventSource would auto-reconnect
    // and replay the history otherwise) and any open modal (covers
    // notify-only decisions whose op already returned).
    if (op) op.es.close();
    state.op = null;
    $("pull-btn").disabled = false;
    $("push-btn").disabled = false;
    hideModal();
    // A run with its own done handler (the review lane) owns the outcome
    // entirely — it changes nothing in the repo, so none of the refreshing
    // below applies to it.
    if (op && op.onDone) {
      op.onDone(ev, kind);
      return;
    }
    if (ev.ok && (kind === "commit" || kind === "stash")) $("commit-msg").value = "";
    if (ev.ok) opLine(ev.summary || "done");
    // commit-graph's !ok-but-changed is a genuine partial failure (the graph
    // write landed, a later step in the chain didn't) — NOT the keep-conflicts
    // shape below, so it must not print the success summary and swallow ev.error.
    else if (!ev.ok && ev.changed && kind === "commit-graph") opLine("error: " + (ev.error || "operation failed"), true);
    // changed && !ok is the engine's deliberate success-with-conflicts shape
    // (a chosen keep-conflicts on merge/rebase/pull/apply-patch/stash-pop):
    // conflicts were left in the tree on purpose, not a failure — the
    // summary already reads as "…has conflicts (left in tree)" etc.
    else if (ev.changed) opLine(ev.summary || "left conflicts in the working tree — resolve them, then commit");
    else opLine("error: " + (ev.error || "operation failed"), true);
    if (kind === "commit-graph") fetchHealth(); // retires the banner group
    if (ev.changed) refreshAfterOp();
    else fetchStatus().then(renderCommits); // a failed switch may still have moved HEAD/stash state
  }
}


async function refreshAfterOp() {
  await Promise.all([loadRepo(), fetchBranches(), fetchStatus()]);
  // an op can change the working tree while its status screen is open
  // (commit empties it) — reconcile instead of showing stale rows
  reconcileStatusView();
  state.rows = [];
  state.cursor = 0;
  await loadCommits(false);
}


// Files edited while the page is in the background are otherwise invisible
// (no polling; ops are the only other refresh trigger) — re-read the
// status when the tab regains focus, throttled, never during a live op.
let lastFocusRefresh = 0;

window.addEventListener("focus", () => {
  if (state.op || Date.now() - lastFocusRefresh < 2000) return;
  lastFocusRefresh = Date.now();
  fetchStatus()
    .then(() => {
      reconcileStatusView();
      renderCommits();
    })
    .catch(() => {});
});


function showModal(ev) {
  $("modal-prompt").textContent = ev.prompt;
  $("modal-options").innerHTML = (ev.options || [])
    .map((o) => `<button data-o="${esc(o)}"${DANGER_OPTIONS.has(o) ? ' class="danger"' : ""}>${esc(o)}</button>`)
    .join("");
  $("modal").dataset.opts = JSON.stringify(ev.options || []);
  pushLayer("modal", $("modal"), {
    onKey: (e) => {
      if (e.key === "Escape") {
        const opts = JSON.parse($("modal").dataset.opts || "[]");
        if (opts.includes("abort")) answerModal("abort"); // the TUI's esc rule
      }
      e.preventDefault();
      return true; // the modal owns the keyboard — even over a focused form field
    },
  });
}


// modalLocalCb, when set, routes the next modal answer to a CLIENT-side
// callback instead of the op decide endpoint — pre-flight confirms (pull)
// reuse the one modal without touching the transport.
let modalLocalCb = null;


function showLocalConfirm(prompt, options, cb) {
  modalLocalCb = cb;
  showModal({ prompt, options });
}


function hideModal() {
  modalLocalCb = null; // a done-driven close must not leak the callback to the next modal
  closeLayer("modal");
}


function openHelp() {
  pushLayer("help", $("help"), {
    onKey: (e) => {
      if (e.key === "Escape" || e.key === "?") closeLayer("help");
      e.preventDefault();
      return true; // help owns the keyboard until closed
    },
  });
}


async function answerModal(option) {
  if (modalLocalCb) {
    const cb = modalLocalCb; // capture first — hideModal clears it
    hideModal();
    cb(option);
    return;
  }
  if (!state.op) return hideModal();
  hideModal();
  try {
    await postJSON("/api/op/" + state.op.id + "/decide", { option });
  } catch (e) {
    opLine("error: " + (e.message || e), true);
  }
}


$("modal-options").addEventListener("click", (e) => {
  const btn = e.target.closest("button");
  if (btn) answerModal(btn.dataset.o);
});

$("commit-btn").addEventListener("click", doCommit);

$("pull-btn").addEventListener("click", doPull);

$("push-btn").addEventListener("click", doPush);

$("stash-btn").addEventListener("click", doStash);

$("conflict-continue").addEventListener("click", () => {
  if (opBusy() || !state.conflict) return;
  startOp({ op: "continue" }, "continue " + state.conflict.op);
});

$("conflict-abort").addEventListener("click", () => {
  if (opBusy() || !state.conflict) return;
  const op = state.conflict.op;
  showLocalConfirm(
    "Abort the paused " + op + "? Conflict resolutions so far are discarded.",
    ["abort " + op, "cancel"],
    (o) => { if (o !== "cancel") startOp({ op: "abort" }, "abort " + op); }
  );
});

async function loadRepo() {
  const repo = await getJSON("/api/repo");
  state.repo = repo; // {name, worktree, branch} — the palette repo-picker filters out the served root
  $("repo-name").textContent = repo.name;
  $("repo-branch").textContent = repo.branch;
  $("repo-worktree").textContent = repo.worktree;
  document.title = "gg web — " + repo.name;
  state.worktree = repo.worktree;
}

export { answerModal, doCommit, doFetch, doForcePush, doPull, doPullBranch, doPush, doPushBranch, doReroot, doStash, followOp, handleOpEvent, hideModal, hideOpLine, lastFocusRefresh, loadRepo, modalLocalCb, opBusy, opLine, opLineTimer, openHelp, parkedRunning, parkedTaskText, refreshAfterOp, showLocalConfirm, showModal, stageFocused, startOp, startSwitch, taskLine, taskRestoreTimer, toggleSidebar };
