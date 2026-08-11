// status.js — part of gg's web client. Split from the original app.js;
// see app.js (the entry module) for the load order.
import { $, ROW_H, esc, getJSON, state } from "./core.js";
import { flatDotSVG } from "./commits.js";

// --- working-tree row + status state ---

function wtCount() {
  return state.wt ? 1 : 0;
}


// The Working-tree row renders taller than a commit row (WT_H vs ROW_H);
// the virtualized list accounts for the difference in its spacer and
// scroll math via wtExtra.
const WT_H = 30;

function wtExtra() {
  return state.wt ? WT_H - ROW_H : 0;
}


function applyStatus(st) {
  state.wt = st.files && st.files.length ? st : null;
  state.conflict = st.conflict || null;
  buildStatusEntries();
  renderConflictBar();
}


// The banner shows whenever a sequencer op is paused — including with zero
// conflicted files (resolved by hand, never continued): that is exactly when
// Continue lights up. Never leave the user in a paused op with no way out.
function renderConflictBar() {
  const bar = $("conflict-bar"), c = state.conflict;
  if (!c) { bar.classList.add("hidden"); return; }
  bar.classList.remove("hidden");
  // innerHTML so the conflicted count can carry its highlight class — c.op
  // and c.desc come off the wire (desc holds branch names), so both esc().
  $("conflict-msg").innerHTML =
    "⏸ " + esc(c.op) + " paused" + (c.desc ? " (" + esc(c.desc) + ")" : "") +
    (c.conflicted
      ? ` — <span class="conflict-count">${c.conflicted} conflicted</span>`
      : " — all conflicts resolved");
  $("conflict-continue").disabled = !!c.conflicted;
}


async function fetchStatus() {
  applyStatus(await getJSON("/api/status"));
}


// A partially-staged file appears twice: once under Changes (stage control),
// once under Staged (unstage control) — the git-status model.
function buildStatusEntries() {
  const es = [];
  for (const f of state.wt ? state.wt.files : []) {
    if (f.kind === "conflicted") es.push({ ...f, section: "conflicts" });
    else if (f.kind === "untracked") es.push({ ...f, section: "untracked" });
    else {
      if (f.staged !== ".") es.push({ ...f, section: "staged" });
      if (f.unstaged !== ".") es.push({ ...f, section: "changes" });
    }
  }
  // Staged sits LAST: everything above it still wants attention, and what is
  // already staged does not. Staging a hunk otherwise made the file jump
  // upward into a section you were done with, pushing the rest of the work
  // down — the list appeared to reorder itself under the cursor.
  const order = { changes: 0, untracked: 1, conflicts: 2, staged: 3 };
  es.sort((a, b) => order[a.section] - order[b.section] || (a.path < b.path ? -1 : a.path > b.path ? 1 : 0));
  state.statusEntries = es;
}


function wtRowHTML(i) {
  const sel = i === state.cursor ? " sel" : "";
  const c = state.wt.counts;
  // parts are pre-escaped HTML fragments (counts are numbers, labels are
  // literals) so the conflicted one can carry its highlight class.
  const parts = [];
  if (c.staged) parts.push(c.staged + " staged");
  if (c.unstaged) parts.push(c.unstaged + " changed");
  if (c.untracked) parts.push(c.untracked + " untracked");
  if (c.conflicted) parts.push(`<span class="conflict-count">${c.conflicted} conflicted</span>`);
  return (
    `<div class="crow wt${sel}" data-i="${i}">` +
    `<span class="graph">${flatDotSVG("#e0c06c")}</span>` +
    `<span class="subj">Working tree</span>` +
    `<span class="meta">${parts.join(" · ")}</span></div>`
  );
}

export { WT_H, applyStatus, buildStatusEntries, fetchStatus, renderConflictBar, wtCount, wtExtra, wtRowHTML };
