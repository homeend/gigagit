// viewer.js — the file viewer overlay (open files on the web, plan 5a): one
// file at one version — the working tree, a commit, a shelf entry — with a
// line cursor, the in-view search and a . menu. Content links land here.
import { $, charWidth, elidePath, esc, fmtBytes, getJSON, postJSON, state, tabId } from "./core.js";
import { closeLayer, copyText, footOwned, mountOverlay, popFoot, pushFoot, pushLayer, showCtxMenu, topLayer } from "./layers.js";
import { Search } from "./inviewsearch.js";
import { bindSearchBar } from "./searchbar.js";
import { collapseDiffRows, cycleTextMode, openFile, openWorkingTree, renderCell } from "./files.js";
import { diffHead, diffNotice, diffSource, lineRows, nextNoteRow, noteAtRow, placeNotes, rowBand, rowNewLine, rowOfLine, sameContent, unifiedRows } from "./splitdiff.js";
import { mdHTML } from "./markdown.js";
import { saveUI } from "./uistate.js";
import { SPLIT_STACK_MAX_IN_FLIGHT, anchorPath, newSlots, nextHeader, placeholderRows, restack, shows, slotOf, stackBands, stackEnd, stackFiles, stackMove, stackTitle, stepStackBand } from "./splitstack.js";
import { clearOpLine, opLine } from "./ops.js";
import { copyFileLink, copyLink, linkDesc, linkFor } from "./links.js";
import { openFileBlame, openFileHistory } from "./filehist.js";
import { openCommitByHash } from "./commits.js";
import { registerHelp, registerRows } from "./menus.js";
import { isSwitcherKey, openSwitcher } from "./openfiles.js";
import { closeConsole } from "./console.js";
import { closeFinder } from "./wtfinder.js";

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
  if (src === "overview") return "overview";
  return "working tree";
}
// pickLine is where a (re)opened file lands (ruling L8): a link's line, else
// where this tab left it, else the server's last line, else 0 (= line 1).
function pickLine(link, place, server) {
  if (link > 0) return link;
  if (place && place.cur > 0) return place.cur;
  return server > 0 ? server : 0;
}

// keepLine is the cursor after a reload (ruling L9): clamped to the new
// length — but kept as is while the deleted placeholder shows, for when the
// file returns.
function keepLine(cur, count, placeholder) {
  if (placeholder) return cur;
  return clampLine(cur > 0 ? cur : 1, count);
}
// --- end viewer model ---

// --- note model (pure; guarded against Go) ---
// An agent's temporary notes on the file (agentdocs): {id, start, end,
// summary, rationale, author, outdated, ref}, lines 1-based.
function noteBoxTitle(n) {
  const at = n.start === n.end ? "line " + n.start : "lines " + n.start + "–" + n.end;
  if (n.readonly) return "review · " + n.author + " · " + at; // a review's remark: its id is internal
  return "note " + n.id + " · " + n.author + " · " + at + (n.outdated ? " · outdated" : "");
}

// noteAtLine is the first note covering line, or null.
function noteAtLine(notes, line) {
  return notes.find((n) => n.start <= line && line <= n.end) || null;
}

// boxesAfter is the notes whose box hangs under 0-based line i: those that
// end on it.
function boxesAfter(notes, i) {
  return notes.filter((n) => n.end - 1 === i);
}

// nextNoteLine is the start of the next (dir > 0) or previous note strictly
// past cur; 0 = none (the TUI's } / { inside one file).
function nextNoteLine(notes, cur, dir) {
  let hit = 0;
  for (const n of notes) {
    if (dir > 0 && n.start > cur) return n.start;
    if (dir < 0 && n.start < cur) hit = n.start;
  }
  return hit;
}

// nextNotedFile is the next (previous) OTHER open file with notes, in the
// order the files were opened — their id numbers, as the TUI orders by seq
// — wrapping around; "" = none.
function nextNotedFile(files, mine, dir) {
  const num = (id) => Number(String(id).slice(1));
  const others = files.filter((f) => f.notes > 0 && f.id !== mine).sort((a, b) => num(a.id) - num(b.id));
  if (!others.length) return "";
  if (dir > 0) return (others.find((f) => num(f.id) > num(mine)) || others[0]).id;
  const before = others.filter((f) => num(f.id) < num(mine));
  return (before.length ? before[before.length - 1] : others[others.length - 1]).id;
}
// mergeNotes is every box a file draws: an agent's notes, then a review's
// remarks, by start line — an agent's note first on a tie (sort is stable).
function mergeNotes(notes, remarks) {
  if (!remarks || !remarks.length) return notes;
  return [...notes, ...remarks].sort((a, b) => a.start - b.start);
}
// --- end note model ---

// --- overview model (pure; guarded against Go) ---
// An agent's overview (agentdocs): markdown whose links are anchors —
// {dest, path, start, end, note, missing, ref}, in document order.

// stepAnchor is tab (dir 1) / shift+tab (-1) from sel (-1 = none),
// wrapping; -1 when there are none. The next anchor that can open — a
// plain one (a stored overview's unresolved anchor) is text, passed over.
function stepAnchor(anchors, sel, dir) {
  const n = anchors.length;
  if (!n) return -1;
  let i = sel < 0 ? (dir > 0 ? 0 : n - 1) : (((sel + dir) % n) + n) % n;
  for (let k = 0; k < n; k++, i = (((i + dir) % n) + n) % n) if (!anchors[i].plain) return i;
  return -1;
}

// anchorStatus is what opening a missing anchor says ("" = it opens).
function anchorStatus(a) {
  if (a.plain) return "anchor " + a.dest + " does not resolve at the reviewed commit";
  if (!a.missing) return "";
  return a.note ? "note " + a.note + " is gone" : "no file " + a.path;
}

// storedAnchorOpen is where a stored overview's anchor opens: the file at
// the reviewed tip, or — a working review (tip "") — the working tree.
function storedAnchorOpen(tip, a) {
  return { src: tip ? "commit" : "worktree", rev: tip || "", path: a.path, line: a.start || 0 };
}

// anchorTarget is where an anchor lands: a file at a line (0 = its top) to
// end, or a note — whose file and lines the server fills while it lives.
function anchorTarget(a) {
  if (a.note && !a.path) return { note: a.note };
  const t = { path: a.path, line: a.start || 0, end: a.end || 0 };
  if (a.note) t.note = a.note;
  return t;
}

// keepAnchor is the selection after the text changed: the anchor at the same
// place with the same destination, else the first with it, else none.
function keepAnchor(next, want, sel) {
  if (!want) return -1;
  if (next[sel] && next[sel].dest === want) return sel;
  return next.findIndex((a) => a.dest === want);
}
// backAnchor is the anchor backspace returns to: the one with the
// destination it left from (a set may have moved it), else the same place
// while it is in range, else none.
function backAnchor(anchors, from) {
  const i = anchors.findIndex((a) => a.dest === from.dest);
  if (i >= 0) return i;
  return from.sel < anchors.length ? from.sel : -1;
}

// escHow is how esc (and a click on the backdrop) leaves a document: an
// overview or a file with an agent's notes steps aside, any other closes.
function escHow(isOverview, noteCount) {
  return isOverview || noteCount > 0 ? "background" : "close";
}

// docName is how a status line names the document v shows: an overview by
// its id and title (the agent's words — the server's OverviewName), a file
// by its path.
function docName(v) {
  if (v.ov && v.ov.stored) return "overview of review " + v.ov.stored.id.slice("review-overview:".length);
  return v.ov ? "overview " + v.id + " " + JSON.stringify(v.ov.title) : v.path;
}

// backOutcome is what backspace's list fetch means: "error" (no list —
// the way back stays), "ok" (the overview is listed) or "closed".
function backOutcome(files, fromId) {
  if (!files) return "error";
  return files.some((x) => x.id === fromId) ? "ok" : "closed";
}

// overviewStale reports whether the overview id on screen (stamp shown)
// needs a re-fetch after an agentdocs change carrying stamps; no stamp for
// it re-fetches.
function overviewStale(stamps, id, shown) {
  return !stamps || !(id in stamps) || stamps[id] !== shown;
}

// evictedText is the line naming a file an open pushed out of the list; cap
// is the server's (sent beside the eviction).
function evictedText(path, cap) {
  return "closed " + path + " (" + cap + " files open)";
}

// ovAnswerApplies reports whether overview refresh mine's answer may apply
// when refresh applied's answer is the last one that did: only a newer
// answer having applied drops it — a newer refresh merely started may fail.
function ovAnswerApplies(mine, applied) {
  return mine > applied;
}

// releaseAfterFailedOpen is what a tab tells the server after an open the
// server already moved it to (regId) failed to load: the file it still shows
// (shownId) is in front again, else it shows nothing; null when it still
// shows regId or the server never heard of the open.
function releaseAfterFailedOpen(regId, shownId) {
  if (!regId || regId === shownId) return null;
  return shownId ? { op: "focus", id: shownId } : { op: "background", id: regId };
}
// anchorBands is the bands an overview's anchors make in path (nLines
// lines): its line and range anchors — never a file's or a note's — sorted
// by start then end, one per range, past the end dropped, cut at it. A
// plain anchor (a stored overview's unresolved one) makes none.
function anchorBands(anchors, path, nLines) {
  const out = [], seen = new Set();
  anchors.forEach((a, i) => {
    if (a.plain || a.note || a.path !== path || !(a.start > 0) || a.start > nLines) return;
    const end = Math.min(Math.max(a.end || 0, a.start), nLines), k = a.start + ":" + end;
    if (seen.has(k)) return;
    seen.add(k);
    out.push({ start: a.start, end, i });
  });
  return out.sort((x, y) => x.start - y.start || x.end - y.end);
}

// bandOf is the index of the band anchor dest makes in nLines lines (its
// range clamped as anchorBands clamps it), or -1.
function bandOf(bands, anchors, dest, nLines) {
  if (!dest) return -1;
  for (const a of anchors) {
    if (a.dest !== dest || a.plain || a.note || !(a.start > 0)) continue;
    const end = Math.min(Math.max(a.end || 0, a.start), nLines);
    const k = bands.findIndex((b) => b.start === a.start && b.end === end);
    if (k >= 0) return k;
  }
  return -1;
}

// stepBand is n (dir 1) / p (-1): from band cur when line is in it, else
// from line; wraps (one band never "wraps"). i is -1 with no bands.
function stepBand(bands, cur, line, dir) {
  const n = bands.length;
  if (!n) return { i: -1, wrapped: false };
  if (cur >= 0 && cur < n && bands[cur].start <= line && line <= bands[cur].end) {
    const next = cur + dir;
    return { i: ((next % n) + n) % n, wrapped: (next < 0 || next >= n) && n > 1 };
  }
  if (dir > 0) {
    const k = bands.findIndex((b) => b.start > line);
    return k >= 0 ? { i: k, wrapped: false } : { i: 0, wrapped: n > 1 };
  }
  for (let k = n - 1; k >= 0; k--) if (bands[k].start < line) return { i: k, wrapped: false };
  return { i: n - 1, wrapped: n > 1 };
}

// fromGone reports whether overview id left the store: named on the closed
// list, or missing from the stamps, which list every overview the store
// holds (a close from the switcher's x takes it off the list first, so it
// is never named closed). No stamps: no word either way.
function fromGone(stamps, closed, id) {
  return closed.includes(id) || (!!stamps && !(id in stamps));
}

// bandKindAt is line's band: "cur" (wins an overlap), "other" or "".
function bandKindAt(bands, cur, line) {
  let k = "";
  for (let i = 0; i < bands.length; i++) {
    if (bands[i].start <= line && line <= bands[i].end) {
      if (i === cur) return "cur";
      k = "other";
    }
  }
  return k;
}

// SPLIT_MIN_PX is the box width the split needs (W7): about 60 characters a
// column at the viewer's 12px monospace; a narrower box shows one document.
const SPLIT_MIN_PX = 900;
function splitFits(boxWidth) {
  return boxWidth >= SPLIT_MIN_PX;
}

// hereOf is the anchor the right column's cursor marks on the overview
// (spec §7): the band covering line — the current band (cur) wins an
// overlap, then the first — as its anchor index; -1 when no band covers it
// or it is the selection's band (selBand: the selection is already marked).
function hereOf(bands, cur, line, selBand) {
  const covers = (b) => b.start <= line && line <= b.end;
  const k = cur >= 0 && cur < bands.length && covers(bands[cur]) ? cur : bands.findIndex(covers);
  return k < 0 || k === selBand ? -1 : bands[k].i;
}

// remarksReview is the review whose remarks a file opened from overview
// way back from carries: a stored overview's review; "" otherwise.
function remarksReview(from) {
  return from && from.stored && !from.closed ? String(from.stored.id).replace(/^review-overview:/, "") : "";
}
// --- end overview model ---

// --- the overlay -------------------------------------------------------------
// view is the ONE file on screen: its version, its lines, the cursor (1-based,
// 0 = no line) and the placeholder shown instead of lines ("" = none). An
// overview shows in document mode: ov holds it ({title, text, blocks,
// anchors, sel}). from is the way back to the overview an anchor opened this
// file from ({id, sel}, one step deep, this tab's); range the lines that
// anchor named.
const view = { id: "", src: "worktree", rev: "", path: "", lines: [], cur: 0, placeholder: "", image: null, notes: [], remarks: [], ov: null, from: null, range: null, diff: null };

// split is the overview beside the file (spec 2026-10-11 §4; null = none):
// id its open-files id ("" for a stored overview), path its entry's path, ov
// its document (view.ov's shape: {title, text, blocks, anchors, sel, want,
// stamp, stored?}), left whether the overview has the keys, here the anchor
// the right column's cursor marks (-1 = none), seq its refreshes' order.
let split = null;
// split.stack (plan 2, spec §11) is the right column as the STACK: every
// file in one scroll (splitstack.js). {slots, cur: {k, line} — line 0 is a
// file's header — curDest (the current band's link), near (files near the
// view), inFlight, want (a steered place whose file is still loading:
// {path, line, note}), observer}. While it shows, view holds no file.
const SINGLE_ONLY = "this acts on a single file — S opens it";
const STACK_READONLY = "notes are read-only in the stack — S opens the file";
// diffPref is the D / f choice (spec 2026-10-11-overview-split-diff §6):
// {key, on, partial}, key the overview whose links it holds for (a file's
// from.id). Page-local; gone when that overview's split or viewer closes.
let diffPref = null;
// lastStep is the file the previous link step showed ({key, path, lines}):
// the next step that opens the same text says so (§7).
let lastStep = null;

// ovDoc is the overview the overview keys and paint act on: the split's left
// column, else document mode's; ovEl the element it is drawn in.
function ovDoc() {
  return split ? split.ov : view.ov;
}
function ovEl() {
  return split ? $("viewer-ov-body") : $("viewer-body");
}

// boxWidth is the viewer box's width — measured while it is up, else the
// width it will open at (94vw).
function boxWidth() {
  return isOpen() ? $("viewer-box").clientWidth : Math.round(window.innerWidth * 0.94);
}

// raiseViewer puts the viewer's layer and chips up (idempotent).
function raiseViewer() {
  viewerRoot.style.bottom = $("foot").offsetHeight + "px"; // the bar stays in sight
  pushLayer("viewer", viewerRoot, { onKey: viewerKey });
  swapFoot(true);
}
const viewerSearch = new Search();
const places = new Map(); // id → {cur, top}: where THIS tab left each file
let loadSeq = 0; // bumped by every open: a reload that sees it move drops (L9)
let ovSeq = 0; // bumped by every overview refresh (refreshOverview)
let ovApplied = 0; // the refresh whose answer applied last: an older answer landing later drops
let ovLast = Promise.resolve(false); // the last refresh started …
let ovLastSeq = 0; // … and its place (an open's own fetch takes one too)
// ownBack: the one browser-history entry an anchor's open adds, so the
// browser's Back (its button, Alt+←, a mouse's back button) comes back like
// backspace. Never more than one; a Back with no way back just uses it up
// instead of leaving the page.
let ownBack = history.state?.gg === "back";
let openSeq = 0; // the loadSeq of the last open started (a close bumps loadSeq, not this)
let ofSync = Promise.resolve(); // this tab's posts about what it shows, in order (ofQueue); the next open waits for them
const closedAt = new Map(); // id → loadSeq when x closed it everywhere while it was not on screen (an open of it in flight drops)

// armBack gives the browser's Back its one entry (ownBack).
function armBack() {
  if (ownBack) return;
  history.pushState({ gg: "back" }, "");
  ownBack = true;
}
let cursorTimer = null;

function ofPost(body) {
  return postJSON("/api/open-files", { tab: tabId, ...body });
}

// ofQueue posts what this tab shows after every such post before it: a
// failed open's report and a close or background right after it must not
// reach the server the other way round.
function ofQueue(body) {
  ofSync = ofSync.then(() => ofPost(body)).catch(() => {});
}

function isOpen() {
  return !viewerRoot.classList.contains("hidden");
}

// viewerFileId is the open-files id this viewer shows; "" for a stored
// overview, which is page-local and never registered (so a change of the
// list — the one its own anchor's open causes — never closes it).
function viewerFileId() {
  return isOpen() && !(view.ov && view.ov.stored) ? view.id : "";
}

function rememberPlace() {
  if (view.id) places.set(view.id, { cur: view.cur, top: $("viewer-body").scrollTop });
}

function placeholderFor(body, lines) {
  if (body.missing) return "(file deleted on disk)";
  if (body.too_large) return "(file too large to preview)";
  if (body.binary) return body.image ? "" : `(binary file, ${fmtBytes(body.size || 0)} — not shown)`;
  return lines.length ? "" : "(empty file)";
}

// imageOf is the image a body describes ({url, info}), or null: the page
// fetches the bytes raw and paints an <img>; the stamp busts the cache when
// the file on disk moves.
function imageOf(body, src, rev, path) {
  if (!body.binary || !body.image) return null;
  const url =
    "/api/file-raw?src=" + encodeURIComponent(src) + "&rev=" + encodeURIComponent(rev) + "&path=" + encodeURIComponent(path) +
    (body.stamp ? "&stamp=" + encodeURIComponent(body.stamp) : "");
  return { url, info: `${body.image} image ${body.width}×${body.height}, ${fmtBytes(body.size || 0)}` };
}

// imageHTML paints an image body: its info line and the <img>.
export function imageHTML(image) {
  return `<div class="notice">${esc(image.info)}</div><img class="vimg" src="${esc(image.url)}" alt="">`;
}

function fetchOverview(id) {
  return getJSON("/api/overview?id=" + encodeURIComponent(id));
}

function fetchContent(src, rev, path, review = "", init = undefined) {
  return getJSON("/api/file-content?src=" + encodeURIComponent(src) + "&rev=" + encodeURIComponent(rev) + "&path=" + encodeURIComponent(path) +
    (review ? "&review=" + encodeURIComponent(review) : ""), init);
}

// reportCursor tells the server this tab's line, debounced: the switcher of
// every tab shows it (read fresh when a switcher opens — never broadcast).
function reportCursor() {
  clearTimeout(cursorTimer);
  const id = view.id, line = view.cur;
  if (!id || !line) return;
  cursorTimer = setTimeout(() => ofPost({ op: "cursor", id, line }).catch(() => {}), 400);
}

// The markup is built at import: bindSearchBar needs its bar in the DOM.
const viewerRoot = mountOverlay("viewer");
viewerRoot.innerHTML =
  `<div id="viewer-box">` +
  `<div id="viewer-ov" class="hidden"><div id="viewer-ov-title"></div><div id="viewer-ov-body" tabindex="-1"></div></div>` +
  `<div id="viewer-file"><div id="viewer-mode" class="hidden"><button data-mode="file">file</button><button data-mode="diff">diff</button></div><div id="viewer-title"></div>` +
  `<div id="viewer-search" class="hidden search-bar"><span id="viewer-search-lead">/</span>` +
  `<input id="viewer-search-input" type="text" autocomplete="off" spellcheck="false" placeholder="find in this file — enter keeps it, ] [ step, esc clears">` +
  `<span id="viewer-search-count"></span></div>` +
  `<div id="viewer-body" tabindex="-1"></div></div></div>`;

viewerRoot.addEventListener("click", (e) => {
  if (e.target.id === "viewer") closeViewer(escHow(!!view.ov, view.notes.length)); // the backdrop is esc; the box is not
});
$("viewer-body").addEventListener("click", (e) => {
  if (split && split.left) focusSplit(false);
  if (split && split.stack) { // the stack's own lines and headers (spec §11)
    const st = split.stack, head = e.target.closest(".vstk-head"), row = e.target.closest(".vline[data-k]");
    const picked = getSelection();
    if (!picked.isCollapsed && $("viewer-body").contains(picked.anchorNode)) return;
    if (head || row) st.want = null; // the reader's own place wins over a step still loading
    if (head) {
      st.cur = { k: Number(head.dataset.k), line: 0 };
      if (e.target.closest(".vstk-fold")) return foldStackFile();
    } else if (row) st.cur = { k: Number(row.dataset.k), line: Number(row.dataset.i) + 1 };
    else return;
    return paintStackCursor();
  }
  const fold = e.target.closest(".vfold");
  if (fold && view.diff) {
    view.diff.open.add(Number(fold.dataset.fold));
    return rerenderKeepingScroll();
  }
  const anc = e.target.closest("a.md-anchor");
  if (anc) {
    e.preventDefault(); // a single click opens (ruling 2)
    openAnchorAt(Number(anc.dataset.a));
    return;
  }
  const act = e.target.closest("button[data-nact]");
  if (act) {
    const n = view.notes.find((x) => x.id === act.closest(".vnote")?.dataset.note);
    if (n) act.dataset.nact === "dismiss" ? dismissNote(n.id) : copyNoteRef(n);
    return;
  }
  const row = e.target.closest(".vline[data-i], .vline[data-r]");
  if (!row) return;
  // A diff row stands for its new line (a deleted one: the line below it).
  const dv = view.diff, hit = row.dataset.r !== undefined ? rowNewLine(dv.rows, Number(row.dataset.r)) : Number(row.dataset.i) + 1;
  // Shift+click on a line NUMBER marks the lines from the cursor to it (the
  // cursor stays): the range "copy file link" then names.
  if (e.shiftKey && e.target.closest(".vno")) {
    getSelection().removeAllRanges();
    view.rangeOwn = true; // the reader's own range: a click or esc clears it
    view.range = viewerRange(Math.min(view.cur || hit, hit), Math.max(view.cur || hit, hit), view.lines.length);
    rerenderKeepingScroll();
    return;
  }
  // A drag that selected text (it may end on the line it began on, which is a
  // click) is copying, not pointing: the cursor and the band stay.
  const picked = getSelection();
  if (!picked.isCollapsed && $("viewer-body").contains(picked.anchorNode)) return;
  if (row.dataset.r !== undefined) dv.row = Number(row.dataset.r);
  view.cur = hit;
  if (clearViewerRange()) return;
  paintCursor();
});
$("viewer-mode").addEventListener("click", (e) => {
  const b = e.target.closest("button[data-mode]");
  if (b && (b.dataset.mode === "diff") !== diffWanted()) flipDiff();
});
$("viewer-ov-body").addEventListener("click", (e) => {
  if (!split) return;
  const anc = e.target.closest("a.md-anchor");
  focusSplit(true);
  if (!anc) return;
  e.preventDefault();
  selectAnchor(Number(anc.dataset.a));
  split.here = -1;
  steerSplit();
});
$("viewer-body").addEventListener("mousedown", (e) => {
  if (e.shiftKey && e.target.closest(".vno")) e.preventDefault(); // no text selection from the old caret
});
$("viewer-body").addEventListener("contextmenu", (e) => {
  if (split && split.stack) {
    e.preventDefault();
    return opLine(SINGLE_ONLY, false);
  }
  const row = e.target.closest(".vline[data-i], .vline[data-r]");
  if (row && row.dataset.r !== undefined) {
    view.diff.row = Number(row.dataset.r);
    view.cur = rowNewLine(view.diff.rows, view.diff.row);
  } else if (row) view.cur = Number(row.dataset.i) + 1;
  e.preventDefault();
  paintCursor();
  openViewerMenu(e.clientX, e.clientY);
});

// openViewer shows path at one version and registers it as this tab's open
// file (reused when already open). id (the switcher) brings an entry back by
// id. The cursor lands per pickLine; a second call replaces the file on
// screen, which stays open in the background. from is the way back an
// overview's link opened this file with — set before its first render, so
// its bands and its stored review's remarks draw at once; steer marks the
// split's own steering of its right column.
async function openViewer({ src = "worktree", rev = "", path = "", line = 0, id = "", from = null, steer = false }) {
  const seq = ++loadSeq;
  openSeq = seq;
  for (const [k, at] of closedAt) if (at < seq) closedAt.delete(k); // closed before this open: no open in flight can land
  if (isOpen()) rememberPlace();
  let reg, body, ov, ovMine, dres;
  try {
    await ofSync; // a failed open's report reaches the server first
    reg = id ? await ofPost({ op: "focus", id }) : await ofPost({ op: "open", src, rev, path, line });
    if (reg.file.source === "overview") {
      ovMine = ++ovSeq; // a refresh started before this fetch is older than its answer
      ov = await fetchOverview(reg.file.id);
    } else {
      [body, dres] = await Promise.all([
        fetchContent(reg.file.source, reg.file.rev || "", reg.file.path, remarksReview(from)),
        diffWanted(from) ? fetchDiff(from, reg.file.path) : null,
      ]);
    }
  } catch (e) {
    reportShown(seq, reg);
    opLine("view failed: " + (e.message || e), true);
    return { ok: false, notice: "", error: String(e.message || e) };
  }
  // A newer open won, the viewer closed, or x closed this file everywhere
  // while it loaded here.
  if (seq !== loadSeq || closedAt.get(reg.file.id) >= seq) {
    reportShown(seq, reg);
    return { ok: false, notice: "" };
  }
  const f = reg.file;
  // A refresh that started after this fetch and landed first is newer: keep it.
  if (ov && view.ov && view.id === f.id && !ovAnswerApplies(ovMine, ovApplied)) ov = view.ov;
  if (ov) return showOverview(f, ov, ovMine);
  if (!from) {
    diffPref = null; // a file opened any other way: the overview's choice ends (§6)
    lastStep = null;
  }
  view.from = from; // an overview's link: its bands and remarks draw at once
  view.range = null;
  view.rangeOwn = false;
  if (split && !steer) { // another way of opening a plain file: the split steps aside (as esc)
    if (split.id) ofQueue({ op: "background", id: split.id });
    leaveSplit();
  }
  view.ov = null;
  Object.assign(view, { id: f.id, src: f.source, rev: f.rev || "", path: f.path, lines: body.lines || [], diff: null });
  view.notes = notesOf(body, view.src);
  view.remarks = body.remarks || [];
  view.placeholder = placeholderFor(body, view.lines);
  view.image = imageOf(body, view.src, view.rev, view.path);
  const place = line > 0 ? null : places.get(f.id);
  const want = pickLine(line, place, f.line || 0);
  const landed = line > 0 ? landLine(line, view.lines.length, f.path) : { line: view.placeholder && body.missing ? want : clampLine(want || 1, view.lines.length), notice: "" };
  view.cur = landed.line;
  view.diff = dres && diffWanted(from) ? buildDiff(dres, from) : null; // read with the file: no flash of it first; a D pressed meanwhile wins
  viewerSearchBar.reset(); // a new file is a new search
  raiseViewer();
  paintTitle();
  renderViewer();
  centerCursor();
  if (place) $("viewer-body").scrollTop = place.top;
  if (!split || !split.left) $("viewer-body").focus({ preventScroll: true });
  // A reopen lands where THIS tab left the file; the server (every tab's
  // switcher) still has the line it last heard.
  if (landed.line !== (f.line || 0)) reportCursor();
  if (diffWanted(from) && !dres) refreshDiff(); // D pressed while the step loaded
  clearOpLine(f.path + " opened in the background"); // it is in front now
  if (landed.notice) opLine(landed.notice, false);
  if (reg.evicted) opLine(evictedText(reg.evicted, reg.cap), false);
  if (from) { // a link step (§7): the same text as the previous step's file says so
    const step = { key: from.id, path: f.path, lines: view.placeholder || view.image ? [] : view.lines.map((l) => l.text || "") };
    if (!landed.notice && !reg.evicted && sameContent(lastStep, step)) opLine("same content as " + elidePath(lastStep.path, 60), false);
    lastStep = step;
  }
  return { ok: true, notice: landed.notice };
}

// reportShown: open seq did not land, though the server already moved this
// tab to reg's file — unless a newer open is under way (it reports itself),
// tell the server what the tab really shows; the next open waits for it.
function reportShown(seq, reg) {
  const back = seq === openSeq ? releaseAfterFailedOpen(reg && reg.file.id, viewerFileId()) : null;
  if (back) ofQueue(back);
}

// showOverview puts overview ov (entry f) on screen in document mode, where
// this tab left it; mine is its fetch's place in the refresh order.
function showOverview(f, ov, mine) {
  ovApplied = Math.max(ovApplied, mine);
  if (splitFits(boxWidth())) return openSplit(f.id, f.path, { ...ov, sel: -1, want: "" });
  Object.assign(view, { id: f.id, src: "overview", rev: "", path: f.path, lines: [], notes: [], remarks: [], cur: 0, placeholder: "", image: null, from: null, range: null, rangeOwn: false, diff: null });
  view.ov = { ...ov, sel: -1, want: "" };
  const place = places.get(f.id);
  viewerSearchBar.reset();
  viewerRoot.style.bottom = $("foot").offsetHeight + "px";
  pushLayer("viewer", viewerRoot, { onKey: viewerKey });
  swapFoot(true);
  paintTitle();
  renderViewer();
  $("viewer-body").scrollTop = place ? place.top : 0;
  $("viewer-body").focus({ preventScroll: true });
  return { ok: true, notice: "" };
}

// ---- a review's STORED overview (spec §4.2) --------------------------------
// The same document kind as an agent's temporary overview, drawn by the
// same code, with three differences: it is page-local (never registered
// with the open-files list — nothing to focus, refresh or close there), its
// anchors open the file at the reviewed tip (a working review: the working
// tree), and Back re-shows the copy kept here. A plain anchor (the review
// could not resolve it, R3) is text: tab skips it, enter says why, no band.
export function openStoredOverview({ id, title, blocks, anchors, tip, text }) {
  if (isOpen()) rememberPlace();
  loadSeq++; // an open in flight must not land over it
  showStoredOverview({ id: "review-overview:" + id, title, blocks, anchors: anchors || [], tip: tip || "", text: text || "" });
}

function showStoredOverview(doc) {
  if (splitFits(boxWidth())) return openSplit("", "", { title: doc.title, blocks: doc.blocks, anchors: doc.anchors, text: doc.text, sel: -1, want: "", stored: doc });
  Object.assign(view, { id: doc.id, src: "overview", rev: "", path: "", lines: [], notes: [], remarks: [], cur: 0, placeholder: "", image: null, from: null, range: null, rangeOwn: false, diff: null });
  view.ov = { title: doc.title, blocks: doc.blocks, anchors: doc.anchors, text: doc.text, sel: -1, want: "", stored: doc };
  const first = view.ov.anchors.findIndex((a) => !a.plain);
  if (first >= 0) view.ov.sel = first; // the TUI lands on the first resolved anchor
  viewerSearchBar.reset();
  viewerRoot.style.bottom = $("foot").offsetHeight + "px";
  pushLayer("viewer", viewerRoot, { onKey: viewerKey });
  swapFoot(true);
  paintTitle();
  renderViewer();
  $("viewer-body").scrollTop = doc.scrollTop || 0;
  $("viewer-body").focus({ preventScroll: true });
}

// closeViewer takes the viewer down. "close" (esc, the backdrop — see escHow) lets go of
// the file — gone from the list unless another tab shows it; "background"
// (ctrl+], the . menu's hand-offs) keeps it open (ruling L4).
function closeViewer(how = "close") {
  if (split) { // W3: the whole split goes — an agent's overview and the file to the background
    const sp = split;
    leaveSplit();
    if (sp.id) ofQueue({ op: "background", id: sp.id });
    how = "background";
  }
  const id = view.id, line = view.cur;
  if (id && !(view.ov && view.ov.stored)) { // a stored overview was never registered: nothing to tell
    rememberPlace();
    if (how === "close") places.delete(id);
    ofQueue(how === "close" ? { op: "close", id } : { op: "background", id, line });
  }
  dropViewer();
}

// viewerClosedFile: x closed id everywhere while it was not on screen
// here — an open of it still loading in this tab must not land.
function viewerClosedFile(id) {
  closedAt.set(id, loadSeq);
}

// dropViewer takes the viewer down WITHOUT telling the server — the file is
// already gone from the list (closed everywhere) or never registered.
function dropViewer() {
  if (split) leaveSplit();
  diffPref = null;
  lastStep = null;
  view.diff = null;
  view.id = "";
  loadSeq++; // an open or reload in flight must not bring it back
  clearTimeout(cursorTimer);
  closeLayer("viewer");
  swapFoot(false);
}

function backgroundViewer() {
  if (view.ov && view.ov.stored) return closeViewer("close"); // page-local: nothing to background
  if (split && split.stack) { // the stack is no open file: the overview is what steps aside
    const name = split.id ? splitName() : "";
    closeViewer("background");
    if (name) opLine(name + " is in the background", false);
    return;
  }
  const name = docName(view);
  closeViewer("background");
  opLine(name + " is in the background", false);
}

// paintTitle cuts the PATH in the middle, never the file name, to fit.
function paintTitle() {
  const el = $("viewer-title");
  paintModeSwitch();
  if (split && split.stack) {
    const st = split.stack;
    el.style.paddingRight = "";
    el.title = split.ov.title;
    el.textContent = stackTitle(split.ov.title, st.cur.k, st.slots.length);
    return;
  }
  if (view.ov) {
    el.title = view.ov.title;
    el.textContent = ovTitle(view.ov, view.id);
    return;
  }
  if (!view.id) { // the split's right column before a link opened a file
    el.title = el.textContent = "";
    return;
  }
  const dv = view.diff, name = dv ? dv.name : view.path;
  const lead = dv ? "Diff " : "View ", tail = " (" + (dv ? dv.range : versionLabel(view.src, view.rev)) + ")";
  const mode = $("viewer-mode"), sw = mode.classList.contains("hidden") ? 0 : mode.offsetWidth + 12;
  el.style.paddingRight = sw ? 14 + sw + "px" : "";
  const cols = Math.floor((el.clientWidth - 28 - sw) / charWidth()) - lead.length - tail.length;
  el.title = name;
  el.textContent = lead + (cols > 0 ? elidePath(name, cols) : name) + tail;
}

function renderViewer() {
  if (split && split.stack) return renderStack();
  if (viewerSearch.query) viewerSearch.refind(view.diff ? view.diff.rows.map((r) => ({ row: r.i, side: 0, text: r.text })) : view.lines.map((l, i) => ({ row: i, side: 0, text: l.text || "" })));
  const body = $("viewer-body");
  if (view.ov) {
    body.innerHTML = `<div class="vdoc md">${mdHTML({ blocks: view.ov.blocks }, esc, { anchors: true })}</div>`;
    paintAnchors();
  } else if (view.diff) {
    body.innerHTML = view.diff.notice ? `<div class="notice">${esc(view.diff.notice)}</div>` : diffRowsHTML();
  } else if (view.placeholder) {
    body.innerHTML = `<div class="notice">${esc(view.placeholder)}</div>`;
  } else if (view.image) {
    body.innerHTML = imageHTML(view.image);
  } else {
    let html = "";
    const bands = viewBands(), curB = view.from ? bandOf(bands, view.from.anchors || [], view.from.cur, view.lines.length) : -1;
    view.lines.forEach((l, i) => {
      const noted = noteAtLine(shownNotes(), i + 1) ? " vnoted" : "";
      const ranged = view.range && view.range.start <= i + 1 && i + 1 <= view.range.end ? " vrange" : "";
      const bk = bandKindAt(bands, curB, i + 1);
      const banded = bk ? " vanchor" + (bk === "cur" ? " acur" : "") : "";
      html +=
        `<div class="vline${i + 1 === view.cur ? " vcur" : ""}${noted}${ranged}${banded}" data-i="${i}"><span class="vno">${i + 1}</span>` +
        `<span class="vtext">${renderCell(l.text, null, l.tok, "", viewerSearch.query ? viewerSearch.hitsOn(i, 0) : null) || " "}</span></div>`;
      for (const n of boxesAfter(shownNotes(), i)) html += noteBoxHTML(n);
    });
    body.innerHTML = html;
  }
  viewerSearchBar.paint();
  syncHere();
}

// ---- an agent's overview (agentdocs) ---------------------------------------
// paintAnchors marks the selected anchor and the missing ones.
function paintAnchors() {
  const ov = ovDoc();
  for (const el of ovEl().querySelectorAll("a.md-anchor")) {
    const i = Number(el.dataset.a), a = ov.anchors[i];
    el.classList.toggle("asel", i === ov.sel);
    el.classList.toggle("agone", !!(a && a.missing));
    el.classList.toggle("ahere", !!split && i === split.here);
  }
}

function anchorEl(i) {
  return ovEl().querySelector(`a.md-anchor[data-a="${i}"]`);
}

function selectAnchor(i) {
  const ov = ovDoc();
  ov.sel = i;
  ov.want = ov.anchors[i]?.dest || ""; // kept while a refresh drops it, back when it returns
  paintAnchors();
  anchorEl(i)?.scrollIntoView({ block: "nearest" });
}

// ---- the overview split (spec 2026-10-11) -----------------------------------
// ovTitle is an overview's title line: a stored one by its title, an
// agent's by its id and title.
function ovTitle(ov, id) {
  return ov.stored ? "Overview: " + ov.title : "Overview " + id + " · " + ov.title;
}

function paintSplitTitle() {
  const el = $("viewer-ov-title");
  el.title = split.ov.title;
  el.textContent = ovTitle(split.ov, split.id);
}

function renderSplitOverview() {
  const body = $("viewer-ov-body"), top = body.scrollTop;
  body.innerHTML = `<div class="vdoc md">${mdHTML({ blocks: split.ov.blocks }, esc, { anchors: true })}</div>`;
  body.scrollTop = top;
  paintAnchors();
}

// paintSplitFocus shows the split's columns and puts the keys in the
// focused one; without a split the box is today's one document.
function paintSplitFocus() {
  $("viewer-box").classList.toggle("split", !!split);
  $("viewer-ov").classList.toggle("hidden", !split);
  $("viewer-ov").classList.toggle("colfocus", !!split && split.left);
  $("viewer-file").classList.toggle("colfocus", !!split && !split.left);
  if (!split) return;
  (split.left ? $("viewer-ov-body") : $("viewer-body")).focus({ preventScroll: true });
  swapFoot(true);
}

function focusSplit(left) {
  if (!split) return;
  split.left = left;
  paintSplitFocus();
}

// leaveSplit drops the left column (the viewer stays up on the right file).
function leaveSplit() {
  if (split && split.stack) leaveStack();
  split = null;
  paintSplitFocus();
}

// openSplit puts overview ov in the left column (spec §5) — id its
// open-files id ("" = a stored overview, ov.stored set). A split already
// showing it keeps its place and selection (the new document's anchors
// replace the old, the selection kept by destination); a new one selects the
// first link that can open when none is selected and, unless told not to,
// steers the right column to it.
function openSplit(id, path, ov, { steer = true } = {}) {
  const same = !!split && (id ? split.id === id : !!split.ov.stored && !!ov.stored && split.ov.stored.id === ov.stored.id);
  if (same) {
    split.ov = { ...ov, sel: keepAnchor(ov.anchors || [], split.ov.want, split.ov.sel), want: split.ov.want, stored: ov.stored || split.ov.stored };
    split.left = true;
    if (view.id) ofQueue({ op: "focus", id: view.id }); // the tab shows the file, not the overview the open just focused
  } else {
    // The right column starts empty unless it keeps the file it shows (the
    // way back): never the lines of a file closed earlier, or of another
    // overview's link — the steer fills it.
    if (split && split.stack) leaveStack(); // another overview takes the split: its stack goes first
    const key = id || (ov.stored && ov.stored.id);
    if (diffPref && diffPref.key !== key) diffPref = null; // another overview's choice ends with its split
    if (lastStep && lastStep.key !== key) lastStep = null;
    if (view.ov || !view.id || steer) Object.assign(view, { id: "", ov: null, src: "worktree", rev: "", path: "", lines: [], notes: [], remarks: [], cur: 0, placeholder: "", image: null, from: null, range: null, rangeOwn: false, diff: null });
    split = { id, path, ov: { ...ov, sel: ov.sel >= 0 ? ov.sel : -1, want: ov.want || "" }, left: true, here: -1, seq: 0 };
    if (split.ov.sel < 0) split.ov.sel = stepAnchor(split.ov.anchors, -1, 1);
  }
  raiseViewer();
  paintSplitTitle();
  renderSplitOverview();
  paintSplitFocus();
  paintTitle(); // the right column's title is cut to its half
  if (split.ov.sel >= 0) selectAnchor(split.ov.sel);
  if (!view.id) {
    view.placeholder = split.ov.sel >= 0 ? "" : "(no link in this overview opens a file)";
    renderViewer();
  }
  if (!same && steer) {
    if (state.ui && state.ui.stacked_review_files) {
      enterStack(null);
      steerSplit(); // into the stack (stackSteer)
    } else steerSplit();
  }
  return { ok: true, notice: "" };
}

// steerSplit shows the selected link's target in the right column (spec §5):
// the file at the link's line with the overview's links as bands (a stored
// overview: at the reviewed tip, with its remarks), or a note's file at the
// note. A link that cannot open says why and leaves the right column as it
// was. The focus stays where it is; no history entry. Quick steps settle on
// the last one (openViewer's loadSeq).
async function steerSplit() {
  const sp = split;
  const a = sp && sp.ov.anchors[sp.ov.sel];
  if (!a) return;
  const why = anchorStatus(a);
  if (why) return opLine(why, false);
  if (sp.stack) return stackSteer(a);
  let target;
  if (sp.ov.stored) target = storedAnchorOpen(sp.ov.stored.tip, a);
  else {
    const t = anchorTarget(a);
    if (!t.path) return opLine("note " + t.note + " is gone", false);
    target = { src: "worktree", path: t.path, line: t.line };
  }
  const from = { id: sp.id || sp.ov.stored.id, sel: sp.ov.sel, dest: a.dest, anchors: sp.ov.anchors, stamp: sp.ov.stamp, cur: a.note ? "" : a.dest };
  if (sp.ov.stored) from.stored = sp.ov.stored;
  const r = await openViewer({ ...target, from, steer: true });
  if (r.ok && a.note) $("viewer-body").querySelector(`.vnote[data-note="${a.note}"]`)?.scrollIntoView({ block: "nearest" });
}

// splitName is how a status line names the split's overview.
function splitName() {
  return "overview " + split.id + " " + JSON.stringify(split.ov.title);
}

// refreshSplitOverview re-reads the left column's overview after the store
// moved it (spec §10), keeping the selection by its destination; the right
// column's bands follow. An older answer landing after a newer one drops.
async function refreshSplitOverview() {
  const sp = split;
  const mine = ++sp.seq;
  let ov;
  try {
    ov = await fetchOverview(sp.id);
  } catch {
    return;
  }
  if (split !== sp || sp.seq !== mine) return;
  sp.ov = { ...ov, sel: keepAnchor(ov.anchors || [], sp.ov.want, sp.ov.sel), want: sp.ov.want };
  paintSplitTitle();
  renderSplitOverview();
  if (sp.stack) { // the links moved: the files they name follow, the read ones kept
    const st = sp.stack, path = st.slots[st.cur.k]?.path;
    st.slots = restack(st.slots, stackFiles(stackReviewFiles(sp), sp.ov.anchors));
    const k = path ? slotOf(st.slots, path) : -1;
    st.cur = k >= 0 ? { k, line: st.cur.line } : { k: 0, line: 0 };
    if (st.want && slotOf(st.slots, st.want.path) < 0) st.want = null;
    rerenderKeepingScroll();
    paintStackCursor();
  }
  if (view.from && !view.from.stored && view.from.id === sp.id) {
    view.from.anchors = sp.ov.anchors;
    view.from.stamp = ov.stamp;
    rerenderKeepingScroll();
  }
  swapFoot(true);
}

// splitOverviewGone: the left column's overview left the store (closed by
// the agent, or by x in a switcher) — the split collapses to the right
// column's file, its bands gone (spec §10).
function splitOverviewGone(name) {
  leaveSplit();
  diffPref = null;
  view.diff = null;
  if (view.from) view.from.closed = true;
  if (view.id) {
    paintTitle();
    rerenderKeepingScroll();
    swapFoot(true);
    $("viewer-body").focus({ preventScroll: true });
  } else dropViewer();
  opLine(name + " was closed", false);
}

// narrowSplit: the box narrowed below the split's width (W7) — the overview
// stays as today's one document, the right column's file goes to the
// background (the TUI's narrowSplits). Widening never re-splits on its own.
function narrowSplit() {
  const sp = split;
  leaveSplit();
  if (view.id) {
    rememberPlace();
    ofQueue({ op: "background", id: view.id, line: view.cur });
  }
  loadSeq++; // a steer in flight must not land over the overview
  if (sp.ov.stored) showStoredOverview(sp.ov.stored);
  else {
    ofQueue({ op: "focus", id: sp.id });
    showOverview({ id: sp.id, path: sp.path }, sp.ov, ovApplied);
  }
  selectAnchor(sp.ov.sel);
}

window.addEventListener("resize", () => {
  if (split && isOpen() && !splitFits($("viewer-box").clientWidth)) narrowSplit();
});

// backIntoSplit is backspace / Back in a file an overview opened when the
// box is wide enough (spec §5): the overview comes back in the left column
// with the link selected, the file stays in the right column where the
// reader left it. The one history entry the open armed is used up.
async function backIntoSplit(f) {
  let ov;
  if (f.stored) ov = { title: f.stored.title, blocks: f.stored.blocks, anchors: f.stored.anchors, text: f.stored.text, stored: f.stored };
  else {
    try {
      ov = await fetchOverview(f.id);
    } catch (e) {
      return opLine(e.status === 404 ? "the overview was closed" : "back failed: " + (e.message || e), e.status !== 404);
    }
  }
  if (!isOpen() || view.from !== f) return; // something else took the screen meanwhile
  f.anchors = ov.anchors || [];
  if (ov.stamp) f.stamp = ov.stamp;
  openSplit(f.stored ? "" : f.id, "", { ...ov, sel: backAnchor(f.anchors, f), want: f.dest }, { steer: false });
  rerenderKeepingScroll();
  if (ownBack) history.back(); // the popstate it fires meets the split and does nothing
}

// splitKey is a key in the split (W2): tab switches columns; the overview
// steps its links and steers; the file keeps every file key.
function splitKey(e) {
  if (e.key === "Tab") {
    e.preventDefault();
    focusSplit(!split.left);
    return true;
  }
  switch (e.key) {
    case "S": e.preventDefault(); toggleSplitStack(); return true;
    case "D": if (!split.stack) break; e.preventDefault(); opLine(SINGLE_ONLY, false); return true;
  }
  if (split.left) return splitOverviewKey(e);
  if (split.stack) return stackKey(e);
  switch (e.key) {
    case "Backspace": e.preventDefault(); focusSplit(true); return true;
    case "N": case "P": e.preventDefault(); stepSplitAnchor(e.key === "N" ? 1 : -1); return true;
  }
  return fileKey(e);
}

// syncHere recomputes the link the right column's cursor marks on the
// overview (spec §7) and repaints the left column's links when it moved.
function syncHere() {
  if (!split) return;
  let here;
  if (split.stack) { // the stack: the band at the cursor's (file, line)
    const st = split.stack, s = st.slots[st.cur.k], anchors = split.ov.anchors;
    here = -1;
    if (shows(s) && st.cur.line > 0) {
      const n = s.lines.length, bands = anchorBands(anchors, s.path, n);
      const cur = stackCurFile(st) === st.cur.k ? bandOf(bands, anchors, st.curDest, n) : -1;
      const sel = anchors[split.ov.sel];
      const selB = sel && anchorPath(sel) === s.path ? bandOf(bands, anchors, sel.dest, n) : -1;
      here = hereOf(bands, cur, st.cur.line, selB);
    }
  } else {
    const f = view.from, bands = viewBands(), n = view.lines.length;
    const cur = f ? bandOf(bands, f.anchors, f.cur, n) : -1;
    const sel = split.ov.anchors[split.ov.sel];
    const selB = sel && f ? bandOf(bands, f.anchors, sel.dest, n) : -1;
    here = hereOf(bands, cur, view.cur, selB);
  }
  if (here === split.here) return;
  split.here = here;
  paintAnchors();
}

// splitOverviewKey is a key on the left column (spec §8).
function splitOverviewKey(e) {
  const body = $("viewer-ov-body");
  switch (e.key) {
    case "ArrowDown": case "j": stepSplitAnchor(1); break;
    case "ArrowUp": case "k": stepSplitAnchor(-1); break;
    case "Enter": enterSplitFile(); break;
    case "r": copyAnchorRef(); break;
    case "y": copyText(split.ov.text, "overview text"); break;
    case "PageDown": case " ": body.scrollBy({ top: body.clientHeight - 40 }); break;
    case "PageUp": body.scrollBy({ top: -(body.clientHeight - 40) }); break;
    case "Home": case "g": body.scrollTop = 0; break;
    case "End": case "G": body.scrollTop = body.scrollHeight; break;
    case "D": if (!canFlip()) return false; flipDiff(); break;
    case "Escape": closeViewer("background"); break; // W3
    default: return false;
  }
  e.preventDefault();
  return true;
}

// stepSplitAnchor is ↓ / ↑ on the overview (and N / P from the file): the
// next link that can open, shown in the right column at once.
function stepSplitAnchor(dir) {
  const i = stepAnchor(split.ov.anchors, split.ov.sel, dir);
  if (i < 0) return;
  selectAnchor(i);
  split.here = -1; // the landing recomputes it
  steerSplit();
}

// enterSplitFile is enter on the overview: into the right column, steering
// first when it does not show the selected link yet.
function enterSplitFile() {
  if (split.stack) return focusSplit(false); // the step already scrolled the stack
  const a = split.ov.anchors[split.ov.sel];
  if (a && (!view.id || !view.from || view.from.dest !== a.dest)) steerSplit();
  focusSplit(false);
}

// ---- the stacked file column (plan 2, spec §11) ----------------------------
// stackReviewFiles is the review's files when the page's review view is this
// overview's review, else null (the links name the files).
function stackReviewFiles(sp) {
  const rv = state.review, st = sp.ov.stored;
  return rv && st && "review-overview:" + rv.id === st.id ? rv.files || [] : null;
}

// toggleSplitStack is S from either column: the single file ⇄ the stack,
// remembered for the next split.
function toggleSplitStack() {
  if (split.stack) {
    const st = split.stack, s = st.slots[st.cur.k];
    // Nothing to open (R4): a file the review deleted, or one whose read failed.
    if (s && s.load === "none") return opLine(s.path + " is deleted at the reviewed commit", false);
    if (s && s.load === "error") return opLine(s.path + ": " + s.error, false);
    saveUI({ stacked_review_files: false });
    return leaveStack(true);
  }
  saveUI({ stacked_review_files: true });
  const a = split.ov.anchors[split.ov.sel];
  // The stack opens on the file the right column shows (its lines come
  // along: no second read), else on the selected link's — also when a step
  // to that link is still loading (the file shown is the previous link's).
  const stepping = !!a && !anchorStatus(a) && !!anchorPath(a) && !!view.from && view.from.dest !== a.dest;
  if (view.id && !view.ov && !stepping) return enterStack({ path: view.path, line: view.cur, dest: view.from ? view.from.cur || "" : "", file: view });
  enterStack(a ? { path: anchorPath(a), line: a.start || 0, dest: a.note ? "" : a.dest } : null);
}

// enterStack puts the stack in the right column, its cursor on at's file
// and line (null: the first header). The single file steps aside to the
// background (the stack is no open file); the overview's D choice is kept
// for S back.
function enterStack(at) {
  const sp = split;
  const files = stackFiles(stackReviewFiles(sp), sp.ov.anchors);
  const k = at ? slotOf(files, at.path) : -1;
  const st = { slots: newSlots(files, k), cur: { k: Math.max(k, 0), line: 0 }, curDest: (at && at.dest) || "", near: new Set(), inFlight: 0, want: null, observer: null, abort: new AbortController() };
  const s = k >= 0 ? st.slots[k] : null;
  if (s && at.file && s.load === "idle") { // the single file's read moves into its slot
    Object.assign(s, { load: "ok", lines: at.file.lines, n: at.file.lines.length, notes: at.file.notes, remarks: at.file.remarks, why: at.file.placeholder || (at.file.image ? "(an image — S opens it)" : "") });
    st.cur.line = shows(s) ? clampLine(at.line || 1, s.lines.length) : 0;
  } else if (s && at.line) st.want = { path: s.path, line: at.line, note: "" };
  if (view.id) {
    rememberPlace();
    ofQueue({ op: "background", id: view.id, line: view.cur });
  }
  loadSeq++; // an open in flight must not land over the stack
  Object.assign(view, { id: "", ov: null, src: "worktree", rev: "", path: "", lines: [], notes: [], remarks: [], cur: 0, placeholder: "", image: null, from: null, range: null, rangeOwn: false, diff: null });
  viewerSearchBar.reset();
  sp.stack = st;
  paintTitle();
  renderViewer();
  paintStackCursor("center");
  swapFoot(true);
}

// leaveStack takes the stack down; open opens the cursor's file as the
// single file (S back) — through the split's own steering, so the
// overview's held D choice brings its diff back.
async function leaveStack(open = false) {
  const st = split.stack;
  if (st.observer) st.observer.disconnect();
  st.abort.abort(); // its reads are cancelled, never left to finish for nothing
  split.stack = null;
  if (!open) return;
  const sp = split, s = st.slots[st.cur.k];
  if (!s) {
    view.placeholder = "(no link in this overview opens a file)";
    paintTitle();
    renderViewer();
    return swapFoot(true);
  }
  const sel = sp.ov.anchors[sp.ov.sel];
  const cur = stackCurFile(st) === st.cur.k ? st.curDest : "";
  const dest = cur || (sel && anchorPath(sel) === s.path ? sel.dest : "");
  const from = { id: sp.id || sp.ov.stored.id, sel: sp.ov.sel, dest, anchors: sp.ov.anchors, stamp: sp.ov.stamp, cur };
  if (sp.ov.stored) from.stored = sp.ov.stored;
  const target = sp.ov.stored ? storedAnchorOpen(sp.ov.stored.tip, { path: s.path, start: st.cur.line }) : { src: "worktree", path: s.path, line: st.cur.line };
  view.placeholder = "(opening " + s.path + "…)";
  paintTitle();
  renderViewer();
  swapFoot(true);
  const r = await openViewer({ ...target, from, steer: true });
  // The file could not be read: the stack comes back as it was, rather
  // than a column left saying "(opening …)". A newer open or a close that
  // took the screen meanwhile is left alone.
  if (!r.ok && r.error !== undefined && split === sp && !sp.stack && !view.id) restoreStack(st, s.path + ": " + r.error);
}

// restoreStack puts stack st back after S back failed to open its file:
// the cursor where it was, the cancelled reads started over, the choice
// remembered as stacked again.
function restoreStack(st, why) {
  const back = { ...st, near: new Set(), inFlight: 0, want: null, observer: null, abort: new AbortController() };
  for (const s of back.slots) if (s.load === "loading") s.load = "idle";
  view.placeholder = "";
  split.stack = back;
  saveUI({ stacked_review_files: true });
  paintTitle();
  renderViewer();
  paintStackCursor("center");
  swapFoot(true);
  opLine("could not open " + why + " — the stack stays", false);
}

// stackCols is how many characters a header's path may take.
function stackCols() {
  return Math.max(20, Math.floor(($("viewer-body").clientWidth - 70) / charWidth()));
}

// renderStack paints every file's section and watches them come near.
function renderStack() {
  const st = split.stack, body = $("viewer-body");
  if (!st.slots.length) {
    body.innerHTML = `<div class="notice">(no link in this overview names a file)</div>`;
    return syncHere();
  }
  const cols = stackCols();
  body.innerHTML = `<div class="vstk">${st.slots.map((_, k) => stackSectionHTML(st, k, cols)).join("")}</div>`;
  observeStack(st);
  syncHere();
}

// stackSectionHTML is file k's section: its header, then its lines (bands,
// read-only boxes) or what stands in for them.
function stackSectionHTML(st, k, cols) {
  const s = st.slots[k], here = st.cur.k === k && st.cur.line === 0;
  const head =
    `<div class="vstk-head${here ? " vcur" : ""}" data-k="${k}"><span class="vstk-fold" title="fold (-)">${s.collapsed ? "▸" : "▾"}</span>` +
    (s.status ? `<span class="st ${esc(s.status)}">${esc(s.status)}</span>` : "") +
    `<span class="vstk-path" title="${esc(s.path)}">${esc(elidePath(s.path, cols))}</span></div>`;
  let inner = "";
  if (s.collapsed) inner = "";
  else if (s.why) inner = `<div class="notice">${esc(s.why)}</div>`;
  else if (s.load === "error") inner = `<div class="notice">${esc(s.error)}</div>`;
  else if (s.load !== "ok") inner = `<div class="vstk-ph" style="height:${placeholderRows(s) * stackRowPx()}px">loading…</div>`;
  else inner = stackLinesHTML(st, k);
  return `<section class="vstk-file" data-k="${k}">${head}${inner}</section>`;
}

// stackRowPx is one line's height (a drawn line's, else the 12px font's).
function stackRowPx() {
  const row = $("viewer-body").querySelector(".vline");
  return row ? row.offsetHeight : 17;
}

// stackLinesHTML is file k's lines: syntax tokens as the single file, the
// overview's bands (the current one only in its own file), every box
// read-only (spec §11).
function stackLinesHTML(st, k) {
  const s = st.slots[k], anchors = split.ov.anchors, n = s.lines.length;
  const bands = anchorBands(anchors, s.path, n);
  const curB = stackCurFile(st) === k ? bandOf(bands, anchors, st.curDest, n) : -1;
  const boxes = mergeNotes(s.notes, s.remarks);
  let html = "";
  s.lines.forEach((l, i) => {
    const bk = bandKindAt(bands, curB, i + 1);
    html +=
      `<div class="vline${st.cur.k === k && st.cur.line === i + 1 ? " vcur" : ""}${noteAtLine(boxes, i + 1) ? " vnoted" : ""}${bk ? " vanchor" + (bk === "cur" ? " acur" : "") : ""}" data-k="${k}" data-i="${i}">` +
      `<span class="vno">${i + 1}</span><span class="vtext">${renderCell(l.text, null, l.tok, "", null) || " "}</span></div>`;
    for (const b of boxesAfter(boxes, i)) html += noteBoxHTML(b, false);
  });
  return html;
}

// repaintStackFile repaints file k's section alone, keeping what the
// reader looks at where it is when the section sits above it.
function repaintStackFile(st, k) {
  const body = $("viewer-body"), old = body.querySelector(`.vstk-file[data-k="${k}"]`);
  if (!old) return;
  const top = body.getBoundingClientRect().top;
  let ref = null;
  for (const el of body.querySelectorAll(".vstk-file")) {
    if (el.getBoundingClientRect().bottom > top) {
      ref = el;
      break;
    }
  }
  const above = ref && Number(ref.dataset.k) > k, off = above ? ref.getBoundingClientRect().top - top : 0;
  const t = document.createElement("template");
  t.innerHTML = stackSectionHTML(st, k, stackCols());
  const el = t.content.firstElementChild;
  old.replaceWith(el);
  if (st.observer) {
    st.observer.unobserve(old);
    st.observer.observe(el);
  }
  if (above) body.scrollTop += ref.getBoundingClientRect().top - top - off;
}

// observeStack watches the sections come near the view (a screen above and
// below): those load (pumpStack).
function observeStack(st) {
  if (st.observer) st.observer.disconnect();
  st.near = new Set();
  st.observer = new IntersectionObserver(
    (entries) => {
      if (!split || split.stack !== st) return;
      for (const e of entries) {
        const k = Number(e.target.dataset.k);
        if (e.isIntersecting) st.near.add(k);
        else st.near.delete(k);
      }
      pumpStack(st);
    },
    { root: $("viewer-body"), rootMargin: "100% 0px" }
  );
  for (const el of $("viewer-body").querySelectorAll(".vstk-file")) st.observer.observe(el);
}

// pumpStack starts reads while fewer than SPLIT_STACK_MAX_IN_FLIGHT are out:
// a steered file first, then the near file closest to the cursor's.
function pumpStack(st) {
  while (split && split.stack === st && st.inFlight < SPLIT_STACK_MAX_IN_FLIGHT) {
    const w = st.want ? slotOf(st.slots, st.want.path) : -1;
    let k = w >= 0 && st.slots[w].load === "idle" && !st.slots[w].collapsed ? w : -1;
    if (k < 0) {
      let best = Infinity;
      for (const i of st.near) {
        const s = st.slots[i];
        if (!s || s.collapsed || s.load !== "idle") continue;
        const d = Math.abs(i - st.cur.k);
        if (d < best || (d === best && i > k)) {
          best = d;
          k = i;
        }
      }
    }
    if (k < 0) return;
    loadStackFile(st, k);
  }
}

// loadStackFile reads file k at the overview's version: the reviewed tip
// with the review's remarks (a stored overview), else the working tree with
// the agent's notes.
async function loadStackFile(st, k) {
  const s = st.slots[k], sp = split;
  s.load = "loading";
  st.inFlight++;
  const at = sp.ov.stored ? storedAnchorOpen(sp.ov.stored.tip, { path: s.path }) : { src: "worktree", rev: "" };
  let body = null;
  try {
    body = await fetchContent(at.src, at.rev, s.path, sp.ov.stored ? remarksReview({ stored: sp.ov.stored }) : "", { signal: st.abort.signal });
  } catch (e) {
    if (st.abort.signal.aborted) return; // the stack was dropped: its reads were cancelled
    s.error = "(load failed: " + (e.message || e) + ")";
  }
  if (!split || split.stack !== st) return; // the stack left the screen while this loaded
  st.inFlight--;
  k = st.slots.indexOf(s); // a refresh of the links may have moved (or dropped) the file meanwhile
  if (k < 0) return pumpStack(st);
  if (!body) s.load = "error";
  else {
    s.load = "ok";
    s.lines = body.lines || [];
    s.n = s.lines.length;
    s.notes = notesOf(body, at.src);
    s.remarks = body.remarks || [];
    s.why = placeholderFor(body, s.lines) || (imageOf(body, at.src, at.rev, s.path) ? "(an image — S opens it)" : "");
  }
  landStackFile(st, k);
  pumpStack(st);
}

// landStackFile paints file k's arrival: a step waiting for it lands (the
// cursor on its line, centred), else the reader stays where they are.
function landStackFile(st, k) {
  const w = st.want, s = st.slots[k];
  if (w && w.path === s.path) {
    st.want = null;
    st.cur = { k, line: shows(s) ? clampLine(stackNoteLine(s, w.note) || w.line || 1, s.lines.length) : 0 };
    repaintStackFile(st, k);
    return paintStackCursor("center");
  }
  repaintStackFile(st, k);
  syncHere();
}

// stackCursorEl is the cursor's element: a line, or a file's header.
function stackCursorEl() {
  const c = split.stack.cur, body = $("viewer-body");
  return c.line ? body.querySelector(`.vline[data-k="${c.k}"][data-i="${c.line - 1}"]`) : body.querySelector(`.vstk-head[data-k="${c.k}"]`);
}

// paintStackCursor moves the cursor's mark and brings it into view
// (block: "nearest" for a step, "center" / "start" for a landing).
function paintStackCursor(block = "nearest") {
  for (const el of $("viewer-body").querySelectorAll(".vcur")) el.classList.remove("vcur");
  const el = stackCursorEl();
  if (el) {
    el.classList.add("vcur");
    el.scrollIntoView({ block });
  }
  paintTitle();
  syncHere();
}

// stackSteer is a step on the overview while the stack shows (spec §11):
// the link's file unfolded and scrolled to — read first when it has not
// been — the cursor on the link's line and its band current. The newest
// step wins over one still waiting for its file.
function stackSteer(a) {
  const st = split.stack;
  const path = anchorPath(a);
  if (!path) return opLine("note " + a.note + " is gone", false);
  const k = slotOf(st.slots, path);
  if (k < 0) return opLine("no file " + path + " in the stack", false);
  const was = stackCurFile(st);
  st.curDest = a.note ? "" : a.dest;
  const s = st.slots[k];
  if (s.collapsed) s.collapsed = false;
  if (s.load === "idle" || s.load === "loading") {
    st.want = { path, line: a.start || 0, note: a.note || "" };
    st.cur = { k, line: 0 };
    repaintStackFile(st, k);
    if (was >= 0 && was !== k) repaintStackFile(st, was);
    paintStackCursor("start");
    return pumpStack(st);
  }
  st.want = null;
  st.cur = { k, line: shows(s) ? clampLine(stackNoteLine(s, a.note) || a.start || 1, s.lines.length) : 0 };
  repaintStackFile(st, k);
  if (was >= 0 && was !== k) repaintStackFile(st, was);
  paintStackCursor("center");
}

// stackNoteLine is the line note id sits on in slot s (0 = not there).
function stackNoteLine(s, id) {
  if (!id) return 0;
  const n = s.notes.find((x) => x.id === id);
  return n ? n.start : 0;
}

// stackCurFile is the file of the current band's link, or -1: bandOf
// matches a band by its range, so only that file may light one current.
function stackCurFile(st) {
  const a = st.curDest ? split.ov.anchors.find((x) => x.dest === st.curDest) : null;
  return a ? slotOf(st.slots, anchorPath(a)) : -1;
}

// stackKey is a key on the stack (spec §11): lines across files, bands,
// headers, folds; the single-file keys say S opens the file. A key that
// moves the cursor drops a step still waiting for its file: a late read
// never pulls the cursor from where the reader went (the TUI's rule).
const STACK_MOVES = new Set(["ArrowDown", "j", "ArrowUp", "k", "PageDown", " ", "PageUp", "Home", "g", "End", "G", "N", "P", "n", "p", "-", "_"]);
function stackKey(e) {
  const st = split.stack;
  if (!st.slots.length && e.key !== "Escape" && e.key !== "Backspace") return false;
  if (STACK_MOVES.has(e.key)) st.want = null;
  switch (e.key) {
    case "Backspace": focusSplit(true); break;
    case "ArrowDown": case "j": st.cur = stackMove(st.slots, st.cur, 1); paintStackCursor(); break;
    case "ArrowUp": case "k": st.cur = stackMove(st.slots, st.cur, -1); paintStackCursor(); break;
    case "PageDown": case " ": st.cur = stackMove(st.slots, st.cur, pageRows()); paintStackCursor(); break;
    case "PageUp": st.cur = stackMove(st.slots, st.cur, -pageRows()); paintStackCursor(); break;
    case "Home": case "g": st.cur = { k: 0, line: 0 }; paintStackCursor(); break;
    case "End": case "G": st.cur = stackEnd(st.slots); paintStackCursor(); break;
    case "N": case "P": { const h = nextHeader(st.slots, st.cur, e.key === "N" ? 1 : -1); if (h) { st.cur = h; paintStackCursor("start"); } break; }
    case "n": stepStackAnchor(1); break;
    case "p": stepStackAnchor(-1); break;
    case "-": foldStackFile(); break;
    case "_": foldAllStack(); break;
    case "/": case "@": case "L": case ".": case "}": case "{": case "f": opLine(SINGLE_ONLY, false); break;
    case "d": case "r": {
      const s = st.slots[st.cur.k];
      if (s && st.cur.line && noteAtLine(mergeNotes(s.notes, s.remarks), st.cur.line)) opLine(STACK_READONLY, false);
      else opLine("no note on this line", false);
      break;
    }
    case "w": cycleTextMode(); break;
    case "Escape": closeViewer("background"); break; // W3
    default: return false;
  }
  e.preventDefault();
  return true;
}

// stepStackAnchor is n / p in the stack: the next / previous band across
// the read files, the cursor on its first line, the band current — the
// overview's selection stays (W5).
function stepStackAnchor(dir) {
  const st = split.stack, anchors = split.ov.anchors;
  const bands = stackBands(st.slots, (p, n) => anchorBands(anchors, p, n));
  if (!bands.length) return opLine("no links in the files read so far", false);
  const ck = stackCurFile(st);
  let cur = -1;
  if (ck >= 0 && shows(st.slots[ck])) {
    const s = st.slots[ck], fb = anchorBands(anchors, s.path, s.lines.length), b = fb[bandOf(fb, anchors, st.curDest, s.lines.length)];
    if (b) cur = bands.findIndex((x) => x.k === ck && x.start === b.start && x.end === b.end);
  }
  const r = stepStackBand(bands, cur, st.cur, dir), b = bands[r.i], a = anchors[b.i];
  st.curDest = a.dest;
  st.cur = { k: b.k, line: b.start };
  repaintStackFile(st, b.k);
  if (ck >= 0 && ck !== b.k) repaintStackFile(st, ck);
  paintStackCursor("center");
  opLine("anchor " + (r.i + 1) + "/" + bands.length + " in the stack · " + (a.label || a.dest) + (r.wrapped ? " · wrapped" : ""), false);
}

// foldStackFile is -: the cursor's file folds to its header (or unfolds).
function foldStackFile() {
  const st = split.stack, k = st.cur.k, s = st.slots[k];
  if (!s) return;
  st.want = null;
  s.collapsed = !s.collapsed;
  st.cur = { k, line: 0 };
  repaintStackFile(st, k);
  paintStackCursor();
  pumpStack(st);
}

// foldAllStack is _: every file folds, or — all folded — every file opens.
function foldAllStack() {
  const st = split.stack, fold = st.slots.some((s) => !s.collapsed);
  st.want = null;
  for (const s of st.slots) s.collapsed = fold;
  st.cur = { k: st.cur.k, line: 0 };
  renderStack();
  paintStackCursor("start");
}

// refreshOverview re-reads the overview on screen (the server re-checks its
// anchors), keeping the selected anchor by its destination; false when it
// is gone or another open won meanwhile. Refreshes may overlap: an answer
// applies unless a newer one already did (a newer refresh that fails keeps
// the older answer), and a refresh whose fetch fails answers with the last
// one started when a newer one is under way (false when none is) — so a
// caller (openAnchorAt) resumes on a list re-checked after its call unless
// every refresh it could wait on fails.
function refreshOverview() {
  if (view.ov && view.ov.stored) return Promise.resolve(true); // immutable: nothing to re-check
  if (!view.ov || !viewerFileId()) return Promise.resolve(false);
  ovLastSeq = ++ovSeq;
  ovLast = refreshOverviewAs(ovLastSeq);
  return ovLast;
}

async function refreshOverviewAs(mine) {
  const id = view.id, seq = loadSeq;
  let ov;
  try {
    ov = await fetchOverview(id);
  } catch {
    return ovLastSeq > mine ? ovLast : false;
  }
  if (seq !== loadSeq || id !== view.id || !view.ov) return false;
  if (!ovAnswerApplies(mine, ovApplied)) return true; // a newer answer is on screen
  ovApplied = mine;
  view.ov = { ...ov, sel: keepAnchor(ov.anchors || [], view.ov.want, view.ov.sel), want: view.ov.want };
  paintTitle();
  rerenderKeepingScroll();
  return true;
}

// viewBands is the shown file's bands: the anchors of the overview that
// opened it, while that overview is open; [] otherwise.
function viewBands() {
  const f = view.from;
  if (!f || f.closed || !f.anchors || view.ov || view.image || view.placeholder) return [];
  return anchorBands(f.anchors, view.path, view.lines.length);
}

// stepViewerAnchor is n / p: the next / previous band, the cursor on its
// first line centred, the band current, and the way back on its anchor.
function stepViewerAnchor(dir) {
  const bands = viewBands(), f = view.from;
  if (!bands.length) return;
  const r = stepBand(bands, bandOf(bands, f.anchors, f.cur, view.lines.length), view.cur, dir);
  const b = bands[r.i], a = f.anchors[b.i];
  f.cur = a.dest; // W5: the current band only — the overview's selection (f.sel / f.dest) stays
  view.cur = b.start;
  rerenderKeepingScroll();
  centerCursor();
  syncHere();
  opLine("anchor " + (r.i + 1) + "/" + bands.length + " in this file · " + (a.label || a.dest) + (r.wrapped ? " · wrapped" : ""), false);
}

// refreshFromAnchors re-reads the overview that opened the shown file, for
// its anchors; a failed read keeps the last ones (a closed overview arrives
// on the closed list instead).
async function refreshFromAnchors(f) {
  if (f.stored) return; // a stored overview's anchors never move
  let ov;
  try {
    ov = await fetchOverview(f.id);
  } catch {
    return;
  }
  if (view.from !== f || !isOpen()) return;
  f.anchors = ov.anchors || [];
  f.stamp = ov.stamp;
  rerenderKeepingScroll();
  swapFoot(true);
}

// openAnchorAt opens anchor i in front — a file at its line, the
// overview's anchors in it drawn as bands, or a note's file at the note —
// after the server re-checked it; a missing one says why and opens nothing.
// Backspace there comes back.
async function openAnchorAt(i) {
  if (!view.ov || !view.ov.anchors[i]) return;
  const id = view.id, seq0 = loadSeq;
  selectAnchor(i);
  const fresh = await refreshOverview(); // keeps the selection by its destination
  if (loadSeq !== seq0) return; // a re-open or a close meanwhile owns the screen
  if (!fresh) { // nothing re-checked the list since the click: open nothing on the old one
    if (view.ov && view.id === id) opLine("could not re-check the overview — try the anchor again", true);
    return;
  }
  if (!view.ov || view.id !== id) return;
  const a = view.ov.anchors[view.ov.sel];
  if (!a) return opLine("that anchor is no longer in the overview", false);
  if (a.missing) return opLine(anchorStatus(a), false);
  if (view.ov.stored) {
    // A stored overview's anchor opens the file at the reviewed tip (a
    // working review: the working tree); the kept copy is the way back.
    const stored = { ...view.ov.stored, scrollTop: $("viewer-body").scrollTop };
    const back = { id, sel: view.ov.sel, dest: a.dest, anchors: view.ov.anchors, cur: a.dest, stored };
    const r = await openViewer({ ...storedAnchorOpen(stored.tip, a), from: back });
    if (!r.ok) return;
    armBack();
    return;
  }
  const t = anchorTarget(a);
  if (!t.path) return opLine("note " + t.note + " is gone", false);
  // The overview's anchors travel with the way back: the file draws its
  // own as bands (viewBands), the opened one current — a note is no band.
  const from = { id, sel: view.ov.sel, dest: a.dest, anchors: view.ov.anchors, stamp: view.ov.stamp, cur: t.note ? "" : a.dest };
  const r = await openViewer({ src: "worktree", path: t.path, line: t.line, from });
  if (!r.ok) return;
  armBack();
  if (t.note) $("viewer-body").querySelector(`.vnote[data-note="${t.note}"]`)?.scrollIntoView({ block: "nearest" });
}

window.addEventListener("popstate", () => {
  ownBack = history.state?.gg === "back"; // Forward can put it back on top
  if (split) return; // the split adds no history entry
  if (!view.from || !isOpen()) return;
  if (topLayer()?.id === "console") { closeConsole(); return armBack(); } // the agent keeps running; the next Back comes back
  if (topLayer()?.id !== "viewer") return armBack(); // a popup is over the file: Back leaves both alone
  anchorBack();
});

// anchorBack is backspace (or the browser's Back) in a file an anchor
// opened: the overview comes back with that anchor selected; the file stays
// open. One step deep.
async function anchorBack() {
  const f = view.from, id = view.id;
  if (f && splitFits(boxWidth())) return backIntoSplit(f);
  if (f && f.stored) {
    // Back to a stored overview re-shows the kept copy; the file it opened
    // stays open in the background, as a temporary overview's does.
    view.from = null;
    rememberPlace();
    ofQueue({ op: "background", id: view.id, line: view.cur });
    loadSeq++;
    showStoredOverview(f.stored);
    selectAnchor(backAnchor(view.ov.anchors, f));
    return;
  }
  view.from = null;
  swapFoot(true);
  let files = null, err = null;
  try {
    files = (await getJSON("/api/open-files")).files || [];
  } catch (e) {
    err = e;
  }
  switch (backOutcome(files, f.id)) {
    case "error":
      if (view.id === id && !view.from) {
        view.from = f; // a failed fetch is no answer: the way back stays
        armBack(); // the way back stays, for Back too
        swapFoot(true);
      }
      return opLine("back failed: " + (err.message || err), true);
    case "closed":
      return opLine("the overview was closed", false);
  }
  rememberPlace();
  ofQueue({ op: "background", id: view.id, line: view.cur });
  const seq = loadSeq + 1; // the open below takes it
  const r = await openViewer({ id: f.id });
  if (r.ok) {
    if (view.ov) selectAnchor(backAnchor(view.ov.anchors, f));
    return;
  }
  // The overview did not come back and nothing else opened or closed since:
  // the file is still on screen, and so is its way back.
  if (loadSeq === seq && isOpen() && view.id === id && !view.from) {
    ofQueue({ op: "focus", id }); // Back sent it to the background; a failed focus post reported nothing
    view.from = f;
    armBack(); // the overview did not come back: the way back stays
    swapFoot(true);
  }
}

function copyAnchorRef() {
  const ov = ovDoc();
  if (ov.stored) return opLine("a stored overview's anchor has no reference — copy the review link instead", false);
  const a = ov.anchors[ov.sel];
  if (!a) return opLine("no anchor selected — tab selects one", false);
  copyText(a.ref, "anchor reference");
}

function scrollDoc(dy) {
  $("viewer-body").scrollBy({ top: dy });
}

// overviewKey is a key in document mode; false = not the overview's.
function overviewKey(e) {
  const body = $("viewer-body");
  switch (e.key) {
    case "Tab": selectAnchor(stepAnchor(view.ov.anchors, view.ov.sel, e.shiftKey ? -1 : 1)); break;
    case "Enter":
      if (view.ov.sel >= 0) openAnchorAt(view.ov.sel);
      else opLine("no anchor selected — tab selects one", false);
      break;
    case "r": copyAnchorRef(); break;
    case "y": copyText(view.ov.text, "overview text"); break;
    case "ArrowDown": case "j": scrollDoc(40); break;
    case "ArrowUp": case "k": scrollDoc(-40); break;
    case "PageDown": case " ": scrollDoc(body.clientHeight - 40); break;
    case "PageUp": scrollDoc(-(body.clientHeight - 40)); break;
    case "Home": case "g": body.scrollTop = 0; break;
    case "End": case "G": body.scrollTop = body.scrollHeight; break;
    case "Escape": closeViewer(view.ov.stored ? "close" : escHow(true, 0)); break; // an overview steps aside (a stored one closes); x closes it
    default: return false;
  }
  e.preventDefault();
  return true;
}

// ---- an agent's notes (agentdocs) ---------------------------------------
// notesOf is a body's notes: only a working-tree read carries any.
function notesOf(body, src) {
  return src === "worktree" ? body.notes || [] : [];
}

// shownNotes is every box the file draws: the agent's notes, then the
// review's remarks (spec 2026-10-11 §6). Code that talks to the store keeps
// view.notes.
function shownNotes() {
  return mergeNotes(view.notes, view.remarks);
}

function noteBoxHTML(n, acts = !n.readonly) {
  return (
    `<div class="vnote${n.outdated ? " outdated" : ""}${n.readonly ? " remark" : ""}" data-note="${esc(n.id)}">` +
    `<div class="vnote-title">${esc(noteBoxTitle(n))}</div>` +
    `<div class="vnote-sum">${esc(n.summary)}</div>` +
    (n.rationale ? `<div class="vnote-why">${esc(n.rationale)}</div>` : "") +
    (acts ? `<div class="vnote-acts"><button data-nact="dismiss">dismiss</button><button data-nact="ref">copy reference</button></div>` : "") +
    `</div>`
  );
}

// rerenderKeepingScroll repaints the lines where the reader is.
function rerenderKeepingScroll() {
  const body = $("viewer-body");
  const top = body.scrollTop, left = body.scrollLeft;
  renderViewer();
  body.scrollTop = top;
  body.scrollLeft = left;
}

// ---- the right column's diff (spec 2026-10-11-overview-split-diff) --------
// canFlip: D acts on a file an overview's link opened (§6).
function canFlip() {
  return !!view.id && !view.ov && !!view.from && !view.from.closed;
}

// diffWanted: the overview that opened the file asks for its diff.
function diffWanted(from = view.from) {
  return !!from && !!diffPref && diffPref.on && diffPref.key === from.id;
}

// fetchDiff reads path's diff for from: {d, status} — status a failed
// read's HTTP status (500 when it had none), 0 when it answered.
async function fetchDiff(from, path) {
  const url = diffSource(from, path);
  if (!url) return { d: null, status: 0 };
  try {
    return { d: await getJSON(url), status: 0 };
  } catch (e) {
    return { d: null, status: e.status || 500 };
  }
}

// buildDiff is view.diff for one read; the cursor's row follows view.cur.
function buildDiff({ d, status }, from) {
  const agent = !from.stored;
  const notice = diffNotice(d, status, agent);
  const rows = notice ? [] : unifiedRows(d);
  const by = lineRows(rows), head = diffHead(d, view.path, agent);
  const partial = !!diffPref && diffPref.key === from.id && diffPref.partial;
  return { rows, by, row: Math.max(0, rowOfLine(rows, by, view.cur)), notice, name: head.name, range: head.range,
    remarks: (d && d.remarks) || [], partial, open: new Set(), shown: null };
}

// flipDiff is D and the title's switch: the file ⇄ its diff, the choice
// held for the overview's next links (§6); the cursor keeps its line and
// its place on screen.
async function flipDiff() {
  if (!canFlip()) return;
  const from = view.from, on = !diffWanted();
  diffPref = { key: from.id, on, partial: !!diffPref && diffPref.key === from.id && diffPref.partial };
  if (!on) {
    keepCursorPlace(() => {
      view.diff = null;
      paintTitle();
      renderViewer();
    });
    return swapFoot(true);
  }
  paintTitle(); // the switch shows the choice while the read is out
  const seq = loadSeq, id = view.id;
  const got = await fetchDiff(from, view.path);
  if (seq !== loadSeq || id !== view.id || view.from !== from || !diffWanted()) return;
  keepCursorPlace(() => {
    view.diff = buildDiff(got, from);
    paintTitle();
    renderViewer();
  });
  swapFoot(true);
}

// refreshDiff re-reads the shown diff (the file moved on disk), keeping the
// fold state; the cursor keeps its line.
async function refreshDiff() {
  const from = view.from, id = view.id, seq = loadSeq, was = view.diff;
  const got = await fetchDiff(from, view.path);
  if (seq !== loadSeq || id !== view.id || view.from !== from || !diffWanted()) return;
  keepCursorPlace(() => {
    view.diff = buildDiff(got, from);
    if (was) Object.assign(view.diff, { partial: was.partial, open: was.open });
    paintTitle();
    renderViewer();
  });
  swapFoot(true);
}

// keepCursorPlace repaints with paint and puts the cursor's row back where
// it was on screen — centred when it was off screen.
function keepCursorPlace(paint) {
  const body = $("viewer-body"), top = body.getBoundingClientRect().top;
  const before = cursorRow(), off = before ? before.getBoundingClientRect().top - top : -1;
  paint();
  const after = cursorRow();
  if (!after) return;
  if (off < 0 || off > body.clientHeight - after.offsetHeight) after.scrollIntoView({ block: "center" });
  else body.scrollTop += after.getBoundingClientRect().top - top - off;
}

// syncDiffRow puts the diff's cursor row on view.cur when something moved
// the line (n / p, a hit, a note); a deleted row stands for the line below
// it, so a cursor there stays.
function syncDiffRow() {
  const dv = view.diff;
  if (!dv || dv.notice || !dv.rows.length) return;
  if (rowNewLine(dv.rows, dv.row) !== view.cur) dv.row = Math.max(0, rowOfLine(dv.rows, dv.by, view.cur));
}

// diffNotes is every box the diff draws: the agent's notes (new side), then
// the review's remarks on both sides.
function diffNotes() {
  return [...view.notes, ...view.diff.remarks];
}

// diffRowsHTML is the diff's rows (§4): one column, deleted lines red with
// −, added green with +, the bands, the marked range and the note boxes;
// with f the unchanged runs fold, never a banded or noted row or the
// cursor's.
function diffRowsHTML() {
  const dv = view.diff, rows = dv.rows, bands = viewBands(), f = view.from;
  const curB = f ? bandOf(bands, f.anchors || [], f.cur, view.lines.length) : -1;
  syncDiffRow();
  const noted = new Set(), after = new Map();
  for (const p of placeNotes(rows, diffNotes())) {
    for (let k = p.first; k <= p.last; k++) noted.add(k);
    if (!after.has(p.last)) after.set(p.last, []);
    after.get(p.last).push(p.n);
  }
  const keep = (r) => r.i === dv.row || noted.has(r.i) || !!rowBand(rows, r.i, bands, curB);
  const items = (dv.partial && collapseDiffRows(rows, dv.open, keep)) || rows;
  dv.shown = [];
  let html = "";
  for (const r of items) {
    if (r.fold) {
      html += `<div class="vfold" data-fold="${r.start}">⋯ ${r.fold} unchanged line${r.fold === 1 ? "" : "s"}</div>`;
      continue;
    }
    dv.shown.push(r.i);
    const bk = rowBand(rows, r.i, bands, curB);
    const ranged = view.range && r.nu && view.range.start <= r.nu && r.nu <= view.range.end ? " vrange" : "";
    const cls = "vline vd-" + r.kind + (r.i === dv.row ? " vcur" : "") + (noted.has(r.i) ? " vnoted" : "") + ranged + (bk ? " vanchor" + (bk === "cur" ? " acur" : "") : "");
    const sign = r.kind === "add" ? "+" : r.kind === "del" ? "−" : " ";
    html +=
      `<div class="${cls}" data-r="${r.i}"><span class="vno">${r.kind === "del" ? r.old : r.nu}</span><span class="vsign">${sign}</span>` +
      `<span class="vtext">${renderCell(r.text, r.spans, r.tok, r.kind === "del" ? "l" : "r", viewerSearch.query ? viewerSearch.hitsOn(r.i, 0) : null) || " "}</span></div>`;
    for (const n of after.get(r.i) || []) html += noteBoxHTML(n);
  }
  return html;
}

// onDeletedRow: the diff's cursor is on a line the review deleted — no
// line at the reviewed version to link or copy (§5).
function onDeletedRow() {
  const dv = view.diff;
  return !!dv && !dv.notice && dv.rows[dv.row]?.kind === "del";
}

// noteAtCursor is the box d / r act on: in a diff the one covering the
// cursor's row (an old-side remark included), else the one on the line.
function noteAtCursor() {
  const dv = view.diff;
  if (dv && !dv.notice) return noteAtRow(placeNotes(dv.rows, diffNotes()), dv.row);
  return noteAtLine(shownNotes(), view.cur);
}

// toggleDiffFold is f in a diff: the changed lines only ⇄ the whole file,
// held with D's choice (§4).
function toggleDiffFold() {
  const dv = view.diff;
  dv.partial = !dv.partial;
  dv.open = new Set();
  if (diffPref && view.from && diffPref.key === view.from.id) diffPref.partial = dv.partial;
  keepCursorPlace(renderViewer);
  swapFoot(true);
}

// diffChips are the footer's D / f chips: D wherever it acts, f in a diff.
function diffChips() {
  if (!canFlip()) return "";
  const dv = view.diff;
  return `<button data-vact="dflip">D file / diff</button>` +
    (dv && !dv.notice ? `<button data-vact="dfold">f ${dv.partial ? "whole file" : "changed only"}</button>` : "");
}

// paintModeSwitch shows the title's file | diff switch when D can act, the
// half in force lit (§4).
function paintModeSwitch() {
  const sw = $("viewer-mode"), on = canFlip();
  sw.classList.toggle("hidden", !on);
  if (!on) return;
  const diff = diffWanted();
  for (const b of sw.querySelectorAll("button")) b.classList.toggle("on", (b.dataset.mode === "diff") === diff);
}

// dismissNote removes a note everywhere (every tab, and a TUI hosting this
// page, hear it from the store).
async function dismissNote(id) {
  try {
    await postJSON("/api/file-notes", { op: "dismiss", id });
  } catch (e) {
    return opLine("dismiss failed: " + (e.message || e), true);
  }
  view.notes = view.notes.filter((n) => n.id !== id);
  rerenderKeepingScroll();
  swapFoot(true);
  opLine("note " + id + " dismissed", false);
}

function copyNoteRef(n) {
  copyText(n.ref, "note reference " + n.id);
}

// stepNote is } / {: the next note in this file, then the next open file
// with notes, landing on its first (last) note.
async function stepNote(dir) {
  const dv = view.diff;
  if (dv && !dv.notice) {
    const r = nextNoteRow(placeNotes(dv.rows, diffNotes()), dv.row, dir);
    if (r >= 0) {
      dv.row = r;
      view.cur = rowNewLine(dv.rows, r);
      paintCursor();
      const n = noteAtCursor();
      for (const b of $("viewer-body").querySelectorAll(".vnote")) if (n && b.dataset.note === n.id) b.scrollIntoView({ block: "nearest" });
      cursorRow()?.scrollIntoView({ block: "nearest" });
      return;
    }
  }
  const line = view.diff ? 0 : nextNoteLine(shownNotes(), view.cur, dir);
  if (line) {
    view.cur = line;
    paintCursor();
    for (const b of $("viewer-body").querySelectorAll(".vnote")) {
      if (noteAtLine(shownNotes(), line)?.id === b.dataset.note) b.scrollIntoView({ block: "nearest" });
    }
    cursorRow()?.scrollIntoView({ block: "nearest" });
    return;
  }
  let files = [];
  try {
    files = (await getJSON("/api/open-files")).files || [];
  } catch {}
  const id = nextNotedFile(files, view.id, dir);
  if (!id) return opLine(view.notes.length ? "no more notes" : "no notes", false);
  const r = await openViewer({ id });
  const ns = shownNotes();
  const t = dir > 0 ? ns[0] : ns[ns.length - 1];
  if (r.ok && t) {
    view.cur = t.start;
    paintCursor();
    centerCursor();
  }
}

function cursorRow() {
  const dv = view.diff;
  if (dv) return dv.notice ? null : $("viewer-body").querySelector(`.vline[data-r="${dv.row}"]`);
  return $("viewer-body").querySelector(`.vline[data-i="${view.cur - 1}"]`);
}

function paintCursor() {
  syncDiffRow();
  if (view.diff && !view.diff.notice && !cursorRow()) rerenderKeepingScroll(); // the row was folded: keep() shows it now
  for (const el of $("viewer-body").querySelectorAll(".vcur")) el.classList.remove("vcur");
  const row = cursorRow();
  if (row) {
    row.classList.add("vcur");
    row.scrollIntoView({ block: "nearest" });
  }
  reportCursor();
  syncHere();
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
  const dv = view.diff;
  if (dv && dv.notice) return; // a notice has no rows: no cursor to move
  if (dv && !dv.notice) {
    const shown = dv.shown && dv.shown.length ? dv.shown : dv.rows.map((r) => r.i);
    if (!shown.length) return;
    const at = Math.max(0, shown.indexOf(dv.row));
    dv.row = shown[Math.min(Math.max(at + delta, 0), shown.length - 1)];
    view.cur = rowNewLine(dv.rows, dv.row);
    return paintCursor();
  }
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
  if (e.ctrlKey && !e.altKey && !e.metaKey && (e.code === "BracketRight" || e.key === "]")) {
    e.preventDefault();
    backgroundViewer();
    return true;
  }
  if (isSwitcherKey(e)) {
    e.preventDefault();
    openSwitcher();
    return true;
  }
  if (e.ctrlKey || e.metaKey || e.altKey) return false;
  if (split) return splitKey(e);
  if (view.ov) return overviewKey(e);
  return fileKey(e);
}

// fileKey is a key on the file the viewer shows (one document, or the
// split's right column); false = not the file's.
function fileKey(e) {
  // shift+↓ / shift+↑ mark lines from the cursor (it stays, as a shift+click
  // leaves it); L copies the link — the band's, else the cursor line's.
  if (e.shiftKey && (e.key === "ArrowDown" || e.key === "ArrowUp")) {
    e.preventDefault();
    stepViewerRange(e.key === "ArrowDown" ? 1 : -1);
    return true;
  }
  if (e.key === "L") {
    e.preventDefault();
    copyViewerLinkHere();
    return true;
  }
  if (viewerSearchKey(e)) return true;
  switch (e.key) {
    case "Backspace": if (!view.from) return false; anchorBack(); break;
    case "ArrowDown": case "j": moveCursor(1); break;
    case "ArrowUp": case "k": moveCursor(-1); break;
    case "PageDown": case " ": moveCursor(pageRows()); break;
    case "PageUp": moveCursor(-pageRows()); break;
    case "Home": case "g": if (view.diff) moveCursor(-Infinity); else { view.cur = clampLine(1, view.lines.length); paintCursor(); } break;
    case "End": case "G": if (view.diff) moveCursor(Infinity); else { view.cur = view.lines.length; paintCursor(); } break;
    case "f": if (!view.diff) return false; if (!view.diff.notice) toggleDiffFold(); break;
    case "w": cycleTextMode(); break;
    case ".": menuAtCursor(); break;
    case "d": { const n = noteAtCursor(); if (!n) return false; if (n.readonly) opLine("review remarks are read-only", false); else dismissNote(n.id); break; }
    case "r": { const n = noteAtCursor(); if (!n) return false; if (n.readonly) opLine("review remarks are read-only", false); else copyNoteRef(n); break; }
    case "}": stepNote(1); break;
    case "{": stepNote(-1); break;
    case "D": if (!canFlip()) return false; flipDiff(); break;
    case "n": stepViewerAnchor(1); break; // always ours: a p in the viewer is never pull
    case "p": stepViewerAnchor(-1); break;
    // A noted file steps aside on esc (the TUI's rule); the server decides
    // the same for a note that landed after this read.
    case "Escape": if (!clearViewerRange()) closeViewer(escHow(false, view.notes.length)); break;
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
    if (view.diff) {
      view.diff.row = h.row;
      view.cur = rowNewLine(view.diff.rows, h.row);
    } else view.cur = h.row + 1;
    paintCursor();
  }
}

const viewerSearchBar = bindSearchBar("viewer-search", {
  search: viewerSearch,
  here: () => ({ row: view.diff ? view.diff.row : Math.max(0, view.cur - 1), side: 0, col: -1 }),
  origin: () => ({ top: $("viewer-body").scrollTop, left: $("viewer-body").scrollLeft, cur: view.cur, row: view.diff ? view.diff.row : -1 }),
  restore: (o) => {
    $("viewer-body").scrollTop = o.top;
    $("viewer-body").scrollLeft = o.left;
    view.cur = o.cur;
    if (view.diff && o.row >= 0) view.diff.row = o.row;
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
// viewerFoot is the chips; a file with an agent's notes adds theirs.
function viewerFoot() {
  if (split && split.left) {
    return (
      `<span>↑↓ links</span><button data-vact="sfile">enter open</button><button data-vact="sfile">tab file</button>` +
      (split.ov.stored ? "" : `<button data-vact="aref">r reference</button>`) +
      `<button data-vact="ytext">y copy text</button><button data-vact="stack">S ${split.stack ? "single file" : "stack files"}</button>` + diffChips() +
      `<button data-vact="close">esc close</button><button data-vact="files">ctrl+\\ open files</button>`
    );
  }
  if (split && split.stack) {
    return (
      `<button data-vact="sov">tab overview</button><span>N P files</span><span>n p bands</span>` +
      `<button data-vact="sfold">- fold</button><button data-vact="sfoldall">_ fold all</button><button data-vact="stack">S single file</button>` +
      `<span>↑↓ j k line</span><button data-vact="wrap">w long lines</button><button data-vact="close">esc close</button><button data-vact="files">ctrl+\\ open files</button>`
    );
  }
  if (split) {
    const bandKeysS = viewBands().length ? "<span>n p bands</span>" : "";
    const notesS = (shownNotes().length ? `<span>} { notes</span>` : "") +
      (view.notes.length ? `<button data-vact="dismiss">d dismiss</button><button data-vact="ref">r reference</button>` : "");
    return (
      `<button data-vact="sov">tab overview</button><span>N P overview links</span>` + bandKeysS + `<button data-vact="stack">S stack files</button>` + diffChips() +
      `<span>↑↓ j k line</span><button data-vact="find">/ find</button><span>] [ next / prev</span>` + notesS +
      `<button data-vact="wrap">w long lines</button><button data-vact="menu">. menu</button><button data-vact="close">esc close</button><button data-vact="files">ctrl+\\ open files</button>`
    );
  }
  if (view.ov && view.ov.stored) {
    return (
      `<span>tab shift+tab anchors</span><button data-vact="aopen">enter open</button>` +
      `<button data-vact="ytext">y copy text</button><button data-vact="close">esc close</button><button data-vact="files">ctrl+\\ open files</button>`
    );
  }
  if (view.ov) {
    return (
      `<span>tab shift+tab anchors</span><button data-vact="aopen">enter open</button><button data-vact="aref">r reference</button>` +
      `<button data-vact="ytext">y copy text</button><button data-vact="bg">esc background</button><button data-vact="files">ctrl+\\ open files</button>`
    );
  }
  const back = view.from ? `<button data-vact="back" title="or your browser's Back">bksp back</button>` : "";
  const bandKeys = view.from && viewBands().length ? "<span>n p anchors</span>" : "";
  const notes = (shownNotes().length ? `<span>} { notes</span>` : "") +
    (view.notes.length ? `<button data-vact="dismiss">d dismiss</button><button data-vact="ref">r reference</button>` : "");
  return (
    back + bandKeys + diffChips() + `<span>↑↓ j k line</span><button data-vact="find">/ find</button><span>] [ next / prev</span>` + notes +
    `<button data-vact="wrap">w long lines</button><button data-vact="menu">. menu</button><button data-vact="bg">ctrl+] background</button><button data-vact="files">ctrl+\\ open files</button><button data-vact="close">esc close</button>`
  );
}

function swapFoot(on) {
  if (on) pushFoot("viewer", viewerFoot());
  else popFoot("viewer");
}

$("foot").addEventListener("click", (e) => {
  const b = e.target.closest("button[data-vact]");
  if (!b) return;
  switch (b.dataset.vact) {
    case "find": viewerSearchBar.open(false); break;
    case "wrap": cycleTextMode(); break;
    case "menu": menuAtCursor(); break;
    case "bg": backgroundViewer(); break;
    case "files": openSwitcher(); break;
    case "close": closeViewer(escHow(!!view.ov, view.notes.length)); break;
    case "dismiss": { const n = noteAtCursor(); if (!n) opLine("no note on this line", false); else if (n.readonly) opLine("review remarks are read-only", false); else dismissNote(n.id); break; }
    case "ref": { const n = noteAtCursor(); if (!n) opLine("no note on this line", false); else if (n.readonly) opLine("review remarks are read-only", false); else copyNoteRef(n); break; }
    case "aopen": if (view.ov && view.ov.sel >= 0) openAnchorAt(view.ov.sel); else opLine("no anchor selected — tab selects one", false); break;
    case "aref": if (ovDoc()) copyAnchorRef(); break;
    case "ytext": if (ovDoc()) copyText(ovDoc().text, "overview text"); break;
    case "sfile": if (split) enterSplitFile(); break;
    case "sov": focusSplit(true); break;
    case "back": if (view.from) anchorBack(); break;
    case "dflip": flipDiff(); break;
    case "dfold": if (view.diff && !view.diff.notice) toggleDiffFold(); break;
    case "stack": if (split) toggleSplitStack(); break;
    case "sfold": if (split && split.stack) foldStackFile(); break;
    case "sfoldall": if (split && split.stack) foldAllStack(); break;
  }
});

// A layer closed from outside (another surface clearing the stack) must not
// leave the viewer's chips behind: the footer follows the stack on every key.
document.addEventListener("keyup", () => {
  if (footOwned("viewer") && !isOpen()) {
    // Closed from outside (another surface cleared the stack): the file goes
    // to the background, not away.
    if (view.id) closeViewer("background");
    else swapFoot(false);
  }
});

// viewerRange is the band for lines line..end of a file of len lines: none
// for a single line or a range starting past the end (the TUI's rule), the
// end clamped to the last line. Pure — TestViewerRangeDecisions runs it.
function viewerRange(line, end, len) {
  if (!(end > line) || line < 1 || line > len) return null;
  return { start: line, end: Math.min(end, len) };
}

// viewerStep is one shift+↓ (dir 1) / shift+↑ (dir -1) in the viewer: the
// band (own: the reader's, or null) grows or shrinks at the end away from the
// cursor, which stays put (a band the cursor left starts over from it).
// {range, end}: range null = back to the one line;
// null when the moving end is already at the file's edge. Pure.
function viewerStep(own, cur, len, dir) {
  if (!len || cur < 1) return null;
  // A band the cursor has walked away from is not "from the cursor": start over.
  if (own && cur !== own.start && cur !== own.end) own = null;
  const anchor = own && cur === own.end ? own.end : own ? own.start : cur;
  const end = own ? (anchor === own.start ? own.end : own.start) : cur;
  const next = Math.min(Math.max(end + dir, 1), len);
  if (next === end) return null;
  return { range: viewerRange(Math.min(anchor, next), Math.max(anchor, next), len), end: next };
}

// stepViewerRange applies viewerStep to the open file and keeps the moving
// end in sight. A range the reader does not own starts over from the cursor
// (an overview's anchors are bands, never this range — viewBands).
function stepViewerRange(dir) {
  if (view.placeholder) return;
  const own = view.range && view.rangeOwn ? view.range : null;
  const got = viewerStep(own, view.cur, view.lines.length, dir);
  if (!got) return;
  const prev = view.range;
  view.range = got.range;
  view.rangeOwn = true;
  if (!paintViewerBand(prev, view.range)) rerenderKeepingScroll();
  const row = view.diff
    ? $("viewer-body").querySelector(`.vline[data-r="${view.diff.by[got.end]}"]`)
    : $("viewer-body").querySelector(`.vline[data-i="${got.end - 1}"]`);
  if (row) row.scrollIntoView({ block: "nearest" });
}

// paintViewerBand moves the band from prev to next (either may be null) on
// the lines already rendered — a repaint of a large file costs a third of a
// second, a step must not. False when the body is not the file's lines.
function paintViewerBand(prev, next) {
  const body = $("viewer-body");
  if (view.ov || view.diff || view.image || view.placeholder || !body.querySelector(".vline")) return false;
  const has = (r, n) => !!r && r.start <= n && n <= r.end;
  // Only the lines that enter or leave the band: a step is one line.
  for (const r of [prev, next]) {
    if (!r) continue;
    for (let n = r.start; n <= r.end; n++) {
      if (has(prev, n) === has(next, n)) continue;
      const el = body.querySelector(`.vline[data-i="${n - 1}"]`);
      if (el) el.classList.toggle("vrange", has(next, n));
    }
  }
  return true;
}

// viewerLinkHere is the content link the viewer offers now: the band's lines,
// else the cursor line. The link names the file ON DISK, so a fingerprint is
// taken only when the viewer shows the disk's text — a commit's or a shelf's
// version of the line may say something else. {link, label}; null = none.
function viewerLinkHere() {
  const line = view.placeholder ? 0 : view.cur;
  const ctx = { path: view.path, state: "unstaged", hint: { kind: "view", id: "content" } };
  const rg = view.placeholder ? null : view.range;
  if (rg) {
    const block = view.src === "worktree" ? view.lines.slice(rg.start - 1, rg.end).map((l) => l.text) : null;
    const link = linkFor(state.repo, state.worktree, ctx, "new", rg.start, "", rg.end, block);
    if (link) return { link, label: "copy file link (lines " + rg.start + "-" + rg.end + ")" };
  }
  const text = line && view.src === "worktree" && view.lines[line - 1] ? view.lines[line - 1].text : "";
  const link = linkFor(state.repo, state.worktree, ctx, "new", line, text);
  return link ? { link, label: "copy file link" + (line ? " (line " + line + ")" : "") } : null;
}

// copyViewerLinkHere is L: the menu's first row without the menu.
function copyViewerLinkHere() {
  if (onDeletedRow() && !view.range) return opLine("a deleted line has no link in the reviewed version", false);
  const got = viewerLinkHere();
  if (got) copyViewerLink(got.link);
}

// markViewerRange bands a range link's lines in the open file.
function markViewerRange(line, end) {
  view.range = viewerRange(line, end, view.lines.length);
  view.rangeOwn = true;
  if (view.range) rerenderKeepingScroll();
}

// clearViewerRange drops a range marked by hand or by a link (an overview's
// anchor bands are not a range: they stay); false when there was none.
function clearViewerRange() {
  if (!view.range || !view.rangeOwn) return false;
  view.range = null;
  rerenderKeepingScroll();
  paintCursor();
  return true;
}

// openViewerMenu is the viewer's . menu (and right-click): the content link
// and the text of the cursor line, then the file's diff, history and blame at
// this version. The surfaces it opens replace the viewer — one full-page
// overlay at a time.
function openViewerMenu(x, y) {
  const line = view.placeholder ? 0 : view.cur;
  const items = [];
  // The band's lines, else the cursor line (viewerLinkHere — L copies the same).
  const here = viewerLinkHere();
  if (here && !(onDeletedRow() && !view.range)) items.push({ label: here.label, act: () => copyViewerLink(here.link) });
  if (line && view.lines[line - 1] && !onDeletedRow()) items.push({ label: "copy line", act: () => copyText(view.lines[line - 1].text, "line " + line) });
  items.push({ sep: true });
  if (view.src === "worktree") items.push({ label: "diff (working tree changes)", act: () => viewerDiffWorktree(view.path) });
  if (view.src === "commit") items.push({ label: "diff (this commit's change)", act: () => viewerDiffCommit(view.rev, view.path) });
  if (view.src !== "shelf") {
    const rev = view.src === "commit" ? view.rev : "";
    const path = view.path;
    items.push({ label: "file history", act: () => { closeViewer("background"); openFileHistory(path, rev); } });
    items.push({ label: "blame", act: () => { closeViewer("background"); openFileBlame(path, rev); } });
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
  if (disk.missing || disk.too_large || disk.binary || !sameLines(disk.lines || [], view.lines)) {
    return opLine("the file on disk differs from this version — no content link", true);
  }
  copyLink(flink, linkDesc("file", view.path, ""));
}

// openWorktreeFileDiff opens a working-tree file's pending change the way a
// click on its row does: the unstaged change (index → disk), else the staged
// one (HEAD → index) — /api/diff has no HEAD ↔ working tree lane. A file
// with neither says so. Shared by the viewer's menu and F's (plan 5d).
async function openWorktreeFileDiff(path) {
  await openWorkingTree(0);
  let i = state.statusEntries.findIndex((f) => f.path === path && f.section !== "staged");
  if (i < 0) i = state.statusEntries.findIndex((f) => f.path === path && f.section === "staged");
  if (i < 0) return opLine(path + " has no changes in the working tree", false);
  await openFile(i);
}

// viewerDiffWorktree: the viewer steps back to the background first — and F,
// when the viewer was opened over it, steps aside so the stage shows.
async function viewerDiffWorktree(path) {
  closeViewer("background");
  closeFinder();
  await openWorktreeFileDiff(path);
}

// viewerDiffCommit opens the commit and the file's row in it.
async function viewerDiffCommit(rev, path) {
  closeViewer("background");
  closeFinder();
  if (!(await openCommitByHash(rev, rev.slice(0, 8), { thenFile: true }))) return;
  const i = state.files.findIndex((f) => f.path === path);
  if (i < 0) return opLine(path + " is not changed in " + rev.slice(0, 8), false);
  await openFile(i);
}

// viewerHello re-reports what this tab shows after its event stream
// (re)connected — the server dropped it when the old stream ended (L2).
function viewerHello() {
  if (viewerFileId()) ofPost({ op: "shown", id: view.id }).catch(() => {});
}

// viewerOpenFiles: the list changed. A file this viewer shows that left it
// was closed in another tab's switcher (x) — this viewer goes too.
function viewerOpenFiles(files) {
  if (split && split.id && !files.some((f) => f.id === split.id)) splitOverviewGone(splitName());
  const id = viewerFileId();
  if (!id || files.some((f) => f.id === id)) return;
  const name = docName(view);
  places.delete(id);
  dropViewer();
  opLine(name + " was closed in another tab", false);
}

// viewerFileChanged: the file on disk changed — re-read it keeping this
// tab's place (L9). A newer open (a link landing) wins over the reload.
async function viewerFileChanged(id) {
  if (!id || id !== viewerFileId() || view.ov) return;
  const seq = loadSeq;
  let body;
  try {
    body = await fetchContent(view.src, view.rev, view.path, remarksReview(view.from));
  } catch {
    return; // the next change retries; a dead server paints its own veil
  }
  if (seq !== loadSeq || id !== view.id) return;
  const el = $("viewer-body");
  const top = el.scrollTop, left = el.scrollLeft;
  view.lines = body.lines || [];
  view.notes = body.missing ? view.notes : notesOf(body, view.src); // a deleted file keeps its notes for its return
  view.remarks = body.missing ? view.remarks : body.remarks || [];
  view.placeholder = placeholderFor(body, view.lines);
  view.image = imageOf(body, view.src, view.rev, view.path);
  view.cur = keepLine(view.cur, view.lines.length, !!body.missing);
  // The file shrank on disk: the band ends with it (gone when it began past the end).
  if (view.range) view.range = viewerRange(view.range.start, view.range.end, view.lines.length);
  renderViewer();
  el.scrollTop = top;
  el.scrollLeft = left;
  if (view.diff) refreshDiff(); // the working tree moved: the diff with it
  if (isOpen()) swapFoot(true);
}

// viewerAgentDocs: the agent-docs store changed (a note added, dismissed or
// moved, an overview added, set or closed — here, in another tab, or in the
// TUI hosting this page). An overview on screen that left the store closes;
// a file this viewer shows that left the list goes; one that stays is
// re-read, which aligns its notes (or re-checks its anchors — only when its
// stamp moved: a change elsewhere in the store leaves it as it is).
function viewerAgentDocs(files, closed = [], stamps = undefined) {
  if (split && split.id) { // the split's left column follows the store too
    if (fromGone(stamps, closed, split.id)) splitOverviewGone(splitName());
    else if (overviewStale(stamps, split.id, split.ov.stamp)) refreshSplitOverview();
  }
  const id = viewerFileId();
  if (id && view.ov && closed.includes(id)) {
    const name = docName(view);
    places.delete(id);
    dropViewer();
    return opLine(name + " was closed", false);
  }
  viewerOpenFiles(files);
  if (!viewerFileId()) return;
  // The shown file's bands follow the overview that opened it: gone with it,
  // re-read when it changed. A closed viewer has none to follow.
  const f = view.from;
  if (f && !f.closed && !f.stored && !view.ov && !split) { // a stored overview's way back is page-local: never in the stamps; the split follows its own
    if (fromGone(stamps, closed, f.id)) {
      f.closed = true;
      rerenderKeepingScroll();
      swapFoot(true);
    } else if (overviewStale(stamps, f.id, f.stamp)) refreshFromAnchors(f);
  }
  if (view.ov) {
    if (overviewStale(stamps, view.id, view.ov.stamp)) refreshOverview();
  } else viewerFileChanged(viewerFileId());
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
    "<b>↑↓ j k</b> line, <b>shift+↓↑</b> mark lines from the cursor (or shift+click a number), <b>L</b> copy their link, <b>/ ] [</b> find, <b>w</b> long lines, <b>.</b> menu (copy file link at the line, copy line, diff, history, blame), <b>esc</b> close; " +
    "an agent's notes (<code>gg session note</code>) sit under their lines: <b>} {</b> next / previous note, <b>d</b> dismiss, <b>r</b> copy its reference; " +
    "an agent's overview (<code>gg session overview</code>) or a review's stored one opens beside the file its selected link points to when the window is wide enough: " +
    "<b>↑↓</b> step its links and the file follows, <b>enter</b> or <b>tab</b> moves into the file, <b>tab / backspace</b> back, <b>N / P</b> step the overview from the file, " +
    "the link whose band holds the cursor is marked in the bands' colour, a review's remarks show as read-only boxes, a click on a link shows its file; " +
    "<b>S</b> stacks every file of the review (or every file the links name) in one scroll beside the overview — <b>↑↓</b> lines across files, <b>N / P</b> file headers, <b>n / p</b> bands, " +
    "<b>- / _</b> fold one / all, the review's remarks and an agent's notes read-only, <b>S</b> again back to the single file at the cursor; " +
    "<b>r</b> copies a link's reference, <b>y</b> the text, <b>esc</b> closes both (an agent's overview stays in the switcher; <b>x</b> there closes it). " +
    "A narrow window shows the overview alone: <b>tab / shift+tab</b> select a link, <b>enter</b> or a click opens it, <b>backspace</b> (or the browser's Back) in that file comes back. " +
    "In a file an overview's link opened, <b>D</b> (or the <b>file | diff</b> switch in its title) flips to the change the review made to it — an agent's overview: HEAD → the working tree — " +
    "drawn as one column, the whole file with the deleted lines red with − and the added green with +, the link's lines banded and the review's remarks under their lines (one on a deleted line too); " +
    "the choice holds while you step the overview's links until <b>D</b> again or <b>esc</b>; <b>f</b> in the diff shows the changed lines only. " +
    "In the file the overview's line and range links are tinted bands, the one you are on brighter, and <b>n / p</b> step through them (in line order, wrapping) without moving the overview's selection",
});

export { closeViewer, dropViewer, evictedText, markViewerRange, openViewer, openWorktreeFileDiff, versionLabel, viewerAgentDocs, viewerClosedFile, viewerFileChanged, viewerFileId, viewerHello, viewerOpenFiles };
