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
import { symActive } from "./symcompare.js";
import {
  activeFileList,
  clearDiffHunks,
  closeConflictPick,
  diffHTML,
  enterFilesStage,
  fileDiffURL,
  mountPanBars,
  openFile,
  openStatusDiff,
  renderFiles,
  setLayout,
  updateDiffNav,
} from "./files.js";
import {
  STACK_MAX_IN_FLIGHT,
  buildSlots,
  countsFromDiff,
  estimateHeight,
  nextToLoad,
  reconcileSlots,
  stackGroup,
  stackRows,
} from "./stack.js";

const ROW_PX = 20; // a diff row's rendered height, for placeholder estimates
let observer = null;
let syncRaf = 0;

// stackOn: the toggle is on AND this screen can stack. The symmetric view
// keeps its single diff until plan 2 teaches it the stack.
function stackOn() {
  return !!(state.ui && state.ui.stacked_diff) && !symActive();
}

function teardownStack() {
  if (observer) observer.disconnect();
  observer = null;
  state.stack = null;
  syncStackChrome();
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
  const st = { list, group, slots, near: new Set(), anchor: 0, inFlight: 0 };
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
  scrollToFile(st, anchorIdx);
}

// --- painting -------------------------------------------------------------

function headHTML(s) {
  const path =
    s.oldPath && s.oldPath !== s.path ? `${esc(s.oldPath)} → ${esc(s.path)}` : esc(s.path);
  const c = s.counts;
  const counts = !c
    ? ""
    : c.binary
    ? `<span class="stk-bin">bin</span>`
    : `<span class="stk-add">+${c.add}</span> <span class="stk-del">−${c.del}</span>`;
  return (
    `<div class="stk-head" title="${esc(s.path)} — click to collapse / expand (-)">` +
    `<span class="stk-fold">${s.collapsed ? "▸" : "▾"}</span>` +
    `<span class="st ${esc(s.status)}">${esc(s.status)}</span>` +
    `<span class="stk-path">${path}</span>` +
    `<span class="stk-counts">${counts}</span></div>`
  );
}

function bodyHTML(s) {
  if (s.collapsed) return "";
  if (s.load === "none") {
    return `<div class="notice">conflicted — <button class="stk-resolve">open resolver</button></div>`;
  }
  if (s.load === "error") return `<div class="notice">error: ${esc(s.error)}</div>`;
  // a kept slot re-fetching after a refresh paints its old diff until the new one lands
  if (s.diff) return diffHTML(s.diff, $("diff-pane").clientWidth, false, s.folds);
  return `<div class="stk-ph" style="height:${estimateHeight(s, ROW_PX)}px">loading…</div>`;
}

function sectionHTML(s, k) {
  return (
    `<section class="stk-file${s.collapsed ? " collapsed" : ""}" data-k="${k}">` +
    headHTML(s) +
    `<div class="stk-body">${bodyHTML(s)}</div></section>`
  );
}

function paintStack(st) {
  const body = $("diff-body");
  body.innerHTML = `<div class="stk">${st.slots.map(sectionHTML).join("")}</div>`;
  // the file headers stick just under the pane's own sticky toolbar
  $("diff-pane").style.setProperty("--diff-head-h", $("diff-header").offsetHeight + "px");
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
  mountPanBars(body, $("diff-hbars"));
  syncStackChrome();
  updateDiffNav();
}

function sectionEl(k) {
  return document.querySelector(`#diff-body .stk-file[data-k="${k}"]`);
}

function repaintSlot(st, k) {
  const el = sectionEl(k);
  if (!el) return;
  const s = st.slots[k];
  el.classList.toggle("collapsed", s.collapsed);
  el.querySelector(".stk-head").outerHTML = headHTML(s);
  el.querySelector(".stk-body").innerHTML = bodyHTML(s);
  mountPanBars($("diff-body"), $("diff-hbars"));
  updateDiffNav(); // the ‹ change › buttons count the rendered change runs
}

// rerenderStack repaints every loaded body for a new width, the f view or
// the w text mode. Browser scroll anchoring keeps the reader's place.
function rerenderStack(resetFolds = false) {
  const st = state.stack;
  if (!st) return;
  // #diff-header wraps (flex-wrap) at narrow widths: re-measure what the
  // file headers stick under
  $("diff-pane").style.setProperty("--diff-head-h", $("diff-header").offsetHeight + "px");
  st.slots.forEach((s, k) => {
    if (resetFolds) s.folds = new Set();
    if (s.diff && !s.collapsed) repaintSlot(st, k);
  });
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
  st.inFlight++;
  try {
    const d = await getJSON(fileDiffURL(s.f));
    s.diff = d;
    s.load = "ok";
    if (!s.counts) s.counts = countsFromDiff(d);
  } catch (e) {
    s.load = "error";
    s.error = e.message || String(e);
  }
  if (state.stack !== st) return; // the stack left the screen while this loaded
  st.inFlight--;
  if (s.again) {
    s.again = false;
    s.load = "idle"; // a refresh landed mid-load: this answer may predate it
  }
  const k = st.slots.indexOf(s);
  if (k >= 0) repaintSlot(st, k);
  pump(st);
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
  updateDiffNav();
  pump(st);
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
  for (const s of st.slots) s.collapsed = !expand;
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
  const anchorKey = st.slots[st.anchor] && st.slots[st.anchor].key;
  st.list = state.statusEntries;
  st.slots = reconcileSlots(st.slots, rows);
  const k = Math.max(0, st.slots.findIndex((s) => s.key === anchorKey));
  paintStack(st);
  scrollToFile(st, st.slots[k].idx);
  loadCounts(st); // a refresh changes counts too; heads and placeholders repaint in place
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
  if (state.layout !== "diff" || symActive()) return;
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
    "list follows the file you are reading. The <b>stacked</b> chip in the diff toolbar is the same " +
    "switch; the choice is remembered per machine. Search (/) works in the single-file view",
});
registerHelp({
  key: "- / _ · fold files",
  html:
    "in the stacked diff: <b>-</b> folds (or unfolds) the file you are reading to its header, " +
    "<b>_</b> folds every file (or, when all are folded, unfolds them all). A click on a file's " +
    "header does the same for that file",
});

export { collapseCurrent, openStack, reconcileStack, rerenderStack, stackOn, teardownStack, toggleAllCollapsed, toggleStacked };
