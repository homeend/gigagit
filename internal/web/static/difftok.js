// difftok.js — part of gg's web client: a big file's colours, arriving after
// its rows, for every host that shows one. /api/diff, /api/file-content and
// /api/blame with defer_tok=1 answer the rows at once and tok_pending when
// the text was too big to colour before answering; the same URL with tok=1
// answers the runs (the server lexes, shares and caches them by content).
// They are laid onto the rows and patched into the painted rows by the
// host's own row painter — never a repaint, so the scroll, the folds, the
// search hits and a text selection all stay.
import { getJSON } from "./core.js";
import { firstRowBelow } from "./inviewsearch.js";

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

// mergeLineToks lays t.tok (per source line, null = plain) onto line rows
// (row i = source line i + 1: blame, a file's content) and returns the
// indexes of the rows that changed.
function mergeLineToks(lines, t) {
  const changed = [];
  const tok = t.tok || [];
  lines.forEach((l, i) => {
    if (tok[i]) {
      l.tok = tok[i];
      changed.push(i);
    }
  });
  return changed;
}

// mergeUnifiedToks lays a diff's runs onto its ONE-column rows (splitdiff.js
// unifiedRows: a "del" row is old line r.old, any other row new line r.nu)
// and returns the indexes of the rows that changed.
function mergeUnifiedToks(rows, t) {
  const changed = [];
  const at = (side, no) => (side && no > 0 && no <= side.length ? side[no - 1] || null : null);
  rows.forEach((r, i) => {
    const tok = r.kind === "del" ? at(t.old, r.old) : at(t.new, r.nu);
    if (tok) {
      r.tok = tok;
      changed.push(i);
    }
  });
  return changed;
}
// --- end merge toks ---

// fetchToks reads d's colours (d.url + tok=1) and merges them into d.rows:
// the changed row indexes, or null when they belong to other bytes (a
// working-tree file edited between the two requests — its refresh repaints
// it anyway). An aborted fetch rejects with AbortError.
async function fetchToks(d, signal) {
  const t = await fetchTokRuns(d, signal);
  return t ? mergeToks(d.rows || [], t) : null;
}

// fetchTokRuns is fetchToks' answer itself ({old, new} runs by source line),
// laid onto nothing — for a host whose rows are not d.rows (the viewer's
// one column) — or null when the runs belong to other bytes.
async function fetchTokRuns(d, signal) {
  const t = await getJSON(d.url + (d.url.includes("?") ? "&" : "?") + "tok=1", { signal });
  if (t.old_sum !== d.old_sum || t.new_sum !== d.new_sum) return null;
  d.tok_pending = false;
  return t;
}

// fetchLineToks is fetchToks for a text shown as line rows: url is the
// answer's own URL, body the answer ({sum, lines}); the changed line
// indexes, or null when the runs belong to other bytes.
async function fetchLineToks(url, body, signal) {
  const t = await getJSON(url + (url.includes("?") ? "&" : "?") + "tok=1", { signal });
  if (t.sum !== body.sum) return null;
  body.tok_pending = false;
  return mergeLineToks(body.lines || [], t);
}

const idle = window.requestIdleCallback
  ? (fn) => window.requestIdleCallback(fn, { timeout: 50 })
  : (fn) => setTimeout(fn, 0);

// patchRows refreshes painted rows in place: rows (elements) through the
// host's own redraw(el, ends) — which leaves alone any cell holding one of
// ends, the text selection's ends, and then answers false — the rows on
// screen first, the rest a chunk per idle slice. live() says whether the
// host still shows what the rows were painted for. A row held for the
// selection is redrawn once the selection lets go of it; every other row
// takes its colours at once, inside a selection too (its ends stay put).
function patchRows(scroller, rows, redraw, live) {
  // rows are in document order: the ones on screen are one run, found by
  // bisection — measuring every row would force the browser to lay out the
  // chunks it skips while they are off screen (all of a big file).
  const view = scroller.getBoundingClientRect();
  const lo = firstRowBelow(rows.length, (k) => rows[k].getBoundingClientRect().bottom, view.top);
  const hi = firstRowBelow(rows.length, (k) => rows[k].getBoundingClientRect().top, view.bottom + 1);
  const order = rows.slice(lo, hi).concat(rows.slice(0, lo), rows.slice(hi));
  const held = [];
  const retry = () => {
    if (live()) {
      const ends = selectionEnds();
      for (let k = held.length - 1; k >= 0; k--) if (redraw(held[k], ends)) held.splice(k, 1);
      if (held.length) return;
    }
    document.removeEventListener("selectionchange", retry);
  };
  let at = 0;
  const step = () => {
    if (!live()) return;
    const ends = selectionEnds();
    for (const end = Math.min(order.length, at + 400); at < end; at++) if (!redraw(order[at], ends)) held.push(order[at]);
    if (at < order.length) idle(step);
    else if (held.length) {
      document.addEventListener("selectionchange", retry);
      retry(); // a selection that let go during the slices fired before the listener
    }
  };
  step();
}

// patchCells is patchRows for a diff table: the painted rows in idx, each
// redrawn cell by cell through table.cell (diffTable's).
function patchCells(host, scroller, table, idx, live) {
  const want = new Set(idx);
  const trs = [...host.querySelectorAll("table.diff tr[data-i]")].filter((tr) => want.has(Number(tr.dataset.i)));
  patchRows(scroller, trs, (tr, ends) => patchRow(tr, table, ends), live);
}

// holdsEnd: el holds one of ends (a line row redraws as one cell, so it
// waits whole while the selection starts or ends in it).
function holdsEnd(el, ends) {
  return ends.some((n) => el.contains(n));
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

export { fetchLineToks, fetchTokRuns, fetchToks, holdsEnd, mergeLineToks, mergeToks, mergeUnifiedToks, patchCells, patchRows };
