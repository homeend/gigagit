// links.js — gg:// links: the JS twin of internal/model's producer half.
// The page never PARSES a link (the CLI does that); it only builds one for
// whatever the user right-clicked, so a human can paste it into a chat.

import { state } from "./core.js";
import { copyText } from "./layers.js";
import { registerRows } from "./menus.js";

// --- link producer (pure; guarded against Go) ---

// A path holding one of the grammar's separators cannot be expressed; the
// producers refuse rather than emit something that reparses as another place
// (internal/model.LinkPathOK). '?' is one of them: it opens the ?<kind>=<id>
// hint, and a path carrying one is legal on Linux/macOS, so without it a
// browser-copied link would not paste back into the CLI.
function linkPathOK(p) {
  return !/[@:#?]/.test(p);
}

// An absolute CHECKOUT path holding '@', '#' or '?' cannot be expressed
// either: those are the target, hunk and hint separators, so the link would
// not reparse. A ':' is fine — a leading drive prefix is skipped, and only a
// NUMBER after the last ':' is read as a line (internal/model.LinkAbsOK).
function linkAbsOK(abs) {
  let s = abs;
  if (/^[A-Za-z]:/.test(s)) s = s.slice(2);
  else if (/^\/[A-Za-z]:/.test(s)) s = s.slice(3);
  return !/[@#?]/.test(s);
}

// repoSegment renders "gg://" + the repo half: the remote repository name, or
// the local form "gg://" + the absolute worktree path (which already starts
// with "/" on POSIX and needs the separator added for a Windows drive) —
// internal/model.Link.String()'s Repo half. "" when the worktree path itself
// cannot be expressed.
function repoSegment(repo, worktree) {
  const name = repo && repo.link_repo;
  if (name) return "gg://" + name;
  const abs = (worktree || "").replace(/\\/g, "/").replace(/\/+$/, "");
  if (!abs || !linkAbsOK(abs)) return "";
  return abs.startsWith("/") ? "gg://" + abs : "gg:///" + abs;
}

// A branch name rides a preview link only when it holds none of the grammar's
// separators ('@', ':', '#', '?'), no whitespace and no ".." — the JS twin of
// internal/model.LinkRefOK; the producer refuses rather than print something
// ParseLink would reject or reparse as a different place.
function linkRefOK(s) {
  // TWO dots, not three: git itself forbids ".." anywhere in a refname, and
  // leaving it legal would let "main..feat" parse as a change-set rather than
  // as the literal name it was meant to be.
  // " \t" literally, not \s: Go's rule stops there, and a name the TUI
  // emits a link for must get one here too.
  return !!s && !s.includes("..") && !/[@:#? \t]/.test(s);
}

// linkFor builds the address for one place. ctx is a diffCtx-shaped
// {path, rev, state, compare, preview}; side is "new"/"old" and no a 1-based
// line (both optional). Returns "" when the place has no expressible link —
// no usable repo identity, a path holding a grammar separator, a commit
// target whose rev is not a full sha (ruling P9: >= 40 hex, never a hard ===
// 40 — a sha256 repo's commits are 64 hex characters), a line with no path,
// or ctx.compare set without a preview. That last refusal is a PRODUCER gap,
// not a grammar one: the grammar now has `@<a>..<b>` for exactly this pair
// (internal/model.LinkPair), so a compare view is addressable — emitting one
// from the browser is deferred UI scope. What must not happen meanwhile is
// falling back to `path@bHash`, which reads as bHash^ → bHash, not the
// aHash → bHash pair actually on screen; the TUI's contextLinkText carries
// the same note.
//
// ctx.preview = {source, target} names an open merge preview: the ONE compare
// that has an address of its own, git's three-dot pair
// (`@<target>...<source>`, internal/model.LinkPreview). The pair is the whole
// target — ctx.rev/ctx.state are not consulted — and a preview has no old
// side (ParseLink refuses `:old:` for it), so an old-side line degrades to the
// file form rather than misdescribing the place. Both names must pass
// linkRefOK or the place is inexpressible.
function linkFor(repo, worktree, ctx, side, no) {
  const preview = (ctx && ctx.preview) || null;
  if (ctx && ctx.compare && !preview) return "";
  if (preview && !(linkRefOK(preview.source) && linkRefOK(preview.target))) return "";
  const head = repoSegment(repo, worktree);
  if (!head) return "";
  const path = (ctx && ctx.path) || "";
  if (path && !linkPathOK(path)) return "";
  let s = head + (path ? "/" + path : "");
  if (preview) {
    s += "@" + preview.target + "..." + preview.source;
    if (side === "old") no = 0;
  } else {
    const st = (ctx && ctx.state) || "unstaged";
    if (st === "staged") {
      s += "@staged";
    } else if (st === "commit") {
      const rev = (ctx && ctx.rev) || "";
      // Go's grammar accepts 7..64 hex (internal/model.isShaLink); this is
      // deliberately STRICTER — a producer always knows the full sha, and
      // being strict here can only REFUSE, never mis-emit an abbreviation
      // that grows ambiguous as history does. Left as is on purpose.
      if (rev.length < 40) return "";
      s += "@" + rev;
    }
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
function copyLinkRow(link) {
  return { label: "copy gg link", act: () => copyText(link, "gg link") };
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
  // ctx.compare and ctx.preview ride straight through from the call site —
  // this contributor never reads state.filesMode or state.previewOpen itself,
  // so it stays pure over its input.
  const link = linkFor(state.repo, state.worktree, {
    path: ctx.path,
    rev: ctx.sha,
    state: st,
    compare: ctx.compare,
    preview: ctx.preview || null,
  });
  return link ? [copyLinkRow(link)] : [];
});

registerRows("commit", (c) => {
  const link = linkFor(state.repo, state.worktree, { path: "", rev: c.hash, state: "commit" });
  return link ? [copyLinkRow(link)] : [];
});

// A Previews group row copies the pair's own link, `gg://<repo>@<target>...
// <source>` — no path, no line: the address of the preview itself, the form
// an agent hands back after annotating its branch (the TUI's Previews-panel
// "Copy link"). e is the registry entry: its NAMES, never the tips.
registerRows("preview", (e) => {
  const link = linkFor(state.repo, state.worktree, {
    path: "",
    state: "commit",
    compare: true,
    preview: { source: e.source, target: e.target },
  });
  return link ? [copyLinkRow(link)] : [];
});

export { linkFor };
