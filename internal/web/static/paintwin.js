// paintwin.js — the in-view search's hits, painted only on the rows near the
// screen (the TUI paints only the rows it draws). A host that keeps a whole
// file in the DOM (blame, the F preview) re-finds over every line on a
// keystroke — the count is the file's — but a redraw of every row cost
// seconds on a large file. Each row remembers the paint generation it was
// drawn for; a new query bumps the generation; only the stale rows within a
// screenful of the window are redrawn, the rest when they scroll near.
import { firstRowBelow, rowsToPaint } from "./inviewsearch.js";

// windowPainter wraps one host:
//   body()     its scroll container
//   rows()     its line rows in document order, row i = line i; null when
//              the body does not show lines (a notice, an image)
//   shown()    whether the host is on screen at all
//   paint(i)   redraw row i with the current hits; returns how many it drew
//   hasHits(i) whether line i has hits for the current query
// The host calls reset(n) after it draws n plain rows (markPainted(i) for a
// row it kept that still carries an older paint), repaint() after a
// re-find, paintRow(i) to freshen one row (the current hit's), and wires
// queue() to its scroll (and anything else that moves rows on screen).
function windowPainter(host) {
  let gen = 0;
  let at = new Uint32Array(0);
  let painted = new Uint8Array(0);
  let queued = false;
  const p = {
    reset(n) {
      // Nothing is fresh after a reset (at[] is 0): a reset inside a paint
      // pass (rows() taking new rows) must not mark every row painted.
      gen = 1;
      at = new Uint32Array(n);
      painted = new Uint8Array(n);
    },
    markPainted(i) {
      painted[i] = 1;
    },
    paintRow(i) {
      painted[i] = host.paint(i) ? 1 : 0;
      at[i] = gen;
    },
    // firstVisible is the first row reaching the window's top edge (0 when
    // there are no rows).
    firstVisible() {
      const rows = host.rows();
      if (!rows || !rows.length) return 0;
      const top = host.body().getBoundingClientRect().top;
      return Math.min(firstRowBelow(rows.length, (r) => rows[r].getBoundingClientRect().bottom, top), rows.length - 1);
    },
    // visible is [first, last]: the rows on screen.
    visible() {
      const rows = host.rows();
      if (!rows || !rows.length) return [0, -1];
      const box = host.body().getBoundingClientRect();
      const bottomOf = (r) => rows[r].getBoundingClientRect().bottom;
      const first = Math.min(firstRowBelow(rows.length, bottomOf, box.top), rows.length - 1);
      const last = Math.min(rows.length - 1, firstRowBelow(rows.length, bottomOf, box.bottom));
      return [first, last];
    },
    paintWindow() {
      if (!host.shown()) return;
      const rows = host.rows();
      if (!rows || rows.length !== at.length || !rows.length) return;
      const [first, last] = p.visible();
      const margin = Math.max(40, last - first);
      const lo = Math.max(0, first - margin), hi = Math.min(rows.length - 1, last + margin);
      for (const i of rowsToPaint(lo, hi, gen, at, painted, host.hasHits)) p.paintRow(i);
    },
    repaint() {
      gen++;
      p.paintWindow();
    },
    queue() {
      if (queued) return;
      queued = true;
      requestAnimationFrame(() => {
        queued = false;
        p.paintWindow();
      });
    },
  };
  return p;
}

export { windowPainter };
