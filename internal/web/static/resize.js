// resize.js — part of gg's web client. Split from the original app.js;
// see app.js (the entry module) for the load order.
import { $, lsGet, lsSet, state } from "./core.js";
import { saveUI } from "./uistate.js";
import { renderCommits } from "./commits.js";
import { renderDiff } from "./files.js";

// --- pane resizing ---
// One drag handle per layout: sidebar↔commits in list mode, and the file
// list's edge in the files/diff stages. rs-sidebar resizes the FIRST grid
// column (width = pointer offset from the #panes left edge); rs-detail
// resizes the LAST — the file list sits on the right — so it carries
// `right: true` and measures from the right edge instead. Branch names
// ellipsize, so a fixed sidebar width left a long name unreadable with no
// recourse. Widths live as CSS custom properties on #panes and persist
// per handle.
// rememberWidth stores a dragged width twice: localStorage for this session
// (instant on reload) and gg's state file for the next run — gg web's port,
// and so this page's origin, changes every launch.
function rememberWidth(cfg) {
  lsSet(cfg.key, String(cfg.want));
  saveUI(cfg.key === "gg.sidebar.width" ? { sidebar_width: cfg.want } : { files_width: cfg.want });
}


// applyStoredWidths re-applies widths restored from the server. Zero or
// missing means "never dragged" and leaves the default standing.
function applyStoredWidths(sidebarWidth, filesWidth) {
  if (sidebarWidth > 0) RESIZERS["rs-sidebar"].want = sidebarWidth;
  if (filesWidth > 0) RESIZERS["rs-detail"].want = filesWidth;
  applyPaneWidths();
}


const RESIZERS = {
  "rs-sidebar": { prop: "--sb-w", key: "gg.sidebar.width", def: 230 },
  "rs-detail": { prop: "--files-w", key: "gg.panes.files-width", def: 320, right: true },
};

const RS_MIN = 120; // narrower than this and the pane holds nothing readable

const RS_KEEP = 200; // always leave this much for the OTHER pane (the flexible column)


function setPaneWidth(cfg, w) {
  $("panes").style.setProperty(cfg.prop, w + "px");
}


// Clamp against the live #panes width so a drag can never squeeze either
// side to nothing — including on a window far narrower than the defaults,
// where the minimum wins over the keep-back. In the files stage BOTH fixed
// columns are on screen at once, so each handle's keep-back also reserves
// the other handle's column, or the flexible commits pane between them
// could be squeezed to zero.
function clampPaneWidth(cfg, w) {
  const total = $("panes").getBoundingClientRect().width;
  let reserve = RS_KEEP;
  if (state.layout === "files" && state.sidebar) {
    const other = Object.values(RESIZERS).find((c) => c !== cfg);
    if (other) reserve += other.want;
  }
  return Math.round(Math.min(Math.max(RS_MIN, total - reserve), Math.max(RS_MIN, w)));
}


// What the user asked for is stored and persisted; what the window can
// currently afford is what gets applied. Shrinking the window therefore
// squeezes the pane without forgetting the chosen width, and widening it
// again restores that width.
function applyPaneWidths() {
  Object.values(RESIZERS).forEach((cfg) => setPaneWidth(cfg, clampPaneWidth(cfg, cfg.want)));
}


function initResizer(id) {
  const cfg = RESIZERS[id];
  const el = $(id);
  const saved = parseInt(lsGet(cfg.key) || "", 10);
  cfg.want = Number.isFinite(saved) ? saved : cfg.def;

  el.addEventListener("pointerdown", (e) => {
    e.preventDefault(); // no text selection, no native drag
    const rect = $("panes").getBoundingClientRect();
    // Capture keeps the move/up events coming while the pointer is off the
    // 5px handle. It throws when the pointer id is not active (a synthetic
    // event), which must not abort the drag.
    try { el.setPointerCapture(e.pointerId); } catch {}
    el.classList.add("dragging");
    document.body.classList.add("resizing");
    const onMove = (ev) => {
      cfg.want = clampPaneWidth(cfg, cfg.right ? rect.right - ev.clientX : ev.clientX - rect.left);
      setPaneWidth(cfg, cfg.want);
    };
    const onUp = () => {
      el.classList.remove("dragging");
      document.body.classList.remove("resizing");
      el.removeEventListener("pointermove", onMove);
      el.removeEventListener("pointerup", onUp);
      el.removeEventListener("pointercancel", onUp);
      rememberWidth(cfg);
    };
    el.addEventListener("pointermove", onMove);
    el.addEventListener("pointerup", onUp);
    el.addEventListener("pointercancel", onUp);
  });

  el.addEventListener("dblclick", () => {
    cfg.want = cfg.def;
    setPaneWidth(cfg, clampPaneWidth(cfg, cfg.want));
    rememberWidth(cfg);
  });
}

Object.keys(RESIZERS).forEach(initResizer);

applyPaneWidths();

window.addEventListener("resize", applyPaneWidths);


window.addEventListener("resize", () => {
  renderCommits();
  if (state.lastDiff) renderDiff(state.lastDiff); // unified↔side-by-side is width-dependent
});

export { RESIZERS, applyStoredWidths, RS_KEEP, RS_MIN, applyPaneWidths, clampPaneWidth, initResizer, setPaneWidth };
