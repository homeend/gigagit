// stackview.js — the stacked diff view: every file of the change set on
// screen as one section (a sticky header, its diff below) in ONE scroll —
// GitHub's "Files changed". The pure half (slots, pick order, reconcile,
// counts) is stack.js. files.js routes openFile here while the S toggle is
// on and calls the lifecycle hooks (teardown on leaving the diff stage,
// reconcile after a status re-read, re-render on f / w / resize).
//
// state.stack = {list, group, slots, near, anchor, inFlight}. list is the
// active file list ARRAY it was built from: a new commit, a filter change or
// a status re-read hands out a new array, which is how openStack knows a
// click is a scroll within this stack or a new one.

import { $, esc, getJSON, state } from "./core.js";
import { finishPaint, onPainted, paintTable } from "./diffpaint.js";
import { fetchToks, patchCells } from "./difftok.js";
import { saveUI } from "./uistate.js";
import { opLine } from "./ops.js";
import { registerHelp } from "./menus.js";
import { focusPane } from "./keys.js";
import { reviewActive, reviewSummaryHTML, showReviewSummary } from "./reviews.js";
import { seedCollapsed } from "./notebox.js";
import { noteHTML } from "./shelfnotes.js";
import { abortDiffTok, curHitEl, repaintDiffHits, activeFileList, changeStepTarget, clearDiffHunks, diffChangeBlocks, closeConflictPick, diffHTML, diffTable, diffSearch, diffSearchBar, enterFilesStage, goToDiffHit, markDiffRow, setDiffRange, firstHeldLine, diffRowAt, globalNoteCtx, hunkEligible, notesArmed, rowLinkCtx, notesFor, rowNoteCount, rowNoteCtx, fileDiffURL, getDiff, footImageChip, mountPanBars, openFile, openStatusDiff, renderFiles, hunkState, landChange, setLayout, stackHuntSlots, updateDiffNav, diffBody, diffBodyEl } from "./files.js";
import { hitPos, stepHit, stepHitStrict } from "./inviewsearch.js";
import { GLYPH, KIND_TIP, STACK_MAX_IN_FLIGHT, buildSlots, countsFromDiff, estimateHeight, nextNoted, nextToLoad, noContentWhy, reconcileSlots, stackGroup, stackRows, slotKey, statusLetter, toUnfold } from "./stack.js";

const ROW_PX = 20; // a diff row's rendered height, for placeholder estimates
let observer = null;
let seeObs = null; // what is ON screen (not near): a folded file scrolled into view unfolds
let syncRaf = 0;

// stackOn: the toggle is on. Every file-list screen stacks — the symmetric
// view's stack holds its VISIBLE rows (state.files = symRows there).
function stackOn() {
  return !!(state.ui && state.ui.stacked_diff);
}

function teardownStack() {
  if (state.stack) abortSlotToks(state.stack.slots);
  if (observer) observer.disconnect();
  observer = null;
  if (seeObs) seeObs.disconnect();
  seeObs = null;
  state.stack = null;
  syncStackChrome();
  diffSearchBar.reset(); // leaving the stack is leaving the view (design D6)
}

// openStack shows list row i inside a stack: a scroll when the stack on
// screen was built from this same list (and working-tree group), else a new
// stack anchored there. openFile has already put the diff stage up.
function openStack(i) {
  const list = activeFileList();
  const group = state.filesMode === "status" ? stackGroup(list[i] || {}) : "";
  const st = state.stack;
  if (st && st.list === list && st.group === group) return scrollToFile(st, i);
  buildStack(list, group, i);
}

async function buildStack(list, group, anchorIdx) {
  teardownStack();
  state.detailGen++; // a single-file diff still loading must not land over the stack
  clearDiffHunks();
  closeConflictPick();
  abortDiffTok();
  state.lastDiff = null; // resize / f / live refresh must not repaint a single diff here
  state.diffCtx = null;
  state.diffRow = state.diffRange = null;
  state.notes = [];
  if (state.layout !== "diff") {
    state.pane = "files";
    setLayout("diff");
    focusPane();
  }
  const slots = buildSlots(stackRows(list, group));
  // A fresh stack starts with no resolved notes anywhere: each slot fetches
  // its own as it loads.
  for (const s of slots) { s.notes = []; s.ctx = null; s.row = null; }
  // painted/want: until the first paint an open (a second openFile in the
  // same tick — applySym and setFilter open row 0, then the kept row) only
  // moves the target; the first paint lands on it.
  const st = { list, group, slots, near: new Set(), anchor: 0, pickK: -1, inFlight: 0, painted: false, want: anchorIdx };
  state.stack = st;
  footImageChip(); // no image pair is up in a stack: w is long lines again
  // Counts FIRST: they size every placeholder. Painted with no counts, all
  // sections are a few rows tall, the whole change set sits "near" the
  // viewport, and the loader fetches the first three files wherever the
  // reader is — lazy in name only. (Best-effort: no counts → small
  // placeholders, as for entry/link sets.)
  diffBodyEl().innerHTML = `<div class="notice">loading…</div>`;
  await loadCounts(st);
  if (state.stack !== st) return; // superseded while the counts loaded
  paintStack(st);
  st.painted = true;
  // A review's stack opened on its Summary stays at the top, where it is.
  if (reviewActive() && state.review.onSummary) {
    $("diff-pane").scrollTop = 0;
    renderFiles();
    updateDiffNav();
    pump(st);
    return;
  }
  scrollToFile(st, st.want);
}

// --- painting -------------------------------------------------------------

const SIDE = { absent: "—", present: "in", deleted: "deletes" };

function headHTML(s) {
  const path =
    s.oldPath && s.oldPath !== s.path ? `${esc(s.oldPath)} → ${esc(s.path)}` : esc(s.path);
  const c = s.counts;
  const counts = !c
    ? ""
    : c.binary
    ? `<span class="stk-bin">bin</span>`
    : `<span class="stk-add">+${c.add}</span> <span class="stk-del">−${c.del}</span>`;
  // a symmetric row says what the two sets have to do with each other, and
  // where each stands (the lists' own glyph and words)
  const glyph = s.kind ? `<span class="symg ${s.kind}" title="${KIND_TIP[s.kind]}">${GLYPH[s.kind]}</span>` : "";
  const sides = s.kind
    ? `<span class="stk-sides">left ${SIDE[s.left] || "—"} · right ${SIDE[s.right] || "—"}</span>`
    : "";
  const letter = s.status === "=" ? "" : esc(s.status); // "=" is the glyph's job
  return (
    `<div class="stk-head" title="${esc(s.path)} — click to collapse / expand (-)">` +
    `<span class="stk-fold">${s.collapsed ? "▸" : "▾"}</span>` +
    glyph +
    `<span class="st ${letter}">${letter}</span>` +
    `<span class="stk-path">${path}</span>` +
    sides +
    `<span class="stk-counts">${counts}</span></div>`
  );
}

function bodyHTML(s) {
  if (s.collapsed) return "";
  if (s.load === "none") {
    if (s.none === "empty") {
      return `<div class="notice">neither set has content for this file — ${esc(noContentWhy(s.f))}</div>`;
    }
    return `<div class="notice">conflicted — <button class="stk-resolve">open resolver</button></div>`;
  }
  if (s.load === "error") return `<div class="notice">error: ${esc(s.error)}</div>`;
  // a kept slot re-fetching after a refresh paints its old diff until the new
  // one lands — through the chunked painter, after this HTML is in (paintSlot)
  if (s.diff) return "";
  return `<div class="stk-ph" style="height:${estimateHeight(s, ROW_PX)}px">loading…</div>`;
}

// slotTable is slot s's diff as the chunked painter's source (diffTable).
function slotTable(s) {
  {
    const nc = { ctx: s.ctx || null, notes: s.notes || [], row: s.row || null, range: s.range || null };
    // The slot paints the stack-wide search and reports the rows it painted,
    // which is what refindStack searches next time — one collapse per slot,
    // and the search and the paint provably share a fold set.
    const hctx = { search: diffSearch, base: slotBase(s), lines: (ls) => (s.lines = ls) };
    const kctx = s.hunks ? { sel: s.hunks.sel } : null;
    // A compare has no notes, but its rows still carry their line numbers:
    // "copy gg link to this line" reads them (ctx.cmpPair, or rowLinkCtx for
    // a slot addressed by side specs).
    const anchored = notesArmed(nc.ctx) || !!(nc.ctx && nc.ctx.cmpPair) || (!nc.ctx && !!rowLinkCtx(s.f));
    s.drawnRows = s.diff.rows || []; // what the search painter redraws cells from (files.js diffRowOf)
    return diffTable(s.diff, $("diff-pane").clientWidth, anchored, s.folds, nc, hctx, kctx);
  }
}

// paintSlot paints a loaded slot's rows into section el's body through the
// shared chunked painter: the screens it can fill now, the rest while idle.
// Its chunks are not skipped off screen while it paints — the stack keeps the
// reader's place by itself (repaintSlot), so every height must be real; each
// is contained at its measured height once painted (diffpaint.js) — and a slot above
// the reader that grows scrolls the pane by as much, the reader's rows
// staying put. Its colours follow when they came late.
function paintSlot(el, s, cover = 0) {
  if (!s.diff || s.collapsed) return;
  const body = el.querySelector(".stk-body");
  const t = slotTable(s);
  s.table = t;
  s.tableFor = s.diff; // the diff those cells are of: late colours patch only them
  const pane = $("diff-pane");
  paintTable(body, t, {
    scroller: pane,
    skip: false,
    cover,
    // the reader is in a later file, or past this one: the rows it grew by
    // went in above them
    onGrow: (px) => {
      const st = state.stack;
      if (!px || !st) return;
      const k = st.slots.indexOf(s);
      if (k < st.anchor || el.getBoundingClientRect().bottom <= pane.getBoundingClientRect().top) pane.scrollTop += px;
    },
  });
  onPainted(body, () => mountSlotBars(el)); // the bars measure the widest line: all of them
  colourSlotLater(state.stack, s);
}

// readingCover: when the reader is inside section el (it meets the screen),
// how far its body must paint at once for their place to hold — past the
// screen's bottom by three screens; 0 = the default first chunks. Measured
// before the body is rewritten.
function readingCover(el) {
  const pane = $("diff-pane"), pr = pane.getBoundingClientRect(), r = el.getBoundingClientRect();
  if (r.top >= pr.bottom || r.bottom <= pr.top) return 0;
  return pr.bottom - r.top + 3 * pane.clientHeight;
}

// colourSlotLater fetches slot s's colours once per diff it shows (a
// deferred answer) and patches the table on screen when it is of that
// diff; one of rows a refresh swapped in unpainted is repainted instead.
// Rows painted later are built from the merged rows.
function colourSlotLater(st, s) {
  const d = s.diff;
  if (!st || !d || !d.tok_pending || s.tokFor === d) return;
  if (s.tokAbort) s.tokAbort.abort();
  const ac = (s.tokAbort = new AbortController());
  s.tokFor = d;
  const live = () => state.stack === st && s.diff === d && !s.collapsed;
  fetchToks(d, ac.signal)
    .then((idx) => {
      if (!idx || !idx.length || !live()) return;
      const k = st.slots.indexOf(s);
      const el = sectionElRaw(k);
      const body = el && el.querySelector(".stk-body");
      if (!body) return;
      if (s.table && s.tableFor === d) patchCells(body, $("diff-pane"), s.table, idx, () => live() && s.tableFor === d);
      else repaintSlot(st, k, true);
    })
    .catch(() => {}); // aborted, or the colours failed: the slot stays plain
}

// abortSlotToks cancels the colour fetches of slots (a stack left behind).
function abortSlotToks(slots) {
  for (const s of slots) {
    if (s.tokAbort) s.tokAbort.abort();
    s.tokAbort = null;
    s.tokFor = null;
  }
}

function sectionHTML(s, k) {
  return (
    `<section class="stk-file${s.collapsed ? " collapsed" : ""}" data-k="${k}">` +
    headHTML(s) +
    `<div class="stk-body">${bodyHTML(s)}</div>` +
    // this file's own long-line bars (scroll mode): sticky at the pane's
    // bottom while the file is on screen, at the file's end after it
    `<div class="hbars stk-hbars hidden"></div></section>`
  );
}

function paintStack(st) {
  const body = diffBodyEl(); // a stack replaces whatever single diff was painting
  // A review's stack starts with its Summary (reviews.js), above the files;
  // a shelf entry's, with its own notes.
  const ov = reviewActive() ? `<div class="stk-ov">${reviewSummaryHTML()}</div>` : "";
  body.innerHTML = `<div class="stk">${ov}${stackNotesHTML()}${st.slots.map(sectionHTML).join("")}</div>`;
  st.slots.forEach((s, k) => {
    const el = sectionElRaw(k);
    if (el) paintSlot(el, s);
  });
  measureChrome();
  const n = st.slots.length;
  $("diff-title").textContent = `${n} file${n === 1 ? "" : "s"} · stacked`;
  if (observer) observer.disconnect();
  observer = new IntersectionObserver(
    (entries) => {
      if (state.stack !== st) return;
      for (const e of entries) {
        const k = Number(e.target.dataset.k);
        if (e.isIntersecting) st.near.add(k);
        else st.near.delete(k);
      }
      pump(st);
    },
    { root: $("diff-pane"), rootMargin: "100% 0px" }
  );
  st.near = new Set();
  for (const el of body.querySelectorAll(".stk-file")) observer.observe(el);
  watchOnScreen(st, body);
  // the pane-wide bars stand aside: each file pans on its own (mountSlotBars)
  $("diff-hbars").classList.add("hidden");
  for (const el of body.querySelectorAll(".stk-file")) mountSlotBars(el);
  syncStackChrome();
  updateDiffNav();
}

// watchOnScreen unfolds every file the stack folded by itself (past
// STACK_COLLAPSE_OVER) whose section is on screen — at the paint, and after
// every scroll: the big stack then loads what is read, as a small one does.
// A fold by hand (-, _, a header click) clears autoFold, so it stays.
function watchOnScreen(st, body) {
  if (seeObs) seeObs.disconnect();
  const onScreen = new Set();
  seeObs = new IntersectionObserver(
    (entries) => {
      if (state.stack !== st) return;
      for (const e of entries) {
        const k = Number(e.target.dataset.k);
        if (e.isIntersecting) onScreen.add(k);
        else onScreen.delete(k);
      }
      const open = toUnfold(st.slots, onScreen);
      for (const k of open) {
        st.slots[k].collapsed = st.slots[k].autoFold = false;
        repaintSlot(st, k, true);
      }
      if (open.length) pump(st);
    },
    { root: $("diff-pane") }
  );
  for (const el of body.querySelectorAll(".stk-file")) seeObs.observe(el);
}

// stackNotesHTML leads a shelf entry's stack with its own notes, one prose
// block each (the TUI's review-overview blocks): what the frozen files could
// not carry reads before them. Not a .stk-file — the loader, the counts and
// n/p never see it.
function stackNotesHTML() {
  const c = state.filesMode === "compare" ? state.compare : null;
  const ns = (c && c.shelfNotes) || [];
  if (!ns.length) return "";
  return `<div class="stk-notes"><div class="stk-notes-head">Notes</div>${ns.map(noteHTML).join("")}</div>`;
}

// measureChrome sizes what the stack lays out around: the file headers stick
// just under the pane's own sticky toolbar, and a tail of one pane height
// after the last file lets ANY header reach the pane top. Without it a file
// near the end cannot be scrolled to (the pane runs out of scroll), the
// header at the top is a file above it, and the list's highlight follows
// that file instead of the one clicked — sharpest where no counts size the
// placeholders (link and entry sets: every unloaded file is a few rows tall).
function measureChrome() {
  const pane = $("diff-pane");
  const head = $("diff-header").offsetHeight;
  pane.style.setProperty("--diff-head-h", head + "px");
  pane.style.setProperty("--stk-tail", Math.max(0, pane.clientHeight - head - 40) + "px");
}

function sectionEl(k) {
  const el = sectionElRaw(k);
  const body = el && el.querySelector(".stk-body");
  if (body) finishPaint(body); // its rows are read: all of them
  return el;
}

// sectionElRaw is section k as it stands (a write, a measure of its box).
function sectionElRaw(k) {
  return diffBodyEl().querySelector(`.stk-file[data-k="${k}"]`);
}

// repaintSlot redraws one section. A section ABOVE the file being read
// changes height when it loads (its placeholder is only an estimate), which
// would push the reader's file away; the reader's header is put back where it
// was. The browser's own scroll anchoring cannot do this reliably — the
// reader's section is often repainted in the same beat, removing the node it
// anchored on — so it is off for the stack (style.css) and done here.
// pinHit: a redraw the reader did not ask for (a file arriving, an unfold
// by the screen, a refresh) keeps the current search hit where it is on
// screen; a fold or unfold by hand keeps what was clicked instead.
function repaintSlot(st, k, pinHit = false) {
  const el = sectionElRaw(k);
  if (!el) return;
  const s = st.slots[k];
  // What stays put on screen: the current search hit while it is on screen
  // below this section (the TUI keeps the cursor's screen row across a
  // rebuild), else the file being read when this one is above it.
  const pin = (pinHit && curHitBelow(el)) || (k < st.anchor ? sectionElRaw(st.anchor) : null);
  const before = pin ? pin.getBoundingClientRect().top : 0;
  const cover = readingCover(el); // before the body is rewritten
  el.classList.toggle("collapsed", s.collapsed);
  el.querySelector(".stk-head").outerHTML = headHTML(s);
  // Its height is held while it is rewritten: emptied for a moment, the pane
  // would clamp its scroll and the reader inside it would land elsewhere.
  const body = el.querySelector(".stk-body");
  body.style.minHeight = s.diff && !s.collapsed ? body.offsetHeight + "px" : "";
  body.innerHTML = bodyHTML(s);
  paintSlot(el, s, cover);
  body.style.minHeight = "";
  mountSlotBars(el); // before the pin: the bars are part of the section's height
  if (pin) $("diff-pane").scrollTop += pin.getBoundingClientRect().top - before;
  updateDiffNav(); // the ‹ change › buttons count the rendered change runs
}

// curHitBelow is the current search hit's element when it is on screen and
// after el (a section about to change height); null otherwise.
function curHitBelow(el) {
  const h = curHitEl(); // by index: an older paint off screen may still wear .cur
  if (!h || el.contains(h) || !(el.compareDocumentPosition(h) & Node.DOCUMENT_POSITION_FOLLOWING)) return null;
  const pane = $("diff-pane").getBoundingClientRect(), r = h.getBoundingClientRect();
  return r.bottom > pane.top && r.top < pane.bottom ? h : null;
}

// mountSlotBars gives one section its own scroll-mode bars: the section's body
// is the pan host (its pan rules and remembered offsets are this
// file's alone), so a bar pans this file and no other. One pane-wide pair
// panned every file's side at once. No table (a placeholder, a notice, a
// folded file) → the bars hide.
function mountSlotBars(el) {
  mountPanBars(el.querySelector(".stk-body"), el.querySelector(".stk-hbars"));
}

// rerenderStack repaints every loaded body for a new width, the f view or
// the w text mode. Browser scroll anchoring keeps the reader's place.
function rerenderStack(resetFolds = false) {
  const st = state.stack;
  if (!st) return;
  measureChrome(); // #diff-header wraps (flex-wrap) at narrow widths
  st.slots.forEach((s, k) => {
    if (resetFolds) s.folds = new Set();
    if (s.diff && !s.collapsed) repaintSlot(st, k);
  });
  // f, w and a resize change WHICH rows exist: the query must be re-found over
  // them, and the bar repainted, or the count goes stale.
  if (diffSearch.query) {
    refindStack();
    diffSearchBar.paint();
  }
}

// repaintStackSlots repaints just these slots (a marked range moved from one
// file to another): the rest of the stack is untouched.
function repaintStackSlots(slots) {
  const st = state.stack;
  if (!st) return;
  for (const s of slots) {
    const k = st.slots.indexOf(s);
    if (k >= 0 && s.diff && !s.collapsed) repaintSlot(st, k);
  }
  // A band unfolds the rows it covers: the query is re-found over them.
  if (diffSearch.query) {
    refindStack();
    diffSearchBar.paint();
  }
}

// --- in-view search ------------------------------------------------------
//
// A stack is many diffs but ONE document: `/` searches every file whose rows
// are here, ] and [ walk the whole stream, and the count is over the stack.
// The pure engine orders hits by (row, side, col) with a NUMERIC row, so each
// slot's rows are keyed into one space: its index times STACK_ROW_SPAN, plus
// the row's index in its own diff. That keeps document order, keeps `data-h`
// unique across the pane (there is ONE Search, so hit indices are global), and
// leaves inviewsearch.js and searchbar.js exactly as the single-file view uses
// them.
const STACK_ROW_SPAN = 1e9; // far past any diff's row count; k * 1e9 stays exact

function slotBase(s) {
  const st = state.stack;
  return st ? st.slots.indexOf(s) * STACK_ROW_SPAN : 0;
}

// searchableSlot: a slot that could hold a hit at all. A conflicted, binary,
// errored or content-less file has no rows of its own, so it is never stepped
// into and never holds the counter's "+" open.
function searchableSlot(s) {
  return !!s && s.load !== "none" && s.load !== "error" && !(s.diff && (s.diff.binary || s.diff.too_large));
}

// unsearchedSlots counts the searchable slots whose rows are NOT in the
// document: folded, or never read. The counter's "+" (design D1).
function unsearchedSlots() {
  const st = state.stack;
  if (!st) return 0;
  return st.slots.filter((s) => searchableSlot(s) && (s.collapsed || s.load !== "ok")).length;
}

// refindStack re-runs the query over EVERY loaded, expanded slot at once and
// repaints the hits near the screen (files.js repaintDiffHits). Re-finding
// per slot would leave the hit list holding only the last slot's hits;
// re-rendering the files that hold hits cost seconds a key on a stack with
// a large file (each line's fold is cached on its row, too).
// keep (every caller but a typed key): the hit the reader is on stays
// current. A file read while a query is typed or kept adds hits; re-finding
// from where typing began would move the current hit (a backward search
// wraps to the stack's new last hit) away from the screen, unpainted.
function refindStack(keep = true) {
  const st = state.stack;
  if (!st) return;
  // An EMPTY query still runs: esc clears the query and then asks the host to
  // render, and that render is the only thing that unpaints the tint the last
  // one left in every section.
  const lines = [];
  if (diffSearch.query) {
    st.slots.forEach((s, k) => {
      if (s.collapsed || !s.diff || !s.lines) return;
      const base = k * STACK_ROW_SPAN;
      for (const l of s.lines) lines.push({ row: base + l.row, side: l.side, text: l.text, fold: l.fold });
    });
  }
  const s = diffSearch, on = keep && s.cur >= 0 && s.cur < s.hits.length;
  s.refind(lines, on ? hitPos(s.hits[s.cur]) : undefined);
  repaintDiffHits();
}

// stackSearchHere is where the reader is with no hit current: the first row on
// screen anywhere in the pane, in the stack-wide key space.
function stackSearchHere() {
  const st = state.stack;
  const pane = $("diff-pane").getBoundingClientRect();
  for (const tr of diffBodyEl().querySelectorAll(".stk-file table.diff tr[data-i]")) {
    if (tr.getBoundingClientRect().bottom < pane.top) continue;
    const sec = tr.closest(".stk-file");
    if (!sec) continue;
    return { row: Number(sec.dataset.k) * STACK_ROW_SPAN + Number(tr.dataset.i), side: 0, col: -1 };
  }
  return { row: (st ? st.anchor : 0) * STACK_ROW_SPAN, side: 0, col: -1 };
}

// awaitSlot waits for one slot's fetch to settle, pumping the queue so it is
// actually picked. Shared by landStackLine, the ]/[ step and the change step:
// all need rows that do not exist yet — so "settled" means PAINTED: load()
// marks a slot ok before it fetches the file's notes, and repaints only after
// them (s.inLoad spans the whole of it). Returning at "ok" handed the caller a
// section still showing its placeholder, and a change step skipped the file.
// The 8 s cap counts only the time the slot sits unpicked; a fetch in flight
// is waited out, however slow the filesystem.
async function awaitSlot(st, s) {
  const settled = () => !s.inLoad && (s.load === "ok" || s.load === "error" || s.load === "none");
  for (let idle = 0; idle < 80 && state.stack === st && !settled(); ) {
    if (!s.inLoad) idle++;
    pump(st);
    await new Promise((r) => setTimeout(r, 100));
  }
  return state.stack === st && s.load === "ok";
}

// stackHitStep is ] / [ in a stack. STRICT first: a hit further on in the
// document wins outright. When the direction runs out, the next slot that is
// folded or unread is unfolded, fetched and re-found — one slot per round
// trip, never a bulk load (design D2) — and the step lands on its edge hit, or
// hands on when it holds none. Only when nothing is left to open does ] wrap,
// exactly as it does single-file (D3).
async function stackHitStep(delta) {
  const st = state.stack;
  if (!st || !diffSearch.query) return;
  const pos = diffSearch.pos(stackSearchHere());
  const i = stepHitStrict(diffSearch.hits, pos, delta);
  if (i >= 0) {
    goToDiffHit(i);
    diffSearchBar.paint();
    return;
  }
  const from = Math.floor((diffSearch.cur >= 0 ? diffSearch.hits[diffSearch.cur].row : pos.row) / STACK_ROW_SPAN);
  const step = delta >= 0 ? 1 : -1;
  for (let k = from + step; k >= 0 && k < st.slots.length; k += step) {
    const s = st.slots[k];
    if (!searchableSlot(s)) continue;
    if (!s.collapsed && s.load === "ok") continue; // already searched
    if (s.collapsed) {
      s.collapsed = false;
      repaintSlot(st, k);
    }
    scrollToFile(st, k);
    if (s.load !== "ok" && !(await awaitSlot(st, s))) return;
    if (state.stack !== st) return;
    refindStack();
    diffSearchBar.paint();
    const lo = k * STACK_ROW_SPAN;
    const mine = [];
    diffSearch.hits.forEach((h, j) => {
      if (h.row >= lo && h.row < lo + STACK_ROW_SPAN) mine.push(j);
    });
    if (mine.length) {
      goToDiffHit(delta >= 0 ? mine[0] : mine[mine.length - 1]);
      diffSearchBar.paint();
      return;
    }
  }
  // Nothing left to search: the wrap stands.
  const w = stepHit(diffSearch.hits, pos, delta);
  if (w >= 0) goToDiffHit(w);
  diffSearchBar.paint();
}

// stackChangeStep is , / . (and ‹ change / change ›) in a stack — the TUI's
// huntChange. The rendered rows decide first, from the viewport
// (changeStepTarget). But a folded or never-read file between the start and
// that target holds changes the document does not have yet: the step opens
// the nearest such file — unfold it, scroll to it so the loader picks it, wait
// for its rows — and lands on its edge change (first going down, last going
// up). One file per round trip, never a bulk load, like ]/[ (stackHitStep); a
// file that turns out to hold no change is passed over. No wrap.
//
// The change last landed on is kept as its ROW (st.changeRow), not an
// ordinal: every file that loads above it shifts the ordinals, and a slot
// repaint replaces the row — then the step re-seats from the viewport.
async function stackChangeStep(delta) {
  const st = state.stack;
  if (!st || st.changeHunting) return;
  const blocks = diffChangeBlocks();
  const pane = $("diff-pane").getBoundingClientRect();
  const tops = blocks.map((tr) => tr.getBoundingClientRect().top);
  const cur = st.changeRow ? blocks.indexOf(st.changeRow) : -1;
  const onScreen = cur >= 0 && tops[cur] >= pane.top && tops[cur] < pane.bottom;
  const slotOf = (tr) => Number(tr.closest(".stk-file").dataset.k);
  const from = onScreen ? slotOf(blocks[cur]) : Math.floor(stackSearchHere().row / STACK_ROW_SPAN);
  const t = changeStepTarget(tops, cur, pane.top, pane.bottom, delta);
  // ahead: t is a change further on in the step's direction, not the clamp.
  const ahead = t >= 0 && (onScreen ? t !== cur : delta > 0 ? tops[t] >= pane.top : tops[t] < pane.top);
  const hole = st.slots.map((s) => searchableSlot(s) && (s.collapsed || s.load !== "ok"));
  const holes = stackHuntSlots(hole, from, ahead ? slotOf(blocks[t]) : null, delta, !onScreen && delta > 0);
  const land = (tr) => {
    st.changeRow = tr;
    landChange(tr);
  };
  if (!holes.length) {
    if (t >= 0) land(blocks[t]);
    return;
  }
  st.changeHunting = true;
  try {
    for (const k of holes) {
      const s = st.slots[k];
      if (s.collapsed) {
        s.collapsed = false;
        repaintSlot(st, k);
      }
      scrollToFile(st, s.idx);
      if (s.load !== "ok" && !(await awaitSlot(st, s))) {
        if (state.stack !== st) return;
        continue;
      }
      if (state.stack !== st) return;
      const sec = sectionEl(k);
      const mine = sec ? diffChangeBlocks(sec) : [];
      if (mine.length) return land(delta > 0 ? mine[0] : mine[mine.length - 1]);
    }
    // Every file on the way was empty: the rendered target stands.
    if (ahead && blocks[t].isConnected) land(blocks[t]);
  } finally {
    st.changeHunting = false;
  }
}

// stackNoteStep is } / { in a stack — the TUI's stackNoteStep. The note last
// landed on (curId) is the place; its file's next note that way wins. Past the
// file's last note the walk continues into the next file that CARRIES notes
// (stack.js nextNoted: a read file by its resolved notes, an unread one by its
// ◆ count — the badge's own rule, rowNoteCount) — unfolded, scrolled to so the
// loader picks it, fetched, one file per round trip like the change step —
// and lands on its first note going down, its last going up. A file whose
// count promised notes but whose rows show none (every thread outdated, or on
// a side the diff lacks) is passed over. No wrap: the ends say so, as the
// terminal does. With nothing landed yet the walk enters from the viewport:
// the file being read (topSlot) answers first — its first ◆ row on or below
// the pane's top (down), its last above it (up) — never a note further on
// that happens to be loaded: a file scrolled past before the loader's turn
// sits unread between, and the walk must open it, not skip it.
async function stackNoteStep(dir, curId, land) {
  const st = state.stack;
  if (!st || st.noteHunting) return;
  const rowsIn = (sec) => (sec ? [...sec.querySelectorAll("tr.note[data-note]")] : []);
  const slotOf = (el) => Number(el.closest(".stk-file").dataset.k);
  const cur = curId ? rowsIn(diffBody()).find((el) => el.dataset.note === curId) : null;
  let from;
  if (cur) {
    from = slotOf(cur);
    const mine = rowsIn(sectionEl(from));
    const i = mine.indexOf(cur) + dir;
    if (i >= 0 && i < mine.length) return land(mine[i]);
  } else {
    from = topSlot();
    const top = $("diff-pane").getBoundingClientRect().top + $("diff-header").offsetHeight;
    const mine = rowsIn(sectionEl(from));
    const below = mine.filter((el) => el.getBoundingClientRect().top >= top);
    const above = mine.filter((el) => el.getBoundingClientRect().top < top);
    const el = dir > 0 ? below[0] : above[above.length - 1];
    if (el) return land(el);
  }
  const countOf = (f) => rowNoteCount(f);
  st.noteHunting = true;
  try {
    for (let k = nextNoted(st.slots, countOf, from, dir); k >= 0; k = nextNoted(st.slots, countOf, k, dir)) {
      const s = st.slots[k];
      if (s.collapsed) {
        s.collapsed = false;
        repaintSlot(st, k);
      }
      if (s.load !== "ok") {
        scrollToFile(st, s.idx);
        if (!(await awaitSlot(st, s))) {
          if (state.stack !== st) return;
          continue;
        }
      }
      if (state.stack !== st) return;
      const mine = rowsIn(sectionEl(k));
      if (mine.length) return land(dir > 0 ? mine[0] : mine[mine.length - 1]);
    }
  } finally {
    st.noteHunting = false;
  }
  opLine(dir > 0 ? "no next file with notes" : "no previous file with notes", false);
}

// --- loading --------------------------------------------------------------

function pump(st) {
  while (state.stack === st && st.inFlight < STACK_MAX_IN_FLIGHT) {
    const k = nextToLoad(st.slots, st.near, st.anchor);
    if (k < 0) return;
    load(st, st.slots[k]);
  }
}

async function load(st, s) {
  s.load = "loading";
  s.inLoad = true; // until the repaint below: awaitSlot waits for the rows
  st.inFlight++;
  try {
    const d = await getDiff(fileDiffURL(s.f), { deferTok: true });
    s.diff = d;
    s.load = "ok";
    // The server tags an eligible unstaged diff with hunk ordinals and the
    // staging freshness hash — the same payload the single-file view arms
    // diffHunks from, kept HERE so every slot answers for its own file.
    s.hunks = d.hunks && hunkEligible(s.f) ? hunkState(s.f.path, d.hunks) : null;
    // A refresh re-fetches every kept slot (reconcileSlots). Where the bytes
    // did not move — same freshness hash — the reader's selection still names
    // the same rows, so staging one file does not wipe the selection in another.
    if (s.hunks && s.hunksPrev && s.hunksPrev.hash === s.hunks.hash) {
      s.hunks.sel = s.hunksPrev.sel;
    }
    s.hunksPrev = null;
    if (!s.counts) s.counts = countsFromDiff(d);
    // This file's own review notes, against the context its own row builds —
    // the same door the single-file view uses (rowNoteCtx), so a note written
    // in a stack is filed at the same address as one written file by file. A
    // file that is not note-addressable fetches nothing at all.
    await loadSlotNotes(st, s);
  } catch (e) {
    s.load = "error";
    s.error = e.message || String(e);
  }
  if (state.stack !== st) return; // the stack left the screen while this loaded
  s.inLoad = false; // (set before the repaint: an awaiter resumes on a later tick)
  st.inFlight--;
  if (s.again) {
    s.again = false;
    s.load = "idle"; // a refresh landed mid-load: this answer may predate it
  }
  const k = st.slots.indexOf(s);
  if (k >= 0) repaintSlot(st, k, true);
  // A slot arriving under a live query brings rows the search has never seen.
  if (diffSearch.query) {
    refindStack();
    diffSearchBar.paint();
  }
  pump(st);
}

// loadSlotNotes resolves ONE slot's notes. Best-effort, like the single-file
// lane: a transient failure leaves the slot's rows as they are rather than
// making its ◆ rows vanish.
async function loadSlotNotes(st, s) {
  s.ctx = rowNoteCtx(s.f);
  if (!s.ctx) return;
  let d = null;
  try {
    d = await notesFor(s.ctx);
  } catch {
    return;
  }
  if (state.stack !== st || !d) return;
  s.notes = d.notes || [];
  // The forge's resolved threads start folded, once per file. The collapse set
  // itself stays view-wide: a thread's id is unique across files, so one set
  // folds the whole stack (and Z folds all of it).
  if (!s.seeded) {
    s.seeded = true;
    for (const id of seedCollapsed(s.notes)) state.noteCollapsed.add(id);
  }
  if (d.counts) state.previewCounts = d.counts;
}


// refreshStackNotes re-reads EVERY loaded slot's notes and repaints them. It
// is what a note write, a sweep or a live "notes" event runs in a stack: one
// address per file, so there is no single fetch to redo — the same fan-out
// the TUI's loadNotesCmd does.
async function refreshStackNotes() {
  const st = state.stack;
  if (!st) return;
  const loaded = st.slots.filter((s) => s.load === "ok" && s.ctx);
  await Promise.all(loaded.map((s) => loadSlotNotes(st, s)));
  if (state.stack !== st) return;
  for (const s of loaded) {
    const k = st.slots.indexOf(s);
    if (k >= 0) repaintSlot(st, k, true);
  }
}


// readyStackSlot brings slot k on screen with its rows: a folded file
// unfolds and an unread one is fetched (awaitSlot). False when its diff did
// not load or the stack went away meanwhile.
async function readyStackSlot(st, k) {
  const s = st.slots[k];
  if (!s) return false;
  if (s.collapsed) {
    s.collapsed = false;
    repaintSlot(st, k);
  }
  scrollToFile(st, s.idx); // a FILE index: slot k of a later group is not file k
  return awaitSlot(st, s);
}

// landStackLine puts a stack on ONE file's exact line — what a gg:// link and
// a `gg session navigate` with a line ask for. Line numbers repeat across a
// stack (every file has a line 12), so the row is looked for inside that
// file's own section, never in the pane at large; a folded file unfolds and an
// unread one is fetched first, because its rows do not exist yet.
// end > line bands the range line..end (a range link); the answer is then
// {held}: how many of those lines the file's diff holds.
async function landStackLine(path, side, line, end = 0) {
  const st = state.stack;
  if (!st) return false;
  const k = st.slots.findIndex((s) => s.path === path);
  if (k < 0) return false;
  const s = st.slots[k];
  if (!(await readyStackSlot(st, k))) return false;
  const sec = sectionEl(k);
  if (!sec) return false;
  const rowAt = (root, no) => diffRowAt(root, side, no);
  // A range whose first line the diff lacks lands on the first one it holds.
  let at = line;
  let tr = rowAt(sec, line);
  if (!tr && end > line) {
    at = firstHeldLine((s.diff || {}).rows, side, line, end);
    tr = at ? rowAt(sec, at) : null;
  }
  if (!tr && end > line && firstHeldLine((s.diff || {}).rows, side, line, end)) {
    // Held but folded away (changes only): the band unfolds its rows, so band
    // first — from any row of the section — and find the line afterwards.
    const any = sec.querySelector("tr[data-i]");
    if (!any) return false;
    at = firstHeldLine(s.diff.rows, side, line, end);
    const held = setDiffRange(any, { side, first: line, last: end });
    const sec3 = sectionEl(k);
    const row = sec3 && rowAt(sec3, at);
    if (row) {
      markDiffRow(row, side, at, true);
      row.scrollIntoView({ block: "center" });
    }
    return { held };
  }
  if (!tr) return false;
  markDiffRow(tr, side, at);
  if (end > line) {
    const held = setDiffRange(tr, { side, first: line, last: end });
    const sec2 = sectionEl(k);
    const again = sec2 && rowAt(sec2, at);
    (again || tr).scrollIntoView({ block: "center" });
    return { held };
  }
  tr.scrollIntoView({ block: "center" });
  return true;
}


// noteScope is the DOM root a per-file note gesture acts in: the active
// slot's section, or the whole pane when there is no stack.
function noteScope() {
  const st = state.stack;
  if (!st) return diffBody();
  return sectionEl(st.anchor) || diffBody();
}


// activeDiff is WHOSE notes the note keys act on: stacked, the slot under the
// cursor (the file whose header the reader is at, or the row they clicked);
// otherwise the single-file view's own globals. This is the accessor the
// design reserved for this phase — every note reader goes through it instead
// of reading state.diffCtx / state.notes directly.
function activeDiff() {
  const st = state.stack;
  if (!st) return globalNoteCtx();
  const s = st.slots[st.anchor] || st.slots[0];
  if (!s) return globalNoteCtx();
  return { ctx: s.ctx || null, notes: s.notes || [], row: s.row || null, slot: s };
}
// rangeDiff is the note context of the file that holds the marked range — in a
// stack that need not be the slot under the cursor (the band's file takes the
// note, as the terminal's marks do) — or null when no range is marked.
function rangeDiff() {
  const st = state.stack;
  if (!st) return state.diffRange ? globalNoteCtx() : null;
  const s = st.slots.find((o) => o.range);
  return s ? { ctx: s.ctx || null, notes: s.notes || [], row: s.row || null, range: s.range, slot: s } : null;
}
// stackAllNotes is every loaded slot's notes, in stream order — what a
// whole-view gesture (Z, the notes list, a }/{ walk) reads.
function stackAllNotes() {
  const st = state.stack;
  if (!st) return state.notes || [];
  return st.slots.flatMap((s) => s.notes || []);
}
// numstatQuery names the change set to /api/numstat, or null when git has no
// tree pair for it (entry, shelf and link sets count from each loaded diff).
function numstatQuery(st) {
  if (state.filesMode === "status") return new URLSearchParams({ wt: st.group === "staged" ? "staged" : "unstaged" });
  if (state.filesMode === "compare") {
    const c = state.compare;
    if (!c || c.links || c.frozen || !c.aHash || !c.bHash) return null;
    return new URLSearchParams({ left: c.aHash, right: c.bHash });
  }
  if (!state.fileSha || st.slots.some((s) => s.f.sha && s.f.sha !== state.fileSha)) return null;
  return new URLSearchParams({ sha: state.fileSha });
}

async function loadCounts(st) {
  const q = numstatQuery(st);
  if (!q) return;
  let r;
  try {
    r = await getJSON("/api/numstat?" + q);
  } catch {
    return; // counts are decoration: each loaded diff still fills its own
  }
  if (state.stack !== st) return;
  const by = new Map((r.files || []).map((x) => [x.path, x]));
  st.slots.forEach((s, k) => {
    const x = by.get(s.path);
    if (!x) return;
    s.counts = x.binary ? { binary: true } : { add: x.add, del: x.del };
    const el = sectionElRaw(k); // absent on a first build: paintStack comes after
    if (!el) return;
    el.querySelector(".stk-head").outerHTML = headHTML(s);
    if (!s.diff && !s.collapsed) el.querySelector(".stk-body").innerHTML = bodyHTML(s); // re-size the placeholder
  });
}

// --- navigation -----------------------------------------------------------

// scrollToFile brings list row i's header to the top of the pane, expanding
// it (which lets the loader fetch it). A row outside this stack (the other
// working-tree group) builds that group's stack instead.
function scrollToFile(st, i, expand = true) {
  const k = st.slots.findIndex((s) => s.idx === i);
  if (k < 0) return buildStack(st.list, stackGroup(st.list[i] || {}), i);
  if (!st.painted) {
    // still awaiting its counts: remember the target, the first paint lands there
    st.want = i;
    state.fileCursor = i;
    return;
  }
  const s = st.slots[k];
  if (expand && s.collapsed) {
    s.collapsed = false;
    repaintSlot(st, k);
  }
  st.anchor = k;
  state.fileCursor = i;
  const pane = $("diff-pane");
  const el = sectionElRaw(k);
  // The first file of a stack led by a shelf entry's notes lands on the top:
  // aligning its header would scroll the notes out of sight above it.
  if (k === 0 && diffBodyEl().querySelector(".stk-notes")) pane.scrollTop = 0;
  else if (el) {
    pane.scrollTop +=
      el.getBoundingClientRect().top - pane.getBoundingClientRect().top - $("diff-header").offsetHeight;
  }
  renderFiles();
  followInList();
  updateDiffNav();
  pump(st);
}

// followInList keeps the highlighted row on screen as the reader moves
// through the stack (the symmetric view's left list mirrors #files-pane).
function followInList() {
  const sel = document.querySelector("#files-list li.sel");
  if (sel) sel.scrollIntoView({ block: "nearest" });
}

// topSlot: the section whose header sits at (or last passed) the line just
// under the pane's toolbar — the file being read. Binary search: sections
// are in document order.
function topSlot() {
  const line = $("diff-pane").getBoundingClientRect().top + $("diff-header").offsetHeight + 1;
  const els = diffBodyEl().querySelectorAll(".stk-file");
  let lo = 0;
  let hi = els.length - 1;
  let ans = els.length ? 0 : -1;
  while (lo <= hi) {
    const m = (lo + hi) >> 1;
    if (els[m].getBoundingClientRect().top <= line) {
      ans = m;
      lo = m + 1;
    } else hi = m - 1;
  }
  return ans;
}

function syncCursor() {
  syncRaf = 0;
  const st = state.stack;
  if (!st) return;
  // In a review's stack the Summary is what is read until the first file's
  // header reaches the line: the list highlights ≡ Summary meanwhile.
  if (reviewActive()) {
    const first = diffBodyEl().querySelector(".stk-file");
    const line = $("diff-pane").getBoundingClientRect().top + $("diff-header").offsetHeight + 1;
    const top = !first || first.getBoundingClientRect().top > line;
    if (top !== state.review.onSummary) {
      state.review.onSummary = top;
      st.anchor = -1; // re-claimed below once a file is read
      renderFiles();
      followInList();
    }
    if (top) return;
  }
  const k = topSlot();
  if (k < 0 || k === st.anchor) return;
  st.anchor = k;
  state.fileCursor = st.slots[k].idx;
  renderFiles(); // the list highlight follows the file being read
  followInList();
  updateDiffNav();
}

$("diff-pane").addEventListener("scroll", () => {
  if (state.stack && !syncRaf) syncRaf = requestAnimationFrame(syncCursor);
});

// --- collapse -------------------------------------------------------------

function toggleSlot(k) {
  const st = state.stack;
  const s = st && st.slots[k];
  if (!s) return;
  s.collapsed = !s.collapsed;
  s.autoFold = false; // the reader's own fold: it stays
  if (s.collapsed && s.hunks) s.hunks.sel = new Set(); // nothing hidden stays selected
  if (s.collapsed) s.range = s.row = null; // …nor marked: `c` must never write over a band nobody sees
  repaintSlot(st, k);
  pump(st);
}

function collapseCurrent() {
  if (state.stack) toggleSlot(state.stack.anchor);
}

// toggleAllCollapsed: every file folded → expand all; otherwise fold all.
function toggleAllCollapsed() {
  const st = state.stack;
  if (!st) return;
  const expand = st.slots.every((s) => s.collapsed);
  for (const s of st.slots) {
    s.collapsed = !expand;
    s.autoFold = false;
    if (s.collapsed && s.hunks) s.hunks.sel = new Set(); // nothing hidden stays selected
    if (s.collapsed) s.range = s.row = null;
  }
  const at = st.slots[st.anchor] ? st.slots[st.anchor].idx : 0;
  paintStack(st);
  scrollToFile(st, at, false); // keep the reader's file at the top, folded or not
}

diffBodyEl().addEventListener("click", (e) => {
  const st = state.stack;
  if (!st) return;
  const sec = e.target.closest(".stk-file");
  if (!sec) return;
  const k = Number(sec.dataset.k);
  if (e.target.closest(".stk-resolve")) {
    const i = st.slots[k].idx;
    teardownStack();
    openStatusDiff(i); // sets the title/ctx the picker's exits expect, then opens it
    return;
  }
  if (e.target.closest(".stk-head")) return toggleSlot(k);
  // a fold row unfolds that run of THIS file (the single-file listener
  // ignores clicks while no single diff is open)
  const tr = e.target.closest("tr.fold[data-fold]");
  if (tr) {
    st.slots[k].folds.add(Number(tr.dataset.fold));
    repaintSlot(st, k);
  }
});

// hunkSlotAt is the slot a clicked diff row belongs to — the door the single
// hunk bar uses to act on ONE file (design D1). Outside a stack it returns
// null and the caller falls back to the single-file globals.
function hunkSlotAt(el) {
  const st = state.stack;
  if (!st || !el) return null;
  const sec = el.closest(".stk-file");
  if (!sec) return null;
  const k = Number(sec.dataset.k);
  const s = st.slots[k];
  return s && s.hunks ? { k, slot: s, hunks: s.hunks, el: sec } : null;
}


// hunkSlots is every stacked file with selectable rows, in stack order —
// the one selection spans them all.
function hunkSlots() {
  const st = state.stack;
  if (!st) return [];
  const out = [];
  st.slots.forEach((s, k) => {
    if (s.hunks && !s.collapsed) out.push({ k, slot: s, hunks: s.hunks, el: sectionEl(k) });
  });
  return out;
}


// showSlotDiff paints ONE file of the stack with the given diff and staging
// state, in place — repaintSlot keeps the reader's header pinned — and
// re-finds a live search over it. It is how a staging action shows its
// prediction, reverts it, and lands the quiet re-read.
function showSlotDiff(slot, d, hunks) {
  const st = state.stack;
  if (!st) return;
  const k = st.slots.indexOf(slot);
  if (k < 0) return;
  slot.diff = d;
  slot.load = "ok";
  slot.hunks = hunks;
  repaintSlot(st, k, true);
  if (diffSearch.query) {
    refindStack();
    diffSearchBar.paint();
  }
}


// --- lifecycle hooks ------------------------------------------------------

// reconcileStack follows a status re-read: the working-tree stack keeps its
// sections, their folds and the file being read.
function reconcileStack() {
  const st = state.stack;
  if (!st || state.filesMode !== "status") return;
  const rows = stackRows(state.statusEntries, st.group);
  if (!rows.length) {
    enterFilesStage(); // the group emptied (all staged / unstaged): back to the list (tears the stack down)
    return;
  }
  // The common case after a staging action or a watcher refresh: the SAME
  // files, in the same order, with the same status. Repainting the whole
  // stack then only flickers — each loaded file re-reads quietly instead and
  // repaints only if its diff actually changed.
  const sameFiles =
    rows.length === st.slots.length &&
    rows.every((r, i) => slotKey(r.f) === st.slots[i].key && statusLetter(r.f) === st.slots[i].status);
  if (sameFiles) {
    st.list = state.statusEntries;
    rows.forEach((r, i) => {
      st.slots[i].f = r.f;
      st.slots[i].idx = r.idx;
    });
    for (const s of st.slots) if (s.load === "ok") void quietReloadSlot(st, s);
    loadCounts(st);
    return;
  }
  const anchorKey = st.slots[st.anchor] && st.slots[st.anchor].key;
  st.list = state.statusEntries;
  st.slots = reconcileSlots(st.slots, rows);
  const k = Math.max(0, st.slots.findIndex((s) => s.key === anchorKey));
  paintStack(st);
  scrollToFile(st, st.slots[k].idx);
  // The slots were renumbered — a hit's composite row now names another file,
  // and every slot's cached lines went with the repaint. Re-anchor on where
  // the reader IS, then re-find.
  if (diffSearch.query) {
    diffSearch.origin = stackSearchHere();
    diffSearch.cur = -1;
    refindStack();
    diffSearchBar.paint();
  }
  loadCounts(st); // a refresh changes counts too; heads and placeholders repaint in place
}

// quietReloadSlot re-reads one loaded file and repaints it ONLY when what
// the server says differs from what is on screen; the selection survives
// while the file's freshness hash is unchanged. No placeholder, no jump.
async function quietReloadSlot(st, s) {
  let d;
  try {
    d = await getDiff(fileDiffURL(s.f), { deferTok: true });
  } catch {
    return; // best-effort: the old diff stays
  }
  if (state.stack !== st || !st.slots.includes(s)) return;
  const hunks = d.hunks && hunkEligible(s.f) ? hunkState(s.f.path, d.hunks) : null;
  if (hunks && s.hunks && s.hunks.hash === hunks.hash) {
    hunks.sel = s.hunks.sel;
  }
  const same = s.diff && JSON.stringify(s.diff.rows) === JSON.stringify(d.rows);
  if (same) {
    s.diff = d; // fresh tags and hash, identical rows: nothing to paint
    s.hunks = hunks;
    colourSlotLater(st, s); // …but its colours, still owed, are this answer's to fetch
    return;
  }
  s.range = null; // other rows now: the band was over the previous ones (the single-file view's rule for a new diff)
  showSlotDiff(s, d, hunks);
}


// --- the toggle -----------------------------------------------------------

function syncStackChrome() {
  const on = !!(state.ui && state.ui.stacked_diff);
  const btn = $("stack-btn");
  btn.classList.toggle("on", on);
  btn.setAttribute("aria-pressed", on ? "true" : "false");
  const chip = document.querySelector('#foot button[data-act="stacked"]');
  if (chip) chip.classList.toggle("on", on);
  $("stack-fold-all").classList.toggle("hidden", !state.stack);
}

// toggleStacked is S: flip the preference; on the diff stage, re-open the
// current file in the other view (the file being read stays on screen).
function toggleStacked() {
  const on = !(state.ui && state.ui.stacked_diff);
  saveUI({ stacked_diff: on });
  syncStackChrome();
  if (state.layout !== "diff" || !activeFileList().length) return; // an empty symmetric view has nothing to show
  if (!on) teardownStack();
  // A review on its Summary keeps the Summary: alone, or atop the stack.
  if (reviewActive() && state.review.onSummary) return showReviewSummary();
  openFile(state.fileCursor);
}

$("stack-btn").addEventListener("click", toggleStacked);
$("stack-fold-all").addEventListener("click", toggleAllCollapsed);

window.addEventListener("resize", () => {
  if (state.stack) rerenderStack();
});

registerHelp({
  key: "S · stacked diff",
  html:
    "show <b>every file</b> of the open commit, comparison or working-tree section in ONE scroll — " +
    "a header per file (status, path, <b>+added −deleted</b>) with its diff below, like GitHub's " +
    "<i>Files changed</i>. Files load as they scroll into view; a change set of more than 100 files " +
    "opens with every file folded to its header, and a folded file unfolds (and loads) once you scroll " +
    "it into view — one you fold yourself stays folded. Clicking a file in the list scrolls to it, and the " +
    "list follows the file you are reading. In the <b>symmetric</b> comparison the stack holds the rows " +
    "the filter shows, in the arrow's direction, between the two lists. The <b>stacked</b> chip in the diff toolbar is the same " +
    "switch; the choice is remembered per machine. <b>/</b> searches every file of the stack and "
    + "<b>]</b>/<b>[</b> step hit to hit across them; in the working tree, clicking a changed block "
    + "selects that row (shift-click a range, ctrl-click to add) and right-click stages the selection or the hunk",
});
registerHelp({
  key: "- / _ · fold files",
  html:
    "in the stacked diff: <b>-</b> folds (or unfolds) the file you are reading to its header, " +
    "<b>_</b> folds every file (or, when all are folded, unfolds them all). A click on a file's " +
    "header does the same for that file",
});

export { STACK_ROW_SPAN, activeDiff, rangeDiff, repaintStackSlots, stackChangeStep, stackNoteStep, hunkSlotAt, hunkSlots, showSlotDiff, followInList, refindStack, stackHitStep, stackSearchHere, unsearchedSlots, landStackLine, readyStackSlot, sectionEl, noteScope, refreshStackNotes, stackAllNotes, syncStackChrome, collapseCurrent, openStack, reconcileStack, rerenderStack, stackOn, teardownStack, toggleAllCollapsed, toggleStacked };
