// memdoc.js — ctrl+\'s n: a title and a text become a memory, plain document of
// the worktree on screen (the TUI's memdocPopup; spec 2026-11-02-memory-docs).
import { postJSON } from "./core.js";
import { closeLayer, mountOverlay, pushLayer } from "./layers.js";

const box = mountOverlay("memdoc");
box.innerHTML =
  `<div id="memdoc-box"><div class="mtitle">New text (in memory, gone when gg exits)</div>` +
  `<input id="memdoc-title" placeholder="title" maxlength="200" spellcheck="false">` +
  `<textarea id="memdoc-text" placeholder="text" spellcheck="false"></textarea>` +
  `<div class="merr" id="memdoc-err"></div><div class="mfoot" id="memdoc-foot"></div></div>`;
const titleEl = box.querySelector("#memdoc-title");
const textEl = box.querySelector("#memdoc-text");
const errEl = box.querySelector("#memdoc-err");
const footEl = box.querySelector("#memdoc-foot");
const FOOT = "ctrl+s save · tab next field · esc cancel";
let done = null; // called with the new document's id
let asked = false; // esc with text typed: the next esc leaves
let saving = false;

function foot() {
  footEl.textContent = asked ? "ctrl+s save · esc again: discard what you typed" : FOOT;
}

async function save() {
  if (saving) return;
  saving = true;
  errEl.textContent = "";
  try {
    const got = await postJSON("/api/session-overview/memory", { title: titleEl.value, text: textEl.value });
    const then = done;
    closeLayer("memdoc");
    if (then) then(got.id);
  } catch (e) {
    errEl.textContent = e.message || String(e); // the window and its text stay
  } finally {
    saving = false;
  }
}

function memdocKey(e) {
  if (e.key === "Escape") {
    e.preventDefault();
    if ((titleEl.value.trim() || textEl.value.trim()) && !asked) {
      asked = true;
      foot();
      return true;
    }
    closeLayer("memdoc");
    return true;
  }
  if ((e.ctrlKey || e.metaKey) && e.key === "s") {
    e.preventDefault();
    save();
    return true;
  }
  if (asked) {
    asked = false;
    foot();
  }
  return true; // the fields take it: the layer owns the keyboard
}

// openMemdocDialog opens the dialog; then(id) runs once a document was made.
export function openMemdocDialog(then) {
  done = then;
  asked = false;
  titleEl.value = "";
  textEl.value = "";
  errEl.textContent = "";
  foot();
  pushLayer("memdoc", box, { onKey: memdocKey });
  titleEl.focus();
}

