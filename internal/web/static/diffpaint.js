// diffpaint.js — part of gg's web client: a diff table painted in chunks.
// The rows that fill the screen go in at once; the rest are appended a chunk
// at a time while the browser is idle, so a 13k-row file shows in a blink
// instead of the second its whole table takes to lay out. The finished DOM
// is exactly the one-shot table's.
//
// Anything that reads the rows must call finishPaint (or paintUntil, when
// the first row matching what it wants is enough) first — the hosts'
// accessors in files.js / filehist.js do that, so a reader never acts on a
// half-painted table.

// --- chunk ranges (pure; guarded against Go) ---
// chunkRanges splits rows [0, n) into the first range [0, first) and then
// ranges of size rows, in order; every row in exactly one range.
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
// --- end chunk ranges ---

const FIRST_ROWS = 60; // the first range: a screenful at the smallest row height
const CHUNK_ROWS = 400; // ≈ 30 ms of building + layout per idle slice

const jobs = new WeakMap(); // host → its pending paint
let genSeq = 0;

const idle = window.requestIdleCallback
  ? (fn) => window.requestIdleCallback(fn, { timeout: 50 })
  : (fn) => setTimeout(fn, 0);

// paintTable paints t (diffTable's result) into host. A non-table outcome
// ({html}) is written as is. Otherwise the rows reach `cover` px of table
// height synchronously — the visible screen, or the scroll position a
// re-render keeps (the scroller's scrollTop is restored if the shorter first
// paint clamped it) — and the rest follow in idle chunks. A later paint of
// the host supersedes this one.
function paintTable(host, t, { scroller = host, keep = false } = {}) {
  jobs.delete(host);
  if (t.html !== undefined) {
    host.innerHTML = t.html;
    return;
  }
  const top = scroller.scrollTop;
  const cover = (keep ? top : 0) + 2 * Math.max(scroller.clientHeight, 300);
  const ranges = chunkRanges(t.n, FIRST_ROWS, CHUNK_ROWS);
  const job = { gen: ++genSeq, t, ranges, next: 0, table: null, body: null, after: [] };
  const first = ranges.length ? ranges[0] : [0, 0];
  busy = true;
  try {
    host.innerHTML = t.head + rowsHTML(t, first) + t.tail;
  } finally {
    busy = false;
  }
  job.next = ranges.length ? 1 : 0;
  job.table = host.querySelector("table.diff:last-of-type") || host.querySelector("table.diff");
  job.body = job.table ? job.table.tBodies[job.table.tBodies.length - 1] || job.table : null;
  if (!job.body || job.next >= ranges.length) return done(host, job);
  jobs.set(host, job);
  while (job.next < ranges.length && job.table.offsetHeight < cover) paintNext(job);
  if (keep && scroller.scrollTop !== top) scroller.scrollTop = top;
  if (job.next >= ranges.length) return done(host, job);
  const step = () => {
    if (jobs.get(host) !== job) return; // superseded or finished meanwhile
    if (!job.table.isConnected) return jobs.delete(host); // the host was rewritten
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
  busy = true;
  try {
    job.body.insertAdjacentHTML("beforeend", rowsHTML(job.t, job.ranges[job.next++]));
  } finally {
    busy = false;
  }
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
  if (!job.table.isConnected) return void jobs.delete(host);
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
