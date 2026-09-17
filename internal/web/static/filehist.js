// filehist.js — part of gg's web client. Split from the original app.js;
// see app.js (the entry module) for the load order.
import { $, esc, getJSON, state } from "./core.js";
import { closeLayer, openPrompt, pushLayer } from "./layers.js";
import { opLine } from "./ops.js";
import { versionWhen } from "./versions.js";
import { rev } from "./review.js";
import { openCommitByHash } from "./commits.js";
import { cycleTextMode, diffHTML, mountPanBars, renderCell, toggleDiffView } from "./files.js";
import { registerHelp } from "./menus.js";

// --- file history overlay ----------------------------------------------------
// A layer, not a layout mode: esc drops you exactly where you were. Gen-guarded
// like every async open (a slow filelog racing a close must not resurrect it).
let hist = null; // {path, rev, rows, sel, gen}

let histGen = 0;


async function openFileHistory(path, rev) {
  const gen = ++histGen;
  hist = { path, rev: rev || "", rows: [], sel: 0, gen };
  setHistoryTitle();
  applyHistoryMax(false); // every open starts two-pane (the TUI's ctrl+t rule)
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


// setHistoryTitle writes the title span (never the div — the » chip lives
// beside it). With rows loaded it carries "n/N · subject" so the reader still
// knows which commit is open once the list is folded away in fullscreen.
function setHistoryTitle() {
  let t = "history — " + hist.path + (hist.rev ? " @ " + hist.rev.slice(0, 8) : "");
  if (hist.rows.length) {
    const r = hist.rows[hist.sel];
    t += " · " + (hist.sel + 1) + "/" + hist.rows.length + " · " + r.short + " " + r.subject;
  }
  $("history-title-text").textContent = t;
}


// applyHistoryMax puts the overlay in (or out of) fullscreen: the commit list
// folds away and the box takes the viewport. The diff is laid out for the
// host's width and the per-side scrollbars are (un)mounted from it, so it is
// redrawn after every flip. Transient like the TUI's ctrl+t — not persisted.
function applyHistoryMax(on) {
  $("history").classList.toggle("max", on);
  const b = $("history-max");
  b.textContent = on ? "«" : "»";
  b.title = on ? "back to the two-pane view — show the commit list (m)" : "fullscreen — hide the commit list, the diff takes the whole screen (m)";
  b.setAttribute("aria-expanded", on ? "false" : "true");
  renderHistoryDiff();
}


function toggleHistoryMax() {
  if (!hist) return;
  applyHistoryMax(!$("history").classList.contains("max"));
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
  if (e.key === "w" && !e.ctrlKey && !e.metaKey && !e.altKey) {
    cycleTextMode(); // the shared long-line mode; CSS on <body> redraws this overlay
    renderHistoryDiff(); // …and the per-side scrollbars are (un)mounted
    return true;
  }
  if (e.key === "m" && !e.ctrlKey && !e.metaKey && !e.altKey) {
    toggleHistoryMax();
    return true;
  }
  if (e.key === "f" && !e.ctrlKey && !e.metaKey && !e.altKey) {
    toggleDiffView(); // the shared preference flips; this overlay redraws itself
    if (state.diffPartial) hist.folds = new Set(); // on = a fresh entry: every run folded
    renderHistoryDiff();
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
  setHistoryTitle();
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
    hist.diff = d;
    hist.folds = new Set(); // a new revision starts fully folded (when the mode is on)
    renderHistoryDiff();
  } catch (e) {
    if (hist && hist.gen === gen && hist.sel === i)
      $("history-diff").innerHTML = `<div class="notice">error: ${esc(e.message || e)}</div>`;
  }
}


// renderHistoryDiff draws (or redraws) the selected revision's diff. The
// overlay follows the main pane's view mode (state.diffPartial) with its own
// fold set, so unfolding here never touches the diff behind it.
function renderHistoryDiff() {
  if (!hist || !hist.diff) return;
  const host = $("history-diff");
  host.innerHTML = diffHTML(hist.diff, host.clientWidth, false, hist.folds);
  const bars = document.createElement("div");
  bars.className = "hbars hidden";
  host.appendChild(bars); // inside the scroll container so `sticky; bottom: 0` pins it
  mountPanBars(host, bars);
}


$("history-diff").addEventListener("click", (e) => {
  const tr = e.target.closest("tr.fold[data-fold]");
  if (!tr || !hist || !hist.diff) return;
  hist.folds.add(Number(tr.dataset.fold));
  renderHistoryDiff();
  // The last run opened: the overlay shows the full file, so the shared
  // switch flips off (and the main pane behind it follows, as on any f).
  if (!$("history-diff").querySelector("tr.fold")) toggleDiffView(true);
});


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

$("history-max").addEventListener("click", toggleHistoryMax);


registerHelp({
  key: "file history · m fullscreen",
  html:
    "inside the file-history overlay, <b>m</b> (or the <b>»</b> chip in its title) folds the commit " +
    "list away and the box takes the whole screen, so the diff gets everything; <b>«</b> or <b>m</b> " +
    "again brings the list back. j/k still walk the commits meanwhile and the title reads " +
    "<b>n/N · subject</b> so you know which one is open. Every open starts two-pane",
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
  blameBase = "blame — " + path + (rev ? " @ " + rev.slice(0, 8) : " (working tree)");
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
    // Every row carries its author time (unix seconds) and an uncommitted
    // mark, so the recent-lines highlight can be re-applied without a refetch.
    html +=
      `<div class="bline${first ? " bfirst" : ""}" data-t="${Number(l.time) || 0}"${l.hash ? "" : ' data-u="1"'}>` +
      `<span class="bgut">${gut}</span>` +
      `<span class="bno">${l.line}</span>` +
      `<span class="btext">${renderCell(l.text, null, l.tok, "") || " "}</span></div>`;
  }
  $("blame-body").innerHTML = html || `<div class="notice">(empty file)</div>`;
  // Off on open: a fresh overlay never inherits the last one's tint; only
  // the last dialog text survives, to prefill the next d.
  state.blameRecent.on = false;
  applyBlameRecent();
  // w cycles the shared long-line mode, d/D drive the age highlight;
  // everything else (esc included) is left to the stack's default handling.
  pushLayer("blame", $("blame"), { onKey: blameKey });
  $("blame-body").scrollTop = 0;
}


// --- age highlight -----------------------------------------------------------
// d asks for an age filter and tints every line whose age falls inside it;
// D turns the tint off. The filter lives in state.blameRecent for the
// session only (not /api/uistate); opening blame always starts OFF — only
// the last dialog text survives, to prefill the next d. Uncommitted lines
// have age 0 (the newest change of all). `now` is the wall clock, NOT the
// blamed revision's date: blaming an old commit may highlight nothing, by
// design.

// --- time span (pure; guarded against Go) ---
// A port of internal/timespan: parseSpan(s) → whole minutes, or null when
// the text is not a span; formatSpan(mins) → the canonical "1d3h5m" badge;
// parseFilter(s) → {older, younger} (minutes, null for an absent half) or
// null; formatFilter(f) → the signed badge "+1d2h -7d4h"; filterMatches(f,
// ageMins) → the inclusive window test.
// Span grammar: tokens <digits><w|d|h|m> (either part optional, not both),
// optional whitespace, any order, summed; a bare number is days, a bare
// unit is one of it; m is MINUTES (not months); units are case-blind.
// Errors: empty input, junk, an unknown unit, junk after a unit ("1mo",
// "1.5d", "1x"), a zero total.
// Filter grammar: up to two signed halves, "-<span>" = younger than (age at
// most span), "+<span>" = older than (age at least span); a leading unsigned
// span is the "-" half; each sign owns every token up to the next sign
// (whitespace after the sign is fine). Errors: a span
// error inside a half, a second "-" or "+", a sign with nothing after it,
// an older bound above the younger one. Pinned to timespan.Table by
// timespanjs_test.go — change both sides or neither.
const SPAN_UNITS = { w: 7 * 24 * 60, d: 24 * 60, h: 60, m: 1 };

function parseSpan(s) {
  s = String(s == null ? "" : s).trim();
  if (s === "") return null;
  let total = 0;
  let i = 0;
  const isDigit = (c) => c >= "0" && c <= "9";
  const isWS = (c) => c === " " || c === "\t";
  while (i < s.length) {
    while (i < s.length && isWS(s[i])) i++;
    if (i >= s.length) break;
    let j = i;
    while (j < s.length && isDigit(s[j])) j++;
    const n = j > i ? Number(s.slice(i, j)) : 1; // a bare unit ("w") is one of it
    let unit = SPAN_UNITS.d; // a bare number is days
    if (j < s.length) {
      const u = SPAN_UNITS[s[j].toLowerCase()];
      if (u !== undefined) {
        unit = u;
        j++;
      }
    }
    if (j === i) return null; // neither a number nor a unit: junk
    // The token must end here: whitespace, another number, or the end.
    if (j < s.length && !isWS(s[j]) && !isDigit(s[j])) return null;
    // Go refuses a token past 1<<62 ns / unit; 76861433 is that bound in
    // minutes, and floor-of-floor matches Go's integer division per unit.
    if (n > Math.floor(76861433 / unit)) return null;
    total += n * unit;
    if (!Number.isSafeInteger(total)) return null;
    i = j;
  }
  if (total <= 0) return null;
  return total;
}

function formatSpan(mins) {
  let m = Math.max(0, Math.floor(Number(mins) || 0));
  const days = Math.floor(m / (24 * 60));
  m -= days * 24 * 60;
  const hours = Math.floor(m / 60);
  m -= hours * 60;
  let out = "";
  if (days > 0) out += days + "d";
  if (hours > 0) out += hours + "h";
  if (m > 0 || out === "") out += m + "m";
  return out;
}

// parseFilter splits the text at its signs: the run before the first sign
// (if any) is an unsigned "-" half; each sign then owns the run up to the
// next sign, and every run goes through parseSpan.
function parseFilter(s) {
  s = String(s == null ? "" : s).trim();
  if (s === "") return null;
  const f = { older: null, younger: null };
  const apply = (sign, text) => {
    const mins = parseSpan(text);
    if (mins === null) return false;
    const key = sign === "+" ? "older" : "younger";
    if (f[key] !== null) return false; // a second - or + half
    f[key] = mins;
    return true;
  };
  let sign = "-";
  let start = 0;
  let seenSign = false;
  for (let i = 0; i <= s.length; i++) {
    if (i < s.length && s[i] !== "+" && s[i] !== "-") continue;
    const seg = s.slice(start, i).trim();
    if (seg === "") {
      if (seenSign) return null; // a sign with nothing after it
    } else if (!apply(sign, seg)) {
      return null;
    }
    if (i < s.length) {
      sign = s[i];
      start = i + 1;
      seenSign = true;
    }
  }
  if (f.older !== null && f.younger !== null && f.older > f.younger) return null; // empty range
  return f;
}

// formatFilter prints the canonical signed form, older half first:
// "-7d", "+30d", "+1d2h -7d4h". Empty when neither half is set.
function formatFilter(f) {
  const parts = [];
  if (f && f.older !== null && f.older !== undefined) parts.push("+" + formatSpan(f.older));
  if (f && f.younger !== null && f.younger !== undefined) parts.push("-" + formatSpan(f.younger));
  return parts.join(" ");
}

// filterMatches reports whether an age (minutes, fractional allowed — the
// boundary is inclusive at full precision) falls inside the window. A
// negative age (clock skew) clamps to zero and behaves like an uncommitted
// line; a filter with no half set matches nothing.
function filterMatches(f, ageMins) {
  if (!f) return false;
  const hasOlder = f.older !== null && f.older !== undefined;
  const hasYounger = f.younger !== null && f.younger !== undefined;
  if (!hasOlder && !hasYounger) return false;
  const age = Math.max(0, Number(ageMins) || 0);
  if (hasOlder && age < f.older) return false;
  if (hasYounger && age > f.younger) return false;
  return true;
}
// --- end time span ---

const BLAME_RECENT_TITLE = "Highlight lines by age…";
const BLAME_RECENT_HINT = "-7d younger · +30d older · +1d -7d between · +w";

// blameBase is the overlay title without the age badge; applyBlameRecent
// rebuilds the title from it so toggling never stacks badges.
let blameBase = "";

function applyBlameRecent() {
  const br = state.blameRecent;
  const on = !!(br && br.on && br.f);
  const now = Date.now();
  for (const row of document.querySelectorAll("#blame-body .bline")) {
    const t = Number(row.dataset.t) || 0;
    // An uncommitted row is age 0: it matches a "-" half, never a "+" half.
    const age = row.dataset.u === "1" ? 0 : (now - t * 1000) / 60000;
    row.classList.toggle("brecent", on && filterMatches(br.f, age));
  }
  $("blame-title").textContent = blameBase + (on ? " · " + formatFilter(br.f) : "");
}

// openBlameRecentPrompt asks for the age filter; a bad answer re-opens the
// prompt prefilled with the offending text and the reason in the title, so
// the error reads above the field and the status line both (the overlay's
// backdrop dims the status line).
function openBlameRecentPrompt(value, err) {
  openPrompt({
    title: err ? BLAME_RECENT_TITLE + " — " + err : BLAME_RECENT_TITLE,
    value,
    placeholder: BLAME_RECENT_HINT,
    onSubmit: (text) => {
      const f = parseFilter(text);
      if (f === null) {
        const msg = "not an age filter: “" + text + "” (" + BLAME_RECENT_HINT + ")";
        opLine(msg, true);
        openBlameRecentPrompt(text, msg);
        return;
      }
      state.blameRecent = { on: true, f, last: text };
      applyBlameRecent();
    },
  });
}

registerHelp({
  key: "d / D · blame age highlight",
  html:
    "in the blame overlay, <b>d</b> asks for an age filter and tints every line whose commit falls inside it, " +
    "measured from now: <b>-7d</b> younger than a week, <b>+30d</b> older than a month, <b>+1d -7d</b> between the two " +
    "(both bounds inclusive; a bare span is the younger half, <b>+w</b> is a week, <b>1d 3h 5m</b> adds up, a bare number is days, m is minutes). " +
    "Uncommitted lines are age 0. <b>D</b> turns the tint off, <b>d</b> again edits the filter. " +
    "Off whenever blame opens; only the last text is kept to prefill the prompt — the TUI's d / D in its blame view",
});

function blameKey(e) {
  if (e.ctrlKey || e.metaKey || e.altKey) return false;
  if (e.key === "w") {
    cycleTextMode();
    return true;
  }
  if (e.key === "d") {
    e.preventDefault(); // the letter must never land in the prompt it opens
    openBlameRecentPrompt(state.blameRecent.last);
    return true;
  }
  if (e.key === "D") {
    e.preventDefault();
    state.blameRecent.on = false;
    applyBlameRecent();
    return true;
  }
  return false;
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

export { applyBlameRecent, closeHistory, filterMatches, formatFilter, formatSpan, hist, histGen, historyKey, openFileBlame, openFileHistory, openHistoryDiff, parseFilter, parseSpan, renderHistoryList };
