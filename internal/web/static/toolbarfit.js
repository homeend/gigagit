// toolbarfit.js — the pure half of the top bar's custom buttons (topbar.js):
// which configured names mean a command, and how many buttons fit. No imports
// and no DOM, so Go can run it under node.

// --- toolbar fit (pure; guarded against Go) ---
const TOOLBAR_MAX = 10;
const TOOLBAR_FIXED = ["pull", "push", "refresh"]; // always on the bar, never a custom button

// resolveToolbar maps configured names onto the command catalog, in order: the
// first of a duplicate, the first ten. A row is "ok", "unavailable" (a command
// that does not apply right now) or "unknown" (no such command, or a fixed one).
function resolveToolbar(labels, catalog) {
  const seen = new Set();
  const out = [];
  for (const label of labels || []) {
    if (seen.has(label) || out.length === TOOLBAR_MAX) continue;
    seen.add(label);
    const row = TOOLBAR_FIXED.includes(label) ? null : (catalog || []).find((r) => r.label === label);
    if (!row) out.push({ label, status: "unknown", row: null });
    else out.push({ label, status: row.available ? "ok" : "unavailable", row });
  }
  return out;
}

// fitToolbar says how many leading buttons stay on the row (px throughout;
// each width includes its gap). `fixed` is everything else on the row, `sep`
// the separator, `more` the ⋯ button, `path` the worktree path at full width.
// Buttons leave from the END, order kept: a wide button goes and takes every
// later one with it. The path is cut only once no button is left to move, and
// with no buttons at all the row is none of this function's business.
function fitToolbar({ room, fixed, sep, more, path, buttons }) {
  if (!buttons.length) return { shown: 0, cutPath: false };
  const base = fixed + path + sep;
  const total = buttons.reduce((sum, w) => sum + w, 0);
  if (base + total <= room) return { shown: buttons.length, cutPath: false };
  let used = base + more;
  let shown = 0;
  while (shown < buttons.length && used + buttons[shown] <= room) used += buttons[shown++];
  return { shown, cutPath: base + more > room };
}
// --- end toolbar fit ---

export { TOOLBAR_FIXED, TOOLBAR_MAX, fitToolbar, resolveToolbar };
