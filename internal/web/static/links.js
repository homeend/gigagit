// links.js — gg:// links: the JS twin of internal/model's producer half.
// The page never PARSES a link (the CLI does that); it only builds one for
// whatever the user right-clicked, so a human can paste it into a chat.

import { state } from "./core.js";
import { copyText } from "./layers.js";
import { opLine } from "./ops.js";
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

// The hint's closed set and its id rule — the JS twins of
// internal/model.LinkHintKindOK and LinkHintIDOK. A hint id may not carry a
// grammar separator, a '/' or whitespace, or the link would not reparse: the
// producer refuses instead of emitting something ParseLink rejects.
function linkHintKindOK(kind) {
  return kind === "bookmark" || kind === "shelf" || kind === "stash" || kind === "preview" || kind === "view";
}

function linkHintIDOK(id) {
  return !!id && !/[@:#?/ \t]/.test(id);
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
// ctx.hint = {kind, id} appends the `?<kind>=<id>` landing hint (spec §3.3):
// WHICH surface the link was copied from. It never changes where the link
// lands — only which row a consumer reveals there — and it goes last,
// because '?' opens the final segment of the grammar. An unknown kind or an
// id that cannot round-trip refuses the whole link rather than dropping the
// hint silently.
function linkFor(repo, worktree, ctx, side, no) {
  let preview = (ctx && ctx.preview) || null;
  if (ctx && ctx.compare && !preview) return "";
  // A COMMIT PAIR rides the same slot with no names at all (files.js hands it
  // over as preview.pair): it is asked for FIRST, because everything below
  // reads source/target — Go's "ask IsPair() before the names". Two full ids
  // or nothing: a producer always knows them, and an abbreviation would grow
  // ambiguous as history does.
  const pair = (preview && preview.pair) || null;
  if (pair) {
    if ((pair.a || "").length < 40 || (pair.b || "").length < 40) return "";
    preview = null;
  }
  // A pull request's diff names its sides for DISPLAY (the head may live in a
  // fork): a link built from them would address some other pair. The server
  // hands out the pair that IS addressable — gg's private refs/gg/pr/<n>
  // against the base — and the link is built from that, or refused.
  if (preview && preview.pr) {
    if (!preview.linkSource || !preview.linkTarget) return "";
    preview = { source: preview.linkSource, target: preview.linkTarget };
  }
  const hint = (ctx && ctx.hint) || null;
  if (hint && !(linkHintKindOK(hint.kind) && linkHintIDOK(hint.id))) return "";
  // A content link (?view=content) names the file ON DISK: a working-tree
  // path, no target, no old side (internal/model ParseLink refuses the rest).
  if (hint && hint.kind === "view") {
    const st0 = (ctx && ctx.state) || "unstaged";
    if (hint.id !== "content" || !(ctx && ctx.path) || preview || pair ||
        (st0 !== "unstaged" && st0 !== "untracked") || (side === "old" && no > 0)) return "";
  }
  if (preview && !(linkRefOK(preview.source) && linkRefOK(preview.target))) return "";
  const head = repoSegment(repo, worktree);
  if (!head) return "";
  const path = (ctx && ctx.path) || "";
  if (path && !linkPathOK(path)) return "";
  let s = head + (path ? "/" + path : "");
  if (pair) {
    // `@<a>..<b>` (internal/model.LinkPair). Like a preview, a pair link has
    // no old side: a line there degrades to the file form.
    s += "@" + pair.a + ".." + pair.b;
    if (side === "old") no = 0;
  } else if (preview) {
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
  // Last, always: '?' opens the grammar's final segment, so anything after it
  // would be read as part of the hint id.
  if (hint) s += "?" + hint.kind + "=" + hint.id;
  return s;
}

// descMax caps the free-text portion of a Desc — a commit subject or a
// bookmark label is otherwise unbounded — so one row of `gg links` stays one
// line. The Go twin is internal/domain.DescMax; TestLinkDescJSMatchesGo pins the
// pair, including the rune-safe cut (a naive slice would split a multi-byte
// character; JS strings are UTF-16, so [...s] is the spread that matches Go's
// []rune).
const descMax = 60;

function truncateDesc(s) {
  const t = (s || "").trim();
  const r = [...t];
  return r.length <= descMax ? t : r.slice(0, descMax).join("");
}

// linkDesc is the human label stored beside a copied link (ruling R7:
// captured at creation, never derived at read time — the context that
// describes it, which row the user was on, is gone by the time anything
// lists it). The forms are spec §4.3's table and MUST match
// internal/cli.linkDesc exactly: the CLI and the web write into the same
// per-repo ring, so a second vocabulary for it would show the user two
// spellings of the same thing.
function linkDesc(kind, id, subject) {
  switch (kind) {
    case "commit":
      return "commit: " + id + " " + truncateDesc(subject);
    case "stash":
      return "stash: " + truncateDesc(subject);
    default:
      return kind + ": " + truncateDesc(id);
  }
}

// --- end link producer ---

// copyLink copies link to the clipboard and then reports it to the
// server-side history (Task 9). Two things are deliberate:
//
//   - The POST is fire-and-forget with a swallowed rejection. A history that
//     cannot be recorded must never make the copy the user asked for look
//     like it failed — the same best-effort posture domain.RecordLink takes
//     on the write side and `gg link` takes on the CLI side.
//   - The history is SERVED, never kept in the browser. `gg web` binds a
//     random port every run and localStorage is per-origin, so a ring kept
//     client-side would vanish on the next start. That is the whole reason
//     /api/linkhist exists.
//
// EVERY "copy gg link" action goes through here. There were five of them and
// only three came through copyLinkRow — files.js's "to this line" and "to
// this note" rows called copyText directly, so recording only in copyLinkRow
// would have left two shipped copy actions silently unrecorded.
function copyLink(link, desc) {
  copyText(link, "gg link");
  fetch("/api/linkhist", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ link, desc: desc || "" }),
  }).catch(() => {});
}

// copyLinkRow is the shared row: an `act` field, never a `run` one (that
// belongs to the command palette's own dispatcher) — showCtxMenu's click
// handler (layers.js) calls .act() with no guard, so a palette-shaped row
// would throw.
function copyLinkRow(link, desc) {
  return { label: "copy gg link", act: () => copyLink(link, desc) };
}

// copyFileLink copies a content link only when the file is on disk in this
// worktree; otherwise the op line says why nothing was copied.
function copyFileLink(path, link) {
  fetch("/api/worktree-present?path=" + encodeURIComponent(path))
    .then((r) => (r.ok ? r.json() : { present: false }))
    .then((j) => {
      if (j.present) copyLink(link, linkDesc("file", path, ""));
      else opLine(path + " is not in the working tree", true);
    })
    .catch(() => opLine("copy failed (server unreachable)", true));
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
  // "copy file link": the file's CONTENT link — no commit, the file as it is
  // on disk — copied only after the server says the file is there.
  const flink = linkFor(state.repo, state.worktree, {
    path: ctx.path,
    state: "unstaged",
    hint: { kind: "view", id: "content" },
  });
  const rows = link ? [copyLinkRow(link, linkDesc("file", ctx.path, ""))] : [];
  if (flink) rows.push({ label: "copy file link", act: () => copyFileLink(ctx.path, flink) });
  return rows;
});

registerRows("commit", (c) => {
  const link = linkFor(state.repo, state.worktree, { path: "", rev: c.hash, state: "commit" });
  // The commit form carries the SHORT sha and the subject, matching the CLI's
  // `commit: <short> <subject>`; c.hash is full on the wire.
  return link ? [copyLinkRow(link, linkDesc("commit", (c.hash || "").slice(0, 8), c.subject || ""))] : [];
});

// A Previews group row copies the pair's own link, `gg://<repo>@<target>...
// <source>` — no path, no line: the address of the preview itself, the form
// an agent hands back after annotating its branch (the TUI's Previews-panel
// "Copy link"). e is the registry entry: its NAMES, never the tips.
//
// A SAVED row says so: `?preview=<id>` is the landing that reveals this row
// again (the id is a lookup key, never part of the address). An id the
// grammar cannot carry degrades to the bare link rather than losing the row,
// and the description is the entry's label — what domain.DescribeLink reads
// the hinted link as.
//
// previewRowLink is the ONE builder of that link: the row's "copy gg link" and
// a drag & drop compare (previews.js) must hand out the very same text. ""
// when the grammar cannot carry the row's ref names.
function previewRowLink(e) {
  const ctx = { path: "", state: "commit", compare: true, preview: { source: e.source, target: e.target } };
  return (
    linkFor(state.repo, state.worktree, { ...ctx, hint: { kind: "preview", id: e.id } }) ||
    linkFor(state.repo, state.worktree, ctx)
  );
}

registerRows("preview", (e) => {
  const link = previewRowLink(e);
  return link ? [copyLinkRow(link, linkDesc("preview", e.label || e.target + "..." + e.source, ""))] : [];
});

// A bookmark or shelf row copies the entry's ADDRESS plus a
// `?<kind>=<id>` hint naming the surface it came from (spec §3.3). This is
// the web PRODUCER of a hinted link: the consumer already exists (Task 6's
// revealHintEntry lands such a link back on this very row), but until now
// only `gg link --bookmark` on the CLI could make one, so the reveal was
// unreachable from the browser that owns the row.
//
// The address is the entry's own: a commit entry's `@<sha>`, a staged
// entry's `@staged`, or the plain working-tree form. A SHELVED FILE has no
// git address at all — its bytes were never committed — so its link is the
// hint-only form `gg://<repo>?shelf=<id>`, which is why entryHintLink asks
// linkFor for a path-less, target-less link and lets the hint carry it.
function entryHintLink(kind, e) {
  const st = e.is_commit || e.kind === "commit" ? "commit" : e.state || "unstaged";
  // A shelved FILE's bytes are frozen in gg's own store, not in git: the
  // entry's recorded path/state describe where it CAME from, and the link
  // that can actually reproduce it is the hint alone.
  const hintOnly = kind === "shelf" && e.kind !== "commit";
  return linkFor(state.repo, state.worktree, {
    path: hintOnly ? "" : e.path || "",
    rev: e.commit || "",
    state: hintOnly ? "unstaged" : st,
    hint: { kind, id: e.id },
  });
}

// entryDesc labels the row by the NAME the user gave the entry, falling back
// to the store's own display string and then the id — the same precedence
// sidebar.js's entryLabel uses, so `gg links` and the sidebar agree about
// what an entry is called.
function entryDesc(kind, e) {
  return linkDesc(kind, e.label || e.display || e.id, "");
}

registerRows("bookmark", (e) => {
  const link = entryHintLink("bookmark", e);
  return link ? [copyLinkRow(link, entryDesc("bookmark", e))] : [];
});

registerRows("shelf", (e) => {
  const link = entryHintLink("shelf", e);
  return link ? [copyLinkRow(link, entryDesc("shelf", e))] : [];
});

export { copyLink, linkDesc, linkFor, previewRowLink };
