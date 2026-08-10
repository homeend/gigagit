// filehist.js — part of gg's web client. Split from the original app.js;
// see app.js (the entry module) for the load order.
import { $, esc, getJSON } from "./core.js";
import { closeLayer, pushLayer } from "./layers.js";
import { opLine } from "./ops.js";
import { versionWhen } from "./versions.js";
import { rev } from "./review.js";
import { openCommitByHash } from "./commits.js";
import { diffHTML } from "./files.js";

// --- file history overlay ----------------------------------------------------
// A layer, not a layout mode: esc drops you exactly where you were. Gen-guarded
// like every async open (a slow filelog racing a close must not resurrect it).
let hist = null; // {path, rev, rows, sel, gen}

let histGen = 0;


async function openFileHistory(path, rev) {
  const gen = ++histGen;
  hist = { path, rev: rev || "", rows: [], sel: 0, gen };
  $("history-title").textContent = "history — " + path + (rev ? " @ " + rev.slice(0, 8) : "");
  $("history-list").innerHTML = `<li class="empty">loading…</li>`;
  $("history-diff").innerHTML = "";
  pushLayer("history", $("history"), { onKey: historyKey });
  let body;
  try {
    body = await getJSON(
      "/api/filelog?path=" + encodeURIComponent(path) + "&rev=" + encodeURIComponent(rev || "")
    );
  } catch (e) {
    if (hist && hist.gen === gen)
      $("history-list").innerHTML = `<li class="empty">error: ${esc(e.message || e)}</li>`;
    return;
  }
  if (!hist || hist.gen !== gen) return; // closed or superseded meanwhile
  hist.rows = body.rows || [];
  if (!hist.rows.length) {
    $("history-list").innerHTML = `<li class="empty">(no history)</li>`;
    return;
  }
  renderHistoryList();
  openHistoryDiff(0);
}


function closeHistory() {
  hist = null;
  closeLayer("history");
}


function historyKey(e) {
  if (e.key === "Escape") {
    closeHistory();
    return true;
  }
  if (["j", "ArrowDown", "k", "ArrowUp"].includes(e.key)) {
    if (hist && hist.rows.length) {
      const d = e.key === "j" || e.key === "ArrowDown" ? 1 : -1;
      openHistoryDiff(Math.max(0, Math.min(hist.rows.length - 1, hist.sel + d)));
    }
    e.preventDefault();
    return true;
  }
  return true; // the overlay owns the keyboard entirely while open
}


function renderHistoryList() {
  $("history-list").innerHTML = hist.rows
    .map(
      (r, i) =>
        `<li data-i="${i}" class="${i === hist.sel ? "sel" : ""}">` +
        `<button class="hshow" data-i="${i}">show</button>` +
        `<span class="hsubj"><span class="st ${esc(r.status)}">${esc(r.status)}</span> ${esc(r.subject)}</span>` +
        `<span class="hmeta">${esc(r.short)} · ${esc(r.author)} · ${versionWhen(r.time)}</span></li>`
    )
    .join("");
  const sel = $("history-list").querySelector("li.sel");
  if (sel) sel.scrollIntoView({ block: "nearest" });
}


async function openHistoryDiff(i) {
  hist.sel = i;
  renderHistoryList();
  const r = hist.rows[i];
  const gen = hist.gen;
  // The /api/diff COMMIT form is already parent-vs-commit with A/D handling —
  // exactly "this file's change at this commit". path is the file's name AT
  // that commit (post-rename), old the parent-side name.
  const q = new URLSearchParams({ sha: r.hash, path: r.path || hist.path, status: r.status });
  if (r.old_path) q.set("old", r.old_path);
  $("history-diff").innerHTML = `<div class="notice">loading…</div>`;
  try {
    const d = await getJSON("/api/diff?" + q);
    // Stale-response guard: rapid j/k can land responses out of order, so a
    // slow response for a commit the selection has since moved past must not
    // clobber a newer diff already on screen — same overlay (gen) AND the
    // selection still sitting on the row this response is for (i).
    if (!hist || hist.gen !== gen || hist.sel !== i) return;
    $("history-diff").innerHTML = diffHTML(d, $("history-diff").clientWidth);
  } catch (e) {
    if (hist && hist.gen === gen && hist.sel === i)
      $("history-diff").innerHTML = `<div class="notice">error: ${esc(e.message || e)}</div>`;
  }
}


$("history-list").addEventListener("click", (e) => {
  if (!hist) return;
  const show = e.target.closest("button.hshow");
  if (show) {
    const r = hist.rows[Number(show.dataset.i)];
    closeHistory();
    openCommitByHash(r.hash, r.short + " " + r.subject);
    return;
  }
  const li = e.target.closest("li[data-i]");
  if (li) openHistoryDiff(Number(li.dataset.i));
});

$("history").addEventListener("click", (e) => {
  if (e.target.id === "history") closeHistory(); // backdrop closes, box does not
});


// --- blame overlay -----------------------------------------------------------
// Fetch-then-open: a blame failure (untracked path, binary) surfaces on the
// status line and the overlay never opens — nothing worse than an empty modal.
async function openFileBlame(path, rev) {
  let body;
  try {
    body = await getJSON(
      "/api/blame?path=" + encodeURIComponent(path) + "&rev=" + encodeURIComponent(rev || "")
    );
  } catch (e) {
    opLine("blame failed: " + (e.message || e), true);
    return;
  }
  $("blame-title").textContent = "blame — " + path + (rev ? " @ " + rev.slice(0, 8) : " (working tree)");
  const lines = body.lines || [];
  let html = "";
  let prev = null;
  for (const l of lines) {
    const first = l.hash !== prev;
    prev = l.hash;
    const gut = !first
      ? ""
      : l.hash
        ? `<span class="bsha" data-h="${esc(l.hash)}" title="${esc(l.summary)}">${esc(l.short)}</span> ${esc(l.author)} · ${versionWhen(l.time)}`
        : `<span class="bwork">working</span>`;
    html +=
      `<div class="bline${first ? " bfirst" : ""}">` +
      `<span class="bgut">${gut}</span>` +
      `<span class="bno">${l.line}</span>` +
      `<span class="btext">${esc(l.text) || " "}</span></div>`;
  }
  $("blame-body").innerHTML = html || `<div class="notice">(empty file)</div>`;
  pushLayer("blame", $("blame"), {}); // no onKey: the stack's default esc-closes applies
  $("blame-body").scrollTop = 0;
}


$("blame-body").addEventListener("click", (e) => {
  const sha = e.target.closest(".bsha");
  if (!sha) return;
  closeLayer("blame");
  openCommitByHash(sha.dataset.h, sha.dataset.h.slice(0, 8));
});

$("blame").addEventListener("click", (e) => {
  if (e.target.id === "blame") closeLayer("blame"); // backdrop closes, box does not
});

export { closeHistory, hist, histGen, historyKey, openFileBlame, openFileHistory, openHistoryDiff, renderHistoryList };
