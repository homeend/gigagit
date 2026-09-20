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
  $("prs-list").innerHTML = rows
    .map((pr) => {
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
    })
    .join("");
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
  clearTimeout(maskTimer);
  maskTimer = setTimeout(maskOff, 90000); // never outlive a wedged request
}
function maskOff() {
  clearTimeout(maskTimer);
  $("pr-mask").classList.add("hidden");
  if (state.prBusy) {
    state.prBusy = 0;
    renderPRs();
  }
}
$("pr-mask").addEventListener("click", maskOff);

function prLabel(n) {
  const pr = (state.prs || []).find((p) => p.number === n);
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
    const pr = (state.prs || []).find((p) => p.number === po.pr);
    if (!pr) return;
    ev.preventDefault();
    showPRMenu(pr, ev.clientX, ev.clientY);
  });
}

registerHelp({
  key: "pull requests",
  html:
    "with a usable <b>gh</b> the sidebar lists the repository's open pull requests (read-only — gg never " +
    "writes to the forge). The row leads with the review verdict: <b>✓</b> approved, <b>✗</b> changes " +
    "requested, <b>●</b> review required. <b>Click</b> fetches the head and opens the PR's diff on the merge " +
    "preview screen (a loading mask covers the panes meanwhile; a pull request opened before shows at once " +
    "and is checked against the forge in the background); <b>right-click</b> for copy URL and forget. A pull request gg already knows stays " +
    "listed, dimmed, after it is closed or merged. The header's <b>⟳</b> re-reads the list; it also " +
    "re-reads itself every <b>[refresh] prs</b> seconds (300; 0 = off), whatever the auto-refresh switch says",
});
