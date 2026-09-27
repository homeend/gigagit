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
import { $, charWidth, elidePath, esc, getJSON, postJSON, state } from "./core.js";
import { closeLayer, footOwned, popFoot, pushFoot, pushLayer, showCtxMenu, topLayer } from "./layers.js";
import { copyPathRows, renderCell } from "./files.js";
import { copyFileLink, linkFor } from "./links.js";
import { openFileBlame, openFileHistory } from "./filehist.js";
import { openViewer, openWorktreeFileDiff } from "./viewer.js";
import { opLine } from "./ops.js";
import { isSwitcherKey, openSwitcher } from "./openfiles.js";
import { registerHelp } from "./menus.js";

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
  return n ? "" : "(empty file)";
}

// wtActions is the action list of a row (enter / . / right-click): the git
// rows only for a tracked file — an untracked one has no history (the TUI's
// worktreeFileRows).
function wtActions(untracked) {
  return untracked ? ["view", "copy"] : ["view", "diff", "history", "blame", "copy"];
}
// --- end finder model ---

// wtf is the finder while it is up: its rows (one page per unfiltered load,
// or the ranked matches), the cursor, the query, the next page's offset and
// the server read the pages were cut from.
const wtf = { on: false, loading: false, rows: [], sel: 0, query: "", next: 0, gen: 0, total: 0, paging: false };
let reqSeq = 0; // bumped by every list request: a later one wins
let queryTimer = null;

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
preview.innerHTML = `<div id="wtf-ptitle"></div><div id="wtf-body" tabindex="-1"></div>`;
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
  $("panes").classList.remove("wtf");
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
    paintPreview("", [], "");
    return;
  }
  if (f.path === previewPath) return;
  previewTimer = setTimeout(() => showPreview(gen, f.path), WT_SETTLE_MS);
}

function closedHook() {
  previewGen++;
  clearTimeout(previewTimer);
  paintPreview("", [], "");
}

// showPreview reads path and paints it; keep (a reload of the file already
// shown, at stamp) holds the preview's scroll where it was.
async function showPreview(gen, path, keep, stamp) {
  let body;
  try {
    body = await getJSON("/api/file-content?src=worktree&path=" + encodeURIComponent(path));
  } catch (e) {
    if (!wtf.on || !wtPreviewFresh(gen, previewGen, path, (selected() || {}).path)) return;
    previewStamp = keep ? stamp : ""; // a failed reload takes the stamp it saw: no retry per tick
    paintPreview(path, [], "(load failed: " + (e.message || e) + ")", keep);
    return;
  }
  if (!wtf.on || !wtPreviewFresh(gen, previewGen, path, (selected() || {}).path)) return;
  previewStamp = body.stamp || "";
  const lines = body.lines || [];
  paintPreview(path, lines, wtPlaceholder(body, lines.length), keep);
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
function paintPreview(path, lines, placeholder, keep) {
  previewPath = path;
  if (!path) previewStamp = "";
  paintPTitle(path);
  const body = $("wtf-body");
  const at = { top: body.scrollTop, left: body.scrollLeft };
  if (!path) body.innerHTML = "";
  else if (placeholder) body.innerHTML = `<div class="notice">${esc(placeholder)}</div>`;
  else
    body.innerHTML = lines
      .map((l, i) => `<div class="vline"><span class="vno">${i + 1}</span><span class="vtext">${renderCell(l.text, null, l.tok, "", null) || " "}</span></div>`)
      .join("");
  body.scrollTop = keep ? at.top : 0;
  body.scrollLeft = keep ? at.left : 0;
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
    if (a === "view") rows.push({ label: "view file", act: () => openViewer({ src: "worktree", path: f.path }) });
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
  const f = selected();
  if (!f) return;
  let ans;
  try {
    ans = await postJSON("/api/open-files", { op: "open", src: "worktree", rev: "", path: f.path, line: 0, tab: "" });
  } catch (e) {
    opLine("background failed: " + (e.message || e), true);
    return;
  }
  opLine(f.path + " opened in the background" + (ans.evicted ? " — closed " + ans.evicted + " (20 files open)" : ""), false);
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
  switch (e.key) {
    case "Escape":
      e.preventDefault();
      leaveSearch();
      setQuery("");
      return true;
    case "Enter":
      e.preventDefault();
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
  }
  return true; // the field's own letter
}

// finderKey: F owns the keyboard. Keys with ctrl/meta/alt that F does not
// use are swallowed WITHOUT preventDefault (the browser's own keys pass);
// every other unused key is swallowed too — nothing reaches the page below.
function finderKey(e) {
  if (e.target === $("wtf-input")) return inputKey(e);
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
    case "Enter": case ".": menuAtCursor(); break;
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
  `<span>↑↓ j k move</span><button data-wact="find">/ filter</button><button data-wact="menu">enter . actions</button>` +
  `<button data-wact="bg">ctrl+] background</button><button data-wact="files">ctrl+\\ open files</button><button data-wact="close">esc close</button>`;

$("foot").addEventListener("click", (e) => {
  const main = e.target.closest('button[data-act="finder"]');
  if (main) return openFinder();
  const b = e.target.closest("button[data-wact]");
  if (!b || !wtf.on) return;
  switch (b.dataset.wact) {
    case "find": openSearch(); break;
    case "menu": menuAtCursor(); break;
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
    "<b>/</b> filters fuzzily, the diff pane previews the file under the cursor, <b>enter</b> / <b>.</b> its actions " +
    "(view file, diff, history, blame, copy), <b>ctrl+]</b> opens it in the background, <b>esc</b> gives the panes back",
});

export { closeFinder, finderOn, openFinder };
