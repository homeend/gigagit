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
import { openPreviewBody } from "./previews.js";
import { prRowParts } from "./prsrow.js";

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

export async function fetchPRs() {
  try {
    take(await getJSON("/api/pr"));
  } catch {
    // A failed read is not "the pull requests are gone": the rows stand.
  }
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
        `<li data-pr="${pr.number}" class="${p.dim ? "prdim" : ""}" title="${esc(p.tip)}">` +
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

// showPR opens PR n's diff. The server resolves the pair; "unfetched" means
// there is no local head yet, which a fetch fixes — once.
async function showPR(n, fetched) {
  let body;
  try {
    body = await getJSON("/api/pr/open?n=" + n);
  } catch (err) {
    opLine("pull request #" + n + ": " + (err.message || err), true);
    return;
  }
  if (body.state === "unfetched") {
    if (fetched) opLine("pull request #" + n + ": the head did not arrive", true);
    else fetchThenShow(n);
    return;
  }
  await openPreviewBody(body, "");
}

async function fetchThenShow(n) {
  if (opBusy()) return; // one live op; the server would 409 anyway
  let resp;
  try {
    resp = await postJSON("/api/op", { op: "pr-fetch", number: n });
  } catch (err) {
    opLine("pull request #" + n + ": " + (err.message || err), true);
    return;
  }
  // onDone REPLACES the generic done handling: a PR fetch writes one private
  // ref, so nothing but this list (and the diff it opens) needs a reload.
  followOp(resp.op_id, "fetching pull request #" + n, "pr-fetch", (ev) => {
    fetchPRs();
    if (!ev.ok) {
      opLine("error: " + (ev.error || "operation failed"), true);
      return;
    }
    opLine(ev.summary || "fetched pull request #" + n);
    showPR(n, true);
  });
}

// openPR is the row click. An OPEN pull request is fetched first, every time:
// its head moves. A closed or merged one no longer does, so a head that is
// already here opens as it is.
function openPR(pr) {
  if (pr.state !== "open" && pr.fetched) showPR(pr.number, false);
  else fetchThenShow(pr.number);
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
  const items = [{ label: "open pull request #" + pr.number, act: () => openPR(pr) }];
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

registerHelp({
  key: "pull requests",
  html:
    "with a usable <b>gh</b> the sidebar lists the repository's open pull requests (read-only — gg never " +
    "writes to the forge). The row leads with the review verdict: <b>✓</b> approved, <b>✗</b> changes " +
    "requested, <b>●</b> review required. <b>Click</b> fetches the head and opens the PR's diff on the merge " +
    "preview screen; <b>right-click</b> for copy URL and forget. A pull request gg already knows stays " +
    "listed, dimmed, after it is closed or merged. The header's <b>⟳</b> re-reads the list; it also " +
    "re-reads itself every <b>[refresh] prs</b> seconds (300; 0 = off), whatever the auto-refresh switch says",
});
