// layers.js — part of gg's web client. Split from the original app.js;
// see app.js (the entry module) for the load order.
import { $, esc } from "./core.js";
import { opLine } from "./ops.js";

// --- overlay layer stack ---
// Every overlay surface (decision modal, help, ctx-menu, future popups)
// registers here. One rule: a non-empty stack owns the keyboard — the top
// layer's onKey sees the event first; an unhandled Escape closes the top
// layer. closeLayer(id) removes a layer WHEREVER it sits in the stack:
// the op transport must be able to close a parked decision modal even
// under an open help overlay.
const layers = [];


function pushLayer(id, el, opts) {
  if (layers.some((l) => l.id === id)) return; // one instance per surface
  el.classList.remove("hidden");
  layers.push({ id, el, onKey: (opts && opts.onKey) || null });
}


function closeLayer(id) {
  const i = layers.findIndex((l) => l.id === id);
  if (i < 0) return; // idempotent
  const [l] = layers.splice(i, 1);
  l.el.classList.add("hidden");
}


function topLayer() {
  return layers[layers.length - 1] || null;
}


function hideCtxMenu() {
  closeLayer("ctx");
  // Emptied, not just hidden: a closed menu that still holds its last rows
  // reads as open to anything inspecting the DOM — which is exactly how a
  // browser check of the versions popup passed while the menu was in fact
  // being closed in the same event it opened.
  const menu = $("ctx-menu");
  menu.innerHTML = "";
  menu._items = null;
}


// showCtxMenu renders the shared right-click menu at (x,y): safe actions
// first; rows flagged danger render red. A row with sep:true renders as a
// non-clickable divider (it occupies an index in _items, which the click
// handler resolves by data-i, so alignment is preserved).
function showCtxMenu(items, x, y) {
  const menu = $("ctx-menu");
  menu._items = items;
  menu.innerHTML = items
    .map((it, i) => {
      if (it.sep) return `<div class="sep"></div>`;
      if (it.header) return `<div class="ctx-header">${esc(it.header)}</div>`; // non-clickable group label
      return `<button data-i="${i}"${it.danger ? ' class="danger"' : ""}>${esc(it.label)}</button>`;
    })
    .join("");
  menu.style.left = Math.min(x, window.innerWidth - 200) + "px";
  menu.style.top = Math.min(y, window.innerHeight - 120) + "px";
  pushLayer("ctx", menu, {
    onKey: (e) => {
      if (e.key === "Escape") closeLayer("ctx");
      return true; // swallowed without preventDefault (today's behavior)
    },
  });
}


$("ctx-menu").addEventListener("click", (e) => {
  const btn = e.target.closest("button");
  const menu = $("ctx-menu");
  if (btn && menu._items) menu._items[Number(btn.dataset.i)].act();
  hideCtxMenu();
});

document.addEventListener("click", (e) => {
  if (!e.target.closest("#ctx-menu")) hideCtxMenu();
});


// A clipboard write is otherwise silent — you cannot tell a success from a
// no-op without pasting. `what` names what landed (the TUI reports the same
// way); it defaults to the copied text.
function copyText(text, what) {
  navigator.clipboard.writeText(text).then(
    () => opLine("copied " + (what || text)),
    () => opLine("copy failed (clipboard unavailable)", true),
  );
}


// --- one-line prompt ---
// A name or a path cannot come from a menu row, so this is the shared way to
// ask for one: a layer like any other (esc closes it, the top layer owns the
// keyboard). onSubmit receives the TRIMMED value and is never called with an
// empty string — every caller would have had to check.
let promptCb = null;


function openPrompt({ title, value, placeholder, onSubmit }) {
  promptCb = onSubmit;
  $("prompt-title").textContent = title;
  const input = $("prompt-input");
  input.value = value || "";
  input.placeholder = placeholder || "";
  pushLayer("prompt", $("prompt"), { onKey: promptKey });
  input.focus();
  input.select();
}


function closePrompt() {
  promptCb = null;
  // Blur before closing: the form-field guard keys off the focused element,
  // and a still-focused input would swallow every global key (the palette's
  // hard-won lesson).
  $("prompt-input").blur();
  closeLayer("prompt");
}


function submitPrompt() {
  const v = $("prompt-input").value.trim();
  if (!v) return; // nothing to submit; leave the prompt open
  const cb = promptCb; // capture before closing clears it
  closePrompt();
  if (cb) cb(v);
}


function promptKey(e) {
  if (e.key === "Enter") {
    e.preventDefault();
    submitPrompt();
    return true;
  }
  if (e.key === "Escape") {
    closePrompt();
    return true;
  }
  return false; // everything else is typing
}

$("prompt-ok").addEventListener("click", submitPrompt);


$("help").addEventListener("click", () => closeLayer("help"));

$("help-box").addEventListener("click", (e) => e.stopPropagation()); // allow selecting/copying text
export { closeLayer, closePrompt, copyText, hideCtxMenu, layers, openPrompt, promptCb, promptKey, pushLayer, showCtxMenu, submitPrompt, topLayer };
