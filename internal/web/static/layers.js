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


// mountOverlay returns the container for a full-screen surface, creating it if
// this is the first call. A feature can then build its own view without adding
// markup to index.html — which is what keeps several features out of one
// another's merges. The element starts hidden and is otherwise an ordinary
// layer: pushLayer/closeLayer drive it like every other overlay.
function mountOverlay(id) {
  let el = document.getElementById(id);
  if (!el) {
    el = document.createElement("div");
    el.id = id;
    el.className = "hidden";
    document.body.append(el);
  }
  return el;
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


// A menu is written as ONE list with {sep:true} wherever a group ends, and
// rows that may not materialize (the delete row on a checked-out branch, the
// remove row on the served worktree) leave their separator behind. Rather than
// make every caller track that, the seps are cleaned up here: no leading one,
// no trailing one, never two in a row. Callers therefore push a separator
// before a group unconditionally.
//
// A menu that carries no separators of its own still gets one above its first
// red row — a destructive action must never sit flush against the row above it.
function groupItems(items) {
  const authored = items.some((it) => it.sep);
  const out = [];
  items.forEach((it, i) => {
    if (it.sep) {
      if (out.length && !out[out.length - 1].sep) out.push(it);
      return;
    }
    const prev = items[i - 1];
    if (!authored && it.danger && prev && !prev.danger && !prev.sep && !prev.header && out.length) {
      out.push({ sep: true });
    }
    out.push(it);
  });
  while (out.length && out[out.length - 1].sep) out.pop();
  return out;
}


// showCtxMenu renders the shared right-click menu at (x,y): safe actions
// first; rows flagged danger render red. A row with sep:true renders as a
// non-clickable divider (it occupies an index in _items, which the click
// handler resolves by data-i, so alignment is preserved).
function showCtxMenu(rows, x, y) {
  const menu = $("ctx-menu");
  const items = groupItems(rows);
  menu._items = items;
  menu.innerHTML = items
    .map((it, i) => {
      if (it.sep) return `<div class="sep"></div>`;
      if (it.header) return `<div class="ctx-header">${esc(it.header)}</div>`; // non-clickable group label
      return `<button data-i="${i}"${it.danger ? ' class="danger"' : ""}>${esc(it.label)}</button>`;
    })
    .join("");
  // Placement is MEASURED, not guessed: a fixed "reserve 120px at the bottom"
  // cut the taller menus off against the viewport floor. The menu has to be
  // visible to have a size, and it has to sit at the origin while measured —
  // a leftover far-right position from the previous open would wrap its rows
  // and inflate the height we then place by.
  menu.style.left = "0px";
  menu.style.top = "0px";
  pushLayer("ctx", menu, {
    onKey: (e) => {
      if (e.key === "Escape") closeLayer("ctx");
      return true; // swallowed without preventDefault (today's behavior)
    },
  });
  placeCtxMenu(menu, x, y);
}


// placeCtxMenu keeps the whole menu on screen: it opens at the pointer, is
// pulled back when its far edge would leave the viewport, and never goes past
// the top-left margin. A menu taller than the window scrolls (the CSS
// max-height) instead of running off the bottom.
const CTX_MARGIN = 8;

function placeCtxMenu(menu, x, y) {
  const w = menu.offsetWidth;
  const h = menu.offsetHeight;
  const left = Math.max(CTX_MARGIN, Math.min(x, window.innerWidth - w - CTX_MARGIN));
  const top = Math.max(CTX_MARGIN, Math.min(y, window.innerHeight - h - CTX_MARGIN));
  menu.style.left = left + "px";
  menu.style.top = top + "px";
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
let promptExtraCb = null;

// promptField is whichever control the open prompt is using: the one-line
// input, or the textarea a MULTILINE prompt swaps in (reword, where retyping
// a message to fix one word is how a commit body gets lost). Everything else
// — submit, cancel, the extra button, the layer stack — is shared.
function promptField() {
  return $("prompt-text").classList.contains("hidden") ? $("prompt-input") : $("prompt-text");
}


// extra: optional {label, run} — a caller-owned side action rendered LEFT of
// cancel/ok (the create-branch prompt's "use prefix…" lane). run() receives
// the CURRENT input value; it owns what happens next (typically: close this
// prompt, run a picker, reopen the prompt prefilled).
function openPrompt({ title, value, placeholder, onSubmit, extra, multiline }) {
  promptCb = onSubmit;
  promptExtraCb = extra ? extra.run : null;
  const xb = $("prompt-extra");
  xb.classList.toggle("hidden", !extra);
  if (extra) xb.textContent = extra.label;
  $("prompt-title").textContent = title;
  $("prompt-input").classList.toggle("hidden", !!multiline);
  $("prompt-text").classList.toggle("hidden", !multiline);
  // In a multiline prompt enter is a NEWLINE, so the confirm key has to be
  // spelled out — and it is ctrl+enter/ctrl+s, the TUI commit popup's key.
  $("prompt-hint").textContent = multiline
    ? "ctrl+enter (or ctrl+s) to confirm · esc to cancel"
    : "enter to confirm · esc to cancel";
  // Clear the field this prompt is NOT using. It is hidden, so its contents
  // are invisible — and an invisible leftover is what would be submitted if a
  // later prompt switched modes, or read back by anything inspecting the DOM.
  const idle = multiline ? $("prompt-input") : $("prompt-text");
  idle.value = "";
  const field = promptField();
  field.value = value || "";
  field.placeholder = placeholder || "";
  pushLayer("prompt", $("prompt"), { onKey: promptKey });
  field.focus();
  if (multiline) field.setSelectionRange(field.value.length, field.value.length);
  else field.select();
}


function closePrompt() {
  promptCb = null;
  promptExtraCb = null;
  $("prompt-extra").classList.add("hidden");
  // Blur before closing: the form-field guard keys off the focused element,
  // and a still-focused input would swallow every global key (the palette's
  // hard-won lesson).
  promptField().blur();
  $("prompt-text").classList.add("hidden"); // back to the one-line default
  $("prompt-input").classList.remove("hidden");
  closeLayer("prompt");
}


function submitPrompt() {
  const v = promptField().value.trim();
  if (!v) return; // nothing to submit; leave the prompt open
  const cb = promptCb; // capture before closing clears it
  closePrompt();
  if (cb) cb(v);
}


function promptKey(e) {
  const multi = promptField() === $("prompt-text");
  if (multi) {
    // ctrl+enter / ctrl+s confirm; a bare enter types a newline like any
    // other character, so the body of a message survives being edited.
    if ((e.key === "Enter" || e.key === "s") && (e.ctrlKey || e.metaKey)) {
      e.preventDefault();
      submitPrompt();
      return true;
    }
    if (e.key === "Escape") {
      closePrompt();
      return true;
    }
    return false;
  }
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
$("prompt-cancel").addEventListener("click", closePrompt);
$("prompt-extra").addEventListener("click", () => {
  const run = promptExtraCb;
  if (run) run(promptField().value);
});


$("help").addEventListener("click", () => closeLayer("help"));

$("help-box").addEventListener("click", (e) => e.stopPropagation()); // allow selecting/copying text
export { closeLayer, closePrompt, copyText, hideCtxMenu, layers, mountOverlay, openPrompt, promptCb, promptKey, pushLayer, showCtxMenu, submitPrompt, topLayer };
