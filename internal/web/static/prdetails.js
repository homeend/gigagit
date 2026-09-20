// prdetails.js — a pull request's details (read-only): its description, the
// conversation with the review verdicts, and the OUTDATED review threads — the
// ones the forge can no longer place on a line, shown with the hunk they were
// written against. Everything that has a place lives in the diff instead.
//
// The load is a POST: the description is not in the listing, so this always
// spends a forge call, and a GET never does (R2). It is the overlay's own,
// explicit load — nothing else triggers it.

import { $, esc, postJSON, runOnce, state } from "./core.js";
import { closeLayer, copyText, pushLayer } from "./layers.js";
import { opLine } from "./ops.js";
import { registerHelp } from "./menus.js";
import { noteAge } from "./notebox.js";

const VERDICTS = { approved: "✓ approved", changes_requested: "✗ changes requested", commented: "commented" };

function commentHTML(c, now) {
  const head = [c.author || "ghost", noteAge(c.created, now), VERDICTS[c.verdict] || ""].filter(Boolean).join(" · ");
  return `<div class="prd-comment"><div class="prd-who">${esc(head)}</div><div class="prd-text">${esc(c.body || "")}</div></div>`;
}

// threadsHTML groups the outdated comments into threads (a reply names its
// parent) and leads each with where it WAS and the hunk it was written on.
function threadsHTML(outdated, now) {
  const roots = [];
  const at = new Map();
  for (const c of outdated) {
    const i = c.parent_id ? at.get(c.parent_id) : undefined;
    if (i === undefined) {
      at.set(c.id, roots.length);
      roots.push({ root: c, replies: [] });
    } else {
      at.set(c.id, i);
      roots[i].replies.push(c);
    }
  }
  return roots
    .map(
      (t) =>
        `<div class="prd-thread"><div class="prd-where">${esc(t.root.path || "")}${t.root.resolved ? " · resolved" : ""}</div>` +
        (t.root.hunk ? `<pre class="prd-hunk">${esc(t.root.hunk)}</pre>` : "") +
        commentHTML(t.root, now) +
        t.replies.map((r) => `<div class="prd-reply">${commentHTML(r, now)}</div>`).join("") +
        `</div>`
    )
    .join("");
}

function render(d) {
  const pr = d.pr || {};
  const now = Date.now();
  const state_ = pr.state === "open" && pr.draft ? "draft" : pr.state || "";
  $("prdetails-title").textContent = "#" + pr.number + " · " + (pr.title || "");
  const meta = [state_, pr.author, (pr.source || "?") + " → " + (pr.target || "?"), noteAge(pr.updated, now)].filter(Boolean).join(" · ");
  const hub = d.hub || [];
  const outdated = d.outdated || [];
  $("prdetails-body").innerHTML =
    `<div class="prd-meta">${esc(meta)}</div>` +
    (pr.url ? `<div class="prd-url"><span>${esc(pr.url)}</span> <button id="prdetails-copy">copy URL</button></div>` : "") +
    `<h3>description</h3>` +
    (pr.body ? `<div class="prd-text">${esc(pr.body)}</div>` : `<div class="prd-none">no description</div>`) +
    `<h3>conversation (${hub.length})</h3>` +
    (hub.length ? hub.map((c) => commentHTML(c, now)).join("") : `<div class="prd-none">no comments</div>`) +
    (outdated.length ? `<h3>outdated review threads</h3>` + threadsHTML(outdated, now) : "") +
    (d.truncated ? `<div class="prd-none">…the forge returned more comments than gg reads (the newest are missing)</div>` : "");
  const copy = $("prdetails-copy");
  if (copy) copy.addEventListener("click", () => copyText(pr.url, "pull request URL"));
  $("prdetails-body").scrollTop = 0;
}

function close() {
  closeLayer("prdetails");
}

// openPRDetails reads PR n's details and shows them. A failed read opens
// nothing: an empty overlay would say "this PR has no description".
export function openPRDetails(n) {
  const run = runOnce("pr-details", async () => {
    opLine("⟳ reading pull request #" + n + "…");
    let d;
    try {
      d = await postJSON("/api/pr/details?n=" + n, {});
    } catch (err) {
      opLine("pull request #" + n + ": " + (err.message || err), true);
      return;
    }
    opLine("");
    render(d);
    pushLayer("prdetails", $("prdetails"), {
      onKey: (e) => {
        const body = $("prdetails-body");
        const page = Math.max(40, body.clientHeight - 40);
        if (e.key === "Escape" || e.key === "q") close();
        else if (e.key === "j" || e.key === "ArrowDown") body.scrollTop += 40;
        else if (e.key === "k" || e.key === "ArrowUp") body.scrollTop -= 40;
        else if (e.key === "PageDown" || e.key === " ") body.scrollTop += page;
        else if (e.key === "PageUp") body.scrollTop -= page;
        else if (e.key === "g") body.scrollTop = 0;
        else if (e.key === "G") body.scrollTop = body.scrollHeight;
        else if ((e.ctrlKey || e.metaKey) && (e.key === "c" || e.key === "a")) return true; // copying text stays the browser's
        e.preventDefault();
        return true; // the overlay owns the keyboard until closed
      },
    });
  });
  if (!run) opLine("pull request details are already loading…");
}

$("prdetails").addEventListener("click", close); // the backdrop
$("prdetails-box").addEventListener("click", (e) => e.stopPropagation()); // select / copy inside
$("prdetails-close").addEventListener("click", close);

// The `details` chip on an open PR's compare bar (files.js paints it).
$("compare-bar").addEventListener("click", (e) => {
  if (!e.target.closest("#pr-details-chip")) return;
  const po = state.previewOpen;
  if (po && po.pr) openPRDetails(po.pr);
});

registerHelp({
  key: "pull request details",
  html:
    "<b>details…</b> in a pull request's right-click menu, or the <b>details</b> chip on its open diff, shows " +
    "the PR's <b>description</b>, the <b>conversation</b> with each review's verdict, and the <b>outdated</b> " +
    "review threads — comments the forge can no longer place on a line — each with the hunk it was written " +
    "on. Threads that still have a place are in the diff itself. <b>j / k</b> scroll, <b>esc</b> closes. " +
    "Read-only, plain text",
});
