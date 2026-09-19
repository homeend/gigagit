// prsrow.js — the pure half of a pull-request row: what its status cell,
// state word and tooltip say. Import-free on purpose, so prsrowjs_test.go can
// run it under node against the values the server really sends.

// review_state arrives LOWER-case (model.PullRequest); anything else paints
// no mark rather than a guessed one.
const MARKS = { approved: "✓", changes_requested: "✗", review_required: "●" };

// ago is a compact relative age ("3h ago"); "" for an unknown time.
function ago(iso, nowMs) {
  const t = Date.parse(iso || "");
  if (!t) return "";
  const s = Math.max(0, Math.floor((nowMs - t) / 1000));
  if (s < 60) return "just now";
  if (s < 3600) return Math.floor(s / 60) + "m ago";
  if (s < 86400) return Math.floor(s / 3600) + "h ago";
  if (s < 86400 * 30) return Math.floor(s / 86400) + "d ago";
  if (s < 86400 * 365) return Math.floor(s / (86400 * 30)) + "mo ago";
  return Math.floor(s / (86400 * 365)) + "y ago";
}

// prRowParts: mark = the review verdict, word = why the row is not a plain
// open PR (draft / closed / merged / unavailable), dim = no longer open.
export function prRowParts(pr, nowMs) {
  const open = pr.state === "open";
  const word = open ? (pr.draft ? "draft" : "") : pr.state || "";
  const pair = pr.source || pr.target ? (pr.source || "?") + " → " + (pr.target || "?") : "";
  const tip = [pr.author, pair, ago(pr.updated, nowMs)].filter(Boolean).join(" · ");
  return {
    mark: MARKS[pr.review_state] || "",
    word,
    title: pr.title || (pr.state === "unavailable" ? "(no longer on the forge)" : ""),
    tip,
    dim: !open,
  };
}
