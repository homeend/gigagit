// sidebar.js — part of gg's web client. Split from the original app.js;
// see app.js (the entry module) for the load order.
import { $, SECTIONS, esc, getJSON, lsGet, lsSet, state } from "./core.js";
import { closePrompt, copyText, openPrompt, showCtxMenu } from "./layers.js";
import { openPrefixPicker } from "./prefixes.js";
import { doForcePush, doPull, doPullBranch, doPush, doPushBranch, doReroot, showLocalConfirm, startOp, startSwitch } from "./ops.js";
import { openVersions } from "./versions.js";
import { openRebaseEditor } from "./rebase.js";
import { startReview } from "./review.js";
import { gotoBranchTip, openCommitByHash, openStashDetail, setSolo } from "./commits.js";
import { openCompare } from "./files.js";

async function fetchBranches() {
  const [b, w, tg, st, rl, rm] = await Promise.all([
    getJSON("/api/branches"),
    getJSON("/api/worktrees").catch(() => ({ worktrees: [] })),
    getJSON("/api/tags").catch(() => ({ tags: [], truncated: false })),
    getJSON("/api/stashes").catch(() => ({ stashes: [] })),
    getJSON("/api/reflog").catch(() => ({ entries: [], truncated: false })),
    getJSON("/api/remotes").catch(() => ({ remotes: [], truncated: false })),
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
  renderBranches();
  renderRemotes();
  renderWorktrees();
  renderTags();
  renderStashes();
  renderReflog();
}


function renderBranches() {
  $("branches-list").innerHTML = state.branches
    .map((b) => {
      const ab =
        (b.ahead ? "↑" + b.ahead : "") + (b.behind ? (b.ahead ? " " : "") + "↓" + b.behind : "");
      return (
        `<li class="${b.is_head ? "head" : ""}" draggable="true" data-n="${esc(b.name)}">` +
        `${b.is_head ? "✓ " : ""}${esc(b.name)}${ab ? `<span class="ab">${ab}</span>` : ""}</li>`
      );
    })
    .join("");
}


function renderRemotes() {
  let html = state.remotes
    .map((rb) => `<li data-n="${esc(rb.name)}" data-h="${esc(rb.hash)}">${esc(rb.name)}</li>`)
    .join("");
  if (state.remotesTruncated) html += `<li class="more">… more (capped at 100)</li>`;
  $("remotes-list").innerHTML = html;
}


function renderWorktrees() {
  $("worktrees-list").innerHTML = state.worktrees
    .map((w) => {
      const label = w.bare ? "(bare)" : w.detached ? "(detached)" : w.branch || "(?)";
      const base = w.path.split("/").pop();
      const cur = state.worktree && w.path === state.worktree ? " cur" : "";
      return `<li class="${cur.trim()}" data-p="${esc(w.path)}" title="${esc(w.path)}">${cur ? "● " : ""}${esc(label)}<span class="wpath">${esc(base)}</span></li>`;
    })
    .join("");
}


function renderTags() {
  let html = state.tags
    .map(
      (t) =>
        `<li data-h="${esc(t.target)}" data-n="${esc(t.name)}">${esc(t.name)}` +
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


function renderReflog() {
  // Column-ish row: @{N} (the recovery selector, shortened for display —
  // data-s keeps the full form the menus use), short sha, subject
  // (ellipsized), compact age pinned right.
  let html = state.reflog
    .map(
      (e) =>
        `<li data-h="${esc(e.hash)}" data-s="${esc(e.selector)}">` +
        `<span class="rsel">${esc(e.selector.replace(/^HEAD@/, "@"))}</span>` +
        `<span class="rsha">${esc(e.short || "")}</span>` +
        `<span class="rsub">${esc(e.subject || "")}</span>` +
        (e.rel ? `<span class="rrel" title="${esc(e.rel)}">${esc(compactRel(e.rel))}</span>` : "") +
        `</li>`
    )
    .join("");
  if (state.reflogTruncated) html += `<li class="more">… more (capped at 100)</li>`;
  $("reflog-list").innerHTML = html;
}


function renderStashes() {
  $("stashes-list").innerHTML = state.stashes
    .map(
      (s) =>
        `<li data-r="${esc(s.ref)}"${s.sha ? ` data-h="${esc(s.sha)}"` : ""}>${esc(s.ref)}` +
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


// A starting point for the worktree-path prompt, not a decision: a sibling of
// the MAIN worktree named <repo>-<branch>. Anchoring on the main worktree
// (git lists it first) rather than the served one keeps new worktrees from
// nesting inside each other when you create one while inside another — the
// same anchor the TUI's template resolver uses. Slashes in a branch name
// become dashes so `feat/x` does not imply a directory.
function defaultWorktreePath(branch) {
  const main = (state.worktrees[0] && state.worktrees[0].path) || (state.repo && state.repo.worktree) || "";
  if (!main) return "";
  const sep = main.includes("\\") && !main.includes("/") ? "\\" : "/";
  const cut = main.lastIndexOf(sep);
  const parent = cut > 0 ? main.slice(0, cut) : main;
  const name = cut >= 0 ? main.slice(cut + 1) : main;
  return parent + sep + name + "-" + branch.replace(/[^\w.-]+/g, "-");
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
// openCreateBranchPrompt: the one create-branch dialog (☰/palette = from
// HEAD; the branch menu passes its start point). "use prefix…" mirrors the
// TUI popup's ctrl+p: pick a saved prefix, fill its <user:…> labels, and
// the resolved name seeds the input — still editable; plain typing needs no
// prefix at all. A completed pick rides along on the submit as the prefix
// identity, so its <seq> counters advance only when the create succeeds;
// canceling the picker restores the prompt with whatever was typed.
function openCreateBranchPrompt(start, seed) {
  openPrompt({
    title: start ? "New branch, starting at " + start + ":" : "New branch, starting at the current HEAD:",
    placeholder: "branch name",
    value: seed ? seed.value : "",
    extra: {
      label: "use prefix…",
      run: (typed) => {
        closePrompt();
        openPrefixPicker((resolved, p) => {
          if (resolved == null) {
            openCreateBranchPrompt(start, typed ? { value: typed } : undefined);
            return;
          }
          openCreateBranchPrompt(start, { value: resolved, prefix: p });
        });
      },
    },
    onSubmit: (name) => {
      const body = { op: "create-branch", name };
      if (start) body.branch = start;
      if (seed && seed.prefix) {
        body.prefix_id = seed.prefix.id;
        body.prefix_scope = seed.prefix.scope;
      }
      startOp(body, "creating " + name);
    },
  });
}


function showBranchMenu(b, x, y) {
  const items = [{ label: "go to tip", act: () => gotoBranchTip(b) }];
  if (!b.is_head) items.push({ label: "switch to " + b.name, act: () => startSwitch(b.name) });
  if (b.is_head) {
    // The checked-out branch: pulling it rewrites the working tree, so it
    // goes through the confirming current-branch path the header button uses.
    items.push({ label: "pull " + b.name, act: () => doPull() });
    items.push({ label: "push " + b.name, act: () => doPush() });
  } else {
    items.push({ label: "pull " + b.name + " (stay here)", act: () => doPullBranch(b.name) });
    items.push({ label: "push " + b.name, act: () => doPushBranch(b.name) });
  }
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
  items.push({ label: "previous versions…", act: () => openVersions(b.name) });
  items.push({ label: "review " + b.name + " (AI)…", act: () => startReview("branch", b.name) });
  if (state.solo === b.name) {
    items.push({ label: "exit solo (show every branch)", act: () => setSolo("") });
  } else {
    items.push({ label: "solo this branch", act: () => setSolo(b.name) });
  }
  items.push({ label: "copy branch name", act: () => copyText(b.name, "branch name " + b.name) });
  // b.hash is git's abbreviated sha (%(objectname:short)) — the same value
  // the TUI's row copies, and short enough to name in full on the line.
  if (b.hash) items.push({ label: "copy commit id", act: () => copyText(b.hash, "commit id " + b.hash) });
  const wt = worktreePathForBranch(b.name);
  if (wt) {
    items.push({ label: "copy worktree absolute path", act: () => copyText(wt, "absolute path " + wt) });
  }
  // Destructive row last, as in the worktree and tag menus.
  if (!b.is_head) {
    items.push({
      label: "delete " + b.name,
      danger: true,
      act: () => startOp({ op: "delete-branch", branch: b.name }, "deleting " + b.name),
    });
  }
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
  items.push({ label: "copy name", act: () => copyText(rb.name, "name " + rb.name) });
  if (rb.hash) items.push({ label: "copy commit id", act: () => copyText(rb.hash, "commit id " + rb.hash) });
  items.push({
    // The engine's own confirm parks in the modal before the deletion is
    // pushed (the delete-branch precedent).
    label: "delete " + rb.name + " from remote",
    danger: true,
    act: () => startOp({ op: "delete-remote-branch", ref: rb.name }, "deleting " + rb.name),
  });
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
    items.push({
      label: "remove worktree",
      danger: true,
      act: () => startOp({ op: "remove-worktree", path: w.path }, "removing " + w.path.split("/").pop()),
    });
  }
  showCtxMenu(items, x, y);
}

$("worktrees-list").addEventListener("contextmenu", (e) => {
  const li = e.target.closest("li");
  if (!li || !li.dataset.p) return;
  e.preventDefault();
  const w = state.worktrees.find((x) => x.path === li.dataset.p);
  if (w) showWorktreeMenu(w, e.clientX, e.clientY);
});

// Double-click a sidebar section header to collapse/expand its list — long
// branch/tag lists otherwise force constant scrolling.
function toggleSection(name) {
  const collapsed = $(name + "-list").classList.toggle("collapsed");
  $(name + "-header").textContent = (collapsed ? "\u25b8 " : "") + name;
  lsSet("gg.sidebar.collapsed", JSON.stringify(SECTIONS.filter((n) => $(n + "-list").classList.contains("collapsed"))));
}

SECTIONS.forEach((n) => {
  $(n + "-header").addEventListener("dblclick", () => toggleSection(n));
});


// Restore persisted sidebar state (b-key visibility + per-section
// collapse). The collapsed class lives on the persistent <ul> containers,
// so a one-time boot restore survives every re-render.
(function restoreSidebar() {
  let names = [];
  try { names = JSON.parse(lsGet("gg.sidebar.collapsed") || "[]"); } catch {}
  SECTIONS.forEach((n) => {
    if (names.includes(n)) {
      $(n + "-list").classList.add("collapsed");
      $(n + "-header").textContent = "\u25b8 " + n;
    }
  });
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
  if (!li || !li.dataset.h) return;
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
      {
        // Empty mode = the engine's interactive flow: the soft/mixed/hard
        // picker (with cancel, plus the non-ancestor confirm) parks in the
        // modal — that modal IS the confirmation, so no local confirm here.
        label: "reset current branch to this entry…",
        danger: true,
        act: () => startOp({ op: "reset", sha: en.hash }, "resetting to " + short),
      },
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

export { branchesList, clearDropTargets, defaultWorktreePath, fetchBranches, openCreateBranchPrompt, renderBranches, renderReflog, renderRemotes, renderStashes, renderTags, renderWorktrees, showBranchMenu, showBranchPairMenu, showReflogMenu, showRemoteMenu, showStashMenu, showTagMenu, showWorktreeMenu, toggleSection, worktreePathForBranch };
