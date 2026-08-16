// conflicts.js — part of gg's web client. A feature module: it contributes
// menu rows, a help row and one button through the registries (menus.js)
// instead of editing commits.js / sidebar.js / index.html.
//
// Three things that were TUI-only until now, and one shape they share — each
// of them is a shortcut past a longer road the browser already had:
//
//   whole-file conflict picks   the hunk picker weaves two sides together
//                               line by line. When you already know which
//                               side wins — or one side deleted the file and
//                               there is nothing to weave — these are one
//                               click. Mark-all is the same idea for the
//                               whole conflicted set.
//   stash a selection           the stash button takes the entire working
//                               tree. The checklist takes the part of it you
//                               name.
//   compare against an entry    a bookmark or a shelf entry is a thing worth
//                               diffing against, and the compare view could
//                               only ever hold two live commits.
import { $, esc, getJSON, state } from "./core.js";
import { closeLayer, mountOverlay, pushLayer, showCtxMenu } from "./layers.js";
import { registerHelp, registerRows } from "./menus.js";
import { opLine, showLocalConfirm, startOp } from "./ops.js";
import { openCompare, openEntryCompare, openEntryFileDiff } from "./files.js";

// This module brings its own stylesheet: style.css belongs to no one feature,
// and — the trap — it has NO global `.hidden` rule. Every surface hides
// itself by id, so an element that ships with class="hidden" and no matching
// rule is plainly visible.
function injectStyle() {
  const css = `
#gg-mark-all.hidden { display: none; }
#gg-stash-pick.hidden { display: none; }
#gg-stash-pick {
  position: fixed; inset: 0; z-index: 60; display: flex;
  align-items: center; justify-content: center; background: rgba(0,0,0,.45);
}
#gg-stash-pick .box {
  background: var(--panel, #1b1d22); border: 1px solid var(--line, #3a3f47);
  border-radius: 6px; padding: 14px; min-width: 380px; max-width: min(640px, 92vw);
  max-height: 80vh; display: flex; flex-direction: column; gap: 10px;
}
#gg-stash-pick h3 { margin: 0; font-size: 13px; font-weight: 600; }
#gg-stash-pick input[type=text] { width: 100%; box-sizing: border-box; }
#gg-stash-pick .files { overflow: auto; border: 1px solid var(--line, #3a3f47); border-radius: 4px; }
#gg-stash-pick .frow { display: flex; align-items: center; gap: 8px; padding: 3px 8px; cursor: pointer; }
#gg-stash-pick .frow.sel { background: var(--sel, #2b3342); }
#gg-stash-pick .frow .kind { opacity: .6; font-size: 11px; }
#gg-stash-pick .row { display: flex; gap: 8px; justify-content: flex-end; align-items: center; }
#gg-stash-pick .hint { opacity: .7; font-size: 11px; margin-right: auto; }
`;
  const el = document.createElement("style");
  el.textContent = css;
  document.head.append(el);
}
injectStyle();

// --- whole-file conflict picks ---------------------------------------------

// The seven porcelain-v2 unmerged codes are fixed mnemonics, NOT independent
// per-side flags: 'U' means "ours has content" in UD and "ours is gone" in
// UA. Stage presence is therefore derived from the whole code — the rule
// internal/model/conflict.go states, ported here because the row has to be
// decided while the menu is opening, with no round-trip to ask.
//
// The port is guarded: conflictactionsjs_test.go re-derives these three lists
// from the Go model and fails if they drift. And the server validates every
// action against the file's real class anyway, so a stale page can at worst
// offer a row that is then refused with a sentence — never resolve a conflict
// the wrong way.
// --- conflict class table (pure; guarded against Go) ---
const HAS_OURS = ["AU", "UD", "AA", "UU"];
const HAS_THEIRS = ["UA", "DU", "AA", "UU"];
const HAS_BASE = ["DD", "UD", "DU", "UU"];

// The file menu's context is {path, sha, section} — it carries no porcelain
// bytes, so the row it was opened on is looked up in the status. Without this
// every conflict reads as code "" and classifies as deleted-on-both-sides,
// which offers exactly the wrong single row.
function statusRow(ctx) {
  if (!ctx || !ctx.path) return null;
  return state.statusEntries.find((x) => x.path === ctx.path && x.section === ctx.section) || null;
}


function conflictCode(f) {
  return (f.staged || "") + (f.unstaged || "");
}

// conflictActions lists the whole-file actions this conflict supports, as
// {action, label} in menu order. Empty for anything that is not conflicted.
function conflictActions(f) {
  if (!f || f.section !== "conflicts") return [];
  const code = conflictCode(f);
  const ours = HAS_OURS.includes(code);
  const theirs = HAS_THEIRS.includes(code);
  if (ours && theirs) {
    // Modified on both sides: the hunk picker is the detailed road, these are
    // the shortcuts along it.
    return [
      { action: "ours", label: "keep current (ours), whole file" },
      { action: "theirs", label: "keep incoming (theirs), whole file" },
    ];
  }
  // Modify/delete: one side changed the file, the other removed it. There is
  // nothing to pick through, which is why the hunk picker cannot help here.
  const rows = [];
  if (ours || theirs) rows.push({ action: "keep", label: "keep the modified file" });
  rows.push({ action: "delete", label: "delete the file", danger: true });
  if (HAS_BASE.includes(code)) rows.push({ action: "base", label: "keep the base version (before both changes)" });
  return rows;
}
// --- end conflict class table ---


// conflictKindLabel names the conflict in the menu header, so the rows below
// it are self-explaining — "delete the file" reads very differently when you
// can see that the other side already did.
function conflictKindLabel(f) {
  const code = conflictCode(f);
  const ours = HAS_OURS.includes(code);
  const theirs = HAS_THEIRS.includes(code);
  if (ours && theirs) return "modified on both sides";
  if (theirs) return "deleted by us, modified by them";
  if (ours) return "modified by us, deleted by them";
  return "deleted on both sides";
}


function resolveConflictWholeFile(f, row) {
  const run = () => startOp({ op: "resolve-conflict", path: f.path, mode: row.action }, row.label + " — " + f.path);
  if (!row.danger) {
    run();
    return;
  }
  showLocalConfirm(
    "Delete " + f.path + " to resolve the conflict? The version still in your working tree goes with it.",
    ["delete", "abort"],
    (o) => { if (o === "delete") run(); }
  );
}


registerRows("file", (ctx) => {
  if (!ctx || ctx.section !== "conflicts") return [];
  const f = statusRow(ctx);
  const rows = conflictActions(f);
  if (!rows.length) return [];
  return [
    { header: "conflict — " + conflictKindLabel(f) },
    ...rows.map((row) => ({
      label: row.label,
      danger: row.danger,
      act: () => resolveConflictWholeFile(f, row),
    })),
  ];
});


// --- mark all resolved ------------------------------------------------------
//
// The button lives in the conflict bar, which is where the other whole-op
// actions (continue / abort) already are — this one belongs beside them, not
// buried in a per-file menu. index.html is off limits in this wave, so the
// button is created here and inserted into that bar.
//
// Keeping it in step with the conflict state is the only subtlety. status.js
// rewrites #conflict-msg on every status read and nothing else in the bar, so
// observing that one element is a precise "the conflict state just changed"
// signal — and observing it rather than the bar means this module's own
// writes (to a sibling) can never retrigger the observer.
const MARK_ALL_ID = "gg-mark-all";

function conflictedCount() {
  return (state.conflict && state.conflict.conflicted) || 0;
}


function markAllResolved() {
  const n = conflictedCount();
  if (!n || state.op) return;
  showLocalConfirm(
    "Mark all " + n + " conflicted file" + (n > 1 ? "s" : "") +
      " resolved? They are staged exactly as they stand — if any still contain conflict markers, the markers are staged too.",
    ["mark resolved", "abort"],
    (o) => { if (o === "mark resolved") startOp({ op: "mark-all-resolved" }, "marking " + n + " resolved"); }
  );
}


function syncMarkAll() {
  const btn = $(MARK_ALL_ID);
  if (!btn) return;
  const n = conflictedCount();
  // Hidden with nothing conflicted: the bar itself stays up in the "all
  // resolved — press Continue" state, and a mark-all offered there would
  // stage whatever else happens to be lying around.
  btn.classList.toggle("hidden", n === 0);
  btn.disabled = !!state.op;
  const label = "mark all " + n + " resolved (stage as-is)";
  if (n && btn.textContent !== label) btn.textContent = label;
}


function mountMarkAll() {
  const bar = $("conflict-bar");
  const msg = $("conflict-msg");
  if (!bar || !msg || $(MARK_ALL_ID)) return;
  const btn = document.createElement("button");
  btn.id = MARK_ALL_ID;
  btn.className = "hidden";
  btn.textContent = "mark all resolved (stage as-is)";
  btn.addEventListener("click", markAllResolved);
  // Before the AI/continue group: this is one of the ways you GET to a state
  // where Continue is possible, so it reads left-to-right in that order.
  bar.insertBefore(btn, $("conflict-ai") || bar.firstChild);
  new MutationObserver(syncMarkAll).observe(msg, { childList: true, characterData: true, subtree: true });
  syncMarkAll();
}
mountMarkAll();


// --- stash a selection ------------------------------------------------------

// stashCandidates: what can go into a stash — untracked files and files with
// unstaged content. A file whose only change is staged has nothing left in
// the working tree to stash, and a conflicted path is never stashable. Mirrors
// the server's stashCandidate (stashes.go), which refuses anything else.
function stashCandidates() {
  return state.statusEntries
    .filter((f) => f.section === "changes" || f.section === "untracked")
    .map((f) => ({ path: f.path, untracked: f.section === "untracked" }));
}


let stashPick = null; // {files:[{path,untracked,on}], sel} while the picker is open

function renderStashPick() {
  if (!stashPick) return;
  $("gg-stash-files").innerHTML = stashPick.files
    .map(
      (f, i) =>
        `<div class="frow${i === stashPick.sel ? " sel" : ""}" data-i="${i}">` +
        `<input type="checkbox" ${f.on ? "checked" : ""} tabindex="-1">` +
        `<span>${esc(f.path)}</span>` +
        (f.untracked ? `<span class="kind">untracked</span>` : "") +
        `</div>`
    )
    .join("");
  const n = stashPick.files.filter((f) => f.on).length;
  $("gg-stash-ok").disabled = n === 0;
  $("gg-stash-ok").textContent = n === stashPick.files.length ? "stash all " + n : "stash " + n;
}


function closeStashPick() {
  stashPick = null;
  closeLayer("gg-stash-pick");
}


function submitStashPick() {
  if (!stashPick) return;
  const picked = stashPick.files.filter((f) => f.on);
  if (!picked.length) return;
  const all = picked.length === stashPick.files.length;
  const message = $("gg-stash-name").value.trim();
  closeStashPick();
  // Everything ticked is the whole-tree stash the button already does — the
  // paths are omitted rather than spelled out, so the two routes cannot drift
  // (that lane stashes untracked files too, which is what "all" means here).
  if (all) {
    startOp({ op: "stash", message }, "stashing");
    return;
  }
  startOp(
    { op: "stash-paths", paths: picked.map((f) => f.path), message },
    "stashing " + picked.length + " file" + (picked.length > 1 ? "s" : "")
  );
}


function stashPickKey(e) {
  if (!stashPick) return false;
  if (e.key === "Escape") {
    closeStashPick();
    return true;
  }
  // Typing in the name field is typing; the list keys must not steal it.
  if (document.activeElement === $("gg-stash-name")) {
    if (e.key === "Enter") {
      e.preventDefault();
      submitStashPick();
      return true;
    }
    return false;
  }
  if (e.key === " " || e.key === "Spacebar") {
    e.preventDefault();
    const f = stashPick.files[stashPick.sel];
    if (f) f.on = !f.on;
    renderStashPick();
    return true;
  }
  if (e.key === "ArrowDown" || e.key === "ArrowUp") {
    e.preventDefault();
    const d = e.key === "ArrowDown" ? 1 : -1;
    stashPick.sel = Math.max(0, Math.min(stashPick.files.length - 1, stashPick.sel + d));
    renderStashPick();
    return true;
  }
  if (e.key === "Enter") {
    e.preventDefault();
    submitStashPick();
    return true;
  }
  return false;
}


// buildStashPick creates the overlay once. mountOverlay hands back an empty
// div the first time and the same one afterwards, so the listeners below are
// attached exactly once.
function buildStashPick() {
  const el = mountOverlay("gg-stash-pick");
  if (el.dataset.built) return el;
  el.dataset.built = "1";
  el.innerHTML =
    `<div class="box">` +
    `<h3>Stash changes</h3>` +
    `<input id="gg-stash-name" type="text" placeholder="WIP">` +
    `<div class="files" id="gg-stash-files"></div>` +
    `<div class="row"><span class="hint">space toggles · enter stashes · esc cancels</span>` +
    `<button id="gg-stash-cancel">cancel</button>` +
    `<button id="gg-stash-ok">stash</button></div>` +
    `</div>`;
  el.addEventListener("click", (e) => {
    if (e.target === el) closeStashPick(); // click outside the box
  });
  $("gg-stash-files").addEventListener("click", (e) => {
    const row = e.target.closest(".frow");
    if (!row || !stashPick) return;
    const i = Number(row.dataset.i);
    stashPick.sel = i;
    stashPick.files[i].on = !stashPick.files[i].on;
    renderStashPick();
  });
  $("gg-stash-cancel").addEventListener("click", closeStashPick);
  $("gg-stash-ok").addEventListener("click", submitStashPick);
  return el;
}


// openStashPick offers the checklist. preselect (a path, or the marked set)
// starts with only those ticked — picking "stash this file" and then having
// to untick everything else would be a worse answer than not offering it.
function openStashPick(preselect) {
  if (state.op) return;
  if (state.conflict) {
    // The same gate the mass staging rows live under: a paused sequencer op
    // owns the working tree, and git refuses to stash mid-merge anyway.
    opLine("finish or abort the " + state.conflict.op + " first — a stash cannot be taken mid-op", true);
    return;
  }
  const cand = stashCandidates();
  if (!cand.length) {
    opLine("nothing to stash — every change is already staged or committed", true);
    return;
  }
  const only = preselect && preselect.size ? preselect : null;
  stashPick = {
    files: cand.map((f) => ({ ...f, on: !only || only.has(f.path) })),
    sel: 0,
  };
  if (!stashPick.files.some((f) => f.on)) stashPick.files.forEach((f) => (f.on = true));
  const el = buildStashPick();
  $("gg-stash-name").value = "WIP on " + ((state.repo && state.repo.branch) || "");
  renderStashPick();
  pushLayer("gg-stash-pick", el, { onKey: stashPickKey });
  $("gg-stash-name").focus();
  $("gg-stash-name").select();
}


// Both entry points carry the paused-op gate the mass staging rows already
// live under: while a sequencer op is paused the working tree belongs to it,
// git refuses to stash mid-merge, and a row that can only be refused is worse
// than no row. openStashPick keeps its own check as the backstop.
registerRows("menu", () => (state.wt && !state.conflict ? [{ label: "stash a selection…", act: () => openStashPick(null) }] : []));

registerRows("file", (f) => {
  if (!f || state.conflict) return [];
  if (f.section === "commit" || f.section === "conflicts" || f.section === "staged") return [];
  const rows = [{ label: "stash a selection…", act: () => openStashPick(new Set([f.path])) }];
  if (state.marked.size > 1) {
    rows.push({
      label: "stash the " + state.marked.size + " marked…",
      act: () => openStashPick(new Set(state.marked)),
    });
  }
  return rows;
});


// --- compare against a bookmark or shelf entry ------------------------------

// entryLabel names an entry the way the sidebar's own lists do. (Copied
// rather than imported: sidebar.js is not this feature's to touch.)
function entryLabel(e) {
  return e.label || e.display || e.id;
}


// A menu row's act() is called with no arguments (layers.js), so a row that
// opens a SECOND menu has nothing to place it by. The last pointer position
// is that anchor: for a row opened from a context menu it is the click on the
// row itself, which is exactly where the picker should appear. Capture phase,
// so it is recorded even where a handler stops propagation.
let lastPointer = { x: 120, y: 120 };
document.addEventListener(
  "pointerdown",
  (e) => {
    if (e.clientX || e.clientY) lastPointer = { x: e.clientX, y: e.clientY };
  },
  true
);


// pickEntry opens a picker over both stores, filtered to the kind of entry
// this comparison can use. The two lists are labelled, because "the fix" can
// be a bookmark and a shelf entry at the same time and they mean different
// things.
// exclude, when given, is the {store, id} this comparison starts FROM: an
// entry cannot be compared with itself, and offering it as the other side
// would produce an empty diff and no explanation.
async function pickEntry({ commits, onPick, exclude, title }) {
  const { x, y } = lastPointer;
  const [bm, sh] = await Promise.all([
    getJSON("/api/bookmarks").catch(() => null),
    getJSON("/api/shelf").catch(() => null),
  ]);
  const other = (store, e) => !(exclude && exclude.store === store && exclude.id === e.id);
  const rows = [];
  const bookmarks = ((bm && bm.entries) || []).filter((e) => !!e.is_commit === commits && other("bookmarks", e));
  const shelf = ((sh && sh.entries) || []).filter((e) => (e.kind === "commit") === commits && other("shelf", e));
  if (title && (bookmarks.length || shelf.length)) rows.push({ header: title });
  if (bookmarks.length) {
    rows.push({ header: "bookmarks" });
    for (const e of bookmarks) rows.push({ label: entryLabel(e), act: () => onPick("bookmarks", e) });
  }
  if (shelf.length) {
    rows.push({ header: "shelf" });
    for (const e of shelf) rows.push({ label: entryLabel(e), act: () => onPick("shelf", e) });
  }
  if (!rows.length) {
    const what = commits ? "commits" : "files";
    opLine(exclude ? "no other " + what + " are bookmarked or shelved" : "no " + what + " are bookmarked or shelved yet", true);
    return;
  }
  showCtxMenu(rows, x, y);
}


// compareCommitWithEntry: the whole-tree lane. Both sides resolve server-side
// — the wire carries a store name and an id, never a path into gg's state
// directory — and the answer says which shape came back:
//
//   live   both sides are real commits: the ordinary compare view, keyed by
//          the two HASHES (a name there would poison the diff cache, which is
//          the bug this rule exists to prevent).
//   frozen the entry's commit is gone and a shelf entry stood in for it: the
//          entry-diff lane, with the fallback named in the header.
async function compareCommitWithEntry(store, entry, sha) {
  let body;
  try {
    body = await getJSON(
      "/api/compare-entry?store=" + encodeURIComponent(store) +
        "&id=" + encodeURIComponent(entry.id) +
        "&sha=" + encodeURIComponent(sha)
    );
  } catch (e) {
    opLine("compare failed: " + (e.message || e), true);
    return;
  }
  if (body.frozen) {
    openEntryCompare(body);
    return;
  }
  openCompare(body.left.hash, body.right.hash, {
    revs: 1,
    aLabel: body.left.label,
    bLabel: body.right.label,
  });
}


registerRows("commit", (c) =>
  c && c.hash
    ? [{
        label: "compare with a bookmark or shelf entry…",
        act: () =>
          pickEntry({
            commits: true,
            onPick: (store, e) => compareCommitWithEntry(store, e, c.hash),
          }),
      }]
    : []
);


// fileSideSpec maps the focused file onto an entry-diff side. A file in a
// commit is that commit's copy; a staged row and its working-tree twin are
// different addresses, not one file in two moods (the bookmark rule).
function fileSideSpec(f) {
  if (f.section === "commit") return f.sha ? "commit:" + f.sha : "";
  if (f.section === "staged") return "staged";
  return "worktree";
}


function fileSideLabel(f) {
  if (f.section === "commit") return (f.sha || "").slice(0, 7);
  return f.section === "staged" ? "staged" : "working tree";
}


registerRows("file", (f) => {
  if (!f || !f.path || f.section === "conflicts") return [];
  const right = fileSideSpec(f);
  if (!right) return [];
  return [{
    label: "compare with a stored copy…",
    act: () =>
      pickEntry({
        commits: false,
        onPick: (store, e) =>
          openEntryFileDiff({
            left: (store === "shelf" ? "shelf:" : "bookmark:") + e.id,
            right,
            path: f.path,
            leftLabel: entryLabel(e),
            rightLabel: fileSideLabel(f),
          }),
      }),
  }];
});


// --- comparing two stored entries -------------------------------------------
//
// The other direction, and the one the TUI reaches from the bookmark and shelf
// popups themselves: not "this commit against something I saved", but "these
// two saved things against each other". Two shelf entries of the same file
// taken a week apart, a bookmark against the frozen copy that was made from
// it, one spike against another.
//
// The entry you open the menu on is the LEFT/older side and the one you pick
// is the right — startEntryCompare's rule, which is what makes an A mean
// "added since" rather than "missing from".
//
// The picker only offers entries of the SAME kind, so the TUI's "cannot
// compare a commit against a file" refusal has nothing to refuse here: a
// comparison that cannot be expressed is never offered. The server holds the
// same line independently (a file entry is not a commit entry).

// entrySpecOf names an entry for /api/entry-diff.
function entrySpecOf(store, e) {
  return (store === "shelf" ? "shelf:" : "bookmark:") + e.id;
}


// compareTwoEntries dispatches on kind: commit entries go through the
// whole-tree lane (which resolves each side hybrid, so a gc'd commit falls
// back to its frozen copy independently of the other side), file entries
// through the one-file lane.
async function compareTwoEntries(leftStore, left, rightStore, right) {
  if (left.kind === "file" || left.is_commit === false) {
    // Two stored FILES: neither side needs a path — a shelved file is one
    // blob and a bookmark resolves its own address — but the endpoint takes
    // one, and the file's own path is what names the diff.
    openEntryFileDiff({
      left: entrySpecOf(leftStore, left),
      right: entrySpecOf(rightStore, right),
      path: left.path || right.path || "",
      leftLabel: entryLabel(left),
      rightLabel: entryLabel(right),
    });
    return;
  }
  let body;
  try {
    body = await getJSON(
      "/api/compare-entry?store=" + encodeURIComponent(leftStore) +
        "&id=" + encodeURIComponent(left.id) +
        "&right_store=" + encodeURIComponent(rightStore) +
        "&right_id=" + encodeURIComponent(right.id)
    );
  } catch (e) {
    opLine("compare failed: " + (e.message || e), true);
    return;
  }
  // Both commits still alive → the ordinary hash-keyed compare view. Either
  // side frozen → the entry-diff lane, fallback named in the header.
  if (body.frozen) {
    openEntryCompare(body);
    return;
  }
  openCompare(body.left.hash, body.right.hash, {
    revs: 1,
    aLabel: body.left.label,
    bLabel: body.right.label,
  });
}


// isCommitEntry reads the two stores' different spellings of the same fact: a
// bookmark says is_commit, a shelf entry says kind.
function isCommitEntry(e) {
  return e.kind === "commit" || e.is_commit === true;
}


function compareWithAnotherEntryRow(store, e) {
  return {
    label: "compare with another entry…",
    act: () =>
      pickEntry({
        commits: isCommitEntry(e),
        exclude: { store, id: e.id },
        title: entryLabel(e) + " ↔ …",
        onPick: (otherStore, other) => compareTwoEntries(store, e, otherStore, other),
      }),
  };
}


registerRows("bookmark", (e) => (e && e.id ? [compareWithAnotherEntryRow("bookmarks", e)] : []));
registerRows("shelf", (e) => (e && e.id ? [compareWithAnotherEntryRow("shelf", e)] : []));


// The static help already has a "conflicts" row (the bar, the block picker),
// so this one is keyed apart from it — two rows under one key read as a
// duplicate rather than as two different things.
registerHelp({
  key: "conflict actions",
  html:
    "right-click a conflicted file for the whole-file answers — <b>keep current</b> / <b>keep incoming</b>, " +
    "or, when one side deleted it, <b>keep the modified file</b> / <b>delete</b> / <b>keep the base version</b>; " +
    "the block picker stays the detailed road. <b>mark all N resolved</b> in the conflict bar stages every " +
    "conflicted file as it stands (markers included) — for when you resolved them in your editor",
});

registerHelp({
  key: "stash · compare",
  html:
    "☰ or a file's menu → <b>stash a selection…</b> opens a checklist (space toggles); ticking everything is the " +
    "plain whole-tree stash. Right-click a commit → <b>compare with a bookmark or shelf entry…</b>, or a file → " +
    "<b>compare with a stored copy…</b>; right-click a bookmark or shelf entry itself → " +
    "<b>compare with another entry…</b> to put two saved things side by side (the one you started from is the " +
    "older side, and only entries of the same kind are offered). A shelved commit still compares after the " +
    "original is gone — the header says when you are looking at the frozen copy",
});

export { conflictActions, conflictCode, openStashPick, stashCandidates };
