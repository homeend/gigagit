// docsearch.js — the in-view search (/ @ ] [) over a PAINTED document: an
// overview's markdown (the viewer on its own, the split's left column) and
// the hit painter the ? help overlay shares. The TUI searches the rows it
// lays the document out in (previewSearchLines); the page searches the
// document's text blocks — a paragraph, a heading, a table cell, a code
// block — each block's textContent one searched line, so a paragraph the
// TUI wraps over several rows is one row here (a query may span its wrap).
//
// Hits are laid over the block's own text nodes (paintRow) and taken off
// again by unwrapping them (unpaintHits), so the markup between — an
// anchor's element and the classes paintAnchors keeps on it — is never
// rebuilt by a search.
import { firstRowBelow } from "./inviewsearch.js";

// DOC_ROWS are the blocks mdHTML paints text into; none holds another (a
// list item's text is its <p>, a quote's are its <p>s).
const DOC_ROWS = "p, .md-h, th, td, pre, .md-cap";

// paintRow lays hit spans over one row's text nodes. hit[i] is 1 + the hit's
// index for rune i of the row's text (0 = none), cur[i] whether it is the
// current one — renderCell's arrays, over text nodes instead of one string.
export function paintRow(el, spans) {
  const hit = new Array(Array.from(el.textContent).length).fill(0);
  const cur = new Array(hit.length).fill(false);
  for (const h of spans) {
    for (let i = h.start; i < h.end && i < hit.length; i++) {
      hit[i] = h.idx + 1;
      cur[i] = !!h.cur;
    }
  }
  const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT);
  const nodes = [];
  for (let n = walker.nextNode(); n; n = walker.nextNode()) nodes.push(n);
  let off = 0; // rune offset of the node's first rune within the row
  for (const node of nodes) {
    const rs = Array.from(node.data);
    let touched = false;
    for (let i = 0; i < rs.length; i++) if (hit[off + i]) { touched = true; break; }
    if (!touched) {
      off += rs.length;
      continue;
    }
    const frag = document.createDocumentFragment();
    for (let i = 0; i < rs.length; ) {
      let j = i + 1;
      while (j < rs.length && hit[off + j] === hit[off + i]) j++;
      const text = rs.slice(i, j).join("");
      if (!hit[off + i]) frag.append(document.createTextNode(text));
      else {
        const span = document.createElement("span");
        span.className = cur[off + i] ? "hit cur" : "hit";
        span.dataset.h = String(hit[off + i] - 1);
        span.textContent = text;
        frag.append(span);
      }
      i = j;
    }
    node.replaceWith(frag);
    off += rs.length;
  }
}

// unpaintHits takes every hit span under root back to plain text.
function unpaintHits(root) {
  const spans = root.querySelectorAll(".hit");
  if (!spans.length) return;
  for (const s of spans) s.replaceWith(document.createTextNode(s.textContent));
  root.normalize();
}

// docSearchHost is the bindSearchBar host over the document painted in
// body() (see searchbar.js for the verbs). A search starts where the TUI's
// line cursor would be: on the current hit while it is on screen, else on
// anchor() — the selected overview link, the cursor's row after a link
// step — while that is on screen, else on the first block on screen. The caller
// repaints by calling render() after every paint of the document (a
// refresh, a re-render), so the hits always follow the markup on screen.
function docSearchHost({ body, search, anchor = () => null, focus }) {
  let rows = []; // the blocks as last searched, in document order
  let onHit = false; // the reader's place is the current hit (until a link step: leaveHit)

  const render = () => {
    const root = body();
    unpaintHits(root);
    rows = Array.from(root.querySelectorAll(DOC_ROWS));
    if (!search.query) return;
    search.refind(rows.map((el, i) => ({ row: i, side: 0, text: el.textContent })));
    rows.forEach((el, i) => {
      const spans = search.hitsOn(i, 0);
      if (spans.length) paintRow(el, spans);
    });
  };
  const hitEls = (i) => body().querySelectorAll(`.hit[data-h="${i}"]`);
  const goTo = (i) => {
    if (i < 0 || i >= search.hits.length) return;
    if (search.cur !== i) {
      for (const el of hitEls(search.cur)) el.classList.remove("cur");
      search.cur = i;
      for (const el of hitEls(i)) el.classList.add("cur");
    }
    hitEls(i)[0]?.scrollIntoView({ block: "center", inline: "nearest" });
    onHit = true;
  };
  const here = () => {
    const root = body();
    if (!rows.length || !root.contains(rows[0])) rows = Array.from(root.querySelectorAll(DOC_ROWS));
    if (!rows.length) return { row: 0, side: 0, col: -1 };
    const box = root.getBoundingClientRect();
    const onScreen = (el) => {
      const r = el.getBoundingClientRect();
      return r.bottom > box.top && r.top < box.bottom;
    };
    // The TUI's cursor follows the hits: the current one, while on screen.
    const cur = onHit && search.cur >= 0 && search.cur < search.hits.length ? hitEls(search.cur)[0] : null;
    if (cur && onScreen(cur)) {
      const h = search.hits[search.cur];
      return { row: h.row, side: h.side, col: h.start };
    }
    const a = anchor();
    if (a && root.contains(a)) {
      const i = rows.findIndex((el) => el.contains(a));
      if (i >= 0 && onScreen(a)) {
        // From the link itself: a hit earlier in its block is behind it.
        const pre = document.createRange();
        pre.setStart(rows[i], 0);
        pre.setEndBefore(a);
        return { row: i, side: 0, col: Array.from(pre.toString()).length };
      }
    }
    const i = firstRowBelow(rows.length, (r) => rows[r].getBoundingClientRect().bottom, box.top);
    return { row: Math.min(i, rows.length - 1), side: 0, col: -1 };
  };
  return {
    search,
    render,
    goTo,
    here,
    origin: () => ({ top: body().scrollTop }),
    restore: (o) => {
      body().scrollTop = o.top;
    },
    focus,
    cursorLed: () => true,
    leaveHit: () => {
      onHit = false;
    },
  };
}

export { docSearchHost };
