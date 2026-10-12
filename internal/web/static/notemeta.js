// notemeta.js — a note's meta as HTML (spec 2026-10-12-note-meta §7): the
// chips, the audience marker, the line and details rows, the counts, and the
// title fit. The server sends the RESOLVED display (notemeta.Resolved) on
// every note; nothing here decides what shows, it only draws it the way the
// TUI does (internal/tui/note_meta.go). Import-free on purpose, so
// notemetajs_test.go runs it under node; callers pass esc in.

// META_COLOURS is the fixed palette (spec §3.3): a name off this list is
// never turned into a class — it falls to the place's default.
export const META_COLOURS = ["red", "orange", "yellow", "green", "blue", "purple", "grey"];
export const META_TITLE_SEP = " · ";
const MIN_PATH_COLS = 8;

const colourOr = (c, dflt) => (META_COLOURS.includes(c) ? c : dflt);
const cols = (s) => [...String(s)].length;

// metaWord is a value as shown: agent data, verbatim (escaped by the caller).
export function metaWord(key, value) {
  return String(value == null ? "" : value);
}

// audienceWord is gg's marker for who a note is for (spec §3.3).
export function audienceWord(a) {
  return a === "author" ? "→ author" : a === "reviewer" ? "for you" : "";
}

// chipText is a badge's text: the glyph (if any), the value; a filled chip
// carries one column of fill each side.
function chipText(b) {
  const v = (b.glyph ? b.glyph + " " : "") + metaWord(b.key, b.value);
  return b.style === "filled" ? " " + v + " " : v;
}

// runParts is the badge run as [text, cls] pieces with the TUI's spacing:
// one space beside a filled chip, two between text badges, two before the
// marker.
function runParts(d) {
  const out = [];
  const bs = (d && d.badges) || [];
  bs.forEach((b, i) => {
    if (i > 0) out.push([b.style === "filled" || bs[i - 1].style === "filled" ? " " : "  ", ""]);
    out.push([chipText(b), `mchip mc-${colourOr(b.color, "grey")} ms-${b.style === "filled" ? "filled" : "text"}`]);
  });
  const m = audienceWord(d && d.audience);
  if (m) {
    if (out.length) out.push(["  ", ""]);
    out.push([m, "maud"]);
  }
  return out;
}

export function badgeRunHTML(d, esc) {
  return runParts(d).map(([t, c]) => (c ? `<span class="${c}">${esc(t)}</span>` : t)).join("");
}

export function badgeRunCols(d) {
  return runParts(d).reduce((n, [t]) => n + cols(t), 0);
}

// titleMetaHTML is what leads a note's title: the run, then " · ".
export function titleMetaHTML(d, esc) {
  const run = badgeRunHTML(d, esc);
  return run ? run + `<span class="msep">${META_TITLE_SEP}</span>` : "";
}

// metaLinesHTML is one dim "label: value" row per line entry (spec §3.2).
export function metaLinesHTML(d, esc) {
  return ((d && d.lines) || []).map((l) => `<div class="mline">${esc(l.label + ": " + l.value)}</div>`).join("");
}

// metaDetailsHTML is the details rows: stored meta, the added changes, the
// display problems (the TUI's metaDetailTexts, word for word).
export function metaDetailsHTML(d, esc) {
  if (!d) return "";
  const rows = [];
  if ((d.stored || []).length) rows.push("meta: " + d.stored.map((kv) => kv.key + ": " + kv.value).join(" · "));
  if ((d.added || []).length)
    rows.push(
      "added: " +
        d.added.map((c) => c.key + ": " + (c.from ? c.from + " → " + c.to : c.to) + (c.by ? " (" + c.by + ")" : "")).join(" · ")
    );
  for (const p of d.problems || []) rows.push("display problem: " + p);
  return rows.map((r) => `<div class="mdetail">${esc(r)}</div>`).join("");
}

// frameClass is the box's border colour class, "" for none (or a colour off
// the palette). The stylesheet lets stale/outdated win over it.
export function frameClass(d) {
  return d && META_COLOURS.includes(d.frame) ? "mf-" + d.frame : "";
}

// countsText is the header's counts in plain words (spec §6.2).
export function countsText(c) {
  if (!c) return "";
  const parts = [];
  for (const g of c.groups || []) for (const v of g.values || []) parts.push(v.n + " " + metaWord(g.key, v.value));
  if (c.author > 0) parts.push(c.author + " for author");
  if (c.reviewer > 0) parts.push(c.reviewer + " for you");
  return parts.join(" · ");
}

// countsRunHTML is a file row's compact counts: "issue 1 good 1", the
// value's colour on its number.
export function countsRunHTML(c, esc) {
  const parts = [];
  for (const g of (c && c.groups) || [])
    for (const v of g.values || [])
      parts.push(`${esc(metaWord(g.key, v.value))} <span class="mc-${colourOr(v.color, "grey")} ms-text">${v.n}</span>`);
  return parts.join(" ");
}

// severityColour is the colour the note's resolved badges give its severity
// — the send panel's .psev chip — "" when no badge says.
export function severityColour(d, severity) {
  const b = ((d && d.badges) || []).find((x) => x.key === "severity" && x.value === severity);
  return b && META_COLOURS.includes(b.color) ? b.color : "";
}

// truncate cuts s to n columns, its last one an ellipsis.
function truncate(s, n) {
  if (n <= 0) return "";
  const r = [...s];
  return r.length <= n ? s : r.slice(0, n - 1).join("") + "…";
}

// fitNoteTitle is the TUI's fitNoteTitle (internal/tui/note_meta.go): it
// fits words + path + after ("note · bot · ", "a/b.go", " R12 (stale)")
// beside badgeCols of badges (and their " · ") into room columns. Whole when
// it fits; else only the PATH loses its middle (elide = core's elidePath),
// the place after it kept whole; below MIN_PATH_COLS the words before the
// path are cut first, then the path's tail; the badges are never cut here
// (the stylesheet's ellipsis backstops). room <= 0 = an unknown width:
// everything whole. Returns {words, at} with at = the fitted path + after.
export function fitNoteTitle(badgeCols, words, path, after, room, elide) {
  const whole = { words, at: path + after };
  if (room <= 0) return whole;
  const gap = badgeCols > 0 ? cols(META_TITLE_SEP) : 0;
  if (badgeCols + gap + cols(words) + cols(path) + cols(after) <= room) return whole;
  const textRoom = room - badgeCols - gap;
  if (textRoom < 1) return { words: "", at: "" };
  const pw = textRoom - cols(words) - cols(after);
  if (pw >= MIN_PATH_COLS) return { words, at: elide(path, pw) + after };
  // The file and its line outrank the words before them: those are cut
  // first, the path's tail only when nothing else is left.
  const tail = elide(path, MIN_PATH_COLS) + after;
  const rest = textRoom - cols(tail);
  if (rest >= 0) return { words: truncate(words, rest), at: tail };
  return { words: "", at: truncate(tail, textRoom) };
}
