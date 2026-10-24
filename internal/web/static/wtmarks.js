// wtmarks.js — the TUI's worktree claim / reserve marks (worktree_marks.go)
// over a /api/worktrees row: the mark column, the bar text (the row's
// tooltip here) and the menu rows. Pure: no imports, no DOM.

// sinceLabel is the TUI's c.Since.Local().Format("2006-01-02 15:04").
function sinceLabel(iso) {
  const d = new Date(iso);
  if (!iso || isNaN(d)) return "";
  const p = (n) => String(n).padStart(2, "0");
  return d.getFullYear() + "-" + p(d.getMonth() + 1) + "-" + p(d.getDate()) + " " + p(d.getHours()) + ":" + p(d.getMinutes());
}

// markPrefix is the row's mark column: ⚑ <agent> for a live claim, ⊘ for a
// reserve, both when both hold.
function markPrefix(w) {
  let s = "";
  if (w && w.claim) s += "⚑ " + (w.claim.agent || "agent") + "  ";
  if (w && w.reserved) s += "⊘  ";
  return s;
}

// claimHint is the TUI bottom bar's text for the row: the claim (since
// when, and why), else the reserve.
function claimHint(w) {
  if (!w) return "";
  if (!w.claim) return w.reserved ? "⊘ reserved: agents never take this worktree" : "";
  const c = w.claim;
  let h = "⚑ claimed by " + (c.agent || "agent") + " since " + sinceLabel(c.since);
  if (c.note) h += " · " + c.note;
  return h;
}

// markRows are the worktree menu's release-claim and reserve rows, in the
// TUI's order and words.
function markRows(w) {
  const rows = [];
  if (w.claim) {
    const agent = w.claim.agent || "agent";
    rows.push({ id: "release", label: "Release claim (" + agent + ")", agent });
  }
  rows.push(w.reserved ? { id: "unreserve", label: "Unreserve" } : { id: "reserve", label: "Reserve (no agents)" });
  return rows;
}

export { claimHint, markPrefix, markRows };
