// viewer.js — the file viewer overlay (open files on the web, plan 5a): one
// file at one version — the working tree, a commit, a shelf entry — with a
// line cursor, the in-view search and a . menu. Content links land here.
import { $, charWidth, elidePath, esc, fmtBytes, getJSON, postJSON, state, tabId } from "./core.js";
import { closeLayer, copyText, footOwned, mountOverlay, popFoot, pushFoot, pushLayer, showCtxMenu, topLayer } from "./layers.js";
import { Search } from "./inviewsearch.js";
import { bindSearchBar } from "./searchbar.js";
import { cycleTextMode, openFile, openWorkingTree, renderCell } from "./files.js";
import { mdHTML } from "./markdown.js";
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
// --- end overview model ---

// --- the overlay -------------------------------------------------------------
// view is the ONE file on screen: its version, its lines, the cursor (1-based,
// 0 = no line) and the placeholder shown instead of lines ("" = none). An
// overview shows in document mode: ov holds it ({title, text, blocks,
// anchors, sel}). from is the way back to the overview an anchor opened this
// file from ({id, sel}, one step deep, this tab's); range the lines that
// anchor named.
const view = { id: "", src: "worktree", rev: "", path: "", lines: [], cur: 0, placeholder: "", image: null, notes: [], ov: null, from: null, range: null };
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

function fetchContent(src, rev, path) {
  return getJSON("/api/file-content?src=" + encodeURIComponent(src) + "&rev=" + encodeURIComponent(rev) + "&path=" + encodeURIComponent(path));
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
  `<div id="viewer-box"><div id="viewer-title"></div>` +
  `<div id="viewer-search" class="hidden search-bar"><span id="viewer-search-lead">/</span>` +
  `<input id="viewer-search-input" type="text" autocomplete="off" spellcheck="false" placeholder="find in this file — enter keeps it, ] [ step, esc clears">` +
  `<span id="viewer-search-count"></span></div>` +
  `<div id="viewer-body" tabindex="-1"></div></div>`;

viewerRoot.addEventListener("click", (e) => {
  if (e.target.id === "viewer") closeViewer(escHow(!!view.ov, view.notes.length)); // the backdrop is esc; the box is not
});
$("viewer-body").addEventListener("click", (e) => {
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
  const row = e.target.closest(".vline[data-i]");
  if (!row) return;
  // Shift+click on a line NUMBER marks the lines from the cursor to it (the
  // cursor stays): the range "copy file link" then names.
  if (e.shiftKey && e.target.closest(".vno")) {
    const hit = Number(row.dataset.i) + 1;
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
  view.cur = Number(row.dataset.i) + 1;
  if (clearViewerRange()) return;
  paintCursor();
});
$("viewer-body").addEventListener("mousedown", (e) => {
  if (e.shiftKey && e.target.closest(".vno")) e.preventDefault(); // no text selection from the old caret
});
$("viewer-body").addEventListener("contextmenu", (e) => {
  const row = e.target.closest(".vline[data-i]");
  if (row) view.cur = Number(row.dataset.i) + 1;
  e.preventDefault();
  paintCursor();
  openViewerMenu(e.clientX, e.clientY);
});

// openViewer shows path at one version and registers it as this tab's open
// file (reused when already open). id (the switcher) brings an entry back by
// id. The cursor lands per pickLine; a second call replaces the file on
// screen, which stays open in the background.
async function openViewer({ src = "worktree", rev = "", path = "", line = 0, id = "" }) {
  const seq = ++loadSeq;
  openSeq = seq;
  for (const [k, at] of closedAt) if (at < seq) closedAt.delete(k); // closed before this open: no open in flight can land
  if (isOpen()) rememberPlace();
  let reg, body, ov, ovMine;
  try {
    await ofSync; // a failed open's report reaches the server first
    reg = id ? await ofPost({ op: "focus", id }) : await ofPost({ op: "open", src, rev, path, line });
    if (reg.file.source === "overview") {
      ovMine = ++ovSeq; // a refresh started before this fetch is older than its answer
      ov = await fetchOverview(reg.file.id);
    } else body = await fetchContent(reg.file.source, reg.file.rev || "", reg.file.path);
  } catch (e) {
    reportShown(seq, reg);
    opLine("view failed: " + (e.message || e), true);
    return { ok: false, notice: "" };
  }
  // A newer open won, the viewer closed, or x closed this file everywhere
  // while it loaded here.
  if (seq !== loadSeq || closedAt.get(reg.file.id) >= seq) {
    reportShown(seq, reg);
    return { ok: false, notice: "" };
  }
  const f = reg.file;
  view.from = view.range = null; // an anchor's open sets them after
  view.rangeOwn = false;
  // A refresh that started after this fetch and landed first is newer: keep it.
  if (ov && view.ov && view.id === f.id && !ovAnswerApplies(ovMine, ovApplied)) ov = view.ov;
  if (ov) return showOverview(f, ov, ovMine);
  view.ov = null;
  Object.assign(view, { id: f.id, src: f.source, rev: f.rev || "", path: f.path, lines: body.lines || [] });
  view.notes = notesOf(body, view.src);
  view.placeholder = placeholderFor(body, view.lines);
  view.image = imageOf(body, view.src, view.rev, view.path);
  const place = line > 0 ? null : places.get(f.id);
  const want = pickLine(line, place, f.line || 0);
  const landed = line > 0 ? landLine(line, view.lines.length, f.path) : { line: view.placeholder && body.missing ? want : clampLine(want || 1, view.lines.length), notice: "" };
  view.cur = landed.line;
  viewerSearchBar.reset(); // a new file is a new search
  viewerRoot.style.bottom = $("foot").offsetHeight + "px"; // the bar stays in sight
  pushLayer("viewer", viewerRoot, { onKey: viewerKey });
  swapFoot(true);
  paintTitle();
  renderViewer();
  centerCursor();
  if (place) $("viewer-body").scrollTop = place.top;
  $("viewer-body").focus({ preventScroll: true });
  // A reopen lands where THIS tab left the file; the server (every tab's
  // switcher) still has the line it last heard.
  if (landed.line !== (f.line || 0)) reportCursor();
  clearOpLine(f.path + " opened in the background"); // it is in front now
  if (landed.notice) opLine(landed.notice, false);
  if (reg.evicted) opLine(evictedText(reg.evicted, reg.cap), false);
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
  Object.assign(view, { id: f.id, src: "overview", rev: "", path: f.path, lines: [], notes: [], cur: 0, placeholder: "", image: null });
  view.ov = { ...ov, sel: -1, want: "" };
  ovApplied = Math.max(ovApplied, mine);
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
  Object.assign(view, { id: doc.id, src: "overview", rev: "", path: "", lines: [], notes: [], cur: 0, placeholder: "", image: null, from: null, range: null, rangeOwn: false });
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
  view.id = "";
  loadSeq++; // an open or reload in flight must not bring it back
  clearTimeout(cursorTimer);
  closeLayer("viewer");
  swapFoot(false);
}

function backgroundViewer() {
  if (view.ov && view.ov.stored) return closeViewer("close"); // page-local: nothing to background
  const name = docName(view);
  closeViewer("background");
  opLine(name + " is in the background", false);
}

// paintTitle cuts the PATH in the middle, never the file name, to fit.
function paintTitle() {
  const el = $("viewer-title");
  if (view.ov) {
    el.title = view.ov.title;
    el.textContent = view.ov.stored ? "Overview: " + view.ov.title : "Overview " + view.id + " · " + view.ov.title;
    return;
  }
  const lead = "View ", tail = " (" + versionLabel(view.src, view.rev) + ")";
  const cols = Math.floor((el.clientWidth - 28) / charWidth()) - lead.length - tail.length;
  el.title = view.path;
  el.textContent = lead + (cols > 0 ? elidePath(view.path, cols) : view.path) + tail;
}

function renderViewer() {
  if (viewerSearch.query) viewerSearch.refind(view.lines.map((l, i) => ({ row: i, side: 0, text: l.text || "" })));
  const body = $("viewer-body");
  if (view.ov) {
    body.innerHTML = `<div class="vdoc md">${mdHTML({ blocks: view.ov.blocks }, esc, { anchors: true })}</div>`;
    paintAnchors();
  } else if (view.placeholder) {
    body.innerHTML = `<div class="notice">${esc(view.placeholder)}</div>`;
  } else if (view.image) {
    body.innerHTML = imageHTML(view.image);
  } else {
    let html = "";
    const bands = viewBands(), curB = view.from ? bandOf(bands, view.from.anchors || [], view.from.cur, view.lines.length) : -1;
    view.lines.forEach((l, i) => {
      const noted = noteAtLine(view.notes, i + 1) ? " vnoted" : "";
      const ranged = view.range && view.range.start <= i + 1 && i + 1 <= view.range.end ? " vrange" : "";
      const bk = bandKindAt(bands, curB, i + 1);
      const banded = bk ? " vanchor" + (bk === "cur" ? " acur" : "") : "";
      html +=
        `<div class="vline${i + 1 === view.cur ? " vcur" : ""}${noted}${ranged}${banded}" data-i="${i}"><span class="vno">${i + 1}</span>` +
        `<span class="vtext">${renderCell(l.text, null, l.tok, "", viewerSearch.query ? viewerSearch.hitsOn(i, 0) : null) || " "}</span></div>`;
      for (const n of boxesAfter(view.notes, i)) html += noteBoxHTML(n);
    });
    body.innerHTML = html;
  }
  viewerSearchBar.paint();
}

// ---- an agent's overview (agentdocs) ---------------------------------------
// paintAnchors marks the selected anchor and the missing ones.
function paintAnchors() {
  for (const el of $("viewer-body").querySelectorAll("a.md-anchor")) {
    const a = view.ov.anchors[Number(el.dataset.a)];
    el.classList.toggle("asel", Number(el.dataset.a) === view.ov.sel);
    el.classList.toggle("agone", !!(a && a.missing));
  }
}

function anchorEl(i) {
  return $("viewer-body").querySelector(`a.md-anchor[data-a="${i}"]`);
}

function selectAnchor(i) {
  view.ov.sel = i;
  view.ov.want = view.ov.anchors[i]?.dest || ""; // kept while a refresh drops it, back when it returns
  paintAnchors();
  anchorEl(i)?.scrollIntoView({ block: "nearest" });
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
  f.cur = f.dest = a.dest; // backAnchor returns to it
  f.sel = b.i;
  view.cur = b.start;
  rerenderKeepingScroll();
  centerCursor();
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
    const r = await openViewer(storedAnchorOpen(stored.tip, a));
    if (!r.ok) return;
    view.from = back;
    armBack();
    rerenderKeepingScroll();
    swapFoot(true);
    return;
  }
  const t = anchorTarget(a);
  if (!t.path) return opLine("note " + t.note + " is gone", false);
  // The overview's anchors travel with the way back: the file draws its
  // own as bands (viewBands), the opened one current — a note is no band.
  const from = { id, sel: view.ov.sel, dest: a.dest, anchors: view.ov.anchors, stamp: view.ov.stamp, cur: t.note ? "" : a.dest };
  const r = await openViewer({ src: "worktree", path: t.path, line: t.line });
  if (!r.ok) return;
  view.from = from;
  armBack();
  rerenderKeepingScroll();
  swapFoot(true);
  if (t.note) $("viewer-body").querySelector(`.vnote[data-note="${t.note}"]`)?.scrollIntoView({ block: "nearest" });
}

window.addEventListener("popstate", () => {
  ownBack = history.state?.gg === "back"; // Forward can put it back on top
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
  if (view.ov.stored) return opLine("a stored overview's anchor has no reference — copy the review link instead", false);
  const a = view.ov.anchors[view.ov.sel];
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

function noteBoxHTML(n) {
  return (
    `<div class="vnote${n.outdated ? " outdated" : ""}" data-note="${esc(n.id)}">` +
    `<div class="vnote-title">${esc(noteBoxTitle(n))}</div>` +
    `<div class="vnote-sum">${esc(n.summary)}</div>` +
    (n.rationale ? `<div class="vnote-why">${esc(n.rationale)}</div>` : "") +
    `<div class="vnote-acts"><button data-nact="dismiss">dismiss</button><button data-nact="ref">copy reference</button></div></div>`
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
  const line = nextNoteLine(view.notes, view.cur, dir);
  if (line) {
    view.cur = line;
    paintCursor();
    for (const b of $("viewer-body").querySelectorAll(".vnote")) {
      if (noteAtLine(view.notes, line)?.id === b.dataset.note) b.scrollIntoView({ block: "nearest" });
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
  const t = dir > 0 ? view.notes[0] : view.notes[view.notes.length - 1];
  if (r.ok && t) {
    view.cur = t.start;
    paintCursor();
    centerCursor();
  }
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
  reportCursor();
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
  if (view.ov) return overviewKey(e);
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
    case "Home": case "g": view.cur = clampLine(1, view.lines.length); paintCursor(); break;
    case "End": case "G": view.cur = view.lines.length; paintCursor(); break;
    case "w": cycleTextMode(); break;
    case ".": menuAtCursor(); break;
    case "d": { const n = noteAtLine(view.notes, view.cur); if (!n) return false; dismissNote(n.id); break; }
    case "r": { const n = noteAtLine(view.notes, view.cur); if (!n) return false; copyNoteRef(n); break; }
    case "}": stepNote(1); break;
    case "{": stepNote(-1); break;
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
// viewerFoot is the chips; a file with an agent's notes adds theirs.
function viewerFoot() {
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
  const notes = view.notes.length
    ? `<span>} { notes</span><button data-vact="dismiss">d dismiss</button><button data-vact="ref">r reference</button>`
    : "";
  return (
    back + bandKeys + `<span>↑↓ j k line</span><button data-vact="find">/ find</button><span>] [ next / prev</span>` + notes +
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
    case "dismiss": { const n = noteAtLine(view.notes, view.cur); if (n) dismissNote(n.id); else opLine("no note on this line", false); break; }
    case "ref": { const n = noteAtLine(view.notes, view.cur); if (n) copyNoteRef(n); else opLine("no note on this line", false); break; }
    case "aopen": if (view.ov && view.ov.sel >= 0) openAnchorAt(view.ov.sel); else opLine("no anchor selected — tab selects one", false); break;
    case "aref": if (view.ov) copyAnchorRef(); break;
    case "ytext": if (view.ov) copyText(view.ov.text, "overview text"); break;
    case "back": if (view.from) anchorBack(); break;
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
  const row = $("viewer-body").querySelector(`.vline[data-i="${got.end - 1}"]`);
  if (row) row.scrollIntoView({ block: "nearest" });
}

// paintViewerBand moves the band from prev to next (either may be null) on
// the lines already rendered — a repaint of a large file costs a third of a
// second, a step must not. False when the body is not the file's lines.
function paintViewerBand(prev, next) {
  const body = $("viewer-body");
  if (view.ov || view.image || view.placeholder || !body.querySelector(".vline")) return false;
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
  if (here) items.push({ label: here.label, act: () => copyViewerLink(here.link) });
  if (line && view.lines[line - 1]) items.push({ label: "copy line", act: () => copyText(view.lines[line - 1].text, "line " + line) });
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
    body = await fetchContent(view.src, view.rev, view.path);
  } catch {
    return; // the next change retries; a dead server paints its own veil
  }
  if (seq !== loadSeq || id !== view.id) return;
  const el = $("viewer-body");
  const top = el.scrollTop, left = el.scrollLeft;
  view.lines = body.lines || [];
  view.notes = body.missing ? view.notes : notesOf(body, view.src); // a deleted file keeps its notes for its return
  view.placeholder = placeholderFor(body, view.lines);
  view.image = imageOf(body, view.src, view.rev, view.path);
  view.cur = keepLine(view.cur, view.lines.length, !!body.missing);
  // The file shrank on disk: the band ends with it (gone when it began past the end).
  if (view.range) view.range = viewerRange(view.range.start, view.range.end, view.lines.length);
  renderViewer();
  el.scrollTop = top;
  el.scrollLeft = left;
  if (isOpen()) swapFoot(true);
}

// viewerAgentDocs: the agent-docs store changed (a note added, dismissed or
// moved, an overview added, set or closed — here, in another tab, or in the
// TUI hosting this page). An overview on screen that left the store closes;
// a file this viewer shows that left the list goes; one that stays is
// re-read, which aligns its notes (or re-checks its anchors — only when its
// stamp moved: a change elsewhere in the store leaves it as it is).
function viewerAgentDocs(files, closed = [], stamps = undefined) {
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
  if (f && !f.closed && !f.stored && !view.ov) { // a stored overview's way back is page-local: never in the stamps
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
    "an agent's overview (<code>gg session overview</code>) opens as a document: <b>tab / shift+tab</b> select an anchor, <b>enter</b> or a click opens it, " +
    "<b>r</b> copies its reference, <b>y</b> the text, <b>esc</b> steps aside (<b>x</b> in the switcher closes it); <b>backspace</b> (or the browser's Back) in the file an anchor opened comes back; " +
    "in that file the overview's line and range anchors are tinted bands, the one you are on brighter, and <b>n / p</b> step through them (in line order, wrapping)",
});

export { closeViewer, dropViewer, evictedText, markViewerRange, openViewer, openWorktreeFileDiff, versionLabel, viewerAgentDocs, viewerClosedFile, viewerFileChanged, viewerFileId, viewerHello, viewerOpenFiles };
