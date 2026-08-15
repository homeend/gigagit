// files.js — part of gg's web client. Split from the original app.js;
// see app.js (the entry module) for the load order.
import { $, esc, getJSON, postJSON, runes, state } from "./core.js";
import { copyText, showCtxMenu } from "./layers.js";
import { addFileEntry } from "./sidebar.js";
import { applyStatus, fetchStatus } from "./status.js";
import { opLine, showLocalConfirm, startOp } from "./ops.js";
import { openFileBlame, openFileHistory } from "./filehist.js";
import { rev } from "./review.js";
import { renderCommits } from "./commits.js";
import { focusPane, moveCursor } from "./keys.js";

// reconcileStatusView keeps an open status screen truthful after any
// status re-read (op done, r, tab focus): the tree may have gone clean or
// shrunk under it.
function reconcileStatusView() {
  if (state.filesMode !== "status") return;
  if (!state.wt) exitStatusToList();
  else {
    state.fileCursor = Math.min(state.fileCursor, Math.max(0, state.statusEntries.length - 1));
    renderFiles();
  }
  // a status re-read can invalidate an open hunk view (file fully staged or
  // gone): exit rather than offer stale positional picks
  if (diffHunks && !state.statusEntries.some((f) => f.path === diffHunks.path && hunkEligible(f))) clearDiffHunks();
  if (conflictPick && !state.statusEntries.some((f) => f.path === conflictPick.path && f.section === "conflicts")) {
    conflictPick = null;
    renderResolveBar();
  }
}


// drillOut steps ONE stage back — diff → file list → full-width commit
// list. The esc key, the ← back button, and the footer chip all share it.
function drillOut() {
  if (state.layout === "diff") {
    enterFilesStage(); // also clears the diff a late fetch may repaint
    focusPane();
    return;
  }
  if (state.layout !== "files") return;
  state.detailGen++; // invalidate any in-flight detail fetch
  state.pane = "commits";
  setLayout("list");
  focusPane();
}

$("back-btn").addEventListener("click", drillOut);


// --- files + diff panes ---

// Staged layout (the GitKraken flow): "list" = the commit list alone, full
// width; "files" = a commit is open — commits shrink left, the file list
// takes a fixed column on the right, NO diff yet; "diff" = a file is open —
// the diff replaces the commits area, file list stays right. esc steps one
// stage back (drillOut).
function setLayout(mode) {
  state.layout = mode;
  const p = $("panes");
  p.classList.toggle("solo", mode === "list");
  p.classList.toggle("files", mode === "files");
  p.classList.toggle("detail", mode === "diff");
  if (mode === "list") moveCursor(0); // re-render + rescroll: display:none dropped the scroll position
}


// enterFilesStage swaps to the file-list stage: browse the changed files
// with the commit list still alongside, nothing auto-opened. The diff pane
// is cleared so a later stage-3 entry never flashes the previous drill's
// diff, and a window resize cannot resurrect it through state.lastDiff.
function enterFilesStage() {
  state.pane = "files";
  setLayout("files");
  $("diff-title").textContent = "";
  $("diff-body").innerHTML = "";
  state.lastDiff = null;
  state.diffCtx = null;
}


// --- branch ↔ branch comparison ---
//
// Opens the same detail screen a commit uses, over the whole tip-to-tip
// changed-file list. Each file's per-side diff runs against the two TIP
// HASHES the server resolved (never the branch names — see compare.go), so
// the diff cache cannot serve a stale side after a commit.
//
// The rev form (opts.revs): a and b are hex commit ids sent with revs=1 —
// no server-side branch resolution — and opts.aLabel/bLabel supply the
// display names, since a bare hash is unreadable in the header and the
// origin-filter buttons. state.compare.a/b hold the LABELS (display-only
// after the fetch; every git-facing consumer reads aHash/bHash).
async function openCompare(a, b, opts) {
  const o = opts || {};
  const gen = ++state.detailGen;
  let body;
  try {
    body = await getJSON(
      "/api/compare?a=" + encodeURIComponent(a) + "&b=" + encodeURIComponent(b) + (o.revs ? "&revs=1" : "")
    );
  } catch (e) {
    opLine("compare failed: " + (e.message || e), true);
    return;
  }
  if (gen !== state.detailGen) return; // superseded by a newer open or esc
  state.compare = {
    a: o.aLabel || a,
    b: o.bLabel || b,
    aHash: body.a_hash,
    bHash: body.b_hash,
    all: body.files || [],
    filter: "all",
    originsError: body.origins_error || "",
  };
  state.filesMode = "compare";
  state.fileSha = null;
  enterFilesStage();
  $("files-header").textContent = state.compare.a + " ↔ " + state.compare.b;
  applyCompareFilter();
  focusPane();
}


// The origin filter (the TUI's f key): "all", or only the files one side
// touched since the two diverged. A file both sides touched stays in both
// filtered views — the TUI's filterCompareFiles rule.
function applyCompareFilter() {
  const c = state.compare;
  state.files =
    c.filter === "all" ? c.all : c.all.filter((f) => f.origin === c.filter || f.origin === "both");
  state.fileCursor = 0;
  renderFiles();
  updateDiffNav();
  if (!state.files.length) {
    // The empty state must live in the FILE LIST: in the files stage the
    // diff pane is not on screen, and stepping back there from an open
    // diff must not strand a stale one.
    $("files-list").innerHTML = `<li class="sect">${
      c.all.length ? "no files match this filter" : "the two branches are identical"
    }</li>`;
    if (state.layout === "diff") drillOut();
    return;
  }
  // Filtering under an open diff keeps a diff open (the pre-staged-layout
  // behavior); in the files stage nothing auto-opens.
  if (state.layout === "diff") openFile(0);
}


function renderCompareBar() {
  const bar = $("compare-bar");
  const c = state.filesMode === "compare" ? state.compare : null;
  if (!c) {
    bar.classList.add("hidden");
    bar.innerHTML = "";
    return;
  }
  bar.classList.remove("hidden");
  // Without a merge base there are no origin sets, so only "all" is
  // meaningful — the comparison itself still stands (compare.go).
  const off = c.originsError
    ? ` disabled title="${esc(c.originsError)} — the per-side filter needs a merge base"`
    : "";
  const rows = [
    ["all", "all (" + c.all.length + ")", ""],
    ["a", "only " + c.a, off],
    ["b", "only " + c.b, off],
  ];
  bar.innerHTML = rows
    .map(
      ([key, label, attrs]) =>
        `<button data-f="${key}"${c.filter === key ? ' class="on"' : ""}${attrs}>${esc(label)}</button>`
    )
    .join("");
}


$("compare-bar").addEventListener("click", (e) => {
  const btn = e.target.closest("button[data-f]");
  if (!btn || !state.compare) return;
  state.compare.filter = btn.dataset.f;
  applyCompareFilter();
});


async function openWorkingTree(i) {
  state.cursor = i;
  renderCommits();
  await fetchStatus(); // refresh on open — external changes since boot
  if (!state.wt) {
    renderCommits();
    return;
  }
  state.filesMode = "status";
  state.fileCursor = 0;
  enterFilesStage();
  $("files-header").textContent = "Working tree";
  renderFiles();
  focusPane();
}


const SECTION_LABELS = { staged: "Staged", changes: "Changes", untracked: "Untracked", conflicts: "Conflicts" };


function renderFiles() {
  // Driven off filesMode, not off state.compare, so the bar cannot linger
  // into the next commit's detail screen.
  renderCompareBar();
  if (state.filesMode !== "status") {
    $("files-actions").classList.add("hidden");
    $("commit-box").classList.add("hidden");
    $("conflict-note").classList.add("hidden");
    $("files-list").innerHTML = state.files
      .map(
        (f, i) =>
          `<li class="${i === state.fileCursor ? "sel" : ""}" data-i="${i}">` +
          `<span class="st ${esc(f.status)}">${esc(f.status)}</span>${esc(f.path)}</li>`
      )
      .join("");
    return;
  }
  // While a sequencer op is paused, the commit box AND the mass staging
  // buttons step aside: finishing the op is the banner's Continue (git
  // supplies the merge message), "stage all" would mark every conflict
  // resolved with the markers still inside the files, and "unstage all"
  // would pull git's auto-merged results back out of the coming merge
  // commit. Per-file actions (mark resolved) stay available.
  if (state.conflict) {
    $("files-actions").classList.add("hidden");
    $("commit-box").classList.add("hidden");
    const note = $("conflict-note");
    note.classList.remove("hidden");
    note.textContent = state.conflict.conflicted
      ? "resolving " + state.conflict.op + " — pick through the conflicts below, then press Continue above"
      : "all conflicts resolved — press Continue above to finish the " + state.conflict.op;
  } else {
    $("files-actions").classList.remove("hidden");
    $("conflict-note").classList.add("hidden");
    $("commit-box").classList.remove("hidden");
    $("commit-btn").disabled = !(state.wt && state.wt.counts.staged > 0) || !!state.op;
    $("stash-btn").disabled = !state.wt || !!state.op;
  }
  let html = "";
  let lastSection = "";
  state.statusEntries.forEach((f, i) => {
    if (f.section !== lastSection) {
      html += `<li class="sect">${SECTION_LABELS[f.section]}</li>`;
      lastSection = f.section;
    }
    const badge = f.section === "staged" ? f.staged : f.unstaged;
    const btn =
      f.section === "conflicts"
        ? ""
        : f.section === "staged"
          ? `<button class="act" data-i="${i}" data-un="1">u</button>`
          : `<button class="act" data-i="${i}">s</button>`;
    html +=
      `<li class="${i === state.fileCursor ? "sel" : ""} ${f.section}${state.marked.has(f.path) ? " marked" : ""}" data-i="${i}">` +
      `<span class="st">${esc(badge)}</span>${esc(f.path)}${btn}</li>`;
  });
  $("files-list").innerHTML = html;
}


async function openFile(i) {
  clearDiffHunks();
  // The layout switch sits in the SYNC prefix: an esc during a slow diff
  // load steps back to the files stage, and the fetch completing later
  // must not be able to undo that.
  if (state.layout !== "diff") {
    state.pane = "files";
    setLayout("diff");
    focusPane();
  }
  state.fileCursor = i;
  renderFiles();
  updateDiffNav();
  if (state.filesMode === "status") return openStatusDiff(i);
  const f = state.files[i];
  state.diffCtx = { path: f.path, rev: state.filesMode === "compare" ? state.compare.bHash : f.sha || state.fileSha };
  const q = new URLSearchParams({ path: f.path, status: f.status });
  if (state.filesMode === "compare") {
    q.set("left", state.compare.aHash);
    q.set("right", state.compare.bHash);
  } else {
    q.set("sha", f.sha || state.fileSha);
  }
  if (f.old_path) q.set("old", f.old_path);
  $("diff-title").textContent = f.path;
  $("diff-body").innerHTML = `<div class="notice">loading…</div>`;
  updateDiffNav();
  try {
    renderDiff(await getJSON("/api/diff?" + q));
  } catch (e) {
    $("diff-body").innerHTML = `<div class="notice">error: ${esc(e.message || e)}</div>`;
    updateDiffNav();
  }
}


async function openStatusDiff(i) {
  clearDiffHunks();
  const f = state.statusEntries[i];
  state.diffCtx = f.section === "conflicts" ? null : { path: f.path, rev: "" };
  $("diff-title").textContent = f.path;
  if (f.section === "conflicts") return openConflictPicker(f);
  const q = new URLSearchParams({ wt: f.section === "staged" ? "staged" : "unstaged", path: f.path });
  if (f.orig_path) q.set("old", f.orig_path);
  $("diff-body").innerHTML = `<div class="notice">loading…</div>`;
  updateDiffNav();
  try {
    const d = await getJSON("/api/diff?" + q);
    // server tags eligible unstaged diffs with hunk ordinals — arm inline
    // staging BEFORE the render so the rows pick up their hk classes
    if (d.hunks && hunkEligible(f)) {
      diffHunks = { path: f.path, hash: d.hunks.hash, count: d.hunks.count, picks: new Set() };
    }
    renderDiff(d);
    renderHunkBar();
  } catch (e) {
    $("diff-body").innerHTML = `<div class="notice">error: ${esc(e.message || e)}</div>`;
    updateDiffNav();
  }
}


// exitStatusToList tears the status screen down to the full-width list —
// used when the working tree goes clean (all staged changes committed, or
// the last change unstaged away).
// toggleMark flips a status file's batch mark. Conflict rows are never
// markable: the batch rows stage/discard, and both are wrong answers to a
// conflict (mark-resolved stays a deliberate per-file act).
function toggleMark(path) {
  const f = state.statusEntries.find((x) => x.path === path);
  if (!f || f.section === "conflicts") return;
  if (state.marked.has(path)) state.marked.delete(path);
  else state.marked.add(path);
  renderFiles();
}

function exitStatusToList() {
  state.marked.clear();
  state.filesMode = "commit";
  state.pane = "commits";
  state.cursor = 0;
  $("files-list").innerHTML = "";
  $("files-actions").classList.add("hidden");
  $("commit-box").classList.add("hidden");
  $("conflict-note").classList.add("hidden");
  $("files-header").textContent = "";
  $("diff-title").textContent = "";
  $("diff-body").innerHTML = "";
  state.lastDiff = null; // a resize must not resurrect the cleared diff
  state.diffCtx = null;
  setLayout("list");
  focusPane();
}


async function stage(body) {
  try {
    applyStatus(await postJSON("/api/stage", body));
  } catch (e) {
    $("files-header").textContent = "error: " + (e.message || e);
    return;
  }
  if (!state.wt) {
    exitStatusToList();
    return;
  }
  state.fileCursor = Math.min(state.fileCursor, state.statusEntries.length - 1);
  renderFiles();
  renderCommits(); // badge counts changed
}


function markSpans(text, spans, side) {
  if (!spans || !spans.length) return esc(text);
  const rs = runes(text);
  let out = "";
  let pos = 0;
  for (const [a, b] of spans) {
    out += esc(rs.slice(pos, a).join(""));
    out += `<mark class="${side}">` + esc(rs.slice(a, b).join("")) + "</mark>";
    pos = b;
  }
  return out + esc(rs.slice(pos).join(""));
}


// hunkCls/hunkAttr decorate a diff row that belongs to a stageable hunk:
// the data-hunk tag drives click-to-select, the classes the highlight (a
// resize re-render keeps the current picks).
function hunkCls(r) {
  if (r.hunk == null || !diffHunks) return "";
  return " hk" + (diffHunks.picks.has(r.hunk) ? " picked" : "");
}


function hunkAttr(r) {
  return r.hunk == null || !diffHunks ? "" : ` data-hunk="${r.hunk}"`;
}


// diffHTML builds the diff table for a /api/diff response — shared by the
// main diff pane and the history overlay. paneWidth picks side-by-side vs
// unified exactly as before. Hunk classes no-op when diffHunks is null, so
// non-staging consumers get a plain read-only table.
function diffHTML(d, paneWidth) {
  if (d.binary) return `<div class="notice">binary file</div>`;
  if (d.too_large) return `<div class="notice">diff too large</div>`;
  const rows = d.rows || [];
  // An all-new or all-deleted file renders single-column: a side-by-side
  // with one permanently empty side wastes half the pane and forces harsh
  // wrapping on the populated half.
  const pureAdd = rows.length > 0 && rows.every((r) => !r.left_no);
  const pureDel = rows.length > 0 && rows.every((r) => !r.right_no);
  let html = `<table class="diff">`;
  if (pureAdd || pureDel) {
    const side = pureAdd ? "r" : "l";
    for (const r of rows) {
      const no = pureAdd ? r.right_no : r.left_no;
      const text = pureAdd ? r.right : r.left;
      const spans = pureAdd ? r.right_spans : r.left_spans;
      html +=
        `<tr class="${r.kind}${hunkCls(r)}"${hunkAttr(r)}>` +
        `<td class="no ${side}">${no || ""}</td>` +
        `<td class="side ${side}">${markSpans(text, spans, side)}</td></tr>`;
    }
  } else if (paneWidth < 950) {
    // Unified: below ~950px each side-by-side half is too narrow to read
    // (heavy wrapping, context text duplicated on both sides). One
    // full-width column; a changed pair becomes a del row then an add row,
    // keeping the intraline marks of each side.
    for (const r of rows) {
      if (r.kind === "same") {
        html +=
          `<tr class="same"><td class="no l">${r.left_no || ""}</td>` +
          `<td class="no r">${r.right_no || ""}</td>` +
          `<td class="side">${esc(r.right)}</td></tr>`;
      } else {
        if (r.kind !== "add")
          html +=
            `<tr class="del${hunkCls(r)}"${hunkAttr(r)}><td class="no l">${r.left_no || ""}</td><td class="no r"></td>` +
            `<td class="side l">${markSpans(r.left, r.left_spans, "l")}</td></tr>`;
        if (r.kind !== "del")
          html +=
            `<tr class="add${hunkCls(r)}"${hunkAttr(r)}><td class="no l"></td><td class="no r">${r.right_no || ""}</td>` +
            `<td class="side r">${markSpans(r.right, r.right_spans, "r")}</td></tr>`;
      }
    }
  } else {
    for (const r of rows) {
      html +=
        `<tr class="${r.kind}${hunkCls(r)}"${hunkAttr(r)}>` +
        `<td class="no l">${r.left_no || ""}</td>` +
        `<td class="side l">${markSpans(r.left, r.left_spans, "l")}</td>` +
        `<td class="no r">${r.right_no || ""}</td>` +
        `<td class="side r">${markSpans(r.right, r.right_spans, "r")}</td></tr>`;
    }
  }
  html += "</table>";
  if (d.truncated) html += `<div class="notice">alignment truncated (size guard)</div>`;
  return html;
}


function renderDiff(d) {
  state.lastDiff = d; // re-rendered on window resize (layout is width-dependent)
  state.diffBlockIdx = -1;
  $("diff-body").innerHTML = diffHTML(d, $("diff-pane").clientWidth);
  updateDiffNav();
}


// --- diff-pane navigation (the diff-header toolbar) ---

function activeFileList() {
  return state.filesMode === "status" ? state.statusEntries : state.files;
}


// changeNavRows: what the ‹/› change buttons step over — conflict regions
// while the picker is open, else the rendered diff's change runs.
function changeNavRows() {
  if (conflictPick) return [...document.querySelectorAll("#cf-doc .cf-region")];
  return diffChangeBlocks();
}


function updateDiffNav() {
  const list = activeFileList();
  $("prev-file").disabled = list.length === 0 || state.fileCursor <= 0;
  $("next-file").disabled = list.length === 0 || state.fileCursor >= list.length - 1;
  const any = changeNavRows().length > 0;
  $("prev-change").disabled = !any;
  $("next-change").disabled = !any;
  $("hist-btn").disabled = $("blame-btn").disabled = !state.diffCtx;
}


function stepFile(delta) {
  const list = activeFileList();
  const i = state.fileCursor + delta;
  if (i < 0 || i >= list.length) return;
  openFile(i);
}


// diffChangeBlocks returns the first row of each contiguous non-"same" run
// in the rendered diff table (add/del/change rows; a unified changed pair
// renders del+add adjacent — still one run). Derived from the live DOM so
// it survives any render mode (side-by-side, unified, single-column).
function diffChangeBlocks() {
  const rows = $("diff-body").querySelectorAll("table.diff tr");
  const blocks = [];
  let inBlock = false;
  rows.forEach((tr) => {
    const change = !tr.classList.contains("same");
    if (change && !inBlock) blocks.push(tr);
    inBlock = change;
  });
  return blocks;
}


function stepChange(delta) {
  const blocks = changeNavRows();
  if (!blocks.length) return;
  const i = Math.max(0, Math.min(blocks.length - 1, state.diffBlockIdx + delta));
  state.diffBlockIdx = i;
  const tr = blocks[i];
  tr.scrollIntoView({ block: "center" });
  tr.classList.add("flash");
  setTimeout(() => tr.classList.remove("flash"), 600);
  if (conflictPick && tr.dataset.b != null) {
    // The output pane follows: scroll the region's contribution (its own
    // scroll container, so the pick area is unaffected) and flash it too.
    const seg = document.querySelector(`#cf-out-body .cf-out-region[data-b="${tr.dataset.b}"]`);
    if (seg) {
      seg.scrollIntoView({ block: "center" });
      seg.classList.add("flash");
      setTimeout(() => seg.classList.remove("flash"), 600);
    }
  }
}


$("prev-file").addEventListener("click", () => stepFile(-1));

$("next-file").addEventListener("click", () => stepFile(1));

$("prev-change").addEventListener("click", () => stepChange(-1));

$("next-change").addEventListener("click", () => stepChange(1));


// ---- inline hunk staging (wave 3, reworked from live feedback) -----------
// Hunks are selected IN the unstaged diff itself (TUI-style: full context,
// line numbers, the same view — a separate block list lost the "what is
// what" context). The server tags eligible /api/diff?wt=unstaged rows with
// hunk ordinals + the staging freshness hash; clicking a tagged block
// toggles it and the diff-header bar stages the picked set. Picks are
// POSITIONAL against the exact bytes the server hashed — every staged
// round changes the hash, so the diff is RELOADED after each round (a 409
// means someone else moved the file: same reload).

let diffHunks = null; // {path, hash, count, picks: Set<int>} — set only while an eligible unstaged diff is open


function hunkEligible(f) {
  return !!f && f.section === "changes" && f.kind === "tracked";
}


function clearDiffHunks() {
  diffHunks = null;
  conflictPick = null;
  renderHunkBar();
  renderResolveBar();
}


function renderHunkBar() {
  const bar = $("hunk-bar");
  if (!diffHunks) {
    bar.classList.add("hidden");
    return;
  }
  bar.classList.remove("hidden");
  const n = diffHunks.picks.size;
  $("hunk-stage").disabled = !n;
  $("hunk-stage").textContent = `stage selected (${n})`;
}


// paintHunkPicks flips only the picked classes — no diff re-render, so
// scroll position and text selection survive a toggle.
function paintHunkPicks() {
  document.querySelectorAll("#diff-body tr[data-hunk]").forEach((tr) => {
    tr.classList.toggle("picked", !!diffHunks && diffHunks.picks.has(Number(tr.dataset.hunk)));
  });
  renderHunkBar();
}


async function stageHunksPicked() {
  const v = diffHunks;
  if (!v || !v.picks.size) return;
  let resp;
  try {
    resp = await postJSON("/api/stage-hunks", {
      path: v.path,
      picks: [...v.picks].sort((a, b) => a - b),
      hash: v.hash,
    });
  } catch (e) {
    opLine("error: " + (e.message || e), true);
    // 409 = stale picks (the file moved): reload the diff for fresh tags
    if (/file changed/.test(e.message || "")) reopenAfterHunkStage(v.path);
    return;
  }
  applyStatus(resp); // the 200 body IS a fresh /api/status payload
  reconcileStatusView();
  renderFiles();
  reopenAfterHunkStage(v.path);
}


// reopenAfterHunkStage re-opens the freshest view of path after a staging
// round: its unstaged diff while hunks remain, else whatever the cursor
// lands on (the file may have moved wholly into Staged).
function reopenAfterHunkStage(path) {
  const i = state.statusEntries.findIndex((f) => f.path === path && f.section === "changes");
  if (i >= 0) {
    state.fileCursor = i;
    renderFiles();
    openStatusDiff(i);
    return;
  }
  clearDiffHunks();
  const f = state.statusEntries[state.fileCursor];
  if (state.filesMode === "status" && f) {
    openStatusDiff(state.fileCursor);
  } else {
    $("diff-title").textContent = "";
    $("diff-body").innerHTML = "";
    updateDiffNav();
  }
}


$("hunk-stage").addEventListener("click", () => void stageHunksPicked());

$("hunk-all").addEventListener("click", () => {
  if (!diffHunks) return;
  diffHunks.picks = new Set(Array.from({ length: diffHunks.count }, (_, i) => i));
  paintHunkPicks();
});

$("hunk-none").addEventListener("click", () => {
  if (!diffHunks) return;
  diffHunks.picks = new Set();
  paintHunkPicks();
});


$("diff-body").addEventListener("click", (e) => {
  if (!diffHunks) return;
  const tr = e.target.closest("tr[data-hunk]");
  if (!tr || !getSelection().isCollapsed) return; // don't toggle mid text-selection
  const i = Number(tr.dataset.hunk);
  if (diffHunks.picks.has(i)) diffHunks.picks.delete(i);
  else diffHunks.picks.add(i);
  paintHunkPicks();
});


// ---- conflict block picker (conflict surface) -----------------------------
// A conflicted row opens the file's marker regions as pickable ours/theirs
// blocks (GET /api/conflict-hunks). Picks are per-LINE and ordered (the TUI
// line-pick rule): resolving POSTs a positional pick per block — a whole-side
// fast path ({mode:"ours"|"theirs"}) when the picks are exactly that side in
// order, else the ordered line list ({mode:"lines", lines:[{side,line},…]}).
// The server writes + stages via engine.ResolveConflictHunks. A 409 means the
// file moved: reload the picker (the stage-hunks rule).

// choices[i] = {picks: Array<{side:"ours"|"theirs", line:number}>, touched:boolean}
// - order of `picks` = order in the assembled result (the TUI rule)
// - touched && picks.length === 0  → decided-empty ("drop both sides")
// - !touched                       → undecided (gates resolve)
let conflictPick = null; // {path, hash, count, items, blocks, choices} — set only while the picker is open


function regionDecided(ch) { return ch.touched; }


function sideState(ch, it, side) {
  // → "all" | "some" | "none" for the tri-state tag
  const total = (side === "ours" ? it.ours : it.theirs)?.length || 0;
  if (!total) return "none";
  const n = ch.picks.filter((p) => p.side === side).length;
  return n === total ? "all" : n ? "some" : "none";
}


function toggleLine(ch, side, line) {
  ch.touched = true;
  const at = ch.picks.findIndex((p) => p.side === side && p.line === line);
  if (at >= 0) ch.picks.splice(at, 1);
  else ch.picks.push({ side, line });
}


function toggleSide(ch, it, side) {
  // TUI ToggleSide: zero-line side is a no-op; fully-on clears that side's
  // picks (others keep order); else append the side's unpicked lines in order.
  const lines = side === "ours" ? it.ours : it.theirs;
  if (!lines || !lines.length) return;
  ch.touched = true;
  if (sideState(ch, it, side) === "all") {
    ch.picks = ch.picks.filter((p) => p.side !== side);
  } else {
    for (let i = 0; i < lines.length; i++)
      if (!ch.picks.some((p) => p.side === side && p.line === i)) ch.picks.push({ side, line: i });
  }
}


function wirePick(ch, it) {
  // Collapse to the fast path when the picks are exactly one full side in order.
  const full = (side) => {
    const lines = side === "ours" ? it.ours : it.theirs;
    return (lines?.length || 0) > 0 && ch.picks.length === lines.length &&
      ch.picks.every((p, i) => p.side === side && p.line === i);
  };
  if (full("ours")) return { mode: "ours" };
  if (full("theirs")) return { mode: "theirs" };
  return { mode: "lines", lines: ch.picks.map((p) => ({ side: p.side, line: p.line })) };
}


// regionSuffix is the dim state note next to a region's header: nothing
// while undecided, "empty" when decided-empty (both sides dropped), else —
// only once BOTH sides are fully picked (matching the TUI's stateSuffix,
// which gates on both sides' all-state) — the side of the first pick. The
// ticks already convey partial/single-side state; this suffix exists solely
// to surface interleave order once everything from both sides is merged. A
// region with a zero-line side can never reach "all" on that side, so it
// never shows this suffix either — same as the TUI.
function regionSuffix(ch, it) {
  if (!ch.touched) return "";
  if (!ch.picks.length) return "empty";
  if (sideState(ch, it, "ours") !== "all" || sideState(ch, it, "theirs") !== "all") return "";
  return (ch.picks[0].side === "ours" ? "ours" : "theirs") + " first";
}


// assembleOutput is the live preview: the resolved file as it would be
// written, undecided regions rendered as a placeholder so the pane always
// reflects the CURRENT (possibly incomplete) pick state.
function assembleOutput(v) {
  // HTML with each region's contribution wrapped in a .cf-out-region span so
  // the ‹/› change nav can scroll the pane to it. The TEXT stays byte-equal
  // to the join of all contributed lines (empty parts are dropped whole, so
  // no stray newlines appear around a decided-empty region).
  const parts = [];
  for (const it of v.items) {
    if (it.kind === "text") {
      if ((it.lines || []).length) parts.push(esc(it.lines.join("\n")));
      continue;
    }
    const ch = v.choices[it.index];
    const lines = !ch.touched
      ? [`‹region ${it.index + 1} undecided›`]
      : ch.picks.map((p) => (p.side === "ours" ? it.ours : it.theirs)[p.line]);
    if (lines.length) parts.push(`<span class="cf-out-region" data-b="${it.index}">${esc(lines.join("\n"))}</span>`);
  }
  return parts.join("\n");
}


async function openConflictPicker(f) {
  clearDiffHunks(); // also nulls conflictPick — order matters, set it after
  $("diff-title").textContent = f.path + " — resolve";
  $("diff-body").innerHTML = `<div class="notice">loading…</div>`;
  updateDiffNav();
  let d;
  try {
    d = await getJSON("/api/conflict-hunks?" + new URLSearchParams({ path: f.path }));
  } catch (e) {
    // typed 422 refusal (binary / markers gone): show the reason + the way out
    $("diff-body").innerHTML =
      `<div class="notice">${esc(e.message || e)}</div>` +
      `<div class="notice">right-click the file → mark resolved when it is done</div>`;
    return;
  }
  const blocks = []; // index → block item, built once for wirePick/toggle/paint lookups
  for (const it of d.items) if (it.kind === "block") blocks[it.index] = it;
  conflictPick = {
    path: f.path,
    hash: d.hash,
    count: d.count,
    items: d.items,
    blocks,
    choices: Array.from({ length: d.count }, () => ({ picks: [], touched: false })),
  };
  let html = '<div id="cf-doc">';
  for (const it of d.items) {
    if (it.kind === "text") {
      html += `<pre class="cf-text">${esc((it.lines || []).join("\n"))}</pre>`;
    } else {
      html += `<div class="cf-region" data-b="${it.index}">` +
        `<div class="cf-region-head">region ${it.index + 1}<span class="cf-region-state"></span></div>` +
        `<div class="cf-block">${cfSideDiv(it, "ours")}${cfSideDiv(it, "theirs")}</div>` +
        `</div>`;
    }
  }
  html += "</div>" +
    `<div id="cf-out">` +
    `<div id="cf-out-head" data-act="collapse">output — live preview</div>` +
    `<pre id="cf-out-body"></pre>` +
    `</div>`;
  $("diff-body").innerHTML = html;
  state.diffBlockIdx = -1; // ‹/› change steps regions from the top
  paintConflictPicks();
  updateDiffNav(); // the early call saw no regions; enable ‹/› change now
}


// cfSideCount renders the tag's " · N lines" suffix — the disambiguator when
// both sides are visually blank (a conflict between runs of empty lines
// otherwise reads as nothing vs nothing).
function cfSideCount(lines) {
  const n = (lines || []).length;
  return n === 0 ? " · empty" : n === 1 ? " · 1 line" : ` · ${n} lines`;
}


// cfLineHTML renders ONE line's inner HTML with emptiness made visible: an
// empty line shows a dim ¶, and a whitespace-only line shows its spaces/tabs
// as ·/→ so "3 spaces" and "empty" stop looking identical. A trailing \r
// (CRLF file — ParseConflict keeps it) is ignored for the blank test so a
// "\r" line reads as empty rather than invisible.
function cfLineHTML(ln) {
  const bare = ln.replace(/\r$/, "");
  if (!/^\s*$/.test(bare)) return esc(ln);
  const glyphs = bare.length ? bare.replace(/\t/g, "→").replace(/ /g, "·") : "¶";
  return `<span class="cf-ws">${esc(glyphs)}</span>`;
}


// cfSideDiv renders one side of a block: a zero-line side gets the existing
// "(empty — this side has no lines)" body and NO actionable tick (nothing to
// toggle); a non-empty side gets a clickable tri-state tag plus one row per
// line, each with its own checkbox tick.
function cfSideDiv(it, side) {
  const lines = side === "ours" ? it.ours : it.theirs;
  const n = (lines || []).length;
  const tag = n
    ? `<div class="cf-tag" data-act="side"><span class="cf-tick">[ ]</span> ${side}${cfSideCount(lines)}</div>`
    : `<div class="cf-tag">${side}${cfSideCount(lines)}</div>`;
  const body = n
    ? lines.map((ln, i) =>
      `<div class="cf-ln" data-side="${side}" data-line="${i}"><span class="cf-tick">[ ]</span>${cfLineHTML(ln)}</div>`).join("")
    : '<span class="cf-empty">(empty — this side has no lines)</span>';
  return `<div class="cf-side cf-${side}" data-side="${side}">${tag}${body}</div>`;
}


// paintConflictPicks repaints tick glyphs, .picked line classes, the tri-state
// tag emphasis, the region suffix, the output pane body, and the resolve
// bar. The one repaint function — called after every toggle.
function paintConflictPicks() {
  const v = conflictPick;
  document.querySelectorAll("#cf-doc .cf-region").forEach((regionEl) => {
    const i = Number(regionEl.dataset.b);
    const ch = v && v.choices[i];
    const it = v && v.blocks[i];
    if (!ch || !it) return;
    regionEl.classList.toggle("decided", regionDecided(ch));
    const stateEl = regionEl.querySelector(".cf-region-state");
    if (stateEl) {
      const suffix = regionSuffix(ch, it);
      stateEl.textContent = suffix ? " · " + suffix : "";
    }
    regionEl.querySelectorAll(".cf-side").forEach((sideEl) => {
      const side = sideEl.dataset.side;
      const tag = sideEl.querySelector(".cf-tag");
      const tick = tag && tag.querySelector(".cf-tick");
      if (tick) {
        const st = sideState(ch, it, side);
        tick.textContent = st === "all" ? "[x]" : st === "some" ? "[~]" : "[ ]";
        tag.classList.toggle("some", st === "some");
        tag.classList.toggle("all", st === "all");
      }
      sideEl.querySelectorAll(".cf-ln").forEach((lnEl) => {
        const line = Number(lnEl.dataset.line);
        const picked = ch.picks.some((p) => p.side === side && p.line === line);
        lnEl.classList.toggle("picked", picked);
        const lt = lnEl.querySelector(".cf-tick");
        if (lt) lt.textContent = picked ? "[x]" : "[ ]";
      });
    });
  });
  const outBody = $("cf-out-body");
  if (outBody) outBody.innerHTML = v ? assembleOutput(v) : "";
  renderResolveBar();
}


function renderResolveBar() {
  const bar = $("resolve-bar");
  if (!conflictPick) { bar.classList.add("hidden"); return; }
  bar.classList.remove("hidden");
  const v = conflictPick;
  const n = v.choices.filter(regionDecided).length;
  $("resolve-count").textContent = n + "/" + v.count + " decided";
  $("resolve-go").disabled = !v.choices.every(regionDecided);
}


// setAllConflictPicks is the resolve bar's "all ours"/"all theirs": a
// document-wide TRI-STATE toggle (the TUI C/I rule) — if every region with a
// non-empty that-side is already fully-on for that side, clear that side
// everywhere (still touched, may become decided-empty); else complete it
// everywhere (append that side's missing lines in order). Zero-line sides
// are skipped entirely, both for the "already all-on" check and the apply.
function setAllConflictPicks(side) {
  const v = conflictPick;
  if (!v) return;
  const eligible = v.blocks.filter((it) => ((side === "ours" ? it.ours : it.theirs) || []).length);
  if (!eligible.length) return;
  const allOn = eligible.every((it) => sideState(v.choices[it.index], it, side) === "all");
  for (const it of eligible) {
    const ch = v.choices[it.index];
    ch.touched = true;
    if (allOn) {
      ch.picks = ch.picks.filter((p) => p.side !== side);
    } else {
      const lines = side === "ours" ? it.ours : it.theirs;
      for (let i = 0; i < lines.length; i++)
        if (!ch.picks.some((p) => p.side === side && p.line === i)) ch.picks.push({ side, line: i });
    }
  }
  paintConflictPicks();
}


async function resolveConflictPicked() {
  const v = conflictPick;
  if (!v || !v.choices.every(regionDecided)) return;
  let resp;
  try {
    resp = await postJSON("/api/resolve-hunks", {
      path: v.path,
      picks: v.choices.map((ch, i) => wirePick(ch, v.blocks[i])),
      hash: v.hash,
    });
  } catch (e) {
    opLine("error: " + (e.message || e), true);
    // 409 = the file moved under the picker: reload it for fresh blocks
    if (/file changed/.test(e.message || "")) {
      const i = state.statusEntries.findIndex((f) => f.path === v.path && f.section === "conflicts");
      if (i >= 0) { state.fileCursor = i; openStatusDiff(i); }
    }
    return;
  }
  const path = v.path;
  conflictPick = null;
  applyStatus(resp); // the 200 body IS a fresh /api/status payload
  reconcileStatusView();
  renderFiles();
  stepToNextConflict(path);
}


// After a resolve: the same path if somehow still conflicted, else the next
// conflicted file, else whatever the cursor lands on (the resolved file
// moved to Staged).
function stepToNextConflict(path) {
  let i = state.statusEntries.findIndex((f) => f.path === path && f.section === "conflicts");
  if (i < 0) i = state.statusEntries.findIndex((f) => f.section === "conflicts");
  if (i >= 0) { state.fileCursor = i; renderFiles(); openStatusDiff(i); return; }
  const f = state.statusEntries[state.fileCursor];
  if (state.filesMode === "status" && f) openStatusDiff(state.fileCursor);
  else { $("diff-title").textContent = ""; $("diff-body").innerHTML = ""; updateDiffNav(); }
}


$("resolve-ours").addEventListener("click", () => setAllConflictPicks("ours"));

$("resolve-theirs").addEventListener("click", () => setAllConflictPicks("theirs"));

$("resolve-go").addEventListener("click", resolveConflictPicked);

$("diff-body").addEventListener("click", (e) => {
  if (!conflictPick) return;
  const collapse = e.target.closest("#cf-out-head");
  if (collapse) {
    $("cf-out-body").classList.toggle("hidden");
    return;
  }
  if (!getSelection().isCollapsed) return; // selecting text is not a pick
  const v = conflictPick;
  const lnEl = e.target.closest(".cf-ln");
  if (lnEl) {
    const region = lnEl.closest(".cf-region");
    const ch = v.choices[Number(region.dataset.b)];
    toggleLine(ch, lnEl.dataset.side, Number(lnEl.dataset.line));
    paintConflictPicks();
    return;
  }
  const tag = e.target.closest('[data-act="side"]');
  if (tag) {
    const region = tag.closest(".cf-region");
    const side = tag.closest(".cf-side").dataset.side;
    const i = Number(region.dataset.b);
    toggleSide(v.choices[i], v.blocks[i], side);
    paintConflictPicks();
  }
});


$("files-list").addEventListener("click", (e) => {
  const btn = e.target.closest("button.act");
  if (btn && state.filesMode === "status") {
    const f = state.statusEntries[Number(btn.dataset.i)];
    stage(btn.dataset.un ? { paths: [f.path], unstage: true } : { paths: [f.path] });
    return;
  }
  const li = e.target.closest("li");
  if (li && li.dataset.i !== undefined && (e.ctrlKey || e.metaKey) && state.filesMode === "status") {
    const f = state.statusEntries[Number(li.dataset.i)];
    if (f) toggleMark(f.path);
    return;
  }
  if (li && li.dataset.i !== undefined) {
    state.pane = "files";
    focusPane();
    openFile(Number(li.dataset.i));
  }
});

// copyPathRows: the three copy actions every file menu offers — the
// repo-relative path, the absolute path anchored on the served worktree
// (the TUI's fileCopyPathName anchor), and the basename.
function copyPathRows(path) {
  const abs = (state.worktree || "").replace(/\/+$/, "") + "/" + path;
  return [
    { label: "copy path", act: () => copyText(path) },
    { label: "copy absolute path", act: () => copyText(abs, "absolute path") },
    { label: "copy file name", act: () => copyText(path.split("/").pop(), "file name") },
  ];
}

// fileExt: the basename's extension including the dot ("" when none) —
// mirrors Go's path.Ext, which the engine uses to build the *<ext> pattern.
function fileExt(path) {
  const base = path.split("/").pop();
  const di = base.lastIndexOf(".");
  return di >= 0 ? base.slice(di) : "";
}

// Right-click on a working-tree status file: stage/unstage it (per its
// section), bulk actions, copy path. Selects the row for feedback without
// opening its diff.
$("files-list").addEventListener("contextmenu", (e) => {
  const li = e.target.closest("li");
  if (!li || li.dataset.i === undefined) return;
  if (state.filesMode !== "status") {
    // commit / compare rows: read-only file actions. rev picks what "here"
    // means — the commit being viewed, or the compare's right tip.
    const f = state.files[Number(li.dataset.i)];
    if (!f) return;
    e.preventDefault();
    state.fileCursor = Number(li.dataset.i);
    renderFiles();
    const rev = state.filesMode === "compare" ? state.compare.bHash : f.sha || state.fileSha;
    showCtxMenu(
      [
        { label: "file history", act: () => openFileHistory(f.path, rev) },
        { label: "blame at this commit", act: () => openFileBlame(f.path, rev) },
        // gg's own stores, addressed at the commit being viewed: a bookmark
        // points AT this version, a shelf entry freezes its bytes.
        { label: "bookmark this file", act: () => addFileEntry("bookmarks", f.path, "committed", rev) },
        { label: "add to shelf", act: () => addFileEntry("shelf", f.path, "committed", rev) },
        ...copyPathRows(f.path),
      ],
      e.clientX,
      e.clientY
    );
    return;
  }
  e.preventDefault();
  const i = Number(li.dataset.i);
  const f = state.statusEntries[i];
  if (!f) return;
  state.fileCursor = i;
  renderFiles();
  const items = [];
  if (f.section === "staged") items.push({ label: "unstage " + f.path, act: () => stage({ paths: [f.path], unstage: true }) });
  else if (f.section === "conflicts") items.push({ label: "mark resolved (stage as-is)", act: () => stage({ paths: [f.path] }) });
  else items.push({ label: "stage " + f.path, act: () => stage({ paths: [f.path] }) });
  items.push(...copyPathRows(f.path));
  // The address depends on WHICH list the row is in — a staged file and its
  // working-tree twin are different addresses, not one file in two moods.
  const fstate = f.section === "staged" ? "staged" : f.section === "untracked" ? "untracked" : "unstaged";
  items.push({ label: "bookmark this file", act: () => addFileEntry("bookmarks", f.path, fstate, "") });
  items.push({ label: "add to shelf", act: () => addFileEntry("shelf", f.path, fstate, "") });
  // history/blame only where git has something to say: an untracked file
  // was never committed (empty history, blame errors), and a staged NEW
  // file ("A") is the same file one step later. A conflicted file keeps
  // both — blame works on unmerged paths (markers blame as uncommitted).
  // A staged rename's history lives under the OLD name (--follow can only
  // follow from a committed path), so the row queries orig_path.
  if (f.section !== "untracked" && !(f.section === "staged" && f.staged === "A")) {
    items.push({ label: "file history", act: () => openFileHistory(f.orig_path || f.path, "") });
    items.push({ label: "blame (working tree)", act: () => openFileBlame(f.path, "") });
  }
  if (f.section === "untracked") {
    // git ignores only untracked paths — the server 422s anything else
    items.push({ label: "add to .gitignore", act: () => startOp({ op: "ignore", path: f.path }, "ignore " + f.path) });
    const ext = fileExt(f.path);
    if (ext) items.push({ label: "add *" + ext + " to .gitignore", act: () => startOp({ op: "ignore", path: f.path, ext: true }, "ignore *" + ext) });
  }
  // batch rows for the marked set (ctrl+click / m): recomputed per section
  // every open, so marks surviving a stage flip from "stage N" to
  // "unstage N" naturally. Marks never include conflict rows (toggleMark).
  if (state.marked.size) {
    const mk = state.statusEntries.filter((x) => state.marked.has(x.path));
    const stageable = mk.filter((x) => x.section === "changes" || x.section === "untracked").map((x) => x.path);
    const unstageable = mk.filter((x) => x.section === "staged").map((x) => x.path);
    if (stageable.length)
      items.push({ label: "stage " + stageable.length + " marked", act: () => stage({ paths: stageable }) });
    if (unstageable.length)
      items.push({ label: "unstage " + unstageable.length + " marked", act: () => stage({ paths: unstageable, unstage: true }) });
    items.push({ label: "clear marks (" + state.marked.size + ")", act: () => { state.marked.clear(); renderFiles(); } });
  }
  // the mass rows vanish while an op is paused — same footguns as the
  // hidden #files-actions buttons (stage all = markers staged as resolved,
  // unstage all = auto-merged results pulled out of the merge commit)
  if (!state.conflict) {
    items.push({ label: "stage all", act: () => stage({ all: true }) });
    if (state.statusEntries.some((x) => x.section === "staged")) {
      items.push({
        label: "unstage all",
        act: () => {
          const paths = state.statusEntries.filter((x) => x.section === "staged").map((x) => x.path);
          if (paths.length) stage({ paths, unstage: true }); // engine.Stage{All} can't unstage
        },
      });
    }
  }
  // One separator for the whole discard block below: whichever of the three
  // conditional red rows materializes, it is fenced off from the safe rows
  // above — and if none does, showCtxMenu trims the stranded line.
  items.push({ sep: true });
  if (f.section === "changes") {
    items.push({
      label: "discard changes", danger: true,
      act: () => showLocalConfirm(
        "Discard changes to " + f.path + "? This cannot be undone.",
        ["discard", "abort"],
        (o) => { if (o === "discard") startOp({ op: "discard", path: f.path }, "discard " + f.path); }
      ),
    });
  } else if (f.section === "untracked") {
    items.push({
      label: "delete untracked file", danger: true,
      act: () => showLocalConfirm(
        "Delete untracked " + f.path + "? This cannot be undone.",
        ["discard", "abort"],
        (o) => { if (o === "discard") startOp({ op: "discard", path: f.path }, "discard " + f.path); }
      ),
    });
  }
  // discard of the marked set: danger, all-or-nothing server-side (any
  // stale mark refuses the whole batch rather than half-discarding)
  if (state.marked.size) {
    const dk = state.statusEntries.filter((x) => state.marked.has(x.path) && (x.section === "changes" || x.section === "untracked")).map((x) => x.path);
    if (dk.length) {
      items.push({
        label: "discard " + dk.length + " marked", danger: true,
        act: () => showLocalConfirm(
          "Discard the " + dk.length + " marked file" + (dk.length > 1 ? "s" : "") + "? Tracked edits are reverted, untracked files are deleted. This cannot be undone.",
          ["discard", "abort"],
          (o) => { if (o === "discard") startOp({ op: "discard", paths: dk }, "discard " + dk.length + " marked"); }
        ),
      });
    }
  }
  // discard-all shares the paused-op gate with the other mass rows, and the
  // confirm names BOTH halves — tracked edits reverted AND untracked files
  // deleted — because "all" spans two different kinds of loss.
  if (!state.conflict && state.statusEntries.some((x) => x.section === "changes" || x.section === "untracked")) {
    items.push({
      label: "discard all changes", danger: true,
      act: () => showLocalConfirm(
        "Discard ALL unstaged changes? Tracked edits are reverted AND untracked files are deleted. Staged changes are kept. This cannot be undone.",
        ["discard", "abort"],
        (o) => { if (o === "discard") startOp({ op: "discard", all: true }, "discard all"); }
      ),
    });
  }
  showCtxMenu(items, e.clientX, e.clientY);
});


$("stage-all").addEventListener("click", () => stage({ all: true }));

$("unstage-all").addEventListener("click", () => {
  const paths = state.statusEntries.filter((f) => f.section === "staged").map((f) => f.path);
  if (paths.length) stage({ paths, unstage: true }); // engine.Stage{All} can't unstage
});

$("hist-btn").addEventListener("click", () => {
  if (state.diffCtx) openFileHistory(state.diffCtx.path, state.diffCtx.rev);
});

$("blame-btn").addEventListener("click", () => {
  if (state.diffCtx) openFileBlame(state.diffCtx.path, state.diffCtx.rev);
});
export { SECTION_LABELS, activeFileList, applyCompareFilter, cfSideCount, clearDiffHunks, conflictPick, diffChangeBlocks, toggleMark, diffHTML, diffHunks, drillOut, enterFilesStage, exitStatusToList, hunkAttr, hunkCls, hunkEligible, markSpans, openCompare, openConflictPicker, openFile, openStatusDiff, openWorkingTree, paintConflictPicks, paintHunkPicks, reconcileStatusView, renderCompareBar, renderDiff, renderFiles, renderHunkBar, renderResolveBar, reopenAfterHunkStage, resolveConflictPicked, setAllConflictPicks, setLayout, stage, stageHunksPicked, stepChange, stepFile, stepToNextConflict, updateDiffNav };
