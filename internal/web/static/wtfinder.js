// wtfinder.js — F: the files pane lists every file in the working tree and
// the diff pane previews the one under the cursor (open files on the web,
// plan 5d; the TUI's F window). The list lives on the server
// (/api/worktree-files ranks a query and pages the rest) — gg's repos hold
// 100k+ paths, which a page must never hold.
//
// F is a LAYER: it owns the keyboard, so every surface it opens (the viewer,
// history, blame, the . menu, the ctrl+\ switcher) sits over it and esc lands
// back here. It DRAWS in the panes: a "wtf" class on #panes shows #wtf in the
// files pane and #wtf-preview in the diff pane over whatever stage is up, and
// hides the panes' own children without touching them — esc restores the
// stage exactly as it was.
import { $, charWidth, elidePath, esc, fmtBytes, getJSON, postJSON, state } from "./core.js";
import { closeLayer, copyText, footOwned, popFoot, pushFoot, pushLayer, showCtxMenu, topLayer, updateFoot } from "./layers.js";
import { copyPathRows, cycleTextMode, renderCell } from "./files.js";
import { copyFileLink, linkFor } from "./links.js";
import { Search } from "./inviewsearch.js";
import { bindSearchBar } from "./searchbar.js";
import { lineSearchHost } from "./linesearch.js";
import { finishPaint, isPainting, onPainted, paintTable } from "./diffpaint.js";
import { fetchLineToks, holdsEnd, patchRows } from "./difftok.js";
import { openGotoLine } from "./gotoline.js";
import { openFileBlame, openFileHistory } from "./filehist.js";
import { evictedText, imageHTML, openViewer, openWorktreeFileDiff } from "./viewer.js";
import { opLine } from "./ops.js";
import { openInEditor } from "./editor.js";
import { isSwitcherKey, openSwitcher } from "./openfiles.js";
import { registerHelp } from "./menus.js";
import { recallKey, recallOpen, recordSearch } from "./searchhist.js";

// --- finder model (pure; guarded against Go) ---
const WT_UNTRACKED = "  (untracked)";

// wtTitle is the list's title: what it shows and how many (the TUI's words).
function wtTitle(loading, query, shown, total) {
  if (loading) return "Files (working tree)  (loading…)";
  return "Files (working tree)  " + (query ? shown : total) + "/" + total;
}

function wtClamp(sel, n) {
  if (n <= 0) return 0;
  return Math.min(Math.max(sel, 0), n - 1);
}

// wtWantMore: an unfiltered list asks for its next page once the cursor is
// within a screen of the rows it has.
function wtWantMore(sel, n, next, page) {
  return next > 0 && n > 0 && sel >= n - Math.max(1, page);
}

// wtPathCols is the path's column budget on a row of cols columns — less the
// "(untracked)" mark; 0 = render the path whole (too narrow to cut usefully).
function wtPathCols(cols, untracked) {
  if (cols <= 0) return 0;
  const c = untracked ? cols - WT_UNTRACKED.length : cols;
  return c >= 4 ? c : 0;
}

function wtEmpty(query) {
  return query ? "(no match)" : "(no files)";
}

// The cursor rests this long before the preview reads the file (the TUI's
// wtPreviewSettle): a held arrow over a slow mount reads only where it stops.
const WT_SETTLE_MS = 150;

// wtPreviewFresh: an answer paints only when no later move superseded it and
// the cursor still sits on its file.
function wtPreviewFresh(gen, curGen, path, curPath) {
  return gen === curGen && path === curPath;
}

// The preview follows the disk while F is up: its file is re-stated this
// often (the open-files poll's shown-file tick) — one stat, never a read.
const WT_STAMP_MS = 1000;

// wtStampChanged: the file moved since the preview read it. An unknown stamp
// ("" — a failed stat, or no read yet) is never a change.
function wtStampChanged(shown, now) {
  return !!shown && !!now && shown !== now;
}

// wtPlaceholder is what the preview shows instead of lines ("" = the lines).
function wtPlaceholder(body, n) {
  if (body.missing) return "(file deleted on disk)";
  if (body.too_large) return "(file too large to preview)";
  if (body.binary) return body.image ? "" : `(binary file, ${fmtBytes(body.size || 0)} — not shown)`;
  return n ? "" : "(empty file)";
}

// wtImage is the image the preview body describes, or null (viewer.js's
// imageOf, at the working tree).
function wtImage(body, path) {
  if (!body.binary || !body.image) return null;
  const url = "/api/file-raw?src=worktree&path=" + encodeURIComponent(path) + (body.stamp ? "&stamp=" + encodeURIComponent(body.stamp) : "");
  return { url, info: `${body.image} image ${body.width}×${body.height}, ${fmtBytes(body.size || 0)}` };
}

// wtActions is the action list of a row (enter / . / right-click): the git
// rows only for a tracked file — an untracked one has no history (the TUI's
// worktreeFileRows).
// wtSelPress is space in the preview (the TUI's lineSel.press): start a
// range at the cursor, then freeze its far end, then start again. A range is
// {on, anchor, end, fixed}; null = nothing marked.
function wtSelPress(s, cur) {
  if (s && s.on && !s.fixed) return { on: true, anchor: s.anchor, end: cur, fixed: true };
  return { on: true, anchor: cur, end: cur, fixed: false };
}

// wtSelBounds is the inclusive [lo, hi] the marks cover with the cursor on
// cur: anchor..cursor while loose, anchor..end once frozen; null = none.
function wtSelBounds(s, cur) {
  if (!s || !s.on) return null;
  const other = s.fixed ? s.end : cur;
  return [Math.min(s.anchor, other), Math.max(s.anchor, other)];
}

// wtCursorStep is alt+↑ / alt+↓ (the TUI's movePreviewCursor): one line, in
// the file; a cursor the reader scrolled out of the window [first, last]
// comes back at its nearest edge first, without moving further.
function wtCursorStep(cur, delta, first, last, n) {
  if (n <= 0) return 0;
  if (cur < first) return first;
  if (cur > last) return Math.min(last, n - 1);
  return Math.min(Math.max(cur + delta, 0), n - 1);
}

function wtActions(untracked) {
  return untracked ? ["view", "copy"] : ["view", "diff", "history", "blame", "copy"];
}

// wtPanStep is where ← (dir −1) or → (dir 1) takes the preview's horizontal
// scroll x: a step either way, never past column 0. ←/→ only ever pan, as
// in every file view; tab and esc give the keys back to the list.
function wtPanStep(x, dir, step) {
  return Math.max(0, x + dir * step);
}
// --- end finder model ---

// wtf is the finder while it is up: its rows (one page per unfiltered load,
// or the ranked matches), the cursor, the query, the next page's offset and
// the server read the pages were cut from.
const wtf = { on: false, loading: false, rows: [], sel: 0, query: "", next: 0, gen: 0, total: 0, paging: false };
let reqSeq = 0; // bumped by every list request: a later one wins
let queryTimer = null;
const PAN_STEP = 40; // px per ←/→ in the preview: the browser's own arrow step

// The markup is built at import, inside the two panes it takes over.
const root = document.createElement("div");
root.id = "wtf";
root.className = "hidden";
root.innerHTML =
  `<div id="wtf-title"></div>` +
  `<div id="wtf-search" class="hidden"><span>/</span><input id="wtf-input" type="text" autocomplete="off" spellcheck="false"></div>` +
  `<ul id="wtf-list"></ul>`;
$("files-pane").append(root);
const preview = document.createElement("div");
preview.id = "wtf-preview";
preview.className = "hidden";
preview.innerHTML =
  `<div id="wtf-ptitle"></div>` +
  `<div id="wtf-psearch" class="hidden search-bar"><span id="wtf-psearch-lead">/</span><input id="wtf-psearch-input" type="text" autocomplete="off" spellcheck="false" placeholder="find in this file — enter keeps it, ] [ step, esc clears"><span id="wtf-psearch-count"></span></div>` +
  `<div id="wtf-body" tabindex="-1"></div>`;
$("diff-pane").append(preview);

function finderOn() {
  return wtf.on;
}

// openFinder takes the files pane (F). Nothing when a layer is up — F is
// opened from the page, never from under another surface.
function openFinder() {
  if (wtf.on || topLayer()) return;
  Object.assign(wtf, { on: true, loading: true, rows: [], sel: 0, query: "", next: 0, gen: 0, total: 0, paging: false });
  $("wtf-input").value = "";
  recallOpen($("wtf-input"));
  $("wtf-search").classList.add("hidden");
  savePanes();
  $("panes").classList.add("wtf");
  preview.classList.remove("hidden");
  pushLayer("wtf", root, { onKey: finderKey });
  pushFoot("wtf", WTF_FOOT);
  clearInterval(stampTimer);
  stampTimer = setInterval(checkStamp, WT_STAMP_MS);
  render();
  load({ fresh: true });
}

// Hiding a pane's children (or the pane) drops its scroll: the panes F
// covers are read before and put back after, so esc lands where you were.
const PANES = ["commits-pane", "files-pane", "diff-pane"];
let paneScroll = {};

function savePanes() {
  paneScroll = {};
  for (const id of PANES) paneScroll[id] = { top: $(id).scrollTop, left: $(id).scrollLeft };
}

function restorePanes() {
  for (const id of PANES) {
    const s = paneScroll[id];
    if (!s) continue;
    $(id).scrollTop = s.top;
    $(id).scrollLeft = s.left;
  }
}

// closeFinder gives the panes back: dropping the class is the whole restore.
function closeFinder() {
  if (!wtf.on) return;
  wtf.on = false;
  reqSeq++; // a list request in flight must not repaint
  clearTimeout(queryTimer);
  clearInterval(stampTimer);
  $("wtf-input").blur();
  pv.focus = pv.full = false;
  $("panes").classList.remove("wtf", "wtf-pv", "wtf-full");
  restorePanes();
  preview.classList.add("hidden");
  closeLayer("wtf");
  popFoot("wtf");
  closedHook();
}

// load fetches the list: a ranked query, or the unfiltered list from offset.
// A failed first read closes F and says why (the TUI's "file finder: …").
async function load({ fresh = false, offset = 0 } = {}) {
  const seq = ++reqSeq;
  const q = new URLSearchParams({ q: wtf.query });
  if (fresh) q.set("fresh", "1");
  if (offset) q.set("offset", String(offset));
  let body;
  try {
    body = await getJSON("/api/worktree-files?" + q);
  } catch (e) {
    if (seq !== reqSeq || !wtf.on) return;
    wtf.paging = false;
    if (offset) return; // a failed page leaves the rows shown; the next scroll retries
    closeFinder();
    opLine("file finder: " + (e.message || e), true);
    return;
  }
  if (seq !== reqSeq || !wtf.on) return;
  wtf.paging = false;
  if (offset && body.gen !== wtf.gen) return load(); // another tab re-read the list under this scroll: start over
  const files = body.files || [];
  Object.assign(wtf, { loading: false, total: body.total || 0, next: body.next || 0, gen: body.gen || 0 });
  if (offset) {
    wtf.rows = wtf.rows.concat(files);
    render();
    return;
  }
  wtf.rows = files;
  wtf.sel = 0;
  render();
  cursorMoved();
}

function loadMore() {
  if (wtf.query || !wtf.next || wtf.paging) return;
  wtf.paging = true;
  load({ offset: wtf.next });
}

function setQuery(q) {
  clearTimeout(queryTimer);
  if (q === wtf.query) return;
  wtf.query = q;
  wtf.next = 0;
  wtf.paging = false;
  if ($("wtf-input").value !== q) $("wtf-input").value = q;
  paintSearch();
  load();
}

function paintSearch() {
  const typing = document.activeElement === $("wtf-input");
  $("wtf-search").classList.toggle("hidden", !typing && !wtf.query);
}

// rowCols is a row's width in columns (the files list's measure).
function rowCols() {
  const px = $("wtf-list").clientWidth;
  if (px <= 0) return 0;
  return Math.floor((px - 16) / charWidth()) - 1;
}

function render() {
  $("wtf-title").textContent = wtTitle(wtf.loading, wtf.query, wtf.rows.length, wtf.total);
  paintSearch();
  const list = $("wtf-list");
  if (wtf.loading) {
    list.innerHTML = `<li class="empty">(loading…)</li>`;
    return;
  }
  if (!wtf.rows.length) {
    list.innerHTML = `<li class="empty">${wtEmpty(wtf.query)}</li>`;
    return;
  }
  const cols = rowCols();
  list.innerHTML = wtf.rows
    .map((f, i) => {
      const pc = wtPathCols(cols, !!f.untracked);
      return (
        `<li data-i="${i}"${i === wtf.sel ? ' class="sel"' : ""} title="${esc(f.path)}">` +
        esc(pc ? elidePath(f.path, pc) : f.path) +
        (f.untracked ? `<span class="wtf-un">${WT_UNTRACKED}</span>` : "") +
        `</li>`
      );
    })
    .join("");
  paintSel();
}

function paintSel() {
  for (const el of $("wtf-list").querySelectorAll("li.sel")) el.classList.remove("sel");
  const el = $("wtf-list").querySelector(`li[data-i="${wtf.sel}"]`);
  if (el) {
    el.classList.add("sel");
    el.scrollIntoView({ block: "nearest" });
  }
}

function pageRows() {
  const row = $("wtf-list").querySelector("li");
  return row ? Math.max(1, Math.floor($("wtf-list").clientHeight / row.offsetHeight) - 1) : 10;
}

function selected() {
  return wtf.loading ? null : wtf.rows[wtf.sel] || null;
}

function move(delta) {
  if (!wtf.rows.length) return;
  wtf.sel = wtClamp(wtf.sel + delta, wtf.rows.length);
  paintSel();
  if (wtWantMore(wtf.sel, wtf.rows.length, wtf.next, pageRows())) loadMore();
  cursorMoved();
}

// The preview: the file under the cursor, read from disk once the cursor
// rests. NOT an open file (the TUI's rule): it never touches /api/open-files,
// so browsing never fills the ctrl+\ list.
let previewGen = 0;
let previewTimer = null;
let previewPath = ""; // the file the preview shows ("" = none)
let previewStamp = ""; // its disk stamp when read ("" = unknown)
let stampTimer = null;
let stampBusy = false;

function cursorMoved() {
  const gen = ++previewGen;
  clearTimeout(previewTimer);
  const f = selected();
  if (!f) {
    focusList(); // nothing to preview: the list takes the keys back (the TUI's wtCursorMoved)
    paintPreview("", [], "");
    return;
  }
  if (f.path === previewPath) return;
  previewTimer = setTimeout(() => showPreview(gen, f.path), WT_SETTLE_MS);
}

function closedHook() {
  abortPreviewTok();
  previewGen++;
  clearTimeout(previewTimer);
  paintPreview("", [], "");
}

// showPreview reads path and paints it; keep (a reload of the file already
// shown, at stamp) holds the preview's scroll where it was.
async function showPreview(gen, path, keep, stamp) {
  let body;
  // The text first; a big file's colours follow (colourPreviewLater).
  const url = "/api/file-content?src=worktree&path=" + encodeURIComponent(path);
  try {
    body = await getJSON(url + "&defer_tok=1");
  } catch (e) {
    if (!wtf.on || !wtPreviewFresh(gen, previewGen, path, (selected() || {}).path)) return;
    previewStamp = keep ? stamp : ""; // a failed reload takes the stamp it saw: no retry per tick
    paintPreview(path, [], "(load failed: " + (e.message || e) + ")", keep);
    return;
  }
  if (!wtf.on || !wtPreviewFresh(gen, previewGen, path, (selected() || {}).path)) return;
  previewStamp = body.stamp || "";
  const lines = body.lines || [];
  paintPreview(path, lines, wtPlaceholder(body, lines.length), keep, wtImage(body, path));
  colourPreviewLater(url, body);
}

// previewTokAbort cancels the colour fetch of the file previewed: another
// file, or F closing, means nobody will see those colours.
let previewTokAbort = null;

function abortPreviewTok() {
  if (previewTokAbort) previewTokAbort.abort();
  previewTokAbort = null;
}

// colourPreviewLater fetches a big file's colours (body: its deferred
// answer, url: its request) and patches them into the painted lines through
// the shared applier; rows painted later are built from the merged lines.
function colourPreviewLater(url, body) {
  abortPreviewTok();
  const lines = body.lines || [];
  if (!body.tok_pending || pv.lines !== lines) return;
  const ac = (previewTokAbort = new AbortController());
  const live = () => wtf.on && pv.lines === lines;
  fetchLineToks(url, body, ac.signal)
    .then((idx) => {
      if (!idx || !idx.length || !live()) return;
      const want = new Set(idx);
      const rows = [...wtfEl().querySelectorAll(".vline[data-i]")].filter((el) => want.has(Number(el.dataset.i)));
      patchRows(wtfEl(), rows, (el, ends) => {
        if (holdsEnd(el, ends)) return false;
        pvLines.paintEl(el, Number(el.dataset.i));
        return true;
      }, live);
    })
    .catch(() => {}); // aborted, or the colours failed: the preview stays plain
}

// wtfEl is #wtf-body for writes, size, scrolling, focus and listeners; a
// reader of every row goes through wtfBody (pvRows), which finishes a
// chunked paint first.
function wtfEl() {
  return $("wtf-body"); // the accessor
}

function wtfBody() {
  finishPaint(wtfEl());
  return wtfEl();
}

// checkStamp re-stats the previewed file and reloads it when it moved (the
// TUI's watchedDoc). Not while a surface covers F, a tab is hidden, or a
// check is still out — over /mnt a stat can be slow.
async function checkStamp() {
  const top = topLayer();
  if (!wtf.on || stampBusy || !previewPath || document.hidden || !top || top.id !== "wtf") return;
  const gen = previewGen;
  const path = previewPath;
  stampBusy = true;
  try {
    let now = "";
    try {
      now = (await getJSON("/api/file-stamp?path=" + encodeURIComponent(path))).stamp || "";
    } catch {
      return;
    }
    if (!wtf.on || gen !== previewGen || path !== previewPath) return;
    if (!wtStampChanged(previewStamp, now)) return;
    // The stamp moves only with a paint: a reload dropped because the cursor
    // stepped off and back leaves the old one, so the next tick retries.
    await showPreview(gen, path, true, now);
  } finally {
    stampBusy = false;
  }
}

// paintPreview draws path's lines (or the placeholder) at the top — or, with
// keep, where the preview was scrolled; path "" empties the pane.
function paintPreview(path, lines, placeholder, keep, image) {
  previewPath = path;
  if (!path) previewStamp = "";
  paintPTitle(path);
  // Another file is another reading: cursor, marks and search start over.
  // The same file re-read from disk keeps them, clamped to its new length.
  pv.lines = path && !placeholder && !image ? lines : [];
  if (!keep || !pv.lines.length) {
    pv.cur = 0;
    pv.sel = null;
    pvSearchBar.reset();
  }
  if (path !== previewPathPainted) abortPreviewTok(); // another file: the old one's colours are moot
  previewPathPainted = path;
  const body = wtfEl();
  const at = { top: body.scrollTop, left: body.scrollLeft };
  pvRowCache = null;
  shownMarks = { cur: -1, lo: 0, hi: -1 }; // the rows are drawn plain
  if (!path) paintTable(body, { html: "" });
  else if (placeholder) paintTable(body, { html: `<div class="notice">${esc(placeholder)}</div>` });
  else if (image) paintTable(body, { html: imageHTML(image) });
  else {
    // The shared chunked painter: three screens now (down to where the
    // reader is, keep), the rest while idle.
    const row = (i) => `<div class="vline" data-i="${i}"><span class="vno">${i + 1}</span><span class="vtext">${renderCell(lines[i].text, null, lines[i].tok, "", null) || " "}</span></div>`;
    const open = `<div class="rchunk">`;
    if (!keep) body.scrollTop = 0;
    paintTable(wtfEl(), { head: open, open, close: "</div>", n: lines.length, row, tail: "</div>" }, { keep });
    onPainted(body, () => {
      if (pv.lines !== lines) return;
      pvRowCache = [...body.querySelectorAll(".vline")];
      paintMarks(); // the cursor and the band, now that every row is there
    });
  }
  body.scrollTop = keep ? at.top : 0;
  body.scrollLeft = keep ? at.left : 0;
  const n = pv.lines.length;
  if (n) {
    pv.cur = Math.min(pv.cur, n - 1);
    if (pv.sel) pv.sel = { ...pv.sel, anchor: Math.min(pv.sel.anchor, n - 1), end: Math.min(pv.sel.end, n - 1) };
  }
  pvLines.rowsChanged();
  paintMarks();
  if (pvSearch.query) {
    pvLines.render();
    pvSearchBar.paint();
  }
  paintFoot();
}

// paintPTitle names the previewed file, the PATH cut in the middle to the
// title's width (never the name).
function paintPTitle(path) {
  const title = $("wtf-ptitle");
  const tail = " (working tree)";
  const cols = Math.floor((title.clientWidth - 16) / charWidth()) - tail.length;
  title.title = path;
  title.textContent = path ? (cols > 3 ? elidePath(path, cols) : path) + tail : "";
}

// ---- the preview with the keys (spec 2026-10-25-finder-preview-focus) -----
// The TUI's focused preview: the pager keys scroll, alt+↑↓ walk a line
// cursor, space marks lines (enter copies them, L their link), / finds, :
// goes to a line. enter, →, tab or a click hand it the keys; tab, esc or
// a click on the list take them back.
const pv = { focus: false, full: false, lines: [], cur: 0, sel: null };
let shownMarks = { cur: -1, lo: 0, hi: -1 }; // what the rows show now

// pvRows is the preview's line rows (row i = line i), every one painted
// (it finishes a chunked paint); null for a notice, an image or an empty
// pane. pvRowsDone is the same without painting: null while rows are owed.
function pvRows() {
  wtfBody();
  return pvRowsDone();
}

function pvRowsDone() {
  return pv.lines.length && pvRowCache && pvRowCache.length === pv.lines.length ? pvRowCache : null;
}

let pvRowCache = null; // a finished paint's rows (row i = line i)
let previewPathPainted = ""; // the file the rows are of

// paintMarks moves the cursor row and the marked band to where pv says,
// touching only the rows that change (a repaint of a large file is slow).
function paintMarks() {
  const rows = pvRowsDone(); // still painting: onPainted paints the marks
  if (!rows) return;
  const cur = pv.focus ? pv.cur : -1;
  const [lo, hi] = wtSelBounds(pv.sel, pv.cur) || [0, -1];
  const was = shownMarks;
  const inBand = (k, a, b) => k >= a && k <= b;
  for (const [a, b] of [[was.lo, was.hi], [lo, hi]]) {
    for (let k = a; k <= b; k++) {
      const want = inBand(k, lo, hi);
      if (want !== inBand(k, was.lo, was.hi)) rows[k].classList.toggle("vrange", want);
    }
  }
  if (was.cur !== cur) {
    if (was.cur >= 0 && rows[was.cur]) rows[was.cur].classList.remove("vcur");
    if (cur >= 0) rows[cur].classList.add("vcur");
  }
  shownMarks = { cur, lo, hi };
}

// focusPreview hands the keys to the preview (enter, →, tab, a click).
function focusPreview() {
  if (!wtf.on || !selected()) return;
  pv.focus = true;
  $("panes").classList.add("wtf-pv");
  wtfEl().focus({ preventScroll: true });
  paintMarks();
  paintFoot();
}

// focusList takes them back (tab, esc, a click on the list); the
// preview's full screen goes with them, as in the TUI.
function focusList() {
  if (!pv.focus) return;
  pv.focus = false;
  wtfEl().blur(); // a focused body would still scroll on the list's space / page keys
  setFull(false);
  $("panes").classList.remove("wtf-pv");
  paintMarks();
  paintFoot();
}

// setFull is m: the preview over the whole width (the TUI's ctrl+t).
function setFull(on) {
  pv.full = on;
  $("panes").classList.toggle("wtf-full", on);
  if (previewPath) paintPTitle(previewPath);
  pvLines.queue();
}

function pvLineHeight() {
  const row = wtfEl().querySelector(".vline");
  return row ? row.offsetHeight : 16;
}

// scrollPv moves the pager by n lines; the cursor stays where it is.
function scrollPv(n) {
  wtfEl().scrollTop += n * pvLineHeight();
}

function pvPageRows() {
  return Math.max(1, Math.floor(wtfEl().clientHeight / pvLineHeight()) - 1);
}

function stepCursor(delta) {
  const rows = pvRows();
  if (!rows) return;
  const [first, last] = pvLines.visible();
  pv.cur = wtCursorStep(pv.cur, delta, first, last, rows.length);
  paintMarks();
  rows[pv.cur].scrollIntoView({ block: "nearest" });
}

function markPress() {
  if (!pvRows()) return;
  pv.sel = wtSelPress(pv.sel, pv.cur);
  paintMarks();
  paintFoot();
}

function clearMarks() {
  pv.sel = null;
  paintMarks();
  paintFoot();
}

// copyMarked is enter with lines marked: their text, then the marks go.
function copyMarked() {
  const b = wtSelBounds(pv.sel, pv.cur);
  if (!b) return;
  const n = b[1] - b[0] + 1;
  copyText(pv.lines.slice(b[0], b[1] + 1).map((l) => l.text).join("\n"), n === 1 ? "line " + (b[0] + 1) : n + " lines");
  clearMarks();
}

// copyMarkedLink is L with lines marked: the content link to them.
function copyMarkedLink() {
  const b = wtSelBounds(pv.sel, pv.cur);
  if (!b || !previewPath) return;
  const ctx = { path: previewPath, state: "unstaged", hint: { kind: "view", id: "content" } };
  const link =
    b[0] === b[1]
      ? linkFor(state.repo, state.worktree, ctx, "new", b[0] + 1, pv.lines[b[0]].text)
      : linkFor(state.repo, state.worktree, ctx, "new", b[0] + 1, "", b[1] + 1, pv.lines.slice(b[0], b[1] + 1).map((l) => l.text));
  if (link) copyFileLink(previewPath, link);
  else opLine("▸ no gg link for this place", false);
}

// gotoPvLine is : — line n gets the cursor, centred; past the end lands on
// the last line and says so.
function gotoPvLine() {
  if (!pvRows()) return;
  openGotoLine((n) => {
    const rows = pvRows();
    if (!rows) return;
    if (n > rows.length) opLine("line " + n + " is past the end of " + previewPath + " (" + rows.length + " lines)", false);
    pv.cur = Math.min(Math.max(n, 1), rows.length) - 1;
    paintMarks();
    rows[pv.cur].scrollIntoView({ block: "center" });
  });
}

// ---- the preview's in-view search (the blame rule: re-find over the whole
// file, paint near the screen) ----
const pvSearch = new Search();

// The search over the preview's lines: linesearch.js, the file viewer's too.
const pvLines = lineSearchHost({
  search: pvSearch,
  body: () => wtfEl(),
  finish: () => wtfBody(), // every row: a search reads them all
  painting: () => isPainting(wtfEl()),
  rowSel: ".vline[data-i]",
  keyOf: (el) => Number(el.dataset.i),
  lines: () => pv.lines.map((l, i) => ({ row: i, text: l.text || "", ref: l })),
  paint: (el, hits, i) => {
    const l = pv.lines[i];
    el.lastElementChild.innerHTML = renderCell(l.text, null, l.tok, "", hits) || " ";
  },
  shown: () => wtf.on,
});
wtfEl().addEventListener("scroll", () => pvLines.queue(), { passive: true });
window.addEventListener("resize", () => pvLines.queue());

const pvSearchBar = bindSearchBar("wtf-psearch", {
  search: pvSearch,
  here: () => pvLines.here(),
  origin: () => ({ top: wtfEl().scrollTop, left: wtfEl().scrollLeft }),
  restore: (o) => {
    wtfEl().scrollTop = o.top;
    wtfEl().scrollLeft = o.left;
  },
  render: () => pvLines.render(),
  goTo: (i) => pvLines.goTo(i),
  focus: () => wtfEl().focus({ preventScroll: true }),
});

// previewKey: the preview holds the keys. TUI order: the marks first (space,
// enter, L, esc), then the search (/ @ ] [ and esc on a kept query), then
// the pager; ←/→ pan long lines; tab and a last esc give the keys back to
// the list. Every
// other key is swallowed — nothing reaches the page below.
function previewKey(e) {
  if (e.ctrlKey && !e.altKey && !e.metaKey && (e.code === "BracketRight" || e.key === "]")) {
    e.preventDefault();
    backgroundRow();
    return true;
  }
  if (isSwitcherKey(e)) {
    e.preventDefault();
    openSwitcher();
    return true;
  }
  if (e.altKey && !e.ctrlKey && !e.metaKey && (e.key === "ArrowDown" || e.key === "ArrowUp")) {
    e.preventDefault();
    stepCursor(e.key === "ArrowDown" ? 1 : -1);
    return true;
  }
  if (e.ctrlKey || e.metaKey || e.altKey) return true;
  const body = wtfEl();
  switch (e.key) {
    case " ": markPress(); break;
    case "Enter": if (!pv.sel) return true; copyMarked(); break;
    case "L": if (!pv.sel) return true; copyMarkedLink(); break;
    case "Escape":
      if (pv.sel) clearMarks();
      else if (pvSearch.active()) pvSearchBar.clear();
      else focusList();
      break;
    case "/": case "@": pvSearchBar.open(e.key === "@"); break;
    case "]": case "[": if (!pvSearch.query) return true; pvSearchBar.step(e.key === "]" ? 1 : -1); break;
    case "ArrowDown": case "j": scrollPv(1); break;
    case "ArrowUp": case "k": scrollPv(-1); break;
    case "PageDown": scrollPv(pvPageRows()); break;
    case "PageUp": scrollPv(-pvPageRows()); break;
    case "Home": body.scrollTop = 0; break;
    case "End": body.scrollTop = body.scrollHeight; break;
    case "ArrowLeft": case "ArrowRight":
      body.scrollLeft = wtPanStep(body.scrollLeft, e.key === "ArrowLeft" ? -1 : 1, PAN_STEP);
      break;
    case "Tab": focusList(); break;
    case ":": gotoPvLine(); break;
    case "m": setFull(!pv.full); break;
    case "w": cycleTextMode(); pvLines.queue(); break;
    default: return true;
  }
  e.preventDefault();
  return true;
}

// A click in the preview hands it the keys and puts the cursor on the
// clicked line; a click on the list takes them back.
preview.addEventListener("mousedown", (e) => {
  if (!wtf.on || e.target.closest("#wtf-psearch")) return;
  focusPreview();
  const row = e.target.closest(".vline[data-i]");
  if (row && pv.focus && pvRows()) {
    pv.cur = Number(row.dataset.i);
    paintMarks();
  }
});
root.addEventListener("mousedown", (e) => {
  if (wtf.on && !e.target.closest("#wtf-search")) focusList();
});

// paintFoot shows the keys of the side holding them.
function paintFoot() {
  if (!wtf.on) return;
  updateFoot("wtf", !pv.focus ? WTF_FOOT : pv.sel ? WTF_MFOOT : WTF_PFOOT);
}

// The row and title budgets are in COLUMNS, so a width change (a window
// resize, the rs-detail drag, the sidebar) re-cuts them — files.js's rule.
// rAF-coalesced (a drag fires per pixel); a hidden F has nothing to re-cut.
let wtfResizePending = false;
const wtfResize = new ResizeObserver(() => {
  if (wtfResizePending) return;
  wtfResizePending = true;
  requestAnimationFrame(() => {
    wtfResizePending = false;
    if (!wtf.on) return;
    const list = $("wtf-list");
    if (list.clientWidth) {
      const top = list.scrollTop;
      render();
      list.scrollTop = top;
    }
    if (previewPath) paintPTitle(previewPath);
  });
});
wtfResize.observe($("wtf-list"));
wtfResize.observe($("wtf-ptitle"));
// menuRows is a row's actions. view file, history and blame open OVER F (esc
// returns here); diff closes F — the page's diff lives in the diff stage.
function menuRows(f) {
  const rows = [];
  for (const a of wtActions(!!f.untracked)) {
    if (a === "view") {
      rows.push({ label: "view file", act: () => openViewer({ src: "worktree", path: f.path }) });
      // the TUI's ff-editor: the file itself, over F (the console's close comes back here)
      rows.push({ label: "edit in editor", act: () => openInEditor({ source: "worktree", path: f.path }, true) });
    }
    if (a === "diff") rows.push({ label: "diff (working tree changes)", act: () => { closeFinder(); openWorktreeFileDiff(f.path); } });
    if (a === "history") rows.push({ label: "file history", act: () => openFileHistory(f.path, "") });
    if (a === "blame") rows.push({ label: "blame", act: () => openFileBlame(f.path, "") });
    if (a === "copy") {
      rows.push({ sep: true }, ...copyPathRows(f.path));
      const flink = linkFor(state.repo, state.worktree, { path: f.path, state: "unstaged", hint: { kind: "view", id: "content" } }, "new", 0);
      if (flink) rows.push({ label: "copy file link", act: () => copyFileLink(f.path, flink) });
    }
  }
  return rows;
}

function menuAtCursor() {
  const f = selected();
  if (!f) return;
  const el = $("wtf-list").querySelector(`li[data-i="${wtf.sel}"]`);
  const r = el ? el.getBoundingClientRect() : null;
  showCtxMenu(menuRows(f), r ? r.left + 24 : 80, r ? r.bottom : 80);
}

// backgroundRow opens the row's file in the background (5b's open with no
// tab — the op 5c's agent verb uses): it joins the ctrl+\ list, no tab shows
// it, F stays up.
async function backgroundRow() {
  // From the preview: the file it shows (the TUI's wtBackgroundPreview) —
  // the list's row may already be a newer one still settling.
  const path = pv.focus && previewPath ? previewPath : (selected() || {}).path;
  if (!path) return;
  let ans;
  try {
    ans = await postJSON("/api/open-files", { op: "open", src: "worktree", rev: "", path, line: 0, tab: "" });
  } catch (e) {
    opLine("background failed: " + (e.message || e), true);
    return;
  }
  opLine(path + " opened in the background" + (ans.evicted ? " — " + evictedText(ans.evicted, ans.cap) : ""), false);
}

function openSearch() {
  $("wtf-search").classList.remove("hidden");
  $("wtf-input").focus();
}

function leaveSearch() {
  $("wtf-input").blur();
  paintSearch();
}

// inputKey: a key typed into the filter field. Typing filters (debounced by
// the input listener); ↑↓ still move; enter keeps the query, esc clears it.
function inputKey(e) {
  // alt+↑ / alt+↓ recall an earlier query (the TUI's file-finder ring).
  if (recallKey(e, $("wtf-input"), "filetree", (v) => { $("wtf-input").value = v; setQuery(v); })) return true;
  switch (e.key) {
    case "Escape":
      e.preventDefault();
      leaveSearch();
      setQuery("");
      return true;
    case "Enter":
      e.preventDefault();
      recordSearch("filetree", $("wtf-input").value);
      setQuery($("wtf-input").value);
      leaveSearch();
      return true;
    case "ArrowDown":
      e.preventDefault();
      move(1);
      return true;
    case "ArrowUp":
      e.preventDefault();
      move(-1);
      return true;
    case "PageDown":
    case "PageUp":
      e.preventDefault();
      move(e.key === "PageDown" ? pageRows() : -pageRows());
      return true;
  }
  return true; // the field's own letter
}

// finderKey: F owns the keyboard. Keys with ctrl/meta/alt that F does not
// use are swallowed WITHOUT preventDefault (the browser's own keys pass);
// every other unused key is swallowed too — nothing reaches the page below.
function finderKey(e) {
  if (e.target === $("wtf-input")) return inputKey(e);
  if (e.target === $("wtf-psearch-input")) return true; // the bar's own listener takes it
  if (pv.focus) return previewKey(e);
  if (e.ctrlKey && !e.altKey && !e.metaKey && (e.code === "BracketRight" || e.key === "]")) {
    e.preventDefault();
    backgroundRow();
    return true;
  }
  if (isSwitcherKey(e)) {
    e.preventDefault();
    openSwitcher();
    return true;
  }
  if (e.ctrlKey || e.metaKey || e.altKey) return true;
  switch (e.key) {
    case "ArrowDown": case "j": move(1); break;
    case "ArrowUp": case "k": move(-1); break;
    case "PageDown": move(pageRows()); break;
    case "PageUp": move(-pageRows()); break;
    case "Home": move(-wtf.rows.length); break;
    case "End": move(wtf.rows.length); break;
    case "/": openSearch(); break;
    case "Enter": focusPreview(); break;
    case ".": menuAtCursor(); break;
    case "ArrowRight": case "Tab": focusPreview(); break;
    case "Escape":
      if (wtf.query) setQuery("");
      else closeFinder();
      break;
    default: return true;
  }
  e.preventDefault();
  return true;
}

$("wtf-input").addEventListener("input", () => {
  clearTimeout(queryTimer);
  const q = $("wtf-input").value;
  queryTimer = setTimeout(() => setQuery(q), 120);
});
$("wtf-input").addEventListener("blur", paintSearch);

$("wtf-list").addEventListener("scroll", () => {
  const l = $("wtf-list");
  if (l.scrollTop + l.clientHeight >= l.scrollHeight - 40) loadMore();
});

function rowAt(e) {
  const li = e.target.closest("li[data-i]");
  if (!li) return null;
  const i = Number(li.dataset.i);
  if (i !== wtf.sel) {
    wtf.sel = i;
    paintSel();
    cursorMoved();
  }
  return wtf.rows[i] || null;
}

$("wtf-list").addEventListener("click", (e) => {
  leaveSearch();
  rowAt(e);
});
$("wtf-list").addEventListener("dblclick", (e) => {
  const f = rowAt(e);
  if (f) openViewer({ src: "worktree", path: f.path });
});
$("wtf-list").addEventListener("contextmenu", (e) => {
  const f = rowAt(e);
  if (!f) return;
  e.preventDefault();
  showCtxMenu(menuRows(f), e.clientX, e.clientY);
});

// The bottom bar while F is up. ctrl+\\ is escaped: "\ " is a space.
const WTF_FOOT =
  `<span>↑↓ j k move</span><button data-wact="find">/ filter</button><button data-wact="menu">. actions</button>` +
  `<button data-wact="preview">enter → preview</button>` +
  `<button data-wact="bg">ctrl+] background</button><button data-wact="files">ctrl+\\ open files</button><button data-wact="close">esc close</button>`;
// …while the preview holds the keys (the TUI's "file:" bar; m and w stand in
// for ctrl+t and ctrl+w, which the browser keeps)…
const WTF_PFOOT =
  `<span>↑↓ scroll</span><span>alt+↑↓ cursor</span><button data-wact="mark">space mark</button><button data-wact="pfind">/ find</button>` +
  `<button data-wact="line">: line</button><button data-wact="full">m full</button><button data-wact="view">w view</button>` +
  `<button data-wact="bg">ctrl+] background</button><span>←→ pan</span><button data-wact="list">tab esc back to list</button>`;
// …and while lines are marked.
const WTF_MFOOT =
  `<button data-wact="mark">space mark end</button><button data-wact="copy">enter copy</button><button data-wact="link">L link</button>` +
  `<button data-wact="unmark">esc unmark</button><span>alt+↑↓ extend</span>`;

$("foot").addEventListener("click", (e) => {
  const main = e.target.closest('button[data-act="finder"]');
  if (main) return openFinder();
  const b = e.target.closest("button[data-wact]");
  if (!b || !wtf.on) return;
  switch (b.dataset.wact) {
    case "find": openSearch(); break;
    case "menu": menuAtCursor(); break;
    case "preview": focusPreview(); break;
    case "list": focusList(); break;
    case "mark": markPress(); break;
    case "copy": copyMarked(); break;
    case "link": copyMarkedLink(); break;
    case "unmark": clearMarks(); break;
    case "pfind": pvSearchBar.open(false); break;
    case "line": gotoPvLine(); break;
    case "full": setFull(!pv.full); break;
    case "view": cycleTextMode(); pvLines.queue(); break;
    case "bg": backgroundRow(); break;
    case "files": openSwitcher(); break;
    case "close": closeFinder(); break;
  }
});

// A layer closed from outside (another surface clearing the stack) must not
// leave the panes taken: the class follows the stack on every key.
document.addEventListener("keyup", () => {
  if (wtf.on && root.classList.contains("hidden")) closeFinder();
  else if (!wtf.on && footOwned("wtf")) popFoot("wtf");
});

// F from the page: the old finder's guards — a layer owns the keyboard, a
// focused field owns every key it can type.
document.addEventListener("keydown", (e) => {
  if (topLayer()) return;
  if (e.target.closest && e.target.closest("input,textarea")) return;
  if (e.key === "F" && !e.ctrlKey && !e.metaKey && !e.altKey) {
    e.preventDefault();
    openFinder();
  }
});

registerHelp({
  key: "F",
  html:
    "<b>files in the working tree</b> — the file list shows every file on disk (untracked ones marked); " +
    "<b>/</b> filters (fuzzy; <b>'exact</b> <b>^prefix</b> <b>suffix$</b> <b>!not</b>, space-separated terms all apply), the diff pane previews the file under the cursor, <b>.</b> its actions " +
    "(view file, diff, history, blame, copy), <b>ctrl+]</b> opens it in the background, <b>esc</b> gives the panes back. " +
    "<b>enter</b>, <b>→</b>, <b>tab</b> or a click hand the keys to the preview: ↑↓ and the page keys scroll, <b>alt+↑↓</b> move a line cursor, " +
    "<b>space</b> marks lines (<b>enter</b> copies them, <b>L</b> copies their link, <b>esc</b> unmarks), <b>/ @ ] [</b> find, <b>:</b> goes to a line, " +
    "<b>m</b> fills the screen, <b>w</b> cycles long lines, <b>← →</b> pan them; <b>tab</b>, <b>esc</b> or a click on the list take them back",
});

export { closeFinder, finderOn, openFinder };
