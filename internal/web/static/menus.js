// menus.js — part of gg's web client. Split from the original app.js;
// see app.js (the entry module) for the load order.
//
// Menu-row registry: the client half of the server's op registry.
//
// A right-click menu used to be a literal list built inside commits.js /
// files.js / sidebar.js, so every new feature edited one of those three files.
// With several features being built at once that is a conflict per merge, in
// the files where a dropped row is hardest to notice.
//
// A feature now contributes rows from its OWN module:
//
//   registerRows("commit", (c) => c.parents === 1
//     ? [{ label: "export as patch…", act: () => exportPatch(c) }]
//     : []);
//
// The callback runs every time the menu opens and receives that menu's
// context, so a row can gate itself on what is under the cursor — return an
// empty array and nothing is added. Rows land after the built-in ones, in
// registration order.
//
// Separators need no coordination: showCtxMenu collapses doubled separators,
// drops leading and trailing ones, and fences a lone red row on its own, so a
// registered row cannot leave the menu looking broken.
import { $ } from "./core.js";

const rowRegistry = new Map(); // menu key -> [fn]

// The menus a feature may extend. A typo in the key is a silent no-op
// otherwise, so it is checked against this list.
const MENUS = ["commit", "file", "branch", "worktree", "tag", "stash", "reflog", "remote", "menu"];

function registerRows(menu, fn) {
  if (!MENUS.includes(menu)) throw new Error("registerRows: unknown menu " + menu);
  if (typeof fn !== "function") throw new Error("registerRows(" + menu + "): not a function");
  if (!rowRegistry.has(menu)) rowRegistry.set(menu, []);
  rowRegistry.get(menu).push(fn);
}


// extraRows collects what every registered contributor offers for this context.
// A contributor that throws must not take the menu down with it: the rest of
// the rows still open, and the failure is reported once.
function extraRows(menu, ctx) {
  const out = [];
  for (const fn of rowRegistry.get(menu) || []) {
    try {
      const rows = fn(ctx);
      if (Array.isArray(rows)) out.push(...rows);
    } catch (e) {
      console.error("menu rows for " + menu + ":", e);
    }
  }
  return out;
}


// --- help registry ---------------------------------------------------------
// The ? overlay is 50 static rows in index.html, which has the same problem:
// every feature edits one file. registerHelp appends a row from the feature's
// own module at boot, after the static ones.
function registerHelp({ key, html }) {
  const box = $("help-box");
  if (!box) return;
  const row = document.createElement("div");
  row.className = "hrow";
  const k = document.createElement("span");
  k.className = "hkey";
  k.textContent = key;
  const v = document.createElement("span");
  v.innerHTML = html; // feature-authored copy, not user input
  row.append(k, v);
  box.append(row);
}

export { extraRows, registerHelp, registerRows };
