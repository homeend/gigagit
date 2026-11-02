// panefocus.js — the page's ONE keyboard focus: which control takes the keys.
// A control is the sidebar, the commit list, the file list or the diff; tab /
// shift+tab, ←/→ and a click move it, and the ring (.focused) shows it. The
// browser's own focus never decides: a page button cannot hold it, so enter on
// a list can never also press pull. keys.js routes every key by focusTarget().
import { $, state } from "./core.js";

const STOP_PANE = { side: "branches-pane", commits: "commits-pane", diff: "diff-pane", files: "files-pane" };

// --- pane focus (pure; guarded against Go) ---
// nextStop is the control tab / shift+tab reach from cur: wraps round; a cur
// that is off screen goes to the first control.
function nextStop(order, cur, dir) {
  if (!order.length) return cur;
  const i = order.indexOf(cur);
  if (i < 0) return order[0];
  return order[(i + dir + order.length) % order.length];
}

// edgeStop is the control ←/→ reach from cur: its neighbour, and no further.
function edgeStop(order, cur, dir) {
  const i = order.indexOf(cur);
  if (i < 0) return order.length ? order[0] : cur;
  return order[Math.max(0, Math.min(order.length - 1, i + dir))];
}

// sideTarget is where a sidebar key puts the row cursor at cur in n rows of
// which page show a screen; -1 for a key that does not move it.
function sideTarget(key, cur, n, page) {
  switch (key) {
    case "ArrowDown": case "j": return n ? Math.min(n - 1, cur + 1) : cur;
    case "ArrowUp": case "k": return Math.max(0, cur - 1);
    case "PageDown": return n ? Math.min(n - 1, cur + page) : cur;
    case "PageUp": return Math.max(0, cur - page);
    case "Home": return 0;
    case "End": return n ? n - 1 : cur;
  }
  return -1;
}
// --- end pane focus ---

const shown = (id) => $(id).getClientRects().length > 0;

// focusTarget names the control that takes the keys. A control that has left
// the screen (the sidebar behind a diff or t, a folded file list) gives way.
function focusTarget() {
  if (state.sideFocus && shown("branches-pane")) return "side";
  if (state.layout === "diff") return state.fileListFocus && !state.filesHidden && shown("files-pane") ? "files" : "diff";
  return state.pane === "commits" ? "commits" : "files";
}

// focusOrder lists the controls on screen, left to right.
function focusOrder() {
  const order = [];
  if (shown("branches-pane")) order.push("side");
  if (shown("commits-pane")) order.push("commits");
  if (shown("diff-pane")) order.push("diff");
  if (shown("files-pane") && !state.filesHidden) order.push("files");
  return order;
}

let epoch = 0; // counts the user's focus moves (a slow open must not undo one)
const focusEpoch = () => epoch;

// setFocus gives the keys to a control, taking them from any page field or
// button that holds the browser's focus (keepField: the click is ON a field).
function setFocus(target, keepField) {
  epoch++;
  state.sideFocus = target === "side";
  if (target === "commits") {
    state.pane = "commits";
    state.fileListFocus = false;
  } else if (target === "files") {
    state.pane = "files";
    state.fileListFocus = true;
  } else if (target === "diff") {
    state.pane = "files";
    state.fileListFocus = false;
  }
  const a = document.activeElement;
  if (!keepField && a && a !== document.body && a.closest("input,textarea,button")) a.blur();
  focusPane();
}

const cycleFocus = (dir) => setFocus(nextStop(focusOrder(), focusTarget(), dir));
const stepFocus = (dir) => setFocus(edgeStop(focusOrder(), focusTarget(), dir));

// focusPane paints the focus: the ring on the control that has it, the row
// cursor when that is the sidebar.
function focusPane() {
  const on = STOP_PANE[focusTarget()];
  Object.values(STOP_PANE).forEach((id) => $(id).classList.toggle("focused", id === on));
  paintSideCursor();
}

// focusFromPointer: a press inside a control focuses it — a right-click and a
// press on its header or a button too, whatever else that press does.
function focusFromPointer(el) {
  if (!el.closest) return;
  const field = !!el.closest("input,textarea");
  if (el.closest("#branches-pane")) {
    const row = el.closest("li, .side-header");
    if (row) setSideRow(row);
    setFocus("side", field);
  } else if (el.closest("#commits-pane")) setFocus("commits", field);
  else if (el.closest("#files-pane")) {
    if (!state.filesHidden) setFocus("files", field); // the folded strip is no list
  }
  else if (el.closest("#diff-pane, #symleft-pane")) setFocus("diff", field);
}

// A page button never takes the browser's focus from a mouse press: it would
// keep it, and the next enter would press it again. The click still fires. A
// field the press leaves still loses it, as with any other press.
const PAGE_CHROME = "#top, #panes, #foot, #conflict-bar, #bigrepo-bar";

function keepButtonsUnfocused(e) {
  if (!e.target.closest || !e.target.closest("button") || !e.target.closest(PAGE_CHROME)) return;
  e.preventDefault();
  const a = document.activeElement;
  if (a && a.closest && a.closest("input,textarea")) a.blur();
}

// pageButtonKey: a page button that holds the browser's focus anyway gets no
// key — enter and space go to the focused control, never to the button.
function pageButtonKey(e) {
  if (e.key !== "Enter" && e.key !== " ") return;
  if (!e.target.closest || !e.target.closest("button") || !e.target.closest(PAGE_CHROME)) return;
  e.preventDefault();
  e.target.blur();
}

// --- the sidebar's row cursor ---
// The rows are what the sidebar shows, top to bottom: every section's header
// (enter folds it) and the rows of the open sections, as one list. A row is
// remembered by what it names, so a live refresh keeps the cursor on it.
let sideKeyOf = "";
let sideIdx = -1;

function sideRows() {
  return [...$("branches-pane").querySelectorAll(".side-header, li")].filter((el) => el.getClientRects().length);
}

const rowKey = (el) => el.id || JSON.stringify({ ...el.dataset }) + el.className.replace(/\bkbcur\b/, "").trim() + (Object.keys(el.dataset).length ? "" : el.textContent);

function setSideRow(row) {
  sideKeyOf = rowKey(row);
  sideIdx = sideRows().indexOf(row);
}

// sideRow finds the cursor's row, re-finding it after a repaint; a sidebar
// entered for the first time starts on the checked-out branch.
function sideRow() {
  const rows = sideRows();
  if (!rows.length) return null;
  // The row at the remembered place first: a session's row shows twice (under
  // Branches and Worktrees), so the name alone could jump back to the other one.
  if (sideIdx >= 0 && rows[sideIdx] && rowKey(rows[sideIdx]) === sideKeyOf) return rows[sideIdx];
  const same = sideKeyOf ? rows.find((r) => rowKey(r) === sideKeyOf) : null;
  if (same) return same;
  if (sideIdx >= 0) return rows[Math.min(sideIdx, rows.length - 1)];
  return rows.find((r) => r.matches("#branches-list li.head")) || rows[0];
}

function paintSideCursor() {
  $("branches-pane").querySelectorAll(".kbcur").forEach((el) => el.classList.remove("kbcur"));
  if (focusTarget() !== "side") return;
  const row = sideRow();
  if (!row) return;
  row.classList.add("kbcur");
  setSideRow(row);
  // A kept key, the pull-requests A: "that section has focus".
  state.prFocus = !!row.closest("#prs-header, #pr-search-box, #prs-list");
}

// The bare keys of the OTHER controls: on the sidebar they do nothing, rather
// than reach a list that is not focused.
const SIDE_DEAD = new Set(["s", "u", "m", "o", "I", "a", "c", "E", "z", "Z", "}", "{", "-", "_", ",", "@", "/", ":", "[", "]", "ArrowLeft", " "]);

// sideKey is a key on the focused sidebar; true when it took it.
function sideKey(e) {
  if (e.ctrlKey || e.metaKey || e.altKey) return false;
  if (e.shiftKey && (e.key === "ArrowDown" || e.key === "ArrowUp")) {
    e.preventDefault();
    return true;
  }
  const rows = sideRows();
  const row = sideRow();
  const cur = row ? rows.indexOf(row) : 0;
  const page = Math.max(1, Math.floor(rows.length && row ? $("branches-pane").clientHeight / (row.offsetHeight || 20) / 4 : 5));
  const to = sideTarget(e.key, cur, rows.length, page);
  if (to >= 0) {
    e.preventDefault();
    if (rows[to]) {
      setSideRow(rows[to]);
      paintSideCursor();
      rows[to].scrollIntoView({ block: "nearest" });
    }
    return true;
  }
  if (e.key === "Enter") {
    e.preventDefault();
    if (row) row.click();
    return true;
  }
  if (e.key === ".") {
    e.preventDefault();
    if (row) {
      const r = row.getBoundingClientRect();
      row.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true, clientX: r.left + 24, clientY: r.bottom }));
    }
    return true;
  }
  if (e.key === "ArrowRight" || e.key === "Escape") {
    e.preventDefault();
    setFocus(state.pane === "files" && shown("files-pane") ? "files" : "commits");
    return true;
  }
  if (SIDE_DEAD.has(e.key)) {
    e.preventDefault();
    return true;
  }
  return false;
}

// A live refresh rebuilds the rows: the cursor's class goes with them.
new MutationObserver(() => {
  if (state.sideFocus) paintSideCursor();
}).observe($("branches-pane"), { childList: true, subtree: true });

export { cycleFocus, focusEpoch, focusFromPointer, focusOrder, focusPane, focusTarget, keepButtonsUnfocused, nextStop, pageButtonKey, setFocus, sideKey, stepFocus };
