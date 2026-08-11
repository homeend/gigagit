// prefixes.js — branch-name prefixes, in two modes over one overlay (the
// TUI split: Settings defines them, the create-branch popup PICKS one):
//   - manage (☰ → branch prefixes…): define/delete the templated skeletons.
//   - pick (the create-branch prompt's "use prefix…"): select-only — choose
//     a prefix, fill its <user:…> labels, and the resolved name goes back
//     to the caller. Canceling reports null so the caller can restore its
//     prompt. The resolve is a server-side PREVIEW (seq counters peeked);
//     counters are consumed only when the create carrying the prefix
//     identity succeeds.
import { $, esc, getJSON, postJSON } from "./core.js";
import { closeLayer, pushLayer } from "./layers.js";

let data = null; // last GET /api/prefixes payload
// null = browse · {kind:"add"} · {kind:"labels", p} (collecting <user:…> inputs)
let mode = null;
// pick-mode callback: (resolved, prefix) — (null, null) = canceled. null = manage mode.
let picking = null;

async function openPrefixesView() {
  picking = null;
  await openOverlay();
}

// openPrefixPicker: select-only mode for the create-branch prompt. onPicked
// fires exactly once — with the resolved name + prefix, or (null, null) on
// cancel (esc from browse / backdrop) so the caller can reopen its prompt.
async function openPrefixPicker(onPicked) {
  picking = onPicked;
  await openOverlay();
}

async function openOverlay() {
  try {
    data = await getJSON("/api/prefixes");
  } catch (e) {
    if (picking) {
      const cb = picking;
      picking = null;
      cb(null, null);
    }
    return;
  }
  mode = null;
  renderPrefixes();
  pushLayer("prefixes", $("prefixes"), {
    onKey: (e) => {
      if (e.key === "Escape") {
        if (mode) {
          mode = null;
          renderPrefixes();
        } else {
          dismiss();
        }
        e.preventDefault();
        return true;
      }
      return true; // swallow WITHOUT preventDefault — inputs stay alive
    },
  });
}

// dismiss closes the overlay; in pick mode that IS a cancel.
function dismiss() {
  const cb = picking;
  picking = null;
  closeLayer("prefixes");
  if (cb) cb(null, null);
}

// finishPick closes the overlay and hands the resolved name to the caller.
function finishPick(resolved, p) {
  const cb = picking;
  picking = null;
  closeLayer("prefixes");
  if (cb) cb(resolved, p);
}

async function reloadPrefixes() {
  try {
    data = await getJSON("/api/prefixes");
  } catch (e) {
    showPfxErr(e.message);
    return;
  }
  mode = null;
  renderPrefixes();
}

function showPfxErr(msg) {
  const err = $("prefixes-box").querySelector(".serr");
  if (err) err.textContent = msg;
}

const pfxScopeTag = (s) => (s === "repo" ? "[this repo]" : "[global]");

function pfxBrowseHTML() {
  const pick = !!picking;
  const rows = (data.prefixes || [])
    .map(
      (p, i) => `
    <div class="srow prow">
      <span class="sval pfxval">${esc(p.value)}</span>
      <span class="snote pscope">${esc(pfxScopeTag(p.scope))}</span>
      <span class="pbtns">${
        pick
          ? `<button data-act="pick" data-i="${i}">use</button>`
          : `<button class="danger" data-act="delete-prefix" data-i="${i}">delete</button>`
      }</span>
    </div>`
    )
    .join("");
  const empty = pick
    ? '<div class="srow"><span class="snote">(none defined yet — add them via ☰ → branch prefixes…)</span></div>'
    : '<div class="srow"><span class="snote">(none yet)</span></div>';
  const manageRow = pick ? "" : '<div class="srow"><button data-act="new-prefix">new prefix…</button></div>';
  const foot = pick
    ? "pick a prefix to seed the branch name — <user:…> fields are asked next; <seq> counters advance only when the create succeeds · esc cancels"
    : "tokens: &lt;user:LABEL&gt; &lt;seq:NAME:N&gt; &lt;date&gt; &lt;parent-branch&gt; &lt;repo&gt; &lt;random-alpha:N&gt; · used from the create-branch prompt's <b>use prefix…</b> · esc closes";
  return `
    ${rows || empty}
    ${manageRow}
    <div class="serr"></div>
    <div class="sfoot">${foot}</div>`;
}

function pfxAddHTML() {
  return `
    <h3>new prefix</h3>
    <div class="srow"><span class="slbl">value</span><input type="text" id="x-value" placeholder="e.g. feat/<user:ticket>-" spellcheck="false"></div>
    <div class="srow"><span class="slbl">scope</span><button class="stgl on" id="x-scope" data-scope="global">global (every repo)</button></div>
    <div class="srow"><button data-act="save-prefix">save</button><button data-act="back">cancel</button></div>
    <div class="serr"></div>
    <div class="sfoot">the value is validated before it is stored (a bad token refuses) · esc backs out</div>`;
}

function pfxLabelsHTML() {
  const fields = mode.p.user_labels
    .map((l) => `<div class="srow"><span class="slbl">${esc(l)}</span><input type="text" data-label="${esc(l)}" spellcheck="false"></div>`)
    .join("");
  return `
    <h3>fill in ${esc(mode.p.value)}</h3>
    ${fields}
    <div class="srow"><button data-act="resolve-go">continue</button><button data-act="back">cancel</button></div>
    <div class="serr"></div>
    <div class="sfoot">these fill the prefix's &lt;user:…&gt; fields; the resolved name lands in the create-branch prompt, still editable · esc backs out</div>`;
}

function renderPrefixes() {
  if (!data) return;
  let body;
  switch (mode && mode.kind) {
    case "add":
      body = pfxAddHTML();
      break;
    case "labels":
      body = pfxLabelsHTML();
      break;
    default:
      body = pfxBrowseHTML();
  }
  const title = picking ? "use a branch prefix" : "branch prefixes";
  $("prefixes-box").innerHTML = `<h2>${title}</h2>${body}`;
  const first = $("prefixes-box").querySelector("input");
  if (first) {
    first.focus();
    first.select();
  }
}

// resolveAndFinish asks the server for the preview and hands it back to the
// pick-mode caller (who reopens the create prompt prefilled).
async function resolveAndFinish(p, inputs) {
  let out;
  try {
    out = await postJSON("/api/prefixes/resolve", { scope: p.scope, id: p.id, inputs });
  } catch (e) {
    showPfxErr("not resolved: " + e.message);
    return;
  }
  finishPick(out.resolved, p);
}

$("prefixes-box").addEventListener("click", (e) => {
  const t = e.target.closest("button");
  if (!t || t.disabled) return;
  const p = t.dataset.i != null ? data.prefixes[Number(t.dataset.i)] : null;
  switch (t.dataset.act) {
    case "back":
      mode = null;
      renderPrefixes();
      break;
    case "new-prefix":
      mode = { kind: "add" };
      renderPrefixes();
      break;
    case "save-prefix": {
      const value = $("x-value").value.trim();
      if (!value) {
        showPfxErr("a value is required");
        return;
      }
      postJSON("/api/prefixes", { value, scope: $("x-scope").dataset.scope })
        .then(reloadPrefixes)
        .catch((err) => showPfxErr("not saved: " + err.message));
      break;
    }
    case "delete-prefix":
      if (!p) return;
      postJSON("/api/prefixes/remove", { scope: p.scope, id: p.id })
        .then(reloadPrefixes)
        .catch((err) => showPfxErr("not deleted: " + err.message));
      break;
    case "pick":
      if (!p) return;
      if (p.user_labels && p.user_labels.length) {
        mode = { kind: "labels", p };
        renderPrefixes();
      } else {
        resolveAndFinish(p, {});
      }
      break;
    case "resolve-go": {
      const inputs = {};
      let missing = false;
      for (const inp of $("prefixes-box").querySelectorAll("input[data-label]")) {
        const v = inp.value.trim();
        if (!v) missing = true;
        inputs[inp.dataset.label] = v;
      }
      if (missing) {
        showPfxErr("every field is required");
        return;
      }
      resolveAndFinish(mode.p, inputs);
      break;
    }
    default:
      if (t.id === "x-scope") {
        const next = t.dataset.scope === "repo" ? "global" : "repo";
        t.dataset.scope = next;
        t.classList.toggle("on", next === "global");
        t.textContent = next === "repo" ? "this repo only" : "global (every repo)";
      }
  }
});

$("prefixes").addEventListener("click", (e) => {
  if (e.target === $("prefixes")) dismiss();
});

export { openPrefixPicker, openPrefixesView };
