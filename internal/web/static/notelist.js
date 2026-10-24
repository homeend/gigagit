// notelist.js — the TUI's "List notes…" (notes_list_popup.go): every thread
// on the open diff (or on every loaded file of a stack), one row each, type
// to filter, enter lands the diff on the thread. A plain inventory: nothing
// here writes.
import { registerHelp } from "./menus.js";
import { $, esc, state } from "./core.js";
import { closeLayer, mountOverlay, pushLayer } from "./layers.js";
import { opLine } from "./ops.js";
import { landNote, toggleNotesAgent } from "./files.js";
import { readyStackSlot } from "./stackview.js";

// --- list model (pure; guarded against Go) ---
// noteListEntries projects root notes (the wire's) to rows; path names the
// file, set only in a stack (the list then covers several files).
function noteListEntries(notes, path) {
  return (notes || []).map((n) => {
    const r = n.range && n.range[0] ? n.range : [n.line, n.line];
    let anchor = n.side + ":" + r[0] + (r[1] !== r[0] ? "-" + r[1] : "");
    if (n.file_level) anchor = "file";
    const author = n.author || "";
    const summary = n.summary || "";
    return {
      rootId: n.id,
      path: path || "",
      anchor,
      author,
      summary,
      replies: (n.replies || []).length,
      stale: n.status === "stale",
      agent: n.source === "agent",
      forge: n.source === "forge",
      resolved: !!n.resolved,
      filter: (summary + "\u0000" + author + "\u0000" + (path || "")).toLowerCase(),
    };
  });
}

// noteListLine is the row: "◆ new:15  ada  summary  +2".
function noteListLine(e) {
  let s = "◆ " + (e.path ? e.path + " " : "") + e.anchor + "  ";
  if (e.forge) s += "review  ";
  if (e.author) s += e.author + "  ";
  s += e.summary;
  if (e.replies > 0) s += "  +" + e.replies;
  if (e.resolved) s += "  resolved";
  return s.replace(/\s+$/, "");
}

// noteListVisible is the filter: a case-insensitive substring of the
// summary, the author or the file.
function noteListVisible(entries, q) {
  const needle = String(q || "").toLowerCase();
  return needle ? entries.filter((e) => e.filter.includes(needle)) : entries;
}
// --- end list model ---

const box = mountOverlay("notelist");
box.innerHTML = `<div id="notelist-box"><div class="nlhead"></div><input id="notelist-filter" type="text" placeholder="type to filter" autocomplete="off" spellcheck="false"><div class="nlrows"></div><div class="nlhint">[↑/↓] move  [enter] go to note  type to filter  [esc] close</div></div>`;
let list = null; // { entries, sel }

// openNotesList lists the open diff's threads; a stack lists the threads of
// the files it has read.
function openNotesList() {
  const st = state.stack;
  const entries = st
    ? st.slots.flatMap((s, k) => noteListEntries(s.notes, s.path || (s.ctx && s.ctx.path) || "").map((e) => ({ ...e, slot: k })))
    : noteListEntries(state.notes, "");
  if (!entries.length) return;
  const title = st ? "this change set" : (state.diffCtx && state.diffCtx.path) || "this diff";
  list = { entries, sel: 0 };
  box.querySelector(".nlhead").textContent = "Notes on " + title;
  $("notelist-filter").value = "";
  pushLayer("notelist", box, { onKey: listKey });
  paint();
  $("notelist-filter").focus();
}

function visible() {
  return noteListVisible(list.entries, $("notelist-filter").value);
}

function paint() {
  const vis = visible();
  list.sel = Math.min(Math.max(list.sel, 0), Math.max(vis.length - 1, 0));
  box.querySelector(".nlrows").innerHTML = vis.length
    ? vis
        .map((e, i) => `<div class="nlrow${i === list.sel ? " sel" : ""}${e.stale ? " stale" : e.agent ? " agent" : ""}" data-i="${i}">${esc(noteListLine(e))}</div>`)
        .join("")
    : `<div class="nlrow none">(no matching notes)</div>`;
  const cur = box.querySelector(".nlrow.sel");
  if (cur) cur.scrollIntoView({ block: "nearest" });
}

function closeList() {
  list = null;
  closeLayer("notelist");
}

async function go(i) {
  const e = visible()[i];
  if (!e) return;
  closeList();
  // The TUI's gotoNote: unfold the thread's file, lift the agent layer.
  if (e.agent && state.notesAgentOff) toggleNotesAgent();
  if (e.slot != null && state.stack) await readyStackSlot(state.stack, e.slot);
  if (!(await landNote(e.rootId))) opLine("Note is no longer in this diff", true);
}

function listKey(e) {
  if (!list) return true;
  switch (e.key) {
    case "Escape":
      closeList();
      break;
    case "ArrowDown":
      list.sel++;
      paint();
      break;
    case "ArrowUp":
      list.sel--;
      paint();
      break;
    case "Home":
      list.sel = 0;
      paint();
      break;
    case "End":
      list.sel = visible().length - 1;
      paint();
      break;
    case "Enter":
      go(list.sel);
      break;
    default:
      return true; // typing reaches the filter
  }
  e.preventDefault();
  return true;
}

$("notelist-filter").addEventListener("input", () => {
  if (!list) return;
  list.sel = 0;
  paint();
});

box.addEventListener("click", (e) => {
  if (e.target === box) return closeList();
  const row = e.target.closest(".nlrow[data-i]");
  if (row) go(Number(row.dataset.i));
});

registerHelp({
  key: "list notes",
  html: "right-click in a diff (or on a ◆ note) → <b>List notes…</b>: every thread on the open diff, type to filter (summary, author, file), ↑/↓ and enter go to one",
});

export { openNotesList };
