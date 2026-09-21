// keys.js — part of gg's web client. Split from the original app.js;
// see app.js (the entry module) for the load order.
import { $, ROW_H, state } from "./core.js";
import { closeLayer, topLayer } from "./layers.js";
import { WT_H, wtCount, wtExtra } from "./status.js";
import { doCommit, doPull, doPush, manualRefresh, openHelp, refreshAfterOp, stageFocused, toggleSidebar } from "./ops.js";
import { closeCommitFilter, gotoCommitPrompt, openCommit, openCommitFilter, renderCommits, toggleGraphMode } from "./commits.js";
import { symKey } from "./symcompare.js";
import { addNotePrompt, cycleFilesSort, cycleTextMode, diffScrollKey, diffSearchBar, diffSearchKey, drillOut, editNotePrompt, notesArmed, openFile, renderFiles, replyNotePrompt, stepNote, toggleDiffView, toggleMark, toggleNoteCollapsed, toggleNotesAgent, collapseNearestNote } from "./files.js";
import { collapseCurrent, toggleAllCollapsed, toggleStacked } from "./stackview.js";
import { toast } from "./toast.js";
import { openPalette } from "./palette.js";
import { branchFilterKey } from "./branchfilter.js";

// --- focus + keyboard ---

function focusPane() {
  document.querySelectorAll(".pane").forEach((p) => p.classList.remove("focused"));
  $(state.pane === "commits" ? "commits-pane" : "files-pane").classList.add("focused");
}


function moveCursor(delta) {
  if (state.pane === "commits") {
    stepCommitCursor(delta);
  } else {
    const list = state.filesMode === "status" ? state.statusEntries : state.files;
    if (!list.length) return;
    state.fileCursor = Math.max(0, Math.min(list.length - 1, state.fileCursor + delta));
    // In a stack j/k walk the sections: the cursor's file scrolls into view.
    if (state.stack && state.layout === "diff") return openFile(state.fileCursor);
    renderFiles();
  }
}


// stepCommitCursor moves the commit cursor by delta, scrolls it into view
// and re-renders the virtual window. delta 0 is the "bring the list back"
// call: the commits pane is display:none behind a diff, which drops its
// scroll position AND makes any render there (a live refresh, r, a notes
// count) size the window for a zero-height pane — ten rows. Returning from
// the diff stage must redo both, whichever pane holds the keyboard.
function stepCommitCursor(delta) {
  if (state.cfilter) {
    // Filtered mode: the spacer/window are sized to matches.length + 1
    // (the hint row), not the full feed, so navigation and scroll math
    // must operate on POSITION WITHIN THE MATCH LIST rather than the
    // full-feed display index state.cursor otherwise holds.
    const m = state.cfilter.matches;
    if (!m.length) return;
    let pos = m.findIndex((idx) => idx + wtCount() === state.cursor);
    if (pos === -1) {
      // Cursor isn't on a match (filter just narrowed, or a fresh open):
      // snap to the nearest match at or after it, else the last match.
      pos = m.findIndex((idx) => idx + wtCount() >= state.cursor);
      if (pos === -1) pos = m.length - 1;
    }
    pos = Math.max(0, Math.min(m.length - 1, pos + delta));
    state.cursor = m[pos] + wtCount();
    const scroll = $("commits-scroll");
    const top = pos * ROW_H;
    if (top < scroll.scrollTop) scroll.scrollTop = top;
    else if (top + ROW_H > scroll.scrollTop + scroll.clientHeight)
      scroll.scrollTop = top + ROW_H - scroll.clientHeight;
    renderCommits();
    return;
  }
  const total = state.rows.length + wtCount();
  if (!total) return;
  state.cursor = Math.max(0, Math.min(total - 1, state.cursor + delta));
  const scroll = $("commits-scroll");
  const top = state.cursor * ROW_H + (state.cursor > 0 ? wtExtra() : 0);
  const h = state.cursor === 0 && state.wt ? WT_H : ROW_H;
  if (top < scroll.scrollTop) scroll.scrollTop = top;
  else if (top + h > scroll.scrollTop + scroll.clientHeight)
    scroll.scrollTop = top + h - scroll.clientHeight;
  renderCommits();
}


// noteKey matches one of the review-note keys pressed BARE, in a diff that is
// note-addressable (notesArmed: never a comparison — its old side belongs to
// no storable address).
// The modifier check is the load-bearing half: `c` and `a` are ctrl+c (copy a
// selected diff line — the commonest thing anyone does in a diff viewer) and
// ctrl+a (select all), and this handler sees those before the browser acts.
function noteKey(e, key) {
  return e.key === key && !e.ctrlKey && !e.metaKey && !e.altKey && notesArmed();
}


document.addEventListener("keydown", (e) => {
  const top = topLayer();
  if (top) {
    if (top.onKey && top.onKey(e)) return;
    if (e.key === "Escape") closeLayer(top.id); // default close for layers without onKey
    return; // a non-empty stack owns the keyboard
  }
  // Palette shortcut: after layer routing (an open layer keeps the keyboard),
  // before the form-field guard (ctrl+k must work from the commit box).
  if ((e.ctrlKey || e.metaKey) && (e.key === "k" || e.key === "p")) {
    e.preventDefault(); // ctrl+p would open the browser print dialog
    openPalette("cmd");
    return;
  }
  // alt+1…5 / alt+shift+1…5: branch-filter slots. Not inside inputs — a
  // digit typed into the commit box must stay a digit.
  if (!(e.target.closest && e.target.closest("input,textarea")) && branchFilterKey(e)) return;
  // Form fields own the keyboard: without this, typing a commit message
  // triggers j/k navigation and s/u staging. Ctrl/Cmd+Enter commits.
  if (e.target.closest && e.target.closest("input,textarea")) {
    if (e.key === "Enter" && (e.ctrlKey || e.metaKey) && e.target.id === "commit-msg") {
      e.preventDefault();
      doCommit();
    }
    return;
  }
  // The diff layout's in-view search has first refusal: / and @ open it
  // (the commits pane is off-screen there, so / has no filter to open), ] [
  // step it, and esc clears a kept query BEFORE it would leave the diff.
  if (diffSearchKey(e)) return;
  // The symmetric comparison view's own keys (v, x, 1–4), only while one is up.
  if (symKey(e)) return;
  // In the diff layout the arrows and page keys scroll the DIFF (the TUI's
  // ↑↓ there), wherever the mouse left the focus; j/k still walk the files.
  if (diffScrollKey(e)) return;
  if (e.key === "j" || e.key === "ArrowDown") {
    e.preventDefault();
    moveCursor(1);
  } else if (e.key === "k" || e.key === "ArrowUp") {
    e.preventDefault();
    moveCursor(-1);
  } else if (e.key === "Enter") {
    if (state.pane === "commits") {
      // A zero-match filter leaves state.cursor pointing at an invisible
      // row (or the hint row) — nothing there to open.
      if (!(state.cfilter && state.cfilter.matches.length === 0)) openCommit(state.cursor);
    } else if (state.filesMode === "status" ? state.statusEntries.length : state.files.length) openFile(state.fileCursor);
  } else if (e.key === "Escape") {
    // The filter bar can be open with its input unfocused (a click landed
    // back on a commit row) — drillOut() no-ops in list layout, so Escape
    // would otherwise do nothing at all. Clearing the filter takes priority
    // over the layered close in list layout; diff/files behavior (drillOut)
    // is unchanged.
    if (state.layout === "list" && (!$("cfilter").classList.contains("hidden") || state.cfilter)) {
      closeCommitFilter();
    } else {
      drillOut();
    }
  } else if (e.key === "g") {
    toggleGraphMode();
  } else if (e.key === "t") {
    toggleSidebar(); // t, not b: the sidebar is the tabs/sections column
  } else if (e.key === "p") {
    doPull();
  } else if (e.key === "P") {
    doPush();
  } else if (e.key === "?") {
    openHelp();
  } else if (e.key === "A" && !e.ctrlKey && !e.metaKey && !e.altKey) {
    // the TUI's A on the Pull requests tab: search PRs of any state. prs.js
    // owns the field (no forge → no field → the key does nothing).
    if (window.__ggFocusPRSearch && window.__ggFocusPRSearch()) e.preventDefault();
  } else if (e.key === "r") {
    manualRefresh(); // the TUI's r: says it is working, and starts the list clean
  } else if (e.key === "s" || e.key === "u") {
    stageFocused(e.key === "u");
  } else if (e.key === "m") {
    // mark the focused status file for a batch action (ctx-menu rows), then
    // advance so a run of files marks with a run of m presses
    if (state.filesMode === "status" && state.pane === "files") {
      const f = state.statusEntries[state.fileCursor];
      if (f) { toggleMark(f.path); moveCursor(1); }
    }
  } else if (e.key === "f" && !e.ctrlKey && !e.metaKey && !e.altKey) {
    toggleDiffView(); // the TUI's f: changed lines only ↔ full file
  } else if (e.key === "w" && !e.ctrlKey && !e.metaKey && !e.altKey) {
    cycleTextMode(); // the TUI's ctrl+w (a browser owns ctrl+w itself): scroll → wrap → cutoff
  } else if (e.key === "o") {
    // the TUI's o: cycle the focused list's display order. The working-tree
    // file list is the one the keyboard can reach — the sidebar's lists cycle
    // from the chips in their own headers.
    if (state.pane === "files" && state.filesMode === "status") cycleFilesSort();
  } else if (noteKey(e, "c")) {
    e.preventDefault(); // the key must not land in the prompt input that opens
    // Review notes (the TUI's c/E/R/a/}/{). The web has no line cursor: `c`
    // anchors on the clicked diff row (tr.cur), else the first changed row,
    // and E/R act on the nearest note at or above it.
    addNotePrompt();
  } else if (noteKey(e, "E")) {
    e.preventDefault(); // the key must not land in the prompt input that opens
    editNotePrompt();
  } else if (noteKey(e, "R")) {
    e.preventDefault(); // the key must not land in the prompt input that opens
    replyNotePrompt();
  } else if (noteKey(e, "a")) {
    toggleNotesAgent();
  } else if (noteKey(e, "z")) {
    collapseNearestNote(); // the TUI's o: fold the note you are on (o is the web's sort key)
  } else if (noteKey(e, "Z")) {
    toggleNoteCollapsed(null); // the TUI's O: every thread of this file
  } else if (noteKey(e, "}") || noteKey(e, "{")) {
    stepNote(e.key === "}" ? 1 : -1);
  } else if (e.key === "S" && !e.ctrlKey && !e.metaKey && !e.altKey) {
    toggleStacked(); // every file in one scroll ↔ one file at a time
  } else if (e.key === "-" && state.stack) {
    e.preventDefault();
    collapseCurrent();
  } else if (e.key === "_" && state.stack) {
    e.preventDefault();
    toggleAllCollapsed();
  } else if (e.key === "/" && state.stack) {
    // in-view search reads ONE diff; the stack has many (a follow-up plan)
    e.preventDefault();
    toast("search works in the single-file view — S switches");
  } else if (e.key === "/") {
    e.preventDefault(); // the browser's quick-find would grab it
    openCommitFilter();
  } else if (e.key === "#") {
    gotoCommitPrompt();
  }
});


// The footer chips execute their key's action on click.
$("foot").addEventListener("click", (e) => {
  const btn = e.target.closest("button[data-act]");
  if (!btn) return;
  switch (btn.dataset.act) {
    case "back": drillOut(); break;
    case "sidebar": toggleSidebar(); break;
    case "graph": toggleGraphMode(); break;
    case "filter": if (state.layout === "diff") diffSearchBar.open(false); else openCommitFilter(); break;
    case "stage": stageFocused(false); break;
    case "unstage": stageFocused(true); break;
    case "sort": if (state.pane === "files" && state.filesMode === "status") cycleFilesSort(); break;
    case "diffview": toggleDiffView(); break;
    case "textmode": cycleTextMode(); break;
    case "stacked": toggleStacked(); break;
    case "pull": doPull(); break;
    case "push": doPush(); break;
    case "refresh": manualRefresh(); break;
    case "help": openHelp(); break;
    case "palette": openPalette("cmd"); break;
  }
});

export { focusPane, moveCursor, stepCommitCursor };
