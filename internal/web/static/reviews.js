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
import { NOTE_BADGE_COLS, enterFilesStage, fileCols, filePathHTML, noteBadgeHTML, refreshNoteCounts, renderCompareBar, renderFiles, setCommitTitle, setDiffTitle, setFilesKind, setFilesMeta, setLayout, updateDiffNav } from "./files.js";
import { openStack, stackOn, teardownStack } from "./stackview.js";
import { openCommitByHash } from "./commits.js";
import { focusPane } from "./keys.js";

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

// branchReviewText is a branch's review sub-row: "└ Review: <stamp> <agent>".
function branchReviewText(r) {
  return "└ Review: " + reviewWords(r);
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
// --- end reviews pure ---


// reviewActive is reviewActiveIn over the page's state: is a review view on
// screen right now?
function reviewActive() {
  return reviewActiveIn(state);
}


// reviewRowsHTML is a commit's "Reviews" section, in front of its files — ""
// when the commit on screen has none, so such a list renders as before. The
// rows carry data-review and no data-i: they are not files, so the cursor,
// staging, the stack and every file action pass them by.
function reviewRowsHTML() {
  const cr = state.commitReviews;
  if (state.filesMode !== "commit" || !cr || cr.sha !== state.fileSha || !cr.list.length) return "";
  return (
    `<li class="sect">Reviews</li>` +
    cr.list
      .map((r) => `<li class="rev${state.reviewSel === r.id ? " sel" : ""}" data-review="${esc(r.id)}" title="${esc(r.summary || "")}">${esc(reviewRowText(r))}</li>`)
      .join("")
  );
}


// stepCommitReviews moves the file cursor through a commit's Reviews rows,
// which sit above its first file: k on the first file enters them, j on the
// last leaves them for the first file. true = the key was the rows'.
function stepCommitReviews(delta) {
  const cr = state.commitReviews;
  if (state.filesMode !== "commit" || !cr || cr.sha !== state.fileSha || !cr.list.length) return false;
  const at = cr.list.findIndex((r) => r.id === state.reviewSel);
  if (at < 0) {
    if (delta >= 0 || state.fileCursor !== 0) return false;
    state.reviewSel = cr.list[cr.list.length - 1].id;
  } else if (at + delta >= cr.list.length) {
    state.reviewSel = ""; // onto the first file
    state.fileCursor = 0;
  } else {
    state.reviewSel = cr.list[Math.max(0, at + delta)].id;
  }
  renderFiles();
  return true;
}


// openSelectedReview is enter on a selected Reviews row; false when none is.
function openSelectedReview() {
  const cr = state.commitReviews;
  if (!state.reviewSel || state.filesMode !== "commit" || !cr || cr.sha !== state.fileSha) return false;
  if (!cr.list.some((r) => r.id === state.reviewSel)) return false;
  openReview(state.reviewSel, reviewBackFromCommit(state.reviewSel));
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
  state.diffRow = null;
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
function reviewMenu(id, x, y) {
  showCtxMenu([{ label: "Delete review", danger: true, act: () => confirmDeleteReview(id) }], x, y);
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


registerHelp({
  key: "reviews",
  html:
    "an AI review is stored with the commit it reviewed: a commit's reviews head its file list under " +
    "<b>Reviews</b>, and a branch's reviews of its current tip sit under its row. Click one to open the review " +
    "— <b>≡ Overview</b> (the summary, meta and notes it could not place), then the files, ◆N on each the review " +
    "notes, with the review's notes in the diffs, read-only. esc goes back; right-click a review row or the " +
    "Overview for <b>Delete review</b>",
});


export { reviewOverviewHTML, branchReviewText, branchReviews, confirmDeleteReview, leaveReview, openReview, openSelectedReview, stepCommitReviews, renderReviewFiles, reviewActive, reviewBackFromCommit, reviewMenu, reviewRowsHTML, setReviewHeader, showReviewOverview };

$("diff-body").addEventListener("click", (e) => {
  if (e.target.id !== "review-copy" || !state.review) return;
  copyText(state.review.data.text || "", "the review");
});
