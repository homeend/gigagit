// texttemplates.js — text templates (the TUI's alt+x window): titled,
// multi-line texts with the branch-prefix tokens, global or per repo.
//   - browse: the title list (8 rows, scrolling), the selected text, the
//     variables it asks for.
//   - fill: one single-line field per <user:…> label, then the server
//     renders a PREVIEW (seq counters peeked).
//   - rendered: the resolved text; Copy puts it on the clipboard, consumes
//     the counters (/take) and closes the overlay — the TUI's y.
//   - form: add / edit with the text in a textarea (the TUI hands it to
//     $EDITOR). The scope is fixed once a template exists.
// Every text is inserted with textContent or esc(): a body is arbitrary
// user text.
import { $, esc, getJSON, postJSON } from "./core.js";
import { closeLayer, pushLayer } from "./layers.js";
import { opLine } from "./ops.js";

let data = null; // last GET /api/text-templates payload
let sel = 0; // index into data.templates
// null = browse · {kind:"fill", t} · {kind:"rendering", t} (a render is in
// flight: keys are swallowed, Escape gives up on it) · {kind:"rendered", t, text}
// · {kind:"form", t|null, seed, warned, err, note} (seed = the form's content
// when it opened, warned = its content when the unsaved-text notice was last
// shown, err = a pending error, note = the notice is showing) · {kind:"confirm", t}
let mode = null;

const scopeTag = (s) => (s === "repo" ? "[this repo]" : "[global]");
const rows = () => (data && data.templates) || [];
const current = () => rows()[sel] || null;
const inField = (e) => !!(e.target.closest && e.target.closest("input,textarea"));

async function openTextTemplates() {
  try {
    data = await getJSON("/api/text-templates");
  } catch (e) {
    opLine("text templates: " + e.message, true);
    return;
  }
  sel = Math.min(sel, Math.max(rows().length - 1, 0));
  mode = null;
  render();
  pushLayer("texttemplates", $("texttemplates"), { onKey });
  scopeError();
}

function close() {
  // No step survives the overlay: a late answer must not land in one.
  mode = null;
  closeLayer("texttemplates");
}

// ttFormContent is what counts as a form's unsaved text, as one comparable
// value: the title and the text. The scope switch alone is nothing to lose.
function ttFormContent(title, body) {
  return JSON.stringify([title, body]);
}

// ttLeaveForm decides a way out of the add/edit form: "leave" for an
// untouched form (now is still the seed) and for one left a second time
// without a change since the notice (now is what was warned about), else
// "warn" — keep the form and show the notice.
function ttLeaveForm(seed, warned, now) {
  return now === seed || now === warned ? "leave" : "warn";
}

// ttFormNotice is the form's notice line: a pending error stays readable
// beside the unsaved-text notice, which names no key — Escape, the cancel
// button and a click outside all leave.
function ttFormNotice(err, unsaved) {
  const note = unsaved ? "unsaved text — leaving again discards it" : "";
  return err && note ? err + " · " + note : err || note;
}

// ttConfirmKey maps a key to the delete question's answer: only y deletes,
// only n cancels (Escape backs out of every step before this is asked);
// "" = not an answer (a bare Shift, Tab, Enter).
function ttConfirmKey(key) {
  if (key === "y") return "delete";
  if (key === "n") return "cancel";
  return "";
}

// ttSelectIndex is the row a reloaded list lands on: the one (id, scope)
// names — the same id may exist in both scopes — else the previous index,
// clamped to the list.
function ttSelectIndex(list, id, scope, prev) {
  const at = list.findIndex((t) => t.id === id && t.scope === scope);
  return at >= 0 ? at : Math.min(prev, Math.max(list.length - 1, 0));
}

// ttAnswerLanding is where a late answer goes — a save, a delete, the list
// reload after either. The step that asked may be gone by then (the user
// left, and may be writing another template):
//   "step"      — the asking step is still the one open: act in it
//   "browse"    — the user is back at the list: it may be redrawn
//   "elsewhere" — another step is open: nothing on screen is touched
function ttAnswerLanding(sameStep, browsing) {
  if (sameStep) return "step";
  return browsing ? "browse" : "elsewhere";
}

// formContent is what the open form holds (ttFormContent).
function formContent() {
  return ttFormContent($("tt-title").value, $("tt-body").value);
}

// canLeaveForm guards every way out of the add/edit form except save. An
// untouched form may go at once. A changed one is kept the first time, with a
// notice; leaving again without touching the text discards it.
function canLeaveForm() {
  if (!mode || mode.kind !== "form") return true;
  const now = formContent();
  if (ttLeaveForm(mode.seed, mode.warned, now) === "leave") return true;
  mode.warned = now;
  mode.note = true;
  paintFormNotice();
  return false;
}

// paintFormNotice writes the form's pending error and unsaved-text notice.
function paintFormNotice() {
  const err = $("texttemplates-box").querySelector(".serr");
  if (err) err.textContent = ttFormNotice(mode.err || "", !!mode.note);
}

// back leaves the current step for the list.
function back() {
  if (!canLeaveForm()) return;
  mode = null;
  render();
}

// onKey owns the keyboard while the overlay is open. Typed text stays in its
// field: only Escape (and Enter in a one-line field) act from inside one.
function onKey(e) {
  if (e.key === "Escape") {
    if (mode) back();
    else close();
    e.preventDefault();
    return true;
  }
  if (inField(e)) {
    if (e.key === "Enter" && e.target.tagName === "INPUT") {
      if (mode && mode.kind === "fill") submitFill();
      e.preventDefault();
    }
    return true;
  }
  if (e.ctrlKey || e.metaKey || e.altKey) return true;
  if (mode && mode.kind === "rendered") {
    if (e.key === "y") copyRendered();
    return true;
  }
  if (mode && mode.kind === "confirm") {
    // A stray key — a bare Shift, Tab — must not answer the question.
    const answer = ttConfirmKey(e.key);
    if (answer === "delete") removeTemplate(mode.t);
    else if (answer === "cancel") back();
    return true;
  }
  if (mode) return true;
  switch (e.key) {
    case "ArrowDown":
    case "j":
      move(1);
      break;
    case "ArrowUp":
    case "k":
      move(-1);
      break;
    case "Enter":
      startFill();
      break;
    case "n":
      mode = { kind: "form", t: null };
      render();
      break;
    case "e":
      if (current()) {
        mode = { kind: "form", t: current() };
        render();
      }
      break;
    case "d":
      if (current()) {
        mode = { kind: "confirm", t: current() };
        render();
      }
      break;
    default:
      return true;
  }
  e.preventDefault();
  return true;
}

function move(d) {
  const n = rows().length;
  if (!n) return;
  sel = Math.max(0, Math.min(sel + d, n - 1));
  render();
}

function showErr(msg) {
  if (mode && mode.kind === "form") {
    // The form's line also carries the unsaved-text notice.
    mode.err = msg;
    paintFormNotice();
    return;
  }
  const err = $("texttemplates-box").querySelector(".serr");
  if (err) err.textContent = msg;
}

// landing is ttAnswerLanding for the step object that asked.
function landing(from) {
  return ttAnswerLanding(mode === from, !mode);
}

// reload refetches the list for the step `from` (a saved form, an answered
// delete question) and selects the row (id, scope) names — the same id may
// exist in both scopes. With another step open by then, only the list is
// replaced: that step, and what is typed in it, stays.
async function reload(selectId, selectScope, from) {
  const was = current();
  let next;
  try {
    next = await getJSON("/api/text-templates");
  } catch (e) {
    if (landing(from) === "step") showErr(e.message);
    else opLine("text templates: " + e.message, true);
    return;
  }
  data = next;
  if (landing(from) === "elsewhere") {
    sel = ttSelectIndex(rows(), was ? was.id : "", was ? was.scope : "", sel);
    return;
  }
  sel = ttSelectIndex(rows(), selectId, selectScope, sel);
  mode = null;
  render();
  scopeError();
}

// scopeError reports a scope whose file could not be read (the list still
// holds the other scope's rows).
function scopeError() {
  if (data && data.error) showErr(data.error);
}

// startFill asks for the variables, or renders at once when there are none.
function startFill() {
  const t = current();
  if (!t) return;
  if (t.user_labels.length) {
    mode = { kind: "fill", t };
    render();
  } else {
    renderTemplate(t, {});
  }
}

function submitFill() {
  const inputs = {};
  for (const inp of $("texttemplates-box").querySelectorAll("input[data-label]")) inputs[inp.dataset.label] = inp.value;
  renderTemplate(mode.t, inputs);
}

async function renderTemplate(t, inputs) {
  // The wait is its own mode: a result lands only if the overlay still
  // waits for THIS request (Escape, or anything that replaced the mode,
  // drops it instead of yanking the user into the rendered view).
  const back = mode;
  const waiting = { kind: "rendering", t };
  mode = waiting;
  let out;
  try {
    out = await postJSON("/api/text-templates/render", { id: t.id, scope: t.scope, inputs });
  } catch (e) {
    if (mode !== waiting) return;
    mode = back;
    showErr("not rendered: " + e.message);
    return;
  }
  if (mode !== waiting) return;
  mode = { kind: "rendered", t, text: out.text };
  render();
}

// copyRendered is the TUI's y: the text is taken — copy it, consume its
// counters, close. A failed copy keeps the overlay open so the text is not lost.
function copyRendered() {
  const m = mode;
  navigator.clipboard.writeText(m.text).then(
    () => {
      close();
      opLine("copied the rendered text");
      postJSON("/api/text-templates/take", { id: m.t.id, scope: m.t.scope }).catch((err) =>
        opLine("copied, but its <seq> counters did not advance: " + err.message, true),
      );
    },
    () => showErr("copy failed (clipboard unavailable) — select the text and copy it by hand"),
  );
}

function saveForm() {
  const title = $("tt-title").value.trim();
  const body = $("tt-body").value;
  if (!title) {
    showErr("a title is required");
    return;
  }
  if (!body.trim()) {
    showErr("the text is empty");
    return;
  }
  const form = mode;
  if (form.saving) return; // one save at a time: a second would be refused as a duplicate
  form.saving = true;
  const t = form.t;
  const req = t
    ? postJSON("/api/text-templates/update", { id: t.id, scope: t.scope, title, body })
    : postJSON("/api/text-templates", { title, body, scope: $("tt-scope").dataset.scope });
  req.then(
    (row) => reload(row.id, row.scope, form),
    (err) => {
      form.saving = false;
      if (landing(form) === "step") showErr("not saved: " + err.message);
      else opLine("text template " + title + " not saved: " + err.message, true);
    },
  );
}

function removeTemplate(t) {
  const asked = mode;
  postJSON("/api/text-templates/remove", { id: t.id, scope: t.scope }).then(
    () => reload("", "", asked),
    (err) => {
      if (landing(asked) !== "step") {
        opLine("text template " + t.title + " not deleted: " + err.message, true);
        return;
      }
      mode = null;
      render();
      showErr("not deleted: " + err.message);
    },
  );
}

function browseHTML() {
  const list = rows()
    .map(
      (t, i) => `
    <div class="ttrow${i === sel ? " sel" : ""}" data-i="${i}">
      <span class="tttitle">${esc(t.title)}</span>
      <span class="snote">${esc(scopeTag(t.scope))}</span>
    </div>`,
    )
    .join("");
  const t = current();
  if (!t) {
    return `
    <div class="srow"><span class="snote">(none yet)</span></div>
    <div class="srow"><button data-act="new">new template…</button></div>
    <div class="serr"></div>
    <div class="sfoot">a text template is a reusable multi-line text with &lt;user:LABEL&gt;, &lt;date&gt;, &lt;branch&gt;, &lt;seq:NAME&gt; … tokens · n adds · esc closes</div>`;
  }
  const vars = t.user_labels.length ? `<div class="ttvars">Variables: ${esc(t.user_labels.join(" · "))}</div>` : "";
  const auto = t.automatic.length ? `<div class="ttvars">Automatic: ${esc(t.automatic.join(" · "))}</div>` : "";
  return `
    <div class="ttlist">${list}</div>
    <pre class="ttbody" id="tt-text"></pre>
    ${vars}${auto}
    <div class="srow">
      <button data-act="fill">fill…</button><button data-act="new">new template…</button>
      <button data-act="edit">edit…</button><button class="danger" data-act="delete">delete</button>
    </div>
    <div class="serr"></div>
    <div class="sfoot">↑/↓ select · enter fills the variables and shows the rendered text · n add · e edit · d delete · esc closes</div>`;
}

function fillHTML() {
  const fields = mode.t.user_labels
    .map((l) => `<div class="srow"><span class="slbl">${esc(l)}</span><input type="text" data-label="${esc(l)}" spellcheck="false"></div>`)
    .join("");
  return `
    <h3>${esc(mode.t.title)} — fill variables</h3>
    ${fields}
    <div class="srow"><button data-act="render">render</button><button data-act="back">cancel</button></div>
    <div class="serr"></div>
    <div class="sfoot">these fill the template's &lt;user:…&gt; fields · enter renders · esc backs out</div>`;
}

function renderedHTML() {
  return `
    <h3>${esc(mode.t.title)} — rendered</h3>
    <pre class="ttbody" id="tt-text"></pre>
    <div class="srow"><button data-act="copy">copy and close</button><button data-act="back">back to templates</button></div>
    <div class="serr"></div>
    <div class="sfoot">y copies the text and closes · &lt;seq&gt; counters advance only when the text is copied · esc goes back</div>`;
}

function formHTML() {
  const t = mode.t;
  const scope = t ? t.scope : "global";
  const scopeText = scope === "repo" ? "this repo only" : "global (every repo)";
  return `
    <h3>${t ? "edit text template" : "new text template"}</h3>
    <div class="srow"><span class="slbl">title</span><input type="text" id="tt-title" maxlength="80" spellcheck="false"></div>
    <div class="srow"><span class="slbl">scope</span><button class="stgl${scope === "global" ? " on" : ""}" id="tt-scope" data-scope="${scope}"${t ? " disabled" : ""}>${scopeText}</button></div>
    <textarea id="tt-body" spellcheck="false"></textarea>
    <div class="srow"><button data-act="save">save</button><button data-act="back">cancel</button></div>
    <div class="serr"></div>
    <div class="sfoot">tokens: &lt;user:LABEL&gt; &lt;date&gt; &lt;date:FMT&gt; &lt;branch&gt; &lt;parent-branch&gt; &lt;repo&gt; &lt;seq:NAME:N&gt; &lt;random-alpha:N&gt; · any other &lt;…&gt; stays as written · esc backs out</div>`;
}

function confirmHTML() {
  return `
    <h3>delete text template ${esc(mode.t.title)}?</h3>
    <div class="srow"><button class="danger" data-act="delete-yes">delete</button><button data-act="back">keep</button></div>
    <div class="serr"></div>`;
}

function render() {
  if (!data) return;
  if (mode && mode.kind === "rendering") return; // keep the screen while waiting
  const box = $("texttemplates-box");
  let body;
  switch (mode && mode.kind) {
    case "fill":
      body = fillHTML();
      break;
    case "rendered":
      body = renderedHTML();
      break;
    case "form":
      body = formHTML();
      break;
    case "confirm":
      body = confirmHTML();
      break;
    default:
      body = browseHTML();
  }
  box.innerHTML = `<h2>text templates</h2>${body}`;
  // Texts land through the DOM, never through markup.
  const pre = box.querySelector("#tt-text");
  if (pre) pre.textContent = mode && mode.kind === "rendered" ? mode.text : current() ? current().body : "";
  if (mode && mode.kind === "form") {
    $("tt-title").value = mode.t ? mode.t.title : "";
    $("tt-body").value = mode.t ? mode.t.body : "";
    mode.seed = formContent();
  }
  const first = box.querySelector("input");
  if (first) {
    first.focus();
    first.select();
  }
  const row = box.querySelector(".ttrow.sel");
  if (row) row.scrollIntoView({ block: "nearest" });
}

$("texttemplates-box").addEventListener("click", (e) => {
  const rowEl = e.target.closest(".ttrow");
  if (rowEl) {
    sel = Number(rowEl.dataset.i);
    render();
    return;
  }
  const t = e.target.closest("button");
  if (!t || t.disabled) return;
  switch (t.dataset.act) {
    case "back":
      back();
      break;
    case "fill":
      startFill();
      break;
    case "render":
      submitFill();
      break;
    case "copy":
      copyRendered();
      break;
    case "new":
      mode = { kind: "form", t: null };
      render();
      break;
    case "edit":
      if (current()) {
        mode = { kind: "form", t: current() };
        render();
      }
      break;
    case "delete":
      if (current()) {
        mode = { kind: "confirm", t: current() };
        render();
      }
      break;
    case "delete-yes":
      removeTemplate(mode.t);
      break;
    case "save":
      saveForm();
      break;
    default:
      if (t.id === "tt-scope") {
        const next = t.dataset.scope === "repo" ? "global" : "repo";
        t.dataset.scope = next;
        t.classList.toggle("on", next === "global");
        t.textContent = next === "repo" ? "this repo only" : "global (every repo)";
      }
  }
});

// Typing after the notice makes it stale: the changed text is asked about
// again. A pending error stays.
$("texttemplates-box").addEventListener("input", () => {
  if (mode && mode.kind === "form" && mode.note) {
    mode.note = false;
    mode.warned = undefined;
    paintFormNotice();
  }
});

$("texttemplates-box").addEventListener("dblclick", (e) => {
  if (e.target.closest(".ttrow")) startFill();
});

$("texttemplates").addEventListener("click", (e) => {
  if (e.target === $("texttemplates") && canLeaveForm()) close();
});

// textTemplatesKey opens the overlay on alt+x and reports whether it took
// the key. keys.js calls it only outside a form field.
function textTemplatesKey(e) {
  if (!e.altKey || e.ctrlKey || e.metaKey || e.shiftKey || e.code !== "KeyX") return false;
  e.preventDefault();
  openTextTemplates();
  return true;
}

export { openTextTemplates, textTemplatesKey };
