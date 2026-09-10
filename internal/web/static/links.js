// links.js — gg:// links: the JS twin of internal/model's producer half.
// The page never PARSES a link (the CLI does that); it only builds one for
// whatever the user right-clicked, so a human can paste it into a chat.

import { state } from "./core.js";
import { copyText } from "./layers.js";
import { registerRows } from "./menus.js";

// --- link producer (pure; guarded against Go) ---

// A path holding one of the grammar's separators cannot be expressed; the
// producers refuse rather than emit something that reparses as another place
// (internal/model.LinkPathOK).
function linkPathOK(p) {
  return !/[@:#]/.test(p);
}

// repoSegment renders "gg://" + the repo half: the remote repository name, or
// the local form "gg://" + the absolute worktree path (which already starts
// with "/" on POSIX and needs the separator added for a Windows drive) —
// internal/model.Link.String()'s Repo half.
function repoSegment(repo, worktree) {
  const name = repo && repo.link_repo;
  if (name) return "gg://" + name;
  const abs = (worktree || "").replace(/\\/g, "/").replace(/\/+$/, "");
  if (!abs) return "";
  return abs.startsWith("/") ? "gg://" + abs : "gg:///" + abs;
}

// linkFor builds the address for one place. ctx is a diffCtx-shaped
// {path, rev, state}; side is "new"/"old" and no a 1-based line (both
// optional). Returns "" when the place has no expressible link — no usable
// repo identity, a path holding a grammar separator, a commit target whose
// rev is not a full sha (ruling P9: >= 40 hex, never a hard === 40 — a
// sha256 repo's commits are 64 hex characters), or a line with no path.
function linkFor(repo, worktree, ctx, side, no) {
  const head = repoSegment(repo, worktree);
  if (!head) return "";
  const path = (ctx && ctx.path) || "";
  if (path && !linkPathOK(path)) return "";
  let s = head + (path ? "/" + path : "");
  const st = (ctx && ctx.state) || "unstaged";
  if (st === "staged") {
    s += "@staged";
  } else if (st === "commit") {
    const rev = (ctx && ctx.rev) || "";
    if (rev.length < 40) return "";
    s += "@" + rev;
  }
  // untracked has no target of its own: the plain working-tree form is the
  // pair the resolver reads for it anyway (index → file).
  if (no > 0) {
    if (!path) return "";
    s += ":" + (side === "old" ? "old:" : "") + no;
  }
  return s;
}

// --- end link producer ---

// copyLinkRow is the shared row: an `act` field, never a `run` one (that
// belongs to the command palette's own dispatcher) — showCtxMenu's click
// handler (layers.js) calls .act() with no guard, so a palette-shaped row
// would throw.
function copyLinkRow(link, label) {
  return { label: label || "copy gg link", act: () => copyText(link, "gg link") };
}

registerRows("file", (ctx) => {
  const st =
    ctx.section === "commit"
      ? "commit"
      : ctx.section === "staged"
        ? "staged"
        : ctx.section === "untracked"
          ? "untracked"
          : "unstaged"; // "changes", "conflicts", anything else: the working file
  const link = linkFor(state.repo, state.worktree, { path: ctx.path, rev: ctx.sha, state: st });
  return link ? [copyLinkRow(link)] : [];
});

registerRows("commit", (c) => {
  const link = linkFor(state.repo, state.worktree, { path: "", rev: c.hash, state: "commit" });
  return link ? [copyLinkRow(link)] : [];
});

export { linkFor };
