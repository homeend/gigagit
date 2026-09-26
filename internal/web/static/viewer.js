// viewer.js — the file viewer overlay (open files on the web, plan 5a): one
// file at one version — the working tree, a commit, a shelf entry — with a
// line cursor, the in-view search and a . menu. Content links land here.
import { $, charWidth, elidePath, esc, getJSON, state } from "./core.js";
import { closeLayer, copyText, mountOverlay, pushLayer, showCtxMenu, topLayer } from "./layers.js";
import { Search } from "./inviewsearch.js";
import { bindSearchBar } from "./searchbar.js";
import { cycleTextMode, openFile, openWorkingTree, renderCell } from "./files.js";
import { opLine } from "./ops.js";
import { copyFileLink, copyLink, linkDesc, linkFor } from "./links.js";
import { openFileBlame, openFileHistory } from "./filehist.js";
import { openCommitByHash } from "./commits.js";
import { registerHelp, registerRows } from "./menus.js";

// --- viewer model (pure; guarded against Go) ---
function clampLine(n, count) {
  if (count <= 0) return 0;
  return Math.min(Math.max(n, 1), count);
}

// landLine is where a link's line lands: past the end clamps to the last
// line and says so (the TUI's words).
function landLine(want, count, path) {
  if (want <= 0) return { line: count > 0 ? 1 : 0, notice: "" };
  if (want > count) return { line: count, notice: "line " + want + " is past the end of " + path + " (" + count + " lines)" };
  return { line: want, notice: "" };
}

// sameLines reports whether two line lists hold the same text — how a
// commit or shelf version decides it may copy a content link (the TUI's
// disk-match rule).
function sameLines(a, b) {
  if (a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) if (a[i].text !== b[i].text) return false;
  return true;
}

function versionLabel(src, rev) {
  if (src === "commit") return "@ " + String(rev || "").slice(0, 7);
  if (src === "shelf") return "shelf";
  return "working tree";
}
// --- end viewer model ---

// --- the overlay -------------------------------------------------------------
// view is the ONE file on screen: its version, its lines, the cursor (1-based,
// 0 = no line) and the placeholder shown instead of lines ("" = none).
const view = { src: "worktree", rev: "", path: "", lines: [], cur: 0, placeholder: "" };
const viewerSearch = new Search();

// The markup is built at import: bindSearchBar needs its bar in the DOM.
const viewerRoot = mountOverlay("viewer");
viewerRoot.innerHTML =
  `<div id="viewer-box"><div id="viewer-title"></div>` +
  `<div id="viewer-search" class="hidden search-bar"><span id="viewer-search-lead">/</span>` +
  `<input id="viewer-search-input" type="text" autocomplete="off" spellcheck="false" placeholder="find in this file — enter keeps it, ] [ step, esc clears">` +
  `<span id="viewer-search-count"></span></div>` +
  `<div id="viewer-body" tabindex="-1"></div></div>`;

viewerRoot.addEventListener("click", (e) => {
  if (e.target.id === "viewer") closeViewer(); // backdrop closes, box does not
});
$("viewer-body").addEventListener("click", (e) => {
  const row = e.target.closest(".vline[data-i]");
  if (!row) return;
  view.cur = Number(row.dataset.i) + 1;
  paintCursor();
});
$("viewer-body").addEventListener("contextmenu", (e) => {
  const row = e.target.closest(".vline[data-i]");
  if (row) view.cur = Number(row.dataset.i) + 1;
  e.preventDefault();
  paintCursor();
  openViewerMenu(e.clientX, e.clientY);
});

// openViewer shows path at one version, the cursor on line (0 = the first).
// A second call while it is open replaces the file (one viewer, one file).
async function openViewer({ src = "worktree", rev = "", path, line = 0 }) {
  let body;
  try {
    body = await getJSON("/api/file-content?src=" + encodeURIComponent(src) + "&rev=" + encodeURIComponent(rev) + "&path=" + encodeURIComponent(path));
  } catch (e) {
    opLine("view failed: " + (e.message || e), true);
    return { ok: false, notice: "" };
  }
  Object.assign(view, { src, rev, path, lines: body.lines || [] });
  view.placeholder = body.missing ? "(file deleted on disk)" : body.too_large ? "(file too large to preview)" : view.lines.length ? "" : "(empty file)";
  const landed = landLine(line, view.lines.length, path);
  view.cur = landed.line;
  viewerSearchBar.reset(); // a new file is a new search
  viewerRoot.style.bottom = $("foot").offsetHeight + "px"; // the bar stays in sight
  pushLayer("viewer", viewerRoot, { onKey: viewerKey });
  swapFoot(true);
  paintTitle();
  renderViewer();
  centerCursor();
  $("viewer-body").focus({ preventScroll: true });
  if (landed.notice) opLine(landed.notice, false);
  return { ok: true, notice: landed.notice };
}

function closeViewer() {
  closeLayer("viewer");
  swapFoot(false);
}

// paintTitle cuts the PATH in the middle, never the file name, to fit.
function paintTitle() {
  const el = $("viewer-title");
  const lead = "View ", tail = " (" + versionLabel(view.src, view.rev) + ")";
  const cols = Math.floor((el.clientWidth - 28) / charWidth()) - lead.length - tail.length;
  el.title = view.path;
  el.textContent = lead + (cols > 0 ? elidePath(view.path, cols) : view.path) + tail;
}

function renderViewer() {
  if (viewerSearch.query) viewerSearch.refind(view.lines.map((l, i) => ({ row: i, side: 0, text: l.text || "" })));
  const body = $("viewer-body");
  if (view.placeholder) {
    body.innerHTML = `<div class="notice">${esc(view.placeholder)}</div>`;
  } else {
    let html = "";
    view.lines.forEach((l, i) => {
      html +=
        `<div class="vline${i + 1 === view.cur ? " vcur" : ""}" data-i="${i}"><span class="vno">${i + 1}</span>` +
        `<span class="vtext">${renderCell(l.text, null, l.tok, "", viewerSearch.query ? viewerSearch.hitsOn(i, 0) : null) || " "}</span></div>`;
    });
    body.innerHTML = html;
  }
  viewerSearchBar.paint();
}

function cursorRow() {
  return $("viewer-body").querySelector(`.vline[data-i="${view.cur - 1}"]`);
}

function paintCursor() {
  for (const el of $("viewer-body").querySelectorAll(".vcur")) el.classList.remove("vcur");
  const row = cursorRow();
  if (row) {
    row.classList.add("vcur");
    row.scrollIntoView({ block: "nearest" });
  }
}

function centerCursor() {
  const row = cursorRow();
  if (row) row.scrollIntoView({ block: "center" });
  else $("viewer-body").scrollTop = 0;
}

function pageRows() {
  const row = $("viewer-body").querySelector(".vline");
  return row ? Math.max(1, Math.floor($("viewer-body").clientHeight / row.offsetHeight) - 1) : 10;
}

function moveCursor(delta) {
  view.cur = clampLine(view.cur + delta, view.lines.length);
  paintCursor();
}

function menuAtCursor() {
  const r = cursorRow()?.getBoundingClientRect();
  openViewerMenu(r ? r.left + 40 : 80, r ? r.bottom : 80);
}

function viewerKey(e) {
  // A key typed into the search bar is the query's (its own listener takes
  // enter and esc).
  if (e.target === $("viewer-search-input")) return true;
  if (e.ctrlKey || e.metaKey || e.altKey) return false;
  if (viewerSearchKey(e)) return true;
  switch (e.key) {
    case "ArrowDown": case "j": moveCursor(1); break;
    case "ArrowUp": case "k": moveCursor(-1); break;
    case "PageDown": case " ": moveCursor(pageRows()); break;
    case "PageUp": moveCursor(-pageRows()); break;
    case "Home": case "g": view.cur = clampLine(1, view.lines.length); paintCursor(); break;
    case "End": case "G": view.cur = view.lines.length; paintCursor(); break;
    case "w": cycleTextMode(); break;
    case ".": menuAtCursor(); break;
    case "Escape": closeViewer(); break;
    default: return false;
  }
  e.preventDefault();
  return true;
}

// ---- in-view search (the blame overlay's, over the viewer's lines) ----------
function viewerHitEls(i) {
  return $("viewer-body").querySelectorAll(`.hit[data-h="${i}"]`);
}

function goToViewerHit(i) {
  if (i < 0 || i >= viewerSearch.hits.length) return;
  if (viewerSearch.cur !== i) {
    for (const el of viewerHitEls(viewerSearch.cur)) el.classList.remove("cur");
    viewerSearch.cur = i;
    for (const el of viewerHitEls(i)) el.classList.add("cur");
  }
  const el = viewerHitEls(i)[0];
  if (el) el.scrollIntoView({ block: "center", inline: "nearest" });
  // The cursor follows the hit: the . menu then acts on the line found.
  const h = viewerSearch.hits[i];
  if (h) {
    view.cur = h.row + 1;
    paintCursor();
  }
}

const viewerSearchBar = bindSearchBar("viewer-search", {
  search: viewerSearch,
  here: () => ({ row: Math.max(0, view.cur - 1), side: 0, col: -1 }),
  origin: () => ({ top: $("viewer-body").scrollTop, left: $("viewer-body").scrollLeft, cur: view.cur }),
  restore: (o) => {
    $("viewer-body").scrollTop = o.top;
    $("viewer-body").scrollLeft = o.left;
    view.cur = o.cur;
    paintCursor();
  },
  render: () => {
    const body = $("viewer-body");
    const top = body.scrollTop, left = body.scrollLeft;
    renderViewer();
    body.scrollTop = top;
    body.scrollLeft = left;
  },
  goTo: goToViewerHit,
  focus: () => $("viewer-body").focus({ preventScroll: true }),
});

function viewerSearchKey(e) {
  if (e.key === "/" || e.key === "@") {
    e.preventDefault();
    viewerSearchBar.open(e.key === "@");
    return true;
  }
  if (e.key === "]" || e.key === "[") {
    if (!viewerSearch.query) return false;
    e.preventDefault();
    viewerSearchBar.step(e.key === "]" ? 1 : -1);
    return true;
  }
  if (e.key === "Escape" && viewerSearch.active()) {
    viewerSearchBar.clear();
    return true;
  }
  return false;
}

// ---- the bottom bar -------------------------------------------------------
// The viewer's keys go in the app's footer, not inside the box: while the
// viewer is open #foot shows them, and gets its own chips back on close.
let savedFoot = null;
const VIEWER_FOOT =
  `<span>↑↓ j k line</span><button data-vact="find">/ find</button><span>] [ next / prev</span>` +
  `<button data-vact="wrap">w long lines</button><button data-vact="menu">. menu</button><button data-vact="close">esc close</button>`;

function swapFoot(on) {
  const foot = $("foot");
  if (on && savedFoot === null) {
    savedFoot = foot.innerHTML;
    foot.innerHTML = VIEWER_FOOT;
  } else if (!on && savedFoot !== null) {
    foot.innerHTML = savedFoot;
    savedFoot = null;
  }
}

$("foot").addEventListener("click", (e) => {
  const b = e.target.closest("button[data-vact]");
  if (!b) return;
  switch (b.dataset.vact) {
    case "find": viewerSearchBar.open(false); break;
    case "wrap": cycleTextMode(); break;
    case "menu": menuAtCursor(); break;
    case "close": closeViewer(); break;
  }
});

// A layer closed from outside (another surface clearing the stack) must not
// leave the viewer's chips behind: the footer follows the stack on every key.
document.addEventListener("keyup", () => {
  if (savedFoot !== null && !(topLayer() && topLayer().id === "viewer") && viewerRoot.classList.contains("hidden")) swapFoot(false);
});

// openViewerMenu is the viewer's . menu (and right-click): the content link
// and the text of the cursor line, then the file's diff, history and blame at
// this version. The surfaces it opens replace the viewer — one full-page
// overlay at a time.
function openViewerMenu(x, y) {
  const line = view.placeholder ? 0 : view.cur;
  const items = [];
  const flink = linkFor(state.repo, state.worktree, { path: view.path, state: "unstaged", hint: { kind: "view", id: "content" } }, "new", line);
  if (flink) items.push({ label: "copy file link" + (line ? " (line " + line + ")" : ""), act: () => copyViewerLink(flink) });
  if (line && view.lines[line - 1]) items.push({ label: "copy line", act: () => copyText(view.lines[line - 1].text, "line " + line) });
  items.push({ sep: true });
  if (view.src === "worktree") items.push({ label: "diff (HEAD ↔ working tree)", act: () => viewerDiffWorktree(view.path) });
  if (view.src === "commit") items.push({ label: "diff (this commit's change)", act: () => viewerDiffCommit(view.rev, view.path) });
  if (view.src !== "shelf") {
    const rev = view.src === "commit" ? view.rev : "";
    const path = view.path;
    items.push({ label: "file history", act: () => { closeViewer(); openFileHistory(path, rev); } });
    items.push({ label: "blame", act: () => { closeViewer(); openFileBlame(path, rev); } });
  }
  showCtxMenu(items, x, y);
}

// copyViewerLink copies the content link: a working-tree version after the
// shared presence check; a commit or shelf version only when the file on disk
// holds exactly the lines shown — a content link names the disk.
async function copyViewerLink(flink) {
  if (view.src === "worktree") return copyFileLink(view.path, flink);
  let disk;
  try {
    disk = await getJSON("/api/file-content?path=" + encodeURIComponent(view.path));
  } catch (e) {
    return opLine("copy failed: " + (e.message || e), true);
  }
  if (disk.missing || disk.too_large || !sameLines(disk.lines || [], view.lines)) {
    return opLine("the file on disk differs from this version — no content link", true);
  }
  copyLink(flink, linkDesc("file", view.path, ""));
}

// viewerDiffWorktree opens the file's working-tree diff the way a click on its
// row does; a file with no change says so.
async function viewerDiffWorktree(path) {
  closeViewer();
  await openWorkingTree(0);
  const i = state.statusEntries.findIndex((f) => f.path === path && f.section !== "staged");
  if (i < 0) return opLine(path + " has no changes in the working tree", false);
  await openFile(i);
}

// viewerDiffCommit opens the commit and the file's row in it.
async function viewerDiffCommit(rev, path) {
  closeViewer();
  if (!(await openCommitByHash(rev, rev.slice(0, 8)))) return;
  const i = state.files.findIndex((f) => f.path === path);
  if (i < 0) return opLine(path + " is not changed in " + rev.slice(0, 8), false);
  await openFile(i);
}

// ---- entry points ---------------------------------------------------------
// view file: the version the row shows — a commit row at its own revision, a
// working-tree row the bytes on disk. A row with no bytes (deleted there)
// offers none.
registerRows("fileview", (ctx) => {
  if (!ctx.path || ctx.deleted) return [];
  if (ctx.section === "commit") {
    return ctx.sha ? [{ label: "view file", act: () => openViewer({ src: "commit", rev: ctx.sha, path: ctx.path }) }] : [];
  }
  return [{ label: "view file", act: () => openViewer({ src: "worktree", path: ctx.path }) }];
});

registerRows("shelf", (e) =>
  e.kind !== "commit" && e.path ? [{ label: "view file", act: () => openViewer({ src: "shelf", rev: e.id, path: e.path }) }] : []
);

registerHelp({
  key: "view file",
  html:
    "a file row's or a shelved file's <b>view file</b> (right-click / <b>.</b>), or a pasted content link, opens the file full-page: " +
    "<b>↑↓ j k</b> line, <b>/ ] [</b> find, <b>w</b> long lines, <b>.</b> menu (copy file link at the line, copy line, diff, history, blame), <b>esc</b> close",
});

export { closeViewer, openViewer };
