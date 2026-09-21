// stack.js — the PURE half of the stacked diff view (stackview.js is the DOM
// half): which rows a stack holds, what loads next, and how a working-tree
// refresh keeps the reader's sections. Import-free on purpose, so
// stackjs_test.go runs it under node.

export const STACK_COLLAPSE_OVER = 100; // a bigger stack opens with every file folded to its header
export const STACK_MAX_IN_FLIGHT = 3; // the server answers reads one at a time under the repo gate

// slotKey is a row's identity across a refresh. A partially staged file is
// listed twice (Changes and Staged), so the section is part of it.
export function slotKey(f) {
  return (f.section || "") + "\u0000" + f.path;
}

// stackGroup: a working-tree stack is the staged files OR everything else
// (changes, untracked, conflicts) — git diff --cached vs git diff.
export function stackGroup(f) {
  return f.section === "staged" ? "staged" : "worktree";
}

// stackRows picks a stack's rows from the active list, keeping each row's
// index in that list (the file cursor's index space). group "" = all rows.
export function stackRows(list, group) {
  const out = [];
  list.forEach((f, idx) => {
    if (!group || stackGroup(f) === group) out.push({ f, idx });
  });
  return out;
}

// statusLetter is the header's status: a listed change carries its own; a
// working-tree row's comes from its section.
export function statusLetter(f) {
  switch (f.section) {
    case "staged":
      return f.staged || "M";
    case "changes":
      return f.unstaged || "M";
    case "untracked":
      return "?";
    case "conflicts":
      return "U";
  }
  return f.status || "";
}

export function buildSlots(rows, collapse = rows.length > STACK_COLLAPSE_OVER) {
  return rows.map(({ f, idx }) => ({
    key: slotKey(f),
    idx,
    f,
    path: f.path,
    oldPath: f.old_path || f.orig_path || "",
    status: statusLetter(f),
    counts: null,
    // a conflict is resolved in the hunk picker, never diffed in a stack
    load: f.section === "conflicts" ? "none" : "idle",
    collapsed: collapse,
    diff: null,
    folds: new Set(), // this file's unfolded runs in the changes-only view (diffHTML's `open`)
    error: "",
    again: false, // a refresh landed mid-load: fetch once more when this one settles
  }));
}

// countsFromDiff derives a header's +a −d from the aligned rows the diff
// endpoint returns — the fallback for sources /api/numstat cannot answer.
export function countsFromDiff(d) {
  if (!d) return null;
  if (d.binary) return { binary: true };
  if (d.too_large) return null;
  let add = 0;
  let del = 0;
  for (const r of d.rows || []) {
    if (r.kind === "add") add++;
    else if (r.kind === "del") del++;
    else if (r.kind === "change") {
      add++;
      del++;
    }
  }
  return { add, del };
}

// nextToLoad picks the next slot to fetch: idle, expanded, near the viewport,
// nearest the current file; a tie goes to the one below (reading direction).
// The near set is re-read at every pick, so a slot scrolled away before its
// turn is simply never picked. -1 = nothing to load.
export function nextToLoad(slots, near, anchor) {
  let best = -1;
  let bestD = Infinity;
  for (const i of near) {
    const s = slots[i];
    if (!s || s.collapsed || s.load !== "idle") continue;
    const d = Math.abs(i - anchor);
    if (d < bestD || (d === bestD && i > best)) {
      bestD = d;
      best = i;
    }
  }
  return best;
}

// reconcileSlots re-lists a working-tree stack after a status re-read. Old
// slot OBJECTS are reused by key, so a load still in flight lands on the
// live slot; the reader's folds survive. A kept slot re-fetches (a refresh
// may have changed its bytes) but keeps painting its old diff meanwhile; a
// status or rename change drops the stale diff and counts outright.
export function reconcileSlots(old, rows) {
  const byKey = new Map(old.map((s) => [s.key, s]));
  const fresh = buildSlots(rows, rows.length > STACK_COLLAPSE_OVER);
  return fresh.map((n) => {
    const o = byKey.get(n.key);
    if (!o) return n;
    const changed = o.status !== n.status || o.oldPath !== n.oldPath;
    o.idx = n.idx;
    o.f = n.f;
    o.status = n.status;
    o.oldPath = n.oldPath;
    if (changed) {
      o.diff = null;
      o.counts = null;
    }
    if (n.load === "none") o.load = "none";
    else if (o.load === "loading") o.again = true;
    else o.load = "idle";
    o.error = "";
    return o;
  });
}

// estimateHeight sizes an unloaded file's placeholder so the scrollbar does
// not lurch as sections load: its changed lines plus context when counted.
export function estimateHeight(slot, rowPx) {
  const c = slot.counts;
  if (!c || c.binary) return 3 * rowPx;
  return Math.min(4000, Math.max(3, c.add + c.del + 6) * rowPx);
}
