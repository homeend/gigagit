// difftok.js — part of gg's web client: a big diff's colours, arriving after
// its rows. /api/diff with defer_tok=1 answers the rows at once and
// tok_pending when a side was too big to colour before answering; the same
// URL with tok=1 answers the runs (the server lexes, shares and caches them
// by content). They are laid onto the rows and patched into the painted
// cells — never a repaint, so the scroll, the folds, the search hits and a
// text selection being dragged all stay.
import { getJSON } from "./core.js";

// --- merge toks (pure; guarded against Go) ---
// mergeToks lays t's per-source-line runs (t.old / t.new, index = line - 1,
// null = plain) onto rows' left_tok / right_tok and returns the indexes of
// the rows that changed.
function mergeToks(rows, t) {
  const changed = [];
  const at = (side, no) => (side && no > 0 && no <= side.length ? side[no - 1] || null : null);
  rows.forEach((r, i) => {
    const l = at(t.old, r.left_no);
    const n = at(t.new, r.right_no);
    if (l) r.left_tok = l;
    if (n) r.right_tok = n;
    if (l || n) changed.push(i);
  });
  return changed;
}
// --- end merge toks ---

// fetchToks reads d's colours (d.url + tok=1) and merges them into d.rows:
// the changed row indexes, or null when they belong to other bytes (a
// working-tree file edited between the two requests — its refresh repaints
// it anyway). An aborted fetch rejects with AbortError.
async function fetchToks(d, signal) {
  const t = await getJSON(d.url + (d.url.includes("?") ? "&" : "?") + "tok=1", { signal });
  if (t.old_sum !== d.old_sum || t.new_sum !== d.new_sum) return null;
  d.tok_pending = false;
  return mergeToks(d.rows || [], t);
}

const idle = window.requestIdleCallback
  ? (fn) => window.requestIdleCallback(fn, { timeout: 50 })
  : (fn) => setTimeout(fn, 0);

// patchCells repaints the text cells of the painted rows in idx through
// table.cell (diffTable's), the rows on screen first, the rest a chunk per
// idle slice. live() says whether this table is still the one shown. While
// the reader drags a selection inside host it waits until they let go:
// rewriting a cell under a selection would collapse it.
function patchCells(host, scroller, table, idx, live) {
  const want = new Set(idx);
  const run = () => {
    if (!live()) return;
    const sel = getSelection();
    if (sel && !sel.isCollapsed && sel.rangeCount && host.contains(sel.getRangeAt(0).commonAncestorContainer)) {
      const wait = () => {
        const s = getSelection();
        if (s && !s.isCollapsed && s.rangeCount && host.contains(s.getRangeAt(0).commonAncestorContainer)) return;
        document.removeEventListener("selectionchange", wait);
        run();
      };
      document.addEventListener("selectionchange", wait);
      return;
    }
    const trs = [...host.querySelectorAll("table.diff tr[data-i]")].filter((tr) => want.has(Number(tr.dataset.i)));
    const view = scroller.getBoundingClientRect();
    const seen = (tr) => {
      const r = tr.getBoundingClientRect();
      return r.bottom >= view.top && r.top <= view.bottom;
    };
    const order = trs.filter(seen).concat(trs.filter((tr) => !seen(tr)));
    let at = 0;
    const step = () => {
      if (!live()) return;
      for (const end = Math.min(order.length, at + 400); at < end; at++) patchRow(order[at], table);
      if (at < order.length) idle(step);
    };
    step();
  };
  run();
}

// patchRow rewrites one row's text cells: the td's l / r class names its
// side, a unified context row's single cell is the new ("r") side.
function patchRow(tr, table) {
  const i = Number(tr.dataset.i);
  for (const td of tr.querySelectorAll("td.side")) {
    const pan = td.querySelector("span.pan");
    const html = table.cell(i, td.classList.contains("l") ? "l" : "r");
    if (pan && html != null) pan.innerHTML = html;
  }
}

export { fetchToks, mergeToks, patchCells };
