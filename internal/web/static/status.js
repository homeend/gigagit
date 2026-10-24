// status.js — part of gg's web client. Split from the original app.js;
// see app.js (the entry module) for the load order.
import { $, ROW_H, esc, getJSON, state } from "./core.js";
import { flatDotSVG } from "./commits.js";
import { sortMode } from "./sortlist.js";

// --- working-tree row + status state ---

// stagedRow: the TUI's ◇ Staged pseudo-row shows while anything is staged.
function stagedRow() {
  return !!(state.wt && state.wt.counts && state.wt.counts.staged > 0);
}

// wtCount is how many pseudo-rows lead the feed: ◇ Working tree, then
// ◇ Staged (the TUI's top→down order).
function wtCount() {
  return state.wt ? (stagedRow() ? 2 : 1) : 0;
}

// pseudoKind names the pseudo-row at a feed index: "wt", "staged" or "".
function pseudoKind(i) {
  if (!state.wt) return "";
  if (i === 0) return "wt";
  if (i === 1 && stagedRow()) return "staged";
  return "";
}


// The Working-tree row renders taller than a commit row (WT_H vs ROW_H);
// the virtualized list accounts for the difference in its spacer and
// scroll math via wtExtra.
const WT_H = 30;

function wtExtra() {
  return state.wt ? WT_H - ROW_H : 0;
}


// shiftCursor keeps the commit cursor on its row when the pseudo-rows above
// it change (◇ Working tree / ◇ Staged appear or go): a cursor on the Staged
// row that vanished lands on the top row.
function shiftCursor(cursor, before, after, wasStaged) {
  if (wasStaged && after < before) return 0;
  const d = after - before;
  return d && cursor >= before ? Math.max(0, cursor + d) : cursor;
}


function applyStatus(st) {
  const before = wtCount();
  const wasStaged = pseudoKind(state.cursor) === "staged";
  state.wt = st.files && st.files.length ? st : null;
  state.cursor = shiftCursor(state.cursor, before, wtCount(), wasStaged);
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
  // Standalone = unmerged paths with NO paused op (a conflicted stash
  // apply): nothing to continue, nothing for abort's sequencer cleanup, and
  // the conflict_complete AI lane finishes a paused op — so those three
  // hide, and the discard-conflicted-changes escape (abort-apply) shows.
  // Per-file resolution works the same either way (click the file).
  const standalone = !!c.standalone;
  // tools… stays: a per-file mergetool works on a stash conflict too (the
  // picker offers the agents only for a paused operation).
  $("conflict-continue").classList.toggle("hidden", standalone);
  $("conflict-abort").classList.toggle("hidden", standalone);
  $("conflict-discard").classList.toggle("hidden", !standalone);
  // innerHTML so the conflicted count can carry its highlight class — c.op
  // and c.desc come off the wire (desc holds branch names), so both esc().
  $("conflict-msg").innerHTML = standalone
    ? `⚠ <span class="conflict-count">${c.conflicted} conflicted</span> — ` +
      esc(c.desc || "a stash apply left conflicts") +
      " · click a file to resolve, or discard"
    : "⏸ " + esc(c.op) + " paused" + (c.desc ? " (" + esc(c.desc) + ")" : "") +
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
  //
  // The sort mode orders files WITHIN a section, never across: the sections
  // are the structure of the list, not one of its orderings. "default" is
  // git's own status order (a stable sort leaves it alone); name↑ is what
  // this list has always done, and stays its starting mode.
  const order = { changes: 0, untracked: 1, conflicts: 2, staged: 3 };
  const mode = sortMode("files");
  const dir = mode === "name-desc" ? -1 : 1;
  es.sort(
    (a, b) =>
      order[a.section] - order[b.section] ||
      (mode === "default" ? 0 : dir * (a.path < b.path ? -1 : a.path > b.path ? 1 : 0))
  );
  state.statusEntries = es;
  // prune marks whose file left the status (committed, discarded, renamed)
  if (state.marked.size) {
    const present = new Set(es.map((f) => f.path));
    for (const p of state.marked) if (!present.has(p)) state.marked.delete(p);
  }
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
    (state.cmarks && state.cmarks.has("@worktree") ? `<span class="wtmark" title="marked for compare">◉ </span>` : "") +
    `<span class="graph">${flatDotSVG("#e0c06c")}</span>` +
    `<span class="subj">Working tree</span>` +
    `<span class="meta">${parts.join(" · ")}</span></div>`
  );
}

// stagedRowHTML is ◇ Staged: HEAD ↔ index, markable (@staged).
function stagedRowHTML(i) {
  const sel = i === state.cursor ? " sel" : "";
  const n = state.wt.counts.staged;
  return (
    `<div class="crow wtstaged${sel}" data-i="${i}">` +
    (state.cmarks && state.cmarks.has("@staged") ? `<span class="wtmark" title="marked for compare">◉ </span>` : "") +
    `<span class="graph">${flatDotSVG("#8fbf7f")}</span>` +
    `<span class="subj">Staged</span>` +
    `<span class="meta">${n} staged</span></div>`
  );
}

export { WT_H, applyStatus, buildStatusEntries, fetchStatus, pseudoKind, renderConflictBar, stagedRow, stagedRowHTML, wtCount, wtExtra, wtRowHTML };
