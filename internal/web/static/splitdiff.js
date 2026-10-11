// splitdiff.js — the overview split's right column as the review's diff
// (spec 2026-10-11-overview-split-diff): /api/diff's aligned pairs read as
// ONE column, and every row ↔ line question the viewer asks of it. The
// viewer keeps view.cur a NEW line number everywhere; these helpers map it.

// --- split diff model (pure; guarded against Go) ---
// A row: {i, kind: "same" | "add" | "del", old, nu, text, tok, spans} — old
// and nu the line's number on each side (0 = none), i its index.

// unifiedRows reads /api/diff's rows as one column: a run of changed rows
// gives its deleted lines first, then its added ones (a unified diff's
// order); an unchanged row is its new line. Changed words keep their spans.
function unifiedRows(d) {
  const src = (d && d.rows) || [], out = [];
  const push = (kind, old, nu, text, tok, spans) =>
    out.push({ i: out.length, kind, old: old || 0, nu: nu || 0, text: text || "", tok: tok || null, spans: spans && spans.length ? spans : null });
  for (let k = 0; k < src.length; ) {
    const r = src[k];
    if (r.kind === "same") {
      push("same", r.left_no, r.right_no, r.right, r.right_tok, null);
      k++;
      continue;
    }
    let j = k;
    while (j < src.length && src[j].kind !== "same") j++;
    for (let x = k; x < j; x++) if (src[x].kind !== "add") push("del", src[x].left_no, 0, src[x].left, src[x].left_tok, src[x].left_spans);
    for (let x = k; x < j; x++) if (src[x].kind !== "del") push("add", 0, src[x].right_no, src[x].right, src[x].right_tok, src[x].right_spans);
    k = j;
  }
  return out;
}

// lineRows maps each new line to its row: lineRows(rows)[n] = the row of
// new line n (undefined = none).
function lineRows(rows) {
  const by = [];
  for (const r of rows) if (r.nu) by[r.nu] = r.i;
  return by;
}

// rowOfLine is new line's row; the first row when the line has none (0, or
// past the end), -1 with no rows at all.
function rowOfLine(rows, by, line) {
  if (!rows.length) return -1;
  const r = by[line];
  return r === undefined ? 0 : r;
}

// rowNewLine is the new line row i stands for: its own; a deleted row's,
// the next new line below it, else the last one above (0 = none).
function rowNewLine(rows, i) {
  if (!rows[i]) return 0;
  for (let k = i; k < rows.length; k++) if (rows[k].nu) return rows[k].nu;
  for (let k = i - 1; k >= 0; k--) if (rows[k].nu) return rows[k].nu;
  return 0;
}

// rowBand is row i's band kind — "cur" (the current band wins an overlap),
// "other" or "" — over bands as the viewer's anchorBands makes them (new
// lines), cur the current band's index. A deleted row is in a band when the
// band holds the new lines on both sides of it.
function rowBand(rows, i, bands, cur) {
  const r = rows[i];
  if (!r || !bands.length) return "";
  const within = (b, n) => b.start <= n && n <= b.end;
  let test;
  if (r.nu) test = (b) => within(b, r.nu);
  else {
    let p = 0, q = 0;
    for (let k = i - 1; k >= 0 && !p; k--) p = rows[k].nu;
    for (let k = i + 1; k < rows.length && !q; k++) q = rows[k].nu;
    if (!p || !q) return "";
    test = (b) => within(b, p) && within(b, q);
  }
  let kind = "";
  for (let x = 0; x < bands.length; x++) {
    if (!test(bands[x])) continue;
    if (x === cur) return "cur";
    kind = "other";
  }
  return kind;
}

// placeNotes lays boxes on rows: a note covers the rows whose number on its
// side (side "old" = the old numbers, anything else the new) is in its
// lines, first..last; one with no such row is left out. Sorted by first
// row, the given order kept on a tie.
function placeNotes(rows, notes) {
  const out = [];
  for (const n of notes || []) {
    const old = n.side === "old";
    let first = -1, last = -1;
    for (const r of rows) {
      const no = old ? r.old : r.nu;
      if (!no || no < n.start || no > n.end) continue;
      if (first < 0) first = r.i;
      last = r.i;
    }
    if (first >= 0) out.push({ n, first, last });
  }
  return out.sort((a, b) => a.first - b.first);
}

// nextNoteRow is the first row of the next (dir > 0) or previous note
// strictly past row; -1 = none.
function nextNoteRow(placed, row, dir) {
  let hit = -1;
  for (const p of placed) {
    if (dir > 0 && p.first > row) return p.first;
    if (dir < 0 && p.first < row) hit = p.first;
  }
  return hit;
}

// noteAtRow is the first placed note covering row, or null.
function noteAtRow(placed, row) {
  const p = placed.find((x) => x.first <= row && row <= x.last);
  return p ? p.n : null;
}

// diffSource is the url a file's diff is read from (§3): a stored review
// overview's link through the review lane, an agent's HEAD → the working
// tree; "" = none.
function diffSource(from, path) {
  if (!from || !path) return "";
  if (from.stored) {
    const id = String(from.stored.id || "").replace(/^review-overview:/, "");
    return id ? "/api/diff?" + new URLSearchParams({ review: id, path }) : "";
  }
  return "/api/diff?" + new URLSearchParams({ wt: "head", path });
}

const D_SHOWS = " — D shows the file";

// diffNotice is what the column says instead of rows (§4); "" = draw the
// rows. status is a failed read's HTTP status (0 = it did not fail).
function diffNotice(d, status, agent) {
  if (status === 404 && !agent) return "the review is gone" + D_SHOWS;
  if (status) return "could not read the diff" + D_SHOWS;
  if (!d) return "no diff for this file" + D_SHOWS;
  if (d.binary) return "binary file" + D_SHOWS;
  if (d.too_large) return "diff too large" + D_SHOWS;
  if (d.changed === false || !(d.rows || []).some((r) => r.kind !== "same"))
    return (agent ? "no changes in the working tree" : "no change to this file in the review") + D_SHOWS;
  return "";
}

// diffHead is the diff title's parts: the shown name (a rename's old path
// first) and the "left → right" range.
function diffHead(d, path, agent) {
  const left = agent ? "HEAD" : (d && d.left_label) || "?";
  const right = agent ? "working tree" : (d && d.right_label) || "?";
  const old = d && d.old_path && d.old_path !== path ? d.old_path + " → " : "";
  return { name: old + path, range: left + " → " + right };
}

// sameContent: the file a link step opened ({key, path, lines}) holds the
// very text the previous step's did — the same overview, another path,
// something to read (§7).
function sameContent(prev, cur) {
  if (!prev || !cur || prev.key !== cur.key || prev.path === cur.path) return false;
  if (!cur.lines.length || prev.lines.length !== cur.lines.length) return false;
  for (let i = 0; i < cur.lines.length; i++) if (prev.lines[i] !== cur.lines[i]) return false;
  return true;
}
// --- end split diff model ---

export { diffHead, diffNotice, diffSource, lineRows, nextNoteRow, noteAtRow, placeNotes, rowBand, rowNewLine, rowOfLine, sameContent, unifiedRows };
