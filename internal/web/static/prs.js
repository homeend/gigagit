// prs.js — pull requests (read-only): the sidebar section, its rows and
// menu, and opening one as a PR diff on the compare screen a merge preview
// uses. Three rules (the server enforces the first two — prs.go):
//
//   - the page names a pull request by its NUMBER, never by a ref or a sha;
//   - a read never reaches the forge: the list is the server's cached lane,
//     re-read on the live "prs" event and by the header's ⟳;
//   - no usable forge → the section is never shown (it is born hidden and only
//     an "available" answer un-hides it).
//
// Like previews.js, this module does NOT import sidebar.js (files.js / ops.js
// already do; the edge would close a cycle), so it owns its own fetch.
import { $, esc, getJSON, postJSON, runOnce, state } from "./core.js";
import { copyText, showCtxMenu } from "./layers.js";
import { followOp, opBusy, opLine, showLocalConfirm } from "./ops.js";
import { extraRows, registerHelp } from "./menus.js";
import { loadPRCounts, openPreviewBody } from "./previews.js";
import { fetchNotes } from "./files.js";
import { prRowParts } from "./prsrow.js";
import { openPRDetails } from "./prdetails.js";

// While the server's first listing is still in flight the answer says
// loaded:false. The "prs" event normally brings the rows in, but a fast forge
// can emit before this page's event stream is up — so the first fetch also
// re-asks on a short backoff, then gives up quietly.
const BACKOFF_MS = [1000, 2000, 4000];
let backoffAt = 0;
let backoffTimer = null;

function take(body) {
  state.prs = body.prs || [];
  state.prsAvailable = !!body.available;
  state.prsError = body.error || "";
  $("prs-header").classList.toggle("hidden", !state.prsAvailable);
  $("prs-list").classList.toggle("hidden", !state.prsAvailable);
  $("pr-search-box").classList.toggle("hidden", !state.prsAvailable);
  bootSearch();
  $("prs-header").title = state.prsError ? "last refresh failed: " + state.prsError : "";
  renderPRs();
  if (!body.loaded && backoffAt < BACKOFF_MS.length && !backoffTimer) {
    backoffTimer = setTimeout(() => {
      backoffTimer = null;
      fetchPRs();
    }, BACKOFF_MS[backoffAt++]);
  }
}

// fetchPRs is single-flight (boot, the backoff and a live event can overlap),
// but a call that arrives mid-flight is not DROPPED: the answer in flight may
// predate what that call was about, so one more read runs after it.
let again = false;
export function fetchPRs() {
  const run = runOnce("prs", async () => {
    try {
      take(await getJSON("/api/pr"));
    } catch {
      // A failed read is not "the pull requests are gone": the rows stand.
    }
  });
  if (!run) {
    again = true;
    return Promise.resolve();
  }
  return run.then(() => {
    if (!again) return;
    again = false;
    return fetchPRs();
  });
}

function renderPRs() {
  const now = Date.now();
  const rows = state.prs || [];
  if (!rows.length) {
    $("prs-list").innerHTML = `<li class="none">no open pull requests</li>`;
    return;
  }
  $("prs-list").innerHTML = rows.map((pr) => prRowHTML(pr, now)).join("");
}

// prRowHTML is the one row painter: the list and the search results read alike.
function prRowHTML(pr, now) {
  const p = prRowParts(pr, now);
  return (
    `<li data-pr="${pr.number}" class="${(p.dim ? "prdim" : "") + (state.prBusy === pr.number ? " prbusy" : "")}" title="${esc(p.tip)}">` +
    `<span class="prnum">#${pr.number}</span>` +
    // The status cell LEADS the row: the sidebar is narrow and cuts a
    // row's tail, and the verdict is what you scan the list for.
    (p.mark ? `<span class="prmark ${esc(pr.review_state)}">${p.mark}</span>` : "") +
    (p.word ? `<span class="prword">${esc(p.word)}</span>` : "") +
    esc(p.title) +
    `</li>`
  );
}

// knownPR finds a pull request the page was shown — in the list, or among the
// search results (a closed PR found by searching is in no list until fetched).
function knownPR(n) {
  return (state.prs || []).find((p) => p.number === n) || ((state.prSearch && state.prSearch.prs) || []).find((p) => p.number === n);
}

// refreshPRs is the header's ⟳: the one page action that spends a forge call.
function refreshPRs() {
  const run = runOnce("prs-refresh", async () => {
    opLine("⟳ reading pull requests…");
    try {
      const body = await postJSON("/api/pr/refresh", {});
      take(body);
      if (body.error) opLine("pull requests: " + body.error, true);
      else opLine("pull requests: " + (body.prs || []).length + " listed");
    } catch (err) {
      opLine("pull requests: " + (err.message || err), true);
    }
  });
  if (run) run.catch(() => {});
}
window.__ggRefreshPRs = refreshPRs;

// --- the loading mask ---------------------------------------------------------
// Opening a pull request is seconds of network and git with nothing to look
// at, so the panes right of the sidebar are masked while it runs and the row
// spins. The mask never traps: a click dismisses it (the open carries on and
// lands when it lands), and it clears itself whatever way the open ends.
let maskTimer = null;
function maskOn(n, text) {
  const side = $("branches-pane").getBoundingClientRect();
  const panes = $("panes").getBoundingClientRect();
  const m = $("pr-mask");
  const left = side.width ? side.right : panes.left; // a hidden sidebar has no width
  m.style.left = left + "px";
  m.style.top = panes.top + "px";
  m.style.width = Math.max(0, panes.right - left) + "px";
  m.style.height = panes.height + "px";
  $("pr-mask-text").textContent = text;
  m.classList.remove("hidden");
  state.prBusy = n;
  renderPRs();
  renderSearch();
  clearTimeout(maskTimer);
  maskTimer = setTimeout(maskOff, 90000); // never outlive a wedged request
}
function maskOff() {
  clearTimeout(maskTimer);
  $("pr-mask").classList.add("hidden");
  if (state.prBusy) {
    state.prBusy = 0;
    renderPRs();
    renderSearch();
  }
}
$("pr-mask").addEventListener("click", maskOff);

function prLabel(n) {
  const pr = knownPR(n);
  return "pull request #" + n + (pr && pr.title ? " \u00b7 " + pr.title : "");
}

// showPR opens PR n's diff from the LOCAL head — no network. The server
// resolves the pair; "unfetched" means there is no local head yet.
async function showPR(n, moved) {
  let body;
  try {
    body = await getJSON("/api/pr/open?n=" + n);
  } catch (err) {
    opLine("pull request #" + n + ": " + (err.message || err), true);
    return "error";
  }
  if (body.state === "unfetched") return "unfetched";
  await openPreviewBody(body, "");
  if (moved) opLine("pull request #" + n + " updated: new commits on the forge");
  // The diff is up with whatever threads the server had cached; only NOW is
  // the forge asked for the PR's comments (never on the click path).
  refreshPRComments(n);
  return "shown";
}

// refreshPRComments re-reads PR n's review comments from the forge and, when
// they changed, redraws the threads of the diff on screen. It runs after a PR
// diff opens and on every `prs` live event; a failure is silent — the diff
// simply stays without (newer) threads.
export function refreshPRComments(n) {
  return runOnce("pr-comments", async () => {
    let r;
    try {
      r = await postJSON("/api/pr/comments/refresh?n=" + n, {});
    } catch {
      return;
    }
    const po = state.previewOpen;
    if (!r.changed || !po || po.pr !== n || state.filesMode !== "compare") return;
    const ctx = state.diffCtx;
    if (ctx && ctx.preview && ctx.preview.pr === n) await fetchNotes(); // also refreshes the ◆N badges
    else await loadPRCounts(n);
  });
}

// fetchPR runs the pr-fetch op and resolves true when the head arrived.
function fetchPR(n) {
  return new Promise((resolve) => {
    if (opBusy()) {
      opLine("pull request #" + n + ": another operation is running", true);
      resolve(false);
      return;
    }
    postJSON("/api/op", { op: "pr-fetch", number: n }).then(
      (resp) =>
        // onDone REPLACES the generic done handling: a PR fetch writes one
        // private ref, so nothing but this list needs a reload.
        followOp(resp.op_id, "fetching " + prLabel(n), "pr-fetch", (ev) => {
          fetchPRs();
          if (!ev.ok) opLine("error: " + (ev.error || "operation failed"), true);
          resolve(!!ev.ok);
        }),
      (err) => {
        opLine("pull request #" + n + ": " + (err.message || err), true);
        resolve(false);
      }
    );
  });
}

// revalidate is the background half of a cached open: the diff is already on
// screen (from the local head), and only now is the forge asked whether that
// head is still the PR's. A moved head is fetched and the diff re-opened — if
// the user is still looking at it.
async function revalidate(n) {
  let rv;
  try {
    rv = await postJSON("/api/pr/revalidate?n=" + n, {});
  } catch {
    return; // offline, rate-limited: the diff on screen stands
  }
  fetchPRs(); // the row may have changed state (merged, closed)
  if (!rv.moved) return;
  const po = state.previewOpen;
  if (!po || po.pr !== n) return; // they moved on; the next open fetches
  opLine("⟳ " + prLabel(n) + " has new commits — updating…");
  if (await fetchPR(n)) {
    const now = state.previewOpen;
    if (now && now.pr === n) await showPR(n, true);
  }
}

// openPR is the row click: serve what is here, then check the forge.
//   - a head that is already local opens AT ONCE (a purely local read), and
//     revalidate() updates it in the background when the forge moved on;
//   - otherwise the head is fetched first, under the mask.
// The server's PR cache makes the fetch itself cheap the second time: the
// head sha comes from the listing, so an unchanged PR skips the network.
let opening = 0;
async function openPR(pr) {
  const n = pr.number;
  if (opening) return; // one open at a time; the mask says which
  opening = n;
  maskOn(n, "opening " + prLabel(n) + "…");
  try {
    if (pr.fetched) {
      const how = await showPR(n, false);
      if (how === "shown") {
        maskOff();
        if (pr.state === "open") revalidate(n); // a closed PR's head no longer moves
        return;
      }
      if (how === "error") return;
    }
    $("pr-mask-text").textContent = "fetching " + prLabel(n) + "…";
    if (!(await fetchPR(n))) return;
    pr.fetched = true; // a second click on this row shows the diff without another fetch
    $("pr-mask-text").textContent = "computing the diff of " + prLabel(n) + "…";
    if ((await showPR(n, false)) === "unfetched") opLine("pull request #" + n + ": the head did not arrive", true);
  } finally {
    opening = 0;
    maskOff();
  }
}

function forgetPR(pr) {
  if (opBusy()) return;
  showLocalConfirm(
    "Forget pull request #" + pr.number + "? Its fetched head is dropped; nothing changes on the forge.",
    ["forget", "abort"],
    async (o) => {
      if (o !== "forget") return;
      let resp;
      try {
        resp = await postJSON("/api/op", { op: "pr-forget", number: pr.number });
      } catch (err) {
        opLine("pull request #" + pr.number + ": " + (err.message || err), true);
        return;
      }
      followOp(resp.op_id, "forgetting pull request #" + pr.number, "pr-forget", (ev) => {
        if (ev.ok) opLine(ev.summary || "forgot pull request #" + pr.number);
        else opLine("error: " + (ev.error || "operation failed"), true);
        fetchPRs();
      });
    }
  );
}

function showPRMenu(pr, x, y) {
  const items = [{ label: (pr.fetched ? "open" : "fetch and open") + " pull request #" + pr.number, act: () => openPR(pr) }];
  // The description, the conversation and the outdated threads: the parts of
  // a PR that have no place in its diff. Not for a row the forge has lost.
  if (pr.state !== "unavailable") items.push({ label: "details…", act: () => openPRDetails(pr.number) });
  if (pr.url) items.push({ label: "copy URL", act: () => copyText(pr.url, "pull request URL") });
  items.push(...extraRows("pr", pr));
  // Forget means something only where gg holds something: a fetched head, or
  // a closed/merged row it keeps listing.
  if (pr.fetched || pr.state !== "open") {
    items.push({ sep: true });
    items.push({ label: "forget…", danger: true, act: () => forgetPR(pr) });
  }
  showCtxMenu(items, x, y);
}

function rowPR(li) {
  const n = Number(li.dataset.pr);
  return (state.prs || []).find((p) => p.number === n);
}

$("prs-list").addEventListener("click", (ev) => {
  const li = ev.target.closest("li");
  const pr = li && li.dataset.pr ? rowPR(li) : null;
  if (pr) openPR(pr);
});

$("prs-list").addEventListener("contextmenu", (ev) => {
  const li = ev.target.closest("li");
  const pr = li && li.dataset.pr ? rowPR(li) : null;
  if (!pr) return;
  ev.preventDefault();
  showPRMenu(pr, ev.clientX, ev.clientY);
});

// Right-click on the open PR's header or bar: the PR's menu, not the browser's.
for (const id of ["files-header", "compare-bar"]) {
  $(id).addEventListener("contextmenu", (ev) => {
    const po = state.previewOpen;
    if (!po || !po.pr || state.filesMode !== "compare") return;
    const pr = knownPR(po.pr);
    if (!pr) return;
    ev.preventDefault();
    showPRMenu(pr, ev.clientX, ev.clientY);
  });
}

// --- search ---------------------------------------------------------------------
// The list holds the OPEN pull requests (plus the ones gg already knows). A
// closed or merged one that was never fetched is found by searching: the text
// goes to the forge's own search, the state chip narrows it, a bare number
// looks that PR up. The results are a transient set under the search row —
// opening one fetches its head, which is what makes it a row of the list.
// The search is the POST (it spends a forge call); the GET is the server's
// last answer, which a reloaded page shows again for free.
const SEARCH_STATES = ["all", "closed", "merged", "open"];
let searchBooted = false;

function takeSearch(body) {
  if (!body || !body.query) return;
  state.prSearch = { query: body.query, prs: body.prs || [], more: !!body.more, error: "", busy: false };
  $("pr-search").value = body.query.text || "";
  $("pr-search-state").textContent = body.query.state || "all";
  $("pr-search-results").classList.remove("hidden");
  renderSearch();
}

function renderSearch() {
  const sr = state.prSearch;
  if (!sr) return;
  const now = Date.now();
  let html = sr.prs.map((pr) => prRowHTML(pr, now)).join("");
  if (sr.busy) html = `<li class="none">searching…</li>`;
  else if (sr.error) html = `<li class="err">${esc(sr.error)}</li>` + html;
  else if (!sr.prs.length) html = `<li class="none">no pull requests match</li>`;
  if (!sr.busy && sr.more) html += `<li class="none">more results — narrow the search</li>`;
  $("pr-search-list").innerHTML = html;
  $("pr-search-count").textContent = sr.busy ? "searching" : "results \u00b7 " + sr.prs.length + (sr.more ? "+" : "");
}

// bootSearch shows the server's last answer once the section is known to
// exist. A GET: it never reaches the forge.
function bootSearch() {
  if (searchBooted || !state.prsAvailable) return;
  searchBooted = true;
  getJSON("/api/pr/search").then(takeSearch, () => {});
}

function runSearch() {
  const text = $("pr-search").value;
  const st = $("pr-search-state").textContent;
  const prev = state.prSearch || { prs: [], more: false };
  const run = runOnce("pr-search", async () => {
    state.prSearch = { query: { text, state: st }, prs: prev.prs, more: prev.more, error: "", busy: true };
    $("pr-search-results").classList.remove("hidden");
    renderSearch();
    try {
      takeSearch(await postJSON("/api/pr/search", { text, state: st }));
    } catch (err) {
      // A failed search keeps the rows that were standing, and says why.
      state.prSearch = { query: { text, state: st }, prs: prev.prs, more: prev.more, error: String(err.message || err), busy: false };
      renderSearch();
    }
  });
  if (run) run.catch(() => {});
}

// focusPRSearch is the page's A: unfold the section if it is folded, and put
// the caret in the search field. keys.js reaches it on the window (importing
// this module there would close a cycle).
function focusPRSearch() {
  if (!state.prsAvailable) return false;
  if ($("prs-list").classList.contains("collapsed")) $("prs-header").click();
  $("pr-search").focus();
  $("pr-search").select();
  return true;
}
window.__ggFocusPRSearch = focusPRSearch;

$("pr-search").addEventListener("keydown", (ev) => {
  if (ev.key === "Enter") {
    ev.preventDefault();
    runSearch();
  } else if (ev.key === "Escape") {
    ev.preventDefault();
    ev.stopPropagation();
    $("pr-search").blur();
  }
});

$("pr-search-state").addEventListener("click", () => {
  const b = $("pr-search-state");
  b.textContent = SEARCH_STATES[(SEARCH_STATES.indexOf(b.textContent) + 1) % SEARCH_STATES.length];
});

// ✕ only puts the results away; the server still has them for the next load.
$("pr-search-close").addEventListener("click", () => $("pr-search-results").classList.add("hidden"));

function searchRowPR(li) {
  const n = Number(li.dataset.pr);
  return ((state.prSearch && state.prSearch.prs) || []).find((p) => p.number === n);
}

$("pr-search-list").addEventListener("click", (ev) => {
  const li = ev.target.closest("li");
  const pr = li && li.dataset.pr ? searchRowPR(li) : null;
  if (pr) openPR(pr);
});

$("pr-search-list").addEventListener("contextmenu", (ev) => {
  const li = ev.target.closest("li");
  const pr = li && li.dataset.pr ? searchRowPR(li) : null;
  if (!pr) return;
  ev.preventDefault();
  showPRMenu(pr, ev.clientX, ev.clientY);
});

registerHelp({
  key: "pull requests",
  html:
    "with a usable <b>gh</b> the sidebar lists the repository's open pull requests (read-only — gg never " +
    "writes to the forge). The row leads with the review verdict: <b>✓</b> approved, <b>✗</b> changes " +
    "requested, <b>●</b> review required. <b>Click</b> fetches the head and opens the PR's diff on the merge " +
    "preview screen (a loading mask covers the panes meanwhile; a pull request opened before shows at once " +
    "and is checked against the forge in the background); <b>right-click</b> for copy URL and forget. A pull request gg already knows stays " +
    "listed, dimmed, after it is closed or merged. <b>A</b> (or a click) puts the caret in the section's " +
    "<b>search</b> field: it asks the forge for pull requests of ANY state — the way to a closed or merged " +
    "one gg never fetched. The text goes to the forge's own search (<b>author:name</b>, <b>head:branch</b>…), " +
    "the chip cycles all / closed / merged / open, a bare number opens that pull request, <b>Enter</b> " +
    "searches; the 50 newest-updated matches show under the field and act like list rows, and opening one " +
    "makes it a row of the list. The last search is shown again after a reload. The header's <b>⟳</b> re-reads the list; it also " +
    "re-reads itself every <b>[refresh] prs</b> seconds (300; 0 = off), whatever the auto-refresh switch says",
});
