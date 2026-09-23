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
import { symPair } from "./stack.js";
import { renderCommits, rewordPrompt } from "./commits.js";
import { focusPane, moveCursor, stepCommitCursor } from "./keys.js";
import { saveUI } from "./uistate.js";
import { openSymRow, renderSymLists, symActive, symBarHTML, symEmpty, symLayoutChanged, symOffered, symReapply, symRows } from "./symcompare.js";
import { Search } from "./inviewsearch.js";
import { bindSearchBar } from "./searchbar.js";
import { noteTitle, seedCollapsed, setAllCollapsed, toggleCollapsed } from "./notebox.js";
import { mdHTML, mdInlineHTML } from "./markdown.js";
import { activeDiff, hunkSlotAt, hunkSlots, showSlotDiff, followInList, noteScope, openStack, reconcileStack, refindStack, refreshStackNotes, rerenderStack, stackAllNotes, stackHitStep, stackOn, stackSearchHere, teardownStack, unsearchedSlots } from "./stackview.js";

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
  // (in a stack each slot answers for itself: reconcileSlots drops its picks
  // and re-fetches, which re-arms them from the fresh tags)
  if (!state.stack && diffHunks && !state.statusEntries.some((f) => f.path === diffHunks.path && hunkEligible(f))) clearDiffHunks();
  if (conflictPick && !state.statusEntries.some((f) => f.path === conflictPick.path && f.section === "conflicts")) {
    conflictPick = null;
    renderResolveBar();
  }
  reconcileStack();
}


// drillOut steps ONE stage back — diff → file list → full-width commit
// list. The esc key, the ← back button, and the footer chip all share it.
function drillOut() {
  if (state.layout === "diff" && symActive()) {
    // The symmetric view has no files-only stage to step back to (it opens
    // straight onto a diff): esc leaves the comparison's screen.
    state.detailGen++;
    state.pane = "commits";
    setLayout("list");
    focusPane();
    return;
  }
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
  // A folded list cannot be one of two aligned lists: the symmetric view
  // steps aside (and comes back with the list), which no layout change says.
  if (symOffered()) return symReapply();
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
  if (mode !== "diff") teardownStack(); // the stack lives in the diff stage only
  const was = state.layout;
  state.layout = mode;
  const p = $("panes");
  p.classList.toggle("solo", mode === "list");
  p.classList.toggle("files", mode === "files");
  p.classList.toggle("detail", mode === "diff");
  // The footer's / chip is the commit filter everywhere but in the diff
  // layout, where the commits pane is off-screen and / finds text instead.
  const fchip = document.querySelector('#foot button[data-act="filter"]');
  if (fchip) fchip.textContent = mode === "diff" ? "/ find" : "/ filter";
  // The commits pane is display:none in the diff stage: that drops its
  // scroll position, and any render while hidden (a live refresh, r, a notes
  // count) sizes the virtual window for a zero-height pane — ten rows, which
  // is all the list showed after esc until the user scrolled. Coming back
  // into either stage that shows the pane re-renders and rescrolls it.
  if (mode === "list" || (mode === "files" && was === "diff")) stepCommitCursor(0);
  symLayoutChanged(); // the symmetric grid exists only in the diff stage
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
  diffSearchBar.reset(); // the pane is empty now; nothing to unpaint
  setFilesMeta(""); // every stage starts without a date; only a commit open sets one
  setFilesDesc(""); // …and without a description
  // The kind badge is DERIVED, not cleared: esc from a diff re-enters this
  // stage inside the same comparison, whose badge must stay; every other
  // screen sets filesMode before coming here, so it reads as "no badge".
  const lc = state.filesMode === "compare" && state.compare && state.compare.links ? state.compare : null;
  setFilesKind(lc ? lc.kindLabel || "" : "", lc ? lc.kindTip || "" : "");
  $("files-title").dataset.sha = ""; // …and without a commit id; see setCommitTitle
  $("files-title").dataset.subject = "";
  $("files-title").dataset.short = "";
}


// setFilesKind draws (or hides) the badge that says WHAT KIND of screen this
// file list is. A comparison's title is two descriptions and an arrow, which
// reads like any other header; the badge is what makes "this is a comparison
// of two previews" obvious at a glance. Hidden by ID (#files-kind.hidden).
function setFilesKind(text, tip) {
  const el = $("files-kind");
  el.textContent = text;
  el.title = tip || "";
  el.classList.toggle("hidden", !text);
}

// compareKindLabel names a link comparison from what its two sides ARE.
function compareKindLabel(body) {
  if (body.pair) return "commit pair";
  const l = body.left.kind, r = body.right.kind;
  if (l === "preview" && r === "preview") return "preview comparison";
  if (l === "pair" && r === "pair") return "commit-pair comparison";
  return "link comparison";
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
// The title is one elided line (a pull request's is "PR #n · title (source →
// target)" — far wider than the files pane). Hovering a CUT title shows it
// whole; a title that fits gets no tooltip. Done on hover, for every mode's
// title at once, so no writer of #files-title has to remember it.
$("files-title").addEventListener("mouseenter", (e) => {
  const el = e.currentTarget;
  el.title = el.scrollWidth > el.clientWidth ? el.textContent : "";
});


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


// openLinkCompare shows a LINK comparison (/api/compare-links): two gg:// link
// texts the server ran through domain.CompareLinks. It is the entry compare's
// screen and, like it, addressed by SPEC rather than by two hashes — but per
// ROW where a member's bytes live somewhere other than its side's own
// endpoint (a `-u` stash keeps its untracked files on a third parent). Two
// arbitrary links have no merge base to speak of, so there are no origin sets.
function openLinkCompare(body) {
  state.detailGen++;
  state.previewOpen = null; // never mistaken for an open merge preview
  // …and the previous screen's per-file totals must not paint on this one's
  // rows: only armPreview cleared them before a pair could read the same slot.
  state.previewCounts = null;
  state.compare = {
    a: body.left.desc,
    b: body.right.desc,
    aHash: "",
    bHash: "",
    aSpec: body.left.spec,
    bSpec: body.right.spec,
    links: { left: body.left.text, right: body.right.text },
    // A PAIR LANDING names its two commits (the server says so; two typed
    // links never do): that is the note scope. It lives on the comparison and
    // dies with it — not in previewOpen, which means "tips that can move".
    pair: body.pair && body.pair.a && body.pair.b ? { a: body.pair.a, b: body.pair.b } : null,
    all: body.files || [],
    // Two BOUNDED sets also answer their aligned rows: the symmetric view
    // (symcompare.js). No `sym` → the view is not offered.
    kindLabel: compareKindLabel(body),
    kindTip: body.left.text + "\n↔\n" + body.right.text,
    sym: body.sym || null,
    symFilter: "diff",
    flipped: false,
    filter: "all",
    originsError: "",
  };
  state.filesMode = "compare";
  state.fileSha = null;
  enterFilesStage();
  $("files-title").textContent = (body.label ? body.label + " — " : "") + state.compare.a + " ↔ " + state.compare.b;
  $("files-title").title = state.compare.a + "  ↔  " + state.compare.b;
  applyCompareFilter();
  focusPane();
  if (state.compare.pair) loadPairCounts();
  // The symmetric view opens straight onto its first row's diff (or, with no
  // row to show, onto its own empty state).
  if (symActive()) symReapply();
}


// updateLinkCompareFiles swaps a LIVE link comparison's rows in place — what
// refreshLinkCompare calls when something under the comparison moved. Unlike
// openLinkCompare it tears nothing down: the filter, the stage and the cursor's
// FILE stay (the cursor follows its path, since rows shift), and an open diff
// closes only when its own row is gone.
function updateLinkCompareFiles(files, sym) {
  const c = state.compare;
  const cur = state.files[state.fileCursor];
  c.all = files || [];
  c.sym = sym || null;
  state.files = compareRows(c);
  if (!state.files.length) {
    applyCompareFilter(); // the one painter of the empty state
    return;
  }
  const i = cur ? state.files.findIndex((f) => f.path === cur.path) : -1;
  state.fileCursor = i >= 0 ? i : Math.min(state.fileCursor, state.files.length - 1);
  // A stack holds the OLD rows: rebuild it on the cursor's file. Deliberately
  // BEFORE the drillOut below: when the reader's file is gone, a stack
  // re-anchors on the clamped neighbour (the working-tree reconcile rule)
  // instead of leaving the screen, as the single-file view does.
  if (state.stack && state.layout === "diff") return openFile(state.fileCursor);
  renderFiles();
  updateDiffNav();
  if (i < 0 && state.layout === "diff") drillOut();
}


// pairCtx is the commit pair whose comparison is ON SCREEN — the predicate the
// pair's note lane gates on, beside openPreviewCtx. The layout check matters:
// drillOut (esc) returns to the commit list leaving filesMode and
// state.compare standing, and a closed screen must not keep fetching.
function pairCtx() {
  if (state.filesMode !== "compare" || state.layout === "list") return null;
  return (state.compare && state.compare.pair) || null;
}


// pairNoteCtx is the `preview` slice a pair's diff context carries: no names
// (there are none), so every reader of source/target must ask .pair first.
function pairNoteCtx(p) {
  return { pair: { a: p.a, b: p.b }, source: "", target: "", pr: 0, linkSource: "", linkTarget: "" };
}


// loadPairCounts fetches the pair's per-file note totals (no path), so the
// file list carries its ◆N badges from the first paint.
async function loadPairCounts() {
  const p = pairCtx();
  if (!p) return;
  let d;
  try {
    d = await getJSON("/api/pair/notes?a=" + p.a + "&b=" + p.b);
  } catch {
    return; // decoration: no badge beats a wrong badge
  }
  const now = pairCtx();
  if (!now || now.a !== p.a || now.b !== p.b) return; // superseded
  state.previewCounts = d.counts || {};
  renderFiles();
}


// openEntryFileDiff opens ONE file between two arbitrary sides (a stored copy
// and the file here, typically). Both labels go in the title: a diff whose
// sides are not named is unreadable when neither of them is "the commit you
// are looking at".
// ctx, when given, is the diff context of a row whose NEW side is a real
// commit's content (a commit pair's row): it turns the note lane on. Every
// other caller leaves it out and the diff stays context-free.
async function openEntryFileDiff({ left, right, path, oldPath, leftLabel, rightLabel, status, ctx }) {
  const gen = ++state.detailGen;
  if (state.layout !== "diff") {
    state.pane = "files";
    setLayout("diff");
    focusPane();
  }
  clearDiffHunks();
  state.diffCtx = ctx || null; // history/blame need a rev; a stored copy has none
  state.diffRow = null; // …and the previous diff's marked row must not paint a row of this one
  state.notes = [];
  setDiffTitle(path, leftLabel + " ↔ " + rightLabel + " · ");
  $("diff-body").innerHTML = `<div class="notice">loading…</div>`;
  updateDiffNav();
  const q = new URLSearchParams({ left, right, path });
  if (status) q.set("status", status);
  if (oldPath) q.set("old_path", oldPath); // a rename's left side lives at its old path
  try {
    // The notes ride ALONGSIDE the diff (openFile's rule): the ◆ rows have to
    // be in the first paint. Without a ctx fetchNotes has nothing to ask.
    const [d] = await Promise.all([getJSON("/api/entry-diff?" + q), fetchNotes(false)]);
    if (gen !== state.detailGen) return; // superseded by a newer open or esc
    renderDiff(d);
    jumpToFirstChange();
    focusDiff();
  } catch (e) {
    if (gen !== state.detailGen) return;
    $("diff-body").innerHTML = `<div class="notice">error: ${esc(e.message || e)}</div>`;
    updateDiffNav();
  }
}


// The origin filter (the TUI's f key): "all", or only the files one side
// touched since the two diverged. A file both sides touched stays in both
// filtered views — the TUI's filterCompareFiles rule.
// compareRows is the ONE place a comparison's rows become state.files: the
// aligned rows while the symmetric view is on, else the (origin-filtered)
// plain listing. Every writer goes through it, or a live refresh would hand
// the symmetric painter rows with no per-side state.
function compareRows(c) {
  if (symActive()) return symRows(c);
  return c.filter === "all" ? c.all : c.all.filter((f) => f.origin === c.filter || f.origin === "both");
}


function applyCompareFilter() {
  const c = state.compare;
  state.files = compareRows(c);
  state.fileCursor = 0;
  renderFiles();
  updateDiffNav();
  if (!state.files.length) {
    // The empty state must live in the FILE LIST: in the files stage the
    // diff pane is not on screen, and stepping back there from an open
    // diff must not strand a stale one.
    $("files-list").innerHTML = `<li class="sect">${
      c.all.length ? "no files match this filter" : c.frozen || c.links ? "nothing differs" : "the two branches are identical"
    }</li>`;
    // The symmetric view's esc LEAVES the comparison (it has no files-only
    // stage), so an empty filter must not route through drillOut there: a chip
    // would close the screen it sits on. The view says so in place instead.
    if (symActive()) { teardownStack(); return symEmpty(); } // an empty view has no files to stack
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
  if (c.frozen || c.links) {
    bar.innerHTML =
      (symOffered() ? symBarHTML() : `<button class="on" disabled>all (${c.all.length})</button>`) +
      // A link comparison is two texts, so it can be kept (linkcompare.js owns
      // the click); a frozen entry compare is addressed by ids and cannot.
      (c.links ? `<button id="link-save-chip" title="keep this comparison in the Previews tab">save comparison…</button>` : "");
    return;
  }
  // A merge preview (and a pull request, which rides the same screen) is
  // merge-base → tip by construction: "only <side>" can never apply, and two
  // dead buttons with elided labels only read as something broken. The bar
  // says what is showing instead.
  if (c.previewBar) {
    bar.innerHTML =
      `<button class="on cmpall" disabled title="all ${c.all.length} changed files — a merge preview / pull request has no per-side filter">all (${c.all.length})</button>` +
      // A pull request also offers what its diff cannot hold (prdetails.js
      // owns the click). Ahead of the note: that text elides, the chip must not.
      (c.previewPR ? `<button id="pr-details-chip" title="description, conversation, outdated review threads">details</button>` : "") +
      `<span class="cmpnote" title="${esc(c.previewBar)}">${esc(c.previewBar)}</span>`;
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
    if (symActive()) return renderSymLists(); // two aligned lists (symcompare.js)
    // A commit file's notes are keyed "<sha>:<path>" — the sha this row's diff
    // would open. A COMPARISON gets no badge at all: its diff is not
    // note-addressable (see openFile), so a ◆ would advertise notes that its
    // rows cannot show and its keys cannot add.
    const cmp = state.filesMode === "compare";
    // …except a MERGE PREVIEW, whose compare is note-addressable: its per-file
    // totals come from the gathered set (state.previewCounts), not from the
    // per-commit index, because a note on an older commit of the branch counts
    // for the file too.
    const prev = openPreviewCtx() || pairCtx();
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


// fileDiffURL is the ONE place a listed file becomes the URL its diff is read
// from, in the screen's current mode — the single-file opens and the stacked
// view's loader (stackview.js) both fetch through it, so the two can never
// show different diffs for one row. null = there is nothing to diff (a
// conflict is resolved in the hunk picker).
function fileDiffURL(f) {
  if (state.filesMode === "status") {
    if (f.section === "conflicts") return null;
    const q = new URLSearchParams({ wt: f.section === "staged" ? "staged" : "unstaged", path: f.path });
    if (f.orig_path) q.set("old", f.orig_path);
    return "/api/diff?" + q;
  }
  const c = state.compare;
  // The symmetric view diffs a row in the ARROW's direction (symPair — the
  // pair openSymRow opens), which the plain link lane below does not know.
  if (state.filesMode === "compare" && c.links && symActive()) {
    const p = symPair(f, c);
    if (!p) return null; // neither set has content: the stack shows a notice instead
    return "/api/entry-diff?" + new URLSearchParams({ left: p.left, right: p.right, path: f.path, status: p.status });
  }
  // A link comparison (a row may name its own sides) and a frozen entry
  // compare address their sides by SPEC: one may be a snapshot git cannot read.
  if (state.filesMode === "compare" && (c.links || c.frozen)) {
    const q = new URLSearchParams({
      left: (c.links && f.left_spec) || c.aSpec,
      right: (c.links && f.right_spec) || c.bSpec,
      path: f.path,
    });
    if (f.status) q.set("status", f.status);
    if (c.links && f.old_path) q.set("old_path", f.old_path);
    return "/api/entry-diff?" + q;
  }
  const q = new URLSearchParams({ path: f.path, status: f.status });
  if (state.filesMode === "compare") {
    q.set("left", c.aHash);
    q.set("right", c.bHash);
  } else {
    q.set("sha", f.sha || state.fileSha);
  }
  if (f.old_path) q.set("old", f.old_path);
  return "/api/diff?" + q;
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
  // The stacked view (S) shows every file in one scroll: this row is a
  // place in it, not a diff of its own (stackview.js).
  if (stackOn()) return openStack(i);
  teardownStack(); // a single-file open replaces any stack on screen
  if (state.filesMode === "status") return openStatusDiff(i);
  const f = state.files[i];
  // A link comparison addresses its sides by spec too, and a ROW may name its
  // own: this arm must run before the hash lane below, which has no hashes to
  // read here.
  if (state.filesMode === "compare" && state.compare.links && symActive()) return openSymRow(f);
  if (state.filesMode === "compare" && state.compare.links) {
    // A pair's row is note-addressable when its new side IS the file at b. A
    // row that names its own right side is not (a `-u` stash keeps untracked
    // files on a third parent): it stays context-free.
    const pair = !f.right_spec ? pairCtx() : null;
    return openEntryFileDiff({
      ctx: pair
        ? { path: f.path, rev: pair.b, state: "commit", notes: true, compare: true, preview: pairNoteCtx(pair) }
        : null,
      left: f.left_spec || state.compare.aSpec,
      right: f.right_spec || state.compare.bSpec,
      path: f.path,
      oldPath: f.old_path || "",
      status: f.status,
      leftLabel: state.compare.a,
      rightLabel: state.compare.b,
    });
  }
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
  state.diffCtx = commitDiffCtx(f);
  state.diffRow = null;
  state.notes = [];
  setDiffTitle(f.path);
  $("diff-body").innerHTML = `<div class="notice">loading…</div>`;
  updateDiffNav();
  try {
    // The notes ride ALONGSIDE the diff fetch, not after it: the ◆ rows have
    // to be in the first paint, and a second serial round-trip would show the
    // diff without them first.
    const gen = state.detailGen; // a newer open, esc or a stack (S) supersedes this one
    const [d] = await Promise.all([getJSON(fileDiffURL(f)), fetchNotes(false)]);
    if (gen !== state.detailGen) return;
    renderDiff(d);
    jumpToFirstChange();
    focusDiff();
  } catch (e) {
    if (state.stack) return; // superseded by a stack: its error is not this pane's
    $("diff-body").innerHTML = `<div class="notice">error: ${esc(e.message || e)}</div>`;
    updateDiffNav();
  }
}


// commitDiffCtx is the note context of ONE row of a commit / compare /
// preview file list — what state.diffCtx becomes when that row is opened
// alone. It is a function, not an inline literal, because a STACK builds the
// same context per file (stackview.js): the two must never drift, or a note
// written in a stack would be filed against a different address than the same
// note written one file at a time.
function commitDiffCtx(f) {
  const cmp = state.filesMode === "compare";
  const prev = openPreviewCtx();
  return {
    path: f.path,
    rev: prev ? prev.tip : cmp ? state.compare.bHash : f.sha || state.fileSha,
    state: "commit",
    notes: !cmp || !!prev,
    // The pair the gathered note set is read by: a preview note may have been
    // written against an OLDER commit on the branch, so the query is the pair,
    // never this one tip.
    // pr (a number) marks a pull request's diff: its source/target are display
    // names, so the note lane and the link builder must not read them as refs.
    preview: prev ? previewCtx(prev) : null,
    // links.js documents ctx.compare as THE refusal for a two-revision view;
    // carrying it here means the diff-LINE copy-link path uses that documented
    // guard too, instead of relying on notesArmed() to happen to be off.
    compare: cmp,
  };
}


// statusDiffCtx is commitDiffCtx's twin for a working-tree entry. A conflicted
// row has no diff at all (the picker opens instead), so it has no context.
function statusDiffCtx(f) {
  if (f.section === "conflicts") return null;
  return { path: f.path, rev: "", state: sectionNoteState(f.section) };
}


// rowNoteCtx is the note context of a file-list row in the CURRENT mode — the
// one door a stack's slot uses, mirroring openFile's own dispatch. null means
// "not note-addressable", which is exactly where the single-file view leaves
// notes inert too: a two-revision compare, a frozen entry compare, a link row
// that names its own right side, and every symmetric row.
function rowNoteCtx(f) {
  if (state.filesMode === "status") return statusDiffCtx(f);
  if (state.filesMode === "compare" && state.compare && state.compare.links) {
    if (symActive()) return null;
    // A pair's row is note-addressable when its new side IS the file at b.
    const pair = !f.right_spec ? pairCtx() : null;
    return pair
      ? { path: f.path, rev: pair.b, state: "commit", notes: true, compare: true, preview: pairNoteCtx(pair) }
      : null;
  }
  if (state.filesMode === "compare" && state.compare && state.compare.frozen) return null;
  if (state.filesMode === "entry") return null;
  return commitDiffCtx(f);
}


async function openStatusDiff(i) {
  clearDiffHunks();
  const f = state.statusEntries[i];
  state.diffCtx = statusDiffCtx(f);
  state.diffRow = null;
  state.notes = [];
  setDiffTitle(f.path);
  if (f.section === "conflicts") return openConflictPicker(f);
  $("diff-body").innerHTML = `<div class="notice">loading…</div>`;
  updateDiffNav();
  try {
    const gen = state.detailGen; // a newer open, esc or a stack (S) supersedes this one
    const [d] = await Promise.all([getJSON(fileDiffURL(f)), fetchNotes(false)]);
    if (gen !== state.detailGen) return;
    // server tags eligible unstaged diffs with hunk ordinals — arm inline
    // staging BEFORE the render so the rows pick up their hk classes
    if (d.hunks && hunkEligible(f)) diffHunks = hunkState(f.path, d.hunks);
    renderDiff(d);
    jumpToFirstChange();
    focusDiff();
  } catch (e) {
    if (state.stack) return; // superseded by a stack: its error is not this pane's
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
// renderCell paints one line: intraline change spans (mark), syntax token
// runs (tk-* spans) and in-view search hits — three masks over the line's
// RUNES (every offset the server or the search engine hands over is a rune
// offset), merged into the fewest elements. A search hit rides as a class on
// whatever element the segment already gets (a mark, a token span, else a
// bare span) plus its hit index, so the current one can be found and moved
// without a re-render; hits are painted HERE, from the host's search state,
// never added to the DOM afterwards — every re-render (a resize, the f
// toggle, a notes refresh) would drop a post-hoc class.
function renderCell(text, spans, toks, side, hits) {
  if ((!spans || !spans.length) && (!toks || !toks.length) && (!hits || !hits.length)) return esc(text);
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
  // hit[i] = 1 + the hit's index (0 = no hit), cur[i] whether it is the
  // current one; hits never overlap (the engine advances past each match).
  const hit = new Array(rs.length).fill(0);
  const cur = new Array(rs.length).fill(false);
  for (const h of hits || []) {
    for (let i = h.start; i < h.end && i < rs.length; i++) {
      hit[i] = h.idx + 1;
      cur[i] = !!h.cur;
    }
  }
  let out = "";
  for (let i = 0; i < rs.length; ) {
    let j = i + 1;
    while (j < rs.length && emph[j] === emph[i] && cls[j] === cls[i] && hit[j] === hit[i]) j++;
    const seg = esc(rs.slice(i, j).join(""));
    const hc = hit[i] ? (cur[i] ? "hit cur" : "hit") : "";
    const hd = hit[i] ? ` data-h="${hit[i] - 1}"` : "";
    if (emph[i]) out += `<mark class="${side}${hc ? " " + hc : ""}"${hd}>${seg}</mark>`;
    else if (cls[i]) out += `<span class="tk-${cls[i]}${hc ? " " + hc : ""}"${hd}>${seg}</span>`;
    else if (hc) out += `<span class="${hc}"${hd}>${seg}</span>`;
    else out += seg;
    i = j;
  }
  return out;
}


// hunkCls/hunkAttr decorate a diff row that belongs to a stageable hunk:
// the data-hunk tag drives click-to-select, the classes the highlight (a
// resize re-render keeps the current picks).
function hunkCls(r, kctx) {
  if (r.hunk == null || r.hr == null || !kctx) return "";
  // hk-top / hk-bot mark the first and last row of one hunk's run: the frame a
  // "Stage hunk" acts on. hk-sel is the reader's selection, and it is the
  // whole row — a modified row is one change, whichever cell was clicked.
  let cls = " hk";
  if (kctx.sel && kctx.sel.has(r.hunk + ":" + r.hr)) cls += " hk-sel";
  if (kctx.first === r.hunk) cls += " hk-top";
  if (kctx.last === r.hunk) cls += " hk-bot";
  return cls;
}


// hunkRunEdges finds, for the rows ACTUALLY PAINTED, which one opens and which
// one closes each hunk's run. A hunk's rows are contiguous, so that is one
// pass; it runs over the painted list (not the raw diff) because the
// changes-only fold can hide a hunk's real first row, and the border has to
// sit where the block is SEEN to begin. Pure — the guard imports it.
function hunkRunEdges(items) {
  const first = new Map();
  const last = new Map();
  let run = null; // the hunk ordinal of the run being walked
  let opener = null;
  let closer = null;
  for (const r of items) {
    const h = r && r.hunk != null ? r.hunk : null;
    if (h !== run) {
      if (closer && run !== null) last.set(closer, run);
      if (h !== null) first.set(r, h);
      opener = h !== null ? r : null;
      run = h;
    }
    closer = h !== null ? r : null;
    void opener;
  }
  if (closer && run !== null) last.set(closer, run);
  return { first, last };
}


function hunkAttr(r, kctx) {
  if (r.hunk == null || r.hr == null || !kctx) return "";
  // The row names its hunk and its ordinal inside it: exactly what a
  // selection sends to /api/stage-hunks.
  return ` data-hunk="${r.hunk}" data-hr="${r.hr}"`;
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
// nctx is WHOSE notes these rows carry: {ctx, notes, row}. It is passed
// explicitly, never read from a module-level "current slot", because a stack
// paints one slot while another slot's notes are still in flight — a shared
// variable would race. Omitted, it is the single-file view's own globals.
// hctx is WHOSE search these rows carry, and it is what makes a STACK one
// document: {search, base, lines}. `base` offsets this slot's row indexes into
// the stack-wide key space (every file has a row 12), `lines` is a sink the
// render hands its searchable lines to — the very rows it just painted, fold
// set and all — and the presence of hctx says "do NOT re-find here": the stack
// re-finds ONCE over every slot, and a per-slot re-find would leave the hit
// list holding only the last slot's hits. Omitted, the single-file view keeps
// its own rule: the render is what re-finds.
function diffHTML(d, paneWidth, notesOn = false, open = state.diffFolds, nctx = null, hctx = null, kctx = null) {
  // kctx is WHOSE hunk picks this table paints: {picks} for the file it
  // belongs to, null where staging does not apply. Explicit like nctx/hctx —
  // a stack paints one file's table while another file's picks are live.
  const hkAttr = (r) => hunkAttr(r, kctx);
  const nc = nctx || globalNoteCtx();
  const hbase = hctx ? hctx.base : 0;
  const hlines = (ls) => { if (hctx && hctx.lines) hctx.lines(ls); };
  if (d.binary) return (hlines([]), `<div class="notice">binary file</div>`);
  if (d.too_large) return (hlines([]), `<div class="notice">diff too large</div>`);
  const rows = d.rows || [];
  // anchor/cur/after are no-ops when notesOn is false, so the two layouts
  // below read the same either way.
  const anchor = (side, no) => (notesOn && no ? ` data-side="${side}" data-no="${no}"` : "");
  const curCls = (side, no) =>
    notesOn && no && nc.row && nc.row.side === side && nc.row.no === no ? " cur" : "";
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
  const attnMarks = (notesOn && state.attention.size && nc.ctx && state.attention.get(attnKey(nc.ctx))) || null;
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
    for (const [side, no] of pairs) out += noteRowsHTML(side, no, cols, nc);
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
    const noted = (side, no) => !!no && nc.notes.some((n) => n.side === side && n.line === no);
    const pinned = (r) => noted("new", r.right_no) || noted("old", r.left_no) || !!attnClsBoth(r);
    items = collapseDiffRows(rows, open, notesOn ? pinned : null);
    if (items === null) {
      const lead = notesOn ? fileNoteRowsHTML(1, nc) : "";
      hlines([]);
      return (
        (lead ? `<table class="diff">${lead}</table>` : "") +
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
  // Every rendered row carries its index into d.rows (data-i): the in-view
  // search addresses hits by that index, and a ]/[ step finds the row by it.
  const rowIndex = new Map(rows.map((r, i) => [r, i]));
  const ri = (r) => rowIndex.get(r);
  // The in-view search runs over the VISIBLE rows (folded runs are not
  // searched — it is an in-view search, as in the TUI), so it is re-found
  // here, where the fold set is known, on every render. Gated on notesOn
  // like curCls: the file-history overlay renders through this same function
  // and must never paint the diff pane's hits.
  // Stacked (hctx), the rows are handed to the stack instead and the hits are
  // painted from the search it has already run over every slot; the notesOn
  // gate does not apply there, since a stack searches files whose notes are
  // not addressable too.
  const hs = hctx ? (hctx.search.query ? hctx.search : null) : notesOn && diffSearch.query ? diffSearch : null;
  const slines = diffSearchLines(items, ri);
  if (hctx) hlines(slines);
  else if (hs) hs.refind(slines);
  // Which row opens and which closes each hunk's run, over the rows actually
  // painted: the block's border goes there (hunkCls).
  const hkAt = kctx ? hunkRunEdges(items) : null;
  const hkCls = (r) =>
    hunkCls(r, kctx && { sel: kctx.sel, first: hkAt.first.get(r), last: hkAt.last.get(r) });
  const hitsL = (r) => (hs ? hs.hitsOn(hbase + ri(r), r.kind === "same" ? 1 : 0) : null);
  const hitsR = (r) => (hs ? hs.hitsOn(hbase + ri(r), 1) : null);
  const cols = pureAdd || pureDel ? 2 : paneWidth < 950 ? 3 : 4;
  const colgroup =
    cols === 2 ? `<col class="no"><col>` : cols === 3 ? `<col class="no"><col class="no"><col>` : `<col class="no"><col><col class="no"><col>`;
  let html = `<table class="diff"><colgroup>${colgroup}</colgroup>`;
  if (notesOn) html += fileNoteRowsHTML(cols, nc);
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
      const hits = pureAdd ? hitsR(r) : hitsL(r);
      html +=
        `<tr class="${r.kind}${hkCls(r)}${curCls(nside, no)}${attnCls(nside, no)}"${hkAttr(r)}${anchor(nside, no)} data-i="${ri(r)}">` +
        `<td class="no ${side}">${no || ""}</td>` +
        `<td class="side ${side}"><span class="pan">${renderCell(text, spans, toks, side, hits)}</span></td></tr>` +
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
          `<tr class="same${curCls("new", r.right_no)}${attnClsBoth(r)}"${anchor("new", r.right_no)} data-i="${ri(r)}">` +
          `<td class="no l">${r.left_no || ""}</td>` +
          `<td class="no r">${r.right_no || ""}</td>` +
          `<td class="side"><span class="pan">${renderCell(r.right, null, r.right_tok, "r", hitsR(r))}</span></td></tr>` +
          after(3, ["new", r.right_no], ["old", r.left_no]);
      } else {
        if (r.kind !== "add")
          html +=
            `<tr class="del${hkCls(r)}${curCls("old", r.left_no)}${attnCls("old", r.left_no)}"${hkAttr(r)}${anchor("old", r.left_no)} data-i="${ri(r)}">` +
            `<td class="no l">${r.left_no || ""}</td><td class="no r"></td>` +
            `<td class="side l"><span class="pan">${renderCell(r.left, r.left_spans, r.left_tok, "l", hitsL(r))}</span></td></tr>` +
            after(3, ["old", r.left_no]);
        if (r.kind !== "del")
          html +=
            `<tr class="add${hkCls(r)}${curCls("new", r.right_no)}${attnCls("new", r.right_no)}"${hkAttr(r)}${anchor("new", r.right_no)} data-i="${ri(r)}">` +
            `<td class="no l"></td><td class="no r">${r.right_no || ""}</td>` +
            `<td class="side r"><span class="pan">${renderCell(r.right, r.right_spans, r.right_tok, "r", hitsR(r))}</span></td></tr>` +
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
        `<tr class="${r.kind}${hkCls(r)}${curClsBoth(r)}${attnClsBoth(r)}"${hkAttr(r)}${anchor(aside, ano)}${both} data-i="${ri(r)}">` +
        `<td class="no l">${r.left_no || ""}</td>` +
        `<td class="side l"><span class="pan">${renderCell(r.left, r.left_spans, r.left_tok, "l", hitsL(r))}</span></td>` +
        `<td class="no r">${r.right_no || ""}</td>` +
        `<td class="side r"><span class="pan">${renderCell(r.right, r.right_spans, r.right_tok, "r", hitsR(r))}</span></td></tr>` +
        after(4, ["new", r.right_no], ["old", r.left_no]);
    }
  }
  html += "</table>";
  if (d.truncated) html += `<div class="notice">alignment truncated (size guard)</div>`;
  return html;
}


function renderDiff(d) {
  // A single diff never paints over a stack: the stack owns #diff-body.
  if (state.stack) return;
  // A NEW diff starts with every run folded; a re-render of the same one (a
  // resize, a notes refresh, the f toggle) keeps the folds the reader opened.
  // A new diff is also a new search: the query does not follow a file step.
  if (d !== state.lastDiff) {
    state.diffFolds = new Set();
    diffSearchBar.reset(); // no re-render: this render is the new file's
  }
  state.lastDiff = d; // re-rendered on window resize (layout is width-dependent)
  state.diffBlockIdx = -1;
  $("diff-body").innerHTML = diffHTML(d, $("diff-pane").clientWidth, true, state.diffFolds, null, null, diffHunks ? { sel: diffHunks.sel } : null);
  mountPanBars($("diff-body"), $("diff-hbars"));
  diffSearchBar.paint(); // the render re-found: the count must follow
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
  if (state.stack) return rerenderStack();
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
  if (state.stack) rerenderStack(on); // flipping ON starts every file folded
  else rerenderDiffKeepingPlace(keepScroll);
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
  // Every table on the host counts, not the first: a stacked diff mixes
  // two-column tables with one-column ones (a pure add or delete). Any
  // two-column table → a bar per side; a one-column cell then pans with the
  // side it shows (.l = the old side, else the new).
  const twoCol = !!host.querySelector("table.diff colgroup col:nth-child(4)");
  // overflow: how far the widest line a bar drives runs past ITS OWN cell —
  // cells differ in width (a one-column cell is the whole pane, a side is
  // half), so the widest line alone says nothing. 12 = the cell padding.
  const overflow = (sel) => {
    let o = 0;
    for (const p of host.querySelectorAll(sel)) o = Math.max(o, p.offsetWidth - (p.parentElement.clientWidth - 12));
    return o;
  };
  const pan = host._pan || (host._pan = { l: 0, r: 0 });
  bars.classList.remove("hidden");
  // Each bar is a native scroller (wheel, touch, scrollLeft — what search
  // and restore drive) with its own scrollbar hidden, under a thumb gg paints
  // itself: Firefox (and macOS) draw OVERLAY scrollbars that only show while
  // hovered or scrolling, so a native 14px bar read as "no scrollbar at all".
  const barHTML = (side) => `<div class="hbar-wrap"><div class="hbar" data-side="${side}"><div></div></div><div class="hthumb"></div></div>`;
  bars.innerHTML = twoCol ? barHTML("l") + barHTML("r") : barHTML("lr");
  for (const bar of bars.querySelectorAll(".hbar")) {
    const side = bar.dataset.side;
    const o = side === "l" ? overflow("td.side.l > .pan") : side === "r" ? overflow("td.side:not(.l) > .pan") : overflow("td.side > .pan");
    bar.firstElementChild.style.width = Math.max(o, 0) + bar.clientWidth + "px";
    const paintThumb = mountThumb(bar);
    const apply = () => {
      const x = bar.scrollLeft;
      if (side !== "r") { pan.l = x; host.style.setProperty("--pan-l", x + "px"); }
      if (side !== "l") { pan.r = x; host.style.setProperty("--pan-r", x + "px"); }
      paintThumb();
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


// mountThumb wires the painted thumb beside a bar's native scroller and
// returns its painter: the thumb's size and place mirror the scroller's
// (hidden when nothing overflows); dragging it pans, a click on the track
// pages toward the click.
function mountThumb(bar) {
  const thumb = bar.nextElementSibling;
  const track = bar.parentElement;
  const geom = () => {
    const cw = bar.clientWidth;
    const range = bar.scrollWidth - cw;
    const w = range > 0 ? Math.max(24, (cw * cw) / bar.scrollWidth) : 0;
    return { cw, range, w };
  };
  const paint = () => {
    const { cw, range, w } = geom();
    thumb.classList.toggle("none", range <= 1);
    if (range <= 1) return;
    thumb.style.width = w + "px";
    thumb.style.left = (bar.scrollLeft / range) * (cw - w) + "px";
  };
  thumb.addEventListener("pointerdown", (e) => {
    e.preventDefault();
    e.stopPropagation();
    const { cw, range, w } = geom();
    const x0 = e.clientX;
    const s0 = bar.scrollLeft;
    const k = cw > w ? range / (cw - w) : 0;
    thumb.setPointerCapture(e.pointerId);
    thumb.classList.add("drag");
    const move = (ev) => { bar.scrollLeft = s0 + (ev.clientX - x0) * k; };
    const up = () => {
      thumb.classList.remove("drag");
      thumb.removeEventListener("pointermove", move);
      thumb.removeEventListener("pointerup", up);
      thumb.removeEventListener("pointercancel", up);
    };
    thumb.addEventListener("pointermove", move);
    thumb.addEventListener("pointerup", up);
    thumb.addEventListener("pointercancel", up);
  });
  track.addEventListener("pointerdown", (e) => {
    if (e.target === thumb) return;
    e.preventDefault();
    const left = thumb.getBoundingClientRect().left;
    bar.scrollLeft += (e.clientX < left ? -0.9 : 0.9) * bar.clientWidth;
  });
  return paint;
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


registerHelp({
  key: "z / Z · fold notes",
  html:
    "every review-note box folds to its title line: <b>click the title</b>, or <b>z</b> for the note you " +
    "are on and <b>Z</b> for every thread of the file (again to unfold). The ◆ menu has the same row. A " +
    "pull request's <b>resolved</b> review threads start folded. Review threads from the forge are " +
    "<b>read-only</b> (teal boxes titled <i>review · author · age</i>): they can be folded and their " +
    "place copied as a gg link, never edited, answered or removed — <b>c</b> still adds a note of your " +
    "own beside them, and the agent-notes switch (<b>a</b>) never hides them",
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
function notesArmed(ctx = state.diffCtx) {
  return !!ctx && ctx.notes !== false;
}

// noteQuery builds the /api/notes query for the open diff. Working-tree diffs
// carry their section as `state`; a commit diff carries rev + state=commit.
function noteQuery(ctx = state.diffCtx) {
  if (!notesArmed(ctx)) return null;
  const q = new URLSearchParams({ path: ctx.path });
  if (ctx.preview && ctx.preview.pr) {
    // A pull request is named by its NUMBER, never by its sides: those are
    // display text (the head may live in a fork). rev and state ride along for
    // the WRITE, exactly as below — `c` on a PR diff adds a LOCAL note on the
    // fetched head.
    q.set("n", String(ctx.preview.pr));
    q.set("rev", ctx.rev);
    q.set("state", "commit");
    return q;
  }
  if (ctx.preview && ctx.preview.pair) {
    // A commit pair has no names: it is read by its two ids. rev and state
    // ride along for the WRITE — a pair note is an ordinary note on b.
    q.set("a", ctx.preview.pair.a);
    q.set("b", ctx.preview.pair.b);
    q.set("rev", ctx.rev);
    q.set("state", "commit");
    return q;
  }
  if (ctx.preview) {
    // The preview gathers notes along the branch, so the READ is keyed on the
    // PAIR: a note written against an older commit still belongs here. rev and
    // state ride along unchanged because they are what the WRITE needs — a
    // preview note is an ordinary commit note on the source tip, and
    // addNotePrompt builds its post out of this same query.
    q.set("source", ctx.preview.source);
    q.set("target", ctx.preview.target);
    q.set("rev", ctx.rev);
    q.set("state", "commit");
    return q;
  }
  if (ctx.state === "commit") {
    if (!ctx.rev) return null;
    q.set("rev", ctx.rev);
    q.set("state", "commit");
  } else {
    q.set("state", ctx.state || "unstaged");
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


// notesFor READS one context's notes and writes nothing. It is the door a
// stack's slot uses (stackview.js), so a file's notes are fetched the same way
// whether it is read alone or inside a stack. null = this context has none to
// read (not note-addressable).
async function notesFor(ctx) {
  const q = noteQuery(ctx);
  if (!q) return null;
  // A pull request reads its own twin of the preview endpoint: the pair
  // endpoint resolves branch NAMES, and a PR's are display text (a fork's
  // branch, or worse a same-named local one).
  const pv = ctx.preview;
  const url = !pv ? "/api/notes?" : pv.pr ? "/api/pr/notes?" : pv.pair ? "/api/pair/notes?" : "/api/preview/notes?";
  return await getJSON(url + q);
}


// noteCollapseKey identifies ONE open diff for the collapse set: a new file
// (or revision) starts with the forge's resolved threads folded, a re-read of
// the same one keeps what the reader folded by hand.
function noteCollapseKey(ctx) {
  return ctx.path + "\0" + (ctx.rev || ctx.state || "");
}


async function fetchNotes(rerender = true) {
  // A stack has one address per FILE, so there is no single read to redo: each
  // loaded slot re-reads its own (the TUI fans out the same way).
  if (state.stack) return refreshStackNotes();
  if (!noteQuery()) {
    state.notes = [];
    return;
  }
  const prev = !!state.diffCtx.preview;
  try {
    const d = await notesFor(state.diffCtx);
    if (!d) {
      state.notes = [];
      return;
    }
    state.notes = d.notes || [];
    // The collapse set is per OPEN DIFF: a new file (or revision) starts with
    // the forge's resolved threads folded, while a re-read of the same diff —
    // a write, a comment re-poll — keeps what the reader folded by hand.
    const key = noteCollapseKey(state.diffCtx);
    if (state.noteCollapsedFor !== key) {
      state.noteCollapsedFor = key;
      state.noteCollapsed = seedCollapsed(state.notes);
    }
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
  // A preview's totals ride fetchPreviews; a pair has no such ride, so a note
  // written elsewhere reaches its badges from here.
  loadPairCounts();
}


// noteRowsHTML renders the <tr class="note"> rows anchored on (side, no).
// Agent notes are skipped while the agent layer is off; user notes always
// show — including a user reply under a hidden agent root (the TUI's rule:
// the filter is per ROW, by that row's own source).
function noteRowsHTML(side, no, cols, nctx = null) {
  const nc = nctx || globalNoteCtx();
  if (!no || !notesArmed(nc.ctx) || !nc.notes.length) return "";
  let html = "";
  for (const n of nc.notes) {
    if (n.side !== side || n.line !== no) continue;
    html += noteBoxHTML(n, cols, nc);
  }
  return html;
}


// noteSlotCtx is the note context of the slot a NOTE belongs to: a ◆ menu's
// "copy gg link to this note" must name that note's own file.
function noteSlotCtx(n) {
  if (!state.stack || !n) return null;
  const s = state.stack.slots.find((x) => (x.notes || []).some((m) => m.id === n.id));
  return s ? s.ctx || null : null;
}


// rowSlotCtx is the note context of the stack slot a DOM row sits in, or null
// when there is no stack (or the row is outside one).
function rowSlotCtx(el) {
  if (!el || !state.stack) return null;
  const sec = el.closest(".stk-file");
  if (!sec) return null;
  const s = state.stack.slots[Number(sec.dataset.k)];
  return s ? s.ctx || null : null;
}


// globalNoteCtx is the single-file view's note context: today's globals, in
// the shape a stack's slot carries. activeDiff() (stackview.js) returns this
// or the cursor slot's own.
function globalNoteCtx() {
  return { ctx: state.diffCtx, notes: state.notes || [], row: state.diffRow };
}


// noteBoxHTML is one thread as a hunk-style box: the title in the top border
// ("agent note · author · path R204"), the summary in bold, the rationale,
// then each reply as its own block. In the split layout (4 columns) the box
// sits in the pane of the note's side and the other pane stays blank, so the
// note visibly hangs off one version; the single-column layouts span the row.
// With the agent layer off, agent-written parts drop out row by row; a thread
// with nothing left renders nothing.
function noteBoxHTML(n, cols, nctx = null) {
  const nc = nctx || globalNoteCtx();
  const off = state.notesAgentOff;
  // A forge review thread is never the agent layer's to hide, whatever its
  // author is called.
  const hidden = (x) => off && x.source === "agent" && !x.read_only;
  const rootOn = !hidden(n);
  const reps = (n.replies || []).filter((r) => !hidden(r));
  if (!rootOn && !reps.length) return "";
  // A preview names it "outdated": there, a note whose lines a later commit
  // changed is the expected case, not an edge one — so the server sends that
  // word and the row wears a class of its own.
  const stale = n.status === "stale" || n.status === "outdated";
  const prev = !!(nc.ctx && nc.ctx.preview);
  const cls = prev ? "outdated" : "stale";
  const agent = n.source === "agent";
  const kind = n.read_only ? "forge" : agent ? "agent" : "user";
  const folded = state.noteCollapsed.has(n.id);
  const title = noteTitle(n, nc.ctx ? nc.ctx.path : "", prev, Date.now());
  // A FORGE note is markdown, and the server sends it parsed (summary_md, md):
  // those trees are painted. A note written here is shown exactly as typed —
  // it carries no tree, so it falls to the escaped text.
  const part = (r, head) =>
    `<div class="notesum">${esc(head)}${r.summary_md && r.summary_md.length ? mdInlineHTML(r.summary_md, esc) : esc(r.summary)}</div>`;
  const text = (r) =>
    r.md ? `<div class="notetext md">${mdHTML(r.md, esc, { skipFirstCaption: r.summary === "suggestion" })}</div>` : r.rationale ? `<div class="notetext">${esc(r.rationale)}</div>` : "";
  // The title is the collapse handle (click, or z): the box keeps its title
  // line and drops its body. The fold is a CLASS on the row, toggled in place
  // — never a re-render, which would reset the ‹/› stepper and jolt the scroll.
  let box =
    `<div class="notebox ${kind}${stale ? " " + cls : ""}">` +
    `<div class="notetitle" data-collapse="${esc(n.id)}" title="click (or z) to collapse / expand">` +
    `<span class="notefold"></span>${esc(title)}</div>`;
  if (rootOn) box += part(n, "") + text(n);
  for (const r of reps) {
    box += `<div class="notereply" data-note="${esc(r.id)}">` + part(r, "↳ " + (r.author ? r.author + ": " : "")) + text(r) + `</div>`;
  }
  box += `</div>`;
  const cell = (span) => `<td class="note" colspan="${span}">${box}</td>`;
  const gap = `<td class="note-gap" colspan="2"></td>`;
  // A whole-file thread belongs to neither pane: it spans the row.
  const cells = cols === 4 && !n.file_level ? (n.side === "old" ? cell(2) + gap : gap + cell(2)) : cell(cols);
  const rowCls = (stale ? " " + cls : "") + (agent ? " agent" : "") + (n.read_only ? " forge" : "") +
    (n.file_level ? " filenote" : "") + (folded ? " collapsed" : "");
  return `<tr class="note${rowCls}" data-note="${esc(n.id)}">${cells}</tr>`;
}


// fileNoteRowsHTML is the whole-file threads (a forge comment on the file,
// not on a line): they have no row to hang off, so they lead the table.
function fileNoteRowsHTML(cols, nctx = null) {
  const nc = nctx || globalNoteCtx();
  if (!notesArmed(nc.ctx)) return "";
  return nc.notes.filter((n) => n.file_level).map((n) => noteBoxHTML(n, cols, nc)).join("");
}


// previewCtx is the slice of state.previewOpen a diff context carries: the
// display pair, the PR number, and — for a PR — the pair its gg:// link names.
function previewCtx(po) {
  return { source: po.source, target: po.target, pr: po.pr || 0, linkSource: po.linkSource || "", linkTarget: po.linkTarget || "" };
}


// setNoteCollapsed folds/unfolds in place: the class on the row, the id in
// the set. id == null is the `Z` form — every thread of this diff, collapsing
// unless they all already are.
function toggleNoteCollapsed(id) {
  if (id == null) {
    const all = stackAllNotes();
    const on = !all.every((n) => state.noteCollapsed.has(n.id));
    setAllCollapsed(state.noteCollapsed, all, on);
    for (const tr of noteRowEls()) tr.classList.toggle("collapsed", on);
    return;
  }
  const root = stackAllNotes().find((n) => n.id === id || (n.replies || []).some((r) => r.id === id));
  if (!root) return;
  const on = toggleCollapsed(state.noteCollapsed, root.id);
  const tr = noteRowEls().find((el) => el.dataset.note === root.id);
  if (tr) tr.classList.toggle("collapsed", on);
}


// collapseNearestNote is the `z` key: the note E/R would act on.
function collapseNearestNote() {
  const n = nearestNote();
  if (n) toggleNoteCollapsed(n.id);
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
  const row = { side, no };
  state.diffRow = row;
  // In a stack the marked row also says WHICH FILE the reader is in: the note
  // keys act on that slot's address, so the row is recorded on it and the slot
  // becomes the active one.
  const sec = tr.closest(".stk-file");
  if (state.stack && sec) {
    const k = Number(sec.dataset.k);
    const s = state.stack.slots[k];
    if (s) {
      for (const o of state.stack.slots) if (o !== s) o.row = null;
      s.row = row;
      state.stack.anchor = k;
      followInList();
    }
  }
  // Marking a row is an explicit "I am looking HERE", so it outranks a
  // previous }/{ landing for nearestNote. stepNote re-claims the id right
  // after its own call.
  noteStepId = null;
}


// firstChangedRow is where `c` lands when nothing has been clicked: the first
// changed row of the diff, which is what a reviewer means by "here".
function firstChangedRow(root = null) {
  for (const tr of diffChangeBlocks(root)) {
    const no = Number(tr.dataset.no);
    if (no) return { side: tr.dataset.side, no };
  }
  const any = (root || $("diff-body")).querySelector("tr[data-no]");
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
function firstNewSideRow(root = null) {
  for (const head of diffChangeBlocks(root)) {
    for (let tr = head; tr; tr = tr.nextElementSibling) {
      if (tr.classList.contains("note")) continue; // a ◆ box, not a diff row
      if (tr.classList.contains("same")) break; // the run ended
      const no = Number(tr.dataset.no);
      if (tr.dataset.side === "new" && no) return { side: "new", no };
    }
  }
  const any = (root || $("diff-body")).querySelector(`tr[data-no][data-side="new"]`);
  return any ? { side: "new", no: Number(any.dataset.no) } : null;
}


function noteRowEls() {
  return [...$("diff-body").querySelectorAll("tr.note[data-note]")];
}


// findNote resolves an id to its wire record, replies included.
function findNote(id) {
  for (const n of stackAllNotes()) {
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
  const ad = activeDiff();
  const q = noteQuery(ad.ctx);
  if (!q) return;
  const scope = noteScope();
  let at = ad.row || firstChangedRow(scope);
  if (!at) return;
  // A preview's old side is the MERGE BASE, which no stored address names, so
  // there is nothing there to anchor to (domain.ErrPreviewOldSide). The refusal
  // is here rather than at the server so the prompt never opens on a line the
  // write would reject afterwards.
  if (ad.ctx.preview && at.side === "old") {
    // Nothing was CLICKED: the old side is just where firstChangedRow landed
    // (a file whose first change is a pure deletion), not a line the user
    // named. Fall forward to the file's first new-side row instead of
    // refusing a file that has a perfectly addressable side. An explicit
    // old-side click still gets the refusal — there the user meant that line.
    const fwd = !ad.row && firstNewSideRow(scope);
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
// readOnlyNote refuses a write on a forge review comment: gg shows those and
// never edits, answers or removes them.
function readOnlyNote(n) {
  if (!n.read_only) return false;
  opLine("forge review comments are read-only", true);
  return true;
}


function editNotePrompt(note) {
  const n = note || nearestNote();
  if (!n || readOnlyNote(n)) return;
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
  if (!n || readOnlyNote(n)) return;
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
  // The gate is the clicked ROW's own file in a stack (each carries its own
  // address), and the single-file view's context otherwise — reading the
  // global one here would leave every row unmarkable inside a stack.
  const handle = e.target.closest(".notetitle[data-collapse]");
  if (!notesArmed(rowSlotCtx(handle || e.target.closest("tr")) || state.diffCtx)) return;
  // A note's title line is its fold handle.
  if (handle && getSelection().isCollapsed) {
    toggleNoteCollapsed(handle.dataset.collapse);
    return;
  }
  const tr = e.target.closest("tr[data-no]");
  if (!tr || !getSelection().isCollapsed) return; // don't re-anchor mid-selection
  const td = e.target.closest("td");
  const { side, no } = rowSideAndLine(tr, td);
  markDiffRow(tr, side, no);
});


$("diff-body").addEventListener("contextmenu", (e) => {
  // A link inside a forge note keeps the BROWSER's menu (copy link address…).
  if (e.target.closest("a[href]")) return;
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
    // In a stack the link must name the ROW's own file, not whichever slot was
    // active: a right-click is itself a "this line" gesture.
    const rowCtx = rowSlotCtx(row) || state.diffCtx;
    if (row && notesArmed(rowCtx)) {
      const td = e.target.closest("td");
      let { side, no } = rowSideAndLine(row, td);
      // A preview has no old side. A context row's LEFT cell still names a
      // line that exists on the new side (data-rno), so that number travels;
      // a deletion row has none, and linkFor degrades the link to the file
      // form (the user's ruling, 2026-09-16 — the TUI's cursor on a deletion
      // row copies the same file link).
      if (side === "old" && rowCtx.preview) {
        const rn = Number(row.dataset.rno || 0);
        if (rn) { side = "new"; no = rn; }
      }
      const link = linkFor(state.repo, state.worktree, rowCtx, side, no);
      // Recorded like every other copy (Task 9): copyLink, never copyText.
      // The Desc names the FILE — a line link's row in `gg links` has to be
      // recognisable, and the line number is already in the link text.
      if (link)
        rows.push({
          label: "copy gg link to this line",
          act: () => copyLink(link, linkDesc("file", (rowCtx && rowCtx.path) || "", "")),
        });
    }
    // Staging, on a selectable row: act on the selection, or on the hunk under
    // the pointer (GitKraken's two rows). They lead the menu — they are what a
    // right-click in a working-tree diff is for.
    const hkRow = e.target.closest("tr[data-hunk][data-hr]");
    if (hkRow) rows.unshift(...hunkMenuRows(hkRow));
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
  const rootId = n.parent_id ? (state.notes.find((x) => (x.replies || []).some((r) => r.id === n.id)) || n).id : n.id;
  const foldRow = {
    label: (state.noteCollapsed.has(rootId) ? "Expand" : "Collapse") + " thread",
    hint: "z",
    act: () => toggleNoteCollapsed(rootId),
  };
  const noteRows = n.read_only
    ? []
    : [
        { label: "Edit note", act: () => editNotePrompt(n) },
        { label: "Reply…", act: () => replyNotePrompt(n) },
      ];
  const nlink = linkFor(state.repo, state.worktree, noteSlotCtx(n) || state.diffCtx, n.side, n.line);
  if (nlink)
    noteRows.push({
      label: "copy gg link to this note",
      act: () => copyLink(nlink, linkDesc("file", ((noteSlotCtx(n) || state.diffCtx) || {}).path || "", "")),
    });
  noteRows.push(foldRow);
  // A forge review comment is read-only: copy its place, fold it, nothing else.
  if (!n.read_only)
    noteRows.push(
      { sep: true },
      {
        label: n.parent_id ? "Remove reply" : "Remove note (and its replies)",
        danger: true,
        act: () => removeNote(n.id),
      }
    );
  showCtxMenu(noteRows, e.clientX, e.clientY);
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


// ---- content focus + keyboard scrolling ----------------------------------
// Opening a diff puts DOM focus on the pane (tabindex=-1 in index.html), so
// the wheel, space and the page keys scroll the diff — not the file row, the
// blame button or whatever the mouse left the focus on. The key handler
// below does not depend on that focus, though: in the diff layout the arrows
// and page keys always scroll the diff, wherever the focus sits.
function focusDiff() {
  const pane = $("diff-pane");
  if (document.activeElement !== pane) pane.focus({ preventScroll: true });
}

// scrollKey scrolls `el` for a navigation key (↑/↓ a step, PgUp/PgDn a page
// less one step so context carries over, Home/End the ends) and reports
// whether the key was one. The step is a few text rows — the TUI scrolls a
// line per arrow; a browser's own arrow scroll is about the same.
const SCROLL_STEP = 40;
function scrollKey(el, e) {
  let dy = null;
  switch (e.key) {
    case "ArrowDown": dy = SCROLL_STEP; break;
    case "ArrowUp": dy = -SCROLL_STEP; break;
    case "PageDown": dy = el.clientHeight - SCROLL_STEP; break;
    case "PageUp": dy = -(el.clientHeight - SCROLL_STEP); break;
    case "Home": el.scrollTop = 0; e.preventDefault(); return true;
    case "End": el.scrollTop = el.scrollHeight; e.preventDefault(); return true;
    default: return false;
  }
  el.scrollTop += dy;
  e.preventDefault();
  return true;
}

// diffScrollKey is the diff layout's arrow / page / home / end handling:
// scroll the diff (the TUI's ↑↓ scroll a line there). j/k stay the file
// cursor's, so the keyboard can still walk the file list behind the diff.
// Not while a conflict picker owns the pane — it has its own cursor.
function diffScrollKey(e) {
  if (state.layout !== "diff" || conflictPick) return false;
  if (e.ctrlKey || e.metaKey || e.altKey || e.shiftKey) return false;
  return scrollKey($("diff-pane"), e);
}


// ---- in-view search (the TUI's / @ ] [ in its diff view) -----------------
// The engine (inviewsearch.js) and the bar (searchbar.js) are shared with
// blame; this block is what the DIFF host owns: which lines are searched,
// how a hit is found in the rendered table, and where the reader "is".
const diffSearch = new Search();

// diffSearchLines lists the searchable text of the rendered items (folds
// skipped) in document order, keyed by the row's index into d.rows. A row
// whose two sides carry the same text (same) is searched on the RIGHT only —
// searching both would make ] stop twice on one piece of text — and painted
// in both columns; every other row is searched on each side it has a line on.
function diffSearchLines(items, ri) {
  const out = [];
  for (const r of items) {
    if (r.fold) continue;
    const i = ri(r);
    if (r.kind === "same") {
      out.push({ row: i, side: 1, text: r.right || "" });
      continue;
    }
    if (r.left_no) out.push({ row: i, side: 0, text: r.left || "" });
    if (r.right_no) out.push({ row: i, side: 1, text: r.right || "" });
  }
  return out;
}

// diffSearchHere is where the reader is when no hit is current: the first
// rendered row inside the pane's viewport (col -1 = before its text), so a
// typed query lands on the nearest hit from the screen, not from the top.
function diffSearchHere() {
  const pane = $("diff-pane").getBoundingClientRect();
  for (const tr of $("diff-body").querySelectorAll("table.diff tr[data-i]")) {
    if (tr.getBoundingClientRect().bottom >= pane.top) return { row: Number(tr.dataset.i), side: 0, col: -1 };
  }
  return { row: 0, side: 0, col: -1 };
}

// diffHitEls are the elements one hit was rendered into (a hit split across
// syntax runs is several), in the diff pane only.
function diffHitEls(i) {
  return $("diff-body").querySelectorAll(`table.diff .hit[data-h="${i}"]`);
}

// goToDiffHit makes hit i current — moving the class, not re-rendering — and
// scrolls it into view; in scroll mode the side's pan bar is dragged so the
// hit's columns are on screen too (the TUI's panFor).
function goToDiffHit(i) {
  if (i < 0 || i >= diffSearch.hits.length) return;
  if (diffSearch.cur !== i) {
    for (const el of diffHitEls(diffSearch.cur)) el.classList.remove("cur");
    diffSearch.cur = i;
    for (const el of diffHitEls(i)) el.classList.add("cur");
  }
  const els = diffHitEls(i);
  if (!els.length) return;
  const el = els[0];
  el.scrollIntoView({ block: "center", inline: "nearest" });
  if (state.textMode !== "scroll") return;
  const td = el.closest("td.side");
  const pan = el.closest(".pan");
  if (!td || !pan) return;
  const side = td.classList.contains("l") ? "l" : "r";
  // In a stack each FILE pans on its own, so the bar is the section's own —
  // the pane-wide pair is hidden there and panning it would move nothing.
  const sec = el.closest(".stk-file");
  const bars = (sec && sec.querySelector(".stk-hbars")) || $("diff-hbars");
  const bar = bars.querySelector(`.hbar[data-side="${side}"]`) || bars.querySelector(".hbar");
  if (!bar) return;
  // The hit's x within the (translated) line, against the cell's width.
  const x = el.getBoundingClientRect().left - pan.getBoundingClientRect().left;
  const w = el.getBoundingClientRect().width;
  const cellW = td.clientWidth - 12;
  const cur = bar.scrollLeft;
  if (x < cur || x + w > cur + cellW) bar.scrollLeft = Math.max(0, x - Math.floor(cellW / 3));
}

// The bar: typing re-renders the diff — the render is what re-finds, over
// the rows it actually paints — keeping the scroll position, then lands on
// the current hit; esc restores the pane's scroll and pan.
const diffSearchBar = bindSearchBar("diff-search", {
  search: diffSearch,
  here: () => (state.stack ? stackSearchHere() : diffSearchHere()),
  // Stacked, the count is over the WHOLE stack and a trailing + says files
  // are still unsearched — ] steps into them (design D1).
  count: () => {
    const c = diffSearch.count();
    if (!c || !state.stack) return c;
    return unsearchedSlots() > 0 ? c + "+" : c;
  },
  // Stacked, a step may have to unfold and fetch a file to reach a hit, which
  // the bar's own synchronous stepHit cannot do.
  step: (delta) => {
    if (!state.stack) return false;
    stackHitStep(delta);
    return true;
  },
  origin: () => {
    const bars = $("diff-hbars");
    const pan = {};
    for (const b of bars.querySelectorAll(".hbar")) pan[b.dataset.side] = b.scrollLeft;
    return { top: $("diff-pane").scrollTop, pan };
  },
  restore: (o) => {
    $("diff-pane").scrollTop = o.top;
    for (const b of $("diff-hbars").querySelectorAll(".hbar")) if (o.pan[b.dataset.side] != null) b.scrollLeft = o.pan[b.dataset.side];
  },
  render: () => (state.stack ? refindStack() : rerenderDiffKeepingPlace(true)),
  goTo: goToDiffHit,
  focus: focusDiff, // enter hands the keys back to the CONTENT, not the body
});

// diffSearchKey is the diff layout's first refusal on a bare key: / and @
// open a search, ] and [ step a kept one, esc clears a kept one (and
// reports it, so the caller's esc does not ALSO leave the diff). Anything
// else — and everything while no diff is open or a conflict picker owns
// the pane — is not the search's.
function diffSearchKey(e) {
  // state.lastDiff is null while a STACK is up — the stack is the open diff
  // there, and it searches every file of itself (design §11 item 2).
  if (state.layout !== "diff" || (!state.lastDiff && !state.stack) || conflictPick) return false;
  if (e.ctrlKey || e.metaKey || e.altKey) return false;
  if (e.key === "/" || e.key === "@") {
    e.preventDefault(); // the browser's quick-find, and the key must not land in the input
    diffSearchBar.open(e.key === "@");
    return true;
  }
  if (e.key === "]" || e.key === "[") {
    if (!diffSearch.query) return false;
    e.preventDefault();
    diffSearchBar.step(e.key === "]" ? 1 : -1);
    return true;
  }
  if (e.key === "Escape" && diffSearch.active()) {
    diffSearchBar.clear();
    return true;
  }
  return false;
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
// root is the element the walk happens in: the whole diff pane, or ONE file's
// section inside a stack (noteScope) when the gesture belongs to a file.
function diffChangeBlocks(root = null) {
  // Note rows are not diff rows: they carry neither "same" nor a change kind,
  // so without this filter every ◆ row would read as the start of a change
  // block and the ‹/› stepper would walk the notes instead of the changes.
  const rows = (root || $("diff-body")).querySelectorAll("table.diff tr:not(.note)");
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


// ---- line staging, GitKraken's model ---------------------------------------
// In a working-tree diff the reader SELECTS rows and then acts on them, and
// every action is immediate — there is no pick-then-apply step:
//
//   click            select this row (and only it)
//   ctrl/cmd-click   add or remove this row
//   shift-click      select from the last clicked row to this one
//   right-click      Stage selected lines / Stage hunk (the unstaged diff),
//                    Unstage selected lines / Unstage hunk (the staged diff)
//
// The unit is the ROW, because that is what a side-by-side diff shows: a
// modified row is ONE change (its old line replaced by its new one) whichever
// cell is clicked, a removed-only row is its old line, an added-only row its
// new line. Staging half of a modified row would put both lines, or neither,
// into the index — so it is not offered.
//
// The server tags every eligible row with its hunk ("hunk") and its ordinal
// inside it ("hr"), in BOTH lanes (d.hunks.lane): the unstaged diff stages,
// the staged diff (HEAD → index) unstages. Selections are positional against
// the bytes the server hashed, so after every action the diff is re-read.

let diffHunks = null; // {path, hash, lane, count, sel: Set<"hunk:row">} while an eligible diff is open


// hunkEligible: a tracked file with an unstaged change (stage its rows) or a
// staged one (unstage them). Untracked, conflicted and newly added files are
// whole-file only; the server refuses to tag them anyway.
function hunkEligible(f) {
  return !!f && f.kind === "tracked" && (f.section === "changes" || f.section === "staged");
}


function clearDiffHunks() {
  diffHunks = null;
  conflictPick = null;
  renderResolveBar();
}


// hunkState builds the per-file state from a diff's tags.
function hunkState(path, h) {
  return { path, hash: h.hash, lane: h.lane || "unstaged", count: h.count, sel: new Set() };
}


const rowKey = (hunk, row) => hunk + ":" + row;


// taggedRows is every selectable row of one file's table, in document order —
// what a shift-click range walks.
function taggedRows(scope) {
  return [...(scope && scope.el ? scope.el : $("diff-body")).querySelectorAll("tr[data-hunk][data-hr]")];
}


// fileOf resolves a clicked row to its file's staging state: the row's own
// slot in a stack, the single open diff otherwise.
function fileOf(tr) {
  const slot = hunkSlotAt(tr);
  if (slot) return slot;
  return diffHunks ? { hunks: diffHunks, el: $("diff-body") } : null;
}


// paintHunkSel marks the selected rows of one file (every row, both cells —
// the row is the unit).
function paintHunkSel(scope) {
  const v = scope ? scope.hunks : diffHunks;
  taggedRows(scope).forEach((tr) => {
    tr.classList.toggle("hk-sel", !!v && v.sel.has(rowKey(tr.dataset.hunk, tr.dataset.hr)));
  });
}


// ONE selection spans everything on screen — every file of a stack, which
// reads as one document (user ruling 2026-09-24). Each file still keeps its
// own share (scope.hunks.sel, positional against ITS bytes); the pieces below
// treat the shares as one. A row's id is "<file>\u0001<hunk>:<row>".
const ROW_SEP = "\u0001";
let rowAnchor = null; // the row id a shift-click ranges from
let preClickSel = null; // the selection as it stood before a click sequence's first click


// scopeId names a file of the selection: its stack slot, or the single diff
// by lane + path (a stale anchor from another file must not match here).
function scopeId(scope) {
  return scope.slot ? scope.slot.key : scope.hunks.lane + "\u0000" + scope.hunks.path;
}


// rowScopes is every file with selectable rows, in the order shown.
function rowScopes() {
  if (state.stack) return hunkSlots();
  return diffHunks ? [{ hunks: diffHunks, el: $("diff-body") }] : [];
}


// selectionOrder is every selectable row on screen, in document order.
function selectionOrder() {
  const out = [];
  for (const sc of rowScopes()) {
    if (!sc.el) continue;
    const id = scopeId(sc);
    for (const tr of taggedRows(sc)) out.push({ id: id + ROW_SEP + rowKey(tr.dataset.hunk, tr.dataset.hr), lane: sc.hunks.lane, tr });
  }
  return out;
}


function currentSelection() {
  const sel = new Set();
  for (const sc of rowScopes()) for (const key of sc.hunks.sel) sel.add(scopeId(sc) + ROW_SEP + key);
  return { sel, anchor: rowAnchor };
}


// applySelection hands each file its share of a selection and repaints.
function applySelection(s) {
  for (const sc of rowScopes()) {
    const pre = scopeId(sc) + ROW_SEP;
    sc.hunks.sel = new Set([...s.sel].filter((id) => id.startsWith(pre)).map((id) => id.slice(pre.length)));
    paintHunkSel(sc);
  }
  rowAnchor = s.anchor;
}


function selectionSize() {
  return rowScopes().reduce((n, sc) => n + sc.hunks.sel.size, 0);
}


// clearRowSelection empties the selection; false when there was none (so Esc
// falls through to what it does otherwise).
function clearRowSelection() {
  rowAnchor = null;
  if (!selectionSize()) return false;
  applySelection({ sel: new Set(), anchor: null });
  return true;
}


// selectStep applies one click to the selection. order is every selectable
// row in document order ({id, lane}), at the clicked one. Plain click: that
// row alone; ctrl/cmd: toggle it; shift: the range from the anchor, across
// files (ctrl+shift adds the range). A selection holds ONE lane: stage and
// unstage never mix. Pure — the guard imports it.
function selectStep(order, cur, at, mods) {
  const hit = order[at];
  const laneOf = new Map(order.map((r) => [r.id, r.lane]));
  let sel = new Set([...cur.sel].filter((id) => laneOf.get(id) === hit.lane));
  const a = cur.anchor == null ? -1 : order.findIndex((r) => r.id === cur.anchor);
  if (mods.shift && a >= 0 && order[a].lane === hit.lane) {
    if (!mods.ctrl) sel = new Set();
    for (let i = Math.min(a, at); i <= Math.max(a, at); i++) if (order[i].lane === hit.lane) sel.add(order[i].id);
    return { sel, anchor: cur.anchor };
  }
  if (mods.ctrl) {
    if (sel.has(hit.id)) sel.delete(hit.id);
    else sel.add(hit.id);
  } else {
    sel = new Set([hit.id]);
  }
  return { sel, anchor: hit.id };
}


// clickRow applies a click on a selectable row.
function clickRow(tr, mods) {
  const order = selectionOrder();
  const at = order.findIndex((r) => r.tr === tr);
  if (at < 0) return;
  applySelection(selectStep(order, currentSelection(), at, mods));
}


// A shift / ctrl click selects ROWS, not text: the browser would otherwise
// extend its own text selection from the previous click, and the plain-click
// guard below would read that as a drag.
$("diff-body").addEventListener("mousedown", (e) => {
  if (!(e.shiftKey || e.ctrlKey || e.metaKey)) return;
  if (e.target.closest("tr[data-hunk][data-hr]")) e.preventDefault();
});


$("diff-body").addEventListener("click", (e) => {
  const tr = e.target.closest("tr[data-hunk][data-hr]");
  if (!tr) return;
  // a double-click's first click reselects; the double-click acts on what
  // was selected BEFORE it
  if (e.detail <= 1) preClickSel = currentSelection().sel;
  const modified = e.shiftKey || e.ctrlKey || e.metaKey;
  // a plain click that ends a text drag is a copy gesture, not a selection
  if (!modified && !getSelection().isCollapsed) return;
  if (modified) getSelection().removeAllRanges();
  clickRow(tr, { shift: e.shiftKey, ctrl: e.ctrlKey || e.metaKey });
});


// A click anywhere that is not a selectable row clears the selection — the
// context lines, the header, the file list, any other pane. The menu's own
// rows act on the selection and clear it themselves.
document.addEventListener("click", (e) => {
  if (e.button !== 0) return;
  if (e.target.closest && e.target.closest("tr[data-hunk][data-hr], #ctx-menu")) return;
  clearRowSelection();
});


// optimisticRows is what a working-tree diff WILL look like once the rows in
// `keys` ("hunk:row") are staged (lane "unstaged") or unstaged (lane
// "staged") — the prediction the UI shows before the server answers, so the
// action feels instant. Staging moves the working-tree line into the index:
// a modified or added row becomes context, a removed row disappears.
// Unstaging moves HEAD's line back into the index: a modified or removed row
// becomes context, an added row disappears. Only the INDEX side's line
// numbers move (left when staging, right when unstaging). Every row loses its
// hunk tags: they name the bytes the server hashed BEFORE the action, and the
// re-read that follows re-tags the file. Pure — the guard imports it.
function optimisticRows(rows, lane, keys) {
  const stage = lane !== "staged";
  const out = [];
  for (const r of rows) {
    const { hunk, hr, hl, hw, ...base } = r;
    const hit = hunk != null && hr != null && keys.has(hunk + ":" + hr);
    if (!hit) {
      out.push(base);
      continue;
    }
    if (stage) {
      if (r.kind === "del") continue; // the index drops the line
      out.push({ ...base, kind: "same", left: r.right, left_tok: r.right_tok, left_spans: null, right_spans: null });
    } else {
      if (r.kind === "add") continue; // the index drops the line
      out.push({ ...base, kind: "same", right: r.left, right_tok: r.left_tok, left_spans: null, right_spans: null });
    }
  }
  // Renumber the index side from where the file's numbering starts.
  const side = stage ? "left_no" : "right_no";
  const first = rows.find((r) => r[side]);
  let n = first ? first[side] - 1 : 0;
  for (const r of out) {
    const present = stage ? r.kind !== "add" : r.kind !== "del";
    r[side] = present ? ++n : 0;
  }
  return out;
}


// selectionWire turns a selection into the /api/stage-hunks blocks: per hunk,
// the rows picked.
function selectionWire(v) {
  const by = new Map();
  for (const key of v.sel) {
    const [h, r] = key.split(":").map(Number);
    if (!by.has(h)) by.set(h, []);
    by.get(h).push(r);
  }
  return [...by].sort((a, b) => a[0] - b[0]).map(([block, rows]) => ({ block, rows: rows.sort((x, y) => x - y) }));
}


// stageJobs stages (or, in the staged diff, unstages) rows of one or more
// files — one job per file ({scope, blocks, keys}: keys are the rows acted on,
// "hunk:row", blocks the wire) — and the SCREEN moves first: every file's
// predicted diff (optimisticRows) is painted at once, the POSTs follow one
// file at a time, and
//   - a file whose POST fails comes back exactly as it was, selection too;
//   - on success the sidebar takes the fresh status and the files acted on
//     are re-read quietly — no "loading…", no jump, the reader's scroll and
//     folds kept — which re-tags them for the next action.
// A file that left its section (fully staged / unstaged) takes the structural
// path instead.
async function stageJobs(jobs) {
  const live = [];
  for (const j of jobs) {
    const v = j.scope.hunks;
    if (!v || !j.blocks.length) continue;
    const before = j.scope.slot ? j.scope.slot.diff : state.lastDiff;
    if (!before) continue;
    const predicted = { ...before, rows: optimisticRows(before.rows || [], v.lane, j.keys) };
    delete predicted.hunks;
    showFileDiff(j.scope, predicted, null); // non-interactive until the answer: its tags are gone
    live.push({ ...j, v, before });
  }
  let status = null;
  const reread = [];
  for (const j of live) {
    const { scope, v, blocks, keys, before } = j;
    try {
      status = await postJSON("/api/stage-hunks", { path: v.path, lane: v.lane, blocks, hash: v.hash });
      reread.push(j);
    } catch (e) {
      opLine("error: " + (e.message || e), true);
      // 409: the file moved under the action — the truth is a fresh read.
      if (/file changed/.test(e.message || "")) {
        reread.push(j);
        continue;
      }
      v.sel = new Set(keys); // put back what the reader had chosen
      showFileDiff(scope, before, v);
    }
  }
  if (status) {
    applyStatus(status); // the 200 body IS a fresh /api/status payload
    renderFiles();
  }
  // One structural pass covers every file of a stack when any file left it.
  if (state.stack && reread.some((j) => !inLaneSection(j.v.path, j.v.lane))) {
    reconcileStack();
    return;
  }
  for (const j of reread) await quietRefreshFile(j.scope, j.v.path, j.v.lane);
}


// stageSelection acts on the whole selection, every file of it.
function stageSelection() {
  const jobs = rowScopes()
    .filter((sc) => sc.hunks.sel.size)
    .map((sc) => {
      const keys = new Set(sc.hunks.sel);
      return { scope: sc, keys, blocks: selectionWire({ sel: keys }) };
    });
  clearRowSelection();
  void stageJobs(jobs);
}


function inLaneSection(path, lane) {
  const section = lane === "staged" ? "staged" : "changes";
  return state.statusEntries.some((x) => x.path === path && x.section === section);
}


// showFileDiff paints one file's diff in place: a stack repaints that slot
// alone (the reader's header stays pinned), the single view re-renders
// without resetting its folds, its search or its scroll.
function showFileDiff(scope, d, hunks) {
  if (scope.slot) {
    showSlotDiff(scope.slot, d, hunks);
    return;
  }
  diffHunks = hunks;
  const pane = $("diff-pane");
  const top = pane.scrollTop;
  state.lastDiff = d; // the SAME file: renderDiff must not treat it as new
  renderDiff(d);
  pane.scrollTop = top;
}


// quietRefreshFile re-reads the file the action touched and repaints only
// it. When the file has left the section the action was taken in, the view
// takes the structural path instead (the single view re-opens what the
// cursor lands on, the stack reconciles its file list).
async function quietRefreshFile(scope, path, lane) {
  const section = lane === "staged" ? "staged" : "changes";
  const f = state.statusEntries.find((x) => x.path === path && x.section === section);
  if (!f) {
    if (state.stack) reconcileStack();
    else reopenAfterHunkStage(path, lane);
    return;
  }
  let d;
  try {
    d = await getJSON(fileDiffURL(f));
  } catch (e) {
    opLine("error: " + (e.message || e), true);
    return;
  }
  const hunks = d.hunks && hunkEligible(f) ? hunkState(f.path, d.hunks) : null;
  if (scope.slot) {
    scope.slot.f = f;
    showSlotDiff(scope.slot, d, hunks);
  } else {
    showFileDiff(scope, d, hunks);
  }
}


// actOnRow is the double-click. On a row that WAS selected it stages the
// whole selection (as it stood before the double-click's own clicks
// reselected); on any other row it drops the selection and stages that row.
function actOnRow(tr) {
  const scope = fileOf(tr);
  if (!scope) return;
  const id = scopeId(scope) + ROW_SEP + rowKey(tr.dataset.hunk, tr.dataset.hr);
  const pre = preClickSel || new Set();
  preClickSel = null;
  if (pre.has(id)) applySelection({ sel: pre, anchor: rowAnchor });
  else applySelection({ sel: new Set([id]), anchor: id });
  stageSelection();
}


$("diff-body").addEventListener("dblclick", (e) => {
  const tr = e.target.closest("tr[data-hunk][data-hr]");
  if (!tr) return;
  getSelection().removeAllRanges(); // a double-click also selects a word: not wanted here
  actOnRow(tr);
});


// reopenAfterHunkStage re-opens path in the lane the action was taken in —
// what is left of its unstaged (or staged) change — and falls back to what the
// cursor lands on when the file has left that section.
function reopenAfterHunkStage(path, lane = "unstaged") {
  const section = lane === "staged" ? "staged" : "changes";
  const i = state.statusEntries.findIndex((f) => f.path === path && f.section === section);
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


// hunkMenuRows are the right-click rows for a selectable row: act on the
// selection, or on the hunk under the pointer. A right-click on a row that is
// not selected selects it alone first — the selection must be what the menu
// names, as in GitKraken.
function hunkMenuRows(tr) {
  const scope = fileOf(tr);
  if (!scope) return [];
  const v = scope.hunks;
  const key = rowKey(tr.dataset.hunk, tr.dataset.hr);
  if (!v.sel.has(key)) {
    clickRow(tr, { shift: false, ctrl: false });
  }
  const verb = v.lane === "staged" ? "Unstage" : "Stage";
  const n = selectionSize();
  const block = Number(tr.dataset.hunk);
  // "Stage hunk" predicts every row of the hunk: the ones this file shows.
  const hunkKeys = new Set(
    taggedRows(scope)
      .filter((x) => Number(x.dataset.hunk) === block)
      .map((x) => rowKey(x.dataset.hunk, x.dataset.hr))
  );
  return [
    {
      label: `${verb} selected line${n === 1 ? "" : "s"}${n > 1 ? ` (${n})` : ""}`,
      act: () => stageSelection(),
    },
    {
      label: `${verb} hunk`,
      act: () => {
        clearRowSelection();
        void stageJobs([{ scope, blocks: [{ block, whole: true }], keys: hunkKeys }]);
      },
    },
  ];
}


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


// closeConflictPick drops an open hunk picker's state (a stack is taking
// the pane) and hides its bar.
function closeConflictPick() {
  conflictPick = null;
  renderResolveBar();
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
    // A link comparison has no single revision to be "here": its right side
    // may be the working tree, a stash's third parent, a shelf entry. The rows
    // that need one are left out rather than sent an empty rev.
    //
    // A PAIR landing is the exception: its right side IS one commit, b — the
    // server named it (pairCtx). Only for a row whose bytes live on that side's
    // own endpoint, though: a `-u` stash keeps its untracked files on a third
    // parent (right_spec), and "history at b" would be history of a file b
    // never held. hereRev is for these rows ONLY — the link contributor below
    // still gets the comparison's own (empty) rev and the pair as its address.
    const pc = pairCtx();
    const hereRev = rev || (pc && !f.right_spec ? pc.b : "");
    const revRows = hereRev
      ? [
          { label: "file history", act: () => openFileHistory(f.path, hereRev) },
          { label: "blame at this commit", act: () => openFileBlame(f.path, hereRev) },
          // gg's own stores, addressed at the commit being viewed: a bookmark
          // points AT this version, a shelf entry freezes its bytes.
          { label: "bookmark this file", act: () => addFileEntry("bookmarks", f.path, "committed", hereRev) },
          { label: "add to shelf", act: () => addFileEntry("shelf", f.path, "committed", hereRev) },
        ]
      : [];
    showCtxMenu(
      [
        ...revRows,
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
          preview: po ? previewCtx(po) : pairCtx() ? pairNoteCtx(pairCtx()) : null,
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
export { SECTION_LABELS, diffSearch, goToDiffHit, rowNoteCtx, notesFor, globalNoteCtx, noteCollapseKey, closeConflictPick, fileDiffURL, setDiffTitle, updateLinkCompareFiles, activeFileList, diffScrollKey, diffSearchKey, diffSearchBar, scrollKey, applyFilesHidden, applyTextMode, cycleTextMode, mountPanBars, toggleFilesHidden, setCommitTitle, setFilesDesc, commitBody, commitMetaParts, addNotePrompt, noteBadgeHTML, applyCompareFilter, cfSideCount, clearDiffHunks, commitMetaLine, conflictPick, cycleFilesSort, diffChangeBlocks, toggleMark, diffHTML, diffHunks, drillOut, editNotePrompt, enterFilesStage, fetchNotes, exitStatusToList, hunkAttr, hunkCls, hunkEligible, markDiffRow, renderCell, openCompare, openConflictPicker, openEntryCompare, openLinkCompare, openEntryFileDiff, notesArmed, openFile, openStatusDiff, openWorkingTree, paintConflictPicks, reconcileStatusView, renderCompareBar, renderDiff, renderFiles, refreshNoteCounts, renderResolveBar, reopenAfterHunkStage, replyNotePrompt, resolveConflictPicked, setAllConflictPicks, setFilesMeta, setLayout, stage, stepChange, stepFile, stepNote, stepToNextConflict, toggleDiffView, toggleNoteCollapsed, collapseNearestNote, applyDiffView, revealDiffRow, toggleNotesAgent, updateDiffNav, paintHunkSel, hunkState, clearRowSelection };
