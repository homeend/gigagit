// The SYMMETRIC view of a link comparison: two row-aligned file lists around
// one diff, and a direction arrow over it.
//
// A link comparison of two BOUNDED sets (two previews, two agents' attempts)
// is two small collections of files, and the plain A/D/M list says that badly:
// "D" there means "only the left set touches this", a file both sets changed
// IDENTICALLY vanishes, and nothing names the diff's direction. The server
// answers the aligned rows (`sym` on /api/compare-links — every member of
// either set, each side saying absent / present / deleted); this module paints
// them. Without `sym` there is nothing to offer and the classic list stands.
//
// Ownership: files.js keeps the comparison, the cursor and the diff. It calls
// in here through a handful of dispatch lines (symRows, renderSymLists,
// symBarHTML, openSymRow, symKey, symLayoutChanged) and everything else stays its own.

import { $, charWidth, elidePath, esc, state } from "./core.js";
import { saveUI } from "./uistate.js";
import { applyCompareFilter, openEntryFileDiff, openFile, renderFiles, setDiffTitle, setLayout } from "./files.js";

// SYM_MIN_WIDTH: two file lists and a side-by-side diff stop fitting below
// this, so the view falls back to the classic list and its chip is disabled.
const SYM_MIN_WIDTH = 1200;

const FILTERS = [
  ["diff", "differ", "rows the two sets disagree on (1)", (r) => r.differs],
  ["all", "all", "every file either set touches (2)", () => true],
  ["one", "one side", "files only ONE of the sets touches (3)", (r) => r.left === "absent" || r.right === "absent"],
  ["eq", "same", "files both sets changed IDENTICALLY (4)", (r) => !r.differs && r.left === "present" && r.right === "present"],
];

// symOffered: the comparison on screen CAN be shown symmetrically.
function symOffered() {
  const c = state.filesMode === "compare" ? state.compare : null;
  return !!(c && c.links && c.sym);
}

const wideEnough = () => window.innerWidth >= SYM_MIN_WIDTH;

// symActive: it IS shown symmetrically — offered, switched on by the user,
// the window is wide enough, and the file list is not folded to its strip.
function symActive() {
  return symOffered() && !!(state.ui && state.ui.sym_compare) && wideEnough() && !state.filesHidden;
}

// kindOf is the row's centre glyph: what the two sides have to do with each
// other. Derived from the two STATES first — "only one side" is a fact about
// membership, whatever the listing reported.
function kindOf(r) {
  if (r.left === "absent") return "or";
  if (r.right === "absent") return "ol";
  return r.differs ? "ne" : "eq";
}
const GLYPH = { ne: "≠", eq: "=", ol: "◁", or: "▷" };
const KIND_TIP = { ne: "differs between the two sets", eq: "the same in both sets", ol: "only in the left set", or: "only in the right set" };

// statusFor is the tree-diff letter of a row IN THE ARROW'S DIRECTION — what
// menus, the stepper and /api/entry-diff read. "=" for a row that does not
// differ (identical, or no bytes on either side).
function statusFor(r, flipped) {
  if (!r.differs) return "=";
  const from = flipped ? r.right : r.left;
  const to = flipped ? r.left : r.right;
  if (from !== "present" && to === "present") return "A";
  if (from === "present" && to !== "present") return "D";
  return "M";
}

// symRows is what state.files holds while the view is active: the VISIBLE
// aligned rows, each with a direction-aware status. One index space for the
// cursor, j/k, the file stepper and both painted lists.
function symRows(c) {
  const keep = FILTERS.find((f) => f[0] === c.symFilter) || FILTERS[0];
  return c.sym.filter(keep[3]).map((r) => ({ ...r, status: statusFor(r, c.flipped), kind: kindOf(r) }));
}

// --- the two lists ---

// A long path is cut the way the normal file list cuts it (files.js's
// filePathHTML): whole segments out of the MIDDLE through elidePath, so the
// file name and the head of the path both survive — not by the CSS ellipsis,
// which drops the tail. The budget is each list's own measured width less
// what else sits on a row; the CSS ellipsis stays as the backstop, so one
// column is left spare.
const SYM_ROW_PX = 16 + 2 * 6 + 12 + 16; // li padding 0 8px, two 6px gaps, .st 12px, .symg 16px

// symCols is list id's path budget in columns, or 0 (render the path whole)
// while the list has no usable width yet.
function symCols(id) {
  const el = $(id);
  const px = el ? el.clientWidth : 0;
  if (px <= 0) return 0;
  const cols = Math.floor((px - SYM_ROW_PX) / charWidth()) - 1;
  return cols >= 4 ? cols : 0; // below "…/x" there is nothing useful to show
}

function sideRowHTML(r, side, i, cols) {
  const st = r[side];
  const glyph = `<span class="symg ${r.kind}" title="${KIND_TIP[r.kind]}">${GLYPH[r.kind]}</span>`;
  if (st === "absent") {
    // A gap, not a row: no data-i, so neither the click nor the context-menu
    // handler can act on it, and it is hidden from assistive tech.
    return `<li class="symgap${i === state.fileCursor ? " sel" : ""}" aria-hidden="true" title="not in this set">${side === "right" ? glyph : ""}<span class="sympath">· · · · · ·</span>${side === "left" ? glyph : ""}</li>`;
  }
  const cls = [i === state.fileCursor ? "sel" : "", st === "deleted" ? "symdel" : "", r.kind === "eq" ? "symeq" : ""].filter(Boolean).join(" ");
  const mark = st === "deleted" ? "D" : r.differs ? "M" : "";
  const tip = st === "deleted" ? "this set DELETES the file" : "in this set";
  const path = `<span class="sympath" title="${esc(r.path)}">${esc(cols ? elidePath(r.path, cols) : r.path)}</span>`;
  const stEl = `<span class="st ${mark}">${mark}</span>`;
  return `<li class="${cls}" data-i="${i}" title="${tip}">${side === "right" ? glyph + stEl + path : stEl + path + glyph}</li>`;
}

function headRowHTML(role, desc) {
  return `<li class="symhead" aria-hidden="true"><span>${role}</span><b title="${esc(desc)}">${esc(desc)}</b></li>`;
}

// renderSymLists paints BOTH lists from state.files. The right list is the
// page's own #files-list, so every handler already on it (click, context
// menu) keeps working; the left one mirrors it row for row.
function renderSymLists() {
  const c = state.compare;
  const rows = state.files;
  const lcols = symCols("symleft-list");
  const rcols = symCols("files-list");
  $("symleft-list").innerHTML = headRowHTML("left set", c.a) + rows.map((r, i) => sideRowHTML(r, "left", i, lcols)).join("");
  $("files-list").innerHTML = headRowHTML("right set", c.b) + rows.map((r, i) => sideRowHTML(r, "right", i, rcols)).join("");
  renderDirBar();
  syncGeometry();
}

// syncGeometry makes row N sit at the same y in both panes: the right pane
// carries a back bar, a title and the chips above its list, the left one
// nothing — so the left list is pushed down by exactly that much.
function syncGeometry() {
  const pane = $("files-pane");
  const list = $("files-list");
  const top = list.getBoundingClientRect().top - pane.getBoundingClientRect().top + pane.scrollTop;
  // Not rounded: the panes' borders put the list on a fractional pixel, and a
  // rounded spacer leaves every row one pixel off its twin.
  $("symleft-spacer").style.height = Math.max(0, top) + "px";
  $("symleft-pane").scrollTop = pane.scrollTop;
}

// The two panes scroll as one. The guard stops the mirrored write from
// echoing back as a second scroll event.
let mirroring = false;
function mirrorScroll(from, to) {
  from.addEventListener("scroll", () => {
    if (mirroring || !$("panes").classList.contains("sym")) return;
    mirroring = true;
    to.scrollTop = from.scrollTop;
    mirroring = false;
  });
}

// --- the direction bar ---

function renderDirBar() {
  const bar = $("sym-dir");
  const c = state.compare;
  if (!symActive()) {
    bar.classList.add("hidden");
    return;
  }
  bar.classList.remove("hidden");
  const side = (desc, role, cls) =>
    `<div class="symlab ${cls}"><b title="${esc(desc)}">${esc(desc)}</b><small>${role} side of the diff</small></div>`;
  bar.innerHTML =
    side(c.a, c.flipped ? "new" : "old", "l " + (c.flipped ? "new" : "old")) +
    `<button id="sym-flip" title="flip the diff's direction (x) — the two lists stay where they are">${c.flipped ? "←" : "→"}</button>` +
    side(c.b, c.flipped ? "old" : "new", "r " + (c.flipped ? "old" : "new"));
}

// --- the chips (files.js's renderCompareBar asks for them) ---

// symBarHTML is the compare bar's content for a comparison that OFFERS the
// view: the filter chips while it is active, and always the on/off chip.
// data-sf / data-symtoggle, never data-f: the bar's own handler owns that.
function symBarHTML() {
  const c = state.compare;
  const active = symActive();
  const chips = active
    ? FILTERS.map(
        ([id, label, tip, fn]) => {
          const n = c.sym.filter(fn).length;
          // A filter with nothing behind it is not offered — except the one
          // that is ON, which must stay pressable-looking so the bar still
          // says where the user is.
          const off = n === 0 && c.symFilter !== id ? " disabled" : "";
          return `<button data-sf="${id}" title="${tip}"${c.symFilter === id ? ' class="on"' : ""}${off}>${label} ${n}</button>`;
        }
      ).join("")
    : `<button class="on" disabled>all (${c.all.length})</button>`;
  const wide = wideEnough();
  const tip = wide
    ? "two aligned file lists around one diff (v)"
    : `needs a window at least ${SYM_MIN_WIDTH}px wide`;
  return (
    chips +
    `<button data-symtoggle="1"${active ? ' class="on"' : ""}${wide ? "" : " disabled"} title="${tip}" aria-pressed="${active}">⇄ symmetric</button>`
  );
}

// applySym re-derives everything from the current state: the rows, the grid,
// both lists, the bar — and, when the view has just come on with nothing open,
// the first row's diff (the view has no files-only stage to sit in).
function applySym(keepPath) {
  const path = keepPath || (state.files[state.fileCursor] || {}).path;
  // The grid FIRST: the lists are painted by the calls below, and their
  // geometry can only be measured while the left pane is actually shown. An
  // open diff stays open across a toggle, so no layout change will do it.
  paintGrid();
  applyCompareFilter();
  if (symActive()) {
    const i = path ? state.files.findIndex((f) => f.path === path) : -1;
    if (i >= 0) state.fileCursor = i;
    if (state.files.length) openFile(state.fileCursor);
    else {
      // Nothing to open, but the view is still the three-column one: put its
      // stage up so the empty lists and the chips are what the user sees.
      if (state.layout !== "diff") setLayout("diff");
      paintGrid();
      symEmpty();
    }
  } else {
    const i = path ? state.files.findIndex((f) => f.path === path) : -1;
    if (i >= 0) state.fileCursor = i;
    paintGrid();
    renderFiles();
  }
}

// paintGrid puts the three-column grid on (only in the diff stage) or off.
function paintGrid() {
  const on = symActive() && state.layout === "diff";
  $("panes").classList.toggle("sym", on);
  if (!on) $("sym-dir").classList.add("hidden");
}

function toggleSym() {
  if (!symOffered() || !wideEnough()) return;
  saveUI({ sym_compare: !(state.ui && state.ui.sym_compare) });
  applySym();
}

function flip() {
  if (!symActive()) return;
  state.compare.flipped = !state.compare.flipped;
  applySym();
}

function setFilter(id) {
  if (!symActive()) return;
  // A filter with nothing behind it is not offered: its chip is disabled, and
  // the key that names it does nothing either.
  const f = FILTERS.find((x) => x[0] === id);
  if (!f || (id !== state.compare.symFilter && !state.compare.sym.some(f[3]))) return;
  state.compare.symFilter = id;
  state.fileCursor = 0;
  applyCompareFilter();
  if (state.files.length && state.layout !== "diff") openFile(0);
}

// --- opening a row ---

// openSymRow opens row f's diff in the arrow's direction. A side with no bytes
// (absent, or deleted by its set) is an add/remove for /api/entry-diff exactly
// as in the classic list; a row with bytes on NEITHER side has nothing to ask
// the server for, and says so instead.
function openSymRow(f) {
  const c = state.compare;
  const fl = c.flipped;
  const left = fl ? f.right_spec || c.bSpec : f.left_spec || c.aSpec;
  const right = fl ? f.left_spec || c.aSpec : f.right_spec || c.bSpec;
  if (f.left !== "present" && f.right !== "present") {
    // openFile already put the diff stage up and cleared the hunk state.
    state.detailGen++; // …and a diff still loading must not land over this
    state.diffCtx = null;
    state.notes = [];
    setDiffTitle(f.path);
    $("diff-body").innerHTML = `<div class="notice">neither set has content for this file — ${esc(noContentWhy(f))}</div>`;
    return;
  }
  return openEntryFileDiff({
    left,
    right,
    path: f.path,
    leftLabel: fl ? c.b : c.a,
    rightLabel: fl ? c.a : c.b,
    status: f.status === "=" ? "M" : f.status,
  });
}

// symEmpty is the view with NO visible row (the two sets agree on everything
// and the filter is "differ"; a live refresh emptied the filter). The screen
// stays: both lists say so, and the bar's chips are the way on.
function symEmpty() {
  const c = state.compare;
  const note = `<li class="symgap" aria-hidden="true"><span class="sympath">${c.sym.length ? "no files match this filter" : "nothing in either set"}</span></li>`;
  $("symleft-list").innerHTML = headRowHTML("left set", c.a) + note;
  $("files-list").innerHTML = headRowHTML("right set", c.b) + note;
  state.detailGen++; // a diff still loading must not land over this
  state.diffCtx = null;
  state.notes = [];
  if (state.layout === "diff") {
    setDiffTitle("");
    $("diff-body").innerHTML = `<div class="notice">${c.symFilter === "diff" && c.sym.length ? "the two sets do not differ — see <b>all</b> or <b>same</b>" : "no files match this filter"}</div>`;
  }
  renderDirBar();
  syncGeometry();
}

function noContentWhy(f) {
  const say = (st, name) => (st === "deleted" ? `the ${name} set deletes it` : `the ${name} set does not touch it`);
  return say(f.left, "left") + ", " + say(f.right, "right");
}

// --- keys, clicks, lifecycle ---

// symKey has first refusal on the view's own keys while a comparison that
// offers it is on screen: v toggles, x flips, 1–4 pick a filter.
function symKey(e) {
  if (!symOffered() || state.layout === "list" || e.ctrlKey || e.metaKey || e.altKey) return false;
  if (e.key === "v") toggleSym();
  else if (e.key === "x" && symActive()) flip();
  else if ("1234".includes(e.key) && e.key.length === 1 && symActive()) setFilter(FILTERS[Number(e.key) - 1][0]);
  else return false;
  e.preventDefault();
  return true;
}

// symLeave is files.js's setLayout hook: any stage but the diff drops the grid.
function symLayoutChanged() {
  paintGrid();
}

$("compare-bar").addEventListener("click", (e) => {
  const chip = e.target.closest("button[data-sf]");
  if (chip) return setFilter(chip.dataset.sf);
  if (e.target.closest("button[data-symtoggle]")) toggleSym();
});
$("sym-dir").addEventListener("click", (e) => {
  if (e.target.closest("#sym-flip")) flip();
});
$("symleft-list").addEventListener("click", (e) => {
  const li = e.target.closest("li");
  if (li && li.dataset.i !== undefined) openFile(Number(li.dataset.i));
});
mirrorScroll($("files-pane"), $("symleft-pane"));
mirrorScroll($("symleft-pane"), $("files-pane"));
// A window dragged narrower than the gate falls back at once; wider, the chip
// comes back to life. Only a comparison that offers the view cares.
window.addEventListener("resize", () => {
  if (!symOffered()) return;
  const on = $("panes").classList.contains("sym");
  if (on !== (symActive() && state.layout === "diff")) applySym();
  else if (on) syncGeometry();
  else renderFiles();
});

export { SYM_MIN_WIDTH, applySym as symReapply, symEmpty, symActive, symOffered, symRows, renderSymLists, symBarHTML, openSymRow, symKey, symLayoutChanged, statusFor, kindOf };
