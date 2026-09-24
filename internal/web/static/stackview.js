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
import { saveUI } from "./uistate.js";
import { registerHelp } from "./menus.js";
import { focusPane } from "./keys.js";
import { seedCollapsed } from "./notebox.js";
import {
  activeFileList,
  changeStepTarget,
  clearDiffHunks,
  diffChangeBlocks,
  closeConflictPick,
  diffHTML,
  diffSearch,
  diffSearchBar,
  enterFilesStage,
  goToDiffHit,
  markDiffRow,
  globalNoteCtx,
  hunkEligible,
  notesArmed,
  notesFor,
  rowNoteCtx,
  fileDiffURL,
  mountPanBars,
  openFile,
  openStatusDiff,
  renderFiles,
  hunkState,
  landChange,
  setLayout,
  stackHuntSlots,
  updateDiffNav,
} from "./files.js";
import { stepHit, stepHitStrict } from "./inviewsearch.js";
import { GLYPH, KIND_TIP, STACK_MAX_IN_FLIGHT, buildSlots, countsFromDiff, estimateHeight, nextToLoad, noContentWhy, reconcileSlots, stackGroup, stackRows, slotKey, statusLetter } from "./stack.js";

const ROW_PX = 20; // a diff row's rendered height, for placeholder estimates
let observer = null;
let syncRaf = 0;

// stackOn: the toggle is on. Every file-list screen stacks — the symmetric
// view's stack holds its VISIBLE rows (state.files = symRows there).
function stackOn() {
  return !!(state.ui && state.ui.stacked_diff);
}

function teardownStack() {
  if (observer) observer.disconnect();
  observer = null;
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
  state.lastDiff = null; // resize / f / live refresh must not repaint a single diff here
  state.diffCtx = null;
  state.diffRow = null;
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
  // Counts FIRST: they size every placeholder. Painted with no counts, all
  // sections are a few rows tall, the whole change set sits "near" the
  // viewport, and the loader fetches the first three files wherever the
  // reader is — lazy in name only. (Best-effort: no counts → small
  // placeholders, as for entry/link sets.)
  $("diff-body").innerHTML = `<div class="notice">loading…</div>`;
  await loadCounts(st);
  if (state.stack !== st) return; // superseded while the counts loaded
  paintStack(st);
  st.painted = true;
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
  // a kept slot re-fetching after a refresh paints its old diff until the new one lands
  if (s.diff) {
    const nc = { ctx: s.ctx || null, notes: s.notes || [], row: s.row || null };
    // The slot paints the stack-wide search and reports the rows it painted,
    // which is what refindStack searches next time — one collapse per slot,
    // and the search and the paint provably share a fold set.
    const hctx = { search: diffSearch, base: slotBase(s), lines: (ls) => (s.lines = ls) };
    const kctx = s.hunks ? { sel: s.hunks.sel } : null;
    return diffHTML(s.diff, $("diff-pane").clientWidth, notesArmed(nc.ctx), s.folds, nc, hctx, kctx);
  }
  return `<div class="stk-ph" style="height:${estimateHeight(s, ROW_PX)}px">loading…</div>`;
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
  const body = $("diff-body");
  body.innerHTML = `<div class="stk">${st.slots.map(sectionHTML).join("")}</div>`;
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
  // the pane-wide bars stand aside: each file pans on its own (mountSlotBars)
  $("diff-hbars").classList.add("hidden");
  for (const el of body.querySelectorAll(".stk-file")) mountSlotBars(el);
  syncStackChrome();
  updateDiffNav();
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
  return document.querySelector(`#diff-body .stk-file[data-k="${k}"]`);
}

// repaintSlot redraws one section. A section ABOVE the file being read
// changes height when it loads (its placeholder is only an estimate), which
// would push the reader's file away; the reader's header is put back where it
// was. The browser's own scroll anchoring cannot do this reliably — the
// reader's section is often repainted in the same beat, removing the node it
// anchored on — so it is off for the stack (style.css) and done here.
function repaintSlot(st, k) {
  const el = sectionEl(k);
  if (!el) return;
  const s = st.slots[k];
  const pin = k < st.anchor ? sectionEl(st.anchor) : null;
  const before = pin ? pin.getBoundingClientRect().top : 0;
  el.classList.toggle("collapsed", s.collapsed);
  el.querySelector(".stk-head").outerHTML = headHTML(s);
  el.querySelector(".stk-body").innerHTML = bodyHTML(s);
  mountSlotBars(el); // before the pin: the bars are part of the section's height
  if (pin) $("diff-pane").scrollTop += pin.getBoundingClientRect().top - before;
  updateDiffNav(); // the ‹ change › buttons count the rendered change runs
}

// mountSlotBars gives one section its own scroll-mode bars: the section's body
// is the pan host (its --pan-l / --pan-r and remembered offsets are this
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
// repaints the slots whose tint changed. Re-finding per slot would leave the
// hit list holding only the last slot's hits; painting every slot on every
// keystroke would repaint a fifty-file stack for nothing.
function refindStack() {
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
      for (const l of s.lines) lines.push({ row: base + l.row, side: l.side, text: l.text });
    });
  }
  diffSearch.refind(lines);
  const hit = new Set(diffSearch.hits.map((h) => Math.floor(h.row / STACK_ROW_SPAN)));
  st.slots.forEach((s, k) => {
    const now = hit.has(k);
    if (!now && !s.hadHits) return; // nothing to paint and nothing to unpaint
    s.hadHits = now;
    if (s.diff && !s.collapsed) repaintSlot(st, k);
  });
}

// stackSearchHere is where the reader is with no hit current: the first row on
// screen anywhere in the pane, in the stack-wide key space.
function stackSearchHere() {
  const st = state.stack;
  const pane = $("diff-pane").getBoundingClientRect();
  for (const tr of $("diff-body").querySelectorAll(".stk-file table.diff tr[data-i]")) {
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
    const d = await getJSON(fileDiffURL(s.f));
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
  if (k >= 0) repaintSlot(st, k);
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
    if (k >= 0) repaintSlot(st, k);
  }
}


// landStackLine puts a stack on ONE file's exact line — what a gg:// link and
// a `gg session navigate` with a line ask for. Line numbers repeat across a
// stack (every file has a line 12), so the row is looked for inside that
// file's own section, never in the pane at large; a folded file unfolds and an
// unread one is fetched first, because its rows do not exist yet.
async function landStackLine(path, side, line) {
  const st = state.stack;
  if (!st) return false;
  const k = st.slots.findIndex((s) => s.path === path);
  if (k < 0) return false;
  const s = st.slots[k];
  if (s.collapsed) {
    s.collapsed = false;
    repaintSlot(st, k);
  }
  scrollToFile(st, k);
  if (!(await awaitSlot(st, s))) return false;
  const sec = sectionEl(k);
  if (!sec) return false;
  const tr =
    sec.querySelector(`tr[data-side="${side}"][data-no="${line}"]`) ||
    (side === "old" ? sec.querySelector(`tr[data-lno="${line}"]`) : null);
  if (!tr) return false;
  markDiffRow(tr, side, line);
  tr.scrollIntoView({ block: "center" });
  return true;
}


// noteScope is the DOM root a per-file note gesture acts in: the active
// slot's section, or the whole pane when there is no stack.
function noteScope() {
  const st = state.stack;
  if (!st) return $("diff-body");
  return sectionEl(st.anchor) || $("diff-body");
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
    const el = sectionEl(k); // absent on a first build: paintStack comes after
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
  const el = sectionEl(k);
  if (el) {
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
  const els = document.querySelectorAll("#diff-body .stk-file");
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
  if (s.collapsed && s.hunks) s.hunks.sel = new Set(); // nothing hidden stays selected
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
    if (s.collapsed && s.hunks) s.hunks.sel = new Set(); // nothing hidden stays selected
  }
  const at = st.slots[st.anchor] ? st.slots[st.anchor].idx : 0;
  paintStack(st);
  scrollToFile(st, at, false); // keep the reader's file at the top, folded or not
}

$("diff-body").addEventListener("click", (e) => {
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
  repaintSlot(st, k);
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
    d = await getJSON(fileDiffURL(s.f));
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
    return;
  }
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
    "opens with every file folded to its header. Clicking a file in the list scrolls to it, and the " +
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

export { activeDiff, stackChangeStep, hunkSlotAt, hunkSlots, showSlotDiff, followInList, refindStack, stackHitStep, stackSearchHere, unsearchedSlots, landStackLine, noteScope, refreshStackNotes, stackAllNotes, syncStackChrome, collapseCurrent, openStack, reconcileStack, rerenderStack, stackOn, teardownStack, toggleAllCollapsed, toggleStacked };
