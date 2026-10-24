// prdetails.js — a pull request's details, the TUI's PR hub: its description,
// the conversation with the review verdicts, and the OUTDATED review threads —
// the ones the forge can no longer place on a line, shown with the hunk they
// were written against. Everything that has a place lives in the diff instead.
// The hub's keys act on the PR: y copy URL, L copy gg link, r reload,
// s Send to GitHub…, v Verdict…, / filter.
//
// The load is a POST: the description is not in the listing, so this always
// spends a forge call, and a GET never does (R2). It is the overlay's own,
// explicit load — nothing else triggers it.

import { $, esc, getJSON, postJSON, runOnce, state } from "./core.js";
import { closeLayer, copyText, pushLayer } from "./layers.js";
import { registerHelp } from "./menus.js";
import { noteAge } from "./notebox.js";
import { mdHTML } from "./markdown.js";
import { opLine } from "./ops.js";
import { recallKey, recallOpen, recordSearch } from "./searchhist.js";

const VERDICTS = { approved: "✓ approved", changes_requested: "✗ changes requested", commented: "commented" };

// textHTML is one forge text: the tree the server parsed it into, painted —
// or, when an answer carries none, the raw text exactly as before.
function textHTML(md, raw) {
  return md ? `<div class="prd-text md">${mdHTML(md, esc)}</div>` : `<div class="prd-text">${esc(raw || "")}</div>`;
}

function commentHTML(c, now) {
  const head = [c.author || "ghost", noteAge(c.created, now), VERDICTS[c.verdict] || ""].filter(Boolean).join(" · ");
  return `<div class="prd-comment"><div class="prd-who">${esc(head)}</div>${textHTML(c.md, c.body)}</div>`;
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
    (pr.url ? `<div class="prd-url">${esc(pr.url)}</div>` : "") +
    `<h3>description</h3>` +
    (pr.body ? textHTML(d.body_md, pr.body) : `<div class="prd-none">no description</div>`) +
    `<h3>conversation (${hub.length})</h3>` +
    (hub.length ? hub.map((c) => commentHTML(c, now)).join("") : `<div class="prd-none">no comments</div>`) +
    (outdated.length ? `<h3>outdated review threads</h3>` + threadsHTML(outdated, now) : "") +
    (d.truncated ? `<div class="prd-none">…the forge returned more comments than gg reads (the newest are missing)</div>` : "");
  shownURL = pr.url || "";
  applyFilter();
}

// status is the line under the title: what the overlay is doing right now
// (spinning), or why what it shows is not fresh (err).
function status(text, kind) {
  const el = $("prdetails-status");
  el.textContent = text || "";
  el.className = text ? kind || "busy" : "hidden";
}

// hubFilter is the TUI content popup's visible() over the overlay's blocks
// ({heading, text}, in order): a block shows when its text holds the query
// (case-insensitive), a heading only while a block under it shows; an empty
// query shows everything. Answers one boolean per block.
function hubFilter(items, q) {
  if (!q) return items.map(() => true);
  const needle = q.toLowerCase();
  const out = items.map((it) => !it.heading && it.text.toLowerCase().includes(needle));
  let head = -1;
  items.forEach((it, i) => {
    if (it.heading) head = i;
    else if (out[i] && head >= 0) out[head] = true;
  });
  return out;
}

// The filter's grain is a BLOCK (a browser has no fixed rows): the meta and
// URL lines, each paragraph / list item / code block of a text, each
// comment's author line, each thread's location and hunk.
const BLOCKS = "h3, .prd-meta, .prd-url, .prd-who, .prd-where, .prd-hunk, .prd-none, .prd-text:not(.md), .prd-text.md > *";

// query is the filter (committed or being typed); shownURL the shown PR's URL.
let query = "";
let shownURL = "";

function applyFilter() {
  const body = $("prdetails-body");
  const els = [...body.querySelectorAll(BLOCKS)];
  const vis = hubFilter(els.map((el) => ({ heading: el.tagName === "H3", text: el.textContent })), query);
  els.forEach((el, i) => el.classList.toggle("prd-off", !vis[i]));
  // A comment or thread with nothing left showing folds away with its frame.
  for (const box of body.querySelectorAll(".prd-comment, .prd-thread, .prd-reply")) {
    box.classList.toggle("prd-off", !!query && !box.querySelector(":is(" + BLOCKS + "):not(.prd-off)"));
  }
}

function filterField() {
  return $("prdetails-filter");
}

// openFilter is `/`: a fresh query typed in the field (the TUI resets it).
function openFilter() {
  const f = filterField();
  query = f.value = "";
  f.classList.remove("hidden");
  recallOpen(f);
  applyFilter();
  f.focus();
}

// clearFilter drops the query and hides the field.
function clearFilter() {
  const f = filterField();
  query = f.value = "";
  f.classList.add("hidden");
  f.blur();
  applyFilter();
}

function copyURL() {
  if (!shownURL) {
    opLine("this pull request has no URL", true);
    return;
  }
  copyText(shownURL, "pull request URL");
}

async function copyLinkOf(n) {
  const { copyPRLink } = await import("./links.js"); // at keypress: links.js reaches prs.js back
  copyPRLink((state.prs || []).find((p) => p.number === n) || { number: n });
}

async function sendOf(n) {
  const { openSendPanel } = await import("./prsendpanel.js");
  openSendPanel(n);
}

async function verdictOf(n) {
  const { verdictBody } = await import("./prsend.js");
  verdictBody(n);
}

// act runs one of the hub's actions on the shown PR (keys and buttons alike).
function act(key) {
  const n = showing;
  if (!n) return;
  if (key === "y") copyURL();
  else if (key === "L") copyLinkOf(n);
  else if (key === "r") {
    clearFilter();
    load(n);
  } else if (key === "s") sendOf(n);
  else if (key === "v") verdictOf(n);
  else if (key === "/") openFilter();
}

// filterKey is a key in the filter field: typing goes to the field; enter
// keeps the filter (and records it), esc drops it; arrows and pages still
// scroll, as the TUI's selection moves while typing.
function filterKey(e, input) {
  if (recallKey(e, input, "filetree", (v) => {
    input.value = query = v;
    applyFilter();
  })) return true;
  if (e.key === "Enter") {
    e.preventDefault();
    input.blur();
    recordSearch("filetree", query);
    if (!query) clearFilter();
  } else if (e.key === "Escape") {
    e.preventDefault();
    clearFilter();
  } else if (["ArrowDown", "ArrowUp", "PageDown", "PageUp"].includes(e.key)) scrollKey(e);
  return true;
}

function scrollKey(e) {
  const body = $("prdetails-body");
  const page = Math.max(40, body.clientHeight - 40);
  if (e.key === "ArrowDown") body.scrollTop += 40;
  else if (e.key === "ArrowUp") body.scrollTop -= 40;
  else if (e.key === "PageDown") body.scrollTop += page;
  else if (e.key === "PageUp") body.scrollTop -= page;
  e.preventDefault();
}

function onKey(e) {
  const input = filterField();
  if (e.target === input) return filterKey(e, input);
  const body = $("prdetails-body");
  const page = Math.max(40, body.clientHeight - 40);
  if (e.ctrlKey || e.metaKey || e.altKey) {
    if ((e.ctrlKey || e.metaKey) && (e.key === "c" || e.key === "a")) return true; // copying text stays the browser's
  } else if (e.key === "Escape") {
    if (query) clearFilter(); // the first esc drops a kept filter, the next closes
    else close();
  } else if (e.key === "q" || e.key === "Enter") close();
  else if (["y", "L", "r", "s", "v", "/"].includes(e.key)) act(e.key);
  else if (e.key === "j" || e.key === "ArrowDown") body.scrollTop += 40;
  else if (e.key === "k" || e.key === "ArrowUp") body.scrollTop -= 40;
  else if (e.key === "PageDown" || e.key === " ") body.scrollTop += page;
  else if (e.key === "PageUp") body.scrollTop -= page;
  else if (e.key === "g") body.scrollTop = 0;
  else if (e.key === "G") body.scrollTop = body.scrollHeight;
  e.preventDefault();
  return true; // the overlay owns the keyboard until closed
}

// showing is the PR the overlay is open on (0 = closed): a slow answer for a
// PR the reader has already left must not repaint someone else's overlay.
let showing = 0;

function close() {
  showing = 0;
  clearFilter();
  closeLayer("prdetails");
}

// openPRDetails shows the overlay AT ONCE, then fills it:
//   1. a loading line (the forge takes seconds; a click must answer now);
//   2. what the server has cached from an earlier look — a GET, no forge call;
//   3. the fresh read (the POST), which replaces it and clears the notice.
// A failed fresh read keeps the cached copy on screen and says so.
export function openPRDetails(n) {
  showing = n;
  clearFilter();
  pushLayer("prdetails", $("prdetails"), { onKey });
  load(n);
}

// load (re)reads PR n into the open overlay — opening it, or r.
function load(n) {
  const pr = (state.prs || []).find((p) => p.number === n);
  $("prdetails-title").textContent = "#" + n + (pr && pr.title ? " · " + pr.title : "");
  shownURL = (pr && pr.url) || "";
  $("prdetails-body").innerHTML = `<div class="prd-loading"><span class="prd-spin">⟳</span> reading pull request #${n} from the forge…</div>`;
  status("");
  const run = runOnce("pr-details", async () => {
    let have = false;
    try {
      const c = await getJSON("/api/pr/details?n=" + n);
      if (showing !== n) return;
      if (c.cached) {
        render(c);
        have = true;
        status("⟳ checking the forge for changes…");
      }
    } catch {
      // no cached copy is not an error: the fresh read below is the real one
    }
    try {
      const d = await postJSON("/api/pr/details?n=" + n, {});
      if (showing !== n) return;
      const at = $("prdetails-body").scrollTop;
      render(d);
      if (have) $("prdetails-body").scrollTop = at; // a refresh must not yank the reader back up
      status("");
    } catch (err) {
      if (showing !== n) return;
      const why = err.message || String(err);
      if (have) status("could not reach the forge — showing the last copy (" + why + ")", "err");
      else $("prdetails-body").innerHTML = `<div class="prd-loading err">${esc(why)}</div>`;
    }
  });
  if (!run) status("⟳ still reading…");
}

$("prdetails").addEventListener("click", close); // the backdrop
$("prdetails-box").addEventListener("click", (e) => e.stopPropagation()); // select / copy inside
$("prdetails-close").addEventListener("click", close);
$("prdetails-keys").addEventListener("click", (e) => {
  const b = e.target.closest("button[data-k]");
  if (!b) return;
  b.blur(); // the overlay owns the keyboard: enter must not close it over a focused button
  act(b.dataset.k);
});
$("prdetails-filter").addEventListener("input", (e) => {
  query = e.target.value;
  applyFilter();
});

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
    "on. Threads that still have a place are in the diff itself. <b>j / k</b> scroll; the TUI hub's keys act on " +
    "the PR — <b>y</b> copy URL, <b>L</b> copy gg link, <b>r</b> reload, <b>s</b> send to GitHub…, " +
    "<b>v</b> verdict…, <b>/</b> filter (enter keeps it, alt+↓ / alt+↑ recall, esc drops it); " +
    "<b>esc</b>, <b>q</b> or <b>enter</b> closes",
});
