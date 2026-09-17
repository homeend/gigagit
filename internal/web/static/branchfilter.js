// branchfilter.js — part of gg's web client. The five branch-filter slots
// (alt+1…5 in the TUI) on the branches and remotes lists: a chip in each
// section header shows the active slot and opens a menu of the five; the
// keys alt+1…5 (branches) and alt+shift+1…5 (remotes) toggle a slot the TUI
// way — the active slot's key clears it. Evaluation is SERVER-SIDE (one
// matcher, Go RE2): this module only remembers the header the server sent
// and asks it to change the slot. Matching on e.code, not e.key: with alt
// held, e.key is a dead or accented character on several layouts.
//
// Imports stay down to core/layers/ops: sidebar.js imports THIS module, and
// ops.js already imports sidebar.js, so an edge back to sidebar.js would be
// a cycle whose evaluation order nothing guarantees. The list refetch goes
// through window.__ggRefetchList instead — the same hand-off the previews
// header uses (window.__ggAddPreview).
import { esc, getJSON, state } from "./core.js";
import { showCtxMenu } from "./layers.js";
import { opLine } from "./ops.js";

const LISTS = new Set(["branches", "remotes"]);

state.branchFilter = state.branchFilter || { branches: null, remotes: null };

// applyFilterHeader remembers the filter block a list fetch returned.
function applyFilterHeader(list, filter) {
  if (!LISTS.has(list)) return;
  state.branchFilter[list] = filter || null;
}


// filterChipHTML is the control itself, drawn at the right of the section
// header next to the sort chip. Its label is the TUI's panel-header
// decoration ("▽2 stale · 12 hidden"), so the two surfaces read the same.
function filterChipHTML(list) {
  if (!LISTS.has(list)) return "";
  const f = state.branchFilter[list];
  const label = f ? `▽${f.slot} ${f.name}${f.hidden ? " · " + f.hidden + " hidden" : ""}` : "▽";
  const title = f
    ? `branch filter ${f.slot} (${f.mode === "show" ? "show only" : "hide"} ${f.name}) — click to change`
    : "branch filter: none — click to pick one (alt+1…5)";
  return `<span class="filterchip${f ? " on" : ""}" data-filter="${esc(list)}" title="${esc(title)}">${esc(label)}</span>`;
}


// setBranchFilterSlot asks the server to activate slot (0 = none) for list,
// then refetches that list so the rows and the chip agree with the server.
async function setBranchFilterSlot(list, slot) {
  try {
    const resp = await fetch("/api/branch-filter", {
      method: "PUT",
      headers: { "Content-Type": "application/json", Accept: "application/json" },
      body: JSON.stringify({ list, slot }),
    });
    if (!resp.ok) {
      let msg = String(resp.status);
      try {
        msg = (await resp.json()).error || msg;
      } catch {}
      opLine("branch filter: " + msg, true);
      return false;
    }
  } catch (e) {
    opLine("branch filter: " + e.message, true);
    return false;
  }
  if (window.__ggRefetchList) await window.__ggRefetchList(list);
  const f = state.branchFilter[list];
  opLine(f ? `${list} filter ${f.slot}: ${f.name}` : `${list} filter off`);
  return true;
}


// toggleSlot is the radio rule shared with the TUI: the active slot's own
// key clears it, any other one switches to it.
function toggleSlot(list, slot) {
  const cur = state.branchFilter[list];
  return setBranchFilterSlot(list, cur && cur.slot === slot ? 0 : slot);
}


// openFilterMenu lists none + the five slots at the pointer. The slots are
// fetched EVERY time (and awaited before the menu opens) on purpose: they
// come from .gg.toml, which the settings view and a text editor both edit
// under a running server — and the await is also what keeps the opening
// click from closing the menu again, since layers.js hides the context menu
// on any document click that is not inside it.
async function openFilterMenu(list, x, y) {
  if (!LISTS.has(list)) return;
  let slots = [];
  try {
    slots = (await getJSON("/api/branch-filters")).slots || [];
  } catch (e) {
    opLine("branch filters: " + e.message, true);
    return;
  }
  state.branchFilterSlots = slots;
  const cur = state.branchFilter[list];
  const rows = [
    { label: (cur ? "  " : "✓ ") + "none", act: () => setBranchFilterSlot(list, 0) },
    // An unusable slot (invalid regex, or nothing set) is shown rather than
    // dropped — five numbered slots always exist, and a gap would read as a
    // bug rather than as "nothing is defined there". It renders as a
    // non-clickable header row: the server refuses it with 409 anyway.
    ...slots.map((s) =>
      s.usable
        ? {
            label: `${cur && cur.slot === s.slot ? "✓ " : "  "}${s.slot}  ${s.name} — ${s.summary}`,
            act: () => setBranchFilterSlot(list, s.slot),
          }
        : { header: `${s.slot}  (${s.summary})` }
    ),
  ];
  showCtxMenu(rows, x, y);
}


// branchFilterKey routes alt+DigitN / alt+shift+DigitN and returns true when
// it consumed the key. keys.js calls it only OUTSIDE a form field: a digit
// typed into the commit box must stay a digit.
function branchFilterKey(e) {
  if (!e.altKey || e.ctrlKey || e.metaKey) return false;
  const m = /^Digit([1-5])$/.exec(e.code || "");
  if (!m) return false;
  e.preventDefault();
  toggleSlot(e.shiftKey ? "remotes" : "branches", Number(m[1]));
  return true;
}


export { applyFilterHeader, branchFilterKey, filterChipHTML, openFilterMenu, setBranchFilterSlot };
