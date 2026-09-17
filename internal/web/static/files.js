// files.js — part of gg's web client. Split from the original app.js;
// see app.js (the entry module) for the load order.
import { $, attnKey, charWidth, elidePath, esc, getJSON, postJSON, runOnce, runes, state } from "./core.js";
import { closePrompt, copyText, openPrompt, showCtxMenu } from "./layers.js";
import { addFileEntry } from "./sidebar.js";
import { extraRows, registerHelp } from "./menus.js";
import { copyLink, linkDesc, linkFor } from "./links.js";
import { applyStatus, buildStatusEntries, fetchStatus } from "./status.js";
import { nextSortMode, setSortMode, sortChipHTML } from "./sortlist.js";
import { opLine, showLocalConfirm, startOp } from "./ops.js";
import { openFileBlame, openFileHistory } from "./filehist.js";
import { rev } from "./review.js";
import { renderCommits, rewordPrompt } from "./commits.js";
import { focusPane, moveCursor, stepCommitCursor } from "./keys.js";
import { saveUI } from "./uistate.js";

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


// applyFilesHidden is the shared "put the file list in this state" step,
// used by the » control and by the layout restored from the server at boot.
// Minimized, the list folds to a strip holding only the restore control (the
// CSS `nofiles` variant) and the flexible pane takes the width — which
// changes the diff's side-by-side/unified verdict and the commit list's
// column, so whichever is on screen is redrawn for the new width.
function applyFilesHidden(hidden) {
  state.filesHidden = hidden;
  $("panes").classList.toggle("nofiles", hidden);
  const b = $("files-min");
  b.textContent = hidden ? "«" : "»";
  b.title = hidden ? "show the file list" : "hide the file list — more room for the diff";
  b.setAttribute("aria-expanded", hidden ? "false" : "true");
  if (state.layout === "diff") rerenderDiffKeepingPlace();
  else renderCommits();
}


// toggleFilesHidden is the user-facing flip. The choice is a per-machine
// preference (/api/uistate — the random port makes localStorage useless).
function toggleFilesHidden() {
  if (state.layout === "list") return; // no file list on the commit-list screen to fold (the sidebar toggle's rule)
  applyFilesHidden(!state.filesHidden);
  saveUI({ files_hidden: state.filesHidden });
}


$("files-min").addEventListener("click", toggleFilesHidden);


registerHelp({
  key: "» · hide the file list",
  html:
    "fold the file list (the right column) to a slim strip so the diff — or the commit list — takes " +
    "its width; <b>«</b> on the strip brings it back. The <b>toggle file list</b> row in the ☰ menu's UI " +
    "group is the same switch, and the choice is remembered per machine. esc and the footer's " +
    "<b>back</b> chip still step out of the stage while the list is folded",
});


// --- files + diff panes ---

// Staged layout (the GitKraken flow): "list" = the commit list alone, full
// width; "files" = a commit is open — commits shrink left, the file list
// takes a fixed column on the right, NO diff yet; "diff" = a file is open —
// the diff replaces the commits area, file list stays right. esc steps one
// stage back (drillOut).
function setLayout(mode) {
  const was = state.layout;
  state.layout = mode;
  const p = $("panes");
  p.classList.toggle("solo", mode === "list");
  p.classList.toggle("files", mode === "files");
  p.classList.toggle("detail", mode === "diff");
  // The commits pane is display:none in the diff stage: that drops its
  // scroll position, and any render while hidden (a live refresh, r, a notes
  // count) sizes the virtual window for a zero-height pane — ten rows, which
  // is all the list showed after esc until the user scrolled. Coming back
  // into either stage that shows the pane re-renders and rescrolls it.
  if (mode === "list" || (mode === "files" && was === "diff")) stepCommitCursor(0);
}


// enterFilesStage swaps to the file-list stage: browse the changed files
// with the commit list still alongside, nothing auto-opened. The diff pane
// is cleared so a later stage-3 entry never flashes the previous drill's
// diff, and a window resize cannot resurrect it through state.lastDiff.
function enterFilesStage() {
  state.pane = "files";
  setLayout("files");
  setDiffTitle("");
  $("diff-body").innerHTML = "";
  state.lastDiff = null;
  state.diffCtx = null;
  setFilesMeta(""); // every stage starts without a date; only a commit open sets one
  setFilesDesc(""); // …and without a description
  $("files-title").dataset.sha = ""; // …and without a commit id; see setCommitTitle
  $("files-title").dataset.subject = "";
  $("files-title").dataset.short = "";
}


// setFilesMeta draws (or hides) the file-list header's second line: the
// commit's date and author, the web twin of the TUI files view's under-title
// line. Called with "" by enterFilesStage, so a stage that has no single commit
// behind it — a comparison, the working tree, a stash — never shows one by
// simply not setting it. The element carries its own #files-meta.hidden rule;
// a bare class="hidden" with no id rule would stay visible.
// --- the diff header's file path ---
//
// The header is the only place the open file's full path is written out, so
// it is also the natural place to copy it from. That makes the path a
// SEPARATE element rather than a slice of the header's text: three of the
// callers decorate it ("a ↔ b · path", "path — resolve"), and a copy that
// parsed the rendered text back would hand out those decorations too.
// setDiffTitle keeps the raw path on the element; nothing reads textContent.
function setDiffTitle(path, prefix, suffix) {
  const el = $("diff-title");
  if (!path) {
    el.textContent = "";
    return;
  }
  el.innerHTML =
    esc(prefix || "") +
    `<span id="diff-path" title="${esc(path)} — click to copy · right-click for more">${esc(path)}</span>` +
    esc(suffix || "");
  $("diff-path").dataset.path = path;
}


// --- path parts (pure; guarded against Go) ---
// pathParts splits what the copy menu offers: the file's own name and the
// directory holding it. git speaks "/" but a Windows path can arrive from a
// worktree address, so both separators count. dir is "" for a file at the
// repo root — there is no parent to name, and the menu drops that row rather
// than offering an empty copy. A trailing separator belongs to the name's
// side (nothing addresses a directory here), so it is left alone.
function pathParts(path) {
  const cut = Math.max(path.lastIndexOf("/"), path.lastIndexOf("\\"));
  return { name: path.slice(cut + 1), dir: cut > 0 ? path.slice(0, cut) : "" };
}


// absPath joins a repo-relative path onto the checkout root the server
// reported. git always speaks "/", so on a Windows root the separators are
// rewritten to "\" — the point of copying an absolute path is pasting it
// into something else on that machine, and a half-"/" path is no use there.
// The root is taken as given (never normalized): it is the checkout gg is
// actually serving. "" for either side means there is nothing to join, and
// the caller drops the row rather than copying a bare root.
function absPath(root, path) {
  if (!root || !path) return "";
  const win = root.includes("\\") && !root.includes("/");
  const sep = win ? "\\" : "/";
  const base = root.replace(/[/\\]+$/, "");
  const rel = win ? path.replace(/\//g, "\\") : path;
  return base + sep + rel;
}
// --- end path parts ---


// diffPathRows is the path menu, shared by the header (right-click) and
// anything else that wants to offer a path.
// The repo-relative rows come first (what git, a review note or a gg:// link
// speaks), then the machine-local absolute ones below a separator. At the
// repo root both "parent dir" rows drop: the parent IS the checkout, and the
// last row already offers it.
function diffPathRows(path) {
  const { name, dir } = pathParts(path);
  const root = (state.repo && state.repo.worktree) || "";
  const rows = [
    { label: "copy full path", act: () => copyText(path, "path") },
    { label: "copy file name", act: () => copyText(name, "file name") },
  ];
  if (dir) rows.push({ label: "copy parent dir", act: () => copyText(dir, "parent dir") });
  if (!root) return rows;
  rows.push({ sep: true });
  rows.push({ label: "copy absolute file path", act: () => copyText(absPath(root, path), "absolute path") });
  if (dir) {
    rows.push({ label: "copy absolute parent dir", act: () => copyText(absPath(root, dir), "absolute parent dir") });
  }
  rows.push({ label: "copy repo absolute path", act: () => copyText(root, "repo path") });
  return rows;
}


$("diff-header").addEventListener("click", (e) => {
  const p = e.target.closest("#diff-path");
  if (p) copyText(p.dataset.path, "path");
});


$("diff-header").addEventListener("contextmenu", (e) => {
  const p = e.target.closest("#diff-path");
  if (!p) return; // the rest of the toolbar keeps the browser's own menu
  e.preventDefault();
  showCtxMenu(diffPathRows(p.dataset.path), e.clientX, e.clientY);
});


function setFilesMeta(text, parts) {
  const el = $("files-meta");
  el.textContent = text || "";
  el.classList.toggle("hidden", !text);
  // The pieces ride along on the element so the header's menu can copy the
  // date and the author separately without re-parsing the rendered line.
  el.dataset.date = (parts && parts.date) || "";
  el.dataset.author = (parts && parts.author) || "";
}


// setFilesDesc draws (or hides) the header's third block: the first lines of
// the commit's description, under the date, clamped to three lines by CSS. The
// open commit's full message rides on the element so the button under it can
// show the whole thing — the TUI's commit-message popup — without a second
// round trip. Called with "" by enterFilesStage, so a stage with no commit (or
// a commit with no description) draws no block, and no button either.
function setFilesDesc(message) {
  const el = $("files-desc");
  const desc = commitBody(message);
  el.textContent = desc;
  el.dataset.message = desc ? message : "";
  el.classList.toggle("hidden", !desc);
  updateFilesDescMore();
}


// The "show full message…" button exists only while the clamp is actually
// hiding something: a three-line description is on screen in full, and a
// button that opens a window showing exactly what is already there is noise.
// Whether the clamp bites depends on the pane's width (a narrow files column
// wraps more), so the check re-runs whenever the block's size changes.
function updateFilesDescMore() {
  const el = $("files-desc");
  const clipped = !!el.dataset.message && el.scrollHeight > el.clientHeight + 1;
  $("files-desc-more").classList.toggle("hidden", !clipped);
}
new ResizeObserver(updateFilesDescMore).observe($("files-desc"));


// The full message opens in the reword prompt's window, read-only: the same
// box, the same wrapping, sized to the text — "same as edit, without the
// edit". Nothing to submit, so esc (or close) is the way out — or edit…,
// which swaps the viewer for the reword prompt over the same text, offered
// under the commit menu's own gate (one parent: the engine refuses to reword
// a merge, and the root is not on a rewritable range). A commit with no feed
// row (opened by hash) has no parent count to check, so no edit… either.
$("files-desc-more").addEventListener("click", () => {
  const message = $("files-desc").dataset.message;
  if (!message) return;
  const title = $("files-title");
  const hash = title.dataset.sha;
  const short = title.dataset.short || (hash || "").slice(0, 9);
  const row = (state.rows || []).find((r) => r.hash === hash);
  const extra = row && row.parents === 1
    ? { label: "edit…", run: () => { closePrompt(); rewordPrompt(hash, short, message); } }
    : undefined;
  openPrompt({ title: "Message of " + short + ":", value: message, multiline: true, readonly: true, extra });
});
// The button is one action, not a place: right-clicking it offers neither the
// header's copy menu (it sits inside the header) nor the browser's own.
$("files-desc-more").addEventListener("contextmenu", (e) => {
  e.preventDefault();
  e.stopPropagation();
});


registerHelp({
  key: "commit header",
  html:
    "an open commit's header shows its id and title (hover an elided title to read it whole), the " +
    "<b>date · author</b> line, and the first three lines of its description; <b>show full message…</b> " +
    "under them opens the whole message read-only (esc closes; <b>edit…</b> there swaps it for the reword " +
    "prompt). Right-click the header to copy the id, " +
    "title, date or author",
});


// A title the header had to elide is readable in full on hover — but only
// then: a tooltip that repeats a title already on screen in full is noise.
// Checked at hover time rather than at render time, because whether the title
// fits changes with every pane resize.
$("files-title").addEventListener("mouseenter", (e) => {
  const el = e.currentTarget;
  el.title = el.dataset.subject && el.scrollWidth > el.clientWidth ? el.dataset.subject : "";
});


// --- the file-list header's commit id ---
//
// The header of an open commit is the one place its id is on screen, so it is
// where the id gets copied from. The full sha lives on the element (the row
// only ever SHOWS the short form), and the short form gets its own span so
// the two can offer different menus: short + full over the sha, full over the
// subject. Stages with no single commit behind them (the working tree, a
// comparison, a preview) keep writing plain text through files-title and
// enterFilesStage clears the sha, so their header offers nothing.
function setCommitTitle(hash, short, subject) {
  const el = $("files-title");
  el.dataset.sha = hash || "";
  el.dataset.subject = subject || "";
  el.dataset.short = short || ""; // copy what is SHOWN, not a second abbreviation
  el.innerHTML =
    (short ? `<span class="csha" title="right-click the header to copy the commit id, title, date or author">${esc(short)}</span> ` : "") +
    esc(subject || "");
}


// The header's menu does not depend on WHICH part of it was clicked: an open
// commit offers everything the header knows, in one list. Aiming at the sha
// to get the sha and at the date to get the date is a rule you have to learn
// and can get wrong; a single list is read at a glance.
//
// A SELECTION overrides it. Once text is highlighted — a date-and-author
// drag, a span across the title and the sha — "copy" can only mean that text,
// and offering five other copies next to it would be noise.
$("files-header").addEventListener("contextmenu", (e) => {
  const title = $("files-title");
  const hash = title.dataset.sha;
  // The MODE decides, not just the stored sha: a stage that writes the title
  // through some other path would otherwise offer the previous commit's id.
  if (state.filesMode !== "commit" || !hash) return; // no commit here: the browser's own menu
  // Read now, into the closure: clicking a menu row moves focus and the
  // selection is gone by the time act() runs.
  const sel = window.getSelection();
  const text = sel && !sel.isCollapsed && $("files-header").contains(sel.anchorNode) ? sel.toString() : "";
  e.preventDefault();
  if (text) {
    showCtxMenu([{ label: "copy", act: () => copyText(text, "selection") }], e.clientX, e.clientY);
    return;
  }
  // The short form the row SHOWS (git's own abbreviation, handed over with
  // the feed row) — copying a different length than the one on screen reads
  // as a different commit. A commit opened by hash has no row, so fall back.
  const short = title.dataset.short || hash.slice(0, 9);
  const meta = $("files-meta");
  const rows = [
    { label: "copy short commit id", act: () => copyText(short, "commit id " + short) },
    { label: "copy commit id", act: () => copyText(hash, "commit id " + short) },
  ];
  // A commit opened by hash (a sidebar tag, a reflog entry) may have a title
  // that is not a subject, and a date the server could not resolve — each row
  // appears only when there is something behind it to copy.
  if (title.dataset.subject) {
    rows.push({ label: "copy commit title", act: () => copyText(title.dataset.subject, "commit title") });
  }
  if (meta.dataset.date) rows.push({ label: "copy date", act: () => copyText(meta.dataset.date, "date") });
  if (meta.dataset.author) rows.push({ label: "copy author", act: () => copyText(meta.dataset.author, "author") });
  showCtxMenu(rows, e.clientX, e.clientY);
});


// --- commit meta line (pure; guarded against Go) ---
// commitMetaLine formats /api/commit/{sha}'s date + author exactly as the TUI
// renders it — "2026-08-17 15:04 · gigagit", local time, the commit's AUTHOR
// date. "" when the server could not resolve one, and then no line is drawn
// rather than an "(unknown)" placeholder parked under the title. The claim
// "the browser and the terminal show the same stamp" is a claim about THIS
// function against the Go formatter; commitmetajs_test.go is what keeps it
// true, so keep the section pure (no DOM) and the markers in place.
function commitMetaLine(body) {
  const { date, author } = commitMetaParts(body);
  if (!date) return "";
  return author ? `${date} · ${author}` : date;
}


// commitMetaParts is the same stamp before it is joined: the header's
// right-click menu copies the date and the author on their own, and pulling
// them back out of the rendered line would mean parsing around a " · " that
// an author name may itself contain.
function commitMetaParts(body) {
  if (!body || !body.time) return { date: "", author: "" };
  const d = new Date(body.time * 1000);
  const p = (n) => String(n).padStart(2, "0");
  const date = `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
  return { date, author: body.author || "" };
}


// commitBody is the commit's DESCRIPTION: the full message minus its subject,
// split by git's own rule — the subject runs to the first blank line, the
// description is everything after it. "" when there is none. Trailing blank
// lines go (git leaves one after the message); inner ones stay, they are the
// paragraph breaks. Pure, and pinned by commitbodyjs_test.go: a wrong split
// shows the subject twice or eats the first paragraph.
function commitBody(message) {
  const lines = String(message || "").replace(/\r\n?/g, "\n").split("\n");
  let i = lines.findIndex((l) => l.trim() === "");
  if (i < 0) return "";
  while (i < lines.length && lines[i].trim() === "") i++;
  let j = lines.length;
  while (j > i && lines[j - 1].trim() === "") j--;
  return lines.slice(i, j).join("\n");
}
// --- end commit meta line ---


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
  $("files-title").textContent = state.compare.a + " ↔ " + state.compare.b;
  applyCompareFilter();
  focusPane();
}


// openEntryCompare shows a comparison one of whose sides is FROZEN — a
// shelf entry standing in for a commit that no longer exists (see
// /api/compare-entry). It is the same screen as a branch compare, with two
// differences it has to be honest about:
//
//   - the per-file diffs cannot go through /api/diff, because git cannot see
//     a tar in gg's state dir; they go through /api/entry-diff, addressed by
//     the two SPECS the server resolved rather than by hash;
//   - there is no merge base between a snapshot and a commit, so there are no
//     origin sets and the per-side filter is off.
//
// The fallback is named in the header. "compared against a shelved copy
// because the commit is gone" is a materially different statement from
// "compared against that commit", and the difference is invisible otherwise.
function openEntryCompare(body) {
  state.detailGen++;
  state.compare = {
    a: body.left.label,
    b: body.right.label,
    aHash: body.left.hash || "",
    bHash: body.right.hash || "",
    aSpec: body.left.spec,
    bSpec: body.right.spec,
    all: body.files || [],
    filter: "all",
    originsError: "",
    frozen: !!body.frozen,
    note: body.frozen_note || "",
  };
  state.filesMode = "compare";
  state.fileSha = null;
  enterFilesStage();
  $("files-title").textContent =
    state.compare.a + " ↔ " + state.compare.b + (state.compare.note ? " — " + state.compare.note : "");
  applyCompareFilter();
  focusPane();
}


// openEntryFileDiff opens ONE file between two arbitrary sides (a stored copy
// and the file here, typically). Both labels go in the title: a diff whose
// sides are not named is unreadable when neither of them is "the commit you
// are looking at".
async function openEntryFileDiff({ left, right, path, leftLabel, rightLabel, status }) {
  const gen = ++state.detailGen;
  if (state.layout !== "diff") {
    state.pane = "files";
    setLayout("diff");
    focusPane();
  }
  clearDiffHunks();
  state.diffCtx = null; // history/blame need a rev; a stored copy has none
  state.diffRow = null; // …and the previous diff's marked row must not paint a row of this one
  setDiffTitle(path, leftLabel + " ↔ " + rightLabel + " · ");
  $("diff-body").innerHTML = `<div class="notice">loading…</div>`;
  updateDiffNav();
  const q = new URLSearchParams({ left, right, path });
  if (status) q.set("status", status);
  try {
    const d = await getJSON("/api/entry-diff?" + q);
    if (gen !== state.detailGen) return; // superseded by a newer open or esc
    renderDiff(d);
    jumpToFirstChange();
  } catch (e) {
    if (gen !== state.detailGen) return;
    $("diff-body").innerHTML = `<div class="notice">error: ${esc(e.message || e)}</div>`;
    updateDiffNav();
  }
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
      c.all.length ? "no files match this filter" : c.frozen ? "nothing differs" : "the two branches are identical"
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
  // A frozen side has no merge base to derive origin sets from — the filter
  // is not "unavailable this time", it has no meaning here — so the bar shows
  // the count alone rather than two buttons that can only disappoint. The
  // frozen note itself is in the header, where the two sides are named; a
  // second copy here would only repeat it.
  if (c.frozen) {
    bar.innerHTML = `<button class="on" disabled>all (${c.all.length})</button>`;
    return;
  }
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
  $("files-title").textContent = "Working tree";
  renderFiles();
  focusPane();
}


const SECTION_LABELS = { staged: "Staged", changes: "Changes", untracked: "Untracked", conflicts: "Conflicts" };


// The sort chip belongs to the working-tree list alone: a commit's file list
// and a compare are both rendered from a server-ordered payload, and neither
// has a stored order to cycle.
function renderFilesSort() {
  const chip = $("files-sort");
  const on = state.filesMode === "status";
  chip.classList.toggle("hidden", !on);
  chip.innerHTML = on ? sortChipHTML("files") : "";
}


// cycleFilesSort re-orders the working-tree list under the next mode. The
// order lives in statusEntries (every index — cursor, marks, hit-testing —
// points into it), so the list is REBUILT rather than sorted at render time,
// and the cursor is carried across by path so it stays on the same file.
function cycleFilesSort() {
  setSortMode("files", nextSortMode("files"));
  const at = state.statusEntries[state.fileCursor];
  buildStatusEntries();
  if (at) {
    const i = state.statusEntries.findIndex((f) => f.path === at.path && f.section === at.section);
    if (i >= 0) state.fileCursor = i;
  }
  renderFiles();
}


$("files-sort").addEventListener("click", (e) => {
  if (!e.target.closest(".sortchip")) return;
  cycleFilesSort();
});


// The path budget is in COLUMNS of the list, so every width change — the
// rs-detail drag, the sidebar toggle, a window resize — has to re-render the
// rows or a widened pane keeps showing yesterday's elision. One observer on
// the list covers all three. rAF-coalesced: a drag fires this per pixel.
let filesPending = false;
new ResizeObserver(() => {
  if (filesPending) return;
  filesPending = true;
  requestAnimationFrame(() => {
    filesPending = false;
    // A hidden list (the commit-list layout, a pane collapsed to nothing)
    // reports 0 and has nothing to re-elide — and renderFiles is not a pure
    // read: it also decides whether the commit box and the staging buttons
    // are on screen. A resize to nothing must not put them back.
    if (!$("files-list").clientWidth) return;
    renderFiles();
  });
}).observe($("files-list"));


// --- file-row path elision ---
//
// A row used to be cut by CSS `text-overflow: ellipsis`, which drops the TAIL
// — exactly the half that says which file this is. The TUI cuts the same paths
// through elidePath (internal/tui/elide.go, ported to core.js and guarded by
// elide_jsport_test.go): whole segments go from the MIDDLE, so the file name
// and the head of the path both survive. The browser now does the same.
//
// The budget is measured, not guessed: the list's pixel width over one
// monospace column, less the row padding and whatever else sits on the row.
// The CSS ellipsis stays as a backstop for the cases this cannot predict (a
// proportional fallback font, a note badge wider than its estimate), so a
// column is left spare rather than filled to the edge.
const FILE_ROW_PAD = 16; // #files-list li padding: 2px 8px
const FILE_ST_COLS = 2; // .st — width: 1.5em, plus its gap
const FILE_BTN_COLS = 3; // the s/u button on a working-tree row
const NOTE_BADGE_COLS = 4; // "◆N" plus its margin, charged to every row in a list that has one

// fileCols is the path budget in columns, or 0 when the list has no usable
// width — renderFiles also runs while the detail pane is hidden (clientWidth
// 0) and while the browser has yet to lay it out, and elidePath(p, 0) is "".
// Callers treat 0 as "render the path whole".
function fileCols(extra) {
  const el = $("files-list");
  const px = el ? el.clientWidth : 0;
  if (px <= 0) return 0;
  const cols = Math.floor((px - FILE_ROW_PAD) / charWidth()) - FILE_ST_COLS - extra - 1;
  return cols >= 4 ? cols : 0; // below "…/x" there is nothing useful to show
}


// filePathHTML renders one row's path, middle-elided to the budget, carrying
// the full path as the row's tooltip so nothing is actually lost.
function filePathHTML(path, cols) {
  return `<span class="fpath" title="${esc(path)}">${esc(cols ? elidePath(path, cols) : path)}</span>`;
}


function renderFiles() {
  // Driven off filesMode, not off state.compare, so the bar cannot linger
  // into the next commit's detail screen.
  renderCompareBar();
  renderFilesSort();
  if (state.filesMode !== "status") {
    $("files-actions").classList.add("hidden");
    $("commit-box").classList.add("hidden");
    $("conflict-note").classList.add("hidden");
    // A commit file's notes are keyed "<sha>:<path>" — the sha this row's diff
    // would open. A COMPARISON gets no badge at all: its diff is not
    // note-addressable (see openFile), so a ◆ would advertise notes that its
    // rows cannot show and its keys cannot add.
    const cmp = state.filesMode === "compare";
    // …except a MERGE PREVIEW, whose compare is note-addressable: its per-file
    // totals come from the gathered set (state.previewCounts), not from the
    // per-commit index, because a note on an older commit of the branch counts
    // for the file too.
    const prev = openPreviewCtx();
    const badge = (f) =>
      prev && state.previewCounts
        ? noteBadgeHTML(state.previewCounts[f.path])
        : cmp
        ? ""
        : noteBadgeHTML(state.noteCounts.by_commit_path[(f.sha || state.fileSha) + ":" + f.path]);
    const anyBadge = state.files.some((f) => badge(f) !== "");
    const cols = fileCols(anyBadge ? NOTE_BADGE_COLS : 0);
    $("files-list").innerHTML = state.files
      .map(
        (f, i) =>
          `<li class="${i === state.fileCursor ? "sel" : ""}" data-i="${i}">` +
          `<span class="st ${esc(f.status)}">${esc(f.status)}</span>` +
          filePathHTML(f.path, cols) +
          badge(f) +
          `</li>`
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
  const anyBadge = state.statusEntries.some((f) => state.noteCounts.by_path[f.path] > 0);
  const cols = fileCols(FILE_BTN_COLS + (anyBadge ? NOTE_BADGE_COLS : 0));
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
      `<span class="st">${esc(badge)}</span>` +
      filePathHTML(f.path, cols) +
      noteBadgeHTML(state.noteCounts.by_path[f.path]) +
      `${btn}</li>`;
  });
  $("files-list").innerHTML = html;
}


// noteBadgeHTML is the ◆N marker the TUI draws on a row with review notes
// (threads, replies excluded). "" for none, so a repo without notes renders
// byte-identically to before.
function noteBadgeHTML(n) {
  return n > 0 ? `<span class="notebadge">◆${n}</span>` : "";
}


// openPreviewCtx is the open merge preview WHEN the compare on screen is still
// the one it opened — the single predicate the preview's note lane gates on.
// state.previewOpen alone is not enough: it is only cleared on the next
// refresh, so a branch↔branch compare opened in between would otherwise
// inherit the preview's tip and its badges (previews.js's previewShowing makes
// the same check for the same reason). It reads state only, so files.js does
// NOT import previews.js — previews.js imports THIS module.
function openPreviewCtx() {
  const po = state.previewOpen;
  if (state.filesMode !== "compare" || !po || !po.tip) return null;
  return state.compare && state.compare.bHash === po.tip ? po : null;
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
  // A frozen entry compare addresses its sides by SPEC, not by two hashes:
  // one of them is a snapshot in gg's own store that git cannot read.
  if (state.filesMode === "compare" && state.compare.frozen) {
    return openEntryFileDiff({
      left: state.compare.aSpec,
      right: state.compare.bSpec,
      path: f.path,
      status: f.status,
      leftLabel: state.compare.a,
      rightLabel: state.compare.b,
    });
  }
  // A compare has two revisions and no single provenance. rev stays the
  // RIGHT-hand hash — the history/blame buttons read diffCtx.rev — but notes
  // are OFF here: this table's new side really is bHash, while its old side is
  // aHash, and a stored commit note resolves its old side against bHash^. A
  // note on a line the comparison removed would therefore be filed against a
  // base it was never taken on and swept away. The TUI refuses notes on a
  // compare view for exactly this reason.
  // A merge preview is the ONE compare whose new side is a real commit's
  // content (the source tip), so its rows ARE note-addressable — at the tip,
  // new side only. Every other compare stays notes:false.
  const cmp = state.filesMode === "compare";
  const prev = openPreviewCtx();
  state.diffCtx = {
    path: f.path,
    rev: prev ? prev.tip : cmp ? state.compare.bHash : f.sha || state.fileSha,
    state: "commit",
    notes: !cmp || !!prev,
    // The pair the gathered note set is read by: a preview note may have been
    // written against an OLDER commit on the branch, so the query is the pair,
    // never this one tip.
    preview: prev ? { source: prev.source, target: prev.target } : null,
    // links.js documents ctx.compare as THE refusal for a two-revision view;
    // carrying it here means the diff-LINE copy-link path uses that documented
    // guard too, instead of relying on notesArmed() to happen to be off.
    compare: cmp,
  };
  state.diffRow = null;
  state.notes = [];
  const q = new URLSearchParams({ path: f.path, status: f.status });
  if (state.filesMode === "compare") {
    q.set("left", state.compare.aHash);
    q.set("right", state.compare.bHash);
  } else {
    q.set("sha", f.sha || state.fileSha);
  }
  if (f.old_path) q.set("old", f.old_path);
  setDiffTitle(f.path);
  $("diff-body").innerHTML = `<div class="notice">loading…</div>`;
  updateDiffNav();
  try {
    // The notes ride ALONGSIDE the diff fetch, not after it: the ◆ rows have
    // to be in the first paint, and a second serial round-trip would show the
    // diff without them first.
    const [d] = await Promise.all([getJSON("/api/diff?" + q), fetchNotes(false)]);
    renderDiff(d);
    jumpToFirstChange();
  } catch (e) {
    $("diff-body").innerHTML = `<div class="notice">error: ${esc(e.message || e)}</div>`;
    updateDiffNav();
  }
}


async function openStatusDiff(i) {
  clearDiffHunks();
  const f = state.statusEntries[i];
  state.diffCtx =
    f.section === "conflicts" ? null : { path: f.path, rev: "", state: sectionNoteState(f.section) };
  state.diffRow = null;
  state.notes = [];
  setDiffTitle(f.path);
  if (f.section === "conflicts") return openConflictPicker(f);
  const q = new URLSearchParams({ wt: f.section === "staged" ? "staged" : "unstaged", path: f.path });
  if (f.orig_path) q.set("old", f.orig_path);
  $("diff-body").innerHTML = `<div class="notice">loading…</div>`;
  updateDiffNav();
  try {
    const [d] = await Promise.all([getJSON("/api/diff?" + q), fetchNotes(false)]);
    // server tags eligible unstaged diffs with hunk ordinals — arm inline
    // staging BEFORE the render so the rows pick up their hk classes
    if (d.hunks && hunkEligible(f)) {
      diffHunks = { path: f.path, hash: d.hunks.hash, count: d.hunks.count, picks: new Set() };
    }
    renderDiff(d);
    renderHunkBar();
    jumpToFirstChange();
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
  $("files-title").textContent = "";
  setDiffTitle("");
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
    $("files-title").textContent = "error: " + (e.message || e);
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


// renderCell escapes one cell's text and wraps (a) word-diff spans in
// <mark class="l|r"> and (b) syntax runs in <span class="tk-…">. Both are
// rune ranges over the raw line; a mark wins over a syntax class inside it so
// the emphasis stays legible, matching the TUI.
function renderCell(text, spans, toks, side) {
  if ((!spans || !spans.length) && (!toks || !toks.length)) return esc(text);
  const rs = runes(text);
  const emph = new Array(rs.length).fill(false);
  for (const [a, b] of spans || []) for (let i = a; i < b && i < rs.length; i++) emph[i] = true;
  const cls = new Array(rs.length).fill("");
  // The class suffix goes straight into a class attribute, so only the shape
  // syntax.Class.String() produces is accepted; anything else stays plain.
  // Filtering here (not at emit time) lets a rejected run merge with the
  // neighbouring plain text instead of splitting it.
  for (const [a, b, c] of toks || []) {
    if (!/^[a-z]{2,3}$/.test(c)) continue;
    for (let i = a; i < b && i < rs.length; i++) cls[i] = c;
  }
  let out = "";
  for (let i = 0; i < rs.length; ) {
    let j = i + 1;
    while (j < rs.length && emph[j] === emph[i] && cls[j] === cls[i]) j++;
    const seg = esc(rs.slice(i, j).join(""));
    if (emph[i]) out += `<mark class="${side}">${seg}</mark>`;
    else if (cls[i]) out += `<span class="tk-${cls[i]}">${seg}</span>`;
    else out += seg;
    i = j;
  }
  return out;
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


// --- diff collapse (pure; guarded against Go) ---
// The "changed lines only" view — the TUI's f toggle. DIFF_CONTEXT is the
// TUI's diffContext: the equal rows kept on each side of a change.
const DIFF_CONTEXT = 3;

// collapseDiffRows folds an aligned row list the way textdiff.Collapse does:
// every non-"same" row plus DIFF_CONTEXT equal rows on each side is kept, and
// each remaining run of equal rows becomes ONE item {fold: n, start: i} — n
// rows hidden, i the index of the first. A fold whose start is in `open` is
// emitted expanded (its rows kept), so a reader can unfold one run without
// leaving the mode. keep(row, i) forces a row visible regardless — a row that
// carries a review note or an attention band must never vanish under a fold.
// No change rows at all → null (the caller shows a notice: a mode-only or
// whitespace-only change has nothing to fold around).
function collapseDiffRows(rows, open, keep) {
  const n = rows.length;
  const mark = new Array(n).fill(false);
  let any = false;
  for (let i = 0; i < n; i++) {
    if (rows[i].kind === "same") continue;
    any = true;
    const hi = Math.min(n - 1, i + DIFF_CONTEXT);
    for (let j = Math.max(0, i - DIFF_CONTEXT); j <= hi; j++) mark[j] = true;
  }
  if (!any) return null;
  const shown = (i) => mark[i] || (keep ? keep(rows[i], i) : false);
  const out = [];
  for (let i = 0; i < n; ) {
    if (shown(i)) {
      out.push(rows[i]);
      i++;
      continue;
    }
    const start = i;
    while (i < n && !shown(i)) i++;
    if (open && open.has(start)) for (let j = start; j < i; j++) out.push(rows[j]);
    else out.push({ fold: i - start, start });
  }
  return out;
}
// --- end diff collapse ---


// foldRowHTML is the separator standing in for a folded run: a full-width
// row (cols = the layout's column count) that reads "N unchanged lines" and
// unfolds on click (data-fold = the run's start index, the key `open` uses).
// It carries the `same` class so diffChangeBlocks never counts it as a change
// start, and no data-no / data-hunk, so the note and hunk click paths ignore
// it.
function foldRowHTML(it, cols) {
  return (
    `<tr class="same fold" data-fold="${it.start}" data-fold-n="${it.fold}" title="click to show these lines">` +
    `<td colspan="${cols}">⋯ ${it.fold} unchanged line${it.fold === 1 ? "" : "s"}</td></tr>`
  );
}


// diffHTML builds the diff table for a /api/diff response — shared by the
// main diff pane and the history overlay. paneWidth picks side-by-side vs
// unified exactly as before. Hunk classes no-op when diffHunks is null, so
// non-staging consumers get a plain read-only table.
//
// notesOn arms the review-note decoration: the per-row data-side/data-no
// anchor, the clicked row's `cur` class, and the ◆ rows themselves. It is OFF
// by default so the file-history overlay — a different file at a different
// revision — stays a plain table rather than borrowing the open diff's notes.
function diffHTML(d, paneWidth, notesOn = false, open = state.diffFolds) {
  if (d.binary) return `<div class="notice">binary file</div>`;
  if (d.too_large) return `<div class="notice">diff too large</div>`;
  const rows = d.rows || [];
  // anchor/cur/after are no-ops when notesOn is false, so the two layouts
  // below read the same either way.
  const anchor = (side, no) => (notesOn && no ? ` data-side="${side}" data-no="${no}"` : "");
  const curCls = (side, no) =>
    notesOn && no && state.diffRow && state.diffRow.side === side && state.diffRow.no === no ? " cur" : "";
  // A split row is "cur" when the mark sits on EITHER of its sides.
  const curClsBoth = (r) => curCls("new", r.right_no) || curCls("old", r.left_no);
  // attnCls: the row class a `gg session highlight` paints. A CLASS on the tr,
  // not an inline background: tr.add/tr.del paint their own cells, so the CSS
  // paints the band as a per-cell `background` rule of HIGHER specificity
  // (`table.diff tr.attn-<tone> td.side`) that replaces the add/del tint on
  // marked rows. It is derived HERE, inside diffHTML, never toggled after the
  // render: renderDiff re-runs from state.lastDiff on a window resize and on
  // every notes refresh, so a class added post-hoc the way markDiffRow adds
  // `cur` would silently vanish.
  // The lookup is done ONCE, not per row: the key is fixed for the whole
  // table. Like curCls it is gated on notesOn, so the file-history overlay —
  // a different file at a different revision, rendered through this same
  // function — never borrows the open diff's bands.
  const attnMarks = (notesOn && state.attention.size && state.attention.get(attnKey(state.diffCtx))) || null;
  const attnCls = (side, no) => {
    if (!no || !attnMarks) return "";
    for (const m of attnMarks) {
      if (m.side === side && no >= m.start && no <= m.end) return " attn-" + m.tone;
    }
    return "";
  };
  const attnClsBoth = (r) => attnCls("new", r.right_no) || attnCls("old", r.left_no);
  const after = (cols, ...pairs) => {
    if (!notesOn) return "";
    let out = "";
    for (const [side, no] of pairs) out += noteRowsHTML(side, no, cols);
    return out;
  };
  // An all-new or all-deleted file renders single-column: a side-by-side
  // with one permanently empty side wastes half the pane and forces harsh
  // wrapping on the populated half.
  const pureAdd = rows.length > 0 && rows.every((r) => !r.left_no);
  const pureDel = rows.length > 0 && rows.every((r) => !r.right_no);
  // "Changed lines only" (the TUI's f): fold the equal runs, keeping every
  // row a note or an attention band sits on — folded away, a ◆ row would
  // vanish and }/{ could not reach it. Layout is decided on the FULL rows
  // above (a fold row has no line numbers to vote with).
  let items = rows;
  if (state.diffPartial) {
    const noted = (side, no) => !!no && state.notes.some((n) => n.side === side && n.line === no);
    const pinned = (r) => noted("new", r.right_no) || noted("old", r.left_no) || !!attnClsBoth(r);
    items = collapseDiffRows(rows, open, notesOn ? pinned : null);
    if (items === null) {
      return (
        `<div class="notice">no changed lines — a mode or whitespace-only change; ` +
        `<b>f</b> shows the full file</div>`
      );
    }
  }
  // The table is `table-layout: fixed`, which sizes its columns from the
  // FIRST row's cells — and in the changes-only view that row is often a
  // colspan fold, which would hand every column an equal share and balloon
  // the line-number gutter to a quarter of the pane. A colgroup fixes the
  // gutter widths up front, whatever row comes first.
  const cols = pureAdd || pureDel ? 2 : paneWidth < 950 ? 3 : 4;
  const colgroup =
    cols === 2 ? `<col class="no"><col>` : cols === 3 ? `<col class="no"><col class="no"><col>` : `<col class="no"><col><col class="no"><col>`;
  let html = `<table class="diff"><colgroup>${colgroup}</colgroup>`;
  if (pureAdd || pureDel) {
    const side = pureAdd ? "r" : "l";
    const nside = pureAdd ? "new" : "old";
    for (const r of items) {
      if (r.fold) {
        html += foldRowHTML(r, 2);
        continue;
      }
      const no = pureAdd ? r.right_no : r.left_no;
      const text = pureAdd ? r.right : r.left;
      const spans = pureAdd ? r.right_spans : r.left_spans;
      const toks = pureAdd ? r.right_tok : r.left_tok;
      html +=
        `<tr class="${r.kind}${hunkCls(r)}${curCls(nside, no)}${attnCls(nside, no)}"${hunkAttr(r)}${anchor(nside, no)}>` +
        `<td class="no ${side}">${no || ""}</td>` +
        `<td class="side ${side}"><span class="pan">${renderCell(text, spans, toks, side)}</span></td></tr>` +
        after(2, [nside, no]);
    }
  } else if (paneWidth < 950) {
    // Unified: below ~950px each side-by-side half is too narrow to read
    // (heavy wrapping, context text duplicated on both sides). One
    // full-width column; a changed pair becomes a del row then an add row,
    // keeping the intraline marks of each side.
    for (const r of items) {
      if (r.fold) {
        html += foldRowHTML(r, 3);
        continue;
      }
      if (r.kind === "same") {
        html +=
          `<tr class="same${curCls("new", r.right_no)}${attnClsBoth(r)}"${anchor("new", r.right_no)}>` +
          `<td class="no l">${r.left_no || ""}</td>` +
          `<td class="no r">${r.right_no || ""}</td>` +
          `<td class="side"><span class="pan">${renderCell(r.right, null, r.right_tok, "r")}</span></td></tr>` +
          after(3, ["new", r.right_no], ["old", r.left_no]);
      } else {
        if (r.kind !== "add")
          html +=
            `<tr class="del${hunkCls(r)}${curCls("old", r.left_no)}${attnCls("old", r.left_no)}"${hunkAttr(r)}${anchor("old", r.left_no)}>` +
            `<td class="no l">${r.left_no || ""}</td><td class="no r"></td>` +
            `<td class="side l"><span class="pan">${renderCell(r.left, r.left_spans, r.left_tok, "l")}</span></td></tr>` +
            after(3, ["old", r.left_no]);
        if (r.kind !== "del")
          html +=
            `<tr class="add${hunkCls(r)}${curCls("new", r.right_no)}${attnCls("new", r.right_no)}"${hunkAttr(r)}${anchor("new", r.right_no)}>` +
            `<td class="no l"></td><td class="no r">${r.right_no || ""}</td>` +
            `<td class="side r"><span class="pan">${renderCell(r.right, r.right_spans, r.right_tok, "r")}</span></td></tr>` +
            after(3, ["new", r.right_no]);
      }
    }
  } else {
    for (const r of items) {
      if (r.fold) {
        html += foldRowHTML(r, 4);
        continue;
      }
      // A side-by-side row anchors on its NEW side when it has one, else on
      // the old side: every row must carry a usable (side, line) pair, or a
      // pure-deletion row would answer `c` with line 0.
      const aside = r.right_no ? "new" : "old";
      const ano = r.right_no || r.left_no;
      // Both sides' numbers ride on the row (data-lno / data-rno) so a click
      // in the LEFT pane can anchor a note on the old side of a row whose
      // default anchor is the new side.
      const both = notesOn ? ` data-lno="${r.left_no || 0}" data-rno="${r.right_no || 0}"` : "";
      html +=
        `<tr class="${r.kind}${hunkCls(r)}${curClsBoth(r)}${attnClsBoth(r)}"${hunkAttr(r)}${anchor(aside, ano)}${both}>` +
        `<td class="no l">${r.left_no || ""}</td>` +
        `<td class="side l"><span class="pan">${renderCell(r.left, r.left_spans, r.left_tok, "l")}</span></td>` +
        `<td class="no r">${r.right_no || ""}</td>` +
        `<td class="side r"><span class="pan">${renderCell(r.right, r.right_spans, r.right_tok, "r")}</span></td></tr>` +
        after(4, ["new", r.right_no], ["old", r.left_no]);
    }
  }
  html += "</table>";
  if (d.truncated) html += `<div class="notice">alignment truncated (size guard)</div>`;
  return html;
}


function renderDiff(d) {
  // A NEW diff starts with every run folded; a re-render of the same one (a
  // resize, a notes refresh, the f toggle) keeps the folds the reader opened.
  if (d !== state.lastDiff) state.diffFolds = new Set();
  state.lastDiff = d; // re-rendered on window resize (layout is width-dependent)
  state.diffBlockIdx = -1;
  $("diff-body").innerHTML = diffHTML(d, $("diff-pane").clientWidth, true);
  mountPanBars($("diff-body"), $("diff-hbars"));
  updateDiffNav();
}


// visibleChangeBlock is the ordinal of the change block the reader is looking
// at: the first one inside the pane's viewport, else the nearest one above it
// (the reader scrolled past it and is reading its trailing context), else -1
// (nothing on screen yet, or no change above the fold).
function visibleChangeBlock() {
  const pane = $("diff-pane").getBoundingClientRect();
  const blocks = diffChangeBlocks();
  let above = -1;
  for (let i = 0; i < blocks.length; i++) {
    const r = blocks[i].getBoundingClientRect();
    if (r.bottom >= pane.top && r.top <= pane.bottom) return i;
    if (r.bottom < pane.top) above = i;
  }
  return above;
}


// rerenderDiffKeepingPlace re-runs renderDiff for the SAME diff and puts the
// reader back where they were. The TUI re-anchors on the change block under
// its cursor; the web has no cursor, so the viewport stands in: the block on
// screen (else the last one above it, else the one the ‹/› stepper named)
// is scrolled back to the same place. The count is the same in both view
// modes — folds only ever replace equal rows — so the ordinal carries across.
// keepScroll: restore the pane's scrollTop instead (an unfold inserts rows at
// the fold's own position, so everything above it stays put — the reader
// must not be thrown to the block they last stepped to).
// A conflict picker owns #diff-body while open: never draw the diff over it.
function rerenderDiffKeepingPlace(keepScroll = false) {
  if (!state.lastDiff || conflictPick) return;
  const pane = $("diff-pane");
  const top = pane.scrollTop;
  const stepped = state.diffBlockIdx;
  const seen = visibleChangeBlock();
  const at = seen >= 0 ? seen : stepped;
  renderDiff(state.lastDiff);
  state.diffBlockIdx = at;
  if (keepScroll) {
    pane.scrollTop = top;
    return;
  }
  const blocks = diffChangeBlocks();
  if (at >= 0 && blocks[at]) blocks[at].scrollIntoView({ block: "center" });
}


// revealDiffRow returns the rendered row for (side, line), unfolding the run
// that hides it first when the changes-only view folded it away — an agent
// steering the page to a line (gg session navigate) must land on it, never
// on a silent miss. null when the diff has no such line at all.
function revealDiffRow(side, line) {
  const find = () =>
    document.querySelector(`#diff-body tr[data-side="${side}"][data-no="${line}"]`) ||
    (side === "old" ? document.querySelector(`#diff-body tr[data-lno="${line}"]`) : null);
  let tr = find();
  if (tr || !state.diffPartial || !state.lastDiff) return tr;
  const rows = state.lastDiff.rows || [];
  const i = rows.findIndex((r) => (side === "new" ? r.right_no : r.left_no) === line);
  if (i < 0) return null;
  for (const f of document.querySelectorAll("#diff-body tr.fold[data-fold]")) {
    const start = Number(f.dataset.fold);
    if (i >= start && i < start + Number(f.dataset.foldN)) {
      state.diffFolds.add(start);
      rerenderDiffKeepingPlace(true);
      tr = find();
      break;
    }
  }
  return tr;
}


// applyDiffView sets the diff view mode from a stored preference ("changed"
// = changed lines only, anything else = full file) and paints the toolbar
// button's pressed state. Boot calls it before any diff is open, so nothing
// re-renders here; toggleDiffView is the user-facing flip.
function applyDiffView(mode) {
  state.diffPartial = mode === "changed";
  const btn = $("diff-view");
  btn.classList.toggle("on", state.diffPartial);
  btn.setAttribute("aria-pressed", state.diffPartial ? "true" : "false");
}


// toggleDiffView is the TUI's f: changed lines only ↔ the full file. The
// choice is a per-machine preference (/api/uistate — the random port makes
// localStorage useless), and applies to every diff opened from now on, the
// file-history overlay included.
// keepScroll: an unfold-all flip (the last fold row clicked) redraws the same
// rows and must not throw the reader to the nearest change block.
//
// Flipping ON starts from every run folded, whatever the reader had opened
// before flipping off: the folds are the changes-only VIEW's state, and
// off→on is a fresh entry into it, not a resume.
function toggleDiffView(keepScroll = false) {
  const on = !state.diffPartial;
  applyDiffView(on ? "changed" : "full");
  if (on) state.diffFolds = new Set();
  saveUI({ diff_view: on ? "changed" : "full" });
  rerenderDiffKeepingPlace(keepScroll);
}


$("diff-view").addEventListener("click", toggleDiffView);


// Long-line display mode — the TUI's ctrl+w (scroll → wrap → cutoff, in that
// order). ONE mode for the diff pane, the history overlay and blame, kept as
// a class on <body> so the three viewers' CSS follows it without a re-render;
// the diff pane alone re-anchors on the change block in view, since a wrap
// ↔ scroll flip changes every row's height. Wrap is the default: the web
// always wrapped, so a stored layout without the field keeps its look.
const TEXT_MODES = ["scroll", "wrap", "cutoff"];

function applyTextMode(mode) {
  if (!TEXT_MODES.includes(mode)) mode = "wrap";
  state.textMode = mode;
  document.body.classList.toggle("lm-scroll", mode === "scroll");
  document.body.classList.toggle("lm-cut", mode === "cutoff");
  const b = $("diff-mode");
  b.textContent = mode;
  b.title = `long lines: ${mode} — w cycles scroll → wrap → cutoff`;
  const chip = document.querySelector('#foot button[data-act="textmode"]');
  if (chip) chip.textContent = "w " + mode;
}


// cycleTextMode is the user-facing step (key w, the toolbar chip, the footer
// chip). The choice is a per-machine preference (/api/uistate).
function cycleTextMode() {
  applyTextMode(TEXT_MODES[(TEXT_MODES.indexOf(state.textMode) + 1) % TEXT_MODES.length]);
  saveUI({ text_mode: state.textMode });
  if (state.layout === "diff") rerenderDiffKeepingPlace();
  else mountPanBars($("diff-body"), $("diff-hbars"));
}


// --- scroll mode: one horizontal scrollbar PER SIDE ---
// The table keeps the pane's width in every mode; in scroll mode each side
// cell hides its overflow and its content (the .pan span) is shifted by a
// CSS variable on the host — --pan-l for the old side, --pan-r for the new.
// A bar per side, pinned to the pane's bottom edge, drives its variable: the
// two halves scroll independently, so a long line on the right never drags
// the left half off screen (one table-wide scroll did exactly that). A
// single-column layout (unified, pure add/delete) shows one bar driving
// both variables. shift+wheel over a side pans that side's bar.
//
// host: the element holding the table (and the variables); bars: the .hbars
// element — a static sibling for the diff pane, appended per render for the
// history overlay. host._pan remembers the offsets across a re-render (a
// notes refresh, a resize) so the reader's place survives.
function mountPanBars(host, bars) {
  const table = host.querySelector("table.diff");
  if (state.textMode !== "scroll" || !table) {
    bars.classList.add("hidden");
    host.style.removeProperty("--pan-l");
    host.style.removeProperty("--pan-r");
    return;
  }
  const twoCol = table.querySelectorAll("colgroup col").length === 4;
  const widest = (sel) => {
    let w = 0;
    for (const p of host.querySelectorAll(sel)) if (p.offsetWidth > w) w = p.offsetWidth;
    return w;
  };
  const cell = host.querySelector("td.side");
  const cellW = cell ? cell.clientWidth - 12 : 0; // minus the cell padding
  const pan = host._pan || (host._pan = { l: 0, r: 0 });
  bars.classList.remove("hidden");
  bars.innerHTML = twoCol
    ? `<div class="hbar" data-side="l"><div></div></div><div class="hbar" data-side="r"><div></div></div>`
    : `<div class="hbar" data-side="lr"><div></div></div>`;
  for (const bar of bars.querySelectorAll(".hbar")) {
    const side = bar.dataset.side;
    const w = side === "l" ? widest("td.side.l > .pan") : side === "r" ? widest("td.side.r > .pan") : widest("td.side > .pan");
    bar.firstElementChild.style.width = Math.max(w - cellW, 0) + bar.clientWidth + "px";
    const apply = () => {
      const x = bar.scrollLeft;
      if (side !== "r") { pan.l = x; host.style.setProperty("--pan-l", x + "px"); }
      if (side !== "l") { pan.r = x; host.style.setProperty("--pan-r", x + "px"); }
    };
    bar.addEventListener("scroll", apply);
    bar.scrollLeft = side === "r" ? pan.r : pan.l;
    apply();
  }
  if (!host._panWheel) {
    host._panWheel = true;
    host.addEventListener("wheel", (e) => {
      if (state.textMode !== "scroll") return;
      const dx = e.shiftKey && !e.deltaX ? e.deltaY : e.deltaX;
      if (!dx) return;
      const td = e.target.closest && e.target.closest("td.side");
      if (!td) return;
      const want = td.classList.contains("l") ? "l" : "r";
      const bar = bars.querySelector(`.hbar[data-side="${want}"]`) || bars.querySelector(".hbar");
      if (!bar) return;
      bar.scrollLeft += dx;
      e.preventDefault();
    }, { passive: false });
  }
}


$("diff-mode").addEventListener("click", cycleTextMode);


registerHelp({
  key: "w · long lines",
  html:
    "cycle how lines wider than the pane are shown — <b>scroll</b> (each side gets its own " +
    "scrollbar at the pane's bottom edge and pans on its own; shift+wheel over a side pans it), <b>wrap</b> (the default) or <b>cutoff</b> (one line per row, " +
    "a trailing …) — the TUI's ctrl+w. The toolbar chip beside <b>changes only</b> names the current " +
    "mode and is the same switch; it applies to the diff, the file-history overlay and blame alike, " +
    "and is remembered per machine",
});


// A click on a fold row unfolds that run (and only that run) until the next
// diff opens. The stepper's place survives: an unfold adds equal rows only.
$("diff-body").addEventListener("click", (e) => {
  const tr = e.target.closest("tr.fold[data-fold]");
  if (!tr || !state.lastDiff) return;
  state.diffFolds.add(Number(tr.dataset.fold));
  rerenderDiffKeepingPlace(true);
  // The last run opened: what is on screen IS the full file, so the chip
  // says so — and the preference follows, exactly as a click on it would.
  if (!$("diff-body").querySelector("tr.fold")) toggleDiffView(true);
});


registerHelp({
  key: "f · changes only",
  html:
    "toggle the diff between the <b>full file</b> and <b>changed lines only</b> — each change with three " +
    "lines of context, every other run folded to a <i>⋯ N unchanged lines</i> row (click it to unfold " +
    "that run). The <b>changes only</b> button in the diff toolbar is the same switch; the choice is " +
    "remembered per machine and the file-history overlay follows it. Unfolding the last run flips the " +
    "switch off (the whole file is on screen); flipping it on again starts with every run folded. Rows " +
    "carrying a review note or an attention band never fold away",
});


// jumpToFirstChange parks a freshly OPENED diff on its first changed line
// rather than at the top of the file — the context above the first hunk can
// run for screens, and scrolling past it was the first thing anyone did.
// state.diffBlockIdx moves with it, so `change ›` continues from there
// instead of walking back over what is already on screen.
//
// Called by the three open paths only, never from renderDiff: that also runs
// on a window resize and on every notes refresh, and jumping there would pull
// the view out from under a reader mid-file. A diff with no changes at all
// (an open with only context, a mode-only change) leaves the view alone.
function jumpToFirstChange() {
  const blocks = diffChangeBlocks();
  if (!blocks.length) return;
  state.diffBlockIdx = 0;
  blocks[0].scrollIntoView({ block: "center" });
}


// --- review notes -----------------------------------------------------------
// Notes hang off (address, side, line). The browser never names a checkout —
// the server stamps Address.Worktree — so the wire carries only path, rev and
// the state allowlist. There is no line CURSOR here as there is in the TUI: a
// clicked diff row is marked `tr.cur` and `c` anchors on it.

// notesArmed reports whether the open diff is NOTE-ADDRESSABLE: a stored
// address must name the same two texts the table shows. A comparison is not
// (its old side is the compared revision, not bHash^), nor is an entry diff
// against a frozen copy git cannot read; both leave `notes: false` (or no
// diffCtx at all). Rows, keys and ◆ badges all gate on this one predicate.
function notesArmed() {
  return !!state.diffCtx && state.diffCtx.notes !== false;
}

// noteQuery builds the /api/notes query for the open diff. Working-tree diffs
// carry their section as `state`; a commit diff carries rev + state=commit.
function noteQuery() {
  if (!notesArmed()) return null;
  const q = new URLSearchParams({ path: state.diffCtx.path });
  if (state.diffCtx.preview) {
    // The preview gathers notes along the branch, so the READ is keyed on the
    // PAIR: a note written against an older commit still belongs here. rev and
    // state ride along unchanged because they are what the WRITE needs — a
    // preview note is an ordinary commit note on the source tip, and
    // addNotePrompt builds its post out of this same query.
    q.set("source", state.diffCtx.preview.source);
    q.set("target", state.diffCtx.preview.target);
    q.set("rev", state.diffCtx.rev);
    q.set("state", "commit");
    return q;
  }
  if (state.diffCtx.state === "commit") {
    if (!state.diffCtx.rev) return null;
    q.set("rev", state.diffCtx.rev);
    q.set("state", "commit");
  } else {
    q.set("state", state.diffCtx.state || "unstaged");
  }
  return q;
}


// sectionNoteState maps a working-tree section to the stored State — the old-side
// base the note re-anchors against. Getting this wrong points the resolver at
// the wrong blob (an untracked file has no index entry at all).
function sectionNoteState(section) {
  if (section === "staged") return "staged";
  if (section === "untracked") return "untracked";
  return "unstaged";
}


async function fetchNotes(rerender = true) {
  const q = noteQuery();
  if (!q) {
    state.notes = [];
    return;
  }
  const prev = !!state.diffCtx.preview;
  try {
    const d = await getJSON((prev ? "/api/preview/notes?" : "/api/notes?") + q);
    state.notes = d.notes || [];
    if (prev) {
      // The preview's read hands back every file's total in the same call, so
      // the file list's ◆N badges follow a write without a second round-trip.
      state.previewCounts = d.counts || {};
      renderFiles();
    }
  } catch {
    // Notes are best-effort — never break the diff — but a transient failure
    // must not make every visible ◆ row VANISH until the next notes event
    // either: keep what we already have and re-render nothing new.
    return;
  }
  if (rerender && state.lastDiff) {
    // A notes event is not a new diff: renderDiff resets the ‹/› change
    // stepper, so carry diffBlockIdx across. (The marked row is a class on a
    // <tr>, re-derived from state.diffRow by curCls, so it survives already.)
    const at = state.diffBlockIdx;
    renderDiff(state.lastDiff);
    state.diffBlockIdx = at;
  }
}


async function refreshNoteCounts() {
  try {
    const c = await getJSON("/api/notes/counts");
    state.noteCounts = {
      by_path: c.by_path || {},
      by_commit: c.by_commit || {},
      by_commit_path: c.by_commit_path || {},
    };
  } catch {
    // Counts are decoration, but a STALE badge is worse than none: a failed
    // fetch means we no longer know, so draw no ◆ at all until the next one
    // succeeds.
    state.noteCounts = { by_path: {}, by_commit: {}, by_commit_path: {} };
  }
  renderFiles();
}


// noteRowsHTML renders the <tr class="note"> rows anchored on (side, no).
// Agent notes are skipped while the agent layer is off; user notes always
// show — including a user reply under a hidden agent root (the TUI's rule:
// the filter is per ROW, by that row's own source).
function noteRowsHTML(side, no, cols) {
  if (!no || !notesArmed() || !state.notes.length) return "";
  let html = "";
  for (const n of state.notes) {
    if (n.side !== side || n.line !== no) continue;
    html += noteBoxHTML(n, cols);
  }
  return html;
}


// noteBoxHTML is one thread as a hunk-style box: the title in the top border
// ("agent note · author · path R204"), the summary in bold, the rationale,
// then each reply as its own block. In the split layout (4 columns) the box
// sits in the pane of the note's side and the other pane stays blank, so the
// note visibly hangs off one version; the single-column layouts span the row.
// With the agent layer off, agent-written parts drop out row by row; a thread
// with nothing left renders nothing.
function noteBoxHTML(n, cols) {
  const off = state.notesAgentOff;
  const rootOn = !(off && n.source === "agent");
  const reps = (n.replies || []).filter((r) => !(off && r.source === "agent"));
  if (!rootOn && !reps.length) return "";
  // A preview names it "outdated": there, a note whose lines a later commit
  // changed is the expected case, not an edge one — so the server sends that
  // word and the row wears a class of its own.
  const stale = n.status === "stale" || n.status === "outdated";
  const prev = !!(state.diffCtx && state.diffCtx.preview);
  const word = prev ? " (outdated)" : " (stale)";
  const cls = prev ? "outdated" : "stale";
  const agent = n.source === "agent";
  const title = (agent ? "agent note" : "note") + (n.author ? " · " + n.author : "") +
    " · " + state.diffCtx.path + " " + (n.side === "old" ? "L" : "R") + n.line + (stale ? word : "");
  const part = (m) => `<div class="notesum">${esc(m)}</div>`;
  const text = (r) => (r.rationale ? `<div class="notetext">${esc(r.rationale)}</div>` : "");
  let box = `<div class="notebox ${agent ? "agent" : "user"}${stale ? " " + cls : ""}"><div class="notetitle">${esc(title)}</div>`;
  if (rootOn) box += part(n.summary) + text(n);
  for (const r of reps) {
    box += `<div class="notereply" data-note="${esc(r.id)}">` + part("↳ " + (r.author ? r.author + ": " : "") + r.summary) + text(r) + `</div>`;
  }
  box += `</div>`;
  const cell = (span) => `<td class="note" colspan="${span}">${box}</td>`;
  const gap = `<td class="note-gap" colspan="2"></td>`;
  const cells = cols === 4 ? (n.side === "old" ? cell(2) + gap : gap + cell(2)) : cell(cols);
  return `<tr class="note${stale ? " " + cls : ""}${agent ? " agent" : ""}" data-note="${esc(n.id)}">${cells}</tr>`;
}


// markDiffRow makes tr the anchor `c` writes against. It toggles ONE class
// rather than re-rendering: a full renderDiff would reset diffBlockIdx (the
// ‹/› change stepper) and jolt the scroll position.
function markDiffRow(tr, side, no) {
  side = side || tr.dataset.side;
  no = no || Number(tr.dataset.no);
  if (!no) return;
  const prev = $("diff-body").querySelector("tr.cur");
  if (prev) prev.classList.remove("cur");
  tr.classList.add("cur");
  state.diffRow = { side, no };
  // Marking a row is an explicit "I am looking HERE", so it outranks a
  // previous }/{ landing for nearestNote. stepNote re-claims the id right
  // after its own call.
  noteStepId = null;
}


// firstChangedRow is where `c` lands when nothing has been clicked: the first
// changed row of the diff, which is what a reviewer means by "here".
function firstChangedRow() {
  for (const tr of diffChangeBlocks()) {
    const no = Number(tr.dataset.no);
    if (no) return { side: tr.dataset.side, no };
  }
  const any = $("diff-body").querySelector("tr[data-no]");
  return any ? { side: any.dataset.side, no: Number(any.dataset.no) } : null;
}


// firstNewSideRow is firstChangedRow restricted to rows a PREVIEW can anchor:
// its old side is the merge base, so only a new-side row is addressable.
//
// It WALKS EACH BLOCK, it does not just test the head. diffChangeBlocks
// returns the first row of each contiguous change run, and in the unified
// layout a modified line renders as a del row then an add row — so every
// modification block's head is `del`/old-side. Testing heads alone found
// nothing on such a file and fell through to tier two, which landed on the
// first new-side row of the WHOLE table: a context line ("Add note on new
// line 1") instead of the modified line the reviewer is looking at. So each
// head walks forward over its own run (note rows ride inside a block and are
// skipped; a `same` row ends it) and the first new-side row of the first
// block that has one wins.
//
// Tier two — any new-side row of the file — remains for a file whose every
// change is a pure deletion; anchoring on a context line still beats refusing
// `c` outright. A file with no new-side row at all returns null, and the
// caller keeps the old-side refusal.
function firstNewSideRow() {
  for (const head of diffChangeBlocks()) {
    for (let tr = head; tr; tr = tr.nextElementSibling) {
      if (tr.classList.contains("note")) continue; // a ◆ box, not a diff row
      if (tr.classList.contains("same")) break; // the run ended
      const no = Number(tr.dataset.no);
      if (tr.dataset.side === "new" && no) return { side: "new", no };
    }
  }
  const any = $("diff-body").querySelector(`tr[data-no][data-side="new"]`);
  return any ? { side: "new", no: Number(any.dataset.no) } : null;
}


function noteRowEls() {
  return [...$("diff-body").querySelectorAll("tr.note[data-note]")];
}


// findNote resolves an id to its wire record, replies included.
function findNote(id) {
  for (const n of state.notes) {
    if (n.id === id) return n;
    for (const r of n.replies || []) if (r.id === id) return r;
  }
  return null;
}


// nearestNote is the note `E`/`R` act on: the last one at or above the marked
// row (the web has no line cursor, so "the one you are looking at" is the one
// the marked row has just scrolled past).
function nearestNote() {
  const els = noteRowEls();
  if (!els.length) return null;
  // }/{ move the marked row to the one the stepped-to note hangs off, and a
  // note sits one row BELOW its anchor — so after a step the "last note at or
  // above the marked row" rule can pick the note before it. While the stepped
  // note is still on screen it IS the one the user is looking at.
  const stepped = els.find((el) => el.dataset.note === noteStepId);
  if (stepped) return findNote(stepped.dataset.note);
  const all = [...$("diff-body").querySelectorAll("table.diff tr")];
  const cur = $("diff-body").querySelector("tr.cur");
  if (!cur) return findNote(els[0].dataset.note);
  const at = all.indexOf(cur);
  let best = null;
  for (const el of els) if (all.indexOf(el) <= at + 1) best = el;
  return findNote((best || els[0]).dataset.note);
}


// noteStepId is the note }/{ last landed on. Stepping is relative to IT, not
// to the marked row: a note hangs one row BELOW its anchor, so a "next note
// after the cursor" rule would keep re-finding the note the cursor is already
// on and }/{ would never move. Held by id, so it survives a re-render and
// simply stops matching once a different file is open.
let noteStepId = null;

// stepNote scrolls to the next/previous ◆ row and re-anchors on the diff row
// it hangs off, so `E`/`R` follow the jump. Both directions wrap.
function stepNote(dir) {
  const els = noteRowEls();
  if (!els.length) return;
  const all = [...$("diff-body").querySelectorAll("table.diff tr")];
  let target;
  const at = els.findIndex((el) => el.dataset.note === noteStepId);
  if (at >= 0) {
    target = els[(at + dir + els.length) % els.length];
  } else {
    // First step of this session: enter the list from the marked row.
    const cur = $("diff-body").querySelector("tr.cur");
    const row = cur ? all.indexOf(cur) : -1;
    if (dir > 0) {
      target = els.find((el) => all.indexOf(el) > row) || els[0];
    } else {
      const before = els.filter((el) => all.indexOf(el) < row);
      target = before.length ? before[before.length - 1] : els[els.length - 1];
    }
  }
  target.scrollIntoView({ block: "center" });
  target.classList.add("flash");
  setTimeout(() => target.classList.remove("flash"), 600);
  let p = target.previousElementSibling;
  while (p && !p.dataset.no) p = p.previousElementSibling;
  if (p) markDiffRow(p); // clears noteStepId…
  noteStepId = target.dataset.note; // …which the step then claims for itself
}


// noteWrite runs one mutation through the single-flight gate and reloads both
// the open diff's notes and the badge counts.
function noteWrite(label, path, body) {
  const run = runOnce("note-write", async () => {
    await postJSON(path, body);
    await Promise.all([fetchNotes(), refreshNoteCounts()]);
  });
  if (!run) {
    opLine(label + ": a note write is already running", true);
    return;
  }
  run.catch((e) => opLine(label + ": " + (e.message || e), true));
}


// addNotePrompt asks for a summary + optional rationale (the prompt's two-field
// shape) and anchors the note on the clicked row, else the first changed row.
function addNotePrompt() {
  const q = noteQuery();
  if (!q) return;
  let at = state.diffRow || firstChangedRow();
  if (!at) return;
  // A preview's old side is the MERGE BASE, which no stored address names, so
  // there is nothing there to anchor to (domain.ErrPreviewOldSide). The refusal
  // is here rather than at the server so the prompt never opens on a line the
  // write would reject afterwards.
  if (state.diffCtx.preview && at.side === "old") {
    // Nothing was CLICKED: the old side is just where firstChangedRow landed
    // (a file whose first change is a pure deletion), not a line the user
    // named. Fall forward to the file's first new-side row instead of
    // refusing a file that has a perfectly addressable side. An explicit
    // old-side click still gets the refusal — there the user meant that line.
    const fwd = !state.diffRow && firstNewSideRow();
    if (fwd) at = fwd;
    if (at.side === "old") {
      opLine("notes in a preview anchor on the new side", true);
      return;
    }
  }
  openPrompt({
    title: `Add note on ${at.side} line ${at.no}`,
    placeholder: "summary",
    body: { label: "rationale (optional)" },
    onSubmit: (summary, rationale) =>
      noteWrite("note", "/api/notes/add", {
        path: q.get("path"),
        rev: q.get("rev") || "",
        state: q.get("state"),
        side: at.side,
        line: at.no,
        summary,
        rationale,
      }),
  });
}


// editNotePrompt/replyNotePrompt default to nearestNote() (the E/R keys) but
// take an explicit note when the ◆ row's own menu names one.
function editNotePrompt(note) {
  const n = note || nearestNote();
  if (!n) return;
  openPrompt({
    title: "Edit note",
    value: n.summary,
    body: { label: "rationale (optional)", value: n.rationale || "" },
    onSubmit: (summary, rationale) =>
      noteWrite("edit note", "/api/notes/edit", { id: n.id, summary, rationale }),
  });
}


function replyNotePrompt(note) {
  const n = note || nearestNote();
  if (!n) return;
  openPrompt({
    title: "Reply to “" + n.summary + "”",
    placeholder: "summary",
    body: { label: "rationale (optional)" },
    onSubmit: (summary, rationale) =>
      noteWrite("reply", "/api/notes/reply", { id: n.id, summary, rationale }),
  });
}


function removeNote(id) {
  noteWrite("remove note", "/api/notes/remove", { id });
}


registerHelp({
  key: "review notes",
  html:
    "in an open diff: click a line to anchor, then <b>c</b> to write a note on it (summary + optional rationale). " +
    "<b>E</b> edits and <b>R</b> replies to the nearest ◆ above the anchored line, <b>}</b>/<b>{</b> step between " +
    "notes, and <b>a</b> folds agent-written notes away. Right-click a ◆ row for the same actions plus " +
    "<b>remove</b>. A file with notes carries a ◆N badge in the file list; notes are machine-local and never " +
    "committed",
});


// toggleNotesAgent is the TUI's `a`: fold the agent layer away and back.
function toggleNotesAgent() {
  state.notesAgentOff = !state.notesAgentOff;
  if (state.lastDiff) renderDiff(state.lastDiff);
}


// rowSideAndLine reads which (side, line) a diff row's data attributes (and
// the specific <td> under the pointer, for a side-by-side row) address. The
// pane you're in is the side the place goes on: the left half reads the old
// side when the row has one, the right half the new side; a row with no
// data-lno/data-rno (the narrow unified layout) has only one address and
// keeps the row's own data-side/data-no. Shared by the note-anchor click
// handler and the diff-line copy-link menu — both read the exact same
// row/pointer geometry.
function rowSideAndLine(tr, td) {
  let side = tr.dataset.side, no = Number(tr.dataset.no);
  if (td && tr.dataset.lno !== undefined) {
    const wantOld = td.classList.contains("l");
    const ln = Number(tr.dataset.lno), rn = Number(tr.dataset.rno);
    if (wantOld && ln) { side = "old"; no = ln; } else if (!wantOld && rn) { side = "new"; no = rn; }
  }
  return { side, no };
}


// A click on a diff row marks it as the note anchor; a right-click on a ◆ row
// opens that note's own menu. The anchor is a NOTE affordance, so it follows
// notesArmed: on a comparison a marked row would promise a `c` that is inert.
// (The ◆ menu needs no guard — a comparison renders no ◆ rows to right-click.)
$("diff-body").addEventListener("click", (e) => {
  if (!notesArmed()) return;
  const tr = e.target.closest("tr[data-no]");
  if (!tr || !getSelection().isCollapsed) return; // don't re-anchor mid-selection
  const td = e.target.closest("td");
  const { side, no } = rowSideAndLine(tr, td);
  markDiffRow(tr, side, no);
});


$("diff-body").addEventListener("contextmenu", (e) => {
  // A reply block carries its own id inside the thread's row, so the menu
  // targets the exact note under the pointer (root or reply).
  const tr = e.target.closest("[data-note]");
  if (!tr || !tr.closest("tr.note")) {
    // Not a ◆ row. Two things can be offered here, independently: the text
    // the pointer sits in (when something is selected) and the line's gg://
    // link (only under notesArmed() — outside it the rows carry no data-side
    // / data-no at all, and a compare has no addressable target).
    const rows = [];
    // The selection is read NOW, into the closure: clicking the menu row
    // moves focus, and by the time act() runs getSelection() may be empty.
    // The line-number gutter is `user-select: none` (style.css td.no), so
    // what comes back is the code, not code interleaved with line numbers.
    const sel = window.getSelection();
    const text = sel && !sel.isCollapsed && $("diff-body").contains(sel.anchorNode) ? sel.toString() : "";
    if (text) {
      rows.push({ label: "copy", act: () => copyText(text, "selection") });
    } else {
      // Nothing selected: offer the line the pointer is on. The cell under
      // the pointer, not the whole <tr> — a side-by-side row holds BOTH
      // versions of the line, and copying the pair glued together is never
      // what was meant. Its `td.no` sibling is not read, so no line number
      // rides along.
      const cell = e.target.closest("td.side");
      const line = cell ? cell.textContent : "";
      if (line) rows.push({ label: "copy line", act: () => copyText(line, "line") });
    }
    const row = e.target.closest("tr[data-no]");
    if (row && notesArmed()) {
      const td = e.target.closest("td");
      let { side, no } = rowSideAndLine(row, td);
      // A preview has no old side. A context row's LEFT cell still names a
      // line that exists on the new side (data-rno), so that number travels;
      // a deletion row has none, and linkFor degrades the link to the file
      // form (the user's ruling, 2026-09-16 — the TUI's cursor on a deletion
      // row copies the same file link).
      if (side === "old" && state.diffCtx.preview) {
        const rn = Number(row.dataset.rno || 0);
        if (rn) { side = "new"; no = rn; }
      }
      const link = linkFor(state.repo, state.worktree, state.diffCtx, side, no);
      // Recorded like every other copy (Task 9): copyLink, never copyText.
      // The Desc names the FILE — a line link's row in `gg links` has to be
      // recognisable, and the line number is already in the link text.
      if (link)
        rows.push({
          label: "copy gg link to this line",
          act: () => copyLink(link, linkDesc("file", (state.diffCtx && state.diffCtx.path) || "", "")),
        });
    }
    if (!rows.length) return; // nothing of our own to say: keep the browser's menu
    e.preventDefault();
    showCtxMenu(rows, e.clientX, e.clientY);
    return;
  }
  const n = findNote(tr.dataset.note);
  if (!n) return;
  e.preventDefault();
  // A note has no address of its OWN: `gg://` names places, and a note id is
  // machine-local (internal/notes is a per-machine store), so it would mean
  // nothing on the machine the link is pasted into. What travels is the note's
  // ANCHOR — the line it hangs off — so "copy gg link to this note" yields the
  // same string the line under it would. It is built from state.diffCtx, never
  // from n.rev/n.path: inside a merge preview a note's rev is an older source
  // commit while the PLACE on screen is the pair, and the two menus must not
  // disagree about the same row. A reply carries its root's inherited
  // side/line on the wire (domain.ToWireNote), so it needs no special case.
  const noteRows = [
    { label: "Edit note", act: () => editNotePrompt(n) },
    { label: "Reply…", act: () => replyNotePrompt(n) },
  ];
  const nlink = linkFor(state.repo, state.worktree, state.diffCtx, n.side, n.line);
  if (nlink)
    noteRows.push({
      label: "copy gg link to this note",
      act: () => copyLink(nlink, linkDesc("file", (state.diffCtx && state.diffCtx.path) || "", "")),
    });
  showCtxMenu(
    [
      ...noteRows,
      { sep: true },
      {
        label: n.parent_id ? "Remove reply" : "Remove note (and its replies)",
        danger: true,
        act: () => removeNote(n.id),
      },
    ],
    e.clientX,
    e.clientY
  );
});


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
  // Note rows are not diff rows: they carry neither "same" nor a change kind,
  // so without this filter every ◆ row would read as the start of a change
  // block and the ‹/› stepper would walk the notes instead of the changes.
  const rows = $("diff-body").querySelectorAll("table.diff tr:not(.note)");
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
    setDiffTitle("");
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
  setDiffTitle(f.path, "", " — resolve");
  $("diff-body").innerHTML = `<div class="notice">loading…</div>`;
  updateDiffNav();
  let d;
  try {
    d = await getJSON("/api/conflict-hunks?" + new URLSearchParams({ path: f.path }));
  } catch (e) {
    // Typed 422 refusal (binary / markers gone): show the reason and the way
    // out. Block-picking is what is impossible here, not resolving — the
    // whole-file answers in the file's own menu work on a binary conflict
    // precisely because they never look inside it.
    $("diff-body").innerHTML =
      `<div class="notice">${esc(e.message || e)}</div>` +
      `<div class="notice">right-click the file for the whole-file answers (keep current / keep incoming), ` +
      `or mark resolved when you have fixed it yourself</div>`;
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
  else { setDiffTitle(""); $("diff-body").innerHTML = ""; updateDiffNav(); }
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
    const po = openPreviewCtx();
    showCtxMenu(
      [
        { label: "file history", act: () => openFileHistory(f.path, rev) },
        { label: "blame at this commit", act: () => openFileBlame(f.path, rev) },
        // gg's own stores, addressed at the commit being viewed: a bookmark
        // points AT this version, a shelf entry freezes its bytes.
        { label: "bookmark this file", act: () => addFileEntry("bookmarks", f.path, "committed", rev) },
        { label: "add to shelf", act: () => addFileEntry("shelf", f.path, "committed", rev) },
        ...copyPathRows(f.path),
        // A compare row's rev is bHash, but the diff on screen is aHash →
        // bHash, not bHash^ → bHash — a commit-state link would misdescribe
        // the place, so the file contributor is told to refuse outright.
        // The exception is an open merge preview: its rows are files IN THE
        // PREVIEW, and the pair (source, target) is their address — the same
        // file form the TUI's preview file tree copies.
        ...extraRows("file", {
          path: f.path,
          sha: rev,
          section: "commit",
          compare: state.filesMode === "compare",
          preview: po ? { source: po.source, target: po.target } : null,
        }),
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
  items.push(...extraRows("file", { path: f.path, sha: "", section: f.section }));
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
export { SECTION_LABELS, activeFileList, applyFilesHidden, applyTextMode, cycleTextMode, mountPanBars, toggleFilesHidden, setCommitTitle, setFilesDesc, commitBody, commitMetaParts, addNotePrompt, noteBadgeHTML, applyCompareFilter, cfSideCount, clearDiffHunks, commitMetaLine, conflictPick, cycleFilesSort, diffChangeBlocks, toggleMark, diffHTML, diffHunks, drillOut, editNotePrompt, enterFilesStage, fetchNotes, exitStatusToList, hunkAttr, hunkCls, hunkEligible, markDiffRow, renderCell, openCompare, openConflictPicker, openEntryCompare, openEntryFileDiff, notesArmed, openFile, openStatusDiff, openWorkingTree, paintConflictPicks, paintHunkPicks, reconcileStatusView, renderCompareBar, renderDiff, renderFiles, renderHunkBar, refreshNoteCounts, renderResolveBar, reopenAfterHunkStage, replyNotePrompt, resolveConflictPicked, setAllConflictPicks, setFilesMeta, setLayout, stage, stageHunksPicked, stepChange, stepFile, stepNote, stepToNextConflict, toggleDiffView, applyDiffView, revealDiffRow, toggleNotesAgent, updateDiffNav };
