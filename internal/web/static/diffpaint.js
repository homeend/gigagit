// diffpaint.js — part of gg's web client: rows painted in chunks, for every
// host that shows a file — a diff's table, blame's and the viewer's line rows.
// The rows that fill three screens go in at once; the rest are appended a
// chunk at a time while the browser is idle, so a 13k-row file shows in a
// blink instead of the second its rows take to lay out. Each chunk is its own
// container (a diff's <table> with the same columns, a line host's <div>)
// that the browser skips while it is off screen (content-visibility):
// appending one never re-lays out the rows before it, which is what made the
// first seconds choppy. The finished rows are exactly the one-shot rows,
// split across those containers — a walk from row to row crosses them (files.js
// nextRow for a diff).
//
// Anything that reads the rows must call finishPaint (or paintUntil, when
// the first row matching what it wants is enough) first — the hosts'
// accessors in files.js / filehist.js do that, so a reader never acts on a
// half-painted table.

// --- chunk ranges (pure; guarded against Go) ---
// chunkRanges splits rows [0, n) into the first range [0, first) and then
// ranges of `size` rows, in order; every row in exactly one range.
function chunkRanges(n, first, size) {
  const out = [];
  let at = 0;
  let len = Math.max(1, first);
  while (at < n) {
    const end = Math.min(n, at + len);
    out.push([at, end]);
    at = end;
    len = Math.max(1, size);
  }
  return out;
}

// chunkOpen is a chunk's opening: the source's container tag (its first tag
// — a diff's <table>, a line host's <div>) with the skip-when-off-screen
// style and a placeholder height for the rows it holds (`auto` = the browser
// keeps the real height once it has laid it out).
function chunkOpen(head, rows, rowPx) {
  const h = rows && rowPx ? Math.round(rows * rowPx) : 0;
  const style = h ? `content-visibility:auto;contain-intrinsic-size:auto ${h}px` : "content-visibility:auto";
  return head.replace(/^<([a-z]+)/, `<$1 style="${style}"`);
}
// --- end chunk ranges ---

const CHUNK_ROWS = 200; // a table of rows per idle slice: ≈ 20–40 ms of building + its own layout

const jobs = new WeakMap(); // host → its pending paint
let genSeq = 0;

const idle = window.requestIdleCallback
  ? (fn) => window.requestIdleCallback(fn, { timeout: 50 })
  : (fn) => setTimeout(fn, 0);

// paintTable paints source t into host: {head, open, close, n, row(k), tail}
// — diffTable's result, or a line host's rows (head = open = its chunk <div>,
// close = </div>). A non-row outcome ({html}) is written as is. Otherwise the
// rows go in as a run of CONTAINERS of CHUNK_ROWS rows each (`open` for every
// one after the first, closed by `close`, default </table>), every one
// `content-visibility: auto` — the browser lays out and paints only the ones
// near the screen, so appending a chunk costs that chunk, not the whole
// file again. The tables reaching `cover` px (three screens, or down to the
// scroll position a re-render keeps) go in at once; the rest follow in idle
// slices. A later paint of the host supersedes this one.
function paintTable(host, t, { scroller = host, keep = false } = {}) {
  jobs.delete(host);
  if (t.html !== undefined) {
    host.innerHTML = t.html;
    return;
  }
  const top = scroller.scrollTop;
  const cover = (keep ? top : 0) + 3 * Math.max(scroller.clientHeight, 300);
  const ranges = chunkRanges(t.n, CHUNK_ROWS, CHUNK_ROWS);
  const job = { gen: ++genSeq, t, ranges, next: 0, first: null, last: null, rowPx: 0, after: [] };
  const close = t.close || "</table>";
  const after = t.tail.startsWith(close) ? t.tail.slice(close.length) : "";
  busy = true;
  try {
    // The first chunk is never skipped: it is on screen, and it is measured
    // for every later chunk's placeholder height — a container the browser
    // has not yet judged visible (a host shown in this very task) would
    // measure 0 and collapse them all.
    host.innerHTML = t.head + rowsHTML(t, ranges[0] || [0, 0]) + close + after;
  } finally {
    busy = false;
  }
  job.next = ranges.length ? 1 : 0;
  job.first = job.last = host.firstElementChild;
  if (!job.first || job.next >= ranges.length) return done(host, job);
  const rows = ranges[0][1] - ranges[0][0];
  job.rowPx = rows ? job.first.offsetHeight / rows : 20; // a later chunk's placeholder height
  jobs.set(host, job);
  // A hidden host (display:none: its overlay not shown yet) has no height to
  // fill — measuring would paint every chunk now; the idle slices do it.
  const shown = host.getClientRects().length > 0;
  while (shown && job.next < ranges.length && host.scrollHeight < cover) paintNext(job);
  if (keep && scroller.scrollTop !== top) scroller.scrollTop = top;
  if (job.next >= ranges.length) return done(host, job);
  const step = () => {
    if (jobs.get(host) !== job) return; // superseded or finished meanwhile
    if (!job.first.isConnected) return jobs.delete(host); // the host was rewritten
    paintNext(job);
    if (job.next >= job.ranges.length) done(host, job);
    else idle(step);
  };
  idle(step);
}

function rowsHTML(t, [a, b]) {
  let s = "";
  for (let k = a; k < b; k++) s += t.row(k);
  return s;
}

// busy is true while rows are being built: a row's HTML may measure the
// host (a note title's width) and must never re-enter the painter.
let busy = false;

function paintNext(job) {
  const r = job.ranges[job.next++];
  busy = true;
  try {
    job.last.insertAdjacentHTML("afterend", chunkOpen(job.t.open, r[1] - r[0], job.rowPx) + rowsHTML(job.t, r) + (job.t.close || "</table>"));
  } finally {
    busy = false;
  }
  job.last = job.last.nextElementSibling;
}

function done(host, job) {
  if (jobs.get(host) === job) jobs.delete(host);
  for (const fn of job.after.splice(0)) fn();
}

// finishPaint paints every row host still owes, now. No-op when none.
function finishPaint(host) {
  paintUntil(host, () => false);
}

// paintUntil paints host's remaining chunks until test() is true (checked
// before each chunk) or the table is complete: a landing on the first
// change needs only the rows up to it, not the whole file.
function paintUntil(host, test) {
  const job = jobs.get(host);
  if (!job || busy) return;
  if (!job.first.isConnected) return void jobs.delete(host);
  while (job.next < job.ranges.length && !test()) paintNext(job);
  if (job.next >= job.ranges.length) done(host, job);
}

// isPainting: host still owes rows.
function isPainting(host) {
  return jobs.has(host);
}

// onPainted runs fn once host's table is complete (now, if it is).
function onPainted(host, fn) {
  const job = jobs.get(host);
  if (job) job.after.push(fn);
  else fn();
}

export { chunkRanges, finishPaint, isPainting, onPainted, paintTable, paintUntil };
