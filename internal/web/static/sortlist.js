// sortlist.js — part of gg's web client. The per-list display order: the web
// twin of the TUI's `o` key, which cycles the focused panel through git's own
// emission order, name↑/↓ and date↑/↓ (internal/tui/viewstate.go's sortMode).
//
// This module owns the modes, the labels and the comparison; the lists
// themselves live in sidebar.js and files.js, which ask for a SORTED COPY at
// render time. Nothing here mutates a state array: state.worktrees[0] must
// stay the main worktree, and the sidebar menus look their subject up by name
// out of the unsorted arrays.
import { esc, state } from "./core.js";
import { saveUI } from "./uistate.js";

const SORT_DEFAULT = "default";
const ALL_MODES = [SORT_DEFAULT, "name-asc", "name-desc", "date-asc", "date-desc"];
// Lists with no date to sort on cycle through the name modes alone, rather
// than offering two modes that would do nothing. (The TUI cycles them anyway —
// its tagList.Date returns 0 — but a visible control that does nothing when
// clicked reads as broken.)
const NAME_MODES = [SORT_DEFAULT, "name-asc", "name-desc"];

// LIST_MODES is also the "is this list sortable" registry: a list absent from
// it gets no chip. Wire names match the server's uiSortLists allowlist.
const LIST_MODES = {
  branches: ALL_MODES,
  remotes: ALL_MODES,
  worktrees: ALL_MODES,
  tags: NAME_MODES, // no per-tag date exists — see internal/web/tags.go
  files: NAME_MODES, // working-tree files; mtimes would cost a stat per file per refresh
};

// INITIAL is where a list starts before you have ever set its order.
// Branches opens newest-first, like the TUI's Branches panel (model.go's
// sortModes: {panelBranches: sortDateDesc}). The files list keeps the
// path-ascending arrangement it has always had, so nothing reshuffles on
// upgrade; its "git order" mode is the new option, not the new default.
const INITIAL = { branches: "date-desc", files: "name-asc" };

const LABELS = {
  "default": "git",
  "name-asc": "name↑",
  "name-desc": "name↓",
  "date-asc": "date↑",
  "date-desc": "date↓",
};

const TITLES = {
  "default": "git's own order",
  "name-asc": "by name, A→Z",
  "name-desc": "by name, Z→A",
  "date-asc": "oldest first",
  "date-desc": "newest first",
};


function listModes(list) {
  return LIST_MODES[list] || ALL_MODES;
}


// sortMode is list's current order: what was stored, else where the list
// starts. A stored mode the list no longer offers (an older gg, a hand-edited
// state file) falls back the same way.
function sortMode(list) {
  const stored = state.sorts ? state.sorts[list] : "";
  if (stored && listModes(list).includes(stored)) return stored;
  return INITIAL[list] || SORT_DEFAULT;
}


// applyStoredSorts installs the orders that came back from the server. A first
// run passes nothing, which leaves every list on its INITIAL mode.
function applyStoredSorts(sorts) {
  state.sorts = sorts && typeof sorts === "object" ? { ...sorts } : {};
}


function setSortMode(list, mode) {
  state.sorts = { ...(state.sorts || {}), [list]: mode };
  saveUI({ sorts: state.sorts });
}


// nextSortMode is the cycle the chip walks, in LIST_MODES order.
function nextSortMode(list) {
  const modes = listModes(list);
  const i = modes.indexOf(sortMode(list));
  return modes[(i + 1) % modes.length];
}


// sortChipHTML is the control itself: a small muted chip carrying the current
// mode, drawn inside the list's header. Lists with no sort get nothing.
function sortChipHTML(list) {
  if (!LIST_MODES[list]) return "";
  const m = sortMode(list);
  const title = `sorted ${TITLES[m]} — click to cycle`;
  return `<span class="sortchip" data-sort="${esc(list)}" title="${esc(title)}">${esc(LABELS[m])}</span>`;
}


// sortedRows returns rows in list order under mode — a COPY, except under the
// default order where the input is already the answer. Mirrors the TUI's
// viewLess: case-insensitive name compare, stable ties, and an unknown date
// (0) sorting LAST in BOTH directions so missing data never floats to the top.
function sortedRows(rows, mode, name, date) {
  if (mode === SORT_DEFAULT || !rows.length) return rows;
  const asc = mode.endsWith("-asc");
  const out = rows.slice();
  if (mode.startsWith("name")) {
    out.sort((a, b) => {
      const na = String(name(a) || "").toLowerCase();
      const nb = String(name(b) || "").toLowerCase();
      if (na === nb) return 0;
      return (na < nb ? -1 : 1) * (asc ? 1 : -1);
    });
    return out;
  }
  out.sort((a, b) => {
    const da = date(a) || 0;
    const db = date(b) || 0;
    if (!da || !db) return da === db ? 0 : da ? -1 : 1; // unknown dates last, either way
    if (da === db) return 0;
    return (da < db ? -1 : 1) * (asc ? 1 : -1);
  });
  return out;
}


// sortedBy is the common case: sort list's rows under the list's own mode.
function sortedBy(list, rows, name, date) {
  return sortedRows(rows, sortMode(list), name, date || (() => 0));
}


export {
  applyStoredSorts,
  nextSortMode,
  setSortMode,
  sortChipHTML,
  sortMode,
  sortedBy,
  sortedRows,
};
