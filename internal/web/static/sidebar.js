// sidebar.js — part of gg's web client. Split from the original app.js;
// see app.js (the entry module) for the load order.
import { $, SECTIONS, charWidth, defaultWorktreePath, elidePath, esc, getJSON, lsGet, lsSet, postJSON, state } from "./core.js";
import { saveUI } from "./uistate.js";
import { closePrompt, copyText, openPrompt, showCtxMenu } from "./layers.js";
import { doForcePush, doPull, doPullBranch, doPush, doPushBranch, doReroot, opLine, openCreateBranchPrompt, showLocalConfirm, startOp, startSwitch } from "./ops.js";
import { openVersions } from "./versions.js";
import { openRebaseEditor } from "./rebase.js";
import { startReview } from "./review.js";
import { gotoBranchTip, openCommitByHash, openStashDetail, setSolo } from "./commits.js";
import { openCompare } from "./files.js";
import { openFileHistory } from "./filehist.js";
import { extraRows } from "./menus.js";
import { nextSortMode, setSortMode, sortChipHTML, sortMode, sortedBy } from "./sortlist.js";

// The remotes and tags payloads are CAPPED at 100 rows server-side, so their
// order has to be decided there — sorting the truncated window would show the
// arbitrary first hundred, sorted, rather than the hundred you asked for.
function sortParam(list) {
  return "sort=" + encodeURIComponent(sortMode(list));
}

async function fetchBranches() {
  const [b, w, tg, st, rl, rm, bm, sh] = await Promise.all([
    getJSON("/api/branches"),
    getJSON("/api/worktrees").catch(() => ({ worktrees: [] })),
    getJSON("/api/tags?" + sortParam("tags")).catch(() => ({ tags: [], truncated: false })),
    getJSON("/api/stashes").catch(() => ({ stashes: [] })),
    getJSON("/api/reflog?limit=" + reflogWindow).catch(() => ({ entries: [], truncated: false })),
    getJSON("/api/remotes?" + sortParam("remotes")).catch(() => ({ remotes: [], truncated: false })),
    getJSON("/api/bookmarks").catch(() => ({ entries: [] })),
    getJSON("/api/shelf").catch(() => ({ entries: [] })),
  ]);
  state.branches = b.branches || [];
  state.worktrees = w.worktrees || [];
  state.tags = tg.tags || [];
  state.tagsTruncated = !!tg.truncated;
  state.stashes = st.stashes || [];
  state.reflog = rl.entries || [];
  state.reflogTruncated = !!rl.truncated;
  state.remotes = rm.remotes || [];
  state.remotesTruncated = !!rm.truncated;
  state.bookmarks = bm.entries || [];
  state.shelf = sh.entries || [];
  renderBranches();
  renderRemotes();
  renderWorktrees();
  renderTags();
  renderStashes();
  renderReflog();
  renderBookmarks();
  renderShelf();
}


// MARK is the one "this is the current one" glyph, shared by every sidebar
// list. Rows without it still reserve the column (mark("") below) so names
// line up down the whole sidebar, section to section.
const MARK = "✓";

function mark(on) {
  return `<span class="mk">${on ? MARK : ""}</span>`;
}


// listCols is how many monospace columns a sidebar row has to work with:
// the list's own width less the li padding. Used to budget the elided
// worktree path — the same column arithmetic the TUI does.
function listCols(id) {
  const el = $(id);
  const w = el ? el.getBoundingClientRect().width : 0;
  return Math.max(0, Math.floor((w - 20) / charWidth()));
}


function renderBranches() {
  const cols = listCols("branches-list");
  $("branches-list").innerHTML = sortedBy("branches", state.branches, (b) => b.name, (b) => b.time)
    .map((b) => {
      const ab =
        (b.ahead ? "↑" + b.ahead : "") + (b.behind ? (b.ahead ? " " : "") + "↓" + b.behind : "");
      // A branch checked out somewhere shows WHERE, like the TUI's
      // "main (/path/to/worktree)" — middle-elided into whatever the row has
      // left after the marker, the name and the ahead/behind counts. Too
      // narrow to say anything useful (< 4 columns) and it is dropped rather
      // than shown as a bare "…".
      const path = worktreePathForBranch(b.name);
      const budget = cols - 2 - Array.from(b.name).length - (ab ? ab.length + 1 : 0) - 1;
      const wt = path && budget >= 4 ? elidePath(path, budget) : "";
      return (
        `<li class="${b.is_head ? "head" : ""}" draggable="true" data-n="${esc(b.name)}"` +
        (path ? ` title="${esc(path)}"` : "") + `>` +
        `${mark(b.is_head)}${esc(b.name)}` +
        (wt ? `<span class="wpath">${esc(wt)}</span>` : "") +
        `${ab ? `<span class="ab">${ab}</span>` : ""}</li>`
      );
    })
    .join("");
}


function renderRemotes() {
  let html = sortedBy("remotes", state.remotes, (rb) => rb.name, (rb) => rb.time)
    .map((rb) => `<li data-n="${esc(rb.name)}" data-h="${esc(rb.hash)}">${mark(false)}${esc(rb.name)}</li>`)
    .join("");
  if (state.remotesTruncated) html += `<li class="more">… more (capped at 100)</li>`;
  $("remotes-list").innerHTML = html;
}


function renderWorktrees() {
  const cols = listCols("worktrees-list");
  // A worktree's "name" is its branch, falling back to the path for a
  // detached or bare one — the TUI's worktreeList.Name rule.
  $("worktrees-list").innerHTML = sortedBy(
    "worktrees",
    state.worktrees,
    (w) => w.branch || w.path,
    (w) => w.time
  )
    .map((w) => {
      const label = w.bare ? "(bare)" : w.detached ? "(detached)" : w.branch || "(?)";
      const cur = state.worktree && w.path === state.worktree ? " cur" : "";
      // Same path treatment as the branch rows: as much of the real path as
      // the row can hold, middle-elided, rather than a bare directory name.
      const budget = cols - 2 - Array.from(label).length - 1;
      const path = budget >= 4 ? elidePath(w.path, budget) : "";
      return (
        `<li class="${cur.trim()}" data-p="${esc(w.path)}" title="${esc(w.path)}">` +
        `${mark(!!cur)}${esc(label)}` +
        (path ? `<span class="wpath">${esc(path)}</span>` : "") + `</li>`
      );
    })
    .join("");
}


function renderTags() {
  let html = sortedBy("tags", state.tags, (t) => t.name)
    .map(
      (t) =>
        `<li data-h="${esc(t.target)}" data-n="${esc(t.name)}">${mark(false)}${esc(t.name)}` +
        (t.subject ? `<span class="tsub">${esc(t.subject)}</span>` : "") +
        `</li>`
    )
    .join("");
  if (state.tagsTruncated) html += `<li class="more">… more (capped at 100)</li>`;
  $("tags-list").innerHTML = html;
}


// compactRel squeezes git's "13 hours ago" into "13h" so the sidebar row's
// width goes to the subject, not the age. Unrecognized forms pass through.
function compactRel(rel) {
  const m = /^(\d+)\s+(second|minute|hour|day|week|month|year)s?\s+ago$/.exec(rel || "");
  if (!m) return rel || "";
  return m[1] + (m[2] === "month" ? "mo" : m[2][0]);
}


// The reflog section opens on one page and grows by another page each time the
// "show more" row is clicked. reflogWindow — not a fixed 100 — is what every
// later fetch asks for, so a background refresh cannot fold an expanded
// section back down to the first page.
const REFLOG_PAGE = 100;
let reflogWindow = REFLOG_PAGE;
let reflogLoading = false;


async function moreReflog() {
  if (reflogLoading || !state.reflogTruncated) return; // one page at a time
  reflogLoading = true;
  const row = $("reflog-list").querySelector("li.more");
  if (row) row.textContent = "… loading";
  const want = reflogWindow + REFLOG_PAGE;
  const rl = await getJSON("/api/reflog?limit=" + want).catch(() => null);
  reflogLoading = false;
  if (!rl) {
    renderReflog(); // put the row back; the section is unchanged
    return;
  }
  reflogWindow = want;
  state.reflog = rl.entries || [];
  state.reflogTruncated = !!rl.truncated;
  renderReflog();
}


function renderReflog() {
  // Column-ish row: @{N} (the recovery selector, shortened for display —
  // data-s keeps the full form the menus use), short sha, subject
  // (ellipsized), compact age pinned right.
  let html = state.reflog
    .map(
      (e) =>
        `<li data-h="${esc(e.hash)}" data-s="${esc(e.selector)}">` +
        mark(false) +
        `<span class="rsel">${esc(e.selector.replace(/^HEAD@/, "@"))}</span>` +
        `<span class="rsha">${esc(e.short || "")}</span>` +
        `<span class="rsub">${esc(e.subject || "")}</span>` +
        (e.rel ? `<span class="rrel" title="${esc(e.rel)}">${esc(compactRel(e.rel))}</span>` : "") +
        `</li>`
    )
    .join("");
  // The last row is the pager, not a notice: clicking it fetches the next page.
  if (state.reflogTruncated) html += `<li class="more">… show ${REFLOG_PAGE} more</li>`;
  $("reflog-list").innerHTML = html;
}


function renderStashes() {
  $("stashes-list").innerHTML = state.stashes
    .map(
      (s) =>
        `<li data-r="${esc(s.ref)}"${s.sha ? ` data-h="${esc(s.sha)}"` : ""}>${mark(false)}${esc(s.ref)}` +
        (s.subject ? `<span class="tsub">${esc(s.subject)}</span>` : "") +
        `</li>`
    )
    .join("");
}


// A branch is checked out in at most one worktree, and both lists are
// already in hand (the sidebar fetches them together), so the join is a
// client-side lookup — same shape as the TUI's worktreeAbsPathForBranch.
// No match (the common case) simply omits the row.
function worktreePathForBranch(name) {
  const w = state.worktrees.find((x) => x.branch === name);
  return w ? w.path : null;
}




// Deliberately NO backdrop-closes handler, unlike the picker overlays. A
// report is a document you read, and the chip that raised it is cleared the
// moment it opens — so a stray click outside the box used to discard the
// only copy in the UI. Double-clicking the ready chip did exactly that: the
// first click opened the report, the second landed on this backdrop.
// Closing stays explicit: the close button, or esc via the layer stack.

// The global create-branch entry (☰ / palette): same op as the branch
// menu's row, but with no start point on the wire — the server reads that as
// HEAD, which is what "new branch" means with nothing selected.
// openCreateBranchPrompt now lives in ops.js — the commits panel opens it too
// (branch from the selected commit), and commits.js cannot import from here
// without closing a cycle (this module already imports commits.js).


// The rows are grouped by what they do to the repo — navigate, move commits
// around, create things, inspect, scope, copy, and finally the two that change
// the branch itself (rename, delete). Separators are pushed unconditionally;
// showCtxMenu drops the ones a missing row leaves stranded.
function showBranchMenu(b, x, y) {
  const items = [{ label: "go to tip", act: () => gotoBranchTip(b) }];
  if (!b.is_head) items.push({ label: "switch to " + b.name, act: () => startSwitch(b.name) });
  if (b.is_head) {
    // The checked-out branch: pulling it rewrites the working tree, so it
    // goes through the confirming current-branch path the header button uses.
    items.push({ label: "pull " + b.name, act: () => doPull() });
  } else {
    items.push({ label: "pull " + b.name + " (stay here)", act: () => doPullBranch(b.name) });
  }
  items.push({ sep: true });
  if (b.is_head) items.push({ label: "push " + b.name, act: () => doPush() });
  else items.push({ label: "push " + b.name, act: () => doPushBranch(b.name) });
  // Not marked danger: the row opens the force-mode modal, where the actual
  // destructive options are the red ones.
  items.push({ label: "force push " + b.name + "…", act: () => doForcePush(b.name) });
  // The DnD pair-menu ops as plain rows against the CURRENT branch — the
  // accessible route to the same startOp calls (drag is the power-user path).
  // Labels spell the direction, and the labelled row is the confirmation (the
  // DnD precedent). Hidden on detached HEAD (no is_head row to pair with).
  const cur = state.branches.find((x) => x.is_head);
  if (!b.is_head && cur) {
    items.push({
      label: "merge " + b.name + " into current (" + cur.name + ")",
      act: () => startOp({ op: "merge", branch: b.name, onto: cur.name }, "merging " + b.name + " into " + cur.name),
    });
    items.push({
      label: "rebase current (" + cur.name + ") onto " + b.name,
      act: () => startOp({ op: "rebase", branch: cur.name, onto: b.name }, "rebasing " + cur.name + " onto " + b.name),
    });
    // ff-only advance of the current branch to this branch's tip: no merge
    // commit, no rewrite; the engine refuses when the tip is not strictly
    // ahead, so the row is safe to offer unconditionally.
    items.push({
      label: "fast-forward current (" + cur.name + ") to " + b.name,
      act: () => startOp({ op: "fast-forward", branch: b.name }, "fast-forwarding " + cur.name + " to " + b.name),
    });
  }
  items.push({ sep: true });
  items.push({
    label: "create branch from here…",
    act: () => openCreateBranchPrompt(b.name),
  });
  // Only offered when the branch has no worktree — git allows exactly one,
  // and the engine would refuse a second (same gate as the copy-path row).
  if (!worktreePathForBranch(b.name)) {
    items.push({
      label: "create worktree for this branch…",
      act: () =>
        openPrompt({
          title: "New worktree for " + b.name + ", at path:",
          value: defaultWorktreePath(b.name),
          onSubmit: (path) => startOp({ op: "create-worktree", branch: b.name, path }, "creating worktree " + path),
        }),
    });
  }
  // Not gated on "does this branch have versions" — that would cost a read
  // on every menu open; the popup shows the empty state instead (the TUI's
  // branchVersionsRow rule).
  items.push({ sep: true });
  items.push({ label: "previous versions…", act: () => openVersions(b.name) });
  items.push({ label: "review " + b.name + " (AI)…", act: () => startReview("branch", b.name) });
  items.push({ sep: true });
  if (state.solo === b.name) {
    items.push({ label: "exit solo (show every branch)", act: () => setSolo("") });
  } else {
    items.push({ label: "solo this branch", act: () => setSolo(b.name) });
  }
  items.push({ sep: true });
  items.push({ label: "copy branch name", act: () => copyText(b.name, "branch name " + b.name) });
  // b.hash is git's abbreviated sha (%(objectname:short)) — the same value
  // the TUI's row copies, and short enough to name in full on the line.
  if (b.hash) items.push({ label: "copy commit id", act: () => copyText(b.hash, "commit id " + b.hash) });
  const wt = worktreePathForBranch(b.name);
  if (wt) {
    items.push({ label: "copy worktree absolute path", act: () => copyText(wt, "absolute path " + wt) });
  }
  // Last group: the two rows that act on the branch itself. Rename sits with
  // delete rather than with the ops above — both change what the branch IS,
  // not where its commits are.
  items.push({ sep: true });
  items.push({
    label: "rename branch…",
    act: () =>
      openPrompt({
        title: "Rename " + b.name + " to:",
        value: b.name,
        onSubmit: (name) => {
          if (name === b.name) return; // no-op, and the engine would refuse it
          startOp({ op: "rename-branch", branch: b.name, name }, "renaming " + b.name);
        },
      }),
  });
  if (!b.is_head) {
    items.push({
      label: "delete " + b.name,
      danger: true,
      act: () => startOp({ op: "delete-branch", branch: b.name }, "deleting " + b.name),
    });
  }
  items.push(...extraRows("branch", b));
  showCtxMenu(items, x, y);
}


$("branches-list").addEventListener("click", (e) => {
  const li = e.target.closest("li");
  if (!li || !li.dataset.n) return;
  const b = state.branches.find((x) => x.name === li.dataset.n);
  if (b) gotoBranchTip(b);
});

$("branches-list").addEventListener("contextmenu", (e) => {
  const li = e.target.closest("li");
  if (!li || !li.dataset.n) return;
  e.preventDefault();
  const b = state.branches.find((x) => x.name === li.dataset.n);
  if (b) showBranchMenu(b, e.clientX, e.clientY);
});


// Drag a branch onto another to merge or rebase. The drop opens the shared
// ctx-menu naming the pair in both directions — the menu row IS the
// confirmation, the same standing the TUI's pair-op popup has.
const branchesList = $("branches-list");


branchesList.addEventListener("dragstart", (e) => {
  const li = e.target.closest("li");
  if (!li || !li.dataset.n) return;
  state.dragBranch = li.dataset.n;
  // Required for a drag to start at all in Firefox; also gives the browser
  // its default drag image.
  e.dataTransfer.setData("text/plain", li.dataset.n);
  e.dataTransfer.effectAllowed = "move";
});


branchesList.addEventListener("dragover", (e) => {
  const li = e.target.closest("li");
  if (!li || !li.dataset.n) return;
  if (!state.dragBranch || li.dataset.n === state.dragBranch) return;
  // preventDefault is what marks this element as a valid drop target.
  e.preventDefault();
  e.dataTransfer.dropEffect = "move";
  li.classList.add("drop-target");
});


branchesList.addEventListener("dragleave", (e) => {
  const li = e.target.closest("li");
  if (li) li.classList.remove("drop-target");
});


branchesList.addEventListener("dragend", () => {
  state.dragBranch = null;
  clearDropTargets();
});


branchesList.addEventListener("drop", (e) => {
  const li = e.target.closest("li");
  const src = state.dragBranch;
  state.dragBranch = null;
  clearDropTargets();
  if (!li || !li.dataset.n || !src) return;
  const dst = li.dataset.n;
  if (dst === src) return;
  e.preventDefault();
  showBranchPairMenu(src, dst, e.clientX, e.clientY);
});


function clearDropTargets() {
  for (const el of $("branches-list").querySelectorAll(".drop-target")) {
    el.classList.remove("drop-target");
  }
}


// showBranchPairMenu offers the two-branch operations on (dragged, dropped-on).
// Directions are spelled out in the labels so the pair never carries implicit
// meaning: merge ends on dst, rebase rewrites and ends on src.
function showBranchPairMenu(src, dst, x, y) {
  showCtxMenu(
    [
      {
        label: "merge " + src + " into " + dst,
        act: () => startOp({ op: "merge", branch: src, onto: dst }, "merging " + src + " into " + dst),
      },
      {
        label: "rebase " + src + " onto " + dst,
        act: () => startOp({ op: "rebase", branch: src, onto: dst }, "rebasing " + src + " onto " + dst),
      },
      {
        // Opens an editor; nothing runs until you start it there, which is
        // why it sits with the ops rather than below the read-only row.
        label: "interactive rebase " + src + " onto " + dst + "…",
        act: () => openRebaseEditor(src, dst),
      },
      // Read-only, so it sits below the ops that rewrite history.
      { label: "compare " + src + " ↔ " + dst, act: () => openCompare(src, dst) },
    ],
    x,
    y
  );
}


// Left-click a remote branch: show its tip commit (a READ, like tags).
$("remotes-list").addEventListener("click", (e) => {
  const li = e.target.closest("li");
  if (!li || !li.dataset.h) return;
  openCommitByHash(li.dataset.h, li.dataset.n);
});


function showRemoteMenu(rb, x, y) {
  const cur = state.branches.find((x) => x.is_head);
  const items = [
    { label: "show commit", act: () => openCommitByHash(rb.hash, rb.name) },
    {
      // Materialize under a local name, staying on the current branch. The
      // engine is fast-forward-safe: a diverged local name is refused —
      // rerun with a different name.
      label: "check out " + rb.name + " as…",
      act: () =>
        openPrompt({
          title: "Check out " + rb.name + " as local branch (stay here):",
          value: rb.branch,
          onSubmit: (name) => startOp({ op: "checkout-remote", ref: rb.name, name }, "checking out " + rb.name + " as " + name),
        }),
    },
    {
      label: "switch to " + rb.name + " as…",
      act: () =>
        openPrompt({
          title: "Check out " + rb.name + " as local branch and switch to it:",
          value: rb.branch,
          onSubmit: (name) => startOp({ op: "checkout-remote", ref: rb.name, name, switch: true }, "switching to " + rb.name + " as " + name),
        }),
    },
  ];
  if (cur) {
    items.push({
      label: "merge " + rb.name + " into current (" + cur.name + ")",
      act: () => startOp({ op: "merge", branch: rb.name, onto: cur.name }, "merging " + rb.name + " into " + cur.name),
    });
    items.push({
      label: "rebase current (" + cur.name + ") onto " + rb.name,
      act: () => startOp({ op: "rebase", branch: cur.name, onto: rb.name }, "rebasing " + cur.name + " onto " + rb.name),
    });
  }
  // Only on the CURRENT branch's own remote counterpart — resetting to
  // another branch's remote would move the wrong branch (the TUI rule; the
  // server enforces it too). The preset hard mode skips every engine guard,
  // so this local confirm is the only one.
  if (cur && rb.branch === cur.name) {
    items.push({ sep: true });
    items.push({
      label: "reset current (" + cur.name + ") to " + rb.name + " tip",
      danger: true,
      act: () =>
        showLocalConfirm(
          "Reset " + cur.name + " to " + rb.name + "? This discards local commits and uncommitted changes.",
          ["reset", "abort"],
          (o) => {
            if (o === "reset") startOp({ op: "reset-remote", ref: rb.name }, "resetting " + cur.name + " to " + rb.name);
          }
        ),
    });
  }
  // Fenced on BOTH sides: the copy rows below must not read as part of the
  // red row above them.
  items.push({ sep: true });
  items.push({ label: "copy name", act: () => copyText(rb.name, "name " + rb.name) });
  if (rb.hash) items.push({ label: "copy commit id", act: () => copyText(rb.hash, "commit id " + rb.hash) });
  items.push({ sep: true });
  items.push({
    // The engine's own confirm parks in the modal before the deletion is
    // pushed (the delete-branch precedent).
    label: "delete " + rb.name + " from remote",
    danger: true,
    act: () => startOp({ op: "delete-remote-branch", ref: rb.name }, "deleting " + rb.name),
  });
  items.push(...extraRows("remote", rb));
  showCtxMenu(items, x, y);
}

$("remotes-list").addEventListener("contextmenu", (e) => {
  const li = e.target.closest("li");
  if (!li || !li.dataset.n) return;
  e.preventDefault();
  const rb = state.remotes.find((x) => x.name === li.dataset.n);
  if (rb) showRemoteMenu(rb, e.clientX, e.clientY);
});


function showWorktreeMenu(w, x, y) {
  const items = [{ label: "copy path", act: () => copyText(w.path) }];
  // A bare or detached worktree has no branch name to copy.
  if (w.branch) {
    items.push({ label: "copy branch name", act: () => copyText(w.branch, "branch name " + w.branch) });
  }
  // Every row except the served worktree can be switched to (the same
  // exemption the remove row uses).
  if (!(state.worktree && w.path === state.worktree)) {
    items.unshift({ label: "switch here", act: () => doReroot(w.path) });
  }
  // The served worktree's row gets no remove (the engine would refuse it
  // anyway); main is engine-guarded too.
  if (!(state.worktree && w.path === state.worktree)) {
    items.push({ sep: true });
    items.push({
      label: "remove worktree",
      danger: true,
      act: () => startOp({ op: "remove-worktree", path: w.path }, "removing " + w.path.split("/").pop()),
    });
  }
  items.push(...extraRows("worktree", w));
  showCtxMenu(items, x, y);
}

$("worktrees-list").addEventListener("contextmenu", (e) => {
  const li = e.target.closest("li");
  if (!li || !li.dataset.p) return;
  e.preventDefault();
  const w = state.worktrees.find((x) => x.path === li.dataset.p);
  if (w) showWorktreeMenu(w, e.clientX, e.clientY);
});

// ONE left click on a sidebar section header folds or unfolds its list - long
// branch/tag lists otherwise force constant scrolling. (It used to take a
// double click, and two of the six lists ignored it entirely.)
//
// COLLAPSED_DEFAULT are the sections a FIRST RUN starts folded: the reference
// lists you consult now and then, so the sidebar opens on what you steer with
// (branches, remotes, worktrees) rather than a screenful of tags. It applies
// only until something is saved - after that, your own layout is what returns.
const COLLAPSED_DEFAULT = ["tags", "stashes", "reflog", "bookmarks", "shelf"];

// Every header carries its state as a chevron - pointing down when open,
// right when folded - so a folded section still reads as something you can
// open. The header owns the sort chip too (rather than the chip living in the
// HTML): this function rewrites the header on every fold, so anything it does
// not draw would be wiped the first time the section is toggled.
function applySection(name, collapsed) {
  $(name + "-list").classList.toggle("collapsed", collapsed);
  $(name + "-header").innerHTML =
    (collapsed ? "\u25b8 " : "\u25be ") + esc(name) + sortChipHTML(name);
}


// isCollapsed reads the fold straight off the list, so redrawing a header
// (after a sort cycle) cannot flip the chevron by accident.
function isCollapsed(name) {
  return $(name + "-list").classList.contains("collapsed");
}


// cycleListSort advances one list to its next order. The remotes and tags
// payloads are capped server-side, so those two REFETCH under the new order
// (see sortParam); the rest are complete in hand and just re-render.
async function cycleListSort(name) {
  setSortMode(name, nextSortMode(name));
  applySection(name, isCollapsed(name)); // redraw the chip's label
  if (name === "remotes") {
    const rm = await getJSON("/api/remotes?" + sortParam("remotes")).catch(() => null);
    if (rm) {
      state.remotes = rm.remotes || [];
      state.remotesTruncated = !!rm.truncated;
    }
    renderRemotes();
    return;
  }
  if (name === "tags") {
    const tg = await getJSON("/api/tags?" + sortParam("tags")).catch(() => null);
    if (tg) {
      state.tags = tg.tags || [];
      state.tagsTruncated = !!tg.truncated;
    }
    renderTags();
    return;
  }
  if (name === "branches") renderBranches();
  if (name === "worktrees") renderWorktrees();
}


function foldedSections() {
  return SECTIONS.filter((n) => $(n + "-list").classList.contains("collapsed"));
}


function toggleSection(name) {
  applySection(name, !isCollapsed(name));
  const folded = foldedSections();
  lsSet("gg.sidebar.collapsed", JSON.stringify(folded)); // same-session cache
  saveUI({ sections: folded }); // the copy that survives a restart
}


// applyStoredSections re-applies a layout that came back from the server. The
// caller passes null on a first run, which leaves COLLAPSED_DEFAULT standing.
function applyStoredSections(names) {
  if (!Array.isArray(names)) return;
  SECTIONS.forEach((n) => applySection(n, names.includes(n)));
}

SECTIONS.forEach((n) => {
  $(n + "-header").addEventListener("click", (e) => {
    // The chip lives INSIDE the header, whose click folds the section — so it
    // claims its own clicks rather than folding what you were re-ordering.
    if (e.target.closest(".sortchip")) {
      cycleListSort(n);
      return;
    }
    toggleSection(n);
  });
});


// Restore persisted sidebar state (b-key visibility + per-section
// collapse). The collapsed class lives on the persistent <ul> containers,
// so a one-time boot restore survives every re-render.
// A MISSING key is a first run and gets COLLAPSED_DEFAULT; a saved EMPTY list
// means "I opened them all", a decision to honour - so the two are
// deliberately not conflated.
(function restoreSidebar() {
  const saved = lsGet("gg.sidebar.collapsed");
  let names = COLLAPSED_DEFAULT;
  if (saved !== null && saved !== undefined && saved !== "") {
    try {
      const parsed = JSON.parse(saved);
      names = Array.isArray(parsed) ? parsed : COLLAPSED_DEFAULT;
    } catch {
      names = COLLAPSED_DEFAULT;
    }
  }
  SECTIONS.forEach((n) => applySection(n, names.includes(n)));
  if (lsGet("gg.sidebar.hidden") === "1") {
    state.sidebar = false;
    $("panes").classList.add("nosb");
  }
})();


$("tags-list").addEventListener("click", (e) => {
  const li = e.target.closest("li");
  if (!li || !li.dataset.h) return;
  openCommitByHash(li.dataset.h, "🏷 " + li.dataset.n);
});


function showTagMenu(tg, x, y) {
  showCtxMenu(
    [
      { label: "show commit", act: () => openCommitByHash(tg.target, "🏷 " + tg.name) },
      // The reflog checkout's two explicit lanes, addressed by tag name so
      // the reflog reads "moving to <tag>". Detached is the inspect-only
      // escape hatch; the branch lane prefills the tag name (TUI parity).
      { label: "check out " + tg.name + " (detached)", act: () => startOp({ op: "checkout-tag", tag: tg.name }, "checking out " + tg.name) },
      {
        label: "check out " + tg.name + " as new branch…",
        act: () =>
          openPrompt({
            title: "New branch at " + tg.name + ", then switch to it:",
            value: tg.name,
            onSubmit: (name) => startOp({ op: "checkout-tag", tag: tg.name, name }, "creating " + name + " at " + tg.name),
          }),
      },
      // A tag is a ref to git log like any other, so the branch solo scope
      // machinery carries it; the top-bar chip is the shared exit.
      state.solo === tg.name
        ? { label: "exit solo (show every branch)", act: () => setSolo("") }
        : { label: "solo this tag", act: () => setSolo(tg.name) },
      { label: "copy name", act: () => copyText(tg.name) },
      // target is git's abbreviated sha (the branch-menu copy-id precedent:
      // short enough to name in full on the notice line).
      { label: "copy commit id", act: () => copyText(tg.target, "commit id " + tg.target) },
      {
        // Force-recreates the tag as annotated at its current target (the
        // server re-reads the target itself). Prefilled with the current
        // subject, the TUI popup's prefill.
        label: "annotate " + tg.name + "…",
        act: () =>
          openPrompt({
            title: "Annotate " + tg.name + " — message:",
            value: tg.subject || "",
            onSubmit: (msg) => startOp({ op: "annotate-tag", tag: tg.name, message: msg }, "annotating " + tg.name),
          }),
      },
      // The engine resolves the remote (auto for one, a parked pick for
      // several); delete-from-remote confirms via its own parked decision,
      // so neither row needs a local confirm.
      { label: "push tag", act: () => startOp({ op: "push-tag", tag: tg.name }, "pushing tag " + tg.name) },
      { sep: true },
      {
        label: "delete " + tg.name + " from remote",
        danger: true,
        act: () => startOp({ op: "delete-remote-tag", tag: tg.name }, "deleting tag " + tg.name + " from remote"),
      },
      {
        label: "delete " + tg.name,
        danger: true,
        // engine.DeleteTag is decision-free, so the confirm lives here — a
        // right-click plus one click must never delete a ref unconfirmed.
        act: () =>
          showLocalConfirm("Delete tag " + tg.name + "?", ["delete", "abort"], (o) => {
            if (o === "delete") startOp({ op: "delete-tag", tag: tg.name }, "deleting tag " + tg.name);
          }),
      },
      ...extraRows("tag", tg),
    ],
    x,
    y
  );
}

$("tags-list").addEventListener("contextmenu", (e) => {
  const li = e.target.closest("li");
  if (!li || !li.dataset.n) return;
  e.preventDefault();
  const tg = state.tags.find((x) => x.name === li.dataset.n);
  if (tg) showTagMenu(tg, e.clientX, e.clientY);
});


$("stashes-list").addEventListener("click", (e) => {
  const li = e.target.closest("li");
  if (!li || !li.dataset.h) return; // a sha-less row ignores left-click
  const st = state.stashes.find((x) => x.ref === li.dataset.r);
  if (st) openStashDetail(st);
});


function showStashMenu(st, x, y) {
  const items = [];
  if (st.sha) items.push({ label: "show changes", act: () => openStashDetail(st) });
  items.push({ label: "apply", act: () => startOp({ op: "stash-apply", ref: st.ref, sha: st.sha || "" }, "applying " + st.ref) });
  items.push({ label: "pop", act: () => startOp({ op: "stash-pop", ref: st.ref, sha: st.sha || "" }, "popping " + st.ref) });
  items.push({ sep: true });
  items.push({
    label: "drop " + st.ref,
    danger: true,
    // engine.StashDrop is decision-free — the confirm lives here (the
    // delete-tag precedent; the TUI confirms drop with y/n too).
    act: () =>
      showLocalConfirm("Drop " + st.ref + "?", ["drop", "abort"], (o) => {
        if (o === "drop") startOp({ op: "stash-drop", ref: st.ref, sha: st.sha || "" }, "dropping " + st.ref);
      }),
  });
  items.push(...extraRows("stash", st));
  showCtxMenu(items, x, y);
}

$("stashes-list").addEventListener("contextmenu", (e) => {
  const li = e.target.closest("li");
  if (!li || !li.dataset.r) return;
  e.preventDefault();
  const st = state.stashes.find((x) => x.ref === li.dataset.r);
  if (st) showStashMenu(st, e.clientX, e.clientY);
});


// Reflog rows address commits by full sha, so a dangling commit (dropped,
// reset past, rewritten) still opens — git serves it by id on demand.
$("reflog-list").addEventListener("click", (e) => {
  const li = e.target.closest("li");
  if (!li) return;
  if (li.classList.contains("more")) {
    moreReflog();
    return;
  }
  if (!li.dataset.h) return;
  openCommitByHash(li.dataset.h, li.dataset.s || li.dataset.h.slice(0, 8));
});


function showReflogMenu(en, x, y) {
  const short = en.short || en.hash.slice(0, 8);
  showCtxMenu(
    [
      { label: "show commit", act: () => openCommitByHash(en.hash, en.selector) },
      { label: "copy commit sha", act: () => copyText(en.hash, "commit sha " + short) },
      // Both lanes are explicit in the label; the detached one is the escape
      // hatch for inspecting a lost state without inventing a branch name.
      { label: "check out here (detached)", act: () => startOp({ op: "checkout", sha: en.hash }, "checking out " + short) },
      {
        label: "check out as new branch…",
        act: () =>
          openPrompt({
            title: "New branch at " + short + ", then switch to it:",
            placeholder: "branch name",
            onSubmit: (name) => startOp({ op: "checkout", sha: en.hash, name }, "creating " + name + " at " + short),
          }),
      },
      // The TUI's reflog rows: keep a lost commit as a live reference, or
      // freeze its files so they survive the commit being gc'd.
      { label: "bookmark this commit…", act: () => addCommitEntry("bookmarks", en.hash, en.subject) },
      { label: "shelf this commit…", act: () => addCommitEntry("shelf", en.hash, en.subject) },
      { sep: true },
      {
        // Empty mode = the engine's interactive flow: the soft/mixed/hard
        // picker (with cancel, plus the non-ancestor confirm) parks in the
        // modal — that modal IS the confirmation, so no local confirm here.
        label: "reset current branch to this entry…",
        danger: true,
        act: () => startOp({ op: "reset", sha: en.hash }, "resetting to " + short),
      },
      ...extraRows("reflog", en),
    ],
    x,
    y
  );
}

$("reflog-list").addEventListener("contextmenu", (e) => {
  const li = e.target.closest("li");
  if (!li || !li.dataset.h) return;
  e.preventDefault();
  const en = state.reflog.find((x) => x.hash === li.dataset.h && x.selector === li.dataset.s);
  if (en) showReflogMenu(en, e.clientX, e.clientY);
});


// The path budget is measured in COLUMNS of the pane, so widening or narrowing
// the sidebar (drag handle, window resize) has to rebuild those rows —
// otherwise a pane with room to spare keeps showing yesterday's elision.
// rAF-coalesced, because a drag fires this per pixel.
let rowsPending = false;
new ResizeObserver(() => {
  if (rowsPending) return;
  rowsPending = true;
  requestAnimationFrame(() => {
    rowsPending = false;
    renderBranches();
    renderWorktrees();
  });
}).observe($("branches-pane"));


// Bookmarks and the shelf are gg's own stores, not git's: a bookmark is a LIVE
// reference (to a file or a commit), a shelf entry is a FROZEN copy. Both rows
// lead with the name you gave the entry, falling back to the store's own
// display string ("path @ container") when you gave none.
function entryLabel(e) {
  return e.label || e.display || e.id;
}


function renderBookmarks() {
  $("bookmarks-list").innerHTML = (state.bookmarks || [])
    .map(
      (e) =>
        `<li data-id="${esc(e.id)}" title="${esc(e.display)}">${mark(false)}` +
        `<span class="ekind">${e.is_commit ? "◆" : "▪"}</span>` +
        `${esc(entryLabel(e))}</li>`
    )
    .join("");
}


function renderShelf() {
  $("shelf-list").innerHTML = (state.shelf || [])
    .map(
      (e) =>
        `<li data-id="${esc(e.id)}" title="${esc(e.display)}">${mark(false)}` +
        `<span class="ekind">${e.kind === "commit" ? "◆" : "▪"}</span>` +
        `${esc(entryLabel(e))}</li>`
    )
    .join("");
}


// --- adding to either store ------------------------------------------------
// Both stores take an optional NAME, and the TUI prefills it with the commit's
// subject — so the same prompt serves both, and an empty name is impossible
// (a prompt never submits one), which is why the subject is the default rather
// than a placeholder.
function addCommitEntry(store, sha, subject) {
  const what = store === "shelf" ? "Shelve" : "Bookmark";
  openPrompt({
    title: what + " " + sha.slice(0, 8) + " — name it:",
    value: subject || sha.slice(0, 8),
    onSubmit: async (label) => {
      try {
        await postJSON("/api/" + store, { sha, label });
      } catch (e) {
        opLine(store + ": " + (e.message || e), true);
        return;
      }
      opLine(store === "shelf" ? "shelved " + sha.slice(0, 8) : "bookmarked " + sha.slice(0, 8));
      fetchBranches();
    },
  });
}


// A FILE entry carries no name — the address IS the name — so it is one click.
async function addFileEntry(store, path, fileState, sha) {
  try {
    await postJSON("/api/" + store, { path, state: fileState, sha: sha || "" });
  } catch (e) {
    opLine(store + ": " + (e.message || e), true);
    return;
  }
  opLine((store === "shelf" ? "shelved " : "bookmarked ") + path);
  fetchBranches();
}


async function removeEntry(store, id) {
  try {
    await fetch("/api/" + store + "?id=" + encodeURIComponent(id), { method: "DELETE" });
  } catch (e) {
    opLine(store + ": " + (e.message || e), true);
    return;
  }
  fetchBranches();
}


// Opening an entry means showing what it points at: a commit opens the commit,
// a file opens that file's history (the surface that works whether or not the
// file is still in the working tree).
function openEntry(e) {
  if (e.is_commit || e.kind === "commit") {
    if (e.commit) openCommitByHash(e.commit, entryLabel(e));
    else opLine("this entry is frozen content, not a commit in git", true);
    return;
  }
  if (e.path) openFileHistory(e.path, e.commit || "");
}


// restorePrompt asks WHERE, prefilled with the entry's own path so the common
// case ("put it back") is one enter. The destination is repo-relative — the
// op writes into the working tree — and the overwrite question, if the file is
// already there and differs, parks in the modal from the engine itself.
function restorePrompt(store, e, innerPath) {
  const path = innerPath || e.path || "";
  openPrompt({
    title: "Restore " + (innerPath || entryLabel(e)) + " to (path in the repo):",
    value: path,
    placeholder: "path/inside/the/repo.txt",
    onSubmit: (dest) =>
      startOp({ op: "restore-entry", store, id: e.id, path: innerPath || "", dest }, "restoring " + dest),
  });
}


// pickShelfFile lists what a shelved COMMIT froze and restores the one picked.
// A menu of paths, because the entry's files are not otherwise visible.
async function pickShelfFile(e, x, y) {
  const got = await getJSON("/api/shelf/files?id=" + encodeURIComponent(e.id)).catch(() => null);
  if (!got || !(got.files || []).length) {
    opLine("this entry lists no files", true);
    return;
  }
  showCtxMenu(
    got.files.map((p) => ({ label: p, act: () => restorePrompt("shelf", e, p) })),
    x,
    y
  );
}


function showBookmarkMenu(e, x, y) {
  const items = [];
  if (e.is_commit && e.commit) items.push({ label: "show commit", act: () => openEntry(e) });
  else if (e.path) items.push({ label: "file history", act: () => openEntry(e) });
  if (e.commit) items.push({ label: "copy commit id", act: () => copyText(e.commit, "commit id " + e.commit.slice(0, 8)) });
  if (e.path) items.push({ label: "copy path", act: () => copyText(e.path, "path " + e.path) });
  if (e.path) {
    items.push({ sep: true });
    // A bookmark is LIVE, so this writes what it points at today — not a
    // snapshot from when you bookmarked it. That is the shelf's job.
    items.push({ label: "copy its content to a path…", act: () => restorePrompt("bookmarks", e, "") });
  }
  items.push({ sep: true });
  items.push({
    label: "remove bookmark",
    danger: true,
    act: () =>
      showLocalConfirm("Remove the bookmark " + entryLabel(e) + "?", ["remove", "abort"], (o) => {
        if (o === "remove") removeEntry("bookmarks", e.id);
      }),
  });
  showCtxMenu(items, x, y);
}


function showShelfMenu(e, x, y) {
  const items = [];
  if (e.kind === "commit") {
    if (e.commit) items.push({ label: "show the original commit", act: () => openEntry(e) });
    items.push({ sep: true });
    // The frozen files are the entry's real content; restoring one is picked
    // from a list of them rather than typed from memory.
    items.push({ label: "restore a file…", act: () => pickShelfFile(e, x, y) });
    items.push({
      // Re-applies the commit: a live cherry-pick while it still exists, else
      // the patch frozen with the files. The server picks the lane.
      label: "cherry-pick this commit",
      act: () =>
        showLocalConfirm("Apply " + entryLabel(e) + " onto the current branch?", ["cherry-pick", "abort"], (o) => {
          if (o === "cherry-pick") startOp({ op: "shelf-cherry-pick", id: e.id }, "applying " + entryLabel(e));
        }),
    });
  } else if (e.path) {
    items.push({ label: "file history", act: () => openEntry(e) });
    items.push({ label: "copy path", act: () => copyText(e.path, "path " + e.path) });
    items.push({ sep: true });
    items.push({ label: "restore to a path…", act: () => restorePrompt("shelf", e, "") });
  }
  items.push({ sep: true });
  items.push({
    label: "remove from the shelf",
    danger: true,
    // A shelf entry is the only copy of its content once the source is gone,
    // so removing one is confirmed even though it costs a click.
    act: () =>
      showLocalConfirm("Remove " + entryLabel(e) + " from the shelf? The frozen copy is deleted.", ["remove", "abort"], (o) => {
        if (o === "remove") removeEntry("shelf", e.id);
      }),
  });
  showCtxMenu(items, x, y);
}


$("bookmarks-list").addEventListener("click", (e) => {
  const li = e.target.closest("li");
  if (!li || !li.dataset.id) return;
  const b = (state.bookmarks || []).find((x) => x.id === li.dataset.id);
  if (b) openEntry(b);
});

$("bookmarks-list").addEventListener("contextmenu", (e) => {
  const li = e.target.closest("li");
  if (!li || !li.dataset.id) return;
  e.preventDefault();
  const b = (state.bookmarks || []).find((x) => x.id === li.dataset.id);
  if (b) showBookmarkMenu(b, e.clientX, e.clientY);
});

$("shelf-list").addEventListener("click", (e) => {
  const li = e.target.closest("li");
  if (!li || !li.dataset.id) return;
  const s = (state.shelf || []).find((x) => x.id === li.dataset.id);
  if (s) openEntry(s);
});

$("shelf-list").addEventListener("contextmenu", (e) => {
  const li = e.target.closest("li");
  if (!li || !li.dataset.id) return;
  e.preventDefault();
  const s = (state.shelf || []).find((x) => x.id === li.dataset.id);
  if (s) showShelfMenu(s, e.clientX, e.clientY);
});

export { addCommitEntry, addFileEntry, applyStoredSections, branchesList, clearDropTargets, fetchBranches, renderBranches, renderReflog, renderRemotes, renderStashes, renderTags, renderWorktrees, showBranchMenu, showBranchPairMenu, showReflogMenu, showRemoteMenu, showStashMenu, showTagMenu, showWorktreeMenu, toggleSection, worktreePathForBranch };
