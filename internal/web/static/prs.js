// prs.js — pull requests: the sidebar section, its rows and menu, and
// opening one as a PR diff on the compare screen a merge preview uses (sending
// to GitHub lives in prsend.js). Three rules (the server enforces the first two — prs.go):
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
import { prRowParts, ago } from "./prsrow.js";
import { openPRDetails } from "./prdetails.js";
import { nextFresh, oncePerKey, sentEvent, serialReads, stickyFlag } from "./prfresh.js";
import { onHeadMoved, onSendDone, sendToGitHub } from "./prsend.js";

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

// The open PR's freshness mark (prfresh.js): "refreshing…" while the forge
// is asked, "updated" when a refresh found something new (not on the first
// read, not for your own send), the cached copy's age while the forge cannot
// be reached, nothing otherwise. Only ever drawn for the PR on screen.
const lastRead = new Map(); // PR number → the read_at the server last reported
let fresh = { seen: 0, updated: 0, ownSend: null, text: "" };
let readSeq = 0; // every comments read's start, in order (prfresh.js's sequence)
// The open PR's forge reads (comments refresh, revalidate) run one at a time;
// a read asked while one runs waits for it (C9).
const reads = serialReads((fn) => runOnce("pr-comments", fn));
const soonComments = stickyFlag(reads);
function setPRFresh(n, text) {
  const po = state.previewOpen;
  $("pr-fresh").textContent = po && po.pr === n ? text : "";
}
function freshEvent(n, ev) {
  fresh = nextFresh(fresh, { n, ...ev });
  setPRFresh(n, fresh.text);
}
function offlineFresh(n) {
  freshEvent(n, { kind: "fail", age: ago(lastRead.get(n), Date.now()) });
}

// The interrupted-send bar (W6): a send gg started that GitHub still holds as
// a pending review — finish it, or (unless it is the user's own review gg
// added to) discard it. Mounted here, under the open PR's compare bar.
const ibar = document.createElement("div");
ibar.id = "pr-interrupted";
ibar.className = "hidden";
$("compare-bar").after(ibar);
const ibarCSS = document.createElement("style");
ibarCSS.textContent = `
#pr-interrupted { display: flex; flex-wrap: wrap; gap: 6px 10px; align-items: baseline; padding: 4px 10px; background: var(--attn-warn); border-bottom: 1px solid var(--border); }
#pr-interrupted.hidden { display: none; }
#pr-interrupted span { flex: 1 1 100%; }
#pr-interrupted button { white-space: nowrap; background: var(--bg); color: var(--fg); border: 1px solid var(--border); border-radius: 3px; padding: 1px 10px; font: inherit; cursor: pointer; }
#pr-interrupted button:hover { border-color: var(--accent); }
`;
document.head.append(ibarCSS);
function showInterrupted(n, info) {
  const po = state.previewOpen;
  if (!info || !po || po.pr !== n || state.filesMode !== "compare") {
    ibar.classList.add("hidden");
    return;
  }
  ibar.innerHTML =
    `<span>An earlier send to #${n} was interrupted: ${info.count === 1 ? "1 item waits" : esc(String(info.count)) + " items wait"} in a pending review on GitHub.</span>` +
    `<button data-k="finish">Finish sending</button>` +
    (info.joined ? "" : `<button data-k="discard">Discard</button>`);
  ibar.classList.remove("hidden");
}
ibar.addEventListener("click", (e) => {
  const b = e.target.closest("button");
  const po = state.previewOpen;
  if (!b || !po || !po.pr) return;
  const k = b.dataset.k;
  sendToGitHub(po.pr, { kind: k }, (k === "finish" ? "finishing" : "discarding") + " the interrupted send on #" + po.pr);
});
// The bar belongs to the open PR's compare screen: it goes when that does.
new MutationObserver(() => {
  if ($("compare-bar").classList.contains("hidden")) ibar.classList.add("hidden");
}).observe($("compare-bar"), { attributes: true, attributeFilter: ["class"] });

// followMovedHead fetches PR n's new head and re-opens its diff — if the user
// is still looking at it. One follow per PR at a time: a waiting read that
// lands after another already saw the move must not fetch it again (a
// second fetch is refused red: "another operation is running").
const followMovedHead = oncePerKey(async (n) => {
  const po = state.previewOpen;
  if (!po || po.pr !== n) return; // they moved on; the next open fetches
  opLine("⟳ " + prLabel(n) + " has new commits — updating…");
  if (await fetchPR(n)) {
    const now = state.previewOpen;
    if (now && now.pr === n) await showPR(n, true);
  }
});

// A send refused because the PR moved on GitHub: follow the new head (the
// diff re-opens on it, if the user is still looking at the PR).
onHeadMoved((n) => followMovedHead(n));

// refreshAfterSend re-reads PR n once a send ended. A read already running
// started before the send landed: wait for it, then read again — the read
// that absorbs the send's own change must start after it.
function refreshAfterSend(n) {
  reads.soon("after-send:" + n, commentsRead(n, false));
}

// A finished send: a change it made is not "updated" (an abort or a failure
// changed nothing, so it arms nothing), and the PR is re-read (the
// interrupted bar follows what GitHub now holds).
onSendDone((n, ev) => {
  const sent = sentEvent(ev, readSeq);
  if (sent) freshEvent(n, sent);
  refreshAfterSend(n);
});

// headMoved: the forge's answer says PR n's head is not the one on screen —
// "moved" from the server (its local ref is behind), or a head that differs
// from the open view's (a background prefetch may already have fetched the
// new head into the local ref, so the server sees nothing behind).
function headMoved(n, r) {
  if (r.moved) return true;
  const po = state.previewOpen;
  return !!(r.forge_head && po && po.pr === n && po.sourceHash && po.sourceHash !== r.forge_head);
}

// reloadPRNotes redraws PR n's threads (or its file badges) when they changed.
async function reloadPRNotes(n) {
  const po = state.previewOpen;
  if (!po || po.pr !== n || state.filesMode !== "compare") return;
  const ctx = state.diffCtx;
  if (ctx && ctx.preview && ctx.preview.pr === n) await fetchNotes(); // also refreshes the ◆N badges
  else await loadPRCounts(n);
}

// showPR opens PR n's diff from the LOCAL head — no network. The server
// resolves the pair; "unfetched" means there is no local head yet. skipComments:
// the caller asks the forge right after (revalidate reads the comments too).
async function showPR(n, moved, skipComments) {
  let body;
  try {
    body = await getJSON("/api/pr/open?n=" + n);
  } catch (err) {
    opLine("pull request #" + n + ": " + (err.message || err), true);
    return "error";
  }
  if (body.state === "unfetched") return "unfetched";
  await openPreviewBody(body, "");
  if (body.read_at) lastRead.set(n, body.read_at);
  setPRFresh(n, moved ? fresh.text : "");
  showInterrupted(n, null); // the next refresh answer says whether one waits
  if (moved) opLine("pull request #" + n + " updated: new commits on the forge");
  // The diff is up with whatever threads the server had cached; only NOW is
  // the forge asked for the PR's comments (never on the click path).
  if (!skipComments) refreshPRComments(n, moved);
  return "shown";
}

// refreshPRComments re-reads PR n's review comments from the forge and, when
// they changed, redraws the threads of the diff on screen. It runs after a PR
// diff opens and on every `prs` live event; a failure is silent — the diff
// simply stays without (newer) threads.
// moved: the view was just re-opened on a moved head — that read is news.
export function refreshPRComments(n, moved = false) {
  // Queued, never dropped: the read a moved-head reopen asks for may arrive
  // while another read is still running — and a moved read replaced while
  // it waits keeps its "updated" (stickyFlag).
  soonComments("comments:" + n, moved, (mv) => commentsRead(n, mv));
}

// commentsRead is refreshPRComments's read, for the serial reader.
function commentsRead(n, moved) {
  return async () => {
    const seq = ++readSeq;
    let r;
    try {
      r = await postJSON("/api/pr/comments/refresh?n=" + n, {});
    } catch {
      offlineFresh(n);
      return;
    }
    freshEvent(n, { kind: "ok", changed: !!(r.changed || moved || headMoved(n, r)), seq });
    showInterrupted(n, r.interrupted);
    if (r.changed) await reloadPRNotes(n);
    // The same read says whether the head moved: follow it OUTSIDE this
    // gate — the re-open asks for the comments again.
    if (headMoved(n, r)) setTimeout(() => followMovedHead(n), 0);
  };
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
// the user is still looking at it. It waits for a read already running (C9).
function revalidate(n) {
  reads.soon("revalidate:" + n, revalidateRead(n));
}

// revalidateRead is revalidate's read, for the serial reader.
function revalidateRead(n) {
  return async () => {
    const seq = ++readSeq;
    freshEvent(n, { kind: "start" });
    let rv;
    try {
      rv = await postJSON("/api/pr/revalidate?n=" + n, {});
    } catch {
      offlineFresh(n); // offline, rate-limited: the diff on screen stands
      return;
    }
    if (rv.read_at) lastRead.set(n, rv.read_at);
    freshEvent(n, { kind: "ok", changed: !!(rv.comments_changed || headMoved(n, rv)), seq });
    showInterrupted(n, rv.interrupted);
    fetchPRs(); // the row may have changed state (merged, closed)
    // One read answered both: the comments, and whether the head moved.
    if (rv.comments_changed) await reloadPRNotes(n);
    // Followed OUTSIDE the gate: the re-open asks for the comments again.
    if (headMoved(n, rv)) setTimeout(() => followMovedHead(n), 0);
  };
}

// openPR is the row click: serve what is here, then check the forge.
//   - a head that is already local opens AT ONCE (a purely local read), and
//     revalidate() updates it in the background when the forge moved on;
//   - otherwise the head is fetched first, under the mask.
// The server's PR cache makes the fetch itself cheap the second time: the
// head sha comes from the listing, so an unchanged PR skips the network.
// It resolves true when the PR's view is on screen (openPRLanding waits on it).
let opening = 0;
async function openPR(pr) {
  const n = pr.number;
  if (opening) return false; // one open at a time; the mask says which
  opening = n;
  maskOn(n, "opening " + prLabel(n) + "…");
  try {
    if (pr.fetched) {
      const how = await showPR(n, false, pr.state === "open"); // an open PR's comments ride revalidate
      if (how === "shown") {
        maskOff();
        if (pr.state === "open") revalidate(n); // a closed PR's head no longer moves
        return true;
      }
      if (how === "error") return false;
    }
    $("pr-mask-text").textContent = "fetching " + prLabel(n) + "…";
    if (!(await fetchPR(n))) return false;
    pr.fetched = true; // a second click on this row shows the diff without another fetch
    $("pr-mask-text").textContent = "computing the diff of " + prLabel(n) + "…";
    const how = await showPR(n, false);
    if (how === "unfetched") opLine("pull request #" + n + ": the head did not arrive", true);
    return how === "shown";
  } finally {
    opening = 0;
    maskOff();
  }
}

// openPRLanding opens PR n's view for a gg:// link (live.js): the same open
// as a row click. true = on screen; false = the open failed (it said why);
// null = the page does not list PR n.
export async function openPRLanding(n) {
  // A link opened as the page loads (gg open --web) may beat the list: read
  // the server's listing once before calling the PR unknown.
  if (!knownPR(n)) await fetchPRs();
  const pr = knownPR(n);
  return pr ? await openPR(pr) : null;
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
    "with a usable <b>gh</b> the sidebar lists the repository's open pull requests (gg writes to GitHub only " +
    "when you send — see <i>send to GitHub</i>). The row leads with the review verdict: <b>✓</b> approved, <b>✗</b> changes " +
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
