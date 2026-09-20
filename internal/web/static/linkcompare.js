// linkcompare.js — "compare with link…": two gg:// link fields, submitted to
// /api/compare-links. The page never parses a link and never rewrites one: it
// carries text to the server and paints what comes back. The server runs the
// text through domain.CompareLinks — the same door the CLI, MCP and the TUI
// use — so no frontend can disagree about what a link means.

import { $, getJSON, runOnce } from "./core.js";
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
  closeLayer("linkcmp");
}

// The overlay owns the keyboard while it is open (it returns true for every
// key), but it preventDefaults ONLY the keys it acts on: everything else is
// the focused field's own.
function onKey(e) {
  if (e.key === "Escape") {
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
  field(field("left").value && !field("right").value ? "right" : "left").focus();
}

$("linkcmp").addEventListener("click", close); // the backdrop
$("linkcmp-box").addEventListener("click", (e) => e.stopPropagation());
$("linkcmp-close").addEventListener("click", close);
$("linkcmp-go").addEventListener("click", submit);

registerRows("menu", () => [{ label: "compare with link…", act: () => openLinkCompareDialog() }]);

registerHelp({
  key: "compare with link",
  html:
    "<b>compare with link…</b> (the command palette, or the ☰ menu) compares any two <b>gg://</b> links — a " +
    "branch, a commit, a stash, a file at a revision, a change-set, the working tree. Paste one link in each " +
    "field; <b>enter</b> moves on and then compares, <b>esc</b> closes. An error is shown under the field it " +
    "belongs to. The result opens as a comparison: every file that differs, each with its diff",
});
