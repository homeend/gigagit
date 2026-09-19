// notebox.js — the pure half of a review-note box: what its title line says
// and which threads are collapsed. Import-free on purpose, so
// noteboxjs_test.go can run it under node against the wire fields the server
// really sends.

// noteAge is a compact age for a title line ("3d"); "" for an unknown time.
export function noteAge(created, nowMs) {
  const t = Date.parse(created || "");
  if (!t) return "";
  const s = Math.max(0, Math.floor((nowMs - t) / 1000));
  if (s < 60) return "now";
  if (s < 3600) return Math.floor(s / 60) + "m";
  if (s < 86400) return Math.floor(s / 3600) + "h";
  if (s < 86400 * 365) return Math.floor(s / 86400) + "d";
  return Math.floor(s / (86400 * 365)) + "y";
}

// noteTitle is the text in a box's top border. A forge review thread
// (read_only) names itself "review" and carries its age and the forge's
// resolved flag; a stored note keeps "note" / "agent note" and the stale word
// — "outdated" inside a merge preview, where a moved line is the expected
// case.
export function noteTitle(n, path, preview, nowMs) {
  const side = n.side === "old" ? "L" : "R";
  if (n.read_only) {
    const at = n.file_level ? path + " (file)" : path + " " + side + n.line;
    return ["review", n.author, noteAge(n.created, nowMs), at, n.resolved ? "resolved" : ""].filter(Boolean).join(" · ");
  }
  const stale = n.status === "stale" || n.status === "outdated";
  const word = stale ? (preview ? " (outdated)" : " (stale)") : "";
  return (n.source === "agent" ? "agent note" : "note") + (n.author ? " · " + n.author : "") + " · " + path + " " + side + n.line + word;
}

// seedCollapsed is the collapse set a freshly opened diff starts with: the
// threads the forge marked resolved. Roots only — a reply has no box.
export function seedCollapsed(notes) {
  return new Set((notes || []).filter((n) => n.resolved).map((n) => n.id));
}

// toggleCollapsed flips one root and reports whether it is now collapsed.
export function toggleCollapsed(set, id) {
  if (set.has(id)) {
    set.delete(id);
    return false;
  }
  set.add(id);
  return true;
}

// setAllCollapsed collapses (on) or expands every root of THIS diff, leaving
// ids that belong to another file alone.
export function setAllCollapsed(set, notes, on) {
  for (const n of notes || []) {
    if (on) set.add(n.id);
    else set.delete(n.id);
  }
  return set;
}
