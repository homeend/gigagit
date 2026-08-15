// review.js — part of gg's web client. Split from the original app.js;
// see app.js (the entry module) for the load order.
import { $, esc, getJSON, postJSON, state } from "./core.js";
import { closeLayer, copyText, pushLayer } from "./layers.js";
import { followOp, hideOpLine, opLine, parkedTaskText, refreshAfterOp, taskLine } from "./ops.js";

// --- AI review ---
//
// One overlay walks the whole lane — choose a tool, approve its command, wait
// — because the three are steps of one decision, not three surfaces. The
// command text is never sent from here: the server looks it up in the config
// and resolves it, and this only ever names a TOOL. What is shown in the
// approval step is what the server said it would run.

let rev = null; // {target, branch, tools, sel, phase, tool, label, mode}


function reviewTitle() {
  if (!rev) return "";
  if (rev.mode === "conflict") return "AI resolve — " + (rev.label || rev.op || "");
  return "Review " + (rev.label || (rev.target === "working" ? "working changes" : rev.branch)) + " (AI)";
}


async function startReview(target, branch, sha) {
  if (rev) {
    // Parked: the overlay is not on screen, so a silent refusal here looks
    // like the menu row does nothing. The parked run may be the OTHER mode
    // (an AI resolve), so name it correctly rather than always saying
    // "review".
    if (rev.parked) opLine((rev.mode === "conflict" ? "an agent" : "a review") + " is already running in the background — open the chip to watch or cancel it", true);
    return;
  }
  if (state.op) {
    // One lane, and the server would 409 anyway — but a menu row that does
    // nothing at all reads as broken.
    opLine("review: an operation is already running", true);
    return;
  }
  let info;
  try {
    const q = "?target=" + encodeURIComponent(target) +
      (branch ? "&branch=" + encodeURIComponent(branch) : "") +
      (sha ? "&sha=" + encodeURIComponent(sha) : "");
    info = await getJSON("/api/review/tools" + q);
  } catch (e) {
    opLine("review: " + (e.message || e), true);
    return;
  }
  const tools = info.tools || [];
  if (!tools.length) {
    // Nothing to run and nothing this UI can do about it — say where it comes
    // from rather than opening an empty chooser.
    opLine('review: no review tool configured — add a [[tools.command]] block with category = "review"', true);
    return;
  }
  rev = { mode: "review", target, branch, sha, label: info.label, tools, sel: 0, phase: "choose", tool: null };
  pushLayer("review", $("review"), { onKey: reviewKey });
  if (tools.length === 1) reviewPick(tools[0]);
  else renderReview();
}


// startConflictAI is the conflict-mode twin of startReview above: same
// overlay, same lane, but sourced from the paused-op tools endpoint and
// dispatched to /api/conflict/complete instead of /api/review.
async function startConflictAI() {
  if (rev) {
    if (rev.parked) opLine("an agent is already running in the background — open the chip to watch or cancel it", true);
    return;
  }
  if (state.op) {
    opLine("AI resolve: an operation is already running", true);
    return;
  }
  let info;
  try {
    info = await getJSON("/api/conflict/tools");
  } catch (e) {
    opLine("AI resolve: " + (e.message || e), true);
    return;
  }
  const tools = info.tools || [];
  if (!tools.length) {
    opLine('AI resolve: no headless conflict agent configured — add a [[tools.command]] block with category = "conflict_complete", mode = "capture", frontends = ["web"]', true);
    return;
  }
  rev = { mode: "conflict", op: info.op, label: info.desc || info.op, tools, sel: 0, phase: "choose", tool: null };
  pushLayer("review", $("review"), { onKey: reviewKey });
  // ALWAYS show the chooser, even with a single agent: opening this dialog
  // must never itself start an agent — clicking a row is the confirmation
  // (user feedback: a menu click that instantly launches a run is a surprise).
  renderReview();
}

$("conflict-ai").addEventListener("click", startConflictAI);


function reviewPick(tool) {
  if (!rev) return;
  rev.tool = tool;
  // The server decides whether an approval is needed; this only skips the
  // step it already told us is unnecessary. An out-of-date "approved" here
  // costs one extra prompt, never an unapproved run.
  if (tool.approved) reviewRun(false);
  else {
    rev.phase = "approve";
    renderReview();
  }
}


async function reviewRun(approve) {
  if (!rev) return;
  const { target, branch, sha, tool } = rev;
  const isConflict = rev.mode === "conflict";
  rev.phase = "running";
  renderReview();
  let resp;
  try {
    resp = await postJSON(
      isConflict ? "/api/conflict/complete" : "/api/review",
      isConflict ? { tool: tool.name, approve: !!approve } : { target, branch, sha, tool: tool.name, approve: !!approve }
    );
  } catch (e) {
    // Most often a 403: the server does not consider this command approved,
    // whatever the tools list said. Fall back to the approval step (the
    // resolved command is already in hand from that list) and show why.
    if (!rev) return;
    rev.phase = "approve";
    renderReview();
    opLine((isConflict ? "AI resolve: " : "review: ") + (e.message || e), true);
    return;
  }
  if (!rev) {
    // Cancelled while the start request was in flight. The run EXISTS —
    // simply returning would leave an agent running with nobody following
    // it, holding the single lane until it finished on its own.
    postJSON("/api/op/" + resp.op_id + "/cancel", {}).catch(() => {});
    opLine(isConflict ? "AI resolve cancelled" : "review cancelled");
    return;
  }
  rev.opID = resp.op_id;
  renderReview();
  followOp(resp.op_id,
    (isConflict ? "AI resolving " : "reviewing ") + (rev.label || ""),
    isConflict ? "conflict_complete" : "review",
    reviewDone);
}


function reviewDone(ev, kind) {
  // kind is the op's own kind ("conflict_complete" | "review"), threaded in
  // from handleOpEvent/state.op — NOT derived from rev.mode. reviewCancel()
  // closes the lane (nulling rev) BEFORE its cancel round-trip completes, so
  // the eventual done event for that cancel arrives with rev already gone;
  // deriving the conflict/review distinction from rev here would silently
  // fall back to "review" on every cancelled (or lost-connection) run,
  // skipping the conflict-only refreshAfterOp() below and misreporting the
  // outcome. rev/state.task are read only for DISPLAY (title, label, tool
  // name) below and degrade gracefully once the lane is gone.
  const isConflict = kind === "conflict_complete";
  const title = isConflict
    ? "Resolution overview — " + ((rev && rev.tool && rev.tool.name) || "agent")
    : reviewTitle() || "Review";
  const parked = !!(rev && rev.parked);
  const label = (state.task && state.task.label) || (rev && rev.label) || "";
  closeReviewLane();
  // A conflict run mutates the repo (or leaves it paused) whether it
  // finished, was cancelled, or was lost — reality first, before anything
  // else in this function decides what to tell the user.
  if (isConflict) refreshAfterOp();
  if (parked) {
    // The whole point of parking is not being interrupted, so the result
    // WAITS: the chip goes loud, and opening it is the user's move.
    state.task = {
      kind: isConflict ? "conflict" : "review",
      label,
      status: ev.ok ? "done" : ev.cancelled ? "cancelled" : "failed",
      title,
      path: ev.path,
      report: ev.report,
      error: ev.error,
    };
    if (state.task.status === "cancelled") state.task = null; // nothing to collect
    renderTaskChip(true);
    // The banner belongs to the RUN, so it goes when the run does — the chip
    // is what announces the result, and a second, permanent status line
    // saying the same thing is clutter you have to dismiss by hand.
    // A failure still speaks: an error that vanished silently would be worse
    // than one line of clutter.
    if (ev.ok) hideOpLine();
    else opLine((isConflict ? "AI resolve failed: " : "review failed: ") + (ev.error || "unknown error"), true);
    return;
  }
  if (ev.ok) {
    if (isConflict) {
      // The agent may have finished its own work but left the sequencer
      // paused (a partial resolve, or a multi-round rebase it didn't
      // finish) — that is not a failure, but it is not "done" either.
      if (ev.still_paused) opLine("the agent left the " + (ev.op || "operation") + " paused — finish it manually or run another agent", true);
      else opLine((ev.op || "operation") + " completed");
      if (ev.report) openReport(title, "", ev.report);
      else if (!ev.still_paused) opLine((ev.op || "operation") + " completed — the agent reported no overview");
      return;
    }
    openReport(title, ev.path, ev.report);
    opLine(ev.summary || "review done");
    return;
  }
  if (ev.cancelled) opLine(isConflict ? "AI resolve cancelled" : "review cancelled");
  else opLine((isConflict ? "AI resolve failed: " : "review failed: ") + (ev.error || "unknown error"), true);
}


// --- parking a running review ---
//
// A review can take minutes and there is nothing to watch while it does.
// Parking hides the overlay and leaves the run streaming into the chip. It
// does NOT free the operation lane — the run still holds it, so another op
// is still refused, but every read (commits, diffs, files, branches) stays
// available, which is what "let me get on with something else" needs.

function parkReview() {
  if (!rev || rev.phase !== "running") return;
  rev.parked = true;
  const kind = rev.mode === "conflict" ? "conflict" : "review";
  state.task = { kind, label: rev.label || "", status: "running" };
  closeLayer("review");
  renderTaskChip(false);
  taskLine(parkedTaskText());
}


function unparkReview() {
  if (!rev || !rev.parked) return;
  rev.parked = false;
  state.task = null;
  renderTaskChip(false);
  // The lane is back on screen, so its status line has nothing left to say.
  if ($("op-line").classList.contains("task")) hideOpLine();
  pushLayer("review", $("review"), { onKey: reviewKey });
  renderReview();
}


// renderTaskChip paints state.task. blink is passed only on the transition
// into a finished state, so a re-render (or a second tab's refresh) does not
// restart the animation.
function renderTaskChip(blink) {
  const el = $("task-chip");
  const t = state.task;
  el.classList.remove("running", "ready", "failed", "blink");
  if (!t) {
    el.classList.add("hidden");
    return;
  }
  const noun = t.kind === "conflict" ? "AI resolve" : "review";
  el.classList.remove("hidden");
  if (t.status === "running") {
    el.textContent = "⟳ " + noun + (t.label ? ": " + t.label : "");
    el.title = noun + " running in the background — click to watch or cancel it";
    el.classList.add("running");
    return;
  }
  if (t.status === "done") {
    el.textContent = "✓ " + noun + (t.kind === "conflict" ? " done" : " ready");
    el.title = t.kind === "conflict" ? "click to read the overview" : "click to read the report";
    el.classList.add("ready");
  } else {
    el.textContent = "✗ " + noun + " failed";
    el.title = "click for the error";
    el.classList.add("failed");
  }
  if (blink) el.classList.add("blink");
}


function collectTask() {
  const t = state.task;
  if (!t) return;
  if (t.status === "running") {
    unparkReview();
    return;
  }
  const noun = t.kind === "conflict" ? "AI resolve" : "review";
  state.task = null;
  renderTaskChip(false);
  if (t.status === "done") openReport(t.title || "Review", t.path, t.report);
  else opLine(noun + " failed: " + (t.error || "unknown error"), true);
}


$("task-chip").addEventListener("click", collectTask);

$("review-park").addEventListener("click", parkReview);


// Cancelling mid-run is not a nicety: an agent can take minutes, it holds the
// single op lane while it does, and the tab must not be a hostage to it.
async function reviewCancel() {
  const id = rev && rev.opID;
  closeReviewLane();
  if (!id) return;
  try {
    await postJSON("/api/op/" + id + "/cancel", {});
  } catch (e) {
    opLine("cancel: " + (e.message || e), true);
  }
}


function closeReviewLane() {
  rev = null;
  closeLayer("review");
  // A RUNNING parked task has just stopped being a thing (cancelled, or the
  // stream was lost) — its chip must not linger as a spinner nobody is
  // driving. A finished one is re-set by reviewDone right after this.
  if (state.task && state.task.status === "running") {
    state.task = null;
    renderTaskChip(false);
  }
}


function renderReview() {
  if (!rev) return;
  $("review-title").textContent = reviewTitle();
  const body = $("review-body");
  const hint = $("review-hint");
  const runBtn = $("review-run");
  const cancelBtn = $("review-cancel");
  const parkBtn = $("review-park");
  parkBtn.classList.toggle("hidden", rev.phase !== "running"); // nothing to background before it starts
  if (rev.phase === "choose") {
    const intro =
      rev.mode === "conflict"
        ? `<div class="rnote rintro">These agents are registered for resolving this conflict. Click one to start the resolution — it runs in the background. Nothing runs until you pick; cancel below closes this dialog.</div>`
        : "";
    body.innerHTML =
      intro +
      "<ul>" +
      rev.tools
        .map(
          (t, i) =>
            `<li data-i="${i}"${i === rev.sel ? ' class="sel"' : ""}>${esc(t.name)}` +
            `<span class="detail">${esc(t.command)}</span></li>`
        )
        .join("") +
      "</ul>";
    hint.textContent = rev.mode === "conflict" ? "click an agent to start · esc cancels" : "choose a review tool · enter runs · esc cancels";
    // Conflict chooser: clicking a row IS the action, so a separate "run"
    // button beside it only contradicts the copy (user report) — cancel is
    // the sole bottom button. (Enter still runs the ↑/↓-selected row for
    // keyboard users; the review chooser keeps its run button.)
    runBtn.classList.toggle("hidden", rev.mode === "conflict");
    runBtn.textContent = "run";
    cancelBtn.textContent = "cancel";
    return;
  }
  if (rev.phase === "approve") {
    body.innerHTML =
      `<div class="rcmd">${esc(rev.tool.command)}</div>` +
      `<div class="rnote">This runs on your machine with your permissions. Approval is remembered for this repo until the command text changes.</div>`;
    hint.textContent = "run this command?";
    runBtn.classList.remove("hidden");
    runBtn.textContent = "run";
    cancelBtn.textContent = "cancel";
    return;
  }
  body.innerHTML =
    `<div class="rnote">${esc(rev.tool ? rev.tool.name : "")} ` +
    (rev.mode === "conflict"
      ? "is resolving the conflicts and completing the operation — this can take a few minutes."
      : "is reading the diff — this can take a few minutes.") +
    ` You can put it in the background and carry on reading the repo; the chip in the top bar lights up when it finishes.</div>`;
  hint.textContent = rev.mode === "conflict" ? "⟳ resolving… · esc backgrounds it" : "⟳ reviewing… · esc backgrounds it";
  runBtn.classList.add("hidden"); // nothing to run twice
  cancelBtn.textContent = "cancel the run";
}


function reviewKey(e) {
  if (!rev) return false;
  if (e.key === "Escape") {
    // esc puts a live run in the BACKGROUND rather than killing it: esc means
    // "off my screen" everywhere else in this UI, and destroying minutes of
    // agent work on the key people press reflexively would be a trap.
    // Cancelling stays an explicit, labelled button.
    if (rev.phase === "running") parkReview();
    else closeReviewLane();
    e.preventDefault();
    return true;
  }
  if (rev.phase === "choose" && (e.key === "ArrowDown" || e.key === "ArrowUp")) {
    rev.sel = Math.min(rev.tools.length - 1, Math.max(0, rev.sel + (e.key === "ArrowDown" ? 1 : -1)));
    renderReview();
    e.preventDefault();
    return true;
  }
  if (e.key === "Enter") {
    reviewConfirm();
    e.preventDefault();
    return true;
  }
  return true; // the lane owns the keyboard while it is open
}


function reviewConfirm() {
  if (!rev) return;
  if (rev.phase === "choose") reviewPick(rev.tools[rev.sel]);
  else if (rev.phase === "approve") reviewRun(true);
}


$("review-run").addEventListener("click", reviewConfirm);

$("review-cancel").addEventListener("click", () => {
  if (rev && rev.phase === "running") reviewCancel();
  else closeReviewLane();
});

$("review-body").addEventListener("click", (e) => {
  const li = e.target.closest("li[data-i]");
  if (li && rev && rev.phase === "choose") reviewPick(rev.tools[Number(li.dataset.i)]);
});

$("review").addEventListener("click", (e) => {
  // Backdrop dismisses. While a run is live that means BACKGROUND it, never
  // kill it — a stray click outside the box must not destroy an agent's work.
  if (e.target.id !== "review" || !rev) return;
  if (rev.phase === "running") parkReview();
  else closeReviewLane();
});


// The report viewer: plain text, deliberately not rendered as markdown — a
// review is prose to read, and a parser here would be a dependency and a
// rendering bug surface for no gain.
function openReport(title, path, content) {
  $("report-title").textContent = title;
  $("report-body").textContent = content || "";
  $("report-path").textContent = path || "";
  $("report-path").title = path || "";
  $("report-body").scrollTop = 0;
  pushLayer("report", $("report"));
}


$("report-close").addEventListener("click", () => closeLayer("report"));

$("report-copy").addEventListener("click", () => copyText($("report-body").textContent, "the report"));
export { closeReviewLane, collectTask, openReport, parkReview, renderReview, renderTaskChip, rev, reviewCancel, reviewConfirm, reviewDone, reviewKey, reviewPick, reviewRun, reviewTitle, startConflictAI, startReview, unparkReview };
