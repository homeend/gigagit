// splitstack.js — the overview split's stacked file column (spec
// 2026-10-11-web-overview-split §11): which files the stack holds and every
// (file, line) question it asks. The cursor is a (k, line) pair — the file's
// index and its 1-based line, 0 = the file's header — never one row number:
// line numbers repeat across files. Import-free on purpose, so
// splitstackjs_test.go runs it under node; the DOM half is viewer.js.

export const SPLIT_STACK_MAX_IN_FLIGHT = 4; // file reads out at once (spec §11)
export const SPLIT_STACK_FOLD_OVER = 100; // more files than this open folded, all but the target

// anchorPath is the file an overview link names: a file or line link's
// path, a note link's file while the note lives; "" for a link that cannot
// open (plain, missing) or a note that is gone.
export function anchorPath(a) {
  if (!a || a.plain || a.missing) return "";
  return a.path || "";
}

// stackFiles is the stack's files: the review's, in its order, when the
// page's review view is this overview's review (reviewFiles; status "D" =
// deleted at the reviewed commit), else the files the links name, each
// once, in the order first linked.
export function stackFiles(reviewFiles, anchors) {
  if (reviewFiles) return reviewFiles.filter((f) => f.path).map((f) => ({ path: f.path, status: f.status || "", deleted: f.status === "D" }));
  const seen = new Set(), out = [];
  for (const a of anchors || []) {
    const p = anchorPath(a);
    if (!p || seen.has(p)) continue;
    seen.add(p);
    out.push({ path: p, status: "", deleted: false });
  }
  return out;
}

// newSlots is a fresh stack over files: nothing read yet (a deleted file
// never is — it says why), all folded but target when there are many.
export function newSlots(files, target) {
  const fold = files.length > SPLIT_STACK_FOLD_OVER;
  return files.map((f, k) => ({
    path: f.path,
    status: f.status || "",
    load: f.deleted ? "none" : "idle",
    why: f.deleted ? "(deleted at the reviewed commit)" : "",
    collapsed: fold && k !== target,
    lines: [],
    notes: [],
    remarks: [],
    n: 0, // the line count once known: sizes the placeholder
    error: "",
  }));
}

export function slotOf(slots, path) {
  return slots.findIndex((s) => s.path === path);
}

// shows reports whether the slot's own lines are in the stack: unfolded,
// read, no placeholder, not empty.
export function shows(s) {
  return !!s && !s.collapsed && s.load === "ok" && !s.why && s.lines.length > 0;
}

// stackMove is the cursor moved delta places — a file's header, then its
// shown lines, file after file — clamped at both ends.
export function stackMove(slots, cur, delta) {
  let { k, line } = cur;
  const dir = delta > 0 ? 1 : -1;
  for (let left = Math.abs(delta); left > 0; left--) {
    if (dir > 0) {
      if (shows(slots[k]) && line < slots[k].lines.length) line++;
      else if (k + 1 < slots.length) {
        k++;
        line = 0;
      } else break;
    } else if (line > 0) line--;
    else if (k > 0) {
      k--;
      line = shows(slots[k]) ? slots[k].lines.length : 0;
    } else break;
  }
  return { k, line };
}

// stackEnd is the stack's last place: the last file's last shown line, else
// its header.
export function stackEnd(slots) {
  const k = Math.max(slots.length - 1, 0);
  return { k, line: shows(slots[k]) ? slots[k].lines.length : 0 };
}

// nextHeader is N (dir 1) / P (-1): the next file's header; going back from
// inside a file stops on its own header first; null at the ends.
export function nextHeader(slots, cur, dir) {
  if (dir > 0) return cur.k + 1 < slots.length ? { k: cur.k + 1, line: 0 } : null;
  if (cur.line > 0) return { k: cur.k, line: 0 };
  return cur.k > 0 ? { k: cur.k - 1, line: 0 } : null;
}

// stackBands is every band of the shown files in reading order, each with
// its file: bandsOf(path, nLines) is the viewer's anchorBands over the
// overview's links ({start, end, i}, sorted).
export function stackBands(slots, bandsOf) {
  const out = [];
  slots.forEach((s, k) => {
    if (!shows(s)) return;
    for (const b of bandsOf(s.path, s.lines.length)) out.push({ k, start: b.start, end: b.end, i: b.i });
  });
  return out;
}

// stepStackBand is n (dir 1) / p (-1) across the stack: from band cur when
// the cursor (at) is in it, else from the cursor; wraps (one band never
// "wraps"). i is -1 with no bands.
export function stepStackBand(bands, cur, at, dir) {
  const n = bands.length;
  if (!n) return { i: -1, wrapped: false };
  const inIt = (b) => b.k === at.k && b.start <= at.line && at.line <= b.end;
  if (cur >= 0 && cur < n && inIt(bands[cur])) {
    const next = cur + dir;
    return { i: ((next % n) + n) % n, wrapped: (next < 0 || next >= n) && n > 1 };
  }
  if (dir > 0) {
    const k = bands.findIndex((b) => b.k > at.k || (b.k === at.k && b.start > at.line));
    return k >= 0 ? { i: k, wrapped: false } : { i: 0, wrapped: n > 1 };
  }
  for (let k = n - 1; k >= 0; k--) if (bands[k].k < at.k || (bands[k].k === at.k && bands[k].start < at.line)) return { i: k, wrapped: false };
  return { i: n - 1, wrapped: n > 1 };
}

// restack is the stack after its overview's links changed: a file still
// listed keeps its slot (lines, fold, notes), a new one starts unread, a
// gone one leaves.
export function restack(old, files) {
  const by = new Map(old.map((s) => [s.path, s]));
  return newSlots(files, -1).map((s) => by.get(s.path) || s);
}

// placeholderRows sizes an unread file's placeholder: its line count when
// known (a file read before), else three rows; capped.
export function placeholderRows(s) {
  return s.n > 0 ? Math.min(s.n, 2000) : 3;
}

// stackTitle is the right column's title line in the stack.
export function stackTitle(label, k, n) {
  return "Files: " + label + (n ? " · file " + (k + 1) + "/" + n : " · no files");
}
