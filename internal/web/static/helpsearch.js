// helpsearch.js — the in-view search (/ @ ] [) of the ? help overlay: the
// third host of the engine in inviewsearch.js and the bar in searchbar.js
// (the diff pane and blame are the other two), and the browser side of the
// TUI's `/` in its help viewer.
//
// The help is static rows (index.html's, plus every module's registerHelp
// at boot), each carrying <b> and <code>. So instead of re-rendering from
// data the way the diff does, the host SNAPSHOTS each row's markup when the
// overlay opens and paints hits by rewrapping the row's text nodes: the
// row's text (its textContent) is the searched line, a TreeWalker over the
// same text nodes lays the hit spans over it in the same rune order, and a
// restore puts the snapshot back before every repaint. The key and the
// description spans meet with no separator in textContent, so a query can
// match across that seam — harmless, and cheaper than inventing one.
import { $ } from "./core.js";
import { Search } from "./inviewsearch.js";
import { bindSearchBar } from "./searchbar.js";

const helpSearch = new Search();

// lines is the snapshot: one entry per searchable row (headings included,
// as in the TUI's content viewer), in document order, taken at open.
let lines = []; // [{el, html, text}]


function snapshot() {
  lines = Array.from($("help-body").querySelectorAll("h3, .hrow")).map((el) => ({
    el,
    html: el.innerHTML,
    text: el.textContent,
  }));
}


// paintRow lays hit spans over one row's text nodes. hit[i] is 1 + the hit's
// index for rune i of the row's text (0 = none), cur[i] whether it is the
// current one — renderCell's arrays, over text nodes instead of one string.
function paintRow(el, spans) {
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


// render restores every row and repaints the hits of the query as it is:
// the re-find runs over the snapshot (the rows never change while open).
function render() {
  if (helpSearch.query) helpSearch.refind(lines.map((l, i) => ({ row: i, side: 0, text: l.text })));
  lines.forEach((l, i) => {
    l.el.innerHTML = l.html;
    if (!helpSearch.query) return;
    const spans = helpSearch.hitsOn(i, 0);
    if (spans.length) paintRow(l.el, spans);
  });
}


function helpHitEls(i) {
  return $("help-body").querySelectorAll(`.hit[data-h="${i}"]`);
}


function goToHelpHit(i) {
  if (i < 0 || i >= helpSearch.hits.length) return;
  if (helpSearch.cur !== i) {
    for (const el of helpHitEls(helpSearch.cur)) el.classList.remove("cur");
    helpSearch.cur = i;
    for (const el of helpHitEls(i)) el.classList.add("cur");
  }
  const el = helpHitEls(i)[0];
  if (el) el.scrollIntoView({ block: "center", inline: "nearest" });
}


const helpSearchBar = bindSearchBar("help-search", {
  search: helpSearch,
  here: () => {
    const box = $("help-body").getBoundingClientRect();
    for (let i = 0; i < lines.length; i++) {
      if (lines[i].el.getBoundingClientRect().bottom >= box.top) return { row: i, side: 0, col: -1 };
    }
    return { row: 0, side: 0, col: -1 };
  },
  origin: () => ({ top: $("help-body").scrollTop }),
  restore: (o) => {
    $("help-body").scrollTop = o.top;
  },
  render: () => {
    const body = $("help-body");
    const top = body.scrollTop;
    render();
    body.scrollTop = top;
  },
  goTo: goToHelpHit,
});


// openHelpSearch is called as the overlay opens: any leftover query goes
// (with its tint), and the rows are snapshotted as they stand now — every
// module's registerHelp has long run by then.
function openHelpSearch() {
  helpSearchBar.clear();
  snapshot();
}


// clearHelpSearch drops the query and its paint; the overlay's close paths
// call it so a kept query never survives into the next open.
function clearHelpSearch() {
  helpSearchBar.clear();
}


// helpSearchKey is the help layer's first refusal on a bare key: / and @
// open a search, ] and [ step a kept one, esc clears a kept one (and
// reports it, so the layer's own esc does not ALSO close the overlay).
function helpSearchKey(e) {
  if (e.ctrlKey || e.metaKey || e.altKey) return false;
  if (e.key === "/" || e.key === "@") {
    e.preventDefault(); // the browser's quick-find, and the key must not land in the input
    helpSearchBar.open(e.key === "@");
    return true;
  }
  if (e.key === "]" || e.key === "[") {
    if (!helpSearch.query) return false;
    e.preventDefault();
    helpSearchBar.step(e.key === "]" ? 1 : -1);
    return true;
  }
  if (e.key === "Escape" && helpSearch.active()) {
    helpSearchBar.clear();
    return true;
  }
  return false;
}


// The backdrop click closes the overlay in layers.js, which cannot import
// this module; the query is dropped here on the same click.
$("help").addEventListener("click", (e) => {
  if (e.target === $("help")) clearHelpSearch();
});


export { clearHelpSearch, helpSearchKey, openHelpSearch };
