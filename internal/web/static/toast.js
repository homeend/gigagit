// toast.js — part of gg's web client.
//
// A pop-up notice for "what you clicked cannot be opened": a bookmark whose
// commit was rebased away, a shelved commit whose original is gone. The op
// line at the foot of the page is the wrong place for it — it is one quiet
// line that the next op event overwrites, and a click that visibly does
// nothing is exactly what this exists to end.
//
// It stays TEN seconds (three is not enough to read a sha and decide what to
// do about it), hovering holds it, a click dismisses it. It takes no keys and
// is not a layer: whatever is open underneath keeps the keyboard, esc included.
import { esc } from "./core.js";

const TOAST_MS = 10000;

// This sheet has NO global .hidden rule — every surface hides itself by id —
// so the feature brings its own styles, the notifications.js convention.
const css = document.createElement("style");
css.textContent = `
#toasts { position: fixed; right: 18px; bottom: 44px; z-index: 40; display: flex; flex-direction: column; gap: 8px; max-width: min(520px, 90vw); }
#toasts .toast { background: var(--bg-alt); color: var(--fg); border: 1px solid var(--accent); border-left-width: 4px; border-radius: 4px; padding: 10px 14px; cursor: pointer; box-shadow: 0 4px 18px rgba(0,0,0,.45); }
#toasts .toast.err { border-color: #f27a6a; }
#toasts .toast .tdetail { color: var(--dim); font-size: 12px; margin-top: 4px; overflow-wrap: anywhere; }
`;
document.head.append(css);

const host = document.createElement("div");
host.id = "toasts";
host.setAttribute("role", "status");
host.setAttribute("aria-live", "polite");
document.body.append(host);

// toast shows text (and an optional dimmer detail line) for ten seconds.
// opts: { err: red border, detail: second line, ms: override the lifetime }.
function toast(text, opts) {
  const o = opts || {};
  const el = document.createElement("div");
  el.className = "toast" + (o.err ? " err" : "");
  el.innerHTML = `<div>${esc(text)}</div>` + (o.detail ? `<div class="tdetail">${esc(o.detail)}</div>` : "");
  let timer = 0;
  const arm = () => (timer = setTimeout(() => el.remove(), o.ms || TOAST_MS));
  el.addEventListener("click", () => {
    clearTimeout(timer);
    el.remove();
  });
  el.addEventListener("mouseenter", () => clearTimeout(timer));
  el.addEventListener("mouseleave", arm);
  host.append(el);
  arm();
  return el;
}

// entryGone is the one sentence for a stored entry that points at nothing —
// the same words domain.EntryGoneError gives the TUI.
function entryGone(what, detail) {
  return toast(what + " is no longer available", { err: true, detail });
}

export { entryGone, toast };
