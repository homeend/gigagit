// identity.js — the Identity & profiles overlay: the TUI identity view's
// browse / edit-identity / profile-form / apply surface over
// GET /api/identity, the profile CRUD endpoints and the set-identity op.
//
// Commit model: every form is explicit (save / apply buttons) — nothing
// writes on blur or keystroke, so there is no pending-edit machinery here;
// escape from a form goes back to browse and simply drops the typing.
import { $, esc, getJSON, postJSON } from "./core.js";
import { closeLayer, pushLayer } from "./layers.js";
import { startOp } from "./ops.js";

let data = null; // last GET /api/identity payload
// null = browse · {kind:"identity"} · {kind:"profile", renameFrom, renameScope, p}
// · {kind:"apply", name, email, label}
let mode = null;

async function openIdentityView() {
  try {
    data = await getJSON("/api/identity");
  } catch (e) {
    return;
  }
  mode = null;
  renderIdentity();
  pushLayer("identity", $("identity"), {
    onKey: (e) => {
      if (e.key === "Escape") {
        // A form's esc is "back", not "close" (the TUI convention): typing
        // is dropped, browse returns. Only browse-level esc leaves.
        if (mode) {
          mode = null;
          renderIdentity();
        } else {
          closeLayer("identity");
        }
        e.preventDefault();
        return true;
      }
      // Swallow WITHOUT preventDefault — app hotkeys stay off, inputs stay alive.
      return true;
    },
  });
}

async function reloadIdentity() {
  try {
    data = await getJSON("/api/identity");
  } catch (e) {
    showIdErr(e.message);
    return;
  }
  mode = null;
  renderIdentity();
}

function showIdErr(msg) {
  const err = $("identity-box").querySelector(".serr");
  if (err) err.textContent = msg;
}

const scopeTag = (s) => (s === "repo" ? "[this repo]" : "[global]");

function idRow(label, name, email, set, note) {
  const val = set ? `${esc(name)} &lt;${esc(email)}&gt;` : `<span class="snote">${esc(note || "(not set)")}</span>`;
  return `<div class="srow"><span class="slbl">${esc(label)}</span><span class="sval">${val}</span></div>`;
}

function browseHTML() {
  const id = data.identity;
  const repoNote = !id.local_set && id.global_set ? "(not set — inherits global)" : "(not set)";
  const effSet = !!(id.effective_name || id.effective_email);
  const rows = (data.profiles || [])
    .map(
      (p, i) => `
    <div class="srow prow">
      <span class="sval">${esc(p.name)}</span>
      <span class="snote">${esc(p.git_name)} &lt;${esc(p.git_email)}&gt;</span>
      <span class="snote pscope">${esc(scopeTag(p.scope))}</span>
      <span class="pbtns">
        <button data-act="apply" data-i="${i}">apply</button>
        <button data-act="edit-profile" data-i="${i}">edit</button>
        <button class="danger" data-act="delete-profile" data-i="${i}">delete</button>
      </span>
    </div>`
    )
    .join("");
  return `
    <h3>current identity</h3>
    ${idRow("global", id.global_name, id.global_email, id.global_set)}
    ${idRow("repo", id.local_name, id.local_email, id.local_set, repoNote)}
    ${idRow("effective", id.effective_name, id.effective_email, effSet)}
    <div class="srow"><button data-act="edit-identity">edit identity…</button></div>
    <h3>profiles</h3>
    ${rows || '<div class="srow"><span class="snote">(none yet)</span></div>'}
    <div class="srow"><button data-act="new-profile">new profile…</button></div>
    <div class="serr"></div>
    <div class="sfoot">apply writes user.name/email to the scope you pick · profiles are gg-local presets, not git config · esc closes</div>`;
}

function identityFormHTML() {
  const id = data.identity;
  return `
    <h3>edit identity</h3>
    <div class="srow"><span class="slbl">name</span><input type="text" id="i-name" value="${esc(id.effective_name || "")}" spellcheck="false"></div>
    <div class="srow"><span class="slbl">email</span><input type="text" id="i-email" value="${esc(id.effective_email || "")}" spellcheck="false"></div>
    <div class="srow"><button data-act="identity-next">choose scope…</button><button data-act="back">cancel</button></div>
    <div class="serr"></div>
    <div class="sfoot">nothing is written until you pick the scope on the next step · esc backs out</div>`;
}

function profileFormHTML() {
  const p = mode.p || { name: "", git_name: "", git_email: "", scope: "global" };
  return `
    <h3>${mode.renameFrom ? "edit profile" : "new profile"}</h3>
    <div class="srow"><span class="slbl">name</span><input type="text" id="i-plabel" value="${esc(p.name)}" spellcheck="false"></div>
    <div class="srow"><span class="slbl">git name</span><input type="text" id="i-pname" value="${esc(p.git_name)}" spellcheck="false"></div>
    <div class="srow"><span class="slbl">git email</span><input type="text" id="i-pemail" value="${esc(p.git_email)}" spellcheck="false"></div>
    <div class="srow"><span class="slbl">scope</span><button class="stgl${p.scope === "repo" ? "" : " on"}" id="i-scope" data-scope="${esc(p.scope)}">${p.scope === "repo" ? "this repo only" : "global (every repo)"}</button></div>
    <div class="srow"><button data-act="save-profile">save</button><button data-act="back">cancel</button></div>
    <div class="serr"></div>
    <div class="sfoot">a profile is a named preset — applying one later writes it to git config · esc backs out</div>`;
}

function applyHTML() {
  return `
    <h3>apply identity</h3>
    <div class="srow"><span class="sval">${esc(mode.name)} &lt;${esc(mode.email)}&gt;</span></div>
    <div class="srow"><span class="snote">from: ${esc(mode.label)}</span></div>
    <div class="srow"><button data-act="apply-repo">to this repo</button><button data-act="apply-global">globally</button><button data-act="back">back</button></div>
    <div class="serr"></div>
    <div class="sfoot">writes user.name and user.email to the chosen git config scope · esc backs out</div>`;
}

function renderIdentity() {
  if (!data) return;
  let body;
  switch (mode && mode.kind) {
    case "identity":
      body = identityFormHTML();
      break;
    case "profile":
      body = profileFormHTML();
      break;
    case "apply":
      body = applyHTML();
      break;
    default:
      body = browseHTML();
  }
  $("identity-box").innerHTML = `<h2>identity &amp; profiles</h2>${body}`;
  const first = $("identity-box").querySelector("input");
  if (first) {
    first.focus();
    first.select();
  }
}

$("identity-box").addEventListener("click", (e) => {
  const t = e.target.closest("button");
  if (!t || t.disabled) return;
  const p = t.dataset.i != null ? data.profiles[Number(t.dataset.i)] : null;
  switch (t.dataset.act) {
    case "back":
      mode = null;
      renderIdentity();
      break;
    case "edit-identity":
      mode = { kind: "identity" };
      renderIdentity();
      break;
    case "identity-next": {
      const name = $("i-name").value.trim();
      const email = $("i-email").value.trim();
      if (!name || !email) {
        showIdErr("name and email are required");
        return;
      }
      mode = { kind: "apply", name, email, label: "edited identity" };
      renderIdentity();
      break;
    }
    case "new-profile":
      mode = { kind: "profile", renameFrom: "", renameScope: "" };
      renderIdentity();
      break;
    case "edit-profile":
      if (!p) return;
      mode = { kind: "profile", renameFrom: p.id, renameScope: p.scope, p };
      renderIdentity();
      break;
    case "delete-profile":
      if (!p) return;
      postJSON("/api/profiles/remove", { scope: p.scope, id: p.id })
        .then(reloadIdentity)
        .catch((err) => showIdErr("not deleted: " + err.message));
      break;
    case "save-profile": {
      const body = {
        name: $("i-plabel").value.trim(),
        git_name: $("i-pname").value.trim(),
        git_email: $("i-pemail").value.trim(),
        scope: $("i-scope").dataset.scope,
      };
      if (!body.name || !body.git_name || !body.git_email) {
        showIdErr("profile name, git name and email are required");
        return;
      }
      if (mode.renameFrom) {
        body.rename_from = mode.renameFrom;
        body.rename_scope = mode.renameScope;
      }
      postJSON("/api/profiles", body)
        .then(reloadIdentity)
        .catch((err) => showIdErr("not saved: " + err.message));
      break;
    }
    case "apply":
      if (!p) return;
      mode = { kind: "apply", name: p.git_name, email: p.git_email, label: p.name };
      renderIdentity();
      break;
    case "apply-repo":
    case "apply-global":
      // Close first (the commit-graph precedent): the op line narrates the
      // write; reopening re-reads the updated identity.
      closeLayer("identity");
      startOp(
        { op: "set-identity", name: mode.name, email: mode.email, global: t.dataset.act === "apply-global" },
        "setting identity"
      );
      break;
    default:
      if (t.id === "i-scope") {
        const next = t.dataset.scope === "repo" ? "global" : "repo";
        t.dataset.scope = next;
        t.classList.toggle("on", next === "global");
        t.textContent = next === "repo" ? "this repo only" : "global (every repo)";
      }
  }
});

// Backdrop click closes (the settings convention — forms warn nothing here;
// they are explicit-save, so a stray click can only drop unsaved typing).
$("identity").addEventListener("click", (e) => {
  if (e.target === $("identity")) closeLayer("identity");
});

export { openIdentityView };
