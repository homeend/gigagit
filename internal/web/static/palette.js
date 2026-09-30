// palette.js — part of gg's web client. Split from the original app.js;
// see app.js (the entry module) for the load order.
import { $, charWidth, elidePath, esc, getJSON, runes, state } from "./core.js";
import { closeLayer, hideCtxMenu, openPrompt, pushLayer, showCtxMenu, topLayer } from "./layers.js";
import { doFetch, doPull, doPush, doReroot, manualRefresh, opLine, openCreateBranchPrompt, openHelp, showLocalConfirm, startOp, toggleSidebar } from "./ops.js";
import { openVersionBranches } from "./versions.js";
import { openFileBlame, openFileHistory } from "./filehist.js";
import { openSettings } from "./settings.js";
import { openIdentityView } from "./identity.js";
import { openPrefixesView } from "./prefixes.js";
import { openExtToolsView } from "./exttools.js";
import { openSessionErrorsView } from "./sessionerrors.js";
import { startReview } from "./review.js";
import { gotoCommitPrompt, openCommitFilter, toggleGraphMode } from "./commits.js";
import { openWorkingTree, toggleFilesHidden } from "./files.js";
import { toggleStacked } from "./stackview.js";
import { extraRows } from "./menus.js";
import { applyPatchPrompt, pickEntryToCopy } from "./patch.js";
import { openStashPick } from "./conflicts.js";
import { startAmend } from "./commitai.js";
import { openGitConfig } from "./gitconfig.js";
import { openAgentSetup } from "./agentsetup.js";
import { openFeedFilter } from "./search.js";
import { openFinder } from "./wtfinder.js";
import { openRemoteHeads } from "./remoteheads.js";
import { openAllNotes } from "./allnotes.js";
import { openLinkCompareDialog } from "./linkcompare.js";
import { locateCurrentBranch } from "./sidebar.js";
import { featureDisabled } from "./preflight.js";
import { saveUI } from "./uistate.js";

// ---- command palette + global ☰ menu (wave 3) ----------------------------
// The palette is a layer with an input INSIDE it: onKey consumes nav keys
// (returns true) and returns false for everything else, so the browser
// delivers the keystroke to the focused input while the router's
// non-empty-stack short-circuit keeps global keys off. Every close path MUST
// go through closePalette() — the input.blur() is load-bearing: a focused
// input after close would trap all global keys in the form-field guard.

let pal = null; // {mode: "cmd"|"repo", fromCmd, rows, filtered, sel, grouped}


function paletteCommands() {
  const rows = [
    { label: "pull", detail: "p", run: () => doPull() },
    { label: "push", detail: "P", run: () => doPush() },
    { label: "fetch all remotes", detail: "", run: () => doFetch() },
    { label: "prune remotes (drop deleted branches)", detail: "", run: () => startOp({ op: "prune" }, "pruning remotes") },
    { label: "settings…", detail: "", run: () => openSettings() },
    { label: "identity & profiles…", detail: "", run: () => openIdentityView() },
    { label: "branch prefixes…", detail: "", run: () => openPrefixesView() },
    { label: "external tools…", detail: "", run: () => openExtToolsView() },
    { label: "session errors…", detail: "", run: () => openSessionErrorsView() },
    { label: "compare with link…", detail: "", run: () => openLinkCompareDialog() },
    { label: "create branch…", detail: "", run: () => openCreateBranchPrompt() },
    { label: "go to current branch", detail: "", run: () => locateCurrentBranch() },
    { label: "branch versions…", detail: "", run: () => openVersionBranches() },
    { label: "view all notes…", detail: "", run: () => openAllNotes() },
    { label: "file history…", detail: "", run: () => openPrompt({ title: "File history — repo-relative path", placeholder: "e.g. internal/web/server.go", onSubmit: (p) => openFileHistory(p, "") }) },
    { label: "file blame…", detail: "", run: () => openPrompt({ title: "File blame — repo-relative path", placeholder: "e.g. internal/web/server.go", onSubmit: (p) => openFileBlame(p, "") }) },
    { label: "review working changes (AI)…", detail: "", run: () => startReview("working", "") },
    { label: "review this branch (AI)…", detail: "", run: () => startReview("branch", "") },
    { label: "undo last commit", detail: "", run: () => undoLastCommit() },
    { label: "goto commit or link…", detail: "#", run: () => gotoCommitPrompt() },
    { label: "filter commits…", detail: "/", run: () => openCommitFilter() },
    { label: "refresh", detail: "r", run: () => manualRefresh() },
    { label: "switch repo…", detail: "", run: null }, // drills into repo mode (runPaletteRow)
    // The TUI palette's Open repo twin: a typed path (custom, ~-expandable
    // server-side) instead of the known-repos picker. doReroot posts it and
    // reloads; the server preflights before swapping, so a bad path is just
    // an error line and the current repo keeps serving.
    { label: "open repo (path)…", detail: "", run: () => openPrompt({ title: "Open repo — path", placeholder: "/path/to/repo or ~/repo", onSubmit: (p) => doReroot(p) }) },
    { label: "open working tree", detail: "", run: () => openWorkingTree(0) }, // 0 = the WT row; a bare call would set state.cursor = undefined and break j/k/enter
    { label: "toggle sidebar", detail: "t", run: () => toggleSidebar() },
    { label: "toggle graph", detail: "g", run: () => toggleGraphMode() },
    { label: "toggle stacked diff", detail: "S", run: () => toggleStacked() },
    { label: "help", detail: "?", run: () => openHelp() },
  ];
  // A feature preflight turned off has nothing to open — the entry point is
  // removed rather than left to fail on click.
  return featureDisabled("versions") ? rows.filter((r) => r.label !== "branch versions…") : rows;
}


function openPalette(mode, fromCmd) {
  const already = !!pal;
  pal = { mode, fromCmd: !!fromCmd, rows: [], filtered: [], sel: 0, gen: ++palGen, grouped: !!(state.ui && state.ui.repo_grouped) };
  if (!already) pushLayer("palette", $("palette"), { onKey: paletteKey });
  $("palette-input").value = "";
  $("palette-input").placeholder = mode === "repo" ? "type a repo, branch or path…" : "type a command…";
  // Repo mode sizes the box to its table; command mode is the fixed-width
  // list. Reset on every open — Escape from repo mode re-enters cmd mode
  // through here, and the inline width would otherwise stick.
  $("palette-box").style.width = "";
  $("palette-box").style.maxWidth = "";
  $("palette-list").style.removeProperty("--repo-cols");
  $("palette-list").classList.remove("repo");
  renderRepoHint();
  if (mode === "cmd") {
    pal.rows = paletteCommands();
    filterPalette();
  } else {
    renderPalette([{ label: "loading…", empty: true }]);
    getJSON("/api/repos")
      .then((j) => {
        if (!pal || pal.mode !== "repo") return; // closed or switched meanwhile
        // The SERVER says which row is the repo it is serving. Comparing
        // paths here looked equivalent and was not: /api/repo reports git's
        // forward-slash top-level while the registry stores platform-cleaned
        // paths, so on Windows the served repo never matched and stayed in
        // the list — picking it re-rooted onto the repo already open.
        pal.rows = (j.repos || [])
          .filter((r) => !r.current)
          .map((r) => ({ label: r.name, path: r.path, branch: "", slow: false, pending: true, age: ageString(r.last_opened), project: r.project || null }));
        layoutRepoTable();
        filterPalette();
        pollRepoDetails(pal.gen, 0);
      })
      .catch((e) => {
        closePalette();
        opLine("error: " + (e.message || e), true);
      });
  }
  $("palette-input").focus();
}


// ---- repo mode: the switcher table ---------------------------------------
// Five columns — branch · name · slow-fs · path · age — each starting at one
// shared column, the TUI's R switcher laid out with the room a browser has.
// The list paints from /api/repos alone; branch and slow-fs verdicts land
// later from /api/repos/details (a checkout on a hung mount would otherwise
// hold the whole list) and patch rows in place, cursor kept.

let palGen = 0; // bumps per open, so a poll from a previous open is dropped
const SLOW_FS = "(slow fs)";
const BRANCH_MAX = 32;

// ageString is the TUI's ageString: a coarse relative age for the row.
function ageString(iso) {
  const t = Date.parse(iso);
  if (!iso || Number.isNaN(t)) return "";
  const d = Math.max(0, Date.now() - t);
  const m = Math.floor(d / 60000);
  if (m < 1) return "just now";
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  return `${Math.floor(h / 24)}d ago`;
}


// layoutRepoTable computes the column widths over ALL rows (not the filtered
// view, so the table holds still while a filter narrows it) and sizes the
// box: as wide as the table needs, floored at the command palette's width
// and capped near the viewport edge. When the cap bites, the PATH column
// gives — each path is cut from the middle by elidePath (leaf, its parent
// and the root survive; one "…" marks the dropped run), never from the
// right by CSS. The slow-fs column is always reserved so verdicts landing
// later never shift the paths.
function layoutRepoTable() {
  const rows = pal.rows;
  const w = (s) => runes(s || "").length;
  const cols = {
    branch: Math.min(BRANCH_MAX, Math.max(w("…"), ...rows.map((r) => w(r.branch)))),
    // A group head shows its project's name instead: sizing for both keeps
    // the columns still across the ctrl+g toggle.
    name: Math.max(1, ...rows.map((r) => Math.max(w(r.label), w(r.project && r.project.label)))),
    slow: w(SLOW_FS),
    path: Math.max(1, ...rows.map((r) => w(r.path))),
    age: Math.max(1, ...rows.map((r) => w(`(${r.age})`))),
  };
  // PAD: the row's 14px side padding twice, plus the list's always-reserved
  // scrollbar gutter (scrollbar-gutter: stable in repo mode) so a long
  // registry's scrollbar never eats the age column.
  const GAP = 2, GAPS = 4, PAD = 28 + 16, BORDER = 2;
  const cw = charWidth();
  const fixed = cols.branch + cols.name + cols.slow + cols.age + GAP * GAPS;
  const maxPx = Math.floor(window.innerWidth * 0.95);
  const minPx = Math.min(560, maxPx);
  let px = Math.ceil((fixed + cols.path) * cw) + PAD + BORDER;
  if (px > maxPx) {
    // The path is the column this table exists for: it keeps at least half
    // of what the slow-fs and age columns leave, and branch + name share
    // the rest (each cut by the same elision), rather than starving it.
    const avail = Math.floor((maxPx - PAD - BORDER) / cw) - GAP * GAPS - cols.slow - cols.age;
    cols.path = Math.max(1, avail - cols.branch - cols.name, Math.floor(avail / 2));
    const share = Math.max(2, avail - cols.path);
    cols.branch = Math.min(cols.branch, Math.floor(share / 2));
    cols.name = Math.max(1, Math.min(cols.name, share - cols.branch));
    px = maxPx;
  }
  pal.cols = cols;
  for (const r of rows) elideRepoRow(r);
  $("palette-list").classList.add("repo");
  $("palette-box").style.width = Math.max(minPx, px) + "px";
  $("palette-box").style.maxWidth = "none"; // the stylesheet caps the cmd list at 560px
  $("palette-list").style.setProperty(
    "--repo-cols",
    `${cols.branch}ch ${cols.name}ch ${cols.slow}ch ${cols.path}ch ${cols.age}ch`
  );
}


// elideRepoRow cuts one row's cells to the current column widths. The name
// cell depends on the row's neighbours when grouped, so filterPalette cuts it.
function elideRepoRow(r) {
  const cols = pal.cols;
  r.pathText = elidePath(r.path, cols.path);
  r.branchText = elidePath(r.branch, cols.branch);
}


// ---- repo mode: grouped by project (ctrl+g) -------------------------------
// The TUI switcher's ctrl+g. The SERVER says which project each row belongs
// to (repos.Projects: a checkout with its linked worktrees through their git
// common dir, clones through their remote name) — /api/repos from the remote
// names at once, /api/repos/details once the common dirs are read. Here the
// filtered rows are only reordered (repos.Group) and named: a group's head
// shows the project, the rows under it a blank name, and a project left with
// one row is no group and keeps its directory name. The choice is remembered
// in the server-side UI state (repo_grouped).

// groupRepoRows pulls each project's later rows up under its first (MRU) one.
function groupRepoRows(rows) {
  const out = [];
  const seen = new Set();
  rows.forEach((r, i) => {
    const key = r.project && r.project.key;
    if (!key) { out.push(r); return; }
    if (seen.has(key)) return; // already pulled up under its head
    seen.add(key);
    out.push(r);
    for (const later of rows.slice(i + 1)) if (later.project && later.project.key === key) out.push(later);
  });
  return out;
}


// repoRowName is the name cell of row i in the displayed rows.
function repoRowName(rows, i) {
  const r = rows[i];
  if (!pal.grouped || !r.project) return r.label;
  const same = (j) => j >= 0 && j < rows.length && !!rows[j].project && rows[j].project.key === r.project.key;
  if (same(i - 1)) return "";
  if (same(i + 1)) return r.project.label;
  return r.label;
}


function toggleRepoGrouped() {
  pal.grouped = !pal.grouped;
  saveUI({ repo_grouped: pal.grouped });
  renderRepoHint();
  refilterKeepingCursor();
}


// renderRepoHint shows the repo table's key line (repo mode only): the
// grouping toggle, clickable, and whether the list is grouped now.
function renderRepoHint() {
  const hint = $("palette-hint");
  const on = !!pal && pal.mode === "repo";
  hint.classList.toggle("hidden", !on);
  if (!on) return;
  hint.innerHTML = `<button type="button" data-act="group">ctrl+g ${pal.grouped ? "flat list" : "group by project"}</button>` +
    (pal.grouped ? `<span class="on">grouped</span>` : "") +
    `<span>enter switch</span><span>esc back</span>`;
}


// pollRepoDetails fetches the verdicts and re-polls with backoff while any
// row is still pending (capped: a mount that never answers leaves its cells
// blank rather than polling forever). The table is laid out again ONCE, when
// the first verdicts land (the branch column has its width then); a
// straggler from a later poll is cut to that width rather than moving the
// columns under the user's cursor a second time.
function pollRepoDetails(gen, n) {
  if (!pal || pal.mode !== "repo" || pal.gen !== gen) return;
  getJSON("/api/repos/details")
    .then((j) => {
      if (!pal || pal.mode !== "repo" || pal.gen !== gen) return;
      const byPath = new Map((j.repos || []).map((d) => [d.path, d]));
      let pending = false;
      for (const r of pal.rows) {
        const d = byPath.get(r.path);
        if (!d) { r.pending = false; continue; }
        r.pending = !!d.pending;
        r.project = d.project || null; // the common dirs can regroup the list
        if (!r.pending) { r.branch = d.branch || ""; r.slow = !!d.slow; }
        pending ||= r.pending;
      }
      if (n === 0) layoutRepoTable();
      else for (const r of pal.rows) elideRepoRow(r);
      refilterKeepingCursor();
      if (pending && n < 8) setTimeout(() => pollRepoDetails(gen, n + 1), Math.min(3000, 300 * 1.6 ** n));
    })
    .catch(() => {}); // the list is already usable without the verdicts
}


// refilterKeepingCursor re-applies the query after a patch, keeping the
// cursor on the same ROW (filterPalette resets it to the top, which would
// jump the cursor every time a verdict lands).
function refilterKeepingCursor() {
  const cur = pal.filtered[pal.sel];
  filterPalette();
  if (cur) {
    const i = pal.filtered.indexOf(cur);
    if (i >= 0) pal.sel = i;
  }
  renderPalette(pal.filtered.length ? pal.filtered : [{ label: pal.mode === "repo" ? "no other repos" : "no match", empty: true }]);
}


function repoRowHTML(r, i) {
  const sel = i === pal.sel ? " sel" : "";
  const branch = r.pending ? "…" : r.branchText || "";
  const title = r.pathText !== r.path ? ` title="${esc(r.path)}"` : "";
  return `<li class="repo${sel}" data-i="${i}">` +
    `<span class="rbranch${r.pending ? " dim" : ""}">${esc(branch)}</span>` +
    `<span class="rname">${esc(r.nameText ?? r.label)}</span>` +
    `<span class="rslow">${r.slow ? SLOW_FS : ""}</span>` +
    `<span class="rpath"${title}>${esc(r.pathText || r.path)}</span>` +
    `<span class="rage">(${esc(r.age)})</span></li>`;
}


function closePalette() {
  closeLayer("palette");
  $("palette-input").blur();
  pal = null;
}


function filterPalette() {
  if (!pal) return;
  const q = $("palette-input").value.trim().toLowerCase();
  pal.filtered = pal.rows.filter(
    (r) => !q || r.label.toLowerCase().includes(q) || (r.detail || "").toLowerCase().includes(q) ||
      (r.path || "").toLowerCase().includes(q) || (r.branch || "").toLowerCase().includes(q)
  );
  if (pal.mode === "repo") {
    // Filter first, then group what is left (the TUI's order).
    if (pal.grouped) pal.filtered = groupRepoRows(pal.filtered);
    const nameW = pal.cols ? pal.cols.name : 0;
    pal.filtered.forEach((r, i) => {
      const name = repoRowName(pal.filtered, i);
      r.nameText = nameW ? elidePath(name, nameW) : name;
    });
  }
  pal.sel = 0;
  renderPalette(pal.filtered.length ? pal.filtered : [{ label: pal.mode === "repo" ? "no other repos" : "no match", empty: true }]);
}


function renderPalette(rows) {
  $("palette-list").innerHTML = rows
    .map((r, i) =>
      r.empty
        ? `<li class="empty">${esc(r.label)}</li>`
        : r.path
          ? repoRowHTML(r, i)
          : `<li data-i="${i}"${i === pal.sel ? ' class="sel"' : ""}><span>${esc(r.label)}</span><span class="detail">${esc(r.detail || "")}</span></li>`
    )
    .join("");
}


function runPaletteRow(row) {
  if (!row) return;
  if (pal.mode === "repo") {
    const path = row.path;
    closePalette();
    doReroot(path);
    return;
  }
  if (row.label === "switch repo…") {
    openPalette("repo", true);
    return;
  }
  const run = row.run;
  closePalette();
  run();
}


function paletteKey(e) {
  if (!pal) return false;
  if (e.key === "ArrowDown" || e.key === "ArrowUp") {
    const n = pal.filtered.length;
    if (n) {
      pal.sel = Math.min(n - 1, Math.max(0, pal.sel + (e.key === "ArrowDown" ? 1 : -1)));
      renderPalette(pal.filtered);
    }
    e.preventDefault();
    return true;
  }
  if (e.key === "Enter") {
    runPaletteRow(pal.filtered[pal.sel]);
    e.preventDefault();
    return true;
  }
  if (e.key === "Escape") {
    if (pal.mode === "repo" && pal.fromCmd) openPalette("cmd");
    else closePalette();
    e.preventDefault();
    return true;
  }
  if (e.key === "Tab") {
    e.preventDefault();
    return true;
  }
  if (pal.mode === "repo" && e.ctrlKey && !e.altKey && !e.metaKey && e.key.toLowerCase() === "g") {
    toggleRepoGrouped(); // preventDefault also keeps the browser's find-next off
    e.preventDefault();
    return true;
  }
  return false; // typing lands in the focused input; its input event re-filters
}


$("palette-input").addEventListener("input", filterPalette);

$("palette").addEventListener("click", closePalette); // backdrop

$("palette-box").addEventListener("click", (e) => e.stopPropagation());

$("palette-hint").addEventListener("click", (e) => {
  if (!pal || !e.target.closest('button[data-act="group"]')) return;
  toggleRepoGrouped();
  $("palette-input").focus(); // typing keeps filtering after a click
});

$("palette-list").addEventListener("click", (e) => {
  const li = e.target.closest("li[data-i]");
  if (li && pal) runPaletteRow(pal.filtered[Number(li.dataset.i)]);
});


function openGlobalMenu() {
  const r = $("menu-btn").getBoundingClientRect();
  // Six labelled groups in a FIXED authored order (user-specified layout —
  // not sorted): a new row picks its group and its place in it. Rows whose
  // feature lives in another module are imported here so the whole menu
  // reads top-to-bottom in one place; the extraRows("menu") hook remains for
  // rows this file does not know about, appended before help.
  const rows = [
    { header: "Actions" },
    { label: "pull", act: () => doPull() },
    { label: "push", act: () => doPush() },
    { label: "review working changes (AI)…", act: () => startReview("working", "") },
    { label: "undo last commit", act: () => undoLastCommit() },
    { label: "apply a patch…", act: () => applyPatchPrompt() },
    {
      label: "copy a bookmark or shelf entry to a directory…",
      // The ☰ button is the anchor this menu opened from, so the picker
      // lands under it rather than at the pointer.
      act: () => {
        const b = $("menu-btn").getBoundingClientRect();
        pickEntryToCopy(b.left, b.bottom + 4);
      },
    },
    // Stashing is gated like the mass staging rows: while a sequencer op is
    // paused the working tree belongs to it, and git refuses to stash
    // mid-merge. openStashPick keeps its own check as the backstop.
    ...(state.wt && !state.conflict ? [{ label: "stash a selection…", act: () => openStashPick(null) }] : []),
    // state.rows is commits only, so a non-empty feed IS "there is something
    // to amend" — the TUI's canAmend gate.
    ...((state.rows || []).length ? [{ label: "amend the last commit…", act: () => startAmend() }] : []),
    { header: "Branches" },
    { label: "branch prefixes…", act: () => openPrefixesView() },
    // A feature preflight turned off has nothing to open — the entry point
    // is removed rather than left to fail on click.
    ...(featureDisabled("versions") ? [] : [{ label: "branch versions…", act: () => openVersionBranches() }]),
    { label: "create branch…", act: () => openCreateBranchPrompt() },
    { label: "go to current branch", act: () => locateCurrentBranch() },
    { label: "fetch all remotes", act: () => doFetch() },
    { label: "browse remote branches…", act: () => openRemoteHeads() },
    { label: "prune remotes (drop deleted branches)", act: () => startOp({ op: "prune" }, "pruning remotes") },
    { header: "Search" },
    { label: "filter the commit list… (\\)", act: () => openFeedFilter() },
    { label: "files in the working tree… (F)", act: () => openFinder() },
    { header: "Repositories" },
    { label: "switch repo…", act: () => openPalette("repo") },
    { header: "UI" },
    { label: "command palette…", act: () => openPalette("cmd") },
    { label: "refresh", act: () => manualRefresh() },
    { label: "session errors…", act: () => openSessionErrorsView() },
    { label: "settings…", act: () => openSettings() },
    { label: "toggle file list", act: () => toggleFilesHidden() },
    { label: "toggle graph", act: () => toggleGraphMode() },
    { label: "toggle sidebar", act: () => toggleSidebar() },
    { label: "toggle stacked diff (S)", act: () => toggleStacked() },
    { header: "Config" },
    { label: "identity & profiles…", act: () => openIdentityView() },
    { label: "agent setup…", act: () => openAgentSetup() },
    { label: "external tools…", act: () => openExtToolsView() },
    { label: "git config…", act: () => openGitConfig() },
    ...extraRows("menu", null),
    { sep: true },
    { label: "help", act: () => openHelp() },
  ];
  showCtxMenu(rows, r.left, r.bottom + 4);
}


$("menu-btn").addEventListener("click", (e) => {
  // stopPropagation: the document-level outside-click closer would otherwise
  // see this same click and close the menu the moment it opens.
  e.stopPropagation();
  const t = topLayer();
  if (t && t.id === "ctx") { hideCtxMenu(); return; } // second click toggles closed
  openGlobalMenu();
});


// undoLastCommit moves HEAD back one commit and leaves the work staged — the
// TUI's `u`. The engine refuses unless the last reflog entry really was a
// commit, so this cannot quietly unwind a merge or a reset; the confirm is
// here because the ☰ menu is one click away from everything.
function undoLastCommit() {
  showLocalConfirm(
    "Undo the last commit? The commit is removed and its changes are left staged.",
    ["undo", "abort"],
    (o) => { if (o === "undo") startOp({ op: "undo-last-commit" }, "undoing the last commit"); }
  );
}


export { closePalette, filterPalette, openGlobalMenu, openPalette, pal, paletteCommands, paletteKey, renderPalette, runPaletteRow };
