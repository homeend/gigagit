// keys.js — part of gg's web client. Split from the original app.js;
// see app.js (the entry module) for the load order.
import { $, ROW_H, state } from "./core.js";
import { closeLayer, topLayer } from "./layers.js";
import { WT_H, wtCount, wtExtra } from "./status.js";
import { doCommit, doPull, doPush, manualRefresh, openHelp, refreshAfterOp, stageFocused, toggleSidebar } from "./ops.js";
import { closeCommitFilter, gotoCommitPrompt, openCommit, openCommitFilter, renderCommits, toggleGraphMode } from "./commits.js";
import { drillOut, openFile, renderFiles, toggleMark } from "./files.js";
import { openPalette } from "./palette.js";

// --- focus + keyboard ---

function focusPane() {
  document.querySelectorAll(".pane").forEach((p) => p.classList.remove("focused"));
  $(state.pane === "commits" ? "commits-pane" : "files-pane").classList.add("focused");
}


function moveCursor(delta) {
  if (state.pane === "commits") {
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
  } else {
    const list = state.filesMode === "status" ? state.statusEntries : state.files;
    if (!list.length) return;
    state.fileCursor = Math.max(0, Math.min(list.length - 1, state.fileCursor + delta));
    renderFiles();
  }
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
  // Form fields own the keyboard: without this, typing a commit message
  // triggers j/k navigation and s/u staging. Ctrl/Cmd+Enter commits.
  if (e.target.closest && e.target.closest("input,textarea")) {
    if (e.key === "Enter" && (e.ctrlKey || e.metaKey) && e.target.id === "commit-msg") {
      e.preventDefault();
      doCommit();
    }
    return;
  }
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
  } else if (e.key === "b") {
    toggleSidebar();
  } else if (e.key === "p") {
    doPull();
  } else if (e.key === "P") {
    doPush();
  } else if (e.key === "?") {
    openHelp();
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
    case "filter": openCommitFilter(); break;
    case "stage": stageFocused(false); break;
    case "unstage": stageFocused(true); break;
    case "pull": doPull(); break;
    case "push": doPush(); break;
    case "refresh": manualRefresh(); break;
    case "help": openHelp(); break;
    case "palette": openPalette("cmd"); break;
  }
});

export { focusPane, moveCursor };
