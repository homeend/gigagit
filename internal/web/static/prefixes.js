// prefixes.js — the Branch prefixes overlay: manage the templated
// branch-name skeletons (global + repo scopes) and start a new branch from
// one (the TUI create-branch popup's ctrl+p lane, inverted: pick the prefix
// here, fill its <user:…> labels, and the create-branch prompt opens
// prefilled with the resolved name — editable, like the TUI seeds its name
// field). The resolve is a server-side PREVIEW (seq counters peeked); the
// counters are consumed only when the create succeeds, because the submit
// carries the picked prefix's identity.
import { $, esc, getJSON, postJSON } from "./core.js";
import { closeLayer, openPrompt, pushLayer } from "./layers.js";
import { startOp } from "./ops.js";

let data = null; // last GET /api/prefixes payload
// null = browse · {kind:"add"} · {kind:"labels", p} (collecting <user:…> inputs)
let mode = null;

async function openPrefixesView() {
  try {
    data = await getJSON("/api/prefixes");
  } catch (e) {
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
          closeLayer("prefixes");
        }
        e.preventDefault();
        return true;
      }
      return true; // swallow WITHOUT preventDefault — inputs stay alive
    },
  });
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
  const rows = (data.prefixes || [])
    .map(
      (p, i) => `
    <div class="srow prow">
      <span class="sval pfxval">${esc(p.value)}</span>
      <span class="snote pscope">${esc(pfxScopeTag(p.scope))}</span>
      <span class="pbtns">
        <button data-act="branch-from" data-i="${i}">new branch…</button>
        <button class="danger" data-act="delete-prefix" data-i="${i}">delete</button>
      </span>
    </div>`
    )
    .join("");
  return `
    ${rows || '<div class="srow"><span class="snote">(none yet)</span></div>'}
    <div class="srow"><button data-act="new-prefix">new prefix…</button></div>
    <div class="serr"></div>
    <div class="sfoot">tokens: &lt;user:LABEL&gt; &lt;seq:NAME:N&gt; &lt;date&gt; &lt;parent-branch&gt; &lt;repo&gt; &lt;random-alpha:N&gt; · <b>new branch…</b> opens the create prompt prefilled with the resolved name · esc closes</div>`;
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
  $("prefixes-box").innerHTML = `<h2>branch prefixes</h2>${body}`;
  const first = $("prefixes-box").querySelector("input");
  if (first) {
    first.focus();
    first.select();
  }
}

// resolveAndPrompt asks the server for the preview, closes the overlay and
// opens the create-branch prompt seeded with it. The submitted op carries the
// prefix identity so the server can bump its seq counters on success.
async function resolveAndPrompt(p, inputs) {
  let out;
  try {
    out = await postJSON("/api/prefixes/resolve", { scope: p.scope, id: p.id, inputs });
  } catch (e) {
    showPfxErr("not resolved: " + e.message);
    return;
  }
  closeLayer("prefixes");
  openPrompt({
    title: "New branch, starting at the current HEAD:",
    value: out.resolved,
    placeholder: "branch name",
    onSubmit: (name) => startOp({ op: "create-branch", name, prefix_id: p.id, prefix_scope: p.scope }, "creating " + name),
  });
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
    case "branch-from":
      if (!p) return;
      if (p.user_labels && p.user_labels.length) {
        mode = { kind: "labels", p };
        renderPrefixes();
      } else {
        resolveAndPrompt(p, {});
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
      resolveAndPrompt(mode.p, inputs);
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
  if (e.target === $("prefixes")) closeLayer("prefixes");
});

export { openPrefixesView };
