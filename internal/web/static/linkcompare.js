// linkcompare.js — "compare with link…": two gg:// link fields, submitted to
// /api/compare-links. The page never parses a link and never rewrites one: it
// carries text to the server and paints what comes back. The server runs the
// text through domain.CompareLinks — the same door the CLI, MCP and the TUI
// use — so no frontend can disagree about what a link means.

import { $, esc, getJSON, runOnce } from "./core.js";
import { openLinkCompare } from "./files.js";
import { closeLayer, pushLayer } from "./layers.js";
import { registerHelp, registerRows } from "./menus.js";
import { opLine } from "./ops.js";

const SIDES = ["left", "right"];
const field = (side) => $("linkcmp-" + side);

// setErr paints a failure under the field it belongs to ("" = the form).
function setErr(side, text) {
  const el = $(side ? "linkcmp-err-" + side : "linkcmp-err");
  el.textContent = text || "";
  el.classList.toggle("hidden", !text);
}

function clearErrs() {
  setErr("", "");
  SIDES.forEach((s) => setErr(s, ""));
}

// runLinkCompare is the ONE client path into /api/compare-links: the dialog, a
// saved row and a pair landing all open the view through it. query is the
// route's query string. A failure goes to onErr (the dialog, which knows the
// side) or to the op line (everyone else).
export async function runLinkCompare(query, onErr) {
  let body;
  try {
    body = await getJSON("/api/compare-links?" + query);
  } catch (e) {
    if (onErr) onErr(e);
    else opLine("compare: " + (e.message || e), true);
    return false;
  }
  openLinkCompare(body);
  return true;
}

// --- the base picker (GET /api/link-base) ---
// A branch tip or a whole commit is an UNBOUNDED point: comparing it means
// comparing every file. A base bounds it — `@<base>...<branch>`, the files a
// merge of the branch into the base would change; `@<parent>..<sha>`, what the
// commit changed. The server says whether a field's link can take one and
// which to offer, and the server writes the bounded link: this page cannot
// parse a link, so it must not rewrite one.
const baseRow = (side) => $("linkcmp-base-" + side);
const baseInput = (side) => $("linkcmp-basein-" + side);
const baseTimer = { left: 0, right: 0 };

function setBaseErr(side, text) {
  const el = $("linkcmp-baseerr-" + side);
  el.textContent = text || "";
  el.classList.toggle("hidden", !text);
}

function hideBase(side) {
  baseRow(side).classList.add("hidden");
  setBaseErr(side, "");
}

// fieldChanged runs whenever a link field's text changed — typed, picked or
// swapped. The lookup is debounced per side; the two sides are independent,
// so they share no single-flight key, and a late answer is dropped by
// comparing the text it was asked for with the text the field holds NOW.
function fieldChanged(side) {
  clearTimeout(baseTimer[side]);
  baseTimer[side] = setTimeout(() => lookupBase(side), 250);
}

// lookupBase shows or hides the base row. It NEVER touches the link field:
// nothing is rewritten until the user acts on the base row (applyBase).
async function lookupBase(side) {
  const text = field(side).value.trim();
  if (!text) return hideBase(side);
  let sug;
  try {
    sug = await getJSON("/api/link-base?" + new URLSearchParams({ link: text }));
  } catch {
    return hideBase(side);
  }
  if (field(side).value.trim() !== text) return; // a late answer for text the field no longer holds
  if (sug.kind !== "ref" && sug.kind !== "commit") return hideBase(side);
  baseInput(side).value = sug.base || "";
  baseInput(side).placeholder = sug.kind === "ref" ? "a branch or tag" : "the parent's full commit id";
  baseRow(side).querySelector(".lc-why").textContent = sug.why ? "(" + sug.why + ")" : "";
  setBaseErr(side, "");
  baseRow(side).classList.remove("hidden");
}

// applyBase is the ONLY thing that rewrites a link field, and only on the
// user's say-so: enter on the base row, or its button.
async function applyBase(side) {
  const base = baseInput(side).value.trim();
  if (!base) return setBaseErr(side, "a base is required");
  const text = field(side).value.trim();
  let out;
  try {
    out = await getJSON("/api/link-base?" + new URLSearchParams({ link: text, base }));
  } catch (e) {
    return setBaseErr(side, e.message || String(e));
  }
  if (field(side).value.trim() !== text) return;
  field(side).value = out.link;
  hideBase(side); // the field is bounded now
  field(side).focus();
}

// --- copied-link history (GET /api/linkhist) ---
// The ring is the server's: gg web binds a random port each run, which empties
// browser storage, and the TUI and CLI feed the same ring.
let histOpen = ""; // the side whose list is showing
let histRows = [];
let histSel = 0;

function paintHist() {
  const ul = $("linkcmp-hist-" + histOpen);
  ul.innerHTML = histRows.length
    ? histRows
        .map(
          (r, i) =>
            `<li data-i="${i}"${i === histSel ? ' class="sel"' : ""} title="${esc(r.link)}">${esc(r.desc || r.link)}` +
            `<span class="psub">${esc(r.link)}</span></li>`
        )
        .join("")
    : `<li class="none">no copied links yet</li>`;
  ul.classList.remove("hidden");
  const sel = ul.querySelector("li.sel");
  if (sel) sel.scrollIntoView({ block: "nearest" });
}

function closeHist() {
  if (!histOpen) return;
  $("linkcmp-hist-" + histOpen).classList.add("hidden");
  histOpen = "";
}

async function openHist(side) {
  closeHist();
  let rows = [];
  try {
    rows = (await getJSON("/api/linkhist")).links || [];
  } catch {
    rows = []; // an unreadable history is an empty one; the field still works
  }
  histOpen = side;
  histRows = rows;
  histSel = 0;
  paintHist();
}

// pickHist FILLS the field. It never compares: the user may still want a base,
// or the other side.
function pickHist(i) {
  const r = histRows[i];
  const side = histOpen;
  closeHist();
  if (!r || !side) return;
  field(side).value = r.link;
  setErr(side, "");
  hideBase(side);
  field(side).focus();
  fieldChanged(side);
}

function swap() {
  closeHist();
  const text = field("left").value;
  field("left").value = field("right").value;
  field("right").value = text;
  const err = $("linkcmp-err-left").textContent;
  setErr("left", $("linkcmp-err-right").textContent);
  setErr("right", err);
  SIDES.forEach(hideBase); // each row was about the OTHER link; the lookups bring back what fits
  SIDES.forEach(fieldChanged);
}

function submit() {
  clearErrs();
  const left = field("left").value.trim();
  const right = field("right").value.trim();
  if (!left) setErr("left", "a link is required");
  if (!right) setErr("right", "a link is required");
  if (!left || !right) return; // nothing to ask the server
  const started = runOnce("link-compare", async () => {
    const ok = await runLinkCompare(new URLSearchParams({ left, right }).toString(), (e) =>
      setErr(e.data && SIDES.includes(e.data.side) ? e.data.side : "", e.message || String(e))
    );
    if (ok) close();
  });
  if (!started) setErr("", "still comparing…");
}

function close() {
  closeHist();
  closeLayer("linkcmp");
}

// The overlay owns the keyboard while it is open (it returns true for every
// key), but it preventDefaults ONLY the keys it acts on: everything else is
// the focused field's own.
function onKey(e) {
  if (histOpen) {
    // The open list takes the keys it understands; esc closes the LIST.
    if (e.key === "ArrowDown") histSel = Math.min(histSel + 1, Math.max(histRows.length - 1, 0));
    else if (e.key === "ArrowUp") histSel = Math.max(histSel - 1, 0);
    else if (e.key === "Enter") pickHist(histSel);
    else if (e.key === "Escape") closeHist();
    else {
      closeHist(); // typing goes to the field
      return true;
    }
    e.preventDefault();
    if (histOpen) paintHist();
    return true;
  }
  const side = SIDES.find((s) => e.target === field(s));
  const baseSide = SIDES.find((s) => e.target === baseInput(s));
  if (e.key === "Enter" && baseSide) {
    e.preventDefault();
    applyBase(baseSide); // enter on a base row rewrites; it never compares
  } else if (e.key === "ArrowDown" && side) {
    e.preventDefault();
    openHist(side);
  } else if ((e.ctrlKey || e.metaKey) && e.key === "s") {
    e.preventDefault(); // the browser's own "save page"
    swap();
  } else if (e.key === "Escape") {
    e.preventDefault();
    close();
  } else if (e.key === "Enter" && e.target === field("left")) {
    e.preventDefault();
    field("right").focus();
  } else if (e.key === "Enter" && e.target === field("right")) {
    e.preventDefault();
    submit();
  }
  return true;
}

// openLinkCompareDialog opens the form. The text typed last time is still
// there: the fields are only overwritten by an explicit prefill.
export function openLinkCompareDialog(prefill) {
  clearErrs();
  const p = prefill || {};
  if (p.left !== undefined) field("left").value = p.left;
  if (p.right !== undefined) field("right").value = p.right;
  pushLayer("linkcmp", $("linkcmp"), { onKey });
  SIDES.forEach(fieldChanged);
  field(field("left").value && !field("right").value ? "right" : "left").focus();
}

$("linkcmp").addEventListener("click", close); // the backdrop
$("linkcmp-box").addEventListener("click", (e) => e.stopPropagation());
$("linkcmp-close").addEventListener("click", close);
$("linkcmp-go").addEventListener("click", submit);
$("linkcmp-swap").addEventListener("click", swap);
$("linkcmp-box").addEventListener("click", (e) => {
  const btn = e.target.closest(".lc-hist-btn");
  if (btn) return histOpen === btn.dataset.side ? closeHist() : openHist(btn.dataset.side);
  const li = e.target.closest(".lc-hist li[data-i]");
  if (li) return pickHist(Number(li.dataset.i));
  const bound = e.target.closest(".lc-bound");
  if (bound) applyBase(bound.dataset.side);
});
SIDES.forEach((s) => field(s).addEventListener("input", () => fieldChanged(s)));

registerRows("menu", () => [{ label: "compare with link…", act: () => openLinkCompareDialog() }]);

registerHelp({
  key: "compare with link",
  html:
    "<b>compare with link…</b> (the command palette, or the ☰ menu) compares any two <b>gg://</b> links — a " +
    "branch, a commit, a stash, a file at a revision, a change-set, the working tree. Paste one link in each " +
    "field; <b>enter</b> moves on and then compares, <b>esc</b> closes. <b>↓</b> (or ▾) lists the links you " +
    "copied — in the web, the TUI or the CLI — and a pick fills the field without comparing. <b>ctrl+s</b> " +
    "(or ⇅ swap) exchanges the two sides. A <b>base</b> row appears under a link to a branch, a tag or a " +
    "whole commit, prefilled with its upstream (else the trunk), or the commit's parent: <b>enter</b> on it " +
    "(or <b>bound</b>) rewrites the link to the files a merge into that base would change — " +
    "<b>@base...branch</b> — or to what the commit changed. Nothing is rewritten until you do; leaving it " +
    "alone compares the whole tips. An error is shown under the field it " +
    "belongs to. The result opens as a comparison: every file that differs, each with its diff",
});
