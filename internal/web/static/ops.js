// ops.js — part of gg's web client. Split from the original app.js;
// see app.js (the entry module) for the load order.
import { $, DANGER_OPTIONS, esc, getJSON, lsSet, postJSON, state } from "./core.js";
import { closeLayer, closePrompt, openPrompt, pushLayer } from "./layers.js";
import { openPrefixPicker } from "./prefixes.js";
import { fetchStatus, wtCount } from "./status.js";
import { saveUI } from "./uistate.js";
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
  // git will not check out a branch that is already checked out in another
  // worktree, and answering that with an error is a dead end — the TUI asks
  // whether to GO THERE instead, and so does this. Re-rooting the server at
  // that worktree is what "switch" means here; doReroot carries the
  // cross-environment repair offer, the analog of the TUI's guardedReRoot.
  const wt = (state.worktrees || []).find((w) => w.branch === branch && w.path !== state.worktree);
  if (wt) {
    showLocalConfirm(branch + " is checked out in another worktree:\n" + wt.path, ["go to worktree", "cancel"], (opt) => {
      if (opt === "go to worktree") doReroot(wt.path);
    });
    return;
  }
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


// Pushing the CURRENT branch asks about tags sitting on its tip that the
// remote does not have — the TUI's P, option for option. The check is bounded
// server-side (5s) and reports nothing on a timeout, so an unreachable remote
// never turns a push into a wait; a branch with no tip tags makes no network
// call at all. Pushing a NAMED branch (the branch menu) skips this, as the TUI
// does — that lane is not the current branch and has no tip-tag prompt.
async function doPush() {
  if (opBusy()) return;
  let tags = [];
  try {
    const got = await getJSON("/api/push-tag-check");
    tags = got.tags || [];
  } catch {
    tags = []; // the check is an offer, never a gate
  }
  if (!tags.length) {
    startOp({ op: "push" }, "pushing");
    return;
  }
  const branch = (state.repo && state.repo.branch) || "the current branch";
  const prompt =
    tags.length === 1
      ? "Push " + branch + ": branch tip has tag " + tags[0] + " not on the remote. Push too?"
      : "Push " + branch + ": branch tip has tags " + tags.join(", ") + " not on the remote. Push too?";
  showLocalConfirm(prompt, ["Push branch + tags", "Push branch only", "Cancel"], (opt) => {
    if (opt === "Push branch + tags") startOp({ op: "push", tags }, "pushing " + branch + " + tags");
    else if (opt === "Push branch only") startOp({ op: "push" }, "pushing");
  });
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
    // Cross-environment worktree (WSL path seen from Windows gg, or vice
    // versa): the server answers 409 repairable and waits for an explicit
    // confirm — repairing rebinds the link records, so the worktree stops
    // working in the other environment until repaired back.
    if (e.data && e.data.repairable) {
      showLocalConfirm(
        "This worktree is linked for another environment. Repair it for this one? It will stop working there until repaired back.",
        ["repair", "cancel"],
        (opt) => {
          if (opt !== "repair") return;
          postJSON("/api/reroot", { path, repair: true })
            .then(() => location.reload())
            .catch((err) => opLine("error: " + (err.message || err), true));
        }
      );
      return;
    }
    opLine("error: " + (e.message || e), true);
  }
}


// toggleSidebar and stageFocused are shared by their keys (t, s/u) and the
// clickable footer chips.
//
// state.sidebar is VISIBILITY and applySidebarHidden takes HIDDENNESS, so the
// argument is state.sidebar itself: "hide it if it is currently shown". It
// used to read `!state.sidebar`, which asserts the state the sidebar is
// already in — the toggle became idempotent, and the key, the footer chip and
// the ☰ row all did nothing at all.
function toggleSidebar() {
  if (state.layout === "diff") return; // no sidebar on the diff screen to toggle
  applySidebarHidden(state.sidebar);
  // Both writes read state.sidebar AFTER the flip, so they persist the state
  // the sidebar is now in.
  lsSet("gg.sidebar.hidden", state.sidebar ? "0" : "1"); // same-session cache
  saveUI({ sidebar_hidden: !state.sidebar }); // the copy that survives a restart
}


// applySidebarHidden is the shared "put the sidebar in this state" step, used
// by the toggle and by the layout restored from the server at boot.
function applySidebarHidden(hidden) {
  state.sidebar = !hidden;
  $("panes").classList.toggle("nosb", hidden);
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


// manualRefresh is the ☰ menu's (and r's) reload: the TUI's `r`, which says
// out loud that it is working and starts the commit list CLEAN. Without the
// notice a refresh over unchanged data looks like a dead button; without the
// clean start it cannot recover a deep tail that a rewrite made stale.
async function manualRefresh() {
  if (state.op) return; // an op owns the data; its own refresh follows
  opLine("⏳ reloading…");
  try {
    await refreshAfterOp(true);
    opLine("reloaded");
  } catch (e) {
    opLine("reload failed: " + (e.message || e), true);
  }
}


async function refreshAfterOp(hardFeed) {
  // Which commit the cursor sits on, read BEFORE anything refreshes: the
  // status fetch below can add or remove the working-tree row, and that
  // shifts every display index by one — reading it afterwards anchors to the
  // neighbouring commit.
  const at = state.rows[state.cursor - wtCount()];
  const keep = at && at.hash;
  await Promise.all([loadRepo(), fetchBranches(), fetchStatus()]);
  // an op can change the working tree while its status screen is open
  // (commit empties it) — reconcile instead of showing stale rows
  reconcileStatusView();
  // The reload RECONCILES server-side (new commits prepend, a vanished tip is
  // trimmed, everything paged in stays), so the list is NOT cleared here: the
  // old rows stay on screen while the request is in flight — which is also
  // what keeps the scroll position, since a zero-height list would reset it —
  // and the cursor is re-anchored to the SAME commit afterwards rather than
  // snapping to the top.
  await loadCommits(false, !!hardFeed);
  const last = state.rows.length + wtCount() - 1;
  const i = keep ? state.rows.findIndex((r) => r.hash === keep) : -1;
  if (i >= 0) state.cursor = i + wtCount();
  else if (state.cursor > last) state.cursor = Math.max(0, last);
  renderCommits();
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

$("conflict-discard").addEventListener("click", () => {
  if (opBusy() || !state.conflict) return;
  showLocalConfirm(
    "Discard the conflicted application? Conflicted files return to HEAD; other local changes and the stash are kept, so the apply can be retried.",
    ["discard", "cancel"],
    (o) => { if (o !== "cancel") startOp({ op: "abort-apply" }, "discarding conflicted apply"); }
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


// openCreateBranchPrompt: the one create-branch dialog — ☰/palette start from
// HEAD, the branch menu passes its branch, the commits menu passes the
// selected commit's sha. "use prefix…" mirrors the TUI popup's ctrl+p: pick a
// saved prefix, fill its <user:…> labels, and the resolved name seeds the
// input — still editable; plain typing needs no prefix at all. A completed
// pick rides along on the submit as the prefix identity, so its <seq>
// counters advance only when the create succeeds; canceling the picker
// restores the prompt with whatever was typed.
//
// It lives HERE rather than in sidebar.js because the commits panel opens it
// too, and commits.js cannot import from sidebar.js (sidebar imports commits).
function openCreateBranchPrompt(start, seed, label) {
  const at = label || start;
  openPrompt({
    title: at ? "New branch, starting at " + at + ":" : "New branch, starting at the current HEAD:",
    placeholder: "branch name",
    value: seed ? seed.value : "",
    extra: {
      label: "use prefix…",
      run: (typed) => {
        closePrompt();
        openPrefixPicker((resolved, p) => {
          if (resolved == null) {
            openCreateBranchPrompt(start, typed ? { value: typed } : undefined, label);
            return;
          }
          openCreateBranchPrompt(start, { value: resolved, prefix: p }, label);
        });
      },
    },
    onSubmit: (name) => {
      const body = { op: "create-branch", name };
      if (start) body.branch = start;
      if (seed && seed.prefix) {
        body.prefix_id = seed.prefix.id;
        body.prefix_scope = seed.prefix.scope;
      }
      startOp(body, "creating " + name);
    },
  });
}


export { applySidebarHidden, answerModal, manualRefresh, doCommit, doFetch, doForcePush, doPull, doPullBranch, doPush, doPushBranch, doReroot, doStash, followOp, handleOpEvent, hideModal, hideOpLine, lastFocusRefresh, loadRepo, modalLocalCb, opBusy, opLine, opLineTimer, openCreateBranchPrompt, openHelp, parkedRunning, parkedTaskText, refreshAfterOp, showLocalConfirm, showModal, stageFocused, startOp, startSwitch, taskLine, taskRestoreTimer, toggleSidebar };
