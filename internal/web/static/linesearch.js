// linesearch.js — the in-view search (/ @ ] [) over a file shown as LINE
// ROWS, shared by every host that draws one: the F preview and the file
// viewer (a file, or its one-column diff). The TUI has one component for
// both (contentPopup); here the two renderers differ, but the search is this
// one host: the same engine, the same painting near the screen, the same
// landing.
//
// A typed key re-finds over every line — each line's fold made once — and
// redraws only the rows within a screenful of the view (paintwin.js); rows
// further away are redrawn as they scroll near. A host's render draws its
// rows plain and calls rowsChanged(); the next paint takes them afresh.
import { firstRowBelow, foldRunes } from "./inviewsearch.js";
import { windowPainter } from "./paintwin.js";

const folds = new WeakMap(); // a line object → its case fold

function foldOf(ref, text) {
  if (!ref) return foldRunes(text);
  let f = folds.get(ref);
  if (f === undefined || f.text !== text) folds.set(ref, (f = { text, fold: foldRunes(text) }));
  return f.fold;
}

// lineSearchHost wraps one host (searchbar.js's host verbs come out of it):
//   search      its Search
//   body()      the scroll container
//   rowSel      the selector of its line rows (other children — note boxes,
//               fold rows — are skipped)
//   keyOf(el)   a row's search row (its line index, a diff row's index)
//   lines()     [{row, text, ref}]: the searched lines in document order;
//               ref is the line's own object (its fold is kept on it)
//   paint(el, hits, row)  redraw a row's text with hits (empty = plain)
//   shown()     whether the host is on screen
//   here()      optional: where a search starts (default: the first row on
//               screen)
//   onLand(h)   optional: a hit was landed on (the host's cursor follows)
//   finish()    optional: finish the host's chunked paint (diffpaint.js) —
//               called before anything that reads every row
//   painting()  optional: the host still owes rows (its rows are not taken
//               until it is done)
function lineSearchHost(o) {
  const s = o.search;
  let els = [];
  let keys = [];
  let idx = new Map(); // search row → its row indices
  let dirty = true;
  let source = null;

  const finish = () => {
    if (o.finish) o.finish();
  };

  const take = () => {
    if (!dirty) return els;
    if (o.painting && o.painting()) return []; // taken once the paint is done
    dirty = false;
    const body = o.body();
    els = body ? Array.from(body.querySelectorAll(o.rowSel)) : [];
    keys = els.map(o.keyOf);
    idx = new Map();
    keys.forEach((k, i) => {
      const l = idx.get(k);
      if (l) l.push(i);
      else idx.set(k, [i]);
    });
    painter.reset(els.length);
    // Rows a host drew with hits already (or kept from an older paint) are
    // on record, so a query that drops their hits redraws them plain.
    if (body) {
      const at = new Map(els.map((el, i) => [el, i]));
      for (const h of body.querySelectorAll(".hit")) {
        const i = at.get(h.closest(o.rowSel));
        if (i !== undefined) painter.markPainted(i);
      }
    }
    return els;
  };

  const painter = windowPainter({
    body: o.body,
    rows: take,
    shown: o.shown,
    paint: (i) => {
      const hits = s.query ? s.hitsOn(keys[i], 0) : [];
      o.paint(els[i], hits, keys[i]);
      return hits.length;
    },
    hasHits: (i) => s.byLine.has(keys[i] + ":0"),
  });

  const lines = () => {
    if (!source) source = o.lines().map((l) => ({ row: l.row, side: 0, text: l.text || "", fold: foldOf(l.ref, l.text || "") }));
    return source;
  };

  const rowsOf = (i) => {
    if (i < 0 || i >= s.hits.length) return [];
    take();
    return idx.get(s.hits[i].row) || [];
  };

  const host = {
    // rowsChanged: the host drew its rows again (and maybe other lines).
    rowsChanged() {
      dirty = true;
      source = null;
    },
    // render re-finds the query and repaints near the screen.
    render() {
      finish();
      s.refind(s.query ? lines() : []);
      take();
      painter.repaint();
    },
    goTo(i) {
      if (i < 0 || i >= s.hits.length) return;
      finish();
      const was = s.cur;
      s.cur = i;
      for (const j of rowsOf(was)) painter.paintRow(j); // the old current hit loses its outline
      const mine = rowsOf(i);
      for (const j of mine) painter.paintRow(j);
      const el = mine.length ? els[mine[0]].querySelector(`.hit[data-h="${i}"]`) : null;
      if (el) el.scrollIntoView({ block: "center", inline: "nearest" });
      if (o.onLand) o.onLand(s.hits[i]);
    },
    here() {
      if (o.here) return o.here();
      finish();
      take();
      if (!els.length) return { row: 0, side: 0, col: -1 };
      const top = o.body().getBoundingClientRect().top;
      const i = Math.min(firstRowBelow(els.length, (r) => els[r].getBoundingClientRect().bottom, top), els.length - 1);
      return { row: keys[i], side: 0, col: -1 };
    },
    queue: () => painter.queue(),
    // paintEl redraws one painted row (search row k) with its current hits:
    // a row whose text took new colours.
    paintEl(el, k) {
      o.paint(el, s.query ? s.hitsOn(k, 0) : [], k);
    },
    // visible is [first, last]: the row indices on screen.
    visible() {
      take();
      return painter.visible();
    },
    firstVisibleRow() {
      take();
      return els.length ? keys[painter.firstVisible()] : 0;
    },
  };
  return host;
}

export { lineSearchHost };
