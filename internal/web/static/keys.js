// keys.js — part of gg's web client. Split from the original app.js;
// see app.js (the entry module) for the load order.
import { $, ROW_H, state } from "./core.js";
import { closeLayer, layerDefaultKey, topLayer } from "./layers.js";
import { WT_H, wtCount, wtExtra } from "./status.js";
import { doCommit, doPull, doPush, manualRefresh, openHelp, refreshAfterOp, stageFocused, toggleSidebar } from "./ops.js";
import { closeCommitFilter, gotoCommitPrompt, markRunStep, openCommitHighlight, stepHighlight, openCommit, openCommitFilter, renderCommits, toggleAllFiles, toggleGraphMode } from "./commits.js";
import { symKey } from "./symcompare.js";
import { openSelectedReview, reviewActive, showReviewSummary, stepCommitReviews, stepReviewFile } from "./reviews.js";
import { closeFileFilter, openFileFilter, shownFileIdx, allFilesOK, gotoDiffKey, addNotePrompt, rangeKey, clearDiffRange, clearRowSelection, cycleFilesSort, cycleImageLayout, flipImage, cycleTextMode, diffScrollKey, diffSearchBar, diffSearchKey, drillOut, editNotePrompt, notesArmed, openFile, renderFiles, replyNotePrompt, stepChange, stepNote, toggleDiffView, toggleMark, toggleNoteCollapsed, toggleNotesAgent, collapseNearestNote } from "./files.js";
import { activeDiff, rangeDiff, collapseCurrent, toggleAllCollapsed, toggleStacked } from "./stackview.js";
import { commitMessageInEditor, diffEditOpen } from "./editor.js";
import { toast } from "./toast.js";
import { openPalette } from "./palette.js";
import { isSwitcherKey, openSwitcher } from "./openfiles.js";
import { branchFilterKey } from "./branchfilter.js";
import { textTemplatesKey } from "./texttemplates.js";
import { walkKey } from "./console.js";

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
    // The rows the list's / filter shows: the cursor never rests on a hidden one.
    const vis = shownFileIdx(list);
    // The review view's ≡ Summary sits above its first file.
    if (reviewActive()) {
      const rv = state.review;
      if (rv.onSummary && delta > 0 && vis.length) {
        rv.onSummary = false;
        state.fileCursor = vis[0];
      } else if (!rv.onSummary && delta < 0 && state.fileCursor === (vis.length ? vis[0] : 0)) {
        if (state.layout === "diff") return showReviewSummary();
        rv.onSummary = true;
        return renderFiles();
      } else if (rv.onSummary) return;
      else state.fileCursor = visibleStep(vis, state.fileCursor, delta);
      if (state.stack && state.layout === "diff") return openFile(state.fileCursor);
      return renderFiles();
    }
    // A commit's Reviews rows sit above its first file.
    if (state.layout !== "diff" && stepCommitReviews(delta)) return;
    if (!vis.length) return;
    state.fileCursor = visibleStep(vis, state.fileCursor, delta);
    // In a stack j/k walk the sections: the cursor's file scrolls into view.
    if (state.stack && state.layout === "diff") return openFile(state.fileCursor);
    renderFiles();
  }
}


// --- list paging (pure; guarded against Go) ---
// pageTarget is where PgDn/PgUp/Home/End put a list cursor at cur in a list
// of n rows showing page rows a screen (the TUI's pgdown/pgup/home/end on a
// list); -1 for a key that is none of them. An empty list stays put.
function pageTarget(key, cur, n, page) {
  switch (key) {
    case "PageDown": return n ? Math.min(n - 1, cur + page) : cur;
    case "PageUp": return Math.max(0, cur - page);
    case "Home": return 0;
    case "End": return n ? n - 1 : cur;
  }
  return -1;
}

// visibleStep moves a cursor delta rows over vis, the ascending indices a
// filter shows; a cursor on a hidden row first snaps (snapVisible).
function visibleStep(vis, cur, delta) {
  if (!vis.length) return cur;
  const pos = vis.indexOf(snapVisible(vis, cur));
  return vis[Math.max(0, Math.min(vis.length - 1, pos + delta))];
}

// snapVisible is the TUI's filter rule for the cursor: it stays on a shown
// row, else goes to the nearest one at or after it, else to the last one.
function snapVisible(vis, cur) {
  if (!vis.length || vis.includes(cur)) return cur;
  const after = vis.find((i) => i >= cur);
  return after === undefined ? vis[vis.length - 1] : after;
}
// --- end list paging ---

// pageKey is PgDn/PgUp/Home/End on the focused list, the TUI's keys on its
// panels (a page = a quarter of the list's rows, its pageStep): the commit
// list or the file list moves its cursor — the browser
// would scroll whichever frame the mouse last touched instead. The commit
// list takes the move as a delta (stepCommitCursor clamps, also inside a
// filter's matches) and its end loads the next batch through renderCommits.
// The file list lands exactly (Home stops on the first file, never walks on
// into a commit's Reviews rows above it) and keeps the cursor on screen.
function pageKey(e) {
  if (e.ctrlKey || e.metaKey || e.altKey || e.shiftKey) return false;
  if (state.pane === "commits") {
    const page = commitPageRows();
    const delta = { PageDown: page, PageUp: -page, Home: -Infinity, End: Infinity }[e.key];
    if (delta === undefined) return false;
    e.preventDefault();
    stepCommitCursor(delta);
    return true;
  }
  const vis = shownFileIdx(state.filesMode === "status" ? state.statusEntries : state.files);
  const pos = vis.indexOf(snapVisible(vis, state.fileCursor));
  const to = pageTarget(e.key, pos, vis.length, filePageRows());
  if (to < 0) return false;
  e.preventDefault();
  if (vis.length && to !== pos) moveCursor(to - pos);
  revealFileCursor();
  return true;
}

// commitPageRows is how far a page moves the commit cursor: a quarter of the
// rows the list shows (the TUI's pageStep).
function commitPageRows() {
  return Math.max(1, Math.floor($("commits-scroll").clientHeight / ROW_H / 4));
}

// filterFieldKey: ↑/↓ and pgup/pgdn typed in a list's / filter field move
// that list's cursor (the TUI's filter typing); home/end stay the field's
// caret keys, every other key its text.
function filterFieldKey(e) {
  if (e.ctrlKey || e.metaKey || e.altKey || e.shiftKey) return false;
  const files = e.target.id === "flfilter-input";
  let delta;
  if (e.key === "ArrowDown" || e.key === "ArrowUp") delta = e.key === "ArrowDown" ? 1 : -1;
  else if (e.key === "PageDown" || e.key === "PageUp") delta = (e.key === "PageDown" ? 1 : -1) * (files ? filePageRows() : commitPageRows());
  else return false;
  e.preventDefault();
  if (!files) stepCommitCursor(delta);
  else if (state.pane === "files") {
    moveCursor(delta);
    revealFileCursor();
  }
  return true;
}

// filePageRows is how far a page moves the file cursor: a quarter of the rows
// the pane shows under its sticky header (the TUI's pageStep).
function filePageRows() {
  const row = $("files-list").querySelector("li:not(.sect)");
  const h = $("files-pane").clientHeight - $("files-top").offsetHeight;
  return row && row.offsetHeight ? Math.max(1, Math.floor(h / row.offsetHeight / 4)) : 5;
}

// revealFileCursor scrolls the file pane so the cursor's row shows — below
// the sticky #files-top, which scrollIntoView knows nothing of.
function revealFileCursor() {
  const li = $("files-list").querySelector("li.sel");
  if (!li) return;
  const pane = $("files-pane");
  const p = pane.getBoundingClientRect();
  const r = li.getBoundingClientRect();
  const top = p.top + $("files-top").offsetHeight;
  if (r.top < top) pane.scrollTop -= top - r.top;
  else if (r.bottom > p.bottom) pane.scrollTop += r.bottom - p.bottom;
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
// A stack has one address per FILE, so the gate reads the ACTIVE slot's
// context (activeDiff), not the single-file view's global one — which is null
// while a stack is up and would leave every note key dead there.
// `c` passes the context of the file that HOLDS a marked range instead: that
// file takes the note, so its address is the one that must be armed.
function noteKey(e, key, ctx = activeDiff().ctx) {
  return e.key === key && !e.ctrlKey && !e.metaKey && !e.altKey && notesArmed(ctx);
}


// The windows alt+w closes (console.js closeWindows) whose own key
// handlers never pass it on; the console's calls walkKey itself.
const WINDOW_LAYERS = ["viewer", "history", "blame"];

document.addEventListener("keydown", (e) => {
  const top = topLayer();
  if (top) {
    if (WINDOW_LAYERS.includes(top.id) && e.altKey && e.code === "KeyW" && !(e.target.closest && e.target.closest("input,textarea")) && walkKey(e)) return;
    if (top.onKey && top.onKey(e)) return layerDefaultKey(top, e);
    if (e.key === "Escape") closeLayer(top.id); // default close for layers without onKey
    else layerDefaultKey(top, e);
    return; // a non-empty stack owns the keyboard — nothing leaks to the page behind
  }
  // Palette shortcut: after layer routing (an open layer keeps the keyboard),
  // before the form-field guard (ctrl+k must work from the commit box).
  if ((e.ctrlKey || e.metaKey) && (e.key === "k" || e.key === "p")) {
    e.preventDefault(); // ctrl+p would open the browser print dialog
    openPalette("cmd");
    return;
  }
  // ctrl+\: the open-files switcher, from anywhere a layer does not own the
  // keyboard (the viewer handles its own) — even from the commit box.
  if (isSwitcherKey(e)) {
    e.preventDefault();
    openSwitcher();
    return;
  }
  // alt+1…5 / alt+shift+1…5: branch-filter slots. Not inside inputs — a
  // digit typed into the commit box must stay a digit.
  if (!(e.target.closest && e.target.closest("input,textarea")) && branchFilterKey(e)) return;
  // alt+x: the text templates overlay (the TUI's key). Not inside inputs.
  if (!(e.target.closest && e.target.closest("input,textarea")) && textTemplatesKey(e)) return;
  // The TUI's alt+a / alt+t / alt+A / alt+T / alt+q / alt+Q with no console
  // shown (a shown one owns them through its layer).
  if (!(e.target.closest && e.target.closest("input,textarea")) && walkKey(e)) return;
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
  // A row selection is the most transient thing on screen: esc drops it
  // first, before a search or the diff itself.
  // …and a marked range of lines goes before that.
  if (e.key === "Escape" && clearDiffRange()) return;
  if (e.key === "Escape" && clearRowSelection()) return;
  if (diffSearchKey(e)) return;
  if (gotoDiffKey(e)) return;
  // shift+↓/↑ mark lines and L copies their link, before the arrows scroll.
  if (rangeKey(e)) return;
  // The symmetric comparison view's own keys (v, x, 1–4), only while one is up.
  if (symKey(e)) return;
  // In the diff layout the arrows and page keys scroll the DIFF (the TUI's
  // ↑↓ there), wherever the mouse left the focus; j/k still walk the files.
  if (diffScrollKey(e)) return;
  // Elsewhere PgDn/PgUp/Home/End move the focused list's cursor.
  if (pageKey(e)) return;
  // shift+↓/↑ on the Commits pane grows the ◉ marks as a contiguous run (the
  // TUI's shift+↑/↓); the diff layout's own shift+arrows were handled above.
  if (state.pane === "commits" && e.shiftKey && !e.ctrlKey && !e.metaKey && !e.altKey && (e.key === "ArrowDown" || e.key === "ArrowUp")) {
    e.preventDefault();
    markRunStep(e.key === "ArrowDown" ? 1 : -1);
    return;
  }
  // the TUI's ctrl+↑/↓ over a kept @ highlight, on the commit list — before
  // the plain arrows take them as a one-row move
  if (state.chl && state.pane === "commits" && (e.ctrlKey || e.metaKey) && (e.key === "ArrowDown" || e.key === "ArrowUp")) {
    e.preventDefault();
    stepHighlight(e.key === "ArrowDown" ? 1 : -1);
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
    } else if (reviewActive() && state.review.onSummary) showReviewSummary();
    else if (openSelectedReview()) return;
    else if (state.filesMode === "status" ? state.statusEntries.length : state.files.length) openFile(state.fileCursor);
  } else if (e.key === "Escape") {
    // The filter bar can be open with its input unfocused (a click landed
    // back on a commit row) — drillOut() no-ops in list layout, so Escape
    // would otherwise do nothing at all. Clearing the filter takes priority
    // over the layered close in list layout; diff/files behavior (drillOut)
    // is unchanged.
    if (state.pane === "files" && closeFileFilter()) return;
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
    // the TUI's ctrl+w (a browser owns ctrl+w itself): an image pair's
    // layout when one is up, else long lines: scroll → wrap → cutoff
    if (!cycleImageLayout()) cycleTextMode();
  } else if (e.key === "Tab" && !e.ctrlKey && !e.metaKey && !e.altKey && !e.shiftKey && flipImage()) {
    e.preventDefault(); // one at a time: old ↔ new (otherwise tab stays the browser's)
  } else if (e.key === "o") {
    // the TUI's o: cycle the focused list's display order. The working-tree
    // file list is the one the keyboard can reach — the sidebar's lists cycle
    // from the chips in their own headers.
    if (state.pane === "files" && state.filesMode === "status") cycleFilesSort();
  } else if (e.key === "I" && !e.ctrlKey && !e.metaKey && !e.altKey && state.pane === "commits") {
    commitMessageInEditor(); // the TUI's I: the commit's message, read-only, in the external editor
  } else if (e.key === "e" && !e.ctrlKey && !e.metaKey && !e.altKey && diffEditOpen()) {
    // the TUI's e in a diff: the file in the external editor at the line
  } else if (noteKey(e, "c", (rangeDiff() || activeDiff()).ctx)) {
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
  } else if (e.key === "a" && !e.ctrlKey && !e.metaKey && !e.altKey && allFilesOK()) {
    // the TUI's a in a commit's files view: every file of its tree ↔ only
    // the files it changed (a diff on screen keeps a for the notes agent)
    toggleAllFiles();
  } else if (noteKey(e, "a")) {
    toggleNotesAgent();
  } else if (noteKey(e, "z")) {
    collapseNearestNote(); // the TUI's o: fold the note you are on (o is the web's sort key)
  } else if (noteKey(e, "Z")) {
    toggleNoteCollapsed(null); // the TUI's O: every thread of this file
  } else if (noteKey(e, "}") || noteKey(e, "{") || (state.stack && (e.key === "}" || e.key === "{") && !e.ctrlKey && !e.metaKey && !e.altKey)) {
    // In a stack the gate cannot be the file under the cursor: a file not
    // read yet has no note context, and } exists to reach the next file that
    // carries notes — stepNote asks the stack itself.
    stepNote(e.key === "}" ? 1 : -1);
  } else if (e.key === "S" && !e.ctrlKey && !e.metaKey && !e.altKey) {
    toggleStacked(); // every file in one scroll ↔ one file at a time
  } else if (e.key === "-" && state.stack) {
    e.preventDefault();
    collapseCurrent();
  } else if (e.key === "_" && state.stack) {
    e.preventDefault();
    toggleAllCollapsed();
  } else if ((e.key === "," || e.key === ".") && reviewActive() && state.layout === "files" && !e.ctrlKey && !e.metaKey && !e.altKey) {
    // a review's file list: the TUI's p / n there, the previous / next file
    // the review notes.
    e.preventDefault();
    stepReviewFile(e.key === "." ? 1 : -1);
  } else if ((e.key === "," || e.key === ".") && state.layout === "diff" && !e.ctrlKey && !e.metaKey && !e.altKey) {
    // the TUI's p / n (p is pull here, - folds a stacked file): the ‹/›
    // change buttons, stepping from the viewport after a free scroll.
    e.preventDefault();
    stepChange(e.key === "." ? 1 : -1);
  } else if (e.key === "/") {
    e.preventDefault(); // the browser's quick-find would grab it
    // the TUI's /: a filter on the FOCUSED list
    if (state.pane === "files") openFileFilter();
    else openCommitFilter();
  } else if (e.key === "@" && state.layout !== "diff" && state.pane === "commits") {
    e.preventDefault();
    openCommitHighlight();
  } else if (e.key === "#") {
    e.preventDefault(); // the keystroke must not type itself into the prompt it opens
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
    case "filter": if (state.layout === "diff") diffSearchBar.open(false); else if (state.pane === "files") openFileFilter(); else openCommitFilter(); break;
    case "stage": stageFocused(false); break;
    case "unstage": stageFocused(true); break;
    case "sort": if (state.pane === "files" && state.filesMode === "status") cycleFilesSort(); break;
    case "diffview": toggleDiffView(); break;
    case "textmode": cycleTextMode(); break;
    case "stacked": toggleStacked(); break;
    case "allfiles": toggleAllFiles(); break;
    case "editor": diffEditOpen(); break;
    case "nextchange": stepChange(1); break;
    case "gotoline": gotoDiffKey(new KeyboardEvent("keydown", { key: ":" })); break;
    case "pull": doPull(); break;
    case "push": doPush(); break;
    case "refresh": manualRefresh(); break;
    case "help": openHelp(); break;
    case "palette": openPalette("cmd"); break;
    case "openfiles": openSwitcher(); break;
  }
});

export { filterFieldKey, focusPane, moveCursor, pageTarget, revealFileCursor, snapVisible, stepCommitCursor };
