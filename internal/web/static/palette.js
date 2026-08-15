// palette.js — part of gg's web client. Split from the original app.js;
// see app.js (the entry module) for the load order.
import { $, esc, getJSON, state } from "./core.js";
import { closeLayer, hideCtxMenu, openPrompt, pushLayer, showCtxMenu, topLayer } from "./layers.js";
import { doFetch, doPull, doPush, doReroot, opLine, openCreateBranchPrompt, openHelp, refreshAfterOp, startOp, toggleSidebar } from "./ops.js";
import { openVersionBranches } from "./versions.js";
import { openFileBlame, openFileHistory } from "./filehist.js";
import { openSettings } from "./settings.js";
import { openIdentityView } from "./identity.js";
import { openPrefixesView } from "./prefixes.js";
import { openExtToolsView } from "./exttools.js";
import { openSessionErrorsView } from "./sessionerrors.js";
import { startReview } from "./review.js";
import { gotoCommitPrompt, openCommitFilter, toggleGraphMode } from "./commits.js";
import { openWorkingTree } from "./files.js";

// ---- command palette + global ☰ menu (wave 3) ----------------------------
// The palette is a layer with an input INSIDE it: onKey consumes nav keys
// (returns true) and returns false for everything else, so the browser
// delivers the keystroke to the focused input while the router's
// non-empty-stack short-circuit keeps global keys off. Every close path MUST
// go through closePalette() — the input.blur() is load-bearing: a focused
// input after close would trap all global keys in the form-field guard.

let pal = null; // {mode: "cmd"|"repo", fromCmd, rows, filtered, sel}


function paletteCommands() {
  return [
    { label: "pull", detail: "p", run: () => doPull() },
    { label: "push", detail: "P", run: () => doPush() },
    { label: "fetch all remotes", detail: "", run: () => doFetch() },
    { label: "prune remotes (drop deleted branches)", detail: "", run: () => startOp({ op: "prune" }, "pruning remotes") },
    { label: "settings…", detail: "", run: () => openSettings() },
    { label: "identity & profiles…", detail: "", run: () => openIdentityView() },
    { label: "branch prefixes…", detail: "", run: () => openPrefixesView() },
    { label: "external tools…", detail: "", run: () => openExtToolsView() },
    { label: "session errors…", detail: "", run: () => openSessionErrorsView() },
    { label: "create branch…", detail: "", run: () => openCreateBranchPrompt() },
    { label: "branch versions…", detail: "", run: () => openVersionBranches() },
    { label: "file history…", detail: "", run: () => openPrompt({ title: "File history — repo-relative path", placeholder: "e.g. internal/web/server.go", onSubmit: (p) => openFileHistory(p, "") }) },
    { label: "file blame…", detail: "", run: () => openPrompt({ title: "File blame — repo-relative path", placeholder: "e.g. internal/web/server.go", onSubmit: (p) => openFileBlame(p, "") }) },
    { label: "review working changes (AI)…", detail: "", run: () => startReview("working", "") },
    { label: "review this branch (AI)…", detail: "", run: () => startReview("branch", "") },
    { label: "goto commit…", detail: "#", run: () => gotoCommitPrompt() },
    { label: "filter commits…", detail: "/", run: () => openCommitFilter() },
    { label: "refresh", detail: "r", run: () => { if (!state.op) refreshAfterOp(); } },
    { label: "switch repo…", detail: "", run: null }, // drills into repo mode (runPaletteRow)
    // The TUI palette's Open repo twin: a typed path (custom, ~-expandable
    // server-side) instead of the known-repos picker. doReroot posts it and
    // reloads; the server preflights before swapping, so a bad path is just
    // an error line and the current repo keeps serving.
    { label: "open repo (path)…", detail: "", run: () => openPrompt({ title: "Open repo — path", placeholder: "/path/to/repo or ~/repo", onSubmit: (p) => doReroot(p) }) },
    { label: "open working tree", detail: "", run: () => openWorkingTree(0) }, // 0 = the WT row; a bare call would set state.cursor = undefined and break j/k/enter
    { label: "toggle sidebar", detail: "b", run: () => toggleSidebar() },
    { label: "toggle graph", detail: "g", run: () => toggleGraphMode() },
    { label: "help", detail: "?", run: () => openHelp() },
  ];
}


function openPalette(mode, fromCmd) {
  const already = !!pal;
  pal = { mode, fromCmd: !!fromCmd, rows: [], filtered: [], sel: 0 };
  if (!already) pushLayer("palette", $("palette"), { onKey: paletteKey });
  $("palette-input").value = "";
  $("palette-input").placeholder = mode === "repo" ? "type a repo name…" : "type a command…";
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
          .map((r) => ({ label: r.name, detail: r.path, path: r.path }));
        filterPalette();
      })
      .catch((e) => {
        closePalette();
        opLine("error: " + (e.message || e), true);
      });
  }
  $("palette-input").focus();
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
    (r) => !q || r.label.toLowerCase().includes(q) || (r.detail || "").toLowerCase().includes(q)
  );
  pal.sel = 0;
  renderPalette(pal.filtered.length ? pal.filtered : [{ label: pal.mode === "repo" ? "no other repos" : "no match", empty: true }]);
}


function renderPalette(rows) {
  $("palette-list").innerHTML = rows
    .map((r, i) =>
      r.empty
        ? `<li class="empty">${esc(r.label)}</li>`
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
  return false; // typing lands in the focused input; its input event re-filters
}


$("palette-input").addEventListener("input", filterPalette);

$("palette").addEventListener("click", closePalette); // backdrop

$("palette-box").addEventListener("click", (e) => e.stopPropagation());

$("palette-list").addEventListener("click", (e) => {
  const li = e.target.closest("li[data-i]");
  if (li && pal) runPaletteRow(pal.filtered[Number(li.dataset.i)]);
});


function openGlobalMenu() {
  const r = $("menu-btn").getBoundingClientRect();
  // Two labelled groups — git operations, then the UI's own controls — each
  // sorted at render so a future entry cannot land unsorted within its
  // group; a new row must pick its group, nothing else. help sits alone
  // below a separator — the one fixed anchor.
  const git = [
    { label: "pull", act: () => doPull() },
    { label: "push", act: () => doPush() },
    { label: "fetch all remotes", act: () => doFetch() },
    { label: "prune remotes (drop deleted branches)", act: () => startOp({ op: "prune" }, "pruning remotes") },
    { label: "create branch…", act: () => openCreateBranchPrompt() },
    { label: "branch versions…", act: () => openVersionBranches() },
    { label: "identity & profiles…", act: () => openIdentityView() },
    { label: "branch prefixes…", act: () => openPrefixesView() },
    { label: "external tools…", act: () => openExtToolsView() },
    { label: "review working changes (AI)…", act: () => startReview("working", "") },
  ].sort((a, b) => a.label.localeCompare(b.label));
  // Switching the repo is neither a git operation nor a UI control, so it
  // gets its own group between them.
  const repositories = [
    { label: "switch repo…", act: () => openPalette("repo") },
  ];
  const ui = [
    { label: "refresh", act: () => { if (!state.op) refreshAfterOp(); } },
    { label: "command palette…", act: () => openPalette("cmd") },
    { label: "toggle sidebar", act: () => toggleSidebar() },
    { label: "toggle graph", act: () => toggleGraphMode() },
    { label: "settings…", act: () => openSettings() },
    { label: "session errors…", act: () => openSessionErrorsView() },
  ].sort((a, b) => a.label.localeCompare(b.label));
  const rows = [{ header: "git" }, ...git, { header: "repositories" }, ...repositories, { header: "ui" }, ...ui, { sep: true }, { label: "help", act: () => openHelp() }];
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

export { closePalette, filterPalette, openGlobalMenu, openPalette, pal, paletteCommands, paletteKey, renderPalette, runPaletteRow };
