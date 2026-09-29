// Notes on a whole shelf entry: gg writes one when a shelved set cannot carry
// what it annotates (a recycled worktree's deletions and renames). The shelf
// sidebar marks such rows ◆N; this read-only dialog shows the notes. There is
// deliberately no way to write one from here.
import { $, esc, getJSON } from "./core.js";
import { closeLayer, mountOverlay, pushLayer } from "./layers.js";

// Own stylesheet (style.css has NO global .hidden rule — surfaces hide by id).
function injectStyle() {
  const css = `
#gg-shelf-notes.hidden { display: none; }
#gg-shelf-notes {
  position: fixed; inset: 0; z-index: 60; display: flex;
  align-items: center; justify-content: center; background: rgba(0,0,0,.45);
}
#gg-shelf-notes .box {
  background: var(--panel, #1b1d22); border: 1px solid var(--line, #3a3f47);
  border-radius: 6px; padding: 14px; min-width: 380px; max-width: min(760px, 92vw);
  max-height: 80vh; display: flex; flex-direction: column; gap: 10px;
}
#gg-shelf-notes h3 { margin: 0; font-size: 13px; font-weight: 600; }
#gg-shelf-notes .notes { overflow: auto; display: flex; flex-direction: column; gap: 12px; }
#gg-shelf-notes .summary { font-weight: 600; }
#gg-shelf-notes .meta { opacity: .65; font-size: 11px; }
#gg-shelf-notes pre { margin: 4px 0 0; white-space: pre; overflow-x: auto; }
#gg-shelf-notes .row { display: flex; justify-content: flex-end; align-items: center; gap: 8px; }
#gg-shelf-notes .hint { opacity: .7; font-size: 11px; margin-right: auto; }
`;
  const el = document.createElement("style");
  el.textContent = css;
  document.head.append(el);
}
injectStyle();

function buildDialog() {
  const el = mountOverlay("gg-shelf-notes");
  if (el.dataset.built) return el;
  el.dataset.built = "1";
  el.innerHTML =
    `<div class="box">` +
    `<h3 id="gg-shelf-notes-title"></h3>` +
    `<div class="notes" id="gg-shelf-notes-body"></div>` +
    `<div class="row"><span class="hint">esc closes</span>` +
    `<button id="gg-shelf-notes-close">close</button></div>` +
    `</div>`;
  el.addEventListener("click", (e) => {
    if (e.target === el) closeLayer("gg-shelf-notes"); // click outside the box
  });
  $("gg-shelf-notes-close").addEventListener("click", () => closeLayer("gg-shelf-notes"));
  return el;
}

function noteHTML(n) {
  const when = n.created ? new Date(n.created).toLocaleString() : "";
  const meta = [n.author, when].filter(Boolean).join(" · ");
  return (
    `<div class="note">` +
    `<div class="summary">${esc(n.summary)}</div>` +
    (meta ? `<div class="meta">${esc(meta)}</div>` : "") +
    (n.rationale ? `<pre>${esc(n.rationale)}</pre>` : "") +
    `</div>`
  );
}

// openShelfNotes shows entry e's notes (e is a /api/shelf row) — only the one
// with id noteId when given (a note row of the entry's file list).
async function openShelfNotes(e, label, noteId) {
  const got = await getJSON("/api/shelf/notes?id=" + encodeURIComponent(e.id)).catch(() => null);
  let notes = (got && got.notes) || [];
  if (noteId) notes = notes.filter((n) => n.id === noteId);
  const el = buildDialog();
  $("gg-shelf-notes-title").textContent = "Notes on " + label;
  $("gg-shelf-notes-body").innerHTML = notes.length ? notes.map(noteHTML).join("") : `<div class="meta">no notes</div>`;
  pushLayer("gg-shelf-notes", el, {
    onKey: (k) => {
      if (k.key === "Escape") {
        closeLayer("gg-shelf-notes");
        return true;
      }
      return false;
    },
  });
}

export { noteHTML, openShelfNotes };
