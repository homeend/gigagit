// Stored AI reviews in the page — the TUI's review rows and review view
// (internal/tui/review_view.go). A review is ONE commit-level note holding a
// review document; the server reads it (/api/review/{id}) and builds its line
// notes at read time, read-only. The page lists a commit's reviews above its
// files, a branch's under its row, and opens one as a review view: the
// commit's files (a range review: the compare of its range) with ≡ Overview
// first and only the review's notes in the diffs.

import { $, esc, getJSON, postJSON, runOnce, state } from "./core.js";
import { copyText, showCtxMenu } from "./layers.js";
import { opLine, showLocalConfirm } from "./ops.js";
import { mdHTML } from "./markdown.js";
import { registerHelp } from "./menus.js";
import { NOTE_BADGE_COLS, loadPairCounts, enterFilesStage, fileCols, filePathHTML, noteBadgeHTML, refreshNoteCounts, renderCompareBar, renderFiles, setCommitTitle, setDiffTitle, setFilesKind, setFilesMeta, setLayout, updateDiffNav } from "./files.js";
import { openStack, stackOn, teardownStack } from "./stackview.js";
import { openCommitByHash } from "./commits.js";
import { focusPane } from "./keys.js";
import { openNotesWindow } from "./shelfnotes.js";
import { runLinkCompare } from "./linkcompare.js";
import { copyLink } from "./links.js";

// --- reviews pure (guarded against Go) ---
// reviewStamp is a review's time as the TUI prints it: local
// "YYYY-MM-DD HH:MM" — a date, never "2h ago". "" for a missing/bad time.
function reviewStamp(iso) {
  const d = new Date(iso || "");
  if (!iso || isNaN(d)) return "";
  const p = (n) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
}

// reviewWords is "<stamp> <agent>", falling back to the summary when both
// are missing (the TUI's reviewRowText).
function reviewWords(r) {
  const parts = [reviewStamp(r.created), (r.agent || "").trim()].filter(Boolean);
  return parts.length ? parts.join(" ") : (r.summary || "").trim();
}

// reviewRowText is a commit's review row: "└ 2026-09-29 14:05 claude".
function reviewRowText(r) {
  return "└ " + reviewWords(r);
}

// branchReviewText is a branch's review sub-row: "└ Review: 09-28 23:37
// Claude Code" — the year only when it is not now's (the TUI's rule), so the
// row fits the sidebar.
function branchReviewText(r, now = new Date()) {
  const d = new Date(r.created || "");
  let stamp = reviewStamp(r.created);
  if (stamp && d.getFullYear() === now.getFullYear()) stamp = stamp.slice(5);
  const parts = [stamp, (r.agent || "").trim()].filter(Boolean);
  return "└ Review: " + (parts.length ? parts.join(" ") : (r.summary || "").trim());
}

// branchReviews is b's reviews of its CURRENT tip, newest first as given. The
// branch list's hash may be short; a review's commit is a full sha.
function branchReviews(reviews, b) {
  const h = (b && b.hash) || "";
  if (h.length < 7) return [];
  return (reviews || []).filter((r) => r.branch === b.name && (r.commit || "").startsWith(h));
}


// reviewActiveIn reports whether st's review overlay still owns the screen:
// the file list (a commit review) or the comparison (a range review) on
// screen must be the very object the review opened. Every other open
// replaces that object, so the overlay lapses without each opener having to
// know about reviews.
function reviewActiveIn(st) {
  const rv = st.review;
  if (!rv || st.layout === "list") return false;
  return rv.cmp ? st.filesMode === "compare" && st.compare === rv.cmp : st.filesMode === "commit" && st.files === rv.files;
}
// nextNotedFile is the index of the next (dir 1) or previous (dir -1) file
// after from that the review places notes on, or -1 when there is none that
// way (the TUI's stepReviewFile stays put at the ends). from -1 = the Overview.
function nextNotedFile(files, counts, from, dir) {
  for (let i = from + dir; i >= 0 && i < files.length; i += dir) {
    if ((counts[files[i].path] || 0) > 0) return i;
  }
  return -1;
}


// reviewShownOn says whether what was created on reviewBranch shows while the
// reader is on the viewing branches (domain.ReviewShownOn): on that branch
// and no other — not on the target it was merged into. Unknown on either side
// (no branch recorded, a detached HEAD) shows. Names match exactly, or as a
// branch and its remote-tracking spelling (feat ~ origin/feat).
function reviewShownOn(reviewBranch, viewing) {
  if (!reviewBranch || !viewing || !viewing.length) return true;
  return viewing.some((v) => v === reviewBranch || reviewBranch.endsWith("/" + v) || v.endsWith("/" + reviewBranch));
}

// shownScopes are the range reviews a COMMIT shows on the viewing branches
// (domain.NoteCounts.ScopesShownOn): the commit pairs written on one of them.
// A merge preview's review is never among them — it shows in its preview.
function shownScopes(scopes, viewing) {
  return (scopes || []).filter((sc) => !sc.preview && reviewShownOn(sc.branch, viewing));
}

// reviewMarkTitle is the tooltip of a commit row's ✎ — "" for a commit that
// earns no mark. A commit is REVIEWED when it has a stored AI review, or when
// it holds a range review (notes written over a commit pair, which
// /api/notes/counts lists per commit: scopes_by_commit) — created on the
// branch being viewed. The TUI's commitReviewed; hasReview is already the
// branch-filtered answer.
function reviewMarkTitle(hash, hasReview, scopes, viewing) {
  const ranges = shownScopes((scopes || {})[hash], viewing).length;
  if (hasReview && ranges) return "has an AI review and a range review — open the commit to read them";
  if (hasReview) return "has an AI review — open the commit to read it";
  if (ranges) return "has a range review — open the commit to read it";
  return "";
}

// notedElsewherePaths lists, sorted, the paths with PLAIN notes at commit sha
// that the commit does not change (the TUI's notedElsewhere): they have no
// file row to carry their ◆N, so the commit lists them under "Notes". plain
// is /api/notes/counts' plain_by_commit_path ("<sha>:<path>" → threads) —
// a range review's notes are its own row, never loose paths here.
function notedElsewherePaths(plain, sha, files) {
  if (!sha) return [];
  const changed = new Set((files || []).map((f) => f.path));
  const pre = sha + ":";
  return Object.keys(plain || {})
    .filter((k) => k.startsWith(pre) && plain[k] > 0 && !changed.has(k.slice(pre.length)))
    .map((k) => k.slice(pre.length))
    .sort();
}

// --- end reviews pure ---


// reviewActive is reviewActiveIn over the page's state: is a review view on
// screen right now?
function reviewActive() {
  return reviewActiveIn(state);
}


// viewBranches are the branches the reader is ON: the one the commit list is
// narrowed to (solo), else the checked-out one. [] when neither is known.
function viewBranches() {
  if (state.solo) return [state.solo];
  const b = state.repo && state.repo.branch;
  return b ? [b] : [];
}


// commitReviewList is the open commit's AI reviews as the viewed branch sees
// them (a branch's review shows on that branch only); [] off a commit's list.
function commitReviewList() {
  const cr = state.commitReviews;
  if (state.filesMode !== "commit" || !cr || cr.sha !== state.fileSha) return [];
  const view = viewBranches();
  return cr.list.filter((r) => reviewShownOn(r.branch, view));
}


// commitScopes is the open commit's range reviews as the viewed branch sees
// them: the commit pairs its notes were written in — [{scope, label, n}].
// Such notes all sit on the range's newest commit, mostly on files it does not
// change, so the commit lists the review as ONE row instead of those files.
function commitScopes() {
  if (state.filesMode !== "commit" || !state.fileSha) return [];
  return shownScopes((state.noteCounts.scopes_by_commit || {})[state.fileSha], viewBranches());
}

// A Range review row shares the Reviews rows' cursor (state.reviewSel), under
// an id no review can have.
const scopeSel = (scope) => "scope:" + scope;


// commitNoted is the open commit's Notes rows: the paths with plain notes at
// this commit that it does not change.
function commitNoted() {
  if (state.filesMode !== "commit") return [];
  return notedElsewherePaths(state.noteCounts.plain_by_commit_path, state.fileSha, state.files);
}

const notedSel = (path) => "noted:" + path;


// headRowIds are the rows above a commit's first file, top to bottom.
function headRowIds() {
  return commitReviewList()
    .map((r) => r.id)
    .concat(commitScopes().map((sc) => scopeSel(sc.scope)))
    .concat(commitNoted().map(notedSel));
}


// reviewRowsHTML is a commit's "Reviews" and "Range reviews" sections, in
// front of its files — "" when the commit on screen has neither, so such a
// list renders as before. The rows carry data-review / data-scope and no
// data-i: they are not files, so the cursor, staging, the stack and every
// file action pass them by.
function reviewRowsHTML() {
  const revs = commitReviewList();
  const scopes = commitScopes();
  return (
    (revs.length
      ? `<li class="sect">Reviews</li>` +
        revs
          .map((r) => `<li class="rev${state.reviewSel === r.id ? " sel" : ""}" data-review="${esc(r.id)}" title="${esc(r.summary || "")}">${esc(reviewRowText(r))}</li>`)
          .join("")
      : "") +
    (scopes.length
      ? `<li class="sect">Range reviews</li>` +
        scopes
          .map(
            (sc) =>
              `<li class="rev${state.reviewSel === scopeSel(sc.scope) ? " sel" : ""}" data-scope="${esc(sc.scope)}" ` +
              `title="open the range these notes were written in">${esc(sc.label)}${noteBadgeHTML(sc.n)}</li>`
          )
          .join("")
      : "") +
    notedRowsHTML()
  );
}


// notedRowsHTML is the commit's "Notes" section: one row per path with plain
// notes here that the commit does not change. Not files (no data-i): the
// notes are read in the notes window, there being no diff of the file here.
function notedRowsHTML() {
  const paths = commitNoted();
  if (!paths.length) return "";
  const plain = state.noteCounts.plain_by_commit_path;
  const cols = fileCols(NOTE_BADGE_COLS);
  return (
    `<li class="sect">Notes</li>` +
    paths
      .map(
        (p) =>
          `<li class="rev${state.reviewSel === notedSel(p) ? " sel" : ""}" data-noted="${esc(p)}">` +
          filePathHTML(p, cols) +
          noteBadgeHTML(plain[state.fileSha + ":" + p]) +
          `</li>`
      )
      .join("")
  );
}


// openNotedPath reads a Notes row's notes: the plain notes on path at the
// commit on screen (/api/notes leaves range reviews' notes to their reviews).
async function openNotedPath(path) {
  const sha = state.fileSha;
  if (state.filesMode !== "commit" || !sha) return;
  let d;
  try {
    d = await getJSON("/api/notes?" + new URLSearchParams({ path, rev: sha, state: "commit" }));
  } catch (e) {
    opLine("note: " + (e.message || e), true);
    return;
  }
  if (state.fileSha !== sha) return; // the reader moved on
  openNotesWindow(path + " @ " + sha.slice(0, 7), d.notes || []);
}


// stepCommitReviews moves the file cursor through a commit's head rows, which
// sit above its first file: k on the first file enters them, j on the last
// leaves them for the first file. true = the key was the rows'.
function stepCommitReviews(delta) {
  const ids = headRowIds();
  if (!ids.length) return false;
  const at = ids.indexOf(state.reviewSel);
  if (at < 0) {
    if (delta >= 0 || state.fileCursor !== 0) return false;
    state.reviewSel = ids[ids.length - 1];
  } else if (at + delta >= ids.length) {
    state.reviewSel = ""; // onto the first file
    state.fileCursor = 0;
  } else {
    state.reviewSel = ids[Math.max(0, at + delta)];
  }
  renderFiles();
  return true;
}


// openSelectedReview is enter on a selected head row; false when none is.
function openSelectedReview() {
  if (!state.reviewSel || !headRowIds().includes(state.reviewSel)) return false;
  const sc = commitScopes().find((x) => scopeSel(x.scope) === state.reviewSel);
  const noted = commitNoted().find((p) => notedSel(p) === state.reviewSel);
  if (sc) openRangeReview(sc.scope);
  else if (noted) openNotedPath(noted);
  else openReview(state.reviewSel, reviewBackFromCommit(state.reviewSel));
  return true;
}


// openScopeRange puts a range review on screen: the range its notes were
// written in, frozen at commit (/api/scope-range), as a pair landing narrowed
// to the review's own notes — every file of the range with its ◆N, the notes
// on their lines. still() says whether the reader is where they asked from
// once the range is resolved. It answers the range ({a, b, label}), or null
// when it could not be opened (the op line says why).
async function openScopeRange(commit, scope, still) {
  let d;
  try {
    d = await getJSON("/api/scope-range?" + new URLSearchParams({ commit, scope }));
  } catch (e) {
    opLine("range review: " + (e.message || e), true);
    return null;
  }
  if (still && !still()) return null; // the reader moved on
  if (!(await runLinkCompare("a=" + d.a + "&b=" + d.b))) return null;
  const c = state.compare;
  if (!c || !c.pair || c.pair.a !== d.a || c.pair.b !== d.b) return null; // superseded
  c.pair.scope = scope; // the review under its own name (pairNoteCtx)
  state.previewCounts = null; // the landing's counts were read under the pair's name
  loadPairCounts();
  $("files-title").textContent = "Range review: " + d.label;
  return d;
}


// openRangeReview opens a Range review row of the commit on screen; esc from
// the range's file list comes back to the commit (leaveRangeReview).
async function openRangeReview(scope) {
  const back = reviewBackFromCommit(scopeSel(scope));
  if (state.filesMode !== "commit" || !back.sha) return;
  const d = await openScopeRange(back.sha, scope, () => state.filesMode === "commit" && state.fileSha === back.sha);
  if (d) state.compare.back = back;
}


// leaveRangeReview is esc on a range opened from a commit's Range review row:
// back to that commit's files, the cursor on the row. false when the screen
// is no such range.
function leaveRangeReview() {
  const back = state.filesMode === "compare" && state.compare && state.compare.back;
  if (!back) return false;
  goBack(back);
  return true;
}


// reviewMetaLine is the line under the review's title: agent · date · how
// many notes on how many files (the TUI's reviewMetaLine, with a date).
function reviewMetaLine(d) {
  return [d.agent, reviewStamp(d.created), d.structured ? `${d.notes} notes on ${d.note_files} files` : ""]
    .filter(Boolean)
    .join(" · ");
}


// setReviewHeader draws the review's title and meta line; the files stage
// clears both on every entry (enterFilesStage), so esc from a diff redraws.
function setReviewHeader() {
  const d = state.review.data;
  setFilesKind("review", "a stored AI review — its notes are read-only");
  $("files-title").textContent = "Review " + d.label;
  setFilesMeta(reviewMetaLine(d));
}


// openReview opens review id as the review view. back says where esc from
// its file list returns: {kind: "commit", sha, short, subject, reviewId} (a
// commit's Reviews row), {kind: "popup", run} (View all notes: run reopens
// it) or {kind: "list"}.
async function openReview(id, back) {
  const gen = ++state.detailGen; // a newer open or esc supersedes this one
  state.review = null;
  state.reviewSel = "";
  state.filesMode = "commit";
  state.fileSha = null;
  state.files = [];
  enterFilesStage();
  $("files-title").textContent = "Review";
  renderFiles();
  $("files-list").innerHTML = `<li class="sect">Opening the review…</li>`;
  focusPane();
  let d;
  try {
    d = await getJSON("/api/review/" + encodeURIComponent(id));
  } catch (e) {
    if (gen !== state.detailGen) return;
    opLine(e.status === 404 ? "the review is gone" : "review: " + (e.message || e), true);
    return goBack(back);
  }
  if (gen !== state.detailGen) return;
  const files = d.files || [];
  let cmp = null;
  if (d.range) {
    // A range review is the compare of its range: the diffs, the stack and
    // the counts read base..tip through the compare lane unchanged.
    const a = d.base.slice(0, 7), b = d.tip.slice(0, 7);
    cmp = { a, b, aHash: d.base, bHash: d.tip, all: files, filter: "all", previewBar: "reviewed range " + a + ".." + b };
    state.compare = cmp;
    state.filesMode = "compare";
  } else {
    state.filesMode = "commit";
    state.fileSha = d.tip;
  }
  state.files = files;
  state.fileCursor = 0;
  state.review = { id, data: d, files, cmp, onOverview: true, back: back || { kind: "list" } };
  setReviewHeader();
  showReviewOverview();
}


// renderReviewFiles is the review view's list: ≡ Overview, then the files —
// ◆n on each the review notes, and that file's summary (dim, not a file)
// under it.
function renderReviewFiles() {
  renderCompareBar();
  const rv = state.review;
  const d = rv.data;
  const counts = d.counts || {};
  const anyBadge = state.files.some((f) => counts[f.path] > 0);
  const cols = fileCols(anyBadge ? NOTE_BADGE_COLS : 0);
  let html = `<li class="rov${rv.onOverview ? " sel" : ""}" data-ov="1" title="the review's summary, meta and the notes it could not place on a line">≡ Overview</li>`;
  state.files.forEach((f, i) => {
    html +=
      `<li class="${!rv.onOverview && i === state.fileCursor ? "sel" : ""}" data-i="${i}">` +
      `<span class="st ${esc(f.status)}">${esc(f.status)}</span>` +
      filePathHTML(f.path, cols) +
      noteBadgeHTML(counts[f.path]) +
      `</li>`;
    const sum = (d.summaries || {})[f.path];
    if (sum) html += `<li class="rsum" title="${esc(sum)}">${esc(sum)}</li>`;
  });
  $("files-list").innerHTML = html;
}


// reviewOverviewHTML is the Overview: the review's markdown, its meta, the
// notes it could not place on a line, and Copy — a centred reading column.
function reviewOverviewHTML() {
  const d = state.review.data;
  const other = d.other || [];
  return (
    `<div class="review-ov">` +
    `<div class="review-ov-bar"><span class="meta">${esc(reviewMetaLine(d))}</span>` +
    `<button id="review-copy" title="copy the review's text">copy</button></div>` +
    (d.overviewMd ? `<div class="md">${mdHTML(d.overviewMd, esc)}</div>` : "") +
    (d.meta ? `<div class="review-ov-meta">${esc(d.meta)}</div>` : "") +
    (other.length
      ? `<h4>Other notes</h4><ul class="review-other">` +
        other.map((n) => `<li>${esc(n.path + ":" + n.line + " — " + n.summary)}</li>`).join("") +
        `</ul>`
      : "") +
    `</div>`
  );
}


// showReviewOverview shows the Overview: alone in the diff pane, or — with
// the stacked view on — as the first element of the stack, above the files
// (the TUI's stacked review view).
function showReviewOverview() {
  const rv = state.review;
  if (!rv) return;
  rv.onOverview = true;
  if (stackOn()) {
    if (state.layout !== "diff") {
      state.pane = "files";
      setLayout("diff");
      focusPane();
      setReviewHeader();
    }
    if (state.stack && state.stack.list === state.files) {
      $("diff-pane").scrollTop = 0; // the stack's top IS the Overview
      renderFiles();
      return;
    }
    return openStack(0); // buildStack lands on the Overview, not on file 0
  }
  teardownStack();
  state.detailGen++; // a file diff still loading must not land over the overview
  if (state.layout !== "diff") {
    state.pane = "files";
    setLayout("diff");
    focusPane();
    setReviewHeader(); // setLayout may have cleared nothing, but the title must say where we are
  }
  state.diffCtx = null;
  state.diffRow = state.diffRange = null;
  state.notes = [];
  state.lastDiff = null;
  setDiffTitle("≡ Overview");
  $("diff-body").innerHTML = reviewOverviewHTML();
  renderFiles();
  updateDiffNav();
}


// goBack returns to where a review was opened from. A popup (View all
// notes) gets the commit list back under it, then reopens itself.
function goBack(back) {
  if (back && back.kind === "popup") {
    state.detailGen++;
    state.pane = "commits";
    setLayout("list");
    focusPane();
    back.run();
    return;
  }
  if (back && back.kind === "commit" && back.sha) {
    state.reviewSel = back.reviewId || "";
    openCommitByHash(back.sha, back.subject || "").then((ok) => {
      if (ok) setCommitTitle(back.sha, back.short || "", back.subject || "");
    });
    return;
  }
  state.detailGen++;
  state.pane = "commits";
  setLayout("list");
  focusPane();
}


// leaveReview is esc from the review view's file list.
function leaveReview() {
  const back = state.review ? state.review.back : null;
  state.review = null;
  goBack(back);
}


// reviewBackFromCommit is where esc returns for a review opened from the
// commit on screen.
function reviewBackFromCommit(reviewId) {
  const t = $("files-title");
  return { kind: "commit", sha: state.fileSha, short: t.dataset.short || "", subject: t.dataset.subject || "", reviewId };
}


// reviewHead finds a review's head in what the page knows (the open commit's
// rows, the counts), for the confirm's wording.
function reviewHead(id) {
  const lists = [(state.commitReviews && state.commitReviews.list) || [], (state.noteCounts && state.noteCounts.reviews) || []];
  for (const l of lists) {
    const r = l.find((x) => x.id === id);
    if (r) return r;
  }
  const d = state.review && state.review.id === id ? state.review.data : null;
  return d ? { id, agent: d.agent, created: d.created } : { id };
}


// reviewMenu is the right-click menu of a review row (a commit's, a branch's
// sub-row, the review view's Overview).
// reviewMenu is a review row's right-click menu: a review is opened or
// removed, nothing else (the TUI's noteRowMenu). open is what a click on the
// row does; the open review's own Overview row passes none.
function reviewMenu(id, x, y, open) {
  const rows = open ? [{ label: "Open review", act: open }] : [];
  rows.push({ label: "Copy gg link", act: () => copyServerLink("/api/review/" + encodeURIComponent(id) + "/link", "review: " + id) });
  rows.push({ label: "Delete review", danger: true, act: () => confirmDeleteReview(id) });
  showCtxMenu(rows, x, y);
}


// copyServerLink copies a link the server builds (a review's needs its
// revs; a note row's the domain builder the TUI shares) and records it like
// every copy (copyLink → /api/linkhist).
function copyServerLink(url, desc) {
  getJSON(url)
    .then((d) => copyLink(d.link, desc))
    .catch((e) => opLine("copy link: " + (e.message || e), true));
}


// scopeRowMenu and notedRowMenu are a commit's Range review and Notes rows'
// right-click menus: Open (what a click does) and Delete (every note the row
// stands for at this commit).
function scopeRowMenu(scope, x, y) {
  const sc = commitScopes().find((c) => c.scope === scope);
  const n = sc ? sc.n : 0;
  const what = n === 1 ? "Delete this range review's note?" : `Delete this range review's ${n} notes?`;
  showCtxMenu(
    [
      { label: "Open range review", act: () => openRangeReview(scope) },
      { label: "Copy gg link", act: () => copyServerLink("/api/notes/row-link?" + new URLSearchParams({ commit: state.fileSha, scope }), "range review: " + scope) },
      { label: "Delete range review", danger: true, act: () => confirmClearRow({ scope }, what + " " + (sc ? sc.label : scope), scopeSel(scope)) },
    ],
    x,
    y
  );
}


function notedRowMenu(path, x, y) {
  const n = state.noteCounts.plain_by_commit_path[state.fileSha + ":" + path] || 0;
  const what = n === 1 ? "Delete the note on this file?" : `Delete the ${n} notes on this file?`;
  showCtxMenu(
    [
      { label: "Open notes", act: () => openNotedPath(path) },
      { label: "Copy gg link", act: () => copyServerLink("/api/notes/row-link?" + new URLSearchParams({ commit: state.fileSha, path }), "file: " + path) },
      { label: "Delete notes", danger: true, act: () => confirmClearRow({ path }, what + " " + path, notedSel(path)) },
    ],
    x,
    y
  );
}


// confirmClearRow asks first — cancel is listed first and is what esc
// answers — then removes the row's notes at the commit on screen; the counts
// refresh redraws the list without the row.
function confirmClearRow(key, prompt, sel) {
  const sha = state.fileSha;
  showLocalConfirm(prompt, ["cancel", "delete"], (o) => {
    if (o !== "delete" || !sha) return;
    const run = runOnce("note-row-clear", async () => {
      const r = await postJSON("/api/notes/clear-row", { commit: sha, ...key });
      if (state.reviewSel === sel) state.reviewSel = "";
      await refreshNoteCounts();
      opLine(r.removed === 1 ? "deleted the note" : `deleted ${r.removed} notes`, false);
    });
    if (!run) return;
    run.catch((e) => opLine("delete notes: " + (e.message || e), true));
  });
}


// confirmDeleteReview asks first — cancel is listed first and is what esc
// answers — then removes the review's note.
function confirmDeleteReview(id) {
  const r = reviewHead(id);
  const who = [r.agent, reviewStamp(r.created)].filter(Boolean).join(", ");
  showLocalConfirm("Delete review" + (who ? " by " + who : "") + "?", ["cancel", "delete"], (o) => {
    if (o === "delete") deleteReview(id);
  });
}


function deleteReview(id) {
  const run = runOnce("review-delete", async () => {
    try {
      await postJSON("/api/notes/remove", { id });
    } catch (e) {
      if (e.status !== 404) throw e; // already gone elsewhere: the same outcome
    }
    const cr = state.commitReviews;
    if (cr) cr.list = cr.list.filter((r) => r.id !== id);
    if (state.reviewSel === id) state.reviewSel = "";
    const open = state.review && state.review.id === id && reviewActive();
    await refreshNoteCounts();
    if (open) leaveReview();
    opLine("review deleted", false);
  });
  if (!run) return;
  run.catch((e) => opLine("delete review: " + (e.message || e), true));
}


// stepReviewFile is `,` / `.` on a review's file list (the TUI's p / n there):
// the cursor goes to the previous / next file with notes; it does not open it.
function stepReviewFile(dir) {
  const rv = state.review;
  const from = rv.onOverview ? -1 : state.fileCursor;
  const i = nextNotedFile(state.files, (rv.data && rv.data.counts) || {}, from, dir);
  if (i < 0) return;
  rv.onOverview = false;
  state.fileCursor = i;
  renderFiles();
}


registerHelp({
  key: "reviews",
  html:
    "an AI review is stored with the commit it reviewed: a commit's reviews head its file list under " +
    "<b>Reviews</b>, and a branch's reviews of its current tip sit under its row. Click one to open the review " +
    "— <b>≡ Overview</b> (the summary, meta and notes it could not place), then the files, ◆N on each the review " +
    "notes, with the review's notes in the diffs, read-only. On the review's file list <b>,</b> / <b>.</b> move to " +
    "the previous / next file the review notes. esc goes back; right-click a review row or the " +
    "Overview for <b>Delete review</b>. A <b>range review</b> — notes written over a commit pair — is stored on the " +
    "pair's newer commit: it carries <b>✎</b> in the commit list like an AI-reviewed one, and lists them as one row under <b>Range reviews</b> (◆N) — click it to " +
    "open the range, every file with its notes; esc returns to the commit. What was created on a branch shows on " +
    "that branch only: on another branch — the one it was merged into included — a range review and a branch's AI " +
    "review leave no ✎ and no row, and View all notes leaves them out (nothing is deleted; solo the branch to see " +
    "them). A merge preview's review is the preview's: it shows in the preview, never on a commit. Notes written " +
    "outside any review on a file the commit does not change are listed under <b>Notes</b>; click one to read them",
});


export { notedRowMenu, scopeRowMenu, reviewShownOn, viewBranches, openNotedPath, openScopeRange, reviewMarkTitle, leaveRangeReview, openRangeReview, nextNotedFile, stepReviewFile, reviewOverviewHTML, branchReviewText, branchReviews, confirmDeleteReview, leaveReview, openReview, openSelectedReview, stepCommitReviews, renderReviewFiles, reviewActive, reviewBackFromCommit, reviewMenu, reviewRowsHTML, setReviewHeader, showReviewOverview };

$("diff-body").addEventListener("click", (e) => {
  if (e.target.id !== "review-copy" || !state.review) return;
  copyText(state.review.data.text || "", "the review");
});
