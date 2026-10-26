// filehist.js — part of gg's web client. Split from the original app.js;
// see app.js (the entry module) for the load order.
import { $, esc, getJSON, state } from "./core.js";
import { closeLayer, openPrompt, pushLayer } from "./layers.js";
import { opLine } from "./ops.js";
import { versionWhen } from "./versions.js";
import { openCommitByHash } from "./commits.js";
import { cycleTextMode, diffTable, getDiff, mountPanBars, openFile, panBarsKey, openFileChangeDiff, renderCell, scrollKey, setDiffBack, setLayout, toggleDiffView } from "./files.js";
import { closeFinder, finderOn } from "./wtfinder.js";
import { focusPane, pageTarget } from "./keys.js";
import { registerHelp } from "./menus.js";
import { Search, firstRowBelow, foldRunes } from "./inviewsearch.js";
import { windowPainter } from "./paintwin.js";
import { bindSearchBar } from "./searchbar.js";
import { openGotoLine } from "./gotoline.js";
import { openViewer } from "./viewer.js";
import { openInEditor } from "./editor.js";
import { finishPaint, onPainted, paintTable } from "./diffpaint.js";
import { fetchLineToks, fetchToks, holdsEnd, patchCells, patchRows } from "./difftok.js";

// --- file history overlay ----------------------------------------------------
// A layer, not a layout mode: esc drops you exactly where you were. Gen-guarded
// like every async open (a slow filelog racing a close must not resurrect it).
let hist = null; // {path, rev, rows, sel, gen}

let histGen = 0;


async function openFileHistory(path, rev) {
  const gen = ++histGen;
  hist = { path, rev: rev || "", rows: [], sel: 0, gen };
  setHistoryTitle();
  applyHistoryMax(false); // every open starts two-pane (the TUI's ctrl+t rule)
  $("history-list").innerHTML = `<li class="empty">loading…</li>`;
  histDiffEl().innerHTML = "";
  pushLayer("history", $("history"), { onKey: historyKey });
  let body;
  try {
    body = await getJSON(
      "/api/filelog?path=" + encodeURIComponent(path) + "&rev=" + encodeURIComponent(rev || "")
    );
  } catch (e) {
    if (hist && hist.gen === gen)
      $("history-list").innerHTML = `<li class="empty">error: ${esc(e.message || e)}</li>`;
    return;
  }
  if (!hist || hist.gen !== gen) return; // closed or superseded meanwhile
  hist.rows = body.rows || [];
  hist.start = body.start || ""; // later pages walk from here
  hist.more = !!body.more;
  if (!hist.rows.length) {
    $("history-list").innerHTML = `<li class="empty">(no history)</li>`;
    return;
  }
  renderHistoryList();
  openHistoryDiff(0);
}


function closeHistory() {
  abortHistTok();
  hist = null;
  closeLayer("history");
}

// histTokAbort cancels the colour fetch of the revision on screen: moving to
// another one, or closing, means nobody will see those colours.
let histTokAbort = null;

function abortHistTok() {
  if (histTokAbort) histTokAbort.abort();
  histTokAbort = null;
}

// hideHistory steps the overlay aside for the diff view, keeping hist: the
// diff's esc brings it back as it was (reshowHistory).
function hideHistory() {
  closeLayer("history");
}

// reshowHistory is the way back from a diff a row opened: the same file, the
// same row, the same fullscreen state.
function reshowHistory() {
  if (!hist) return;
  pushLayer("history", $("history"), { onKey: historyKey });
  setHistoryTitle();
  renderHistoryList();
  if (hist.diff && hist.diffFor === hist.sel) renderHistoryDiff();
  else openHistoryDiff(hist.sel);
}

// openHistoryInDiff is enter (or a click) on a history row: that file's
// change at that commit opens on its own, full width, with every diff key
// (/, the change steppers, marks, notes, :) — the TUI's history → enter,
// a diff layer over the history. esc in it brings this overlay back on the
// same row, the page as it was (returnFromHistoryDiff). F steps aside
// first, as the viewer's "diff (this commit's change)" does: the diff stage
// must show and own the keys.
async function openHistoryInDiff(i) {
  if (!hist || !hist.rows[i]) return;
  if (i !== hist.sel) {
    hist.sel = i;
    hist.diffFor = -1; // the overlay's diff is another row's now
  }
  const r = hist.rows[i];
  const snap = pageSnapshot();
  hideHistory();
  if (finderOn()) closeFinder();
  const opened = openFileChangeDiff({ sha: r.hash, short: r.hash.slice(0, 7), subject: r.subject, path: r.path || hist.path, old: r.old_path || "", status: r.status });
  setDiffBack(() => returnFromHistoryDiff(snap)); // diffCtx is set already: an esc during the load comes back too
  await opened;
}

// pageSnapshot is what the page showed under the history: its stage and,
// in the diff stage, which row of its list was open.
function pageSnapshot() {
  const list = state.filesMode === "status" ? state.statusEntries : state.files;
  const f = state.layout === "diff" && !state.soloDiff ? list[state.fileCursor] : null;
  return { layout: state.layout, pane: state.pane, cursor: state.fileCursor, path: f ? f.path : "" };
}

// returnFromHistoryDiff is the diff's esc (drillOut has stepped to the files
// stage): the page goes back to the stage it showed — the commit list, or
// the diff of the row that was open — and the history comes back over it.
async function returnFromHistoryDiff(snap) {
  if (snap.layout === "list") {
    state.pane = snap.pane;
    setLayout("list");
    focusPane();
  } else if (snap.layout === "diff" && snap.path) {
    const list = state.filesMode === "status" ? state.statusEntries : state.files;
    const f = list[snap.cursor];
    if (f && f.path === snap.path) await openFile(snap.cursor);
  }
  reshowHistory();
}


// setHistoryTitle writes the title span (never the div — the » chip lives
// beside it). With rows loaded it carries "n/N · subject" so the reader still
// knows which commit is open once the list is folded away in fullscreen.
function setHistoryTitle() {
  let t = "history — " + hist.path + (hist.rev ? " @ " + hist.rev.slice(0, 8) : "");
  if (hist.rows.length) {
    const r = hist.rows[hist.sel];
    t += " · " + (hist.sel + 1) + "/" + hist.rows.length + " · " + r.short + " " + r.subject;
  }
  $("history-title-text").textContent = t;
}


// applyHistoryMax puts the overlay in (or out of) fullscreen: the commit list
// folds away and the box takes the viewport. The diff is laid out for the
// host's width and the per-side scrollbars are (un)mounted from it, so it is
// redrawn after every flip. Transient like the TUI's ctrl+t — not persisted.
function applyHistoryMax(on) {
  $("history").classList.toggle("max", on);
  const b = $("history-max");
  b.textContent = on ? "«" : "»";
  b.title = on ? "back to the two-pane view — show the commit list (m)" : "fullscreen — hide the commit list, the diff takes the whole screen (m)";
  b.setAttribute("aria-expanded", on ? "false" : "true");
  renderHistoryDiff();
}


function toggleHistoryMax() {
  if (!hist) return;
  applyHistoryMax(!$("history").classList.contains("max"));
}


function historyKey(e) {
  if (e.key === "Escape") {
    closeHistory();
    return true;
  }
  if (e.key === "Enter") {
    e.preventDefault();
    if (hist && hist.rows.length) openHistoryInDiff(hist.sel);
    return true;
  }
  if (e.key === ":" && !e.ctrlKey && !e.metaKey && !e.altKey) {
    e.preventDefault(); // the key must not land in the prompt it opens
    if (hist && hist.rows.length) openGotoLine(historyGoToLine);
    return true;
  }
  if (["j", "ArrowDown", "k", "ArrowUp"].includes(e.key)) {
    if (hist && hist.rows.length) {
      const d = e.key === "j" || e.key === "ArrowDown" ? 1 : -1;
      // ↓ past the last row loads 200 older (the TUI's paging).
      if (d > 0 && hist.sel === hist.rows.length - 1 && hist.more) {
        loadOlderHistory();
        e.preventDefault();
        return true;
      }
      openHistoryDiff(Math.max(0, Math.min(hist.rows.length - 1, hist.sel + d)));
    }
    e.preventDefault();
    return true;
  }
  if (e.key === "PageDown" || e.key === "PageUp") {
    // the TUI's pgdn/pgup: a page of commits; pgdn on the last row loads the
    // older ones, as ↓ does
    e.preventDefault();
    if (!hist || !hist.rows.length) return true;
    const to = pageTarget(e.key, hist.sel, hist.rows.length, historyPageRows());
    if (to !== hist.sel) openHistoryDiff(to);
    else if (e.key === "PageDown") loadOlderHistory(); // no-op without more, or while loading
    return true;
  }
  if (panBarsKey(histDiffEl(), e)) return true; // ←/→ pan the diff's long lines (scroll mode)
  if (e.key === "w" && !e.ctrlKey && !e.metaKey && !e.altKey) {
    cycleTextMode(); // the shared long-line mode; CSS on <body> redraws this overlay
    renderHistoryDiff(); // …and the per-side scrollbars are (un)mounted
    return true;
  }
  if (e.key === "m" && !e.ctrlKey && !e.metaKey && !e.altKey) {
    toggleHistoryMax();
    return true;
  }
  if (e.key === "e" && !e.ctrlKey && !e.metaKey && !e.altKey) {
    // the TUI's e: the selected commit's version, read-only, in the editor
    const r = hist && hist.rows[hist.sel];
    if (r) openInEditor({ source: "commit", rev: r.hash, path: r.path || hist.path });
    return true;
  }
  if (e.key === "f" && !e.ctrlKey && !e.metaKey && !e.altKey) {
    toggleDiffView(); // the shared preference flips; this overlay redraws itself
    if (state.diffPartial) hist.folds = new Set(); // on = a fresh entry: every run folded
    renderHistoryDiff();
    return true;
  }
  // The overlay owns the keyboard entirely while open — and a printable key
  // it does not use must not reach the browser either (/ and ' open
  // Firefox's quick find over the overlay).
  if (!e.ctrlKey && !e.metaKey && !e.altKey) {
    if (e.key.length === 1) e.preventDefault();
  }
  return true;
}


// historyGoToLine opens the file AT the selected commit at line n, over the
// history (esc comes back). A commit that deleted the file has no version of
// it: the version it deleted opens — the next older row's, under the old name.
function historyGoToLine(n) {
  if (!hist || !hist.rows[hist.sel]) return;
  const r = hist.rows[hist.sel];
  if (r.status === "D") {
    const prev = hist.rows[hist.sel + 1];
    if (!prev) return opLine("this commit deleted " + (r.path || hist.path) + " — walk to an older commit to read it", false);
    return openViewer({ src: "commit", rev: prev.hash, path: r.old_path || r.path || hist.path, line: n });
  }
  openViewer({ src: "commit", rev: r.hash, path: r.path || hist.path, line: n });
}

registerHelp({
  key: "file history · enter opens the diff",
  html:
    "in the file-history overlay <b>j</b>/<b>k</b> walk the commits and preview each change; <b>enter</b> (or a click on a row) opens " +
    "that change in the diff view, where every diff key works (<b>/</b> find, <b>n</b>/<b>p</b>, marks, notes, <b>:</b>…), " +
    "and <b>esc</b> there comes back to the history on the same commit",
});

registerHelp({
  key: "file history · older commits",
  html:
    "the file-history overlay lists 200 commits at a time; <b>pgup</b>/<b>pgdn</b> move a page. <b>↓</b> or <b>pgdn</b> on the last row (or a click on " +
    "<b>↓ load 200 older</b>) brings the next 200, walked from the commit the list started at",
});

registerHelp({
  key: ": · go to line (history, blame)",
  html:
    "in the file-history overlay <b>:</b> asks for a line number and opens the file as it is at the selected commit, " +
    "the cursor on that line (<b>esc</b> comes back to the history); in blame it moves to that line and marks it. " +
    "Past the end lands on the last line",
});


// historyPageRows is how many commits the list shows (each row two lines).
function historyPageRows() {
  const row = $("history-list").querySelector("li[data-i]");
  return row && row.offsetHeight ? Math.max(1, Math.floor($("history-list").clientHeight / row.offsetHeight) - 1) : 10;
}


function renderHistoryList() {
  $("history-list").innerHTML = hist.rows
    .map(
      (r, i) =>
        `<li data-i="${i}" class="${i === hist.sel ? "sel" : ""}">` +
        `<button class="hshow" data-i="${i}">show</button>` +
        `<span class="hsubj"><span class="st ${esc(r.status)}">${esc(r.status)}</span> ${esc(r.subject)}</span>` +
        `<span class="hmeta">${esc(r.short)} · ${esc(r.author)} · ${versionWhen(r.time)}</span></li>`
    )
    .join("") +
    (hist.more ? `<li class="hmore" data-more="1">${hist.loading ? "loading…" : "↓ load 200 older"}</li>` : "");
  const sel = $("history-list").querySelector("li.sel");
  if (sel) sel.scrollIntoView({ block: "nearest" });
}


// loadOlderHistory appends the next 200 commits, walked from the commit the
// first page started at (a commit landing meanwhile cannot shift the list).
async function loadOlderHistory() {
  if (!hist || hist.loading || !hist.more) return;
  const h = hist;
  h.loading = true;
  renderHistoryList();
  let body;
  try {
    body = await getJSON(
      "/api/filelog?path=" + encodeURIComponent(h.path) + "&rev=" + encodeURIComponent(h.start || h.rev || "") + "&skip=" + h.rows.length + "&limit=200"
    );
  } catch (e) {
    h.loading = false;
    if (hist === h) {
      renderHistoryList();
      opLine("older history: " + (e.message || e), true);
    }
    return;
  }
  h.loading = false;
  if (hist !== h) return; // closed or reopened meanwhile
  const first = h.rows.length;
  h.rows = h.rows.concat(body.rows || []);
  h.more = !!body.more;
  // the cursor follows into the new page only from the last row (the TUI's
  // advanceAt): a reader who moved up meanwhile stays where they are
  if (h.rows.length > first && h.sel === first - 1) openHistoryDiff(first);
  else renderHistoryList();
}


async function openHistoryDiff(i) {
  abortHistTok();
  hist.sel = i;
  renderHistoryList();
  setHistoryTitle();
  const r = hist.rows[i];
  const gen = hist.gen;
  // The /api/diff COMMIT form is already parent-vs-commit with A/D handling —
  // exactly "this file's change at this commit". path is the file's name AT
  // that commit (post-rename), old the parent-side name.
  const q = new URLSearchParams({ sha: r.hash, path: r.path || hist.path, status: r.status });
  if (r.old_path) q.set("old", r.old_path);
  histDiffEl().innerHTML = `<div class="notice">loading…</div>`;
  try {
    const d = await getDiff("/api/diff?" + q, { deferTok: true }); // an image pair's <img>s read from its url
    // Stale-response guard: rapid j/k can land responses out of order, so a
    // slow response for a commit the selection has since moved past must not
    // clobber a newer diff already on screen — same overlay (gen) AND the
    // selection still sitting on the row this response is for (i).
    if (!hist || hist.gen !== gen || hist.sel !== i) return;
    hist.diff = d;
    hist.diffFor = i;
    hist.folds = new Set(); // a new revision starts fully folded (when the mode is on)
    renderHistoryDiff(false); // a new revision paints from its top
    if (d.tok_pending) {
      // A big file came plain: its colours follow, for this revision only.
      const ac = (histTokAbort = new AbortController());
      fetchToks(d, ac.signal)
        .then((idx) => {
          const tbl = hist && hist.table;
          const live = () => !!hist && hist.diff === d && hist.table === tbl;
          if (idx && idx.length && live()) patchCells(histDiffEl(), histDiffEl(), tbl, idx, live);
        })
        .catch(() => {}); // aborted (moved on / closed) or failed: it stays plain
    }
  } catch (e) {
    if (hist && hist.gen === gen && hist.sel === i)
      histDiffEl().innerHTML = `<div class="notice">error: ${esc(e.message || e)}</div>`;
  }
}


// renderHistoryDiff draws (or redraws) the selected revision's diff. The
// overlay follows the main pane's view mode (state.diffPartial) with its own
// fold set, so unfolding here never touches the diff behind it.
// keep: a redraw of the same revision (fold, f, w, m, a return) keeps the
// scroll position — its first chunk paints down to it.
function renderHistoryDiff(keep = true) {
  if (!hist || !hist.diff) return;
  const host = histDiffEl();
  const d = hist.diff;
  // Painted in chunks (diffpaint.js): the screen now, the rest while idle.
  hist.table = diffTable(d, host.clientWidth, false, hist.folds);
  paintTable(host, hist.table, { keep });
  const bars = document.createElement("div");
  bars.className = "hbars hidden";
  host.appendChild(bars); // inside the scroll container so `sticky; bottom: 0` pins it
  mountPanBars(host, bars);
  onPainted(host, () => {
    if (hist && hist.diff === d && bars.isConnected) mountPanBars(host, bars);
  });
}

// histDiffEl is the overlay's diff host as an ELEMENT (write, size, listen);
// histDiff is it for READING ITS ROWS — the chunked paint finished first.
function histDiffEl() {
  return $("history-diff"); // the accessor
}

function histDiff() {
  finishPaint(histDiffEl());
  return histDiffEl();
}


histDiffEl().addEventListener("click", (e) => {
  const tr = e.target.closest("tr.fold[data-fold]");
  if (!tr || !hist || !hist.diff) return;
  hist.folds.add(Number(tr.dataset.fold));
  renderHistoryDiff();
  // The last run opened: the overlay shows the full file, so the shared
  // switch flips off (and the main pane behind it follows, as on any f).
  if (!histDiff().querySelector("tr.fold")) toggleDiffView(true);
});


$("history-list").addEventListener("click", (e) => {
  if (e.target.closest("li.hmore")) return loadOlderHistory();
  if (!hist) return;
  const show = e.target.closest("button.hshow");
  if (show) {
    const r = hist.rows[Number(show.dataset.i)];
    closeHistory();
    openCommitByHash(r.hash, r.short + " " + r.subject);
    return;
  }
  const li = e.target.closest("li[data-i]");
  if (li) openHistoryInDiff(Number(li.dataset.i));
});

$("history").addEventListener("click", (e) => {
  if (e.target.id === "history") closeHistory(); // backdrop closes, box does not
});

$("history-max").addEventListener("click", toggleHistoryMax);


registerHelp({
  key: "file history · m fullscreen",
  html:
    "inside the file-history overlay, <b>m</b> (or the <b>»</b> chip in its title) folds the commit " +
    "list away and the box takes the whole screen, so the diff gets everything; <b>«</b> or <b>m</b> " +
    "again brings the list back. j/k still walk the commits meanwhile and the title reads " +
    "<b>n/N · subject</b> so you know which one is open. Every open starts two-pane",
});


// --- blame overlay -----------------------------------------------------------
// Fetch-then-open: a blame failure (untracked path, binary) surfaces on the
// status line and the overlay never opens — nothing worse than an empty modal.
async function openFileBlame(path, rev) {
  abortBlameTok();
  // The rows first; a big file's colours follow (defer_tok, then tok=1).
  const url = "/api/blame?path=" + encodeURIComponent(path) + "&rev=" + encodeURIComponent(rev || "");
  let body;
  try {
    body = await getJSON(url + "&defer_tok=1");
  } catch (e) {
    opLine("blame failed: " + (e.message || e), true);
    return;
  }
  blameBase = "blame — " + path + (rev ? " @ " + rev.slice(0, 8) : " (working tree)");
  blameAt = { path, rev: rev || "" };
  blameLines = body.lines || [];
  blameSource = null;
  blameSearchBar.reset(); // a new file is a new search
  // Off on open: a fresh overlay never inherits the last one's tint; only
  // the last dialog text survives, to prefill the next d.
  state.blameRecent.on = false;
  // w cycles the shared long-line mode, d/D drive the age highlight, / @ ] [
  // the in-view search; everything else (esc included) is left to the
  // stack's default handling. Shown BEFORE the rows are painted: the
  // painter fills the screens it can measure.
  pushLayer("blame", $("blame"), { onKey: blameKey, onClose: abortBlameTok });
  blameEl().scrollTop = 0;
  renderBlame();
  blamePaint.reset(blameLines.length);
  applyBlameRecent();
  // Focus the content, not the button that opened it: the wheel, space and
  // the page keys then scroll the blame — and blameKey scrolls it for the
  // arrows and page keys whatever holds the focus.
  blameEl().focus({ preventScroll: true });
  if (!body.tok_pending) return;
  const ac = (blameTokAbort = new AbortController());
  const lines = blameLines;
  const live = () => blameLines === lines && !$("blame").classList.contains("hidden");
  fetchLineToks(url, body, ac.signal)
    .then((idx) => {
      if (!idx || !idx.length || !live()) return;
      // rows painted later are built from the merged lines: only these patch
      const want = new Set(idx);
      const rows = [...blameEl().querySelectorAll(".bline")].filter((el) => want.has(Number(el.dataset.i)));
      patchRows(blameEl(), rows, (el, ends) => {
        if (holdsEnd(el, ends)) return false;
        paintBlameEl(el, Number(el.dataset.i));
        return true;
      }, live);
    })
    .catch(() => {}); // aborted, or the colours failed: the blame stays plain
}

// blameTokAbort cancels the colour fetch of the blame on screen: closing it,
// or opening another, means nobody will see those colours.
let blameTokAbort = null;

function abortBlameTok() {
  if (blameTokAbort) blameTokAbort.abort();
  blameTokAbort = null;
}

// blameEl is #blame-body for writes, size, scrolling and listeners; every
// reader of its rows goes through blameBody, which finishes a chunked paint
// first.
function blameEl() {
  return $("blame-body"); // the accessor
}

function blameBody() {
  finishPaint(blameEl());
  return blameEl();
}

// blameLines is the open file's blame, kept so a search can repaint rows
// without a refetch.
let blameLines = [];
let blameAt = null; // the blamed file: what e opens

// renderBlame draws blameLines into the overlay, plain of hits, through the
// chunked painter (three screens now, the rest while idle): it runs once per
// open, and a search paints its hits row by row afterwards (paintBlameRow).
// Every row carries its index (data-i); the rows of a finished paint are
// blameRows. A row is tinted for the age highlight as it is built
// (applyBlameRecent re-tints the rows already painted).
function renderBlame() {
  blameRowCache = null;
  const host = blameEl();
  if (!blameLines.length) {
    paintTable(host, { html: `<div class="notice">(empty file)</div>` });
    return;
  }
  const lines = blameLines;
  const row = (i) => {
    const l = lines[i];
    const first = i === 0 || l.hash !== lines[i - 1].hash;
    const gut = !first
      ? ""
      : l.hash
        ? `<span class="bsha" data-h="${esc(l.hash)}" title="${esc(l.summary)}">${esc(l.short)}</span> ${esc(l.author)} · ${versionWhen(l.time)}`
        : `<span class="bwork">working</span>`;
    // Every row carries its author time (unix seconds) and an uncommitted
    // mark, so the recent-lines highlight can be re-applied without a refetch.
    return (
      `<div class="bline${first ? " bfirst" : ""}${blameRecentAt(l.hash ? Number(l.time) || 0 : 0, !l.hash) ? " brecent" : ""}" data-i="${i}" data-t="${Number(l.time) || 0}"${l.hash ? "" : ' data-u="1"'}>` +
      `<span class="bgut">${gut}</span>` +
      `<span class="bno">${l.line}</span>` +
      `<span class="btext">${renderCell(l.text, null, l.tok, "", null) || " "}</span></div>`
    );
  };
  const open = `<div class="rchunk">`;
  paintTable(host, { head: open, open, close: "</div>", n: lines.length, row, tail: "</div>" });
  onPainted(host, () => {
    if (blameLines === lines) blameRowCache = [...host.querySelectorAll(".bline")];
  });
}

// blameRowCache is a finished paint's rows (row i = line i); null while the
// rows are still being painted.
let blameRowCache = null;

// ---- in-view search (the TUI's / @ ] [ in its blame view) ----------------
// The TUI paints hits only on the rows it draws; here the whole file is in
// the DOM, so a keystroke re-finds over every line (the count is the file's)
// but repaints only the rows near the screen. Rows further away keep
// whatever they last showed and are repainted when they scroll near.
const blameSearch = new Search();

// blameSource is the open file's search lines, each folded once (on the
// first search) instead of on every keystroke.
let blameSource = null;

function blameSearchSource() {
  if (!blameSource) blameSource = blameLines.map((l, i) => ({ row: i, side: 0, text: l.text || "", fold: foldRunes(l.text || "") }));
  return blameSource;
}

// blameRows is the body's line rows (row i = line i); null for a notice or
// while the rows are still being painted (a reader that needs them all
// goes through blameBody first).
function blameRows() {
  return blameLines.length && blameRowCache && blameRowCache.length === blameLines.length ? blameRowCache : null;
}

// paintBlameRow redraws row i's text with the current query's hits.
function paintBlameRow(i) {
  const rows = blameRows();
  return rows ? paintBlameEl(rows[i], i) : 0;
}

// paintBlameEl redraws row el (line i): its text, colours and the current
// query's hits — the search's and the late colours' row painter.
function paintBlameEl(el, i) {
  const l = blameLines[i];
  const hits = blameSearch.query ? blameSearch.hitsOn(i, 0) : [];
  el.lastElementChild.innerHTML = renderCell(l.text, null, l.tok, "", hits) || " ";
  return hits.length;
}

const blamePaint = windowPainter({
  body: () => blameEl(),
  rows: blameRows,
  shown: () => !$("blame").classList.contains("hidden"),
  paint: paintBlameRow,
  hasHits: (i) => blameSearch.byLine.has(i + ":0"),
});

// repaintBlameSearch is the bar's render: re-find, then repaint the window.
function repaintBlameSearch() {
  blameBody(); // the hits are painted on rows: all of them
  if (blameSearch.query) blameSearch.refind(blameSearchSource());
  blamePaint.repaint();
}

// Scrolling (and a resize, which reflows wrapped rows) brings stale rows
// near the screen.
blameEl().addEventListener("scroll", () => blamePaint.queue(), { passive: true });
window.addEventListener("resize", () => blamePaint.queue());

// goToBlameHit makes hit i current and scrolls it into view; #blame-body
// scrolls both ways, so a long line's hit is revealed horizontally by the
// same call. The current hit's class rides the paint, so the old and the new
// current row are redrawn — wherever they are.
function goToBlameHit(i) {
  const s = blameSearch;
  if (i < 0 || i >= s.hits.length) return;
  const was = s.cur;
  s.cur = i;
  if (was >= 0 && was < s.hits.length && s.hits[was].row !== s.hits[i].row) blamePaint.paintRow(s.hits[was].row);
  blamePaint.paintRow(s.hits[i].row);
  const rows = blameRows();
  const el = rows && rows[s.hits[i].row].querySelector(`.hit[data-h="${i}"]`);
  if (el) el.scrollIntoView({ block: "center", inline: "nearest" });
}

const blameSearchBar = bindSearchBar("blame-search", {
  search: blameSearch,
  here: () => {
    blameBody();
    const rows = blameRows();
    if (!rows) return { row: 0, side: 0, col: -1 };
    const top = blameEl().getBoundingClientRect().top;
    const i = firstRowBelow(rows.length, (r) => rows[r].getBoundingClientRect().bottom, top);
    return { row: Math.min(i, rows.length - 1), side: 0, col: -1 };
  },
  origin: () => ({ top: blameEl().scrollTop, left: blameEl().scrollLeft }),
  restore: (o) => {
    blameEl().scrollTop = o.top;
    blameEl().scrollLeft = o.left;
  },
  render: () => repaintBlameSearch(),
  goTo: goToBlameHit,
  focus: () => blameEl().focus({ preventScroll: true }),
});

// blameSearchKey is the blame layer's first refusal on a bare key: / and @
// open a search, ] and [ step a kept one, esc clears a kept one (and
// reports it, so the layer's default esc does not ALSO close the overlay).
function blameSearchKey(e) {
  if (e.key === "/" || e.key === "@") {
    e.preventDefault(); // the browser's quick-find, and the key must not land in the input
    blameSearchBar.open(e.key === "@");
    return true;
  }
  if (e.key === "]" || e.key === "[") {
    if (!blameSearch.query) return false;
    e.preventDefault();
    blameSearchBar.step(e.key === "]" ? 1 : -1);
    return true;
  }
  if (e.key === "Escape" && blameSearch.active()) {
    blameSearchBar.clear();
    return true;
  }
  return false;
}


// --- age highlight -----------------------------------------------------------
// d asks for an age filter and tints every line whose age falls inside it;
// D turns the tint off. The filter lives in state.blameRecent for the
// session only (not /api/uistate); opening blame always starts OFF — only
// the last dialog text survives, to prefill the next d. Uncommitted lines
// have age 0 (the newest change of all). `now` is the wall clock, NOT the
// blamed revision's date: blaming an old commit may highlight nothing, by
// design.

// --- time span (pure; guarded against Go) ---
// A port of internal/timespan: parseSpan(s) → whole minutes, or null when
// the text is not a span; formatSpan(mins) → the canonical "1d3h5m" badge;
// parseFilter(s) → {older, younger} (minutes, null for an absent half) or
// null; formatFilter(f) → the signed badge "+1d2h -7d4h"; filterMatches(f,
// ageMins) → the inclusive window test.
// Span grammar: tokens <digits><w|d|h|m> (either part optional, not both),
// optional whitespace, any order, summed; a bare number is days, a bare
// unit is one of it; m is MINUTES (not months); units are case-blind.
// Errors: empty input, junk, an unknown unit, junk after a unit ("1mo",
// "1.5d", "1x"), a zero total.
// Filter grammar: up to two signed halves, "-<span>" = younger than (age at
// most span), "+<span>" = older than (age at least span); a leading unsigned
// span is the "-" half; each sign owns every token up to the next sign
// (whitespace after the sign is fine). Errors: a span
// error inside a half, a second "-" or "+", a sign with nothing after it,
// an older bound above the younger one. Pinned to timespan.Table by
// timespanjs_test.go — change both sides or neither.
const SPAN_UNITS = { w: 7 * 24 * 60, d: 24 * 60, h: 60, m: 1 };

function parseSpan(s) {
  s = String(s == null ? "" : s).trim();
  if (s === "") return null;
  let total = 0;
  let i = 0;
  const isDigit = (c) => c >= "0" && c <= "9";
  const isWS = (c) => c === " " || c === "\t";
  while (i < s.length) {
    while (i < s.length && isWS(s[i])) i++;
    if (i >= s.length) break;
    let j = i;
    while (j < s.length && isDigit(s[j])) j++;
    const n = j > i ? Number(s.slice(i, j)) : 1; // a bare unit ("w") is one of it
    let unit = SPAN_UNITS.d; // a bare number is days
    if (j < s.length) {
      const u = SPAN_UNITS[s[j].toLowerCase()];
      if (u !== undefined) {
        unit = u;
        j++;
      }
    }
    if (j === i) return null; // neither a number nor a unit: junk
    // The token must end here: whitespace, another number, or the end.
    if (j < s.length && !isWS(s[j]) && !isDigit(s[j])) return null;
    // Go refuses a token past 1<<62 ns / unit; 76861433 is that bound in
    // minutes, and floor-of-floor matches Go's integer division per unit.
    if (n > Math.floor(76861433 / unit)) return null;
    total += n * unit;
    if (!Number.isSafeInteger(total)) return null;
    i = j;
  }
  if (total <= 0) return null;
  return total;
}

function formatSpan(mins) {
  let m = Math.max(0, Math.floor(Number(mins) || 0));
  const days = Math.floor(m / (24 * 60));
  m -= days * 24 * 60;
  const hours = Math.floor(m / 60);
  m -= hours * 60;
  let out = "";
  if (days > 0) out += days + "d";
  if (hours > 0) out += hours + "h";
  if (m > 0 || out === "") out += m + "m";
  return out;
}

// parseFilter splits the text at its signs: the run before the first sign
// (if any) is an unsigned "-" half; each sign then owns the run up to the
// next sign, and every run goes through parseSpan.
function parseFilter(s) {
  s = String(s == null ? "" : s).trim();
  if (s === "") return null;
  const f = { older: null, younger: null };
  const apply = (sign, text) => {
    const mins = parseSpan(text);
    if (mins === null) return false;
    const key = sign === "+" ? "older" : "younger";
    if (f[key] !== null) return false; // a second - or + half
    f[key] = mins;
    return true;
  };
  let sign = "-";
  let start = 0;
  let seenSign = false;
  for (let i = 0; i <= s.length; i++) {
    if (i < s.length && s[i] !== "+" && s[i] !== "-") continue;
    const seg = s.slice(start, i).trim();
    if (seg === "") {
      if (seenSign) return null; // a sign with nothing after it
    } else if (!apply(sign, seg)) {
      return null;
    }
    if (i < s.length) {
      sign = s[i];
      start = i + 1;
      seenSign = true;
    }
  }
  if (f.older !== null && f.younger !== null && f.older > f.younger) return null; // empty range
  return f;
}

// formatFilter prints the canonical signed form, older half first:
// "-7d", "+30d", "+1d2h -7d4h". Empty when neither half is set.
function formatFilter(f) {
  const parts = [];
  if (f && f.older !== null && f.older !== undefined) parts.push("+" + formatSpan(f.older));
  if (f && f.younger !== null && f.younger !== undefined) parts.push("-" + formatSpan(f.younger));
  return parts.join(" ");
}

// filterMatches reports whether an age (minutes, fractional allowed — the
// boundary is inclusive at full precision) falls inside the window. A
// negative age (clock skew) clamps to zero and behaves like an uncommitted
// line; a filter with no half set matches nothing.
function filterMatches(f, ageMins) {
  if (!f) return false;
  const hasOlder = f.older !== null && f.older !== undefined;
  const hasYounger = f.younger !== null && f.younger !== undefined;
  if (!hasOlder && !hasYounger) return false;
  const age = Math.max(0, Number(ageMins) || 0);
  if (hasOlder && age < f.older) return false;
  if (hasYounger && age > f.younger) return false;
  return true;
}
// --- end time span ---

const BLAME_RECENT_TITLE = "Highlight lines by age…";
const BLAME_RECENT_HINT = "-7d younger · +30d older · +1d -7d between · +w";

// blameBase is the overlay title without the age badge; applyBlameRecent
// rebuilds the title from it so toggling never stacks badges.
let blameBase = "";

// applyBlameRecent re-tints the painted rows (rows still to be painted are
// tinted as they are built: blameRecentAt) and retitles the overlay.
function applyBlameRecent() {
  const br = state.blameRecent;
  const on = !!(br && br.on && br.f);
  for (const row of blameEl().querySelectorAll(".bline")) {
    row.classList.toggle("brecent", blameRecentAt(Number(row.dataset.t) || 0, row.dataset.u === "1"));
  }
  $("blame-title").textContent = blameBase + (on ? " · " + formatFilter(br.f) : "");
}

// blameRecentAt: a line of author time t (unix seconds) — uncommitted:
// age 0, which matches a "-" half, never a "+" half — takes the age tint.
function blameRecentAt(t, uncommitted) {
  const br = state.blameRecent;
  if (!(br && br.on && br.f)) return false;
  const age = uncommitted ? 0 : (Date.now() - t * 1000) / 60000;
  return filterMatches(br.f, age);
}

// openBlameRecentPrompt asks for the age filter; a bad answer re-opens the
// prompt prefilled with the offending text and the reason in the title, so
// the error reads above the field and the status line both (the overlay's
// backdrop dims the status line).
function openBlameRecentPrompt(value, err) {
  openPrompt({
    title: err ? BLAME_RECENT_TITLE + " — " + err : BLAME_RECENT_TITLE,
    value,
    placeholder: BLAME_RECENT_HINT,
    onSubmit: (text) => {
      const f = parseFilter(text);
      if (f === null) {
        const msg = "not an age filter: “" + text + "” (" + BLAME_RECENT_HINT + ")";
        opLine(msg, true);
        openBlameRecentPrompt(text, msg);
        return;
      }
      state.blameRecent = { on: true, f, last: text };
      applyBlameRecent();
    },
  });
}

registerHelp({
  key: "d / D · blame age highlight",
  html:
    "in the blame overlay, <b>d</b> asks for an age filter and tints every line whose commit falls inside it, " +
    "measured from now: <b>-7d</b> younger than a week, <b>+30d</b> older than a month, <b>+1d -7d</b> between the two " +
    "(both bounds inclusive; a bare span is the younger half, <b>+w</b> is a week, <b>1d 3h 5m</b> adds up, a bare number is days, m is minutes). " +
    "Uncommitted lines are age 0. <b>D</b> turns the tint off, <b>d</b> again edits the filter. " +
    "Off whenever blame opens; only the last text is kept to prefill the prompt — the TUI's d / D in its blame view",
});

function blameKey(e) {
  // A key typed into the search bar is the query's (its own listener takes
  // enter and esc): it must reach neither w / d / D nor the layer's default
  // esc, which would close the overlay under the reader's fingers.
  if (e.target === $("blame-search-input")) return true;
  if (e.ctrlKey || e.metaKey || e.altKey) return false;
  if (blameSearchKey(e)) return true;
  if (!e.shiftKey && scrollKey(blameEl(), e)) return true; // ↑↓ PgUp PgDn Home End scroll the blame
  if (e.key === "j" || e.key === "k") {
    // the TUI's j/k in blame: a line down / up, as ↓ / ↑
    scrollKey(blameEl(), { key: e.key === "j" ? "ArrowDown" : "ArrowUp", preventDefault: () => e.preventDefault() });
    return true;
  }
  if (e.key === "w") {
    cycleTextMode();
    blamePaint.queue(); // rows rewrap: other rows are near the screen now
    return true;
  }
  if (e.key === "d") {
    e.preventDefault(); // the letter must never land in the prompt it opens
    openBlameRecentPrompt(state.blameRecent.last);
    return true;
  }
  if (e.key === "e" && !e.ctrlKey && !e.metaKey && !e.altKey) {
    // the TUI's e: the blamed version; a working-tree blame's on-disk file as a copy
    if (blameAt) openInEditor(blameAt.rev ? { source: "commit", rev: blameAt.rev, path: blameAt.path } : { source: "worktree-copy", path: blameAt.path });
    return true;
  }
  if (e.key === "D") {
    e.preventDefault();
    state.blameRecent.on = false;
    applyBlameRecent();
    return true;
  }
  if (e.key === ":") {
    e.preventDefault(); // the letter must never land in the prompt it opens
    openGotoLine(blameGoToLine);
    return true;
  }
  return false;
}

// blameGoToLine marks blame line n (the last when n is past the end) and
// centres it.
function blameGoToLine(n) {
  const body = blameBody(), count = body.querySelectorAll(".bline").length;
  if (!count) return;
  if (n > count) {
    opLine("line " + n + " is past the end of the file (" + count + " lines)", false);
    n = count;
  }
  for (const el of body.querySelectorAll(".bline.goto")) el.classList.remove("goto");
  const row = body.querySelector(`.bline[data-i="${n - 1}"]`);
  if (!row) return;
  row.classList.add("goto");
  row.scrollIntoView({ block: "center", inline: "nearest" });
}


blameEl().addEventListener("click", (e) => {
  const sha = e.target.closest(".bsha");
  if (!sha) return;
  closeLayer("blame");
  openCommitByHash(sha.dataset.h, sha.dataset.h.slice(0, 8));
});

$("blame").addEventListener("click", (e) => {
  if (e.target.id === "blame") closeLayer("blame"); // backdrop closes, box does not
});

export { applyBlameRecent, closeHistory, filterMatches, formatFilter, formatSpan, hist, histGen, historyKey, openFileBlame, openFileHistory, openHistoryDiff, parseFilter, parseSpan, renderHistoryList };
