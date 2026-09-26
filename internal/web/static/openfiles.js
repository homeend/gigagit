// openfiles.js — the ctrl+\ switcher (open files on the web, plan 5b): the
// worktree's open files, shared by every tab of this gg web, most recently
// shown first. ● marks the file THIS tab's viewer shows. enter brings one
// back where this tab left it, x closes it in every tab.
import { $, charWidth, elidePath, esc, getJSON, postJSON, tabId } from "./core.js";
import { closeLayer, mountOverlay, popFoot, pushFoot, pushLayer } from "./layers.js";
import { opLine } from "./ops.js";
import { dropViewer, openViewer, versionLabel, viewerFileId } from "./viewer.js";
import { registerHelp } from "./menus.js";

// --- switcher model (pure; guarded against Go) ---
function switcherRows(files, mine) {
  return files.map((f) => ({
    id: f.id,
    mark: f.id === mine ? "●" : "○",
    path: f.path,
    line: f.line > 0 ? ":" + f.line : "",
    source: f.source,
    rev: f.rev || "",
  }));
}

function clampSel(sel, n) {
  return n ? Math.min(Math.max(sel, 0), n - 1) : 0;
}
// --- end switcher model ---

const sw = { files: [], sel: 0 };
const root = mountOverlay("openfiles");
root.innerHTML = `<div id="openfiles-box"><div id="openfiles-title">Open files</div><div id="openfiles-list"></div></div>`;
root.addEventListener("click", (e) => {
  if (e.target.id === "openfiles") return closeSwitcher();
  const row = e.target.closest(".ofrow[data-i]");
  if (!row) return;
  sw.sel = Number(row.dataset.i);
  bringBack();
});

const SWITCHER_FOOT =
  `<span>↑↓ j k move</span><button data-oact="enter">enter bring back</button>` +
  `<button data-oact="x">x close file</button><button data-oact="esc">esc close</button>`;

$("foot").addEventListener("click", (e) => {
  const b = e.target.closest("button[data-oact]");
  if (!b) return;
  if (b.dataset.oact === "enter") bringBack();
  else if (b.dataset.oact === "x") closeSelected();
  else closeSwitcher();
});

function isSwitcherKey(e) {
  return e.ctrlKey && !e.altKey && !e.metaKey && (e.code === "Backslash" || e.key === "\\");
}

function isOpen() {
  return !root.classList.contains("hidden");
}

async function openSwitcher() {
  let body;
  try {
    body = await getJSON("/api/open-files");
  } catch (e) {
    return opLine("open files: " + (e.message || e), true);
  }
  sw.files = body.files || [];
  if (!sw.files.length) return opLine("no open files — view file on a file row opens one", false);
  const mine = viewerFileId();
  sw.sel = Math.max(0, sw.files.findIndex((f) => f.id === mine));
  root.style.bottom = $("foot").offsetHeight + "px";
  pushLayer("openfiles", root, { onKey: switcherKey });
  pushFoot("openfiles", SWITCHER_FOOT);
  renderSwitcher();
}

function closeSwitcher() {
  closeLayer("openfiles");
  popFoot("openfiles");
}

// switcherOpenFiles: the list changed while the switcher is up (another tab,
// an eviction, a stream ending) — re-render from it, never act on a stale row.
function switcherOpenFiles(files) {
  if (!isOpen()) return;
  const cur = sw.files[sw.sel] && sw.files[sw.sel].id;
  sw.files = files;
  if (!files.length) return closeSwitcher();
  const i = files.findIndex((f) => f.id === cur);
  sw.sel = clampSel(i >= 0 ? i : sw.sel, files.length);
  renderSwitcher();
}

function renderSwitcher() {
  const list = $("openfiles-list");
  const cols = Math.floor(list.clientWidth / charWidth()) - 4;
  const rows = switcherRows(sw.files, viewerFileId());
  list.innerHTML = rows
    .map((r, i) => {
      const meta = r.line + "  " + versionLabel(r.source, r.rev);
      const room = cols - 2 - meta.length;
      return (
        `<div class="ofrow${i === sw.sel ? " sel" : ""}" data-i="${i}" title="${esc(r.path)}">` +
        `${r.mark} ${esc(room > 0 ? elidePath(r.path, room) : r.path)}<span class="ofmeta">${esc(meta)}</span></div>`
      );
    })
    .join("");
  const cur = list.querySelector(".ofrow.sel");
  if (cur) cur.scrollIntoView({ block: "nearest" });
}

function bringBack() {
  const f = sw.files[sw.sel];
  if (!f) return;
  closeSwitcher();
  openViewer({ id: f.id });
}

// closeSelected: x closes the file in EVERY tab (ruling L4). This tab's own
// viewer on it goes first, so it does not read "closed in another tab".
async function closeSelected() {
  const f = sw.files[sw.sel];
  if (!f) return;
  if (f.id === viewerFileId()) dropViewer();
  let ans;
  try {
    ans = await postJSON("/api/open-files", { op: "close", id: f.id, tab: tabId, everywhere: true });
  } catch (e) {
    return opLine("close failed: " + (e.message || e), true);
  }
  sw.files = ans.files || [];
  if (!sw.files.length) return closeSwitcher();
  sw.sel = clampSel(sw.sel, sw.files.length);
  renderSwitcher();
}

function switcherKey(e) {
  switch (e.key) {
    case "ArrowDown": case "j": sw.sel = clampSel(sw.sel + 1, sw.files.length); renderSwitcher(); break;
    case "ArrowUp": case "k": sw.sel = clampSel(sw.sel - 1, sw.files.length); renderSwitcher(); break;
    case "Enter": bringBack(); break;
    case "x": closeSelected(); break;
    case "Escape": closeSwitcher(); break;
    default:
      if (!isSwitcherKey(e)) return true; // swallow: the popup owns the keyboard
      closeSwitcher(); // ctrl+\ again toggles it off
  }
  e.preventDefault();
  return true;
}

registerHelp({
  key: "open files",
  html:
    "files opened in the viewer stay open, one list per worktree shared by every tab: <b>ctrl+]</b> in the viewer sends the file to the background, " +
    "<b>ctrl+\\</b> lists the open files (<b>●</b> the one this tab shows) — <b>enter</b> brings one back where this tab left it, <b>x</b> closes it in every tab. " +
    "A working-tree file follows the disk.",
});

export { isSwitcherKey, openSwitcher, switcherOpenFiles };
