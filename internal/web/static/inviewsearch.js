// inviewsearch.js — the in-view text search behind `/` in the diff pane and
// the blame overlay: the browser port of the TUI's textsearch.go (spec §4.3 of
// the hunk-parity roadmap). Two halves live here:
//
//   the ENGINE — findHits / nearestHit / stepHit / Search — pure functions
//   over {row, side, text} lines, so the semantics are testable outside a
//   browser and identical to the TUI's: case-insensitive, matched on RUNES
//   (a hit's offsets are what renderCell's rune walk paints), overlapping
//   matches not reported, `]`/`[` strict and wrapping in DOCUMENT order
//   whatever direction the search was opened in;
//
//   the BAR — searchbar.js's bindSearchBar wires one host's `<input>` to
//   its Search: typing re-finds live and scrolls to the nearest hit from
//   where the reader was, enter keeps the query and hands the keyboard back
//   to the view, esc while typing puts the view back where it started, esc
//   with a kept query clears it (and only the NEXT esc leaves the view).
//
// This file is the engine alone: no imports, no DOM, so `node` can run it
// against the TUI's expectations. Hosts (files.js for the diff, filehist.js
// for blame) own their lines, their rendering and their notion of "where the
// reader is".

// foldRunes lowercases s rune by rune, 1:1: a rune whose lowercase is more
// than one code point (İ → i̇) keeps itself, so an index into the folded
// array is an index into the original — the TUI's foldRunes.
function foldRunes(s) {
  return Array.from(s).map((r) => {
    const l = r.toLowerCase();
    return Array.from(l).length === 1 ? l : r;
  });
}

// findHits returns every case-insensitive match of query in lines, in the
// order the lines are given (the caller controls document order) and left to
// right within a line, each {row, side, start, end} in rune offsets.
function findHits(lines, query) {
  const q = foldRunes(query);
  if (!q.length) return [];
  const out = [];
  for (const ln of lines) {
    const t = foldRunes(ln.text || "");
    for (let i = 0; i + q.length <= t.length; ) {
      let ok = true;
      for (let k = 0; k < q.length; k++) {
        if (t[i + k] !== q[k]) { ok = false; break; }
      }
      if (ok) {
        out.push({ row: ln.row, side: ln.side, start: i, end: i + q.length });
        i += q.length;
      } else i++;
    }
  }
  return out;
}

// A position is a (row, side, col) triple; col -1 is "before anything on
// this line", which is what a row-only anchor (the first row on screen) uses.
function before(a, b) {
  if (a.row !== b.row) return a.row < b.row;
  if (a.side !== b.side) return a.side < b.side;
  return a.col < b.col;
}

function hitPos(h) {
  return { row: h.row, side: h.side, col: h.start };
}

// nearestHit is where a typed query lands: the first hit at or after pos
// (forward) or at or before it (backward), wrapping; -1 with no hits.
function nearestHit(hits, pos, backward) {
  if (!hits.length) return -1;
  if (backward) {
    for (let i = hits.length - 1; i >= 0; i--) if (!before(pos, hitPos(hits[i]))) return i;
    return hits.length - 1;
  }
  for (let i = 0; i < hits.length; i++) if (!before(hitPos(hits[i]), pos)) return i;
  return 0;
}

// stepHit is `]` (delta 1) / `[` (delta -1): STRICTLY after / before pos,
// wrapping around the document; -1 with no hits.
function stepHit(hits, pos, delta) {
  if (!hits.length) return -1;
  if (delta >= 0) {
    for (let i = 0; i < hits.length; i++) if (before(pos, hitPos(hits[i]))) return i;
    return 0;
  }
  for (let i = hits.length - 1; i >= 0; i--) if (before(hitPos(hits[i]), pos)) return i;
  return hits.length - 1;
}

// Search is one host's search state: the query, whether it is being typed,
// the direction it was opened in, the hits over the host's current lines and
// the current one, plus the origin the typing anchors on (and esc restores).
class Search {
  constructor() {
    this.clear();
  }

  clear() {
    this.query = "";
    this.typing = false;
    this.backward = false;
    this.hits = [];
    this.cur = -1;
    this.origin = { row: 0, side: 0, col: -1 };
    this.byLine = new Map();
  }

  active() {
    return this.typing || this.query !== "";
  }

  open(backward, pos) {
    this.query = "";
    this.typing = true;
    this.backward = backward;
    this.hits = [];
    this.cur = -1;
    this.origin = pos;
    this.byLine = new Map();
  }

  // anchor is where a re-find lands the current hit: the origin while the
  // query is being typed (every keystroke measures from where the reader
  // was), else the current hit itself, so a re-render (f toggle, resize,
  // notes refresh) keeps the reader on the same hit when it survives.
  anchor() {
    if (!this.typing && this.cur >= 0 && this.cur < this.hits.length) return hitPos(this.hits[this.cur]);
    return this.origin;
  }

  // refind re-runs the query over lines (the host's VISIBLE lines, in
  // document order) and re-anchors the current hit.
  refind(lines) {
    const pos = this.anchor();
    this.hits = this.query ? findHits(lines, this.query) : [];
    this.cur = nearestHit(this.hits, pos, this.backward);
    this.byLine = new Map();
    this.hits.forEach((h, i) => {
      const k = h.row + ":" + h.side;
      const l = this.byLine.get(k);
      if (l) l.push(i);
      else this.byLine.set(k, [i]);
    });
  }

  // badge is the TUI's status text: the lead (/ or @), the query, a block
  // cursor while typing, "i/n" once there is a query ("0/0" = no match).
  badge() {
    if (!this.active()) return "";
    let b = (this.backward ? "@" : "/") + this.query;
    if (this.typing) b += "█";
    if (this.query === "") return b;
    const n = this.hits.length;
    const i = n > 0 && this.cur >= 0 && this.cur < n ? this.cur + 1 : 0;
    return b + "  " + i + "/" + n;
  }

  // count is the badge's trailing "i/n" alone — the bar shows the query in
  // its input, so only the count is painted beside it.
  count() {
    if (this.query === "") return "";
    const n = this.hits.length;
    const i = n > 0 && this.cur >= 0 && this.cur < n ? this.cur + 1 : 0;
    return i + "/" + n;
  }

  // hitsOn returns the spans a renderer paints on one line, each with its
  // hit index and whether it is the current one.
  hitsOn(row, side) {
    const l = this.byLine.get(row + ":" + side);
    if (!l) return [];
    return l.map((i) => ({ start: this.hits[i].start, end: this.hits[i].end, idx: i, cur: i === this.cur }));
  }

  // pos is where `]` / `[` measure from: the current hit, else the host's
  // notion of where the reader is (the first row on screen).
  pos(fallback) {
    if (this.cur >= 0 && this.cur < this.hits.length) return hitPos(this.hits[this.cur]);
    return fallback;
  }
}


export { Search, findHits, foldRunes, hitPos, nearestHit, stepHit };
