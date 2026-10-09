// prsendrows.js — which GitHub rows a note's right-click menu offers in a
// PR's own diff (the TUI's forgeNoteRows, row for row). Pure and
// import-free: prsendjs_test.go runs it under node.
//
//   - a local root (a note, an AI review's remark) not on GitHub and not in
//     flight: Send as GitHub comment (Retry … after a failure) and its group's review;
//   - a GitHub thread: Reply & send…, Resolve / Reopen on GitHub, and Send
//     draft reply / Send N draft replies when local drafts hang under it.
export function sendRows(n, pr) {
  if (!pr || !n) return [];
  const rows = [];
  if (n.source === "forge" && !n.parent_id) {
    rows.push({ id: "reply-send", label: "Reply & send…" });
    rows.push({ id: "resolve", label: n.resolved ? "Reopen on GitHub" : "Resolve on GitHub" });
    const drafts = (n.replies || []).filter((r) => r.source !== "forge").length;
    if (drafts) rows.push({ id: "send-drafts", label: drafts === 1 ? "Send draft reply" : "Send " + drafts + " draft replies" });
    return rows;
  }
  if (n.source === "forge" || n.parent_id || n.sync === "sending" || n.sync === "github") return rows;
  rows.push({ id: "send", label: n.sync === "failed" ? "Retry sending as GitHub comment" : "Send as GitHub comment" });
  rows.push({ id: "send-review", label: n.group && n.group !== "mine" ? "Send this AI review…" : "Send my draft review…" });
  return rows;
}
