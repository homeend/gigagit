// difftok.js — part of gg's web client: a big diff's colours, arriving after
// its rows. /api/diff with defer_tok=1 answers the rows at once and
// tok_pending when a side was too big to colour before answering; the same
// URL with tok=1 answers the runs (the server lexes, shares and caches them
// by content). They are laid onto the rows and patched into the painted
// cells — never a repaint, so the scroll, the folds, the search hits and a
// text selection all stay.
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
// idle slice. live() says whether this table is still the one shown. A cell
// holding an end of the reader's text selection waits (rewriting it would
// move that end) until the selection lets go of it; every other cell takes
// its colours at once — inside a selection too, whose ends stay put.
function patchCells(host, scroller, table, idx, live) {
  const want = new Set(idx);
  const trs = [...host.querySelectorAll("table.diff tr[data-i]")].filter((tr) => want.has(Number(tr.dataset.i)));
  const view = scroller.getBoundingClientRect();
  const seen = (tr) => {
    const r = tr.getBoundingClientRect();
    return r.bottom >= view.top && r.top <= view.bottom;
  };
  const order = trs.filter(seen).concat(trs.filter((tr) => !seen(tr)));
  const held = [];
  const retry = () => {
    if (live()) {
      const ends = selectionEnds();
      for (let k = held.length - 1; k >= 0; k--) if (patchRow(held[k], table, ends)) held.splice(k, 1);
      if (held.length) return;
    }
    document.removeEventListener("selectionchange", retry);
  };
  let at = 0;
  const step = () => {
    if (!live()) return;
    const ends = selectionEnds();
    for (const end = Math.min(order.length, at + 400); at < end; at++) if (!patchRow(order[at], table, ends)) held.push(order[at]);
    if (at < order.length) idle(step);
    else if (held.length) document.addEventListener("selectionchange", retry);
  };
  step();
}

// selectionEnds are the nodes holding the ends of the page's text selection
// ([] when there is none, or only a caret).
function selectionEnds() {
  const s = getSelection();
  if (!s || s.isCollapsed) return [];
  const out = [];
  for (let i = 0; i < s.rangeCount; i++) {
    const r = s.getRangeAt(i);
    out.push(r.startContainer, r.endContainer);
  }
  return out;
}

// patchRow rewrites one row's text cells: the td's l / r class names its
// side, a unified context row's single cell is the new ("r") side. A cell
// holding one of ends is left as it is; false = some cell was.
function patchRow(tr, table, ends) {
  const i = Number(tr.dataset.i);
  let all = true;
  for (const td of tr.querySelectorAll("td.side")) {
    const pan = td.querySelector("span.pan");
    if (pan && ends.some((n) => pan.contains(n))) {
      all = false;
      continue;
    }
    const html = table.cell(i, td.classList.contains("l") ? "l" : "r");
    if (pan && html != null) pan.innerHTML = html;
  }
  return all;
}

export { fetchToks, mergeToks, patchCells };
